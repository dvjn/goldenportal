package blueprint

import (
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/dvjn/goldenportal/internal/configdoc"
)

// InputFields are the keys an input takes: a stored property's, since
// inputs are values people enter, which nothing derives.
var InputFields = []string{
	"title", "description", "type", "format", "enum", "required", "pattern", "minLength", "maxLength",
	"minimum", "maximum", "items", "relation", "many",
}

// ParseInputs reads inputs written as blueprint properties are, keyed by
// identifier, such as the values an action asks for. Issues go to r.
func ParseInputs(r *configdoc.Reader, node *yaml.Node, path string) []Property {
	inputs := []Property{}
	if node == nil {
		return inputs
	}
	p := &parser{}
	r.Each(node, path, func(id string, key, value *yaml.Node) {
		inputPath := path + "." + id
		if !fieldPattern.MatchString(id) {
			r.Errorf(key, inputPath, "%q must start with a letter and use only letters, digits and _", id)
			return
		}
		prop := Property{ID: id, Title: id, line: key.Line}
		fields, ok := r.Fields(value, inputPath, InputFields...)
		if !ok {
			return
		}
		inputs = append(inputs, p.shape(prop, inputPath, key, fields))
	})
	r.Issues = append(r.Issues, p.Issues...)

	return inputs
}

// ValidateInputs checks values against inputs and returns them cleaned up,
// with relations among them, and what's wrong with each by input. Problems
// that aren't about one input are under "".
func ValidateInputs(inputs []Property, values map[string]any) (map[string]any, map[string][]string) {
	spec := Spec{Identifier: "inputs", Properties: inputs}
	clean, problems := spec.ValidateEntity(Entity{Identifier: "inputs", Properties: values})
	out := clean.Properties
	for key, value := range clean.Relations {
		out[key] = value
	}
	byInput := map[string][]string{}
	for _, problem := range problems {
		rest, ok := strings.CutPrefix(problem, "properties.")
		if !ok {
			byInput[""] = append(byInput[""], strings.Replace(problem, "inputs has no property", "there's no input", 1))
			continue
		}
		id, message, _ := strings.Cut(rest, " ")
		byInput[id] = append(byInput[id], message)
	}

	return out, byInput
}
