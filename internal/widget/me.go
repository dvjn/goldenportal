package widget

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/dvjn/goldenportal/internal/auth"
)

// attribute is one thing known about whoever is looking, which filters
// compare with and text shows as $me.<name>.
type attribute struct {
	name string
	// multi is set when the attribute is a list, which binds like a
	// multiple filter: eq becomes in.
	multi bool
	read  func(auth.Principal) []string
}

// attributes are every $me.<name>, in the order messages list them. They
// all come off the signed-in principal, so nothing loads per widget, and
// only what the viewer may see about themselves is here: their internal
// id stays out, since it's a database key, not an identity.
var attributes = []attribute{
	{"email", false, func(p auth.Principal) []string { return []string{p.Email} }},
	{"name", false, func(p auth.Principal) []string { return []string{p.DisplayName} }},
	{"role", false, func(p auth.Principal) []string { return []string{string(p.Role)} }},
	{"groups", true, func(p auth.Principal) []string { return p.Groups }},
}

// MeVariables lists what $me can be, as written.
func MeVariables() []string {
	names := []string{ValueMe}
	for _, a := range attributes {
		names = append(names, ValueMe+"."+a.name)
	}

	return names
}

// MeValues is what $me and every $me.<name> stand for, for whoever is
// looking. $me alone is their email. An attribute with no value is
// Nothing, so comparing with it matches nothing rather than everything.
func MeValues(p auth.Principal) Values {
	values := Values{ValueMe: {p.Email}}
	for _, a := range attributes {
		found := a.read(p)
		if len(found) == 0 || (!a.multi && found[0] == "") {
			found = Nothing
		}
		values[ValueMe+"."+a.name] = found
	}

	return values
}

// checkVariable reports a $me.<name> that isn't an attribute, so a typo
// fails at publish time rather than quietly comparing with the text. Other
// texts are literals.
func checkVariable(text string) error {
	if strings.HasPrefix(text, "$today") {
		_, err := dayOffset(text)
		return err
	}
	if strings.HasPrefix(text, "$entity.") && !entityName.MatchString(text) {
		return fmt.Errorf("invalid entity variable %s", text)
	}
	if !strings.HasPrefix(text, ValueMe+".") {
		return nil
	}

	return checkMe(text)
}

func checkMe(name string) error {
	if slices.Contains(MeVariables(), name) {
		return nil
	}

	return fmt.Errorf("unknown variable %s; known: %s", name, strings.Join(MeVariables(), ", "))
}

// tokenPattern finds {{ ... }} in text. Only this spelling is a variable:
// a bare $ is ordinary in prose.
var tokenPattern = regexp.MustCompile(`\{\{([^{}]*)\}\}`)

// CheckText validates templates outside entity pages.
func CheckText(text string) error { return CheckTextIn(text, Scope{}) }

func CheckTextIn(text string, scope Scope) error {
	for _, match := range tokenPattern.FindAllStringSubmatch(text, -1) {
		name := strings.TrimSpace(match[1])
		if name == "" {
			return fmt.Errorf("%s must name a variable, like {{ %s.name }}", match[0], ValueMe)
		}
		if name == "$entity" || strings.HasPrefix(name, "$entity.") {
			if !scope.Entity {
				return fmt.Errorf("%s is only available on entity pages", name)
			}
			if name != "$entity" && !entityName.MatchString(name) {
				return fmt.Errorf("invalid entity variable %s", name)
			}
			continue
		}
		if strings.HasPrefix(name, "$today") {
			if _, err := dayOffset(name); err != nil {
				return err
			}
			continue
		}
		if err := checkMe(name); err != nil {
			return err
		}
	}

	return nil
}

// Render fills in template variables in text. A list joins
// with commas, and a variable with no value leaves nothing behind.
func Render(text string, values Values) string {
	if !strings.Contains(text, "{{") {
		return text
	}

	return tokenPattern.ReplaceAllStringFunc(text, func(token string) string {
		name := strings.TrimSpace(token[2 : len(token)-2])
		value, _ := lookup(values, name)
		return strings.Join(value, ", ")
	})
}

// RenderAll renders the text on every widget, leaving the originals alone.
func RenderAll(widgets []Widget, values Values) []Widget {
	rendered := make([]Widget, len(widgets))
	for i, w := range widgets {
		w.Title, w.Description, w.Text = Render(w.Title, values), Render(w.Description, values), Render(w.Text, values)
		rendered[i] = w
	}

	return rendered
}
