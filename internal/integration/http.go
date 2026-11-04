package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/itchyny/gojq"
	"go.yaml.in/yaml/v3"

	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/httpx"
)

// HTTPConfig is the http connector's config: its resources, each fetched
// from a URL page by page, sent to the ingest API, or both.
type HTTPConfig struct {
	Push     HTTPPush
	Resource map[string]HTTPResource
	order    []string
}

// HTTPPush is how requests to the ingest API are checked.
type HTTPPush struct {
	// Signature lets a system push without an ingestion token by signing
	// each request. Without it, pushes need a token.
	Signature *Signature
}

// HTTPResource is one resource: a pull side, a push side, or both. Both
// sides must give the same objects, since the same mappings run over them.
type HTTPResource struct {
	// URL is the first page to fetch. Without it, the resource is only pushed.
	URL string
	// Next is jq over a page, with the answer's headers as $headers, that
	// gives the next page's URL, absolute or relative to the page, or null
	// on the last page.
	Next string
	// Items is jq over a page giving its items; by default the page is one.
	Items string
	// Deleted is jq over an item that's true when it's a deletion.
	Deleted  string
	MaxPages int
	Headers  map[string]HeaderValue
	Push     *HTTPReceive
}

// HTTPReceive is a resource's push side. Its jq runs over the request as
// {body, headers, query}, with header names in lowercase.
type HTTPReceive struct {
	// Items gives the request's objects of this resource, maybe none.
	Items string
	// Deleted is true when the request's objects are deletions.
	Deleted string
}

// HeaderValue is a header's value, written out or read from the server's
// environment, so secrets stay out of documents.
type HeaderValue struct {
	Value string
	Env   string
}

const (
	// SecretEnvPrefix limits which environment variables documents can read,
	// so a builder can't reach the server's own settings.
	SecretEnvPrefix = "GP_SECRET_"

	httpTimeout  = 30 * time.Second
	maxPageBytes = 10 << 20
	// DefaultMaxPages stops a resource whose pages never end.
	DefaultMaxPages = 100

	// pageRetries is how many times a page is fetched again after a network
	// error, a 5xx or a 429, before the sync fails.
	pageRetries = 3
	// maxRetryWait is the longest Retry-After a sync waits out. Anything
	// longer fails it, and the next scheduled sync tries again.
	maxRetryWait = 30 * time.Second
)

var (
	resourceNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,62}$`)
	headerNamePattern   = regexp.MustCompile(`^[A-Za-z0-9-]+$`)
	// sincePattern is where a URL takes the time to fetch changes since.
	sincePattern = regexp.MustCompile(`\{\{\s*\$since\s*\}\}`)
	httpClient   = &http.Client{Timeout: httpTimeout}
	// retryBackoff is the wait before the first retry when the answer names
	// none, doubling for each one after. Tests shorten it.
	retryBackoff = time.Second
)

// ReadHeaders reads headers, each written out or read from an environment
// variable starting with SecretEnvPrefix. Issues go to r.
func ReadHeaders(r *configdoc.Reader, node *yaml.Node, path string) map[string]HeaderValue {
	headers := map[string]HeaderValue{}
	r.Each(node, path, func(header string, key, value *yaml.Node) {
		headerPath := path + "." + header
		if !headerNamePattern.MatchString(header) {
			r.Errorf(key, headerPath, "%q is not a header name", header)
			return
		}
		if value.Kind == yaml.ScalarNode {
			headers[header] = HeaderValue{Value: value.Value}
			return
		}
		if env, ok := readSecret(r, value, headerPath); ok {
			headers[header] = env
		}
	})

	return headers
}

// readSecret reads {env: GP_SECRET_...}.
func readSecret(r *configdoc.Reader, node *yaml.Node, path string) (HeaderValue, bool) {
	fields, ok := r.Fields(node, path, HTTPHeaderFields...)
	if !ok {
		return HeaderValue{}, false
	}
	env := r.Text(fields["env"], path+".env")
	if !strings.HasPrefix(env, SecretEnvPrefix) {
		r.Errorf(configdoc.OrNode(fields["env"], node), path+".env",
			"env must name a variable starting with %s", SecretEnvPrefix)
		return HeaderValue{}, false
	}

	return HeaderValue{Env: env}, true
}

// Resolve is the header's value, read from the environment when it names a variable.
func (h HeaderValue) Resolve() string {
	if h.Env != "" {
		return os.Getenv(h.Env)
	}

	return h.Value
}

// parseHTTPConfig reads and checks the http connector's config.
func parseHTTPConfig(r *configdoc.Reader, node *yaml.Node, path string) Config {
	if node == nil {
		return nil
	}
	config := &HTTPConfig{Resource: map[string]HTTPResource{}}
	fields, ok := r.Fields(node, path, HTTPConfigFields...)
	if !ok {
		return config
	}
	if n := fields["push"]; n != nil {
		if pushFields, ok := r.Fields(n, path+".push", HTTPPushFields...); ok && pushFields["signature"] != nil {
			config.Push.Signature = parseSignature(r, pushFields["signature"], path+".push.signature")
		}
	}
	r.Each(fields["resources"], path+".resources", func(name string, key, value *yaml.Node) {
		resourcePath := path + ".resources." + name
		if !resourceNamePattern.MatchString(name) {
			r.Errorf(key, resourcePath, "%q must start with a lowercase letter and use only lowercase letters, digits, - and _", name)
			return
		}
		if resource, ok := parseHTTPResource(r, value, resourcePath); ok {
			config.Resource[name] = resource
			config.order = append(config.order, name)
		}
	})
	if len(config.order) == 0 {
		r.Errorf(node, path+".resources", "add a resource, with a url to fetch it from, a push side, or both")
	}

	return config
}

func parseHTTPResource(r *configdoc.Reader, node *yaml.Node, path string) (HTTPResource, bool) {
	resource := HTTPResource{MaxPages: DefaultMaxPages}
	fields, ok := r.Fields(node, path, HTTPResourceFields...)
	if !ok {
		return resource, false
	}
	if n := fields["url"]; n != nil {
		resource.URL = r.Text(n, path+".url")
		if u, err := url.Parse(sincePattern.ReplaceAllString(resource.URL, "0")); err != nil ||
			(u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			r.Errorf(n, path+".url", "url must be an http or https URL")
		}
	}
	resource.Next = jqField(r, fields, path, "next", "$headers")
	resource.Items = jqField(r, fields, path, "items")
	resource.Deleted = jqField(r, fields, path, "deleted")
	if n := fields["maxPages"]; n != nil {
		if err := n.Decode(&resource.MaxPages); err != nil || resource.MaxPages < 1 {
			r.Errorf(n, path+".maxPages", "maxPages must be a whole number of at least 1")
		}
	}
	resource.Headers = ReadHeaders(r, fields["headers"], path+".headers")
	if resource.URL == "" {
		for _, key := range []string{"next", "items", "deleted", "maxPages", "headers"} {
			if n := fields[key]; n != nil {
				r.Errorf(n, path+"."+key, "%s is for fetching, so add a url or drop it", key)
			}
		}
	}
	if n := fields["push"]; n != nil {
		push := &HTTPReceive{}
		if pushFields, ok := r.Fields(n, path+".push", HTTPReceiveFields...); ok {
			push.Items = jqField(r, pushFields, path+".push", "items")
			push.Deleted = jqField(r, pushFields, path+".push", "deleted")
			if push.Items == "" {
				r.Errorf(n, path+".push.items", "add jq over {body, headers, query} giving the objects a request carries, such as .body.items[]")
			}
		}
		resource.Push = push
	}
	if resource.URL == "" && resource.Push == nil {
		r.Errorf(node, path, "add a url to fetch the resource from, a push side for requests that carry it, or both")
	}

	return resource, true
}

// jqField reads an optional jq field, which may use the variables named.
func jqField(r *configdoc.Reader, fields map[string]*yaml.Node, path, name string, variables ...string) string {
	n := fields[name]
	value := jqText(r, n, path+"."+name)
	if value == "" {
		return ""
	}
	if _, err := compile(value, variables...); err != nil {
		r.Errorf(n, path+"."+name, "%s is not valid jq: %v", name, err)
	}

	return value
}

func (c *HTTPConfig) Resources() []string { return c.order }

// Pulls lists the resources with a URL.
func (c *HTTPConfig) Pulls() []string {
	var out []string
	for _, name := range c.order {
		if c.Resource[name].URL != "" {
			out = append(out, name)
		}
	}

	return out
}

// Receives lists the resources with a push side.
func (c *HTTPConfig) Receives() []string {
	var out []string
	for _, name := range c.order {
		if c.Resource[name].Push != nil {
			out = append(out, name)
		}
	}

	return out
}

// UsesSince reports whether any URL fetches only what changed.
func (c *HTTPConfig) UsesSince() bool {
	for _, resource := range c.Resource {
		if sincePattern.MatchString(resource.URL) {
			return true
		}
	}

	return false
}

// Pull fetches each resource asked for, page by page.
func (c *HTTPConfig) Pull(ctx context.Context, _ configdoc.DB, p PullRequest,
	emit func(resource string, items []Item) error,
) (map[string]error, error) {
	failed := map[string]error{}
	since := url.QueryEscape(p.Since.UTC().Format(time.RFC3339))
	for _, name := range p.Resources {
		resource, ok := c.Resource[name]
		if !ok || resource.URL == "" {
			failed[name] = fmt.Errorf("no resource %q is fetched", name)
			continue
		}
		err := fetchPages(ctx, name, resource, sincePattern.ReplaceAllLiteralString(resource.URL, since),
			func(page any) error {
				items, err := resource.split(name, page)
				if err != nil {
					return err
				}
				if err := emit(name, items); err != nil {
					return emitError{err}
				}
				return nil
			})
		var emitErr emitError
		switch {
		case errors.As(err, &emitErr):
			return failed, emitErr.err
		case err != nil:
			failed[name] = err
		}
	}

	return failed, nil
}

// emitError is a failure to write what was fetched, which stops the sync
// rather than counting against one resource.
type emitError struct{ err error }

func (e emitError) Error() string { return e.err.Error() }

// split turns a page into items.
func (resource HTTPResource) split(name string, page any) ([]Item, error) {
	objects := []any{page}
	if resource.Items != "" {
		var err error
		if objects, err = all(resource.Items, page); err != nil {
			return nil, fmt.Errorf("%s items: %w", name, err)
		}
	}
	items := make([]Item, 0, len(objects))
	for _, object := range objects {
		item := Item{Resource: name, Object: object}
		if resource.Deleted != "" {
			deleted, err := all(resource.Deleted, object)
			if err != nil {
				return nil, fmt.Errorf("%s deleted: %w", name, err)
			}
			item.Deleted = len(deleted) > 0 && deleted[0] == true
		}
		items = append(items, item)
	}

	return items, nil
}

// all runs jq and returns every result.
func all(expression string, input any) ([]any, error) {
	code, err := compile(expression)
	if err != nil {
		return nil, err
	}
	var out []any
	iter := code.Run(input)
	for {
		value, ok := iter.Next()
		if !ok {
			return out, nil
		}
		if err, isErr := value.(error); isErr {
			return nil, err
		}
		out = append(out, normalise(value))
	}
}

// fetchPages fetches a resource from address, handing each page to page as
// it arrives.
func fetchPages(ctx context.Context, name string, resource HTTPResource, address string, page func(any) error) error {
	var next *gojq.Code
	if resource.Next != "" {
		code, err := compile(resource.Next, "$headers")
		if err != nil {
			return err
		}
		next = code
	}

	seen := map[string]bool{}
	for pages := 0; address != ""; pages++ {
		if pages == resource.MaxPages {
			return fmt.Errorf("%s has more than %d pages; raise maxPages if that's expected", name, resource.MaxPages)
		}
		if seen[address] {
			return fmt.Errorf("%s pages loop back to %s", name, address)
		}
		seen[address] = true

		body, headers, err := fetchJSON(ctx, address, resource.Headers)
		if err != nil {
			return fmt.Errorf("fetch %s: %w", name, err)
		}
		if err := page(body); err != nil {
			return err
		}
		if next == nil {
			break
		}

		value, err := firstWith(next, body, headers)
		if err != nil {
			return fmt.Errorf("%s next: %w", name, err)
		}
		link, _ := value.(string)
		if link == "" {
			break
		}
		base, _ := url.Parse(address)
		ref, err := url.Parse(link)
		if err != nil {
			return fmt.Errorf("%s next gave %q, which isn't a URL", name, link)
		}
		resolved := base.ResolveReference(ref)
		if resolved.Scheme != "http" && resolved.Scheme != "https" {
			return fmt.Errorf("%s next gave %q, which isn't an http URL", name, link)
		}
		address = resolved.String()
	}

	return nil
}

// fetchJSON fetches one page, trying again after failures that may pass. It
// returns the page and the answer's headers, by lowercase name.
func fetchJSON(ctx context.Context, address string, headers map[string]HeaderValue) (any, map[string]any, error) {
	for retry := 0; ; retry++ {
		page, answered, wait, err := fetchJSONOnce(ctx, address, headers)
		if wait < 0 || retry == pageRetries {
			return page, answered, err
		}
		if wait == 0 {
			wait = retryBackoff << retry
		}
		if wait > maxRetryWait {
			return nil, nil, fmt.Errorf("%w, and asked to wait %s before trying again", err, wait.Round(time.Second))
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, nil, errors.Join(err, ctx.Err())
		case <-timer.C:
		}
	}
}

// fetchJSONOnce fetches one page. wait is negative when trying again won't
// help, and otherwise what the answer asked to wait, if anything.
func fetchJSONOnce(ctx context.Context, address string, headers map[string]HeaderValue) (any, map[string]any, time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, nil, -1, err
	}
	req.Header.Set("Accept", "application/json")
	for name, header := range headers {
		req.Header.Set(name, header.Resolve())
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, nil, -1, err
		}
		return nil, nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, nil, httpx.RetryAfter(resp.Header.Get("Retry-After")), fmt.Errorf("%s answered %s", address, resp.Status)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, nil, -1, fmt.Errorf("%s answered %s", address, resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPageBytes+1))
	if err != nil {
		return nil, nil, 0, err
	}
	if len(body) > maxPageBytes {
		return nil, nil, -1, fmt.Errorf("%s answered with more than %d MB", address, maxPageBytes>>20)
	}
	var page any
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, nil, -1, fmt.Errorf("%s didn't answer with JSON: %w", address, err)
	}

	return page, lowerHeaders(resp.Header), -1, nil
}

// lowerHeaders gives jq a request's or answer's headers by lowercase name,
// each its first value.
func lowerHeaders(h http.Header) map[string]any {
	out := map[string]any{}
	for name, values := range h {
		if len(values) > 0 {
			out[strings.ToLower(name)] = values[0]
		}
	}

	return out
}

// Receive turns a request into items of the resources it asks for.
func (c *HTTPConfig) Receive(req Request, authenticated bool) ([]Item, error) {
	if !authenticated {
		if c.Push.Signature == nil {
			return nil, ErrUnverified
		}
		if err := c.Push.Signature.Verify(req.Header, req.Body); err != nil {
			return nil, err
		}
	}
	var body any
	if err := json.Unmarshal(req.Body, &body); err != nil {
		return nil, ErrNotJSON
	}
	query := map[string]any{}
	for name, values := range req.Query {
		query[name] = values[0]
	}
	envelope := map[string]any{"body": body, "headers": lowerHeaders(req.Header), "query": query}

	var items []Item
	for _, name := range req.Resources {
		push := c.Resource[name].Push
		objects, err := all(push.Items, envelope)
		if err != nil {
			return nil, ReceiveError{fmt.Sprintf("resources.%s.push.items: %v", name, err)}
		}
		deleted := false
		if push.Deleted != "" {
			values, err := all(push.Deleted, envelope)
			if err != nil {
				return nil, ReceiveError{fmt.Sprintf("resources.%s.push.deleted: %v", name, err)}
			}
			deleted = len(values) > 0 && values[0] == true
		}
		for _, object := range objects {
			items = append(items, Item{Resource: name, Object: object, Deleted: deleted})
		}
	}

	return items, nil
}

// Check fetches the first page of each resource it pulls, and checks the
// secrets it reads are set.
func (c *HTTPConfig) Check(ctx context.Context, _ configdoc.DB) error {
	if c.Push.Signature != nil && c.Push.Signature.Secret.Resolve() == "" {
		return fmt.Errorf("%s isn't set on the server, so signed pushes fail", c.Push.Signature.Secret.Env)
	}
	since := url.QueryEscape(time.Now().UTC().Format(time.RFC3339))
	for _, name := range c.order {
		resource := c.Resource[name]
		if resource.URL == "" {
			continue
		}
		for header, value := range resource.Headers {
			if value.Env != "" && value.Resolve() == "" {
				return fmt.Errorf("%s isn't set on the server, so %s's %s header is empty", value.Env, name, header)
			}
		}
		if _, _, _, err := fetchJSONOnce(ctx, sincePattern.ReplaceAllLiteralString(resource.URL, since), resource.Headers); err != nil {
			return fmt.Errorf("fetch %s: %w", name, err)
		}
	}

	return nil
}
