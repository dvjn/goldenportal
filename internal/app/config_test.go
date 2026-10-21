package app

import "testing"

func TestConfigHandlerReturnsThePortalConfig(t *testing.T) {
	want := Config{Name: "Acme"}

	got, err := configHandler(want)(nil)
	if err != nil {
		t.Fatalf("configHandler() error = %v", err)
	}
	if got != want {
		t.Fatalf("configHandler() = %v, want %v", got, want)
	}
}
