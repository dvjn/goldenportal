package integration

import (
	"context"
	"encoding/json"

	"gofr.dev/pkg/gofr/container"
)

// lookupReader reads the entities a mapping's lookups bind, as
// LookupRecords makes them.
type lookupReader interface {
	// LookupRecords returns the entities of the blueprint, leaving out those
	// the excluded integration owns.
	LookupRecords(ctx context.Context, blueprint, excludedIntegration string) ([]any, error)
}

type PostgresLookups struct {
	db container.DB
}

func NewPostgresLookups(db container.DB) PostgresLookups {
	return PostgresLookups{db: db}
}

const queryLookupRecords = `SELECT jsonb_build_object(
		'identifier', e.identifier,
		'title', e.title,
		'properties', e.properties,
		'relations', COALESCE((
			SELECT jsonb_object_agg(grouped.relation, grouped.targets) FROM (
				SELECT r.relation, jsonb_agg(r.target_identifier ORDER BY r.target_identifier) AS targets
				FROM entity_relations r
				WHERE r.source_blueprint = e.blueprint AND r.source_identifier = e.identifier
				GROUP BY r.relation) grouped), '{}'::jsonb))
	FROM entities e
	WHERE e.blueprint = $1 AND e.integration <> $2
	ORDER BY e.identifier`

func (l PostgresLookups) LookupRecords(ctx context.Context, blueprint, excludedIntegration string) ([]any, error) {
	rows, err := l.db.QueryContext(ctx, queryLookupRecords, blueprint, excludedIntegration)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records := []any{}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var record any
		if err := json.Unmarshal(raw, &record); err != nil {
			return nil, err
		}
		records = append(records, record)
	}

	return records, rows.Err()
}

// lookups returns the entities each of the mapper's lookups binds. A
// blueprint the run already mapped is read from the run, so a mapping sees
// what an earlier mapping of the same sync wrote; the catalog gives the
// entities other integrations own.
func lookups(ctx context.Context, reader lookupReader, integration string, mapper *Mapper, mapped map[string][]any) ([][]any, error) {
	bound := make([][]any, 0, len(mapper.Lookups()))
	for _, blueprint := range mapper.Lookups() {
		excluded := ""
		if _, ok := mapped[blueprint]; ok {
			excluded = integration
		}
		records, err := reader.LookupRecords(ctx, blueprint, excluded)
		if err != nil {
			return nil, err
		}
		bound = append(bound, append(records, mapped[blueprint]...))
	}

	return bound, nil
}
