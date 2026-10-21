package apitoken

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/dvjn/goldport/internal/access"
	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/container"
)

func newStore(t *testing.T) (*Store, sqlmock.Sqlmock) {
	t.Helper()
	c, mocks := container.NewMockContainer(t)

	return NewStore(c.SQL), mocks.SQL
}

var createdAt = time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)

func tokenRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "kind", "name", "role", "created_by", "created_by_email", "created_at", "expires_at", "last_used_at",
	})
}

func ciToken() Token {
	return Token{
		ID: "t-1", Kind: KindAPI, Name: "ci", Role: access.Maintainer, CreatedBy: "u-1", CreatedByEmail: "alice@example.test",
		CreatedAt: createdAt, ExpiresAt: createdAt.AddDate(0, 3, 0),
	}
}

func addToken(rows *sqlmock.Rows, token Token) *sqlmock.Rows {
	lastUsed := sql.NullTime{Time: token.LastUsedAt, Valid: !token.LastUsedAt.IsZero()}
	return rows.AddRow(token.ID, token.Kind, token.Name, token.Role, token.CreatedBy, token.CreatedByEmail,
		token.CreatedAt, token.ExpiresAt, lastUsed)
}

func TestVerifyAcceptsOnlyLiveTokensOfTheKind(t *testing.T) {
	store, db := newStore(t)
	scim := Token{ID: "t-1", Kind: KindSCIM, Name: "GOLDPORT_SCIM_TOKEN", CreatedAt: createdAt}
	db.ExpectQuery(queryFindToken).WithArgs(KindSCIM, hash("live")).WillReturnRows(addToken(tokenRows(), scim))
	db.ExpectQuery(queryFindToken).WithArgs(KindSCIM, hash("revoked")).WillReturnRows(tokenRows())

	if ok, err := store.Verify(context.Background(), KindSCIM, "live"); !ok || err != nil {
		t.Fatalf("Verify(live) = %t, %v, want true", ok, err)
	}
	if ok, err := store.Verify(context.Background(), KindSCIM, "revoked"); ok || err != nil {
		t.Fatalf("Verify(revoked) = %t, %v, want false", ok, err)
	}
}

func TestFindReturnsTheTokenWithItsRole(t *testing.T) {
	store, db := newStore(t)
	db.ExpectQuery(queryFindToken).WithArgs(KindAPI, hash("gpt_live")).WillReturnRows(addToken(tokenRows(), ciToken()))
	db.ExpectQuery(queryFindToken).WithArgs(KindAPI, hash("gpt_expired")).WillReturnRows(tokenRows())

	if token, err := store.Find(context.Background(), KindAPI, "gpt_live"); err != nil || token != ciToken() {
		t.Fatalf("Find(live) = %+v, %v, want %+v", token, err, ciToken())
	}
	if _, err := store.Find(context.Background(), KindAPI, "gpt_expired"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Find(expired) error = %v, want ErrNotFound", err)
	}
}

func TestCreateStoresOnlyTheHashOfAPrefixedSecret(t *testing.T) {
	store, db := newStore(t)
	want := ciToken()
	db.ExpectQuery(queryCreateToken).
		WithArgs(KindAPI, "ci", sqlmock.AnyArg(), access.Maintainer, "u-1", sql.NullTime{Time: want.ExpiresAt, Valid: true}).
		WillReturnRows(addToken(tokenRows(), want))

	token, secret, err := store.Create(context.Background(), NewToken{
		Kind: KindAPI, Name: "ci", Role: access.Maintainer, CreatedBy: "u-1", ExpiresAt: want.ExpiresAt,
	})
	if err != nil || token != want {
		t.Fatalf("Create() = %+v, %v, want %+v", token, err, want)
	}
	if !strings.HasPrefix(secret, Prefix) || len(secret) < len(Prefix)+20 {
		t.Fatalf("secret = %q, want a long secret with the %s prefix", secret, Prefix)
	}
}

func TestRotateCreatesATokenAndRevokesTheOthers(t *testing.T) {
	store, db := newStore(t)
	rotated := Token{ID: "t-2", Kind: KindSCIM, Name: "Rotated", CreatedBy: "u-1", CreatedAt: createdAt}
	db.ExpectBegin()
	db.ExpectQuery(queryCreateToken).
		WithArgs(KindSCIM, "Rotated", sqlmock.AnyArg(), access.NoRole, "u-1", sql.NullTime{}).
		WillReturnRows(addToken(tokenRows(), rotated))
	db.ExpectExec(queryRevokeOthers).WithArgs(KindSCIM, "t-2").WillReturnResult(sqlmock.NewResult(0, 1))
	db.ExpectCommit()

	token, secret, err := store.Rotate(context.Background(), NewToken{Kind: KindSCIM, Name: "Rotated", CreatedBy: "u-1"})
	if err != nil || token.ID != "t-2" || secret == "" {
		t.Fatalf("Rotate() = %+v, %q, %v", token, secret, err)
	}
}

func TestListAndRevoke(t *testing.T) {
	store, db := newStore(t)
	db.ExpectQuery(queryListTokens).WithArgs(KindAPI).WillReturnRows(addToken(tokenRows(), ciToken()))
	db.ExpectQuery(queryRevokeToken).WithArgs(KindAPI, "t-1").WillReturnRows(addToken(tokenRows(), ciToken()))
	db.ExpectQuery(queryRevokeToken).WithArgs(KindAPI, "t-1").WillReturnRows(tokenRows())

	if tokens, err := store.List(context.Background(), KindAPI); err != nil || len(tokens) != 1 || tokens[0] != ciToken() {
		t.Fatalf("List() = %+v, %v", tokens, err)
	}
	if token, err := store.Revoke(context.Background(), KindAPI, "t-1"); err != nil || token.Name != "ci" {
		t.Fatalf("Revoke() = %+v, %v", token, err)
	}
	if _, err := store.Revoke(context.Background(), KindAPI, "t-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second Revoke() error = %v, want ErrNotFound", err)
	}
}

func TestLastUsedIsZeroWithoutUse(t *testing.T) {
	store, db := newStore(t)
	db.ExpectQuery(queryLastUsed).WithArgs(KindSCIM).WillReturnRows(sqlmock.NewRows([]string{"max"}).AddRow(nil))
	db.ExpectQuery(queryLastUsed).WithArgs(KindSCIM).WillReturnRows(sqlmock.NewRows([]string{"max"}).AddRow(createdAt))

	if at, err := store.LastUsed(context.Background(), KindSCIM); err != nil || !at.IsZero() {
		t.Fatalf("LastUsed() = %v, %v, want the zero time", at, err)
	}
	if at, err := store.LastUsed(context.Background(), KindSCIM); err != nil || !at.Equal(createdAt) {
		t.Fatalf("LastUsed() = %v, %v, want %v", at, err, createdAt)
	}
}

func TestSeedStoresTheTokenOnlyWhileNoneOfTheKindExists(t *testing.T) {
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
