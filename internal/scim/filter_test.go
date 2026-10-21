package scim

import (
	"net/http/httptest"
	"testing"

	"github.com/dvjn/goldport/internal/directory"
)

func TestParseFilterReadsEqualityLookups(t *testing.T) {
	cases := map[string]directory.UserFilter{
		``:                          {},
		`userName eq "alice"`:       {UserName: "alice"},
		`USERNAME EQ "alice"`:       {UserName: "alice"},
		`externalId eq "uid-1"`:     {ExternalID: "uid-1"},
		`userName eq "say \"hi\""`:  {UserName: `say "hi"`},
		`  userName eq "spaced"   `: {UserName: "spaced"},
	}
	for raw, want := range cases {
		got, err := parseFilter(raw, userFilters)
		if err != nil || got != want {
			t.Errorf("parseFilter(%q) = %+v, %v, want %+v", raw, got, err, want)
		}
	}
}

func TestParseFilterRefusesEverythingElse(t *testing.T) {
	for _, raw := range []string{
		`userName eq ""`,
		`userName eq alice`,
		`userName sw "al"`,
		`userName eq "a" and active eq "true"`,
		`displayName eq "team"`,
	} {
		if _, err := parseFilter(raw, userFilters); err == nil {
			t.Errorf("parseFilter(%q) error = nil, want invalidFilter", raw)
		}
	}
}

func TestParsePageClampsTheWindow(t *testing.T) {
	cases := map[string]directory.Page{
		"":                         {Offset: 0, Limit: maxResults},
		"?startIndex=3&count=10":   {Offset: 2, Limit: 10},
		"?startIndex=0&count=-5":   {Offset: 0, Limit: 0},
		"?startIndex=x&count=5000": {Offset: 0, Limit: maxResults},
	}
	for query, want := range cases {
		if got := parsePage(httptest.NewRequest("GET", "/Users"+query, nil)); got != want {
			t.Errorf("parsePage(%q) = %+v, want %+v", query, got, want)
		}
	}
}
