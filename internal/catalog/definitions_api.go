package catalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dvjn/goldport/internal/audit"
	"github.com/dvjn/goldport/internal/auth"
	"github.com/dvjn/goldport/internal/definition"
	"github.com/dvjn/goldport/internal/httpx"
	"gofr.dev/pkg/gofr"
)

const (
	defaultRevisionLimit = 50
	maxRevisionLimit     = 200
	maxMessageLength     = 500
	auditTargetRevision  = "definitions_revision"
)

var (
	errInvalidBody     = httpx.Error{Code: http.StatusBadRequest, Message: "the request body is not valid JSON"}
	errRevisionMissing = httpx.Error{Code: http.StatusNotFound, Message: "no revision has that number"}
	errPlanGone        = httpx.Error{
		Code:    http.StatusNotFound,
		Message: "the plan expired or was already applied, plan the change again",
	}
	errPlanStale = httpx.Error{
		Code:    http.StatusConflict,
		Message: "the definitions changed since this plan, plan the change again",
	}
	errMessageLength = httpx.Error{
		Code:    http.StatusBadRequest,
		Message: "keep the message to at most 500 characters",
	}
)

// problemsError answers a plan whose definitions have problems, listing
// each with where it is.
type problemsError struct {
	problems []definition.Problem
}

func (e problemsError) Error() string {
	if len(e.problems) == 1 {
		return "the definitions have a problem, fix it and plan again"
	}

	return fmt.Sprintf("the definitions have %d problems, fix them and plan again", len(e.problems))
}

func (e problemsError) StatusCode() int { return http.StatusUnprocessableEntity }

func (e problemsError) Response() map[string]any { return map[string]any{"problems": e.problems} }

type documentResponse struct {
	Kind        definition.Kind     `json:"kind"`
	Identifier  string              `json:"identifier"`
	Title       string              `json:"title"`
	Description string              `json:"description,omitempty"`
	YAML        string              `json:"yaml"`
	Document    definition.Document `json:"document"`
}

type definitionsResponse struct {
	Revision  int                `json:"revision"`
	Documents []documentResponse `json:"documents"`
}

func newDocumentsResponse(set definition.Set) ([]documentResponse, error) {
	documents := make([]documentResponse, 0, len(set.Documents))
	for _, document := range set.Documents {
		text, err := definition.DocumentYAML(document)
		if err != nil {
			return nil, err
		}
		header := document.Heading()
		documents = append(documents, documentResponse{
			Kind:        header.Kind,
			Identifier:  header.Identifier,
			Title:       header.Title,
			Description: header.Description,
			YAML:        string(text),
			Document:    document,
		})
	}

	return documents, nil
}

func (s *Service) listDefinitions(ctx *gofr.Context) (any, error) {
	set, number, err := s.definitions.Definitions(ctx)
	if err != nil {
		return nil, err
	}
	documents, err := newDocumentsResponse(set)

	return definitionsResponse{Revision: number, Documents: documents}, err
}

func (s *Service) getSchema(*gofr.Context) (any, error) {
	return json.RawMessage(definition.SchemaJSON()), nil
}

type exportResponse struct {
	Revision int    `json:"revision"`
	YAML     string `json:"yaml"`
}

func (s *Service) exportDefinitions(ctx *gofr.Context) (any, error) {
	set, number, err := s.definitions.Definitions(ctx)
	if err != nil {
		return nil, err
	}
	text, err := set.YAML()

	return exportResponse{Revision: number, YAML: string(text)}, err
}

// planRequest asks for a plan. Source holds YAML or JSON documents. By
// default they are the whole desired set; with Partial they replace or add
// to the current documents, and Remove names documents to take out.
type planRequest struct {
	Source  string           `json:"source"`
	Partial bool             `json:"partial"`
	Remove  []definition.Ref `json:"remove"`
}

type planSummary struct {
	Added   int `json:"added"`
	Changed int `json:"changed"`
	Removed int `json:"removed"`
}

type planResponse struct {
	ID         string              `json:"id,omitempty"`
	ExpiresAt  time.Time           `json:"expires_at,omitzero"`
	Base       int                 `json:"base_revision"`
	RollbackOf int                 `json:"rollback_of,omitempty"`
	Summary    planSummary         `json:"summary"`
	Changes    []definition.Change `json:"changes"`
	Impact     Impact              `json:"impact"`
}

func summarize(changes []definition.Change) planSummary {
	var summary planSummary
	counters := map[definition.Action]*int{
		definition.Added:   &summary.Added,
		definition.Changed: &summary.Changed,
		definition.Removed: &summary.Removed,
	}
	for _, change := range changes {
		*counters[change.Action]++
	}

	return summary
}

func newPlanResponse(plan Plan) planResponse {
	return planResponse{
		ID:         plan.ID,
		ExpiresAt:  plan.ExpiresAt,
		Base:       plan.Base,
		RollbackOf: plan.RollbackOf,
		Summary:    summarize(plan.Changes),
		Changes:    nonNil(plan.Changes),
		Impact:     plan.Impact,
	}
}

func (s *Service) plan(ctx *gofr.Context) (any, error) {
	var request planRequest
	if err := ctx.Bind(&request); err != nil {
		return nil, errInvalidBody
	}
	current, base, err := s.definitions.Definitions(ctx)
	if err != nil {
		return nil, err
	}
	desired, source, problems := desiredSet(current, request)
	if len(problems) > 0 {
		return nil, problemsError{problems: problems}
	}
	if problems := source.Locate(desired.Check()); len(problems) > 0 {
		return nil, problemsError{problems: problems}
	}

	return s.propose(ctx, Plan{Base: base, Desired: desired}, current)
}

func desiredSet(current definition.Set, request planRequest) (definition.Set, definition.Source, []definition.Problem) {
	if request.Partial && strings.TrimSpace(request.Source) == "" {
		return current.Merge(nil, request.Remove), definition.Source{}, nil
	}
	source, problems := definition.Parse([]byte(request.Source))
	if request.Partial {
		return current.Merge(source.Documents, request.Remove), source, problems
	}

	return definition.NewSet(source.Documents...), source, problems
}

// propose works out the plan's changes and impact and keeps it to apply,
// unless it changes nothing.
func (s *Service) propose(ctx *gofr.Context, plan Plan, current definition.Set) (any, error) {
	changes, err := definition.Diff(current, plan.Desired)
	if err != nil {
		return nil, err
	}
	plan.Changes = changes
	plan.Effects = definition.EffectsOf(current, plan.Desired)
	if plan.Impact, err = s.definitions.Impact(ctx, plan.Effects); err != nil {
		return nil, err
	}
	if len(changes) > 0 {
		plan = s.plans.add(plan)
	}

	return newPlanResponse(plan), nil
}

func (s *Service) planRollback(ctx *gofr.Context) (any, error) {
	revision, err := s.revision(ctx)
	if err != nil {
		return nil, err
	}
	current, base, err := s.definitions.Definitions(ctx)
	if err != nil {
		return nil, err
	}
	if problems := revision.Snapshot.Check(); len(problems) > 0 {
		return nil, problemsError{problems: problems}
	}

	return s.propose(ctx, Plan{Base: base, RollbackOf: revision.Number, Desired: revision.Snapshot}, current)
}

type applyRequest struct {
	PlanID  string `json:"plan_id"`
	Message string `json:"message"`
}

func (s *Service) apply(ctx *gofr.Context) (any, error) {
	var request applyRequest
	if err := ctx.Bind(&request); err != nil {
		return nil, errInvalidBody
	}
	message := strings.TrimSpace(request.Message)
	if len(message) > maxMessageLength {
		return nil, errMessageLength
	}
	plan, ok := s.plans.take(request.PlanID)
	if !ok {
		return nil, errPlanGone
	}
	principal, _ := auth.PrincipalFrom(ctx)
	revision, err := s.definitions.Apply(ctx, plan, principal.Actor(), message)
	if errors.Is(err, ErrStale) {
		return nil, errPlanStale
	}
	if err != nil {
		return nil, err
	}
	if err := s.audit.Record(ctx, revisionEntry(principal.Actor(), revision)); err != nil {
		return nil, err
	}

	return newRevisionResponse(revision), nil
}

func revisionEntry(actor audit.Actor, revision Revision) audit.Entry {
	summary := summarize(revision.Changes)
	deleted := 0
	for _, group := range revision.Impact.DeletedEntities {
		deleted += group.Count
	}
	entry := audit.Entry{
		Actor:  actor,
		Action: audit.DefinitionsApplied,
		Target: audit.Target{Kind: auditTargetRevision, ID: strconv.Itoa(revision.Number), Name: fmt.Sprintf("Revision %d", revision.Number)},
		Details: map[string]any{
			"message":          revision.Message,
			"added":            summary.Added,
			"changed":          summary.Changed,
			"removed":          summary.Removed,
			"deleted_entities": deleted,
		},
	}
	if revision.RollbackOf > 0 {
		entry.Action = audit.DefinitionsRolledBack
		entry.Details["rollback_of"] = revision.RollbackOf
	}

	return entry
}

type revisionResponse struct {
	Number     int                 `json:"number"`
	Author     string              `json:"author"`
	Message    string              `json:"message"`
	RollbackOf int                 `json:"rollback_of,omitempty"`
	CreatedAt  time.Time           `json:"created_at"`
	Summary    planSummary         `json:"summary"`
	Changes    []definition.Change `json:"changes"`
	Impact     Impact              `json:"impact"`
	Documents  []documentResponse  `json:"documents,omitempty"`
}

func newRevisionResponse(revision Revision) revisionResponse {
	return revisionResponse{
		Number:     revision.Number,
		Author:     revision.Author.Label,
		Message:    revision.Message,
		RollbackOf: revision.RollbackOf,
		CreatedAt:  revision.CreatedAt,
		Summary:    summarize(revision.Changes),
		Changes:    nonNil(revision.Changes),
		Impact:     revision.Impact,
	}
}

type revisionsResponse struct {
	Revisions []revisionResponse `json:"revisions"`
	Offset    int                `json:"offset"`
	Limit     int                `json:"limit"`
	Total     int                `json:"total"`
}

func (s *Service) listRevisions(ctx *gofr.Context) (any, error) {
	page := Page{
		Offset: clamp(ctx.Param("offset"), 0, 0, math.MaxInt),
		Limit:  clamp(ctx.Param("limit"), defaultRevisionLimit, 1, maxRevisionLimit),
	}
	revisions, total, err := s.definitions.Revisions(ctx, page)
	if err != nil {
		return nil, err
	}
	response := revisionsResponse{Revisions: make([]revisionResponse, 0, len(revisions)), Offset: page.Offset, Limit: page.Limit, Total: total}
	for _, revision := range revisions {
		response.Revisions = append(response.Revisions, newRevisionResponse(revision))
	}

	return response, nil
}

func (s *Service) getRevision(ctx *gofr.Context) (any, error) {
	revision, err := s.revision(ctx)
	if err != nil {
		return nil, err
	}
	response := newRevisionResponse(revision)
	response.Documents, err = newDocumentsResponse(revision.Snapshot)

	return response, err
}

func (s *Service) revision(ctx *gofr.Context) (Revision, error) {
	number, err := strconv.Atoi(ctx.PathParam("number"))
	if err != nil {
		return Revision{}, errRevisionMissing
	}
	revision, err := s.definitions.Revision(ctx, number)
	if errors.Is(err, ErrNotFound) {
		return Revision{}, errRevisionMissing
	}

	return revision, err
}

type diffResponse struct {
	From    int                 `json:"from"`
	To      int                 `json:"to"`
	Changes []definition.Change `json:"changes"`
}

// diffRevisions lists the changes from one revision to another. Revision 0
// is the empty set before the first apply.
func (s *Service) diffRevisions(ctx *gofr.Context) (any, error) {
	numbers := make([]int, 2)
	sets := make([]definition.Set, 2)
	for index, name := range []string{"from", "to"} {
		number, err := strconv.Atoi(ctx.Param(name))
		if err != nil || number < 0 {
			return nil, errRevisionMissing
		}
		numbers[index] = number
		if number == 0 {
			continue
		}
		revision, err := s.definitions.Revision(ctx, number)
		if errors.Is(err, ErrNotFound) {
			return nil, errRevisionMissing
		}
		if err != nil {
			return nil, err
		}
		sets[index] = revision.Snapshot
	}
	changes, err := definition.Diff(sets[0], sets[1])

	return diffResponse{From: numbers[0], To: numbers[1], Changes: nonNil(changes)}, err
}

// clamp parses a query value, falls back when it is not a number and keeps
// it within the bounds.
func clamp(value string, fallback, lowest, highest int) int {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		parsed = fallback
	}

	return min(max(parsed, lowest), highest)
}
