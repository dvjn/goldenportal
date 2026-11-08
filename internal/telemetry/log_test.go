package telemetry

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"gofr.dev/pkg/gofr/logging"
	"gofr.dev/pkg/gofr/testutil"
)

// TestLogLine checks a line logged within a span is gofr's JSON with the
// trace ID beside the message, and the message the fields: the text, the
// span's fields then the line's, the error with URLs redacted, and the
// span ID.
func TestLogLine(t *testing.T) {
	recordSpans(t)
	ctx, span := Start(t.Context(), "sync", Integration.String("gitlab"))
	defer span.End()

	out := testutil.StdoutOutputForFunc(func() {
		SetLogger(logging.NewLogger(logging.INFO))
		Warn(ctx, "scope not read", errors.New(`GET https://gitlab.example.com/api/v4/projects?private_token=glpat-secret answered 403`),
			Scope.String("platform/payments"))
		Debug(ctx, "batch written")
	})
	t.Cleanup(func() { logger.Store(nil) })

	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 1 {
		t.Fatalf("logged %q, want the warning alone, at info", out)
	}
	if strings.Contains(out, "glpat-secret") {
		t.Fatalf("logged %s, with the token", out)
	}
	var line struct {
		Level   string         `json:"level"`
		TraceID string         `json:"trace_id"`
		Message map[string]any `json:"message"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &line); err != nil {
		t.Fatal(err)
	}
	sc := span.SpanContext()
	want := map[string]any{
		"msg": "scope not read", "gp.integration": "gitlab", "gp.scope": "platform/payments",
		"error":   "GET https://gitlab.example.com/api/v4/projects?private_token=REDACTED answered 403",
		"span_id": sc.SpanID().String(),
	}
	if line.Level != "WARN" || line.TraceID != sc.TraceID().String() || len(line.Message) != len(want) {
		t.Fatalf("line = %s", lines[0])
	}
	for key, value := range want {
		if line.Message[key] != value {
			t.Errorf("%s = %v, want %v", key, line.Message[key], value)
		}
	}
	if !strings.HasPrefix(lines[0][strings.Index(lines[0], `"message":`):], `"message":{"msg":"scope not read","gp.integration"`) {
		t.Errorf("line = %s, want msg first, then the span's fields", lines[0])
	}
}
