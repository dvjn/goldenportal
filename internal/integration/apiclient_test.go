package integration

import (
	"errors"
	"net/url"
	"testing"
)

func TestAPIClientWithin(t *testing.T) {
	client, err := newAPIClient("GitLab", "https://gitlab.example.com/api/v4")
	if err != nil {
		t.Fatal(err)
	}
	for address, want := range map[string]bool{
		"https://gitlab.example.com/api/v4/projects/1":             true,
		"https://GITLAB.example.com/api/v4/projects/acme%2Fapi":    true,
		"https://gitlab.example.com/api/v4/../../admin":            false,
		"https://gitlab.example.com/api/v4/%2e%2e/%2E%2E/admin":    false,
		"https://gitlab.example.com/api/v4/projects/./1":           false,
		"https://gitlab.example.com/api/v40/projects":              false,
		"http://gitlab.example.com/api/v4/projects":                false,
		"https://user@gitlab.example.com/api/v4/projects":          false,
		"https://elsewhere.example.com/api/v4/projects":            false,
		"https://gitlab.example.com/api/v4/projects?page=2#anchor": true,
	} {
		u, _ := url.Parse(address)
		if got := client.within(u); got != want {
			t.Errorf("within(%s) = %v, want %v", address, got, want)
		}
	}
}

func TestCurrent(t *testing.T) {
	cutoff := timeOf(map[string]any{"at": "2026-01-10T00:00:00Z"}, "at")
	open := []string{"opened", "locked"}
	for _, test := range []struct {
		object map[string]any
		want   bool
	}{
		{map[string]any{"state": "opened", "created_at": "2020-01-01T00:00:00Z"}, true},
		{map[string]any{"state": "merged", "merged_at": "2026-01-11T00:00:00Z", "closed_at": "2020-01-01T00:00:00Z"}, true},
		{map[string]any{"state": "closed", "closed_at": "2026-01-09T00:00:00Z"}, false},
		{map[string]any{"state": "closed", "merged_at": nil, "closed_at": "2026-01-10T00:00:00Z"}, true},
		{map[string]any{"state": "closed"}, false},
	} {
		if got := current(test.object, cutoff, open, "merged_at", "closed_at"); got != test.want {
			t.Errorf("current(%v) = %v, want %v", test.object, got, test.want)
		}
	}
}

func TestForEachStopsAtTheFirstFailure(t *testing.T) {
	errBoom := errors.New("boom")
	var ran int
	err := forEach([]int{1, 2, 3, 4, 5}, 1, func(i int) error {
		ran++
		if i == 2 {
			return errBoom
		}
		return nil
	})
	if !errors.Is(err, errBoom) || ran != 2 {
		t.Errorf("err = %v after %d, want the failure, and no more started", err, ran)
	}
}
