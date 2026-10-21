package scim

import (
	"net/http"

	"github.com/dvjn/goldport/internal/directory"
)

type supported struct {
	Supported bool `json:"supported"`
}

type serviceProviderConfig struct {
	Schemas               []string               `json:"schemas"`
	Patch                 supported              `json:"patch"`
	Bulk                  bulkConfig             `json:"bulk"`
	Filter                filterConfig           `json:"filter"`
	ChangePassword        supported              `json:"changePassword"`
	Sort                  supported              `json:"sort"`
	ETag                  supported              `json:"etag"`
	AuthenticationSchemes []authenticationScheme `json:"authenticationSchemes"`
	Meta                  meta                   `json:"meta"`
}

type bulkConfig struct {
	Supported      bool `json:"supported"`
	MaxOperations  int  `json:"maxOperations"`
	MaxPayloadSize int  `json:"maxPayloadSize"`
}

type filterConfig struct {
	Supported  bool `json:"supported"`
	MaxResults int  `json:"maxResults"`
}

type authenticationScheme struct {
	Type        string `json:"type"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Primary     bool   `json:"primary"`
}

type resourceType struct {
	Schemas  []string `json:"schemas"`
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Endpoint string   `json:"endpoint"`
	Schema   string   `json:"schema"`
	Meta     meta     `json:"meta"`
}

type schema struct {
	Schemas     []string    `json:"schemas"`
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Attributes  []attribute `json:"attributes"`
	Meta        meta        `json:"meta"`
}

type attribute struct {
	Name          string      `json:"name"`
	Type          string      `json:"type"`
	MultiValued   bool        `json:"multiValued"`
	Required      bool        `json:"required"`
	CaseExact     bool        `json:"caseExact"`
	Mutability    string      `json:"mutability"`
	Returned      string      `json:"returned"`
	Uniqueness    string      `json:"uniqueness"`
	SubAttributes []attribute `json:"subAttributes,omitempty"`
}

var serviceProviderConfiguration = serviceProviderConfig{
	Schemas:        []string{schemaSPConfig},
	Patch:          supported{Supported: true},
	Bulk:           bulkConfig{},
	Filter:         filterConfig{Supported: true, MaxResults: maxResults},
	ChangePassword: supported{},
	Sort:           supported{},
	ETag:           supported{},
	AuthenticationSchemes: []authenticationScheme{{
		Type:        "oauthbearertoken",
		Name:        "OAuth Bearer Token",
		Description: "Authentication with a bearer token issued by the portal",
		Primary:     true,
	}},
	Meta: meta{ResourceType: "ServiceProviderConfig", Location: basePath + "/ServiceProviderConfig"},
}

var resourceTypeList = []resourceType{
	{
		Schemas:  []string{schemaResourceType},
		ID:       "User",
		Name:     "User",
		Endpoint: "/Users",
		Schema:   schemaUser,
		Meta:     meta{ResourceType: "ResourceType", Location: basePath + "/ResourceTypes/User"},
	},
	{
		Schemas:  []string{schemaResourceType},
		ID:       "Group",
		Name:     "Group",
		Endpoint: "/Groups",
		Schema:   schemaGroup,
		Meta:     meta{ResourceType: "ResourceType", Location: basePath + "/ResourceTypes/Group"},
	},
}

func stringAttribute(name string, required bool, uniqueness string) attribute {
	return attribute{
		Name: name, Type: "string", Required: required, Mutability: "readWrite", Returned: "default", Uniqueness: uniqueness,
	}
}

var schemaList = []schema{
	{
		Schemas:     []string{schemaSchema},
		ID:          schemaUser,
		Name:        "User",
		Description: "User Account",
		Attributes: []attribute{
			stringAttribute("userName", true, "server"),
			{
				Name: "name", Type: "complex", Mutability: "readWrite", Returned: "default", Uniqueness: "none",
				SubAttributes: []attribute{stringAttribute("formatted", false, "none")},
			},
			stringAttribute("displayName", false, "none"),
			{
				Name: "emails", Type: "complex", MultiValued: true, Mutability: "readWrite", Returned: "default", Uniqueness: "none",
				SubAttributes: []attribute{
					stringAttribute("value", false, "none"),
					{Name: "primary", Type: "boolean", Mutability: "readWrite", Returned: "default", Uniqueness: "none"},
				},
			},
			{Name: "active", Type: "boolean", Mutability: "readWrite", Returned: "default", Uniqueness: "none"},
		},
		Meta: meta{ResourceType: "Schema", Location: basePath + "/Schemas/" + schemaUser},
	},
	{
		Schemas:     []string{schemaSchema},
		ID:          schemaGroup,
		Name:        "Group",
		Description: "Group",
		Attributes: []attribute{
			stringAttribute("displayName", true, "server"),
			{
				Name: "members", Type: "complex", MultiValued: true, Mutability: "readWrite", Returned: "default", Uniqueness: "none",
				SubAttributes: []attribute{
					stringAttribute("value", false, "none"),
					{Name: "display", Type: "string", Mutability: "readOnly", Returned: "default", Uniqueness: "none"},
				},
			},
		},
		Meta: meta{ResourceType: "Schema", Location: basePath + "/Schemas/" + schemaGroup},
	},
}

func (s *Server) serviceProviderConfig(w http.ResponseWriter, _ *http.Request) error {
	writeJSON(w, http.StatusOK, serviceProviderConfiguration)
	return nil
}

func (s *Server) resourceTypes(w http.ResponseWriter, _ *http.Request) error {
	writeJSON(w, http.StatusOK, newListResponse(resourceTypeList, len(resourceTypeList), directory.Page{}))
	return nil
}

func (s *Server) schemas(w http.ResponseWriter, _ *http.Request) error {
	writeJSON(w, http.StatusOK, newListResponse(schemaList, len(schemaList), directory.Page{}))
	return nil
}
