package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/dvjn/goldport/internal/definition"
)

// EntityFilter selects entities of a blueprint and orders them. Search
// matches the title or the identifier; an empty field matches every entity.
type EntityFilter struct {
	Blueprint   string
	Search      string
	Integration string
	Conditions  []Condition
	Sort        EntitySort
}

// EntitySort orders entities by title, by update time, or by the first
// value an attribute's path reaches.
type EntitySort struct {
	By         string
	Path       definition.Path
	Descending bool
}

// Condition keeps the entities whose values at the path pass every test set:
// one of In (property values, or identifiers of the entities reached when
// the path ends in a relation), within the bounds, and empty or not.
type Condition struct {
	Path   definition.Path
	In     []string
	Empty  *bool
	From   *time.Time
	Before *time.Time
	Min    *float64
	Max    *float64
}

type sqlArgs struct {
	values []any
}

func (a *sqlArgs) bind(value any) string {
	a.values = append(a.values, value)
	return fmt.Sprintf("$%d", len(a.values))
}

func (a *sqlArgs) list(values []string) string {
	encoded, _ := json.Marshal(nonNil(values))
	return "(SELECT jsonb_array_elements_text(" + a.bind(string(encoded)) + "::jsonb))"
}

// Entities returns a page of the entities that match the filter, in the
// filter's order, and how many match.
func (s *Store) Entities(ctx context.Context, filter EntityFilter, page Page) ([]Entity, int, error) {
	counting := &sqlArgs{}
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM entities e WHERE `+entityConditions(counting, filter),
		counting.values...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args := &sqlArgs{}
	query := `SELECT e.blueprint, e.identifier, e.title, e.properties, e.integration, e.created_at, e.updated_at
		FROM entities e WHERE ` + entityConditions(args, filter) +
		` ORDER BY ` + entityOrder(args, filter.Sort) +
		` OFFSET ` + args.bind(page.Offset) + ` LIMIT ` + args.bind(page.Limit)
	rows, err := s.db.QueryContext(ctx, query, args.values...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	entities := []Entity{}
	for rows.Next() {
		entity, err := scanEntity(rows)
		if err != nil {
			return nil, 0, err
		}
		entities = append(entities, entity)
	}

	return entities, total, rows.Err()
}

func entityConditions(args *sqlArgs, filter EntityFilter) string {
	conditions := []string{"e.blueprint = " + args.bind(filter.Blueprint)}
	if filter.Search != "" {
		pattern := args.bind("%" + escapeLike(filter.Search) + "%")
		conditions = append(conditions, fmt.Sprintf("(e.title ILIKE %[1]s OR e.identifier ILIKE %[1]s)", pattern))
	}
	if filter.Integration != "" {
		conditions = append(conditions, "e.integration = "+args.bind(filter.Integration))
	}
	for _, condition := range filter.Conditions {
		conditions = append(conditions, conditionSQL(args, condition)...)
	}

	return strings.Join(conditions, " AND ")
}

func entityOrder(args *sqlArgs, sort EntitySort) string {
	direction := " ASC"
	if sort.Descending {
		direction = " DESC"
	}
	column := "e.title"
	switch {
	case sort.By == definition.SortUpdatedAt:
		column = "e.updated_at"
	case sort.Path.Property != "" && len(sort.Path.Hops) == 0:
		column = "e.properties -> " + args.bind(sort.Path.Property) + "::text"
	case sort.Path.Property != "":
		from, where := reach(args, sort.Path.Hops)
		value := "t.properties -> " + args.bind(sort.Path.Property) + "::text"
		column = "(SELECT " + value + " FROM " + from + " WHERE " + strings.Join(where, " AND ") +
			" ORDER BY 1" + direction + " NULLS LAST LIMIT 1)"
	}

	return column + direction + " NULLS LAST, e.title ASC, e.identifier ASC"
}

// reach follows the hops from the entity e and returns the FROM list and
// the WHERE conditions that name each entity reached t.
func reach(args *sqlArgs, hops []definition.Hop) (string, []string) {
	blueprint, identifier := "e.blueprint", "e.identifier"
	tables := make([]string, 0, len(hops)+1)
	var where []string
	for index, hop := range hops {
		relation := fmt.Sprintf("r%d", index)
		anchor, next := "source", "target"
		if hop.Inverse {
			anchor, next = "target", "source"
			where = append(where, relation+".source_blueprint = "+args.bind(hop.To))
		}
		tables = append(tables, "entity_relations "+relation)
		where = append(where,
			fmt.Sprintf("%s.%s_blueprint = %s", relation, anchor, blueprint),
			fmt.Sprintf("%s.%s_identifier = %s", relation, anchor, identifier),
			relation+".relation = "+args.bind(hop.Relation),
		)
		blueprint, identifier = relation+"."+next+"_blueprint", relation+"."+next+"_identifier"
	}
	tables = append(tables, "entities t")
	where = append(where, "t.blueprint = "+blueprint, "t.identifier = "+identifier)

	return strings.Join(tables, ", "), where
}

// onPath applies a test to the entity itself, or to any entity the path
// reaches.
func onPath(args *sqlArgs, path definition.Path, test func(alias string) string) string {
	if len(path.Hops) == 0 {
		return test("e")
	}
	from, where := reach(args, path.Hops)
	where = append(where, test("t"))

	return "EXISTS (SELECT 1 FROM " + from + " WHERE " + strings.Join(where, " AND ") + ")"
}

const datePattern = `'^\d{4}-\d{2}-\d{2}'`

func conditionSQL(args *sqlArgs, condition Condition) []string {
	var conditions []string
	property := condition.Path.Property
	if condition.Empty != nil {
		present := onPath(args, condition.Path, func(alias string) string {
			if property == "" {
				return "TRUE"
			}
			key := args.bind(property) + "::text"
			return fmt.Sprintf(`(%[1]s.properties -> %[2]s) NOT IN ('null'::jsonb, '""'::jsonb, '[]'::jsonb)`, alias, key)
		})
		if *condition.Empty {
			present = "NOT " + present
		}
		conditions = append(conditions, present)
	}
	tests := func(alias string) []string {
		var tests []string
		if property == "" {
			if condition.In != nil {
				tests = append(tests, alias+".identifier IN "+args.list(condition.In))
			}
			return tests
		}
		key := args.bind(property) + "::text"
		text := fmt.Sprintf("(%s.properties ->> %s)", alias, key)
		date := fmt.Sprintf("(CASE WHEN %[1]s ~ %[2]s THEN %[1]s::timestamptz END)", text, datePattern)
		number := fmt.Sprintf("(CASE WHEN jsonb_typeof(%[1]s.properties -> %[2]s) = 'number' THEN (%[1]s.properties -> %[2]s)::numeric END)", alias, key)
		if condition.In != nil {
			tests = append(tests, text+" IN "+args.list(condition.In))
		}
		if condition.From != nil {
			tests = append(tests, date+" >= "+args.bind(*condition.From))
		}
		if condition.Before != nil {
			tests = append(tests, date+" < "+args.bind(*condition.Before))
		}
		if condition.Min != nil {
			tests = append(tests, number+" >= "+args.bind(*condition.Min))
		}
		if condition.Max != nil {
			tests = append(tests, number+" <= "+args.bind(*condition.Max))
		}
		return tests
	}
	bounded := condition.From != nil || condition.Before != nil || condition.Min != nil || condition.Max != nil
	if condition.In != nil || property != "" && bounded {
		conditions = append(conditions, onPath(args, condition.Path, func(alias string) string {
			return strings.Join(tests(alias), " AND ")
		}))
	}

	return conditions
}
