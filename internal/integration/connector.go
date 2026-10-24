package integration

// Connector is a source of entities an integration can use.
type Connector struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	// Mode is "push" when other systems send entities, or "pull" when the portal fetches them.
	Mode string `json:"mode"`
}

// Connectors are the built-in connectors.
var Connectors = []Connector{
	{
		ID:          "push",
		Title:       "Push",
		Description: "Other systems send entities to the ingest API with an ingestion token.",
		Mode:        "push",
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
