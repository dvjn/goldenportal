package httpx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExposeRawHandsTheWriterAndRequestToHandlers(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)
	request.AddCookie(&http.Cookie{Name: "flavour", Value: "oat"})

	ExposeRaw(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		cookie, err := RawRequest(r.Context()).Cookie("flavour")
		if err != nil || cookie.Value != "oat" {
			t.Fatalf("Cookie() = %v, %v, want oat", cookie, err)
		}
		http.SetCookie(RawWriter(r.Context()), &http.Cookie{Name: "seen", Value: "yes"})
	})).ServeHTTP(recorder, request)

	if got := recorder.Header().Get("Set-Cookie"); got != "seen=yes" {
		t.Fatalf("Set-Cookie = %q, want %q", got, "seen=yes")
	}
}

func TestRawAccessorsAreEmptyOutsideExposeRaw(t *testing.T) {
	if RawRequest(context.Background()) != nil || RawWriter(context.Background()) != nil {
		t.Fatal("accessors returned values without ExposeRaw")
	}
}
