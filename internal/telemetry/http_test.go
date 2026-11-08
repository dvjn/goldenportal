package telemetry

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.opentelemetry.io/otel/trace"
)

// TestTransport checks an outbound request is a client span under its
// caller, passes the trace on, and shows its URL without credentials or
// query values.
func TestTransport(t *testing.T) {
	exporter := recordSpans(t)
	var traceparent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		traceparent = r.Header.Get("traceparent")
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	ctx, parent := Start(t.Context(), "pull project")
	address := strings.Replace(server.URL, "http://", "http://user:hunter2@", 1) + "/api/v4/projects?private_token=glpat-secret&per_page=100"
	req, _ := http.NewRequestWithContext(Attempt(ctx, 2), http.MethodGet, address, nil)
	req.Header.Set("PRIVATE-TOKEN", "glpat-secret")
	resp, err := (&http.Client{Transport: Transport(nil)}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	parent.End()

	spans := exporter.GetSpans()
	if len(spans) != 2 || spans[0].Name != "GET" || spans[0].SpanKind != trace.SpanKindClient {
		t.Fatalf("spans = %v", spans)
	}
	client := spans[0]
	if client.Parent.SpanID() != parent.SpanContext().SpanID() {
		t.Error("the request isn't under its caller")
	}
	if want := "00-" + client.SpanContext.TraceID().String() + "-" + client.SpanContext.SpanID().String() + "-01"; traceparent != want {
		t.Errorf("traceparent = %q, want %q", traceparent, want)
	}
	u, _ := url.Parse(server.URL)
	attrs := attributesOf(client)
	if got := attrs[semconv.URLFullKey].AsString(); got != "http://REDACTED@"+u.Host+"/api/v4/projects?per_page=REDACTED&private_token=REDACTED" {
		t.Errorf("url.full = %s", got)
	}
	if attrs[semconv.HTTPResponseStatusCodeKey].AsInt64() != http.StatusForbidden || attrs[semconv.HTTPRequestResendCountKey].AsInt64() != 2 ||
		attrs[semconv.ErrorTypeKey].AsString() != "403" || attrs[semconv.ServerAddressKey].AsString() != u.Hostname() {
		t.Errorf("attributes = %v", attrs)
	}
	for _, a := range client.Attributes {
		if strings.Contains(a.Value.Emit(), "glpat-secret") || strings.Contains(a.Value.Emit(), "hunter2") {
			t.Errorf("%s holds a secret: %s", a.Key, a.Value.Emit())
		}
	}
}
