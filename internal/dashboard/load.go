package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/entity"
	"github.com/dvjn/goldenportal/internal/filter"
	"github.com/dvjn/goldenportal/internal/httpx"
	"github.com/dvjn/goldenportal/internal/scorecard"
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
		Value *float64  `json:"value"`
		Unit  string    `json:"unit,omitempty"`
		Link  *linkData `json:"link,omitempty"`
	}
	groupData struct {
		Groups []group `json:"groups"`
		None   int     `json:"none"`
		// NoneLink opens the entities without a value.
		NoneLink *linkData `json:"noneLink,omitempty"`
		Link     *linkData `json:"link,omitempty"`
	}
	group struct {
		entity.Facet
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
	logf       func(format string, args ...any)
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
	pages      map[string]*page
	tiers      map[string]*tiers
	documents  *documents
}

type grouping struct {
	path   string
	limit  int
	facets []entity.Facet
	none   int
	err    error
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
			return &plan{err: notFound("no published blueprint " + q.Blueprint)}
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
		if fn := l.need(w, planFor); fn != nil {
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
		data, err := fn()
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
	case widget.TypeBar, widget.TypeDonut:
		if w.Query == nil {
			return nil
		}
		return l.groups(w, planFor(w.Query, w.Query.Filter))
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
	unit := v.Unit
	switch op {
	case widget.OpJQ:
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
	case widget.OpShare:
		unit = "%"
	case widget.OpCount:
	default:
		if info, err := entity.Describe(l.blueprints, p.query.Blueprint, path); err == nil && unit == "" && info.Property != nil && info.Property.Display != nil {
			unit = info.Property.Display.Unit
		}
	}
	i := len(p.aggregates)
	p.aggregates = append(p.aggregates, entity.Aggregate{Op: op, Path: path, Share: l.bind(v.Share)})

	return func() (any, error) {
		if p.err != nil {
			return nil, p.err
		}
		if p.valuesErr != nil {
			return nil, p.valuesErr
		}
		return statData{Value: p.values[i], Unit: unit, Link: l.link(w, nil)}, nil
	}
}

func (l loader) scorecard(w widget.Widget, planFor func(*widget.Query, *filter.Filter) *plan) func() (any, error) {
	sc, ok := l.scorecards[w.Scorecard]
	if !ok {
		return func() (any, error) { return nil, notFound("no published scorecard " + w.Scorecard) }
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

func (l loader) groups(w widget.Widget, p *plan) func() (any, error) {
	g, ok := p.groups[w.GroupBy]
	if !ok {
		g = &grouping{path: w.GroupBy}
		p.groups[w.GroupBy] = g
	}
	g.limit = max(g.limit, w.Limit)

	return func() (any, error) {
		if p.err != nil {
			return nil, p.err
		}
		if g.err != nil {
			return nil, g.err
		}
		facets := g.facets[:min(len(g.facets), w.Limit)]
		data := groupData{Groups: make([]group, len(facets)), None: g.none}
		linkGroups := w.Link != nil && w.Link.Group
		if !linkGroups {
			data.Link = l.link(w, nil)
		}
		info, _ := entity.Describe(l.blueprints, p.query.Blueprint, w.GroupBy)
		single := info.Property == nil || (!info.Property.Many && info.Property.Type != "array") || info.Kind == "tier"
		for i, facet := range facets {
			// A group takes its property's color for its value, unless it
			// has its own, as a tier does.
			if facet.Color == "" && info.Property != nil && info.Property.Display != nil {
				facet.Color = info.Property.Display.Colors[fmt.Sprint(facet.Value)]
			}
			data.Groups[i] = group{Facet: facet}
			if linkGroups {
				data.Groups[i].Link = l.groupLink(w, single, &filter.Filter{Property: w.GroupBy, Op: filter.OpEq, Value: facet.Value})
			}
		}
		// A list counts as none when it's empty, which exists can't say.
		if g.none > 0 && linkGroups && single {
			data.NoneLink = l.groupLink(w, single, &filter.Filter{Not: &filter.Filter{Property: w.GroupBy, Op: filter.OpExists}})
		}
		return data, nil
	}
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
	docs, err := l.documents(q)
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
				return badRequest(fmt.Sprintf("column %s, for %s: %v", c.Title, out[i].Identifier, err))
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
	if len(p.aggregates) > 0 {
		tasks = append(tasks, func() { p.values, p.valuesErr = l.store.Aggregates(l.ctx, p.match, p.aggregates) })
	}
	for _, g := range p.groups {
		tasks = append(tasks, func() { g.facets, g.none, g.err = l.store.Group(l.ctx, p.match, g.path, g.limit) })
	}
	for _, pg := range p.pages {
		tasks = append(tasks, func() { pg.rows, pg.total, pg.err = l.store.Rows(l.ctx, p.match, pg.sort, pg.limit, pg.paths) })
	}
	for _, t := range p.tiers {
		tasks = append(tasks, func() {
			t.counts, t.checks, t.err = scorecard.Tiers(l.ctx, l.db, t.scorecard, l.blueprints, p.query.Blueprint, p.query.Filter)
		})
	}
	if d := p.documents; d != nil {
		tasks = append(tasks, func() {
			q := p.query
			q.Page, q.PageSize = 1, MaxJQEntities+1
			d.list, d.err = l.documents(q)
		})
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
func (l loader) documents(q entity.Query) ([]any, error) {
	page, err := l.store.List(l.ctx, q)
	if err != nil {
		return nil, err
	}
	if err := l.store.Resolve(l.ctx, l.blueprints, q.Blueprint, page.Entities); err != nil {
		return nil, err
	}
	out := make([]any, len(page.Entities))
	for i, e := range page.Entities {
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
		return nil, badRequest("jq doesn't compile: " + err.Error())
	}
	if len(d.list) > MaxJQEntities {
		return nil, badRequest(fmt.Sprintf("jq runs over at most %d entities, and the query matches more; narrow it, or use count, sum, avg, min, max or share", MaxJQEntities))
	}
	result, err := widget.RunJQ(l.ctx, code, d.list, l.env)
	if err != nil {
		return nil, badRequest("jq: " + err.Error())
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

	return nil, badRequest(fmt.Sprintf("jq gave %s, which isn't a number", describe(result)))
}

func (l loader) bind(f *filter.Filter) *filter.Filter {
	return widget.BindContext(f, l.env.Values, l.env.Data)
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
	l.logf("load widget %s: %v", w.ID, err)

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
	var conditions []filter.Filter
	for _, f := range []*filter.Filter{l.bind(link.Query.Filter), l.bind(link.Filter), extra} {
		switch {
		case f == nil:
		case len(f.And) > 0:
			conditions = append(conditions, f.And...)
		default:
			conditions = append(conditions, *f)
		}
	}
	data := &linkData{Blueprint: link.Query.Blueprint}
	switch len(conditions) {
	case 0:
	case 1:
		data.Filter = &conditions[0]
	default:
		data.Filter = &filter.Filter{And: conditions}
	}

	return data
}

// groupLink links a group's entities. A group's value already met every
// condition the widget's own query puts on a field holding one value, so
// when the link is to that query those give way to the group's.
func (l loader) groupLink(w widget.Widget, single bool, condition *filter.Filter) *linkData {
	data := l.link(w, condition)
	if data == nil || data.Filter == nil || len(data.Filter.And) == 0 || !single || !reflect.DeepEqual(w.Link.Query, w.Query) {
		return data
	}
	kept := slices.DeleteFunc(slices.Clone(data.Filter.And[:len(data.Filter.And)-1]), func(f filter.Filter) bool { return f.Property == w.GroupBy })
	if len(kept) == 0 {
		data.Filter = condition
	} else {
		data.Filter = &filter.Filter{And: append(kept, *condition)}
	}

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
		l.logf("link of widget %s: %v", w.ID, err)
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
