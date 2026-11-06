package integration

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/dvjn/goldenportal/internal/configdoc"
)

// Check signs in, reaches each group and project the config lists, reads
// one page of each resource asked for from the first of them, and checks
// it can set up the webhooks, saying in one go what access is missing. What
// stops only the webhooks it warns of, since syncs still read everything
// without them.
func (c *GitLabConfig) Check(ctx context.Context, _ configdoc.DB, request CheckRequest) error {
	client, err := newGitLabClient(c)
	if err != nil {
		return err
	}
	check := &gitlabCheck{config: c, client: client}
	if err := check.signIn(ctx); err != nil {
		return err
	}
	if err := check.reach(ctx); err != nil {
		return errors.New(sentence(err.Error()))
	}

	var group, project []string
	for _, resource := range request.Resources {
		probe := gitlabResourceNamed(resource).probe
		fromGroups := slices.Contains(gitlabGroupResources, resource)
		on := check.project
		if fromGroups {
			on = check.group
		}
		if probe == "" || on == nil || !fromGroups && !gitlabEnabled(on, resource) {
			continue
		}
		kind := "projects/"
		if fromGroups {
			kind = "groups/"
		}
		var page any
		_, err := client.get(ctx, kind+idOf(on["id"])+"/"+probe+"?per_page=1", &page)
		switch {
		case gitlabRefused(err) && fromGroups:
			group = append(group, resource)
		case gitlabRefused(err):
			project = append(project, resource)
		case err != nil:
			return err
		}
	}

	var problems []string
	if len(group) > 0 {
		problems = append(problems, fmt.Sprintf("The token can't read %s of the group %s; give its user at least the Reporter role there.",
			sentenceList(group), check.group["full_path"]))
	}
	if len(project) > 0 {
		problems = append(problems, fmt.Sprintf("The token can't read %s of %s; give its user at least the Reporter role there, or turn those features on for it.",
			sentenceList(project), check.project["path_with_namespace"]))
	}
	warnings, err := check.webhookWarnings(ctx, request)
	if err != nil {
		return err
	}
	for _, warning := range warnings {
		request.Warn(warning)
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, " "))
	}

	return nil
}

type gitlabCheck struct {
	config *GitLabConfig
	client *gitlabClient
	viewer gitlabViewer
	// group and project are those resources are read from: the first group
	// listed, and the first project listed, or else the first group's.
	group, project map[string]any
	// hooked are the projects whose webhooks are checked: those listed, and
	// the first of each group.
	hooked []map[string]any
}

func (c *gitlabCheck) signIn(ctx context.Context) error {
	var err error
	c.viewer, err = gitlabSignIn(ctx, c.client)
	switch {
	case apiStatus(err) == http.StatusUnauthorized:
		return fmt.Errorf("GitLab doesn't accept the token in %s; it may have expired or been revoked", c.config.Token.Env)
	case apiStatus(err) == http.StatusForbidden:
		return fmt.Errorf("the token in %s can't read GitLab's API; give it the read_api scope, or api to have webhooks too", c.config.Token.Env)
	case err != nil:
		return err
	case c.viewer.scopes != nil && !slices.Contains(c.viewer.scopes, "api") && !slices.Contains(c.viewer.scopes, "read_api"):
		return fmt.Errorf("the token in %s has the %s scopes, which can't read GitLab's API; give it the read_api scope, or api to have webhooks too",
			c.config.Token.Env, sentenceList(c.viewer.scopes))
	}

	return nil
}

// reach checks each listed group and project is there, where it's listed,
// and seen whole.
func (c *gitlabCheck) reach(ctx context.Context) error {
	find := func(kind, path string) (map[string]any, error) {
		object, moved, err := gitlabFind(ctx, c.client, kind, path)
		switch {
		case err != nil:
			return nil, err
		case moved != "":
			return nil, fmt.Errorf("GitLab has moved the %s %s to %s; list it under its new path", kind, path, moved)
		}
		return object, gitlabWholeView(ctx, c.client, &c.viewer, kind, object)
	}
	for _, path := range c.config.Projects {
		project, err := find("project", path)
		if err != nil {
			return err
		}
		c.hooked = append(c.hooked, project)
	}
	for _, path := range c.config.Groups {
		group, err := find("group", path)
		if err != nil {
			return err
		}
		if c.group == nil {
			c.group = group
		}
		var first []map[string]any
		query := url.Values{"include_subgroups": {"true"}, "with_shared": {"false"}, "per_page": {"1"}}
		if c.config.ExcludeArchived {
			query.Set("archived", "false")
		}
		if _, err := c.client.get(ctx, "groups/"+idOf(group["id"])+"/projects?"+query.Encode(), &first); err != nil {
			return err
		}
		c.hooked = append(c.hooked, first...)
	}
	if len(c.hooked) > 0 {
		c.project = c.hooked[0]
	}

	return nil
}

// webhookWarnings say where webhooks are unavailable, and why: the server
// has no key to sign them with, the portal doesn't know its public URL, the
// token can't manage them, or GitLab turned one off. Syncs read everything
// either way, so these are warnings. A webhook not made yet isn't one,
// since the next sync makes it.
func (c *gitlabCheck) webhookWarnings(ctx context.Context, request CheckRequest) ([]string, error) {
	scopes := c.viewer.scopes
	switch {
	case gitlabHookFlags(request.Resources) == nil || len(c.hooked) == 0:
		return nil, nil
	case request.WebhookSecret == "":
		return []string{WebhookKeyEnv + " isn't set on the server, so GitLab's webhooks are unavailable, and syncs alone keep the catalog current."}, nil
	case request.IngestURL == "":
		return []string{"The portal doesn't know its public URL, as GP_BASE_URL gives it, so GitLab's webhooks are unavailable, and syncs alone keep the catalog current."}, nil
	case scopes != nil && !slices.Contains(scopes, "api"):
		return []string{"The token has no api scope, so it can't manage webhooks, and syncs alone keep the catalog current; to have GitLab's webhooks, give it the api scope."}, nil
	}
	var warnings []string
	for _, project := range c.hooked {
		fullPath, _ := project["path_with_namespace"].(string)
		found, err := findGitLabHook(ctx, c.client, "projects/"+idOf(project["id"])+"/hooks", request.IngestURL)
		switch {
		case gitlabRefused(err):
			warnings = append(warnings, "The token can't manage the webhooks of "+fullPath+", so webhooks are unavailable there, and syncs alone keep it current; "+
				"to have them, give its user at least the Maintainer role there.")
		case err != nil:
			return nil, err
		case found != nil && gitlabHookDisabled(found):
			warnings = append(warnings, "GitLab turned off the webhook on "+fullPath+" after its deliveries kept failing, so its events don't reach the portal "+
				"until the next sync makes it anew; its recent events in GitLab say why it failed.")
		case found == nil && scopes == nil:
			warnings = append(warnings, "There's no webhook on "+fullPath+" yet. The next sync makes one if the token has the api scope; "+
				"with only read_api, webhooks are unavailable there, and syncs alone keep it current.")
		}
	}

	return warnings, nil
}
