package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/dvjn/goldenportal/internal/configdoc"
)

// AuthentikConfig reads an authentik instance's users and groups, each
// group with its members by username.
type AuthentikConfig struct {
	// URL is the instance's address, whose API is under /api/v3.
	URL   string
	Token HeaderValue
}

const (
	authentikUsers  = "users"
	authentikGroups = "groups"
)

// AuthentikConfigFields are the keys the authentik connector's config takes,
// exported so the documents' JSON Schema can be checked against them.
var AuthentikConfigFields = []string{"url", "token"}

func parseAuthentikConfig(r *configdoc.Reader, node *yaml.Node, path string) Config {
	if node == nil {
		return nil
	}
	config := &AuthentikConfig{}
	fields, ok := r.Fields(node, path, AuthentikConfigFields...)
	if !ok {
		return config
	}
	if n := fields["url"]; n != nil {
		config.URL = strings.TrimRight(r.Text(n, path+".url"), "/")
		u, err := url.Parse(config.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			r.Errorf(n, path+".url", "url must be the instance's http or https address without credentials, a query or a fragment, such as https://auth.example.com")
		}
	} else {
		r.Errorf(node, path+".url", "add the instance's url, such as https://auth.example.com")
	}
	if n := fields["token"]; n != nil {
		config.Token, _ = readSecret(r, n, path+".token")
	} else {
		r.Errorf(node, path+".token", "add a token, as {env: %s...} holding an API token whose user can view users and groups", SecretEnvPrefix)
	}

	return config
}

func (c *AuthentikConfig) Resources() []string { return []string{authentikUsers, authentikGroups} }

func (c *AuthentikConfig) Pulls() []string { return []string{authentikUsers, authentikGroups} }

func newAuthentikClient(config *AuthentikConfig) (*apiClient, error) {
	api, err := newAPIClient("authentik", config.URL+"/api/v3")
	if err != nil {
		return nil, err
	}
	token := config.Token.Resolve()
	if token == "" {
		return nil, fmt.Errorf("%s isn't set on the server, so the connector can't sign in to authentik", config.Token.Env)
	}
	cred := credential{header: "Authorization", value: "Bearer " + token, bucket: config.URL}
	api.headers = map[string]string{"Accept": "application/json", "User-Agent": "golden-portal"}
	api.authorize = func(context.Context, string) (credential, error) { return cred, nil }
	api.message = authentikMessage
	api.limited = sonarLimited

	return api, nil
}

// authentikMessage is the detail authentik answers a refusal with.
func authentikMessage(body []byte) string {
	var answer struct {
		Detail string `json:"detail"`
	}
	if json.Unmarshal(body, &answer) != nil {
		return ""
	}

	return answer.Detail
}

// Check checks the token is valid and its user can see users and groups
// besides its own.
func (c *AuthentikConfig) Check(ctx context.Context, _ configdoc.DB, req CheckRequest) error {
	client, err := newAuthentikClient(c)
	if err != nil {
		return err
	}
	var me map[string]any
	if _, err := client.get(ctx, "", "core/users/me/", &me); err != nil {
		return err
	}
	for _, resource := range []string{authentikUsers, authentikGroups} {
		var page struct {
			Pagination struct {
				Count int `json:"count"`
			} `json:"pagination"`
		}
		if _, err := client.get(ctx, "", "core/"+resource+"/?page_size=1", &page); err != nil {
			return err
		}
		if page.Pagination.Count <= 1 {
			req.Warn(fmt.Sprintf("The token's user sees %d %s, so a sync reads next to none; give it a role that can view %s.",
				page.Pagination.Count, resource, resource))
		}
	}

	return nil
}

// Pull reads every user and group, by name under _gp: each user's groups,
// with those it's in through a child group, as authentik counts them, and
// those it was added to directly; and each group's members the same two
// ways, and its ancestors. The objects' own lists are by primary key.
func (c *AuthentikConfig) Pull(ctx context.Context, _ configdoc.DB, p PullRequest,
	emit func(resource string, items []Item) error,
) (map[string]error, error) {
	failed := func(err error) (map[string]error, error) {
		out := map[string]error{}
		for _, r := range p.Resources {
			out[r] = err
		}
		return out, nil
	}
	client, err := newAuthentikClient(c)
	if err != nil {
		return failed(err)
	}
	// Each side names the other by primary key, so both are read whichever
	// the sync asked for.
	users, err := authentikList(ctx, client, "core/users/?include_groups=false")
	if err != nil {
		return failed(err)
	}
	groups, err := authentikList(ctx, client, "core/groups/?include_users=false")
	if err != nil {
		return failed(err)
	}
	byKey := map[string]map[string]any{}
	for _, group := range groups {
		byKey[authentikKey(group["pk"])] = group
	}
	ancestors := map[string][]string{}
	for key := range byKey {
		ancestors[key] = authentikAncestors(byKey, key)
	}
	name := func(key string) string { n, _ := byKey[key]["name"].(string); return n }
	members, direct := map[string][]string{}, map[string][]string{}
	for _, user := range users {
		username, _ := user["username"].(string)
		var in, added []string
		pks, _ := user["groups"].([]any)
		for _, pk := range pks {
			key := authentikKey(pk)
			if _, ok := byKey[key]; !ok {
				continue
			}
			added = append(added, name(key))
			direct[key] = append(direct[key], username)
			for _, k := range append([]string{key}, ancestors[key]...) {
				in = append(in, name(k))
				members[k] = append(members[k], username)
			}
		}
		user[parentKey] = map[string]any{"groups": sortedNames(in), "directGroups": sortedNames(added)}
	}
	for key, group := range byKey {
		above := make([]string, len(ancestors[key]))
		for i, k := range ancestors[key] {
			above[i] = name(k)
		}
		group[parentKey] = map[string]any{
			"members": sortedNames(members[key]), "directMembers": sortedNames(direct[key]), "ancestors": sortedNames(above),
		}
	}
	for _, resource := range []string{authentikUsers, authentikGroups} {
		if !slices.Contains(p.Resources, resource) {
			continue
		}
		objects := users
		if resource == authentikGroups {
			objects = groups
		}
		if err := emit(resource, authentikItems(resource, objects)); err != nil {
			return nil, err
		}
	}

	return nil, nil
}

// authentikAncestors are the keys of a group's parents, theirs, and so on.
// A group lists its parents, or in older authentik, its one parent.
func authentikAncestors(byKey map[string]map[string]any, key string) []string {
	var out []string
	seen := map[string]bool{key: true}
	queue := []string{key}
	for len(queue) > 0 {
		group := byKey[queue[0]]
		queue = queue[1:]
		parents, _ := group["parents"].([]any)
		if parent := group["parent"]; parent != nil {
			parents = append(parents, parent)
		}
		for _, parent := range parents {
			k := authentikKey(parent)
			if _, ok := byKey[k]; ok && !seen[k] {
				seen[k] = true
				out = append(out, k)
				queue = append(queue, k)
			}
		}
	}

	return out
}

// sortedNames is names sorted, each once, as JSON values.
func sortedNames(names []string) []any {
	slices.Sort(names)
	return toAny(slices.Compact(names))
}

// authentikKey is a primary key as text: users' are numbers, groups' UUIDs.
func authentikKey(pk any) string {
	if n, ok := pk.(float64); ok {
		return strconv.FormatFloat(n, 'f', -1, 64)
	}
	s, _ := pk.(string)

	return s
}

func toAny(names []string) []any {
	out := make([]any, len(names))
	for i, n := range names {
		out[i] = n
	}

	return out
}

func authentikItems(resource string, objects []map[string]any) []Item {
	items := make([]Item, len(objects))
	for i, o := range objects {
		items[i] = Item{Resource: resource, Object: o}
	}

	return items
}

// authentikList reads every page of a list.
func authentikList(ctx context.Context, client *apiClient, path string) ([]map[string]any, error) {
	var out []map[string]any
	for page := 1; ; page++ {
		if page > DefaultMaxPages {
			return nil, fmt.Errorf("authentik lists more than %d pages at %s", DefaultMaxPages, path)
		}
		var answer struct {
			Pagination struct {
				Next float64 `json:"next"`
			} `json:"pagination"`
			Results []map[string]any `json:"results"`
		}
		if _, err := client.get(ctx, "", path+"&page_size=100&page="+strconv.Itoa(page), &answer); err != nil {
			return nil, err
		}
		out = append(out, answer.Results...)
		if answer.Pagination.Next == 0 || len(answer.Results) == 0 {
			return out, nil
		}
	}
}
