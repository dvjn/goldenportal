package scim

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/dvjn/goldenportal/internal/audit"
)

type patchRequest struct {
	Schemas    []string         `json:"schemas"`
	Operations []patchOperation `json:"Operations"`
}

type patchOperation struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value any    `json:"value"`
}

type patchError string

func (e patchError) Error() string { return string(e) }

func errPatch(message string) error { return patchError(message) }

func (h handler) groups(rest []string) {
	switch {
	case len(rest) == 0 && h.r.Method == http.MethodGet:
		h.listGroups()
	case len(rest) == 0 && h.r.Method == http.MethodPost:
		h.createGroup()
	case len(rest) == 1 && !validID(rest[0]):
		writeError(h.w, http.StatusNotFound, "", "no such group")
	case len(rest) == 1 && h.r.Method == http.MethodGet:
		h.getGroup(rest[0])
	case len(rest) == 1 && h.r.Method == http.MethodPut:
		h.replaceGroup(rest[0])
	case len(rest) == 1 && h.r.Method == http.MethodPatch:
		h.patchGroup(rest[0])
	case len(rest) == 1 && h.r.Method == http.MethodDelete:
		h.deleteGroup(rest[0])
	default:
		writeError(h.w, http.StatusMethodNotAllowed, "", "that method isn't supported here")
	}
}

func (h handler) groupResource(g group) groupResource {
	return groupResource{
		Schemas:     []string{schemaGroup},
		ID:          g.ID,
		ExternalID:  g.ExternalID,
		DisplayName: g.Name,
		Members:     g.Members,
		Meta: &meta{
			ResourceType: "Group", Created: g.Created, LastModified: g.Updated,
			Location: h.s.URL() + "/Groups/" + g.ID,
		},
	}
}

func (h handler) listGroups() {
	f, ok := parseFilter(h.r.URL.Query().Get("filter"), groupFilterAttributes)
	if !ok {
		writeError(h.w, http.StatusBadRequest, "invalidFilter", `filter groups with displayName or externalId, like displayName eq "platform"`)
		return
	}
	if f != nil && f.attribute == "id" && !validID(f.value) {
		h.json(http.StatusOK, listOf(nil))
		return
	}

	// Members can be long lists, so they come only when asked for.
	withMembers := !strings.Contains(strings.ToLower(h.r.URL.Query().Get("excludedAttributes")), "members")
	start, count := h.page()
	groups, total, err := h.st.listGroups(h.ctx(), f, start, count, withMembers)
	if err != nil {
		h.fail(err, "group")
		return
	}

	resources := make([]any, 0, len(groups))
	for _, g := range groups {
		resources = append(resources, h.groupResource(g))
	}
	h.json(http.StatusOK, listResponse{
		Schemas: []string{schemaListResponse}, TotalResults: total,
		StartIndex: start, ItemsPerPage: len(resources), Resources: resources,
	})
}

func (h handler) getGroup(id string) {
	g, err := h.st.getGroup(h.ctx(), id)
	if err != nil {
		h.fail(err, "group")
		return
	}
	h.json(http.StatusOK, h.groupResource(g))
}

func (h handler) readGroup() (groupResource, bool) {
	var res groupResource
	if !h.bind(&res) {
		return groupResource{}, false
	}
	res.DisplayName = strings.TrimSpace(res.DisplayName)
	if res.DisplayName == "" {
		writeError(h.w, http.StatusBadRequest, "invalidValue", "displayName is required")
		return groupResource{}, false
	}

	return res, true
}

func (h handler) createGroup() {
	res, ok := h.readGroup()
	if !ok {
		return
	}

	id, err := h.st.createGroup(h.ctx(), res.DisplayName, res.ExternalID)
	if err == nil {
		err = h.st.setMembers(h.ctx(), id, memberIDs(res.Members))
	}
	if err != nil {
		h.fail(err, "group")
		return
	}
	h.record(audit.SCIMGroupProvision, audit.Target{Type: "group", ID: id, Label: res.DisplayName}, nil)
	h.respondGroup(http.StatusCreated, id)
}

func (h handler) replaceGroup(id string) {
	res, ok := h.readGroup()
	if !ok {
		return
	}

	err := h.st.updateGroup(h.ctx(), id, res.DisplayName, res.ExternalID)
	if err == nil {
		err = h.st.setMembers(h.ctx(), id, memberIDs(res.Members))
	}
	if err != nil {
		h.fail(err, "group")
		return
	}
	h.respondGroup(http.StatusOK, id)
}

// memberFilter matches the member paths identity providers remove by:
// members[value eq "id"].
var memberFilter = regexp.MustCompile(`(?i)^members\[\s*value\s+eq\s+"([^"]+)"\s*\]$`)

func (h handler) patchGroup(id string) {
	var req patchRequest
	if !h.bind(&req) {
		return
	}

	current, err := h.st.getGroup(h.ctx(), id)
	if err != nil {
		h.fail(err, "group")
		return
	}

	for _, op := range req.Operations {
		if err := h.applyGroupOperation(current, op); err != nil {
			if _, ok := err.(patchError); ok {
				writeError(h.w, http.StatusBadRequest, "invalidPath", err.Error())
			} else {
				h.fail(err, "group")
			}
			return
		}
	}

	if err := h.st.touchGroup(h.ctx(), id); err != nil {
		h.fail(err, "group")
		return
	}
	h.respondGroup(http.StatusOK, id)
}

func (h handler) applyGroupOperation(g group, op patchOperation) error {
	kind := strings.ToLower(op.Op)
	path := strings.TrimSpace(op.Path)
	ctx := h.ctx()

	if match := memberFilter.FindStringSubmatch(path); match != nil {
		if kind != "remove" {
			return errPatch("only remove works on a single member")
		}
		return h.st.removeMembers(ctx, g.ID, []string{match[1]})
	}

	switch strings.ToLower(path) {
	case "members":
		ids := memberIDs(refsFrom(op.Value))
		switch kind {
		case "add":
			return h.st.addMembers(ctx, g.ID, ids)
		case "remove":
			if op.Value == nil {
				return h.st.setMembers(ctx, g.ID, nil)
			}
			return h.st.removeMembers(ctx, g.ID, ids)
		case "replace":
			return h.st.setMembers(ctx, g.ID, ids)
		}
	case "displayname":
		if value, ok := op.Value.(string); ok && strings.TrimSpace(value) != "" {
			return h.st.updateGroup(ctx, g.ID, strings.TrimSpace(value), g.ExternalID)
		}
		return errPatch("displayName needs a non-empty string")
	case "externalid":
		value, _ := op.Value.(string)
		return h.st.updateGroup(ctx, g.ID, g.Name, value)
	case "":
		values, ok := op.Value.(map[string]any)
		if !ok {
			return errPatch("an operation without a path needs an object value")
		}
		for key, value := range values {
			if err := h.applyGroupOperation(g, patchOperation{Op: op.Op, Path: key, Value: value}); err != nil {
				return err
			}
			if strings.EqualFold(key, "displayName") {
				g.Name, _ = value.(string)
			}
		}
		return nil
	}

	// Attributes the portal doesn't keep are ignored.
	return nil
}

func (h handler) deleteGroup(id string) {
	name, err := h.st.deleteGroup(h.ctx(), id)
	if err != nil {
		h.fail(err, "group")
		return
	}
	h.record(audit.SCIMGroupDelete, audit.Target{Type: "group", ID: id, Label: name}, nil)
	h.w.WriteHeader(http.StatusNoContent)
}

func (h handler) respondGroup(status int, id string) {
	g, err := h.st.getGroup(h.ctx(), id)
	if err != nil {
		h.fail(err, "group")
		return
	}
	h.json(status, h.groupResource(g))
}

// refsFrom reads member references from a PATCH value: a list of
// {"value": id} objects, or a single one.
func refsFrom(value any) []ref {
	var items []any
	switch v := value.(type) {
	case []any:
		items = v
	case map[string]any:
		items = []any{v}
	}

	refs := make([]ref, 0, len(items))
	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			if id, ok := m["value"].(string); ok {
				refs = append(refs, ref{Value: id})
			}
		}
	}

	return refs
}

// memberIDs keeps the member ids that could name a user.
func memberIDs(refs []ref) []string {
	ids := make([]string, 0, len(refs))
	for _, r := range refs {
		if validID(r.Value) {
			ids = append(ids, r.Value)
		}
	}

	return ids
}
