package connection

import (
	"errors"
	"strings"
	"testing"
)

// TestLastingRefusals checks a scope refused on each of the last syncs
// stops holding back the rows no one scope owns, and becomes a warning,
// while one that failed otherwise, or only lately, still holds them back.
func TestLastingRefusals(t *testing.T) {
	refused := func(scope string) failedScope {
		return failedScope{Resource: "deployments", Scope: scope, Refused: true}
	}
	report := Report{ScopeFailures: []ScopeFailure{
		{Resource: "deployments", Scope: "7", Refused: true, Err: errors.New("deployments of acme/sec: 403")},
		{Resource: "deployments", Scope: "8", Refused: true, Err: errors.New("deployments of acme/new: 403")},
		{Resource: "tags", Scope: "7", Err: errors.New("tags of acme/sec: 500")},
	}}
	markLasting(&report, [][]failedScope{{refused("7"), refused("8")}, {refused("7")}})
	if !report.ScopeFailures[0].lasting || report.ScopeFailures[1].lasting || report.ScopeFailures[2].lasting {
		t.Fatalf("lasting = %v", report.ScopeFailures)
	}
	failed, warning := failureText(report)
	if failed != "deployments of acme/new: 403; tags of acme/sec: 500" || !strings.HasPrefix(warning, "deployments of acme/sec: 403. Refused on each of the last 3 syncs") {
		t.Fatalf("failed %q, warning %q", failed, warning)
	}

	fresh := Report{ScopeFailures: []ScopeFailure{{Resource: "deployments", Scope: "7", Refused: true}}}
	markLasting(&fresh, [][]failedScope{{refused("7")}})
	if fresh.ScopeFailures[0].lasting {
		t.Error("want a refusal lasting only once there are enough syncs before")
	}
}
