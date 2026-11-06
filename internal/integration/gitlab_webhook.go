package integration

import (
	"context"
	"errors"
	"maps"
	"math/rand/v2"
	"net/http"
	"slices"
	"strings"
	"sync"
)

// gitlabHookFlags says, of every event setting the connector manages,
// whether resources need it; nil when none do. Push events are on unless
// a webhook says otherwise, so each is set either way.
func gitlabHookFlags(resources []string) map[string]bool {
	flags := map[string]bool{}
	wanted := false
	for _, r := range gitlabTable {
		for _, event := range r.events {
			flags[event] = flags[event] || slices.Contains(resources, r.name)
			wanted = wanted || flags[event]
		}
	}
	if !wanted {
		return nil
	}

	return flags
}

// gitlabRefused reports whether GitLab refused for want of access, which
// the check reports, rather than for a moment.
func gitlabRefused(err error) bool {
	status := apiStatus(err)
	return status == http.StatusUnauthorized || status == http.StatusForbidden || status == http.StatusNotFound
}

// gitlabHookDisabled reports whether GitLab turned a webhook off for good
// after its deliveries kept failing. One turned off for a while turns on by
// itself.
func gitlabHookDisabled(hook map[string]any) bool {
	return hook["alert_status"] == "disabled"
}

// ensureGitLabWebhooks points a webhook of each project at ingestURL. Each
// project has its own, since a group's webhooks are for GitLab's paid
// tiers. It goes through the projects in a random order, so those it doesn't
// reach in its time a later sync does. What lasts, such as missing access,
// the check reports, so the warnings are of what may pass, and of what only
// setting one up shows.
func ensureGitLabWebhooks(ctx context.Context, client *gitlabClient, projects []map[string]any, ingestURL, secret string,
	resources []string, warn func(string),
) {
	flags := gitlabHookFlags(resources)
	if ingestURL == "" || secret == "" || flags == nil {
		return
	}
	var (
		mu       sync.Mutex
		failed   []string
		refusals = map[string][]string{}
	)
	projects = slices.Clone(projects)
	rand.Shuffle(len(projects), func(i, j int) { projects[i], projects[j] = projects[j], projects[i] })
	_ = forEach(projects, apiConcurrency, func(project map[string]any) error {
		fullPath, _ := project["path_with_namespace"].(string)
		err := ensureGitLabWebhook(ctx, client, idOf(project["id"]), ingestURL, secret, flags)
		mu.Lock()
		defer mu.Unlock()
		var aerr *apiError
		switch status := apiStatus(err); {
		case err == nil, gitlabRefused(err):
		case (status == http.StatusBadRequest || status == http.StatusUnprocessableEntity) && errors.As(err, &aerr):
			refusals[aerr.Message] = append(refusals[aerr.Message], fullPath)
		default:
			failed = append(failed, fullPath)
		}
		return nil
	})
	for _, message := range slices.Sorted(maps.Keys(refusals)) {
		advice := ""
		if strings.Contains(strings.ToLower(message), "local network") {
			advice = " An administrator can allow webhooks to the local network in GitLab's Admin area, under Settings, Network, Outbound requests."
		}
		warn("GitLab refused the webhook on " + sentenceList(slices.Sorted(slices.Values(refusals[message]))) + ": " +
			strings.TrimSuffix(message, ".") + "." + advice + " Syncs alone keep what's there current.")
	}
	if len(failed) > 0 {
		warn("The webhook on " + sentenceList(slices.Sorted(slices.Values(failed))) +
			" couldn't be set up this time, so some events may not arrive until a later sync sets it up.")
	}
}

// ensureGitLabWebhook sets up one project's webhook. One GitLab turned off
// for good after its deliveries kept failing, as they do once the server's
// key changes, is made anew, since only a test delivery would turn it back
// on, and that waits on the portal reading GitLab with the same token.
func ensureGitLabWebhook(ctx context.Context, client *gitlabClient, projectID, ingestURL, secret string, flags map[string]bool) error {
	path := "projects/" + projectID + "/hooks"
	found, err := findGitLabHook(ctx, client, path, ingestURL)
	if err != nil {
		return err
	}
	want := hookURL(ingestURL, secret)
	body := map[string]any{"url": want, "token": secret, "enable_ssl_verification": true}
	for flag, on := range flags {
		body[flag] = on
	}
	// Someone who turned off certificate checks, for a portal behind a
	// certificate GitLab doesn't trust, meant to.
	if verify, ok := found["enable_ssl_verification"].(bool); ok {
		body["enable_ssl_verification"] = verify
	}
	switch {
	case found == nil:
	case gitlabHookDisabled(found):
		if err := client.delete(ctx, path+"/"+idOf(found["id"])); err != nil {
			return err
		}
	case found["url"] != want || !gitlabSameFlags(found, flags):
		return client.write(ctx, http.MethodPut, path+"/"+idOf(found["id"]), body, &map[string]any{})
	default:
		return nil
	}

	return client.write(ctx, http.MethodPost, path, body, &map[string]any{})
}

// findGitLabHook is the webhook listed at path pointing at ingestURL, or
// nil.
func findGitLabHook(ctx context.Context, client *gitlabClient, path, ingestURL string) (map[string]any, error) {
	var found map[string]any
	err := client.list(ctx, path, func(hooks []map[string]any) (bool, error) {
		for _, h := range hooks {
			if address, _ := h["url"].(string); ingestPart(address) == ingestURL {
				found = h
				return false, nil
			}
		}
		return true, nil
	})

	return found, err
}

// gitlabSameFlags reports whether a webhook sends what flags say. An older
// GitLab doesn't know of some, and shows no value for them.
func gitlabSameFlags(hook map[string]any, flags map[string]bool) bool {
	for flag, on := range flags {
		if value, ok := hook[flag]; ok && value != nil && value != on {
			return false
		}
	}

	return true
}
