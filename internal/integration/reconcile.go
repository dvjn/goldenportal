package integration

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/entity"
	"github.com/dvjn/goldenportal/internal/telemetry"
)

// sightings is what a run's entries named, which its reconcile keeps.
type sightings struct {
	// seen are each blueprint's identifiers, from any scope.
	seen map[string]map[string]bool
	// unsafe are the scopes, "" for none, each blueprint had an entry
	// without an identifier from, which could have been any of their
	// entities. A deletion without one deletes nothing, so it's only a
	// failed item.
	unsafe map[string]map[string]bool
}

func newSightings() *sightings {
	return &sightings{seen: map[string]map[string]bool{}, unsafe: map[string]map[string]bool{}}
}

func sightingsOf(entries []Entry) *sightings {
	s := newSightings()
	s.add(entries)

	return s
}

func (s *sightings) add(entries []Entry) {
	mark := func(sets map[string]map[string]bool, bp, value string) {
		if sets[bp] == nil {
			sets[bp] = map[string]bool{}
		}
		sets[bp][value] = true
	}
	for _, e := range entries {
		switch {
		case e.Entity.Identifier == "" && !e.Deleted:
			mark(s.unsafe, e.Blueprint, e.Scope)
		case !e.Deleted:
			mark(s.seen, e.Blueprint, e.Entity.Identifier)
		}
	}
}

// reconcile deletes, after a complete run over resources, the integration's
// entities that neither it nor the complete run before it saw, and marks
// those it missed for the first time, of what the run read in full. A
// blueprint is judged only when its mappings reconcile and the run covered
// every resource they name. Within it, each scope's entities are judged when every one of those
// resources was read in full in that scope, and none of its entries from
// there lacked an identifier; entities with no scope or a shared one, when
// every scope was, and every listing of scopes succeeded; and those of
// scopes the run didn't list, when every listing succeeded.
func reconcile(ctx context.Context, db configdoc.DB, integrationID string, spec Spec, covered []string,
	report Report, saw *sightings, started time.Time,
) (deleted int, err error) {
	ctx, span := telemetry.Start(ctx, "reconcile")
	defer func() {
		span.SetAttributes(telemetry.Deleted.Int(deleted))
		telemetry.End(span, err)
	}()
	var units []entity.Unit
	for _, bp := range spec.reconcilable(covered) {
		if saw.unsafe[bp][""] {
			continue
		}
		id, err := configdoc.NewStore(db, blueprint.Name).PublishedID(ctx, bp)
		switch {
		case errors.Is(err, configdoc.ErrNotFound):
			continue
		case err != nil:
			return 0, err
		}
		resources := spec.resourcesOf(bp)
		failed, lastingOnly := map[string]bool{}, true
		for _, f := range report.ScopeFailures {
			if slices.Contains(resources, f.Resource) {
				failed[f.Scope] = true
				lastingOnly = lastingOnly && f.lasting
			}
		}
		listed := len(report.Unlisted) == 0
		u := entity.Unit{
			BlueprintID: id, Seen: slices.Collect(maps.Keys(saw.seen[bp])),
			Unscoped: listed && lastingOnly && len(saw.unsafe[bp]) == 0,
			Gone:     listed && len(saw.unsafe[bp]) == 0, Listed: report.Scopes,
		}
		for _, scope := range report.Scopes {
			if !failed[scope] && !saw.unsafe[bp][scope] {
				u.Scopes = append(u.Scopes, scope)
			}
		}
		units = append(units, u)
	}

	if len(units) == 0 {
		return 0, nil
	}
	changes, err := NewStore(db).readChanges(ctx, integrationID)
	if err != nil {
		return 0, err
	}

	return entity.NewStore(db).Reconcile(ctx, integrationID, units, started, changes.complete)
}

// reconcilable lists, in order, the blueprints a complete run over covered
// can reconcile: those whose mappings reconcile, and whose resources it all
// covered. Upsert mappings never delete what a run didn't see.
func (s Spec) reconcilable(covered []string) []string {
	whole := map[string]bool{}
	for _, m := range s.Mappings {
		if _, seen := whole[m.Blueprint]; !seen {
			whole[m.Blueprint] = true
		}
		if m.Upsert() || !slices.Contains(covered, m.Resource) {
			whole[m.Blueprint] = false
		}
	}
	var out []string
	for bp, ok := range whole {
		if ok {
			out = append(out, bp)
		}
	}
	slices.Sort(out)

	return out
}

// resourcesOf lists the resources the mappings of a blueprint read.
func (s Spec) resourcesOf(bp string) []string {
	var resources []string
	for _, m := range s.Mappings {
		if m.Blueprint == bp && !slices.Contains(resources, m.Resource) {
			resources = append(resources, m.Resource)
		}
	}

	return resources
}

// lastingRuns is how many syncs in a row a scope must be refused a resource
// before the refusal stops holding back the deletes of entities that aren't
// that scope's alone. A refusal that lasts is a setting, not an outage, and
// would otherwise hold them back for good.
const lastingRuns = 3

// failedScope is a scope failure as a run records it.
type failedScope struct {
	Resource string `json:"resource"`
	Scope    string `json:"scope"`
	Refused  bool   `json:"refused,omitempty"`
}

func failedScopes(report Report) []failedScope {
	out := []failedScope{}
	for _, f := range report.ScopeFailures {
		out = append(out, failedScope{Resource: f.Resource, Scope: f.Scope, Refused: f.Refused})
	}

	return out
}

// markLasting marks the refusals in report that the syncs before, whose
// failed scopes are earlier, newest first, also met, each of the last
// lastingRuns-1 of them.
func markLasting(report *Report, earlier [][]failedScope) {
	if len(earlier) < lastingRuns-1 {
		return
	}
	for i, f := range report.ScopeFailures {
		if !f.Refused {
			continue
		}
		lasting := true
		for _, run := range earlier[:lastingRuns-1] {
			lasting = lasting && slices.Contains(run, failedScope{Resource: f.Resource, Scope: f.Scope, Refused: true})
		}
		report.ScopeFailures[i].lasting = lasting
	}
}

// failureText says what a pull couldn't read, as the run records it: an
// error for what held back deletes, and a warning for refusals that have
// lasted, which hold back only their own scope's.
func failureText(report Report) (failed, warning string) {
	byResource, lasting := map[string][]error{}, map[string][]error{}
	for resource, err := range report.Failed {
		byResource[resource] = append(byResource[resource], err)
	}
	for _, f := range report.ScopeFailures {
		if f.lasting {
			lasting[f.Resource] = append(lasting[f.Resource], f.Err)
		} else {
			byResource[f.Resource] = append(byResource[f.Resource], f.Err)
		}
	}
	summarize := func(by map[string][]error) []string {
		var out []string
		for _, resource := range slices.Sorted(maps.Keys(by)) {
			errs := by[resource]
			text := errs[0].Error()
			if len(errs) > 1 {
				text = fmt.Sprintf("%s, and %d more failed", text, len(errs)-1)
			}
			out = append(out, text)
		}
		return out
	}
	reasons := summarize(byResource)
	for _, err := range report.Unlisted {
		reasons = append(reasons, err.Error())
	}
	if refused := summarize(lasting); len(refused) > 0 {
		warning = fmt.Sprintf("%s. Refused on each of the last %d syncs, so it holds back deleting only what was read from there.",
			strings.Join(refused, "; "), lastingRuns)
	}

	return strings.Join(reasons, "; "), warning
}
