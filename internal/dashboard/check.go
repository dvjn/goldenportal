package dashboard

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/entity"
	"github.com/dvjn/goldenportal/internal/filter"
	"github.com/dvjn/goldenportal/internal/scorecard"
	"github.com/dvjn/goldenportal/internal/widget"
)

// Check checks a dashboard's inputs, queries and widgets.
func Check(ctx context.Context, db configdoc.DB, spec Spec) ([]configdoc.Issue, error) {
	blueprints, scorecards, err := published(ctx, db, nil)
	if err != nil {
		return nil, err
	}
	issues := checkInputs(spec.Inputs, blueprints)
	issues = append(issues, checkQueries(spec.Queries, blueprints)...)

	return append(issues, checkWidgets(spec.Widgets, "", blueprints, scorecards)...), nil
}

// checkQueries checks named queries against their blueprints, once, so
// the widgets that use them only report what's their own.
func checkQueries(queries []widget.Query, blueprints map[string]blueprint.Spec) []configdoc.Issue {
	var issues []configdoc.Issue
	for _, q := range queries {
		bp, ok := blueprints[q.Blueprint]
		if !ok {
			issues = append(issues, configdoc.Issue{
				Severity: configdoc.SeverityWarning, Path: q.Path + ".blueprint", Line: q.Line,
				Message: fmt.Sprintf("no blueprint %q is published yet", q.Blueprint),
			})
			continue
		}
		if err := checkFilter(blueprints, bp, q.Filter); err != nil {
			issues = append(issues, configdoc.Issue{Severity: configdoc.SeverityError, Path: q.Path + ".filter", Line: q.Line, Message: err.Error()})
		}
	}

	return issues
}

// checkInputs checks the blueprints the inputs pick from. A default
// query's blueprint is the input's own, so it's only reported once.
func checkInputs(inputs []Input, blueprints map[string]blueprint.Spec) []configdoc.Issue {
	var issues []configdoc.Issue
	for _, in := range inputs {
		if in.Type != InputRelation {
			continue
		}
		bp, ok := blueprints[in.Relation]
		if !ok {
			issues = append(issues, configdoc.Issue{
				Severity: configdoc.SeverityWarning, Path: "spec.inputs." + in.ID + ".relation", Line: in.line,
				Message: fmt.Sprintf("no blueprint %q is published yet", in.Relation),
			})
			continue
		}
		if in.DefaultQuery == nil || in.DefaultQuery.Query.Filter == nil {
			continue
		}
		bound := widget.Bind(in.DefaultQuery.Query.Filter, sampleValues())
		if bound == nil {
			continue
		}
		if err := entity.CheckFilter(blueprints, bp, *bound); err != nil {
			issues = append(issues, configdoc.Issue{
				Severity: configdoc.SeverityError, Path: "spec.inputs." + in.ID + ".default.query.filter", Line: in.line,
				Message: err.Error(),
			})
		}
	}

	return issues
}

// sampleMe stands in for whoever will look, to check filters before anyone
// does. $me is never unset, so binding it never drops a condition.
var sampleMe = widget.MeValues(auth.Principal{Email: "me@example.com", DisplayName: "Me", Role: auth.RoleMember, Groups: []string{"group"}})

// CheckPages checks the queries and widgets on a blueprint's pages. They
// may show the blueprint itself, so they're checked against the version
// being checked. On entity pages, $entity stands for an entity with every
// property, typed.
func CheckPages(ctx context.Context, db configdoc.DB, spec blueprint.Spec) ([]configdoc.Issue, error) {
	blueprints, scorecards, err := published(ctx, db, &spec)
	if err != nil {
		return nil, err
	}

	return checkPages(spec, blueprints, scorecards), nil
}

func checkPages(spec blueprint.Spec, blueprints map[string]blueprint.Spec, scorecards map[string]scorecard.Spec) []configdoc.Issue {
	samples := sampleEntity(spec, blueprints)
	bind := func(f *filter.Filter) *filter.Filter { return widget.BindData(f, samples) }
	var issues []configdoc.Issue
	for _, page := range []*blueprint.Page{spec.Catalog, spec.Entity} {
		if page == nil {
			continue
		}
		queries := page.Queries
		if page == spec.Entity {
			queries = make([]widget.Query, len(page.Queries))
			for i, q := range page.Queries {
				q.Filter = bind(q.Filter)
				queries[i] = q
			}
		}
		issues = append(issues, checkQueries(queries, blueprints)...)
		var widgets []widget.Widget
		for _, tab := range page.Tabs {
			for _, w := range tab.Widgets {
				// Check with the page entity's types, even when the widget shows another blueprint.
				if page == spec.Entity {
					w = w.MapFilters(bind)
				}
				widgets = append(widgets, w)
			}
		}
		issues = append(issues, checkWidgets(widgets, spec.Identifier, blueprints, scorecards)...)
	}

	return issues
}

// published loads what widgets can show, with own in place of its
// published version.
func published(ctx context.Context, db configdoc.DB, own *blueprint.Spec) (map[string]blueprint.Spec, map[string]scorecard.Spec, error) {
	blueprints, err := blueprint.Published(ctx, db)
	if err != nil {
		return nil, nil, err
	}
	if own != nil {
		blueprints[own.Identifier] = *own
	}
	scorecards, err := scorecard.Published(ctx, db)
	if err != nil {
		return nil, nil, err
	}

	return blueprints, scorecards, nil
}

// checkWidgets checks widgets against the blueprints and scorecards they
// show; page is the blueprint whose page they're on, if any. One that
// isn't published is a warning, since it may be on its way; a property or
// filter they can't answer is an error. A named query's own problems are
// checkQueries'.
func checkWidgets(widgets []widget.Widget, page string, blueprints map[string]blueprint.Spec, scorecards map[string]scorecard.Spec) []configdoc.Issue {
	var issues []configdoc.Issue
	for _, w := range widgets {
		c := widgetCheck{w: w, blueprints: blueprints, scorecards: scorecards}
		c.check(page)
		issues = append(issues, c.issues...)
	}

	return issues
}

type widgetCheck struct {
	w          widget.Widget
	blueprints map[string]blueprint.Spec
	scorecards map[string]scorecard.Spec
	issues     []configdoc.Issue
}

func (c *widgetCheck) report(severity, field, format string, args ...any) {
	c.issues = append(c.issues, configdoc.Issue{Severity: severity, Path: c.w.Path + "." + field, Line: c.w.Line, Message: fmt.Sprintf(format, args...)})
}

func (c *widgetCheck) fail(field string, err error) {
	if err != nil {
		c.report(configdoc.SeverityError, field, "%v", err)
	}
}

// query finds the blueprint a query picks from and checks a filter written
// in place. It's false when there's nothing more to check it against.
func (c *widgetCheck) query(q *widget.Query, field string) (blueprint.Spec, bool) {
	if q == nil {
		return blueprint.Spec{}, false
	}
	bp, ok := c.blueprints[q.Blueprint]
	switch {
	case !ok && q.Name == "":
		c.report(configdoc.SeverityWarning, field+".blueprint", "no blueprint %q is published yet", q.Blueprint)
	case ok && q.Name == "":
		c.fail(field+".filter", checkFilter(c.blueprints, bp, q.Filter))
	}

	return bp, ok
}

func (c *widgetCheck) check(page string) {
	w := c.w
	switch w.Type {
	case widget.TypeMarkdown:
		return
	case widget.TypeProperties:
		if bp, ok := c.blueprints[page]; ok {
			for _, property := range w.Properties {
				c.fail("properties", entity.CheckColumn(c.blueprints, bp, property))
			}
		}
		return
	case widget.TypeStat:
		if w.Value != nil {
			c.value(*w.Value)
		}
	case widget.TypeScorecard:
		c.scorecard()
	default:
		if bp, ok := c.query(w.Query, "query"); ok {
			c.entities(bp)
		}
	}
	c.link()
}

func (c *widgetCheck) value(v widget.Value) {
	bp, ok := c.query(&v.Query, "value.query")
	op, path := v.Op()
	if !ok || op == widget.OpJQ || op == widget.OpCount {
		return
	}
	a := entity.Aggregate{Op: op, Path: path}
	if v.Share != nil {
		a.Share = bindSample(v.Share)
		if a.Share == nil {
			return
		}
	}
	c.fail("value."+op, entity.CheckAggregate(c.blueprints, bp, a))
}

func (c *widgetCheck) scorecard() {
	sc, ok := c.scorecards[c.w.Scorecard]
	if !ok {
		c.report(configdoc.SeverityWarning, "scorecard", "no scorecard %q is published yet", c.w.Scorecard)
		return
	}
	q := c.w.Query
	switch {
	case q == nil:
	case q.Blueprint != "" && q.Blueprint != sc.Blueprint:
		c.report(configdoc.SeverityError, "query", "the query picks %s, but %s scores %s", q.Blueprint, sc.Identifier, sc.Blueprint)
	case q.Name == "":
		if bp, ok := c.blueprints[sc.Blueprint]; ok {
			c.fail("query.filter", checkFilter(c.blueprints, bp, q.Filter))
		}
	}
}

// entities checks what bars, donuts and tables show of bp's entities.
func (c *widgetCheck) entities(bp blueprint.Spec) {
	w := c.w
	switch w.Type {
	case widget.TypeBar, widget.TypeDonut:
		c.fail("groupBy", entity.CheckGroup(c.blueprints, bp, w.GroupBy))
	case widget.TypeTable:
		for _, column := range w.Columns {
			if column.Path != "" {
				c.fail("columns", entity.CheckColumn(c.blueprints, bp, column.Path))
			}
		}
		if w.Sort != "" {
			c.fail("sort", entity.CheckSort(c.blueprints, bp, w.Sort))
		}
	}
}

// link checks a query link's entities: its filter, and for a link to each
// group, that the group's value can narrow them.
func (c *widgetCheck) link() {
	l := c.w.Link
	if l == nil || l.Query == nil {
		return
	}
	bp, ok := c.query(l.Query, "link.query")
	if !ok {
		return
	}
	c.fail("link.filter", checkFilter(c.blueprints, bp, l.Filter))
	if l.Group && c.w.GroupBy != "" {
		c.fail("link.group", entity.CheckFilter(c.blueprints, bp, filter.Filter{Property: c.w.GroupBy, Op: filter.OpExists}))
	}
}

// checkFilter checks a filter with its context values bound to text, as
// they will be when the widget loads.
func checkFilter(blueprints map[string]blueprint.Spec, bp blueprint.Spec, f *filter.Filter) error {
	bound := bindSample(f)
	if bound == nil {
		return nil
	}

	return entity.CheckFilter(blueprints, bp, *bound)
}

// bindSample binds a filter's context values to text, as they will be when
// the widget loads.
func bindSample(f *filter.Filter) *filter.Filter {
	values := maps.Clone(sampleMe)
	values[widget.ValueEntity] = []string{"entity"}
	widget.SetToday(values, time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC))
	for _, ref := range widget.InputRefs(f) {
		values[ref] = []string{"value"}
	}

	return widget.Bind(f, values)
}

func sampleValues() widget.Values {
	values := maps.Clone(sampleMe)
	widget.SetToday(values, time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC))
	return values
}

func sampleEntity(spec blueprint.Spec, blueprints map[string]blueprint.Spec) map[string][]any {
	properties := map[string]any{}
	names := []string{}
	for _, p := range spec.Properties {
		names = append(names, p.ID)
		id := p.ID
		if p.Lookup != "" {
			at := spec
			segments := strings.Split(p.Lookup, ".")
			for _, segment := range segments[:len(segments)-1] {
				relation, _ := at.Property(segment)
				at = blueprints[relation.Relation]
			}
			if target, ok := at.Property(segments[len(segments)-1]); ok {
				p = target
			}
		}
		var value any = "value"
		switch p.Type {
		case "number", "integer":
			value = float64(1)
		case "boolean":
			value = true
		case "object":
			value = map[string]any{}
		case "array":
			var item any = "value"
			if p.Items != nil {
				switch p.Items.Type {
				case "number", "integer":
					item = float64(1)
				case "boolean":
					item = true
				case "object":
					item = map[string]any{}
				}
			}
			value = []any{item}
		}
		if p.Format == "date" {
			value = "2026-10-25"
		}
		if p.Format == "date-time" {
			value = "2026-10-25T00:00:00Z"
		}
		if p.Many {
			value = []any{"entity"}
		}
		properties[id] = value
	}
	return widget.EntityValues("entity", "Entity", spec.Identifier, properties, names)
}
