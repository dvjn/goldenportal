package telemetry

import (
	"context"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"golang.org/x/time/rate"
)

type uploads struct{ batches [][]*tracepb.ResourceSpans }

func (*uploads) Start(context.Context) error { return nil }
func (*uploads) Stop(context.Context) error  { return nil }

func (u *uploads) UploadTraces(_ context.Context, spans []*tracepb.ResourceSpans) error {
	u.batches = append(u.batches, spans)
	return nil
}

const browserBatch = `{"resourceSpans":[{
	"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"someone-else"}}]},
	"scopeSpans":[{"scope":{"name":"goldenportal-ui"},"spans":[{
		"traceId":"5b8efff798038103d269b633813fc60c","spanId":"eee19b7ec3c1b174","name":"GET","kind":3,
		"startTimeUnixNano":"1","endTimeUnixNano":"2",
		"attributes":[
			{"key":"http.request.method","value":{"stringValue":"GET"}},
			{"key":"url.full","value":{"stringValue":"https://portal.example.com/api/web/catalog?q=payments&token=abc"}},
			{"key":"http.request.header.authorization","value":{"stringValue":"Bearer abc"}},
			{"key":"session.id","value":{"stringValue":"abc"}}
		]}]}]}]}`

// TestRelayCleansBrowserSpans checks the UI's spans go on as the UI's,
// whatever they claimed, without credentials or query values.
func TestRelayCleansBrowserSpans(t *testing.T) {
	sent := &uploads{}
	relay := &Relay{client: sent, service: "goldenportal-ui", limits: newSenderLimits(rate.Inf, 1)}
	req := httptest.NewRequest(http.MethodPost, "/api/web/traces", strings.NewReader(browserBatch))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Firefox")
	w := httptest.NewRecorder()
	relay.Serve(w, req, "u1")

	if w.Code != http.StatusOK || w.Body.String() != "{}" || len(sent.batches) != 1 {
		t.Fatalf("answered %d %s, and sent %d batches", w.Code, w.Body, len(sent.batches))
	}
	rs := sent.batches[0][0]
	span := rs.GetScopeSpans()[0].GetSpans()[0]
	if hex.EncodeToString(span.GetTraceId()) != "5b8efff798038103d269b633813fc60c" || hex.EncodeToString(span.GetSpanId()) != "eee19b7ec3c1b174" {
		t.Errorf("IDs = %x, %x, want those sent in hex", span.GetTraceId(), span.GetSpanId())
	}
	resource := map[string]string{}
	for _, kv := range rs.GetResource().GetAttributes() {
		resource[kv.GetKey()] = kv.GetValue().GetStringValue()
	}
	if len(resource) != 2 || resource["service.name"] != "goldenportal-ui" || resource["user_agent.original"] != "Firefox" {
		t.Errorf("resource = %v", resource)
	}
	attrs := map[string]string{}
	for _, kv := range rs.GetScopeSpans()[0].GetSpans()[0].GetAttributes() {
		attrs[kv.GetKey()] = kv.GetValue().GetStringValue()
	}
	want := map[string]string{
		"http.request.method": "GET",
		"url.full":            "https://portal.example.com/api/web/catalog?q=REDACTED&token=REDACTED",
	}
	if len(attrs) != len(want) {
		t.Errorf("attributes = %v, want %v", attrs, want)
	}
	for key, value := range want {
		if attrs[key] != value {
			t.Errorf("%s = %q, want %q", key, attrs[key], value)
		}
	}
}

func TestRelayRefuses(t *testing.T) {
	relay := &Relay{client: &uploads{}, service: "goldenportal-ui", limits: newSenderLimits(rate.Inf, 1)}
	for name, tt := range map[string]struct {
		body   string
		status int
	}{
		"not OTLP":  {body: `{"resourceSpans":"no"}`, status: http.StatusBadRequest},
		"too large": {body: `{"resourceSpans":[` + strings.Repeat(" ", maxBrowserBytes) + `]}`, status: http.StatusRequestEntityTooLarge},
	} {
		w := httptest.NewRecorder()
		relay.Serve(w, httptest.NewRequest(http.MethodPost, "/api/web/traces", strings.NewReader(tt.body)), "u1")
		if w.Code != tt.status {
			t.Errorf("%s: answered %d, want %d", name, w.Code, tt.status)
		}
	}

	limited := &Relay{client: &uploads{}, service: "goldenportal-ui", limits: newSenderLimits(0, 1)}
	for i, tt := range []struct {
		sender string
		status int
	}{
		{"u1", http.StatusOK},
		{"u1", http.StatusTooManyRequests},
		{"u2", http.StatusOK},
	} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/web/traces", strings.NewReader(browserBatch))
		req.Header.Set("Content-Type", "application/json")
		limited.Serve(w, req, tt.sender)
		if w.Code != tt.status {
			t.Errorf("batch %d from %s: answered %d, want %d", i+1, tt.sender, w.Code, tt.status)
		}
	}
}

// TestNewRelayOnlyForOTLP checks the UI traces only when the server's
// spans go to an OTLP collector, the one the relay can pass them on to.
func TestNewRelayOnlyForOTLP(t *testing.T) {
	for _, tt := range []struct {
		settings mapSettings
		relays   bool
	}{
		{mapSettings{}, false},
		{mapSettings{"TRACE_EXPORTER": "zipkin", "TRACER_URL": "http://localhost:9411/api/v2/spans"}, false},
		{mapSettings{"TRACE_EXPORTER": "otlp"}, false},
		{mapSettings{"TRACE_EXPORTER": "otlp", "TRACER_URL": "localhost:4317", "TRACER_RATIO": "0.25"}, true},
	} {
		relay := NewRelay(tt.settings)
		if (relay != nil) != tt.relays {
			t.Errorf("NewRelay(%v) = %v, want relaying %v", tt.settings, relay, tt.relays)
		}
		if relay != nil && relay.SampleRatio != 0.25 {
			t.Errorf("sample ratio = %v, want TRACER_RATIO", relay.SampleRatio)
		}
	}
}

type mapSettings map[string]string

func (s mapSettings) Get(key string) string { return s[key] }

func (s mapSettings) GetOrDefault(key, fallback string) string {
	if value, ok := s[key]; ok {
		return value
	}
	return fallback
}
