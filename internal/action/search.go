package action

import (
	"gofr.dev/pkg/gofr"

	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/search"
)

// Search finds published actions, for builders, who maintain them, and the
// workflows the caller can run.
func Search(ctx *gofr.Context, p auth.Principal, text string) ([]search.Result, error) {
	results := []search.Result{}
	items, err := publishedDocs(ctx, ctx.SQL, WorkflowName)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if item.spec.Run.matches(p) || p.Can(auth.RoleBuilder) {
			results = append(results, search.Result{
				Kind: WorkflowName, Identifier: item.spec.Identifier, Title: item.spec.Title,
				Detail: item.spec.Group, Icon: item.spec.Icon, Color: item.spec.Color,
			})
		}
	}
	if !p.Can(auth.RoleBuilder) {
		return search.Match(results, text), nil
	}
	actions, err := publishedActions(ctx)
	if err != nil {
		return nil, err
	}
	for _, a := range actions {
		results = append(results, search.Result{
			Kind: Name, Identifier: a.spec.Identifier, Title: a.spec.Title, Icon: a.spec.Icon, Color: a.spec.Color,
		})
	}

	return search.Match(results, text), nil
}
