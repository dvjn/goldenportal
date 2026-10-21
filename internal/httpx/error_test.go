package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestErrorCarriesTheStatusAndMessage(t *testing.T) {
	err := Error{Code: http.StatusNotFound, Message: "entity not found"}

	if got := err.StatusCode(); got != http.StatusNotFound {
		t.Fatalf("StatusCode() = %d, want %d", got, http.StatusNotFound)
	}
	if got := err.Error(); got != "entity not found" {
		t.Fatalf("Error() = %q, want %q", got, "entity not found")
	}
}

func TestWriteErrorWritesTheEnvelope(t *testing.T) {
	recorder := httptest.NewRecorder()

	WriteError(recorder, http.StatusUnauthorized, "token is invalid")

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want %q", got, "application/json")
	}
	want := `{"error":{"message":"token is invalid"}}` + "\n"
	if got := recorder.Body.String(); got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}
