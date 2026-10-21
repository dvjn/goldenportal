package app

import (
	"errors"
	"testing"

	"gofr.dev/pkg/gofr"
)

func TestRunReturnsTheFailureOfAStartHook(t *testing.T) {
	failure := errors.New("start: boom")
	server := &Server{App: gofr.New()}
	server.OnStart(func(*gofr.Context) error { return failure })

	if err := server.Run(); !errors.Is(err, failure) {
		t.Fatalf("Run() = %v, want %v", err, failure)
	}
}
