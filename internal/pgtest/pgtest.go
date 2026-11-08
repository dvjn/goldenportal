// Package pgtest gives end-to-end tests, those built with the e2e tag, a
// real Postgres database, migrated as the portal migrates its own, for what
// only a real database shows, such as which rows a statement changes.
package pgtest

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"net/url"
	"os"
	"slices"
	"testing"

	_ "github.com/lib/pq" // Registers the postgres driver.
	gofrSQL "gofr.dev/pkg/gofr/datasource/sql"
	"gofr.dev/pkg/gofr/logging"
	"gofr.dev/pkg/gofr/migration"

	"github.com/dvjn/goldenportal/migrations"
)

// URLVariable names the server tests make their databases on, as a
// postgres:// URL whose user may create databases. `mise run test:e2e`
// sets it to a server of its own.
const URLVariable = "GP_TEST_DATABASE_URL"

// New makes an empty database on the server URLVariable names, applies
// every migration, and drops it when the test ends.
func New(t *testing.T) *gofrSQL.DB {
	t.Helper()
	server := os.Getenv(URLVariable)
	if server == "" {
		t.Fatal(URLVariable + " isn't set, so there's no Postgres to test against; run mise run test:e2e")
	}
	admin, err := sql.Open("postgres", server)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })

	suffix := make([]byte, 6)
	_, _ = rand.Read(suffix)
	name := "gp_test_" + hex.EncodeToString(suffix)
	if _, err := admin.ExecContext(t.Context(), `CREATE DATABASE `+name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.ExecContext(context.Background(), `DROP DATABASE `+name+` WITH (FORCE)`)
	})

	u, err := url.Parse(server)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	raw, err := sql.Open("postgres", u.String())
	if err != nil {
		t.Fatal(err)
	}
	db := gofrSQL.NewSQLFromDB(raw, &gofrSQL.DBConfig{Dialect: "postgres", Database: name}, logging.NewMockLogger(logging.ERROR), noMetrics{})
	t.Cleanup(func() { _ = db.Close() })
	migrate(t, raw)

	return db
}

// migrate applies the migrations in order, each in a transaction, as gofr
// does.
func migrate(t *testing.T, db *sql.DB) {
	t.Helper()
	all := migrations.All()
	versions := make([]int64, 0, len(all))
	for version := range all {
		versions = append(versions, version)
	}
	slices.Sort(versions)
	for _, version := range versions {
		tx, err := db.BeginTx(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := all[version].UP(migration.Datasource{SQL: tx}); err != nil {
			_ = tx.Rollback()
			t.Fatalf("migration %d: %v", version, err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
}

type noMetrics struct{}

func (noMetrics) RecordHistogram(context.Context, string, float64, ...string) {}

func (noMetrics) SetGauge(string, float64, ...string) {}

// Publish stores a published document, such as a blueprint, and gives its
// ID.
func Publish(t *testing.T, db *gofrSQL.DB, kind, identifier, source string) string {
	t.Helper()
	var id string
	if err := db.QueryRowContext(t.Context(), `
		WITH d AS (INSERT INTO config_documents (kind, identifier, published_version) VALUES ($1, $2, 1) RETURNING id)
		INSERT INTO config_versions (document_id, version, source) SELECT id, 1, $3 FROM d RETURNING document_id`,
		kind, identifier, source).Scan(&id); err != nil {
		t.Fatal(err)
	}

	return id
}
