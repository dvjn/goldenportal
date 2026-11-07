package filter

import (
	"reflect"
	"slices"
)

// And is every condition given, simplified: nested and flattened, exact
// duplicates dropped, and a condition dropped when another on the same
// property already implies it. It's nil without any condition.
//
// A property equal to v makes ne w (for any other w), an in holding v and
// exists redundant; an in of one value is an eq. The result matches the
// same entities as the conditions together.
func And(filters ...*Filter) *Filter {
	var flat []Filter
	var add func(f Filter)
	add = func(f Filter) {
		if len(f.And) > 0 && f.Property == "" && f.Op == "" {
			for _, part := range f.And {
				add(part)
			}
			return
		}
		if f.Op == OpIn && f.ValuesFrom == "" && len(f.Values) == 1 {
			f.Op, f.Value, f.Values = OpEq, f.Values[0], nil
		}
		flat = append(flat, f)
	}
	for _, f := range filters {
		if f != nil {
			add(*f)
		}
	}

	var kept []Filter
	for i, f := range flat {
		duplicate := slices.ContainsFunc(flat[:i], func(g Filter) bool { return reflect.DeepEqual(f, g) })
		if duplicate || slices.ContainsFunc(flat, func(g Filter) bool { return implies(g, f) }) {
			continue
		}
		kept = append(kept, f)
	}

	switch len(kept) {
	case 0:
		return nil
	case 1:
		return &kept[0]
	}

	return &Filter{And: kept}
}

// implies reports whether eq makes the condition other redundant.
func implies(eq, other Filter) bool {
	if eq.Op != OpEq || eq.Property == "" || other.Property != eq.Property {
		return false
	}
	switch other.Op {
	case OpExists:
		return true
	case OpNe:
		return !same(eq.Value, other.Value)
	case OpIn:
		return other.ValuesFrom == "" && slices.ContainsFunc(other.Values, func(v any) bool { return same(eq.Value, v) })
	}

	return false
}

// same compares values as JSON would, where every number is a float.
func same(a, b any) bool {
	if x, ok := number(a); ok {
		y, ok := number(b)
		return ok && x == y
	}

	return reflect.DeepEqual(a, b)
}

func number(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case float64:
		return n, true
	}

	return 0, false
}
