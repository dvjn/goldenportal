package integration

import (
	"context"
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
	PublishedAt time.Time  `json:"publishedAt"`
}

// portalSnapshot reads the portal's users, groups and published blueprints
// and integrations. Users are named by email and groups by name, so
// mappings can relate them to each other.
func portalSnapshot(ctx context.Context, db configdoc.DB) (any, error) {
	data := portalData{Users: []portalUser{}, Groups: []portalGroup{}, Blueprints: []portalBlueprint{},
		Integrations: []portalIntegration{}}

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
		return nil, err
	}
	defer users.Close()
	for users.Next() {
		var u portalUser
		if err := users.Scan(&u.ID, &u.Email, &u.DisplayName, &u.Username, &u.Active, pq.Array(&u.Groups),
			&u.CreatedAt, &u.LastSignedIn); err != nil {
			return nil, err
		}
		data.Users = append(data.Users, u)
	}
	if err := users.Err(); err != nil {
		return nil, err
	}

	groups, err := db.QueryContext(ctx, `
		SELECT g.id, g.name,
			coalesce(array_agg(u.email ORDER BY u.email) FILTER (WHERE u.id IS NOT NULL), '{}'), g.created_at
		FROM groups g
		LEFT JOIN group_members gm ON gm.group_id = g.id
		LEFT JOIN users u ON u.id = gm.user_id
		GROUP BY g.id
		ORDER BY g.name`)
	if err != nil {
		return nil, err
	}
	defer groups.Close()
	for groups.Next() {
		var g portalGroup
		if err := groups.Scan(&g.ID, &g.Name, pq.Array(&g.Members), &g.CreatedAt); err != nil {
			return nil, err
		}
		data.Groups = append(data.Groups, g)
	}
	if err := groups.Err(); err != nil {
		return nil, err
	}

	counts, lastRuns, err := portalCounts(ctx, db)
	if err != nil {
		return nil, err
	}

	blueprints, err := configdoc.NewStore(db, blueprint.Name).List(ctx)
	if err != nil {
		return nil, err
	}
	for _, record := range blueprints {
		if !record.Published() {
			continue
		}
		spec, _ := blueprint.Parse(record.PublishedSource)
		b := portalBlueprint{Identifier: record.Identifier, Title: spec.Title, Description: spec.Description,
			Icon: spec.Icon, Version: record.PublishedVersion, Relations: []string{},
			Entities: counts["blueprint:"+record.Identifier], PublishedAt: record.PublishedAt}
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
		return nil, err
	}
	for _, record := range integrations {
		if !record.Published() {
			continue
		}
		spec, _ := Parse(record.PublishedSource)
		in := portalIntegration{Identifier: record.Identifier, Title: spec.Title, Description: spec.Description,
			Connector: spec.Connector, Version: record.PublishedVersion, Blueprints: []string{},
			Entities: counts["integration:"+record.Identifier], PublishedAt: record.PublishedAt}
		if last, ok := lastRuns[record.Identifier]; ok {
			in.LastRunAt = &last
		}
		for _, m := range spec.Mappings {
			in.Blueprints = append(in.Blueprints, m.Blueprint)
		}
		data.Integrations = append(data.Integrations, in)
	}

	return toJSONValue(data)
}

// portalCounts counts entities by blueprint and by integration, keyed as
// "blueprint:<id>" and "integration:<id>", and finds each integration's last run.
func portalCounts(ctx context.Context, db configdoc.DB) (map[string]int, map[string]time.Time, error) {
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

	lastRuns := map[string]time.Time{}
	runs, err := db.QueryContext(ctx, `
		SELECT d.identifier, max(r.finished_at) FROM ingestion_runs r
		JOIN config_documents d ON d.id = r.integration_id
		GROUP BY d.identifier`)
	if err != nil {
		return nil, nil, err
	}
	defer runs.Close()
	for runs.Next() {
		var (
			id   string
			last time.Time
		)
		if err := runs.Scan(&id, &last); err != nil {
			return nil, nil, err
		}
		lastRuns[id] = last
	}

	return counts, lastRuns, runs.Err()
}
