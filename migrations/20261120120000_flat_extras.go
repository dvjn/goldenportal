package migrations

import (
	"bytes"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"
	"gofr.dev/pkg/gofr/migration"
)

// flatExtras rewrites blueprint sources' reads of what connectors added
// under _gp, such as ._gp.project, to the top-level fields they add
// instead, ._project. Reads it can't rewrite on their own, such as
// ._gp | keys, are logged.
func flatExtras() migration.Migrate {
	return migration.Migrate{
		UP: func(d migration.Datasource) error {
			return rewriteSources(d.SQL, func(text string) (string, error) {
				return rewriteGP(text, func(identifier, expression string) {
					d.Logger.Warnf("blueprint %s still reads _gp in %q: read what connectors add as ._<name>", identifier, expression)
				})
			})
		},
	}
}

var (
	gpRead = regexp.MustCompile(`(?:\._gp|\."_gp"|\.\["_gp"\])(\?)?` +
		`(?:\.([A-Za-z_][A-Za-z0-9_]*)|\."([A-Za-z_][A-Za-z0-9_]*)"|\.?\["([A-Za-z_][A-Za-z0-9_]*)"\])`)
	gpHead = regexp.MustCompile(`\._gp\b|\."_gp"|\.\["_gp"\]`)
)

// rewriteGP rewrites one document source's blueprint source expressions,
// calling left with each that still reads _gp. Other documents, and
// sources that don't parse, come back as they are.
func rewriteGP(text string, left func(identifier, expression string)) (string, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil || len(doc.Content) == 0 {
		return text, nil
	}
	root := doc.Content[0]
	if kind := field(root, "kind"); kind == nil || kind.Value != "Blueprint" {
		return text, nil
	}
	var identifier string
	if n := field(field(root, "metadata"), "identifier"); n != nil {
		identifier = n.Value
	}
	sources := field(field(root, "spec"), "sources")
	if sources == nil || sources.Kind != yaml.SequenceNode {
		return text, nil
	}

	changed := false
	rewrite := func(node *yaml.Node) {
		if node == nil || node.Kind != yaml.ScalarNode {
			return
		}
		value, stuck := gpRewrite(node.Value)
		if value != node.Value {
			node.Value, changed = value, true
		}
		if stuck {
			left(identifier, value)
		}
	}
	for _, source := range sources.Content {
		entity := field(source, "entity")
		rewrite(field(source, "filter"))
		rewrite(field(entity, "identifier"))
		rewrite(field(entity, "title"))
		if properties := field(entity, "properties"); properties != nil && properties.Kind == yaml.MappingNode {
			for i := 1; i < len(properties.Content); i += 2 {
				rewrite(properties.Content[i])
			}
		}
	}
	if !changed {
		return text, nil
	}

	var out bytes.Buffer
	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(2)
	if err := encoder.Encode(&doc); err != nil {
		return "", err
	}

	return out.String(), nil
}

// gpRewrite rewrites the reads of _gp that start a path from the item, and
// reports whether any is left it couldn't rewrite. Paths further in, such
// as .labels["_gp"], and string literals are left alone.
func gpRewrite(expression string) (string, bool) {
	code := jqCode(expression)
	var out strings.Builder
	last := 0
	for _, m := range gpRead.FindAllStringSubmatchIndex(expression, -1) {
		if !startsPath(expression, code, m[0]) {
			continue
		}
		group := func(i int) string {
			if m[2*i] < 0 {
				return ""
			}
			return expression[m[2*i]:m[2*i+1]]
		}
		out.WriteString(expression[last:m[0]] + "._" + group(2) + group(3) + group(4) + group(1))
		last = m[1]
	}
	out.WriteString(expression[last:])
	rewritten := out.String()
	code = jqCode(rewritten)
	for _, m := range gpHead.FindAllStringIndex(rewritten, -1) {
		if startsPath(rewritten, code, m[0]) {
			return rewritten, true
		}
	}

	return rewritten, false
}

// startsPath reports whether the . at at starts a path in code, rather than
// going on from a value, as in .x._gp or $x._gp.
func startsPath(expression string, code []bool, at int) bool {
	return code[at] && (at == 0 || !strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_$.?)]}\"", rune(expression[at-1])))
}

// jqCode marks the bytes of a jq expression that are code, leaving out
// string literals, but for their interpolations, and comments.
func jqCode(expression string) []bool {
	code := make([]bool, len(expression))
	// outer holds the paren depth to return to as each interpolation ends.
	var outer []int
	depth, inString := 0, false
	for i := 0; i < len(expression); i++ {
		c := expression[i]
		if inString {
			switch {
			case c == '\\' && i+1 < len(expression) && expression[i+1] == '(':
				outer, depth, inString = append(outer, depth), 0, false
				i++
			case c == '\\':
				i++
			case c == '"':
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '#':
			for i < len(expression) && expression[i] != '\n' {
				i++
			}
		case ')':
			if depth == 0 && len(outer) > 0 {
				depth, outer, inString = outer[len(outer)-1], outer[:len(outer)-1], true
				continue
			}
			depth--
			code[i] = true
		case '(':
			depth++
			code[i] = true
		default:
			code[i] = true
		}
	}

	return code
}
