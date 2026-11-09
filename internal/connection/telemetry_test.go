package connection

import (
	"slices"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/dvjn/goldenportal/internal/telemetry"
)

func recordSpans(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	provider, propagator := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter)))
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		otel.SetTracerProvider(provider)
		otel.SetTextMapPropagator(propagator)
	})

	return exporter
}

// TestGitLabPullSpans checks a pull reads as what it did: a span for each
// project, with a span for each resource read of it, holding a span for
// each request, and the resource GitLab refused failed.
func TestGitLabPullSpans(t *testing.T) {
	exporter := recordSpans(t)
	f := newGitLabFixture(t)
	_, config := gitlabSpec(t, gitlabYAML, f.url())
	f.deny("/deployments")
	request := PullRequest{Resources: []string{gitlabDeployments, gitlabReleases}, Warn: func(string) {}}
	if _, err := config.Pull(t.Context(), nil, request, func(string, []Item) error { return nil }); err != nil {
		t.Fatal(err)
	}

	spans := exporter.GetSpans()
	byID := map[string]tracetest.SpanStub{}
	for _, s := range spans {
		byID[s.SpanContext.SpanID().String()] = s
	}
	var projects []string
	requests := 0
	for _, s := range spans {
		parent := byID[s.Parent.SpanID().String()]
		switch s.Name {
		case "pull project":
			for _, a := range s.Attributes {
				if a.Key == telemetry.Scope {
					projects = append(projects, a.Value.AsString())
				}
			}
		case "read " + gitlabDeployments:
			if parent.Name != "pull project" || s.Status.Code != codes.Error {
				t.Errorf("deployments read under %q, status %v", parent.Name, s.Status)
			}
		case "read " + gitlabReleases:
			if parent.Name != "pull project" || s.Status.Code == codes.Error {
				t.Errorf("releases read under %q, status %v", parent.Name, s.Status)
			}
		case "GET":
			requests++
		}
	}
	if len(projects) != 3 || slices.Contains(projects, "") {
		t.Errorf("projects = %v, want the three", projects)
	}
	if requests != len(f.asked()) {
		t.Errorf("request spans = %d, want one for each of the %d requests", requests, len(f.asked()))
	}
}
