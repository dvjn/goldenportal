package integration

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
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

var (
	// portalURL is the portal's public URL, which systems send events to.
	portalURL string
	// webhookKey is the server's key each integration's webhook secret is
	// derived from.
	webhookKey string
)

// Register serves integrations. The handlers read the request and call the
// services below, which MCP calls too.
func Register(app httpx.Routes, baseURL, key string) {
	portalURL, webhookKey = baseURL, key
	configdoc.Register(app, Kind)
	app.GET("connectors", listConnectors)
	app.GET("integrations/{identifier}/tokens", listTokens)
	app.POST("integrations/{identifier}/tokens", createToken)
	app.DELETE("integrations/{identifier}/tokens/{id}", revokeToken)
	app.GET("integrations/{identifier}/runs", listRuns)
	app.POST("integrations/{identifier}/sync", syncNow)
	app.POST("integrations/{identifier}/check", checkNow)
	app.AddCronJob(SyncSchedule, "sync-pull-integrations", syncAll)
	app.OnStart(func(ctx *gofr.Context) error { return NewStore(ctx.SQL).finishInterrupted(ctx) })
	app.GET("blueprints/{identifier}/usage", blueprintUsage)
	app.GET("integration-status", listStatus)
}

// RegisterIngest serves the push endpoints, outside the web API: callers
// sign in with an ingestion token, not a session.
func RegisterIngest(app *gofr.App) {
	app.POST(auth.IngestPrefix+"{integration}", ingest)
	app.DELETE(auth.IngestPrefix+"{integration}/entities/{blueprint}/{identifier:.+}", deleteEntity)
}

// WebhookKeyEnv names the server's key that each integration's webhook
// secret is derived from, so systems that sign their events can be set up
// without a secret written for each. It's outside SecretEnvPrefix, so no
// document can reference it, and send it anywhere.
const WebhookKeyEnv = "GP_WEBHOOK_KEY"

// WebhookSecret is the secret an integration's webhooks are signed with,
// or "" while the server has no key.
func WebhookSecret(identifier string) string {
	if webhookKey == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(webhookKey))
	mac.Write([]byte("webhook " + identifier))

	return hex.EncodeToString(mac.Sum(nil))
}

// IngestURL is where systems send an integration's pushes, or "" while the
// portal doesn't know its public URL.
func IngestURL(identifier string) string {
	if portalURL == "" {
		return ""
	}

	return strings.TrimRight(portalURL, "/") + auth.IngestPrefix + url.PathEscape(identifier)
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
		return "", httpx.NotFound("no such integration")
	}
	var id string
	err := ctx.SQL.QueryRowContext(ctx, `SELECT id FROM config_documents WHERE kind = $1 AND identifier = $2`,
		Name, identifier).Scan(&id)
	if err != nil {
		return "", httpx.NotFound("no such integration")
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
		return nil, httpx.BadRequest("the request body is not valid JSON")
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
		return CreatedToken{}, httpx.BadRequest("give the token a name of up to 100 characters")
	}

	if !receives(ctx, integration) {
		return CreatedToken{}, httpx.Conflict("the " + integration + " integration takes no pushes, so it has no ingestion tokens; give one of its resources a push side")
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
		return httpx.NotFound("no such token")
	}

	name, err := NewStore(ctx.SQL).RevokeToken(ctx, id, tokenID)
	if errors.Is(err, ErrTokenNotFound) {
		return httpx.NotFound("no such token")
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
	if req.Page, req.Size, err = httpx.Paging(ctx.Param); err != nil {
		return nil, err
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

	q := RunQuery{Search: req.Search}
	if q.Page, q.PageSize, err = httpx.CheckPaging(req.Page, req.Size); err != nil {
		return RunPage{}, err
	}
	if q.Triggers, err = oneOf("trigger", req.Triggers, TriggerSchedule, TriggerManual, TriggerPush, TriggerAction); err != nil {
		return RunPage{}, err
	}
	if q.Outcomes, err = oneOf("outcome", req.Outcomes, OutcomeRunning, OutcomeSucceeded, OutcomeWarnings, OutcomeFailed); err != nil {
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
			return nil, httpx.BadRequest("unknown " + name + " " + strconv.Quote(v))
		}
	}

	return values, nil
}

// CheckResult is how a connector's check went.
type CheckResult struct {
	// Version is what was checked: the published version, or the draft of
	// an integration that hasn't been published.
	Version string `json:"version"`
	Passed  bool   `json:"passed"`
	Error   string `json:"error,omitempty"`
	// Warning is what won't work, though the check passed.
	Warning string `json:"warning,omitempty"`
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
		return CheckResult{}, httpx.Conflict("fix the " + result.Version + "'s errors before checking it")
	}
	if !spec.Checks {
		return CheckResult{}, httpx.Conflict("the " + spec.Connector.Type + " connector can't check its system")
	}
	warnings, err := spec.check(ctx, ctx.SQL)
	result.Warning = strings.Join(warnings, " ")
	if err != nil {
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
