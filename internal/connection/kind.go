package connection

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
)

// Name is the configdoc kind connections are stored as.
const Name = "connection"

// Kind serves connections as configuration documents.
var Kind = configdoc.Kind[Spec]{
	Name:     Name,
	Document: DocumentKind,
	Plural:   "connections",
	Parse:    Parse,
	Check: func(ctx context.Context, db configdoc.DB, spec Spec) ([]configdoc.Issue, error) {
		published, err := blueprint.Published(ctx, db)
		if err != nil {
			return nil, err
		}
		return checkSources(spec, published), nil
	},
	Preflight: func(ctx context.Context, db configdoc.DB, spec Spec) []configdoc.Issue {
		var issues []configdoc.Issue
		published, err := blueprint.Published(ctx, db)
		if err != nil {
			return append(issues, configdoc.Issue{
				Severity: configdoc.SeverityWarning, Path: "spec.connector",
				Message: "the connector's check failed: " + err.Error(),
			})
		}
		spec.AddSources(published)
		warnings, err := spec.check(ctx, db)
		if err != nil {
			issues = append(issues, configdoc.Issue{
				Severity: configdoc.SeverityWarning, Path: "spec.connector",
				Message: "the connector's check failed: " + err.Error(),
			})
		}
		for _, warning := range warnings {
			issues = append(issues, configdoc.Issue{Severity: configdoc.SeverityWarning, Path: "spec.connector", Message: "the connector's check warns: " + warning})
		}
		return issues
	},
	Diff:      Diff,
	Affected:  countEntities,
	Deletable: deletable,
}

// checkTimeout bounds a connector's check, which waits on its system.
const checkTimeout = 15 * time.Second

// check runs the connector's check, if it has one, giving what it warned
// of and why it failed.
func (s Spec) check(ctx context.Context, db configdoc.DB) ([]string, error) {
	checker, ok := s.config.(Checker)
	if !ok {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	var warnings []string
	err := checker.Check(ctx, db, CheckRequest{
		Resources: s.mappedResources(), IngestURL: IngestURL(s.Identifier), WebhookSecret: WebhookSecret(s.Identifier),
		Warn: func(warning string) { warnings = append(warnings, warning) },
	})

	return warnings, err
}

// Diff lists what publishing next over previous changes. Entities already
// written stay, so only switching connector is breaking.
func Diff(previous, next Spec) []configdoc.Change {
	changes := []configdoc.Change{}
	if previous.Connector.Type != next.Connector.Type {
		changes = append(changes, configdoc.Change{
			Section: "connection", Change: configdoc.ChangeChanged, Breaking: true,
			Detail: fmt.Sprintf("switches the connector from %s to %s", previous.Connector.Type, next.Connector.Type),
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
			changes = append(changes, configdoc.Change{Section: "connection", Change: configdoc.ChangeChanged, Detail: side.starts})
		case side.before && !side.after:
			changes = append(changes, configdoc.Change{
				Section: "connection", Change: configdoc.ChangeChanged, Breaking: side.breaking, Detail: side.stops,
			})
		}
	}
	if previous.Pulls && next.Pulls && previous.SyncEvery() != next.SyncEvery() {
		changes = append(changes, configdoc.Change{
			Section: "connection", Change: configdoc.ChangeChanged,
			Detail: fmt.Sprintf("syncs every %s instead of every %s", next.SyncEvery(), previous.SyncEvery()),
		})
	}
	if previous.Title != next.Title || previous.Description != next.Description || previous.Icon != next.Icon || previous.Color != next.Color {
		changes = append(changes, configdoc.Change{
			Section: "connection", Change: configdoc.ChangeChanged,
			Detail: "changes the title, description or icon",
		})
	}

	if previous.Connector.Type == next.Connector.Type {
		switch resources, all := connectorReads(previous, next); {
		case all:
			changes = append(changes, configdoc.Change{
				Section: "connection", Change: configdoc.ChangeChanged,
				Detail: fmt.Sprintf("changes what %s reads; the next sync re-reads everything it reads, completely", next.Identifier),
			})
		case len(resources) > 0:
			changes = append(changes, configdoc.Change{
				Section: "connection", Change: configdoc.ChangeChanged,
				Detail: fmt.Sprintf("changes how %s reads %s; the next sync re-reads them", next.Identifier, strings.Join(resources, ", ")),
			})
		case !reflect.DeepEqual(previous.Connector.Config, next.Connector.Config):
			changes = append(changes, configdoc.Change{
				Section: "connection", Change: configdoc.ChangeChanged,
				Detail: "changes the connector config",
			})
		}
	}

	return changes
}

func countEntities(ctx context.Context, db configdoc.DB, identifier string) (int, error) {
	var count int
	err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM entities e
		JOIN config_documents i ON i.id = e.connection_id
		WHERE i.kind = $1 AND i.identifier = $2`, Name, identifier).Scan(&count)

	return count, err
}
