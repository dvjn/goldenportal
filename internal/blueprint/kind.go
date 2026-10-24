package blueprint

import (
	"context"
	"net/http"
	"slices"
	"strings"

	"gofr.dev/pkg/gofr"

	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/httpx"
)

// Name is the configdoc kind blueprints are stored as.
const Name = "blueprint"

// Kind serves blueprints as configuration documents.
var Kind = configdoc.Kind[Spec]{
	Name:   Name,
	Plural: "blueprints",
	Parse:  Parse,
	Check: func(ctx context.Context, db configdoc.DB, spec Spec) ([]Issue, error) {
		published, err := Published(ctx, db)
		if err != nil {
			return nil, err
		}
		return Check(spec, published), nil
	},
	Diff:      Diff,
	Deletable: deletable,
}

func Register(app *gofr.App) {
	configdoc.Register(app, Kind)
	app.GET("/api/v1/blueprints/{identifier}/references", references)
}

// Reference is a relation on a published blueprint that points at another.
type Reference struct {
	Blueprint     string `json:"blueprint"`
	Title         string `json:"title"`
	Property      string `json:"property"`
	PropertyTitle string `json:"propertyTitle"`
	Many          bool   `json:"many"`
}

// references lists the relations that point at a blueprint, so its page can
// show what depends on it.
func references(ctx *gofr.Context) (any, error) {
	if _, err := auth.Require(ctx, auth.RoleMember); err != nil {
		return nil, err
	}
	published, err := Published(ctx, ctx.SQL)
	if err != nil {
		return nil, err
	}

	identifier := ctx.PathParam("identifier")
	refs := []Reference{}
	for _, spec := range published {
		for _, prop := range spec.Properties {
			if prop.Relation == identifier {
				refs = append(refs, Reference{Blueprint: spec.Identifier, Title: spec.Title, Property: prop.ID,
					PropertyTitle: prop.Title, Many: prop.Many})
			}
		}
	}
	slices.SortFunc(refs, func(a, b Reference) int {
		return strings.Compare(a.Blueprint+"."+a.Property, b.Blueprint+"."+b.Property)
	})

	return refs, nil
}

// Published returns every published blueprint, by identifier. Sources that
// no longer parse are left out.
func Published(ctx context.Context, db configdoc.DB) (map[string]Spec, error) {
	sources, err := configdoc.NewStore(db, Name).PublishedSources(ctx)
	if err != nil {
		return nil, err
	}
	specs := map[string]Spec{}
	for identifier, source := range sources {
		if spec, issues := Parse(source); !HasErrors(issues) {
			specs[identifier] = spec
		}
	}

	return specs, nil
}

// deletable refuses while another published blueprint relates to this one.
func deletable(ctx context.Context, db configdoc.DB, identifier string) error {
	published, err := Published(ctx, db)
	if err != nil {
		return err
	}
	var related []string
	for other, spec := range published {
		if other != identifier && spec.RelatesTo(identifier) {
			related = append(related, other)
		}
	}
	if len(related) == 0 {
		return nil
	}
	slices.Sort(related)

	return httpx.Error{Code: http.StatusConflict, Message: "remove the relations to it from " + joinWords(related) + " first"}
}
