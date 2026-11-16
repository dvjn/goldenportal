package scorecard

import (
	"errors"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/entity"
	"github.com/dvjn/goldenportal/internal/httpx"
	"github.com/dvjn/goldenportal/internal/keyset"
)

const readinessYAML = `
kind: Scorecard
metadata:
  identifier: readiness
  title: Readiness
spec:
  blueprint: service
  filter: {property: tier, op: ne, value: experimental}
  tiers:
    - name: bronze
      title: Bronze
      color: amber
      criteria:
        hasOwner:
          title: Has an owner
          check: {property: owner, op: exists}
          fix: Set the owner.
    - name: silver
      criteria:
        hasRepository:
          check: {property: repository, op: exists}
        pagesOnCall:
          when: {property: tier, op: eq, value: critical}
          check: {property: oncall, op: exists}
    - name: gold
      criteria:
        manyStars:
          check: {property: stars, op: gt, value: 100}
`

func mustParse(t *testing.T, source string) Spec {
	t.Helper()
	spec, issues := Parse(source)
	if configdoc.HasErrors(issues) {
		t.Fatalf("Parse() issues = %+v", issues)
	}

	return spec
}

func TestParse(t *testing.T) {
	spec := mustParse(t, readinessYAML)
	if spec.Blueprint != "service" || len(spec.Tiers) != 3 || spec.Tiers[1].Title != "silver" || spec.Filter == nil {
		t.Fatalf("spec = %+v", spec)
	}
	if got := spec.Stats(); got["criteria"] != 4 {
		t.Errorf("stats = %v", got)
	}
	if got := spec.Facts(); got["blueprint"] != "service" {
		t.Errorf("facts = %v", got)
	}

	if c := spec.Tiers[1].Criteria[1]; c.When == nil || c.When.Property != "tier" || c.Check.Op != "exists" {
		t.Errorf("pagesOnCall = %+v", c)
	}

	head := "kind: Scorecard\nmetadata: {identifier: s, title: S}\nspec:\n  blueprint: service\n"
	check := "{check: {property: owner, op: exists}}"
	tests := []struct{ name, source, want string }{
		{"no tiers", head, "add the tiers"},
		{"empty tier", head + "  tiers:\n    - name: a\n", "add the criteria"},
		{"no check", head + "  tiers:\n    - name: a\n      criteria:\n        c: {title: C}\n", "add a check, a filter such as {property: hasReadme, op: eq, value: true}"},
		{"check a list", head + "  tiers:\n    - name: a\n      criteria:\n        c: {check: [1]}\n", "a check is a filter, a mapping such as {property: hasReadme, op: eq, value: true}"},
		{"check true", head + "  tiers:\n    - name: a\n      criteria:\n        c: {check: true}\n", "a check is a filter, a mapping such as"},
		{"empty check", head + "  tiers:\n    - name: a\n      criteria:\n        c: {check: {}}\n", "a check is a filter and needs a condition, such as"},
		{"empty when", head + "  tiers:\n    - name: a\n      criteria:\n        c: {when: {}, check: {property: a, op: exists}}\n", "when is a filter and needs a condition"},
		{"bad filter", head + "  tiers:\n    - name: a\n      criteria:\n        c: {check: {values: 1}}\n", "values must be a list"},
		{"repeated criterion", head + "  tiers:\n    - name: a\n      criteria:\n        c: " + check + "\n    - name: b\n      criteria:\n        c: " + check + "\n", "named across tiers"},
		{"repeated tier", head + "  tiers:\n    - name: a\n      criteria:\n        c: " + check + "\n    - name: a\n      criteria:\n        d: " + check + "\n", "already named \"a\""},
		{"bad color", head + "  tiers:\n    - name: a\n      color: gold\n      criteria:\n        c: " + check + "\n", "color must be one of"},
		{"bad scope", head + "  filter: [1]\n  tiers:\n    - name: a\n      criteria:\n        c: " + check + "\n", "filter is not valid JSON"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, issues := Parse(test.source)
			var messages []string
			for _, issue := range issues {
				messages = append(messages, issue.Message)
			}
			if !strings.Contains(strings.Join(messages, "\n"), test.want) {
				t.Fatalf("issues = %v, want %q", messages, test.want)
			}
		})
	}
}

// TestParseRejectsJQ checks a check or when written as jq, as scorecards
// once were, points to filters and to a computed property on the blueprint.
func TestParseRejectsJQ(t *testing.T) {
	tests := []struct{ name, from, to, path, want string }{
		{
			"check", "check: {property: owner, op: exists}", "check: .owner != null",
			"spec.tiers[0].criteria.hasOwner.check",
			"a check is a filter, as the catalog writes them, such as {property: hasReadme, op: eq, value: true}; " +
				"jq over one entity belongs in a computed property on service, which the check reads",
		},
		{
			"when", "when: {property: tier, op: eq, value: critical}", `when: .tier == "critical"`,
			"spec.tiers[1].criteria.pagesOnCall.when",
			"when is a filter, as the catalog writes them, such as {property: hasReadme, op: eq, value: true}; " +
				"jq over one entity belongs in a computed property on service, which when reads",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, issues := Parse(strings.Replace(readinessYAML, test.from, test.to, 1))
			if len(issues) != 1 || issues[0].Path != test.path || issues[0].Message != test.want || issues[0].Line == 0 {
				t.Fatalf("issues = %+v, want one at %s saying %q", issues, test.path, test.want)
			}
		})
	}
}

func TestDiff(t *testing.T) {
	before := mustParse(t, readinessYAML)
	if changes := Diff(before, before); len(changes) != 0 {
		t.Fatalf("no change: %+v", changes)
	}
	after := mustParse(t, strings.Replace(readinessYAML, "value: 100", "value: 10", 1))
	changes := Diff(before, after)
	if len(changes) != 1 || changes[0].ID != "manyStars" || changes[0].Breaking {
		t.Fatalf("changes = %+v", changes)
	}
}

func TestResultsNarrowByFilter(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	graded, issues := blueprint.Parse("kind: Blueprint\nmetadata: {identifier: service, title: Service}\n" +
		"spec:\n  properties:\n    readiness: {type: scorecard, scorecard: sc}\n")
	if blueprint.HasErrors(issues) {
		t.Fatal(issues)
	}
	f, err := entity.ParseFilter(`{"property": "readiness.checks.hasOwner", "op": "in", "values": ["failed", "skipped"]}`)
	if err != nil {
		t.Fatal(err)
	}
	spec := Spec{Blueprint: "service", Tiers: []Tier{{Name: "bronze"}, {Name: "silver"}}}
	// The filter's values follow the query's own, before the rest.
	hasOwner := func(status string) string { return `{"hasOwner":{"status":"` + status + `"}}` }
	columns := []string{"b", "e", "t", "changed", "at", "tier", "score", "p", "f", "s", "c", "k0", "k1", "k2", "k3"}
	result := func(rows *sqlmock.Rows, identifier string) *sqlmock.Rows {
		return rows.AddRow("service", identifier, "Ledger", time.Time{}, time.Time{}, "silver", 80, 4, 1, 0, []byte(`{}`),
			"2", "80", `"Ledger"`, `"`+identifier+`"`)
	}
	mock.ExpectQuery(regexp.QuoteMeta(`(sr.criteria @> $3::jsonb OR sr.criteria @> $4::jsonb)`)+`.*`+regexp.QuoteMeta(`ORDER BY r.tier_rank, r.score ASC NULLS FIRST, e.title, e.identifier`)).
		WithArgs("sc", "%led%", hasOwner("failed"), hasOwner("skipped"), "service", 2).
		WillReturnRows(result(result(sqlmock.NewRows(columns), "ledger"), "ledgers"))
	mock.ExpectQuery(`^SELECT count\(\*\)\s+FROM scorecard_results r\s+JOIN entities e ON e.id = r.entity_id\s+WHERE r.scorecard_id = \$1\s+`+
		regexp.QuoteMeta(`AND ($2 = '' OR lower(e.title) LIKE lower($2) OR lower(e.identifier) LIKE lower($2))`)+`.*sr.criteria @>`).
		WithArgs("sc", "%led%", hasOwner("failed"), hasOwner("skipped")).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))

	scope := Scope{Schema: entity.Schema{Blueprints: map[string]blueprint.Spec{"service": graded}, Blueprint: graded}, Filter: f}
	page, err := results(t.Context(), db, "sc", spec, ResultQuery{Scope: scope, Search: "led", PageSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total == nil || *page.Total != 3 || len(page.Items) != 1 || page.Items[0].Identifier != "ledger" || page.Next == "" || page.Previous != "" {
		t.Errorf("page = %+v", page)
	}

	// The next page reads after the last row's values, and doesn't count.
	mock.ExpectQuery(regexp.QuoteMeta(`AND ((r.tier_rank = $5::numeric AND r.score = $6::numeric AND (e.title, e.identifier) > ($7::text, $8::text)) OR `+
		`(r.tier_rank = $5::numeric AND r.score > $6::numeric) OR (r.tier_rank > $5::numeric))`)).
		WithArgs("sc", "%led%", hasOwner("failed"), hasOwner("skipped"), "2", "80", "Ledger", "ledger", "service", 2).
		WillReturnRows(result(sqlmock.NewRows(columns), "ledgers"))
	next, err := results(t.Context(), db, "sc", spec, ResultQuery{Scope: scope, Search: "led", PageSize: 1, Cursor: page.Next})
	if err != nil {
		t.Fatal(err)
	}
	if next.Total != nil || len(next.Items) != 1 || next.Items[0].Identifier != "ledgers" || next.Next != "" || next.Previous == "" {
		t.Errorf("next page = %+v", next)
	}
	// A cursor pages only the results it came from.
	if _, err := results(t.Context(), db, "sc", spec, ResultQuery{Scope: scope, Search: "le", PageSize: 1, Cursor: page.Next}); !errors.Is(err, keyset.ErrCursor) {
		t.Errorf("another search: err = %v, want ErrCursor", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestResultsRequestChecksSortAndPaging(t *testing.T) {
	q, err := ResultsRequest{Search: "led", Sort: "-tier"}.query()
	if err != nil {
		t.Fatal(err)
	}
	if q.Search != "led" || q.Sort != "-tier" || q.Cursor != "" || q.PageSize != 10 {
		t.Errorf("query = %+v", q)
	}
	for _, sort := range []string{"", "tier", "score", "-score", "passed", "-failed", "skipped"} {
		if _, err := (ResultsRequest{Sort: sort}).query(); err != nil {
			t.Errorf("sort %q: %v", sort, err)
		}
	}

	tests := []struct {
		name string
		req  ResultsRequest
		want string
	}{
		{"unknown sort", ResultsRequest{Sort: "title"}, `sort must be tier, score, passed, failed or skipped, with a leading - for descending, not "title"`},
		{"doubled minus", ResultsRequest{Sort: "--tier"}, "sort must be"},
		{"bad size", ResultsRequest{Size: 7}, "size must be"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := test.req.query()
			var httpErr httpx.Error
			if !errors.As(err, &httpErr) || httpErr.Code != http.StatusBadRequest ||
				!strings.Contains(err.Error(), test.want) {
				t.Fatalf("err = %v, want a 400 saying %q", err, test.want)
			}
		})
	}
}

func TestResultOrder(t *testing.T) {
	tests := []struct{ sort, want string }{
		{"", "r.tier_rank, r.score ASC NULLS FIRST, e.title, e.identifier"},
		{"-tier", "r.tier_rank DESC, r.score DESC NULLS LAST, e.title, e.identifier"},
		{"score", "r.score ASC NULLS FIRST, r.tier_rank, e.title, e.identifier"},
		{"-failed", "r.failed DESC, r.tier_rank DESC, r.score DESC NULLS LAST, e.title, e.identifier"},
		{"skipped", "r.skipped, r.tier_rank, r.score ASC NULLS FIRST, e.title, e.identifier"},
	}
	for _, test := range tests {
		if got := resultOrder(test.sort).SQL(); got != test.want {
			t.Errorf("resultOrder(%q) = %q, want %q", test.sort, got, test.want)
		}
	}
}

// TestNoErroredStatus checks filters can't ask for an errored check or count,
// since a check that can't run fails its publish instead.
func TestNoErroredStatus(t *testing.T) {
	graded, issues := blueprint.Parse("kind: Blueprint\nmetadata: {identifier: service, title: Service}\n" +
		"spec:\n  properties:\n    readiness: {type: scorecard, scorecard: readiness}\n")
	if blueprint.HasErrors(issues) {
		t.Fatal(issues)
	}
	specs := map[string]blueprint.Spec{"service": graded}
	ladders := Ladders(map[string]Spec{"readiness": mustParse(t, readinessYAML)})
	for raw, want := range map[string]string{
		`{"property": "readiness.checks.hasOwner", "op": "eq", "value": "errored"}`: "passed, failed, skipped, not \"errored\"",
		`{"property": "readiness.errored", "op": "gt", "value": 0}`:                 "readiness.errored isn't on a scorecard result",
	} {
		f, err := entity.ParseFilter(raw)
		if err != nil {
			t.Fatal(err)
		}
		if err := entity.CheckFilter(entity.Schema{Blueprint: graded, Blueprints: specs, Ladders: ladders}, *f); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want %q", raw, err, want)
		}
	}
}
