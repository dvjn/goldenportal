package entity

import (
	"database/sql/driver"
	"fmt"
	"strings"
	"testing"

	"github.com/dvjn/goldenportal/internal/blueprint"
)

const readinessID = `(SELECT id FROM config_documents WHERE kind = 'scorecard' AND identifier = 'service-readiness' AND published_version IS NOT NULL)`

// resultsWhere is a semi-join on the entity's results meeting condition.
func resultsWhere(condition string) string {
	return `(e.id IN (SELECT sr.entity_id FROM scorecard_results sr WHERE sr.scorecard_id = ` + readinessID + ` AND ` + condition + `))`
}

func compileFilter(t *testing.T, c *compiler, filter string) string {
	t.Helper()
	f, err := ParseFilter(filter)
	if err != nil {
		t.Fatal(err)
	}
	sql, err := c.compile(*f, 1)
	if err != nil {
		t.Fatal(err)
	}

	return sql
}

func TestScorecardFilters(t *testing.T) {
	tests := []struct {
		name, filter, sql string
		args              []any
	}{
		{"tier", `{"property":"readiness","op":"eq","value":"gold"}`, resultsWhere("sr.tier = ANY($1::text[])"), []any{`{"gold"}`}},
		{
			"evaluated with no tier", `{"property":"readiness","op":"in","values":["","gold"]}`,
			resultsWhere("sr.tier = ANY($1::text[])"),
			[]any{`{"","gold"}`},
		},
		{"not a tier", `{"property":"readiness","op":"ne","value":"gold"}`, "NOT " + resultsWhere("sr.tier = ANY($1::text[])"), []any{`{"gold"}`}},
		{"any tier", `{"property":"readiness","op":"exists"}`, resultsWhere("sr.tier <> ''"), nil},
		{"score", `{"property":"readiness.score","op":"gte","value":50}`, resultsWhere("((sr.score)::numeric >= $1)"), []any{float64(50)}},
		{"failed", `{"property":"readiness.failed","op":"eq","value":0}`, resultsWhere("((sr.failed)::numeric = $1)"), []any{float64(0)}},
		{"a check", `{"property":"readiness.checks.hasOwner","op":"eq","value":"failed"}`, resultsWhere(`(sr.criteria @> $1::jsonb)`), []any{`{"hasOwner":{"status":"failed"}}`}},
		{
			"a check's statuses", `{"property":"readiness.checks.hasOwner","op":"in","values":["skipped","failed"]}`,
			resultsWhere(`(sr.criteria @> $1::jsonb OR sr.criteria @> $2::jsonb)`),
			[]any{`{"hasOwner":{"status":"failed"}}`, `{"hasOwner":{"status":"skipped"}}`},
		},
		{
			"a check run, as statuses the GIN on criteria answers", `{"property":"readiness.checks.hasOwner","op":"exists"}`,
			resultsWhere(`(sr.criteria @> $1::jsonb OR sr.criteria @> $2::jsonb OR sr.criteria @> $3::jsonb)`),
			[]any{`{"hasOwner":{"status":"passed"}}`, `{"hasOwner":{"status":"failed"}}`, `{"hasOwner":{"status":"skipped"}}`},
		},
		{"failing", `{"property":"readiness.failing","op":"contains","value":"onCall"}`, resultsWhere(`(sr.criteria @> $1::jsonb)`), []any{`{"onCall":{"status":"failed"}}`}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c := &compiler{spec: service(t)}
			sql := compileFilter(t, c, test.filter)
			if sql != test.sql {
				t.Errorf("sql =\n%s\nwant\n%s", sql, test.sql)
			}
			if len(c.args) != len(test.args) {
				t.Fatalf("args = %#v, want %#v", c.args, test.args)
			}
			for i, arg := range c.args {
				if got, want := argText(arg), argText(test.args[i]); got != want {
					t.Errorf("args[%d] = %s, want %s", i, got, want)
				}
			}
		})
	}
}

// argText is an argument as the database gets it.
func argText(arg any) string {
	if valuer, ok := arg.(driver.Valuer); ok {
		arg, _ = valuer.Value()
	}

	return fmt.Sprint(arg)
}

func TestReservedPathsReadAsScorecardProperties(t *testing.T) {
	// A property names the scorecard on the blueprint; _scorecards reaches
	// it either way, as the same SQL.
	pairs := []struct {
		paths   [2]string
		filters []string
	}{
		{[2]string{"readiness", "_scorecards.service-readiness"}, []string{
			`{"property":"%s","op":"in","values":["","gold"]}`, `{"property":"%s","op":"exists"}`, `{"property":"%s","op":"ne","value":"gold"}`,
		}},
		{[2]string{"readiness.score", "_scorecards.service-readiness.score"}, []string{
			`{"property":"%s","op":"gte","value":50}`, `{"not":{"property":"%s","op":"exists"}}`,
		}},
		{[2]string{"readiness.failing", "_scorecards.service-readiness.failing"}, []string{
			`{"property":"%s","op":"contains","value":"onCall"}`,
		}},
		{[2]string{"readiness.checks.hasOwner", "_scorecards.service-readiness.checks.hasOwner"}, []string{
			`{"property":"%s","op":"in","values":["failed","skipped"]}`, `{"not":{"property":"%s","op":"eq","value":"failed"}}`,
		}},
	}
	for _, p := range pairs {
		pair := p.paths
		for _, filter := range p.filters {
			var sqls [2]string
			for i, path := range pair {
				sqls[i] = compileFilter(t, &compiler{spec: service(t)}, strings.Replace(filter, "%s", path, 1))
			}
			if sqls[0] != sqls[1] {
				t.Errorf("%s filters as\n%s\nbut %s as\n%s", pair[0], sqls[0], pair[1], sqls[1])
			}
		}
		var fields [2]field
		for i, path := range pair {
			fld, err := fieldFor(Schema{Blueprint: service(t), Blueprints: nil}, false, path)
			if err != nil {
				t.Fatal(err)
			}
			fields[i] = fld
		}
		if fields[0].value != fields[1].value || fields[0].rank != fields[1].rank || fields[0].kind != fields[1].kind {
			t.Errorf("%s reads %s, but %s reads %s", pair[0], fields[0].value, pair[1], fields[1].value)
		}
	}
	for _, sort := range []string{"readiness", "-readiness.score"} {
		a, errA := (&compiler{spec: service(t)}).orderBy(sort)
		b, errB := (&compiler{spec: service(t)}).orderBy(strings.Replace(sort, "readiness", "_scorecards.service-readiness", 1))
		if errA != nil || errB != nil || a != b {
			t.Errorf("sort %s: %s (%v) and %s (%v)", sort, a, errA, b, errB)
		}
	}
}

func TestReservedPathsTitle(t *testing.T) {
	ladders := map[string]blueprint.Ladder{
		"service-readiness": {Title: "Service Readiness", Blueprint: "service"},
		"security":          {Title: "Security", Blueprint: "service"},
	}
	for path, want := range map[string]string{
		"_scorecards.service-readiness":       "readiness",
		"_scorecards.security":                "Security",
		"_scorecards.security.checks.hasSast": "Security › hasSast",
	} {
		info, err := Describe(Schema{Blueprint: service(t), Ladders: ladders}, path)
		if err != nil || info.Title != want {
			t.Errorf("%s: title %q, %v, want %q", path, info.Title, err, want)
		}
	}
}

func TestReservedPathsRejected(t *testing.T) {
	ladders := map[string]blueprint.Ladder{
		"service-readiness": {Blueprint: "service", Tiers: []blueprint.Rung{{Name: "gold", Criteria: map[string]any{"onCall": nil}}}},
		"team-health":       {Blueprint: "team"},
	}
	deployment := blueprint.Spec{Identifier: "deployment", Properties: []blueprint.Property{{ID: "service", Type: "relation", Relation: "service"}}}
	specs := map[string]blueprint.Spec{"service": service(t), "team": {Identifier: "team"}, "deployment": deployment}
	tests := []struct {
		on           blueprint.Spec
		filter, want string
	}{
		{service(t), `{"property":"_scorecards","op":"exists"}`, "_scorecards names a scorecard after it, as in _scorecards.<scorecard>"},
		{service(t), `{"property":"_scorecards.nope","op":"exists"}`, "_scorecards.nope names no published scorecard; service is graded by service-readiness"},
		{service(t), `{"property":"_scorecards.team-health","op":"exists"}`, "team-health grades team, not service; service is graded by service-readiness"},
		{deployment, `{"property":"_scorecards.service-readiness","op":"exists"}`, "service-readiness grades service, not deployment; no published scorecard grades deployment"},
		{service(t), `{"property":"_scorecards.service-readiness","op":"eq","value":"silver"}`, `_scorecards.service-readiness is a tier of service-readiness: gold, or "" for evaluated with no tier, not "silver"`},
		{service(t), `{"property":"_scorecards.service-readiness.checks.hasDocs","op":"eq","value":"failed"}`, `service-readiness has no check "hasDocs"; its checks are onCall`},
		{service(t), `{"property":"_scorecards.service-readiness.grade","op":"exists"}`, "_scorecards.service-readiness.grade isn't on a scorecard result"},
		// Through a relation, the path reads the scorecards of the blueprint it ends on.
		{deployment, `{"property":"service._scorecards.team-health","op":"exists"}`, "team-health grades team, not service"},
		{deployment, `{"property":"service._scorecards.service-readiness","op":"eq","value":"gold"}`, ""},
	}
	for _, test := range tests {
		f, err := ParseFilter(test.filter)
		if err != nil {
			t.Fatal(err)
		}
		err = CheckFilter(Schema{Blueprint: test.on, Blueprints: specs, Ladders: ladders}, *f)
		if test.want == "" && err != nil || test.want != "" && (err == nil || !strings.Contains(err.Error(), test.want)) {
			t.Errorf("%s: err = %v, want %q", test.filter, err, test.want)
		}
	}
	schema := Schema{Blueprint: service(t), Blueprints: specs, Ladders: ladders}
	for _, check := range []func() error{
		func() error { return CheckSort(schema, "-_scorecards.nope") },
		func() error { return CheckColumn(schema, "_scorecards.nope.score") },
		func() error { return CheckGroup(schema, "_scorecards.nope") },
		func() error { return CheckAggregate(schema, Aggregate{Op: "avg", Path: "_scorecards.nope.score"}) },
	} {
		if err := check(); err == nil || !strings.Contains(err.Error(), "names no published scorecard") {
			t.Errorf("err = %v, want an unknown scorecard", err)
		}
	}
	// Checks are checked wherever a path names one, through a property too.
	for _, check := range []func() error{
		func() error { return CheckSort(schema, "readiness.checks.hasDocs") },
		func() error { return CheckColumn(schema, "_scorecards.service-readiness.checks.hasDocs") },
		func() error { return CheckGroup(schema, "readiness.checks.hasDocs") },
	} {
		if err := check(); err == nil || !strings.Contains(err.Error(), `no check "hasDocs"`) {
			t.Errorf("err = %v, want an unknown check", err)
		}
	}
	// A property naming a scorecard that's being deleted, or grades another
	// blueprint, can't be read either; one not yet published can.
	for _, test := range []struct {
		ladder blueprint.Ladder
		want   string
	}{
		{blueprint.Ladder{Deleted: true}, "readiness reads scorecard service-readiness, which is being deleted"},
		{blueprint.Ladder{Blueprint: "team"}, "service-readiness grades team, not service"},
	} {
		ladder, want := test.ladder, test.want
		changed := Schema{Blueprint: service(t), Blueprints: specs, Ladders: map[string]blueprint.Ladder{"service-readiness": ladder}}
		if err := CheckColumn(changed, "readiness"); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want %q", err, want)
		}
	}
	if err := CheckColumn(Schema{Blueprint: service(t), Ladders: map[string]blueprint.Ladder{}}, "readiness.score"); err != nil {
		t.Errorf("a scorecard not yet published: %v", err)
	}
}

func TestScorecardHops(t *testing.T) {
	deployment := blueprint.Spec{Identifier: "deployment", Properties: []blueprint.Property{{ID: "service", Type: "relation", Relation: "service"}}}
	specs := map[string]blueprint.Spec{"service": service(t), "team": {Identifier: "team"}}
	for _, path := range []string{"service.readiness.failed", "service._scorecards.service-readiness.failed"} {
		fld, err := fieldFor(Schema{Blueprint: deployment, Blueprints: specs}, false, path)
		if err != nil {
			t.Fatal(err)
		}
		// The scorecard reads the service the deployment relates to, not the deployment.
		if !strings.HasPrefix(fld.value, "(SELECT (SELECT r.failed FROM scorecard_results r WHERE r.entity_id = h1.id") ||
			!strings.Contains(fld.value, "h1.identifier = e.relations ->> 'service'") || fld.kind != "integer" || fld.result != nil {
			t.Fatalf("%s: field = %+v", path, fld)
		}
	}
	tier, err := fieldFor(Schema{Blueprint: deployment, Blueprints: specs}, false, "service._scorecards.service-readiness")
	if err != nil || tier.owner != "service.identifier" || !strings.HasPrefix(tier.rank, "(SELECT (SELECT r.tier_rank FROM") {
		t.Fatalf("tier = %+v, %v", tier, err)
	}
	// A scorecard counts as a hop toward the limit.
	if _, err := fieldFor(Schema{Blueprint: service(t), Blueprints: specs}, false, "owner.title"); err != nil {
		t.Fatal(err)
	}
	deep := blueprint.Spec{Identifier: "deep", Properties: []blueprint.Property{{ID: "d", Type: "relation", Relation: "deployment"}}}
	specs["deployment"], specs["deep"] = deployment, deep
	chain := blueprint.Spec{Identifier: "chain", Properties: []blueprint.Property{{ID: "c", Type: "relation", Relation: "deep"}}}
	for _, path := range []string{"c.d.service.readiness.failed", "c.d.service._scorecards.service-readiness.failed"} {
		if _, err := fieldFor(Schema{Blueprint: chain, Blueprints: specs}, false, path); err == nil || !strings.Contains(err.Error(), "more than 3") {
			t.Fatalf("%s: err = %v, want the hop limit", path, err)
		}
	}
	if _, err := fieldFor(Schema{Blueprint: deep, Blueprints: specs}, false, "d.service.readiness.failed"); err != nil {
		t.Fatalf("two relations and a scorecard: %v", err)
	}
}

func TestScorecardSort(t *testing.T) {
	for _, direction := range []string{"", "-"} {
		c := &compiler{spec: service(t)}
		sql, err := c.orderBy(direction + "readiness")
		if err != nil {
			t.Fatal(err)
		}
		order := "ASC NULLS FIRST"
		if direction == "-" {
			order = "DESC NULLS LAST"
		}
		// Not evaluated sorts below no tier, by the rank stored with each result.
		if sql != "sr1.tier_rank "+order+", e.title, e.identifier" {
			t.Fatalf("sql = %s", sql)
		}
		if from := c.from(); from != " FROM entities e\n\tLEFT JOIN scorecard_results sr1 ON sr1.scorecard_id = "+readinessID+" AND sr1.entity_id = e.id" {
			t.Fatalf("from = %s", from)
		}
	}
	// Through a relation, it's looked up per row.
	team := blueprint.Spec{Identifier: "team", Properties: []blueprint.Property{{ID: "lead", Type: "relation", Relation: "service"}}}
	c := &compiler{spec: team, specs: map[string]blueprint.Spec{"service": service(t)}}
	sql, err := c.orderBy("lead._scorecards.service-readiness")
	if err != nil || !strings.HasPrefix(sql, "(SELECT (SELECT r.tier_rank FROM scorecard_results r WHERE r.entity_id = h1.id") || c.from() != " FROM entities e" {
		t.Fatalf("sql = %s, %v", sql, err)
	}
}
