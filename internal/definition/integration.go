package definition

import (
	"bytes"
	"encoding/json"
	"regexp"
	"time"

	"github.com/invopop/jsonschema"
	"github.com/itchyny/gojq"
)

type Connector string

const (
	ConnectorPush      Connector = "push"
	ConnectorGitLab    Connector = "gitlab"
	ConnectorSonarQube Connector = "sonarqube"
)

// Manual is the built-in integration that owns the entities people create
// in the portal. No document defines it.
const Manual = "manual"

const minInterval = time.Minute

var (
	envPattern    = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
	secretPattern = regexp.MustCompile(`(?i)(token|password|passwd|secret|api_?key|private_?key|credential)`)
)

type Integration struct {
	Header
	Connector Connector       `json:"connector" jsonschema:"enum=push,enum=gitlab,enum=sonarqube"`
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
// a blueprint. Every value is a jq expression over the resource.
type Mapping struct {
	Kind       string      `json:"kind" jsonschema:"minLength=1" jsonschema_description:"The kind of resource the connector reads, such as project."`
	Blueprint  string      `json:"blueprint"`
	Identifier string      `json:"identifier" jsonschema_description:"A jq expression for the entity identifier."`
	Title      string      `json:"title,omitempty" jsonschema_description:"A jq expression for the entity title."`
	Properties Map[string] `json:"properties,omitzero" jsonschema_description:"A jq expression per property of the blueprint."`
	Relations  Map[string] `json:"relations,omitzero" jsonschema_description:"A jq expression per relation of the blueprint, giving target identifiers."`
}

func (Integration) JSONSchemaExtend(schema *jsonschema.Schema) {
	extendHeader(schema, KindIntegration)
	schema.Properties.Set("config", &jsonschema.Schema{Type: "object"})
}

func (Secret) JSONSchemaExtend(schema *jsonschema.Schema) {
	secretRef, _ := schema.Properties.Get("secretRef")
	secretRef.Pattern = envPattern.String()
}

func (i *Integration) Pull() bool { return i.Connector != ConnectorPush }

func (i *Integration) check(c *checker) {
	if i.Identifier == Manual {
		c.report(i, path("identifier"), "manual is the built-in integration for entities people create, choose another identifier")
	}
	i.checkInterval(c)
	i.checkConfig(c)
	for index, mapping := range i.Mappings {
		mapping.check(c, i, path("mappings", index))
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
// secret ends up in the definitions and their history.
func (i *Integration) checkConfig(c *checker) {
	if len(i.Config) == 0 {
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
	checkJQ(c, integration, append(at, "identifier"), m.Identifier)
	if m.Title != "" {
		checkJQ(c, integration, append(at, "title"), m.Title)
	}
	for key, expression := range m.Properties.All() {
		if _, has := blueprint.Properties.Get(key); known && !has {
			c.report(integration, append(at, "properties", key), "%q is not a property of blueprint %s", key, m.Blueprint)
		}
		checkJQ(c, integration, append(at, "properties", key), expression)
	}
	for key, expression := range m.Relations.All() {
		if _, has := blueprint.Relations.Get(key); known && !has {
			c.report(integration, append(at, "relations", key), "%q is not a relation of blueprint %s", key, m.Blueprint)
		}
		checkJQ(c, integration, append(at, "relations", key), expression)
	}
}

func checkJQ(c *checker, document Document, at []string, expression string) {
	if _, err := gojq.Parse(expression); err != nil {
		c.report(document, at, "not a valid jq expression: %v", err)
	}
}
