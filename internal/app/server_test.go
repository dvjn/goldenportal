package app

import (
	"errors"
	"os"
	"os/exec"
	"testing"

	"gofr.dev/pkg/gofr"
)

const failingStartEnv = "GOLDPORT_TEST_FAILING_START"

func TestRunExitsWithFailureWhenAStartHookFails(t *testing.T) {
	if os.Getenv(failingStartEnv) == "1" {
		server := &Server{App: gofr.New()}
		server.OnStart(func(*gofr.Context) error { return errors.New("start: boom") })
		_ = server.Run()
		os.Exit(0)
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestRunExitsWithFailureWhenAStartHookFails$")
	cmd.Env = append(os.Environ(), failingStartEnv+"=1")

	var exitErr *exec.ExitError
	if err := cmd.Run(); !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
		t.Fatalf("Run() with a failing start hook: %v, want exit status 1", err)
	}
}
