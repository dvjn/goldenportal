package catalog

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/dvjn/goldport/internal/audit"
	"github.com/dvjn/goldport/internal/auth"
	"github.com/dvjn/goldport/internal/definition"
	"github.com/dvjn/goldport/internal/httpx"
	"gofr.dev/pkg/gofr"
)

const (
	defaultEntityLimit = 25
	maxEntityLimit     = 100
	maxTitleLength     = 200
)

var (
	entityIdentifierPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

	errEntityMissing = httpx.Error{Code: http.StatusNotFound, Message: "no entity of this blueprint has that identifier"}
	errEntityExists  = httpx.Error{
		Code:    http.StatusConflict,
		Message: "an entity of this blueprint already has that identifier, choose another",
	}
)

// fieldsError answers an entity write with invalid fields, by field:
// identifier, title, properties.<key> or relations.<key>.
type fieldsError struct {
	fields map[string]string
}

func (e fieldsError) Error() string {
	return "the entity has invalid fields, fix them and save again"
}

func (e fieldsError) StatusCode() int { return http.StatusUnprocessableEntity }

func (e fieldsError) Response() map[string]any { return map[string]any{"fields": e.fields} }

func managedError(integration integrationView) httpx.Error {
	return httpx.Error{
		Code:    http.StatusConflict,
		Message: fmt.Sprintf("the %s integration manages this entity, change it at its source", integration.Title),
	}
}

type entitySummary struct {
	Identifier  string          `json:"identifier"`
	Title       string          `json:"title"`
	Integration integrationView `json:"integration"`
	Properties  map[string]any  `json:"properties"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

type integrationCount struct {
	integrationView
	Count int `json:"count"`
}

type entitiesResponse struct {
	Entities     []entitySummary    `json:"entities"`
	Offset       int                `json:"offset"`
	Limit        int                `json:"limit"`
	Total        int                `json:"total"`
	Integrations []integrationCount `json:"integrations"`
}

func (s *Service) listEntities(ctx *gofr.Context) (any, error) {
	set, blueprint, err := s.blueprint(ctx)
	if err != nil {
		return nil, err
	}
	page := Page{
		Offset: clamp(ctx.Param("offset"), 0, 0, math.MaxInt),
		Limit:  clamp(ctx.Param("limit"), defaultEntityLimit, 1, maxEntityLimit),
	}
	filter := EntityFilter{Blueprint: blueprint.Identifier, Search: strings.TrimSpace(ctx.Param("q")), Integration: ctx.Param("integration")}
	entities, total, err := s.entities.Entities(ctx, filter, page)
	if err != nil {
		return nil, err
	}
	counts, err := s.entities.IntegrationCounts(ctx, blueprint.Identifier)
	if err != nil {
		return nil, err
	}

	response := entitiesResponse{Entities: []entitySummary{}, Offset: page.Offset, Limit: page.Limit, Total: total, Integrations: []integrationCount{}}
	for _, entity := range entities {
		response.Entities = append(response.Entities, entitySummary{
			Identifier:  entity.Identifier,
			Title:       entity.Title,
			Integration: integrationOf(set, entity.Integration),
			Properties:  entity.Properties,
			UpdatedAt:   entity.UpdatedAt,
		})
	}
	for _, count := range counts {
		response.Integrations = append(response.Integrations, integrationCount{integrationView: integrationOf(set, count.Integration), Count: count.Count})
	}

	return response, nil
}

type linkGroup struct {
	Key      string       `json:"key"`
	Title    string       `json:"title"`
	Target   string       `json:"target"`
	Many     bool         `json:"many"`
	Entities []EntityName `json:"entities"`
}

type inverseGroup struct {
	inverseView
	Entities []EntityName `json:"entities"`
}

type entityResponse struct {
	Blueprint   string          `json:"blueprint"`
	Identifier  string          `json:"identifier"`
	Title       string          `json:"title"`
	Properties  map[string]any  `json:"properties"`
	Integration integrationView `json:"integration"`
	Relations   []linkGroup     `json:"relations"`
	Incoming    []inverseGroup  `json:"incoming"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

func (s *Service) getEntity(ctx *gofr.Context) (any, error) {
	set, blueprint, err := s.blueprint(ctx)
	if err != nil {
		return nil, err
	}
	entity, err := s.entity(ctx, blueprint)
	if err != nil {
		return nil, err
	}
	incoming, err := s.entities.Incoming(ctx, entity.Name())
	if err != nil {
		return nil, err
	}

	return s.entityResponse(set, blueprint, entity, incoming), nil
}

func (s *Service) entityResponse(set definition.Set, blueprint *definition.Blueprint, entity Entity, incoming []Link) entityResponse {
	view := newBlueprintView(set, blueprint, 0)
	response := entityResponse{
		Blueprint:   entity.Blueprint,
		Identifier:  entity.Identifier,
		Title:       entity.Title,
		Properties:  entity.Properties,
		Integration: integrationOf(set, entity.Integration),
		Relations:   []linkGroup{},
		Incoming:    []inverseGroup{},
		CreatedAt:   entity.CreatedAt,
		UpdatedAt:   entity.UpdatedAt,
	}
	for _, relation := range view.Relations {
		group := linkGroup{Key: relation.Key, Title: relation.Title, Target: relation.Target, Many: relation.Many, Entities: []EntityName{}}
		for _, link := range entity.Links {
			if link.Relation == relation.Key {
				group.Entities = append(group.Entities, link.Target)
			}
		}
		response.Relations = append(response.Relations, group)
	}
	for _, inverse := range view.Inverses {
		group := inverseGroup{inverseView: inverse, Entities: []EntityName{}}
		for _, link := range incoming {
			if link.Source.Blueprint == inverse.Source && link.Relation == inverse.Relation {
				group.Entities = append(group.Entities, link.Source)
			}
		}
		if len(group.Entities) > 0 {
			response.Incoming = append(response.Incoming, group)
		}
	}

	return response
}

func (s *Service) entity(ctx *gofr.Context, blueprint *definition.Blueprint) (Entity, error) {
	entity, err := s.entities.Entity(ctx, blueprint.Identifier, ctx.PathParam("identifier"))
	if errors.Is(err, ErrNotFound) {
		return Entity{}, errEntityMissing
	}

	return entity, err
}

// entityRequest is a manual entity as people write it. Relations holds the
// target identifiers of each relation.
type entityRequest struct {
	Identifier string              `json:"identifier"`
	Title      string              `json:"title"`
	Properties map[string]any      `json:"properties"`
	Relations  map[string][]string `json:"relations"`
}

func (s *Service) createEntity(ctx *gofr.Context) (any, error) {
	set, blueprint, err := s.blueprint(ctx)
	if err != nil {
		return nil, err
	}
	var request entityRequest
	if err := ctx.Bind(&request); err != nil {
		return nil, errInvalidBody
	}
	entity, err := s.validEntity(ctx, set, blueprint, request)
	if err != nil {
		return nil, err
	}
	created, err := s.entities.CreateEntity(ctx, entity)
	if errors.Is(err, ErrExists) {
		return nil, errEntityExists
	}
	if err != nil {
		return nil, err
	}
	if err := s.recordEntity(ctx, audit.EntityCreated, created, nil); err != nil {
		return nil, err
	}

	return s.written(ctx, set, blueprint, created)
}

func (s *Service) updateEntity(ctx *gofr.Context) (any, error) {
	set, blueprint, err := s.blueprint(ctx)
	if err != nil {
		return nil, err
	}
	previous, err := s.manualEntity(ctx, set, blueprint)
	if err != nil {
		return nil, err
	}
	var request entityRequest
	if err := ctx.Bind(&request); err != nil {
		return nil, errInvalidBody
	}
	request.Identifier = previous.Identifier
	entity, err := s.validEntity(ctx, set, blueprint, request)
	if err != nil {
		return nil, err
	}
	updated, err := s.entities.UpdateEntity(ctx, entity)
	if errors.Is(err, ErrNotFound) {
		return nil, errEntityMissing
	}
	if err != nil {
		return nil, err
	}
	if err := s.recordEntity(ctx, audit.EntityUpdated, updated, changedFields(previous, updated)); err != nil {
		return nil, err
	}

	return s.written(ctx, set, blueprint, updated)
}

func (s *Service) deleteEntity(ctx *gofr.Context) (any, error) {
	set, blueprint, err := s.blueprint(ctx)
	if err != nil {
		return nil, err
	}
	entity, err := s.manualEntity(ctx, set, blueprint)
	if err != nil {
		return nil, err
	}
	err = s.entities.DeleteEntity(ctx, entity.Blueprint, entity.Identifier)
	if errors.Is(err, ErrNotFound) {
		return nil, errEntityMissing
	}
	if err != nil {
		return nil, err
	}

	return nil, s.recordEntity(ctx, audit.EntityDeleted, entity, nil)
}

// manualEntity returns the entity the path names, refusing one that an
// integration owns.
func (s *Service) manualEntity(ctx *gofr.Context, set definition.Set, blueprint *definition.Blueprint) (Entity, error) {
	entity, err := s.entity(ctx, blueprint)
	if err != nil {
		return Entity{}, err
	}
	if entity.Integration != definition.Manual {
		return Entity{}, managedError(integrationOf(set, entity.Integration))
	}

	return entity, nil
}

// written reads the entity back, so its links carry their targets' titles.
func (s *Service) written(ctx *gofr.Context, set definition.Set, blueprint *definition.Blueprint, written Entity) (any, error) {
	entity, err := s.entities.Entity(ctx, written.Blueprint, written.Identifier)
	if err != nil {
		return nil, err
	}
	incoming, err := s.entities.Incoming(ctx, entity.Name())
	if err != nil {
		return nil, err
	}

	return s.entityResponse(set, blueprint, entity, incoming), nil
}

// validEntity checks the request against the blueprint and returns the
// manual entity it describes, with every link's target title.
func (s *Service) validEntity(ctx *gofr.Context, set definition.Set, blueprint *definition.Blueprint, request entityRequest) (Entity, error) {
	fields := map[string]string{}
	if !entityIdentifierPattern.MatchString(request.Identifier) {
		fields["identifier"] = "use letters, digits, dots, hyphens and underscores, starting with a letter or digit, at most 128"
	}
	title := strings.TrimSpace(request.Title)
	if title == "" || len(title) > maxTitleLength {
		fields["title"] = "give the entity a title of at most 200 characters"
	}
	properties := maps.Clone(request.Properties)
	maps.DeleteFunc(properties, func(_ string, value any) bool { return value == nil })
	for key, problem := range blueprint.CheckValues(properties) {
		fields["properties."+key] = problem
	}
	entity := Entity{
		Blueprint:   blueprint.Identifier,
		Identifier:  request.Identifier,
		Title:       title,
		Properties:  orEmpty(properties),
		Integration: definition.Manual,
	}
	if err := s.validLinks(ctx, set, blueprint, request, &entity, fields); err != nil {
		return Entity{}, err
	}
	if len(fields) > 0 {
		return Entity{}, fieldsError{fields: fields}
	}

	return entity, nil
}

func orEmpty(properties map[string]any) map[string]any {
	if properties == nil {
		return map[string]any{}
	}

	return properties
}

func (s *Service) validLinks(ctx *gofr.Context, set definition.Set, blueprint *definition.Blueprint, request entityRequest, entity *Entity, fields map[string]string) error {
	for _, key := range slices.Sorted(maps.Keys(request.Relations)) {
		targets := slices.Compact(slices.Sorted(slices.Values(request.Relations[key])))
		relation, ok := blueprint.Relations.Get(key)
		if !ok {
			fields["relations."+key] = fmt.Sprintf("%s is not a relation of %s", key, blueprint.Title)
			continue
		}
		target, _ := set.Blueprint(relation.Target)
		problem, err := s.linkProblem(ctx, blueprint, key, relation, target, entity.Identifier, targets)
		if err != nil {
			return err
		}
		if problem != "" {
			fields["relations."+key] = problem
			continue
		}
		for _, identifier := range targets {
			entity.Links = append(entity.Links, Link{
				Relation: key,
				Source:   entity.Name(),
				Target:   EntityName{Blueprint: relation.Target, Identifier: identifier},
			})
		}
	}

	return nil
}

func (s *Service) linkProblem(ctx *gofr.Context, blueprint *definition.Blueprint, key string, relation definition.Relation, target *definition.Blueprint, identifier string, targets []string) (string, error) {
	selfRelation := relation.Target == blueprint.Identifier
	switch {
	case len(targets) > 1 && !relation.Many:
		return fmt.Sprintf("%s links to one %s", relation.Title, target.Title), nil
	case selfRelation && slices.Contains(targets, identifier):
		return "an entity cannot link to itself", nil
	}
	missing, err := s.entities.Missing(ctx, relation.Target, targets)
	if err != nil || len(missing) > 0 {
		return fmt.Sprintf("no %s has the identifier %s", target.Title, strings.Join(missing, ", ")), err
	}
	if !selfRelation || relation.Many || len(targets) == 0 {
		return "", nil
	}
	loops, err := s.entities.Reaches(ctx, blueprint.Identifier, key, targets[0], identifier)
	if loops {
		return fmt.Sprintf("%s already links back to this entity through %s, links cannot form a loop", targets[0], relation.Title), err
	}

	return "", err
}

func (s *Service) recordEntity(ctx *gofr.Context, action audit.Action, entity Entity, changed []string) error {
	principal, _ := auth.PrincipalFrom(ctx)
	details := map[string]any{"blueprint": entity.Blueprint}
	if changed != nil {
		details["changed"] = changed
	}

	return s.audit.Record(ctx, audit.Entry{
		Actor:    principal.Actor(),
		Action:   action,
		Resource: audit.Resource{Kind: audit.ResourceEntity, ID: entity.Blueprint + "/" + entity.Identifier, Name: entity.Title},
		Details:  details,
	})
}

// changedFields names the fields an update changed: title, a property key
// or a relation key.
func changedFields(previous, updated Entity) []string {
	changed := []string{}
	if previous.Title != updated.Title {
		changed = append(changed, "title")
	}
	for _, key := range unionKeys(previous.Properties, updated.Properties) {
		if fmt.Sprint(previous.Properties[key]) != fmt.Sprint(updated.Properties[key]) {
			changed = append(changed, key)
		}
	}
	before, after := targetsByRelation(previous), targetsByRelation(updated)
	for _, key := range unionKeys(before, after) {
		if !slices.Equal(before[key], after[key]) {
			changed = append(changed, key)
		}
	}

	return changed
}

func targetsByRelation(entity Entity) map[string][]string {
	targets := map[string][]string{}
	for _, link := range entity.Links {
		targets[link.Relation] = append(targets[link.Relation], link.Target.Identifier)
	}
	for relation := range targets {
		slices.Sort(targets[relation])
	}

	return targets
}

func unionKeys[V any](first, second map[string]V) []string {
	keys := slices.Collect(maps.Keys(first))
	keys = slices.AppendSeq(keys, maps.Keys(second))
	slices.Sort(keys)

	return slices.Compact(keys)
}
