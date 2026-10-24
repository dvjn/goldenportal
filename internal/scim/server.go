package scim

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"gofr.dev/pkg/gofr/logging"

	"github.com/dvjn/goldenportal/internal/audit"
)

const prefix = "/scim/v2"

// minTokenLength keeps the SCIM bearer token long enough not to be guessed.
const minTokenLength = 32

// Server answers SCIM requests from the identity provider. It is on when a
// bearer token is configured.
type Server struct {
	tokenHash []byte
	baseURL   string
	logger    logging.Logger
}

func New(token, baseURL string, logger logging.Logger) *Server {
	s := &Server{baseURL: strings.TrimRight(baseURL, "/"), logger: logger}
	if token != "" {
		sum := sha256.Sum256([]byte(token))
		s.tokenHash = sum[:]
		if len(token) < minTokenLength {
			logger.Warnf("GP_SCIM_TOKEN is shorter than %d characters; use a long random token", minTokenLength)
		}
	}

	return s
}

func (s *Server) Enabled() bool { return s.tokenHash != nil }

// URL is the SCIM base address to give the identity provider.
func (s *Server) URL() string { return s.baseURL + prefix }

// Routes serves /scim/v2 with the database from db.
func (s *Server) Routes(db DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != prefix && !strings.HasPrefix(r.URL.Path, prefix+"/") {
				next.ServeHTTP(w, r)
				return
			}
			s.serve(w, r, store{db: db}, db)
		})
	}
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request, st store, db DB) {
	if !s.Enabled() {
		writeError(w, http.StatusNotFound, "", "SCIM provisioning is turned off")
		return
	}
	if !s.authorized(r) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="scim"`)
		writeError(w, http.StatusUnauthorized, "", "send the SCIM token as \"Authorization: Bearer <token>\"")
		return
	}

	h := handler{s: s, st: st, db: db, w: w, r: r}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, prefix), "/"), "/")
	switch {
	case len(parts) == 1 && parts[0] == "ServiceProviderConfig" && r.Method == http.MethodGet:
		h.serviceProviderConfig()
	case len(parts) == 1 && parts[0] == "ResourceTypes" && r.Method == http.MethodGet:
		h.resourceTypes()
	case len(parts) == 1 && parts[0] == "Schemas" && r.Method == http.MethodGet:
		h.json(http.StatusOK, listOf(nil))
	case parts[0] == "Users":
		h.users(parts[1:])
	case parts[0] == "Groups":
		h.groups(parts[1:])
	default:
		writeError(w, http.StatusNotFound, "", "no such SCIM endpoint")
	}
}

func (s *Server) authorized(r *http.Request) bool {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return false
	}
	sum := sha256.Sum256([]byte(token))

	return subtle.ConstantTimeCompare(sum[:], s.tokenHash) == 1
}

type handler struct {
	s  *Server
	st store
	db DB
	w  http.ResponseWriter
	r  *http.Request
}

func (h handler) ctx() context.Context { return h.r.Context() }

func (h handler) json(status int, body any) {
	h.w.Header().Set("Content-Type", contentType)
	h.w.WriteHeader(status)
	_ = json.NewEncoder(h.w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, scimType, detail string) {
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorResponse{
		Schemas: []string{schemaError}, Status: strconv.Itoa(status), ScimType: scimType, Detail: detail,
	})
}

func (h handler) fail(err error, kind string) {
	switch {
	case errors.Is(err, errNotFound):
		writeError(h.w, http.StatusNotFound, "", "no such "+kind)
	case errors.Is(err, errConflict):
		writeError(h.w, http.StatusConflict, "uniqueness", "a "+kind+" with that name already exists")
	default:
		h.s.logger.Errorf("scim: %v", err)
		writeError(h.w, http.StatusInternalServerError, "", "the request failed; check the portal's log")
	}
}

func (h handler) bind(v any) bool {
	body, err := io.ReadAll(io.LimitReader(h.r.Body, maxRequestBodyBytes))
	if err == nil {
		err = json.Unmarshal(body, v)
	}
	if err != nil {
		writeError(h.w, http.StatusBadRequest, "invalidSyntax", "the request body is not valid JSON")
		return false
	}

	return true
}

// page reads startIndex and count, which SCIM counts from 1.
func (h handler) page() (int, int) {
	start, err := strconv.Atoi(h.r.URL.Query().Get("startIndex"))
	if err != nil || start < 1 {
		start = 1
	}
	count, err := strconv.Atoi(h.r.URL.Query().Get("count"))
	if err != nil || count < 0 {
		count = defaultListCount
	}

	return start, min(count, maxListCount)
}

// record writes an audit event with SCIM as the actor.
func (h handler) record(action string, target audit.Target, details map[string]any) {
	if err := audit.Record(h.ctx(), h.db, h.s.logger, audit.Actor{Label: "SCIM"}, action, target, details); err != nil {
		h.s.logger.Errorf("record audit event %s: %v", action, err)
	}
}

func listOf(resources []any) listResponse {
	if resources == nil {
		resources = []any{}
	}

	return listResponse{
		Schemas: []string{schemaListResponse}, TotalResults: len(resources),
		StartIndex: 1, ItemsPerPage: len(resources), Resources: resources,
	}
}

func (h handler) serviceProviderConfig() {
	supported := func(on bool) map[string]bool { return map[string]bool{"supported": on} }
	h.json(http.StatusOK, map[string]any{
		"schemas":        []string{schemaProviderCfg},
		"patch":          supported(true),
		"bulk":           map[string]any{"supported": false, "maxOperations": 0, "maxPayloadSize": 0},
		"filter":         map[string]any{"supported": true, "maxResults": maxListCount},
		"changePassword": supported(false),
		"sort":           supported(false),
		"etag":           supported(false),
		"authenticationSchemes": []map[string]any{{
			"type": "oauthbearertoken", "name": "Bearer token",
			"description": "The token set in GP_SCIM_TOKEN.",
		}},
	})
}

func (h handler) resourceTypes() {
	resourceType := func(name, endpoint, schema string) any {
		return map[string]any{
			"schemas": []string{schemaResourceType}, "id": name, "name": name,
			"endpoint": endpoint, "schema": schema,
		}
	}
	h.json(http.StatusOK, listOf([]any{
		resourceType("User", "/Users", schemaUser),
		resourceType("Group", "/Groups", schemaGroup),
	}))
}

// validID reports whether id could name a portal user or group.
func validID(id string) bool {
	return uuid.Validate(id) == nil
}
