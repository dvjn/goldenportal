package integration

import (
	"slices"
	"strings"
	"time"

	"gofr.dev/pkg/gofr"

	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
)

// Writer is an integration that writes a blueprint's entities: one whose
// mappings name it, or one that wrote some before its mappings changed.
type Writer struct {
	Integration string     `json:"integration"`
	Title       string     `json:"title"`
	Entities    int        `json:"entities"`
	Mapped      bool       `json:"mapped"`
	LastRunAt   *time.Time `json:"lastRunAt"`
}

// Usage is what a blueprint's page shows about how it's used.
type Usage struct {
	Entities   int                   `json:"entities"`
	Writers    []Writer              `json:"writers"`
	References []blueprint.Reference `json:"references"`
}

// blueprintUsage lives here because only this package knows both
// blueprints and integrations.
func blueprintUsage(ctx *gofr.Context) (any, error) {
	return BlueprintUsage(ctx, ctx.PathParam("identifier"))
}

// BlueprintUsage counts a blueprint's entities and lists who writes them
// and which blueprints relate to it.
func BlueprintUsage(ctx *gofr.Context, identifier string) (Usage, error) {
	if _, err := auth.Require(ctx, auth.RoleMember); err != nil {
		return Usage{}, err
	}

	published, err := blueprint.Published(ctx, ctx.SQL)
	if err != nil {
		return Usage{}, err
	}
	usage := Usage{Writers: []Writer{}, References: blueprint.References(published, identifier)}

	writers := map[string]*Writer{}
	sources, err := configdoc.NewStore(ctx.SQL, Name).PublishedSources(ctx)
	if err != nil {
		return Usage{}, err
	}
	for id, source := range sources {
		spec, _ := Parse(source)
		if slices.ContainsFunc(spec.Mappings, func(m Mapping) bool { return m.Blueprint == identifier }) {
			writers[id] = &Writer{Integration: id, Title: spec.Title, Mapped: true}
		}
	}

	rows, err := ctx.SQL.QueryContext(ctx, `
		SELECT i.identifier, count(*)
		FROM entities e
		JOIN config_documents b ON b.id = e.blueprint_id
		JOIN config_documents i ON i.id = e.integration_id
		WHERE b.kind = $1 AND b.identifier = $2
		GROUP BY i.id, i.identifier`, blueprint.Name, identifier)
	if err != nil {
		return Usage{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id    string
			count int
		)
		if err := rows.Scan(&id, &count); err != nil {
			return Usage{}, err
		}
		w, ok := writers[id]
		if !ok {
			w = &Writer{Integration: id, Title: id}
			writers[id] = w
		}
		w.Entities = count
		usage.Entities += count
	}
	if err := rows.Err(); err != nil {
		return Usage{}, err
	}

	runs, err := ctx.SQL.QueryContext(ctx, `
		SELECT i.identifier, max(r.finished_at) FROM ingestion_runs r
		JOIN config_documents i ON i.id = r.integration_id
		WHERE r.finished_at IS NOT NULL
		GROUP BY i.identifier`)
	if err != nil {
		return Usage{}, err
	}
	defer runs.Close()
	for runs.Next() {
		var (
			id      string
			lastRun time.Time
		)
		if err := runs.Scan(&id, &lastRun); err != nil {
			return Usage{}, err
		}
		if w, ok := writers[id]; ok {
			w.LastRunAt = &lastRun
		}
	}
	if err := runs.Err(); err != nil {
		return Usage{}, err
	}

	for _, w := range writers {
		usage.Writers = append(usage.Writers, *w)
	}
	slices.SortFunc(usage.Writers, func(a, b Writer) int { return strings.Compare(a.Integration, b.Integration) })

	return usage, nil
}
