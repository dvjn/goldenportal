package apitoken

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/container"
)

func newStore(t *testing.T) (*Store, sqlmock.Sqlmock) {
	t.Helper()
	c, mocks := container.NewMockContainer(t)

	return NewStore(c.SQL), mocks.SQL
}

func TestVerifyAcceptsOnlyLiveTokensOfTheKind(t *testing.T) {
	store, db := newStore(t)
	db.ExpectQuery(queryVerifyToken).WithArgs(KindSCIM, hash("live")).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("t-1"))
	db.ExpectQuery(queryVerifyToken).WithArgs(KindSCIM, hash("revoked")).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	if ok, err := store.Verify(context.Background(), KindSCIM, "live"); !ok || err != nil {
		t.Fatalf("Verify(live) = %t, %v, want true", ok, err)
	}
	if ok, err := store.Verify(context.Background(), KindSCIM, "revoked"); ok || err != nil {
		t.Fatalf("Verify(revoked) = %t, %v, want false", ok, err)
	}
}

func TestSeedStoresTheHashOnce(t *testing.T) {
	store, db := newStore(t)
	db.ExpectExec(querySeedToken).WithArgs(KindSCIM, "seed", hash("token")).WillReturnResult(sqlmock.NewResult(0, 1))
	db.ExpectExec(querySeedToken).WithArgs(KindSCIM, "seed", hash("token")).WillReturnResult(sqlmock.NewResult(0, 0))

	if seeded, err := store.Seed(context.Background(), KindSCIM, "seed", "token"); !seeded || err != nil {
		t.Fatalf("first Seed() = %t, %v, want true", seeded, err)
	}
	if seeded, err := store.Seed(context.Background(), KindSCIM, "seed", "token"); seeded || err != nil {
		t.Fatalf("second Seed() = %t, %v, want false", seeded, err)
	}
}

type mapConfig map[string]string

func (m mapConfig) Get(key string) string { return m[key] }

func (m mapConfig) GetOrDefault(key, fallback string) string {
	if value, ok := m[key]; ok {
		return value
	}

	return fallback
}

func TestSCIMTokenIsRequired(t *testing.T) {
	if _, err := SCIMToken(mapConfig{}); err == nil {
		t.Fatal("SCIMToken() error = nil without GOLDPORT_SCIM_TOKEN")
	}
	if token, err := SCIMToken(mapConfig{"GOLDPORT_SCIM_TOKEN": "seed"}); token != "seed" || err != nil {
		t.Fatalf("SCIMToken() = %q, %v, want seed", token, err)
	}
}

type recordingSeeder struct {
	kind, token string
	err         error
}

func (r *recordingSeeder) Seed(_ context.Context, kind, _, token string) (bool, error) {
	r.kind, r.token = kind, token
	return r.err == nil, r.err
}

func TestSeedSCIMTokenSeedsTheConfiguredToken(t *testing.T) {
	c, _ := container.NewMockContainer(t)
	seeder := &recordingSeeder{}

	if err := SeedSCIMToken(seeder, "from-env")(&gofr.Context{Context: context.Background(), Container: c}); err != nil {
		t.Fatalf("hook error = %v", err)
	}
	if seeder.kind != KindSCIM || seeder.token != "from-env" {
		t.Fatalf("seeded %q %q, want the scim token from-env", seeder.kind, seeder.token)
	}

	seeder.err = errors.New("database down")
	if err := SeedSCIMToken(seeder, "from-env")(&gofr.Context{Context: context.Background(), Container: c}); err == nil {
		t.Fatal("hook error = nil, want the seed failure")
	}
}
