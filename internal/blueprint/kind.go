package blueprint

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"slices"
	"strings"

	"github.com/lib/pq"

	"gofr.dev/pkg/gofr"

	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/httpx"
)

// Name is the configdoc kind blueprints are stored as.
const Name = "blueprint"

// Kind serves blueprints as configuration documents.
var Kind = configdoc.Kind[Spec]{
	Name:     Name,
	Document: DocumentKind,
	Plural:   "blueprints",
	Parse:    Parse,
	Check: func(ctx context.Context, db configdoc.DB, spec Spec) ([]Issue, error) {
		published, err := Published(ctx, db)
		if err != nil {
			return nil, err
		}
		return Check(spec, published), nil
	},
	Diff:      Diff,
	Deletable: deletable,
	Affected:  countEntities,
	Published: published,
}

func Register(app *gofr.App) { configdoc.Register(app, Kind) }

// Reference is a relation on a published blueprint that points at another.
type Reference struct {
	Blueprint     string `json:"blueprint"`
	Title         string `json:"title"`
	Property      string `json:"property"`
	PropertyTitle string `json:"propertyTitle"`
	Many          bool   `json:"many"`
}

// References lists the relations among published that point at identifier.
func References(published map[string]Spec, identifier string) []Reference {
	refs := []Reference{}
	for _, spec := range published {
		for _, prop := range spec.Properties {
			if prop.Relation == identifier {
				refs = append(refs, Reference{
					Blueprint: spec.Identifier, Title: spec.Title, Property: prop.ID,
					PropertyTitle: prop.Title, Many: prop.Many,
				})
			}
		}
	}
	slices.SortFunc(refs, func(a, b Reference) int {
		return strings.Compare(a.Blueprint+"."+a.Property, b.Blueprint+"."+b.Property)
	})

	return refs
}

// Published returns every published blueprint, by identifier, with any
// that ctx has pending in their place, so documents applied or published
// together see each other. Sources that don't parse are left out.
func Published(ctx context.Context, db configdoc.DB) (map[string]Spec, error) {
	sources, err := configdoc.NewStore(db, Name).PublishedSources(ctx)
	if err != nil {
		return nil, err
	}
	for name, source := range configdoc.PendingSources(ctx, DocumentKind) {
		sources[name] = source
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

func countEntities(ctx context.Context, db configdoc.DB, identifier string) (int, error) {
	var count int
	err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM entities e
		JOIN config_documents b ON b.id = e.blueprint_id
		WHERE b.kind = $1 AND b.identifier = $2`, Name, identifier).Scan(&count)

	return count, err
}

// published brings existing entities in line with a new version. Values of
// removed properties go, as do relations that now point elsewhere or at one
// entity instead of many, and computed properties are recomputed.
func published(ctx context.Context, db configdoc.DB, previous *Spec, next Spec) error {
	if previous == nil {
		return nil
	}

	removedProperties, removedRelations := []string{}, []string{}
	for _, prop := range previous.Properties {
		now, ok := next.property(prop.ID)
		switch {
		case prop.Derived():
		case prop.IsRelation() && (!ok || now.Relation != prop.Relation || (prop.Many && !now.Many)):
			removedRelations = append(removedRelations, prop.ID)
		case !prop.IsRelation() && (!ok || now.Derived() || now.IsRelation()):
			removedProperties = append(removedProperties, prop.ID)
		}
	}
	if len(removedProperties)+len(removedRelations) > 0 {
		if _, err := db.ExecContext(ctx, `
			UPDATE entities e SET properties = properties - $3::text[], relations = relations - $4::text[], updated_at = now()
			FROM config_documents b
			WHERE b.id = e.blueprint_id AND b.kind = $1 AND b.identifier = $2`,
			Name, next.Identifier, pq.Array(removedProperties), pq.Array(removedRelations)); err != nil {
			return err
		}
	}

	if reflect.DeepEqual(previous.computations(), next.computations()) && len(removedProperties)+len(removedRelations) == 0 {
		return nil
	}

	return recompute(ctx, db, next)
}

// recompute recomputes the computed properties of every entity of spec.
func recompute(ctx context.Context, db configdoc.DB, spec Spec) error {
	rows, err := db.QueryContext(ctx, `
		SELECT e.id, e.identifier, e.title, e.properties, e.relations FROM entities e
		JOIN config_documents b ON b.id = e.blueprint_id
		WHERE b.kind = $1 AND b.identifier = $2`, Name, spec.Identifier)
	if err != nil {
		return err
	}

	type update struct {
		id       string
		computed []byte
	}
	var updates []update
	for rows.Next() {
		var (
			id                    string
			entity                Entity
			properties, relations []byte
		)
		if err := rows.Scan(&id, &entity.Identifier, &entity.Title, &properties, &relations); err != nil {
			rows.Close()
			return err
		}
		_ = json.Unmarshal(properties, &entity.Properties)
		_ = json.Unmarshal(relations, &entity.Relations)
		values, _ := spec.Compute(entity)
		computed, _ := json.Marshal(values)
		updates = append(updates, update{id: id, computed: computed})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, u := range updates {
		if _, err := db.ExecContext(ctx, `UPDATE entities SET computed = $2 WHERE id = $1`, u.id, u.computed); err != nil {
			return err
		}
	}

	return nil
}

// computations maps each computed property to its expression and type.
func (s Spec) computations() map[string]string {
	out := map[string]string{}
	for _, prop := range s.Properties {
		if prop.Compute != "" {
			out[prop.ID] = prop.Type + ":" + prop.Format + ":" + prop.Compute
		}
	}

	return out
}
