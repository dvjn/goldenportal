package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/entity"
	"github.com/dvjn/goldenportal/internal/filter"
	"github.com/dvjn/goldenportal/internal/httpx"
	"github.com/dvjn/goldenportal/internal/scorecard"
	"github.com/dvjn/goldenportal/internal/telemetry"
	"github.com/dvjn/goldenportal/internal/widget"
)

// MaxJQEntities caps the entities a jq value loads to run over. A query
// matching more fails rather than giving a number from some of them.
const MaxJQEntities = 1000

// tableTimeout bounds the jq a table runs for its rows, all together.
const tableTimeout = time.Second

// parallel caps the statements one page runs at once, so one dashboard
// can't take every connection.
const parallel = 4

type (
	statData struct {
		Value *float64 `json:"value"`
		Unit  string   `json:"unit,omitempty"`
		// A share also says how many of how many it is, so a percentage of a
		// handful reads as the handful it is.
		Count *int      `json:"count,omitempty"`
		Total *int      `json:"total,omitempty"`
		Link  *linkData `json:"link,omitempty"`
	}
	groupData struct {
		Groups []group `json:"groups"`
		None   int     `json:"none"`
		// NoneLink opens the entities without a value.
		NoneLink *linkData `json:"noneLink,omitempty"`
		// NoneMeasure is a bar's measure of the entities without a value,
		// and Unit follows each group's.
		NoneMeasure *reading  `json:"noneMeasure,omitempty"`
		Unit        string    `json:"unit,omitempty"`
		Link        *linkData `json:"link,omitempty"`
	}
	group struct {
		entity.Facet
		// Measure is what a bar's measure comes to for the group.
		Measure *reading  `json:"measure,omitempty"`
		Link    *linkData `json:"link,omitempty"`
	}
	// reading is what a measure comes to for a group or a day: its value,
	// and for a share, how many of how many it is. One with nothing to
	// work on, such as a share of no entities, has no value.
	reading struct {
		Value *float64 `json:"value"`
		Count *int     `json:"count,omitempty"`
		Total *int     `json:"total,omitempty"`
	}
	// trendData is a measure for each day of a trend's range, oldest first,
	// and over all of them, in a unit.
	trendData struct {
		Unit string     `json:"unit,omitempty"`
		All  reading    `json:"all"`
		Days []trendDay `json:"days"`
		Link *linkData  `json:"link,omitempty"`
	}
	// trendDay is a UTC day, as 2026-11-09, and its link, when it links
	// each day to its entities.
	trendDay struct {
		Day string `json:"day"`
		reading
		Link *linkData `json:"link,omitempty"`
	}
	// tierData counts entities by tier, with the scorecard's tiers to
	// show them by.
	tierData struct {
		Tiers     []scorecard.TierCount `json:"tiers"`
		Checks    scorecard.Checks      `json:"checks"`
		Scorecard *tableScorecard       `json:"scorecard"`
		Link      *linkData             `json:"link,omitempty"`
	}
	// matrixData is each measure per group: a row for each of the widget's
	// groups, a cell in it for each measure, and when asked, the measures
	// over every group.
	matrixData struct {
		Measures []matrixMeasure `json:"measures"`
		Overall  []matrixCell    `json:"overall,omitempty"`
		Rows     []matrixRow     `json:"rows"`
	}
	// matrixMeasure heads a column, whose numbers follow its unit.
	matrixMeasure struct {
		Title string `json:"title"`
		Unit  string `json:"unit,omitempty"`
	}
	// matrixRow is a group's cells, and links to its entities.
	matrixRow struct {
		entity.Facet
		Cells []matrixCell `json:"cells"`
		Link  *linkData    `json:"link,omitempty"`
	}
	// matrixCell is what a measure comes to for a group. A share links to
	// the entities that don't match, the ones to work on, and any other
	// measure to the entities it applies to.
	matrixCell struct {
		reading
		Link *linkData `json:"link,omitempty"`
	}
	textData struct {
		Text string `json:"text"`
	}
	tableData struct {
		Blueprint string        `json:"blueprint"`
		Total     int           `json:"total"`
		Columns   []tableColumn `json:"columns"`
		Rows      []tableRow    `json:"rows"`
		Link      *linkData     `json:"link,omitempty"`
	}
	// tableColumn says how to show a column: as the property its path ends
	// on, a built-in field or a scorecard's tier, or else as jq gave it.
	tableColumn struct {
		Title     string              `json:"title"`
		Path      string              `json:"path,omitempty"`
		JQ        string              `json:"jq,omitempty"`
		Unit      string              `json:"unit,omitempty"`
		Property  *blueprint.Property `json:"property,omitempty"`
		BuiltIn   string              `json:"builtIn,omitempty"`
		Scorecard *tableScorecard     `json:"scorecard,omitempty"`
	}
	tableScorecard struct {
		Identifier string           `json:"identifier"`
		Blueprint  string           `json:"blueprint"`
		Tiers      []scorecard.Tier `json:"tiers"`
	}
	// tableRow is one entity, with each column's value by index, what
	// relation and tier columns link to, and where the row links.
	tableRow struct {
		Identifier string          `json:"identifier"`
		Title      string          `json:"title"`
		Values     []any           `json:"values"`
		Links      [][]entity.Link `json:"links,omitempty"`
		Link       *linkData       `json:"link,omitempty"`
	}
)

// linkData is where a link goes: the catalog showing the entities of a
// blueprint that match a filter, with every variable filled in, or href, a
// URL or a path in the portal.
type linkData struct {
	Blueprint string         `json:"blueprint,omitempty"`
	Filter    *filter.Filter `json:"filter,omitempty"`
	Sort      string         `json:"sort,omitempty"`
	Href      string         `json:"href,omitempty"`
}

// result is one widget's data, or why it couldn't load, so one failing
// query leaves the rest of the page standing.
type result struct {
	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}

// PageData is every widget's data on a page, by identifier, and on a
// dashboard, the entities its relation inputs are set to, with their
// titles, by input.
type PageData struct {
	Widgets map[string]result        `json:"widgets"`
	Inputs  map[string][]entity.Link `json:"inputs,omitempty"`
}

// loader loads a page's widgets for one request.
type loader struct {
	ctx        context.Context
	db         entity.DB
	store      entity.Store
	blueprints map[string]blueprint.Spec
	ladders    map[string]blueprint.Ladder
	scorecards map[string]scorecard.Spec
	env        widget.Env
	// now is when the page loads, which trends end on the day of.
	now time.Time
	// log logs what failed without the viewer being told why.
	log func(ctx context.Context, msg string, err error, attrs ...attribute.KeyValue)
}

// plan is what a page's widgets need of one query's entities, gathered so
// each kind of read is one statement: every aggregate together, each
// grouping, each first page, each scorecard's tiers, and for jq, the
// entities themselves.
type plan struct {
	query      entity.Query
	match      entity.Match
	err        error
	aggregates []entity.Aggregate
	values     []*float64
	valuesErr  error
	groups     map[string]*grouping
	measured   []*measuredGroups
	trends     []*trendDays
	pages      map[string]*page
	tiers      map[string]*tiers
	documents  *documents
	// widgets are the widgets that read it, for its statements' spans.
	widgets []string
}

type grouping struct {
	path   string
	limit  int
	facets []entity.Facet
	none   int
	err    error
}

// measuredGroups are aggregates for each group, in an order of their own,
// which a widget reads alone.
type measuredGroups struct {
	path       string
	aggregates []entity.Aggregate
	sort       entity.GroupSort
	limit      int
	groups     []entity.Measured
	none       *entity.Measured
	err        error
}

// trendDays are aggregates for each day of a range.
type trendDays struct {
	path       string
	from, to   time.Time
	aggregates []entity.Aggregate
	days       []entity.Day
	all        entity.Day
	err        error
}

type page struct {
	sort  string
	limit int
	paths []string
	rows  []entity.Row
	total int
	err   error
}

type tiers struct {
	scorecard scorecard.Spec
	counts    []scorecard.TierCount
	checks    scorecard.Checks
	err       error
}

// documents are the entities jq values run over, as jq reads them.
type documents struct {
	list []any
	err  error
}

// load works out what every widget on a page shows for whoever is
// looking. Widgets that read the same query share its plan, and the
// statements run a few at a time.
func (l loader) load(widgets []widget.Widget) PageData {
	plans := map[string]*plan{}
	planFor := func(q *widget.Query, f *filter.Filter) *plan {
		bp, ok := l.blueprints[q.Blueprint]
		if !ok {
			return &plan{err: httpx.NotFound("no published blueprint " + q.Blueprint)}
		}
		bound := l.bind(f)
		encoded, _ := json.Marshal(bound)
		key := q.Blueprint + "\x00" + string(encoded)
		if p, ok := plans[key]; ok {
			return p
		}
		p := &plan{
			query:  entity.Query{Blueprint: bp, Blueprints: l.blueprints, Ladders: l.ladders, Filter: bound},
			groups: map[string]*grouping{}, pages: map[string]*page{}, tiers: map[string]*tiers{},
		}
		p.match, p.err = entity.NewMatch(p.query)
		plans[key] = p
		return p
	}

	assemble := map[string]func() (any, error){}
	for _, w := range widgets {
		read := func(q *widget.Query, f *filter.Filter) *plan {
			p := planFor(q, f)
			if !slices.Contains(p.widgets, w.ID) {
				p.widgets = append(p.widgets, w.ID)
			}
			return p
		}
		if fn := l.need(w, read); fn != nil {
			assemble[w.ID] = fn
		}
	}

	var tasks []func()
	for _, p := range plans {
		tasks = append(tasks, l.tasks(p)...)
	}
	run(tasks)

	out := PageData{Widgets: map[string]result{}}
	for _, w := range widgets {
		fn, ok := assemble[w.ID]
		if !ok {
			continue
		}
		_, span := telemetry.Start(l.ctx, "load widget", telemetry.Widget.String(w.ID), telemetry.WidgetType.String(w.Type))
		data, err := fn()
		telemetry.End(span, err)
		if err != nil {
			out.Widgets[w.ID] = result{Error: l.explain(w, err)}
			continue
		}
		out.Widgets[w.ID] = result{Data: data}
	}

	return out
}

// need adds what a widget reads to the plans, and returns how to put its
// data together once they've run.
func (l loader) need(w widget.Widget, planFor func(*widget.Query, *filter.Filter) *plan) func() (any, error) {
	switch w.Type {
	case widget.TypeMarkdown:
		return func() (any, error) { return textData{Text: widget.Render(w.Text, l.env.Values)}, nil }
	case widget.TypeActions:
		// The page asks for the actions itself, as it does on entity pages,
		// since who can run one depends on the entity.
		return nil
	case widget.TypeStat:
		if w.Value == nil {
			return nil
		}
		return l.stat(w, planFor(&w.Value.Query, w.Value.Query.Filter))
	case widget.TypeScorecard:
		return l.scorecard(w, planFor)
	case widget.TypeBar, widget.TypeDonut, widget.TypeSplit:
		if w.Query == nil {
			return nil
		}
		return l.groups(w, planFor(w.Query, w.Query.Filter))
	case widget.TypeMatrix:
		if w.Query == nil {
			return nil
		}
		return l.matrix(w, planFor(w.Query, w.Query.Filter))
	case widget.TypeTrend:
		if w.Query == nil {
			return nil
		}
		return l.trend(w, planFor(w.Query, w.Query.Filter))
	case widget.TypeTable:
		if w.Query == nil {
			return nil
		}
		return l.table(w, planFor(w.Query, w.Query.Filter))
	}

	return nil
}

func (l loader) stat(w widget.Widget, p *plan) func() (any, error) {
	v := *w.Value
	op, path := v.Op()
	unit := l.unit(p.query.Blueprint, v.Summary)
	if op == widget.OpJQ {
		if p.documents == nil {
			p.documents = &documents{}
		}
		return func() (any, error) {
			if p.err != nil {
				return nil, p.err
			}
			value, err := l.jqValue(p.documents, v.JQ)
			if err != nil {
				return nil, err
			}
			return statData{Value: value, Unit: unit, Link: l.link(w, nil)}, nil
		}
	}
	i := len(p.aggregates)
	p.aggregates = append(p.aggregates, entity.Aggregate{Op: op, Path: path, Share: l.bind(v.Share)})
	total := -1
	if op == widget.OpShare {
		total = p.countIndex()
	}

	return func() (any, error) {
		if p.err != nil {
			return nil, p.err
		}
		if p.valuesErr != nil {
			return nil, p.valuesErr
		}
		data := statData{Value: p.values[i], Unit: unit, Link: l.link(w, nil)}
		if total >= 0 && p.values[i] != nil && p.values[total] != nil {
			n := int(math.Round(*p.values[total]))
			matched := int(math.Round(*p.values[i] * float64(n) / 100))
			data.Count, data.Total = &matched, &n
		}
		return data, nil
	}
}

// unit is what follows a summary's number: % for a share, the summary's
// own, or else the unit of the field it works on.
func (l loader) unit(bp blueprint.Spec, s widget.Summary) string {
	op, path := s.Op()
	switch {
	case op == widget.OpShare:
		return "%"
	case s.Unit != "" || op == widget.OpCount:
		return s.Unit
	}
	if info, err := entity.Describe(l.blueprints, bp, path); err == nil && info.Property != nil && info.Property.Display != nil {
		return info.Property.Display.Unit
	}

	return ""
}

// measurement is where a measure's aggregates are among those a statement
// works out: its value, and for a share, how many it applies to and how
// many of those match, so it can say how many of how many. A measure that
// counts without narrowing reads the count every group has.
type measurement struct {
	at    int
	share bool
	count bool
}

// measurement adds what a measure reads to aggregates, its filters bound
// for whoever is looking; nil counts.
func (l loader) measurement(m *widget.Measure, aggregates *[]entity.Aggregate) measurement {
	if m == nil {
		return measurement{at: -1, count: true}
	}
	op, path := m.Op()
	narrow := l.bind(m.Filter)
	out := measurement{at: len(*aggregates), share: op == widget.OpShare, count: op == widget.OpCount}
	*aggregates = append(*aggregates, entity.Aggregate{Op: op, Path: path, Share: l.bind(m.Share), Filter: narrow})
	if out.share {
		*aggregates = append(*aggregates,
			entity.Aggregate{Op: widget.OpCount, Filter: narrow},
			entity.Aggregate{Op: widget.OpCount, Filter: and(narrow, l.bind(m.Share))})
	}

	return out
}

// read is what the measure comes to from a statement's values and count.
func (m measurement) read(values []*float64, count int) reading {
	if m.at < 0 {
		n := float64(count)
		return reading{Value: &n}
	}
	r := reading{Value: values[m.at]}
	if m.share {
		total, matched := countOf(values[m.at+1]), countOf(values[m.at+2])
		r.Count, r.Total = &matched, &total
	}

	return r
}

// groupSort is the order a bar's or a matrix's sort asks for, with the
// measurement each measure title names.
func groupSort(sort string, measures map[string]measurement) entity.GroupSort {
	key, descending := strings.CutPrefix(sort, "-")
	out := entity.GroupSort{By: entity.SortCount, Descending: descending}
	switch key {
	case widget.SortCount:
	case widget.SortGroup:
		out.By = entity.SortGroup
	default:
		if m, ok := measures[key]; ok && m.at >= 0 {
			out.By = m.at
		}
	}

	return out
}

// countIndex is where the plan's count of its entities is among its
// aggregates, asking for it unless another stat already has.
func (p *plan) countIndex() int {
	for j, a := range p.aggregates {
		if a.Op == widget.OpCount {
			return j
		}
	}
	p.aggregates = append(p.aggregates, entity.Aggregate{Op: widget.OpCount})

	return len(p.aggregates) - 1
}

func (l loader) scorecard(w widget.Widget, planFor func(*widget.Query, *filter.Filter) *plan) func() (any, error) {
	sc, ok := l.scorecards[w.Scorecard]
	if !ok {
		return func() (any, error) { return nil, httpx.NotFound("no published scorecard " + w.Scorecard) }
	}
	q := &widget.Query{Blueprint: sc.Blueprint}
	var f *filter.Filter
	if w.Query != nil {
		f = w.Query.Filter
	}
	p := planFor(q, f)
	t, ok := p.tiers[sc.Identifier]
	if !ok {
		t = &tiers{scorecard: sc}
		p.tiers[sc.Identifier] = t
	}

	return func() (any, error) {
		if p.err != nil {
			return nil, p.err
		}
		if t.err != nil {
			return nil, t.err
		}
		return tierData{
			Tiers: t.counts, Checks: t.checks, Link: l.link(w, nil),
			Scorecard: &tableScorecard{Identifier: sc.Identifier, Blueprint: sc.Blueprint, Tiers: sc.Tiers},
		}, nil
	}
}

// grouping asks a plan to count its entities by path, up to limit groups,
// unless another widget already has.
func (p *plan) grouping(path string, limit int) *grouping {
	g, ok := p.groups[path]
	if !ok {
		g = &grouping{path: path}
		p.groups[path] = g
	}
	g.limit = max(g.limit, limit)

	return g
}

func (l loader) table(w widget.Widget, p *plan) func() (any, error) {
	var paths []string
	jq := w.RowLink != nil
	for _, c := range w.Columns {
		if c.Path != "" {
			paths = append(paths, c.Path)
		}
		jq = jq || c.JQ != ""
	}
	key := fmt.Sprintf("%s\x00%d\x00%s", w.Sort, w.Limit, strings.Join(paths, "\x00"))
	pg, ok := p.pages[key]
	if !ok {
		pg = &page{sort: w.Sort, limit: w.Limit, paths: paths}
		p.pages[key] = pg
	}

	return func() (any, error) {
		if p.err != nil {
			return nil, p.err
		}
		if pg.err != nil {
			return nil, pg.err
		}
		data := tableData{
			Blueprint: p.query.Blueprint.Identifier, Total: pg.total,
			Columns: l.columns(w, p.query.Blueprint), Rows: make([]tableRow, len(pg.rows)), Link: l.link(w, nil),
		}
		if data.Link != nil && data.Link.Blueprint == p.query.Blueprint.Identifier {
			data.Link.Sort = w.Sort
		}
		for i, r := range pg.rows {
			data.Rows[i] = tableRow{Identifier: r.Identifier, Title: r.Title, Values: make([]any, len(w.Columns))}
			if hasLinks(r.Links) {
				data.Rows[i].Links = make([][]entity.Link, len(w.Columns))
			}
			at := 0
			for j, c := range w.Columns {
				if c.Path == "" {
					continue
				}
				data.Rows[i].Values[j] = r.Values[at]
				if data.Rows[i].Links != nil {
					data.Rows[i].Links[j] = r.Links[at]
				}
				at++
			}
		}
		if jq && len(pg.rows) > 0 {
			if err := l.tableJQ(w, p.query, pg.rows, data.Rows); err != nil {
				return nil, err
			}
		}
		return data, nil
	}
}

func hasLinks(links [][]entity.Link) bool {
	return slices.ContainsFunc(links, func(l []entity.Link) bool { return l != nil })
}

// columns says how each of a table's columns shows, from what its path ends on.
func (l loader) columns(w widget.Widget, bp blueprint.Spec) []tableColumn {
	out := make([]tableColumn, len(w.Columns))
	for i, c := range w.Columns {
		col := tableColumn{Title: c.Title, Path: c.Path, JQ: c.JQ, Unit: c.Unit}
		if c.Path != "" {
			info, _ := entity.Describe(l.blueprints, bp, c.Path)
			if col.Title == "" {
				col.Title = info.Title
			}
			col.BuiltIn = info.BuiltIn
			if sc, ok := l.scorecards[info.Scorecard]; ok {
				col.Scorecard = &tableScorecard{Identifier: sc.Identifier, Blueprint: sc.Blueprint, Tiers: sc.Tiers}
			} else {
				col.Property = info.Property
			}
		}
		out[i] = col
	}

	return out
}

// tableJQ loads the shown rows' entities, which jq columns and row links
// read whole, and fills in what they give.
func (l loader) tableJQ(w widget.Widget, q entity.Query, rows []entity.Row, out []tableRow) error {
	identifiers := make([]any, len(rows))
	for i, r := range rows {
		identifiers[i] = r.Identifier
	}
	q.Filter = &filter.Filter{Property: "identifier", Op: filter.OpIn, Values: identifiers}
	q.Sort, q.Page, q.PageSize = "", 1, len(rows)
	docs, err := l.documents(l.ctx, q)
	if err != nil {
		return err
	}
	byIdentifier := map[string]any{}
	for _, doc := range docs {
		byIdentifier[doc.(map[string]any)["identifier"].(string)] = doc
	}
	ctx, cancel := context.WithTimeout(l.ctx, tableTimeout)
	defer cancel()
	for i := range out {
		input := byIdentifier[out[i].Identifier]
		for j, c := range w.Columns {
			if c.JQ == "" {
				continue
			}
			code, err := widget.CompileJQ(c.JQ, widget.Scope{Entity: true})
			if err == nil {
				out[i].Values[j], err = widget.RunJQ(ctx, code, input, l.env)
			}
			if err != nil {
				return httpx.BadRequest(fmt.Sprintf("column %s, for %s: %v", c.Title, out[i].Identifier, err))
			}
		}
		if w.RowLink != nil {
			out[i].Link = l.template(ctx, w, w.RowLink, input)
		}
	}

	return nil
}

// tasks lists the statements a plan runs, each filling in its own part.
func (l loader) tasks(p *plan) []func() {
	if p.err != nil {
		return nil
	}
	var tasks []func()
	// Each statement is a span of its own, saying which widgets wait on it,
	// since widgets that read the same query share it.
	read := func(name string, fn func(ctx context.Context) error, attrs ...attribute.KeyValue) func() {
		return func() {
			ctx, span := telemetry.Start(l.ctx, name, append(attrs, telemetry.Blueprint.String(p.query.Blueprint.Identifier),
				attribute.StringSlice("gp.widgets", p.widgets))...)
			telemetry.End(span, fn(ctx))
		}
	}
	if len(p.aggregates) > 0 {
		tasks = append(tasks, read("read aggregates", func(ctx context.Context) error {
			p.values, p.valuesErr = l.store.Aggregates(ctx, p.match, p.aggregates)
			return p.valuesErr
		}))
	}
	for _, g := range p.groups {
		tasks = append(tasks, read("read groups", func(ctx context.Context) error {
			g.facets, g.none, g.err = l.store.Group(ctx, p.match, g.path, g.limit)
			return g.err
		}, attribute.String("gp.group_by", g.path)))
	}
	for _, g := range p.measured {
		tasks = append(tasks, read("read measured groups", func(ctx context.Context) error {
			g.groups, g.none, g.err = l.store.GroupBy(ctx, p.match, g.path, g.aggregates, g.sort, g.limit)
			return g.err
		}, attribute.String("gp.group_by", g.path)))
	}
	for _, t := range p.trends {
		tasks = append(tasks, read("read trend", func(ctx context.Context) error {
			t.days, t.all, t.err = l.store.Trend(ctx, p.match, t.path, t.from, t.to, t.aggregates)
			return t.err
		}, attribute.String("gp.date", t.path)))
	}
	for _, pg := range p.pages {
		tasks = append(tasks, read("read rows", func(ctx context.Context) error {
			pg.rows, pg.total, pg.err = l.store.Rows(ctx, p.match, pg.sort, pg.limit, pg.paths)
			return pg.err
		}))
	}
	for _, t := range p.tiers {
		tasks = append(tasks, read("read tiers", func(ctx context.Context) error {
			t.counts, t.checks, t.err = scorecard.Tiers(ctx, l.db, t.scorecard, l.blueprints, p.query.Blueprint, p.query.Filter)
			return t.err
		}, telemetry.Scorecard.String(t.scorecard.Identifier)))
	}
	if d := p.documents; d != nil {
		tasks = append(tasks, read("read documents", func(ctx context.Context) error {
			q := p.query
			q.Page, q.PageSize = 1, MaxJQEntities+1
			d.list, d.err = l.documents(ctx, q)
			return d.err
		}))
	}

	return tasks
}

// run runs tasks, at most parallel at once, and waits for them all.
func run(tasks []func()) {
	slots := make(chan struct{}, parallel)
	var wg sync.WaitGroup
	for _, task := range tasks {
		wg.Add(1)
		slots <- struct{}{}
		go func() {
			defer func() { <-slots; wg.Done() }()
			task()
		}()
	}
	wg.Wait()
}

// documents loads a page of q's entities as jq reads them, with their
// relations' titles, lookups and scorecard results.
func (l loader) documents(ctx context.Context, q entity.Query) ([]any, error) {
	page, err := l.store.List(ctx, q)
	if err != nil {
		return nil, err
	}
	if err := l.store.Resolve(ctx, l.blueprints, q.Blueprint, page.Items); err != nil {
		return nil, err
	}
	out := make([]any, len(page.Items))
	for i, e := range page.Items {
		out[i] = entity.Document(e)
	}

	return out, nil
}

// jqValue runs a stat's jq over the query's entities, as a list.
func (l loader) jqValue(d *documents, expression string) (*float64, error) {
	if d.err != nil {
		return nil, d.err
	}
	code, err := widget.CompileJQ(expression, widget.Scope{Entity: true})
	if err != nil {
		return nil, httpx.BadRequest("jq doesn't compile: " + err.Error())
	}
	if len(d.list) > MaxJQEntities {
		return nil, httpx.BadRequest(fmt.Sprintf("jq runs over at most %d entities, and the query matches more; narrow it, or use count, sum, avg, min, max or share", MaxJQEntities))
	}
	result, err := widget.RunJQ(l.ctx, code, d.list, l.env)
	if err != nil {
		return nil, httpx.BadRequest("jq: " + err.Error())
	}
	switch v := result.(type) {
	case nil:
		return nil, nil
	case int:
		n := float64(v)
		return &n, nil
	case float64:
		return &v, nil
	case *big.Int:
		n, _ := new(big.Float).SetInt(v).Float64()
		return &n, nil
	}

	return nil, httpx.BadRequest(fmt.Sprintf("jq gave %s, which isn't a number", describe(result)))
}

func (l loader) bind(f *filter.Filter) *filter.Filter {
	return widget.BindContext(f, l.env.Values, l.env.Data)
}

// linkFilter is a filter bound and simplified for a link, which is what
// the catalog shows as chips. It simplifies after binding, as only then
// are the values known.
func (l loader) linkFilter(f *filter.Filter) *filter.Filter {
	return filter.And(l.bind(f))
}

// explain says why a widget couldn't load. A filter or path the data can't
// answer is the builder's to fix, so it says what; anything else is the
// server's, which the log has.
func (l loader) explain(w widget.Widget, err error) string {
	var filterErr entity.FilterError
	var httpErr httpx.Error
	switch {
	case errors.As(err, &filterErr):
		return filterErr.Error()
	case errors.As(err, &httpErr):
		return httpErr.Message
	}
	l.log(l.ctx, "widget not loaded", err, telemetry.Widget.String(w.ID), telemetry.WidgetType.String(w.Type))

	return "the widget's data couldn't be loaded"
}

// link is where a widget's link goes, with extra narrowing a query link
// further, as each group's value does. It's nil without a link, or when
// the link can't be made, which the log says why.
func (l loader) link(w widget.Widget, extra *filter.Filter) *linkData {
	link := w.Link
	if link == nil {
		return nil
	}
	if link.Query == nil {
		return l.template(l.ctx, w, link, nil)
	}
	if _, ok := l.blueprints[link.Query.Blueprint]; !ok {
		return nil
	}
	return &linkData{Blueprint: link.Query.Blueprint, Filter: filter.And(l.bind(link.Query.Filter), l.bind(link.Filter), extra)}
}

// groupLink links a group's entities. A group's value already met every
// condition the widget's own query puts on a field holding one value, so
// when the link is to that query those give way to the group's.
func (l loader) groupLink(w widget.Widget, single bool, condition *filter.Filter) *linkData {
	data := l.link(w, nil)
	if data == nil || !single || w.Link.Query == nil || !reflect.DeepEqual(w.Link.Query, w.Query) {
		return l.link(w, condition)
	}
	var conditions []*filter.Filter
	if data.Filter != nil {
		parts := []filter.Filter{*data.Filter}
		if len(data.Filter.And) > 0 {
			parts = data.Filter.And
		}
		for _, part := range parts {
			if part.Property != w.GroupBy {
				conditions = append(conditions, &part)
			}
		}
	}
	// The group's condition goes last, so it reads after the query's.
	data.Filter = filter.And(append(conditions, condition)...)

	return data
}

// template fills in a url or path link template for input, the row's
// entity or nothing.
func (l loader) template(ctx context.Context, w widget.Widget, link *widget.Link, input any) *linkData {
	text := link.URL
	if text == "" {
		text = link.Path
	}
	href, err := widget.RenderTemplate(ctx, text, input, l.env)
	if err == nil {
		err = checkHref(href, link.Path != "")
	}
	if err != nil {
		l.log(ctx, "widget link not made", err, telemetry.Widget.String(w.ID))
		return nil
	}

	return &linkData{Href: href}
}

// checkHref checks a filled-in link still goes where its template said: a
// web URL, or a path that stays in the portal.
func checkHref(href string, path bool) error {
	if path {
		if !strings.HasPrefix(href, "/") || strings.HasPrefix(href, "//") || strings.HasPrefix(href, "/\\") {
			return fmt.Errorf("%q isn't a path in the portal", href)
		}
		return nil
	}
	u, err := url.Parse(href)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return fmt.Errorf("%q isn't a web URL", href)
	}

	return nil
}

// entityVariable is the page entity as jq reads $entity: flat, as jq
// reads every entity.
func entityVariable(e *entity.Entity) map[string]any { return entity.Document(e) }

func describe(value any) string {
	switch value.(type) {
	case string:
		return "text"
	case bool:
		return "true or false"
	case []any:
		return "a list"
	case map[string]any:
		return "an object"
	}

	return fmt.Sprintf("%T", value)
}
