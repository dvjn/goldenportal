package catalog

import (
	"cmp"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/dvjn/goldport/internal/definition"
	"github.com/dvjn/goldport/internal/httpx"
	"gofr.dev/pkg/gofr"
)

const (
	defaultChangeLimit = 10
	maxChangeLimit     = 50
)

type propertyView struct {
	Key         string                  `json:"key"`
	Title       string                  `json:"title"`
	Description string                  `json:"description,omitempty"`
	Type        definition.PropertyType `json:"type"`
	Format      definition.Format       `json:"format,omitempty"`
	Enum        []string                `json:"enum,omitempty"`
	Display     string                  `json:"display"`
	EnumColors  map[string]string       `json:"enum_colors,omitempty"`
	EnumLabels  map[string]string       `json:"enum_labels,omitempty"`
	LinkLabel   string                  `json:"link_label,omitempty"`
	EmptyLabel  string                  `json:"empty_label,omitempty"`
	Hidden      bool                    `json:"hidden"`
	Sortable    bool                    `json:"sortable"`
	FilterKind  definition.FilterKind   `json:"filter_kind,omitempty"`
}

func newPropertyView(key string, property definition.Property) propertyView {
	return propertyView{
		Key: key, Title: property.Title, Description: property.Description,
		Type: property.Type, Format: property.Format, Enum: property.Enum,
		Display: property.DisplayOrDefault(), EnumColors: property.EnumColors, EnumLabels: property.EnumLabels,
		LinkLabel: property.LinkLabel, EmptyLabel: property.EmptyLabel, Hidden: property.Hidden,
		Sortable:   property.IsSortable(),
		FilterKind: definition.Attribute{Property: &property}.FilterKind(),
	}
}

type relationView struct {
	Key          string `json:"key"`
	Title        string `json:"title"`
	Description  string `json:"description,omitempty"`
	Target       string `json:"target"`
	TargetTitle  string `json:"target_title"`
	Many         bool   `json:"many"`
	Inverse      string `json:"inverse,omitempty"`
	InverseTitle string `json:"inverse_title,omitempty"`
}

// inverseView is a relation of another blueprint that links to this one.
// Key is set when the relation names its inverse, which makes it usable
// in columns, filters and derived paths.
type inverseView struct {
	Key         string `json:"key,omitempty"`
	Source      string `json:"source"`
	SourceTitle string `json:"source_title"`
	Relation    string `json:"relation"`
	Title       string `json:"title"`
}

// derivedView is a value read through relations. Property describes the
// value when the path ends in a property; otherwise the value lists the
// entities of Target the path reaches.
type derivedView struct {
	Key         string        `json:"key"`
	Title       string        `json:"title"`
	Description string        `json:"description,omitempty"`
	Path        string        `json:"path"`
	Target      string        `json:"target"`
	TargetTitle string        `json:"target_title"`
	Many        bool          `json:"many"`
	EmptyLabel  string        `json:"empty_label,omitempty"`
	Sortable    bool          `json:"sortable"`
	Property    *propertyView `json:"property,omitempty"`
}

type fedByView struct {
	Identifier string               `json:"identifier"`
	Title      string               `json:"title"`
	Connector  definition.Connector `json:"connector"`
}

type blueprintView struct {
	Identifier  string             `json:"identifier"`
	Title       string             `json:"title"`
	Description string             `json:"description,omitempty"`
	Icon        string             `json:"icon,omitempty"`
	EntityCount int                `json:"entity_count"`
	Properties  []propertyView     `json:"properties"`
	Relations   []relationView     `json:"relations"`
	Inverses    []inverseView      `json:"inverses"`
	Derived     []derivedView      `json:"derived"`
	Display     definition.Display `json:"display"`
	Table       definition.Table   `json:"table"`
	Columns     []string           `json:"columns"`
	FedBy       []fedByView        `json:"fed_by"`
}

func newBlueprintView(set definition.Set, blueprint *definition.Blueprint, count int) blueprintView {
	table := set.TableOf(blueprint)
	view := blueprintView{
		Identifier:  blueprint.Identifier,
		Title:       blueprint.Title,
		Description: blueprint.Description,
		Icon:        blueprint.Icon,
		EntityCount: count,
		Properties:  []propertyView{},
		Relations:   []relationView{},
		Inverses:    []inverseView{},
		Derived:     []derivedView{},
		Display: definition.Display{
			Subtitle: blueprint.Display.Subtitle,
			Badges:   nonNil(blueprint.Display.Badges),
			Groups:   nonNil(blueprint.Display.Groups),
		},
		Table:   table,
		Columns: table.Columns,
		FedBy:   fedBy(set, blueprint.Identifier),
	}
	for key, property := range blueprint.Properties.All() {
		view.Properties = append(view.Properties, newPropertyView(key, property))
	}
	for key, relation := range blueprint.Relations.All() {
		target, _ := set.Blueprint(relation.Target)
		view.Relations = append(view.Relations, relationView{
			Key: key, Title: relation.Title, Description: relation.Description,
			Target: relation.Target, TargetTitle: target.Title, Many: relation.Many,
			Inverse: relation.Inverse, InverseTitle: relation.InverseTitle,
		})
	}
	for _, source := range set.Blueprints() {
		for key, relation := range source.Relations.All() {
			if relation.Target == blueprint.Identifier {
				view.Inverses = append(view.Inverses, inverseView{
					Key: relation.Inverse, Source: source.Identifier, SourceTitle: source.Title, Relation: key,
					Title: cmp.Or(relation.InverseTitle, fmt.Sprintf("%s (%s)", source.Title, relation.Title)),
				})
			}
		}
	}
	for key, derived := range blueprint.Derived.All() {
		attribute, ok := set.Attribute(blueprint, key)
		if !ok {
			continue
		}
		target := attribute.Path.Target(blueprint.Identifier)
		targetBlueprint, _ := set.Blueprint(target)
		item := derivedView{
			Key: key, Title: attribute.Title, Description: derived.Description, Path: derived.Path,
			Target: target, TargetTitle: targetBlueprint.Title, Many: reachesMany(set, attribute.Path),
			EmptyLabel: derived.EmptyLabel, Sortable: attribute.Sortable(),
		}
		if attribute.Property != nil {
			property := newPropertyView(attribute.Path.Property, *attribute.Property)
			item.Property = &property
		}
		view.Derived = append(view.Derived, item)
	}

	return view
}

// reachesMany reports whether the path can reach more than one entity.
func reachesMany(set definition.Set, path definition.Path) bool {
	return slices.ContainsFunc(path.Hops, func(hop definition.Hop) bool {
		if hop.Inverse {
			return true
		}
		from, _ := set.Blueprint(hop.From)
		relation, _ := from.Relations.Get(hop.Relation)
		return relation.Many
	})
}

// fedBy lists the integrations whose mappings write entities of the
// blueprint.
func fedBy(set definition.Set, blueprint string) []fedByView {
	feeds := []fedByView{}
	for _, integration := range set.Integrations() {
		if slices.ContainsFunc(integration.Mappings, func(mapping definition.Mapping) bool { return mapping.Blueprint == blueprint }) {
			feeds = append(feeds, fedByView{Identifier: integration.Identifier, Title: integration.Title, Connector: integration.Connector})
		}
	}

	return feeds
}

type blueprintsResponse struct {
	Blueprints []blueprintView `json:"blueprints"`
}

func (s *Service) listBlueprints(ctx *gofr.Context) (any, error) {
	set, _, err := s.definitions.Definitions(ctx)
	if err != nil {
		return nil, err
	}
	counts, err := s.entities.BlueprintCounts(ctx)
	if err != nil {
		return nil, err
	}
	response := blueprintsResponse{Blueprints: []blueprintView{}}
	for _, blueprint := range set.Blueprints() {
		response.Blueprints = append(response.Blueprints, newBlueprintView(set, blueprint, counts[blueprint.Identifier]))
	}

	return response, nil
}

func (s *Service) getBlueprint(ctx *gofr.Context) (any, error) {
	set, blueprint, err := s.blueprint(ctx)
	if err != nil {
		return nil, err
	}
	counts, err := s.entities.BlueprintCounts(ctx)

	return newBlueprintView(set, blueprint, counts[blueprint.Identifier]), err
}

// blueprint returns the current definitions and the blueprint the path
// names.
func (s *Service) blueprint(ctx *gofr.Context) (definition.Set, *definition.Blueprint, error) {
	set, _, err := s.definitions.Definitions(ctx)
	if err != nil {
		return definition.Set{}, nil, err
	}
	blueprint, ok := set.Blueprint(ctx.PathParam("blueprint"))
	if !ok {
		return definition.Set{}, nil, httpx.Error{Code: http.StatusNotFound, Message: "no blueprint has that identifier"}
	}

	return set, blueprint, nil
}

type integrationView struct {
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
	Manual     bool   `json:"manual"`
}

// integrationOf names the integration that owns entities. Manual is built in
// and no document defines it.
func integrationOf(set definition.Set, identifier string) integrationView {
	if identifier == definition.Manual {
		return integrationView{Identifier: identifier, Title: definition.ManualIntegration().Title, Manual: true}
	}
	integration, ok := set.Integration(identifier)
	if !ok {
		return integrationView{Identifier: identifier, Title: identifier}
	}

	return integrationView{Identifier: identifier, Title: integration.Title}
}

type blueprintRef struct {
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
	Icon       string `json:"icon,omitempty"`
}

type changeView struct {
	Blueprint   blueprintRef    `json:"blueprint"`
	Identifier  string          `json:"identifier"`
	Title       string          `json:"title"`
	Integration integrationView `json:"integration"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

type changesResponse struct {
	Changes []changeView `json:"changes"`
}

func (s *Service) listChanges(ctx *gofr.Context) (any, error) {
	set, _, err := s.definitions.Definitions(ctx)
	if err != nil {
		return nil, err
	}
	entities, err := s.entities.Changes(ctx, clamp(ctx.Param("limit"), defaultChangeLimit, 1, maxChangeLimit))
	if err != nil {
		return nil, err
	}
	response := changesResponse{Changes: []changeView{}}
	for _, entity := range entities {
		ref := blueprintRef{Identifier: entity.Blueprint, Title: entity.Blueprint}
		if blueprint, ok := set.Blueprint(entity.Blueprint); ok {
			ref = blueprintRef{Identifier: blueprint.Identifier, Title: blueprint.Title, Icon: blueprint.Icon}
		}
		response.Changes = append(response.Changes, changeView{
			Blueprint:   ref,
			Identifier:  entity.Identifier,
			Title:       entity.Title,
			Integration: integrationOf(set, entity.Integration),
			UpdatedAt:   entity.UpdatedAt,
		})
	}

	return response, nil
}
