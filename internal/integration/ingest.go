package integration

import (
	"context"
	"errors"
	"net/http"
	"time"

	"gofr.dev/pkg/gofr"

	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/entity"
	"github.com/dvjn/goldenportal/internal/httpx"
)

// writer is the integration an ingestion token signs in as.
type writer struct {
	id      string
	tokenID string
	spec    Spec
}

// authenticate checks the ingestion token is for the integration in the
// path, and that the integration is published.
func authenticate(ctx *gofr.Context) (writer, error) {
	secret := auth.IngestionSecret(ctx)
	if secret == "" {
		return writer{}, httpx.Error{
			Code:    http.StatusUnauthorized,
			Message: `send the ingestion token as "Authorization: Bearer <token>"`,
		}
	}
	tokenID, integration, err := NewStore(ctx.SQL).TokenIntegration(ctx, auth.HashSecret(secret))
	if errors.Is(err, ErrTokenNotFound) {
		return writer{}, httpx.Error{Code: http.StatusUnauthorized, Message: "the ingestion token is invalid or revoked"}
	}
	if err != nil {
		return writer{}, err
	}
	if integration != ctx.PathParam("integration") {
		return writer{}, httpx.Error{Code: http.StatusForbidden, Message: "this ingestion token is for the " + integration + " integration"}
	}

	store := configdoc.NewStore(ctx.SQL, Name)
	record, err := store.Get(ctx, integration)
	if err != nil {
		return writer{}, err
	}
	if !record.Published() {
		return writer{}, httpx.Error{Code: http.StatusConflict, Message: "publish the integration before ingesting"}
	}
	spec, _ := Parse(record.PublishedSource)
	if c, _ := connector(spec.Connector.Type); c.Mode != ModePush {
		return writer{}, httpx.Error{Code: http.StatusConflict, Message: "the " + integration + " integration pulls its data, so nothing can push to it"}
	}
	id, err := store.PublishedID(ctx, integration)

	return writer{id: id, tokenID: tokenID, spec: spec}, err
}

// ingest maps the request body to entities and writes them. Items that
// fail are reported without stopping the rest.
func ingest(ctx *gofr.Context) (any, error) {
	started := time.Now().UTC()
	w, err := authenticate(ctx)
	if err != nil {
		return nil, err
	}
	var data any
	if err := ctx.Bind(&data); err != nil {
		return nil, badRequest("the request body is not valid JSON")
	}

	items, err := w.spec.Map(data)
	if err != nil {
		return nil, httpx.Error{Code: http.StatusUnprocessableEntity, Message: err.Error()}
	}

	run := &Run{StartedAt: started, Trigger: TriggerPush, Received: len(items), Errors: []RunError{}}
	if _, err := write(ctx, ctx.SQL, w.id, items, run); err != nil {
		return nil, err
	}

	if err := NewStore(ctx.SQL).RecordRun(ctx, w.id, w.tokenID, run); err != nil {
		return nil, err
	}

	return run, nil
}

// deleteEntity removes an entity the integration wrote.
func deleteEntity(ctx *gofr.Context) (any, error) {
	started := time.Now().UTC()
	w, err := authenticate(ctx)
	if err != nil {
		return nil, err
	}
	blueprintID, err := configdoc.NewStore(ctx.SQL, blueprint.Name).PublishedID(ctx, ctx.PathParam("blueprint"))
	if errors.Is(err, configdoc.ErrNotFound) {
		return nil, httpx.Error{Code: http.StatusNotFound, Message: "no such blueprint"}
	}
	if err != nil {
		return nil, err
	}

	err = entity.NewStore(ctx.SQL).Delete(ctx, blueprintID, w.id, ctx.PathParam("identifier"))
	var owned entity.OwnedError
	switch {
	case errors.Is(err, entity.ErrNotFound):
		return nil, httpx.Error{Code: http.StatusNotFound, Message: "no such entity"}
	case errors.As(err, &owned):
		return nil, httpx.Error{Code: http.StatusForbidden, Message: owned.Error()}
	case err != nil:
		return nil, err
	}

	run := &Run{StartedAt: started, Trigger: TriggerPush, Received: 1, Deleted: 1, Errors: []RunError{}}

	return nil, NewStore(ctx.SQL).RecordRun(ctx, w.id, w.tokenID, run)
}

// write validates items against their blueprints and writes them as the
// integration, counting outcomes in run. Items that fail are reported
// without stopping the rest. It returns the entities it saw, written or
// not, by blueprint, so a full sync knows what not to delete.
func write(ctx context.Context, db configdoc.DB, integrationID string, items []Item, run *Run) (map[string][]string, error) {
	blueprints, err := blueprint.Published(ctx, db)
	if err != nil {
		return nil, err
	}
	blueprintIDs := map[string]string{}
	seen := map[string][]string{}
	entities := entity.NewStore(db)

	for i, item := range items {
		if item.Entity.Identifier != "" {
			seen[item.Blueprint] = append(seen[item.Blueprint], item.Entity.Identifier)
		}
		fail := func(problems ...string) {
			run.Failed++
			if len(run.Errors) < MaxRunErrors {
				run.Errors = append(run.Errors, RunError{
					Index: i, Blueprint: item.Blueprint,
					Identifier: item.Entity.Identifier, Problems: problems,
				})
			}
		}
		if len(item.Problems) > 0 {
			fail(item.Problems...)
			continue
		}
		spec, ok := blueprints[item.Blueprint]
		if !ok {
			fail("no blueprint " + item.Blueprint + " is published")
			continue
		}
		if _, ok := blueprintIDs[item.Blueprint]; !ok {
			if blueprintIDs[item.Blueprint], err = configdoc.NewStore(db, blueprint.Name).PublishedID(ctx, item.Blueprint); err != nil {
				return nil, err
			}
		}

		clean, problems := spec.ValidateEntity(item.Entity)
		if len(problems) > 0 {
			fail(problems...)
			continue
		}
		computed, _ := spec.Compute(clean)
		outcome, err := entities.Upsert(ctx, blueprintIDs[item.Blueprint], integrationID, clean, computed)
		var owned entity.OwnedError
		switch {
		case errors.As(err, &owned):
			fail(owned.Error())
			continue
		case err != nil:
			return nil, err
		}
		switch outcome {
		case entity.Created:
			run.Created++
		case entity.Updated:
			run.Updated++
		default:
			run.Unchanged++
		}
	}

	return seen, nil
}
