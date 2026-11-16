package index

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"slices"

	"github.com/lib/pq"

	"github.com/dvjn/goldenportal/internal/entity"
)

// prefix starts the name of every index this package manages, and no
// other, so it drops only its own.
const prefix = "gpx_"

// planned is an index on one blueprint's entities, partial so that it holds
// only theirs and statements naming the blueprint's ID pick it.
type planned struct {
	Name        string
	Blueprint   string
	BlueprintID string
	// Expression is the index's columns, as CREATE INDEX writes them.
	Expression string
	Reasons    []string
	Weight     int
	// Skipped says why it isn't built, when a cap left it out.
	Skipped string
}

// create is the statement that builds the index without blocking writes.
func (i planned) create() string {
	return "CREATE INDEX CONCURRENTLY " + pq.QuoteIdentifier(i.Name) + " ON entities (" + i.Expression + ") WHERE blueprint_id = " + pq.QuoteLiteral(i.BlueprintID)
}

// Settings bound which blueprints get indexes and how many.
type Settings struct {
	// MinRows is how many entities a blueprint has before it gets indexes;
	// below that, reading them all is about as quick.
	MinRows int
	// PerBlueprint and Total cap the indexes, since each one slows every
	// write to its blueprint; either at 0 turns them off.
	PerBlueprint int
	Total        int
}

var defaults = Settings{MinRows: 50000, PerBlueprint: 8, Total: 64}

func (s Settings) off() bool { return s.PerBlueprint == 0 || s.Total == 0 }

// plan works out the indexes uses ask for on the blueprints ids names, by
// identifier, which are those big enough for them, and splits them into
// those to build and those the caps leave out, most used first. A sort's
// index also serves ranges on its key, so they share it.
func plan(uses []keyUse, ids map[string]string, s Settings) (build, skipped []planned) {
	type key struct{ blueprint, expression string }
	sorts, others := map[key][]keyUse{}, map[key][]keyUse{}
	for _, use := range uses {
		if _, ok := ids[use.Blueprint]; !ok {
			continue
		}
		k := key{use.Blueprint, use.Expression}
		if use.Use == entity.UseSort {
			sorts[k] = append(sorts[k], use)
		} else {
			others[k] = append(others[k], use)
		}
	}
	byBlueprint := map[string][]planned{}
	add := func(k key, expression string, uses []keyUse) {
		ix := planned{Blueprint: k.blueprint, BlueprintID: ids[k.blueprint], Expression: expression}
		for _, use := range uses {
			ix.Weight += use.Weight
			ix.Reasons = append(ix.Reasons, use.Reason+": "+use.Use)
		}
		ix.Reasons = slices.Compact(slices.Sorted(slices.Values(ix.Reasons)))
		ix.Name = name(ix.BlueprintID, expression)
		byBlueprint[k.blueprint] = append(byBlueprint[k.blueprint], ix)
	}
	for k, uses := range sorts {
		// Descending unless every sort is ascending; read backwards, the
		// index serves the other way too, with ties sorted per key.
		direction := "ASC NULLS FIRST"
		if slices.ContainsFunc(uses, func(u keyUse) bool { return u.Descending }) {
			direction = "DESC NULLS LAST"
		}
		add(k, "("+k.expression+") "+direction+", title, identifier", slices.Concat(uses, others[k]))
		delete(others, k)
	}
	for k, uses := range others {
		add(k, "("+k.expression+")", uses)
	}

	var kept []planned
	for _, blueprint := range slices.Sorted(maps.Keys(byBlueprint)) {
		indexes := byBlueprint[blueprint]
		slices.SortFunc(indexes, mostUsed)
		for i, ix := range indexes {
			if i >= s.PerBlueprint {
				ix.Skipped = fmt.Sprintf("%s has %d indexes, the most a blueprint gets", blueprint, s.PerBlueprint)
				skipped = append(skipped, ix)
				continue
			}
			kept = append(kept, ix)
		}
	}
	slices.SortFunc(kept, mostUsed)
	for i, ix := range kept {
		if i >= s.Total {
			ix.Skipped = fmt.Sprintf("there are %d indexes, the most there can be", s.Total)
			skipped = append(skipped, ix)
			continue
		}
		build = append(build, ix)
	}

	return build, skipped
}

func mostUsed(a, b planned) int {
	return cmp.Or(cmp.Compare(b.Weight, a.Weight), cmp.Compare(a.Blueprint, b.Blueprint), cmp.Compare(a.Expression, b.Expression))
}

// name is an index's, the same for the same blueprint and columns, so
// reconciling finds what's built by name.
func name(blueprintID, expression string) string {
	sum := sha256.Sum256([]byte(blueprintID + " " + expression))
	return prefix + hex.EncodeToString(sum[:8])
}
