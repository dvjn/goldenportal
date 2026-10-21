package scim

import (
	"encoding/json"
	"net/http"
	"strconv"
)

const (
	schemaError        = "urn:ietf:params:scim:api:messages:2.0:Error"
	schemaListResponse = "urn:ietf:params:scim:api:messages:2.0:ListResponse"
	schemaPatchOp      = "urn:ietf:params:scim:api:messages:2.0:PatchOp"
	schemaUser         = "urn:ietf:params:scim:schemas:core:2.0:User"
	schemaGroup        = "urn:ietf:params:scim:schemas:core:2.0:Group"
	schemaSPConfig     = "urn:ietf:params:scim:schemas:core:2.0:ServiceProviderConfig"
	schemaResourceType = "urn:ietf:params:scim:schemas:core:2.0:ResourceType"
	schemaSchema       = "urn:ietf:params:scim:schemas:core:2.0:Schema"
)

// Error is the error response of RFC 7644 section 3.12. Status is a string
// there, not a number.
type Error struct {
	Status   int
	ScimType string
	Detail   string
}

func (e Error) Error() string { return e.Detail }

func (e Error) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Schemas  []string `json:"schemas"`
		Status   string   `json:"status"`
		ScimType string   `json:"scimType,omitempty"`
		Detail   string   `json:"detail"`
	}{[]string{schemaError}, strconv.Itoa(e.Status), e.ScimType, e.Detail})
}

func badRequest(scimType, detail string) Error {
	return Error{Status: http.StatusBadRequest, ScimType: scimType, Detail: detail}
}

func notFound(detail string) Error {
	return Error{Status: http.StatusNotFound, Detail: detail}
}

func conflict(detail string) Error {
	return Error{Status: http.StatusConflict, ScimType: "uniqueness", Detail: detail}
}

func unauthorized() Error {
	return Error{Status: http.StatusUnauthorized, Detail: "a valid bearer token is required"}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/scim+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, err Error) {
	writeJSON(w, err.Status, err)
}
