package integration

import (
	"cmp"
	"encoding/json"
	"slices"

	"gofr.dev/pkg/gofr"

	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
)

// Status is how a published integration is doing: its latest run, and the
// blueprints it feeds with how many of their entities it wrote.
type Status struct {
	Integration string `json:"integration"`
	LastRun     *Run   `json:"lastRun"`
	Feeds       []Feed `json:"feeds"`
}

// Feed is a blueprint an integration maps to or has written entities of.
type Feed struct {
	Blueprint string `json:"blueprint"`
	Title     string `json:"title"`
	Entities  int    `json:"entities"`
}

func listStatus(ctx *gofr.Context) (any, error) {
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
	titles := map[string]string{}
	for id, spec := range published {
		titles[id] = spec.Title
	}

	counts := map[string]map[string]int{}
	rows, err := ctx.SQL.QueryContext(ctx, `
		SELECT i.identifier, b.identifier, count(*)
		FROM entities e
		JOIN config_documents b ON b.id = e.blueprint_id
		JOIN config_documents i ON i.id = e.integration_id
		GROUP BY i.identifier, b.identifier`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			integration, bp string
			count           int
		)
		if err := rows.Scan(&integration, &bp, &count); err != nil {
			return nil, err
		}
		if counts[integration] == nil {
			counts[integration] = map[string]int{}
		}
		counts[integration][bp] = count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	lastRuns, err := latestRuns(ctx)
	if err != nil {
		return nil, err
	}

	return statuses(sources, titles, lastRuns, counts), nil
}

// latestRuns finds each integration's newest run.
func latestRuns(ctx *gofr.Context) (map[string]Run, error) {
	rows, err := ctx.SQL.QueryContext(ctx, `
		SELECT DISTINCT ON (r.integration_id) i.identifier, r.id, r.started_at, r.finished_at, r.trigger,
			coalesce(t.name, ''), r.received, r.created, r.updated, r.unchanged, r.deleted, r.failed, r.errors, r.error
		FROM ingestion_runs r
		JOIN config_documents i ON i.id = r.integration_id
		LEFT JOIN ingestion_tokens t ON t.id = r.token_id
		ORDER BY r.integration_id, r.id DESC`)
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
		if err := rows.Scan(&id, &r.ID, &r.StartedAt, &r.FinishedAt, &r.Trigger, &r.Token, &r.Received, &r.Created,
			&r.Updated, &r.Unchanged, &r.Deleted, &r.Failed, &errors, &r.Error); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(errors, &r.Errors); err != nil {
			return nil, err
		}
		runs[id] = r
	}

	return runs, rows.Err()
}

// statuses puts together each published integration's status. It feeds the
// blueprints its mappings name, and any it wrote entities of before they
// stopped naming them.
func statuses(sources, titles map[string]string, lastRuns map[string]Run, counts map[string]map[string]int) []Status {
	list := []Status{}
	for id, source := range sources {
		status := Status{Integration: id, Feeds: []Feed{}}
		if run, ok := lastRuns[id]; ok {
			status.LastRun = &run
		}
		feeds := map[string]int{}
		spec, _ := Parse(source)
		for _, m := range spec.Mappings {
			feeds[m.Blueprint] = 0
		}
		for bp, count := range counts[id] {
			feeds[bp] = count
		}
		for bp, count := range feeds {
			status.Feeds = append(status.Feeds, Feed{Blueprint: bp, Title: cmp.Or(titles[bp], bp), Entities: count})
		}
		slices.SortFunc(status.Feeds, func(a, b Feed) int { return cmp.Compare(a.Title, b.Title) })
		list = append(list, status)
	}
	slices.SortFunc(list, func(a, b Status) int { return cmp.Compare(a.Integration, b.Integration) })

	return list
}
