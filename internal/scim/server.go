// Package scim is the SCIM 2.0 service provider (RFC 7643 and RFC 7644) that
// the identity provider pushes users and groups to.
package scim

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/dvjn/goldport/internal/directory"
	"github.com/dvjn/goldport/internal/token"
	gofrHTTP "gofr.dev/pkg/gofr/http"
	"gofr.dev/pkg/gofr/logging"
)

const (
	basePath     = "/scim/v2"
	maxBodyBytes = 1 << 20
)

type directoryStore interface {
	UserByID(ctx context.Context, id string) (directory.User, error)
	ListUsers(ctx context.Context, filter directory.UserFilter, page directory.Page) ([]directory.User, int, error)
	CreateUser(ctx context.Context, user directory.User) (directory.User, error)
	ReplaceUser(ctx context.Context, id string, user directory.User) (directory.User, error)
	DeleteUser(ctx context.Context, id string) error

	GroupByID(ctx context.Context, id string) (directory.Group, error)
	ListGroups(ctx context.Context, filter directory.GroupFilter, page directory.Page) ([]directory.Group, int, error)
	CreateGroup(ctx context.Context, group directory.Group) (directory.Group, error)
	ReplaceGroup(ctx context.Context, id string, group directory.Group) (directory.Group, error)
	DeleteGroup(ctx context.Context, id string) error

	Members(ctx context.Context, groupID string) ([]directory.Member, error)
	SetMembers(ctx context.Context, groupID string, userIDs []string) error
	AddMembers(ctx context.Context, groupID string, userIDs []string) error
	RemoveMembers(ctx context.Context, groupID string, userIDs []string) error
}

type sessionRevoker interface {
	DeleteForUser(ctx context.Context, userID string) error
}

type tokenVerifier interface {
	Verify(ctx context.Context, kind, token string) (bool, error)
}

type Server struct {
	directory directoryStore
	sessions  sessionRevoker
	tokens    tokenVerifier
	audit     auditLog
	logger    logging.Logger
	mux       *http.ServeMux
}

func NewServer(store directoryStore, sessions sessionRevoker, tokens tokenVerifier, log auditLog, logger logging.Logger) *Server {
	s := &Server{directory: store, sessions: sessions, tokens: tokens, audit: log, logger: logger, mux: http.NewServeMux()}

	s.handle("GET /ServiceProviderConfig", s.serviceProviderConfig)
	s.handle("GET /ResourceTypes", s.resourceTypes)
	s.handle("GET /Schemas", s.schemas)

	s.handle("GET /Users", s.listUsers)
	s.handle("POST /Users", s.createUser)
	s.handle("GET /Users/{id}", s.getUser)
	s.handle("PUT /Users/{id}", s.replaceUser)
	s.handle("PATCH /Users/{id}", s.patchUser)
	s.handle("DELETE /Users/{id}", s.deleteUser)

	s.handle("GET /Groups", s.listGroups)
	s.handle("POST /Groups", s.createGroup)
	s.handle("GET /Groups/{id}", s.getGroup)
	s.handle("PUT /Groups/{id}", s.replaceGroup)
	s.handle("PATCH /Groups/{id}", s.patchGroup)
	s.handle("DELETE /Groups/{id}", s.deleteGroup)

	s.handle("/", func(http.ResponseWriter, *http.Request) error {
		return notFound("no such SCIM endpoint")
	})

	return s
}

// Middleware answers every request under /scim/v2/ itself, because SCIM
// needs its own media type, status codes and error body, which gofr handlers
// do not give.
func (s *Server) Middleware() gofrHTTP.Middleware {
	handler := http.StripPrefix(basePath, http.HandlerFunc(s.serveAuthorized))

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.URL.Path, basePath+"/") {
				next.ServeHTTP(w, r)
				return
			}
			handler.ServeHTTP(w, r)
		})
	}
}

func (s *Server) serveAuthorized(w http.ResponseWriter, r *http.Request) {
	authorized, err := s.authorized(r)
	if err != nil {
		s.fail(w, err)
		return
	}
	if !authorized {
		w.Header().Set("WWW-Authenticate", `Bearer realm="scim"`)
		writeError(w, unauthorized())
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	s.mux.ServeHTTP(w, r)
}

func (s *Server) authorized(r *http.Request) (bool, error) {
	secret, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !found {
		return false, nil
	}

	return s.tokens.Verify(r.Context(), token.KindSCIM, secret)
}

func (s *Server) handle(pattern string, handler func(http.ResponseWriter, *http.Request) error) {
	s.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		if err := handler(w, r); err != nil {
			s.fail(w, err)
		}
	})
}

func (s *Server) fail(w http.ResponseWriter, err error) {
	var scimError Error
	switch {
	case errors.As(err, &scimError):
		writeError(w, scimError)
	case errors.Is(err, directory.ErrNotFound):
		writeError(w, notFound("the resource does not exist"))
	case errors.Is(err, directory.ErrConflict):
		writeError(w, conflict("a resource with the same unique attribute exists"))
	default:
		s.logger.Errorf("scim request failed: %v", err)
		writeError(w, Error{Status: http.StatusInternalServerError, Detail: "the request failed"})
	}
}

func decode(r *http.Request, into any) error {
	if err := json.NewDecoder(r.Body).Decode(into); err != nil {
		return badRequest("invalidSyntax", "the request body is not valid JSON: "+err.Error())
	}

	return nil
}
