package entity

import (
	"database/sql/driver"
	"strings"
	"testing"

	"github.com/dvjn/goldenportal/internal/blueprint"
)

func TestScorecardFilters(t *testing.T) {
	result := `(SELECT r.tier FROM scorecard_results r WHERE r.entity_id = e.id AND r.scorecard_id = (SELECT id FROM config_documents WHERE kind = 'scorecard' AND identifier = 'service-readiness' AND published_version IS NOT NULL))`
	tests := []struct {
		name, filter, sql string
		args              []any
	}{
		{"tier", `{"property":"readiness","op":"eq","value":"gold"}`, "(coalesce(" + result + ", '') = $1)", []any{"gold"}},
		{"no tier, evaluated or not", `{"property":"readiness","op":"in","values":[""]}`, "((coalesce(" + result + ", '') = $1))", []any{""}},
		{"any tier", `{"property":"readiness","op":"exists"}`, "(coalesce(" + result + ", '') <> '')", nil},
		{"score", `{"property":"readiness.score","op":"gte","value":50}`, "((" + strings.Replace(result, "r.tier", "r.score", 1) + ")::numeric >= $1)", []any{float64(50)}},
		{"failed", `{"property":"readiness.failed","op":"eq","value":0}`, "((" + strings.Replace(result, "r.tier", "r.failed", 1) + ")::numeric = $1)", []any{float64(0)}},
		{"a check", `{"property":"readiness.checks.hasOwner","op":"eq","value":"failed"}`, "(" + strings.Replace(result, "r.tier", "r.criteria -> 'hasOwner' ->> 'status'", 1) + " = $1)", []any{"failed"}},
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
			if len(c.args) != len(test.args) || (len(test.args) > 0 && c.args[0] != test.args[0]) {
				t.Errorf("args = %#v, want %#v", c.args, test.args)
			}
		})
	}
}

func TestScorecardHops(t *testing.T) {
	deployment := blueprint.Spec{Identifier: "deployment", Properties: []blueprint.Property{{ID: "service", Type: "relation", Relation: "service"}}}
	specs := map[string]blueprint.Spec{"service": service(t), "team": {Identifier: "team"}}
	fld, err := fieldFor(specs, deployment, "service.readiness.failed")
	if err != nil {
		t.Fatal(err)
	}
	// The scorecard reads the service the deployment relates to, not the deployment.
	if !strings.HasPrefix(fld.value, "(SELECT (SELECT r.failed FROM scorecard_results r WHERE r.entity_id = h1.id") ||
		!strings.Contains(fld.value, "h1.identifier = e.relations ->> 'service'") || fld.kind != "integer" {
		t.Fatalf("field = %+v", fld)
	}
	// A scorecard counts as a hop toward the limit.
	if _, err := fieldFor(specs, service(t), "owner.title"); err != nil {
		t.Fatal(err)
	}
	deep := blueprint.Spec{Identifier: "deep", Properties: []blueprint.Property{{ID: "d", Type: "relation", Relation: "deployment"}}}
	specs["deployment"], specs["deep"] = deployment, deep
	chain := blueprint.Spec{Identifier: "chain", Properties: []blueprint.Property{{ID: "c", Type: "relation", Relation: "deep"}}}
	if _, err := fieldFor(specs, chain, "c.d.service.readiness.failed"); err == nil || !strings.Contains(err.Error(), "more than 3") {
		t.Fatalf("err = %v, want the hop limit", err)
	}
	if _, err := fieldFor(specs, deep, "d.service.readiness.failed"); err != nil {
		t.Fatalf("two relations and a scorecard: %v", err)
	}
}

func TestScorecardSort(t *testing.T) {
	ladders := map[string]blueprint.Ladder{"service-readiness": {Blueprint: "service", Tiers: []blueprint.Rung{{Name: "bronze"}, {Name: "silver"}, {Name: "gold"}}}}
	for _, direction := range []string{"", "-"} {
		c := &compiler{spec: service(t), args: []any{"service"}}
		sql, err := c.orderBy(direction+"readiness", ladders)
		if err != nil {
			t.Fatal(err)
		}
		order := "ASC"
		if direction == "-" {
			order = "DESC"
		}
		if !strings.HasPrefix(sql, "array_position($2::text[], coalesce((SELECT r.tier") || !strings.HasSuffix(sql, order+" NULLS LAST, e.title, e.identifier") {
			t.Fatalf("wrong ladder ordering: %s", sql)
		}
		value, err := c.args[1].(interface{ Value() (driver.Value, error) }).Value()
		if err != nil || value != `{"","bronze","silver","gold"}` {
			t.Fatalf("ladder argument: %v %v", value, err)
		}
	}
	// Without its ladder, a tier sorts by name.
	c := &compiler{spec: service(t)}
	if sql, _ := c.orderBy("readiness", nil); !strings.HasPrefix(sql, "(SELECT r.tier") {
		t.Fatalf("sql = %s", sql)
	}
}
