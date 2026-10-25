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
	"slices"
	"strings"
	"time"

	"github.com/itchyny/gojq"
	"go.yaml.in/yaml/v3"

	"github.com/dvjn/goldenportal/internal/configdoc"
)

// HTTPConfig is the http connector's config: the resources it fetches, each
// a JSON document at a URL, maybe split into pages.
type HTTPConfig struct {
	Resources map[string]HTTPResource
	order     []string
}

// HTTPResource is one JSON document to fetch, page by page.
type HTTPResource struct {
	URL string
	// Next is jq over a page that gives the next page's URL, absolute or
	// relative to the page, or null on the last page.
	Next    string
	Headers map[string]HeaderValue
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
	maxPages     = 100
)

var (
	resourceNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,62}$`)
	headerNamePattern   = regexp.MustCompile(`^[A-Za-z0-9-]+$`)
	httpClient          = &http.Client{Timeout: httpTimeout}
)

// parseHTTPConfig reads and checks the http connector's config.
func parseHTTPConfig(r *configdoc.Reader, node *yaml.Node, path string) *HTTPConfig {
	config := &HTTPConfig{Resources: map[string]HTTPResource{}}
	fields, ok := r.Fields(node, path, "resources")
	if !ok {
		return config
	}
	r.Each(fields["resources"], path+".resources", func(name string, key, value *yaml.Node) {
		resourcePath := path + ".resources." + name
		if !resourceNamePattern.MatchString(name) {
			r.Errorf(key, resourcePath, "%q must start with a lowercase letter and use only lowercase letters, digits, - and _", name)
			return
		}
		resource := HTTPResource{Headers: map[string]HeaderValue{}}
		resourceFields, ok := r.Fields(value, resourcePath, "url", "next", "headers")
		if !ok {
			return
		}
		resource.URL = r.Text(resourceFields["url"], resourcePath+".url")
		if u, err := url.Parse(resource.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			r.Errorf(configdoc.OrNode(resourceFields["url"], value), resourcePath+".url", "url must be an http or https URL")
		}
		if n := resourceFields["next"]; n != nil {
			resource.Next = jqText(r, n, resourcePath+".next")
			if err := compileJQ(resource.Next); err != nil {
				r.Errorf(n, resourcePath+".next", "next is not valid jq: %v", err)
			}
		}
		r.Each(resourceFields["headers"], resourcePath+".headers", func(header string, key, value *yaml.Node) {
			headerPath := resourcePath + ".headers." + header
			if !headerNamePattern.MatchString(header) {
				r.Errorf(key, headerPath, "%q is not a header name", header)
				return
			}
			if value.Kind == yaml.ScalarNode {
				resource.Headers[header] = HeaderValue{Value: value.Value}
				return
			}
			envFields, ok := r.Fields(value, headerPath, "env")
			if !ok {
				return
			}
			env := r.Text(envFields["env"], headerPath+".env")
			if !strings.HasPrefix(env, SecretEnvPrefix) {
				r.Errorf(configdoc.OrNode(envFields["env"], value), headerPath+".env",
					"env must name a variable starting with %s", SecretEnvPrefix)
				return
			}
			resource.Headers[header] = HeaderValue{Env: env}
		})
		config.Resources[name] = resource
		config.order = append(config.order, name)
	})
	if len(config.Resources) == 0 {
		r.Errorf(node, path+".resources", "add a resource to fetch, with its url")
	}

	return config
}

func compileJQ(expression string) error {
	_, err := compile(expression)
	return err
}

// httpPull fetches the resources asked for. Each resource is the list of
// its pages, so mappings run over every page.
func httpPull(ctx context.Context, _ configdoc.DB, spec Spec, resources []string) (map[string]any, error) {
	if spec.http == nil {
		return nil, errors.New("the http connector has no config")
	}
	out := map[string]any{}
	for _, name := range resources {
		resource, ok := spec.http.Resources[name]
		if !ok {
			return nil, fmt.Errorf("no resource %q is configured", name)
		}
		pages, err := fetchPages(ctx, name, resource)
		if err != nil {
			return nil, err
		}
		out[name] = pages
	}

	return out, nil
}

func fetchPages(ctx context.Context, name string, resource HTTPResource) ([]any, error) {
	var next *gojq.Code
	if resource.Next != "" {
		code, err := compile(resource.Next)
		if err != nil {
			return nil, err
		}
		next = code
	}

	pages := []any{}
	seen := map[string]bool{}
	address := resource.URL
	for address != "" {
		if len(pages) == maxPages {
			return nil, fmt.Errorf("%s has more than %d pages", name, maxPages)
		}
		if seen[address] {
			return nil, fmt.Errorf("%s pages loop back to %s", name, address)
		}
		seen[address] = true

		page, err := fetchJSON(ctx, address, resource.Headers)
		if err != nil {
			return nil, fmt.Errorf("fetch %s: %w", name, err)
		}
		pages = append(pages, page)
		if next == nil {
			break
		}

		value, err := first(next, page)
		if err != nil {
			return nil, fmt.Errorf("%s next: %w", name, err)
		}
		link, _ := value.(string)
		if link == "" {
			break
		}
		base, _ := url.Parse(address)
		ref, err := url.Parse(link)
		if err != nil {
			return nil, fmt.Errorf("%s next gave %q, which isn't a URL", name, link)
		}
		resolved := base.ResolveReference(ref)
		if resolved.Scheme != "http" && resolved.Scheme != "https" {
			return nil, fmt.Errorf("%s next gave %q, which isn't an http URL", name, link)
		}
		address = resolved.String()
	}

	return pages, nil
}

func fetchJSON(ctx context.Context, address string, headers map[string]HeaderValue) (any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	for name, header := range headers {
		value := header.Value
		if header.Env != "" {
			value = os.Getenv(header.Env)
		}
		req.Header.Set(name, value)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("%s answered %s", address, resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPageBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxPageBytes {
		return nil, fmt.Errorf("%s answered with more than %d MB", address, maxPageBytes>>20)
	}
	var page any
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, fmt.Errorf("%s didn't answer with JSON: %w", address, err)
	}

	return page, nil
}

// resourceNames lists what a pull integration can fetch: the connector's
// fixed resources, or for http, the ones its config defines.
func (s Spec) resourceNames(c Connector) []string {
	if s.http != nil {
		return slices.Clone(s.http.order)
	}

	return c.Resources
}
