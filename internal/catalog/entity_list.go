package catalog

import (
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dvjn/goldport/internal/definition"
	"github.com/dvjn/goldport/internal/httpx"
	"gofr.dev/pkg/gofr"
)

// entitySummary is one row of the entity table. Relations holds the
// entities each relation and inverse links, by key; Derived holds what each
// derived value reaches, by key.
type entitySummary struct {
	Identifier  string                  `json:"identifier"`
	Title       string                  `json:"title"`
	Integration integrationView         `json:"integration"`
	Properties  map[string]any          `json:"properties"`
	Relations   map[string][]EntityName `json:"relations"`
	Derived     map[string][]Reached    `json:"derived"`
	UpdatedAt   time.Time               `json:"updated_at"`
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
	Sort         string             `json:"sort"`
	Dir          string             `json:"dir"`
	Integrations []integrationCount `json:"integrations"`
}

func badRequest(format string, args ...any) error {
	return httpx.Error{Code: http.StatusBadRequest, Message: fmt.Sprintf(format, args...)}
}

func (s *Service) listEntities(ctx *gofr.Context) (any, error) {
	set, blueprint, err := s.blueprint(ctx)
	if err != nil {
		return nil, err
	}
	limit := clamp(ctx.Param("limit"), defaultEntityLimit, 1, maxEntityLimit)
	page := Page{Offset: clamp(ctx.Param("offset"), 0, 0, math.MaxInt), Limit: limit}
	if number := ctx.Param("page"); number != "" {
		page.Offset = (clamp(number, 1, 1, math.MaxInt32) - 1) * limit
	}
	filter := EntityFilter{Blueprint: blueprint.Identifier, Search: strings.TrimSpace(ctx.Param("q")), Integration: ctx.Param("integration")}
	if filter.Sort, err = entitySort(ctx, set, blueprint); err != nil {
		return nil, err
	}
	if filter.Conditions, err = filterConditions(ctx, set, blueprint); err != nil {
		return nil, err
	}
	entities, total, err := s.entities.Entities(ctx, filter, page)
	if err != nil {
		return nil, err
	}
	counts, err := s.entities.IntegrationCounts(ctx, blueprint.Identifier)
	if err != nil {
		return nil, err
	}
	rows, err := s.summaries(ctx, set, blueprint, entities)
	if err != nil {
		return nil, err
	}

	response := entitiesResponse{
		Entities: rows, Offset: page.Offset, Limit: page.Limit, Total: total,
		Sort: filter.Sort.By, Dir: definition.SortAscending, Integrations: []integrationCount{},
	}
	if filter.Sort.Descending {
		response.Dir = definition.SortDescending
	}
	for _, count := range counts {
		response.Integrations = append(response.Integrations, integrationCount{integrationView: integrationOf(set, count.Integration), Count: count.Count})
	}

	return response, nil
}

func entitySort(ctx *gofr.Context, set definition.Set, blueprint *definition.Blueprint) (EntitySort, error) {
	table := set.TableOf(blueprint)
	by, direction := table.Sort.By, table.Sort.Direction
	if requested := ctx.Param("sort"); requested != "" {
		by, direction = requested, definition.SortAscending
	}
	if requested := ctx.Param("dir"); requested != "" {
		direction = requested
	}
	if direction != definition.SortAscending && direction != definition.SortDescending {
		return EntitySort{}, badRequest("dir must be asc or desc")
	}
	sort := EntitySort{By: by, Descending: direction == definition.SortDescending}
	if by == definition.SortTitle || by == definition.SortUpdatedAt {
		return sort, nil
	}
	attribute, ok := set.Attribute(blueprint, by)
	if !ok || !attribute.Sortable() {
		return EntitySort{}, badRequest("%s cannot sort by %q", blueprint.Title, by)
	}
	sort.Path = attribute.Path

	return sort, nil
}

// filterKeys are the keys a request can filter the blueprint's entities by.
func filterKeys(set definition.Set, blueprint *definition.Blueprint) []string {
	keys := blueprint.Properties.Keys()
	keys = append(keys, blueprint.Relations.Keys()...)
	for _, inverse := range set.Inverses(blueprint.Identifier) {
		keys = append(keys, inverse.Key)
	}

	return append(keys, blueprint.Derived.Keys()...)
}

// filterConditions reads filter[key]=a,b (one of), filter[key][gte] and
// filter[key][lte] (bounds of a date or number), and
// filter[key][empty]=true|false.
func filterConditions(ctx *gofr.Context, set definition.Set, blueprint *definition.Blueprint) ([]Condition, error) {
	var conditions []Condition
	for _, key := range filterKeys(set, blueprint) {
		name := "filter[" + key + "]"
		in := nonEmpty(ctx.Params(name))
		empty, lowest, highest := ctx.Param(name+"[empty]"), ctx.Param(name+"[gte]"), ctx.Param(name+"[lte]")
		if in == nil && empty == "" && lowest == "" && highest == "" {
			continue
		}
		attribute, ok := set.Attribute(blueprint, key)
		kind := attribute.FilterKind()
		if !ok || kind == "" {
			return nil, badRequest("%s cannot filter by %q", blueprint.Title, key)
		}
		condition, err := newCondition(attribute.Path, kind, in, empty, lowest, highest)
		if err != nil {
			return nil, badRequest("filter %s: %s", key, err)
		}
		conditions = append(conditions, condition)
	}

	return conditions, nil
}

func nonEmpty(values []string) []string {
	var kept []string
	for _, value := range values {
		if value != "" {
			kept = append(kept, value)
		}
	}

	return kept
}

func newCondition(path definition.Path, kind definition.FilterKind, in []string, empty, lowest, highest string) (Condition, error) {
	condition := Condition{Path: path, In: in}
	if empty != "" {
		value, err := strconv.ParseBool(empty)
		if err != nil {
			return Condition{}, fmt.Errorf("empty takes true or false")
		}
		condition.Empty = &value
	}
	if kind == definition.FilterBoolean {
		for _, value := range in {
			if value != "true" && value != "false" {
				return Condition{}, fmt.Errorf("give true or false")
			}
		}
	}
	if (lowest != "" || highest != "") && kind != definition.FilterDate && kind != definition.FilterNumber {
		return Condition{}, fmt.Errorf("gte and lte apply only to dates and numbers")
	}
	var err error
	switch kind {
	case definition.FilterDate:
		if condition.From, err = boundTime(lowest, false); err == nil {
			condition.Before, err = boundTime(highest, true)
		}
	case definition.FilterNumber:
		if condition.Min, err = boundNumber(lowest); err == nil {
			condition.Max, err = boundNumber(highest)
		}
	}

	return condition, err
}

// boundTime reads a date or a date-time. An upper bound is inclusive, so a
// date as upper bound takes in the whole day.
func boundTime(value string, upper bool) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	if day, err := time.Parse(time.DateOnly, value); err == nil {
		if upper {
			day = day.AddDate(0, 0, 1)
		}
		return &day, nil
	}
	moment, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, fmt.Errorf("give a date as YYYY-MM-DD or an RFC 3339 date-time")
	}
	if upper {
		moment = moment.Add(time.Microsecond)
	}

	return &moment, nil
}

func boundNumber(value string) (*float64, error) {
	if value == "" {
		return nil, nil
	}
	number, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return nil, fmt.Errorf("give a number")
	}

	return &number, nil
}

// summaries turns entities of one blueprint into table rows, with the
// targets of their relations and inverses and their derived values.
func (s *Service) summaries(ctx *gofr.Context, set definition.Set, blueprint *definition.Blueprint, entities []Entity) ([]entitySummary, error) {
	rows := make([]entitySummary, 0, len(entities))
	identifiers := make([]string, 0, len(entities))
	for _, entity := range entities {
		identifiers = append(identifiers, entity.Identifier)
	}
	if len(identifiers) == 0 {
		return rows, nil
	}
	links, err := s.entities.LinksFrom(ctx, blueprint.Identifier, identifiers)
	if err != nil {
		return nil, err
	}
	derived, err := s.derivedValues(ctx, set, blueprint, identifiers)
	if err != nil {
		return nil, err
	}
	inverses := map[string]map[string][]Reached{}
	for _, inverse := range set.Inverses(blueprint.Identifier) {
		attribute, _ := set.Attribute(blueprint, inverse.Key)
		if inverses[inverse.Key], err = s.entities.Reach(ctx, blueprint.Identifier, identifiers, attribute.Path); err != nil {
			return nil, err
		}
	}

	for _, entity := range entities {
		row := entitySummary{
			Identifier:  entity.Identifier,
			Title:       entity.Title,
			Integration: integrationOf(set, entity.Integration),
			Properties:  entity.Properties,
			Relations:   map[string][]EntityName{},
			Derived:     derived[entity.Identifier],
			UpdatedAt:   entity.UpdatedAt,
		}
		for _, link := range links {
			if link.Source.Identifier == entity.Identifier {
				row.Relations[link.Relation] = append(row.Relations[link.Relation], link.Target)
			}
		}
		for key, reached := range inverses {
			for _, item := range reached[entity.Identifier] {
				row.Relations[key] = append(row.Relations[key], item.Entity)
			}
		}
		rows = append(rows, row)
	}

	return rows, nil
}

// derivedValues reads every derived value of the blueprint for each of the
// entities, by identifier and then by derived key.
func (s *Service) derivedValues(ctx *gofr.Context, set definition.Set, blueprint *definition.Blueprint, identifiers []string) (map[string]map[string][]Reached, error) {
	values := make(map[string]map[string][]Reached, len(identifiers))
	for _, identifier := range identifiers {
		values[identifier] = map[string][]Reached{}
	}
	for _, key := range blueprint.Derived.Keys() {
		attribute, ok := set.Attribute(blueprint, key)
		if !ok {
			continue
		}
		reached, err := s.entities.Reach(ctx, blueprint.Identifier, identifiers, attribute.Path)
		if err != nil {
			return nil, err
		}
		for _, identifier := range identifiers {
			values[identifier][key] = reached[identifier]
		}
	}

	return values, nil
}
