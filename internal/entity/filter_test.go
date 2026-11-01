package entity

import (
	"strings"
	"testing"

	"github.com/dvjn/goldenportal/internal/blueprint"
)

const serviceYAML = `
kind: Blueprint
metadata:
  name: service
  title: Service
spec:
  properties:
    tier: {type: string, enum: [critical, standard]}
    stars: {type: integer}
    tags: {type: array, items: {type: string}}
    lastDeployedAt: {type: string, format: date-time}
    owner: {relation: team}
    dependsOn: {relation: service, many: true}
    ownerSlack: {lookup: owner.slackChannel}
    critical: {type: boolean, compute: .tier == "critical"}
    readiness: {type: scorecard, scorecard: service-readiness}
`

func service(t *testing.T) blueprint.Spec {
	t.Helper()
	spec, issues := blueprint.Parse(serviceYAML)
	if blueprint.HasErrors(issues) {
		t.Fatalf("issues = %+v", issues)
	}

	return spec
}

func TestCompileFilter(t *testing.T) {
	tests := []struct {
		name, filter, sql string
		args              []any
	}{
		{
			"enum in", `{"property":"tier","op":"in","values":["critical","standard"]}`,
			`((e.properties @> $1::jsonb) OR (e.properties @> $2::jsonb))`,
			[]any{`{"tier":"critical"}`, `{"tier":"standard"}`},
		},
		{
			"many relation", `{"property":"dependsOn","op":"eq","value":"ledger"}`,
			`(e.relations @> $1::jsonb)`,
			[]any{`{"dependsOn":["ledger"]}`},
		},
		{
			"single relation", `{"property":"owner","op":"eq","value":"payments"}`,
			`(e.relations @> $1::jsonb)`,
			[]any{`{"owner":"payments"}`},
		},
		{
			"computed", `{"property":"critical","op":"eq","value":true}`,
			`(e.computed @> $1::jsonb)`,
			[]any{`{"critical":true}`},
		},
		{
			"number", `{"property":"stars","op":"gte","value":10}`,
			`((e.properties -> 'stars')::numeric >= $1)`,
			[]any{float64(10)},
		},
		{
			"date", `{"property":"lastDeployedAt","op":"lt","value":"2026-10-01T00:00:00Z"}`,
			`(((e.properties -> 'lastDeployedAt' #>> '{}'))::timestamptz < $1::timestamptz)`,
			[]any{"2026-10-01T00:00:00Z"},
		},
		{
			"title contains", `{"property":"title","op":"contains","value":"50%"}`,
			`(e.title ILIKE $1)`,
			[]any{`%50\%%`},
		},
		{
			"tag contains", `{"property":"tags","op":"contains","value":"pci"}`,
			`(e.properties @> $1::jsonb)`,
			[]any{`{"tags":["pci"]}`},
		},
		{
			"many relation contains", `{"property":"dependsOn","op":"contains","value":"ledger"}`,
			`(e.relations @> $1::jsonb)`,
			[]any{`{"dependsOn":["ledger"]}`},
		},
		{"in no values", `{"property":"tier","op":"in","values":[]}`, `false`, nil},
		{
			"within", `{"property":"lastDeployedAt","op":"within","value":"7d"}`,
			`(((e.properties -> 'lastDeployedAt' #>> '{}'))::timestamptz >= now() - $1::interval)`,
			[]any{"604800 seconds"},
		},
		{"within a built-in", `{"property":"updatedAt","op":"within","value":"12h"}`, `(e.updated_at >= now() - $1::interval)`, []any{"43200 seconds"}},
		{"integration", `{"property":"integration","op":"eq","value":"registry"}`, `(i.identifier = $1)`, []any{"registry"}},
		{
			"not exists", `{"not":{"property":"owner","op":"exists"}}`,
			`NOT coalesce((e.relations -> 'owner' IS NOT NULL AND e.relations -> 'owner' <> 'null'::jsonb), false)`, nil,
		},
		{
			"and or", `{"and":[{"property":"tier","op":"eq","value":"critical"},{"or":[{"property":"owner","op":"eq","value":"a"},{"property":"owner","op":"eq","value":"b"}]}]}`,
			`((e.properties @> $1::jsonb) AND ((e.relations @> $2::jsonb) OR (e.relations @> $3::jsonb)))`,
			[]any{`{"tier":"critical"}`, `{"owner":"a"}`, `{"owner":"b"}`},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f, err := ParseFilter(test.filter)
			if err != nil {
				t.Fatal(err)
			}
			c := &compiler{spec: service(t)}
			sql, err := c.compile(*f, 1)
			if err != nil {
				t.Fatal(err)
			}
			if sql != test.sql {
				t.Errorf("sql =\n%s\nwant\n%s", sql, test.sql)
			}
			if len(c.args) != len(test.args) {
				t.Fatalf("args = %#v, want %#v", c.args, test.args)
			}
			for i := range test.args {
				if c.args[i] != test.args[i] {
					t.Errorf("arg %d = %#v, want %#v", i, c.args[i], test.args[i])
				}
			}
		})
	}
}

func TestCompileFilterRejects(t *testing.T) {
	tests := []struct{ name, filter, want string }{
		{"unknown property", `{"property":"nope","op":"exists"}`, `no property "nope"`},
		{"lookup without its blueprint", `{"property":"ownerSlack","op":"eq","value":"x"}`, "to team, which isn't published"},
		{"follow a non-relation", `{"property":"tier.name","op":"eq","value":"x"}`, "tier, which isn't a relation"},
		{"follow too far", `{"property":"dependsOn.dependsOn.dependsOn.dependsOn.tier","op":"eq","value":"x"}`, "more than 3 relations"},
		{"unknown op", `{"property":"tier","op":"like","value":"x"}`, `unknown op "like"`},
		{"compare text", `{"property":"tier","op":"gt","value":"x"}`, "only compares numbers and dates"},
		{"empty in", `{"property":"tier","op":"in"}`, "needs values"},
		{"in with value", `{"property":"tier","op":"in","value":"critical"}`, "takes values"},
		{"unbound values", `{"property":"tier","op":"in","values":"$input.tier"}`, "isn't set here"},
		{"old built-in", `{"property":"$title","op":"exists"}`, "built-in fields are written plain, as title"},
		{"old scorecard", `{"property":"$scorecard.readiness","op":"exists"}`, "declare a scorecard property"},
		{"within needs a duration", `{"property":"lastDeployedAt","op":"within","value":"604800 seconds"}`, "such as 30m, 12h, 7d or 2w"},
		{"within needs a date", `{"property":"stars","op":"within","value":"7d"}`, "only compares dates"},
		{"scorecard result field", `{"property":"readiness.tier","op":"exists"}`, "isn't on a scorecard result"},
		{"null eq", `{"property":"tier","op":"eq"}`, "use exists"},
		{"too deep", `{"not":{"not":{"not":{"not":{"not":{"property":"tier","op":"exists"}}}}}}`, "nest at most"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f, err := ParseFilter(test.filter)
			if err != nil {
				t.Fatal(err)
			}
			c := &compiler{spec: service(t), specs: map[string]blueprint.Spec{"service": service(t)}}
			if _, err := c.compile(*f, 1); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("err = %v, want %q", err, test.want)
			}
		})
	}
}

func TestCompileFollowsRelations(t *testing.T) {
	team := blueprint.Spec{Identifier: "team", Properties: []blueprint.Property{{ID: "slackChannel", Type: "string"}}}
	specs := map[string]blueprint.Spec{"service": service(t), "team": team}
	deployment := blueprint.Spec{Identifier: "deployment", Properties: []blueprint.Property{
		{ID: "service", Type: "relation", Relation: "service"},
		{ID: "ownerSlack", Lookup: "service.ownerSlack"},
	}}
	subquery := func(relation, arg, condition string) string {
		return "((e.relations ->> '" + relation + "') IN (SELECT e.identifier" + entityFrom +
			" WHERE b.kind = 'blueprint' AND b.identifier = " + arg + " AND " + condition + "))"
	}
	tests := []struct {
		name, property, sql string
		args                []any
	}{
		{"one hop", "service.owner", subquery("service", "$2", "(e.relations @> $1::jsonb)"), []any{`{"owner":"payments"}`, "service"}},
		{"two hops", "service.owner.slackChannel", subquery("service", "$3", subquery("owner", "$2", "(e.properties @> $1::jsonb)")), []any{`{"slackChannel":"payments"}`, "team", "service"}},
		{"through a lookup", "ownerSlack", subquery("service", "$3", subquery("owner", "$2", "(e.properties @> $1::jsonb)")), []any{`{"slackChannel":"payments"}`, "team", "service"}},
		{
			"through many", "service.dependsOn.owner",
			subquery("service", "$3", "EXISTS (SELECT 1 FROM jsonb_array_elements_text(CASE WHEN jsonb_typeof(e.relations -> 'dependsOn') = 'array' THEN e.relations -> 'dependsOn' END) target(identifier) WHERE target.identifier IN (SELECT e.identifier"+entityFrom+
				" WHERE b.kind = 'blueprint' AND b.identifier = $2 AND (e.relations @> $1::jsonb)))"),
			[]any{`{"owner":"payments"}`, "service", "service"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c := &compiler{spec: deployment, specs: specs}
			sql, err := c.compile(Filter{Property: test.property, Op: OpEq, Value: "payments"}, 1)
			if err != nil {
				t.Fatal(err)
			}
			if sql != test.sql {
				t.Errorf("sql =\n%s\nwant\n%s", sql, test.sql)
			}
			if len(c.args) != len(test.args) {
				t.Fatalf("args = %#v, want %#v", c.args, test.args)
			}
			for i := range test.args {
				if c.args[i] != test.args[i] {
					t.Errorf("arg %d = %#v, want %#v", i, c.args[i], test.args[i])
				}
			}
		})
	}
}

func TestOrderBy(t *testing.T) {
	c := &compiler{spec: service(t)}
	if got, _ := c.orderBy("-stars", nil); got != "e.properties -> 'stars' DESC NULLS LAST, e.title, e.identifier" {
		t.Errorf("orderBy = %s", got)
	}
	if got, _ := c.orderBy("updatedAt", nil); got != "e.updated_at ASC NULLS LAST, e.title, e.identifier" {
		t.Errorf("orderBy = %s", got)
	}
	if _, err := c.orderBy("tags", nil); err == nil {
		t.Error("sorting by a list should fail")
	}
	if _, err := c.orderBy("$updatedAt", nil); err == nil || !strings.Contains(err.Error(), "as updatedAt") {
		t.Errorf("err = %v, want the plain spelling", err)
	}
}
