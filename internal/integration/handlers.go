package integration

import (
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"gofr.dev/pkg/gofr"

	"github.com/dvjn/goldenportal/internal/audit"
	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/httpx"
)

const (
	// tokenPrefix marks ingestion tokens, so secret scanners can recognise them.
	tokenPrefix        = "gpi_"
	tokenDisplayLength = len(tokenPrefix) + 8
	maxTokenName       = 100
)

var runPageSizes = []int{10, 20, 50, 100}

func Register(app *gofr.App) {
	configdoc.Register(app, Kind)
	app.GET("/api/v1/connectors", listConnectors)
	app.GET("/api/v1/integrations/{identifier}/tokens", listTokens)
	app.POST("/api/v1/integrations/{identifier}/tokens", createToken)
	app.DELETE("/api/v1/integrations/{identifier}/tokens/{id}", revokeToken)
	app.GET("/api/v1/integrations/{identifier}/runs", listRuns)
	app.POST("/api/v1/integrations/{identifier}/sync", syncNow)
	app.AddCronJob(SyncSchedule, "sync-pull-integrations", syncAll)
	app.GET("/api/v1/blueprints/{identifier}/usage", blueprintUsage)
	app.GET("/api/v1/integration-status", listStatus)
	app.POST(auth.IngestPrefix+"{integration}", ingest)
	app.DELETE(auth.IngestPrefix+"{integration}/entities/{blueprint}/{identifier:.+}", deleteEntity)
}

func listConnectors(ctx *gofr.Context) (any, error) {
	if _, err := auth.Require(ctx, auth.RoleMember); err != nil {
		return nil, err
	}

	return Connectors, nil
}

// integrationID returns the row ID of the integration in the path. Tokens
// and runs belong to it, so it needs to exist, though not be published.
func integrationID(ctx *gofr.Context) (string, string, error) {
	identifier := ctx.PathParam("identifier")
	if !configdoc.ValidIdentifier(identifier) {
		return "", "", notFound()
	}
	var id string
	err := ctx.SQL.QueryRowContext(ctx, `SELECT id FROM config_documents WHERE kind = $1 AND identifier = $2`,
		Name, identifier).Scan(&id)
	if err != nil {
		return "", "", notFound()
	}

	return id, identifier, nil
}

func listTokens(ctx *gofr.Context) (any, error) {
	if _, err := auth.Require(ctx, auth.RoleBuilder); err != nil {
		return nil, err
	}
	id, _, err := integrationID(ctx)
	if err != nil {
		return nil, err
	}

	return NewStore(ctx.SQL).Tokens(ctx, id)
}

type createTokenRequest struct {
	Name string `json:"name"`
}

type createdToken struct {
	Token
	// Secret is shown once, now.
	Secret string `json:"secret"`
}

func createToken(ctx *gofr.Context) (any, error) {
	p, err := auth.Require(ctx, auth.RoleBuilder)
	if err != nil {
		return nil, err
	}
	id, identifier, err := integrationID(ctx)
	if err != nil {
		return nil, err
	}
	var req createTokenRequest
	if err := ctx.Bind(&req); err != nil {
		return nil, badRequest("the request body is not valid JSON")
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > maxTokenName {
		return nil, badRequest("give the token a name of up to 100 characters")
	}

	if pulls(ctx, identifier) {
		return nil, httpx.Error{Code: http.StatusConflict, Message: "the " + identifier + " integration pulls its data, so it has no ingestion tokens"}
	}

	secret := tokenPrefix + auth.NewSecret()
	token, err := NewStore(ctx.SQL).CreateToken(ctx, id, name, secret[:tokenDisplayLength], auth.HashSecret(secret), p.ID)
	if err != nil {
		return nil, err
	}
	auth.Record(ctx, p, audit.IngestionTokenCreate, audit.Target{Type: Name, ID: identifier, Label: identifier},
		map[string]any{"token": name})

	return createdToken{Token: token, Secret: secret}, nil
}

func revokeToken(ctx *gofr.Context) (any, error) {
	p, err := auth.Require(ctx, auth.RoleBuilder)
	if err != nil {
		return nil, err
	}
	id, identifier, err := integrationID(ctx)
	if err != nil {
		return nil, err
	}
	tokenID := ctx.PathParam("id")
	if _, err := uuid.Parse(tokenID); err != nil {
		return nil, httpx.Error{Code: http.StatusNotFound, Message: "no such token"}
	}

	name, err := NewStore(ctx.SQL).RevokeToken(ctx, id, tokenID)
	if errors.Is(err, ErrTokenNotFound) {
		return nil, httpx.Error{Code: http.StatusNotFound, Message: "no such token"}
	}
	if err != nil {
		return nil, err
	}
	auth.Record(ctx, p, audit.IngestionTokenRevoke, audit.Target{Type: Name, ID: identifier, Label: identifier},
		map[string]any{"token": name})

	return nil, nil
}

// listRuns returns a page of an integration's runs, newest first. It filters
// by free text (q), triggers and outcomes (comma-separated lists), and pages
// with page and size.
func listRuns(ctx *gofr.Context) (any, error) {
	if _, err := auth.Require(ctx, auth.RoleBuilder); err != nil {
		return nil, err
	}
	id, _, err := integrationID(ctx)
	if err != nil {
		return nil, err
	}

	q := RunQuery{Search: ctx.Param("q"), Page: 1, PageSize: runPageSizes[0]}
	if value := ctx.Param("page"); value != "" {
		page, err := strconv.Atoi(value)
		if err != nil || page < 1 {
			return nil, badRequest("page must be a whole number from 1")
		}
		q.Page = page
	}
	if value := ctx.Param("size"); value != "" {
		size, err := strconv.Atoi(value)
		if err != nil || !slices.Contains(runPageSizes, size) {
			return nil, badRequest("size must be 10, 20, 50 or 100")
		}
		q.PageSize = size
	}
	if q.Triggers, err = listParam(ctx, "trigger", TriggerSchedule, TriggerManual, TriggerPush); err != nil {
		return nil, err
	}
	if q.Outcomes, err = listParam(ctx, "outcome", OutcomeSucceeded, OutcomeWarnings, OutcomeFailed); err != nil {
		return nil, err
	}

	return NewStore(ctx.SQL).Runs(ctx, id, q)
}

// listParam reads a comma-separated list of the allowed values.
func listParam(ctx *gofr.Context, name string, allowed ...string) ([]string, error) {
	var values []string
	if value := ctx.Param(name); value != "" {
		for v := range strings.SplitSeq(value, ",") {
			if !slices.Contains(allowed, v) {
				return nil, badRequest("unknown " + name + " " + strconv.Quote(v))
			}
			values = append(values, v)
		}
	}

	return values, nil
}

func notFound() error {
	return httpx.Error{Code: http.StatusNotFound, Message: "no such integration"}
}

func badRequest(message string) error {
	return httpx.Error{Code: http.StatusBadRequest, Message: message}
}

// pulls reports whether the integration's latest version, draft or
// published, uses a pull connector.
func pulls(ctx *gofr.Context, identifier string) bool {
	record, err := configdoc.NewStore(ctx.SQL, Name).Get(ctx, identifier)
	if err != nil {
		return false
	}
	source := record.PublishedSource
	if record.HasDraft() {
		source = record.DraftSource
	}
	spec, _ := Parse(source)
	c, _ := connector(spec.Connector.Type)

	return c.Mode == ModePull
}
