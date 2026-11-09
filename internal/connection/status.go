package connection

import (
	"cmp"
	"encoding/json"
	"slices"

	"gofr.dev/pkg/gofr"

	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
)

// Status is how a published connection is doing: its latest run, and the
// blueprints it feeds with how many of their entities it wrote.
type Status struct {
	Connection string `json:"connection"`
	LastRun    *Run   `json:"lastRun"`
	Feeds      []Feed `json:"feeds"`
	// Sources are the published blueprint sources that read it.
	Sources []ReadSource `json:"sources"`
}

// ReadSource is a resource a connection reads for a blueprint.
type ReadSource struct {
	Resource  string `json:"resource"`
	Blueprint string `json:"blueprint"`
	Title     string `json:"title"`
	Mode      string `json:"mode"`
}

// Feed is a blueprint a connection maps to or has written entities of.
type Feed struct {
	Blueprint string `json:"blueprint"`
	Title     string `json:"title"`
	Entities  int    `json:"entities"`
}

func listStatus(ctx *gofr.Context) (any, error) { return ListStatus(ctx) }

// ListStatus says how each published connection is doing: what it wrote
// and how its latest runs went.
func ListStatus(ctx *gofr.Context) ([]Status, error) {
	if _, err := auth.Require(ctx, auth.RoleMember); err != nil {
		return nil, err
	}
	sources, err := configdoc.NewStore(ctx.SQL, Name).PublishedSources(ctx)
	if err != nil {
		return nil, err
	}
	published, err := blueprint.Published(ctx, ctx.SQL)
	if err != nil {
		return nil, err
	}

	counts := map[string]map[string]int{}
	rows, err := ctx.SQL.QueryContext(ctx, `
		SELECT i.identifier, b.identifier, count(*)
		FROM entities e
		JOIN config_documents b ON b.id = e.blueprint_id
		JOIN config_documents i ON i.id = e.connection_id
		GROUP BY i.identifier, b.identifier`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			connection, bp string
			count          int
		)
		if err := rows.Scan(&connection, &bp, &count); err != nil {
			return nil, err
		}
		if counts[connection] == nil {
			counts[connection] = map[string]int{}
		}
		counts[connection][bp] = count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	lastRuns, err := latestRuns(ctx)
	if err != nil {
		return nil, err
	}

	return statuses(sources, published, lastRuns, counts), nil
}

// latestRuns finds each connection's newest run, or its running sync, which
// a push finishing meanwhile mustn't hide.
func latestRuns(ctx *gofr.Context) (map[string]Run, error) {
	rows, err := ctx.SQL.QueryContext(ctx, `
		SELECT DISTINCT ON (r.connection_id) i.identifier, r.id, r.started_at, r.finished_at, r.trigger,
			coalesce(t.name, ''), coalesce(r.action_run_id::text, ''), coalesce(a.title, ''),
			r.received, r.created, r.updated, r.unchanged, r.deleted, r.failed, r.errors, r.error, r.warning
		FROM ingestion_runs r
		JOIN config_documents i ON i.id = r.connection_id
		LEFT JOIN ingestion_tokens t ON t.id = r.token_id
		LEFT JOIN runs a ON a.id = r.action_run_id
		ORDER BY r.connection_id, r.finished_at IS NULL DESC, r.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	runs := map[string]Run{}
	for rows.Next() {
		var (
			id     string
			r      Run
			errors []byte
		)
		if err := rows.Scan(&id, &r.ID, &r.StartedAt, &r.FinishedAt, &r.Trigger, &r.Token, &r.ActionRun, &r.Action,
			&r.Received, &r.Created, &r.Updated, &r.Unchanged, &r.Deleted, &r.Failed, &errors, &r.Error, &r.Warning); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(errors, &r.Errors); err != nil {
			return nil, err
		}
		runs[id] = r
	}

	return runs, rows.Err()
}

// statuses puts together each published connection's status. It feeds the
// blueprints whose published sources name it, and any it wrote entities of
// before they stopped naming it.
func statuses(sources map[string]string, published map[string]blueprint.Spec, lastRuns map[string]Run, counts map[string]map[string]int) []Status {
	list := []Status{}
	for id, source := range sources {
		status := Status{Connection: id, Feeds: []Feed{}, Sources: []ReadSource{}}
		if run, ok := lastRuns[id]; ok {
			status.LastRun = &run
		}
		feeds := map[string]int{}
		spec, _ := Parse(source)
		spec.AddSources(published)
		for _, m := range spec.sources {
			feeds[m.Blueprint] = 0
			status.Sources = append(status.Sources, ReadSource{Resource: m.Resource, Blueprint: m.Blueprint, Title: cmp.Or(published[m.Blueprint].Title, m.Blueprint), Mode: m.Mode})
		}
		slices.SortStableFunc(status.Sources, func(a, b ReadSource) int { return cmp.Compare(a.Resource, b.Resource) })
		for bp, count := range counts[id] {
			feeds[bp] = count
		}
		for bp, count := range feeds {
			status.Feeds = append(status.Feeds, Feed{Blueprint: bp, Title: cmp.Or(published[bp].Title, bp), Entities: count})
		}
		slices.SortFunc(status.Feeds, func(a, b Feed) int { return cmp.Compare(a.Title, b.Title) })
		list = append(list, status)
	}
	slices.SortFunc(list, func(a, b Status) int { return cmp.Compare(a.Connection, b.Connection) })

	return list
}
