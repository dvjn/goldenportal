package connection

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"gofr.dev/pkg/gofr"

	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/entity"
)

// Writer is a connection that writes a blueprint's entities: one whose
// sources read it, or one that wrote some before they changed.
type Writer struct {
	Connection string     `json:"connection"`
	Title      string     `json:"title"`
	Entities   int        `json:"entities"`
	Mapped     bool       `json:"mapped"`
	LastRunAt  *time.Time `json:"lastRunAt"`
}

// UsedSource is one of a blueprint's published sources, with how the
// connection it reads last ran. The run is the connection's, so it may have
// read other resources.
type UsedSource struct {
	Connection     string     `json:"connection"`
	Title          string     `json:"title"`
	Resource       string     `json:"resource"`
	Mode           string     `json:"mode"`
	LastRunAt      *time.Time `json:"lastRunAt"`
	LastRunOutcome string     `json:"lastRunOutcome,omitempty"`
}

// Usage is what a blueprint's page shows about how it's used.
type Usage struct {
	Entities   int                   `json:"entities"`
	Sources    []UsedSource          `json:"sources"`
	Writers    []Writer              `json:"writers"`
	References []blueprint.Reference `json:"references"`
}

// blueprintUsage lives here because only this package knows both
// blueprints and connections.
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
	titles := map[string]string{}
	for id, source := range sources {
		spec, _ := Parse(source)
		titles[id] = spec.Title
		spec.AddSources(published)
		if slices.ContainsFunc(spec.sources, func(m Mapping) bool { return m.Blueprint == identifier }) {
			writers[id] = &Writer{Connection: id, Title: spec.Title, Mapped: true}
		}
	}

	spec, ok := published[identifier]
	if !ok {
		spec = blueprint.Spec{Identifier: identifier}
	}
	rows, err := ctx.SQL.QueryContext(ctx, `
		SELECT (SELECT identifier FROM config_documents WHERE id = g.connection_id), g.count
		FROM (SELECT connection_id, count(*) FROM entities e WHERE e.blueprint_id = `+entity.BlueprintID(spec)+` GROUP BY connection_id) g`)
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
			w = &Writer{Connection: id, Title: id}
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
		JOIN config_documents i ON i.id = r.connection_id
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

	_, lastRuns, err := portalCounts(ctx, ctx.SQL)
	if err != nil {
		return Usage{}, err
	}
	usage.Sources = usedSources(published[identifier], titles, lastRuns)

	for _, w := range writers {
		usage.Writers = append(usage.Writers, *w)
	}
	slices.SortFunc(usage.Writers, func(a, b Writer) int { return strings.Compare(a.Connection, b.Connection) })

	return usage, nil
}

// usedSources lists a blueprint's sources in the order it declares them.
func usedSources(spec blueprint.Spec, titles map[string]string, lastRuns map[string]lastRun) []UsedSource {
	used := []UsedSource{}
	for _, src := range spec.Sources {
		u := UsedSource{Connection: src.Connection, Title: cmp.Or(titles[src.Connection], src.Connection), Resource: src.Resource, Mode: src.Mode}
		if last, ok := lastRuns[src.Connection]; ok {
			u.LastRunAt = &last.at
			u.LastRunOutcome = last.outcome
		}
		used = append(used, u)
	}

	return used
}
