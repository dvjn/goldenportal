package scorecard

import (
	"errors"
	"slices"
	"strconv"
	"strings"

	"gofr.dev/pkg/gofr"

	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/entity"
	"github.com/dvjn/goldenportal/internal/httpx"
)

// Register serves scorecards and evaluates every one of them at start and
// on schedule, a cron expression. The handlers read the request and call
// the services below, which MCP calls too. committed, if set, follows up a
// scorecard's publish once it commits, after its evaluation is asked for.
func Register(app httpx.Routes, schedule string, committed func(ctx *gofr.Context)) {
	kind := Kind
	if committed != nil {
		kind.Committed = func(ctx *gofr.Context, next Spec) {
			Kind.Committed(ctx, next)
			committed(ctx)
		}
	}
	configdoc.Register(app, kind)
	app.GET("scorecards/{identifier}/summary", summary)
	app.GET("scorecards/{identifier}/results", listResults)
	app.POST("scorecards/{identifier}/evaluate", evaluateNow)
	app.GET("catalog/{blueprint}/scorecards", forEntity)
	app.AddCronJob(schedule, "evaluate-scorecards", func(ctx *gofr.Context) { evaluateAll(ctx, ctx.SQL) })
	app.OnStart(evaluateAtStart)
}

// published returns a published scorecard's spec and row ID.
func published(ctx *gofr.Context, name string) (Spec, string, error) {
	store := configdoc.NewStore(ctx.SQL, Name)
	record, err := store.Get(ctx, name)
	if errors.Is(err, configdoc.ErrNotFound) || (err == nil && !record.Published()) {
		return Spec{}, "", httpx.NotFound("no published scorecard " + name)
	}
	if err != nil {
		return Spec{}, "", err
	}
	spec, _ := Parse(record.PublishedSource)
	id, err := store.PublishedID(ctx, name)

	return spec, id, err
}

// SummaryView is a scorecard with how entities fare on it.
type SummaryView struct {
	Spec Spec `json:"spec"`
	Summary
}

func summary(ctx *gofr.Context) (any, error) {
	return httpx.Result(GetSummary(ctx, ctx.PathParam("identifier"), SummaryRequest{Filter: ctx.Param("filter")}))
}

// SummaryRequest narrows a summary to the entities Filter matches, as
// ResultsRequest does results; empty counts every entity.
type SummaryRequest struct {
	Filter string
}

// GetSummary reads how many of the entities a request matches reached each
// of a scorecard's tiers and how each criterion fares on them.
func GetSummary(ctx *gofr.Context, name string, req SummaryRequest) (SummaryView, error) {
	if _, err := auth.Require(ctx, auth.RoleMember); err != nil {
		return SummaryView{}, err
	}
	spec, id, err := published(ctx, name)
	if err != nil {
		return SummaryView{}, err
	}
	scope, err := scopeOf(ctx, spec, req.Filter)
	if err != nil {
		return SummaryView{}, err
	}
	s, err := summarize(ctx, ctx.SQL, id, spec, scope)
	if err != nil {
		return SummaryView{}, err
	}

	return SummaryView{Spec: spec, Summary: s}, nil
}

// scopeOf reads a filter on the blueprint a scorecard grades and checks it
// against that blueprint and the scorecards' tiers and checks, so a wrong
// name is the caller's to fix rather than matching nothing.
func scopeOf(ctx *gofr.Context, spec Spec, raw string) (Scope, error) {
	f, err := entity.ParseFilter(raw)
	if err != nil {
		return Scope{}, httpx.BadRequest(err.Error())
	}
	if f == nil {
		return Scope{}, nil
	}
	specs, err := blueprint.Published(ctx, ctx.SQL)
	if err != nil {
		return Scope{}, err
	}
	graded, ok := specs[spec.Blueprint]
	if !ok {
		return Scope{}, httpx.BadRequest("filter can't apply: blueprint " + spec.Blueprint + " isn't published")
	}
	ladders, err := blueprint.Ladders(ctx, ctx.SQL)
	if err != nil {
		return Scope{}, err
	}
	if err := entity.CheckFilter(specs, ladders, graded, *f); err != nil {
		return Scope{}, httpx.BadRequest(err.Error())
	}

	return Scope{Blueprints: specs, Ladders: ladders, Blueprint: graded, Filter: f}, nil
}

// ResultsRequest asks for a page of a scorecard's results, searched by
// Search and narrowed by Filter, the catalog's filter on the scorecard's
// blueprint as JSON, which reaches tiers and checks through its scorecard
// property. Sort is one of ResultSorts, or that with a leading - for
// descending; empty is worst tier first. A zero Page or Size means the
// first page or the smallest size.
type ResultsRequest struct {
	Search, Filter, Sort string
	Page, Size           int
}

// ResultSorts are the fields results sort by. Tiers sort up the scorecard's
// tiers, from none to the highest, not by name.
var ResultSorts = []string{"tier", "score", "passed", "failed", "skipped"}

func listResults(ctx *gofr.Context) (any, error) {
	req := ResultsRequest{Search: ctx.Param("q"), Filter: ctx.Param("filter"), Sort: ctx.Param("sort")}
	var err error
	if req.Page, req.Size, err = httpx.Paging(ctx.Param); err != nil {
		return nil, err
	}

	return httpx.Result(Results(ctx, ctx.PathParam("identifier"), req))
}

// Results pages a scorecard's entity results, worst tier first unless
// sorted otherwise.
func Results(ctx *gofr.Context, name string, req ResultsRequest) (ResultPage, error) {
	if _, err := auth.Require(ctx, auth.RoleMember); err != nil {
		return ResultPage{}, err
	}
	spec, id, err := published(ctx, name)
	if err != nil {
		return ResultPage{}, err
	}
	q, err := req.query()
	if err != nil {
		return ResultPage{}, err
	}
	if q.Scope, err = scopeOf(ctx, spec, req.Filter); err != nil {
		return ResultPage{}, err
	}

	return results(ctx, ctx.SQL, id, spec, q)
}

func (req ResultsRequest) query() (ResultQuery, error) {
	q := ResultQuery{Search: req.Search, Sort: req.Sort}
	var err error
	if q.Page, q.PageSize, err = httpx.CheckPaging(req.Page, req.Size); err != nil {
		return ResultQuery{}, err
	}
	if field := strings.TrimPrefix(req.Sort, "-"); req.Sort != "" && !slices.Contains(ResultSorts, field) {
		return ResultQuery{}, httpx.BadRequest("sort must be " + strings.Join(ResultSorts[:len(ResultSorts)-1], ", ") +
			" or " + ResultSorts[len(ResultSorts)-1] + ", with a leading - for descending, not " + strconv.Quote(req.Sort))
	}

	return q, nil
}

// Evaluated is how many entities an evaluation scored.
type Evaluated struct {
	Entities int `json:"entities"`
}

func evaluateNow(ctx *gofr.Context) (any, error) {
	return httpx.Result(EvaluateNow(ctx, ctx.PathParam("identifier")))
}

// EvaluateNow evaluates a published scorecard and stores its results, after
// any evaluation of it already under way.
func EvaluateNow(ctx *gofr.Context, name string) (Evaluated, error) {
	if _, err := auth.Require(ctx, auth.RoleBuilder); err != nil {
		return Evaluated{}, err
	}
	if _, _, err := published(ctx, name); err != nil {
		return Evaluated{}, err
	}

	n, err := evaluateInTurn(ctx, ctx.SQL, name)
	if errors.Is(err, ErrEvaluating) {
		return Evaluated{}, httpx.Conflict(err.Error())
	}
	if err != nil {
		return Evaluated{}, err
	}

	return Evaluated{Entities: n}, nil
}

func forEntity(ctx *gofr.Context) (any, error) {
	return ForEntity(ctx, ctx.PathParam("blueprint"), ctx.Param("identifier"))
}

// ForEntity lists an entity's results on every scorecard that scores it.
func ForEntity(ctx *gofr.Context, blueprint, identifier string) ([]EntityScorecard, error) {
	if _, err := auth.Require(ctx, auth.RoleMember); err != nil {
		return nil, err
	}
	if blueprint == "" || identifier == "" {
		return nil, httpx.BadRequest("name the entity with blueprint and identifier")
	}

	return entityResults(ctx, ctx.SQL, blueprint, identifier)
}
