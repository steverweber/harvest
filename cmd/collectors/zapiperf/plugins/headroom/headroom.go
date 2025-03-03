/*
 * Copyright NetApp Inc, 2021 All rights reserved
 */
package main

import (
	"goharvest2/cmd/poller/plugin"
	"goharvest2/pkg/matrix"
	"strings"
)

type Headroom struct {
	*plugin.AbstractPlugin
}

func New(p *plugin.AbstractPlugin) plugin.Plugin {
	return &Headroom{AbstractPlugin: p}
}

func (me *Headroom) Run(data *matrix.Matrix) ([]*matrix.Matrix, error) {

	for _, instance := range data.GetInstances() {

		// no need to continue if labels are already parsed
		if instance.GetLabel("aggr") != "" {
			break
		}

		name := instance.GetLabel("headroom_aggr")

		// example name = DISK_SSD_aggr01_8a700cc6-068b-4a42-9a66-9d97f0e761c1
		// disk_type    = SSD
		// aggr         = aggr01

		if split := strings.Split(name, "_"); len(split) >= 3 {
			instance.SetLabel("disk_type", split[1])
			instance.SetLabel("aggr", strings.Join(split[2:len(split)-1], "_"))
		}
	}

	return nil, nil
}

// Need to appease go build - see https://github.com/golang/go/issues/20312
func main() {}
