/*
 * Copyright NetApp Inc, 2021 All rights reserved
 */

// Package prometheus creates an HTTP end-point for Prometheus to scrape on `/metrics`
//It also publishes a list of available metrics for human consumption on `/`
package main

import (
	"bytes"
	"fmt"
	"goharvest2/pkg/set"
	"net/http"
	"strings"
	"time"
)

func (me *Prometheus) startHttpD(addr, port string) {

	mux := http.NewServeMux()
	mux.HandleFunc("/", me.ServeInfo)
	mux.HandleFunc("/metrics", me.ServeMetrics)

	me.Logger.Debug().Msgf("(httpd) starting server at [%s:%s]", addr, port)
	server := &http.Server{Addr: addr + ":" + port, Handler: mux}

	if err := server.ListenAndServe(); err != nil {
		me.Logger.Fatal().Msgf("(httpd) %v", err.Error())
	} else {
		me.Logger.Info().Msgf("(httpd) listening at [http://%s:%s]", addr, port)
	}
}

// checks if address is allowed access
// current implementation only checks for addresses, discarding ports
func (me *Prometheus) checkAddr(addr string) bool {
	if !me.checkAddrs {
		return true
	}

	if value, ok := me.cacheAddrs[addr]; ok {
		return value
	}

	addr = strings.TrimPrefix(addr, "http://")
	addr = strings.Split(addr, ":")[0]

	if me.allowAddrs != nil {
		for _, a := range me.allowAddrs {
			if a == addr {
				me.cacheAddrs[addr] = true
				return true
			}
		}
	}

	if me.allowAddrsRegex != nil {
		for _, r := range me.allowAddrsRegex {
			if r.MatchString(addr) {
				me.cacheAddrs[addr] = true
				return true
			}
		}
	}

	me.cacheAddrs[addr] = false
	return false
}

// send a deny request response
func (me *Prometheus) denyAccess(w http.ResponseWriter, r *http.Request) {

	me.Logger.Debug().Msgf("(httpd) denied request [%s] (%s)", r.RequestURI, r.RemoteAddr)
	w.WriteHeader(403)
	w.Header().Set("content-type", "text/plain")
	_, err := w.Write([]byte("403 Forbidden"))
	if err != nil {
		me.Logger.Error().Stack().Err(err).Msg("error")
	}
}

func (me *Prometheus) ServeMetrics(w http.ResponseWriter, r *http.Request) {

	var (
		data  [][]byte
		count int
	)

	start := time.Now()

	if !me.checkAddr(r.RemoteAddr) {
		me.denyAccess(w, r)
		return
	}

	me.Logger.Debug().Msgf("(httpd) serving request [%s] (%s)", r.RequestURI, r.RemoteAddr)

	me.cache.Lock()
	for _, metrics := range me.cache.Get() {
		data = append(data, metrics...)
		count += len(metrics)
	}
	me.cache.Unlock()

	// serve our own metadata
	// notice that some values are always taken from previous session
	if md, err := me.render(me.Metadata); err == nil {
		data = append(data, md...)
		count += len(md)
	} else {
		me.Logger.Error().Stack().Err(err).Msg("(httpd) render metadata")
	}
	/*

		e.Metadata.SetValueSS("count", "render", float64(count))

		if md, err := e.Render(e.Metadata); err == nil {
			data = append(data, md...)
		}*/

	w.WriteHeader(200)
	w.Header().Set("content-type", "text/plain")
	_, err := w.Write(bytes.Join(data, []byte("\n")))
	if err != nil {
		me.Logger.Error().Stack().Err(err).Msg("error")
	}

	me.Metadata.Reset()
	err = me.Metadata.LazySetValueInt64("time", "http", time.Since(start).Microseconds())
	if err != nil {
		me.Logger.Error().Stack().Err(err).Msg("error")
	}
	err = me.Metadata.LazySetValueInt("count", "http", count)
	if err != nil {
		me.Logger.Error().Stack().Err(err).Msg("error")
	}
}

// ServeInfo provides a human-friendly overview of metric types and source collectors
// this is done in a very inefficient way, by "reverse engineering" the metrics.
// That's probably ok, since we don't expect this to be called often.
func (me *Prometheus) ServeInfo(w http.ResponseWriter, r *http.Request) {
	// TODO: also add plugins and plugin metrics
	start := time.Now()

	if !me.checkAddr(r.RemoteAddr) {
		me.denyAccess(w, r)
		return
	}

	me.Logger.Debug().Msgf("(httpd) serving info request [%s] (%s)", r.RequestURI, r.RemoteAddr)

	body := make([]string, 0)

	numCollectors := 0
	numObjects := 0
	numMetrics := 0

	uniqueData := map[string]map[string][]string{}

	// copy cache so we don't lock it
	me.cache.Lock()
	cache := make(map[string][][]byte)
	for key, data := range me.cache.Get() {
		cache[key] = make([][]byte, len(data))
		copy(cache[key], data)
	}
	me.cache.Unlock()

	me.Logger.Debug().Msgf("(httpd) fetching %d cached elements", len(cache))

	for key, data := range cache {
		me.Logger.Debug().Msgf("(httpd) key => [%s] (%d)", key, len(data))
		var collector, object string

		if keys := strings.Split(key, "."); len(keys) == 2 {
			collector = keys[0]
			object = keys[1]
			me.Logger.Debug().Msgf("(httpd) collector [%s] - object [%s]", collector, object)
		} else {
			continue
		}

		// skip metadata
		if strings.HasPrefix(object, "metadata_") {
			continue
		}

		metricNames := set.New()
		for _, m := range data {
			if x := strings.Split(string(m), "{"); len(x) >= 2 && x[0] != "" {
				metricNames.Add(x[0])
			}
		}
		numMetrics += metricNames.Size()

		if _, exists := uniqueData[collector]; !exists {
			uniqueData[collector] = make(map[string][]string)
		}
		uniqueData[collector][object] = metricNames.Values()

	}

	for col, perObject := range uniqueData {
		objects := make([]string, 0)
		for obj, metricNames := range perObject {
			metrics := make([]string, 0)
			for _, m := range metricNames {
				if m != "" {
					metrics = append(metrics, fmt.Sprintf(metric_template, m))
				}
			}
			objects = append(objects, fmt.Sprintf(object_template, obj, strings.Join(metrics, "\n")))
			numObjects += 1
		}

		body = append(body, fmt.Sprintf(collector_template, col, strings.Join(objects, "\n")))
		numCollectors += 1
	}

	poller := me.Options.Poller
	bodyFlat := fmt.Sprintf(html_template, poller, poller, poller, numCollectors, numObjects, numMetrics, strings.Join(body, "\n\n"))

	w.WriteHeader(200)
	w.Header().Set("content-type", "text/html")
	_, err := w.Write([]byte(bodyFlat))
	if err != nil {
		me.Logger.Error().Stack().Err(err).Msg("error")
	}

	err = me.Metadata.LazyAddValueInt64("time", "info", time.Since(start).Microseconds())
	if err != nil {
		me.Logger.Error().Stack().Err(err).Msg("error")
	}
}
