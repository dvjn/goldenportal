package scorecard

import (
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"gofr.dev/pkg/gofr"

	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/httpx"
)

// EvaluateSchedule is how often every scorecard is re-evaluated, as a cron
// expression, so results follow entity changes and time-based checks.
const EvaluateSchedule = "* * * * *"

// Register serves scorecards. The handlers read the request and call the
// services below, which MCP calls too.
func Register(app httpx.Routes) {
	configdoc.Register(app, Kind)
	app.GET("scorecards/{identifier}/summary", summary)
	app.GET("scorecards/{identifier}/results", listResults)
	app.POST("scorecards/{identifier}/evaluate", evaluateNow)
	app.GET("catalog/{blueprint}/scorecards", forEntity)
	app.AddCronJob(EvaluateSchedule, "evaluate-scorecards", evaluateAll)
}

// published returns a published scorecard's spec and row ID.
func published(ctx *gofr.Context, name string) (Spec, string, error) {
	store := configdoc.NewStore(ctx.SQL, Name)
	record, err := store.Get(ctx, name)
	if errors.Is(err, configdoc.ErrNotFound) || (err == nil && !record.Published()) {
		return Spec{}, "", httpx.Error{Code: http.StatusNotFound, Message: "no published scorecard " + name}
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

func summary(ctx *gofr.Context) (any, error) { return GetSummary(ctx, ctx.PathParam("identifier")) }

// GetSummary reads how many entities reached each of a scorecard's tiers
// and how each criterion fares.
func GetSummary(ctx *gofr.Context, name string) (SummaryView, error) {
	if _, err := auth.Require(ctx, auth.RoleMember); err != nil {
		return SummaryView{}, err
	}
	spec, id, err := published(ctx, name)
	if err != nil {
		return SummaryView{}, err
	}
	s, err := summarize(ctx, ctx.SQL, id, spec)
	if err != nil {
		return SummaryView{}, err
	}

	return SummaryView{Spec: spec, Summary: s}, nil
}

var pageSizes = []int{10, 20, 50, 100}

// ResultsRequest asks for a page of a scorecard's results, searched by
// Search and narrowed to Tiers, which are the scorecard's tiers or none. A
// zero Page or Size means the first page or the smallest size.
type ResultsRequest struct {
	Search     string
	Tiers      []string
	Page, Size int
}

func listResults(ctx *gofr.Context) (any, error) {
	req := ResultsRequest{Search: ctx.Param("q")}
	// tier is a comma-separated list of the scorecard's tiers, or none.
	if value := ctx.Param("tier"); value != "" {
		req.Tiers = strings.Split(value, ",")
	}
	var err error
	if req.Page, err = intParam(ctx, "page"); err != nil {
		return nil, badRequest("page must be a whole number from 1")
	}
	if req.Size, err = intParam(ctx, "size"); err != nil {
		return nil, badRequest("size must be 10, 20, 50 or 100")
	}

	return httpx.Result(Results(ctx, ctx.PathParam("identifier"), req))
}

// Results pages a scorecard's entity results, worst tier first.
func Results(ctx *gofr.Context, name string, req ResultsRequest) (ResultPage, error) {
	if _, err := auth.Require(ctx, auth.RoleMember); err != nil {
		return ResultPage{}, err
	}
	spec, id, err := published(ctx, name)
	if err != nil {
		return ResultPage{}, err
	}

	q := ResultQuery{Search: req.Search, Page: 1, PageSize: pageSizes[0]}
	for _, tier := range req.Tiers {
		if tier != "none" && !slices.ContainsFunc(spec.Tiers, func(t Tier) bool { return t.Name == tier }) {
			return ResultPage{}, badRequest("tier must be the scorecard's tiers, or none")
		}
		q.Tiers = append(q.Tiers, tier)
	}
	if req.Page != 0 {
		if req.Page < 1 {
			return ResultPage{}, badRequest("page must be a whole number from 1")
		}
		q.Page = req.Page
	}
	if req.Size != 0 {
		if !slices.Contains(pageSizes, req.Size) {
			return ResultPage{}, badRequest("size must be 10, 20, 50 or 100")
		}
		q.PageSize = req.Size
	}

	return results(ctx, ctx.SQL, id, spec, q)
}

// intParam reads a whole-number query parameter, zero when it's absent.
func intParam(ctx *gofr.Context, name string) (int, error) {
	value := ctx.Param(name)
	if value == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(value)
	if err == nil && n == 0 {
		err = strconv.ErrRange
	}

	return n, err
}

// Evaluated is how many entities an evaluation scored.
type Evaluated struct {
	Entities int `json:"entities"`
}

func evaluateNow(ctx *gofr.Context) (any, error) {
	return httpx.Result(EvaluateNow(ctx, ctx.PathParam("identifier")))
}

// EvaluateNow evaluates a published scorecard and stores its results.
func EvaluateNow(ctx *gofr.Context, name string) (Evaluated, error) {
	if _, err := auth.Require(ctx, auth.RoleBuilder); err != nil {
		return Evaluated{}, err
	}
	if _, _, err := published(ctx, name); err != nil {
		return Evaluated{}, err
	}

	n, err := Evaluate(ctx, ctx.SQL, name)
	if errors.Is(err, ErrEvaluating) {
		return Evaluated{}, httpx.Error{Code: http.StatusConflict, Message: err.Error()}
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
		return nil, badRequest("name the entity with blueprint and identifier")
	}

	return entityResults(ctx, ctx.SQL, blueprint, identifier)
}

// evaluateAll re-evaluates every published scorecard.
func evaluateAll(ctx *gofr.Context) {
	records, err := configdoc.NewStore(ctx.SQL, Name).List(ctx)
	if err != nil {
		ctx.Errorf("list scorecards to evaluate: %v", err)
		return
	}
	for _, record := range records {
		if !record.Published() {
			continue
		}
		if _, err := Evaluate(ctx, ctx.SQL, record.Identifier); err != nil && !errors.Is(err, ErrEvaluating) {
			ctx.Errorf("evaluate scorecard %s: %v", record.Identifier, err)
		}
	}
}

func badRequest(message string) error {
	return httpx.Error{Code: http.StatusBadRequest, Message: message}
}
