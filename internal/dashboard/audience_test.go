package dashboard

import (
	"strings"
	"testing"

	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/configdoc"
)

func TestAudience(t *testing.T) {
	source := `kind: Dashboard
metadata: {name: mine, title: Mine}
spec:
  visibility: {roles: [builder], groups: [payments]}
  defaultFor: {groups: [payments, search]}
  widgets:
    hello: {type: markdown, text: Hello}
`
	spec, issues := Parse(source)
	if configdoc.HasErrors(issues) {
		t.Fatal(issues)
	}
	for _, test := range []struct {
		role               auth.Role
		groups             []string
		visible, preferred bool
	}{
		{auth.RoleMember, nil, false, false},
		{auth.RoleMember, []string{"payments"}, true, true},
		{auth.RoleMember, []string{"search"}, false, true},
		{auth.RoleBuilder, nil, true, false},
		{auth.RoleAdmin, nil, false, false},
	} {
		p := auth.Principal{Role: test.role, Groups: test.groups}
		if spec.Visible(p) != test.visible || spec.DefaultFor.Matches(p) != test.preferred {
			t.Errorf("%+v matched incorrectly", p)
		}
	}
	for _, value := range []string{"{}", "{roles: [owner]}", "{groups: ['']}", "{users: [ada]}", "{roles: member}"} {
		_, issues := Parse(strings.Replace(source, "{roles: [builder], groups: [payments]}", value, 1))
		if !configdoc.HasErrors(issues) {
			t.Errorf("accepted audience %s", value)
		}
		for _, issue := range issues {
			if issue.Line == 0 {
				t.Errorf("missing line: %+v", issue)
			}
		}
	}
	public := Spec{}
	if !public.Visible(auth.Principal{Role: auth.RoleMember}) {
		t.Fatal("audience-less dashboard must remain public")
	}
	changes := Diff(Spec{}, spec)
	if len(changes) < 2 {
		t.Fatal("audience changes missing from impact preview")
	}
}
