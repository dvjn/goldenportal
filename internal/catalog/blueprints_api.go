package catalog

import (
	"cmp"
	"fmt"
	"net/http"

	"github.com/dvjn/goldport/internal/definition"
	"github.com/dvjn/goldport/internal/httpx"
	"gofr.dev/pkg/gofr"
)

type propertyView struct {
	Key         string                  `json:"key"`
	Title       string                  `json:"title"`
	Description string                  `json:"description,omitempty"`
	Type        definition.PropertyType `json:"type"`
	Format      definition.Format       `json:"format,omitempty"`
	Enum        []string                `json:"enum,omitempty"`
}

type relationView struct {
	Key         string `json:"key"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Target      string `json:"target"`
	TargetTitle string `json:"target_title"`
	Many        bool   `json:"many"`
}

// inverseView is a relation of another blueprint that links to this one.
type inverseView struct {
	Source      string `json:"source"`
	SourceTitle string `json:"source_title"`
	Relation    string `json:"relation"`
	Title       string `json:"title"`
}

type blueprintView struct {
	Identifier  string         `json:"identifier"`
	Title       string         `json:"title"`
	Description string         `json:"description,omitempty"`
	Icon        string         `json:"icon,omitempty"`
	EntityCount int            `json:"entity_count"`
	Properties  []propertyView `json:"properties"`
	Relations   []relationView `json:"relations"`
	Inverses    []inverseView  `json:"inverses"`
	Columns     []string       `json:"columns"`
}

func newBlueprintView(set definition.Set, blueprint *definition.Blueprint, count int) blueprintView {
	view := blueprintView{
		Identifier:  blueprint.Identifier,
		Title:       blueprint.Title,
		Description: blueprint.Description,
		Icon:        blueprint.Icon,
		EntityCount: count,
		Properties:  []propertyView{},
		Relations:   []relationView{},
		Inverses:    []inverseView{},
		Columns:     nonNil(blueprint.Columns()),
	}
	for key, property := range blueprint.Properties.All() {
		view.Properties = append(view.Properties, propertyView{
			Key: key, Title: property.Title, Description: property.Description,
			Type: property.Type, Format: property.Format, Enum: property.Enum,
		})
	}
	for key, relation := range blueprint.Relations.All() {
		target, _ := set.Blueprint(relation.Target)
		view.Relations = append(view.Relations, relationView{
			Key: key, Title: relation.Title, Description: relation.Description,
			Target: relation.Target, TargetTitle: target.Title, Many: relation.Many,
		})
	}
	for _, source := range set.Blueprints() {
		for key, relation := range source.Relations.All() {
			if relation.Target == blueprint.Identifier {
				view.Inverses = append(view.Inverses, inverseView{
					Source: source.Identifier, SourceTitle: source.Title, Relation: key,
					Title: cmp.Or(relation.InverseTitle, fmt.Sprintf("%s (%s)", source.Title, relation.Title)),
				})
			}
		}
	}

	return view
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
