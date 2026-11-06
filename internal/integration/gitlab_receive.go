package integration

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

// gitlabNoCommit is the commit a push names as a tag's new head when it
// deletes the tag.
const gitlabNoCommit = "0000000000000000000000000000000000000000"

type gitlabRef = eventRef[*gitlabClient]

// Receive turns a GitLab webhook event into items of the resources it's
// about, read again from GitLab's API as a sync reads them. Events about
// what the config or mappings don't read give no items rather than failing.
func (c *GitLabConfig) Receive(ctx context.Context, req Request, authenticated bool) ([]Item, error) {
	if !authenticated {
		if req.WebhookSecret == "" {
			return nil, ErrUnverified
		}
		token := req.Header.Get("X-Gitlab-Token")
		if token == "" {
			return nil, SignatureError{"send the webhook's secret token in the X-Gitlab-Token header, or an ingestion token as \"Authorization: Bearer <token>\""}
		}
		if subtle.ConstantTimeCompare([]byte(token), []byte(req.WebhookSecret)) != 1 {
			return nil, SignatureError{"the X-Gitlab-Token header isn't this integration's webhook secret"}
		}
	}
	event := req.Header.Get("X-Gitlab-Event")
	if event == "" {
		return nil, ReceiveError{"send GitLab's webhook events, which name themselves in the X-Gitlab-Event header"}
	}
	var payload map[string]any
	if err := json.Unmarshal(req.Body, &payload); err != nil || payload == nil {
		return nil, ErrNotJSON
	}
	named := objectOf(payload, "project")
	fullPath, _ := named["path_with_namespace"].(string)
	wanted := func(r gitlabRef) bool { return !slices.Contains(req.Resources, r.resource) }
	if idOf(named["id"]) == "" || !c.selects(fullPath) || len(slices.DeleteFunc(gitlabEventRefs(payload, named), wanted)) == 0 {
		return nil, nil
	}

	client, err := newGitLabClient(c)
	if err != nil {
		return nil, FetchError{err}
	}
	// GitLab waits ten seconds for an answer; a rate limit fails the event
	// rather than outlasting that.
	client.maxWait = 0
	// What an event says of its project is read again too, so one that
	// names another project than its ID, by mistake or forged, writes
	// nothing.
	project, err := gitlabFetch("projects/"+idOf(named["id"]))(ctx, client)
	now, _ := project["path_with_namespace"].(string)
	switch {
	case apiGone(err):
		return nil, nil
	case err != nil:
		return nil, FetchError{fmt.Errorf("GitLab didn't answer when asked again for the project of a webhook event, %s, so nothing was written, "+
			"and the next sync catches up: %w", event, err)}
	case !strings.EqualFold(now, fullPath), c.ExcludeArchived && project["archived"] == true:
		return nil, nil
	}
	refs := slices.DeleteFunc(gitlabEventRefs(payload, gitlabProjectShape(project)), wanted)
	cutoff := c.cutoff()

	return resolveRefs(ctx, client, "GitLab", event, refs, func(resource string, object map[string]any) bool {
		return gitlabCurrent(resource, object, cutoff)
	})
}

func gitlabFetch(path string) func(context.Context, *gitlabClient) (map[string]any, error) {
	return func(ctx context.Context, client *gitlabClient) (map[string]any, error) {
		var object map[string]any
		_, err := client.get(ctx, path, &object)
		return object, err
	}
}

// gitlabFetchListed reads one object from a list, such as one merge request
// by its iid, which gives it exactly as a sync does, as answers about one
// don't. One the list doesn't have is gone.
func gitlabFetchListed(path, query string) func(context.Context, *gitlabClient) (map[string]any, error) {
	return func(ctx context.Context, client *gitlabClient) (map[string]any, error) {
		var objects []map[string]any
		if _, err := client.get(ctx, path+"?"+query, &objects); err != nil {
			return nil, err
		}
		if len(objects) == 0 {
			return nil, &apiError{System: "GitLab", Path: path, Status: http.StatusNotFound}
		}
		return objects[0], nil
	}
}

// gitlabApprovalFields are what GitLab's paid tiers add to a deployment
// read on its own, which its list doesn't have.
var gitlabApprovalFields = []string{"approval_summary", "approvals", "pending_approval_count"}

// gitlabEventRefs are what an event is about, in project.
func gitlabEventRefs(payload, project map[string]any) []gitlabRef {
	fullPath, _ := project["path_with_namespace"].(string)
	base := "projects/" + idOf(project["id"]) + "/"
	parent := map[string]any{"project": fullPath}
	attributes := objectOf(payload, "object_attributes")
	ref := func(resource string, fetch func(context.Context, *gitlabClient) (map[string]any, error), gone map[string]any) gitlabRef {
		return gitlabRef{resource: resource, parent: parent, fetch: fetch, gone: gone}
	}

	switch payload["object_kind"] {
	case "push", "tag_push":
		name, _ := payload["ref"].(string)
		// A push to the default branch moves its head and maybe its CI
		// config, which the project's item carries.
		if branch, ok := strings.CutPrefix(name, "refs/heads/"); ok && branch == project["default_branch"] {
			return []gitlabRef{gitlabProjectRef(project)}
		}
		if tag, ok := strings.CutPrefix(name, "refs/tags/"); ok {
			var gone map[string]any
			if payload["after"] == gitlabNoCommit {
				gone = map[string]any{"name": tag}
			}
			return []gitlabRef{ref(gitlabTags, gitlabFetch(base+"repository/tags/"+url.PathEscape(tag)), gone)}
		}
	case "merge_request":
		if iid := idOf(attributes["iid"]); iid != "" {
			return []gitlabRef{ref(gitlabMergeRequests, gitlabFetchListed(base+"merge_requests", "iids[]="+iid), nil)}
		}
	case "issue", "work_item":
		// Work items other than issues, such as tasks, are issues to the
		// API, and those it doesn't list, such as epics, it doesn't find.
		if iid := idOf(attributes["iid"]); iid != "" {
			return []gitlabRef{ref(gitlabIssues, gitlabFetchListed(base+"issues", "iids[]="+iid), nil)}
		}
	case "pipeline":
		id := idOf(attributes["id"])
		if id == "" {
			return nil
		}
		refs := []gitlabRef{ref(gitlabPipelines, func(ctx context.Context, client *gitlabClient) (map[string]any, error) {
			pipeline, err := gitlabFetch(base+"pipelines/"+id)(ctx, client)
			return gitlabShape(pipeline, gitlabPipelineFields), err
		}, nil)}
		// A pipeline on the default branch may be the project's latest,
		// which its item carries.
		if attributes["ref"] == project["default_branch"] && attributes["tag"] != true {
			refs = append(refs, gitlabProjectRef(project))
		}
		return refs
	case "build":
		id, pipeline := idOf(payload["build_id"]), idOf(payload["pipeline_id"])
		if id == "" || pipeline == "" {
			return nil
		}
		return []gitlabRef{ref(gitlabJobs, func(ctx context.Context, client *gitlabClient) (map[string]any, error) {
			return gitlabLatestAttempt(ctx, client, base, pipeline, id)
		}, nil)}
	case "deployment":
		id := idOf(payload["deployment_id"])
		if id == "" {
			return nil
		}
		refs := []gitlabRef{ref(gitlabDeployments, func(ctx context.Context, client *gitlabClient) (map[string]any, error) {
			d, err := gitlabFetch(base+"deployments/"+id)(ctx, client)
			for _, field := range gitlabApprovalFields {
				delete(d, field)
			}
			return d, err
		}, nil)}
		// A deployment that finished may be what's deployed to its
		// environment, which the environment's item carries.
		if environment, _ := payload["environment"].(string); environment != "" {
			refs = append(refs, gitlabRef{resource: gitlabEnvironments, fetch: func(ctx context.Context, client *gitlabClient) (map[string]any, error) {
				e, err := gitlabFetchListed(base+"environments", url.Values{"name": {environment}}.Encode())(ctx, client)
				if err != nil {
					return nil, err
				}
				return e, gitlabWithLastDeployment(ctx, client, base, fullPath, e)
			}})
		}
		return refs
	case "release":
		tag, _ := payload["tag"].(string)
		if tag == "" {
			return nil
		}
		var gone map[string]any
		// GitLab's docs put the action at the top, and also under
		// object_attributes.
		if payload["action"] == "delete" || attributes["action"] == "delete" {
			gone = map[string]any{"tag_name": tag}
		}
		return []gitlabRef{ref(gitlabReleases, gitlabFetch(base+"releases/"+url.PathEscape(tag)), gone)}
	}

	return nil
}

// gitlabLatestAttempt is a job as a sync reads it: from its pipeline's jobs,
// which hold only each job's latest attempt, of a pipeline made since the
// cutoff. Its retried attempts, and jobs of older pipelines, are nil.
func gitlabLatestAttempt(ctx context.Context, client *gitlabClient, base, pipeline, id string) (map[string]any, error) {
	var job map[string]any
	err := client.list(ctx, base+"pipelines/"+pipeline+"/jobs", func(jobs []map[string]any) (bool, error) {
		for _, j := range jobs {
			if idOf(j["id"]) == id {
				job = j
				return false, nil
			}
		}
		return true, nil
	})
	if err != nil || job == nil {
		return nil, err
	}
	created := objectOf(job, "pipeline")
	if timeOf(created, "created_at").IsZero() {
		if _, err := client.get(ctx, base+"pipelines/"+pipeline, &created); err != nil {
			return nil, err
		}
	}
	if !gitlabCurrent(gitlabPipelines, created, client.config.cutoff()) {
		return nil, nil
	}

	return job, nil
}

// gitlabProjectRef is the project an event is in, read again with what its
// default branch has.
func gitlabProjectRef(project map[string]any) gitlabRef {
	return gitlabRef{resource: gitlabProjects, fetch: func(ctx context.Context, client *gitlabClient) (map[string]any, error) {
		p := maps.Clone(project)
		return p, gitlabWithDefaultBranch(ctx, client, newGitLabMergeGroups(), p)
	}}
}
