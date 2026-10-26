package entity

import (
	"database/sql/driver"
	"strings"
	"testing"
)

func TestScorecardFilters(t *testing.T) {
	bp := service(t)
	for _, op := range []string{OpEq, OpIn, OpExists, OpNe} {
		f := Filter{Property: "$scorecard.readiness", Op: op, Value: "gold"}
		if op == OpIn {
			f.Value = nil
			f.Values = []any{"gold", ""}
		}
		c := compiler{spec: bp}
		sql, err := c.compile(f, 1)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(sql, "scorecard_results") || !strings.Contains(sql, "r.entity_id = e.id") || !strings.Contains(sql, "s.identifier = 'readiness'") {
			t.Fatalf("tier filter must correlate with each entity: %s", sql)
		}
		if op == OpExists && !strings.Contains(sql, "IS NOT NULL") {
			t.Fatal("unscored must differ from no tier")
		}
		if op == OpIn && (len(c.args) != 2 || c.args[1] != "") {
			t.Fatalf("no-tier value lost: %+v", c.args)
		}
	}
	for _, value := range []string{"$scorecard.", "$scorecard.bad.name", "$scorecard.x' OR true --"} {
		if err := CheckColumn(bp, value); err == nil {
			t.Errorf("accepted %s", value)
		}
	}
	if err := CheckColumn(bp, "$scorecard.readiness"); err != nil {
		t.Fatal(err)
	}
}

func TestScorecardSort(t *testing.T) {
	for _, direction := range []string{"", "-"} {
		c := &compiler{spec: service(t), args: []any{"service"}}
		sql, err := scorecardOrder("$scorecard.readiness", direction+"$scorecard.readiness", []string{"", "bronze", "silver", "gold"}, c)
		if err != nil {
			t.Fatal(err)
		}
		order := "ASC"
		if direction == "-" {
			order = "DESC"
		}
		if !strings.Contains(sql, "array_position($2::text[]") || !strings.Contains(sql, order+" NULLS LAST, e.title, e.identifier") {
			t.Fatalf("wrong ladder ordering: %s", sql)
		}
		value, err := c.args[1].(interface{ Value() (driver.Value, error) }).Value()
		if err != nil || value != `{"","bronze","silver","gold"}` {
			t.Fatalf("ladder argument: %v %v", value, err)
		}
	}
}
