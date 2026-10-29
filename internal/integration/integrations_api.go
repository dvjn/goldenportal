package integration

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/dvjn/goldport/internal/audit"
	"github.com/dvjn/goldport/internal/auth"
	"github.com/dvjn/goldport/internal/catalog"
	"github.com/dvjn/goldport/internal/definition"
	"github.com/dvjn/goldport/internal/httpx"
	"gofr.dev/pkg/gofr"
)

var errNoIntegration = httpx.Error{Code: http.StatusNotFound, Message: "no integration has that identifier"}

type runView struct {
	ID         int64     `json:"id"`
	Trigger    Trigger   `json:"trigger"`
	Actor      string    `json:"actor"`
	Kind       string    `json:"kind,omitempty"`
	Mode       string    `json:"mode,omitempty"`
	Status     Status    `json:"status"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at,omitzero"`
	Counts     Counts    `json:"counts"`
	Warnings   []string  `json:"warnings"`
	Error      string    `json:"error,omitempty"`
}

func newRunView(run Run) runView {
	return runView{
		ID:         run.ID,
		Trigger:    run.Trigger,
		Actor:      run.Actor,
		Kind:       run.Kind,
		Mode:       run.Mode,
		Status:     run.Status,
		StartedAt:  run.StartedAt,
		FinishedAt: run.FinishedAt,
		Counts:     run.Counts,
		Warnings:   nonNil(run.Warnings),
		Error:      run.Error,
	}
}

const maxRuns = keptRuns

type integrationSummary struct {
	Identifier  string               `json:"identifier"`
	Title       string               `json:"title"`
	Description string               `json:"description,omitempty"`
	Connector   definition.Connector `json:"connector"`
	Manual      bool                 `json:"manual"`
	Pull        bool                 `json:"pull"`
	Interval    string               `json:"interval,omitempty"`
	EntityCount int                  `json:"entity_count"`
	LastRun     *runView             `json:"last_run,omitempty"`
	NextRunAt   time.Time            `json:"next_run_at,omitzero"`
}

type blueprintCount struct {
	Blueprint string `json:"blueprint"`
	Title     string `json:"title"`
	Count     int    `json:"count"`
}

type kindView struct {
	Kind           string `json:"kind"`
	Blueprint      string `json:"blueprint"`
	BlueprintTitle string `json:"blueprint_title"`
	Canonical      bool   `json:"canonical"`
}

type secretView struct {
	Name     string `json:"name"`
	Variable string `json:"variable"`
}

// integrationDetail adds what the integration's page shows. IngestURL is
// where a push integration receives entities, followed by /<kind>.
type integrationDetail struct {
	integrationSummary
	Entities  []blueprintCount `json:"entities"`
	Kinds     []kindView       `json:"kinds"`
	Secrets   []secretView     `json:"secrets"`
	IngestURL string           `json:"ingest_url,omitempty"`
}

// overview is what every integration's summary is made from.
type overview struct {
	set    definition.Set
	counts []catalog.OwnedCount
	latest map[string]Run
}

func (s *Service) overview(ctx context.Context) (overview, error) {
	set, _, err := s.definitions.Definitions(ctx)
	if err != nil {
		return overview{}, err
	}
	counts, err := s.counts.OwnedCounts(ctx)
	if err != nil {
		return overview{}, err
	}
	latest, err := s.runs.Latest(ctx)

	return overview{set: set, counts: counts, latest: latest}, err
}

// integrations are the built-in manual integration and the defined ones.
func (o overview) integrations() []*definition.Integration {
	return append([]*definition.Integration{definition.ManualIntegration()}, o.set.Integrations()...)
}

func (o overview) find(identifier string) (*definition.Integration, bool) {
	integrations := o.integrations()
	index := slices.IndexFunc(integrations, func(integration *definition.Integration) bool {
		return integration.Identifier == identifier
	})
	if index < 0 {
		return nil, false
	}

	return integrations[index], true
}

func (s *Service) summary(ctx context.Context, o overview, integration *definition.Integration) (integrationSummary, error) {
	summary := integrationSummary{
		Identifier:  integration.Identifier,
		Title:       integration.Title,
		Description: integration.Description,
		Connector:   integration.Connector,
		Manual:      integration.Identifier == definition.Manual,
		Pull:        integration.Pull(),
		Interval:    integration.Interval,
	}
	for _, count := range o.counts {
		if count.Integration == integration.Identifier {
			summary.EntityCount += count.Count
		}
	}
	if run, ok := o.latest[integration.Identifier]; ok {
		view := newRunView(run)
		summary.LastRun = &view
	}
	if !integration.Pull() {
		return summary, nil
	}
	lastPull, err := s.runs.LastPull(ctx, integration.Identifier)
	if err != nil {
		return integrationSummary{}, err
	}
	summary.NextRunAt, _ = s.scheduler.Next(integration, lastPull)

	return summary, nil
}

type integrationsResponse struct {
	Integrations []integrationSummary `json:"integrations"`
}

func (s *Service) listIntegrations(ctx *gofr.Context) (any, error) {
	o, err := s.overview(ctx)
	if err != nil {
		return nil, err
	}
	response := integrationsResponse{Integrations: []integrationSummary{}}
	for _, integration := range o.integrations() {
		summary, err := s.summary(ctx, o, integration)
		if err != nil {
			return nil, err
		}
		response.Integrations = append(response.Integrations, summary)
	}

	return response, nil
}

func (s *Service) getIntegration(ctx *gofr.Context) (any, error) {
	o, err := s.overview(ctx)
	if err != nil {
		return nil, err
	}
	integration, ok := o.find(ctx.PathParam(httpx.IntegrationParam))
	if !ok {
		return nil, errNoIntegration
	}
	summary, err := s.summary(ctx, o, integration)
	if err != nil {
		return nil, err
	}
	detail := integrationDetail{
		integrationSummary: summary,
		Entities:           []blueprintCount{},
		Kinds:              []kindView{},
		Secrets:            []secretView{},
	}
	for _, count := range o.counts {
		if count.Integration == integration.Identifier {
			blueprint, _ := o.set.Blueprint(count.Blueprint)
			detail.Entities = append(detail.Entities, blueprintCount{Blueprint: count.Blueprint, Title: titleOf(blueprint, count.Blueprint), Count: count.Count})
		}
	}
	for _, mapping := range integration.Mappings {
		blueprint, _ := o.set.Blueprint(mapping.Blueprint)
		detail.Kinds = append(detail.Kinds, kindView{
			Kind: mapping.Kind, Blueprint: mapping.Blueprint, BlueprintTitle: titleOf(blueprint, mapping.Blueprint), Canonical: mapping.Canonical(),
		})
	}
	for name, secret := range integration.Secrets.All() {
		detail.Secrets = append(detail.Secrets, secretView{Name: name, Variable: secret.SecretRef})
	}
	if integration.Connector == definition.ConnectorPush {
		detail.IngestURL = s.ingestURL(integration)
	}

	return detail, nil
}

func (s *Service) ingestURL(integration *definition.Integration) string {
	return s.publicURL + "/api/v1/ingest/" + integration.Identifier
}

func titleOf(blueprint *definition.Blueprint, identifier string) string {
	if blueprint == nil {
		return identifier
	}

	return blueprint.Title
}

type runsResponse struct {
	Runs []runView `json:"runs"`
}

func (s *Service) listRuns(ctx *gofr.Context) (any, error) {
	o, err := s.overview(ctx)
	if err != nil {
		return nil, err
	}
	integration, ok := o.find(ctx.PathParam(httpx.IntegrationParam))
	if !ok {
		return nil, errNoIntegration
	}
	runs, err := s.runs.List(ctx, integration.Identifier, maxRuns)
	if err != nil {
		return nil, err
	}
	response := runsResponse{Runs: make([]runView, 0, len(runs))}
	for _, run := range runs {
		response.Runs = append(response.Runs, newRunView(run))
	}

	return response, nil
}

// integration returns the definitions and the integration the path names.
func (s *Service) integration(ctx *gofr.Context) (definition.Set, *definition.Integration, error) {
	set, _, err := s.definitions.Definitions(ctx)
	if err != nil {
		return definition.Set{}, nil, err
	}
	integration, ok := set.Integration(ctx.PathParam(httpx.IntegrationParam))
	if !ok {
		return definition.Set{}, nil, errNoIntegration
	}

	return set, integration, nil
}

// syncNow starts a sync of a pull integration and answers with the running
// run; the sync goes on after the response.
func (s *Service) syncNow(ctx *gofr.Context) (any, error) {
	set, integration, err := s.integration(ctx)
	if err != nil {
		return nil, err
	}
	if !integration.Pull() {
		return nil, httpx.Error{
			Code:    http.StatusConflict,
			Message: fmt.Sprintf("the %s integration receives pushes, it has nothing to sync", integration.Title),
		}
	}
	principal, _ := auth.PrincipalFrom(ctx)
	run, perform, err := s.syncer.Start(ctx, syncStart{
		set: set, integration: integration, trigger: TriggerManual, actor: principal.Actor().Label(), due: always,
	})
	if err != nil {
		return nil, err
	}
	go perform(context.WithoutCancel(ctx.Context))
	if err := s.audit.Record(ctx, audit.Entry{
		Actor:    principal.Actor(),
		Action:   audit.IntegrationSynced,
		Resource: audit.Resource{Kind: audit.ResourceIntegration, ID: integration.Identifier, Name: integration.Title},
		Details:  map[string]any{"run": run.ID},
	}); err != nil {
		return nil, err
	}

	return newRunView(run), nil
}
