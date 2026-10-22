// Package httpx holds transport-level primitives shared across feature packages.
package httpx

import (
	"encoding/json"
	"net/http"

	"gofr.dev/pkg/gofr"
)

// Error is the JSON error envelope returned by handlers. Its StatusCode method
// is recognized by the gofr framework to set the HTTP response status.
type Error struct {
	Code    int    `json:"-"`
	Message string `json:"error"`
}

func (e Error) Error() string   { return e.Message }
func (e Error) StatusCode() int { return e.Code }

var errUnexpected = Error{Code: http.StatusInternalServerError, Message: "Something went wrong. Try again."}

type statusCoder interface {
	StatusCode() int
}

// hideUnexpected logs an error that carries no client error status and
// answers it with a generic message, so no database or internal detail
// reaches the caller. It reads the status the way gofr does, without
// unwrapping.
func hideUnexpected(handler gofr.Handler) gofr.Handler {
	return func(ctx *gofr.Context) (any, error) {
		result, err := handler(ctx)
		if err == nil {
			return result, nil
		}
		if expected, ok := err.(statusCoder); ok && expected.StatusCode() < http.StatusInternalServerError {
			return result, err
		}
		ctx.Errorf("unexpected error: %v", err)

		return nil, errUnexpected
	}
}

type errorBody struct {
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

// WriteError writes the error envelope gofr uses for handler errors, for
// middleware that answers before a handler runs.
func WriteError(w http.ResponseWriter, status int, message string) {
	var body errorBody
	body.Error.Message = message
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
