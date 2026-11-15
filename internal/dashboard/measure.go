package dashboard

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/entity"
	"github.com/dvjn/goldenportal/internal/filter"
	"github.com/dvjn/goldenportal/internal/widget"
)

// measure asks a plan for aggregates of each group of path, in sort's
// order, which only the widget asking reads.
func (p *plan) measure(path string, aggregates []entity.Aggregate, sort entity.GroupSort, limit int) *measuredGroups {
	g := &measuredGroups{path: path, aggregates: aggregates, sort: sort, limit: limit}
	p.measured = append(p.measured, g)

	return g
}

func (l loader) groups(w widget.Widget, p *plan) func() (any, error) {
	if w.Measure != nil || w.Sort != "" {
		return l.measuredBars(w, p)
	}
	g := p.grouping(w.GroupBy, w.Limit)

	return func() (any, error) {
		if err := cmp.Or(p.err, g.err); err != nil {
			return nil, err
		}
		data := l.groupData(w, p, g.facets[:min(len(g.facets), w.Limit)], g.none)
		if w.Type == widget.TypeSplit {
			info, _ := entity.Describe(l.schema(p.query.Blueprint), w.GroupBy)
			byEnum(data.Groups, func(g group) any { return g.Value }, info.Property)
		}
		return data, nil
	}
}

// measuredBars are bars of a measure for each group, or of counts in an
// order of their own, largest first unless the sort says otherwise.
func (l loader) measuredBars(w widget.Widget, p *plan) func() (any, error) {
	var aggregates []entity.Aggregate
	sort := entity.MostFirst
	var m *measurement
	unit := ""
	if w.Measure != nil {
		measured := l.measurement(p.query.Blueprint, w.Measure, &aggregates)
		m = &measured
		sort.By = m.at
		unit = l.unit(p.query.Blueprint, w.Measure.Summary)
	}
	if w.Sort != "" {
		named := map[string]measurement{}
		if m != nil {
			named[widget.SortMeasure] = *m
		}
		sort = groupSort(w.Sort, named)
	}
	g := p.measure(w.GroupBy, aggregates, sort, w.Limit)

	return func() (any, error) {
		if err := cmp.Or(p.err, g.err); err != nil {
			return nil, err
		}
		facets := make([]entity.Facet, len(g.groups))
		for i, group := range g.groups {
			facets[i] = group.Facet
		}
		none := 0
		if g.none != nil {
			none = g.none.Count
		}
		data := l.groupData(w, p, facets, none)
		if m == nil {
			return data, nil
		}
		data.Unit = unit
		for i, group := range g.groups {
			r := m.read(group.Values, group.Count)
			data.Groups[i].Measure = &r
		}
		if g.none != nil {
			r := m.read(g.none.Values, g.none.Count)
			data.NoneMeasure = &r
		}
		return data, nil
	}
}

// groupData shows groups with their property's colors, linking each to its
// entities when the widget links its groups, or else linking the widget.
func (l loader) groupData(w widget.Widget, p *plan, facets []entity.Facet, none int) groupData {
	data := groupData{Groups: make([]group, len(facets)), None: none}
	linkGroups := w.Link != nil && w.Link.Group
	if !linkGroups {
		data.Link = l.link(w, nil)
	}
	info, _ := entity.Describe(l.schema(p.query.Blueprint), w.GroupBy)
	single := singleValued(info)
	for i, facet := range facets {
		data.Groups[i] = group{Facet: colored(facet, info)}
		if linkGroups {
			data.Groups[i].Link = l.groupLink(w, single, &filter.Filter{Property: w.GroupBy, Op: filter.OpEq, Value: facet.Value})
		}
	}
	// A list counts as none when it's empty, which exists can't say.
	if none > 0 && linkGroups && single {
		data.NoneLink = l.groupLink(w, single, &filter.Filter{Not: &filter.Filter{Property: w.GroupBy, Op: filter.OpExists}})
	}

	return data
}

// singleValued reports whether a field holds one value, so its groups
// split its entities, rather than counting an entity in each of its items.
func singleValued(info entity.FieldInfo) bool {
	return info.Property == nil || (!info.Property.Many && info.Property.Type != "array") || info.Kind == "tier"
}

// colored gives a group its property's color for its value, unless it has
// its own, as a tier does.
func colored(facet entity.Facet, info entity.FieldInfo) entity.Facet {
	if facet.Color == "" && info.Property != nil && info.Property.Display != nil {
		facet.Color = info.Property.Display.Colors[fmt.Sprint(facet.Value)]
	}

	return facet
}

// matrix works out each measure for each group in one statement, and when
// asked, over every group with the plan's other aggregates. Rows are the
// query's largest groups, in the order the property lists its values,
// unless the sort says otherwise.
func (l loader) matrix(w widget.Widget, p *plan) func() (any, error) {
	var aggregates []entity.Aggregate
	measurements := make([]measurement, len(w.Measures))
	named := map[string]measurement{}
	data := matrixData{Measures: make([]matrixMeasure, len(w.Measures))}
	for i, m := range w.Measures {
		measurements[i] = l.measurement(p.query.Blueprint, &m, &aggregates)
		named[m.Title] = measurements[i]
		data.Measures[i] = matrixMeasure{Title: m.Title, Unit: l.unit(p.query.Blueprint, m.Summary)}
	}
	sort := entity.MostFirst
	if w.Sort != "" {
		sort = groupSort(w.Sort, named)
	}
	g := p.measure(w.GroupBy, aggregates, sort, w.Limit)
	overall := -1
	if w.Overall {
		overall = len(p.aggregates)
		p.aggregates = append(p.aggregates, aggregates...)
	}

	return func() (any, error) {
		if err := cmp.Or(p.err, g.err); err != nil {
			return nil, err
		}
		if overall >= 0 {
			if p.valuesErr != nil {
				return nil, p.valuesErr
			}
			values := p.values[overall : overall+len(aggregates)]
			for i, m := range measurements {
				data.Overall = append(data.Overall, l.cell(w, w.Measures[i], m.read(values, 0), nil))
			}
		}
		info, _ := entity.Describe(l.schema(p.query.Blueprint), w.GroupBy)
		op := filter.OpEq
		if !singleValued(info) {
			op = filter.OpContains
		}
		data.Rows = make([]matrixRow, len(g.groups))
		for r, group := range g.groups {
			condition := &filter.Filter{Property: w.GroupBy, Op: op, Value: group.Value}
			row := matrixRow{Facet: colored(group.Facet, info)}
			row.Link = &linkData{Blueprint: w.Query.Blueprint, Filter: l.linkFilter(and(w.Query.Filter, condition))}
			for i, m := range measurements {
				row.Cells = append(row.Cells, l.cell(w, w.Measures[i], m.read(group.Values, group.Count), condition))
			}
			data.Rows[r] = row
		}
		if w.Sort == "" {
			byEnum(data.Rows, func(row matrixRow) any { return row.Value }, info.Property)
		}
		// A group no measure applies to, such as repositories without CI
		// when every measure counts those with, has nothing to read.
		data.Rows = slices.DeleteFunc(data.Rows, func(row matrixRow) bool {
			return !slices.ContainsFunc(row.Cells, func(c matrixCell) bool { return c.Value != nil })
		})
		return data, nil
	}
}

// cell is one measure in a matrix, of a group or of all of them. A share
// links to the entities it applies to that don't match, when there are
// any; another measure to the entities it applies to.
func (l loader) cell(w widget.Widget, m widget.Measure, r reading, group *filter.Filter) matrixCell {
	c := matrixCell{reading: r}
	base := and(w.Query.Filter, m.Filter, group)
	switch {
	case r.Value == nil:
	case m.Share != nil:
		if *r.Count < *r.Total {
			c.Link = &linkData{Blueprint: w.Query.Blueprint, Filter: l.linkFilter(and(base, &filter.Filter{Not: m.Share}))}
		}
	default:
		c.Link = &linkData{Blueprint: w.Query.Blueprint, Filter: l.linkFilter(base)}
	}

	return c
}

// trend works out a measure for each UTC day of the widget's range, which
// ends today, and over the whole range, in one statement.
func (l loader) trend(w widget.Widget, p *plan) func() (any, error) {
	days, _ := widget.TrendDays(w.Range)
	to := l.clock().UTC().Truncate(24*time.Hour).AddDate(0, 0, 1)
	t := &trendDays{path: w.Date, from: to.AddDate(0, 0, -days), to: to}
	m := l.measurement(p.query.Blueprint, w.Measure, &t.aggregates)
	unit := ""
	if w.Measure != nil {
		unit = l.unit(p.query.Blueprint, w.Measure.Summary)
	}
	p.trends = append(p.trends, t)
	linkDays := w.Link != nil && w.Link.Group

	return func() (any, error) {
		if err := cmp.Or(p.err, t.err); err != nil {
			return nil, err
		}
		byDay := map[time.Time]entity.Day{}
		for _, d := range t.days {
			byDay[d.Day] = d
		}
		data := trendData{Unit: unit, All: m.read(t.all.Values, t.all.Count), Days: make([]trendDay, days)}
		if !linkDays {
			data.Link = l.link(w, nil)
		}
		for i := range data.Days {
			day := t.from.AddDate(0, 0, i)
			out := trendDay{Day: day.Format(time.DateOnly)}
			d, ok := byDay[day]
			switch {
			case ok:
				out.reading = m.read(d.Values, d.Count)
			case m.count:
				// A day without entities counts none; any other measure
				// has nothing to work on.
				zero := 0.0
				out.Value = &zero
			}
			if ok && linkDays {
				out.Link = l.link(w, &filter.Filter{And: []filter.Filter{
					{Property: w.Date, Op: filter.OpGte, Value: day.Format(time.RFC3339)},
					{Property: w.Date, Op: filter.OpLt, Value: day.AddDate(0, 0, 1).Format(time.RFC3339)},
				}})
			}
			data.Days[i] = out
		}
		return data, nil
	}
}

// and is every condition given, flattened, or nil without any.
func and(filters ...*filter.Filter) *filter.Filter {
	var conditions []filter.Filter
	for _, f := range filters {
		switch {
		case f == nil:
		case len(f.And) > 0:
			conditions = append(conditions, f.And...)
		default:
			conditions = append(conditions, *f)
		}
	}
	switch len(conditions) {
	case 0:
		return nil
	case 1:
		return &conditions[0]
	}

	return &filter.Filter{And: conditions}
}

func countOf(value *float64) int {
	if value == nil {
		return 0
	}
	return int(math.Round(*value))
}

// byEnum puts groups in the order their property lists its values, such as
// a status's lifecycle, since a split reads as one whole left to right;
// values it doesn't list keep their count order, after those it does.
func byEnum[T any](groups []T, value func(T) any, property *blueprint.Property) {
	if property == nil {
		return
	}
	values := property.Enum
	if values == nil && property.Items != nil {
		values = property.Items.Enum
	}
	rank := func(g T) int {
		if i := slices.IndexFunc(values, func(v any) bool { return fmt.Sprint(v) == fmt.Sprint(value(g)) }); i >= 0 {
			return i
		}
		return len(values)
	}
	slices.SortStableFunc(groups, func(a, b T) int { return rank(a) - rank(b) })
}
