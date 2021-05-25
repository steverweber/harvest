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

// if label contains value, set instance as non-exportable
type excludeContainsRule struct {
	label string
	value string
}

func parseExcludeContainsRule(ruleDefinition string) (labelRule, error) {
	if fields := strings.SplitN(ruleDefinition, " `", 2); len(fields) == 2 {
		r := excludeContainsRule{label: fields[0]}
		r.value = strings.TrimSuffix(fields[1], "`")
		return &r, nil
	}
	return nil, errors.New(InvalidRuleDefinition, ruleDefinition+" (should have two fields)")
}

func (r *excludeContainsRule) toString() string {
	return fmt.Sprintf("label=%s, value=%s", r.label, r.value)
}

func (r *excludeContainsRule) wantsInstance() bool {
	return true
}

func (r *excludeContainsRule) runOnMatrix(m *matrix.Matrix, instance *matrix.Instance, key, prefix string) error {
	// nothing to do
	return nil
}

func (r *excludeContainsRule) runOnInstance(instance *matrix.Instance, prefix string) {
	if strings.Contains(instance.GetLabel(r.label), r.value) {
		instance.SetExportable(false)
		logger.Trace(prefix, "excludeContains: (%s) [%s] instance with labels [%s] => excluded", r.label, r.value, instance.GetLabels().String())
	}
}
