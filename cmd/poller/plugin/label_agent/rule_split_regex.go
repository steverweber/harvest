/*
 * Copyright NetApp Inc, 2021 All rights reserved
 */

package label_agent

import (
	"fmt"
	"goharvest2/pkg/errors"
	"goharvest2/pkg/logger"
	"goharvest2/pkg/matrix"
	"regexp"
	"strings"
)

// splits one label value into multiple labels based on regex match
type splitRegexRule struct {
	reg     *regexp.Regexp
	source  string
	targets []string
}

// example rule:
// node `.*_(ag\d+)_(p\d+)_(d\d+)` aggr,plex,disk
// if node="jamaica1_ag1_p1_d1", then:
// aggr="ag1", plex="p1", disk="d1"

func parseSplitRegexRule(ruleDefinition string) (labelRule, error) {
	if fields := strings.SplitN(ruleDefinition, " `", 2); len(fields) == 2 {
		r := splitRegexRule{source: strings.TrimSpace(fields[0])}
		if fields = strings.SplitN(fields[1], "` ", 2); len(fields) == 2 {
			var err error
			if r.reg, err = regexp.Compile(fields[0]); err != nil {
				return nil, err
			}
			if r.targets = strings.Split(fields[1], ","); len(r.targets) != 0 {
				return &r, nil
			}
		}
	}
	return nil, errors.New(InvalidRuleDefinition, ruleDefinition)
}

func (r *splitRegexRule) toString() string {
	return fmt.Sprintf("reg=%s, source=%s targets=%v", r.reg.String(), r.source, r.targets)
}

func (r *splitRegexRule) wantsInstance() bool {
	return true
}

func (r *splitRegexRule) runOnMatrix(m *matrix.Matrix, instance *matrix.Instance, key, prefix string) error {
	// nothing to do
	return nil
}

func (r *splitRegexRule) runOnInstance(instance *matrix.Instance, prefix string) {
	if m := r.reg.FindStringSubmatch(instance.GetLabel(r.source)); m != nil && len(m) == len(r.targets)+1 {
		for i := range r.targets {
			if r.targets[i] != "" && m[i+1] != "" {
				instance.SetLabel(r.targets[i], m[i+1])
				logger.Trace(prefix, "splitRegex: (%s) [%s] => (%s) [%s]", r.source, instance.GetLabel(r.source), r.targets[i], m[i+1])
			}
		}
	}
}
