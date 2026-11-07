package widget

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/dvjn/goldenportal/internal/jq"
)

// jqVariables are what jq can read wherever widgets run it, in values,
// columns and link templates. $x.y means what it does in a filter: $me is
// whoever is looking, as email, name, role and groups; $input.<id> what a
// dashboard input is set to, always a list; $today the date; and $entity
// the entity whose page it is, flat, as jq reads every entity.
var jqVariables = []string{"$input", "$me", "$today", "$entity"}

// CompileJQ compiles jq as widgets run it, with their variables. Off entity
// pages $entity isn't one, so using it fails to compile.
func CompileJQ(expression string, scope Scope) (*jq.Code, error) {
	names := jqVariables
	if !scope.Entity {
		names = names[:len(names)-1]
	}

	return jq.Compile(expression, names...)
}

// Env is what widgets load against: the values filters compare with, and
// the entity whose page it is.
type Env struct {
	Values Values
	// Data are the page entity's values as filters compare with them,
	// typed, as EntityValues gives them.
	Data map[string][]any
	// Entity is the page entity as jq reads $entity.
	Entity map[string]any
}

// jq is what jqVariables stand for, in their order.
func (e Env) jq() []any {
	inputs := map[string]any{}
	for name, values := range e.Values {
		if id, ok := strings.CutPrefix(name, ValueInput); ok {
			inputs[id] = anys(values)
		}
	}
	first := func(name string) any {
		if values := e.Values[name]; len(values) > 0 {
			return values[0]
		}
		return nil
	}
	me := map[string]any{
		"email": first(ValueMe + ".email"), "name": first(ValueMe + ".name"),
		"role": first(ValueMe + ".role"), "groups": anys(e.Values[ValueMe+".groups"]),
	}
	var entity any
	if e.Entity != nil {
		entity = e.Entity
	}

	return []any{inputs, me, first(ValueToday), entity}
}

func anys(values []string) []any {
	out := make([]any, len(values))
	for i, value := range values {
		out[i] = value
	}

	return out
}

// RunJQ runs compiled jq on input and returns its first result, within
// the default timeout of ctx.
func RunJQ(ctx context.Context, code *jq.Code, input any, env Env) (any, error) {
	value, _, err := code.First(ctx, jq.DefaultTimeout, input, env.jq()...)

	return value, err
}

// CheckTemplate checks a template's {{ }} jq compiles.
func CheckTemplate(text string, scope Scope) error {
	for _, match := range tokenPattern.FindAllStringSubmatch(text, -1) {
		if strings.TrimSpace(match[1]) == "" {
			return fmt.Errorf("%s must hold jq, like {{ .identifier }}", match[0])
		}
		if _, err := CompileJQ(match[1], scope); err != nil {
			return fmt.Errorf("%s doesn't compile: %v", strings.TrimSpace(match[0]), err)
		}
	}

	return nil
}

// RenderTemplate fills in a link template, running the jq in each {{ }}
// on input. What each gives is escaped for a URL, and a list joins with
// commas, so a value can't change where the link goes.
func RenderTemplate(ctx context.Context, text string, input any, env Env) (string, error) {
	var failed error
	rendered := tokenPattern.ReplaceAllStringFunc(text, func(token string) string {
		code, err := CompileJQ(token[2:len(token)-2], Scope{Entity: true})
		if err == nil {
			var value any
			if value, err = RunJQ(ctx, code, input, env); err == nil {
				return escape(format(value))
			}
		}
		if failed == nil {
			failed = fmt.Errorf("%s: %w", strings.TrimSpace(token), err)
		}
		return ""
	})

	return rendered, failed
}

func format(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case []any:
		items := make([]string, len(v))
		for i, item := range v {
			items[i] = format(item)
		}
		return strings.Join(items, ",")
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	encoded, _ := json.Marshal(value)

	return string(encoded)
}

// escape percent-encodes all but what reads the same anywhere in a path or
// a query string. @ is encoded too, so a value can't make the host before
// it a user name.
func escape(text string) string {
	var b strings.Builder
	for _, c := range []byte(text) {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("-._~/,:!$*", c) >= 0 {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", c)
	}

	return b.String()
}
