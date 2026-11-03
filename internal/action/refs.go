package action

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/dvjn/goldenportal/internal/filter"
)

// Values in steps can refer to what a run knows. A string that is one
// reference becomes its value, whatever its type; {{ ref }} inside a longer
// string becomes text:
//
//	$input.<id>            what was entered for an input
//	$entity                the entity's identifier, $entity.<field> its fields
//	$me                    the requester's email, $me.<field> id, email, name, role or groups
//	$run.<field>           the run's id and url
//	$step.<id>.<field>     what an earlier step gave
const (
	RootInput  = "input"
	RootEntity = "entity"
	RootMe     = "me"
	RootRun    = "run"
	RootStep   = "step"
)

var (
	roots      = []string{RootInput, RootEntity, RootMe, RootRun, RootStep}
	refPattern = regexp.MustCompile(`^\$([a-z]+)((?:\.[A-Za-z0-9_-]+)*)$`)
	looksLike  = regexp.MustCompile(`^\$[a-z]+(\.|$)`)
	templated  = regexp.MustCompile(`\{\{\s*(.*?)\s*\}\}`)
	meFields   = []string{"id", "email", "name", "role", "groups"}
	runFields  = []string{"id", "url"}
)

// Ref is a parsed reference: its root and the path after it.
type Ref struct {
	Root string
	Path []string
}

func (r Ref) String() string { return "$" + strings.Join(append([]string{r.Root}, r.Path...), ".") }

// parseRef reads a whole reference. ok is false for text that isn't
// written as one; err is set for text that is, but names nothing.
func parseRef(text string) (ref Ref, ok bool, err error) {
	match := refPattern.FindStringSubmatch(text)
	if match == nil {
		if looksLike.MatchString(text) {
			return Ref{}, true, fmt.Errorf("%s is not a reference; use $input, $entity, $me, $run or $step", text)
		}
		return Ref{}, false, nil
	}
	ref = Ref{Root: match[1]}
	if match[2] != "" {
		ref.Path = strings.Split(match[2][1:], ".")
	}
	switch ref.Root {
	case RootInput:
		if len(ref.Path) == 0 {
			return ref, true, fmt.Errorf("name the input, as $input.<id>")
		}
	case RootMe:
		if len(ref.Path) > 1 || (len(ref.Path) == 1 && !slices.Contains(meFields, ref.Path[0])) {
			return ref, true, fmt.Errorf("$me has %s", strings.Join(meFields, ", "))
		}
	case RootRun:
		if len(ref.Path) != 1 || !slices.Contains(runFields, ref.Path[0]) {
			return ref, true, fmt.Errorf("$run has %s", strings.Join(runFields, ", "))
		}
	case RootStep:
		if len(ref.Path) == 0 {
			return ref, true, fmt.Errorf("name the step, as $step.<id>")
		}
	case RootEntity:
	default:
		return ref, true, fmt.Errorf("$%s is not a reference; use $input, $entity, $me, $run or $step", ref.Root)
	}

	return ref, true, nil
}

// refsIn lists every reference a value makes, whole or inside {{ }}, and
// what's wrong with those that name nothing.
func refsIn(value any) ([]Ref, []error) {
	var refs []Ref
	var errs []error
	walkStrings(value, func(text string) {
		if ref, ok, err := parseRef(text); ok {
			if err != nil {
				errs = append(errs, err)
			} else {
				refs = append(refs, ref)
			}
			return
		}
		for _, match := range templated.FindAllStringSubmatch(text, -1) {
			ref, ok, err := parseRef(match[1])
			switch {
			case !ok:
				errs = append(errs, fmt.Errorf("{{ %s }} must hold a reference, such as {{ $input.name }}", match[1]))
			case err != nil:
				errs = append(errs, err)
			default:
				refs = append(refs, ref)
			}
		}
	})

	return refs, errs
}

// filterRefs lists the references a filter compares with.
func filterRefs(f *filter.Filter) ([]Ref, []error) {
	var texts []any
	eachFilterText(f, func(text string) { texts = append(texts, text) })

	return refsIn(texts)
}

func walkStrings(value any, fn func(string)) {
	switch v := value.(type) {
	case string:
		fn(v)
	case []any:
		for _, item := range v {
			walkStrings(item, fn)
		}
	case map[string]any:
		for _, item := range v {
			walkStrings(item, fn)
		}
	}
}

func eachFilterText(f *filter.Filter, fn func(string)) {
	if f == nil {
		return
	}
	if text, ok := f.Value.(string); ok {
		fn(text)
	}
	if f.ValuesFrom != "" {
		fn(f.ValuesFrom)
	}
	for _, item := range f.Values {
		if text, ok := item.(string); ok {
			fn(text)
		}
	}
	eachFilterText(f.Not, fn)
	for i := range f.And {
		eachFilterText(&f.And[i], fn)
	}
	for i := range f.Or {
		eachFilterText(&f.Or[i], fn)
	}
}

// Scope is what references read while a run goes.
type Scope struct {
	Inputs map[string]any
	// Entity is the entity as jq reads it: flat, its fields beside identifier and title.
	Entity map[string]any
	Me     map[string]any
	Run    map[string]any
	Steps  map[string]any
}

// Resolve fills in a value's references.
func (s Scope) Resolve(value any) any {
	switch v := value.(type) {
	case string:
		if ref, ok, err := parseRef(v); ok && err == nil {
			return s.lookup(ref)
		}
		return templated.ReplaceAllStringFunc(v, func(match string) string {
			ref, ok, err := parseRef(templated.FindStringSubmatch(match)[1])
			if !ok || err != nil {
				return match
			}
			return text(s.lookup(ref))
		})
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = s.Resolve(item)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			out[key] = s.Resolve(item)
		}
		return out
	}

	return value
}

// Text resolves a value and writes it as text.
func (s Scope) Text(value string) string { return text(s.Resolve(value)) }

// ResolveFilter fills in a filter's references. A list compared with by
// values can come from one reference, such as values: $input.teams.
func (s Scope) ResolveFilter(f *filter.Filter) *filter.Filter {
	if f == nil {
		return nil
	}
	out := *f
	out.Value = s.Resolve(f.Value)
	if f.ValuesFrom != "" {
		out.ValuesFrom = ""
		switch resolved := s.Resolve(f.ValuesFrom).(type) {
		case []any:
			out.Values = resolved
		case nil:
			out.Values = []any{}
		default:
			out.Values = []any{resolved}
		}
	} else if f.Values != nil {
		out.Values = s.Resolve(f.Values).([]any)
	}
	out.Not = s.ResolveFilter(f.Not)
	out.And = s.resolveAll(f.And)
	out.Or = s.resolveAll(f.Or)

	return &out
}

func (s Scope) resolveAll(parts []filter.Filter) []filter.Filter {
	if parts == nil {
		return nil
	}
	out := make([]filter.Filter, len(parts))
	for i := range parts {
		out[i] = *s.ResolveFilter(&parts[i])
	}

	return out
}

func (s Scope) lookup(ref Ref) any {
	var value any
	path := ref.Path
	switch ref.Root {
	case RootInput:
		value = s.Inputs
	case RootEntity:
		if len(path) == 0 {
			return s.Entity["identifier"]
		}
		value = s.Entity
	case RootMe:
		if len(path) == 0 {
			return s.Me["email"]
		}
		value = s.Me
	case RootRun:
		value = s.Run
	case RootStep:
		value = s.Steps
	}
	for _, segment := range path {
		switch v := value.(type) {
		case map[string]any:
			value = v[segment]
		case []any:
			i, err := strconv.Atoi(segment)
			if err != nil || i < 0 || i >= len(v) {
				return nil
			}
			value = v[i]
		default:
			return nil
		}
	}

	return value
}

// text writes a value into a string: text as it is, nothing as empty, and
// anything else as JSON.
func text(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	}
	encoded, _ := json.Marshal(value)

	return string(encoded)
}
