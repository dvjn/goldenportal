// Package static serves the records written into an integration's
// definition, for reference data that has no source to read.
package static

import (
	"context"

	"github.com/dvjn/goldport/internal/connector"
	"github.com/dvjn/goldport/internal/definition"
)

type Connector struct{}

func New() Connector { return Connector{} }

// Kinds is empty: the static connector reads the kinds its config gives
// records for.
func (Connector) Kinds() []string { return nil }

func (Connector) Fetch(_ context.Context, request connector.Request) (connector.Records, error) {
	var config definition.StaticConfig
	if err := definition.DecodeConfig(request.Config, &config); err != nil {
		return nil, err
	}
	records := connector.Records{}
	for kind, items := range config.Records.All() {
		if !request.Wants(kind) {
			continue
		}
		records[kind] = make([]any, 0, len(items))
		for _, item := range items {
			records[kind] = append(records[kind], map[string]any{
				"identifier": item.Identifier,
				"title":      item.Title,
				"properties": item.Properties,
				"relations":  item.Relations,
			})
		}
	}

	return records, nil
}
