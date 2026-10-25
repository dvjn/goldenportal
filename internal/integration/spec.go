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
)

// DocumentKind is how integration documents name their kind.
const DocumentKind = "Integration"

// Spec is a parsed integration.
type Spec struct {
	Identifier  string        `json:"identifier"`
	Title       string        `json:"title"`
	Description string        `json:"description,omitempty"`
	Icon        string        `json:"icon,omitempty"`
	Connector   ConnectorSpec `json:"connector"`
	Sync        *SyncSpec     `json:"sync,omitempty"`
	// Mappings turn incoming data into entities. They're also the only
	// blueprints the integration can write.
	Mappings []Mapping `json:"mappings"`
	http     *HTTPConfig
}

// ConnectorSpec picks the connector and configures it.
type ConnectorSpec struct {
	Type   string         `json:"type"`
	Config map[string]any `json:"config,omitempty"`
}

// SyncSpec is how often a pull integration syncs.
type SyncSpec struct {
	Interval string        `json:"interval"`
	Every    time.Duration `json:"-"`
}

// Mapping turns items of incoming data into entities of one blueprint.
// Every expression is jq.
type Mapping struct {
	// Resource names what a pull connector fetches for this mapping, such
	// as users. Push integrations have none.
	Resource  string `json:"resource,omitempty"`
	Blueprint string `json:"blueprint"`
	// Items picks the items from the data: the pushed body, or each fetched
	// object. Each result is one item.
	Items string `json:"items"`
	// Filter keeps the items it's true for.
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

func (s Spec) Key() string { return s.Identifier }

func (s Spec) Meta() configdoc.Meta {
	return configdoc.Meta{Title: s.Title, Description: s.Description, Icon: s.Icon}
}

func (s Spec) Stats() map[string]int { return map[string]int{"mappings": len(s.Mappings)} }

// SyncEvery is how often a pull integration syncs.
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
		Identifier: doc.Name, Title: doc.Meta.Title, Description: doc.Meta.Description,
		Icon: doc.Meta.Icon, Mappings: []Mapping{},
	}
	if doc.Spec == nil {
		return spec, r.Issues
	}
	fields, ok := r.Fields(doc.Spec, "spec", "connector", "sync", "mappings")
	if !ok {
		return spec, r.Issues
	}

	c := parseConnector(r, &spec, fields["connector"], doc.Spec)
	parseSync(r, &spec, fields["sync"], c)

	r.List(fields["mappings"], "spec.mappings", func(i int, item *yaml.Node) {
		spec.Mappings = append(spec.Mappings, parseMapping(r, fmt.Sprintf("spec.mappings[%d]", i), item, c))
	})
	if len(spec.Mappings) == 0 {
		r.Errorf(configdoc.OrNode(fields["mappings"], doc.Spec), "spec.mappings",
			"add a mapping for each blueprint the integration writes")
	}

	return spec, r.Issues
}

func parseConnector(r *configdoc.Reader, spec *Spec, node, parent *yaml.Node) Connector {
	if node == nil {
		r.Errorf(parent, "spec.connector", "add a connector, such as connector: {type: push}")
		return Connector{}
	}
	fields, ok := r.Fields(node, "spec.connector", "type", "config")
	if !ok {
		return Connector{}
	}
	spec.Connector.Type = r.Text(fields["type"], "spec.connector.type")
	c, known := connector(spec.Connector.Type)
	if !known {
		var ids []string
		for _, c := range Connectors {
			ids = append(ids, c.ID)
		}
		r.Errorf(configdoc.OrNode(fields["type"], node), "spec.connector.type",
			"type must be one of %s", strings.Join(ids, ", "))
	}
	n := fields["config"]
	switch {
	case c.ID == "http" && n == nil:
		r.Errorf(node, "spec.connector.config", "add the resources to fetch, under config.resources")
	case c.ID == "http":
		spec.http = parseHTTPConfig(r, n, "spec.connector.config")
		_ = n.Decode(&spec.Connector.Config)
		c.Resources = spec.resourceNames(c)
	case n != nil:
		r.Errorf(n, "spec.connector.config", "the %s connector takes no config", spec.Connector.Type)
	}

	return c
}

func parseSync(r *configdoc.Reader, spec *Spec, node *yaml.Node, c Connector) {
	if node == nil {
		return
	}
	if c.Mode == ModePush {
		r.Errorf(node, "spec.sync", "push integrations don't sync; other systems send them data")
		return
	}
	fields, ok := r.Fields(node, "spec.sync", "interval")
	if !ok {
		return
	}
	interval := r.Text(fields["interval"], "spec.sync.interval")
	every, err := time.ParseDuration(interval)
	switch {
	case err != nil:
		r.Errorf(configdoc.OrNode(fields["interval"], node), "spec.sync.interval",
			"interval must be a duration, such as 5m or 1h")
	case every < MinSyncInterval:
		r.Errorf(fields["interval"], "spec.sync.interval", "interval must be at least %s", MinSyncInterval)
	default:
		spec.Sync = &SyncSpec{Interval: interval, Every: every}
	}
}

func parseMapping(r *configdoc.Reader, path string, node *yaml.Node, c Connector) Mapping {
	m := Mapping{Items: ".", line: node.Line, Entity: EntityMapping{Properties: []Field{}}}
	fields, ok := r.Fields(node, path, "resource", "blueprint", "items", "filter", "entity")
	if !ok {
		return m
	}

	m.Resource = r.Text(fields["resource"], path+".resource")
	switch {
	case c.Mode == ModePush && fields["resource"] != nil:
		r.Errorf(fields["resource"], path+".resource", "push integrations map what's sent to them, so they have no resources")
	case c.Mode == ModePull && fields["resource"] == nil:
		r.Errorf(node, path+".resource", "add the resource to map, one of %s", strings.Join(c.Resources, ", "))
	case c.Mode == ModePull && !slices.Contains(c.Resources, m.Resource):
		r.Errorf(fields["resource"], path+".resource", "resource must be one of %s", strings.Join(c.Resources, ", "))
	}

	m.Blueprint = r.Text(fields["blueprint"], path+".blueprint")
	if !configdoc.ValidIdentifier(m.Blueprint) {
		r.Errorf(configdoc.OrNode(fields["blueprint"], node), path+".blueprint", "blueprint must name a blueprint")
	}
	if items := expression(r, fields, node, path, "items", false); items != "" {
		m.Items = items
	}
	m.Filter = expression(r, fields, node, path, "filter", false)

	entity := fields["entity"]
	if entity == nil {
		r.Errorf(node, path+".entity", "add the entity to make, with at least an identifier")
		return m
	}
	entityPath := path + ".entity"
	entityFields, ok := r.Fields(entity, entityPath, "identifier", "title", "properties")
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
