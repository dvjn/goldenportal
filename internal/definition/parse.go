package definition

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
	"github.com/goccy/go-yaml/token"
	validator "github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

// Source is definitions text, YAML or JSON, with one or more documents. It
// keeps the parsed text so problems can point at a line.
type Source struct {
	Documents []Document
	bodies    map[Ref]located
}

type located struct {
	index int
	body  ast.Node
}

var printer = message.NewPrinter(language.English)

// Parse reads every document in the text and checks each against the
// schema. A document with problems is left out of Documents.
func Parse(text []byte) (Source, []Problem) {
	source := Source{bodies: map[Ref]located{}}
	file, err := parser.ParseBytes(text, 0)
	if err != nil {
		return source, []Problem{syntaxProblem(err)}
	}

	var problems []Problem
	index := 0
	for _, node := range file.Docs {
		if node.Body == nil {
			continue
		}
		index++
		document, documentProblems := parseDocument(node.Body)
		for _, problem := range documentProblems {
			problems = append(problems, at(problem, index, node.Body))
		}
		if document == nil {
			continue
		}
		if _, duplicate := source.bodies[document.Ref()]; duplicate {
			problem := Problem{Message: fmt.Sprintf("the source defines %s twice", document.Ref()), segments: path("identifier")}
			problems = append(problems, at(problem, index, node.Body))
			continue
		}
		source.bodies[document.Ref()] = located{index: index, body: node.Body}
		source.Documents = append(source.Documents, document)
	}
	if index == 0 {
		problems = append(problems, Problem{Message: "the source holds no documents"})
	}

	return source, problems
}

func parseDocument(body ast.Node) (Document, []Problem) {
	var value any
	if err := yaml.NodeToValue(body, &value, yaml.UseOrderedMap()); err != nil {
		return nil, []Problem{syntaxProblem(err)}
	}
	data, err := orderedJSON(value)
	if err != nil {
		return nil, []Problem{{Message: err.Error()}}
	}
	instance, err := validator.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return nil, []Problem{{Message: err.Error()}}
	}
	_, schema := compiled()
	var invalid *validator.ValidationError
	if err := schema.Validate(instance); errors.As(err, &invalid) {
		return nil, schemaProblems(invalid, nil)
	}
	document, err := DecodeDocument(data)
	if err != nil {
		return nil, []Problem{{Message: err.Error()}}
	}

	return document, nil
}

// schemaProblems keeps the innermost errors, which name the field at fault;
// the errors around them only say that a subschema failed. The validator
// gives an invalid key and the errors under it a wrong location, so they
// take the location of the object that holds the key.
func schemaProblems(err *validator.ValidationError, around []string) []Problem {
	location := err.InstanceLocation
	if _, keyError := err.ErrorKind.(*kind.PropertyNames); keyError || len(location) < len(around) {
		location = around
	}
	if len(err.Causes) == 0 {
		return []Problem{{Path: dotted(location), Message: err.ErrorKind.LocalizedString(printer), segments: location}}
	}
	var problems []Problem
	for _, cause := range err.Causes {
		problems = append(problems, schemaProblems(cause, location)...)
	}

	return problems
}

// Locate fills in where each problem of a document from this source is.
func (s Source) Locate(problems []Problem) []Problem {
	for index, problem := range problems {
		if where, ok := s.bodies[Ref{Kind: problem.Kind, Identifier: problem.Identifier}]; ok {
			problems[index] = at(problem, where.index, where.body)
		}
	}

	return problems
}

func at(problem Problem, index int, body ast.Node) Problem {
	problem.Document = index
	if problem.Kind == "" {
		problem.Kind, problem.Identifier = header(body)
	}
	if problem.Line == 0 {
		position := find(body, problem.segments)
		problem.Line, problem.Column = position.Line, position.Column
	}

	return problem
}

func header(body ast.Node) (Kind, string) {
	return Kind(scalar(body, "kind")), scalar(body, "identifier")
}

func scalar(body ast.Node, key string) string {
	if value, ok := child(unwrap(body), key).(ast.ScalarNode); ok {
		return value.GetToken().Value
	}

	return ""
}

// find walks the path from the body and returns where the deepest field it
// reaches is written: the value of a scalar, otherwise the key, so a missing
// field points at the object that lacks it.
func find(body ast.Node, segments []string) *token.Position {
	node, key := body, body.GetToken()
	for _, segment := range segments {
		next, nextKey := child(unwrap(node), segment), pairKey(unwrap(node), segment)
		if next == nil {
			break
		}
		node, key = next, cmp.Or(nextKey, node.GetToken())
	}
	if _, ok := unwrap(node).(ast.ScalarNode); ok {
		return node.GetToken().Position
	}

	return key.Position
}

func pairKey(node ast.Node, segment string) *token.Token {
	switch typed := node.(type) {
	case *ast.MappingNode:
		for _, pair := range typed.Values {
			if pair.Key.GetToken().Value == segment {
				return pair.Key.GetToken()
			}
		}
	case *ast.MappingValueNode:
		if typed.Key.GetToken().Value == segment {
			return typed.Key.GetToken()
		}
	}

	return nil
}

func child(node ast.Node, segment string) ast.Node {
	switch typed := node.(type) {
	case *ast.MappingNode:
		for _, pair := range typed.Values {
			if pair.Key.GetToken().Value == segment {
				return pair.Value
			}
		}
	case *ast.MappingValueNode:
		if typed.Key.GetToken().Value == segment {
			return typed.Value
		}
	case *ast.SequenceNode:
		if index, err := strconv.Atoi(segment); err == nil && index < len(typed.Values) {
			return typed.Values[index]
		}
	}

	return nil
}

func unwrap(node ast.Node) ast.Node {
	switch typed := node.(type) {
	case *ast.AnchorNode:
		return unwrap(typed.Value)
	case *ast.TagNode:
		return unwrap(typed.Value)
	}

	return node
}

func syntaxProblem(err error) Problem {
	problem := Problem{Message: yaml.FormatError(err, false, false)}
	var syntax *yaml.SyntaxError
	if errors.As(err, &syntax) && syntax.Token != nil {
		problem.Line, problem.Column = syntax.Token.Position.Line, syntax.Token.Position.Column
		problem.Message = syntax.Message
	}

	return problem
}

// orderedJSON writes a decoded YAML value as JSON, keeping the key order.
func orderedJSON(value any) ([]byte, error) {
	switch typed := value.(type) {
	case yaml.MapSlice:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			key, err := json.Marshal(fmt.Sprint(item.Key))
			if err != nil {
				return nil, err
			}
			inner, err := orderedJSON(item.Value)
			if err != nil {
				return nil, err
			}
			parts = append(parts, string(key)+":"+string(inner))
		}
		return []byte("{" + strings.Join(parts, ",") + "}"), nil
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			inner, err := orderedJSON(item)
			if err != nil {
				return nil, err
			}
			parts = append(parts, string(inner))
		}
		return []byte("[" + strings.Join(parts, ",") + "]"), nil
	}

	return json.Marshal(value)
}
