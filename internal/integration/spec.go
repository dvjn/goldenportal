// Package integration defines integrations: connectors that write entities
// into the catalog, with mappings from their data to blueprints.
package integration

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/filter"
)

// DocumentKind is how integration documents name their kind.
const DocumentKind = "Integration"

// Spec is a parsed integration.
type Spec struct {
	Identifier  string        `json:"identifier"`
	Title       string        `json:"title"`
	Description string        `json:"description,omitempty"`
	Icon        string        `json:"icon,omitempty"`
	Color       string        `json:"color,omitempty"`
	Connector   ConnectorSpec `json:"connector"`
	Sync        *SyncSpec     `json:"sync,omitempty"`
	// Mappings turn items into entities. They're also the only blueprints
	// the integration can write.
	Mappings []Mapping `json:"mappings"`
	// Pulls, Receives and Checks are what this integration's connector
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

// SyncSpec is how often a pull integration syncs: incrementally every
// Interval, and completely every Complete, for connectors that can fetch
// only what changed. Without Complete, every sync is complete.
type SyncSpec struct {
	Interval      string        `json:"interval"`
	Complete      string        `json:"complete,omitempty"`
	Every         time.Duration `json:"-"`
	CompleteEvery time.Duration `json:"-"`
}

// Mapping turns the items of one resource into entities of one blueprint.
// Every expression is jq over an item.
type Mapping struct {
	// Resource names the items it maps, such as users.
	Resource  string `json:"resource"`
	Blueprint string `json:"blueprint"`
	// Filter keeps the items it's true for. Deleted items skip it, since
	// what a system sends for a deletion may be no more than an ID.
	Filter string        `json:"filter,omitempty"`
	Entity EntityMapping `json:"entity"`
	line   int
}

// EntityMapping makes an entity from an item.
type EntityMapping struct {
	Identifier string  `json:"identifier"`
	Title      string  `json:"title,omitempty"`
	Properties []Field `json:"properties"`
}

// Field maps one property, which may be a relation.
type Field struct {
	ID         string `json:"identifier"`
	Expression string `json:"expression"`
	line       int
}

const (
	// MinSyncInterval keeps pull integrations from syncing constantly; the
	// scheduler checks once a minute anyway.
	MinSyncInterval     = time.Minute
	DefaultSyncInterval = 15 * time.Minute
)

// The keys each mapping of an integration takes, exported so the documents'
// JSON Schema can be checked against them.
var (
	SpecFields         = []string{"connector", "sync", "mappings"}
	ConnectorFields    = []string{"type", "config"}
	SyncFields         = []string{"interval", "complete"}
	MappingFields      = []string{"resource", "blueprint", "filter", "entity"}
	EntityFields       = []string{"identifier", "title", "properties"}
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

func (s Spec) Stats() map[string]int { return map[string]int{"mappings": len(s.Mappings)} }

// SyncEvery is how often a pull integration syncs, incrementally if it
// also has a complete interval.
func (s Spec) SyncEvery() time.Duration {
	if s.Sync != nil && s.Sync.Every > 0 {
		return s.Sync.Every
	}

	return DefaultSyncInterval
}

// Parse reads an integration document and checks everything that doesn't
// depend on blueprints.
func Parse(source string) (Spec, []configdoc.Issue) {
	r := &configdoc.Reader{}
	doc := configdoc.ReadDocument(r, source, DocumentKind)
	spec := Spec{
		Identifier: doc.Identifier, Title: doc.Meta.Title, Description: doc.Meta.Description,
		Mappings: []Mapping{},
	}
	metadata := configdoc.Field(doc.Root, "metadata")
	for _, key := range []string{"icon", "color"} {
		if n := configdoc.Field(metadata, key); n != nil {
			r.Errorf(n, "metadata."+key, "an integration's %s comes from its connector, so drop it", key)
		}
	}
	if doc.Spec == nil {
		return spec, r.Issues
	}
	fields, ok := r.Fields(doc.Spec, "spec", SpecFields...)
	if !ok {
		return spec, r.Issues
	}

	parseConnector(r, &spec, fields["connector"], doc.Spec)
	parseSync(r, &spec, fields["sync"])

	r.List(fields["mappings"], "spec.mappings", func(i int, item *yaml.Node) {
		spec.Mappings = append(spec.Mappings, parseMapping(r, fmt.Sprintf("spec.mappings[%d]", i), item, spec.config))
	})
	if len(spec.Mappings) == 0 {
		r.Errorf(configdoc.OrNode(fields["mappings"], doc.Spec), "spec.mappings",
			"add a mapping for each blueprint the integration writes")
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
		r.Errorf(node, "spec.sync", "only integrations that pull sync; give a resource a url to fetch, or drop sync")
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

func parseMapping(r *configdoc.Reader, path string, node *yaml.Node, config Config) Mapping {
	m := Mapping{line: node.Line, Entity: EntityMapping{Properties: []Field{}}}
	fields, ok := r.Fields(node, path, MappingFields...)
	if !ok {
		return m
	}

	m.Resource = r.Text(fields["resource"], path+".resource")
	var resources []string
	if config != nil {
		resources = config.Resources()
	}
	switch {
	case config == nil:
	case fields["resource"] == nil:
		r.Errorf(node, path+".resource", "add the resource to map, one of %s", strings.Join(resources, ", "))
	case !slices.Contains(resources, m.Resource):
		r.Errorf(fields["resource"], path+".resource", "resource must be one of %s", strings.Join(resources, ", "))
	default:
		if rp, ok := config.(resourceProblemer); ok {
			if problem := rp.resourceProblem(m.Resource); problem != "" {
				r.Errorf(fields["resource"], path+".resource", "%s", problem)
			}
		}
	}

	m.Blueprint = r.Text(fields["blueprint"], path+".blueprint")
	if !configdoc.ValidIdentifier(m.Blueprint) {
		r.Errorf(configdoc.OrNode(fields["blueprint"], node), path+".blueprint", "blueprint must name a blueprint")
	}
	m.Filter = expression(r, fields, node, path, "filter", false)

	entity := fields["entity"]
	if entity == nil {
		r.Errorf(node, path+".entity", "add the entity to make, with at least an identifier")
		return m
	}
	entityPath := path + ".entity"
	entityFields, ok := r.Fields(entity, entityPath, EntityFields...)
	if !ok {
		return m
	}
	m.Entity.Identifier = expression(r, entityFields, entity, entityPath, "identifier", true)
	m.Entity.Title = expression(r, entityFields, entity, entityPath, "title", false)
	r.Each(entityFields["properties"], entityPath+".properties", func(id string, key, value *yaml.Node) {
		fieldPath := entityPath + ".properties." + id
		field := Field{ID: id, Expression: jqText(r, value, fieldPath), line: key.Line}
		if err := blueprint.CompileExpression(field.Expression); err != nil {
			r.Errorf(value, fieldPath, "%s is not valid jq: %v", id, err)
		}
		m.Entity.Properties = append(m.Entity.Properties, field)
	})

	return m
}

// resourceProblemer is a config that can say why a resource it has can't
// be read with how it's set up.
type resourceProblemer interface {
	resourceProblem(resource string) string
}

// expression reads a jq field of a mapping.
func expression(r *configdoc.Reader, fields map[string]*yaml.Node, parent *yaml.Node, path, name string, required bool) string {
	n := fields[name]
	value := jqText(r, n, path+"."+name)
	if strings.TrimSpace(value) == "" {
		if required {
			r.Errorf(configdoc.OrNode(n, parent), path+"."+name, "add a jq expression for %s, such as .id", name)
		}
		return ""
	}
	if err := blueprint.CompileExpression(value); err != nil {
		r.Errorf(n, path+"."+name, "%s is not valid jq: %v", name, err)
	}

	return value
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

// Check checks mappings against the published blueprints: unpublished
// blueprints are warnings, properties they can't write are errors.
func Check(spec Spec, published map[string]blueprint.Spec) []configdoc.Issue {
	var issues []configdoc.Issue
	for i, m := range spec.Mappings {
		path := fmt.Sprintf("spec.mappings[%d]", i)
		bp, ok := published[m.Blueprint]
		if !ok {
			if configdoc.ValidIdentifier(m.Blueprint) {
				issues = append(issues, configdoc.Issue{
					Severity: configdoc.SeverityWarning, Path: path + ".blueprint", Line: m.line,
					Message: fmt.Sprintf("no blueprint %q is published yet, so writing to it fails until it is", m.Blueprint),
				})
			}
			continue
		}
		for _, f := range m.Entity.Properties {
			if !bp.HasProperty(f.ID) {
				issues = append(issues, configdoc.Issue{
					Severity: configdoc.SeverityError,
					Path:     path + ".entity.properties." + f.ID, Line: f.line,
					Message: fmt.Sprintf("%s has no stored property %q to write", m.Blueprint, f.ID),
				})
			}
		}
	}

	return issues
}
