package integration

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/entity"
)

// EntityError is why an entity couldn't be written or deleted.
type EntityError struct{ Problems []string }

func (e EntityError) Error() string { return strings.Join(e.Problems, "; ") }

// pushID returns a published push integration's row ID, which others,
// such as actions, write entities as.
func pushID(ctx context.Context, db configdoc.DB, name string) (string, error) {
	store := configdoc.NewStore(db, Name)
	record, err := store.Get(ctx, name)
	if errors.Is(err, configdoc.ErrNotFound) || (err == nil && !record.Published()) {
		return "", EntityError{[]string{"no integration " + name + " is published"}}
	}
	if err != nil {
		return "", err
	}
	spec, _ := Parse(record.PublishedSource)
	if c, _ := connector(spec.Connector.Type); c.Mode != ModePush {
		return "", EntityError{[]string{"the " + name + " integration pulls its data, so nothing else can write it"}}
	}

	return store.PublishedID(ctx, name)
}

// PushOnly reports a problem when name isn't a push integration's spec,
// for documents that write entities as it.
func PushOnly(spec Spec) string {
	if c, _ := connector(spec.Connector.Type); c.Mode != ModePush {
		return "the " + spec.Identifier + " integration pulls its data, so nothing else can write it; name a push integration"
	}

	return ""
}

// WriteEntity validates an entity and writes it as a push integration.
// With merge, the properties it leaves out keep their stored values. It
// returns the outcome: created, updated or unchanged.
func WriteEntity(ctx context.Context, db configdoc.DB, integration, blueprintName string, e blueprint.Entity, merge bool) (string, error) {
	integrationID, err := pushID(ctx, db, integration)
	if err != nil {
		return "", err
	}
	published, err := blueprint.Published(ctx, db)
	if err != nil {
		return "", err
	}
	spec, ok := published[blueprintName]
	if !ok {
		return "", EntityError{[]string{"no blueprint " + blueprintName + " is published"}}
	}
	blueprintID, err := configdoc.NewStore(db, blueprint.Name).PublishedID(ctx, blueprintName)
	if err != nil {
		return "", err
	}

	store := entity.NewStore(db)
	if merge {
		existing, err := store.Get(ctx, blueprintName, e.Identifier)
		switch {
		case errors.Is(err, entity.ErrNotFound):
		case err != nil:
			return "", err
		default:
			properties := map[string]any{}
			for _, prop := range spec.Properties {
				if value, ok := existing.Properties[prop.ID]; ok && !prop.Derived() {
					properties[prop.ID] = value
				}
			}
			for key, value := range e.Properties {
				properties[key] = value
			}
			e.Properties = properties
			if e.Title == "" {
				e.Title = existing.Title
			}
		}
	}

	clean, problems := spec.ValidateEntity(e)
	if len(problems) > 0 {
		return "", EntityError{problems}
	}
	computed, _ := spec.Compute(clean)
	outcome, err := store.Upsert(ctx, blueprintID, integrationID, clean, computed)
	var owned entity.OwnedError
	if errors.As(err, &owned) {
		return "", EntityError{[]string{owned.Error()}}
	}

	return outcome, err
}

// RemoveEntity deletes an entity a push integration wrote.
func RemoveEntity(ctx context.Context, db configdoc.DB, integration, blueprintName, identifier string) error {
	integrationID, err := pushID(ctx, db, integration)
	if err != nil {
		return err
	}
	blueprintID, err := configdoc.NewStore(db, blueprint.Name).PublishedID(ctx, blueprintName)
	if errors.Is(err, configdoc.ErrNotFound) {
		return EntityError{[]string{"no blueprint " + blueprintName + " is published"}}
	}
	if err != nil {
		return err
	}
	err = entity.NewStore(db).Delete(ctx, blueprintID, integrationID, identifier)
	var owned entity.OwnedError
	switch {
	case errors.Is(err, entity.ErrNotFound):
		return EntityError{[]string{fmt.Sprintf("there's no %s %s", blueprintName, identifier)}}
	case errors.As(err, &owned):
		return EntityError{[]string{owned.Error()}}
	}

	return err
}
