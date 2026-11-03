package action

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/entity"
	"github.com/dvjn/goldenportal/internal/integration"
)

const (
	// maxResponseBytes caps what an HTTP step reads of a response.
	maxResponseBytes = 1 << 20
	// detailBytes is how much of a failed response the run page shows.
	detailBytes = 2 << 10
)

var httpClient = &http.Client{}

// runStep runs one step and gives its output.
func (rn Runner) runStep(ctx context.Context, store Store, run Run, position int, step Step, scope Scope) (any, error) {
	if err := store.startStep(ctx, run.ID, position); err != nil {
		return nil, err
	}
	switch step.Type {
	case StepHTTP:
		return rn.httpStep(ctx, store, run, position, step, scope)
	case StepEntity:
		return rn.entityStep(ctx, step.Entity, scope)
	case StepCheck:
		return rn.checkStep(ctx, step.Check, scope)
	case StepOutput:
		summary := scope.Text(step.Output.Summary)
		links := make([]Link, 0, len(step.Output.Links))
		for _, link := range step.Output.Links {
			links = append(links, Link{Label: link.Label, URL: scope.Text(link.URL)})
		}
		return nil, store.setOutput(ctx, run.ID, summary, links)
	}

	return nil, fmt.Errorf("unknown step type %s", step.Type)
}

// response is an HTTP step's answer as jq reads it.
type response struct {
	status  int
	headers map[string]any
	body    any
}

func (r response) value() map[string]any {
	return map[string]any{"status": float64(r.status), "headers": r.headers, "body": r.body}
}

// httpStep calls the step's URL, trying again after network errors, 5xx
// and 429 answers as many times as its retry allows.
func (rn Runner) httpStep(ctx context.Context, store Store, run Run, position int, step Step, scope Scope) (any, error) {
	h := step.HTTP
	attempts := 1
	if step.Retry != nil {
		attempts += step.Retry.Attempts
	}
	for attempt := 1; ; attempt++ {
		resp, wait, err := rn.call(ctx, run, position, step, scope)
		if err == nil {
			if !succeeded(h.Success, resp.status) {
				err = failed(map[string]any{"status": resp.status, "body": snippet(resp.body)},
					"%s answered %d %s", hostOf(scope.Text(h.URL)), resp.status, http.StatusText(resp.status))
				if resp.status < 500 && resp.status != http.StatusTooManyRequests {
					return nil, err
				}
			} else {
				return httpOutput(ctx, h, resp)
			}
		}
		if attempt >= attempts || ctx.Err() != nil {
			return nil, err
		}
		if wait == 0 {
			wait = time.Duration(step.Retry.Backoff)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(min(wait, maxBackoff)):
		}
		if err := store.startStep(ctx, run.ID, position); err != nil {
			return nil, err
		}
	}
}

// call makes one attempt. wait is how long the other side asked to wait
// before trying again, if it said.
func (rn Runner) call(ctx context.Context, run Run, position int, step Step, scope Scope) (response, time.Duration, error) {
	h := step.HTTP
	address := scope.Text(h.URL)
	if !strings.HasPrefix(address, "http://") && !strings.HasPrefix(address, "https://") {
		return response{}, 0, failed(nil, "the url came out as %q, which isn't an http or https URL", address)
	}
	var body io.Reader
	if h.Body != nil {
		encoded, err := json.Marshal(scope.Resolve(h.Body))
		if err != nil {
			return response{}, 0, failed(nil, "the body can't be written as JSON: %v", err)
		}
		body = bytes.NewReader(encoded)
	}
	attemptCtx, cancel := context.WithTimeout(ctx, time.Duration(step.Timeout))
	defer cancel()
	req, err := http.NewRequestWithContext(attemptCtx, h.Method, address, body)
	if err != nil {
		return response{}, 0, failed(nil, "the request can't be made: %v", err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	// The same key on every attempt lets the other side drop repeats.
	req.Header.Set("Idempotency-Key", fmt.Sprintf("%s-%d", run.ID, position))
	for name, header := range h.Headers {
		if header.Env != "" {
			req.Header.Set(name, header.Resolve())
		} else {
			req.Header.Set(name, scope.Text(header.Value))
		}
	}

	res, err := httpClient.Do(req)
	if err != nil {
		if errors.Is(attemptCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
			return response{}, 0, failed(nil, "%s didn't answer within %s", req.URL.Host, time.Duration(step.Timeout))
		}
		return response{}, 0, failed(nil, "couldn't reach %s: %v", req.URL.Host, unwrapURLError(err))
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxResponseBytes+1))
	if err != nil {
		return response{}, 0, failed(nil, "couldn't read the answer from %s: %v", req.URL.Host, err)
	}
	if len(raw) > maxResponseBytes {
		return response{}, 0, failed(nil, "%s answered with more than %d MB", req.URL.Host, maxResponseBytes>>20)
	}

	resp := response{status: res.StatusCode, headers: map[string]any{}}
	for name := range res.Header {
		resp.headers[strings.ToLower(name)] = res.Header.Get(name)
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &resp.body); err != nil {
			resp.body = string(raw)
		}
	}

	return resp, retryAfter(res.Header.Get("Retry-After")), nil
}

func unwrapURLError(err error) error {
	var urlErr interface{ Unwrap() error }
	if errors.As(err, &urlErr) {
		if inner := urlErr.Unwrap(); inner != nil {
			return inner
		}
	}

	return err
}

func retryAfter(value string) time.Duration {
	if seconds, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if at, err := http.ParseTime(value); err == nil {
		return max(time.Until(at), 0)
	}

	return 0
}

func succeeded(success []int, status int) bool {
	if len(success) == 0 {
		return status >= 200 && status <= 299
	}
	for _, code := range success {
		if code == status {
			return true
		}
	}

	return false
}

// snippet is the start of a body, for the run page.
func snippet(body any) any {
	text, ok := body.(string)
	if !ok {
		encoded, _ := json.Marshal(body)
		if len(encoded) <= detailBytes {
			return body
		}
		text = string(encoded)
	}
	if len(text) > detailBytes {
		return text[:detailBytes] + "…"
	}

	return text
}

// httpOutput is the step's output: what its jq picks, or else the status and body.
func httpOutput(ctx context.Context, h *HTTPStep, resp response) (any, error) {
	if len(h.Output) == 0 {
		return map[string]any{"status": float64(resp.status), "body": resp.body}, nil
	}
	input := jsonish(roundTrip(resp.value()))
	out := map[string]any{}
	for _, output := range h.Output {
		code, err := compileJQ(output.Value.(string))
		if err != nil {
			return nil, failed(nil, "output %s is not valid jq: %v", output.Name, err)
		}
		jqCtx, cancel := context.WithTimeout(ctx, time.Second)
		value, ok := code.RunWithContext(jqCtx, input).Next()
		cancel()
		if err, isErr := value.(error); isErr {
			return nil, failed(map[string]any{"status": resp.status, "body": snippet(resp.body)},
				"output %s couldn't be read from the answer: %v", output.Name, err)
		}
		if ok {
			out[output.Name] = value
		}
	}

	return out, nil
}

// roundTrip makes a value plain JSON, as jq takes it.
func roundTrip(value any) any {
	encoded, _ := json.Marshal(value)
	var out any
	_ = json.Unmarshal(encoded, &out)

	return out
}

func (rn Runner) entityStep(ctx context.Context, e *EntityStep, scope Scope) (any, error) {
	identifier := strings.TrimSpace(scope.Text(e.Identifier))
	if identifier == "" {
		return nil, failed(nil, "the identifier came out empty")
	}
	link := rn.baseURL + "/catalog/" + e.Blueprint + "/" + identifier
	var entityErr integration.EntityError
	if e.Operation == OpDelete {
		err := integration.RemoveEntity(ctx, rn.db, e.Integration, e.Blueprint, identifier)
		if errors.As(err, &entityErr) {
			return nil, failed(map[string]any{"problems": entityErr.Problems}, "%s", entityErr.Error())
		}
		return map[string]any{"identifier": identifier, "blueprint": e.Blueprint}, err
	}

	properties, _ := scope.Resolve(e.Properties).(map[string]any)
	title := scope.Text(e.Title)
	outcome, err := integration.WriteEntity(ctx, rn.db, e.Integration, e.Blueprint,
		blueprint.Entity{Identifier: identifier, Title: title, Properties: properties}, true)
	if errors.As(err, &entityErr) {
		return nil, failed(map[string]any{"problems": entityErr.Problems}, "%s", entityErr.Error())
	}
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"identifier": identifier, "blueprint": e.Blueprint, "title": orString(title, identifier),
		"url": link, "outcome": outcome,
	}, nil
}

func (rn Runner) checkStep(ctx context.Context, c *CheckStep, scope Scope) (any, error) {
	published, err := blueprint.Published(ctx, rn.db)
	if err != nil {
		return nil, err
	}
	spec, ok := published[c.Blueprint]
	if !ok {
		return nil, failed(nil, "no blueprint %s is published", c.Blueprint)
	}
	page, err := entity.NewStore(rn.db).List(ctx, entity.Query{
		Blueprint: spec, Blueprints: published, Filter: scope.ResolveFilter(c.Filter), Page: 1, PageSize: 1,
	})
	var filterErr entity.FilterError
	if errors.As(err, &filterErr) {
		return nil, failed(nil, "the filter can't be answered: %v", filterErr)
	}
	if err != nil {
		return nil, err
	}
	count := page.Total
	ok = (c.Count == nil || count == *c.Count) && (c.Min == nil || count >= *c.Min) && (c.Max == nil || count <= *c.Max)
	if !ok {
		return nil, failed(map[string]any{"count": count}, "%s", scope.Text(c.Message))
	}

	return map[string]any{"count": float64(count)}, nil
}

func orString(s, fallback string) string {
	if s == "" {
		return fallback
	}

	return s
}
