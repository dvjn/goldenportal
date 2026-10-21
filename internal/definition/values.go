package definition

import (
	"errors"
	"fmt"

	validator "github.com/santhosh-tekuri/jsonschema/v6"
)

var jsonSchemaFormats = map[Format]string{
	FormatURL:      "uri",
	FormatEmail:    "email",
	FormatDate:     "date",
	FormatDateTime: "date-time",
}

// valueSchema is the JSON Schema that an entity's value of the property
// must match.
func (p Property) valueSchema() map[string]any {
	scalar := map[string]any{"type": string(p.Type)}
	if p.Type == TypeArray {
		scalar["type"] = string(TypeString)
	}
	if format, ok := jsonSchemaFormats[p.Format]; ok {
		scalar["format"] = format
	}
	if len(p.Enum) > 0 {
		values := make([]any, 0, len(p.Enum))
		for _, value := range p.Enum {
			values = append(values, value)
		}
		scalar["enum"] = values
	}
	if p.Type == TypeArray {
		return map[string]any{"type": "array", "items": scalar}
	}

	return scalar
}

// Check returns why the property does not accept the value, or nil.
func (p Property) Check(value any) error {
	compiler := validator.NewCompiler()
	compiler.AssertFormat()
	if err := compiler.AddResource("property.json", p.valueSchema()); err != nil {
		return err
	}
	schema, err := compiler.Compile("property.json")
	if err != nil {
		return err
	}
	var invalid *validator.ValidationError
	if err := schema.Validate(value); errors.As(err, &invalid) {
		return errors.New(firstCause(invalid).ErrorKind.LocalizedString(printer))
	}

	return nil
}

func firstCause(err *validator.ValidationError) *validator.ValidationError {
	for len(err.Causes) > 0 {
		err = err.Causes[0]
	}

	return err
}

// CheckValues returns why each property does not accept its value, by
// property key. A nil value means the entity has no value for the property.
func (b *Blueprint) CheckValues(values map[string]any) map[string]string {
	problems := map[string]string{}
	for key, value := range values {
		property, ok := b.Properties.Get(key)
		switch {
		case !ok:
			problems[key] = fmt.Sprintf("%s is not a property of %s", key, b.Title)
		case value == nil:
		default:
			if err := property.Check(value); err != nil {
				problems[key] = err.Error()
			}
		}
	}

	return problems
}
