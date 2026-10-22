// Package connector holds what the pull connectors share: how a sync asks
// one for records, and how they call a source's JSON API.
package connector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"time"
)

// Records are the resources a connector read, by kind. Each record is a
// JSON value as encoding/json decodes it.
type Records map[string][]any

// Request is what a sync asks a connector for: the integration's config and
// secrets, and the kinds its mappings read.
type Request struct {
	Config  json.RawMessage
	Secrets map[string]string
	Kinds   []string
}

func (r Request) Wants(kind string) bool {
	return slices.Contains(r.Kinds, kind)
}

// Secret returns the named secret, or an error that says the integration
// does not give it.
func (r Request) Secret(name string) (string, error) {
	secret, ok := r.Secrets[name]
	if !ok || secret == "" {
		return "", fmt.Errorf("give the integration a %s secret under secrets", name)
	}

	return secret, nil
}

type Connector interface {
	// Kinds are the kinds of resource the connector reads.
	Kinds() []string
	Fetch(ctx context.Context, request Request) (Records, error)
}

// DecodeConfig reads the config into target, refusing unknown keys.
func DecodeConfig(config json.RawMessage, target any) error {
	if len(config) == 0 {
		config = json.RawMessage("{}")
	}
	decoder := json.NewDecoder(bytes.NewReader(config))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("the config is not valid: %w", err)
	}

	return nil
}

// Client calls one source's JSON API.
type Client struct {
	Source string
	HTTP   *http.Client
	Header http.Header
}

// DefaultHTTPClient gives up on a source that takes longer than a minute
// to answer.
func DefaultHTTPClient() *http.Client {
	return &http.Client{Timeout: time.Minute}
}

// Get decodes the JSON the address answers with into target, and returns the
// response headers.
func (c Client) Get(ctx context.Context, address string, target any) (http.Header, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	for name, values := range c.Header {
		request.Header[name] = values
	}
	request.Header.Set("Accept", "application/json")
	response, err := c.HTTP.Do(request)
	var failed *url.Error
	if errors.As(err, &failed) {
		return nil, fmt.Errorf("could not reach %s at %s: %w", c.Source, origin(address), failed.Err)
	}
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, response.Body)
		return nil, fmt.Errorf("%s answered %s for %s", c.Source, response.Status, pathOf(address))
	}
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		return nil, fmt.Errorf("%s answered %s with JSON that could not be read: %w", c.Source, pathOf(address), err)
	}

	return response.Header, nil
}

func origin(address string) string {
	parsed, err := url.Parse(address)
	if err != nil {
		return address
	}

	return parsed.Scheme + "://" + parsed.Host
}

func pathOf(address string) string {
	parsed, err := url.Parse(address)
	if err != nil {
		return address
	}

	return parsed.EscapedPath()
}
