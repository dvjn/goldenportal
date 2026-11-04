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

// Register serves integrations. The handlers read the request and call the
// services below, which MCP calls too.
func Register(app httpx.Routes) {
	configdoc.Register(app, Kind)
	app.GET("connectors", listConnectors)
	app.GET("integrations/{identifier}/tokens", listTokens)
	app.POST("integrations/{identifier}/tokens", createToken)
	app.DELETE("integrations/{identifier}/tokens/{id}", revokeToken)
	app.GET("integrations/{identifier}/runs", listRuns)
	app.POST("integrations/{identifier}/sync", syncNow)
	app.POST("integrations/{identifier}/check", checkNow)
	app.AddCronJob(SyncSchedule, "sync-pull-integrations", syncAll)
	app.GET("blueprints/{identifier}/usage", blueprintUsage)
	app.GET("integration-status", listStatus)
}

// RegisterIngest serves the push endpoints, outside the web API: callers
// sign in with an ingestion token, not a session.
func RegisterIngest(app *gofr.App) {
	app.POST(auth.IngestPrefix+"{integration}", ingest)
	app.DELETE(auth.IngestPrefix+"{integration}/entities/{blueprint}/{identifier:.+}", deleteEntity)
}

func listConnectors(ctx *gofr.Context) (any, error) { return ListConnectors(ctx) }

// ListConnectors lists the connector types and what each reads.
func ListConnectors(ctx *gofr.Context) ([]Connector, error) {
	if _, err := auth.Require(ctx, auth.RoleMember); err != nil {
		return nil, err
	}

	return Connectors, nil
}

// integrationID returns an integration's row ID. Tokens and runs belong to
// it, so it needs to exist, though not be published.
func integrationID(ctx *gofr.Context, identifier string) (string, error) {
	if !configdoc.ValidIdentifier(identifier) {
		return "", notFound()
	}
	var id string
	err := ctx.SQL.QueryRowContext(ctx, `SELECT id FROM config_documents WHERE kind = $1 AND identifier = $2`,
		Name, identifier).Scan(&id)
	if err != nil {
		return "", notFound()
	}

	return id, nil
}

func listTokens(ctx *gofr.Context) (any, error) { return ListTokens(ctx, ctx.PathParam("identifier")) }

// ListTokens lists an integration's ingestion tokens, without secrets.
func ListTokens(ctx *gofr.Context, integration string) ([]Token, error) {
	if _, err := auth.Require(ctx, auth.RoleBuilder); err != nil {
		return nil, err
	}
	id, err := integrationID(ctx, integration)
	if err != nil {
		return nil, err
	}

	return NewStore(ctx.SQL).Tokens(ctx, id)
}

type createTokenRequest struct {
	Name string `json:"name"`
}

// CreatedToken is a new ingestion token with its secret, which is shown
// once, now.
type CreatedToken struct {
	Token
	Secret string `json:"secret"`
}

func createToken(ctx *gofr.Context) (any, error) {
	var req createTokenRequest
	if err := ctx.Bind(&req); err != nil {
		return nil, badRequest("the request body is not valid JSON")
	}

	return httpx.Result(CreateToken(ctx, ctx.PathParam("identifier"), req.Name))
}

// CreateToken creates an ingestion token for an integration that takes pushes.
func CreateToken(ctx *gofr.Context, integration, name string) (CreatedToken, error) {
	p, err := auth.Require(ctx, auth.RoleBuilder)
	if err != nil {
		return CreatedToken{}, err
	}
	id, err := integrationID(ctx, integration)
	if err != nil {
		return CreatedToken{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > maxTokenName {
		return CreatedToken{}, badRequest("give the token a name of up to 100 characters")
	}

	if !receives(ctx, integration) {
		return CreatedToken{}, httpx.Error{Code: http.StatusConflict, Message: "the " + integration + " integration takes no pushes, so it has no ingestion tokens; give one of its resources a push side"}
	}

	secret := tokenPrefix + auth.NewSecret()
	token, err := NewStore(ctx.SQL).CreateToken(ctx, id, name, secret[:tokenDisplayLength], auth.HashSecret(secret), p.ID)
	if err != nil {
		return CreatedToken{}, err
	}
	auth.Record(ctx, p, audit.IngestionTokenCreate, audit.Target{Type: Name, ID: integration, Label: integration},
		map[string]any{"token": name})

	return CreatedToken{Token: token, Secret: secret}, nil
}

func revokeToken(ctx *gofr.Context) (any, error) {
	return nil, RevokeToken(ctx, ctx.PathParam("identifier"), ctx.PathParam("id"))
}

// RevokeToken revokes an ingestion token, so pushes with it stop being accepted.
func RevokeToken(ctx *gofr.Context, integration, tokenID string) error {
	p, err := auth.Require(ctx, auth.RoleBuilder)
	if err != nil {
		return err
	}
	id, err := integrationID(ctx, integration)
	if err != nil {
		return err
	}
	if _, err := uuid.Parse(tokenID); err != nil {
		return httpx.Error{Code: http.StatusNotFound, Message: "no such token"}
	}

	name, err := NewStore(ctx.SQL).RevokeToken(ctx, id, tokenID)
	if errors.Is(err, ErrTokenNotFound) {
		return httpx.Error{Code: http.StatusNotFound, Message: "no such token"}
	}
	if err != nil {
		return err
	}
	auth.Record(ctx, p, audit.IngestionTokenRevoke, audit.Target{Type: Name, ID: integration, Label: integration},
		map[string]any{"token": name})

	return nil
}

// RunsRequest asks for a page of an integration's runs, searched by Search
// and narrowed to Triggers and Outcomes. A zero Page or Size means the
// first page or the smallest size.
type RunsRequest struct {
	Search             string
	Triggers, Outcomes []string
	Page, Size         int
}

func listRuns(ctx *gofr.Context) (any, error) {
	// trigger and outcome are comma-separated lists.
	req := RunsRequest{Search: ctx.Param("q"), Triggers: listParam(ctx, "trigger"), Outcomes: listParam(ctx, "outcome")}
	var err error
	if req.Page, err = intParam(ctx, "page"); err != nil {
		return nil, badRequest("page must be a whole number from 1")
	}
	if req.Size, err = intParam(ctx, "size"); err != nil {
		return nil, badRequest("size must be 10, 20, 50 or 100")
	}

	return httpx.Result(Runs(ctx, ctx.PathParam("identifier"), req))
}

// Runs returns a page of an integration's runs, newest first.
func Runs(ctx *gofr.Context, integration string, req RunsRequest) (RunPage, error) {
	if _, err := auth.Require(ctx, auth.RoleBuilder); err != nil {
		return RunPage{}, err
	}
	id, err := integrationID(ctx, integration)
	if err != nil {
		return RunPage{}, err
	}

	q := RunQuery{Search: req.Search, Page: 1, PageSize: runPageSizes[0]}
	if req.Page != 0 {
		if req.Page < 1 {
			return RunPage{}, badRequest("page must be a whole number from 1")
		}
		q.Page = req.Page
	}
	if req.Size != 0 {
		if !slices.Contains(runPageSizes, req.Size) {
			return RunPage{}, badRequest("size must be 10, 20, 50 or 100")
		}
		q.PageSize = req.Size
	}
	if q.Triggers, err = oneOf("trigger", req.Triggers, TriggerSchedule, TriggerManual, TriggerPush, TriggerAction); err != nil {
		return RunPage{}, err
	}
	if q.Outcomes, err = oneOf("outcome", req.Outcomes, OutcomeSucceeded, OutcomeWarnings, OutcomeFailed); err != nil {
		return RunPage{}, err
	}

	return NewStore(ctx.SQL).Runs(ctx, id, q)
}

// listParam reads a comma-separated list.
func listParam(ctx *gofr.Context, name string) []string {
	if value := ctx.Param(name); value != "" {
		return strings.Split(value, ",")
	}

	return nil
}

// oneOf checks that every value is allowed.
func oneOf(name string, values []string, allowed ...string) ([]string, error) {
	for _, v := range values {
		if !slices.Contains(allowed, v) {
			return nil, badRequest("unknown " + name + " " + strconv.Quote(v))
		}
	}

	return values, nil
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

func notFound() error {
	return httpx.Error{Code: http.StatusNotFound, Message: "no such integration"}
}

func badRequest(message string) error {
	return httpx.Error{Code: http.StatusBadRequest, Message: message}
}

// CheckResult is how a connector's check went.
type CheckResult struct {
	// Version is what was checked: the published version, or the draft of
	// an integration that hasn't been published.
	Version string `json:"version"`
	Passed  bool   `json:"passed"`
	Error   string `json:"error,omitempty"`
}

func checkNow(ctx *gofr.Context) (any, error) { return CheckNow(ctx, ctx.PathParam("identifier")) }

// CheckNow checks the integration's connector can reach its system with the
// current config and secrets.
func CheckNow(ctx *gofr.Context, identifier string) (CheckResult, error) {
	if _, err := auth.Require(ctx, auth.RoleBuilder); err != nil {
		return CheckResult{}, err
	}
	if _, err := integrationID(ctx, identifier); err != nil {
		return CheckResult{}, err
	}
	record, err := configdoc.NewStore(ctx.SQL, Name).Get(ctx, identifier)
	if err != nil {
		return CheckResult{}, err
	}
	result := CheckResult{Version: "published"}
	source := record.PublishedSource
	if !record.Published() {
		result.Version, source = "draft", record.DraftSource
	}
	spec, issues := Parse(source)
	if configdoc.HasErrors(issues) {
		return CheckResult{}, httpx.Error{Code: http.StatusConflict, Message: "fix the " + result.Version + "'s errors before checking it"}
	}
	if !spec.Checks {
		return CheckResult{}, httpx.Error{Code: http.StatusConflict, Message: "the " + spec.Connector.Type + " connector can't check its system"}
	}
	if err := spec.check(ctx, ctx.SQL); err != nil {
		result.Error = err.Error()
	} else {
		result.Passed = true
	}

	return result, nil
}

// receives reports whether the integration's latest version, draft or
// published, takes pushes.
func receives(ctx *gofr.Context, identifier string) bool {
	record, err := configdoc.NewStore(ctx.SQL, Name).Get(ctx, identifier)
	if err != nil {
		return false
	}
	source := record.PublishedSource
	if record.HasDraft() {
		source = record.DraftSource
	}
	spec, _ := Parse(source)

	return spec.Receives
}
