package filter

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

// The UI's duration tests read the same file, so the two parsers agree.
func TestParseDuration(t *testing.T) {
	raw, err := os.ReadFile("testdata/durations.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Input   string `json:"input"`
		Seconds int64  `json:"seconds"`
		Error   bool   `json:"error"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		got, ok := ParseDuration(c.Input)
		if ok == c.Error {
			t.Errorf("ParseDuration(%q) ok = %v, want %v", c.Input, ok, !c.Error)
			continue
		}
		if want := time.Duration(c.Seconds) * time.Second; got != want {
			t.Errorf("ParseDuration(%q) = %v, want %v", c.Input, got, want)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	for d, want := range map[time.Duration]string{
		30 * time.Second: "30s", 90 * time.Minute: "90m", 48 * time.Hour: "2d", 14 * 24 * time.Hour: "2w",
		1500 * time.Millisecond: "2s", 25 * time.Hour: "25h",
	} {
		if got := FormatDuration(d); got != want {
			t.Errorf("FormatDuration(%v) = %q, want %q", d, got, want)
		}
		if back, ok := ParseDuration(FormatDuration(d)); !ok || back < d {
			t.Errorf("FormatDuration(%v) doesn't read back", d)
		}
	}
}
