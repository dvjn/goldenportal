package dashboard

import (
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/configdoc"
)

type Audience struct {
	Roles  []auth.Role `json:"roles,omitempty"`
	Groups []string    `json:"groups,omitempty"`
}

func (a Audience) Matches(p auth.Principal) bool {
	if slices.Contains(a.Roles, p.Role) {
		return true
	}
	return slices.ContainsFunc(a.Groups, func(group string) bool { return slices.Contains(p.Groups, group) })
}

func (s Spec) Visible(p auth.Principal) bool {
	return s.Visibility == nil || s.Visibility.Matches(p)
}

func parseAudience(r *configdoc.Reader, node *yaml.Node, path string) *Audience {
	if node == nil {
		return nil
	}
	fields, ok := r.Fields(node, path, "roles", "groups")
	if !ok {
		return nil
	}
	a := &Audience{}
	for _, value := range texts(r, fields["roles"], path+".roles") {
		role, ok := auth.ParseRole(value)
		if !ok {
			r.Errorf(fields["roles"], path+".roles", "role must be member, builder or admin")
		} else if !slices.Contains(a.Roles, role) {
			a.Roles = append(a.Roles, role)
		}
	}
	for _, group := range texts(r, fields["groups"], path+".groups") {
		if strings.TrimSpace(group) == "" {
			r.Errorf(fields["groups"], path+".groups", "group names can't be empty")
		} else if !slices.Contains(a.Groups, group) {
			a.Groups = append(a.Groups, group)
		}
	}
	if len(a.Roles)+len(a.Groups) == 0 {
		r.Errorf(node, path, "name at least one role or group; leave %s out to match everyone", path)
	}
	return a
}
