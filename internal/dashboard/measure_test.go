package dashboard

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"go.opentelemetry.io/otel/attribute"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/entity"
)

const jobYAML = `
kind: Blueprint
metadata: {identifier: job, title: Job}
spec:
  properties:
    stage: {type: string, enum: [build, test, deploy]}
    status: {type: string}
    duration: {type: number, display: {unit: s}}
    finishedAt: {type: string, format: date-time}
`

// loadMocked loads a dashboard's widgets over a blueprint, against
// statements the mock expects in any order, and gives each widget's data
// as JSON.
func loadMocked(t *testing.T, dashboard, bpSource string, expect func(sqlmock.Sqlmock)) map[string]string {
	t.Helper()
	spec := mustParse(t, dashboard)
	bp, issues := blueprint.Parse(bpSource)
	if configdoc.HasErrors(issues) {
		t.Fatal(issues)
	}
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.MatchExpectationsInOrder(false)
	expect(mock)

	l := loader{
		ctx: t.Context(), db: db, store: entity.NewStore(db), blueprints: map[string]blueprint.Spec{bp.Identifier: bp},
		now: time.Date(2026, 11, 9, 15, 0, 0, 0, time.UTC),
		log: func(_ context.Context, msg string, err error, _ ...attribute.KeyValue) { t.Errorf("%s: %v", msg, err) },
	}
	data := l.load(spec.Widgets)
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for id, result := range data.Widgets {
		encoded, _ := json.Marshal(result)
		out[id] = string(encoded)
	}

	return out
}

func wantIn(t *testing.T, id, got string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Errorf("%s = %s\nwant %s", id, got, want)
		}
	}
}

func TestMatrixWorksOutEachMeasureByGroupInOneStatement(t *testing.T) {
	got := loadMocked(t, `
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
      measures:
        - {title: Documented, share: {property: docs, op: eq, value: true}}
        - {title: P95 Cost, p95: cost}
      overall: true
`, `
kind: Blueprint
metadata: {identifier: service, title: Service}
spec:
  properties:
    team: {type: string}
    docs: {type: boolean}
    cost: {type: number, display: {unit: USD}}
`, func(mock sqlmock.Sqlmock) {
		measures := `\(100\.0 \* count\(\*\) FILTER \(WHERE \(e\.properties @> \$1::jsonb\)\) / nullif\(count\(\*\), 0\)\)::float8, ` +
			`\(count\(\*\)\)::float8, \(count\(\*\) FILTER \(WHERE \(e\.properties @> \$2::jsonb\)\)\)::float8, ` +
			`\(percentile_cont\(0\.95\) WITHIN GROUP \(ORDER BY \(CASE WHEN jsonb_typeof\(e\.properties -> 'cost'\)`
		mock.ExpectQuery(`^SELECT `+measures).WithArgs(`{"docs":true}`, `{"docs":true}`).
			WillReturnRows(sqlmock.NewRows([]string{"share", "total", "count", "p95"}).AddRow(50, 4, 2, 110))
		mock.ExpectQuery(`^SELECT g\.value, g\.count, NULL::text, g\.v0, g\.v1, g\.v2, g\.v3 FROM \(SELECT e\.properties -> 'team' AS value, count\(\*\) AS count, \(100\.0 .* AS v0, .* AS v3\s+FROM`+
			`.*ORDER BY g\.value IS NULL DESC, g\.count DESC, g\.value`).
			WithArgs(`{"docs":true}`, `{"docs":true}`, 11).
			WillReturnRows(sqlmock.NewRows([]string{"value", "count", "title", "v0", "v1", "v2", "v3"}).
				AddRow([]byte(`"payments"`), 3, nil, 66.66666666666667, 3, 2, 120).
				AddRow([]byte(`"search"`), 1, nil, 0, 1, 0, 40))
	})
	wantIn(t, "byTeam", got["byTeam"],
		`"measures":[{"title":"Documented","unit":"%"},{"title":"P95 Cost","unit":"USD"}]`,
		`"overall":[{"value":50,"count":2,"total":4,"link":{"blueprint":"service","filter":{"not":{"property":"docs","op":"eq","value":true}}}},{"value":110,"link":{"blueprint":"service"}}]`,
		`{"value":"payments","count":3,"cells":[{"value":66.66666666666667,"count":2,"total":3,"link":{"blueprint":"service","filter":{"and":[{"property":"team","op":"eq","value":"payments"},{"not":{"property":"docs","op":"eq","value":true}}]}}},`+
			`{"value":120,"link":{"blueprint":"service","filter":{"property":"team","op":"eq","value":"payments"}}}]`,
		`{"value":"search","count":1,"cells":[{"value":0,"count":0,"total":1,"link":`,
	)
}

func TestBarsShowAMeasurePerGroupInTheirOrder(t *testing.T) {
	got := loadMocked(t, `
kind: Dashboard
metadata: {identifier: ci, title: CI}
spec:
  queries:
    jobs: {blueprint: job}
  widgets:
    failing:
      type: bar
      title: Failure Share by Stage
      query: jobs
      groupBy: stage
      measure:
        filter: {property: status, op: ne, value: skipped}
        share: {property: status, op: eq, value: failed}
    stages:
      type: bar
      title: Stages
      query: jobs
      groupBy: stage
      sort: group
      link: {query: jobs, group: true}
`, jobYAML, func(mock sqlmock.Sqlmock) {
		mock.ExpectQuery(`^SELECT g\.value, g\.count, NULL::text, g\.v0, g\.v1, g\.v2 FROM \(SELECT e\.properties -> 'stage' AS value, count\(\*\) AS count, `+
			`\(100\.0 \* count\(\*\) FILTER \(WHERE NOT coalesce\(\(e\.properties @> \$1::jsonb\), false\) AND \(e\.properties @> \$2::jsonb\)\) / nullif\(count\(\*\) FILTER \(WHERE NOT coalesce`+
			`.*ORDER BY g\.value IS NULL DESC, g\.v0 DESC NULLS LAST, g\.count DESC, g\.value\s+LIMIT \$6$`).
			WithArgs(`{"status":"skipped"}`, `{"status":"failed"}`, `{"status":"skipped"}`, `{"status":"skipped"}`, `{"status":"failed"}`, 9).
			WillReturnRows(sqlmock.NewRows([]string{"value", "count", "title", "v0", "v1", "v2"}).
				AddRow(nil, 1, nil, 100, 1, 1).
				AddRow([]byte(`"test"`), 4, nil, 50, 4, 2).
				AddRow([]byte(`"build"`), 5, nil, 20, 5, 1))
		mock.ExpectQuery(`^SELECT g\.value, g\.count, NULL::text FROM .*ORDER BY g\.value IS NULL DESC, array_position\(\$1::text\[\], g\.value #>> '\{\}'\) ASC NULLS FIRST, g\.value ASC\s+LIMIT \$2$`).
			WithArgs(sqlmock.AnyArg(), 9).
			WillReturnRows(sqlmock.NewRows([]string{"value", "count", "title"}).
				AddRow([]byte(`"build"`), 5, nil).AddRow([]byte(`"test"`), 4, nil))
	})
	wantIn(t, "failing", got["failing"], `{"data":{"groups":[{"value":"test","count":4,"measure":{"value":50,"count":2,"total":4}},`+
		`{"value":"build","count":5,"measure":{"value":20,"count":1,"total":5}}],"none":1,"noneMeasure":{"value":100,"count":1,"total":1},"unit":"%"}}`)
	wantIn(t, "stages", got["stages"], `"groups":[{"value":"build","count":5,"link":{"blueprint":"job","filter":{"property":"stage","op":"eq","value":"build"}}},{"value":"test"`)
}

// A date's newest shows as its age, which the database works out, so the
// groups sort by the ages they show, without one sorting lowest.
func TestBarsShowDatesAsTheirAge(t *testing.T) {
	got := loadMocked(t, `
kind: Dashboard
metadata: {identifier: ci, title: CI}
spec:
  queries:
    jobs: {blueprint: job}
  widgets:
    stale:
      type: bar
      title: Since Each Stage Last Finished
      query: jobs
      groupBy: stage
      measure: {max: finishedAt}
    stages:
      type: matrix
      title: Stages
      query: jobs
      groupBy: stage
      sort: Last
      measures:
        - {title: Last, max: finishedAt}
        - {title: Statuses, distinct: status}
`, jobYAML, func(mock sqlmock.Sqlmock) {
		last := `\(extract\(epoch FROM now\(\) - max\(gp_time\(\(e\.properties -> 'finishedAt' #>> '\{\}'\)\)\)\)\)::float8`
		mock.ExpectQuery(`^SELECT g\.value, g\.count, NULL::text, g\.v0 FROM \(SELECT e\.properties -> 'stage' AS value, count\(\*\) AS count, ` + last +
			`.*ORDER BY g\.value IS NULL DESC, g\.v0 DESC NULLS LAST`).
			WillReturnRows(sqlmock.NewRows([]string{"value", "count", "title", "v0"}).
				AddRow([]byte(`"deploy"`), 2, nil, 3*86400).
				AddRow([]byte(`"build"`), 5, nil, 3600).
				AddRow([]byte(`"test"`), 1, nil, nil))
		mock.ExpectQuery(`^SELECT g\.value, g\.count, NULL::text, g\.v0, g\.v1 FROM .*` + last + ` AS v0, \(count\(DISTINCT nullif\(e\.properties -> 'status', 'null'::jsonb\)\)\)::float8 AS v1` +
			`.*ORDER BY g\.value IS NULL DESC, g\.v0 ASC NULLS FIRST`).
			WillReturnRows(sqlmock.NewRows([]string{"value", "count", "title", "v0", "v1"}).
				AddRow([]byte(`"build"`), 5, nil, 3600, 2))
	})
	wantIn(t, "stale", got["stale"], `"groups":[{"value":"deploy","count":2,"measure":{"value":259200}},{"value":"build","count":5,"measure":{"value":3600}},{"value":"test","count":1,"measure":{"value":null}}]`, `"unit":"s"`)
	wantIn(t, "stages", got["stages"], `"measures":[{"title":"Last","unit":"s"},{"title":"Statuses"}]`, `"cells":[{"value":3600,`, `{"value":2,`)
}

func TestTrendsFillEveryDayOfTheirRange(t *testing.T) {
	got := loadMocked(t, `
kind: Dashboard
metadata: {identifier: ci, title: CI}
spec:
  queries:
    jobs: {blueprint: job}
  widgets:
    perDay:
      type: trend
      title: Jobs per Day
      query: jobs
      date: finishedAt
      range: 3d
      link: {query: jobs, group: true}
    p95:
      type: trend
      title: P95 Duration per Day
      query: jobs
      date: finishedAt
      range: 3d
      measure: {p95: duration}
      link: {query: jobs}
`, jobYAML, func(mock sqlmock.Sqlmock) {
		day := `\(gp_time\(\(e\.properties -> 'finishedAt' #>> '\{\}'\)\) AT TIME ZONE 'UTC'\)::date`
		from, to := time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
		mock.ExpectQuery(`^SELECT `+day+`, count\(\*\)\s+FROM .* >= \$1 AND .* < \$2\s+GROUP BY GROUPING SETS \(\(`+day+`\), \(\)\)$`).
			WithArgs(from, to).
			WillReturnRows(sqlmock.NewRows([]string{"day", "count"}).
				AddRow(time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC), 2).
				AddRow(time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC), 5).
				AddRow(nil, 7))
		mock.ExpectQuery(`^SELECT `+day+`, count\(\*\), \(percentile_cont\(0\.95\) WITHIN GROUP`).
			WithArgs(from, to).
			WillReturnRows(sqlmock.NewRows([]string{"day", "count", "p95"}).
				AddRow(time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC), 5, 300).
				AddRow(nil, 5, 300))
	})
	wantIn(t, "perDay", got["perDay"],
		`{"data":{"all":{"value":7},"days":[{"day":"2026-11-07","value":2,"link":{"blueprint":"job","filter":{"and":[{"property":"finishedAt","op":"gte","value":"2026-11-07T00:00:00Z"},{"property":"finishedAt","op":"lt","value":"2026-11-08T00:00:00Z"}]}}},`+
			`{"day":"2026-11-08","value":0},{"day":"2026-11-09","value":5,"link":`)
	wantIn(t, "p95", got["p95"],
		`{"data":{"unit":"s","all":{"value":300},"days":[{"day":"2026-11-07","value":null},{"day":"2026-11-08","value":null},{"day":"2026-11-09","value":300}],"link":{"blueprint":"job"}}}`)
}

// A trend can count each day's distinct values, which don't add up across
// days, so the range's count is the statement's own.
func TestTrendsCountDistinctValues(t *testing.T) {
	got := loadMocked(t, `
kind: Dashboard
metadata: {identifier: ci, title: CI}
spec:
  queries:
    jobs: {blueprint: job}
  widgets:
    stages:
      type: trend
      title: Stages Run per Day
      query: jobs
      date: finishedAt
      range: 2d
      measure: {distinct: stage}
`, jobYAML, func(mock sqlmock.Sqlmock) {
		from, to := time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
		mock.ExpectQuery(`, count\(\*\), \(count\(DISTINCT nullif\(e\.properties -> 'stage', 'null'::jsonb\)\)\)::float8\s+FROM .*GROUP BY GROUPING SETS`).
			WithArgs(from, to).
			WillReturnRows(sqlmock.NewRows([]string{"day", "count", "stages"}).
				AddRow(time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC), 4, 2).
				AddRow(time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC), 6, 3).
				AddRow(nil, 10, 3))
	})
	wantIn(t, "stages", got["stages"], `{"data":{"all":{"value":3},"days":[{"day":"2026-11-08","value":2},{"day":"2026-11-09","value":3}]}}`)
}
