package jq

import (
	"context"
	"strings"
	"testing"
	"time"
)

func mustCompile(t *testing.T, expression string, variables ...string) *Code {
	t.Helper()
	code, err := Compile(expression, variables...)
	if err != nil {
		t.Fatal(err)
	}

	return code
}

func TestFirst(t *testing.T) {
	ctx := context.Background()

	value, ok, err := mustCompile(t, ".a, .b").First(ctx, DefaultTimeout, map[string]any{"a": 1, "b": 2})
	if err != nil || !ok || value != float64(1) {
		t.Fatalf("got %v %v %v, want the first value as a float", value, ok, err)
	}

	if _, ok, err = mustCompile(t, "empty").First(ctx, DefaultTimeout, nil); ok || err != nil {
		t.Fatalf("empty gave %v %v, want nothing and no error", ok, err)
	}

	if _, _, err = mustCompile(t, "error(\"no\")").First(ctx, DefaultTimeout, nil); err == nil || !strings.Contains(err.Error(), "no") {
		t.Fatalf("got %v, want the expression's own error", err)
	}
}

func TestVariables(t *testing.T) {
	code := mustCompile(t, "$x + $y", "$x", "$y")
	value, _, err := code.First(context.Background(), DefaultTimeout, nil, 1, 2)
	if err != nil || value != float64(3) {
		t.Fatalf("got %v %v, want 3", value, err)
	}

	if _, err := Compile("$z"); err == nil {
		t.Fatal("an undeclared variable compiled")
	}
	if _, err := Compile(".["); err == nil {
		t.Fatal("bad jq compiled")
	}
}

func TestTimeout(t *testing.T) {
	code := mustCompile(t, "[range(1e9)] | length")
	_, _, err := code.First(context.Background(), 20*time.Millisecond, nil)
	if err == nil || !strings.Contains(err.Error(), "took longer than 20ms") {
		t.Fatalf("got %v, want a timeout error", err)
	}

	_, err = mustCompile(t, "range(1e9)").All(context.Background(), 20*time.Millisecond, nil)
	if err == nil || !strings.Contains(err.Error(), "took longer than 20ms") {
		t.Fatalf("All got %v, want a timeout error", err)
	}
}

func TestAll(t *testing.T) {
	out, err := mustCompile(t, ".[]").All(context.Background(), DefaultTimeout, []any{1, "a"})
	if err != nil || len(out) != 2 || out[0] != float64(1) || out[1] != "a" {
		t.Fatalf("got %v %v", out, err)
	}
}
