package entity

import (
	"context"
	"database/sql"
	"fmt"
	"slices"

	"github.com/dvjn/goldenportal/internal/blueprint"
)

// Aggregates are the ways Aggregate sums up a property.
var Aggregates = []string{"count", "sum", "avg", "min", "max"}

// Aggregate sums up the entities matching filter: count counts them, and
// the others work on a number property. It's nil when there's nothing to
// work on, such as the average of no values.
func (s Store) Aggregate(ctx context.Context, spec blueprint.Spec, filter *Filter, op, property string) (*float64, error) {
	if err := CheckAggregate(spec, op, property); err != nil {
		return nil, FilterError{err}
	}
	c := &compiler{spec: spec, args: []any{spec.Identifier}}
	where := "b.kind = 'blueprint' AND b.identifier = $1"
	if filter != nil {
		sql, err := c.compile(*filter, 1)
		if err != nil {
			return nil, FilterError{err}
		}
		where += " AND coalesce(" + sql + ", false)"
	}
	expression := "count(*)"
	if op != "count" {
		fld, _ := fieldFor(spec, property)
		expression = op + "(CASE WHEN jsonb_typeof(" + fld.value + ") = 'number' THEN (" + fld.value + ")::numeric END)"
	}

	var value sql.NullFloat64
	if err := s.db.QueryRowContext(ctx, `SELECT (`+expression+`)::float8`+entityFrom+` WHERE `+where, c.args...).Scan(&value); err != nil {
		return nil, err
	}
	if !value.Valid {
		return nil, nil
	}

	return &value.Float64, nil
}

// CheckAggregate reports whether a blueprint can be summed up this way.
func CheckAggregate(spec blueprint.Spec, op, property string) error {
	if !slices.Contains(Aggregates, op) {
		return fmt.Errorf("aggregate must be one of count, sum, avg, min or max")
	}
	if op == "count" {
		if property != "" {
			return fmt.Errorf("count counts entities, so it takes no property")
		}
		return nil
	}
	if property == "" {
		return fmt.Errorf("%s needs a number property", op)
	}
	fld, err := fieldFor(spec, property)
	if err != nil {
		return err
	}
	if fld.kind != "number" && fld.kind != "integer" {
		return fmt.Errorf("%s works on numbers, and %s isn't one", op, property)
	}

	return nil
}

// CheckGroup reports whether entities can be counted by a property's values.
func CheckGroup(spec blueprint.Spec, property string) error {
	fld, err := fieldFor(spec, property)
	if err != nil {
		return err
	}
	if fld.column || fld.kind == "object" {
		return fmt.Errorf("%s can't be counted by value", property)
	}

	return nil
}

// CheckSort reports whether a blueprint's entities can be sorted this way.
func CheckSort(spec blueprint.Spec, sort string) error {
	_, err := orderBy(spec, sort)
	return err
}

// CheckColumn reports whether a property, or a built-in field, can be shown.
func CheckColumn(spec blueprint.Spec, column string) error {
	if slices.Contains([]string{"$identifier", "$title", "$integration", "$createdAt", "$updatedAt"}, column) {
		return nil
	}
	if _, ok := spec.Property(column); !ok {
		return fmt.Errorf("%s has no property %q", spec.Identifier, column)
	}

	return nil
}

// Matching is SQL selecting the IDs of the entities of spec matching
// filter, with its arguments numbered after args.
func Matching(spec blueprint.Spec, filter *Filter, args []any) (string, []any, error) {
	c := &compiler{spec: spec, args: append(args, spec.Identifier)}
	where := "b.kind = 'blueprint' AND b.identifier = $" + fmt.Sprint(len(c.args))
	if filter != nil {
		sql, err := c.compile(*filter, 1)
		if err != nil {
			return "", nil, FilterError{err}
		}
		where += " AND coalesce(" + sql + ", false)"
	}

	return `SELECT e.id` + entityFrom + ` WHERE ` + where, c.args, nil
}
