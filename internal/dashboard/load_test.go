package dashboard

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

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
	where := regexp.QuoteMeta(`WHERE b.kind = 'blueprint' AND b.identifier = $1 AND ((e.properties @> $2::jsonb))`)
	mock.ExpectQuery(`^SELECT \(count\(\*\)\)::float8, \(100\.0 \* count\(\*\) FILTER .*\(sum\(.*`+where+`$`).
		WithArgs("service", `{"tier":"critical"}`, `{"tier":"critical"}`).
		WillReturnRows(sqlmock.NewRows([]string{"count", "share", "sum"}).AddRow(4, 50, 1200))
	mock.ExpectQuery(`^SELECT g\.value, g\.count, t\.title FROM \(SELECT e\.relations -> 'owner' AS value.*`+where+`.*LIMIT \$3$`).
		WithArgs("service", `{"tier":"critical"}`, 6).
		WillReturnRows(sqlmock.NewRows([]string{"value", "count", "title"}).
			AddRow(nil, 1, nil).AddRow([]byte(`"payments"`), 2, "Payments").AddRow([]byte(`"search"`), 1, "Search"))
	mock.ExpectQuery(`^SELECT g\.value, g\.count, NULL::text FROM \(SELECT e\.properties -> 'tier' AS value.*`+where).
		WithArgs("service", `{"tier":"critical"}`, 9).
		WillReturnError(errors.New("connection reset"))
	mock.ExpectQuery(`^SELECT e\.id, e\.identifier, e\.title, count\(\*\) OVER \(\), e\.properties -> 'tier', NULL::jsonb, to_jsonb\(\(SELECT h1\.title .*`+where+`.*ORDER BY e\.properties -> 'cost' DESC`).
		WithArgs("service", `{"tier":"critical"}`, 5).
		WillReturnRows(sqlmock.NewRows([]string{"id", "identifier", "title", "total", "v1", "l1", "v2", "l2"}).
			AddRow("1", "ledger", "Ledger", 4, []byte(`"critical"`), nil, []byte(`"Payments"`), nil))

	var logged []string
	l := loader{
		ctx: t.Context(), db: db, store: entity.NewStore(db),
		blueprints: map[string]blueprint.Spec{"service": bp, "team": {Identifier: "team"}},
		env:        widget.Env{Values: widget.Values{widget.ValueInput + "tier": {"critical"}}},
		logf:       func(format string, args ...any) { logged = append(logged, format) },
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
		`"blueprint":"service","total":4`,
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
	mock.ExpectQuery(`AND e\.identifier = ANY\(\$2\)`).
		WithArgs("team", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "blueprint", "identifier", "title", "properties", "relations", "computed", "integration", "created", "updated"}).
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

func TestMatrixSharesEachMeasureByGroup(t *testing.T) {
	spec := mustParse(t, `
kind: Dashboard
metadata: {identifier: docs, title: Docs}
spec:
  queries:
    services: {blueprint: service}
  widgets:
    byTeam:
      type: matrix
      title: By Team
      query: services
      groupBy: team
      measures: [{title: Documented, share: {property: docs, op: eq, value: true}}]
      overall: true
`)
	bp, issues := blueprint.Parse(`
kind: Blueprint
metadata: {identifier: service, title: Service}
spec:
  properties:
    team: {type: string}
    docs: {type: boolean}
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
	// The rows' query is the measure's base too, so they share a grouping.
	mock.ExpectQuery(`^SELECT \(count\(\*\)\)::float8 FROM`).WithArgs("service").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(4))
	mock.ExpectQuery(`^SELECT g\.value, g\.count, NULL::text FROM \(SELECT e\.properties -> 'team' AS value`).WithArgs("service", 21).
		WillReturnRows(sqlmock.NewRows([]string{"value", "count", "title"}).
			AddRow([]byte(`"payments"`), 3, nil).AddRow([]byte(`"search"`), 1, nil))
	mock.ExpectQuery(`^SELECT \(count\(\*\)\)::float8 FROM`).WithArgs("service", `{"docs":true}`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))
	mock.ExpectQuery(`^SELECT g\.value, g\.count, NULL::text FROM \(SELECT e\.properties -> 'team' AS value`).WithArgs("service", `{"docs":true}`, 21).
		WillReturnRows(sqlmock.NewRows([]string{"value", "count", "title"}).AddRow([]byte(`"payments"`), 2, nil))

	l := loader{ctx: t.Context(), db: db, store: entity.NewStore(db), blueprints: map[string]blueprint.Spec{"service": bp}}
	data := l.load(spec.Widgets)
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(data.Widgets["byTeam"])
	for _, want := range []string{
		`"measures":["Documented"]`,
		`"overall":[{"value":50,"count":2,"total":4,"link":{"blueprint":"service","filter":{"not":{"property":"docs","op":"eq","value":true}}}}]`,
		`{"value":"payments","count":3,"cells":[{"value":66.66666666666667,"count":2,"total":3,"link":{"blueprint":"service","filter":{"and":[{"property":"team","op":"eq","value":"payments"},{"not":{"property":"docs","op":"eq","value":true}}]}}}],"link":{"blueprint":"service","filter":{"property":"team","op":"eq","value":"payments"}}}`,
		`{"value":"search","count":1,"cells":[{"value":0,"count":0,"total":1,"link":`,
	} {
		if !strings.Contains(string(encoded), want) {
			t.Errorf("byTeam = %s\nwant %s", encoded, want)
		}
	}
}
