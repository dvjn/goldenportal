package integration

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/dvjn/goldport/internal/auth"
	"github.com/dvjn/goldport/internal/catalog"
	"github.com/dvjn/goldport/internal/definition"
	"github.com/dvjn/goldport/internal/httpx"
	"gofr.dev/pkg/gofr"
)

const (
	// maxIngestBody is in bytes: 4 MiB.
	maxIngestBody     = 4 << 20
	maxIngestEntities = 1000

	modeUpsert   = "upsert"
	modeSnapshot = "snapshot"
	modeDelete   = "delete"
)

var (
	errNoPushIntegration = httpx.Error{Code: http.StatusNotFound, Message: "no push integration has that identifier"}
	errInvalidBody       = httpx.Error{Code: http.StatusBadRequest, Message: "the request body is not valid JSON"}
	errBodyTooLarge      = httpx.Error{
		Code:    http.StatusRequestEntityTooLarge,
		Message: "the request body is larger than 4 MiB, push fewer entities at a time",
	}
	errBatchSize = httpx.Error{Code: http.StatusBadRequest, Message: "push 1 to 1000 entities at a time"}
	errMode      = httpx.Error{Code: http.StatusBadRequest, Message: "the mode must be upsert or snapshot"}
)

// ingestRequest is a push. Entities are records the kind's mapping reads,
// or entities as they are for a mapping without expressions.
type ingestRequest struct {
	Mode     string `json:"mode"`
	Force    bool   `json:"force"`
	Entities []any  `json:"entities"`
}

type ingestResponse struct {
	Run      int64    `json:"run"`
	Status   Status   `json:"status"`
	Upserted int      `json:"upserted"`
	Deleted  int      `json:"deleted"`
	Warnings []string `json:"warnings"`
}

func (s *Service) ingest(ctx *gofr.Context) (any, error) {
	set, integration, mapping, err := s.pushMapping(ctx)
	if err != nil {
		return nil, err
	}
	run := s.pushRun(ctx, integration, mapping)
	request, err := readIngest(ctx)
	if err == nil {
		run.Mode = request.Mode
		err = s.receive(ctx, set, integration, mapping, request, &run)
	}

	return s.recordPush(ctx, run, err)
}

func (s *Service) ingestDelete(ctx *gofr.Context) (any, error) {
	set, integration, mapping, err := s.pushMapping(ctx)
	if err != nil {
		return nil, err
	}
	run := s.pushRun(ctx, integration, mapping)
	run.Mode = modeDelete
	blueprint, _ := set.Blueprint(mapping.Blueprint)
	deleted, err := s.entities.DeleteOwned(ctx, integration.Identifier, mapping.Blueprint, ctx.PathParam("identifier"))
	switch {
	case err == nil && !deleted:
		err = httpx.Error{
			Code:    http.StatusNotFound,
			Message: fmt.Sprintf("the %s integration owns no %s with that identifier", integration.Title, blueprint.Title),
		}
	case err == nil:
		run.Counts.Deleted = 1
	}

	return s.recordPush(ctx, run, err)
}

// pushMapping returns the definitions, the push integration the path names
// and its mapping for the path's kind.
func (s *Service) pushMapping(ctx *gofr.Context) (definition.Set, *definition.Integration, definition.Mapping, error) {
	set, _, err := s.definitions.Definitions(ctx)
	if err != nil {
		return definition.Set{}, nil, definition.Mapping{}, err
	}
	integration, ok := set.Integration(ctx.PathParam(httpx.IntegrationParam))
	if !ok || integration.Pull() {
		return definition.Set{}, nil, definition.Mapping{}, errNoPushIntegration
	}
	mapping, ok := integration.Mapping(ctx.PathParam("kind"))
	if !ok {
		return definition.Set{}, nil, definition.Mapping{}, httpx.Error{
			Code:    http.StatusNotFound,
			Message: fmt.Sprintf("the %s integration receives no kind %q, it receives %s", integration.Title, ctx.PathParam("kind"), kindsOf(integration)),
		}
	}

	return set, integration, mapping, nil
}

func kindsOf(integration *definition.Integration) string {
	kinds := make([]string, 0, len(integration.Mappings))
	for _, mapping := range integration.Mappings {
		kinds = append(kinds, mapping.Kind)
	}

	return cmp.Or(strings.Join(kinds, ", "), "no kinds")
}

func (s *Service) pushRun(ctx *gofr.Context, integration *definition.Integration, mapping definition.Mapping) Run {
	principal, _ := auth.PrincipalFrom(ctx)
	return Run{
		Integration: integration.Identifier,
		Trigger:     TriggerPush,
		Actor:       principal.Actor().Label,
		Kind:        mapping.Kind,
		StartedAt:   s.now(),
	}
}

func readIngest(ctx *gofr.Context) (ingestRequest, error) {
	body := http.MaxBytesReader(httpx.RawWriter(ctx), httpx.RawRequest(ctx).Body, maxIngestBody)
	var request ingestRequest
	err := json.NewDecoder(body).Decode(&request)
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		return ingestRequest{}, errBodyTooLarge
	case err != nil:
		return ingestRequest{}, errInvalidBody
	}
	request.Mode = cmp.Or(request.Mode, modeUpsert)
	switch {
	case request.Mode != modeUpsert && request.Mode != modeSnapshot:
		return ingestRequest{}, errMode
	case len(request.Entities) < 1 || len(request.Entities) > maxIngestEntities:
		return ingestRequest{}, errBatchSize
	}

	return request, nil
}

// receive maps the pushed records and writes the entities, refusing the
// whole push when any record makes no entity.
func (s *Service) receive(ctx context.Context, set definition.Set, integration *definition.Integration, mapping definition.Mapping, request ingestRequest, run *Run) error {
	mapper, err := NewMapper(set, mapping)
	if err != nil {
		return err
	}
	entities, problems := mapper.Map(ctx, request.Entities)
	run.Counts.Fetched = len(request.Entities)
	if len(problems) > 0 {
		for _, problem := range problems {
			run.Warnings = append(run.Warnings, problem.String())
		}
		return detailedError{
			status:  http.StatusUnprocessableEntity,
			message: problemsMessage(len(problems)),
			details: map[string]any{"problems": problems},
		}
	}

	return s.write(ctx, catalog.OwnedWrite{
		Integration: integration.Identifier,
		Groups:      []catalog.OwnedGroup{{Blueprint: mapper.Blueprint(), Entities: entities}},
		Sweep:       request.Mode == modeSnapshot,
		Force:       request.Force,
	}, run, pushGuardWarning)
}

func problemsMessage(count int) string {
	if count == 1 {
		return "a record has a problem, fix it and push again, nothing was written"
	}

	return fmt.Sprintf("the records have %d problems, fix them and push again, nothing was written", count)
}

// write writes the entities and adds what the write did to the run.
func (s *Service) write(ctx context.Context, write catalog.OwnedWrite, run *Run, guardWarning func(catalog.GuardedSweep) string) error {
	result, err := s.entities.WriteOwned(ctx, write)
	var conflict catalog.ConflictError
	if errors.As(err, &conflict) {
		return detailedError{
			status:  http.StatusConflict,
			message: conflict.Error(),
			details: map[string]any{"conflicts": conflict.Conflicts},
		}
	}
	if err != nil {
		return err
	}
	run.Counts.Upserted, run.Counts.Deleted = result.Upserted, result.Deleted
	for _, link := range result.Dropped {
		run.Warnings = append(run.Warnings, fmt.Sprintf("%s: dropped the %s link to %s %s, which does not exist",
			link.Source.Identifier, link.Relation, link.Target.Blueprint, link.Target.Identifier))
	}
	for _, guarded := range result.Guarded {
		run.Warnings = append(run.Warnings, guardWarning(guarded))
	}

	return nil
}

func pushGuardWarning(guarded catalog.GuardedSweep) string {
	return fmt.Sprintf("kept %d %s entities that the snapshot leaves out, because it holds %d of the %d the integration had, "+
		"fewer than half; push the snapshot again with force set to delete them",
		guarded.Kept, guarded.Blueprint, guarded.Written, guarded.Existing)
}

// recordPush records the finished push and answers with it, or with the
// error that ended it.
func (s *Service) recordPush(ctx *gofr.Context, run Run, err error) (any, error) {
	run.finish(s.now(), failureText(err))
	recorded, recordErr := s.runs.Record(ctx, run)
	switch {
	case err != nil && recordErr != nil:
		ctx.Errorf("record the failed push to %s: %v", run.Integration, recordErr)
		return nil, err
	case err != nil:
		return nil, err
	case recordErr != nil:
		return nil, recordErr
	}

	return ingestResponse{
		Run:      recorded.ID,
		Status:   recorded.Status,
		Upserted: recorded.Counts.Upserted,
		Deleted:  recorded.Counts.Deleted,
		Warnings: nonNil(recorded.Warnings),
	}, nil
}
