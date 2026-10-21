// Package httpx holds transport-level primitives shared across feature packages.
package httpx

import (
	"encoding/json"
	"net/http"
)

// Error is the JSON error envelope returned by handlers. Its StatusCode method
// is recognized by the gofr framework to set the HTTP response status.
type Error struct {
	Code    int    `json:"-"`
	Message string `json:"error"`
}

func (e Error) Error() string   { return e.Message }
func (e Error) StatusCode() int { return e.Code }

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
