package blueprint

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/dvjn/goldenportal/internal/filter"
	"github.com/dvjn/goldenportal/internal/widget"
)

// Retention deletes a blueprint's rows once their By property is older than
// Keep, on the instance's retention schedule. Rows whose By is empty, such
// as merge requests still open, are kept. Rules keep the rows their filter
// matches longer.
type Retention struct {
	By    string          `json:"by"`
	Keep  string          `json:"keep"`
	Rules []RetentionRule `json:"rules,omitempty"`
	keep  time.Duration
}

// RetentionRule keeps the rows its filter matches for Keep, longer than
// the blueprint's own keep.
type RetentionRule struct {
	Filter *filter.Filter `json:"filter"`
	Keep   string         `json:"keep"`
	keep   time.Duration
}

var (
	RetentionFields = []string{"by", "keep", "rules"}
	RuleFields      = []string{"filter", "keep"}
	// ruleOps are what a rule's filter can compare with. Each gives the
	// same answer for a row until it's written again, so a row is kept the
	// same, however long, from one day to the next.
	ruleOps = []string{filter.OpEq, filter.OpNe, filter.OpIn, filter.OpExists}
)

// RetainedBy is the property the blueprint's rows are kept by, or "" when
// it keeps them for ever.
func (s Spec) RetainedBy() string {
	if s.Retention == nil {
		return ""
	}

	return s.Retention.By
}

// Keep is how long the blueprint keeps rows that no rule keeps longer, or
// zero for ever.
func (s Spec) Keep() time.Duration {
	if s.Retention == nil {
		return 0
	}

	return s.Retention.keep
}

// Rules are the blueprint's retention rules.
func (s Spec) Rules() []RetentionRule {
	if s.Retention == nil {
		return nil
	}

	return s.Retention.Rules
}

// KeepFor is how long a row is kept: the longest keep of the rules it
// matches, else the blueprint's, or zero for ever.
func (s Spec) KeepFor(e Entity, computed map[string]any) time.Duration {
	keep := s.Keep()
	for _, rule := range s.Rules() {
		if rule.keep > keep && s.matches(*rule.Filter, e, computed) {
			keep = rule.keep
		}
	}

	return keep
}

// Longest is the longest any row is kept, or zero for ever.
func (s Spec) Longest() time.Duration {
	keep := s.Keep()
	for _, rule := range s.Rules() {
		keep = max(keep, rule.keep)
	}

	return keep
}

// RetainedAt is when a row is kept from, the time its By property holds,
// or zero when it has none or the blueprint keeps rows for ever.
func (s Spec) RetainedAt(e Entity) time.Time {
	text, _ := e.Properties[s.RetainedBy()].(string)
	at, err := time.Parse(time.RFC3339, text)
	if err != nil {
		return time.Time{}
	}

	return at
}

// Duration is how long the rule keeps rows.
func (r RetentionRule) Duration() time.Duration { return r.keep }

// SameRetention reports whether two versions keep the same rows equally
// long.
func SameRetention(a, b Spec) bool {
	if a.RetainedBy() != b.RetainedBy() || a.Keep() != b.Keep() || len(a.Rules()) != len(b.Rules()) {
		return false
	}
	for i, rule := range a.Rules() {
		other := b.Rules()[i]
		if rule.keep != other.keep || !reflect.DeepEqual(rule.Filter, other.Filter) {
			return false
		}
	}

	return true
}

// diffRetention says how a retention changes. Keeping any rows for less
// time, or by another property, is breaking, since the next retention run
// may delete rows the published version keeps.
func diffRetention(previous, next Spec) Change {
	switch {
	case previous.Retention == nil:
		return Change{
			Section: "retention", Change: ChangeAdded, Breaking: true,
			Detail: fmt.Sprintf("keeps rows %s by %s, so the next retention run deletes older ones", next.Retention.Keep, next.RetainedBy()),
		}
	case next.Retention == nil:
		return Change{Section: "retention", Change: ChangeRemoved, Detail: "keeps rows for ever"}
	}
	var details []string
	if previous.RetainedBy() != next.RetainedBy() {
		details = append(details, fmt.Sprintf("keeps rows by %s instead of %s", next.RetainedBy(), previous.RetainedBy()))
	}
	if previous.Keep() != next.Keep() {
		details = append(details, fmt.Sprintf("keeps rows %s instead of %s", next.Retention.Keep, previous.Retention.Keep))
	}
	if !reflect.DeepEqual(ruleKeeps(previous), ruleKeeps(next)) {
		details = append(details, "changes the rules")
	}
	shorter := keepsLess(previous, next)
	detail := strings.Join(details, " and ")
	if shorter {
		detail += ", so the next retention run may delete rows the published version keeps"
	}

	return Change{Section: "retention", Change: ChangeChanged, Breaking: shorter, Detail: detail}
}

// keepsLess reports whether next, retention as well as previous, may keep
// some row for less time: a shorter keep, another by, or a rule of
// previous that next doesn't keep as long.
func keepsLess(previous, next Spec) bool {
	less := next.Keep() < previous.Keep() || previous.RetainedBy() != next.RetainedBy()
	for _, rule := range previous.Rules() {
		less = less || !slices.ContainsFunc(next.Rules(), func(r RetentionRule) bool {
			return r.keep >= rule.keep && reflect.DeepEqual(r.Filter, rule.Filter)
		})
	}

	return less
}

// KeepsLonger reports whether next may keep some row longer than previous
// did, so what previous's retention left unread may be wanted again:
// dropping retention, a longer keep, another by, or a rule that keeps
// longer.
func KeepsLonger(previous, next Spec) bool {
	switch {
	case previous.Retention == nil:
		return false
	case next.Retention == nil:
		return true
	}

	return keepsLess(next, previous)
}

func ruleKeeps(s Spec) []string {
	var out []string
	for _, rule := range s.Rules() {
		encoded, _ := json.Marshal(rule)
		out = append(out, string(encoded))
	}

	return out
}

// matches is a rule filter's answer for a row, as the catalog's would be
// for the row stored, so the write path and the retention job agree.
func (s Spec) matches(f filter.Filter, e Entity, computed map[string]any) bool {
	switch {
	case len(f.And) > 0:
		return !slices.ContainsFunc(f.And, func(part filter.Filter) bool { return !s.matches(part, e, computed) })
	case len(f.Or) > 0:
		return slices.ContainsFunc(f.Or, func(part filter.Filter) bool { return s.matches(part, e, computed) })
	case f.Not != nil:
		return !s.matches(*f.Not, e, computed)
	}
	value, set := e.Properties[f.Property]
	if prop, ok := s.Property(f.Property); ok && prop.IsRelation() {
		value, set = e.Relations[f.Property]
	} else if ok && prop.Compute != "" {
		value, set = computed[f.Property]
	}
	set = set && value != nil
	switch f.Op {
	case filter.OpExists:
		return set
	case filter.OpEq:
		return set && holds(value, f.Value)
	case filter.OpNe:
		return !set || !holds(value, f.Value)
	case filter.OpIn:
		return set && slices.ContainsFunc(f.Values, func(want any) bool { return holds(value, want) })
	}

	return false
}

// holds reports whether a value is want, or for a list, holds it.
func holds(value, want any) bool {
	if list, ok := value.([]any); ok {
		return slices.ContainsFunc(list, func(item any) bool { return sameJSON(item, want) })
	}

	return sameJSON(value, want)
}

func sameJSON(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)

	return string(x) == string(y)
}

// retention reads spec.retention.
func (p *parser) retention(node *yaml.Node, spec Spec) *Retention {
	if node == nil {
		return nil
	}
	const path = "spec.retention"
	r := &Retention{}
	fields, ok := p.Fields(node, path, RetentionFields...)
	if !ok {
		return r
	}
	if n := fields["by"]; n == nil {
		p.Errorf(node, path+".by", "add by, the date-time property rows are kept by, such as by: startedAt")
	} else {
		r.By = p.Text(n, path+".by")
		if problem := spec.byProblem(r.By); problem != "" {
			p.Errorf(n, path+".by", "%s", problem)
		}
	}
	if n := fields["keep"]; n == nil {
		p.Errorf(node, path+".keep", "add how long rows are kept, such as keep: 30d")
	} else {
		r.Keep, r.keep = p.keep(n, path+".keep")
	}
	p.List(fields["rules"], path+".rules", func(i int, item *yaml.Node) {
		rulePath := fmt.Sprintf("%s.rules[%d]", path, i)
		ruleFields, ok := p.Fields(item, rulePath, RuleFields...)
		if !ok {
			return
		}
		rule := RetentionRule{}
		n := ruleFields["keep"]
		if n == nil {
			p.Errorf(item, rulePath+".keep", "add how long the rows it matches are kept, such as keep: 365d")
			return
		}
		rule.Keep, rule.keep = p.keep(n, rulePath+".keep")
		if rule.keep != 0 && rule.keep < r.keep {
			p.Errorf(n, rulePath+".keep", "a rule keeps rows longer than the blueprint does, so keep must be at least %s", r.Keep)
		}
		f := ruleFields["filter"]
		if f == nil {
			p.Errorf(item, rulePath+".filter", "add the filter that matches the rows to keep, such as {property: environment, op: eq, value: production}")
			return
		}
		if rule.Filter = widget.ReadFilter(&p.Reader, f, rulePath+".filter"); rule.Filter == nil {
			return
		}
		if err := spec.checkRuleFilter(*rule.Filter); err != nil {
			p.Errorf(f, rulePath+".filter", "%v", err)
			return
		}
		r.Rules = append(r.Rules, rule)
	})

	return r
}

// byProblem says why rows can't be kept by a property, or nothing when
// they can: it must be one connections write, holding a date and time.
func (s Spec) byProblem(by string) string {
	prop, ok := s.Property(by)
	switch {
	case !ok:
		return fmt.Sprintf("%s has no property %q", s.Identifier, by)
	case prop.Derived() || prop.IsRelation() || prop.Type != "string" || prop.Format != "date-time":
		return fmt.Sprintf("rows are kept by a stored date-time property, and %s isn't one; give it type: string, format: date-time", by)
	}

	return ""
}

func (p *parser) keep(node *yaml.Node, path string) (string, time.Duration) {
	text := p.Text(node, path)
	d, ok := filter.ParseDuration(text)
	if !ok || d < 24*time.Hour {
		p.Errorf(node, path, "keep must be a duration of at least 1d, such as 30d or 52w")
		return "", 0
	}

	return text, d
}

// checkRuleFilter allows only what gives a row the same answer until the
// row is written again: its own stored and computed properties and
// relations, compared with values written in the filter.
func (s Spec) checkRuleFilter(f filter.Filter) error {
	for _, part := range slices.Concat(f.And, f.Or) {
		if err := s.checkRuleFilter(part); err != nil {
			return err
		}
	}
	if f.Not != nil {
		return s.checkRuleFilter(*f.Not)
	}
	if len(f.And)+len(f.Or) > 0 {
		return nil
	}
	prop, ok := s.Property(f.Property)
	switch {
	case strings.Contains(f.Property, "."):
		return fmt.Errorf("a retention rule compares the row's own properties, so %s can't follow a relation", f.Property)
	case slices.Contains(BuiltIns, f.Property):
		return fmt.Errorf("a retention rule compares properties, not the built-in %s", f.Property)
	case !ok:
		return fmt.Errorf("%s has no property %q", s.Identifier, f.Property)
	case prop.Lookup != "" || prop.IsScorecard():
		return fmt.Errorf("%s changes without its row being written, so a retention rule can't compare it", f.Property)
	case prop.Type == "object":
		return fmt.Errorf("%s holds an object, which a retention rule can't compare", f.Property)
	case !slices.Contains(ruleOps, f.Op):
		return fmt.Errorf("a retention rule compares with %s, not %s", strings.Join(ruleOps, ", "), f.Op)
	case f.ValuesFrom != "" || isVariable(f.Value) || slices.ContainsFunc(f.Values, isVariable):
		return fmt.Errorf("a retention rule compares with values written in it, not variables")
	}

	return nil
}

func isVariable(value any) bool {
	text, ok := value.(string)
	return ok && strings.HasPrefix(text, "$")
}
