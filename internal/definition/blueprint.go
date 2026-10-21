package definition

import "github.com/invopop/jsonschema"

type PropertyType string

const (
	TypeString  PropertyType = "string"
	TypeNumber  PropertyType = "number"
	TypeBoolean PropertyType = "boolean"
	TypeArray   PropertyType = "array"
	TypeObject  PropertyType = "object"
)

type Format string

const (
	FormatURL      Format = "url"
	FormatEmail    Format = "email"
	FormatDate     Format = "date"
	FormatDateTime Format = "date-time"
)

// Icons are the icon names a blueprint can use. The UI draws each one.
var Icons = []string{
	"box", "boxes", "server", "database", "cloud", "globe", "network", "cpu",
	"code", "git-branch", "package", "layers", "component", "app-window", "workflow", "rocket",
	"users", "user", "building", "briefcase", "shield", "key-round", "book-open", "file-text",
}

type Blueprint struct {
	Header
	Icon       string        `json:"icon,omitempty"`
	Properties Map[Property] `json:"properties,omitzero" jsonschema_description:"The values an entity of this blueprint holds, by key."`
	Relations  Map[Relation] `json:"relations,omitzero" jsonschema_description:"Links from an entity of this blueprint to entities of other blueprints, by key."`
	Display    Display       `json:"display,omitzero"`
}

// Property is one value an entity holds. An array holds strings; format and
// enum then apply to each item.
type Property struct {
	Title       string       `json:"title" jsonschema:"minLength=1"`
	Description string       `json:"description,omitempty"`
	Type        PropertyType `json:"type" jsonschema:"enum=string,enum=number,enum=boolean,enum=array,enum=object"`
	Format      Format       `json:"format,omitempty" jsonschema:"enum=url,enum=email,enum=date,enum=date-time"`
	Enum        []string     `json:"enum,omitempty" jsonschema:"minItems=1,uniqueItems=true" jsonschema_description:"The only values the property accepts."`
}

type Relation struct {
	Title        string `json:"title" jsonschema:"minLength=1"`
	Description  string `json:"description,omitempty"`
	Target       string `json:"target" jsonschema_description:"The identifier of the blueprint the relation links to."`
	Many         bool   `json:"many,omitempty" jsonschema_description:"Whether an entity links to more than one target."`
	InverseTitle string `json:"inverseTitle,omitempty" jsonschema_description:"The title of the list of linking entities on the target's page."`
}

// Display holds hints for how the portal shows the blueprint's entities.
type Display struct {
	Columns []string `json:"columns,omitempty" jsonschema:"maxItems=6,uniqueItems=true" jsonschema_description:"The properties the entity table shows. Defaults to the first three."`
}

func (Blueprint) JSONSchemaExtend(schema *jsonschema.Schema) {
	extendHeader(schema, KindBlueprint)
	icons := make([]any, 0, len(Icons))
	for _, icon := range Icons {
		icons = append(icons, icon)
	}
	schema.Properties.Set("icon", &jsonschema.Schema{Type: "string", Enum: icons})
}

// Columns are the properties the entity table shows.
func (b *Blueprint) Columns() []string {
	if len(b.Display.Columns) > 0 {
		return b.Display.Columns
	}
	keys := b.Properties.Keys()

	return keys[:min(len(keys), 3)]
}

func (b *Blueprint) check(c *checker) {
	for key, property := range b.Properties.All() {
		if property.Type != TypeString && property.Type != TypeArray && (property.Format != "" || len(property.Enum) > 0) {
			c.report(b, path("properties", key), "format and enum apply only to string and array properties")
		}
		if property.Format != "" && len(property.Enum) > 0 {
			c.report(b, path("properties", key), "a property takes either a format or an enum, not both")
		}
	}
	for key, relation := range b.Relations.All() {
		if _, ok := b.Properties.Get(key); ok {
			c.report(b, path("relations", key), "%q is already a property of this blueprint", key)
		}
		if _, ok := c.set.Blueprint(relation.Target); !ok {
			c.report(b, path("relations", key, "target"), "no blueprint has the identifier %q", relation.Target)
		}
	}
	for index, column := range b.Display.Columns {
		if _, ok := b.Properties.Get(column); !ok {
			c.report(b, path("display", "columns", index), "%q is not a property of this blueprint", column)
		}
	}
}

func extendHeader(schema *jsonschema.Schema, kind Kind) {
	schema.Properties.Set("kind", &jsonschema.Schema{Type: "string", Const: string(kind)})
	identifier, _ := schema.Properties.Get("identifier")
	identifier.Pattern = identifierPattern.String()
}
