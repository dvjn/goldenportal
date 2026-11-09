package entity

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/dvjn/goldenportal/internal/blueprint"
)

// Day is the entities whose date falls on one day, in UTC: how many there
// are, and their aggregates, in the order they were asked for.
type Day struct {
	Day    time.Time
	Count  int
	Values []*float64
}

// CheckTrend reports whether entities can be put on the day of a field.
func CheckTrend(specs map[string]blueprint.Spec, spec blueprint.Spec, path string) error {
	_, err := trendField(&compiler{spec: spec, specs: specs}, path)
	return err
}

func trendField(c *compiler, path string) (field, error) {
	fld, err := c.field(path)
	if err != nil {
		return field{}, err
	}
	if fld.many || fld.kind != kindDate {
		return field{}, fmt.Errorf("%s isn't a date, so entities can't be put on its day", path)
	}

	return fld, nil
}

// Trend works out aggregates of m's entities for each UTC day their date
// at path falls on, from from up to to, and over all of them, in one
// statement. Days without entities are left out. A date that's the
// property a blueprint's retention keeps rows by reads its index.
func (s Store) Trend(ctx context.Context, m Match, path string, from, to time.Time, aggregates []Aggregate) (days []Day, all Day, err error) {
	c := m.compiler()
	fld, err := trendField(c, path)
	if err != nil {
		return nil, Day{}, FilterError{err}
	}
	columns, err := aggregateColumns(c, aggregates)
	if err != nil {
		return nil, Day{}, err
	}
	date := dateOf(fld)
	day := "(" + date + " AT TIME ZONE 'UTC')::date"
	var extra string
	if len(columns) > 0 {
		extra = ", " + strings.Join(columns, ", ")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+day+`, count(*)`+extra+entityFrom+`
	WHERE `+m.where+` AND `+date+` >= `+c.arg(from)+` AND `+date+` < `+c.arg(to)+`
	GROUP BY GROUPING SETS ((`+day+`), ())`, c.args...)
	if err != nil {
		return nil, Day{}, err
	}
	defer rows.Close()

	days = []Day{}
	for rows.Next() {
		var (
			on sql.NullTime
			d  Day
		)
		values := make([]sql.NullFloat64, len(aggregates))
		targets := []any{&on, &d.Count}
		for i := range values {
			targets = append(targets, &values[i])
		}
		if err := rows.Scan(targets...); err != nil {
			return nil, Day{}, err
		}
		d.Values = scanValues(values)
		// Every row in range has a date, so only the total has none.
		if !on.Valid {
			all = d
			continue
		}
		d.Day = on.Time.UTC()
		days = append(days, d)
	}
	if err := rows.Err(); err != nil {
		return nil, Day{}, err
	}
	if all.Values == nil {
		all.Values = make([]*float64, len(aggregates))
	}

	return days, all, nil
}
