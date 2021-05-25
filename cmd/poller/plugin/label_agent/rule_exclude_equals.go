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

// if label equals to value, set instance as non-exportable
type excludeEqualsRule struct {
	label string
	value string
}

// example rule
// vol_type `flexgroup_constituent`
// all instances with matching label type, will not be exported

func parseExcludeEqualsRule(ruleDefinition string) (labelRule, error) {
	if fields := strings.SplitN(ruleDefinition, " `", 2); len(fields) == 2 {
		r := excludeEqualsRule{label: fields[0]}
		r.value = strings.TrimSuffix(fields[1], "`")
		return &r, nil
	}
	return nil, errors.New(InvalidRuleDefinition, ruleDefinition+" (should have two fields)")
}

func (r *excludeEqualsRule) toString() string {
	return fmt.Sprintf("label=%s, value=%s", r.label, r.value)
}

func (r *excludeEqualsRule) wantsInstance() bool {
	return true
}

func (r *excludeEqualsRule) runOnMatrix(m *matrix.Matrix, instance *matrix.Instance, key, prefix string) error {
	// nothing to do
	return nil
}

func (r *excludeEqualsRule) runOnInstance(instance *matrix.Instance, prefix string) {
	if instance.GetLabel(r.label) == r.value {
		instance.SetExportable(false)
		logger.Trace(prefix, "excludeEquals: (%s) [%s] instance with labels [%s] => excluded", r.label, r.value, instance.GetLabels().String())
	}
}
