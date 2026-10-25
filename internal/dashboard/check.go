package dashboard

import (
	"context"
	"fmt"
	"maps"

	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/entity"
	"github.com/dvjn/goldenportal/internal/filter"
	"github.com/dvjn/goldenportal/internal/scorecard"
	"github.com/dvjn/goldenportal/internal/widget"
)

// Check checks a dashboard's filters and widgets.
func Check(ctx context.Context, db configdoc.DB, spec Spec) ([]configdoc.Issue, error) {
	blueprints, scorecards, err := published(ctx, db, nil)
	if err != nil {
		return nil, err
	}
	return append(checkFilters(spec.Filters, blueprints), checkWidgets(spec.Widgets, blueprints, scorecards)...), nil
}

// checkFilters checks the blueprints the filters pick from. A lookup
// default's blueprint is the filter's own, so it's only reported once.
func checkFilters(filters []Filter, blueprints map[string]blueprint.Spec) []configdoc.Issue {
	var issues []configdoc.Issue
	for _, f := range filters {
		if f.Type != FilterRelation {
			continue
		}
		bp, ok := blueprints[f.Relation]
		if !ok {
			issues = append(issues, configdoc.Issue{
				Severity: configdoc.SeverityWarning, Path: "spec.filters." + f.ID + ".relation", Line: f.line,
				Message: fmt.Sprintf("no blueprint %q is published yet", f.Relation),
			})
			continue
		}
		if f.Lookup == nil || f.Lookup.Where == nil {
			continue
		}
		if err := entity.CheckFilter(bp, *widget.Bind(f.Lookup.Where, sampleMe)); err != nil {
			issues = append(issues, configdoc.Issue{
				Severity: configdoc.SeverityError, Path: "spec.filters." + f.ID + ".default.where", Line: f.line,
				Message: err.Error(),
			})
		}
	}

	return issues
}

// sampleMe stands in for whoever will look, to check filters before anyone
// does. $me is never unset, so binding it never drops a condition.
var sampleMe = widget.MeValues(auth.Principal{Email: "me@example.com", DisplayName: "Me", Role: auth.RoleMember, Groups: []string{"group"}})

// CheckPages checks the widgets on a blueprint's pages. They may show the
// blueprint itself, so they're checked against the version being checked.
func CheckPages(ctx context.Context, db configdoc.DB, spec blueprint.Spec) ([]configdoc.Issue, error) {
	var widgets []widget.Widget
	for _, page := range []*blueprint.Page{spec.Catalog, spec.Entity} {
		if page == nil {
			continue
		}
		for _, tab := range page.Tabs {
			widgets = append(widgets, tab.Widgets...)
		}
	}

	return CheckWidgets(ctx, db, widgets, &spec)
}

// CheckWidgets checks widgets against the blueprints and scorecards they
// show, with own in place of its published version. One that isn't
// published is a warning, since it may be on its way; a property or
// filter they can't answer is an error.
func CheckWidgets(ctx context.Context, db configdoc.DB, widgets []widget.Widget, own *blueprint.Spec) ([]configdoc.Issue, error) {
	if len(widgets) == 0 {
		return nil, nil
	}
	blueprints, scorecards, err := published(ctx, db, own)
	if err != nil {
		return nil, err
	}

	return checkWidgets(widgets, blueprints, scorecards), nil
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

func checkWidgets(widgets []widget.Widget, blueprints map[string]blueprint.Spec, scorecards map[string]scorecard.Spec) []configdoc.Issue {
	var issues []configdoc.Issue
	report := func(severity, path string, line int, format string, args ...any) {
		issues = append(issues, configdoc.Issue{Severity: severity, Path: path, Line: line, Message: fmt.Sprintf(format, args...)})
	}
	for _, w := range widgets {
		if w.Type == widget.TypeScorecard {
			sc, ok := scorecards[w.Scorecard]
			if !ok {
				report(configdoc.SeverityWarning, w.Path+".scorecard", w.Line, "no scorecard %q is published yet", w.Scorecard)
				continue
			}
			if bp, ok := blueprints[sc.Blueprint]; ok {
				if err := checkFilter(bp, w.Filter); err != nil {
					report(configdoc.SeverityError, w.Path+".filter", w.Line, "%v", err)
				}
			}
			continue
		}
		if w.Blueprint == "" {
			continue
		}
		bp, ok := blueprints[w.Blueprint]
		if !ok {
			report(configdoc.SeverityWarning, w.Path+".blueprint", w.Line, "no blueprint %q is published yet", w.Blueprint)
			continue
		}
		for _, problem := range checkWidget(bp, w) {
			report(configdoc.SeverityError, w.Path+"."+problem.field, w.Line, "%v", problem.err)
		}
	}

	return issues
}

type problem struct {
	field string
	err   error
}

func checkWidget(bp blueprint.Spec, w widget.Widget) []problem {
	var problems []problem
	add := func(field string, err error) {
		if err != nil {
			problems = append(problems, problem{field, err})
		}
	}
	add("filter", checkFilter(bp, w.Filter))
	switch w.Type {
	case widget.TypeStat:
		add("aggregate", entity.CheckAggregate(bp, w.Aggregate, w.Property))
	case widget.TypeBar, widget.TypeDonut:
		add("groupBy", entity.CheckGroup(bp, w.GroupBy))
	case widget.TypeTable:
		for _, column := range w.Columns {
			add("columns", entity.CheckColumn(bp, column))
		}
		if w.Sort != "" {
			add("sort", entity.CheckSort(bp, w.Sort))
		}
	case widget.TypeProperties:
		for _, property := range w.Properties {
			add("properties", entity.CheckColumn(bp, property))
		}
	}

	return problems
}

// checkFilter checks a filter with its context values bound to text, as
// they will be when the widget loads.
func checkFilter(bp blueprint.Spec, f *filter.Filter) error {
	values := maps.Clone(sampleMe)
	values[widget.ValueEntity] = []string{"entity"}
	for _, ref := range widget.FilterRefs(f) {
		values[ref] = []string{"value"}
	}
	bound := widget.Bind(f, values)
	if bound == nil {
		return nil
	}

	return entity.CheckFilter(bp, *bound)
}
