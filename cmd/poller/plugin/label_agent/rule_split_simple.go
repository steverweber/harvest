/*
 * Copyright NetApp Inc, 2021 All rights reserved
 */

package label_agent

import (
	"fmt"
	"goharvest2/pkg/errors"
	"goharvest2/pkg/logger"
	"goharvest2/pkg/matrix"
	"strings"
)

// splits one label value into multiple labels using seperator symbol
type splitSimpleRule struct {
	sep     string
	source  string
	targets []string
}

// example rule:
// node `/` ,aggr,plex,disk
// if node="jamaica1/ag1/p1/d1", then:
// aggr="ag1", plex="p1", disk="d1"

func parseSplitSimpleRule(ruleDefinition string) (labelRule, error) {
	if fields := strings.SplitN(ruleDefinition, " `", 2); len(fields) == 2 {
		r := splitSimpleRule{source: strings.TrimSpace(fields[0])}
		if fields = strings.SplitN(fields[1], "` ", 2); len(fields) == 2 {
			r.sep = fields[0]
			if r.targets = strings.Split(fields[1], ","); len(r.targets) != 0 {
				return &r, nil
			}
		}
	}
	return nil, errors.New(InvalidRuleDefinition, ruleDefinition)
}

func (r *splitSimpleRule) toString() string {
	return fmt.Sprintf("sep=%s source=%s, targets=%v", r.sep, r.source, r.targets)
}

func (r *splitSimpleRule) wantsInstance() bool {
	return true
}

func (r *splitSimpleRule) runOnMatrix(m *matrix.Matrix, instance *matrix.Instance, key, prefix string) error {
	// nothing to do
	return nil
}

func (r *splitSimpleRule) runOnInstance(instance *matrix.Instance, prefix string) {
	if values := strings.Split(instance.GetLabel(r.source), r.sep); len(values) >= len(r.targets) {
		for i := range r.targets {
			if r.targets[i] != "" && values[i] != "" {
				instance.SetLabel(r.targets[i], values[i])
				logger.Trace(prefix, "splitSimple: (%s) [%s] => (%s) [%s]", r.source, instance.GetLabel(r.source), r.targets[i], values[i])
			}
		}
	}
}
