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

// replace in source label, if present, and add as new label
type replaceSimpleRule struct {
	source string
	target string
	old    string
	new    string
}

// example rule:
// node node_short `node_` ``
// if node="node_jamaica1"; then:
// node_short="jamaica1"

func parseReplaceSimpleRule(ruleDefinition string) (labelRule, error) {
	if fields := strings.SplitN(ruleDefinition, " `", 3); len(fields) == 3 {
		if labels := strings.Fields(fields[0]); len(labels) == 2 {
			r := replaceSimpleRule{source: labels[0], target: labels[1]}
			r.old = strings.TrimSuffix(fields[1], "`")
			r.new = strings.TrimSuffix(fields[2], "`")
			return &r, nil
		}
	}
	return nil, errors.New(InvalidRuleDefinition, ruleDefinition)
}

func (r *replaceSimpleRule) toString() string {
	return fmt.Sprintf("source=%s, target=%s old=%s new=%s", r.source, r.target, r.old, r.new)
}

func (r *replaceSimpleRule) wantsInstance() bool {
	return true
}

func (r *replaceSimpleRule) runOnMatrix(m *matrix.Matrix, instance *matrix.Instance, key, prefix string) error {
	// nothing to do
	return nil
}

func (r *replaceSimpleRule) runOnInstance(instance *matrix.Instance, prefix string) {
	if old := instance.GetLabel(r.source); old != "" {
		if value := strings.ReplaceAll(old, r.old, r.new); value != old {
			instance.SetLabel(r.target, value)
			logger.Trace(prefix, "replaceSimple: (%s) [%s] => (%s) [%s]", r.source, old, r.target, value)
		}
	}
}
