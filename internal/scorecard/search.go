package scorecard

import (
	"gofr.dev/pkg/gofr"

	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/search"
)

// Search finds published scorecards, which everyone can open.
func Search(ctx *gofr.Context, _ auth.Principal, text string) ([]search.Result, error) {
	specs, err := Published(ctx, ctx.SQL)
	if err != nil {
		return nil, err
	}
	results := make([]search.Result, 0, len(specs))
	for _, spec := range specs {
		results = append(results, search.Result{
			Kind: "scorecard", Identifier: spec.Identifier, Title: spec.Title, Icon: spec.Icon, Color: spec.Color,
		})
	}

	return search.Match(results, text), nil
}
