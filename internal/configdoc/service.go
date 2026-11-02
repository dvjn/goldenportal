package configdoc

import "gofr.dev/pkg/gofr"

// Documents is the service for one kind's documents, whatever its spec
// type. The web API and MCP both call it, and it checks the caller's role
// itself. Get and Validate return the kind's own spec type.
type Documents interface {
	List(ctx *gofr.Context) ([]Summary, error)
	Get(ctx *gofr.Context, identifier string) (any, error)
	Validate(ctx *gofr.Context, source string) (any, error)
	SaveDraft(ctx *gofr.Context, identifier, source string) (Saved, error)
	DiscardDraft(ctx *gofr.Context, identifier string) error
	Impact(ctx *gofr.Context, identifier string) (Impact, error)
	Publish(ctx *gofr.Context, identifier string, acknowledgeBreaking bool) (Published, error)
	Versions(ctx *gofr.Context, identifier string) ([]Version, error)
	Version(ctx *gofr.Context, identifier string, n int) (Version, error)
	Export(ctx *gofr.Context, identifier string, draft bool) (string, error)
	Delete(ctx *gofr.Context, identifier, confirm string) error
}

// DocumentsOf returns the service for a registered kind, by its plural
// name, such as blueprints.
func DocumentsOf(plural string) (Documents, bool) {
	for _, k := range kinds {
		if d, ok := k.(Documents); ok && k.plural() == plural {
			return d, true
		}
	}

	return nil, false
}
