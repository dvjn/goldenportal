package integration

import (
	"context"

	"github.com/dvjn/goldenportal/internal/configdoc"
)

// Connector is a source of entities an integration can use.
type Connector struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	// Mode is "push" when other systems send entities, or "pull" when the portal fetches them.
	Mode string `json:"mode"`
}

// pull fetches the data a pull connector's mappings run over. It's a
// function rather than a field so the connector list doesn't depend on
// the code that parses integrations.
func (c Connector) pull() func(ctx context.Context, db configdoc.DB) (any, error) {
	switch c.ID {
	case "portal":
		return portalSnapshot
	}

	return nil
}

const (
	ModePush = "push"
	ModePull = "pull"
)

// Connectors are the built-in connectors.
var Connectors = []Connector{
	{
		ID:          "push",
		Title:       "Push",
		Description: "Other systems send entities to the ingest API with an ingestion token.",
		Mode:        ModePush,
	},
	{
		ID:          "portal",
		Title:       "Portal",
		Description: "The portal's own users, groups, blueprints and integrations, synced one way every minute. Changes to the entities never flow back.",
		Mode:        ModePull,
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
