package definition

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"sync"

	"github.com/invopop/jsonschema"
	orderedmap "github.com/pb33f/ordered-map/v2"
	validator "github.com/santhosh-tekuri/jsonschema/v6"
)

const schemaID = "https://goldport.dev/schemas/definition.json"

func newReflector() *jsonschema.Reflector {
	return &jsonschema.Reflector{Anonymous: true, Namer: definitionName}
}

// definitionName names Map[pkg.Property] MapOfProperty, as $defs keys must
// survive a JSON pointer.
func definitionName(t reflect.Type) string {
	name := t.Name()
	open := strings.IndexByte(name, '[')
	if open < 0 {
		return name
	}
	argument := name[open+1 : len(name)-1]
	argument = argument[strings.LastIndexByte(argument, '.')+1:]

	return name[:open] + "Of" + strings.ToUpper(argument[:1]) + argument[1:]
}

// Schema is the JSON Schema of one definition document of any kind. The kind
// field picks the schema the rest of the document follows.
func Schema() *jsonschema.Schema {
	kindNames := make([]any, 0, len(kinds))
	root := &jsonschema.Schema{
		Version:     jsonschema.Version,
		ID:          schemaID,
		Title:       "GoldPort definition",
		Type:        "object",
		Required:    []string{"kind"},
		Definitions: jsonschema.Definitions{},
	}
	for _, spec := range kinds {
		kindNames = append(kindNames, string(spec.kind))
		reflected := newReflector().Reflect(spec.new())
		for name, definition := range reflected.Definitions {
			root.Definitions[name] = definition
		}
		root.AllOf = append(root.AllOf, &jsonschema.Schema{
			If: &jsonschema.Schema{
				Properties: properties("kind", &jsonschema.Schema{Const: string(spec.kind)}),
				Required:   []string{"kind"},
			},
			Then: &jsonschema.Schema{Ref: reflected.Ref},
		})
	}
	root.Properties = properties("kind", &jsonschema.Schema{Type: "string", Enum: kindNames})

	return root
}

func properties(name string, schema *jsonschema.Schema) *orderedmap.OrderedMap[string, *jsonschema.Schema] {
	built := jsonschema.NewProperties()
	built.Set(name, schema)

	return built
}

var compiled = sync.OnceValues(func() ([]byte, *validator.Schema) {
	data, err := json.Marshal(Schema())
	if err != nil {
		panic(err)
	}
	document, err := validator.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		panic(err)
	}
	compiler := validator.NewCompiler()
	compiler.AssertFormat()
	if err := compiler.AddResource(schemaID, document); err != nil {
		panic(err)
	}

	return data, compiler.MustCompile(schemaID)
})

// SchemaJSON is the schema that the API serves to editors.
func SchemaJSON() []byte {
	data, _ := compiled()
	return data
}
