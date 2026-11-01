package entity

import (
	"maps"
	"slices"

	"gofr.dev/pkg/gofr"

	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/search"
)

// Search finds entities of every published blueprint, each with its
// blueprint's title, icon and color. Entities are what people search for
// most, so it returns twice as many as the other kinds, and none until
// there's text to match.
func Search(ctx *gofr.Context, _ auth.Principal, text string) ([]search.Result, error) {
	if text == "" {
		return nil, nil
	}
	specs, err := blueprint.Published(ctx, ctx.SQL)
	if err != nil {
		return nil, err
	}
	found, err := NewStore(ctx.SQL).Search(ctx, slices.Collect(maps.Keys(specs)), text, 2*search.Limit)
	if err != nil {
		return nil, err
	}
	results := make([]search.Result, len(found))
	for i, f := range found {
		bp := specs[f.Blueprint]
		results[i] = search.Result{
			Kind: "entity", Identifier: f.Identifier, Title: f.Title,
			Detail: bp.Title, Blueprint: f.Blueprint, Icon: bp.Icon, Color: bp.Color,
		}
	}

	return results, nil
}
