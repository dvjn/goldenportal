package mcp

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/dvjn/goldenportal/internal/action"
)

// The status argument's description is a struct tag, so it can't be built
// from the constants; this keeps it listing exactly the statuses a run has.
func TestRunsStatusDescriptionListsEveryStatus(t *testing.T) {
	field, _ := reflect.TypeFor[runsArgs]().FieldByName("Status")
	desc := strings.TrimPrefix(field.Tag.Get("jsonschema"), "Comma-separated: ")
	desc = strings.Replace(desc, " or ", ", ", 1)
	got := strings.Split(desc, ", ")

	want := []string{
		action.RunQueued, action.RunRunning, action.RunWaiting, action.RunSucceeded,
		action.RunFailed, action.RunRejected, action.RunCancelled, action.RunInterrupted,
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("status description lists %v, runs have %v", got, want)
	}
}
