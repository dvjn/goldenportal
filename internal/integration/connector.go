package integration

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
	// Icon and Color are the integrations' too, so the same source looks the
	// same everywhere.
	Icon  string `json:"icon"`
	Color string `json:"color"`
	// Pulls, Receives and Checks are what the connector can do: fetch its
	// resources, take requests the system sends, and check it can reach the
	// system. An integration's config may use less, as an http integration
	// with no URLs doesn't pull.
	Pulls    bool `json:"pulls"`
	Receives bool `json:"receives"`
	Checks   bool `json:"checks"`
	// Resources are the fixed ones a connector reads, which mappings name.
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
}

// Config is an integration's parsed connector config. What it can do beyond
// naming resources it says by also being a Puller, Receiver or Checker.
type Config interface {
	// Resources are what mappings can name.
	Resources() []string
}

// Puller is a config that fetches its resources.
type Puller interface {
	Config
	// Pulls lists the resources it fetches.
	Pulls() []string
	// Pull fetches each resource asked for and hands its items to emit, a
	// page at a time. A resource that fails doesn't stop the others; its
	// error is returned by name.
	Pull(ctx context.Context, db configdoc.DB, p PullRequest, emit func(resource string, items []Item) error) (map[string]error, error)
}

// PullRequest is what one sync asks a Puller for.
type PullRequest struct {
	Resources []string
	// Since is when the last successful sync started, less a minute, for an
	// incremental sync; the zero time for a complete one.
	Since time.Time
	// IngestURL is where the system can send the integration events, on a
	// sync of every resource once the portal knows its public URL, for
	// connectors that set up their own webhooks.
	IngestURL string
	// WebhookSecret is what the system signs the integration's events with,
	// on the same syncs, once the server has a key to derive it from.
	WebhookSecret string
	// Warn reports what went wrong without failing anything read, such as
	// a webhook it couldn't set up, which the run records.
	Warn func(string)
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
	// with the integration's ingestion token; without one the connector
	// verifies it, returning ErrUnverified if it can't. ctx bounds anything
	// it reads from its system to complete the request; a FetchError says
	// that failed.
	Receive(ctx context.Context, req Request, authenticated bool) ([]Item, error)
}

// Request is a request sent to an integration's ingest endpoint.
type Request struct {
	// Resources are the ones to read from it, of those the config receives.
	Resources []string
	Header    http.Header
	Query     url.Values
	Body      []byte
	// WebhookSecret is what the integration's webhooks are signed with,
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
	// Resources are those the mappings read, for systems that grant access
	// to each separately.
	Resources []string
	// IngestURL and WebhookSecret are as a sync gives them.
	IngestURL     string
	WebhookSecret string
	// Warn reports something optional that won't work, though the check
	// passes.
	Warn func(string)
}

// Connectors are the built-in connectors.
var Connectors = []Connector{
	{
		ID:          "portal",
		Icon:        "panels-top-left",
		Color:       "indigo",
		Title:       "Portal",
		Description: "The portal's own users, groups, blueprints, integrations, actions and recent runs, synced one way. Changes to the entities never flow back.",
		Pulls:       true,
		Checks:      true,
		Resources:   []string{"users", "groups", "blueprints", "integrations"},
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
}

func connector(id string) (Connector, bool) {
	for _, c := range Connectors {
		if c.ID == id {
			return c, true
		}
	}

	return Connector{}, false
}
