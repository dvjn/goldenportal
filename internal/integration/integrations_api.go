package integration

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/dvjn/goldport/internal/audit"
	"github.com/dvjn/goldport/internal/auth"
	"github.com/dvjn/goldport/internal/definition"
	"github.com/dvjn/goldport/internal/httpx"
	"gofr.dev/pkg/gofr"
)

const auditTargetIntegration = "integration"

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
		set: set, integration: integration, trigger: TriggerManual, actor: principal.Actor().Label, due: always,
	})
	if err != nil {
		return nil, err
	}
	go perform(context.WithoutCancel(ctx.Context))
	if err := s.audit.Record(ctx, audit.Entry{
		Actor:   principal.Actor(),
		Action:  audit.IntegrationSynced,
		Target:  audit.Target{Kind: auditTargetIntegration, ID: integration.Identifier, Name: integration.Title},
		Details: map[string]any{"run": run.ID},
	}); err != nil {
		return nil, err
	}

	return newRunView(run), nil
}
