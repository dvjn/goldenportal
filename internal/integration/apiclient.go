package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// What calling a code host's REST API takes, whichever it is: staying within
// its API, reading lists page by page, trying again what may pass, and
// holding back each credential's requests through its rate limit.

const (
	apiPerPage   = 100
	apiRedirects = 5
	// apiConcurrency is how many requests may be in flight with one
	// credential: enough for a burst of webhook events to each read its
	// object within the seconds the system waits, and few enough to stay
	// clear of the limits it sets on bursts.
	apiConcurrency = 4
)

var (
	// apiMaxWait is the longest a sync waits out a rate limit. Anything
	// longer fails what's left, and a later sync carries on.
	apiMaxWait  = time.Minute
	apiNextLink = regexp.MustCompile(`<([^>]+)>\s*;\s*rel="next"`)
	// apiBuckets are the credentials' rate limits, which every sync and
	// webhook event using a credential share.
	apiBuckets = bucketCache{buckets: map[string]*apiBucket{}}
)

type bucketCache struct {
	mu      sync.Mutex
	buckets map[string]*apiBucket
}

// apiBucket is one credential's rate limit.
type apiBucket struct {
	// resetAt is when it lets requests through again, once an answer said
	// to wait.
	resetAt time.Time
	// slots hold the requests in flight with it.
	slots chan struct{}
}

func (b *bucketCache) get(key string) *apiBucket {
	b.mu.Lock()
	defer b.mu.Unlock()
	bucket, ok := b.buckets[key]
	if !ok {
		bucket = &apiBucket{slots: make(chan struct{}, apiConcurrency)}
		b.buckets[key] = bucket
	}

	return bucket
}

// credential signs a request in.
type credential struct {
	header, value string
	// bucket names the rate limit it counts against.
	bucket string
	// renew gives a fresh one once this one is refused, for credentials a
	// system can stop taking before they expire, such as a GitHub App
	// installation's token, or is nil.
	renew func(ctx context.Context) (credential, error)
}

// apiClient calls one system's REST API, such as GitLab's, for one sync,
// event or check. The fields after base say how the system differs.
type apiClient struct {
	system  string
	base    *url.URL
	http    *http.Client
	maxWait time.Duration

	headers map[string]string
	// remaining and reset name the headers an answer says its rate limit's
	// state in: what's left, and when it resets, in Unix seconds.
	remaining, reset string
	// authorize gives the credential for a request about scope, such as a
	// GitHub owner.
	authorize func(ctx context.Context, scope string) (credential, error)
	// limited reads a refusal that was a rate limit, or is nil.
	limited func(status int, header http.Header, body []byte, attempt int) *rateLimit
	// retryable says whether a request that failed in a way the system may
	// still have acted on can be sent again, as a POST making something
	// can't.
	retryable func(method, address string) bool
	// message reads why the system refused.
	message func(body []byte) string
}

func newAPIClient(system, apiURL string) (*apiClient, error) {
	base, err := url.Parse(apiURL)
	if err != nil {
		return nil, err
	}
	c := &apiClient{
		system: system, base: base, maxWait: apiMaxWait,
		retryable: func(method, _ string) bool { return method != http.MethodPost },
		message:   func([]byte) string { return "" },
	}
	c.http = &http.Client{
		Timeout: httpTimeout,
		// A system may redirect within its API, as GitHub does for a moved
		// repository. Anywhere else mustn't get the credentials.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= apiRedirects || !c.within(req.URL) {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}

	return c, nil
}

// within reports whether u is under the API root, so it may get credentials.
// Dot segments, written out or encoded, could climb out of the root once a
// server resolves them, so they're never within.
func (c *apiClient) within(u *url.URL) bool {
	if slices.ContainsFunc(strings.Split(u.Path, "/"), func(segment string) bool { return segment == "." || segment == ".." }) {
		return false
	}
	root := strings.TrimRight(c.base.EscapedPath(), "/") + "/"
	return u.User == nil && u.Scheme == c.base.Scheme && strings.EqualFold(u.Host, c.base.Host) &&
		strings.HasPrefix(u.EscapedPath()+"/", root)
}

func (c *apiClient) address(path string) string {
	return strings.TrimRight(c.base.String(), "/") + "/" + strings.TrimPrefix(path, "/")
}

// relative is an address within the API as a path under its root, for
// messages.
func (c *apiClient) relative(address string) string {
	if u, err := url.Parse(address); err == nil && c.within(u) {
		path := strings.TrimPrefix(u.EscapedPath(), strings.TrimRight(c.base.EscapedPath(), "/"))
		if u.RawQuery != "" {
			path += "?" + u.RawQuery
		}
		return path
	}

	return address
}

// apiError is an answer a system gave that isn't what was asked for, kept
// apart from network errors so callers can act on its status.
type apiError struct {
	System  string
	Path    string
	Status  int
	Message string
}

func (e *apiError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("%s answered %s with %d %s", e.System, e.Path, e.Status, http.StatusText(e.Status))
	}

	return fmt.Sprintf("%s answered %s with %d: %s", e.System, e.Path, e.Status, e.Message)
}

func apiStatus(err error) int {
	var aerr *apiError
	if errors.As(err, &aerr) {
		return aerr.Status
	}

	return 0
}

// apiGone reports whether a system answered that what was asked for isn't
// there.
func apiGone(err error) bool {
	status := apiStatus(err)
	return status == http.StatusNotFound || status == http.StatusGone
}

// rateLimit is a refusal that was a rate limit.
type rateLimit struct {
	// wait is how long it asked to wait.
	wait time.Duration
	// shared is whether the limit is the credential's, which holds back all
	// its requests, rather than the request's alone.
	shared bool
}

// rateLimitError is a rate limit that lasts longer than a sync waits.
type rateLimitError struct {
	system string
	until  time.Time
}

func (e rateLimitError) Error() string {
	return e.system + "'s rate limit for these credentials is spent until " + e.until.UTC().Format("15:04:05 UTC") +
		"; the next sync after that carries on"
}

func (c *apiClient) get(ctx context.Context, scope, path string, out any) (http.Header, error) {
	body, header, err := c.request(ctx, http.MethodGet, scope, c.address(path), nil)

	return header, c.decode(path, body, err, out)
}

func (c *apiClient) write(ctx context.Context, method, scope, path string, in, out any) error {
	payload, err := json.Marshal(in)
	if err != nil {
		return err
	}
	body, _, err := c.request(ctx, method, scope, c.address(path), payload)

	return c.decode(path, body, err, out)
}

func (c *apiClient) decode(path string, body []byte, err error, out any) error {
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("%s answered %s with something other than the JSON expected", c.system, c.relative(c.address(path)))
	}

	return nil
}

// request sends with scope's credential, and once more with a fresh one if
// that's refused and can be renewed.
func (c *apiClient) request(ctx context.Context, method, scope, address string, payload []byte) ([]byte, http.Header, error) {
	cred, err := c.authorize(ctx, scope)
	if err != nil {
		return nil, nil, err
	}
	body, header, err := c.send(ctx, method, address, cred, payload)
	if apiStatus(err) != http.StatusUnauthorized || cred.renew == nil {
		return body, header, err
	}
	if cred, err = cred.renew(ctx); err != nil {
		return nil, nil, err
	}

	return c.send(ctx, method, address, cred, payload)
}

// list reads a list page by page, handing each page's objects to page
// until it says to stop or the pages run out. key names the list in
// answers that wrap it. It follows the next link each answer gives, signing
// in afresh for each page, so a long list can't outlive a credential.
func (c *apiClient) list(ctx context.Context, scope, path, key string, page func(objects []map[string]any) (bool, error)) error {
	u, err := url.Parse(c.address(path))
	if err != nil {
		return err
	}
	if q := u.Query(); q.Get("per_page") == "" {
		q.Set("per_page", strconv.Itoa(apiPerPage))
		u.RawQuery = q.Encode()
	}

	seen := map[string]bool{}
	for address, pages := u.String(), 0; address != ""; pages++ {
		if pages == DefaultMaxPages {
			return fmt.Errorf("%s has more than %d pages", c.relative(u.String()), DefaultMaxPages)
		}
		if seen[address] {
			return fmt.Errorf("%s pages loop back to %s", c.relative(u.String()), c.relative(address))
		}
		seen[address] = true

		body, header, err := c.request(ctx, http.MethodGet, scope, address, nil)
		if err != nil {
			return err
		}
		objects, err := pageObjects(body, key)
		if err != nil {
			return fmt.Errorf("%s answered %s with %w", c.system, c.relative(address), err)
		}
		more, err := page(objects)
		if err != nil || !more {
			return err
		}

		match := apiNextLink.FindStringSubmatch(header.Get("Link"))
		if match == nil {
			break
		}
		current, _ := url.Parse(address)
		next, err := current.Parse(match[1])
		if err != nil || !c.within(next) {
			return fmt.Errorf("%s's next page for %s is outside its API, at %s", c.system, c.relative(u.String()), match[1])
		}
		address = next.String()
	}

	return nil
}

// pageObjects reads a page of a list, wrapped under key or not. A page
// without its list is an error rather than an empty page, which a complete
// sync would take to mean everything was deleted.
func pageObjects(body []byte, key string) ([]map[string]any, error) {
	var list []map[string]any
	if key == "" {
		if err := json.Unmarshal(body, &list); err != nil {
			return nil, errors.New("something other than a list")
		}
		return list, nil
	}
	var wrapped map[string]json.RawMessage
	if err := json.Unmarshal(body, &wrapped); err != nil {
		return nil, errors.New("something other than an object")
	}
	raw, ok := wrapped[key]
	if !ok {
		return nil, fmt.Errorf("no %s", key)
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("a %s that isn't a list", key)
	}

	return list, nil
}

// send makes a request, waiting out rate limits that end soon, which a
// system refuses before acting, and trying again after failures that may
// pass when the request can be sent again. A rate limit of the credential's
// holds back every later request with it until it ends, failing them at
// once if that's longer than the client waits.
func (c *apiClient) send(ctx context.Context, method, address string, cred credential, payload []byte) ([]byte, http.Header, error) {
	for attempt := 0; ; attempt++ {
		if err := c.waitForReset(ctx, cred.bucket); err != nil {
			return nil, nil, err
		}
		body, header, status, err := c.sendOnce(ctx, method, address, cred, payload)
		if err == nil {
			return body, header, nil
		}
		var limit *rateLimit
		if status != 0 {
			limit = c.limited(status, header, body, attempt)
		}
		switch {
		case limit == nil:
		case limit.shared:
			c.holdUntil(cred.bucket, time.Now().Add(limit.wait))
		case limit.wait > c.maxWait:
			return nil, header, fmt.Errorf("%w, and asked to wait %s before trying again", err, limit.wait.Round(time.Second))
		default:
			if err := sleep(ctx, limit.wait); err != nil {
				return nil, header, errors.Join(err, ctx.Err())
			}
		}
		switch {
		case attempt == pageRetries:
			return nil, header, err
		case limit != nil:
		case (status == 0 || status >= 500) && c.retryable(method, address):
			if err := sleep(ctx, retryBackoff<<attempt); err != nil {
				return nil, header, errors.Join(err, ctx.Err())
			}
		default:
			return nil, header, err
		}
	}
}

// sendOnce makes a request once. status is 0 when there was no answer to
// read, which may pass.
func (c *apiClient) sendOnce(ctx context.Context, method, address string, cred credential, payload []byte) ([]byte, http.Header, int, error) {
	var reader io.Reader
	if payload != nil {
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, address, reader)
	if err != nil {
		return nil, nil, 0, err
	}
	for name, value := range c.headers {
		req.Header.Set(name, value)
	}
	req.Header.Set(cred.header, cred.value)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	slots := apiBuckets.get(cred.bucket).slots
	select {
	case slots <- struct{}{}:
	case <-ctx.Done():
		return nil, nil, 0, ctx.Err()
	}
	defer func() { <-slots }()
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, nil, 0, err
	}
	defer resp.Body.Close()
	c.noteLimit(cred.bucket, resp.Header)
	body, err := readPage(resp.Body)
	var tooLarge tooLargeError
	switch {
	case errors.As(err, &tooLarge):
		return nil, resp.Header, resp.StatusCode, fmt.Errorf("%s answered %s %w", c.system, c.relative(address), err)
	case err != nil:
		return nil, resp.Header, 0, fmt.Errorf("%s's answer to %s broke off: %w", c.system, c.relative(address), err)
	}
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return nil, resp.Header, resp.StatusCode, fmt.Errorf("%s redirected %s outside its API, to %s", c.system, c.relative(address), resp.Header.Get("Location"))
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return body, resp.Header, resp.StatusCode, &apiError{System: c.system, Path: c.relative(address), Status: resp.StatusCode, Message: c.message(body)}
	}

	return body, resp.Header, resp.StatusCode, nil
}

// noteLimit holds back a credential's requests until its rate limit resets,
// once an answer says it's spent.
func (c *apiClient) noteLimit(bucket string, header http.Header) {
	if c.remaining == "" || header.Get(c.remaining) != "0" {
		return
	}
	if reset, err := strconv.ParseInt(header.Get(c.reset), 10, 64); err == nil {
		c.holdUntil(bucket, time.Unix(reset, 0).Add(time.Second))
	}
}

func (c *apiClient) holdUntil(bucket string, until time.Time) {
	b := apiBuckets.get(bucket)
	apiBuckets.mu.Lock()
	defer apiBuckets.mu.Unlock()
	if until.After(b.resetAt) {
		b.resetAt = until
	}
}

// waitForReset waits until bucket lets requests through, failing at once
// if that's longer than the client waits.
func (c *apiClient) waitForReset(ctx context.Context, bucket string) error {
	b := apiBuckets.get(bucket)
	apiBuckets.mu.Lock()
	until := b.resetAt
	apiBuckets.mu.Unlock()
	wait := time.Until(until)
	if wait <= 0 {
		return nil
	}
	if wait > c.maxWait {
		return rateLimitError{system: c.system, until: until}
	}

	return sleep(ctx, wait)
}
