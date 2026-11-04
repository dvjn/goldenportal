package integration

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
)

// Name is the configdoc kind integrations are stored as.
const Name = "integration"

// Kind serves integrations as configuration documents.
var Kind = configdoc.Kind[Spec]{
	Name:     Name,
	Document: DocumentKind,
	Plural:   "integrations",
	Parse:    Parse,
	Check: func(ctx context.Context, db configdoc.DB, spec Spec) ([]configdoc.Issue, error) {
		published, err := blueprint.Published(ctx, db)
		if err != nil {
			return nil, err
		}
		return Check(spec, published), nil
	},
	Preflight: func(ctx context.Context, db configdoc.DB, spec Spec) []configdoc.Issue {
		err := spec.check(ctx, db)
		if err == nil {
			return nil
		}
		return []configdoc.Issue{{
			Severity: configdoc.SeverityWarning, Path: "spec.connector",
			Message: "the connector's check failed: " + err.Error(),
		}}
	},
	Diff:     Diff,
	Affected: countEntities,
}

// checkTimeout bounds a connector's check, which waits on its system.
const checkTimeout = 15 * time.Second

// check runs the connector's check, if it has one.
func (s Spec) check(ctx context.Context, db configdoc.DB) error {
	checker, ok := s.config.(Checker)
	if !ok {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	return checker.Check(ctx, db)
}

// Diff lists what publishing next over previous changes. Entities already
// written stay, so only switching connector is breaking.
func Diff(previous, next Spec) []configdoc.Change {
	changes := []configdoc.Change{}
	if previous.Connector.Type != next.Connector.Type {
		changes = append(changes, configdoc.Change{
			Section: "integration", Change: configdoc.ChangeChanged, Breaking: true,
			Detail: fmt.Sprintf("switches the connector from %s to %s", previous.Connector.Type, next.Connector.Type),
		})
	}
	if !reflect.DeepEqual(previous.Connector.Config, next.Connector.Config) && previous.Connector.Type == next.Connector.Type {
		changes = append(changes, configdoc.Change{
			Section: "integration", Change: configdoc.ChangeChanged,
			Detail: "changes the connector config",
		})
	}
	for _, side := range []struct {
		before, after bool
		starts, stops string
		breaking      bool
	}{
		{previous.Pulls, next.Pulls, "starts syncing on a schedule", "stops syncing; the entities it pulled stay", false},
		// Systems that push get refused, so they need telling.
		{previous.Receives, next.Receives, "starts taking pushes", "stops taking pushes, so systems pushing to it are refused", true},
	} {
		switch {
		case !side.before && side.after:
			changes = append(changes, configdoc.Change{Section: "integration", Change: configdoc.ChangeChanged, Detail: side.starts})
		case side.before && !side.after:
			changes = append(changes, configdoc.Change{
				Section: "integration", Change: configdoc.ChangeChanged, Breaking: side.breaking, Detail: side.stops,
			})
		}
	}
	if previous.Pulls && next.Pulls && previous.SyncEvery() != next.SyncEvery() {
		changes = append(changes, configdoc.Change{
			Section: "integration", Change: configdoc.ChangeChanged,
			Detail: fmt.Sprintf("syncs every %s instead of every %s", next.SyncEvery(), previous.SyncEvery()),
		})
	}
	if previous.Title != next.Title || previous.Description != next.Description || previous.Icon != next.Icon || previous.Color != next.Color {
		changes = append(changes, configdoc.Change{
			Section: "integration", Change: configdoc.ChangeChanged,
			Detail: "changes the title, description or icon",
		})
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
			changes = append(changes, configdoc.Change{
				Section: "mappings", ID: m.Blueprint, Change: configdoc.ChangeAdded,
				Detail: "starts writing " + m.Blueprint + " entities",
			})
		case !sameMapping(old, m):
			changes = append(changes, configdoc.Change{
				Section: "mappings", ID: m.Blueprint, Change: configdoc.ChangeChanged,
				Detail: "changes how " + m.Blueprint + " entities are made",
			})
		}
	}
	for _, m := range previous.Mappings {
		if !seen[m.Blueprint] {
			changes = append(changes, configdoc.Change{
				Section: "mappings", ID: m.Blueprint, Change: configdoc.ChangeRemoved,
				Detail: "stops writing " + m.Blueprint + " entities; the ones already written stay",
			})
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
	a.Entity.Properties, b.Entity.Properties = strip(a.Entity.Properties), strip(b.Entity.Properties)

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
