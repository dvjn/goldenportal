package integration

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"gofr.dev/pkg/gofr"

	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/entity"
	"github.com/dvjn/goldenportal/internal/httpx"
)

// Run modes.
const (
	ModeComplete    = "complete"
	ModeIncremental = "incremental"
)

var (
	// ErrUnverified is a push that came with neither an ingestion token nor
	// a signature the connector can check.
	ErrUnverified = errors.New(`send the ingestion token as "Authorization: Bearer <token>"`)
	ErrNotJSON    = errors.New("the request body is not valid JSON")
)

// ReceiveError is a request a connector couldn't read.
type ReceiveError struct{ Reason string }

func (e ReceiveError) Error() string { return e.Reason }

// FetchError is a request a connector couldn't complete because its system
// didn't answer what it asked. Nothing the request carried was written, so
// the sender may try it again, except an event, which the next sync
// catches up on.
type FetchError struct{ Err error }

func (e FetchError) Error() string { return e.Err.Error() }

func (e FetchError) Unwrap() error { return e.Err }

// receiveTimeout keeps a connector that reads its system while receiving
// within the ten seconds webhook senders commonly wait for an answer.
const receiveTimeout = 8 * time.Second

// writer is the integration a push writes as.
type writer struct {
	id      string
	tokenID string
	spec    Spec
}

// authenticate finds the integration in the path, and checks the ingestion
// token is for it, if one was sent. Without one, it only says the request
// is unverified, so unsigned requests can't learn which integrations exist.
func authenticate(ctx *gofr.Context, secret string) (writer, error) {
	identifier := ctx.PathParam("integration")
	var tokenID string
	if secret != "" {
		var (
			integration string
			err         error
		)
		tokenID, integration, err = NewStore(ctx.SQL).TokenIntegration(ctx, auth.HashSecret(secret))
		if errors.Is(err, ErrTokenNotFound) {
			return writer{}, httpx.Error{Code: http.StatusUnauthorized, Message: "the ingestion token is invalid or revoked"}
		}
		if err != nil {
			return writer{}, err
		}
		if integration != identifier {
			return writer{}, httpx.Error{Code: http.StatusForbidden, Message: "this ingestion token is for the " + integration + " integration"}
		}
	}

	store := configdoc.NewStore(ctx.SQL, Name)
	record, err := store.Get(ctx, identifier)
	switch {
	case secret == "" && (errors.Is(err, configdoc.ErrNotFound) || (err == nil && !record.Published())):
		return writer{}, unauthorized(ErrUnverified)
	case err != nil:
		return writer{}, err
	case !record.Published():
		return writer{}, httpx.Error{Code: http.StatusConflict, Message: "publish the integration before ingesting"}
	}
	spec, _ := Parse(record.PublishedSource)
	id, err := store.PublishedID(ctx, identifier)

	return writer{id: id, tokenID: tokenID, spec: spec}, err
}

func unauthorized(err error) error {
	return httpx.Error{Code: http.StatusUnauthorized, Message: err.Error()}
}

// ingest hands a request to the integration's connector and writes the
// items it gives. Items that fail are reported without stopping the rest.
// ?mode=complete, with an ingestion token, also deletes what neither it nor the complete run before it saw
// of the resources it covers: ?resources=, or every resource the
// integration receives.
func ingest(ctx *gofr.Context) (any, error) {
	in := auth.IngestRequest(ctx)
	w, err := authenticate(ctx, in.Secret)
	if err != nil {
		return nil, err
	}
	receiver, ok := w.spec.config.(Receiver)
	if !ok || !w.spec.Receives {
		if in.Secret == "" {
			return nil, unauthorized(ErrUnverified)
		}
		return nil, httpx.Error{
			Code:    http.StatusConflict,
			Message: "the " + w.spec.Identifier + " integration takes no pushes; give one of its resources a push side",
		}
	}

	mode := ModeIncremental
	switch m := in.Query.Get("mode"); m {
	case "", ModeIncremental:
	case ModeComplete:
		if in.Secret == "" {
			return nil, httpx.Error{Code: http.StatusForbidden, Message: "a complete push needs an ingestion token"}
		}
		if w.spec.Events {
			return nil, httpx.Error{
				Code:    http.StatusConflict,
				Message: "the " + w.spec.Identifier + " integration takes events, each about one object, so a push can't be complete; its syncs are",
			}
		}
		mode = ModeComplete
	default:
		return nil, badRequest("mode must be complete or incremental")
	}
	resources := receiver.Receives()
	if r := in.Query.Get("resources"); r != "" {
		resources = strings.Split(r, ",")
		for _, name := range resources {
			if !slices.Contains(receiver.Receives(), name) {
				return nil, badRequest("resources must name resources the integration receives: " + strings.Join(receiver.Receives(), ", "))
			}
		}
	}

	started, err := dbNow(ctx, ctx.SQL)
	if err != nil {
		return nil, err
	}
	// The connector reads only what a mapping reads, since it may ask its
	// system for each.
	mapped := slices.DeleteFunc(slices.Clone(resources), func(r string) bool { return !slices.Contains(w.spec.mappedResources(), r) })
	receiveCtx, cancel := context.WithTimeout(ctx, receiveTimeout)
	request := Request{Resources: mapped, Header: in.Header, Query: in.Query, Body: in.Body, WebhookSecret: WebhookSecret(w.spec.Identifier)}
	items, err := receiver.Receive(receiveCtx, request, in.Secret != "")
	cancel()
	var (
		signatureErr SignatureError
		receiveErr   ReceiveError
		fetchErr     FetchError
	)
	switch {
	case errors.Is(err, ErrUnverified):
		return nil, unauthorized(err)
	case errors.As(err, &signatureErr):
		return nil, unauthorized(err)
	case errors.Is(err, ErrNotJSON):
		return nil, badRequest(err.Error())
	case errors.As(err, &receiveErr):
		return nil, httpx.Error{Code: http.StatusUnprocessableEntity, Message: err.Error()}
	case errors.As(err, &fetchErr):
		// Recorded, so it shows with the integration's runs, and answered as
		// a failure, so the sender's own log shows it to try again. An event
		// is answered as taken: the next sync reads what it was about anyway,
		// and a system that turns off a webhook whose deliveries keep
		// failing, as GitLab does, would otherwise stop sending them all.
		run := &Run{StartedAt: started, Trigger: TriggerPush, Mode: mode, Error: err.Error(), Errors: []RunError{}}
		if err := NewStore(ctx.SQL).RecordRun(ctx, w.id, w.tokenID, run); err != nil {
			return nil, err
		}
		if w.spec.Events {
			return run, nil
		}
		return nil, httpx.Error{Code: http.StatusBadGateway, Message: err.Error()}
	case err != nil:
		return nil, err
	}
	m, err := w.spec.mapper()
	if err != nil {
		return nil, err
	}

	run := &Run{StartedAt: started, Trigger: TriggerPush, Mode: mode, Errors: []RunError{}}
	unsafe, err := write(ctx, ctx.SQL, w.id, m.Map(items), run)
	if err != nil {
		return nil, err
	}
	if mode == ModeComplete {
		if run.Deleted, err = reconcile(ctx, ctx.SQL, w.id, w.spec, resources, unsafe, started); err != nil {
			return nil, err
		}
	}

	if err := NewStore(ctx.SQL).RecordRun(ctx, w.id, w.tokenID, run); err != nil {
		return nil, err
	}

	return run, nil
}

// deleteEntity removes an entity the integration wrote.
func deleteEntity(ctx *gofr.Context) (any, error) {
	started := time.Now().UTC()
	in := auth.IngestRequest(ctx)
	if in.Secret == "" {
		return nil, unauthorized(ErrUnverified)
	}
	w, err := authenticate(ctx, in.Secret)
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

	run := &Run{StartedAt: started, Trigger: TriggerPush, Mode: ModeIncremental, Received: 1, Deleted: 1, Errors: []RunError{}}

	return nil, NewStore(ctx.SQL).RecordRun(ctx, w.id, w.tokenID, run)
}

// dbNow is the database's clock, which seen_at is set by, so a complete run
// compares like with like.
func dbNow(ctx context.Context, db configdoc.DB) (time.Time, error) {
	var now time.Time
	err := db.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now)

	return now.UTC(), err
}

// write validates entries against their blueprints and writes or deletes
// them as the integration, counting outcomes in run. Entries that fail are
// reported without stopping the rest. It returns the blueprints a complete
// run mustn't reconcile, because an entry of theirs had no identifier and
// could have been any of their entities.
func write(ctx context.Context, db configdoc.DB, integrationID string, entries []Entry, run *Run) (map[string]bool, error) {
	blueprints, err := blueprint.Published(ctx, db)
	if err != nil {
		return nil, err
	}
	blueprintIDs := map[string]string{}
	unsafe := map[string]bool{}
	entities := entity.NewStore(db)

	for _, e := range entries {
		index := run.Received
		run.Received++
		if e.Entity.Identifier == "" {
			unsafe[e.Blueprint] = true
			e.Problems = append(e.Problems, "it has no identifier, so this run deleted no "+e.Blueprint+" entities")
		}
		fail := func(problems ...string) {
			run.Failed++
			if len(run.Errors) < MaxRunErrors {
				run.Errors = append(run.Errors, RunError{
					Index: index, Blueprint: e.Blueprint,
					Identifier: e.Entity.Identifier, Problems: problems,
				})
			}
		}
		spec, ok := blueprints[e.Blueprint]
		if !ok {
			fail(append(e.Problems, "no blueprint "+e.Blueprint+" is published")...)
			continue
		}
		if _, ok := blueprintIDs[e.Blueprint]; !ok {
			if blueprintIDs[e.Blueprint], err = configdoc.NewStore(db, blueprint.Name).PublishedID(ctx, e.Blueprint); err != nil {
				return nil, err
			}
		}
		// An item that can't be written still counts as seen, so a bad value
		// never deletes the entity it would have updated.
		failSeen := func(problems ...string) error {
			fail(problems...)
			if e.Deleted || e.Entity.Identifier == "" {
				return nil
			}
			return entities.Seen(ctx, blueprintIDs[e.Blueprint], integrationID, e.Entity.Identifier)
		}
		if len(e.Problems) > 0 {
			if err := failSeen(e.Problems...); err != nil {
				return nil, err
			}
			continue
		}

		var owned entity.OwnedError
		if e.Deleted {
			err := entities.Delete(ctx, blueprintIDs[e.Blueprint], integrationID, e.Entity.Identifier)
			switch {
			case errors.Is(err, entity.ErrNotFound):
				run.Unchanged++
			case errors.As(err, &owned):
				fail(owned.Error())
			case err != nil:
				return nil, err
			default:
				run.Deleted++
			}
			continue
		}

		clean, problems := spec.ValidateEntity(e.Entity)
		if len(problems) > 0 {
			if err := failSeen(problems...); err != nil {
				return nil, err
			}
			continue
		}
		computed, _ := spec.Compute(clean)
		outcome, err := entities.Upsert(ctx, blueprintIDs[e.Blueprint], integrationID, clean, computed)
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

	return unsafe, nil
}

// reconcile deletes, after a complete run over resources, the integration's
// entities that neither it nor the complete run before it saw, and
// marks those it missed for the first time. It only reconciles a blueprint
// when the run covered every resource its mappings name, and none of its
// entries lacked an identifier.
func reconcile(ctx context.Context, db configdoc.DB, integrationID string, spec Spec, covered []string,
	unsafe map[string]bool, started time.Time,
) (int, error) {
	var ids []string
	for _, bp := range spec.reconcilable(covered, unsafe) {
		id, err := configdoc.NewStore(db, blueprint.Name).PublishedID(ctx, bp)
		switch {
		case errors.Is(err, configdoc.ErrNotFound):
			continue
		case err != nil:
			return 0, err
		}
		ids = append(ids, id)
	}

	if len(ids) == 0 {
		return 0, nil
	}
	published, err := publishedAt(ctx, db, integrationID)
	if err != nil {
		return 0, err
	}

	return entity.NewStore(db).Reconcile(ctx, integrationID, ids, started, published)
}

// publishedAt is when the integration's current version was published.
func publishedAt(ctx context.Context, db configdoc.DB, integrationID string) (time.Time, error) {
	var at time.Time
	err := db.QueryRowContext(ctx, `
		SELECT v.published_at FROM config_versions v JOIN config_documents d
			ON d.id = v.document_id AND v.version = d.published_version WHERE d.id = $1`, integrationID).Scan(&at)

	return at, err
}

// reconcilable lists, in order, the blueprints a complete run over covered
// can reconcile: those whose mappings' resources it all covered, and whose
// entries all had identifiers.
func (s Spec) reconcilable(covered []string, unsafe map[string]bool) []string {
	whole := map[string]bool{}
	for _, m := range s.Mappings {
		if _, seen := whole[m.Blueprint]; !seen {
			whole[m.Blueprint] = true
		}
		if !slices.Contains(covered, m.Resource) {
			whole[m.Blueprint] = false
		}
	}
	var out []string
	for bp, ok := range whole {
		if ok && !unsafe[bp] {
			out = append(out, bp)
		}
	}
	slices.Sort(out)

	return out
}
