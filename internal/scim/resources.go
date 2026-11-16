// Package scim provisions users and groups from an identity provider over
// SCIM 2.0 (RFC 7643 and 7644). It writes to the identity tables that sign-in
// and role grants read, never to the catalog.
package scim

import (
	"strings"
	"time"
)

const (
	schemaUser          = "urn:ietf:params:scim:schemas:core:2.0:User"
	schemaGroup         = "urn:ietf:params:scim:schemas:core:2.0:Group"
	schemaGroupParents  = "urn:goldenportal:params:scim:schemas:extension:2.0:Group"
	schemaListResponse  = "urn:ietf:params:scim:api:messages:2.0:ListResponse"
	schemaPatchOp       = "urn:ietf:params:scim:api:messages:2.0:PatchOp"
	schemaError         = "urn:ietf:params:scim:api:messages:2.0:Error"
	schemaProviderCfg   = "urn:ietf:params:scim:schemas:core:2.0:ServiceProviderConfig"
	schemaResourceType  = "urn:ietf:params:scim:schemas:core:2.0:ResourceType"
	schemaSchema        = "urn:ietf:params:scim:schemas:core:2.0:Schema"
	contentType         = "application/scim+json"
	defaultListCount    = 100
	maxListCount        = 1000
	maxRequestBodyBytes = 1 << 20
)

type meta struct {
	ResourceType string    `json:"resourceType"`
	Created      time.Time `json:"created"`
	LastModified time.Time `json:"lastModified"`
	Location     string    `json:"location,omitempty"`
}

type ref struct {
	Value   string `json:"value"`
	Display string `json:"display,omitempty"`
}

type name struct {
	Formatted  string `json:"formatted,omitempty"`
	GivenName  string `json:"givenName,omitempty"`
	FamilyName string `json:"familyName,omitempty"`
}

type email struct {
	Value   string `json:"value"`
	Type    string `json:"type,omitempty"`
	Primary bool   `json:"primary,omitempty"`
}

type userResource struct {
	Schemas     []string `json:"schemas"`
	ID          string   `json:"id,omitempty"`
	ExternalID  string   `json:"externalId,omitempty"`
	UserName    string   `json:"userName"`
	Name        *name    `json:"name,omitempty"`
	DisplayName string   `json:"displayName,omitempty"`
	Emails      []email  `json:"emails,omitempty"`
	Active      *bool    `json:"active,omitempty"`
	Groups      []ref    `json:"groups,omitempty"`
	Meta        *meta    `json:"meta,omitempty"`
}

type groupResource struct {
	Schemas     []string      `json:"schemas"`
	ID          string        `json:"id,omitempty"`
	ExternalID  string        `json:"externalId,omitempty"`
	DisplayName string        `json:"displayName"`
	Members     []ref         `json:"members,omitempty"`
	Parents     *groupParents `json:"urn:goldenportal:params:scim:schemas:extension:2.0:Group,omitempty"`
	Meta        *meta         `json:"meta,omitempty"`
}

// groupParents is the portal's group extension: the groups this group is
// in, by the identity provider's id for them.
type groupParents struct {
	// Parents is read the way PATCH reads it, so a single object or a
	// malformed element doesn't fail the whole request.
	Parents any `json:"parents"`
}

type parentRef struct {
	ExternalID string `json:"externalId"`
}

func (p *groupParents) externalIDs() ([]string, error) {
	if p == nil {
		return nil, nil
	}

	return parentList(p.Parents)
}

type listResponse struct {
	Schemas      []string `json:"schemas"`
	TotalResults int      `json:"totalResults"`
	StartIndex   int      `json:"startIndex"`
	ItemsPerPage int      `json:"itemsPerPage"`
	Resources    []any    `json:"Resources"`
}

type errorResponse struct {
	Schemas  []string `json:"schemas"`
	Status   string   `json:"status"`
	ScimType string   `json:"scimType,omitempty"`
	Detail   string   `json:"detail"`
}

// userInput is what a create or replace asks for, reduced to what the portal keeps.
type userInput struct {
	UserName    string
	ExternalID  string
	DisplayName string
	Email       string
	Active      bool
}

func (u userResource) input() userInput {
	in := userInput{
		UserName:   strings.TrimSpace(u.UserName),
		ExternalID: u.ExternalID,
		Active:     u.Active == nil || *u.Active,
	}

	in.DisplayName = strings.TrimSpace(u.DisplayName)
	if in.DisplayName == "" && u.Name != nil {
		in.DisplayName = strings.TrimSpace(u.Name.Formatted)
		if in.DisplayName == "" {
			in.DisplayName = strings.TrimSpace(u.Name.GivenName + " " + u.Name.FamilyName)
		}
	}
	if in.DisplayName == "" {
		in.DisplayName = in.UserName
	}

	for _, e := range u.Emails {
		if e.Primary || in.Email == "" {
			in.Email = strings.TrimSpace(e.Value)
		}
	}
	if in.Email == "" && strings.Contains(in.UserName, "@") {
		in.Email = in.UserName
	}

	return in
}
