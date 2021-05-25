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

// if label value matches regular expression, set instance as non-exportable
type excludeRegexRule struct {
	label string
	reg   *regexp.Regexp
}

func parseExcludeRegexRule(ruleDefinition string) (labelRule, error) {
	if fields := strings.SplitN(ruleDefinition, " `", 2); len(fields) == 2 {
		r := excludeRegexRule{label: fields[0]}
		var err error
		if r.reg, err = regexp.Compile(strings.TrimSuffix(fields[1], "`")); err == nil {
			return &r, nil
		}
		return nil, err
	}
	return nil, errors.New(InvalidRuleDefinition, ruleDefinition+" (should have two fields)")
}

func (r *excludeRegexRule) toString() string {
	return fmt.Sprintf("label=%s, regex=%s", r.label, r.reg.String())
}

func (r *excludeRegexRule) wantsInstance() bool {
	return true
}

func (r *excludeRegexRule) runOnMatrix(m *matrix.Matrix, instance *matrix.Instance, key, prefix string) error {
	// nothing to do
	return nil
}

func (r *excludeRegexRule) runOnInstance(instance *matrix.Instance, prefix string) {
	if r.reg.MatchString(instance.GetLabel(r.label)) {
		instance.SetExportable(false)
		logger.Trace(prefix, "excludeEquals: (%s) [%s] instance with labels [%s] => excluded", r.label, r.reg.String(), instance.GetLabels().String())
	}
}
