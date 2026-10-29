// Package connector holds what the pull connectors share: how a sync asks
// one for records, and how they call a source's JSON API.
package connector

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"sync"
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

// EachConcurrently calls read for every item with at most workers calls at a
// time, and returns what they read in the order of the items. It stops at
// the first error.
func EachConcurrently[T any](ctx context.Context, items []T, workers int, read func(ctx context.Context, item T) ([]any, error)) ([]any, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make([][]any, len(items))
	failures := make([]error, len(items))
	slots := make(chan struct{}, workers)
	var group sync.WaitGroup
	for index, item := range items {
		group.Go(func() {
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				failures[index] = ctx.Err()
				return
			}
			defer func() { <-slots }()
			results[index], failures[index] = read(ctx, item)
			if failures[index] != nil {
				cancel()
			}
		})
	}
	group.Wait()
	if err := firstError(failures); err != nil {
		return nil, err
	}

	return slices.Concat(results...), nil
}

// firstError is the first failure that is not a cancellation another
// failure caused.
func firstError(failures []error) error {
	var cancelled error
	for _, err := range failures {
		switch {
		case err == nil:
		case errors.Is(err, context.Canceled):
			cancelled = cmp.Or(cancelled, err)
		default:
			return err
		}
	}

	return cancelled
}

// StatusError is a source's answer that is not a success.
type StatusError struct {
	Source string
	Status string
	Code   int
	Path   string
}

func (e StatusError) Error() string {
	return fmt.Sprintf("%s answered %s for %s", e.Source, e.Status, e.Path)
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
		return nil, StatusError{Source: c.Source, Status: response.Status, Code: response.StatusCode, Path: pathOf(address)}
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
