package scim

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/dvjn/goldenportal/internal/audit"
)

func (h handler) users(rest []string) {
	switch {
	case len(rest) == 0 && h.r.Method == http.MethodGet:
		h.listUsers()
	case len(rest) == 0 && h.r.Method == http.MethodPost:
		h.createUser()
	case len(rest) == 1 && !validID(rest[0]):
		writeError(h.w, http.StatusNotFound, "", "no such user")
	case len(rest) == 1 && h.r.Method == http.MethodGet:
		h.getUser(rest[0])
	case len(rest) == 1 && h.r.Method == http.MethodPut:
		h.replaceUser(rest[0])
	case len(rest) == 1 && h.r.Method == http.MethodPatch:
		h.patchUser(rest[0])
	case len(rest) == 1 && h.r.Method == http.MethodDelete:
		h.deleteUser(rest[0])
	default:
		writeError(h.w, http.StatusMethodNotAllowed, "", "that method isn't supported here")
	}
}

func (h handler) userResource(u user) userResource {
	active := u.Active
	res := userResource{
		Schemas:     []string{schemaUser},
		ID:          u.ID,
		ExternalID:  u.ExternalID,
		UserName:    u.UserName,
		Name:        &name{Formatted: u.DisplayName},
		DisplayName: u.DisplayName,
		Active:      &active,
		Groups:      u.Groups,
		Meta: &meta{
			ResourceType: "User", Created: u.Created, LastModified: u.Updated,
			Location: h.s.URL() + "/Users/" + u.ID,
		},
	}
	if u.Email != "" {
		res.Emails = []email{{Value: u.Email, Type: "work", Primary: true}}
	}

	return res
}

func (h handler) listUsers() {
	f, ok := parseFilter(h.r.URL.Query().Get("filter"), userFilterAttributes)
	if !ok {
		writeError(h.w, http.StatusBadRequest, "invalidFilter", `filter users with userName, externalId or emails.value, like userName eq "ada"`)
		return
	}
	if f != nil && f.attribute == "id" && !validID(f.value) {
		h.json(http.StatusOK, listOf(nil))
		return
	}

	start, count := h.page()
	users, total, err := h.st.listUsers(h.ctx(), f, start, count)
	if err != nil {
		h.fail(err, "user")
		return
	}

	resources := make([]any, 0, len(users))
	for _, u := range users {
		resources = append(resources, h.userResource(u))
	}
	h.json(http.StatusOK, listResponse{
		Schemas: []string{schemaListResponse}, TotalResults: total,
		StartIndex: start, ItemsPerPage: len(resources), Resources: resources,
	})
}

func (h handler) getUser(id string) {
	u, err := h.st.getUser(h.ctx(), id)
	if err != nil {
		h.fail(err, "user")
		return
	}
	h.json(http.StatusOK, h.userResource(u))
}

func (h handler) readUser() (userInput, bool) {
	var res userResource
	if !h.bind(&res) {
		return userInput{}, false
	}
	in := res.input()
	if in.UserName == "" {
		writeError(h.w, http.StatusBadRequest, "invalidValue", "userName is required")
		return userInput{}, false
	}

	return in, true
}

func (h handler) createUser() {
	in, ok := h.readUser()
	if !ok {
		return
	}

	id, err := h.st.createUser(h.ctx(), in)
	if err != nil {
		h.fail(err, "user")
		return
	}
	h.record(audit.SCIMUserProvision, audit.Target{Type: "user", ID: id, Label: in.Email}, nil)
	h.respondUser(http.StatusCreated, id)
}

func (h handler) replaceUser(id string) {
	in, ok := h.readUser()
	if !ok {
		return
	}
	h.saveUser(id, in)
}

// saveUser stores a user and records a change to whether they can sign in.
func (h handler) saveUser(id string, in userInput) {
	changed, err := h.st.replaceUser(h.ctx(), id, in)
	if err != nil {
		h.fail(err, "user")
		return
	}
	if changed {
		action := audit.SCIMUserReactivate
		if !in.Active {
			action = audit.SCIMUserDeactivate
		}
		h.record(action, audit.Target{Type: "user", ID: id, Label: in.Email}, nil)
	}
	h.respondUser(http.StatusOK, id)
}

func (h handler) patchUser(id string) {
	var req patchRequest
	if !h.bind(&req) {
		return
	}

	current, err := h.st.getUser(h.ctx(), id)
	if err != nil {
		h.fail(err, "user")
		return
	}

	// Apply the operations to the user as SCIM sees it, then save the result.
	doc := map[string]any{}
	raw, _ := json.Marshal(h.userResource(current))
	_ = json.Unmarshal(raw, &doc)
	for _, op := range req.Operations {
		if err := applyUserOperation(doc, op); err != nil {
			writeError(h.w, http.StatusBadRequest, "invalidPath", err.Error())
			return
		}
	}

	var res userResource
	raw, _ = json.Marshal(doc)
	if err := json.Unmarshal(raw, &res); err != nil {
		writeError(h.w, http.StatusBadRequest, "invalidValue", "the changes don't make a valid user")
		return
	}
	in := res.input()
	if in.UserName == "" {
		writeError(h.w, http.StatusBadRequest, "invalidValue", "userName is required")
		return
	}
	h.saveUser(id, in)
}

func (h handler) deleteUser(id string) {
	u, err := h.st.getUser(h.ctx(), id)
	if err != nil {
		h.fail(err, "user")
		return
	}
	if err := h.st.deactivateUser(h.ctx(), id); err != nil {
		h.fail(err, "user")
		return
	}
	h.record(audit.SCIMUserDeactivate, audit.Target{Type: "user", ID: id, Label: u.Email}, nil)
	h.w.WriteHeader(http.StatusNoContent)
}

func (h handler) respondUser(status int, id string) {
	u, err := h.st.getUser(h.ctx(), id)
	if err != nil {
		h.fail(err, "user")
		return
	}
	h.json(status, h.userResource(u))
}

// applyUserOperation applies one PATCH operation to a user document. It
// understands the paths identity providers send for users: plain attributes,
// name sub-attributes and emails, and operations with no path that carry an
// object of attributes.
func applyUserOperation(doc map[string]any, op patchOperation) error {
	kind := strings.ToLower(op.Op)
	path := strings.TrimSpace(op.Path)

	if path == "" {
		values, ok := op.Value.(map[string]any)
		if !ok {
			return errPatch("an operation without a path needs an object value")
		}
		for key, value := range values {
			if err := applyUserOperation(doc, patchOperation{Op: op.Op, Path: key, Value: value}); err != nil {
				return err
			}
		}
		return nil
	}

	lower := strings.ToLower(strings.TrimPrefix(path, schemaUser+":"))
	switch {
	case lower == "active":
		if kind == "remove" {
			return nil
		}
		doc["active"] = boolValue(op.Value)
	case lower == "username", lower == "displayname", lower == "externalid":
		key := map[string]string{"username": "userName", "displayname": "displayName", "externalid": "externalId"}[lower]
		if kind == "remove" {
			delete(doc, key)
		} else {
			doc[key] = op.Value
		}
	case lower == "name":
		doc["name"] = op.Value
	case strings.HasPrefix(lower, "name."):
		sub := map[string]string{"formatted": "formatted", "givenname": "givenName", "familyname": "familyName"}[strings.TrimPrefix(lower, "name.")]
		if sub == "" {
			return nil
		}
		nameDoc, _ := doc["name"].(map[string]any)
		if nameDoc == nil {
			nameDoc = map[string]any{}
		}
		nameDoc[sub] = op.Value
		// A changed name part replaces the display name built from the old one.
		delete(doc, "displayName")
		doc["name"] = nameDoc
	case lower == "emails":
		doc["emails"] = op.Value
	case strings.HasPrefix(lower, "emails["):
		// e.g. emails[type eq "work"].value: the portal keeps one email.
		if value, ok := op.Value.(string); ok {
			doc["emails"] = []any{map[string]any{"value": value, "primary": true}}
		}
	default:
		// Attributes the portal doesn't keep, such as phone numbers, are ignored.
	}

	return nil
}

// boolValue reads a boolean that some identity providers send as a string.
func boolValue(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(v, "true")
	default:
		return false
	}
}
