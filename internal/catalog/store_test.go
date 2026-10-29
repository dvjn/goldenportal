package catalog

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/dvjn/goldport/internal/audit"
	"github.com/dvjn/goldport/internal/definition"
	"github.com/lib/pq"
	"gofr.dev/pkg/gofr/container"
)

const (
	teamYAML = `kind: Blueprint
identifier: team
title: Team
properties:
  email:
    title: Email
    type: string
    format: email
`
	serviceYAML = `kind: Blueprint
identifier: service
title: Service
icon: server
properties:
  tier:
    title: Tier
    type: string
    enum:
      - "1"
      - "2"
relations:
  owner:
    title: Owner
    target: team
`
)

var at = time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)

func newStore(t *testing.T) (*Store, sqlmock.Sqlmock) {
	t.Helper()
	c, mocks := container.NewMockContainer(t)

	return NewStore(c.SQL), mocks.SQL
}

func setOf(t *testing.T, documents ...string) definition.Set {
	t.Helper()
	source, problems := definition.Parse([]byte(strings.Join(documents, "---\n")))
	if len(problems) > 0 {
		t.Fatalf("Parse() = %v", problems)
	}

	return definition.NewSet(source.Documents...)
}

func nameRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"blueprint", "identifier", "title", "count"})
}

func TestApplyReplacesTheDefinitionsAndRecordsTheRevision(t *testing.T) {
	store, db := newStore(t)
	from := setOf(t, teamYAML, serviceYAML)
	to := setOf(t, teamYAML)
	changes, _ := definition.Diff(from, to)
	plan := Plan{Base: 3, Desired: to, Changes: changes, Effects: definition.EffectsOf(from, to)}
	alice := audit.Actor{Kind: audit.ActorUser, ID: "u-1", Name: "Alice", Email: "alice@example.test"}

	db.ExpectBegin()
	db.ExpectExec(queryLockRevisions).WillReturnResult(sqlmock.NewResult(0, 0))
	db.ExpectQuery(queryRevisionNumber).WillReturnRows(sqlmock.NewRows([]string{"number"}).AddRow(3))
	db.ExpectExec(queryClearDefs).WillReturnResult(sqlmock.NewResult(0, 2))
	db.ExpectExec(queryInsertDef).WithArgs(definition.KindBlueprint, "team", sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
	db.ExpectQuery(queryBlueprintEntities).WithArgs("service", impactExamples).
		WillReturnRows(nameRows().AddRow("service", "payments-api", "Payments API", 2).AddRow("service", "ledger", "Ledger", 2))
	db.ExpectExec(queryDeleteBlueprintEntities).WithArgs("service").WillReturnResult(sqlmock.NewResult(0, 2))
	db.ExpectQuery(queryInsertRevision).WithArgs(4, "u-1", "alice@example.test", "drop services", 0, sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"created_at"}).AddRow(at))
	db.ExpectCommit()

	revision, err := store.Apply(context.Background(), plan, alice, "drop services")
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	want := []EntityGroup{{Blueprint: "service", Count: 2, Examples: []EntityName{
		{Blueprint: "service", Identifier: "payments-api", Title: "Payments API"},
		{Blueprint: "service", Identifier: "ledger", Title: "Ledger"},
	}}}
	if revision.Number != 4 || !revision.CreatedAt.Equal(at) || !reflect.DeepEqual(revision.Impact.DeletedEntities, want) {
		t.Fatalf("Apply() = %+v, want revision 4 that deleted both services", revision)
	}
	if err := db.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestApplyRemovesAnIntegrationsEntitiesTokensAndRuns(t *testing.T) {
	store, db := newStore(t)
	ci := "kind: Integration\nidentifier: ci\ntitle: CI\nconnector: push\n"
	from := setOf(t, teamYAML, ci)
	to := setOf(t, teamYAML)
	changes, _ := definition.Diff(from, to)
	plan := Plan{Base: 1, Desired: to, Changes: changes, Effects: definition.EffectsOf(from, to)}

	db.ExpectBegin()
	db.ExpectExec(queryLockRevisions).WillReturnResult(sqlmock.NewResult(0, 0))
	db.ExpectQuery(queryRevisionNumber).WillReturnRows(sqlmock.NewRows([]string{"number"}).AddRow(1))
	db.ExpectExec(queryClearDefs).WillReturnResult(sqlmock.NewResult(0, 2))
	db.ExpectExec(queryInsertDef).WithArgs(definition.KindBlueprint, "team", sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
	db.ExpectQuery(queryIntegrationEntities).WithArgs("ci", "[]", impactExamples).WillReturnRows(nameRows())
	db.ExpectExec(queryDeleteIntegrationEntities).WithArgs("ci").WillReturnResult(sqlmock.NewResult(0, 0))
	db.ExpectExec(queryRevokeIngestTokens).WithArgs("ci").WillReturnResult(sqlmock.NewResult(0, 1))
	db.ExpectExec(queryDeleteSyncRuns).WithArgs("ci").WillReturnResult(sqlmock.NewResult(0, 3))
	db.ExpectQuery(queryInsertRevision).WillReturnRows(sqlmock.NewRows([]string{"created_at"}).AddRow(at))
	db.ExpectCommit()

	if _, err := store.Apply(context.Background(), plan, audit.Actor{Kind: audit.ActorUser, Email: "alice@example.test"}, ""); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if err := db.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestApplyRefusesAPlanMadeOnAnotherRevision(t *testing.T) {
	store, db := newStore(t)
	db.ExpectBegin()
	db.ExpectExec(queryLockRevisions).WillReturnResult(sqlmock.NewResult(0, 0))
	db.ExpectQuery(queryRevisionNumber).WillReturnRows(sqlmock.NewRows([]string{"number"}).AddRow(4))
	db.ExpectRollback()

	_, err := store.Apply(context.Background(), Plan{Base: 3}, audit.Actor{Kind: audit.ActorServiceToken, ID: "t-1", Name: "ci"}, "")
	if !errors.Is(err, ErrStale) {
		t.Fatalf("Apply() error = %v, want ErrStale", err)
	}
	if err := db.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestApplyClearsOnlyTheValuesAChangedPropertyRefuses(t *testing.T) {
	store, db := newStore(t)
	from := setOf(t, teamYAML, serviceYAML)
	to := setOf(t, teamYAML, strings.Replace(serviceYAML, "      - \"2\"\n", "", 1))
	plan := Plan{Base: 1, Desired: to, Effects: definition.EffectsOf(from, to)}

	db.ExpectBegin()
	db.ExpectExec(queryLockRevisions).WillReturnResult(sqlmock.NewResult(0, 0))
	db.ExpectQuery(queryRevisionNumber).WillReturnRows(sqlmock.NewRows([]string{"number"}).AddRow(1))
	db.ExpectExec(queryClearDefs).WillReturnResult(sqlmock.NewResult(0, 2))
	db.ExpectExec(queryInsertDef).WithArgs(definition.KindBlueprint, "service", sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
	db.ExpectExec(queryInsertDef).WithArgs(definition.KindBlueprint, "team", sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
	db.ExpectQuery(queryValues).WithArgs("service", "tier").WillReturnRows(sqlmock.NewRows([]string{"identifier", "title", "value"}).
		AddRow("ledger", "Ledger", []byte(`"1"`)).AddRow("payments-api", "Payments API", []byte(`"2"`)))
	db.ExpectExec(queryClearValues).WithArgs("service", "tier", `["payments-api"]`).WillReturnResult(sqlmock.NewResult(0, 1))
	db.ExpectQuery(queryInsertRevision).WithArgs(2, "", "ci", "", 0, sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"created_at"}).AddRow(at))
	db.ExpectCommit()

	revision, err := store.Apply(context.Background(), plan, audit.Actor{Kind: audit.ActorServiceToken, ID: "t-1", Name: "ci"}, "")
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	want := []FieldImpact{{Field: definition.Field{Blueprint: "service", Name: "tier"}, Count: 1, Examples: []EntityName{
		{Blueprint: "service", Identifier: "payments-api", Title: "Payments API"},
	}}}
	if !reflect.DeepEqual(revision.Impact.ClearedValues, want) {
		t.Fatalf("ClearedValues = %+v, want payments-api only", revision.Impact.ClearedValues)
	}
}

func TestDefinitionsReadsTheCurrentSet(t *testing.T) {
	store, db := newStore(t)
	db.ExpectQuery(queryRevisionNumber).WillReturnRows(sqlmock.NewRows([]string{"number"}).AddRow(2))
	db.ExpectQuery(queryDefinitions).WillReturnRows(sqlmock.NewRows([]string{"document"}).
		AddRow([]byte(`{"kind":"Blueprint","identifier":"team","title":"Team","properties":{"b":{"title":"B","type":"number"},"a":{"title":"A","type":"number"}}}`)))

	set, number, err := store.Definitions(context.Background())
	if err != nil || number != 2 {
		t.Fatalf("Definitions() = %d, %v", number, err)
	}
	team, ok := set.Blueprint("team")
	if !ok || !reflect.DeepEqual(team.Properties.Keys(), []string{"b", "a"}) {
		t.Fatalf("team = %+v, want properties in stored order", team)
	}
}

func TestRevisionReadsTheSnapshot(t *testing.T) {
	store, db := newStore(t)
	db.ExpectQuery(queryRevision).WithArgs(2).WillReturnRows(sqlmock.NewRows([]string{
		"number", "author_user_id", "author_label", "message", "rollback_of", "created_at", "changes", "impact",
	}).AddRow(2, "u-1", "alice@example.test", "add teams", 0, at, []byte(`[{"kind":"Blueprint","identifier":"team","title":"Team","action":"add"}]`), []byte(`{}`)))
	db.ExpectQuery(querySnapshot).WithArgs(2).WillReturnRows(sqlmock.NewRows([]string{"snapshot"}).
		AddRow([]byte(`[{"kind":"Blueprint","identifier":"team","title":"Team"}]`)))

	revision, err := store.Revision(context.Background(), 2)
	if err != nil {
		t.Fatalf("Revision() error = %v", err)
	}
	if len(revision.Snapshot.Documents) != 1 || len(revision.Changes) != 1 || revision.Author.Label != "alice@example.test" {
		t.Fatalf("Revision() = %+v", revision)
	}

	db.ExpectQuery(queryRevision).WithArgs(9).WillReturnRows(sqlmock.NewRows([]string{"number"}))
	if _, err := store.Revision(context.Background(), 9); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Revision(9) error = %v, want ErrNotFound", err)
	}
}

func TestCreateEntityWritesItsLinks(t *testing.T) {
	store, db := newStore(t)
	entity := Entity{
		Blueprint: "service", Identifier: "payments-api", Title: "Payments API", Integration: definition.Manual,
		Properties: map[string]any{"tier": "1"},
		Links:      []Link{{Relation: "owner", Target: EntityName{Blueprint: "team", Identifier: "payments"}}},
	}
	db.ExpectBegin()
	db.ExpectQuery(queryInsertEntity).WithArgs("service", "payments-api", "Payments API", `{"tier":"1"}`, "manual").
		WillReturnRows(sqlmock.NewRows([]string{"created_at", "updated_at"}).AddRow(at, at))
	db.ExpectExec(queryClearLinks).WithArgs("service", "payments-api").WillReturnResult(sqlmock.NewResult(0, 0))
	db.ExpectExec(queryInsertLink).WithArgs("service", "payments-api", "owner", "team", "payments").WillReturnResult(sqlmock.NewResult(0, 1))
	db.ExpectCommit()

	created, err := store.CreateEntity(context.Background(), entity)
	if err != nil || !created.CreatedAt.Equal(at) {
		t.Fatalf("CreateEntity() = %+v, %v", created, err)
	}
	if err := db.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCreateEntityRefusesAnIdentifierInUse(t *testing.T) {
	store, db := newStore(t)
	db.ExpectBegin()
	db.ExpectQuery(queryInsertEntity).WillReturnError(&pq.Error{Code: uniqueViolation})
	db.ExpectRollback()

	if _, err := store.CreateEntity(context.Background(), Entity{Blueprint: "service", Identifier: "ledger"}); !errors.Is(err, ErrExists) {
		t.Fatalf("CreateEntity() error = %v, want ErrExists", err)
	}
}

func TestEntitiesEscapesTheSearch(t *testing.T) {
	store, db := newStore(t)
	db.ExpectQuery(queryCountMatches).WithArgs("service", `%50\%%`, "").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	db.ExpectQuery(queryEntities).WithArgs("service", `%50\%%`, "", 0, 20).WillReturnRows(sqlmock.NewRows([]string{
		"blueprint", "identifier", "title", "properties", "integration", "created_at", "updated_at",
	}))

	if _, _, err := store.Entities(context.Background(), EntityFilter{Blueprint: "service", Search: "50%"}, Page{Limit: 20}); err != nil {
		t.Fatalf("Entities() error = %v", err)
	}
}

func TestPlansExpireAfterFifteenMinutes(t *testing.T) {
	now := at
	store := newPlans(func() time.Time { return now })
	kept := store.add(Plan{Base: 1})
	expired := store.add(Plan{Base: 1})

	if _, ok := store.take(kept.ID); !ok {
		t.Fatal("take() of a fresh plan = false")
	}
	if _, ok := store.take(kept.ID); ok {
		t.Fatal("take() of an applied plan = true, want a plan to apply once")
	}
	now = now.Add(planLifetime)
	if _, ok := store.take(expired.ID); ok {
		t.Fatal("take() of an expired plan = true")
	}
}

func TestMissingWithoutIdentifiersSendsAnEmptyList(t *testing.T) {
	store, db := newStore(t)
	db.ExpectQuery(queryExisting).WithArgs("team", "[]").WillReturnRows(sqlmock.NewRows([]string{"identifier"}))

	if missing, err := store.Missing(context.Background(), "team", nil); err != nil || len(missing) != 0 {
		t.Fatalf("Missing() = %v, %v, want none", missing, err)
	}
}
