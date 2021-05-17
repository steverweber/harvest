/*
	Copyright NetApp Inc, 2021 All rights reserved

	ZapiPerf collects and processes metrics from the "perf" APIs of the
	ZAPI protocol. This collector inherits some methods and fields of
	the Zapi collector (as they use the same protocol). However,
	ZapiPerf calculates final metric values from the deltas of two
	consecutive polls.

	The exact formula of doing these calculations, depends on the property
	of each counter and some counters require a "base-counter" additionally.

	Counter properties and other metadata are fetched from the target system
	during PollCounter() making the collector ONTAP-version independent.

	The collector maintains a cache of instances, updated periodically as well,
	during PollInstance().

	The source code prioritizes performance over simplicity/readability.
	Additionally, some objects (e.g. workloads) come with twists that
	force the collector to do acrobatics. Don't expect to easily
	comprehend what comes below.
*/
package main

import (
	"goharvest2/cmd/poller/collector"
	"goharvest2/pkg/color"
	"goharvest2/pkg/dict"
	"goharvest2/pkg/errors"
	"goharvest2/pkg/logger"
	"goharvest2/pkg/matrix"
	"goharvest2/pkg/set"
	"goharvest2/pkg/tree/node"
	"strconv"
	"strings"
	"time"

	// our parent collector
	zapi "goharvest2/cmd/collectors/zapi/collector"
)

// default parameter values
const (
	instanceKey   = "uuid"
	batchSize     = 500
	latencyIoReqd = 10
)

const BILLION = 1000000000

type ZapiPerf struct {
	*zapi.Zapi      // provides: AbstractCollector, Client, Object, Query, TemplateFn, TemplateType
	object          string
	batchSize       int
	latencyIoReqd   int
	instanceKey     string
	instanceLabels  map[string]string
	histogramLabels map[string][]string
	qosLabels       map[string]string
	isCacheEmpty    bool
}

// New returns a new ZapiPerf collector for one perf object
func New(a *collector.AbstractCollector) collector.Collector {
	return &ZapiPerf{Zapi: zapi.NewZapi(a)}
}

// Init initializes collector parameters and connects to target system
func (me *ZapiPerf) Init() error {

	if err := me.InitVars(); err != nil {
		return err
	}
	// Invoke generic initializer
	// this will load Schedule, initialize data and metadata Matrices
	if err := collector.Init(me); err != nil {
		return err
	}

	if err := me.InitMatrix(); err != nil {
		return err
	}

	if err := me.InitCache(); err != nil {
		return err
	}

	logger.Debug(me.Prefix, "initialized")
	return nil
}

func (me *ZapiPerf) InitCache() error {
	me.histogramLabels = make(map[string][]string)
	me.instanceLabels = make(map[string]string)
	me.instanceKey = me.loadParamStr("instance_key", instanceKey)
	me.batchSize = me.loadParamInt("batch_size", batchSize)
	me.latencyIoReqd = me.loadParamInt("latency_io_reqd", latencyIoReqd)
	me.isCacheEmpty = true
	me.object = me.loadParamStr("object", "")
	// hack to override from AbstractCollector
	// @TODO need cleaner solution
	if me.object == "" {
		me.object = me.Object
	}
	me.Matrix.Object = me.object
	logger.Debug(me.Prefix, "object= %s --> %s", me.Object, me.object)
	return nil
}

// load a string parameter use default
func (me *ZapiPerf) loadParamStr(name, defaultValue string) string {

	var x string

	if x = me.Params.GetChildContentS(name); x != "" {
		logger.Debug(me.Prefix, "using %s = [%s]", name, x)
		return x
	}
	logger.Debug(me.Prefix, "using %s = [%s] (default)", name, defaultValue)
	return defaultValue
}

// load an int parameter or use default value
func (me *ZapiPerf) loadParamInt(name string, defaultValue int) int {

	var (
		x string
		n int
		e error
	)

	if x = me.Params.GetChildContentS(name); x != "" {
		if n, e = strconv.Atoi(x); e == nil {
			logger.Debug(me.Prefix, "using %s = [%d]", name, n)
			return n
		}
		logger.Warn(me.Prefix, "invalid parameter %s = [%s] (expected integer)", name, x)
	}

	logger.Debug(me.Prefix, "using %s = [%d] (default)", name, defaultValue)
	return defaultValue
}

// PollData updates the data cache of the collector. During first poll, no data will
// be emitted. Afterwards, final metric values will be calculated from previous poll.
func (me *ZapiPerf) PollData() (*matrix.Matrix, error) {

	var (
		instanceKeys    []string
		resourceLatency matrix.Metric // for workload* objects
		err             error
	)

	logger.Debug(me.Prefix, "updating data cache")

	// clone matrix without numeric data
	newData := me.Matrix.Clone(false, true, true)
	newData.Reset()

	timestamp := newData.GetMetric("timestamp")
	if timestamp == nil {
		return nil, errors.New(errors.MISSING_PARAM, "timestamp metric") // @TODO errconfig??
	}

	// for updating metadata
	count := uint64(0)
	batchCount := 0
	apiT := 0 * time.Second
	parseT := 0 * time.Second

	// determine what will serve as instance key (either "uuid" or "instance")
	keyName := "instance-uuid"
	if me.instanceKey == "name" {
		keyName = "instance"
	}

	// list of instance keys (instance names or uuids) for which
	// we will request counter data
	if me.Query == "workload_detail" || me.Query == "workload_detail_volume" {
		if resourceMap := me.Params.GetChildS("resource_map"); resourceMap == nil {
			return nil, errors.New(errors.MISSING_PARAM, "resource_map")
		} else {
			instanceKeys = make([]string, 0)
			for _, layer := range resourceMap.GetAllChildNamesS() {
				for key := range me.Matrix.GetInstances() {
					instanceKeys = append(instanceKeys, key+"."+layer)
				}
			}
		}
	} else {
		instanceKeys = newData.GetInstanceKeys()
	}

	// build ZAPI request
	request := node.NewXmlS("perf-object-get-instances")
	request.NewChildS("objectname", me.Query)

	// load requested counters (metrics + labels)
	requestCounters := request.NewChildS("counters", "")
	// load scalar metrics
	for key, m := range newData.GetMetrics() {
		// skip histograms
		if !m.HasLabels() {
			requestCounters.NewChildS("counter", key)
		}
	}
	// load histograms
	for key := range me.histogramLabels {
		requestCounters.NewChildS("counter", key)
	}
	// load instance labels
	for key := range me.instanceLabels {
		requestCounters.NewChildS("counter", key)
	}

	// batch indices
	startIndex := 0
	endIndex := 0

	for endIndex < len(instanceKeys) {

		// update batch indices
		endIndex += me.batchSize
		if endIndex > len(instanceKeys) {
			endIndex = len(instanceKeys)
		}

		logger.Debug(me.Prefix, "starting batch poll for instances [%d:%d]", startIndex, endIndex)

		request.PopChildS(keyName + "s")
		requestInstances := request.NewChildS(keyName+"s", "")
		for _, key := range instanceKeys[startIndex:endIndex] {
			requestInstances.NewChildS(keyName, key)
		}

		startIndex = endIndex

		if err = me.Client.BuildRequest(request); err != nil {
			logger.Error(me.Prefix, "build request: %v", err)
			return nil, err
		}

		response, rd, pd, err := me.Client.InvokeWithTimers()
		if err != nil {

			if strings.Contains(err.Error(), "resource limit exceeded") && me.batchSize > 100 {
				logger.Error(me.Prefix, "%s", err.Error())
				logger.Error(me.Prefix, "changed batch_size to avoid this error (%d => %d)", me.batchSize, me.batchSize-100)
				me.batchSize -= 100
				return nil, nil
			}
			return nil, err
		}

		apiT += rd
		parseT += pd
		batchCount++

		// fetch instances
		instances := response.GetChildS("instances")
		if instances == nil || len(instances.GetChildren()) == 0 {
			err = errors.New(errors.ERR_NO_INSTANCE, "")
			break
		}

		logger.Debug(me.Prefix, "fetched batch with %d instances", len(instances.GetChildren()))

		// timestamp for batch instances
		// ignore timestamp from ZAPI which is always integer
		// we want float, since our poll interval can be float
		ts := float64(time.Now().UnixNano()) / BILLION

		for _, i := range instances.GetChildren() {

			key := i.GetChildContentS(me.instanceKey)
			layer := ""

			if me.Query == "workload_detail" || me.Query == "workload_detail_volume" {

				if x := strings.Split(key, "."); len(x) == 2 {
					key = x[0]
					layer = x[1]
				} else {
					logger.Warn(me.Prefix, "instance name [%s] has unexpected format", key)
					continue
				}

				if resourceLatency = newData.GetMetric(layer); resourceLatency == nil {
					logger.Warn(me.Prefix, "resource-latency metric [%s] missing in cache", layer)
					continue
				}
			}

			if key == "" {
				logger.Debug(me.Prefix, "skip instance, no key [%s] (name=%s, uuid=%s)", me.instanceKey, i.GetChildContentS("name"), i.GetChildContentS("uuid"))
				continue
			}

			instance := newData.GetInstance(key)
			if instance == nil {
				logger.Debug(me.Prefix, "skip instance [%s], not found in cache", key)
				continue
			}

			counters := i.GetChildS("counters")
			if counters == nil {
				logger.Debug(me.Prefix, "skip instance [%s], no data counters", key)
				continue
			}

			logger.Debug(me.Prefix, "fetching data of instance [%s] <%s>", key, instance.GetLabels().String())

			// add batch timestamp as custom counter
			if err := timestamp.SetValueFloat64(instance, ts); err != nil {
				logger.Error(me.Prefix, "timestamp set value: %v", err)
			}

			for _, cnt := range counters.GetChildren() {

				name := cnt.GetChildContentS("name")
				value := cnt.GetChildContentS("value")

				// sanity check
				// @TODO - redundant
				if name == "" || value == "" {
					logger.Debug(me.Prefix, "skipping incomplete counter [%s] with value [%s]", name, value)
					continue
				}

				logger.Trace(me.Prefix, "(%s%s%s%s) parsing counter (%s) = %v", color.Grey, key, color.End, layer, name, value)

				// ZAPI counter for us is either instance label (string)
				// or numeric metric (scalar or histogram)

				// string => instance label
				if display, has := me.instanceLabels[name]; has {
					instance.SetLabel(display, value)
					logger.Trace(me.Prefix, "+ label (%s) = [%s%s%s]", display, color.Yellow, value, color.End)
					continue
				}

				// histogram => convert to flat metrics
				if labels, has := me.histogramLabels[name]; has {

					values := strings.Split(value, ",")

					if len(labels) != len(values) {
						// warn & skip
						logger.Error(me.Prefix, "histogram (%s) labels don't match with parsed values [%s]", name, value)
						continue
					}

					for i, label := range labels {
						if metric := newData.GetMetric(name + "." + label); metric != nil {
							if err = metric.SetValueString(instance, values[i]); err != nil {
								logger.Error(me.Prefix, "set histogram (%s.%s) value [%s]: %v", name, label, values[i], err)
							} else {
								logger.Trace(me.Prefix, "+ histogram (%s.%s) = [%s%s%s]", name, label, color.Pink, values[i], color.End)
								count++
							}
						} else {
							logger.Warn(me.Prefix, "histogram (%s.%s) = [%s] not in cache", name, label, value)
						}
					}
					continue
				}

				// special case for workload_detail
				if me.Query == "workload_detail" || me.Query == "workload_detail_volume" {
					if name == "wait_time" || name == "service_time" {
						if err := resourceLatency.AddValueString(instance, value); err != nil {
							logger.Error(me.Prefix, "add resource-latency (%s) value [%s]: %v", name, value, err)
						} else {
							logger.Trace(me.Prefix, "++ resource-latency (%s) = [%s%s%s]", name, color.Blue, value, color.End)
							count++
						}
						continue
					}
					// "visits" are ignored
					if name == "visits" {
						continue
					}
				}

				// store as scalar metric
				if metric := newData.GetMetric(name); metric != nil {

					if err = metric.SetValueString(instance, value); err != nil {
						logger.Error(me.Prefix, "set metric (%s) value [%s]: %v", name, value, err)
					} else {
						logger.Trace(me.Prefix, "+ metric (%s) = [%s%s%s]", name, color.Cyan, value, color.End)
						count++
					}
					continue
				}

				logger.Warn(me.Prefix, "counter (%s) [%s] not found in cache", name, value)

			} // end loop over counters

		} // end loop over instances
	} // end batch request

	logger.Debug(me.Prefix, "collected %d data points in %d batch polls", count, batchCount)

	if me.Query == "workload_detail" || me.Query == "workload_detail_volume" {
		if rd, pd, err := me.GetParentOpsCounters(newData, keyName); err == nil {
			apiT += rd
			parseT += pd
		} else {
			// no point to continue as we can't calculate the other counters
			return nil, err
		}
	}

	// update metadata
	me.Metadata.LazySetValueInt64("api_time", "data", apiT.Microseconds())
	me.Metadata.LazySetValueInt64("parse_time", "data", parseT.Microseconds())
	me.Metadata.LazySetValueUint64("count", "data", count)
	me.AddCollectCount(count)

	/* Uncomment to debug metric values and get huge ugly output (only in foreground mode)
	logger.Debug(me.Prefix, ">> PREVIOUS MATRIX ~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~")
	me.Matrix.Print()

	logger.Debug(me.Prefix, ">>  CURRENT MATRIX ~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~")
	newData.Print()
	*/

	// skip calculating from delta if no data from previous poll
	if me.isCacheEmpty {
		logger.Debug(me.Prefix, "skip postprocessing until next poll (previous cache empty)")
		me.Matrix = newData
		me.isCacheEmpty = false
		return nil, nil
	}

	calcStart := time.Now()

	logger.Debug(me.Prefix, "starting delta calculations from previous cache")

	// cache raw data for next polls
	cachedData := newData.Clone(true, true, true) // @TODO implement copy data

	// order metrics, such that those requiring base counters are processed last
	orderedMetrics := make([]matrix.Metric, 0, len(newData.GetMetrics()))
	orderedKeys := make([]string, 0, len(orderedMetrics))

	for key, metric := range newData.GetMetrics() {
		if metric.GetComment() == "" { // does not require base counter
			orderedMetrics = append(orderedMetrics, metric)
			orderedKeys = append(orderedKeys, key)
		}
	}
	for key, metric := range newData.GetMetrics() {
		if metric.GetComment() != "" { // requires base counter
			orderedMetrics = append(orderedMetrics, metric)
			orderedKeys = append(orderedKeys, key)
		}
	}

	// calculate timestamp delta first since many counters require it for postprocessing
	// timestamp has "raw" property, so won't be postprocessed automatically
	if err = timestamp.Delta(me.Matrix.GetMetric("timestamp")); err != nil {
		logger.Error(me.Prefix, "(timestamp) calculate delta: %v", err)
		// @TODO terminate since other counters will be incorrect
	}

	var base matrix.Metric

	for i, metric := range orderedMetrics {

		property := metric.GetProperty()
		key := orderedKeys[i]

		// RAW - submit without posprocessing
		if property == "raw" {
			continue
		}

		// all other properties - first calculate delta
		if err = metric.Delta(me.Matrix.GetMetric(key)); err != nil {
			logger.Error(me.Prefix, "(%s) calculate delta: %v", key, err)
			continue
		}

		// DELTA - subtract previous value from current
		if property == "delta" {
			// already done
			continue
		}

		// RATE - delta, normalized by elapsed time
		if property == "rate" {
			// defer calculation, so we can first calculate averages/percents
			// Note: calculating rate before averages are averages/percentages are calculated
			// used to be a bug in Harvest 2.0 (Alpha, RC1, RC2) resulting in very high latency values
			continue
		}

		// For the next two properties we need base counters
		// We assume that delta of base counters is already calculated
		// (name of base counter is stored as Comment)
		if base = newData.GetMetric(metric.GetComment()); base == nil {
			logger.Warn(me.Prefix, "(%s) <%s> base counter (%s) missing", key, property, metric.GetComment())
			continue
		}

		// remaining properties: average and percent
		//
		// AVERAGE - delta, divided by base-counter delta
		//
		// PERCENT - average * 100
		// special case for latency counter: apply minimum number of iops as threshold
		if property == "average" || property == "percent" {

			if strings.HasSuffix(metric.GetName(), "latency") {
				err = metric.DivideWithThreshold(base, me.latencyIoReqd)
			} else {
				err = metric.Divide(base)
			}

			if err != nil {
				logger.Error(me.Prefix, "(%s) division by base: %v", key, err)
			}

			if property == "average" {
				continue
			}
		}

		if property == "percent" {
			if err = metric.MultiplyByScalar(100); err != nil {
				logger.Error(me.Prefix, "(%s) multiply by scalar: %v", key, err)
			}
			continue
		}

		logger.Error(me.Prefix, "(%s) unknown property: %s", key, property)
	}

	// calculate rates (which we deferred to calculate averages/percents first)
	for i, metric := range orderedMetrics {
		if metric.GetProperty() == "rate" {
			if err = metric.Divide(timestamp); err != nil {
				logger.Error(me.Prefix, "(%s) calculate rate: %v", orderedKeys[i], err)
			}
		}
	}

	me.Metadata.LazySetValueInt64("calc_time", "data", time.Since(calcStart).Microseconds())

	// store cache for next poll
	me.Matrix = cachedData

	/* Uncomment to debug metric values
	logger.Debug(me.Prefix, ">> RESULTING MATRIX ~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~")
	newData.Print()
	*/

	return newData, nil
}

// Poll counter "ops" of the rlated/parent object, required for objects
// workload_detail and workload_detail_volume. This counter is already
// collected by the other ZapiPerf collectors, so this poll is redundant
// (until we implement some sort of inter-collector communication).
func (me *ZapiPerf) GetParentOpsCounters(data *matrix.Matrix, KeyAttr string) (time.Duration, time.Duration, error) {

	var (
		ops          matrix.Metric
		object       string
		instanceKeys []string
		apiT, parseT time.Duration
		err          error
	)

	if me.Query == "workload_detail" {
		object = "workload"
	} else {
		object = "workload_volume"
	}

	logger.Debug(me.Prefix, "(%s) starting redundancy poll for ops from parent object (%s)", me.Query, object)

	apiT = 0 * time.Second
	parseT = 0 * time.Second

	if ops = data.GetMetric("ops"); ops == nil {
		logger.Error(me.Prefix, "ops counter not found in cache")
		return apiT, parseT, errors.New(errors.MISSING_PARAM, "counter ops")
	}

	instanceKeys = data.GetInstanceKeys()

	// build ZAPI request
	request := node.NewXmlS("perf-object-get-instances")
	request.NewChildS("objectname", object)

	requestCounters := request.NewChildS("counters", "")
	requestCounters.NewChildS("counter", "ops")

	// batch indices
	startIndex := 0
	endIndex := 0

	count := 0

	for endIndex < len(instanceKeys) {

		// update batch indices
		endIndex += me.batchSize
		if endIndex > len(instanceKeys) {
			endIndex = len(instanceKeys)
		}

		logger.Debug(me.Prefix, "starting batch poll for instances [%d:%d]", startIndex, endIndex)

		request.PopChildS(KeyAttr + "s")
		requestInstances := request.NewChildS(KeyAttr+"s", "")
		for _, key := range instanceKeys[startIndex:endIndex] {
			requestInstances.NewChildS(KeyAttr, key)
		}

		startIndex = endIndex

		if err = me.Client.BuildRequest(request); err != nil {
			return apiT, parseT, err
		}

		response, rt, pt, err := me.Client.InvokeWithTimers()
		if err != nil {
			return apiT, parseT, err
		}

		apiT += rt
		parseT += pt

		// fetch instances
		instances := response.GetChildS("instances")
		if instances == nil || len(instances.GetChildren()) == 0 {
			return apiT, parseT, err
		}

		for _, i := range instances.GetChildren() {

			key := i.GetChildContentS(me.instanceKey)

			if key == "" {
				logger.Debug(me.Prefix, "skip instance, no key [%s] (name=%s, uuid=%s)", me.instanceKey, i.GetChildContentS("name"), i.GetChildContentS("uuid"))
				continue
			}

			instance := data.GetInstance(key)
			if instance == nil {
				logger.Warn(me.Prefix, "skip instance [%s], not found in cache", key)
				continue
			}

			counters := i.GetChildS("counters")
			if counters == nil {
				logger.Debug(me.Prefix, "skip instance [%s], no data counters", key)
				continue
			}

			for _, cnt := range counters.GetChildren() {

				name := cnt.GetChildContentS("name")
				value := cnt.GetChildContentS("value")

				logger.Trace(me.Prefix, "(%s%s%s%s) parsing counter = %v", color.Grey, key, color.End, name, value)

				if name == "ops" {
					if err = ops.SetValueString(instance, value); err != nil {
						logger.Error(me.Prefix, "set metric (%s) value [%s]: %v", name, value, err)
					} else {
						logger.Trace(me.Prefix, "+ metric (%s) = [%s%s%s]", name, color.Cyan, value, color.End)
						count++
					}
				} else {
					logger.Error(me.Prefix, "unrequested metric (%s)", name)
				}
			}
		}
	}
	logger.Debug(me.Prefix, "(%s) completed redundant ops poll (%s): collected %d", me.Query, object, count)
	return apiT, parseT, nil
}

// Update counter metadata cache
func (me *ZapiPerf) PollCounter() (*matrix.Matrix, error) {

	var (
		err                                      error
		request, response, counterList           *node.Node
		oldMetrics, oldLabels, replaced, missing *set.Set
		wanted                                   *dict.Dict
		oldMetricsSize, oldLabelsSize            int
		counters                                 map[string]*node.Node
	)

	counters = make(map[string]*node.Node)
	oldMetrics = set.New() // current set of metrics, so we can remove from matrix if not updated
	oldLabels = set.New()  // current set of labels
	wanted = dict.New()    // counters listed in template, maps raw name to display name
	missing = set.New()    // required base counters, missing in template
	replaced = set.New()   // deprecated and replaced counters

	for key := range me.Matrix.GetMetrics() {
		oldMetrics.Add(key)
	}
	oldMetricsSize = oldMetrics.Size()

	for key := range me.instanceLabels {
		oldLabels.Add(key)
	}
	oldLabelsSize = oldLabels.Size()

	// parse list of counters defined in template
	if counterList = me.Params.GetChildS("counters"); counterList != nil {
		for _, cnt := range counterList.GetAllChildContentS() {
			if renamed := strings.Split(cnt, "=>"); len(renamed) == 2 {
				wanted.Set(strings.TrimSpace(renamed[0]), strings.TrimSpace(renamed[1]))
			} else if cnt == "instance_name" {
				wanted.Set(cnt, me.object)
			} else {
				display := strings.ReplaceAll(cnt, "-", "_")
				if strings.HasPrefix(display, me.object) {
					display = strings.TrimPrefix(display, me.object)
					display = strings.TrimPrefix(display, "_")
				}
				wanted.Set(cnt, display)
			}
		}
	} else {
		return nil, errors.New(errors.MISSING_PARAM, "counters")
	}

	logger.Debug(me.Prefix, "updating metric cache (old cache has %d metrics and %d labels", oldMetrics.Size(), oldLabels.Size())

	// build request
	request = node.NewXmlS("perf-object-counter-list-info")
	request.NewChildS("objectname", me.Query)

	if err = me.Client.BuildRequest(request); err != nil {
		return nil, err
	}

	if response, err = me.Client.Invoke(); err != nil {
		return nil, err
	}

	// fetch counter elements
	if elems := response.GetChildS("counters"); elems != nil && len(elems.GetChildren()) != 0 {
		for _, counter := range elems.GetChildren() {
			if name := counter.GetChildContentS("name"); name != "" {
				counters[name] = counter
			}
		}
	} else {
		return nil, errors.New(errors.ERR_NO_METRIC, "no counters in response")
	}

	for key, counter := range counters {

		// override counter properties from template
		if p := me.GetOverride(key); p != "" {
			logger.Debug(me.Prefix, "%soverride counter [%s] properties with [%s] => [%s]%s", color.Red, key, counter.GetChildContentS("properties"), p, color.End)
			counter.SetChildContentS("properties", p)
		}

		display, ok := wanted.GetHas(key)
		// counter not requested
		if !ok {
			logger.Trace(me.Prefix, "%sskip [%s], not requested%s", color.Grey, key, color.End)
			continue
		}

		// deprecated and possibly replaced counter
		if counter.GetChildContentS("is-deprecated") == "true" {
			if r := counter.GetChildContentS("replaced-by"); r != "" {
				logger.Info(me.Prefix, "replaced deprecated counter [%s] with [%s]", key, r)
				if !wanted.Has(r) {
					replaced.Add(r)
				}
			} else {
				logger.Info(me.Prefix, "skip [%s], deprecated", key)
				continue
			}
		}

		// string metric, add as instance label
		if strings.Contains(counter.GetChildContentS("properties"), "string") {
			oldLabels.Delete(key)
			if key == "instance_name" && display == "" {
				display = me.object
			}
			me.instanceLabels[key] = display
			logger.Debug(me.Prefix, "%s+[%s] added as label name (%s)%s", color.Yellow, key, display, color.End)
		} else {
			// add counter as numeric metric
			oldMetrics.Delete(key)
			if r := me.addCounter(counter, key, display, true, counters); r != "" && !wanted.Has(r) {
				missing.Add(r) // required base counter, missing in template
				logger.Debug(me.Prefix, "%smarking [%s] as required base counter for [%s]%s", color.Red, r, key, color.End)
			}
		}
	}

	// second loop for replaced counters
	if replaced.Size() > 0 {
		logger.Debug(me.Prefix, "attempting to retrieve metadata of %d replaced counters", replaced.Size())
		for name, counter := range counters {
			if replaced.Has(name) {
				oldMetrics.Delete(name)
				logger.Debug(me.Prefix, "adding [%s] (replacment for deprecated counter)", name)
				if r := me.addCounter(counter, name, name, true, counters); r != "" && !wanted.Has(r) {
					missing.Add(r) // required base counter, missing in template
					logger.Debug(me.Prefix, "%smarking [%s] as required base counter for [%s]%s", color.Red, r, name, color.End)
				}
			}
		}
	}

	// third loop for required base counters, not in template
	if missing.Size() > 0 {
		logger.Debug(me.Prefix, "attempting to retrieve metadata of %d missing base counters", missing.Size())
		for name, counter := range counters {
			//logger.Debug(me.Prefix, "%shas??? [%s]%s", color.Grey, name, color.End)
			if missing.Has(name) {
				oldMetrics.Delete(name)
				logger.Debug(me.Prefix, "adding [%s] (missing base counter)", name)
				me.addCounter(counter, name, "", false, counters)
			}
		}
	}

	// @TODO check dtype!!!!
	// Create an artificial metric to hold timestamp of each instance data.
	// The reason we don't keep a single timestamp for the whole data
	// is because we might get instances in different batches
	if !oldMetrics.Has("timestamp") {
		m, err := me.Matrix.NewMetricFloat64("timestamp")
		if err != nil {
			logger.Error(me.Prefix, "add timestamp metric: %v", err)
		}
		m.SetProperty("raw")
		m.SetExportable(false)
	}

	// hack for workload objects, @TODO replace with a plugin
	if me.Query == "workload" || me.Query == "workload_detail" || me.Query == "workload_volume" || me.Query == "workload_detail_volume" {

		// for these two objects, we need to create latency/ops counters for each of the workload layers
		// there original counters will be discarded
		if me.Query == "workload_detail" || me.Query == "workload_detail_volume" {

			var service, wait, visits, ops matrix.Metric
			oldMetrics.Delete("service_time")
			oldMetrics.Delete("wait_time")
			oldMetrics.Delete("visits")
			oldMetrics.Delete("ops")

			if service = me.Matrix.GetMetric("service_time"); service == nil {
				logger.Error(me.Prefix, "metric [service_time] required to calculate workload missing")
			}

			if wait = me.Matrix.GetMetric("wait_time"); wait == nil {
				logger.Error(me.Prefix, "metric [wait-time] required to calculate workload missing")
			}

			if visits = me.Matrix.GetMetric("visits"); visits == nil {
				logger.Error(me.Prefix, "metric [visits] required to calculate workload missing")
			}

			if service == nil || wait == nil || visits == nil {
				return nil, errors.New(errors.MISSING_PARAM, "workload metrics")
			}

			if ops = me.Matrix.GetMetric("ops"); ops == nil {
				if ops, err = me.Matrix.NewMetricFloat64("ops"); err != nil {
					return nil, err
				}
				ops.SetProperty(visits.GetProperty())
				logger.Debug(me.Prefix, "+ [resource_ops] [%s] added workload ops metric with property", ops.GetName(), ops.GetProperty())
			}

			service.SetExportable(false)
			wait.SetExportable(false)
			visits.SetExportable(false)

			if resourceMap := me.Params.GetChildS("resource_map"); resourceMap == nil {
				return nil, errors.New(errors.MISSING_PARAM, "resource_map")
			} else {
				for _, x := range resourceMap.GetChildren() {
					name := x.GetNameS()
					resource := x.GetContentS()

					if m, err := me.Matrix.NewMetricFloat64(name); err != nil {
						return nil, err
					} else {
						m.SetName("resource_latency")
						m.SetLabel("resource", resource)
						m.SetProperty(service.GetProperty())
						// base counter is the ops of the same resource
						m.SetComment("ops")

						oldMetrics.Delete(name)
						logger.Debug(me.Prefix, "+ [%s] (=> %s) added workload latency metric", name, resource)
					}
				}
			}
		}

		if qosLabels := me.Params.GetChildS("qos_labels"); qosLabels == nil {
			return nil, errors.New(errors.MISSING_PARAM, "qos_labels")
		} else {
			me.qosLabels = make(map[string]string)
			for _, label := range qosLabels.GetAllChildContentS() {

				display := strings.ReplaceAll(label, "-", "_")
				if x := strings.Split(label, "=>"); len(x) == 2 {
					label = strings.TrimSpace(x[0])
					display = strings.TrimSpace(x[1])
				}
				me.qosLabels[label] = display
				//me.instanceLabels[label] = display
				//oldLabels.Delete(label)
			}
		}
	}

	for key := range oldMetrics.Iter() {
		// temporary fix: prevent removing array counters
		// @TODO
		if key != "timestamp" && !strings.Contains(key, ".") {
			me.Matrix.RemoveMetric(key)
			logger.Debug(me.Prefix, "removed metric [%s]", key)
		}
	}

	for key := range oldLabels.Iter() {
		delete(me.instanceLabels, key)
		logger.Debug(me.Prefix, "removed label [%s]", key)
	}

	metricsAdded := len(me.Matrix.GetMetrics()) - (oldMetricsSize - oldMetrics.Size())
	labelsAdded := len(me.instanceLabels) - (oldLabelsSize - oldLabels.Size())

	logger.Debug(me.Prefix, "added %d new, removed %d metrics (total: %d)", metricsAdded, oldMetrics.Size(), len(me.Matrix.GetMetrics()))
	logger.Debug(me.Prefix, "added %d new, removed %d labels (total: %d)", labelsAdded, oldLabels.Size(), len(me.instanceLabels))

	if len(me.Matrix.GetMetrics()) == 0 {
		return nil, errors.New(errors.ERR_NO_METRIC, "")
	}

	return nil, nil
}

// create a metric for a Zapi counter
func (me *ZapiPerf) addCounter(counter *node.Node, name, display string, enabled bool, cache map[string]*node.Node) string {

	var (
		property, baseCounter, unit string
		err                         error
	)

	p := counter.GetChildContentS("properties")
	if strings.Contains(p, "raw") {
		property = "raw"
	} else if strings.Contains(p, "delta") {
		property = "delta"
	} else if strings.Contains(p, "rate") {
		property = "rate"
	} else if strings.Contains(p, "average") {
		property = "average"
	} else if strings.Contains(p, "percent") {
		property = "percent"
	} else {
		logger.Warn(me.Prefix, "skip counter [%s] with unknown property [%s]", name, p)
		return ""
	}

	baseCounter = counter.GetChildContentS("base-counter")
	unit = counter.GetChildContentS("unit")

	if display == "" {
		display = strings.ReplaceAll(name, "-", "_") // redundant for zapiperf
	}

	logger.Debug(me.Prefix, "handling counter [%s] with property [%s] and unit [%s]", name, property, unit)

	// counter type is array, each element will be converted to a metric instance
	if counter.GetChildContentS("type") == "array" {

		var labels, baseLabels []string
		var e string

		if labels, e = parseHistogramLabels(counter); e != "" {
			logger.Warn(me.Prefix, "skipping [%s] of type array: %s", name, e)
			return ""
		}

		if baseCounter != "" {
			if base, ok := cache[baseCounter]; ok {
				if base.GetChildContentS("type") == "array" {
					baseLabels, e = parseHistogramLabels(base)
					if e != "" {
						logger.Warn(me.Prefix, "skipping [%s], base counter [%s] is array, but %s", name, baseCounter, e)
						return ""
					} else if len(baseLabels) != len(labels) {
						logger.Warn(me.Prefix, "skipping [%s], array labels don't match with base counter labels [%s]", name, baseCounter)
						return ""
					}
				}
			} else {
				logger.Warn(me.Prefix, "skipping [%s], base counter [%s] not found", name, baseCounter)
				return ""
			}
		}

		for _, label := range labels {

			var m matrix.Metric

			key := name + "." + label
			baseKey := baseCounter
			if baseCounter != "" && len(baseLabels) != 0 {
				baseKey += "." + baseLabels[0]
			}

			if m = me.Matrix.GetMetric(key); m != nil {
				logger.Debug(me.Prefix, "updating array metric [%s] attributes", key)
			} else if m, err = me.Matrix.NewMetricFloat64(key); err == nil {
				logger.Debug(me.Prefix, "%s+[%s] added array metric (%s), element with label (%s)%s", color.Pink, name, display, label, color.End)
			} else {
				logger.Error(me.Prefix, "add array metric element [%s]: %v", key, err)
				return ""
			}

			m.SetName(display)
			m.SetProperty(property)
			m.SetComment(baseKey)
			m.SetExportable(enabled)

			if x := strings.Split(label, "."); len(x) == 2 {
				m.SetLabel("metric", x[0])
				m.SetLabel("submetric", x[1])
			} else {
				m.SetLabel("metric", label)
			}
		}
		// cache labels only when parsing counter was success
		me.histogramLabels[name] = labels

		// counter type is scalar
	} else {
		var m matrix.Metric
		if m = me.Matrix.GetMetric(name); m != nil {
			logger.Debug(me.Prefix, "updating scalar metric [%s] attributes", name)
		} else if m, err = me.Matrix.NewMetricFloat64(name); err == nil {
			logger.Debug(me.Prefix, "%s+[%s] added scalar metric (%s)%s", color.Cyan, name, display, color.End)
		} else {
			logger.Error(me.Prefix, "add scalar metric [%s]: %v", name, err)
			return ""
		}

		m.SetName(display)
		m.SetProperty(property)
		m.SetComment(baseCounter)
		m.SetExportable(enabled)

	}
	return baseCounter
}

// override counter property
func (me *ZapiPerf) GetOverride(counter string) string {
	if o := me.Params.GetChildS("override"); o != nil {
		return o.GetChildContentS(counter)
	}
	return ""
}

// parse ZAPI array counter (histogram), so we can store it
// as multiple flat metrics
func parseHistogramLabels(elem *node.Node) ([]string, string) {
	var (
		labels []string
		msg    string
	)

	if x := elem.GetChildS("labels"); x == nil {
		msg = "array labels missing"
	} else if d := len(x.GetChildren()); d == 1 {
		labels = strings.Split(node.DecodeHtml(x.GetChildren()[0].GetContentS()), ",")
	} else if d == 2 {
		labelsA := strings.Split(node.DecodeHtml(x.GetChildren()[0].GetContentS()), ",")
		labelsB := strings.Split(node.DecodeHtml(x.GetChildren()[1].GetContentS()), ",")
		for _, a := range labelsA {
			for _, b := range labelsB {
				labels = append(labels, a+"."+b)
			}
		}
	} else {
		msg = "unexpected dimensions"
	}

	return labels, msg
}

// Update instance cache
func (me *ZapiPerf) PollInstance() (*matrix.Matrix, error) {

	var (
		err                                        error
		request, results                           *node.Node
		oldInstances                               *set.Set
		oldSize, newSize, removed, added           int
		keyAttr, instancesAttr, nameAttr, uuidAttr string
	)

	oldInstances = set.New()
	for key := range me.Matrix.GetInstances() {
		oldInstances.Add(key)
	}
	oldSize = oldInstances.Size()

	logger.Debug(me.Prefix, "updating instance cache (old cache has: %d)", oldInstances.Size())

	nameAttr = "name"
	uuidAttr = "uuid"
	keyAttr = me.instanceKey

	// hack for workload objects: get instances from Zapi
	if me.Query == "workload" || me.Query == "workload_detail" || me.Query == "workload_volume" || me.Query == "workload_detail_volume" {
		request = node.NewXmlS("qos-workload-get-iter")
		queryElem := request.NewChildS("query", "")
		infoElem := queryElem.NewChildS("qos-workload-info", "")
		if me.Query == "workload_volume" || me.Query == "workload_detail_volume" {
			infoElem.NewChildS("workload-class", "autovolume")
		} else {
			infoElem.NewChildS("workload-class", "user-defined")
		}

		instancesAttr = "attributes-list"
		nameAttr = "workload-name"
		uuidAttr = "workload-uuid"
		if me.instanceKey == "instance_name" || me.instanceKey == "name" {
			keyAttr = "workload-name"
		} else {
			keyAttr = "workload-uuid"
		}
		// syntax for cdot/perf
	} else if me.Client.IsClustered() {
		request = node.NewXmlS("perf-object-instance-list-info-iter")
		request.NewChildS("objectname", me.Query)
		instancesAttr = "attributes-list"
		// syntax for 7mode/perf
	} else {
		request = node.NewXmlS("perf-object-instance-list-info")
		request.NewChildS("objectname", me.Query)
		instancesAttr = "instances"
	}

	if me.Client.IsClustered() {
		request.NewChildS("max-records", strconv.Itoa(me.batchSize))
	}

	batchTag := "initial"

	for {

		if results, batchTag, err = me.Client.InvokeBatchRequest(request, batchTag); err != nil {
			logger.Error(me.Prefix, "instance request: %v", err)
			break
		}

		if results == nil {
			break
		}

		// fetch instances
		instances := results.GetChildS(instancesAttr)
		if instances == nil || len(instances.GetChildren()) == 0 {
			break
		}

		for _, i := range instances.GetChildren() {

			if key := i.GetChildContentS(keyAttr); key == "" {
				// instance key missing
				name := i.GetChildContentS(nameAttr)
				uuid := i.GetChildContentS(uuidAttr)
				logger.Debug(me.Prefix, "skip instance, missing key [%s] (name=%s, uuid=%s)", keyAttr, name, uuid)
			} else if oldInstances.Delete(key) {
				// instance already in cache
				logger.Debug(me.Prefix, "updated instance [%s%s%s%s]", color.Bold, color.Yellow, key, color.End)
				continue
			} else if instance, err := me.Matrix.NewInstance(key); err != nil {
				logger.Error(me.Prefix, "add instance [%s]: %v", key, err)
			} else {
				logger.Debug(me.Prefix, "added new instance [%s%s%s%s]", color.Bold, color.Yellow, key, color.End)
				if me.Query == "workload" || me.Query == "workload_detail" || me.Query == "workload_volume" || me.Query == "workload_detail_volume" {
					for label, display := range me.qosLabels {
						if value := i.GetChildContentS(label); value != "" {
							instance.SetLabel(display, value)
							// @TODO cleanup redundant logging
							//logger.Debug(me.Prefix, "(%s) [%s] added QOS label (%s) => [%s]", me.Query, key, display, value)
						} else {
							//logger.Debug(me.Prefix, "(%s) [%s] skip QOS label (%s) - no value", me.Query, key, display)
						}
					}
					logger.Debug(me.Prefix, "(%s) [%s] added QOS labels: %s", me.Query, key, instance.GetLabels().String())
				}
			}
		}
	}

	for key := range oldInstances.Iter() {
		me.Matrix.RemoveInstance(key)
		logger.Debug(me.Prefix, "removed instance [%s]", key)
	}

	removed = oldInstances.Size()
	newSize = len(me.Matrix.GetInstances())
	added = newSize - (oldSize - removed)

	logger.Debug(me.Prefix, "added %d new, removed %d (total instances %d)", added, removed, newSize)

	if newSize == 0 {
		return nil, errors.New(errors.ERR_NO_INSTANCE, "")
	}

	return nil, err
}

// Need to appease go build - see https://github.com/golang/go/issues/20312
func main() {}
