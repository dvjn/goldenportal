package integration

import (
	"net/http"

	"github.com/dvjn/goldport/internal/connector"
	"github.com/dvjn/goldport/internal/connector/gitlab"
	"github.com/dvjn/goldport/internal/connector/sonarqube"
	"github.com/dvjn/goldport/internal/connector/static"
	"github.com/dvjn/goldport/internal/definition"
)

// Connectors are the pull connectors an integration can name.
func Connectors(client *http.Client) map[definition.Connector]connector.Connector {
	return map[definition.Connector]connector.Connector{
		definition.ConnectorGitLab:    gitlab.New(client),
		definition.ConnectorSonarQube: sonarqube.New(client),
		definition.ConnectorStatic:    static.New(),
	}
}
