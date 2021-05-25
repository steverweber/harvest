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
	"strconv"
	"strings"
)

// same as replaceSimple, but use regex
type replaceRegexRule struct {
	reg     *regexp.Regexp
	source  string
	target  string
	indices []int
	format  string
}

// example rule:
// node node `^(node)_(\d+)_.*$` `Node-$2`
// if node="node_10_dc2"; then:
// node="Node-10"

func parseReplaceRegexRule(ruleDefinition string) (labelRule, error) {

	if fields := strings.SplitN(ruleDefinition, " `", 3); len(fields) == 3 {
		if labels := strings.Fields(fields[0]); len(labels) == 2 {
			r := replaceRegexRule{source: labels[0], target: labels[1]}
			var err error
			if r.reg, err = regexp.Compile(strings.TrimSuffix(fields[1], "`")); err != nil {
				//logger.Error(me.Prefix, "(replace_regex) invalid regex: %v", err)
				return nil, err
			}
			//logger.Trace(me.Prefix, "(replace_regex) compiled regular expression [%s]", r.reg.String())

			r.indices = make([]int, 0)
			err_pos := -1

			if fields[2] = strings.TrimSuffix(fields[2], "`"); len(fields[2]) != 0 {
				//logger.Trace(me.Prefix, "(replace_regex) parsing substitution string [%s] (%d)", fields[2], len(fields[2]))
				inside_num := false
				num := ""
				for i, b := range fields[2] {
					ch := string(b)
					if inside_num {
						if _, err := strconv.Atoi(ch); err == nil {
							num += ch
							continue
						} else if index, err := strconv.Atoi(num); err == nil && index > 0 {
							r.indices = append(r.indices, index-1)
							r.format += "%s"
							inside_num = false
							num = ""
						} else {
							err_pos = i
							break
						}
					}
					if ch == "$" {
						if strings.HasSuffix(r.format, `\`) {
							r.format = strings.TrimSuffix(r.format, `\`) + "$"
						} else {
							inside_num = true
						}
					} else {
						r.format += ch
					}
				}
			}
			if err_pos != -1 {
				return nil, errors.New(InvalidRuleDefinition, fmt.Sprintf("invalid char in substitution string at pos %d (%s)", err_pos, string(fields[2][err_pos])))
			}
			return &r, nil
		}
	}
	return nil, errors.New(InvalidRuleDefinition, ruleDefinition)
}

func (r *replaceRegexRule) toString() string {
	return fmt.Sprintf("regex=%s, source=%s, target=%s format=%s indices=%v", r.reg.String(), r.source, r.target, r.format, r.indices)
}

func (r *replaceRegexRule) wantsInstance() bool {
	return true
}

func (r *replaceRegexRule) runOnMatrix(m *matrix.Matrix, instance *matrix.Instance, key, prefix string) error {
	// nothing to do
	return nil
}

func (r *replaceRegexRule) runOnInstance(instance *matrix.Instance, prefix string) {
	old := instance.GetLabel(r.source)
	if m := r.reg.FindStringSubmatch(old); m != nil {
		logger.Trace(prefix, "replaceRegex: (%d) matches= %v", len(m)-1, m[1:])
		s := make([]interface{}, 0)
		for _, i := range r.indices {
			if i < len(m)-1 {
				s = append(s, m[i+1])
				//logger.Trace(prefix, "substring [%d] = (%s)", i, m[i+1])
			} else {
				// probably we need to throw warning
				s = append(s, "")
				//logger.Trace(prefix, "substring [%d] = no match!", i)
			}
		}
		logger.Trace(prefix, "replaceRegex: (%d) substitution strings= %v", len(s), s)
		if value := fmt.Sprintf(r.format, s...); value != "" && value != old {
			instance.SetLabel(r.target, value)
			logger.Trace(prefix, "replaceRegex: (%s) [%s] => (%s) [%s]", r.source, old, r.target, value)
		}
	}
}
