package entity

import (
	"regexp"
	"slices"
	"strings"

	"github.com/dvjn/goldenportal/internal/blueprint"
)

// How a statement reads an IndexKey.
const (
	// UseSort orders a page by it, ties broken by title and identifier.
	UseSort = "sort"
	// UseRange compares it with gt, gte, lt, lte or within.
	UseRange = "range"
	// UseRelation matches it with the related entities a condition picks.
	UseRelation = "relation"
)

// IndexKey is an expression of a blueprint's entities that statements
// sort or narrow them by, as the compiler writes it but without the alias,
// so the planner matches an index on it to the statements. Equality on
// stored values isn't one, as the jsonb GIN indexes answer containment,
// and nor is exists, which the planner doesn't answer from an index on it.
type IndexKey struct {
	Blueprint  string
	Expression string
	Use        string
	Descending bool
}

// alias is the compiler's name for the entity a statement reads.
var alias = regexp.MustCompile(`\be\.`)

// indexKey is a key on fld, or false when no index on entities can hold
// it: a value read through a subquery, such as a scorecard result, whose
// indexes are scorecard_results' own, or a column an index of the table's
// own already leads with.
func indexKey(spec blueprint.Spec, fld field, expression, use string) (IndexKey, bool) {
	switch {
	case strings.Contains(expression, "SELECT"), fld.result != nil, fld.timestamp != "", fld.kind == kindTier:
		return IndexKey{}, false
	case fld.builtIn == "title", fld.builtIn == "identifier", fld.builtIn == "blueprint", fld.builtIn == "connection":
		return IndexKey{}, false
	}

	return IndexKey{Blueprint: spec.Identifier, Expression: alias.ReplaceAllString(expression, ""), Use: use}, true
}

// SortKey is what sorting spec's entities by sort, a path or -path,
// orders them by.
func SortKey(specs map[string]blueprint.Spec, spec blueprint.Spec, sort string) (IndexKey, bool) {
	path, descending := strings.CutPrefix(sort, "-")
	fld, err := fieldFor(Schema{Blueprint: spec, Blueprints: specs}, false, path)
	if err != nil || fld.many || fld.kind == "object" {
		return IndexKey{}, false
	}
	key, ok := indexKey(spec, fld, fld.sortKey(), UseSort)
	key.Descending = descending

	return key, ok
}

// DateKey is what a trend ranges over to put spec's entities on the day of
// the date at path.
func DateKey(specs map[string]blueprint.Spec, spec blueprint.Spec, path string) (IndexKey, bool) {
	fld, err := trendField(Schema{Blueprint: spec, Blueprints: specs}.compiler(false), path)
	if err != nil {
		return IndexKey{}, false
	}

	return indexKey(spec, fld, fld.time(), UseRange)
}

// FilterKeys are what a filter on spec's entities narrows them by, on spec
// and on the blueprints its relations lead to. A negated condition can't
// be looked up in an index, so it has none.
func FilterKeys(specs map[string]blueprint.Spec, spec blueprint.Spec, f *Filter) []IndexKey {
	if f == nil {
		return nil
	}
	w := keyWalker{specs: specs}
	w.walk(spec, *f, 0)

	return w.keys
}

type keyWalker struct {
	specs map[string]blueprint.Spec
	keys  []IndexKey
}

func (w *keyWalker) walk(spec blueprint.Spec, f Filter, hops int) {
	switch {
	case len(f.And) > 0 || len(f.Or) > 0:
		for _, part := range slices.Concat(f.And, f.Or) {
			w.walk(spec, part, hops)
		}
	case f.Not == nil:
		w.condition(spec, f, hops)
	}
}

// condition follows compiler.condition and compiler.follow.
func (w *keyWalker) condition(spec blueprint.Spec, f Filter, hops int) {
	if name, rest, ok := strings.Cut(f.Property, "."); ok {
		if prop, ok := spec.Property(name); ok && (prop.IsRelation() || prop.Lookup != "") {
			w.follow(spec, prop, f, rest, hops)
			return
		}
	}
	if prop, ok := spec.Property(f.Property); ok && prop.Lookup != "" {
		f.Property = prop.Lookup
		w.condition(spec, f, hops)
		return
	}
	fld, err := reader{specs: w.specs}.read(spec, "e", f.Property, hops)
	if err != nil {
		return
	}
	if !slices.Contains([]string{OpGt, OpGte, OpLt, OpLte, OpWithin}, f.Op) {
		return
	}
	switch {
	case fld.kind == kindDate:
		w.add(indexKey(spec, fld, fld.time(), UseRange))
	case fld.kind == "number" || fld.kind == "integer":
		w.add(indexKey(spec, fld, fld.number(), UseRange))
	}
}

func (w *keyWalker) follow(spec blueprint.Spec, prop blueprint.Property, f Filter, rest string, hops int) {
	if prop.Lookup != "" {
		f.Property = prop.Lookup + "." + rest
		w.condition(spec, f, hops)
		return
	}
	target, ok := w.specs[prop.Relation]
	if !ok || hops >= MaxFilterHops {
		return
	}
	if !prop.Many {
		w.keys = append(w.keys, IndexKey{Blueprint: spec.Identifier, Expression: "relations ->> " + quoteLiteral(prop.ID), Use: UseRelation})
	}
	f.Property = rest
	w.condition(target, f, hops+1)
}

func (w *keyWalker) add(key IndexKey, ok bool) {
	if ok {
		w.keys = append(w.keys, key)
	}
}
