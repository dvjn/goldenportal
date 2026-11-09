// Package connection defines connections: connectors that write entities
// into the catalog, as blueprints' sources map their data.
package connection

import (
	"maps"
	"slices"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/filter"
)

// DocumentKind is how connection documents name their kind.
const DocumentKind = "Connection"

// Spec is a parsed connection.
type Spec struct {
	Identifier  string        `json:"identifier"`
	Title       string        `json:"title"`
	Description string        `json:"description,omitempty"`
	Icon        string        `json:"icon,omitempty"`
	Color       string        `json:"color,omitempty"`
	Connector   ConnectorSpec `json:"connector"`
	Sync        *SyncSpec     `json:"sync,omitempty"`
	// sources are the published blueprints' sources naming the connection,
	// the only blueprints it can write.
	sources []Mapping
	// Pulls, Receives and Checks are what this connection's connector
	// config does: sync on a schedule, take pushes, check its system.
	Pulls    bool `json:"pulls"`
	Receives bool `json:"receives"`
	Checks   bool `json:"checks"`
	// Events is whether what it receives are its system's events, sent to a
	// webhook its connector sets up, rather than pushes.
	Events bool `json:"events"`
	config Config
}

// ConnectorSpec picks the connector and configures it.
type ConnectorSpec struct {
	Type   string         `json:"type"`
	Config map[string]any `json:"config,omitempty"`
}

// SyncSpec is how often a pull connection syncs: incrementally every
// Interval, and completely every Complete, for connectors that can fetch
// only what changed. Without Complete, every sync is complete.
type SyncSpec struct {
	Interval      string        `json:"interval"`
	Complete      string        `json:"complete,omitempty"`
	Every         time.Duration `json:"-"`
	CompleteEvery time.Duration `json:"-"`
}

// Mapping turns the items of one resource into entities of one blueprint:
// a source plus the blueprint it sits on. Every expression is jq over an
// item.
type Mapping struct {
	Blueprint string `json:"blueprint"`
	blueprint.Source
}

const (
	// MinSyncInterval keeps pull connections from syncing constantly; the
	// scheduler checks once a minute anyway.
	MinSyncInterval     = time.Minute
	DefaultSyncInterval = 15 * time.Minute
)

// The keys each part of a connection takes, exported so the documents'
// JSON Schema can be checked against them.
var (
	SpecFields         = []string{"connector", "sync"}
	ConnectorFields    = []string{"type", "config"}
	SyncFields         = []string{"interval", "complete"}
	HTTPConfigFields   = []string{"push", "resources"}
	HTTPPushFields     = []string{"signature"}
	SignatureFields    = []string{"header", "algorithm", "prefix", "secret"}
	HTTPResourceFields = []string{"url", "next", "items", "deleted", "maxPages", "headers", "push"}
	HTTPReceiveFields  = []string{"items", "deleted"}
	SecretFields       = []string{"env"}
)

func (s Spec) Key() string { return s.Identifier }

func (s Spec) Meta() configdoc.Meta {
	return configdoc.Meta{Title: s.Title, Description: s.Description, Icon: s.Icon, Color: s.Color}
}

func (s Spec) Stats() map[string]int { return map[string]int{} }

// AddSources adds the published blueprints' sources that name the
// connection to what it reads, in blueprint order. Every runtime reader of a
// published connection calls it, since without it a connection reads
// nothing.
func (s *Spec) AddSources(published map[string]blueprint.Spec) {
	s.sources = nil
	for _, id := range slices.Sorted(maps.Keys(published)) {
		for _, src := range published[id].Sources {
			if src.Connection == s.Identifier {
				s.sources = append(s.sources, Mapping{Blueprint: id, Source: src})
			}
		}
	}
}

// SyncEvery is how often a pull connection syncs, incrementally if it
// also has a complete interval.
func (s Spec) SyncEvery() time.Duration {
	if s.Sync != nil && s.Sync.Every > 0 {
		return s.Sync.Every
	}

	return DefaultSyncInterval
}

// Parse reads a connection document and checks everything that doesn't
// depend on blueprints.
func Parse(source string) (Spec, []configdoc.Issue) {
	r := &configdoc.Reader{}
	doc := configdoc.ReadDocument(r, source, DocumentKind)
	spec := Spec{
		Identifier: doc.Identifier, Title: doc.Meta.Title, Description: doc.Meta.Description,
	}
	metadata := configdoc.Field(doc.Root, "metadata")
	for _, key := range []string{"icon", "color"} {
		if n := configdoc.Field(metadata, key); n != nil {
			r.Errorf(n, "metadata."+key, "a connection's %s comes from its connector, so drop it", key)
		}
	}
	if doc.Spec == nil {
		return spec, r.Issues
	}
	// mappings is allowed only to say where it went.
	fields, ok := r.Fields(doc.Spec, "spec", append(SpecFields, "mappings")...)
	if !ok {
		return spec, r.Issues
	}

	parseConnector(r, &spec, fields["connector"], doc.Spec)
	parseSync(r, &spec, fields["sync"])
	if n := fields["mappings"]; n != nil {
		r.Errorf(n, "spec.mappings", "mappings moved to each blueprint's sources, such as sources on %s", movedTo(n))
	}

	return spec, r.Issues
}

func parseConnector(r *configdoc.Reader, spec *Spec, node, parent *yaml.Node) {
	if node == nil {
		r.Errorf(parent, "spec.connector", "add a connector, such as connector: {type: http, config: {...}}")
		return
	}
	fields, ok := r.Fields(node, "spec.connector", ConnectorFields...)
	if !ok {
		return
	}
	spec.Connector.Type = r.Text(fields["type"], "spec.connector.type")
	c, known := connector(spec.Connector.Type)
	spec.Icon, spec.Color = c.Icon, c.Color
	if !known {
		var ids []string
		for _, c := range Connectors {
			ids = append(ids, c.ID)
		}
		r.Errorf(configdoc.OrNode(fields["type"], node), "spec.connector.type",
			"type must be one of %s", strings.Join(ids, ", "))
		return
	}
	n := fields["config"]
	spec.config = c.parse(r, n, "spec.connector.config")
	if spec.config == nil {
		if n == nil {
			r.Errorf(node, "spec.connector.config", "add the %s connector's config", c.ID)
		}
		return
	}
	if n != nil {
		_ = n.Decode(&spec.Connector.Config)
	}
	if p, ok := spec.config.(Puller); ok && len(p.Pulls()) > 0 {
		spec.Pulls = true
	}
	if rc, ok := spec.config.(Receiver); ok && len(rc.Receives()) > 0 {
		spec.Receives = true
		_, spec.Events = rc.(EventReceiver)
	}
	_, spec.Checks = spec.config.(Checker)
}

func parseSync(r *configdoc.Reader, spec *Spec, node *yaml.Node) {
	if node == nil {
		return
	}
	if !spec.Pulls {
		r.Errorf(node, "spec.sync", "only connections that pull sync; give a resource a url to fetch, or drop sync")
		return
	}
	fields, ok := r.Fields(node, "spec.sync", SyncFields...)
	if !ok {
		return
	}
	every, ok := syncInterval(r, fields["interval"], node, "interval")
	if !ok {
		return
	}
	spec.Sync = &SyncSpec{Interval: fields["interval"].Value, Every: every}
	n := fields["complete"]
	if n == nil {
		return
	}
	inc, ok := spec.config.(Incremental)
	if !ok {
		r.Report(configdoc.SeverityWarning, n, "spec.sync.complete",
			"the %s connector reads everything on every sync, so every sync is complete and complete is ignored", spec.Connector.Type)
		return
	}
	complete, ok := syncInterval(r, n, node, "complete")
	switch {
	case !ok:
	case complete < every:
		r.Errorf(n, "spec.sync.complete", "complete must be at least the interval, %s", spec.Sync.Interval)
	default:
		spec.Sync.Complete, spec.Sync.CompleteEvery = n.Value, complete
		if !inc.UsesSince() {
			r.Report(configdoc.SeverityWarning, n, "spec.sync.complete",
				"no resource url uses {{ $since }}, so incremental syncs fetch everything and delete nothing")
		}
	}
}

// syncInterval reads one of spec.sync's durations, at least MinSyncInterval
// (the message spells it as 1m).
func syncInterval(r *configdoc.Reader, node, parent *yaml.Node, name string) (time.Duration, bool) {
	path := "spec.sync." + name
	every, ok := filter.ParseDuration(r.Text(node, path))
	switch {
	case !ok:
		r.Errorf(configdoc.OrNode(node, parent), path, "%s must be a duration, such as 5m or 1h", name)
		return 0, false
	case every < MinSyncInterval:
		r.Errorf(node, path, "%s must be at least 1m", name)
		return 0, false
	}

	return every, true
}

// resourceProblemer is a config that can say why a resource it has can't
// be read with how it's set up.
type resourceProblemer interface {
	resourceProblem(resource string) string
}

// jqText reads a jq expression. A common slip is writing one that YAML reads
// as a list or mapping, such as [.a], so that gets a pointed hint.
func jqText(r *configdoc.Reader, node *yaml.Node, path string) string {
	if node == nil {
		return ""
	}
	if node.Kind != yaml.ScalarNode {
		r.Errorf(node, path, "quote this jq expression, or write it as a block scalar with |-, since YAML reads it as a list or mapping")
		return ""
	}

	return node.Value
}

// movedTo names the blueprint the first of a connection's old mappings
// wrote, for the message saying where mappings went.
func movedTo(mappings *yaml.Node) string {
	if mappings.Kind == yaml.SequenceNode && len(mappings.Content) > 0 {
		if bp := configdoc.Field(mappings.Content[0], "blueprint"); bp != nil && bp.Kind == yaml.ScalarNode && bp.Value != "" {
			return bp.Value
		}
	}

	return "the blueprint each wrote"
}
