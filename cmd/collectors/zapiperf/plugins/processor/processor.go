package main

import (
    "goharvest2/cmd/poller/collector/plugin"
    "goharvest2/pkg/logger"
    "goharvest2/pkg/matrix"
    "strconv"
)

type Processor struct {
    *plugin.AbstractPlugin
}

func New(p *plugin.AbstractPlugin) plugin.Plugin {
    return &Processor{AbstractPlugin: p}
}

func (p *Processor) Run(data *matrix.Matrix) ([]*matrix.Matrix, error) {

    cpu_count := make(map[string]int)

    summary := matrix.New("processor", "processor_avg", "processor")
    summary.GlobalLabels = data.GlobalLabels
    summary.SetExportOptions(data.ExportOptions.Copy())

    for key, m := range data.GetMetrics() {
        if m.Enabled {
            if m.Labels != nil && m.Labels.Get("metric") == "idle" {
                summary.AddMetric(key, m.Name, false)
            } else {
                nm, _ := summary.AddMetric(key, m.Name, true)
                if m.Labels != nil {
                    nm.Labels = m.Labels
                }
            }
        }
    }

    for _, i := range data.GetInstances() {
        node := i.Labels.Get("node")
        if summary.GetInstance(node) == nil {
            if instance, err := summary.AddInstance(node); err == nil {
                instance.Labels.Set("node", node)
            } else {
                panic(err)
            }
        }
        cpu_count[node]++

    }

    if err := summary.InitData(); err != nil {
        return nil, err
    }

    for _, instance := range data.GetInstances() {

        node := instance.Labels.Get("node")

        if new_instance := summary.GetInstance(node); new_instance != nil {

            count, _ := cpu_count[node]
            logger.Debug(p.Prefix, "creating summary instance [%s] with %d CPUs", node, count)

            new_instance.Labels.Set("cpus", strconv.Itoa(count))

            for key, new_metric := range summary.GetMetrics() {

                if metric := data.GetMetric(key); metric != nil {

                    if value, ok := data.GetValue(metric, instance); ok {

                        if new_value, ok := summary.GetValue(new_metric, new_instance); ok {
                            summary.SetValue(new_metric, new_instance, new_value+value)
                        } else {
                            summary.SetValue(new_metric, new_instance, value)
                        }
                    }
                }
            }
        }
    }
    // normalize processor_busy by cpu_count

    for _, m := range summary.GetMetrics() {
        if m.Name == "busy" || m.Name == "domain_busy" {
            for _, i := range summary.GetInstances() {
                if v, ok := summary.GetValue(m, i); ok {
                    count, _ := cpu_count[i.Labels.Get("node")]
                    summary.SetValue(m, i, v/float64(count))
                }
            }
        }
    }

    return []*matrix.Matrix{summary}, nil
}
