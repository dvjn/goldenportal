package definition

import (
	"bytes"
	"encoding/json"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/invopop/jsonschema"
	"github.com/itchyny/gojq"
)

type Connector string

const (
	ConnectorPush      Connector = "push"
	ConnectorGitLab    Connector = "gitlab"
	ConnectorSonarQube Connector = "sonarqube"
	ConnectorStatic    Connector = "static"
	// ConnectorManual is the connector of the built-in manual integration
	// only; no document names it.
	ConnectorManual Connector = "manual"
)

// Manual is the built-in integration that owns the entities people create
// in the portal. No document defines it.
const Manual = "manual"

// ManualIntegration describes the built-in manual integration as if a
// document defined it.
func ManualIntegration() *Integration {
	return &Integration{
		Header: Header{
			Kind:        KindIntegration,
			Identifier:  Manual,
			Title:       "Manual",
			Description: "Entities people create and edit in the portal",
		},
		Connector: ConnectorManual,
	}
}

const minInterval = time.Minute

var (
	envPattern    = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
	secretPattern = regexp.MustCompile(`(?i)(token|password|passwd|secret|api_?key|private_?key|credential)`)
)

// IntegrationIcons are the icon names an integration can use. The UI draws
// each one.
var IntegrationIcons = []string{"gitlab", "sonarqube", "database", "upload", "pencil", "webhook"}

var connectorIcons = map[Connector]string{
	ConnectorGitLab:    "gitlab",
	ConnectorSonarQube: "sonarqube",
	ConnectorStatic:    "database",
	ConnectorPush:      "upload",
	ConnectorManual:    "pencil",
}

type Integration struct {
	Header
	Connector Connector       `json:"connector" jsonschema:"enum=push,enum=gitlab,enum=sonarqube,enum=static"`
	Icon      string          `json:"icon,omitempty" jsonschema_description:"The icon the portal shows for the integration. Defaults to the connector's."`
	Interval  string          `json:"interval,omitempty" jsonschema_description:"How often a pull connector syncs, as a duration such as 30m or 6h."`
	Config    json.RawMessage `json:"config,omitempty" jsonschema_description:"Settings for the connector. Reference secrets under secrets instead of writing them here."`
	Secrets   Map[Secret]     `json:"secrets,omitzero" jsonschema_description:"Secrets the connector reads from the server's environment, by name."`
	Mappings  []Mapping       `json:"mappings,omitempty"`
}

// Secret names the environment variable that holds a secret, never the
// secret itself.
type Secret struct {
	SecretRef string `json:"secretRef" jsonschema_description:"The environment variable that holds the secret."`
}

// Mapping turns one kind of resource the connector reads into entities of
// a blueprint. Every value is a jq expression over the resource. A push
// mapping without expressions takes records that are already entities.
type Mapping struct {
	Kind       string      `json:"kind" jsonschema:"minLength=1" jsonschema_description:"The kind of resource the connector reads, such as project."`
	Blueprint  string      `json:"blueprint"`
	Lookups    []string    `json:"lookups,omitempty" jsonschema:"uniqueItems=true" jsonschema_description:"Blueprints whose current entities the expressions read, each as a jq variable named after the blueprint with hyphens as underscores, such as $ci_job. Each entity is an object with identifier, title, properties and relations, a list of target identifiers by relation."`
	Identifier string      `json:"identifier,omitempty" jsonschema_description:"A jq expression for the entity identifier. Leave out every expression in a push or static integration to receive entities as they are."`
	Title      string      `json:"title,omitempty" jsonschema_description:"A jq expression for the entity title."`
	Properties Map[string] `json:"properties,omitzero" jsonschema_description:"A jq expression per property of the blueprint."`
	Relations  Map[string] `json:"relations,omitzero" jsonschema_description:"A jq expression per relation of the blueprint, giving target identifiers."`
}

// LookupVariable is the jq variable a lookup of the blueprint binds, without
// its $.
func LookupVariable(blueprint string) string {
	return strings.ReplaceAll(blueprint, "-", "_")
}

func (Integration) JSONSchemaExtend(schema *jsonschema.Schema) {
	extendHeader(schema, KindIntegration)
	schema.Properties.Set("config", &jsonschema.Schema{Type: "object"})
	icons := make([]any, 0, len(IntegrationIcons))
	for _, icon := range IntegrationIcons {
		icons = append(icons, icon)
	}
	icon, _ := schema.Properties.Get("icon")
	icon.Enum = icons
	for _, connector := range []Connector{ConnectorGitLab, ConnectorSonarQube, ConnectorStatic} {
		schema.AllOf = append(schema.AllOf, &jsonschema.Schema{
			If: &jsonschema.Schema{
				Properties: properties("connector", &jsonschema.Schema{Const: string(connector)}),
				Required:   []string{"connector"},
			},
			Then: &jsonschema.Schema{
				Properties: properties("config", configSchema(connectorConfigs[connector])),
			},
		})
	}
}

func (Mapping) JSONSchemaExtend(schema *jsonschema.Schema) {
	lookups, _ := schema.Properties.Get("lookups")
	lookups.Items = &jsonschema.Schema{Type: "string", Pattern: identifierPattern.String()}
}

func (Secret) JSONSchemaExtend(schema *jsonschema.Schema) {
	secretRef, _ := schema.Properties.Get("secretRef")
	secretRef.Pattern = envPattern.String()
}

// connectorKinds are the kinds of resource each pull connector reads. The
// static connector reads the kinds its config gives records for.
var connectorKinds = map[Connector][]string{
	ConnectorGitLab:    {"group", "project", "pipeline", "ciConfig"},
	ConnectorSonarQube: {"project", "qualityGate"},
	ConnectorStatic:    nil,
}

// Pull reports whether a connector reads the integration's source, as
// opposed to the source pushing to it or people creating its entities.
func (i *Integration) Pull() bool {
	_, pull := connectorKinds[i.Connector]
	return pull
}

// IconName is the icon the portal shows for the integration.
func (i *Integration) IconName() string {
	if i.Icon != "" {
		return i.Icon
	}

	return connectorIcons[i.Connector]
}

// Kinds returns the fixed kinds of resource the connector reads, or nil for a
// push or static integration.
func (c Connector) Kinds() []string { return slices.Clone(connectorKinds[c]) }

// SourceKinds returns the kinds of resource the connector reads, or nil for
// a push integration, which receives any kind its mappings name.
func (i *Integration) SourceKinds() []string {
	if i.Connector != ConnectorStatic {
		return i.Connector.Kinds()
	}
	var config StaticConfig
	if err := DecodeConfig(i.Config, &config); err != nil {
		return []string{}
	}

	return config.Records.Keys()
}

// Mapping returns the mapping a push integration receives the kind through.
func (i *Integration) Mapping(kind string) (Mapping, bool) {
	index := slices.IndexFunc(i.Mappings, func(mapping Mapping) bool { return mapping.Kind == kind })
	if index < 0 {
		return Mapping{}, false
	}

	return i.Mappings[index], true
}

// Canonical reports whether the mapping takes records that are already
// entities.
func (m Mapping) Canonical() bool {
	return m.Identifier == "" && m.Title == "" && m.Properties.IsZero() && m.Relations.IsZero()
}

// takesCanonical reports whether the integration's records can already be
// entities, so its mappings may leave out every expression.
func (i *Integration) takesCanonical() bool {
	return !i.Pull() || i.Connector == ConnectorStatic
}

func (i *Integration) check(c *checker) {
	if i.Identifier == Manual {
		c.report(i, path("identifier"), "manual is the built-in integration for entities people create, choose another identifier")
	}
	i.checkInterval(c)
	i.checkConfig(c)
	i.checkKinds(c)
	for index, mapping := range i.Mappings {
		mapping.check(c, i, path("mappings", index))
	}
}

// checkKinds keeps every kind a pull connector reads, and gives each kind and
// each blueprint of a push integration one mapping, so a snapshot of a kind
// replaces exactly the entities of its blueprint.
func (i *Integration) checkKinds(c *checker) {
	kinds, blueprints := map[string]bool{}, map[string]bool{}
	sourceKinds := i.SourceKinds()
	for index, mapping := range i.Mappings {
		at := path("mappings", index)
		switch {
		case i.Pull() && !slices.Contains(sourceKinds, mapping.Kind):
			c.report(i, append(at, "kind"), "the %s connector reads %s, not %q",
				i.Connector, strings.Join(sourceKinds, ", "), mapping.Kind)
		case i.Pull():
		case kinds[mapping.Kind]:
			c.report(i, append(at, "kind"), "another mapping already receives kind %q, a push integration maps each kind once", mapping.Kind)
		case blueprints[mapping.Blueprint]:
			c.report(i, append(at, "blueprint"), "another mapping already writes blueprint %q, a push integration writes each blueprint from one kind", mapping.Blueprint)
		}
		kinds[mapping.Kind], blueprints[mapping.Blueprint] = true, true
	}
}

func (i *Integration) checkInterval(c *checker) {
	switch {
	case i.Interval == "":
	case !i.Pull():
		c.report(i, path("interval"), "a push integration receives entities and has no interval")
	default:
		interval, err := time.ParseDuration(i.Interval)
		if err != nil || interval < minInterval {
			c.report(i, path("interval"), "give the interval as a duration of at least 1m, such as 30m or 6h")
		}
	}
}

// checkConfig refuses config keys that look like they hold a secret, so no
// secret ends up in the definitions and their history. A static
// integration's records are data, not settings.
func (i *Integration) checkConfig(c *checker) {
	if len(i.Config) == 0 || i.Connector == ConnectorStatic {
		return
	}
	var config Map[json.RawMessage]
	if err := json.Unmarshal(i.Config, &config); err != nil {
		return
	}
	for key, value := range config.All() {
		if secretPattern.MatchString(key) && bytes.HasPrefix(bytes.TrimSpace(value), []byte(`"`)) {
			c.report(i, path("config", key), "%s looks like a secret, reference an environment variable under secrets instead", key)
		}
	}
}

func (m Mapping) check(c *checker, integration *Integration, at []string) {
	blueprint, known := c.set.Blueprint(m.Blueprint)
	if !known {
		c.report(integration, append(at, "blueprint"), "no blueprint has the identifier %q", m.Blueprint)
		blueprint = &Blueprint{}
	}
	for index, lookup := range m.Lookups {
		if _, ok := c.set.Blueprint(lookup); !ok {
			c.report(integration, append(at, "lookups", strconv.Itoa(index)), "no blueprint has the identifier %q", lookup)
		}
	}
	switch {
	case m.Identifier != "":
		checkMappingJQ(c, integration, append(at, "identifier"), m.Identifier, m.Lookups)
	case !integration.takesCanonical():
		c.report(integration, at, "give a jq expression for the identifier, a pull connector reads records that are not entities yet")
	case !m.Canonical():
		c.report(integration, at, "give a jq expression for the identifier, or leave out every expression to receive entities as they are")
	}
	if m.Title != "" {
		checkMappingJQ(c, integration, append(at, "title"), m.Title, m.Lookups)
	}
	for key, expression := range m.Properties.All() {
		if _, has := blueprint.Properties.Get(key); known && !has {
			c.report(integration, append(at, "properties", key), "%q is not a property of blueprint %s", key, m.Blueprint)
		}
		checkMappingJQ(c, integration, append(at, "properties", key), expression, m.Lookups)
	}
	for key, expression := range m.Relations.All() {
		if _, has := blueprint.Relations.Get(key); known && !has {
			c.report(integration, append(at, "relations", key), "%q is not a relation of blueprint %s", key, m.Blueprint)
		}
		checkMappingJQ(c, integration, append(at, "relations", key), expression, m.Lookups)
	}
}

func checkJQ(c *checker, document Document, at []string, expression string) {
	if _, err := gojq.Parse(expression); err != nil {
		c.report(document, at, "not a valid jq expression: %v", err)
	}
}

// checkMappingJQ compiles the expression with the variables its lookups bind,
// so a misspelt variable fails the check instead of every record.
func checkMappingJQ(c *checker, document Document, at []string, expression string, lookups []string) {
	query, err := gojq.Parse(expression)
	if err == nil {
		_, err = gojq.Compile(query, gojq.WithVariables(LookupVariables(lookups)))
	}
	if err != nil {
		c.report(document, at, "not a valid jq expression: %v", err)
	}
}

// LookupVariables are the jq variables, with their $, that the lookups bind.
func LookupVariables(lookups []string) []string {
	variables := make([]string, 0, len(lookups))
	for _, lookup := range lookups {
		variables = append(variables, "$"+LookupVariable(lookup))
	}

	return variables
}
