package integration

import (
	"context"
	"fmt"
	"reflect"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
)

// Name is the configdoc kind integrations are stored as.
const Name = "integration"

// Kind serves integrations as configuration documents.
var Kind = configdoc.Kind[Spec]{
	Name:   Name,
	Plural: "integrations",
	Parse:  Parse,
	Check: func(ctx context.Context, db configdoc.DB, spec Spec) ([]configdoc.Issue, error) {
		published, err := blueprint.Published(ctx, db)
		if err != nil {
			return nil, err
		}
		return Check(spec, published), nil
	},
	Diff:     Diff,
	Affected: countEntities,
}

// Diff lists what publishing next over previous changes. Entities already
// written stay, so only switching connector is breaking.
func Diff(previous, next Spec) []configdoc.Change {
	changes := []configdoc.Change{}
	if previous.Connector != next.Connector {
		changes = append(changes, configdoc.Change{Section: "integration", Change: configdoc.ChangeChanged, Breaking: true,
			Detail: fmt.Sprintf("switches the connector from %s to %s", previous.Connector, next.Connector)})
	}
	if previous.Title != next.Title || previous.Description != next.Description || previous.Icon != next.Icon {
		changes = append(changes, configdoc.Change{Section: "integration", Change: configdoc.ChangeChanged,
			Detail: "changes the title, description or icon"})
	}

	before := map[string]Mapping{}
	for _, m := range previous.Mappings {
		before[m.Blueprint] = m
	}
	seen := map[string]bool{}
	for _, m := range next.Mappings {
		seen[m.Blueprint] = true
		old, ok := before[m.Blueprint]
		switch {
		case !ok:
			changes = append(changes, configdoc.Change{Section: "mappings", ID: m.Blueprint, Change: configdoc.ChangeAdded,
				Detail: "starts writing " + m.Blueprint + " entities"})
		case !sameMapping(old, m):
			changes = append(changes, configdoc.Change{Section: "mappings", ID: m.Blueprint, Change: configdoc.ChangeChanged,
				Detail: "changes how " + m.Blueprint + " entities are made"})
		}
	}
	for _, m := range previous.Mappings {
		if !seen[m.Blueprint] {
			changes = append(changes, configdoc.Change{Section: "mappings", ID: m.Blueprint, Change: configdoc.ChangeRemoved,
				Detail: "stops writing " + m.Blueprint + " entities; the ones already written stay"})
		}
	}

	return changes
}

func sameMapping(a, b Mapping) bool {
	a.line, b.line = 0, 0
	strip := func(fields []Field) []Field {
		out := make([]Field, len(fields))
		for i, f := range fields {
			out[i] = Field{ID: f.ID, Expression: f.Expression}
		}
		return out
	}
	a.Properties, b.Properties = strip(a.Properties), strip(b.Properties)

	return reflect.DeepEqual(a, b)
}

func countEntities(ctx context.Context, db configdoc.DB, identifier string) (int, error) {
	var count int
	err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM entities e
		JOIN config_documents i ON i.id = e.integration_id
		WHERE i.kind = $1 AND i.identifier = $2`, Name, identifier).Scan(&count)

	return count, err
}
