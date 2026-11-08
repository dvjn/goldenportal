package telemetry

import (
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// recordSpans sends the spans a test makes to the exporter it returns, and
// passes trace context on as the server does.
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

func attributesOf(span tracetest.SpanStub) map[attribute.Key]attribute.Value {
	attrs := map[attribute.Key]attribute.Value{}
	for _, a := range span.Attributes {
		attrs[a.Key] = a.Value
	}

	return attrs
}

func TestStartLogsItsFieldsUnder(t *testing.T) {
	recordSpans(t)
	ctx, outer := Start(t.Context(), "sync", Integration.String("gitlab"), Resource.String("projects"))
	ctx, inner := Start(ctx, "read pipelines", Resource.String("pipelines"))
	inner.End()
	outer.End()

	got := map[attribute.Key]string{}
	for _, f := range fieldsOf(ctx) {
		got[f.Key] = f.Value.AsString()
	}
	if len(got) != 2 || got[Integration] != "gitlab" || got[Resource] != "pipelines" {
		t.Errorf("fields = %v, want the integration and the inner resource", got)
	}
}
