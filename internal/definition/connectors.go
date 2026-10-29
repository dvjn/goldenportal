package definition

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/invopop/jsonschema"
)

// GitLabConfig is the config of the gitlab connector.
type GitLabConfig struct {
	URL         string               `json:"url" jsonschema:"format=uri" jsonschema_description:"The address of the GitLab instance, such as https://gitlab.example.com."`
	Group       string               `json:"group" jsonschema:"minLength=1" jsonschema_description:"The full path of the group to read, such as acme/platform. Subgroups are read too."`
	Projects    GitLabProjectsConfig `json:"projects,omitzero" jsonschema_description:"Which projects of the group to read."`
	PipelineRef string               `json:"pipelineRef,omitempty" jsonschema_description:"The ref whose latest pipeline the pipeline kind reads. Defaults to each project's default branch."`
}

type GitLabProjectsConfig struct {
	ExcludeArchived          bool     `json:"excludeArchived,omitempty" jsonschema_description:"Leave out archived projects."`
	ExcludeMarkedForDeletion bool     `json:"excludeMarkedForDeletion,omitempty" jsonschema_description:"Leave out projects marked for deletion."`
	ExcludeEmpty             bool     `json:"excludeEmpty,omitempty" jsonschema_description:"Leave out projects without a repository."`
	PathPrefixes             []string `json:"pathPrefixes,omitempty" jsonschema_description:"Read only projects whose full path starts with one of these."`
}

// SonarQubeConfig is the config of the sonarqube connector.
type SonarQubeConfig struct {
	URL           string   `json:"url" jsonschema:"format=uri" jsonschema_description:"The address of the SonarQube server, such as https://sonarqube.example.com."`
	Projects      []string `json:"projects,omitempty" jsonschema_description:"The keys of the projects to read. Defaults to every project the token can browse."`
	ProjectFilter string   `json:"projectFilter,omitempty" jsonschema_description:"A SonarQube project search filter, such as qualifier = TRK."`
	Metrics       []string `json:"metrics,omitempty" jsonschema_description:"The metric keys each project record carries under measures."`
}

// DefaultSonarQubeMetrics are the measures a project record carries when the
// config names none.
var DefaultSonarQubeMetrics = []string{
	"alert_status", "ncloc", "coverage", "new_coverage", "duplicated_lines_density",
	"bugs", "vulnerabilities", "code_smells", "security_hotspots", "security_hotspots_reviewed",
	"reliability_rating", "security_rating", "sqale_rating", "security_review_rating", "sqale_index",
}

// StaticConfig is the config of the static connector: the records of each
// kind, written into the definition.
type StaticConfig struct {
	Records Map[[]StaticRecord] `json:"records"`
}

// StaticRecord is an entity as a static integration gives it.
type StaticRecord struct {
	Identifier string         `json:"identifier" jsonschema:"minLength=1"`
	Title      string         `json:"title,omitempty"`
	Properties map[string]any `json:"properties,omitempty"`
	Relations  map[string]any `json:"relations,omitempty" jsonschema_description:"Target identifiers by relation, one or a list."`
}

func (StaticConfig) JSONSchema() *jsonschema.Schema {
	reflector := newReflector()
	reflector.DoNotReference = true
	record := reflector.Reflect(new(StaticRecord))
	record.Version = ""
	record.ID = ""

	return &jsonschema.Schema{
		Type:     "object",
		Required: []string{"records"},
		Properties: properties("records", &jsonschema.Schema{
			Type:                 "object",
			Description:          "The records of each kind, by kind.",
			PropertyNames:        &jsonschema.Schema{Pattern: keyPattern.String()},
			AdditionalProperties: &jsonschema.Schema{Type: "array", Items: record},
		}),
		AdditionalProperties: jsonschema.FalseSchema,
	}
}

// DecodeConfig reads a connector's config into target, refusing unknown keys.
func DecodeConfig(config json.RawMessage, target any) error {
	if len(config) == 0 {
		config = json.RawMessage("{}")
	}
	decoder := json.NewDecoder(bytes.NewReader(config))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("the config is not valid: %w", err)
	}

	return nil
}

// connectorConfigs are the config each connector reads, for the schema. The
// schema leaves the required keys to the connector, which names what is
// missing when a sync starts.
var connectorConfigs = map[Connector]any{
	ConnectorGitLab:    new(GitLabConfig),
	ConnectorSonarQube: new(SonarQubeConfig),
	ConnectorStatic:    new(StaticConfig),
}

func configSchema(config any) *jsonschema.Schema {
	reflector := newReflector()
	reflector.DoNotReference = true
	schema := reflector.Reflect(config)
	schema.Version = ""
	schema.ID = ""
	schema.Required = nil

	return schema
}
