package scorecard

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/entity"
	"github.com/dvjn/goldenportal/internal/httpx"
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
          check: .owner != null
          fix: Set the owner.
    - name: silver
      criteria:
        hasRepository:
          check: .repository != null
        pagesOnCall:
          when: .tier == "critical"
          check: .oncall != null
    - name: gold
      criteria:
        manyStars:
          check: (.stars | tonumber) > 100
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

	head := "kind: Scorecard\nmetadata: {identifier: s, title: S}\nspec:\n  blueprint: service\n"
	tests := []struct{ name, source, want string }{
		{"no tiers", head, "add the tiers"},
		{"empty tier", head + "  tiers:\n    - name: a\n", "add the criteria"},
		{"no check", head + "  tiers:\n    - name: a\n      criteria:\n        c: {title: C}\n", "add a jq check"},
		{"bad jq", head + "  tiers:\n    - name: a\n      criteria:\n        c: {check: '.a |'}\n", "not valid jq"},
		{"repeated criterion", head + "  tiers:\n    - name: a\n      criteria:\n        c: {check: 'true'}\n    - name: b\n      criteria:\n        c: {check: 'true'}\n", "named across tiers"},
		{"repeated tier", head + "  tiers:\n    - name: a\n      criteria:\n        c: {check: 'true'}\n    - name: a\n      criteria:\n        d: {check: 'true'}\n", "already named \"a\""},
		{"bad color", head + "  tiers:\n    - name: a\n      color: gold\n      criteria:\n        c: {check: 'true'}\n", "color must be one of"},
		{"bad filter", head + "  filter: [1]\n  tiers:\n    - name: a\n      criteria:\n        c: {check: 'true'}\n", "filter is not valid JSON"},
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

func TestEvaluate(t *testing.T) {
	spec := mustParse(t, readinessYAML)
	checks, err := compileSpec(spec)
	if err != nil {
		t.Fatal(err)
	}
	evaluate := func(properties map[string]any) Result {
		return checks.evaluate(context.Background(), spec, properties)
	}

	tests := []struct {
		name       string
		properties map[string]any
		tier       string
		score      int
		status     map[string]string
	}{
		{
			"nothing",
			map[string]any{},
			"", 0,
			map[string]string{"hasOwner": StatusFailed, "pagesOnCall": StatusSkipped},
		},
		{"bronze only", map[string]any{"owner": "payments"}, "bronze", 33, nil},
		{
			"silver, on-call skipped",
			map[string]any{"owner": "a", "repository": "r", "stars": 5.0},
			"silver", 66,
			map[string]string{"manyStars": StatusFailed},
		},
		{
			"silver needs on-call when critical",
			map[string]any{"owner": "a", "repository": "r", "tier": "critical", "stars": 500.0},
			"bronze", 75,
			map[string]string{"pagesOnCall": StatusFailed},
		},
		{"gold", map[string]any{"owner": "a", "repository": "r", "stars": 500.0}, "gold", 100, nil},
		{"a failed lower tier caps the tier", map[string]any{"repository": "r", "stars": 500.0}, "", 66, nil},
		{
			"an error blocks its tier",
			map[string]any{"owner": "a", "repository": "r", "stars": "many"},
			"silver", 66,
			map[string]string{"manyStars": StatusErrored},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := evaluate(test.properties)
			if r.Tier != test.tier {
				t.Errorf("tier = %q, want %q (%+v)", r.Tier, test.tier, r.Criteria)
			}
			if r.Score == nil || *r.Score != test.score {
				t.Errorf("score = %v, want %d", r.Score, test.score)
			}
			for id, status := range test.status {
				if got := r.Criteria[id].Status; got != status {
					t.Errorf("%s = %s, want %s", id, got, status)
				}
			}
		})
	}
}

func TestChecksMustBeBoolean(t *testing.T) {
	spec := mustParse(t, strings.Replace(readinessYAML, "check: .owner != null", "check: .owner", 1))
	checks, _ := compileSpec(spec)
	r := checks.evaluate(context.Background(), spec, map[string]any{"properties": map[string]any{"owner": "a"}})
	if got := r.Criteria["hasOwner"]; got.Status != StatusErrored || !strings.Contains(got.Message, "true or false") {
		t.Fatalf("hasOwner = %+v", got)
	}
}

func TestDiff(t *testing.T) {
	before := mustParse(t, readinessYAML)
	if changes := Diff(before, before); len(changes) != 0 {
		t.Fatalf("no change: %+v", changes)
	}
	after := mustParse(t, strings.Replace(readinessYAML, "> 100", "> 10", 1))
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
	spec := Spec{Tiers: []Tier{{Name: "bronze"}, {Name: "silver"}}}
	// The filter's values follow the query's own, before the paging.
	mock.ExpectQuery(regexp.QuoteMeta(`'hasOwner'`)).
		WithArgs("sc", "", pq.Array([]string{"", "bronze", "silver"}), "failed", "skipped", 10, 0).
		WillReturnRows(sqlmock.NewRows([]string{"b", "e", "t", "changed", "at", "tier", "score", "p", "f", "s", "x", "c", "total"}).
			AddRow("service", "ledger", "Ledger", time.Time{}, time.Time{}, "silver", 80, 4, 1, 0, 0, []byte(`{}`), 1))

	scope := Scope{Blueprints: map[string]blueprint.Spec{"service": graded}, Blueprint: graded, Filter: f}
	page, err := results(t.Context(), db, "sc", spec, ResultQuery{Scope: scope, Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || page.Items[0].Identifier != "ledger" {
		t.Errorf("page = %+v", page)
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
	if q.Search != "led" || q.Sort != "-tier" || q.Page != 1 || q.PageSize != 10 {
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
		{"", "tier ASC, r.score ASC NULLS FIRST, e.title, e.identifier"},
		{"-tier", "tier DESC, r.score DESC NULLS LAST, e.title, e.identifier"},
		{"score", "r.score ASC NULLS FIRST, tier ASC, e.title, e.identifier"},
		{"-failed", "r.failed DESC, tier DESC, r.score DESC NULLS LAST, e.title, e.identifier"},
		{"skipped", "r.skipped ASC, tier ASC, r.score ASC NULLS FIRST, e.title, e.identifier"},
	}
	for _, test := range tests {
		if got := resultOrder(test.sort, "tier"); got != test.want {
			t.Errorf("resultOrder(%q) = %q, want %q", test.sort, got, test.want)
		}
	}
}
