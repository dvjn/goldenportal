package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// OwnedWrite is what one integration writes in one transaction: entities by
// blueprint, and with Sweep the integration's other entities of those
// blueprints go. Force sweeps even when the write holds fewer than half of
// the entities the integration had.
type OwnedWrite struct {
	Integration string
	Groups      []OwnedGroup
	Sweep       bool
	Force       bool
}

type OwnedGroup struct {
	Blueprint string
	Entities  []OwnedEntity
}

// OwnedEntity is an entity an integration writes. Its links replace the
// entity's links of the relations named in Relations and keep the others.
type OwnedEntity struct {
	Entity
	Relations []string
}

// WriteResult is what a write did. A dropped link names a target that does
// not exist; a guarded sweep kept entities the write left out.
type WriteResult struct {
	Upserted int
	Deleted  int
	Dropped  []Link
	Guarded  []GuardedSweep
}

type GuardedSweep struct {
	Blueprint string
	Kept      int
	Written   int
	Existing  int
}

// Conflict is an entity a write would touch that another integration owns.
type Conflict struct {
	Blueprint   string `json:"blueprint"`
	Identifier  string `json:"identifier"`
	Integration string `json:"integration"`
}

// ConflictError refuses a whole write that would touch entities another
// integration owns.
type ConflictError struct {
	Conflicts []Conflict
}

func (e ConflictError) Error() string {
	first := e.Conflicts[0]
	message := fmt.Sprintf("ownership conflict: %s is owned by the %s integration", first.Identifier, first.Integration)
	if len(e.Conflicts) > 1 {
		message += fmt.Sprintf(", and %d more entities are owned by other integrations", len(e.Conflicts)-1)
	}

	return message
}

// Batch is the transaction a write runs in.
type Batch interface {
	// Owners returns the integration that owns each of the identifiers that
	// exist, and keeps them locked until the transaction ends.
	Owners(ctx context.Context, blueprint string, identifiers []string) (map[string]string, error)
	Owned(ctx context.Context, integration, blueprint string) ([]string, error)
	// Upsert reports false when another integration owns the entity.
	Upsert(ctx context.Context, entity Entity) (bool, error)
	Missing(ctx context.Context, blueprint string, identifiers []string) ([]string, error)
	ReplaceLinks(ctx context.Context, source EntityName, relation string, targets []EntityName) error
	Delete(ctx context.Context, integration, blueprint string, identifiers []string) (int, error)
}

// WriteOwned checks that the integration owns every entity it writes before
// it writes any, then upserts them, replaces their links and sweeps.
func WriteOwned(ctx context.Context, batch Batch, write OwnedWrite) (WriteResult, error) {
	if err := checkOwners(ctx, batch, write); err != nil {
		return WriteResult{}, err
	}
	existing := map[string][]string{}
	if write.Sweep {
		for _, group := range write.Groups {
			owned, err := batch.Owned(ctx, write.Integration, group.Blueprint)
			if err != nil {
				return WriteResult{}, err
			}
			existing[group.Blueprint] = owned
		}
	}

	var result WriteResult
	for _, group := range write.Groups {
		for _, entity := range group.Entities {
			entity.Integration = write.Integration
			written, err := batch.Upsert(ctx, entity.Entity)
			if err != nil {
				return WriteResult{}, err
			}
			if !written {
				return WriteResult{}, raceConflict(ctx, batch, write, group.Blueprint, entity.Identifier)
			}
			result.Upserted++
		}
	}
	for _, group := range write.Groups {
		for _, entity := range group.Entities {
			dropped, err := replaceLinks(ctx, batch, entity)
			if err != nil {
				return WriteResult{}, err
			}
			result.Dropped = append(result.Dropped, dropped...)
		}
	}
	for _, group := range write.Groups {
		if err := sweep(ctx, batch, write, group, existing[group.Blueprint], &result); err != nil {
			return WriteResult{}, err
		}
	}

	return result, nil
}

func checkOwners(ctx context.Context, batch Batch, write OwnedWrite) error {
	var conflicts []Conflict
	for _, group := range write.Groups {
		owners, err := batch.Owners(ctx, group.Blueprint, identifiersOf(group))
		if err != nil {
			return err
		}
		for _, identifier := range slices.Sorted(maps.Keys(owners)) {
			if owners[identifier] != write.Integration {
				conflicts = append(conflicts, Conflict{Blueprint: group.Blueprint, Identifier: identifier, Integration: owners[identifier]})
			}
		}
	}
	if len(conflicts) > 0 {
		return ConflictError{Conflicts: conflicts}
	}

	return nil
}

// raceConflict names the owner of an entity that another integration created
// after the owners were checked.
func raceConflict(ctx context.Context, batch Batch, write OwnedWrite, blueprint, identifier string) error {
	err := checkOwners(ctx, batch, OwnedWrite{
		Integration: write.Integration,
		Groups:      []OwnedGroup{{Blueprint: blueprint, Entities: []OwnedEntity{{Entity: Entity{Identifier: identifier}}}}},
	})
	if err != nil {
		return err
	}

	return ConflictError{Conflicts: []Conflict{{Blueprint: blueprint, Identifier: identifier, Integration: "another"}}}
}

func identifiersOf(group OwnedGroup) []string {
	identifiers := make([]string, 0, len(group.Entities))
	for _, entity := range group.Entities {
		identifiers = append(identifiers, entity.Identifier)
	}

	return identifiers
}

// replaceLinks replaces the entity's links of each relation it names, and
// drops the links to targets that do not exist.
func replaceLinks(ctx context.Context, batch Batch, entity OwnedEntity) ([]Link, error) {
	var dropped []Link
	for _, relation := range entity.Relations {
		var targets []EntityName
		for _, link := range entity.Links {
			if link.Relation == relation {
				targets = append(targets, link.Target)
			}
		}
		kept, missing, err := existingTargets(ctx, batch, targets)
		if err != nil {
			return nil, err
		}
		for _, target := range missing {
			dropped = append(dropped, Link{Relation: relation, Source: entity.Name(), Target: target})
		}
		if err := batch.ReplaceLinks(ctx, entity.Name(), relation, kept); err != nil {
			return nil, err
		}
	}

	return dropped, nil
}

func existingTargets(ctx context.Context, batch Batch, targets []EntityName) (kept, missing []EntityName, err error) {
	if len(targets) == 0 {
		return nil, nil, nil
	}
	identifiers := make([]string, 0, len(targets))
	for _, target := range targets {
		identifiers = append(identifiers, target.Identifier)
	}
	absent, err := batch.Missing(ctx, targets[0].Blueprint, identifiers)
	if err != nil {
		return nil, nil, err
	}
	for _, target := range targets {
		if slices.Contains(absent, target.Identifier) {
			missing = append(missing, target)
		} else {
			kept = append(kept, target)
		}
	}

	return kept, missing, nil
}

// sweep deletes the integration's entities of the group's blueprint that the
// write left out, unless the write holds fewer than half of the entities the
// integration had and is not forced.
func sweep(ctx context.Context, batch Batch, write OwnedWrite, group OwnedGroup, existing []string, result *WriteResult) error {
	written := identifiersOf(group)
	stale := slices.DeleteFunc(slices.Clone(existing), func(identifier string) bool { return slices.Contains(written, identifier) })
	if len(stale) == 0 {
		return nil
	}
	if !write.Force && 2*len(written) < len(existing) {
		result.Guarded = append(result.Guarded, GuardedSweep{
			Blueprint: group.Blueprint, Kept: len(stale), Written: len(written), Existing: len(existing),
		})
		return nil
	}
	deleted, err := batch.Delete(ctx, write.Integration, group.Blueprint, stale)
	result.Deleted += deleted

	return err
}

const (
	queryOwners = `SELECT identifier, integration FROM entities
		WHERE blueprint = $1 AND identifier IN (SELECT jsonb_array_elements_text($2::jsonb)) FOR UPDATE`
	queryOwned       = `SELECT identifier FROM entities WHERE integration = $1 AND blueprint = $2 ORDER BY identifier`
	queryUpsertOwned = `INSERT INTO entities (blueprint, identifier, title, properties, integration)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (blueprint, identifier) DO UPDATE SET
			title = EXCLUDED.title,
			properties = EXCLUDED.properties,
			updated_at = CASE
				WHEN (entities.title, entities.properties) IS DISTINCT FROM (EXCLUDED.title, EXCLUDED.properties) THEN NOW()
				ELSE entities.updated_at
			END
		WHERE entities.integration = EXCLUDED.integration
		RETURNING identifier`
	queryClearRelation = `DELETE FROM entity_relations WHERE source_blueprint = $1 AND source_identifier = $2 AND relation = $3`
	queryDeleteOwned   = `DELETE FROM entities
		WHERE integration = $1 AND blueprint = $2 AND identifier IN (SELECT jsonb_array_elements_text($3::jsonb))`
)

// WriteOwned runs the write in one transaction.
func (s *Store) WriteOwned(ctx context.Context, write OwnedWrite) (WriteResult, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return WriteResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	result, err := WriteOwned(ctx, sqlBatch{q: tx}, write)
	if err != nil {
		return WriteResult{}, err
	}

	return result, tx.Commit()
}

// DeleteOwned deletes the entity when the integration owns it, and reports
// whether it did.
func (s *Store) DeleteOwned(ctx context.Context, integration, blueprint, identifier string) (bool, error) {
	deleted, err := sqlBatch{q: s.db}.Delete(ctx, integration, blueprint, []string{identifier})
	return deleted == 1, err
}

type sqlBatch struct {
	q querier
}

func (b sqlBatch) Owners(_ context.Context, blueprint string, identifiers []string) (map[string]string, error) {
	encoded, err := json.Marshal(nonNil(identifiers))
	if err != nil {
		return nil, err
	}
	rows, err := b.q.Query(queryOwners, blueprint, string(encoded))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	owners := map[string]string{}
	for rows.Next() {
		var identifier, integration string
		if err := rows.Scan(&identifier, &integration); err != nil {
			return nil, err
		}
		owners[identifier] = integration
	}

	return owners, rows.Err()
}

func (b sqlBatch) Owned(_ context.Context, integration, blueprint string) ([]string, error) {
	rows, err := b.q.Query(queryOwned, integration, blueprint)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var identifiers []string
	for rows.Next() {
		var identifier string
		if err := rows.Scan(&identifier); err != nil {
			return nil, err
		}
		identifiers = append(identifiers, identifier)
	}

	return identifiers, rows.Err()
}

func (b sqlBatch) Upsert(ctx context.Context, entity Entity) (bool, error) {
	properties, err := json.Marshal(orEmpty(entity.Properties))
	if err != nil {
		return false, err
	}
	var identifier string
	err = b.q.QueryRowContext(ctx, queryUpsertOwned, entity.Blueprint, entity.Identifier, entity.Title,
		string(properties), entity.Integration).Scan(&identifier)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}

	return err == nil, err
}

func (b sqlBatch) Missing(_ context.Context, blueprint string, identifiers []string) ([]string, error) {
	encoded, err := json.Marshal(nonNil(identifiers))
	if err != nil {
		return nil, err
	}
	rows, err := b.q.Query(queryExisting, blueprint, string(encoded))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	existing := map[string]bool{}
	for rows.Next() {
		var identifier string
		if err := rows.Scan(&identifier); err != nil {
			return nil, err
		}
		existing[identifier] = true
	}

	return slices.DeleteFunc(slices.Clone(identifiers), func(identifier string) bool { return existing[identifier] }), rows.Err()
}

func (b sqlBatch) ReplaceLinks(ctx context.Context, source EntityName, relation string, targets []EntityName) error {
	if _, err := b.q.ExecContext(ctx, queryClearRelation, source.Blueprint, source.Identifier, relation); err != nil {
		return err
	}
	for _, target := range targets {
		if _, err := b.q.ExecContext(ctx, queryInsertLink, source.Blueprint, source.Identifier, relation,
			target.Blueprint, target.Identifier); err != nil {
			return err
		}
	}

	return nil
}

func (b sqlBatch) Delete(ctx context.Context, integration, blueprint string, identifiers []string) (int, error) {
	encoded, err := json.Marshal(nonNil(identifiers))
	if err != nil {
		return 0, err
	}
	result, err := b.q.ExecContext(ctx, queryDeleteOwned, integration, blueprint, string(encoded))
	if err != nil {
		return 0, err
	}
	deleted, err := result.RowsAffected()

	return int(deleted), err
}

// ValidIdentifier reports whether an entity can have the identifier.
func ValidIdentifier(identifier string) bool {
	return entityIdentifierPattern.MatchString(identifier)
}

// ValidTitle reports whether an entity can have the title.
func ValidTitle(title string) bool {
	trimmed := strings.TrimSpace(title)
	return trimmed != "" && len(trimmed) <= maxTitleLength
}
