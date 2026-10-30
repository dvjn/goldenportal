package integration

import (
	"context"
	"fmt"
	"net/url"
	"slices"

	"github.com/dvjn/goldenportal/internal/configdoc"
)

func gitlabPull(ctx context.Context, _ configdoc.DB, spec Spec, resources []string) (map[string]any, error) {
	c, err := newSCMClient(spec.scm, "gitlab")
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	for _, resource := range resources {
		out[resource] = []any{}
	}
	add := func(name string, items []any) {
		if slices.Contains(resources, name) {
			out[name] = append(out[name].([]any), items...)
		}
	}
	if slices.Contains(resources, "groups") {
		for _, group := range spec.scm.Groups {
			obj, err := c.object(ctx, "groups/"+url.PathEscape(group))
			if err != nil {
				return nil, err
			}
			add("groups", []any{obj})
		}
	}
	if len(resources) == 1 && resources[0] == "groups" {
		return out, nil
	}
	projects := map[string]map[string]any{}
	order := []string{}
	remember := func(obj map[string]any) error {
		id := sourceID(obj["id"])
		if id == "" {
			return fmt.Errorf("gitlab project response has no id")
		}
		if _, ok := projects[id]; !ok {
			order = append(order, id)
		}
		projects[id] = obj
		return nil
	}
	for _, group := range spec.scm.Groups {
		items, err := c.list(ctx, "groups/"+url.PathEscape(group)+"/projects?include_subgroups=true&with_shared=false", "", 0)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			if err := remember(item.(map[string]any)); err != nil {
				return nil, err
			}
		}
	}
	for _, project := range spec.scm.Projects {
		obj, err := c.object(ctx, "projects/"+url.PathEscape(project))
		if err != nil {
			return nil, err
		}
		if err := remember(obj); err != nil {
			return nil, err
		}
	}
	for _, id := range order {
		project := projects[id]
		add("projects", []any{project})
		base := "projects/" + url.PathEscape(id) + "/"
		parent := map[string]any{"project_id": project["id"], "project": project["path_with_namespace"]}
		for _, r := range []struct{ name, path, feature string }{{"branches", "repository/branches", "repository"}, {"merge_requests", "merge_requests?scope=all&state=all", "merge_requests"}} {
			if !slices.Contains(resources, r.name) || !gitlabEnabled(project, r.feature) {
				continue
			}
			items, err := c.list(ctx, base+r.path, "", 0)
			if err != nil {
				return nil, fmt.Errorf("gitlab %s: %w", r.name, err)
			}
			add(r.name, scmContext(items, parent))
		}
		if (!slices.Contains(resources, "pipelines") && !slices.Contains(resources, "jobs")) || !gitlabEnabled(project, "builds") {
			continue
		}
		pipelines, err := c.list(ctx, base+"pipelines?order_by=id&sort=desc", "", spec.scm.MaxRuns)
		if err != nil {
			return nil, err
		}
		for _, raw := range pipelines {
			pid := sourceID(raw.(map[string]any)["id"])
			if pid == "" {
				return nil, fmt.Errorf("gitlab pipeline response has no id")
			}
			if slices.Contains(resources, "pipelines") {
				full, err := c.object(ctx, base+"pipelines/"+url.PathEscape(pid))
				if err != nil {
					return nil, err
				}
				add("pipelines", scmContext([]any{full}, parent))
			}
			if slices.Contains(resources, "jobs") {
				jobs, err := c.list(ctx, base+"pipelines/"+url.PathEscape(pid)+"/jobs?include_retried=true", "", 0)
				if err != nil {
					return nil, err
				}
				add("jobs", scmContext(jobs, map[string]any{"project_id": project["id"], "project": project["path_with_namespace"], "pipeline_id": raw.(map[string]any)["id"]}))
			}
		}
	}
	return out, nil
}

// gitlabEnabled reports whether a project has a feature turned on. GitLab
// answers 403 for a disabled feature's lists, which would fail the whole sync
// for one project's settings. Projects that don't say are assumed enabled.
func gitlabEnabled(project map[string]any, feature string) bool {
	level, ok := project[feature+"_access_level"].(string)
	return !ok || level != "disabled"
}
