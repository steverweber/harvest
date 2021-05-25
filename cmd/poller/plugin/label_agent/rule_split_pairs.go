/*
 * Copyright NetApp Inc, 2021 All rights reserved
 */

package label_agent

import (
	"fmt"
	"goharvest2/pkg/errors"
	"goharvest2/pkg/matrix"
	"strings"
)

// splits one label value into multiple key-value pairs
type splitPairsRule struct {
	source string
	sep1   string
	sep2   string
}

// example rule:
// node ` ` `:`
// will use single space to extract pairs
// will use colon to extract key-value
func parseSplitPairsRule(ruleDefinition string) (labelRule, error) {
	if fields := strings.Split(ruleDefinition, "`"); len(fields) == 5 {
		r := splitPairsRule{source: strings.TrimSpace(fields[0])}
		r.sep1 = fields[1]
		r.sep2 = fields[3]
		return &r, nil
	}
	return nil, errors.New(InvalidRuleDefinition, ruleDefinition)
}

func (r *splitPairsRule) toString() string {
	return fmt.Sprintf("source=%s, sep1=%s sep2=%s", r.source, r.sep1, r.sep2)
}

func (r *splitPairsRule) wantsInstance() bool {
	return true
}

func (r *splitPairsRule) runOnMatrix(m *matrix.Matrix, instance *matrix.Instance, key, prefix string) error {
	// nothing to do
	return nil
}

func (r *splitPairsRule) runOnInstance(instance *matrix.Instance, prefix string) {
	if value := instance.GetLabel(r.source); value != "" {
		for _, pair := range strings.Split(value, r.sep1) {
			if kv := strings.Split(pair, r.sep2); len(kv) == 2 {
				instance.SetLabel(kv[0], kv[1])
				//logger.Trace(me.Prefix, "splitPair: ($s) [%s] => (%s) [%s]", r.source, value, kv[0], kv[1])
			}
		}
	}
}
