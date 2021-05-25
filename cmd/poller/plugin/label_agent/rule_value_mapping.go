/*
 * Copyright NetApp Inc, 2021 All rights reserved
 */

package label_agent

import (
	"fmt"
	"goharvest2/pkg/errors"
	"goharvest2/pkg/logger"
	"goharvest2/pkg/matrix"
	"strconv"
	"strings"
)

// generate metric and map instance label to a numeric value
type valueMappingRule struct {
	metric       string
	label        string
	defaultValue uint8
	hasDefault   bool
	mapping      map[string]uint8
}

// example rule:
// status state ok,pending,failed `8`
// will create a new metric "status" of type uint8
// if value of label "state" is any of ok,pending,failed
// the metric value will be respectively 0, 1 or 2

func parseValueMappingRule(ruleDefinition string) (labelRule, error) {
	if fields := strings.Fields(ruleDefinition); len(fields) == 3 || len(fields) == 4 {
		r := valueMappingRule{metric: fields[0], label: fields[1]}
		r.mapping = make(map[string]uint8)
		for i, v := range strings.Split(fields[2], ",") {
			r.mapping[v] = uint8(i)
		}

		if len(fields) == 4 {

			fields[3] = strings.TrimPrefix(strings.TrimSuffix(fields[3], "`"), "`")

			if v, err := strconv.ParseUint(fields[3], 10, 8); err != nil {
				return nil, errors.New(InvalidRuleDefinition, fmt.Sprintf("parse default value (%s): %v", fields[3], err))
			} else {
				r.hasDefault = true
				r.defaultValue = uint8(v)
			}
		}

		return &r, nil
	}

	return nil, errors.New(InvalidRuleDefinition, ruleDefinition)
}

func (r *valueMappingRule) toString() string {
	if r.hasDefault {
		return fmt.Sprintf("metric=%s, label=%s, default=%d, mapping=%v", r.metric, r.label, r.defaultValue, r.mapping)
	}
	return fmt.Sprintf("metric=%s, label=%s, mapping=%v", r.metric, r.label, r.mapping)

}

func (r *valueMappingRule) wantsInstance() bool {
	return false
}

func (r *valueMappingRule) runOnMatrix(m *matrix.Matrix, instance *matrix.Instance, key, prefix string) error {

	var (
		metric matrix.Metric
		err    error
	)

	if metric = m.GetMetric(r.metric); metric == nil {
		if metric, err = m.NewMetricUint8(r.metric); err != nil {
			logger.Error(prefix, "valueMapping: new metric (%s): %v", r.metric, err)
			return err
		}
		metric.SetProperty("mapping")
	}

	if v, ok := r.mapping[instance.GetLabel(r.label)]; ok {
		if err = metric.SetValueUint8(instance, v); err != nil {
			logger.Error(prefix, "ValueMapping: set value (%s) [%d]: %v", r.metric, v, err)
			return err
		}
		logger.Trace(prefix, "valueMapping: (%s) [%s] mapped (%s) value to %d", r.metric, key, instance.GetLabel(r.label), v)
	} else if r.hasDefault {
		if err = metric.SetValueUint8(instance, r.defaultValue); err != nil {
			logger.Error(prefix, "ValueMapping: set value (%s) [%d]: %v", r.metric, r.defaultValue, err)
			return err
		}
		logger.Trace(prefix, "valueMapping: [%s] [%s] mapped (%s) value to default %d", r.metric, key, instance.GetLabel(r.label), r.defaultValue)
	}

	return nil
}

func (r *valueMappingRule) runOnInstance(instance *matrix.Instance, prefix string) {
	// nothing to do
}
