//go:build e2e

package entity

import (
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/filter"
)

// jobsYAML is CI job runs, kept by when they finished, the shape the
// percentiles, measures per group and trends are for.
const jobsYAML = `kind: Blueprint
metadata: {identifier: service, title: Job}
spec:
  properties:
    stage: {type: string, enum: [build, test, deploy]}
    status: {type: string}
    duration: {type: number, display: {unit: s}}
    finishedAt: {type: string, format: date-time}
  retention: {by: finishedAt, keep: 90d}
`

// Volumes like a busy instance's: a month of job runs in range, and two
// older months that a short range must skip by the index.
const (
	recentJobs = 60000
	olderJobs  = 120000
)

var stages = []string{"build", "test", "deploy"}

// job is what the SQL below inserts for n, worked out again in Go.
type job struct {
	stage, status string
	duration      float64
	day           time.Time
}

func jobFor(n int, today time.Time) job {
	j := job{stage: stages[n%3], status: "success", duration: float64(n * 7919 % 3600)}
	switch n % 10 {
	case 0:
		j.status = "failed"
	case 1:
		j.status = "skipped"
	}
	j.day = today.AddDate(0, 0, -(n % 30))
	return j
}

// insertJobs writes jobs straight to the table, as an ingest would leave
// them, from first to last, back days before today.
func (f *storeFixture) insertJobs(first, last, back int, today time.Time) {
	f.t.Helper()
	if _, err := f.db.ExecContext(f.t.Context(), `
		INSERT INTO entities (blueprint_id, identifier, title, connection_id, properties, occurred_at)
		SELECT $1, 'j' || n, 'Job ' || n, $2, jsonb_build_object(
				'stage', (ARRAY['build', 'test', 'deploy'])[n % 3 + 1],
				'status', CASE n % 10 WHEN 0 THEN 'failed' WHEN 1 THEN 'skipped' ELSE 'success' END,
				'duration', n * 7919 % 3600,
				'finishedAt', to_char(at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')),
			at
		FROM generate_series($3::int, $4::int) n,
			LATERAL (SELECT $5::timestamptz - ($6::int + n % 30) * interval '1 day' + (n % 3600) * interval '1 second' AS at) t`,
		f.blueprint, f.mine, first, last, today, back); err != nil {
		f.t.Fatal(err)
	}
}

// percentileCont is Postgres's percentile_cont: interpolated between the
// two values either side of where the fraction falls.
func percentileCont(values []float64, fraction float64) float64 {
	sorted := slices.Sorted(slices.Values(values))
	at := fraction * float64(len(sorted)-1)
	lo, hi := math.Floor(at), math.Ceil(at)
	return sorted[int(lo)] + (at-lo)*(sorted[int(hi)]-sorted[int(lo)])
}

func near(a *float64, b float64) bool { return a != nil && math.Abs(*a-b) < 1e-6 }

func timed(t *testing.T, name string, fn func() error) {
	t.Helper()
	start := time.Now()
	if err := fn(); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	t.Logf("%s took %s", name, time.Since(start).Round(time.Millisecond))
}

// TestMeasuresAtVolume checks exact percentiles, a measure per group sorted
// by it, and a trend per day against what Go works out from the same rows,
// at a busy instance's volume, and that a trend over a short range reads
// only its days through the retention index.
func TestMeasuresAtVolume(t *testing.T) {
	f := newStoreFixture(t)
	f.publishBlueprint(jobsYAML)
	today := time.Now().UTC().Truncate(24 * time.Hour)
	f.insertJobs(1, recentJobs, 0, today)
	f.insertJobs(recentJobs+1, recentJobs+olderJobs, 30, today)
	if _, err := f.db.ExecContext(t.Context(), `ANALYZE entities`); err != nil {
		t.Fatal(err)
	}
	// Published specs carry their row's ID, which statements name.
	specs, err := blueprint.Published(t.Context(), f.db)
	if err != nil {
		t.Fatal(err)
	}
	spec := specs["service"]
	if spec.ID != f.blueprint {
		t.Fatalf("spec ID = %q, want %q", spec.ID, f.blueprint)
	}
	from, to := today.AddDate(0, 0, -29), today.AddDate(0, 0, 1)
	recent := &filter.Filter{And: []filter.Filter{
		{Property: "finishedAt", Op: filter.OpGte, Value: from.Format(time.RFC3339)},
	}}
	m, err := NewMatch(Query{Blueprint: spec, Blueprints: specs, Filter: recent})
	if err != nil {
		t.Fatal(err)
	}

	var all []float64
	byStage := map[string][]float64{}
	failed, ran := map[string]int{}, map[string]int{}
	for n := 1; n <= recentJobs; n++ {
		j := jobFor(n, today)
		all = append(all, j.duration)
		byStage[j.stage] = append(byStage[j.stage], j.duration)
		if j.status != "skipped" {
			ran[j.stage]++
			if j.status == "failed" {
				failed[j.stage]++
			}
		}
	}

	var values []*float64
	timed(t, "percentiles", func() (err error) {
		values, err = f.store.Aggregates(t.Context(), m, []Aggregate{
			{Op: "count"}, {Op: "p50", Path: "duration"}, {Op: "p95", Path: "duration"}, {Op: "p99", Path: "duration"},
		})
		return err
	})
	if !near(values[0], recentJobs) || !near(values[1], percentileCont(all, 0.5)) ||
		!near(values[2], percentileCont(all, 0.95)) || !near(values[3], percentileCont(all, 0.99)) {
		t.Fatalf("count, p50, p95, p99 = %v %v %v %v, want %d %v %v %v", *values[0], *values[1], *values[2], *values[3],
			recentJobs, percentileCont(all, 0.5), percentileCont(all, 0.95), percentileCont(all, 0.99))
	}

	notSkipped := &filter.Filter{Not: &filter.Filter{Property: "status", Op: filter.OpEq, Value: "skipped"}}
	isFailed := &filter.Filter{Property: "status", Op: filter.OpEq, Value: "failed"}
	var groups []Measured
	timed(t, "failure share and p95 by stage", func() (err error) {
		groups, _, err = f.store.GroupBy(t.Context(), m, "stage", []Aggregate{
			{Op: "share", Filter: notSkipped, Share: isFailed}, {Op: "p95", Path: "duration"},
		}, GroupSort{By: 0, Descending: true}, 10)
		return err
	})
	if len(groups) != 3 {
		t.Fatalf("groups = %+v", groups)
	}
	for i, g := range groups {
		stage := g.Value.(string)
		share := 100 * float64(failed[stage]) / float64(ran[stage])
		if !near(g.Values[0], share) || !near(g.Values[1], percentileCont(byStage[stage], 0.95)) {
			t.Errorf("%s = %v %v, want %v %v", stage, *g.Values[0], *g.Values[1], share, percentileCont(byStage[stage], 0.95))
		}
		if i > 0 && *groups[i-1].Values[0] < *g.Values[0] {
			t.Errorf("groups aren't by share, most first: %+v", groups)
		}
	}

	unfiltered, err := NewMatch(Query{Blueprint: spec, Blueprints: specs})
	if err != nil {
		t.Fatal(err)
	}
	var days []Day
	var total Day
	timed(t, "30 days of count, p95 and failure share", func() (err error) {
		days, total, err = f.store.Trend(t.Context(), unfiltered, "finishedAt", from, to, []Aggregate{
			{Op: "p95", Path: "duration"}, {Op: "share", Filter: notSkipped, Share: isFailed},
		})
		return err
	})
	if len(days) != 30 || total.Count != recentJobs || !near(total.Values[0], percentileCont(all, 0.95)) {
		t.Fatalf("days %d, total %+v", len(days), total)
	}
	perDay := map[time.Time][]float64{}
	for n := 1; n <= recentJobs; n++ {
		j := jobFor(n, today)
		perDay[j.day] = append(perDay[j.day], j.duration)
	}
	for _, d := range days {
		if d.Count != len(perDay[d.Day]) || !near(d.Values[0], percentileCont(perDay[d.Day], 0.95)) {
			t.Errorf("%s = %d, %v; want %d, %v", d.Day.Format(time.DateOnly), d.Count, *d.Values[0], len(perDay[d.Day]), percentileCont(perDay[d.Day], 0.95))
		}
	}

	timed(t, "7 days of p95", func() error {
		_, _, err := f.store.Trend(t.Context(), unfiltered, "finishedAt", today.AddDate(0, 0, -6), to, []Aggregate{{Op: "p95", Path: "duration"}})
		return err
	})
	empty, none, err := f.store.Trend(t.Context(), unfiltered, "finishedAt", to, to.AddDate(0, 0, 1), []Aggregate{{Op: "p95", Path: "duration"}})
	if err != nil || len(empty) != 0 || none.Count != 0 || none.Values[0] != nil {
		t.Fatalf("a range without entities = %+v, %+v, %v", empty, none, err)
	}

	c := unfiltered.compiler()
	fld, _ := trendField(c, "finishedAt")
	var plan []string
	rows, err := f.db.QueryContext(t.Context(), `EXPLAIN SELECT count(*) FROM entities e WHERE `+unfiltered.where+
		` AND `+fld.time()+` >= `+c.arg(today.AddDate(0, 0, -6))+` AND `+fld.time()+` < `+c.arg(to), c.args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, line)
	}
	t.Logf("7 day trend plan:\n%s", strings.Join(plan, "\n"))
	if !strings.Contains(strings.Join(plan, "\n"), "entities_occurred_idx") {
		t.Errorf("a 7 day trend doesn't read the retention index")
	}
}
