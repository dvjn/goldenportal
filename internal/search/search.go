// Package search finds things across the portal by title or identifier,
// from sources each package provides for its own kind.
package search

import (
	"cmp"
	"slices"
	"strings"

	"gofr.dev/pkg/gofr"

	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/httpx"
)

// Limit is how many results each source returns, and twice it how many a
// search returns, enough to pick from without paging.
const Limit = 10

// Result is something a search found. An entity has Blueprint, which its
// page is under, and Detail, that blueprint's title, which says what kind
// of entity it is.
type Result struct {
	Kind       string `json:"kind"`
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
	Detail     string `json:"detail,omitempty"`
	Blueprint  string `json:"blueprint,omitempty"`
	Icon       string `json:"icon,omitempty"`
	Color      string `json:"color,omitempty"`
}

// Source finds results of one kind that p may open. Text is trimmed and
// may be empty, which asks for what to start from, or none.
type Source func(ctx *gofr.Context, p auth.Principal, text string) ([]Result, error)

// Service searches every source it was made with.
type Service struct{ sources []Source }

func New(sources ...Source) Service { return Service{sources: sources} }

// Register serves GET search?q=.
func Register(app httpx.Routes, s Service) {
	app.GET("search", func(ctx *gofr.Context) (any, error) { return s.Search(ctx, ctx.Param("q")) })
}

// Search returns every source's results in one list, closest first: exact
// matches, then those starting with the text, then those containing it.
// Ties keep the order of sources, then go by title.
func (s Service) Search(ctx *gofr.Context, text string) ([]Result, error) {
	p, err := auth.Require(ctx, auth.RoleMember)
	if err != nil {
		return nil, err
	}
	text = strings.TrimSpace(text)
	found := make([][]Result, len(s.sources))
	for i, source := range s.sources {
		if found[i], err = source(ctx, p, text); err != nil {
			return nil, err
		}
	}

	return merge(text, found...), nil
}

// Match keeps the results whose title or identifier contains text,
// ignoring case: exact matches first, then those starting with it, then by
// title, up to Limit. Empty text keeps them all, by title.
func Match(results []Result, text string) []Result {
	kept := slices.DeleteFunc(slices.Clone(results), func(r Result) bool { return rank(r, text) < 0 })
	slices.SortStableFunc(kept, func(a, b Result) int {
		return cmp.Or(cmp.Compare(rank(a, text), rank(b, text)), compareTitles(a, b))
	})

	return kept[:min(len(kept), Limit)]
}

// merge puts each source's results in one list, closest to text first,
// keeping the order of sources and then titles among equals, up to twice
// Limit.
func merge(text string, found ...[]Result) []Result {
	var results []ranked
	for order, list := range found {
		for _, r := range list {
			results = append(results, ranked{Result: r, rank: rank(r, text), order: order})
		}
	}
	slices.SortStableFunc(results, func(a, b ranked) int {
		return cmp.Or(cmp.Compare(a.rank, b.rank), cmp.Compare(a.order, b.order), compareTitles(a.Result, b.Result))
	})
	out := make([]Result, 0, min(len(results), 2*Limit))
	for _, r := range results[:min(len(results), 2*Limit)] {
		out = append(out, r.Result)
	}

	return out
}

type ranked struct {
	Result
	rank, order int
}

// rank is how closely r matches text, ignoring case: 0 exactly, 1 starting
// with it, 2 containing it, and -1 not at all. Empty text matches all alike.
func rank(r Result, text string) int {
	needle := strings.ToLower(text)
	title, id := strings.ToLower(r.Title), strings.ToLower(r.Identifier)
	switch {
	case needle == "":
		return 0
	case title == needle || id == needle:
		return 0
	case strings.HasPrefix(title, needle) || strings.HasPrefix(id, needle):
		return 1
	case strings.Contains(title, needle) || strings.Contains(id, needle):
		return 2
	}
	return -1
}

func compareTitles(a, b Result) int {
	return cmp.Or(strings.Compare(a.Title, b.Title), strings.Compare(a.Identifier, b.Identifier))
}
