package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/dvjn/goldenportal/internal/configdoc"
)

type SCMConfig struct {
	APIURL        string
	TokenEnv      string
	Organizations []string
	Repositories  []string
	Groups        []string
	Projects      []string
	MaxRuns       int
}

var (
	nextLink      = regexp.MustCompile(`<([^>]+)>\s*;\s*rel="next"`)
	secretEnvName = regexp.MustCompile(`^GP_SECRET_[A-Za-z0-9_]+$`)
)

func parseSCMConfig(r *configdoc.Reader, node *yaml.Node, path, provider string) *SCMConfig {
	c := &SCMConfig{MaxRuns: 20, APIURL: "https://api.github.com"}
	selectors := []string{"organizations", "repositories"}
	if provider == "gitlab" {
		c.APIURL = "https://gitlab.com/api/v4"
		selectors = []string{"groups", "projects"}
	}
	if node == nil {
		r.Errorf(node, path, "configure %s or %s", selectors[0], selectors[1])
		return c
	}
	fields, ok := r.Fields(node, path, append([]string{"apiURL", "token", "maxRuns"}, selectors...)...)
	if !ok {
		return c
	}
	if n := fields["apiURL"]; n != nil {
		c.APIURL = r.Text(n, path+".apiURL")
	}
	u, err := url.Parse(c.APIURL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		r.Errorf(configdoc.OrNode(fields["apiURL"], node), path+".apiURL", "apiURL must be an HTTP(S) API root without credentials, query or fragment")
	}
	c.APIURL = strings.TrimRight(c.APIURL, "/")
	if n := fields["token"]; n != nil {
		f, ok := r.Fields(n, path+".token", "env")
		if ok {
			c.TokenEnv = r.Text(f["env"], path+".token.env")
			if !secretEnvName.MatchString(c.TokenEnv) {
				r.Errorf(n, path+".token.env", "token must reference an environment variable starting with %s", SecretEnvPrefix)
			}
		}
	}
	read := func(name string) []string {
		out := []string{}
		r.List(fields[name], path+"."+name, func(_ int, n *yaml.Node) {
			value := r.Text(n, path+"."+name)
			if strings.TrimSpace(value) == "" || strings.ContainsAny(value, "?#\\\r\n") || strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") || strings.Contains(value, "//") {
				r.Errorf(n, path+"."+name, "use a nonempty source identifier, not a URL")
				return
			}
			for _, part := range strings.Split(value, "/") {
				if part == "." || part == ".." {
					r.Errorf(n, path+"."+name, "source identifiers cannot contain . or .. path segments")
					return
				}
			}
			if provider == "github" && ((name == "organizations" && strings.Contains(value, "/")) || (name == "repositories" && strings.Count(value, "/") != 1)) {
				r.Errorf(n, path+"."+name, "use an organization name or an owner/repository identifier")
				return
			}
			out = append(out, value)
		})
		return out
	}
	if provider == "github" {
		c.Organizations = read("organizations")
		c.Repositories = read("repositories")
	} else {
		c.Groups = read("groups")
		c.Projects = read("projects")
	}
	if len(c.Organizations)+len(c.Repositories)+len(c.Groups)+len(c.Projects) == 0 {
		r.Errorf(node, path, "configure at least one %s or %s entry", selectors[0], selectors[1])
	}
	if n := fields["maxRuns"]; n != nil {
		if err := n.Decode(&c.MaxRuns); err != nil || c.MaxRuns < 1 || c.MaxRuns > 1000 {
			r.Errorf(n, path+".maxRuns", "maxRuns must be a whole number from 1 to 1000")
		}
	}
	return c
}

type scmClient struct {
	base            *url.URL
	token, provider string
	client          *http.Client
}

func newSCMClient(c *SCMConfig, provider string) (*scmClient, error) {
	if c == nil {
		return nil, fmt.Errorf("%s has no config", provider)
	}
	u, err := url.Parse(c.APIURL)
	if err != nil {
		return nil, err
	}
	token := ""
	if c.TokenEnv != "" {
		token = os.Getenv(c.TokenEnv)
		if token == "" {
			return nil, fmt.Errorf("%s token environment reference is not set", provider)
		}
	}
	s := &scmClient{base: u, provider: provider, token: token}
	client := *httpClient
	// Pagination and redirects must never forward a source token to another
	// origin, but GitHub redirects renamed and transferred repositories within its API.
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || !s.allowed(req.URL) {
			return http.ErrUseLastResponse
		}
		return nil
	}
	s.client = &client
	return s, nil
}

func (c *scmClient) address(path string) string {
	return strings.TrimRight(c.base.String(), "/") + "/" + path
}

func (c *scmClient) allowed(u *url.URL) bool {
	root := strings.TrimRight(c.base.EscapedPath(), "/") + "/"
	return u.User == nil && u.Scheme == c.base.Scheme && strings.EqualFold(u.Host, c.base.Host) && strings.HasPrefix(u.EscapedPath(), root)
}

func (c *scmClient) get(ctx context.Context, address string) (any, http.Header, error) {
	u, err := url.Parse(address)
	if err != nil || !c.allowed(u) {
		return nil, nil, fmt.Errorf("%s pagination must stay within its API root", c.provider)
	}
	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
		if err != nil {
			return nil, nil, err
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "GoldenPortal")
		if c.provider == "github" {
			req.Header.Set("Accept", "application/vnd.github+json")
			req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		}
		if c.token != "" {
			if c.provider == "gitlab" {
				req.Header.Set("PRIVATE-TOKEN", c.token)
			} else {
				req.Header.Set("Authorization", "Bearer "+c.token)
			}
		}
		resp, err := c.client.Do(req)
		if err != nil {
			return nil, nil, fmt.Errorf("%s API request failed: %w", c.provider, err)
		}
		limited := resp.StatusCode == 429 || (resp.StatusCode == 403 && (resp.Header.Get("X-RateLimit-Remaining") == "0" || resp.Header.Get("Retry-After") != ""))
		if limited || resp.StatusCode >= 500 {
			resp.Body.Close()
			if attempt == 2 {
				return nil, nil, fmt.Errorf("%s API answered %s; retry the sync later", c.provider, resp.Status)
			}
			delay := time.Duration(attempt+1) * 100 * time.Millisecond
			if limited {
				delay = time.Second
				if value := resp.Header.Get("Retry-After"); value != "" {
					if seconds, e := strconv.Atoi(value); e == nil {
						delay = time.Duration(seconds) * time.Second
					} else if at, e := http.ParseTime(value); e == nil {
						delay = time.Until(at)
					}
				} else if value := resp.Header.Get("X-RateLimit-Reset"); value != "" {
					if unix, e := strconv.ParseInt(value, 10, 64); e == nil {
						delay = time.Until(time.Unix(unix, 0))
					}
				}
				if delay > 30*time.Second {
					return nil, nil, fmt.Errorf("%s API rate limit reached; retry the sync later", c.provider)
				}
			}
			if delay < 0 {
				delay = 0
			}
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, nil, ctx.Err()
			case <-timer.C:
			}
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			resp.Body.Close()
			return nil, nil, fmt.Errorf("%s API answered %s", c.provider, resp.Status)
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxPageBytes+1))
		resp.Body.Close()
		if readErr != nil {
			return nil, nil, fmt.Errorf("read %s response: %w", c.provider, readErr)
		}
		if len(body) > maxPageBytes {
			return nil, nil, fmt.Errorf("%s API response exceeds %d MB", c.provider, maxPageBytes>>20)
		}
		var data any
		if err := json.Unmarshal(body, &data); err != nil {
			return nil, nil, fmt.Errorf("%s API returned invalid JSON", c.provider)
		}
		return data, resp.Header, nil
	}
	return nil, nil, fmt.Errorf("%s API request failed", c.provider)
}

func (c *scmClient) object(ctx context.Context, path string) (map[string]any, error) {
	data, _, err := c.get(ctx, c.address(path))
	if err != nil {
		return nil, err
	}
	obj, ok := data.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s API expected an object", c.provider)
	}
	return obj, nil
}

func (c *scmClient) list(ctx context.Context, path, key string, limit int) ([]any, error) {
	address := c.address(path)
	u, _ := url.Parse(address)
	q := u.Query()
	q.Set("per_page", "100")
	u.RawQuery = q.Encode()
	address = u.String()
	out := []any{}
	seen := map[string]bool{}
	for page := 0; address != ""; page++ {
		if page >= maxPages {
			return nil, fmt.Errorf("%s collection exceeds %d pages", c.provider, maxPages)
		}
		if seen[address] {
			return nil, fmt.Errorf("%s pagination loop", c.provider)
		}
		seen[address] = true
		data, headers, err := c.get(ctx, address)
		if err != nil {
			return nil, err
		}
		if key != "" {
			obj, ok := data.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("%s API expected a collection object", c.provider)
			}
			data = obj[key]
		}
		items, ok := data.([]any)
		if !ok {
			return nil, fmt.Errorf("%s API expected an array for %s", c.provider, key)
		}
		for _, item := range items {
			if _, ok := item.(map[string]any); !ok {
				return nil, fmt.Errorf("%s API collection contains a non-object", c.provider)
			}
		}
		out = append(out, items...)
		if limit > 0 && len(out) >= limit {
			return out[:limit], nil
		}
		next := ""
		if match := nextLink.FindStringSubmatch(headers.Get("Link")); len(match) > 0 {
			next = match[1]
		} else if value := headers.Get("X-Next-Page"); value != "" {
			n, e := strconv.Atoi(value)
			if e != nil || n < 1 {
				return nil, fmt.Errorf("%s API returned an invalid next page", c.provider)
			}
			current, _ := url.Parse(address)
			q := current.Query()
			q.Set("page", strconv.Itoa(n))
			current.RawQuery = q.Encode()
			next = current.String()
		}
		if next != "" {
			current, _ := url.Parse(address)
			ref, err := url.Parse(next)
			if err != nil {
				return nil, fmt.Errorf("%s API returned an invalid pagination link", c.provider)
			}
			resolved := current.ResolveReference(ref)
			if !c.allowed(resolved) {
				return nil, fmt.Errorf("%s pagination must stay within its API root", c.provider)
			}
			next = resolved.String()
		}
		address = next
	}
	return out, nil
}

func scmContext(items []any, values map[string]any) []any {
	for _, item := range items {
		item.(map[string]any)["_gp"] = values
	}
	return items
}

func sourceID(value any) string {
	if n, ok := value.(float64); ok {
		return strconv.FormatFloat(n, 'f', 0, 64)
	}
	s, _ := value.(string)
	return s
}
