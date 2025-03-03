/*
 * Copyright NetApp Inc, 2021 All rights reserved
 */
package main

import (
	zapi "goharvest2/cmd/collectors/zapi/collector"
	"goharvest2/cmd/poller/collector"
)

func New(a *collector.AbstractCollector) collector.Collector {
	return zapi.New(a)
}

// Need to appease go build - see https://github.com/golang/go/issues/20312
func main() {}
