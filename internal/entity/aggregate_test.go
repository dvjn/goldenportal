package entity

import (
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/dvjn/goldenportal/internal/blueprint"
)

func mockStore(t *testing.T) (Store, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
		db.Close()
	})

	return NewStore(db), mock
}

func match(t *testing.T, filter string) Match {
	t.Helper()
	f, err := ParseFilter(filter)
	if err != nil {
		t.Fatal(err)
	}
	svc := service(t)
	svc.ID = serviceID
	specs := map[string]blueprint.Spec{"service": svc, "team": {ID: teamID, Identifier: "team"}}
	ladders := map[string]blueprint.Ladder{"service-readiness": {Blueprint: "service", Tiers: []blueprint.Rung{{Name: "gold", Title: "Gold", Color: "amber"}}}}
	m, err := NewMatch(Query{Blueprint: svc, Blueprints: specs, Ladders: ladders, Filter: f})
	if err != nil {
		t.Fatal(err)
	}

	return m
}

func TestAggregatesAreOneStatement(t *testing.T) {
	store, mock := mockStore(t)
	m := match(t, `{"property":"owner","op":"eq","value":"payments"}`)
	critical, _ := ParseFilter(`{"property":"tier","op":"eq","value":"critical"}`)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT (count(*))::float8, (sum((CASE WHEN jsonb_typeof(e.properties -> 'stars') = 'number' THEN (e.properties -> 'stars')::numeric END)))::float8, `+
		`(100.0 * count(*) FILTER (WHERE (e.properties @> $2::jsonb)) / nullif(count(*), 0))::float8, (avg(((SELECT r.score`)+
		`.*`+regexp.QuoteMeta(` FROM entities e
	WHERE e.blueprint_id = '`+serviceID+`' AND (e.relations @> $1::jsonb)`)+`$`).
		WithArgs(`{"owner":"payments"}`, `{"tier":"critical"}`).
		WillReturnRows(sqlmock.NewRows([]string{"a", "b", "c", "d"}).AddRow(4, 120, 25, nil))

	values, err := store.Aggregates(t.Context(), m, []Aggregate{
		{Op: "count"}, {Op: "sum", Path: "stars"}, {Op: "share", Share: critical}, {Op: "avg", Path: "readiness.score"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if *values[0] != 4 || *values[1] != 120 || *values[2] != 25 || values[3] != nil {
		t.Fatalf("values = %v", values)
	}
}

func TestNarrowedAggregatesFilterTheirRows(t *testing.T) {
	c := &compiler{spec: service(t), specs: map[string]blueprint.Spec{"team": {Identifier: "team"}}}
	critical := &Filter{Property: "tier", Op: OpEq, Value: "critical"}
	for _, test := range []struct {
		a    Aggregate
		want string
	}{
		{Aggregate{Op: "count", Filter: critical}, `count(*) FILTER (WHERE (e.properties @> $1::jsonb))`},
		{Aggregate{Op: "p95", Path: "stars"}, `percentile_cont(0.95) WITHIN GROUP (ORDER BY (CASE WHEN jsonb_typeof(e.properties -> 'stars') = 'number' THEN (e.properties -> 'stars')::numeric END))`},
		{
			Aggregate{Op: "share", Filter: critical, Share: &Filter{Property: "critical", Op: OpEq, Value: true}},
			`100.0 * count(*) FILTER (WHERE (e.properties @> $1::jsonb) AND (e.computed @> $2::jsonb)) / nullif(count(*) FILTER (WHERE (e.properties @> $1::jsonb)), 0)`,
		},
	} {
		c.args = nil
		if got, err := aggregateSQL(c, test.a); err != nil || got != test.want {
			t.Errorf("%+v = %s, %v\nwant %s", test.a, got, err, test.want)
		}
	}
}

func TestCheckAggregate(t *testing.T) {
	specs := map[string]blueprint.Spec{"team": {Identifier: "team"}}
	for _, test := range []struct {
		a    Aggregate
		want string
	}{
		{Aggregate{Op: "sum", Path: "stars"}, ""},
		{Aggregate{Op: "max", Path: "readiness.passed"}, ""},
		{Aggregate{Op: "sum", Path: "tier"}, "works on numbers"},
		{Aggregate{Op: "sum", Path: "tags"}, "works on numbers"},
		{Aggregate{Op: "median", Path: "stars"}, "must be one of"},
		{Aggregate{Op: "share"}, "needs the condition"},
		{Aggregate{Op: "p95", Path: "stars"}, ""},
		{Aggregate{Op: "p99", Path: "tier"}, "p99 works on numbers, and tier isn't one"},
		{Aggregate{Op: "count", Filter: &Filter{Property: "nope", Op: OpExists}}, `no property "nope"`},
	} {
		err := CheckAggregate(specs, service(t), test.a)
		if (test.want == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), test.want)) {
			t.Errorf("%+v: err = %v, want %q", test.a, err, test.want)
		}
	}
}

func TestGroupIsOneStatement(t *testing.T) {
	store, mock := mockStore(t)
	m := match(t, "")
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT g.value, g.count, t.title FROM (SELECT e.relations -> 'owner' AS value, count(*) AS count`) + `.*` +
		regexp.QuoteMeta(`GROUP BY 1) g
	LEFT JOIN entities t ON t.blueprint_id = '`+teamID+`' AND t.identifier = g.value #>> '{}'
	ORDER BY g.value IS NULL DESC, g.count DESC, g.value
	LIMIT $1`)).
		WithArgs(3).
		WillReturnRows(sqlmock.NewRows([]string{"value", "count", "title"}).
			AddRow(nil, 2, nil).AddRow([]byte(`"payments"`), 5, "Payments").AddRow([]byte(`"search"`), 3, "Search").AddRow([]byte(`"ads"`), 1, nil))

	facets, none, err := store.Group(t.Context(), m, "owner", 2)
	if err != nil {
		t.Fatal(err)
	}
	if none != 2 || len(facets) != 2 || facets[0].Value != "payments" || facets[0].Title != "Payments" || facets[1].Count != 3 {
		t.Fatalf("facets = %+v, none %d", facets, none)
	}
}

func TestGroupByListAndTier(t *testing.T) {
	store, mock := mockStore(t)
	m := match(t, "")
	mock.ExpectQuery(regexp.QuoteMeta(`LEFT JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(e.properties -> 'tags') = 'array' THEN e.properties -> 'tags' END) item ON true`)).
		WithArgs(9).
		WillReturnRows(sqlmock.NewRows([]string{"value", "count", "title"}).AddRow([]byte(`"pci"`), 2, nil))
	if _, _, err := store.Group(t.Context(), m, "tags", 8); err != nil {
		t.Fatal(err)
	}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT g.value, g.count, NULL::text FROM (SELECT to_jsonb(nullif((SELECT r.tier`)).
		WithArgs(9).
		WillReturnRows(sqlmock.NewRows([]string{"value", "count", "title"}).AddRow(nil, 4, nil).AddRow([]byte(`"gold"`), 2, nil))
	facets, none, err := store.Group(t.Context(), m, "readiness", 8)
	if err != nil {
		t.Fatal(err)
	}
	if none != 4 || facets[0].Title != "Gold" || facets[0].Color != "amber" {
		t.Fatalf("facets = %+v, none %d", facets, none)
	}
}

func TestRowsReadOnlyTheirPaths(t *testing.T) {
	store, mock := mockStore(t)
	m := match(t, "")
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT e.id, e.identifier, e.title, e.properties -> 'tier', NULL::jsonb, `+
		`to_jsonb((SELECT h1.title FROM entities h1 WHERE h1.blueprint_id = '`+teamID+`' AND h1.identifier = e.relations ->> 'owner')), NULL::jsonb, `+
		`e.relations -> 'owner', (SELECT jsonb_agg(jsonb_build_object('identifier', x.id, 'title', t.title) ORDER BY x.n)`) + `.*` +
		regexp.QuoteMeta(`to_jsonb((SELECT r.tier FROM scorecard_results r WHERE r.entity_id = e.id`) + `.*` +
		regexp.QuoteMeta(`jsonb_build_array(jsonb_build_object('identifier', e.identifier))`) + `.*` +
		regexp.QuoteMeta(`FROM entities e
	WHERE e.blueprint_id = '`+serviceID+`'
	ORDER BY gp_time((e.properties -> 'lastDeployedAt' #>> '{}')) DESC NULLS LAST, e.title, e.identifier
	LIMIT $1`)).
		WithArgs(1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "identifier", "title", "v1", "l1", "v2", "l2", "v3", "l3", "v4", "l4"}).
			AddRow("1", "ledger", "Ledger", []byte(`"critical"`), nil, []byte(`"Payments"`), nil,
				[]byte(`"payments"`), []byte(`[{"identifier":"payments","title":"Payments"}]`), []byte(`""`), []byte(`[{"identifier":"ledger"}]`)))
	mock.ExpectQuery(`^SELECT count\(\*\) FROM entities e WHERE e.blueprint_id = '` + serviceID + `'$`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(7))

	rows, total, err := store.Rows(t.Context(), m, "-lastDeployedAt", 1, []string{"tier", "owner.title", "owner", "readiness"})
	if err != nil {
		t.Fatal(err)
	}
	r := rows[0]
	if total != 7 || r.Identifier != "ledger" || r.Values[0] != "critical" || r.Values[1] != "Payments" || r.Values[3] != "" {
		t.Fatalf("rows = %+v, total %d", rows, total)
	}
	if r.Links[0] != nil || r.Links[2][0].Title != "Payments" || r.Links[3][0].Identifier != "ledger" {
		t.Fatalf("links = %+v", r.Links)
	}
}

func TestRowsRejectWhatTheyCantRead(t *testing.T) {
	store, _ := mockStore(t)
	m := match(t, "")
	for path, want := range map[string]string{
		"dependsOn.title": "relates to many entities",
		"nope":            `no property "nope"`,
		"$title":          `no property "$title"`,
	} {
		if _, _, err := store.Rows(t.Context(), m, "", 5, []string{path}); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want %q", path, err, want)
		}
	}
}
