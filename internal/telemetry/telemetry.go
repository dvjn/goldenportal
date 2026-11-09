// Package telemetry shows what the portal does as traces and logs that
// agree: each unit of work is a span, and what's logged within it carries
// the span's fields and IDs, so a log line leads to its trace and back.
//
// Spans go wherever gofr's tracing settings (TRACE_EXPORTER, TRACER_URL,
// TRACER_RATIO) send them, and cost next to nothing when they're not set.
package telemetry

import (
	"context"
	"slices"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.opentelemetry.io/otel/trace"
	"gofr.dev/pkg/gofr"
)

const scope = "github.com/dvjn/goldenportal"

// What spans and log lines say about the portal's own objects, under one
// name each so a field found in a log is the attribute to search traces by.
const (
	Connection = attribute.Key("gp.connection")
	Resource   = attribute.Key("gp.resource")
	// Scope is the part of a system a connector reads at once, such as a
	// GitLab project.
	Scope      = attribute.Key("gp.scope")
	Blueprint  = attribute.Key("gp.blueprint")
	Scorecard  = attribute.Key("gp.scorecard")
	Dashboard  = attribute.Key("gp.dashboard")
	Widget     = attribute.Key("gp.widget")
	WidgetType = attribute.Key("gp.widget.type")
	Action     = attribute.Key("gp.action")
	RunID      = attribute.Key("gp.run.id")
	Trigger    = attribute.Key("gp.run.trigger")
	Mode       = attribute.Key("gp.run.mode")
	// Items is how many items a step read or wrote; the counts below split
	// a run's by what became of them.
	Items     = attribute.Key("gp.items")
	Received  = attribute.Key("gp.items.received")
	Created   = attribute.Key("gp.items.created")
	Updated   = attribute.Key("gp.items.updated")
	Unchanged = attribute.Key("gp.items.unchanged")
	Deleted   = attribute.Key("gp.items.deleted")
	Failed    = attribute.Key("gp.items.failed")
	// Written is how many were created or updated; Older, how many of the
	// unchanged the store already held from a newer read; Expired, how many
	// weren't written for being older than their blueprint keeps rows;
	// Retried, how many were written again after a deadlock.
	Written = attribute.Key("gp.items.written")
	Older   = attribute.Key("gp.items.older")
	Expired = attribute.Key("gp.items.expired")
	Retried = attribute.Key("gp.items.retried")
	// Wait is how long a step held back, as for a rate limit, in
	// milliseconds.
	Wait = attribute.Key("gp.wait_ms")
	// Duration is how long a step took, in milliseconds, for logs; spans
	// carry their own.
	Duration = attribute.Key("gp.duration_ms")
)

func tracer() trace.Tracer { return otel.Tracer(scope) }

type fieldsKey struct{}

// Start starts a span for a unit of work, named for what it does, such as
// "sync" or "load widget". attrs go on the span and on every line logged
// under the context it returns.
func Start(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	ctx, span := tracer().Start(ctx, name, trace.WithAttributes(attrs...))

	return withFields(ctx, attrs), span
}

// withFields adds attrs to what's logged under ctx, a field set again
// taking the newer value.
func withFields(ctx context.Context, attrs []attribute.KeyValue) context.Context {
	if len(attrs) == 0 {
		return ctx
	}
	fields := slices.DeleteFunc(slices.Clone(fieldsOf(ctx)), func(f attribute.KeyValue) bool {
		return slices.ContainsFunc(attrs, func(a attribute.KeyValue) bool { return a.Key == f.Key })
	})

	return context.WithValue(ctx, fieldsKey{}, append(fields, attrs...))
}

func fieldsOf(ctx context.Context) []attribute.KeyValue {
	fields, _ := ctx.Value(fieldsKey{}).([]attribute.KeyValue)
	return fields
}

// Within starts a span for the rest of a handler's work, under which ctx
// runs, as gofr's own ctx.Trace does. The function it returns ends the
// span, failed with err unless err is nil, and puts ctx back.
func Within(ctx *gofr.Context, name string, attrs ...attribute.KeyValue) func(err error) {
	outer := ctx.Context
	var span trace.Span
	ctx.Context, span = Start(outer, name, attrs...)

	return func(err error) {
		End(span, err)
		ctx.Context = outer
	}
}

// End ends span, marking it failed with err unless err is nil.
func End(span trace.Span, err error) {
	if err != nil {
		Fail(span, err)
	}
	span.End()
}

// Fail marks span failed with err, for a span that goes on.
func Fail(span trace.Span, err error) {
	message := Scrub(err.Error())
	span.AddEvent(semconv.ExceptionEventName, trace.WithAttributes(semconv.ExceptionMessage(message)))
	span.SetStatus(codes.Error, message)
	span.SetAttributes(semconv.ErrorTypeOther)
}

// Resume is ctx under from's span, logging from's fields, for work that
// carries on a span started under another context, such as a request's
// that has ended.
func Resume(ctx, from context.Context) context.Context {
	return context.WithValue(trace.ContextWithSpan(ctx, trace.SpanFromContext(from)), fieldsKey{}, fieldsOf(from))
}
