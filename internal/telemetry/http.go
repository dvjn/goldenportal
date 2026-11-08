package telemetry

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strconv"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.opentelemetry.io/otel/trace"
)

// Transport makes a client span of every request base sends, and passes
// the trace on to the system it calls, so connectors' and actions' calls
// show under the work that made them. A nil base is
// http.DefaultTransport.
func Transport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}

	return transport{base}
}

type transport struct{ base http.RoundTripper }

type attemptKey struct{}

// Attempt marks the requests made under ctx as the attempt'th try of the
// same request, counting from 0, for the span to say so.
func Attempt(ctx context.Context, attempt int) context.Context {
	return context.WithValue(ctx, attemptKey{}, attempt)
}

func (t transport) RoundTrip(req *http.Request) (*http.Response, error) {
	attrs := []attribute.KeyValue{
		semconv.HTTPRequestMethodKey.String(req.Method),
		semconv.URLFull(RedactURL(req.URL)),
		semconv.ServerAddress(req.URL.Hostname()),
	}
	if port, err := strconv.Atoi(req.URL.Port()); err == nil {
		attrs = append(attrs, semconv.ServerPort(port))
	}
	if attempt, _ := req.Context().Value(attemptKey{}).(int); attempt > 0 {
		attrs = append(attrs, semconv.HTTPRequestResendCount(attempt))
	}
	ctx, span := tracer().Start(req.Context(), req.Method, trace.WithSpanKind(trace.SpanKindClient), trace.WithAttributes(attrs...))
	defer span.End()

	// The request is the caller's, so the trace headers go on a copy.
	req = req.Clone(ctx)
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		Fail(span, err)
		return nil, err
	}
	span.SetAttributes(semconv.HTTPResponseStatusCode(resp.StatusCode))
	if resp.StatusCode >= 400 {
		span.SetStatus(codes.Error, "")
		span.SetAttributes(semconv.ErrorTypeKey.String(strconv.Itoa(resp.StatusCode)))
	}

	return resp, nil
}

var urlPattern = regexp.MustCompile(`https?://[^\s"'<>]+`)

// Scrub redacts the URLs in text, such as an error that names the address
// it failed at, as RedactURL does.
func Scrub(text string) string {
	return urlPattern.ReplaceAllStringFunc(text, func(raw string) string {
		u, err := url.Parse(raw)
		if err != nil {
			return "REDACTED"
		}
		return RedactURL(u)
	})
}

// RedactURL is u as spans and logs may show it: without credentials, and
// with each query value replaced, since systems take tokens there too.
// Query names stay, so it's still clear what was asked.
func RedactURL(u *url.URL) string {
	redacted := *u
	if redacted.User != nil {
		redacted.User = url.User("REDACTED")
	}
	redacted.Fragment = ""
	if redacted.RawQuery != "" {
		q := redacted.Query()
		for name, values := range q {
			for i := range values {
				values[i] = "REDACTED"
			}
			q[name] = values
		}
		redacted.RawQuery = q.Encode()
	}

	return redacted.String()
}
