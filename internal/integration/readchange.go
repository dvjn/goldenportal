package integration

import (
	"context"
	"reflect"
	"slices"
	"time"
)

// readChanges are when an integration was last published to read
// differently, as its published versions say, so publishing stores nothing
// of it. A version reads a resource differently when a mapping that reads it
// is added or changed, since what earlier syncs left unwritten may now be
// written, or when its connector config reads it differently. Removing a
// mapping, or changing the schedule, title or description, reads nothing
// differently.
type readChanges struct {
	// all is when every resource last was: on the first version, and on
	// one with another connector or config, but for a connector whose
	// config reads each resource with a part of its own.
	all time.Time
	// resources is when each resource last was.
	resources map[string]time.Time
	// complete is when what a reconcile mapping reads last was, which a
	// complete sync then reads in full before any can be incremental, and
	// before which no complete run counts towards deleting what it missed.
	complete time.Time
}

func (c readChanges) since(resource string) time.Time {
	if at := c.resources[resource]; at.After(c.all) {
		return at
	}

	return c.all
}

// publish notes next published at at over previous, or as the first version
// when previous is nil.
func (c *readChanges) publish(previous *Spec, next Spec, at time.Time) {
	if c.resources == nil {
		c.resources = map[string]time.Time{}
	}
	if previous == nil {
		c.all, c.complete = at, at
		return
	}
	resources, all := connectorReads(*previous, next)
	if all {
		c.all, c.complete = at, at
	}
	for _, resource := range resources {
		c.resources[resource] = at
		if next.reconciles(resource) {
			c.complete = at
		}
	}
	for _, m := range next.Mappings {
		if slices.ContainsFunc(previous.Mappings, func(p Mapping) bool { return sameMapping(p, m) }) {
			continue
		}
		c.resources[m.Resource] = at
		if !m.Upsert() {
			c.complete = at
		}
	}
}

// resourceReader is a config that reads each resource with a part of its
// own, so a change to one part reads only that resource differently.
type resourceReader interface {
	readsWith(resource string) any
}

// connectorReads lists the resources next's connector reads differently from
// previous's, or says it reads all of them differently. A connector without
// a part per resource reads them all with all of its config: an instance,
// a token that sees more or less, which projects, so any change counts for
// all.
func connectorReads(previous, next Spec) (resources []string, all bool) {
	switch {
	case previous.Connector.Type != next.Connector.Type:
		return nil, true
	case reflect.DeepEqual(previous.Connector.Config, next.Connector.Config):
		return nil, false
	}
	was, ok := previous.config.(resourceReader)
	is, isOK := next.config.(resourceReader)
	if !ok || !isOK {
		return nil, true
	}
	for _, resource := range next.config.Resources() {
		if !reflect.DeepEqual(was.readsWith(resource), is.readsWith(resource)) {
			resources = append(resources, resource)
		}
	}

	return resources, false
}

// reconciles reports whether a reconcile mapping reads resource.
func (s Spec) reconciles(resource string) bool {
	return slices.ContainsFunc(s.Mappings, func(m Mapping) bool { return m.Resource == resource && !m.Upsert() })
}

const readChangesSQL = `
		SELECT v.source, v.published_at FROM config_versions v JOIN config_documents d ON d.id = v.document_id
		WHERE d.id = $1 AND v.version <= d.published_version ORDER BY v.version`

// readChanges are when an integration was last published to read
// differently, from its versions up to the published one.
func (s Store) readChanges(ctx context.Context, integrationID string) (readChanges, error) {
	var changes readChanges
	rows, err := s.db.QueryContext(ctx, readChangesSQL, integrationID)
	if err != nil {
		return changes, err
	}
	defer rows.Close()
	var previous *Spec
	for rows.Next() {
		var (
			source string
			at     time.Time
		)
		if err := rows.Scan(&source, &at); err != nil {
			return changes, err
		}
		spec, _ := Parse(source)
		changes.publish(previous, spec, at)
		previous = &spec
	}

	return changes, rows.Err()
}
