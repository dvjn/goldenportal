package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"go.opentelemetry.io/otel/attribute"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/entity"
	"github.com/dvjn/goldenportal/internal/widget"
)

const planYAML = `
kind: Dashboard
metadata: {identifier: plan, title: Plan}
spec:
  inputs:
    tier: {type: enum, values: [critical, standard], multiple: true}
  queries:
    services:
      blueprint: service
      filter: {property: tier, op: in, values: $input.tier}
    unknownField:
      blueprint: service
      filter: {property: nope, op: exists}
  widgets:
    count: {type: stat, title: Services, value: {query: services}}
    critical: {type: stat, title: Critical, value: {query: services, share: {property: tier, op: eq, value: critical}}}
    cost: {type: stat, title: Cost, value: {query: services, sum: cost}}
    byOwner: {type: bar, title: By Owner, query: services, groupBy: owner, limit: 3}
    owners: {type: donut, title: Owners, query: services, groupBy: owner, limit: 5}
    byTier: {type: bar, title: By Tier, query: services, groupBy: tier}
    list: {type: table, title: Services, query: services, columns: [{path: tier}, {title: Team, path: owner.title}], sort: -cost, limit: 5}
    broken: {type: stat, title: Broken, value: {query: unknownField}}
    note: {type: markdown, text: Hello}
`

func TestLoadPlansEachQueryOnce(t *testing.T) {
	spec := mustParse(t, planYAML)
	bp, issues := blueprint.Parse(`
kind: Blueprint
metadata: {identifier: service, title: Service}
spec:
  properties:
    tier: {type: string}
    cost: {type: number, display: {unit: USD}}
    owner: {relation: team}
`)
	if configdoc.HasErrors(issues) {
		t.Fatal(issues)
	}
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// Statements run in parallel, so they may arrive in any order.
	mock.MatchExpectationsInOrder(false)
	bp.ID = "5a0c1f3e-2b4d-4e6f-8a9b-0c1d2e3f4a5b"
	where := regexp.QuoteMeta(`WHERE e.blueprint_id = '` + bp.ID + `' AND ((e.properties @> $1::jsonb))`)
	mock.ExpectQuery(`^SELECT \(count\(\*\)\)::float8, \(100\.0 \* count\(\*\) FILTER .*\(sum\(.*`+where+`$`).
		WithArgs(`{"tier":"critical"}`, `{"tier":"critical"}`).
		WillReturnRows(sqlmock.NewRows([]string{"count", "share", "sum"}).AddRow(4, 50, 1200))
	mock.ExpectQuery(`^SELECT g\.value, g\.count, t\.title FROM \(SELECT e\.relations -> 'owner' AS value.*`+where+`.*LIMIT \$2$`).
		WithArgs(`{"tier":"critical"}`, 6).
		WillReturnRows(sqlmock.NewRows([]string{"value", "count", "title"}).
			AddRow(nil, 1, nil).AddRow([]byte(`"payments"`), 2, "Payments").AddRow([]byte(`"search"`), 1, "Search"))
	mock.ExpectQuery(`^SELECT g\.value, g\.count, NULL::text FROM \(SELECT e\.properties -> 'tier' AS value.*`+where).
		WithArgs(`{"tier":"critical"}`, 9).
		WillReturnError(errors.New("connection reset"))
	mock.ExpectQuery(`^SELECT e\.id, e\.identifier, e\.title, e\.properties -> 'tier' AS k0, NULL::jsonb, to_jsonb\(\(SELECT h1\.title .*`+where+
		`.*ORDER BY \(CASE WHEN jsonb_typeof\(e\.properties -> 'cost'\) = 'number' THEN \(e\.properties -> 'cost'\)::numeric END\) DESC NULLS LAST`).
		WithArgs(`{"tier":"critical"}`, 5).
		WillReturnRows(sqlmock.NewRows([]string{"id", "identifier", "title", "v1", "l1", "v2", "l2"}).
			AddRow("1", "ledger", "Ledger", []byte(`"critical"`), nil, []byte(`"Payments"`), nil))

	var logged []string
	l := loader{
		ctx: t.Context(), db: db, store: entity.NewStore(db),
		blueprints: map[string]blueprint.Spec{"service": bp, "team": {Identifier: "team"}},
		env:        widget.Env{Values: widget.Values{widget.ValueInput + "tier": {"critical"}}},
		log:        func(_ context.Context, msg string, _ error, _ ...attribute.KeyValue) { logged = append(logged, msg) },
	}
	data := l.load(spec.Widgets)
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}

	encode := func(id string) string {
		encoded, _ := json.Marshal(data.Widgets[id])
		return string(encoded)
	}
	for id, want := range map[string]string{
		"count":    `{"data":{"value":4}}`,
		"critical": `{"data":{"value":50,"unit":"%","count":2,"total":4}}`,
		"cost":     `{"data":{"value":1200,"unit":"USD"}}`,
		"byOwner":  `{"data":{"groups":[{"value":"payments","title":"Payments","count":2},{"value":"search","title":"Search","count":1}],"none":1}}`,
		"owners":   `{"data":{"groups":[{"value":"payments","title":"Payments","count":2},{"value":"search","title":"Search","count":1}],"none":1}}`,
		"byTier":   `{"error":"the widget's data couldn't be loaded"}`,
		"broken":   `{"error":"service has no property \"nope\""}`,
		"note":     `{"data":{"text":"Hello"}}`,
	} {
		if got := encode(id); got != want {
			t.Errorf("%s = %s\nwant %s", id, got, want)
		}
	}
	list := encode("list")
	for _, want := range []string{
		`"blueprint":"service","total":1`,
		`"columns":[{"title":"tier","path":"tier","property":{"identifier":"tier"`,
		`{"title":"Team","path":"owner.title","builtIn":"title"}`,
		`"rows":[{"identifier":"ledger","title":"Ledger","values":["critical","Payments"]}]`,
	} {
		if !strings.Contains(list, want) {
			t.Errorf("list = %s\nwant %s", list, want)
		}
	}
	if len(logged) != 1 {
		t.Errorf("logged = %v, want the failed statement only", logged)
	}
}

// A table reads its page and count, with its conditions, its formats'
// values and its row link's paths, and a date's newest shows as its age:
// nothing loads entities to work on. A condition whose variable is unset
// falls away, as a query's does, so every row meets it.
func TestTablesAndAgesReadNoDocuments(t *testing.T) {
	spec := mustParse(t, `
kind: Dashboard
metadata: {identifier: plan, title: Plan}
spec:
  inputs:
    team: {type: relation, relation: team, multiple: true}
  queries:
    services: {blueprint: service}
  widgets:
    deployed: {type: stat, title: Deployed, value: {query: services, max: deployedAt}}
    first: {type: stat, title: First, value: {query: services, min: deployedAt, unit: date}}
    owners: {type: stat, title: Owners, value: {query: services, distinct: owner}}
    born: {type: stat, title: Born, value: {query: services, min: bornOn, unit: date}}
    mine:
      type: table
      title: Mine
      query: services
      columns:
        - {path: homepage}
        - {title: Mine, filter: {property: owner, op: in, values: $input.team}}
      sort: -Mine
      rowLink: {url: '{{ homepage }}'}
    list:
      type: table
      title: Services
      query: services
      columns:
        - {title: Dollars, path: cost, format: . / 100}
        - {title: Critical, filter: {property: tier, op: eq, value: critical}}
      sort: -Critical
      limit: 2
      link: {query: services}
      rowLink: {url: 'https://x.example.com/{{ identifier }}?team={{ owner.title | ascii_downcase }}'}
`)
	bp, issues := blueprint.Parse(`
kind: Blueprint
metadata: {identifier: service, title: Service}
spec:
  properties:
    tier: {type: string}
    cost: {type: number}
    owner: {relation: team}
    deployedAt: {type: string, format: date-time}
    bornOn: {type: string, format: date}
    homepage: {type: string}
`)
	if configdoc.HasErrors(issues) {
		t.Fatal(issues)
	}
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.MatchExpectationsInOrder(false)
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	deployed := now.AddDate(0, 0, -12)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT (extract(epoch FROM now() - max(gp_time((e.properties -> 'deployedAt' #>> '{}')))))::float8, ` +
		`(extract(epoch FROM min(gp_time((e.properties -> 'deployedAt' #>> '{}')))))::float8, ` +
		`(count(DISTINCT nullif(e.relations -> 'owner', 'null'::jsonb)))::float8, ` +
		`(extract(epoch FROM min(gp_time((e.properties -> 'bornOn' #>> '{}')))))::float8 FROM entities e`)).
		WillReturnRows(sqlmock.NewRows([]string{"a", "b", "c", "d"}).AddRow(12*86400, float64(deployed.Unix()), 3, float64(time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC).Unix())))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT e.id, e.identifier, e.title, e.properties -> 'homepage' AS k0, NULL::jsonb, `+
		`to_jsonb(coalesce(true, false)) AS k1, NULL::jsonb FROM entities e`) + `.*` + regexp.QuoteMeta(`ORDER BY k1 DESC, e.title, e.identifier
	LIMIT $1`)).
		WithArgs(10).
		WillReturnRows(sqlmock.NewRows([]string{"id", "identifier", "title", "v0", "l0", "v1", "l1"}).
			AddRow("1", "ledger", "Ledger", []byte(`"https://ledger.example.com/a?b=c&d#e"`), nil, []byte(`true`), nil).
			AddRow("2", "search", "Search", []byte(`"javascript:alert(1)"`), nil, []byte(`true`), nil))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT e.id, e.identifier, e.title, e.properties -> 'cost' AS k0, NULL::jsonb, `+
		`to_jsonb(coalesce((e.properties @> $1::jsonb), false)) AS k1, NULL::jsonb, to_jsonb(e.identifier) AS k2, NULL::jsonb, `+
		`to_jsonb((SELECT h1.title`)+`.*`+regexp.QuoteMeta(`ORDER BY k1 DESC, e.title, e.identifier
	LIMIT $2`)).
		WithArgs(`{"tier":"critical"}`, 2).
		WillReturnRows(sqlmock.NewRows([]string{"id", "identifier", "title", "v0", "l0", "v1", "l1", "v2", "l2", "v3", "l3"}).
			AddRow("1", "ledger", "Ledger", []byte(`1250`), nil, []byte(`true`), nil, []byte(`"ledger"`), nil, []byte(`"Payments"`), nil).
			AddRow("2", "search", "Search", nil, nil, []byte(`false`), nil, []byte(`"search"`), nil, nil, nil))
	mock.ExpectQuery(`^SELECT count\(\*\) FROM entities e WHERE `).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(7))

	var logged []string
	l := loader{
		ctx: t.Context(), db: db, store: entity.NewStore(db), now: now,
		blueprints: map[string]blueprint.Spec{"service": bp, "team": {Identifier: "team"}},
		// Cleared, the team input shows every team.
		env: widget.Env{Values: widget.Values{widget.ValueInput + "team": nil}},
		log: func(_ context.Context, msg string, err error, _ ...attribute.KeyValue) {
			logged = append(logged, fmt.Sprint(msg, ": ", err))
		},
	}
	data := l.load(spec.Widgets)
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	encode := func(id string) string {
		encoded, _ := json.Marshal(data.Widgets[id])
		return string(encoded)
	}
	for id, want := range map[string]string{
		"deployed": `{"data":{"value":1036800,"unit":"s"}}`,
		"first":    fmt.Sprintf(`{"data":{"value":%d,"unit":"date-time"}}`, deployed.Unix()),
		"owners":   `{"data":{"value":3}}`,
		"born":     `{"data":{"value":1792886400,"unit":"date"}}`,
		"mine": `{"data":{"blueprint":"service","total":2,"columns":[{"title":"homepage","path":"homepage","property":{"identifier":"homepage","title":"homepage","type":"string"}},{"title":"Mine","condition":true}],` +
			`"rows":[{"identifier":"ledger","title":"Ledger","values":["https://ledger.example.com/a?b=c\u0026d#e",true],"link":{"href":"https://ledger.example.com/a?b=c\u0026d#e"}},` +
			`{"identifier":"search","title":"Search","values":["javascript:alert(1)",true]}]}}`,
		"list": `{"data":{"blueprint":"service","total":7,"columns":[{"title":"Dollars","path":"cost","format":". / 100"},{"title":"Critical","condition":true}],` +
			`"rows":[{"identifier":"ledger","title":"Ledger","values":[12.5,true],"link":{"href":"https://x.example.com/ledger?team=payments"}},` +
			`{"identifier":"search","title":"Search","values":[null,false],"link":{"href":"https://x.example.com/search?team="}}],"link":{"blueprint":"service"}}}`,
	} {
		if got := encode(id); got != want {
			t.Errorf("%s = %s\nwant %s", id, got, want)
		}
	}
	// A stored URL that isn't a web URL leaves its row to open its entity.
	if len(logged) != 1 || !strings.Contains(logged[0], `"javascript:alert(1)" isn't a web URL`) {
		t.Errorf("logged = %q", logged)
	}
}

func TestGroupsTakeTheirPropertysColors(t *testing.T) {
	spec := mustParse(t, `
kind: Dashboard
metadata: {identifier: tiers, title: Tiers}
spec:
  queries:
    services: {blueprint: service}
  widgets:
    byTier: {type: donut, title: By Tier, query: services, groupBy: tier}
`)
	bp, issues := blueprint.Parse(`
kind: Blueprint
metadata: {identifier: service, title: Service}
spec:
  properties:
    tier: {type: string, enum: [critical, standard], display: {as: badge, colors: {critical: rose}}}
`)
	if configdoc.HasErrors(issues) {
		t.Fatal(issues)
	}
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery(`^SELECT g\.value, g\.count, NULL::text FROM \(SELECT e\.properties -> 'tier' AS value`).
		WillReturnRows(sqlmock.NewRows([]string{"value", "count", "title"}).
			AddRow([]byte(`"critical"`), 2, nil).AddRow([]byte(`"standard"`), 1, nil))

	l := loader{ctx: t.Context(), db: db, store: entity.NewStore(db), blueprints: map[string]blueprint.Spec{"service": bp}}
	encoded, _ := json.Marshal(l.load(spec.Widgets).Widgets["byTier"])
	want := `{"data":{"groups":[{"value":"critical","color":"rose","count":2},{"value":"standard","count":1}],"none":0}}`
	if string(encoded) != want {
		t.Errorf("byTier = %s\nwant %s", encoded, want)
	}
}

func TestSplitsFollowTheirPropertysValues(t *testing.T) {
	spec := mustParse(t, `
kind: Dashboard
metadata: {identifier: status, title: Status}
spec:
  queries:
    services: {blueprint: service}
  widgets:
    status: {type: split, title: Status, query: services, groupBy: status}
`)
	bp, issues := blueprint.Parse(`
kind: Blueprint
metadata: {identifier: service, title: Service}
spec:
  properties:
    status: {type: string, enum: [success, failed, canceled]}
`)
	if configdoc.HasErrors(issues) {
		t.Fatal(issues)
	}
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// Most first, as the count comes, with a value the enum doesn't list.
	mock.ExpectQuery(`^SELECT g\.value, g\.count, NULL::text FROM \(SELECT e\.properties -> 'status' AS value`).
		WillReturnRows(sqlmock.NewRows([]string{"value", "count", "title"}).
			AddRow([]byte(`"failed"`), 5, nil).AddRow([]byte(`"skipped"`), 3, nil).AddRow([]byte(`"success"`), 2, nil))

	l := loader{ctx: t.Context(), db: db, store: entity.NewStore(db), blueprints: map[string]blueprint.Spec{"service": bp}}
	encoded, _ := json.Marshal(l.load(spec.Widgets).Widgets["status"])
	want := `{"data":{"groups":[{"value":"success","count":2},{"value":"failed","count":5},{"value":"skipped","count":3}],"none":0}}`
	if string(encoded) != want {
		t.Errorf("status = %s\nwant %s", encoded, want)
	}
}

func TestInputLinksNameRelationInputs(t *testing.T) {
	spec := mustParse(t, `
kind: Dashboard
metadata: {identifier: team, title: Team}
spec:
  inputs:
    team: {type: relation, relation: team, multiple: true}
    tier: {type: enum, values: [critical]}
  widgets:
    note: {type: markdown, text: Hello}
`)
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery(`AND e\.identifier = ANY\(\$1\)`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "blueprint", "identifier", "title", "properties", "relations", "computed", "connection", "created", "updated"}).
			AddRow("1", "team", "payments", "Payments", []byte(`{}`), []byte(`{}`), []byte(`{}`), "registry", time.Time{}, time.Time{}))

	got, err := inputLinks(t.Context(), entity.NewStore(db), spec, widget.Values{
		widget.ValueInput + "team": {"search", "payments"},
		widget.ValueInput + "tier": {"critical"},
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(got)
	if want := `{"team":[{"identifier":"search"},{"identifier":"payments","title":"Payments"}]}`; string(encoded) != want {
		t.Errorf("inputLinks = %s\nwant %s", encoded, want)
	}
}
