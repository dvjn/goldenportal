package integration

import (
	"context"
	"slices"
	"time"

	"github.com/lib/pq"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
)

// The Portal connector's snapshot is the portal's own objects, for mappings
// to project into entities. It leaves out entities, which are already in the
// catalog, and anything about access, such as roles and tokens, since
// whatever is mapped becomes visible to every member.
type portalData struct {
	Users        []portalUser        `json:"users"`
	Groups       []portalGroup       `json:"groups"`
	Blueprints   []portalBlueprint   `json:"blueprints"`
	Integrations []portalIntegration `json:"integrations"`
}

type portalUser struct {
	ID           string     `json:"id"`
	Email        string     `json:"email"`
	DisplayName  string     `json:"displayName"`
	Username     string     `json:"username"`
	Active       bool       `json:"active"`
	Groups       []string   `json:"groups"`
	CreatedAt    time.Time  `json:"createdAt"`
	LastSignedIn *time.Time `json:"lastSignedIn"`
}

type portalGroup struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Members   []string  `json:"members"`
	CreatedAt time.Time `json:"createdAt"`
}

type portalBlueprint struct {
	Identifier  string    `json:"identifier"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Icon        string    `json:"icon"`
	Version     int       `json:"version"`
	Properties  int       `json:"properties"`
	Relations   []string  `json:"relations"`
	Entities    int       `json:"entities"`
	PublishedAt time.Time `json:"publishedAt"`
}

type portalIntegration struct {
	Identifier  string     `json:"identifier"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Connector   string     `json:"connector"`
	Version     int        `json:"version"`
	Blueprints  []string   `json:"blueprints"`
	Entities    int        `json:"entities"`
	LastRunAt   *time.Time `json:"lastRunAt"`
	// LastRunOutcome is how the last run went, as the runs table filters
	// them: succeeded, warnings or failed. It's null before the first run.
	LastRunOutcome *string   `json:"lastRunOutcome"`
	PublishedAt    time.Time `json:"publishedAt"`
}

// portalReaders read the portal's objects that other packages add to the
// Portal connector, such as actions and their runs, by resource.
var portalReaders = map[string]func(context.Context, configdoc.DB) (any, error){}

// AddPortalResource lets the Portal connector read another of the portal's
// own objects. Call it at start-up, before any sync.
func AddPortalResource(name string, read func(context.Context, configdoc.DB) (any, error)) {
	portalReaders[name] = read
	for i := range Connectors {
		if Connectors[i].ID == "portal" && !slices.Contains(Connectors[i].Resources, name) {
			Connectors[i].Resources = append(Connectors[i].Resources, name)
		}
	}
}

// lastRun is when an integration last ran, and how it went.
type lastRun struct {
	at      time.Time
	outcome string
}

// portalSnapshot reads the resources asked for: users, groups, published
// blueprints and integrations, and those other packages added. Users are named by email and groups by name,
// so mappings can relate them to each other.
func portalSnapshot(ctx context.Context, db configdoc.DB, _ Spec, resources []string) (map[string]any, error) {
	data := portalData{
		Users: []portalUser{}, Groups: []portalGroup{}, Blueprints: []portalBlueprint{},
		Integrations: []portalIntegration{},
	}
	wants := func(resource string) bool { return slices.Contains(resources, resource) }

	if wants("users") {
		if err := portalUsers(ctx, db, &data); err != nil {
			return nil, err
		}
	}
	if wants("groups") {
		if err := portalGroups(ctx, db, &data); err != nil {
			return nil, err
		}
	}
	if wants("blueprints") || wants("integrations") {
		if err := portalDocuments(ctx, db, &data); err != nil {
			return nil, err
		}
	}

	value, err := toJSONValue(data)
	if err != nil {
		return nil, err
	}
	all, _ := value.(map[string]any)
	out := map[string]any{}
	for _, resource := range resources {
		read, ok := portalReaders[resource]
		if !ok {
			out[resource] = all[resource]
			continue
		}
		objects, err := read(ctx, db)
		if err != nil {
			return nil, err
		}
		if out[resource], err = toJSONValue(objects); err != nil {
			return nil, err
		}
	}

	return out, nil
}

func portalUsers(ctx context.Context, db configdoc.DB, data *portalData) error {
	users, err := db.QueryContext(ctx, `
		SELECT u.id, u.email, u.display_name, coalesce(u.username, ''), u.status = 'active',
			coalesce(array_agg(g.name ORDER BY g.name) FILTER (WHERE g.id IS NOT NULL), '{}'),
			u.created_at, u.last_login_at
		FROM users u
		LEFT JOIN group_members gm ON gm.user_id = u.id
		LEFT JOIN groups g ON g.id = gm.group_id
		GROUP BY u.id
		ORDER BY u.email`)
	if err != nil {
		return err
	}
	defer users.Close()
	for users.Next() {
		var u portalUser
		if err := users.Scan(&u.ID, &u.Email, &u.DisplayName, &u.Username, &u.Active, pq.Array(&u.Groups),
			&u.CreatedAt, &u.LastSignedIn); err != nil {
			return err
		}
		data.Users = append(data.Users, u)
	}

	return users.Err()
}

func portalGroups(ctx context.Context, db configdoc.DB, data *portalData) error {
	groups, err := db.QueryContext(ctx, `
		SELECT g.id, g.name,
			coalesce(array_agg(u.email ORDER BY u.email) FILTER (WHERE u.id IS NOT NULL), '{}'), g.created_at
		FROM groups g
		LEFT JOIN group_members gm ON gm.group_id = g.id
		LEFT JOIN users u ON u.id = gm.user_id
		GROUP BY g.id
		ORDER BY g.name`)
	if err != nil {
		return err
	}
	defer groups.Close()
	for groups.Next() {
		var g portalGroup
		if err := groups.Scan(&g.ID, &g.Name, pq.Array(&g.Members), &g.CreatedAt); err != nil {
			return err
		}
		data.Groups = append(data.Groups, g)
	}

	return groups.Err()
}

func portalDocuments(ctx context.Context, db configdoc.DB, data *portalData) error {
	counts, lastRuns, err := portalCounts(ctx, db)
	if err != nil {
		return err
	}

	blueprints, err := configdoc.NewStore(db, blueprint.Name).List(ctx)
	if err != nil {
		return err
	}
	for _, record := range blueprints {
		if !record.Published() {
			continue
		}
		spec, _ := blueprint.Parse(record.PublishedSource)
		b := portalBlueprint{
			Identifier: record.Identifier, Title: spec.Title, Description: spec.Description,
			Icon: spec.Icon, Version: record.PublishedVersion, Relations: []string{},
			Entities: counts["blueprint:"+record.Identifier], PublishedAt: record.PublishedAt,
		}
		for _, prop := range spec.Properties {
			b.Properties++
			if prop.IsRelation() {
				b.Relations = append(b.Relations, prop.Relation)
			}
		}
		data.Blueprints = append(data.Blueprints, b)
	}

	integrations, err := configdoc.NewStore(db, Name).List(ctx)
	if err != nil {
		return err
	}
	for _, record := range integrations {
		if !record.Published() {
			continue
		}
		spec, _ := Parse(record.PublishedSource)
		in := portalIntegration{
			Identifier: record.Identifier, Title: spec.Title, Description: spec.Description,
			Connector: spec.Connector.Type, Version: record.PublishedVersion, Blueprints: []string{},
			Entities: counts["integration:"+record.Identifier], PublishedAt: record.PublishedAt,
		}
		if last, ok := lastRuns[record.Identifier]; ok {
			in.LastRunAt, in.LastRunOutcome = &last.at, &last.outcome
		}
		for _, m := range spec.Mappings {
			in.Blueprints = append(in.Blueprints, m.Blueprint)
		}
		data.Integrations = append(data.Integrations, in)
	}

	return nil
}

// portalCounts counts entities by blueprint and by integration, keyed as
// "blueprint:<id>" and "integration:<id>", and finds each integration's last run.
func portalCounts(ctx context.Context, db configdoc.DB) (map[string]int, map[string]lastRun, error) {
	counts := map[string]int{}
	rows, err := db.QueryContext(ctx, `
		SELECT d.kind || ':' || d.identifier, count(*) FROM entities e
		JOIN config_documents d ON d.id IN (e.blueprint_id, e.integration_id)
		GROUP BY 1`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			key   string
			count int
		)
		if err := rows.Scan(&key, &count); err != nil {
			return nil, nil, err
		}
		counts[key] = count
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	lastRuns := map[string]lastRun{}
	runs, err := db.QueryContext(ctx, `
		SELECT DISTINCT ON (r.integration_id) d.identifier, r.finished_at,
			CASE WHEN r.error <> '' THEN 'failed' WHEN r.failed > 0 THEN 'warnings' ELSE 'succeeded' END
		FROM ingestion_runs r
		JOIN config_documents d ON d.id = r.integration_id
		ORDER BY r.integration_id, r.finished_at DESC`)
	if err != nil {
		return nil, nil, err
	}
	defer runs.Close()
	for runs.Next() {
		var (
			id   string
			last lastRun
		)
		if err := runs.Scan(&id, &last.at, &last.outcome); err != nil {
			return nil, nil, err
		}
		lastRuns[id] = last
	}

	return counts, lastRuns, runs.Err()
}
