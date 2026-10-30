package integration

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/dvjn/goldenportal/internal/configdoc"
)

func githubPull(ctx context.Context, _ configdoc.DB, spec Spec, resources []string) (map[string]any, error) {
	c, err := newSCMClient(spec.scm, "github")
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
	if slices.Contains(resources, "organizations") {
		for _, org := range spec.scm.Organizations {
			obj, err := c.object(ctx, "orgs/"+url.PathEscape(org))
			if err != nil {
				return nil, err
			}
			add("organizations", []any{obj})
		}
	}
	if len(resources) == 1 && resources[0] == "organizations" {
		return out, nil
	}
	repos := map[string]map[string]any{}
	order := []string{}
	remember := func(obj map[string]any) error {
		name, _ := obj["full_name"].(string)
		if strings.Count(name, "/") != 1 {
			return fmt.Errorf("github repository response has no owner/repository name")
		}
		if _, ok := repos[name]; !ok {
			order = append(order, name)
		}
		repos[name] = obj
		return nil
	}
	for _, org := range spec.scm.Organizations {
		items, err := c.list(ctx, "orgs/"+url.PathEscape(org)+"/repos?type=all", "", 0)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			if err := remember(item.(map[string]any)); err != nil {
				return nil, err
			}
		}
	}
	for _, name := range spec.scm.Repositories {
		parts := strings.Split(name, "/")
		if len(parts) != 2 {
			return nil, fmt.Errorf("github repository must be owner/repository")
		}
		obj, err := c.object(ctx, "repos/"+url.PathEscape(parts[0])+"/"+url.PathEscape(parts[1]))
		if err != nil {
			return nil, err
		}
		if err := remember(obj); err != nil {
			return nil, err
		}
	}
	for _, name := range order {
		repo := repos[name]
		add("repositories", []any{repo})
		parts := strings.Split(name, "/")
		base := "repos/" + url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1]) + "/"
		parent := map[string]any{"repository": name, "repository_id": repo["id"]}
		for _, r := range []struct{ name, path, key string }{{"branches", "branches", ""}, {"workflows", "actions/workflows", "workflows"}, {"pull_requests", "pulls?state=all", ""}} {
			if !slices.Contains(resources, r.name) {
				continue
			}
			items, err := c.list(ctx, base+r.path, r.key, 0)
			if err != nil {
				return nil, fmt.Errorf("github %s: %w", r.name, err)
			}
			add(r.name, scmContext(items, parent))
		}
		if !slices.Contains(resources, "workflow_runs") && !slices.Contains(resources, "jobs") {
			continue
		}
		runs, err := c.list(ctx, base+"actions/runs", "workflow_runs", spec.scm.MaxRuns)
		if err != nil {
			return nil, err
		}
		add("workflow_runs", scmContext(runs, parent))
		if slices.Contains(resources, "jobs") {
			for _, run := range runs {
				id := sourceID(run.(map[string]any)["id"])
				if id == "" {
					return nil, fmt.Errorf("github workflow run has no id")
				}
				jobs, err := c.list(ctx, base+"actions/runs/"+url.PathEscape(id)+"/jobs?filter=all", "jobs", 0)
				if err != nil {
					return nil, err
				}
				add("jobs", scmContext(jobs, map[string]any{"repository": name, "repository_id": repo["id"], "run_id": run.(map[string]any)["id"]}))
			}
		}
	}
	return out, nil
}
