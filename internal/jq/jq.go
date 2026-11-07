// Package jq is the one place goldenportal compiles and runs jq. Every
// expression an author writes, in a widget, a scorecard check, a computed
// property, a mapping or an action step, goes through it, so each is bounded
// the same way.
package jq

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/itchyny/gojq"
)

// DefaultTimeout bounds one run, so a slow expression can't hold up a page,
// the rest of a scorecard or a sync. It is for a run on one value; a caller
// whose input is larger passes its own.
const DefaultTimeout = 250 * time.Millisecond

// Code is a compiled expression.
type Code struct {
	code *gojq.Code
}

// Compile compiles an expression that may read the named variables, each
// written with its $, such as "$me". Runs give their values in the same
// order.
func Compile(expression string, variables ...string) (*Code, error) {
	query, err := gojq.Parse(expression)
	if err != nil {
		return nil, err
	}
	code, err := gojq.Compile(query, gojq.WithVariables(variables))
	if err != nil {
		return nil, err
	}

	return &Code{code}, nil
}

// First runs the expression on input and returns its first result, with
// numbers as float64, as JSON decoding gives. ok is false when it gave
// nothing. The run is cut short after timeout, or when ctx ends.
func (c *Code) First(ctx context.Context, timeout time.Duration, input any, values ...any) (value any, ok bool, err error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	value, ok = c.code.RunWithContext(ctx, input, values...).Next()
	if !ok {
		return nil, false, nil
	}
	if err, isErr := value.(error); isErr {
		return nil, false, runError(err, timeout)
	}

	return normalise(value), true, nil
}

// All is First for every result. The timeout bounds the whole run.
func (c *Code) All(ctx context.Context, timeout time.Duration, input any, values ...any) ([]any, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var out []any
	iter := c.code.RunWithContext(ctx, input, values...)
	for {
		value, ok := iter.Next()
		if !ok {
			return out, nil
		}
		if err, isErr := value.(error); isErr {
			return nil, runError(err, timeout)
		}
		out = append(out, normalise(value))
	}
}

func runError(err error, timeout time.Duration) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("took longer than %s", timeout)
	}

	return err
}

// normalise turns gojq's ints into float64, as JSON decoding gives.
func normalise(value any) any {
	switch v := value.(type) {
	case int:
		return float64(v)
	case []any:
		for i := range v {
			v[i] = normalise(v[i])
		}
	case map[string]any:
		for key := range v {
			v[key] = normalise(v[key])
		}
	}

	return value
}
