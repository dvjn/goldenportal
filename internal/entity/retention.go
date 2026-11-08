package entity

import (
	"context"
	"strconv"
	"time"

	"github.com/dvjn/goldenportal/internal/blueprint"
)

// expired is SQL for whether e, an entity of spec, is older than spec's
// retention keeps it: the time it's kept by is older than the blueprint's
// keep, and than the keep of each rule it matches. Rows without one never
// are.
func (c *compiler) expired(spec blueprint.Spec) (string, error) {
	sql := "(e.occurred_at < now() - " + c.arg(seconds(spec.Keep())) + "::interval"
	for _, rule := range spec.Rules() {
		if rule.Duration() <= spec.Keep() {
			continue
		}
		matches, err := c.compile(*rule.Filter, 1)
		if err != nil {
			return "", err
		}
		sql += " AND NOT (e.occurred_at >= now() - " + c.arg(seconds(rule.Duration())) + "::interval AND coalesce(" + matches + ", false))"
	}

	return sql + ")", nil
}

func seconds(d time.Duration) string { return strconv.FormatInt(int64(d/time.Second), 10) + " seconds" }

// Expire deletes, oldest first, up to limit entities of spec that are
// older than its retention keeps them, and says how many it deleted.
// specs are the published blueprints, which rule filters may name.
func (s Store) Expire(ctx context.Context, specs map[string]blueprint.Spec, spec blueprint.Spec, limit int) (int, error) {
	c := &compiler{spec: spec, specs: specs, args: []any{spec.Identifier}}
	expired, err := c.expired(spec)
	if err != nil {
		return 0, err
	}
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM entities WHERE id IN (
			SELECT e.id FROM entities e WHERE e.blueprint_id = `+blueprintIDArg+` AND e.occurred_at IS NOT NULL AND `+expired+`
			ORDER BY e.occurred_at LIMIT `+c.arg(limit)+`)`, c.args...)
	if err != nil {
		return 0, err
	}
	n, err := result.RowsAffected()

	return int(n), err
}

// blueprintIDArg is the published blueprint the first argument names.
const blueprintIDArg = `(SELECT id FROM config_documents WHERE kind = 'blueprint' AND identifier = $1)`
