package dashboard

import (
	"context"
	"fmt"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/entity"
	"github.com/dvjn/goldenportal/internal/filter"
	"github.com/dvjn/goldenportal/internal/scorecard"
	"github.com/dvjn/goldenportal/internal/widget"
)

// Check checks a dashboard's widgets.
func Check(ctx context.Context, db configdoc.DB, spec Spec) ([]configdoc.Issue, error) {
	return CheckWidgets(ctx, db, spec.Widgets, nil)
}

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
	blueprints, err := blueprint.Published(ctx, db)
	if err != nil {
		return nil, err
	}
	if own != nil {
		blueprints[own.Identifier] = *own
	}
	scorecards, err := scorecard.Published(ctx, db)
	if err != nil {
		return nil, err
	}

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
			if bp, ok := blueprints[sc.Blueprint]; ok && w.Filter != nil {
				if err := entity.CheckFilter(bp, *placeholders(w.Filter)); err != nil {
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

	return issues, nil
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
	if w.Filter != nil {
		add("filter", entity.CheckFilter(bp, *placeholders(w.Filter)))
	}
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

// placeholders binds context values to text, so a filter using them can be checked.
func placeholders(f *filter.Filter) *filter.Filter {
	return widget.Bind(f, map[string]string{widget.ValueEntity: "entity", widget.ValueMe: "me@example.com"})
}
