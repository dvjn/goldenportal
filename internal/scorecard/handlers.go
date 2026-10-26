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

func Register(app *gofr.App) {
	configdoc.Register(app, Kind)
	app.GET("/api/v1/scorecards/{identifier}/summary", summary)
	app.GET("/api/v1/scorecards/{identifier}/results", listResults)
	app.POST("/api/v1/scorecards/{identifier}/evaluate", evaluateNow)
	app.GET("/api/v1/entity-scorecards", forEntity)
	app.GET("/api/v1/scorecard-columns", columns)
	app.AddCronJob(EvaluateSchedule, "evaluate-scorecards", evaluateAll)
}

// published returns a published scorecard's spec and row ID.
func published(ctx *gofr.Context) (Spec, string, error) {
	name := ctx.PathParam("identifier")
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

type summaryView struct {
	Spec Spec `json:"spec"`
	Summary
}

func summary(ctx *gofr.Context) (any, error) {
	if _, err := auth.Require(ctx, auth.RoleMember); err != nil {
		return nil, err
	}
	spec, id, err := published(ctx)
	if err != nil {
		return nil, err
	}
	s, err := summarize(ctx, ctx.SQL, id, spec)
	if err != nil {
		return nil, err
	}

	return summaryView{Spec: spec, Summary: s}, nil
}

var pageSizes = []int{10, 20, 50, 100}

func listResults(ctx *gofr.Context) (any, error) {
	if _, err := auth.Require(ctx, auth.RoleMember); err != nil {
		return nil, err
	}
	spec, id, err := published(ctx)
	if err != nil {
		return nil, err
	}

	q := ResultQuery{Tier: ctx.Param("tier"), Search: ctx.Param("q"), Page: 1, PageSize: pageSizes[0]}
	if q.Tier != "" && q.Tier != "none" && !slices.ContainsFunc(spec.Tiers, func(t Tier) bool { return t.Name == q.Tier }) {
		return nil, badRequest("tier must be one of the scorecard's tiers, or none")
	}
	if value := ctx.Param("page"); value != "" {
		page, err := strconv.Atoi(value)
		if err != nil || page < 1 {
			return nil, badRequest("page must be a whole number from 1")
		}
		q.Page = page
	}
	if value := ctx.Param("size"); value != "" {
		size, err := strconv.Atoi(value)
		if err != nil || !slices.Contains(pageSizes, size) {
			return nil, badRequest("size must be 10, 20, 50 or 100")
		}
		q.PageSize = size
	}

	page, err := results(ctx, ctx.SQL, id, spec, q)
	if err != nil {
		return nil, err
	}

	return page, nil
}

type evaluated struct {
	Entities int `json:"entities"`
}

func evaluateNow(ctx *gofr.Context) (any, error) {
	if _, err := auth.Require(ctx, auth.RoleBuilder); err != nil {
		return nil, err
	}
	if _, _, err := published(ctx); err != nil {
		return nil, err
	}

	n, err := Evaluate(ctx, ctx.SQL, ctx.PathParam("identifier"))
	if errors.Is(err, ErrEvaluating) {
		return nil, httpx.Error{Code: http.StatusConflict, Message: err.Error()}
	}
	if err != nil {
		return nil, err
	}

	return evaluated{Entities: n}, nil
}

// forEntity lists an entity's results on every scorecard that scores it.
func forEntity(ctx *gofr.Context) (any, error) {
	if _, err := auth.Require(ctx, auth.RoleMember); err != nil {
		return nil, err
	}
	bp, identifier := ctx.Param("blueprint"), ctx.Param("identifier")
	if bp == "" || identifier == "" {
		return nil, badRequest("name the entity with blueprint and identifier")
	}

	out, err := entityResults(ctx, ctx.SQL, bp, identifier)
	if err != nil {
		return nil, err
	}

	return out, nil
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

type column struct {
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
	Tiers      []Tier `json:"tiers"`
}

func columns(ctx *gofr.Context) (any, error) {
	if _, err := auth.Require(ctx, auth.RoleMember); err != nil {
		return nil, err
	}
	if ctx.Param("blueprint") == "" {
		return nil, badRequest("choose a blueprint")
	}
	specs, err := Published(ctx, ctx.SQL)
	if err != nil {
		return nil, err
	}
	out := []column{}
	for _, spec := range specs {
		if spec.Blueprint == ctx.Param("blueprint") {
			out = append(out, column{Identifier: spec.Identifier, Title: spec.Title, Tiers: spec.Tiers})
		}
	}
	slices.SortFunc(out, func(a, b column) int {
		return strings.Compare(a.Title+"\x00"+a.Identifier, b.Title+"\x00"+b.Identifier)
	})
	return out, nil
}
