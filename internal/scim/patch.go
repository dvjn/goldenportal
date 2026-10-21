package scim

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/dvjn/goldport/internal/directory"
)

type patchRequest struct {
	Operations []patchOperation `json:"Operations"`
}

type patchOperation struct {
	Op    string          `json:"op"`
	Path  string          `json:"path"`
	Value json.RawMessage `json:"value"`
}

// Attributes that an operation without a path may carry and that the server
// sets itself.
var readOnlyAttributes = []string{"id", "meta", "schemas"}

var memberFilterPath = regexp.MustCompile(`(?i)^members\[value eq "([^"]+)"\]$`)

// decodePatch reads a PatchOp request into operations that each name one
// attribute in lower case. An operation without a path, such as Authentik's
// replace of a whole group, becomes one operation per attribute of its value.
// Removing members[value eq "id"] becomes removing that member from members,
// and removing members without a value becomes replacing them with none.
func decodePatch(r *http.Request) ([]patchOperation, error) {
	var request patchRequest
	if err := decode(r, &request); err != nil {
		return nil, err
	}

	var operations []patchOperation
	for _, operation := range request.Operations {
		spread, err := spreadOperation(operation)
		if err != nil {
			return nil, err
		}
		for _, attribute := range spread {
			operations = append(operations, normalize(attribute))
		}
	}

	return operations, nil
}

func spreadOperation(operation patchOperation) ([]patchOperation, error) {
	operation.Op = strings.ToLower(operation.Op)
	if !slices.Contains([]string{"add", "remove", "replace"}, operation.Op) {
		return nil, badRequest("invalidSyntax", "unsupported patch operation "+operation.Op)
	}
	if operation.Path != "" {
		return []patchOperation{operation}, nil
	}
	if operation.Op == "remove" {
		return nil, badRequest("noTarget", "remove needs a path")
	}

	var attributes map[string]json.RawMessage
	if err := json.Unmarshal(operation.Value, &attributes); err != nil {
		return nil, badRequest("invalidValue", "an operation without a path needs an object value")
	}
	var spread []patchOperation
	for _, name := range slices.Sorted(maps.Keys(attributes)) {
		if !slices.Contains(readOnlyAttributes, strings.ToLower(name)) {
			spread = append(spread, patchOperation{Op: operation.Op, Path: name, Value: attributes[name]})
		}
	}

	return spread, nil
}

func normalize(operation patchOperation) patchOperation {
	if match := memberFilterPath.FindStringSubmatch(operation.Path); match != nil && operation.Op == "remove" {
		return patchOperation{Op: "remove", Path: "members", Value: json.RawMessage(`[{"value":` + strconv.Quote(match[1]) + `}]`)}
	}
	operation.Path = strings.ToLower(operation.Path)
	if operation.Path == "members" && operation.Op == "remove" && len(operation.Value) == 0 {
		return patchOperation{Op: "replace", Path: "members", Value: json.RawMessage(`[]`)}
	}

	return operation
}

var userAttributes = map[string]func(*directory.User, json.RawMessage) error{
	"active":      func(user *directory.User, value json.RawMessage) error { return decodeValue(value, &user.Active) },
	"displayname": func(user *directory.User, value json.RawMessage) error { return decodeValue(value, &user.Name) },
	"username":    func(user *directory.User, value json.RawMessage) error { return decodeValue(value, &user.UserName) },
	"externalid":  func(user *directory.User, value json.RawMessage) error { return decodeValue(value, &user.ExternalID) },
}

func patchUser(user *directory.User, operations []patchOperation) error {
	for _, operation := range operations {
		set, supported := userAttributes[operation.Path]
		if !supported || operation.Op == "remove" {
			return badRequest("invalidPath", "cannot "+operation.Op+" "+operation.Path+" on a user")
		}
		if err := set(user, operation.Value); err != nil {
			return err
		}
	}

	return nil
}

var groupAttributes = map[string]func(*directory.Group, json.RawMessage) error{
	"displayname": func(group *directory.Group, value json.RawMessage) error {
		return decodeValue(value, &group.DisplayName)
	},
	"externalid": func(group *directory.Group, value json.RawMessage) error {
		return decodeValue(value, &group.ExternalID)
	},
}

type membershipChange func(ctx context.Context, groupID string, userIDs []string) error

type memberUpdate struct {
	apply   membershipChange
	userIDs []string
}

// patchGroupOperations applies the attribute operations to the group and
// returns the membership updates in order, for the caller to store after the
// group.
func (s *Server) patchGroupOperations(group *directory.Group, operations []patchOperation) ([]memberUpdate, error) {
	changes := map[string]membershipChange{
		"add":     s.directory.AddMembers,
		"remove":  s.directory.RemoveMembers,
		"replace": s.directory.SetMembers,
	}

	var updates []memberUpdate
	for _, operation := range operations {
		if set, isAttribute := groupAttributes[operation.Path]; isAttribute && operation.Op != "remove" {
			if err := set(group, operation.Value); err != nil {
				return nil, err
			}
			continue
		}
		if operation.Path != "members" {
			return nil, badRequest("invalidPath", "cannot "+operation.Op+" "+operation.Path+" on a group")
		}

		var members []member
		if err := decodeValue(operation.Value, &members); err != nil {
			return nil, err
		}
		updates = append(updates, memberUpdate{apply: changes[operation.Op], userIDs: memberIDs(members)})
	}

	return updates, nil
}

func decodeValue(value json.RawMessage, into any) error {
	if err := json.Unmarshal(value, into); err != nil {
		return badRequest("invalidValue", "the patch value has the wrong type")
	}

	return nil
}
