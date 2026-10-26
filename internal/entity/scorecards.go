package entity

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/lib/pq"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
)

const ScorecardPrefix = "$scorecard."

type Score struct {
	Tier  string `json:"tier"`
	Score *int   `json:"score"`
}

func ScorecardName(property string) (string, bool) {
	name, ok := strings.CutPrefix(property, ScorecardPrefix)
	return name, ok && configdoc.ValidIdentifier(name)
}

func scorecardField(property string) (field, error) {
	name, ok := ScorecardName(property)
	if !ok {
		return field{}, fmt.Errorf("%s must name a scorecard, like $scorecard.readiness", property)
	}
	return field{column: true, nullable: true, kind: "string", value: `(SELECT r.tier FROM scorecard_results r
 JOIN config_documents s ON s.id = r.scorecard_id
 WHERE r.entity_id = e.id AND s.kind = 'scorecard' AND s.published_version IS NOT NULL
 AND s.identifier = ` + quoteLiteral(name) + `)`}, nil
}

// ResolveScores loads one page's tiers in one query, independently of its columns.
func (s Store) ResolveScores(ctx context.Context, entities []*Entity) error {
	if len(entities) == 0 {
		return nil
	}
	ids := make([]string, 0, len(entities))
	byID := map[string]*Entity{}
	for _, e := range entities {
		ids = append(ids, e.ID)
		byID[e.ID] = e
		e.Scorecards = map[string]Score{}
	}
	rows, err := s.db.QueryContext(ctx, `SELECT r.entity_id, s.identifier, r.tier, r.score
 FROM scorecard_results r JOIN config_documents s ON s.id = r.scorecard_id
 WHERE r.entity_id = ANY($1::uuid[]) AND s.kind = 'scorecard' AND s.published_version IS NOT NULL`, pq.Array(ids))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, name string
		var result Score
		if err := rows.Scan(&id, &name, &result.Tier, &result.Score); err != nil {
			return err
		}
		byID[id].Scorecards[name] = result
	}
	return rows.Err()
}

func (s Store) orderBy(ctx context.Context, bp blueprint.Spec, sort string, c *compiler) (string, error) {
	property := strings.TrimPrefix(sort, "-")
	if !strings.HasPrefix(property, ScorecardPrefix) {
		order, err := orderBy(bp, sort)
		if err != nil {
			return "", FilterError{err}
		}
		return order, nil
	}
	name, ok := ScorecardName(property)
	if !ok {
		return "", FilterError{fmt.Errorf("%s must name a scorecard", property)}
	}
	record, err := configdoc.NewStore(s.db, "scorecard").Get(ctx, name)
	if err != nil {
		if errors.Is(err, configdoc.ErrNotFound) {
			return "", FilterError{fmt.Errorf("no published scorecard %s", name)}
		}
		return "", err
	}
	if !record.Published() {
		return "", FilterError{fmt.Errorf("no published scorecard %s", name)}
	}
	// Read the published ladder here to keep pagination in SQL without storing a second rank.
	r := &configdoc.Reader{}
	doc := configdoc.ReadDocument(r, record.PublishedSource, "Scorecard")
	target := configdoc.Field(doc.Spec, "blueprint")
	tiers := configdoc.Field(doc.Spec, "tiers")
	if configdoc.HasErrors(r.Issues) || target == nil || tiers == nil || target.Value != bp.Identifier {
		return "", FilterError{fmt.Errorf("%s does not score %s", name, bp.Identifier)}
	}
	var ladder []struct {
		Name string `yaml:"name"`
	}
	if err := tiers.Decode(&ladder); err != nil {
		return "", FilterError{fmt.Errorf("%s has an invalid tier ladder", name)}
	}
	names := []string{""}
	for _, tier := range ladder {
		names = append(names, tier.Name)
	}
	return scorecardOrder(property, sort, names, c)
}

func scorecardOrder(property, sort string, names []string, c *compiler) (string, error) {
	fld, err := scorecardField(property)
	if err != nil {
		return "", err
	}
	direction := "ASC"
	if strings.HasPrefix(sort, "-") {
		direction = "DESC"
	}
	return "array_position(" + c.arg(pq.Array(names)) + "::text[], " + fld.value + ") " + direction + " NULLS LAST, e.title, e.identifier", nil
}
