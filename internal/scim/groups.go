package scim

import (
	"context"
	"net/http"
	"strings"

	"github.com/dvjn/goldport/internal/directory"
)

type groupResource struct {
	Schemas     []string `json:"schemas"`
	ID          string   `json:"id,omitempty"`
	ExternalID  string   `json:"externalId,omitempty"`
	DisplayName string   `json:"displayName"`
	Members     []member `json:"members"`
	Meta        *meta    `json:"meta,omitempty"`
}

type member struct {
	Value   string `json:"value"`
	Display string `json:"display,omitempty"`
}

func (g groupResource) toGroup() (directory.Group, error) {
	if strings.TrimSpace(g.DisplayName) == "" {
		return directory.Group{}, badRequest("invalidValue", "displayName is required")
	}

	return directory.Group{ExternalID: g.ExternalID, DisplayName: g.DisplayName}, nil
}

func groupLocation(id string) string {
	return basePath + "/Groups/" + id
}

func memberIDs(members []member) []string {
	ids := make([]string, 0, len(members))
	for _, member := range members {
		ids = append(ids, member.Value)
	}

	return ids
}

func (s *Server) groupResource(ctx context.Context, group directory.Group) (groupResource, error) {
	members, err := s.directory.Members(ctx, group.ID)
	if err != nil {
		return groupResource{}, err
	}

	resource := groupResource{
		Schemas:     []string{schemaGroup},
		ID:          group.ID,
		ExternalID:  group.ExternalID,
		DisplayName: group.DisplayName,
		Members:     make([]member, 0, len(members)),
		Meta: &meta{
			ResourceType: "Group",
			Created:      group.CreatedAt,
			LastModified: group.UpdatedAt,
			Location:     groupLocation(group.ID),
		},
	}
	for _, m := range members {
		resource.Members = append(resource.Members, member{Value: m.UserID, Display: m.UserName})
	}

	return resource, nil
}

func (s *Server) writeGroup(w http.ResponseWriter, r *http.Request, status int, group directory.Group) error {
	resource, err := s.groupResource(r.Context(), group)
	if err != nil {
		return err
	}
	writeJSON(w, status, resource)

	return nil
}

func (s *Server) listGroups(w http.ResponseWriter, r *http.Request) error {
	filter, err := parseFilter(r.URL.Query().Get("filter"), groupFilters)
	if err != nil {
		return err
	}
	page := parsePage(r)
	groups, total, err := s.directory.ListGroups(r.Context(), filter, page)
	if err != nil {
		return err
	}

	resources := make([]groupResource, 0, len(groups))
	for _, group := range groups {
		resource, err := s.groupResource(r.Context(), group)
		if err != nil {
			return err
		}
		resources = append(resources, resource)
	}
	writeJSON(w, http.StatusOK, newListResponse(resources, total, page))

	return nil
}

// createGroup stores the group, then its members. Authentik sends a new
// group without members and adds them with PATCH.
func (s *Server) createGroup(w http.ResponseWriter, r *http.Request) error {
	var body groupResource
	if err := decode(r, &body); err != nil {
		return err
	}
	group, err := body.toGroup()
	if err != nil {
		return err
	}
	created, err := s.directory.CreateGroup(r.Context(), group)
	if err != nil {
		return err
	}
	if err := s.directory.SetMembers(r.Context(), created.ID, memberIDs(body.Members)); err != nil {
		return err
	}

	w.Header().Set("Location", groupLocation(created.ID))
	return s.writeGroup(w, r, http.StatusCreated, created)
}

func (s *Server) getGroup(w http.ResponseWriter, r *http.Request) error {
	group, err := s.directory.GroupByID(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}

	return s.writeGroup(w, r, http.StatusOK, group)
}

// replaceGroup replaces the members too: a PUT without members leaves the
// group empty.
func (s *Server) replaceGroup(w http.ResponseWriter, r *http.Request) error {
	var body groupResource
	if err := decode(r, &body); err != nil {
		return err
	}
	group, err := body.toGroup()
	if err != nil {
		return err
	}
	replaced, err := s.directory.ReplaceGroup(r.Context(), r.PathValue("id"), group)
	if err != nil {
		return err
	}
	if err := s.directory.SetMembers(r.Context(), replaced.ID, memberIDs(body.Members)); err != nil {
		return err
	}

	return s.writeGroup(w, r, http.StatusOK, replaced)
}

func (s *Server) patchGroup(w http.ResponseWriter, r *http.Request) error {
	operations, err := decodePatch(r)
	if err != nil {
		return err
	}
	group, err := s.directory.GroupByID(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	updates, err := s.patchGroupOperations(&group, operations)
	if err != nil {
		return err
	}

	patched, err := s.directory.ReplaceGroup(r.Context(), group.ID, group)
	if err != nil {
		return err
	}
	for _, update := range updates {
		if err := update.apply(r.Context(), group.ID, update.userIDs); err != nil {
			return err
		}
	}

	return s.writeGroup(w, r, http.StatusOK, patched)
}

func (s *Server) deleteGroup(w http.ResponseWriter, r *http.Request) error {
	if err := s.directory.DeleteGroup(r.Context(), r.PathValue("id")); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)

	return nil
}
