//go:build e2e

package access

import (
	"slices"
	"testing"

	gofrSQL "gofr.dev/pkg/gofr/datasource/sql"

	"github.com/dvjn/goldenportal/internal/pgtest"
)

type directory struct {
	t  *testing.T
	db *gofrSQL.DB
}

func (d directory) user(name, email, status string) string {
	d.t.Helper()
	var id string
	if err := d.db.QueryRowContext(d.t.Context(), `
		INSERT INTO users (oidc_issuer, oidc_subject, display_name, email, status)
		VALUES ('issuer', $1, $2, $1, $3) RETURNING id`, email, name, status).Scan(&id); err != nil {
		d.t.Fatal(err)
	}

	return id
}

func (d directory) group(name string, members ...string) {
	d.t.Helper()
	var id string
	if err := d.db.QueryRowContext(d.t.Context(), `INSERT INTO groups (name) VALUES ($1) RETURNING id`, name).Scan(&id); err != nil {
		d.t.Fatal(err)
	}
	for _, member := range members {
		if _, err := d.db.ExecContext(d.t.Context(), `
			INSERT INTO group_members (group_id, user_id) VALUES ($1, $2)`, id, member); err != nil {
			d.t.Fatal(err)
		}
	}
}

func names(found []Subject) []string {
	out := make([]string, 0, len(found))
	for _, s := range found {
		out = append(out, s.Type+":"+s.Name)
	}

	return out
}

func TestSearchRanksPeopleAndGroupsTogether(t *testing.T) {
	d := directory{t: t, db: pgtest.New(t)}
	me := d.user("Grace Self", "me@example.com", "active")
	hopper := d.user("Grace Hopper", "hopper@example.com", "active")
	d.user("Ann Gracey", "ann@example.com", "active")
	d.user("Disgraced Admin", "dis@example.com", "active")
	d.user("Ungraceful Bot", "bot@example.com", "active")
	d.user("Grace Gone", "gone@example.com", "deactivated")
	d.user("Linus", "linus@example.com", "active")
	d.group("grace", hopper, me)
	d.group("ops-graceful")
	store := NewStore(d.db)

	found, err := store.Search(t.Context(), "GRACE", me, searchLimit)
	if err != nil {
		t.Fatal(err)
	}
	// Exact, then prefixes, then a word's start, then text inside, up to the limit.
	want := []string{"group:grace", "user:Grace Hopper", "user:Ann Gracey", "group:ops-graceful", "user:Disgraced Admin"}
	if got := names(found); !slices.Equal(got, want) {
		t.Errorf("search = %v, want %v", got, want)
	}
	if found[0].MemberCount == nil || *found[0].MemberCount != 2 || found[0].Email != "" {
		t.Errorf("group = %+v, want 2 members and no email", found[0])
	}
	if found[1].Email != "hopper@example.com" || found[1].MemberCount != nil || found[1].ID != hopper {
		t.Errorf("user = %+v", found[1])
	}

	if found, err = store.Search(t.Context(), "Linus@Example", me, searchLimit); err != nil || !slices.Equal(names(found), []string{"user:Linus"}) {
		t.Errorf("by email = %v, %v", names(found), err)
	}
	if found, err = store.Search(t.Context(), "gone", me, searchLimit); err != nil || len(found) != 0 {
		t.Errorf("deactivated = %v, %v", names(found), err)
	}
	if found, err = store.Search(t.Context(), "self", me, searchLimit); err != nil || len(found) != 0 {
		t.Errorf("self = %v, %v", names(found), err)
	}
	// A token's principal has no user, and everyone stays searchable.
	if found, err = store.Search(t.Context(), "self", "", searchLimit); err != nil || !slices.Equal(names(found), []string{"user:Grace Self"}) {
		t.Errorf("no caller = %v, %v", names(found), err)
	}
	if found, err = store.Search(t.Context(), "%", me, searchLimit); err != nil || len(found) != 0 {
		t.Errorf("a wildcard = %v, %v", names(found), err)
	}
}
