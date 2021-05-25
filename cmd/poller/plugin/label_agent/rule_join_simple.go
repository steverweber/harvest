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

// joins multiple labels into one label
type joinSimpleRule struct {
	sep     string
	target  string
	sources []string
}

// example rule:
// plex_long `_` aggr,plex
// if aggr="aggr1" and plex="plex1"; then
// plex_long="aggr1_plex1"

func parseJoinSimpleRule(ruleDefinition string) (labelRule, error) {
	if fields := strings.SplitN(ruleDefinition, " `", 2); len(fields) == 2 {
		r := joinSimpleRule{target: strings.TrimSpace(fields[0])}
		if fields = strings.SplitN(fields[1], "` ", 2); len(fields) == 2 {
			r.sep = fields[0]
			if r.sources = strings.Split(fields[1], ","); len(r.sources) != 0 {
				return &r, nil
			}
		}
	}
	return nil, errors.New(InvalidRuleDefinition, ruleDefinition)
}

func (r *joinSimpleRule) toString() string {
	return fmt.Sprintf("sep=%s, target=%s sources=%v", r.sep, r.target, r.sources)
}

func (r *joinSimpleRule) wantsInstance() bool {
	return true
}

func (r *joinSimpleRule) runOnMatrix(m *matrix.Matrix, instance *matrix.Instance, key, prefix string) error {
	// nothing to do
	return nil
}

func (r *joinSimpleRule) runOnInstance(instance *matrix.Instance, prefix string) {
	values := make([]string, 0)
	for _, label := range r.sources {
		if v := instance.GetLabel(label); v != "" {
			values = append(values, v)
		}
	}
	if len(values) != 0 {
		instance.SetLabel(r.target, strings.Join(values, r.sep))
		logger.Trace(prefix, "joinSimple: (%v) => (%s) [%s]", r.sources, r.target, instance.GetLabel(r.target))
	}
}
