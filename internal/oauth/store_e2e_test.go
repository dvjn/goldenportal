//go:build e2e

package oauth

import (
	"errors"
	"slices"
	"testing"

	"github.com/dvjn/goldenportal/internal/pgtest"
)

const grantsPerEndpoint = 20261116150000

// An app connected to both MCP servers holds a grant for each, so
// consenting at one leaves the other as it was, and each refresh stays
// bound to its own server.
func TestGrantsAreKeptPerEndpoint(t *testing.T) {
	db := pgtest.NewBefore(t, grantsPerEndpoint)
	ctx := t.Context()
	var userID, clientID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO users (oidc_issuer, oidc_subject, display_name) VALUES ('i', 's', 'Ada') RETURNING id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO oauth_clients (client_id, name, redirect_uris) VALUES ('gpc_1', 'Claude', '{}') RETURNING id`).Scan(&clientID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO oauth_grants (user_id, client_id, scopes) VALUES ($1, $2, '{portal:read,portal:author}')`, userID, clientID); err != nil {
		t.Fatal(err)
	}
	// A grant of authoring alone has nothing left at /api/mcp.
	var authorOnly string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO oauth_clients (client_id, name, redirect_uris) VALUES ('gpc_2', 'Cursor', '{}') RETURNING id`).Scan(&authorOnly); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO oauth_grants (user_id, client_id, scopes) VALUES ($1, $2, '{portal:author}')`, userID, authorOnly); err != nil {
		t.Fatal(err)
	}
	pgtest.MigrateRest(t, db, grantsPerEndpoint)

	store := NewStore(db)
	client := Client{ID: clientID, ClientID: "gpc_1"}
	builderGrant, err := store.Authorize(ctx, userID, client, BuilderMCP, Code{
		Resource: base + BuilderMCP.Path, Scopes: []string{ScopeRead, ScopeAuthor},
	}, []byte("code"))
	if err != nil {
		t.Fatal(err)
	}
	grants, err := store.Grants(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	servers := map[string]string{}
	for _, g := range grants {
		servers[g.Server] = g.ID
	}
	if len(grants) != 2 || servers["User MCP"] == "" || servers["Builder MCP"] != builderGrant {
		t.Fatalf("grants = %+v", grants)
	}
	for _, g := range grants {
		if g.Server == "User MCP" && !slices.Equal(g.Scopes, []string{ScopeRead}) {
			t.Errorf("the user server's grant kept %v", g.Scopes)
		}
	}

	// Consenting again at the builder server replaces only its scopes.
	if again, err := store.Authorize(ctx, userID, client, BuilderMCP, Code{
		Resource: base + BuilderMCP.Path, Scopes: []string{ScopeRead},
	}, []byte("code2")); err != nil || again != builderGrant {
		t.Fatalf("again = %s, %v", again, err)
	}

	grant := Grant{ID: builderGrant, UserID: userID, ClientID: "gpc_1", Scopes: []string{ScopeRead}}
	if err := store.IssueTokens(ctx, grant, base+BuilderMCP.Path, []byte("access"), []byte("refresh")); err != nil {
		t.Fatal(err)
	}
	// Asking for another resource, or as another client, leaves it usable.
	if _, _, err := store.TakeRefresh(ctx, []byte("refresh"), "gpc_1", base+UserMCP.Path); !errors.Is(err, errOtherResource) {
		t.Errorf("refresh for the user server: %v", err)
	}
	if _, _, err := store.TakeRefresh(ctx, []byte("refresh"), "gpc_2", ""); !errors.Is(err, errOtherClient) {
		t.Errorf("refresh by another client: %v", err)
	}
	if _, resource, err := store.TakeRefresh(ctx, []byte("refresh"), "gpc_1", ""); err != nil || resource != base+BuilderMCP.Path {
		t.Errorf("refresh is for %q, %v", resource, err)
	}
	if _, _, err := store.TakeRefresh(ctx, []byte("refresh"), "gpc_1", ""); !errors.Is(err, errNotFound) {
		t.Errorf("a refresh token worked twice: %v", err)
	}

	// The code is taken the same way.
	if _, err := store.TakeCode(ctx, []byte("code2"), "gpc_1", base+UserMCP.Path); !errors.Is(err, errOtherResource) {
		t.Errorf("code for the user server: %v", err)
	}
	if code, err := store.TakeCode(ctx, []byte("code2"), "gpc_1", base+BuilderMCP.Path); err != nil || code.Grant.ID != builderGrant {
		t.Errorf("code = %+v, %v", code, err)
	}
}
