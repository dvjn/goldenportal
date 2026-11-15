//go:build e2e

package entity

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/pgtest"
)

// shapesMigration is the migration that indexes titles.
const shapesMigration = 20261117120000

// TestMigrationCutsTitlesTheIndexCantHold checks titles longer than writes
// now allow are cut to it on a character boundary, so the title index
// builds, and shorter ones are left alone.
func TestMigrationCutsTitlesTheIndexCantHold(t *testing.T) {
	db := pgtest.NewBefore(t, shapesMigration)
	blueprintID := pgtest.Publish(t, db, "blueprint", "service", "kind: Blueprint")
	connectionID := pgtest.Publish(t, db, "connection", "mine", "kind: Connection")
	// Three bytes a character, so the limit falls inside one.
	long, short := strings.Repeat("€", 2000), "Ledger"
	for identifier, title := range map[string]string{"long": long, "short": short} {
		if _, err := db.ExecContext(t.Context(), `INSERT INTO entities (blueprint_id, identifier, title, connection_id) VALUES ($1, $2, $3, $4)`,
			blueprintID, identifier, title, connectionID); err != nil {
			t.Fatal(err)
		}
	}

	pgtest.MigrateRest(t, db, shapesMigration)

	titles := map[string]string{}
	rows, err := db.QueryContext(t.Context(), `SELECT identifier, title FROM entities`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var identifier, title string
		if err := rows.Scan(&identifier, &title); err != nil {
			t.Fatal(err)
		}
		titles[identifier] = title
	}
	if got := titles["long"]; len(got) > blueprint.MaxEntityTitle || len(got) < blueprint.MaxEntityTitle-3 || !utf8.ValidString(got) || !strings.HasPrefix(long, got) {
		t.Errorf("long title is %d bytes, valid %t", len(got), utf8.ValidString(got))
	}
	if titles["short"] != short {
		t.Errorf("short title = %q", titles["short"])
	}
}
