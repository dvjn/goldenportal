package migrations

import (
	"github.com/lib/pq"
	"go.yaml.in/yaml/v3"
	"gofr.dev/pkg/gofr/migration"
)

func scorecardResultPaths() migration.Migrate {
	return migration.Migrate{
		UP: func(d migration.Datasource) error {
			// A tier's place up its ladder, which tiers sort by, as evaluations write it.
			if _, err := d.SQL.Exec(`ALTER TABLE scorecard_results ADD COLUMN tier_rank smallint NOT NULL DEFAULT 0`); err != nil {
				return err
			}
			if err := rankTiers(d.SQL); err != nil {
				return err
			}
			statements := []string{
				// Filters on tiers are semi-joins on a scorecard's entities in a tier.
				`CREATE INDEX scorecard_results_tier_idx ON scorecard_results (scorecard_id, tier, entity_id)`,
				// Filters on checks' statuses are containments on criteria.
				`CREATE INDEX scorecard_results_criteria_idx ON scorecard_results USING gin (criteria jsonb_path_ops)`,
			}
			for _, statement := range statements {
				if _, err := d.SQL.Exec(statement); err != nil {
					return err
				}
			}

			return nil
		},
	}
}

// rankTiers ranks the stored results by the ladder of the version they
// were evaluated against.
func rankTiers(db migration.SQL) error {
	rows, err := db.Query(`SELECT d.id, v.source FROM config_documents d
		LEFT JOIN scorecard_evaluations e ON e.scorecard_id = d.id
		JOIN config_versions v ON v.document_id = d.id AND v.version = coalesce(e.evaluated_version, d.published_version)
		WHERE d.kind = 'scorecard'`)
	if err != nil {
		return err
	}
	defer rows.Close()
	ladders := map[string][]string{}
	for rows.Next() {
		var id, source string
		if err := rows.Scan(&id, &source); err != nil {
			return err
		}
		if names, ok := tierNames(source); ok {
			ladders[id] = names
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for id, names := range ladders {
		if _, err := db.Exec(`UPDATE scorecard_results SET tier_rank = coalesce(array_position($2::text[], tier), 0)
			WHERE scorecard_id = $1 AND tier <> ''`, id, pq.Array(names)); err != nil {
			return err
		}
	}

	return nil
}

// tierNames reads a scorecard's tiers, lowest first, whose positions are
// their ranks. A source that doesn't read has none; its next evaluation
// ranks its results.
func tierNames(source string) ([]string, bool) {
	var doc struct {
		Spec struct {
			Tiers []struct {
				Name string `yaml:"name"`
			} `yaml:"tiers"`
		} `yaml:"spec"`
	}
	if yaml.Unmarshal([]byte(source), &doc) != nil {
		return nil, false
	}
	names := make([]string, len(doc.Spec.Tiers))
	for i, tier := range doc.Spec.Tiers {
		names[i] = tier.Name
	}

	return names, true
}
