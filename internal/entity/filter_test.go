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
    critical: {type: boolean, compute: .properties.tier == "critical"}
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
			`((e.properties -> 'tier' = $1::jsonb) OR (e.properties -> 'tier' = $2::jsonb))`,
			[]any{`"critical"`, `"standard"`},
		},
		{
			"many relation", `{"property":"dependsOn","op":"eq","value":"ledger"}`,
			`(e.relations -> 'dependsOn' @> $1::jsonb)`,
			[]any{`["ledger"]`},
		},
		{
			"single relation", `{"property":"owner","op":"eq","value":"payments"}`,
			`(e.relations -> 'owner' = $1::jsonb)`,
			[]any{`"payments"`},
		},
		{
			"computed", `{"property":"critical","op":"eq","value":true}`,
			`(e.computed -> 'critical' = $1::jsonb)`,
			[]any{`true`},
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
			"title contains", `{"property":"$title","op":"contains","value":"50%"}`,
			`(e.title ILIKE $1)`,
			[]any{`%50\%%`},
		},
		{
			"tag contains", `{"property":"tags","op":"contains","value":"pci"}`,
			`(e.properties -> 'tags' @> $1::jsonb)`,
			[]any{`["pci"]`},
		},
		{
			"not exists", `{"not":{"property":"owner","op":"exists"}}`,
			`NOT coalesce((e.relations -> 'owner' IS NOT NULL AND e.relations -> 'owner' <> 'null'::jsonb), false)`, nil,
		},
		{
			"and or", `{"and":[{"property":"tier","op":"eq","value":"critical"},{"or":[{"property":"owner","op":"eq","value":"a"},{"property":"owner","op":"eq","value":"b"}]}]}`,
			`((e.properties -> 'tier' = $1::jsonb) AND ((e.relations -> 'owner' = $2::jsonb) OR (e.relations -> 'owner' = $3::jsonb)))`,
			[]any{`"critical"`, `"a"`, `"b"`},
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
		{"lookup", `{"property":"ownerSlack","op":"eq","value":"x"}`, "can't be filtered"},
		{"unknown op", `{"property":"tier","op":"like","value":"x"}`, `unknown op "like"`},
		{"compare text", `{"property":"tier","op":"gt","value":"x"}`, "only compares numbers and dates"},
		{"empty in", `{"property":"tier","op":"in"}`, "needs values"},
		{"null eq", `{"property":"tier","op":"eq"}`, "use exists"},
		{"too deep", `{"not":{"not":{"not":{"not":{"not":{"property":"tier","op":"exists"}}}}}}`, "nest at most"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f, err := ParseFilter(test.filter)
			if err != nil {
				t.Fatal(err)
			}
			c := &compiler{spec: service(t)}
			if _, err := c.compile(*f, 1); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("err = %v, want %q", err, test.want)
			}
		})
	}
}

func TestOrderBy(t *testing.T) {
	spec := service(t)
	if got, _ := orderBy(spec, "-stars"); got != "e.properties -> 'stars' DESC NULLS LAST, e.title, e.identifier" {
		t.Errorf("orderBy = %s", got)
	}
	if got, _ := orderBy(spec, "$updatedAt"); got != "e.updated_at ASC NULLS LAST, e.title, e.identifier" {
		t.Errorf("orderBy = %s", got)
	}
	if _, err := orderBy(spec, "tags"); err == nil {
		t.Error("sorting by a list should fail")
	}
}
