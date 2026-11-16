//go:build e2e

package connection

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/pgtest"
)

// The portal's users and groups count membership through a group below, as
// role grants do, and keep the direct lists beside them.
func TestPortalGroupsCountNestedMembers(t *testing.T) {
	auth.UseGroupParents(true)
	t.Cleanup(func() { auth.UseGroupParents(false) })
	db := pgtest.New(t)
	ctx := t.Context()
	if _, err := db.ExecContext(ctx, `
		WITH ada AS (
			INSERT INTO users (email, display_name) VALUES ('ada@example.com', 'Ada') RETURNING id
		), lead AS (
			INSERT INTO groups (name, external_id) VALUES ('lead', '11') RETURNING id
		), parents AS (
			INSERT INTO groups (name, external_id) VALUES ('discovery', '10'), ('engineering', '1')
		), edges AS (
			INSERT INTO group_parents (group_id, parent_external_id) SELECT id, '10' FROM lead
		)
		INSERT INTO group_members (group_id, user_id) SELECT lead.id, ada.id FROM lead, ada`); err != nil {
		t.Fatal(err)
	}

	got := readPortal(t, db)
	if u := got.Users[0]; !slices.Equal(u.Groups, []string{"discovery", "lead"}) || !slices.Equal(u.DirectGroups, []string{"lead"}) {
		t.Errorf("user groups = %v, direct %v", u.Groups, u.DirectGroups)
	}
	want := map[string][2][]string{
		"discovery":   {{"ada@example.com"}, {}},
		"engineering": {{}, {}},
		"lead":        {{"ada@example.com"}, {"ada@example.com"}},
	}
	for _, g := range got.Groups {
		w := want[g.Name]
		if !slices.Equal(g.Members, w[0]) || !slices.Equal(g.DirectMembers, w[1]) {
			t.Errorf("%s members = %v, direct %v; want %v, %v", g.Name, g.Members, g.DirectMembers, w[0], w[1])
		}
	}

	auth.UseGroupParents(false)
	if u := readPortal(t, db).Users[0]; !slices.Equal(u.Groups, []string{"lead"}) {
		t.Errorf("without SCIM, user groups = %v", u.Groups)
	}
}

func readPortal(t *testing.T, db configdoc.DB) portalData {
	t.Helper()
	snapshot, err := portalSnapshot(t.Context(), db, []string{"users", "groups"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(snapshot)
	var got portalData
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}

	return got
}
