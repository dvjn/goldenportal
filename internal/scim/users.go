package scim

import (
	"cmp"
	"net/http"
	"strings"
	"time"

	"github.com/dvjn/goldport/internal/directory"
)

type meta struct {
	ResourceType string    `json:"resourceType"`
	Created      time.Time `json:"created,omitzero"`
	LastModified time.Time `json:"lastModified,omitzero"`
	Location     string    `json:"location"`
}

type listResponse[T any] struct {
	Schemas      []string `json:"schemas"`
	TotalResults int      `json:"totalResults"`
	ItemsPerPage int      `json:"itemsPerPage"`
	StartIndex   int      `json:"startIndex"`
	Resources    []T      `json:"Resources"`
}

func newListResponse[T any](resources []T, total int, page directory.Page) listResponse[T] {
	return listResponse[T]{
		Schemas:      []string{schemaListResponse},
		TotalResults: total,
		ItemsPerPage: len(resources),
		StartIndex:   page.Offset + 1,
		Resources:    resources,
	}
}

// userResource is both the request and the response body. A request without
// active creates an active user.
type userResource struct {
	Schemas     []string `json:"schemas"`
	ID          string   `json:"id,omitempty"`
	ExternalID  string   `json:"externalId,omitempty"`
	UserName    string   `json:"userName"`
	Name        userName `json:"name,omitzero"`
	DisplayName string   `json:"displayName,omitempty"`
	Emails      []email  `json:"emails,omitempty"`
	Active      *bool    `json:"active,omitempty"`
	Meta        *meta    `json:"meta,omitempty"`
}

type userName struct {
	Formatted string `json:"formatted,omitempty"`
}

type email struct {
	Value   string `json:"value"`
	Primary bool   `json:"primary,omitempty"`
}

func (u userResource) toUser() (directory.User, error) {
	if strings.TrimSpace(u.UserName) == "" {
		return directory.User{}, badRequest("invalidValue", "userName is required")
	}

	return directory.User{
		ExternalID: u.ExternalID,
		UserName:   u.UserName,
		Email:      u.primaryEmail(),
		Name:       cmp.Or(strings.TrimSpace(u.Name.Formatted), strings.TrimSpace(u.DisplayName)),
		Active:     u.Active == nil || *u.Active,
	}, nil
}

func (u userResource) primaryEmail() string {
	for _, address := range u.Emails {
		if address.Primary {
			return address.Value
		}
	}
	if len(u.Emails) == 0 {
		return ""
	}

	return u.Emails[0].Value
}

func newUserResource(user directory.User) userResource {
	var emails []email
	if user.Email != "" {
		emails = []email{{Value: user.Email, Primary: true}}
	}

	return userResource{
		Schemas:     []string{schemaUser},
		ID:          user.ID,
		ExternalID:  user.ExternalID,
		UserName:    user.UserName,
		Name:        userName{Formatted: user.Name},
		DisplayName: user.Name,
		Emails:      emails,
		Active:      &user.Active,
		Meta: &meta{
			ResourceType: "User",
			Created:      user.CreatedAt,
			LastModified: user.UpdatedAt,
			Location:     basePath + "/Users/" + user.ID,
		},
	}
}

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) error {
	filter, err := parseFilter(r.URL.Query().Get("filter"), userFilters)
	if err != nil {
		return err
	}
	page := parsePage(r)
	users, total, err := s.directory.ListUsers(r.Context(), filter, page)
	if err != nil {
		return err
	}

	resources := make([]userResource, 0, len(users))
	for _, user := range users {
		resources = append(resources, newUserResource(user))
	}
	writeJSON(w, http.StatusOK, newListResponse(resources, total, page))

	return nil
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) error {
	var body userResource
	if err := decode(r, &body); err != nil {
		return err
	}
	user, err := body.toUser()
	if err != nil {
		return err
	}
	created, err := s.directory.CreateUser(r.Context(), user)
	if err != nil {
		return err
	}

	resource := newUserResource(created)
	w.Header().Set("Location", resource.Meta.Location)
	writeJSON(w, http.StatusCreated, resource)

	return nil
}

func (s *Server) getUser(w http.ResponseWriter, r *http.Request) error {
	user, err := s.directory.UserByID(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, newUserResource(user))

	return nil
}

func (s *Server) replaceUser(w http.ResponseWriter, r *http.Request) error {
	var body userResource
	if err := decode(r, &body); err != nil {
		return err
	}
	user, err := body.toUser()
	if err != nil {
		return err
	}

	return s.saveUser(w, r, user)
}

func (s *Server) patchUser(w http.ResponseWriter, r *http.Request) error {
	operations, err := decodePatch(r)
	if err != nil {
		return err
	}
	user, err := s.directory.UserByID(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	if err := patchUser(&user, operations); err != nil {
		return err
	}

	return s.saveUser(w, r, user)
}

func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request) error {
	if err := s.directory.DeleteUser(r.Context(), r.PathValue("id")); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)

	return nil
}

// saveUser replaces the user and ends their sessions when the user is no
// longer active. Deleting a user ends them through the foreign key.
func (s *Server) saveUser(w http.ResponseWriter, r *http.Request, user directory.User) error {
	saved, err := s.directory.ReplaceUser(r.Context(), r.PathValue("id"), user)
	if err != nil {
		return err
	}
	if !saved.Active {
		if err := s.sessions.DeleteForUser(r.Context(), saved.ID); err != nil {
			return err
		}
	}
	writeJSON(w, http.StatusOK, newUserResource(saved))

	return nil
}
