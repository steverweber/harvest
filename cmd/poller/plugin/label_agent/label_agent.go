/*
 * Copyright NetApp Inc, 2021 All rights reserved
 */

package label_agent

import (
	"goharvest2/cmd/poller/plugin"
	"goharvest2/pkg/logger"
	"goharvest2/pkg/matrix"
	"strings"
)

// module errors
const (
	InvalidRuleName       = "invalid rule"
	InvalidRuleDefinition = "invalid rule definition"
)

type LabelAgent struct {
	*plugin.AbstractPlugin
	rules []labelRule
}

type labelRule interface {
	runOnInstance(*matrix.Instance, string)
	runOnMatrix(*matrix.Matrix, *matrix.Instance, string, string) error
	wantsInstance() bool
	toString() string
}

func New(p *plugin.AbstractPlugin) plugin.Plugin {
	return &LabelAgent{AbstractPlugin: p}
}

func (me *LabelAgent) Init() error {

	var (
		err error
	)

	if err = me.AbstractPlugin.Init(); err != nil {
		return err
	}

	if err = me.parseRules(); err != nil {
		return err
	}
	logger.Debug(me.Prefix, "parsed %d rules", len(me.rules))

	return nil
}

// parse rules from plugin parameters and return error if as soon as we fail
// rules will be added (and later exectured) in the exact same order as defined by user
func (me *LabelAgent) parseRules() error {

	var (
		rule           labelRule
		ruleName       string
		ruleDefinition string
		err            error
	)

	me.rules = make([]labelRule, 0)

	for _, c := range me.Params.GetChildren() {
		ruleName = c.GetNameS()
		ruleDefinition = strings.TrimSpace(c.GetContentS())

		switch ruleName {
		case "split":
			rule, err = parseSplitSimpleRule(ruleDefinition)
		case "split_regex":
			rule, err = parseSplitRegexRule(ruleDefinition)
		case "split_pairs":
			rule, err = parseSplitPairsRule(ruleDefinition)
		case "join":
			rule, err = parseJoinSimpleRule(ruleDefinition)
		case "replace":
			rule, err = parseReplaceSimpleRule(ruleDefinition)
		case "replace_regex":
			rule, err = parseReplaceRegexRule(ruleDefinition)
		case "exclude_equals":
			rule, err = parseExcludeEqualsRule(ruleDefinition)
		case "exclude_contains":
			rule, err = parseExcludeContainsRule(ruleDefinition)
		case "exclude_regex":
			rule, err = parseExcludeRegexRule(ruleDefinition)
		case "value_mapping":
			rule, err = parseValueMappingRule(ruleDefinition)
		default:
			err = errors.New(InvalidRuleName, ruleName)
		}

		if err != nil {
			logger.Error(me.Prefix, "parsing (%s) %s", ruleName, err.Error())
			return err
		}

		if rule != nil {
			logger.Debug(me.Prefix, "parsed rule (%s): %s", ruleName, rule.toString())
			me.rules = append(me.rules, rule)
		}
	}
	return nil
}

func (me *LabelAgent) Run(m *matrix.Matrix) ([]*matrix.Matrix, error) {

	for key, instance := range m.GetInstances() {
		for _, r := range me.rules {
			if r.wantsInstance() {
				r.runOnInstance(instance, me.Prefix)
			} else {
				if err := r.runOnMatrix(m, instance, key, me.Prefix); err != nil {
					logger.Error("running rule (%s)", r.toString())
					return nil, err
				}
			}
		}
	}

	return nil, nil
}
