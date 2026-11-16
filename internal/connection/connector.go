package connection

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/dvjn/goldenportal/internal/configdoc"
)

// Connector is built-in code for one kind of system. It turns what the system
// has into items; the core maps, owns and reconciles them.
type Connector struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	// Icon and Color are the connections' too, so the same source looks the
	// same everywhere.
	Icon  string `json:"icon"`
	Color string `json:"color"`
	// Pulls, Receives and Checks are what the connector can do: fetch its
	// resources, take requests the system sends, and check it can reach the
	// system. A connection's config may use less, as an http connection
	// with no URLs doesn't pull.
	Pulls    bool `json:"pulls"`
	Receives bool `json:"receives"`
	Checks   bool `json:"checks"`
	// Resources are the fixed ones a connector reads, which sources name.
	// Connectors whose config defines them have none here.
	Resources []string `json:"resources,omitempty"`
	// parse reads the connector's config, nil when the document has none.
	parse func(r *configdoc.Reader, node *yaml.Node, path string) Config
}

// Item is one object of one resource, or its deletion.
type Item struct {
	Resource string
	Object   any
	Deleted  bool
	// Scope is the part of the system the object was read from, such as a
	// project, which the core doesn't interpret, or empty for none. A
	// complete sync deletes a scope's entities only when it read that scope
	// in full.
	Scope string
	// SourceAt is when the system answered with what the object says, the
	// earliest answer when several fed it, or zero when the connector
	// doesn't know. An entity written from a later answer is never
	// overwritten from an earlier one.
	SourceAt time.Time
}

// Config is a connection's parsed connector config. What it can do beyond
// naming resources it says by also being a Puller, Receiver or Checker.
type Config interface {
	// Resources are what sources can name.
	Resources() []string
}

// Puller is a config that fetches its resources.
type Puller interface {
	Config
	// Pulls lists the resources it fetches.
	Pulls() []string
	// Pull fetches each resource asked for and hands its items to emit, a
	// page at a time. A resource that fails doesn't stop the others; the
	// report says what it read in full.
	Pull(ctx context.Context, db configdoc.DB, p PullRequest, emit func(resource string, items []Item) error) (Report, error)
}

// Report is what a pull read in full, which is all a complete sync goes by
// to delete what it didn't see.
type Report struct {
	// Failed is why each resource that failed as a whole failed.
	Failed map[string]error
	// ScopeFailures are resources that failed in one scope only.
	ScopeFailures []ScopeFailure
	// Scopes are the scopes the pull listed, of a connector whose items
	// carry them. A stored scope it didn't list is gone.
	Scopes []string
	// Unlisted is why some scopes may be missing from Scopes, such as a
	// listing that failed, so none counts as gone.
	Unlisted []error
}

// ScopeFailure is a resource a pull couldn't read in one scope.
type ScopeFailure struct {
	Resource, Scope string
	Err             error
	// Refused is whether the system refused access, which, unlike most
	// failures, tends to last.
	Refused bool
	// lasting is a refusal the syncs before met too, run after run.
	lasting bool
}

// PullRequest is what one sync asks a Puller for.
type PullRequest struct {
	Resources []string
	// Cursors are where to read each resource from, which From answers. A
	// resource without one is read in full.
	Cursors map[string]Cursor
	// IngestURL is where the system can send the connection events, on a
	// sync of every resource once the portal knows its public URL, for
	// connectors that set up their own webhooks.
	IngestURL string
	// WebhookSecret is what the system signs the connection's events with,
	// on the same syncs, once the server has a key to derive it from.
	WebhookSecret string
	// Warn reports what went wrong without failing anything read, such as
	// a webhook it couldn't set up, which the run records. It's never nil.
	Warn func(string)
	// read notes what From answered, so the sync knows where it left off.
	read *readLog
}

// Cursor is where a sync reads a resource from in each scope, such as a
// project, or "" for a resource read from none in particular.
type Cursor struct {
	// Scopes are when the last sync that read each scope without failing
	// it started, less a minute, so nothing that changed as it read is
	// missed.
	Scopes map[string]time.Time
	// Floor is as far back as any sync reads, as the blueprints written
	// keep rows no longer, and where one reads a scope Scopes doesn't name.
	// Zero reads such a scope in full.
	Floor time.Time
}

// From is when to read a resource in a scope from: what changed since, or
// everything for the zero time. first is whether no sync read it there
// before, when since only says how far back the blueprints keep rows, and
// what's still open is read too, however long ago it last changed. A
// connector that reads only what changed asks before each read; one that
// doesn't ask reads everything.
func (p PullRequest) From(resource, scope string) (since time.Time, first bool) {
	cursor := p.Cursors[resource]
	since, ok := cursor.Scopes[scope]
	if !ok {
		since = cursor.Floor
	} else if since.Before(cursor.Floor) {
		since = cursor.Floor
	}
	p.read.note(resource, scope, since)

	return since, !ok
}

// Incremental is a Puller that can fetch only what changed since a time.
// Without it, every sync fetches everything.
type Incremental interface {
	Puller
	UsesSince() bool
}

// Receiver is a config that takes requests its system sends.
type Receiver interface {
	Config
	// Receives lists the resources requests can carry.
	Receives() []string
	// Receive turns a request into items. authenticated is whether it came
	// with the connection's ingestion token; without one the connector
	// verifies it, returning ErrUnverified if it can't. ctx bounds anything
	// it reads from its system to complete the request; a FetchError says
	// that failed.
	Receive(ctx context.Context, req Request, authenticated bool) ([]Item, error)
}

// Request is a request sent to a connection's ingest endpoint.
type Request struct {
	// Resources are the ones to read from it, of those the config receives.
	Resources []string
	Header    http.Header
	Query     url.Values
	Body      []byte
	// WebhookSecret is what the connection's webhooks are signed with,
	// once the server has a key to derive it from.
	WebhookSecret string
}

// EventReceiver is a Receiver whose requests are its system's events, each
// about one object, sent to a webhook the connector sets up: never a whole
// resource, so none is a complete push, and never data pushed with a token.
type EventReceiver interface {
	Receiver
	ReceivesEvents()
}

// Checker is a config that can check its system is reachable.
type Checker interface {
	Config
	// Check checks it can reach the system and read what it's asked to,
	// failing with what's missing, and warns of what optional won't work.
	Check(ctx context.Context, db configdoc.DB, c CheckRequest) error
}

// CheckRequest is what a check is asked to check.
type CheckRequest struct {
	// Resources are those the sources read, for systems that grant access
	// to each separately.
	Resources []string
	// IngestURL and WebhookSecret are as a sync gives them.
	IngestURL     string
	WebhookSecret string
	// Warn reports something optional that won't work, though the check
	// passes. It's never nil.
	Warn func(string)
}

// Connectors are the built-in connectors.
var Connectors = []Connector{
	{
		ID:          "portal",
		Icon:        "panels-top-left",
		Color:       "indigo",
		Title:       "Portal",
		Description: "The portal's own users, groups, blueprints, connections, actions and recent runs, synced one way. Changes to the entities never flow back.",
		Pulls:       true,
		Checks:      true,
		Resources:   []string{"users", "groups", "blueprints", "connections"},
		parse:       parsePortalConfig,
	},
	{
		ID:          "http",
		Icon:        "globe",
		Color:       "cyan",
		Title:       "HTTP",
		Description: "JSON fetched from URLs on a schedule, following pages, and JSON other systems send to the ingest API, with an ingestion token or a signature.",
		Pulls:       true,
		Receives:    true,
		Checks:      true,
		parse:       parseHTTPConfig,
	},
	{
		ID:          "gitlab",
		Icon:        "git-merge",
		Color:       "orange",
		Title:       "GitLab",
		Description: "Groups, projects, merge requests, issues, pipelines, deployments, members and more, read with an access token from GitLab.com or a self-managed instance, and kept fresh by each project's webhook.",
		Pulls:       true,
		Receives:    true,
		Checks:      true,
		Resources:   gitlabResources,
		parse:       parseGitLabConfig,
	},
	{
		ID:          "sonarqube",
		Icon:        "gauge",
		Color:       "sky",
		Title:       "SonarQube",
		Description: "Projects with their quality gate, coverage, issues, hotspots and ratings, and the repository each is bound to, read with a user token from SonarQube Server or Cloud.",
		Pulls:       true,
		Checks:      true,
		Resources:   []string{sonarProjects},
		parse:       parseSonarQubeConfig,
	},
	{
		ID:          "authentik",
		Icon:        "key-round",
		Color:       "orange",
		Title:       "Authentik",
		Description: "Users and groups with their attributes, and each group's members and parents, read with an API token from an authentik instance.",
		Pulls:       true,
		Checks:      true,
		Resources:   []string{authentikUsers, authentikGroups},
		parse:       parseAuthentikConfig,
	},
}

func connector(id string) (Connector, bool) {
	for _, c := range Connectors {
		if c.ID == id {
			return c, true
		}
	}

	return Connector{}, false
}
