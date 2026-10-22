package catalog

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

// memoryBatch keeps entities with their owner, and links as source,
// relation and target.
type memoryBatch struct {
	entities map[EntityName]Entity
	links    []Link
	// stolen is an identifier another integration creates between the owner
	// check and the upsert.
	stolen string
}

func newMemoryBatch(entities ...Entity) *memoryBatch {
	batch := &memoryBatch{entities: map[EntityName]Entity{}}
	for _, entity := range entities {
		batch.entities[key(entity.Blueprint, entity.Identifier)] = entity
	}

	return batch
}

func (m *memoryBatch) Owners(_ context.Context, blueprint string, identifiers []string) (map[string]string, error) {
	owners := map[string]string{}
	for _, identifier := range identifiers {
		if entity, ok := m.entities[key(blueprint, identifier)]; ok {
			owners[identifier] = entity.Integration
		}
	}

	return owners, nil
}

func (m *memoryBatch) Owned(_ context.Context, integration, blueprint string) ([]string, error) {
	var owned []string
	for name, entity := range m.entities {
		if name.Blueprint == blueprint && entity.Integration == integration {
			owned = append(owned, name.Identifier)
		}
	}
	slices.Sort(owned)

	return owned, nil
}

func (m *memoryBatch) Upsert(_ context.Context, entity Entity) (bool, error) {
	if entity.Identifier == m.stolen {
		m.entities[key(entity.Blueprint, entity.Identifier)] = Entity{Blueprint: entity.Blueprint, Identifier: entity.Identifier, Integration: "gitlab"}
	}
	if existing, ok := m.entities[key(entity.Blueprint, entity.Identifier)]; ok && existing.Integration != entity.Integration {
		return false, nil
	}
	entity.Links = nil
	m.entities[key(entity.Blueprint, entity.Identifier)] = entity

	return true, nil
}

func (m *memoryBatch) Missing(_ context.Context, blueprint string, identifiers []string) ([]string, error) {
	var missing []string
	for _, identifier := range identifiers {
		if _, ok := m.entities[key(blueprint, identifier)]; !ok {
			missing = append(missing, identifier)
		}
	}

	return missing, nil
}

func (m *memoryBatch) ReplaceLinks(_ context.Context, source EntityName, relation string, targets []EntityName) error {
	m.links = slices.DeleteFunc(m.links, func(link Link) bool { return link.Source == source && link.Relation == relation })
	for _, target := range targets {
		m.links = append(m.links, Link{Relation: relation, Source: source, Target: target})
	}

	return nil
}

func (m *memoryBatch) Delete(_ context.Context, integration, blueprint string, identifiers []string) (int, error) {
	deleted := 0
	for _, identifier := range identifiers {
		if entity, ok := m.entities[key(blueprint, identifier)]; ok && entity.Integration == integration {
			delete(m.entities, key(blueprint, identifier))
			deleted++
		}
	}

	return deleted, nil
}

func (m *memoryBatch) identifiers(blueprint string) []string {
	var identifiers []string
	for name := range m.entities {
		if name.Blueprint == blueprint {
			identifiers = append(identifiers, name.Identifier)
		}
	}
	slices.Sort(identifiers)

	return identifiers
}

func service(identifier string, owner ...string) OwnedEntity {
	entity := OwnedEntity{
		Entity:    Entity{Blueprint: "service", Identifier: identifier, Title: identifier, Properties: map[string]any{}},
		Relations: []string{"owner"},
	}
	for _, team := range owner {
		entity.Links = append(entity.Links, Link{Relation: "owner", Target: EntityName{Blueprint: "team", Identifier: team}})
	}

	return entity
}

func ciServices(entities ...OwnedEntity) OwnedWrite {
	return OwnedWrite{Integration: "ci", Groups: []OwnedGroup{{Blueprint: "service", Entities: entities}}}
}

func owned(integration, blueprint, identifier string) Entity {
	return Entity{Blueprint: blueprint, Identifier: identifier, Title: identifier, Integration: integration}
}

func TestWriteOwnedRefusesTheWholeWriteWhenAnotherIntegrationOwnsAnEntity(t *testing.T) {
	batch := newMemoryBatch(owned("manual", "service", "payments-api"))

	_, err := WriteOwned(context.Background(), batch, ciServices(service("checkout"), service("payments-api")))

	var conflict ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("WriteOwned() error = %v, want a ConflictError", err)
	}
	want := []Conflict{{Blueprint: "service", Identifier: "payments-api", Integration: "manual"}}
	if !reflect.DeepEqual(conflict.Conflicts, want) {
		t.Fatalf("conflicts = %+v, want %+v", conflict.Conflicts, want)
	}
	if err.Error() != "ownership conflict: payments-api is owned by the manual integration" {
		t.Fatalf("Error() = %q", err.Error())
	}
	if got := batch.identifiers("service"); !slices.Equal(got, []string{"payments-api"}) {
		t.Fatalf("services = %v, want nothing written", got)
	}
}

func TestWriteOwnedNamesTheOwnerOfAnEntityCreatedDuringTheWrite(t *testing.T) {
	batch := newMemoryBatch()
	batch.stolen = "checkout"

	_, err := WriteOwned(context.Background(), batch, ciServices(service("checkout")))

	var conflict ConflictError
	if !errors.As(err, &conflict) || conflict.Conflicts[0].Integration != "gitlab" {
		t.Fatalf("WriteOwned() error = %v, want a conflict with gitlab", err)
	}
}

func TestWriteOwnedReplacesTheNamedRelationsAndDropsMissingTargets(t *testing.T) {
	batch := newMemoryBatch(owned("manual", "team", "payments"), owned("ci", "service", "ledger"))
	ledger := EntityName{Blueprint: "service", Identifier: "ledger", Title: "ledger"}
	batch.links = []Link{
		{Relation: "owner", Source: ledger, Target: key("team", "platform")},
		{Relation: "dependsOn", Source: ledger, Target: key("service", "checkout")},
	}

	result, err := WriteOwned(context.Background(), batch, ciServices(service("ledger", "payments", "billing")))
	if err != nil {
		t.Fatal(err)
	}

	wantLinks := []Link{
		{Relation: "dependsOn", Source: ledger, Target: key("service", "checkout")},
		{Relation: "owner", Source: ledger, Target: key("team", "payments")},
	}
	if !reflect.DeepEqual(batch.links, wantLinks) {
		t.Fatalf("links = %+v, want %+v", batch.links, wantLinks)
	}
	wantDropped := []Link{{Relation: "owner", Source: ledger, Target: key("team", "billing")}}
	if result.Upserted != 1 || !reflect.DeepEqual(result.Dropped, wantDropped) {
		t.Fatalf("result = %+v, want one upsert and the billing link dropped", result)
	}
}

func TestWriteOwnedLinksToTargetsInTheSameWrite(t *testing.T) {
	batch := newMemoryBatch()
	write := OwnedWrite{Integration: "ci", Groups: []OwnedGroup{
		{Blueprint: "service", Entities: []OwnedEntity{service("ledger", "payments")}},
		{Blueprint: "team", Entities: []OwnedEntity{{Entity: Entity{Blueprint: "team", Identifier: "payments", Title: "Payments"}}}},
	}}

	result, err := WriteOwned(context.Background(), batch, write)

	if err != nil || len(result.Dropped) > 0 || len(batch.links) != 1 {
		t.Fatalf("WriteOwned() = %+v, %v with links %+v, want the owner link kept", result, err, batch.links)
	}
}

func TestWriteOwnedSweepsWhatTheWriteLeavesOut(t *testing.T) {
	existing := []Entity{owned("ci", "service", "checkout"), owned("ci", "service", "ledger"), owned("ci", "service", "search"), owned("manual", "service", "payments-api")}
	cases := []struct {
		name      string
		write     OwnedWrite
		remaining []string
		deleted   int
		guarded   []GuardedSweep
	}{
		{
			name:      "upsert keeps the others",
			write:     ciServices(service("ledger")),
			remaining: []string{"checkout", "ledger", "payments-api", "search"},
		},
		{
			name:      "snapshot with at least half deletes the others it owns",
			write:     OwnedWrite{Integration: "ci", Sweep: true, Groups: ciServices(service("ledger"), service("search")).Groups},
			remaining: []string{"ledger", "payments-api", "search"},
			deleted:   1,
		},
		{
			name:      "snapshot with fewer than half is guarded",
			write:     OwnedWrite{Integration: "ci", Sweep: true, Groups: ciServices(service("ledger")).Groups},
			remaining: []string{"checkout", "ledger", "payments-api", "search"},
			guarded:   []GuardedSweep{{Blueprint: "service", Kept: 2, Written: 1, Existing: 3}},
		},
		{
			name:      "forced snapshot with fewer than half deletes",
			write:     OwnedWrite{Integration: "ci", Sweep: true, Force: true, Groups: ciServices(service("ledger")).Groups},
			remaining: []string{"ledger", "payments-api"},
			deleted:   2,
		},
		{
			name:      "empty snapshot is guarded",
			write:     OwnedWrite{Integration: "ci", Sweep: true, Groups: []OwnedGroup{{Blueprint: "service"}}},
			remaining: []string{"checkout", "ledger", "payments-api", "search"},
			guarded:   []GuardedSweep{{Blueprint: "service", Kept: 3, Written: 0, Existing: 3}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			batch := newMemoryBatch(existing...)

			result, err := WriteOwned(context.Background(), batch, tc.write)
			if err != nil {
				t.Fatal(err)
			}

			if got := batch.identifiers("service"); !slices.Equal(got, tc.remaining) {
				t.Errorf("services = %v, want %v", got, tc.remaining)
			}
			if result.Deleted != tc.deleted || !reflect.DeepEqual(result.Guarded, tc.guarded) {
				t.Errorf("result = %+v, want %d deleted and guarded %+v", result, tc.deleted, tc.guarded)
			}
		})
	}
}

func TestStoreWriteOwnedRunsInOneTransaction(t *testing.T) {
	store, db := newStore(t)

	db.ExpectBegin()
	db.ExpectQuery(queryOwners).WithArgs("service", `["ledger"]`).
		WillReturnRows(sqlmock.NewRows([]string{"identifier", "integration"}).AddRow("ledger", "ci"))
	db.ExpectQuery(queryOwned).WithArgs("ci", "service").
		WillReturnRows(sqlmock.NewRows([]string{"identifier"}).AddRow("ledger").AddRow("search"))
	db.ExpectQuery(queryUpsertOwned).WithArgs("service", "ledger", "ledger", "{}", "ci").
		WillReturnRows(sqlmock.NewRows([]string{"identifier"}).AddRow("ledger"))
	db.ExpectQuery(queryExisting).WithArgs("team", `["payments"]`).
		WillReturnRows(sqlmock.NewRows([]string{"identifier"}).AddRow("payments"))
	db.ExpectExec(queryClearRelation).WithArgs("service", "ledger", "owner").WillReturnResult(sqlmock.NewResult(0, 1))
	db.ExpectExec(queryInsertLink).WithArgs("service", "ledger", "owner", "team", "payments").WillReturnResult(sqlmock.NewResult(0, 1))
	db.ExpectExec(queryDeleteOwned).WithArgs("ci", "service", `["search"]`).WillReturnResult(sqlmock.NewResult(0, 1))
	db.ExpectCommit()

	result, err := store.WriteOwned(context.Background(), OwnedWrite{
		Integration: "ci", Sweep: true, Groups: ciServices(service("ledger", "payments")).Groups,
	})

	if err != nil || result.Upserted != 1 || result.Deleted != 1 {
		t.Fatalf("WriteOwned() = %+v, %v, want one upsert and one delete", result, err)
	}
	if err := db.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestStoreUpsertReportsAnEntityAnotherIntegrationOwns(t *testing.T) {
	store, db := newStore(t)
	db.ExpectQuery(queryUpsertOwned).WillReturnRows(sqlmock.NewRows([]string{"identifier"}))

	written, err := sqlBatch{q: store.db}.Upsert(context.Background(), owned("ci", "service", "ledger"))

	if written || err != nil {
		t.Fatalf("Upsert() = %t, %v, want false without an error", written, err)
	}
}
