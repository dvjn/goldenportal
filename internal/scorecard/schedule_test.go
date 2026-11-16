package scorecard

import (
	"testing"

	"github.com/dvjn/goldenportal/internal/httpx/httpxtest"
)

// TestRegisterSchedulesEvaluation checks every scorecard is evaluated on
// the schedule the server is given.
func TestRegisterSchedulesEvaluation(t *testing.T) {
	r := httpxtest.New()
	Register(r, "30 2 * * *", nil)
	if got := r.Schedules["evaluate-scorecards"]; got != "30 2 * * *" {
		t.Errorf("scorecards evaluated on %q, want the schedule given", got)
	}
}
