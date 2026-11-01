package integration

import (
	"gofr.dev/pkg/gofr"

	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/search"
)

// Search finds published integrations, for builders, whose pages they are.
func Search(ctx *gofr.Context, p auth.Principal, text string) ([]search.Result, error) {
	if !p.Can(auth.RoleBuilder) {
		return nil, nil
	}
	sources, err := configdoc.NewStore(ctx.SQL, Name).PublishedSources(ctx)
	if err != nil {
		return nil, err
	}
	results := []search.Result{}
	for _, source := range sources {
		spec, issues := Parse(source)
		if configdoc.HasErrors(issues) {
			continue
		}
		results = append(results, search.Result{
			Kind: "integration", Identifier: spec.Identifier, Title: spec.Title, Icon: spec.Icon, Color: spec.Color,
		})
	}

	return search.Match(results, text), nil
}
