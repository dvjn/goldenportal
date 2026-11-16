// Package index builds and drops indexes on entities for what published
// configuration sorts and narrows them by, on blueprints big enough for an
// index to beat reading them all. Nobody declares them, and nobody sees
// them other than as pages that load faster.
package index

import (
	"context"
	"maps"
	"slices"
	"strings"

	"github.com/dvjn/goldenportal/internal/action"
	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/dashboard"
	"github.com/dvjn/goldenportal/internal/entity"
	"github.com/dvjn/goldenportal/internal/filter"
	"github.com/dvjn/goldenportal/internal/scorecard"
	"github.com/dvjn/goldenportal/internal/widget"
)

// configuration is the published configuration indexes are derived from.
type configuration struct {
	Blueprints map[string]blueprint.Spec
	Scorecards map[string]scorecard.Spec
	Dashboards []dashboard.Spec
	Actions    []action.Spec
}

// loadConfiguration reads every published document indexes are derived
// from. Sources that don't parse are left out, as everywhere else.
func loadConfiguration(ctx context.Context, db configdoc.DB) (configuration, error) {
	var (
		cfg configuration
		err error
	)
	if cfg.Blueprints, err = blueprint.Published(ctx, db); err != nil {
		return configuration{}, err
	}
	if cfg.Scorecards, err = scorecard.Published(ctx, db); err != nil {
		return configuration{}, err
	}
	if cfg.Dashboards, err = published(ctx, db, dashboard.Name, dashboard.Parse); err != nil {
		return configuration{}, err
	}
	actions, err := published(ctx, db, action.Name, action.Parse)
	if err != nil {
		return configuration{}, err
	}
	workflows, err := published(ctx, db, action.WorkflowName, action.ParseWorkflow)
	if err != nil {
		return configuration{}, err
	}
	cfg.Actions = append(actions, workflows...)

	return cfg, nil
}

// published parses every published document of a kind, by identifier.
func published[S any](ctx context.Context, db configdoc.DB, kind string, parse func(string) (S, []configdoc.Issue)) ([]S, error) {
	sources, err := configdoc.NewStore(db, kind).PublishedSources(ctx)
	if err != nil {
		return nil, err
	}
	var specs []S
	for _, identifier := range slices.Sorted(maps.Keys(sources)) {
		if spec, issues := parse(sources[identifier]); !configdoc.HasErrors(issues) {
			specs = append(specs, spec)
		}
	}

	return specs, nil
}

// How often the statements a use serves run, which decides what gets an
// index when there are more than the caps allow.
const (
	// occasional statements run when someone sorts by a column or follows a
	// link, or as scorecards are evaluated, hourly.
	occasional = 1
	// onLoad statements run whenever a page shows.
	onLoad = 2
)

// keyUse is a key that published configuration sorts or narrows entities
// by, and where.
type keyUse struct {
	entity.IndexKey
	// Reason names the document and the part of it that asks for the key.
	Reason string
	Weight int
}

// derive is the keys published configuration sorts and narrows entities by:
// widgets' queries, sorts, trends, links, measures and condition columns,
// dashboards' default inputs, scorecards' scopes and actions' check steps,
// and catalog columns that also sort or narrow something else. Some are left out, since no index would serve
// them: action scopes, run filters and entity tabs' when only ever test one
// entity, scorecard checks are worked out for every entity in scope,
// retention rules only keep rows a delete would otherwise take, and a sort
// by a condition column orders by a condition worked out per row. A
// retention's own date is already indexed, and counting by a group reads
// every entity whatever indexes there are.
func derive(cfg configuration) []keyUse {
	d := deriver{blueprints: cfg.Blueprints, scorecards: cfg.Scorecards}
	for _, identifier := range slices.Sorted(maps.Keys(cfg.Blueprints)) {
		d.blueprint(cfg.Blueprints[identifier])
	}
	for _, spec := range cfg.Dashboards {
		where := "dashboard " + spec.Identifier
		for _, input := range spec.Inputs {
			if q := input.DefaultQuery; q != nil {
				d.filter(where+" input "+input.ID, q.Query.Blueprint, q.Query.Filter, onLoad)
			}
		}
		d.widgets(where, spec.Widgets)
	}
	for _, identifier := range slices.Sorted(maps.Keys(cfg.Scorecards)) {
		spec := cfg.Scorecards[identifier]
		d.filter("scorecard "+identifier+" filter", spec.Blueprint, spec.Filter, occasional)
	}
	for _, spec := range cfg.Actions {
		for _, step := range spec.Steps {
			if step.Check != nil {
				d.filter(strings.ToLower(spec.Kind)+" "+spec.Identifier+" step "+step.ID, step.Check.Blueprint, step.Check.Filter, onLoad)
			}
		}
	}

	// Sorting by a column of the catalog alone isn't worth slowing every
	// write for; a key something else reads too gets its index anyway.
	read := map[[2]string]bool{}
	for _, use := range d.uses {
		read[[2]string{use.Blueprint, use.Expression}] = true
	}
	for _, use := range d.columns {
		if read[[2]string{use.Blueprint, use.Expression}] {
			d.uses = append(d.uses, use)
		}
	}

	return d.uses
}

type deriver struct {
	blueprints map[string]blueprint.Spec
	scorecards map[string]scorecard.Spec
	// columns are the catalog's sorts, kept apart from the uses until the
	// rest are known.
	uses, columns []keyUse
}

// defaultColumns are how many properties the catalog shows when a
// blueprint doesn't pick its columns, as the UI picks them.
const defaultColumns = 5

func (d *deriver) blueprint(spec blueprint.Spec) {
	where := "blueprint " + spec.Identifier
	var columns []string
	if spec.Catalog != nil && spec.Catalog.Columns != nil {
		columns = spec.Catalog.Columns
	} else {
		for _, prop := range spec.Properties {
			if prop.Type != "object" && len(columns) < defaultColumns {
				columns = append(columns, prop.ID)
			}
		}
		columns = append(columns, "updatedAt")
	}
	// Either way round, as one index serves both.
	for _, column := range columns {
		if key, ok := entity.SortKey(d.blueprints, spec, "-"+column); ok {
			d.columns = append(d.columns, keyUse{IndexKey: key, Reason: where + " catalog column " + column, Weight: occasional})
		}
	}
	for _, page := range []struct {
		name   string
		layout *blueprint.Page
	}{{"catalog", spec.Catalog}, {"entity", spec.Entity}} {
		if page.layout == nil {
			continue
		}
		for _, tab := range page.layout.Tabs {
			d.widgets(where+" "+page.name+" tab "+tab.ID, tab.Widgets)
		}
	}
}

func (d *deriver) widgets(where string, widgets []widget.Widget) {
	for _, w := range widgets {
		reason := where + " widget " + w.ID
		on := d.queryBlueprint(w)
		switch {
		case w.Value != nil:
			d.filter(reason, w.Value.Query.Blueprint, w.Value.Query.Filter, onLoad)
			d.filter(reason+" share", w.Value.Query.Blueprint, w.Value.Share, occasional)
		case w.Query != nil:
			d.filter(reason, on, w.Query.Filter, onLoad)
		}
		switch w.Type {
		case widget.TypeTable:
			if _, byCondition, _ := w.SortCondition(); w.Sort != "" && !byCondition {
				d.sort(reason+" sort", on, w.Sort, onLoad)
			}
			// A condition column is worked out for the rows the query picks, so
			// its filter only counts for a look at them in the catalog.
			for _, column := range w.Columns {
				d.filter(reason+" column "+column.Title, on, column.Filter, occasional)
			}
		case widget.TypeTrend:
			if spec, ok := d.blueprints[on]; ok {
				d.add(reason+" date", onLoad)(entity.DateKey(d.blueprints, spec, w.Date))
			}
		}
		// Measures and shares narrow counts within the query's entities, and
		// the links on them open those entities in the catalog.
		for _, m := range append(slices.Clone(w.Measures), ptrs(w.Measure)...) {
			d.filter(reason+" measure", on, m.Filter, occasional)
			d.filter(reason+" measure", on, m.Share, occasional)
		}
		if l := w.Link; l != nil && l.Query != nil {
			d.filter(reason+" link", l.Query.Blueprint, l.Query.Filter, occasional)
			d.filter(reason+" link", l.Query.Blueprint, l.Filter, occasional)
		}
	}
}

// queryBlueprint is the blueprint whose entities a widget's query picks:
// a scorecard widget's is its scorecard's unless it says.
func (d *deriver) queryBlueprint(w widget.Widget) string {
	if w.Query != nil && w.Query.Blueprint != "" {
		return w.Query.Blueprint
	}
	if w.Type == widget.TypeScorecard {
		return d.scorecards[w.Scorecard].Blueprint
	}

	return ""
}

func ptrs[T any](p *T) []T {
	if p == nil {
		return nil
	}

	return []T{*p}
}

func (d *deriver) filter(reason, on string, f *filter.Filter, weight int) {
	spec, ok := d.blueprints[on]
	if !ok || f == nil {
		return
	}
	for _, key := range entity.FilterKeys(d.blueprints, spec, f) {
		d.uses = append(d.uses, keyUse{IndexKey: key, Reason: reason, Weight: weight})
	}
}

func (d *deriver) sort(reason, on, sort string, weight int) {
	if spec, ok := d.blueprints[on]; ok {
		d.add(reason, weight)(entity.SortKey(d.blueprints, spec, sort))
	}
}

func (d *deriver) add(reason string, weight int) func(entity.IndexKey, bool) {
	return func(key entity.IndexKey, ok bool) {
		if ok {
			d.uses = append(d.uses, keyUse{IndexKey: key, Reason: reason, Weight: weight})
		}
	}
}
