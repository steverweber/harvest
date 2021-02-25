package main

import (
	"strings"
	"goharvest2/poller/collector/plugin"
    "goharvest2/share/matrix"
)

type Headroom struct {
	*plugin.AbstractPlugin
}
func New(p *plugin.AbstractPlugin) plugin.Plugin {
	return &Headroom{AbstractPlugin: p}
}

func (p *Headroom) Run(data *matrix.Matrix) ([]*matrix.Matrix, error) {

	for _, instance := range data.GetInstances() {

        // no need to continue if labels are already parsed
        if instance.Labels.Get("aggr") != "" {
            break
        }

        name := instance.Labels.Get("headroom_aggr")

        // example name = DISK_SSD_aggr01_8a700cc6-068b-4a42-9a66-9d97f0e761c1
        // disk_type    = SSD
        // aggr         = aggr01

        if split := strings.Split(name, "_"); len(split) >= 3 {
            instance.Labels.Set("disk_type", split[1])
            instance.Labels.Set("aggr", strings.Join(split[2:len(split)-1], "_"))
        }
	}

	return nil, nil
}
