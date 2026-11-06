package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"

	"github.com/dvjn/goldenportal/internal/httpx"
)

// gitlabClient calls GitLab's API with the config's token, which reads
// everything the connector reads, so requests have no scope.
type gitlabClient struct {
	*apiClient
	config *GitLabConfig
}

func newGitLabClient(config *GitLabConfig) (*gitlabClient, error) {
	api, err := newAPIClient("GitLab", config.apiURL())
	if err != nil {
		return nil, err
	}
	token := config.Token.Resolve()
	if token == "" {
		return nil, fmt.Errorf("%s isn't set on the server, so the connector can't sign in to GitLab", config.Token.Env)
	}
	sum := sha256.Sum256([]byte(token))
	cred := credential{header: "PRIVATE-TOKEN", value: token, bucket: config.URL + " " + hex.EncodeToString(sum[:8])}
	api.headers = map[string]string{"Accept": "application/json", "User-Agent": "golden-portal"}
	api.remaining, api.reset = "RateLimit-Remaining", "RateLimit-Reset"
	api.authorize = func(context.Context, string) (credential, error) { return cred, nil }
	api.limited = gitlabLimited
	api.message = gitlabMessage

	return &gitlabClient{apiClient: api, config: config}, nil
}

func (c *gitlabClient) get(ctx context.Context, path string, out any) (http.Header, error) {
	return c.apiClient.get(ctx, "", path, out)
}

func (c *gitlabClient) write(ctx context.Context, method, path string, in, out any) error {
	return c.apiClient.write(ctx, method, "", path, in, out)
}

// graphql runs a query on GitLab's GraphQL API, beside the REST one under
// the same instance, for what REST can't ask, such as a project's open
// vulnerabilities by severity. A GraphQL error fails the call; a field the
// token can't see comes back null.
func (c *gitlabClient) graphql(ctx context.Context, query string, variables map[string]any, out any) error {
	payload, err := json.Marshal(map[string]any{"query": query, "variables": variables})
	if err != nil {
		return err
	}
	body, _, err := c.request(ctx, http.MethodPost, "", c.config.URL+"/api/graphql", payload)
	if err != nil {
		return err
	}
	var answer struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &answer); err != nil {
		return fmt.Errorf("GitLab's GraphQL API answered with something that isn't JSON: %w", err)
	}
	if len(answer.Errors) > 0 {
		return fmt.Errorf("GitLab's GraphQL API refused the query: %s", answer.Errors[0].Message)
	}

	return json.Unmarshal(answer.Data, out)
}

// delete deletes what's at path, which is done if it's gone already.
func (c *gitlabClient) delete(ctx context.Context, path string) error {
	_, _, err := c.request(ctx, http.MethodDelete, "", c.address(path), nil)
	return skipGone(err)
}

func (c *gitlabClient) list(ctx context.Context, path string, page func(objects []map[string]any) (bool, error)) error {
	return c.apiClient.list(ctx, "", path, "", page)
}

// gitlabLimited reads GitLab's 429s. Its HTTP layer's throttles, named
// throttle_ in RateLimit-Name, count against the token; an endpoint's own
// limit, such as on making webhooks, holds back only the request.
func gitlabLimited(status int, header http.Header, _ []byte, attempt int) *rateLimit {
	if status != http.StatusTooManyRequests {
		return nil
	}
	wait := httpx.RetryAfter(header.Get("Retry-After"))
	if wait == 0 {
		wait = retryBackoff << attempt
	}

	return &rateLimit{wait: wait, shared: strings.HasPrefix(header.Get("RateLimit-Name"), "throttle_")}
}

// gitlabMessage reads why GitLab refused, which it says as a message, a
// message of fields and their problems, or an OAuth-style error.
func gitlabMessage(body []byte) string {
	var answer struct {
		Message          any    `json:"message"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if json.Unmarshal(body, &answer) != nil {
		return strings.TrimSpace(string(body[:min(len(body), 200)]))
	}
	switch message := answer.Message.(type) {
	case string:
		return message
	case map[string]any:
		var parts []string
		for _, field := range slices.Sorted(maps.Keys(message)) {
			parts = append(parts, fmt.Sprintf("%s %v", field, message[field]))
		}
		return strings.Join(parts, "; ")
	}
	if answer.ErrorDescription != "" {
		return answer.ErrorDescription
	}

	return answer.Error
}

// skipGone reads an answer that something just listed is gone, deleted
// since, as nothing to read.
func skipGone(err error) error {
	if apiGone(err) {
		return nil
	}

	return err
}
