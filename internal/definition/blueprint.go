package definition

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/invopop/jsonschema"
)

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

const (
	SortTitle      = "title"
	SortUpdatedAt  = "updatedAt"
	SortAscending  = "asc"
	SortDescending = "desc"

	maxPathHops        = 8
	defaultColumnCount = 5
)

// Icons are the icon names a blueprint can use. The UI draws each one.
var Icons = []string{
	"box", "boxes", "server", "database", "cloud", "globe", "network", "cpu",
	"code", "git-branch", "package", "layers", "component", "app-window", "workflow", "rocket",
	"users", "user", "building", "briefcase", "shield", "key-round", "book-open", "file-text",
	"folder-git", "git-fork", "git-pull-request", "shield-check", "gauge", "key", "link",
	"layout-dashboard", "award",
}

// EnumColors are the colors an enum value can show in.
var EnumColors = []string{"gray", "blue", "green", "yellow", "orange", "red", "purple", "teal"}

// Displays are the ways the portal can show a property value. Which ones a
// property takes depends on its type, format and enum.
var Displays = []string{
	"text", "mono", "badge", "status", "relative", "absolute", "link", "email", "avatar", "icon", "number", "list", "json",
}

type Blueprint struct {
	Header
	Icon       string        `json:"icon,omitempty"`
	Properties Map[Property] `json:"properties,omitzero" jsonschema_description:"The values an entity of this blueprint holds, by key."`
	Relations  Map[Relation] `json:"relations,omitzero" jsonschema_description:"Links from an entity of this blueprint to entities of other blueprints, by key."`
	Derived    Map[Derived]  `json:"derived,omitzero" jsonschema_description:"Values the portal reads from related entities, by key."`
	Display    Display       `json:"display,omitzero"`
	Table      Table         `json:"table,omitzero"`
}

// Property is one value an entity holds. An array holds strings; format and
// enum then apply to each item.
type Property struct {
	Title       string            `json:"title" jsonschema:"minLength=1"`
	Description string            `json:"description,omitempty"`
	Type        PropertyType      `json:"type" jsonschema:"enum=string,enum=number,enum=boolean,enum=array,enum=object"`
	Format      Format            `json:"format,omitempty" jsonschema:"enum=url,enum=email,enum=date,enum=date-time"`
	Enum        []string          `json:"enum,omitempty" jsonschema:"minItems=1,uniqueItems=true" jsonschema_description:"The only values the property accepts."`
	Display     string            `json:"display,omitempty" jsonschema_description:"How the portal shows the value. The type, format and enum decide which choices apply."`
	EnumColors  map[string]string `json:"enumColors,omitempty" jsonschema_description:"A color per enum value."`
	EnumLabels  map[string]string `json:"enumLabels,omitempty" jsonschema_description:"A label per enum value, shown in place of the value."`
	LinkLabel   string            `json:"linkLabel,omitempty" jsonschema_description:"The text of the link a url property shows, in place of the url."`
	Hidden      bool              `json:"hidden,omitempty" jsonschema_description:"Keeps the property out of tables and entity pages."`
	Sortable    *bool             `json:"sortable,omitempty" jsonschema_description:"Whether people can sort the entity table by the property. Defaults to true for string, number and boolean properties."`
	EmptyLabel  string            `json:"emptyLabel,omitempty" jsonschema_description:"What the portal shows when an entity has no value."`
}

type Relation struct {
	Title        string `json:"title" jsonschema:"minLength=1"`
	Description  string `json:"description,omitempty"`
	Target       string `json:"target" jsonschema_description:"The identifier of the blueprint the relation links to."`
	Many         bool   `json:"many,omitempty" jsonschema_description:"Whether an entity links to more than one target."`
	Inverse      string `json:"inverse,omitempty" jsonschema_description:"A key on the target blueprint for the entities that link to it, usable in its columns, filters and derived paths."`
	InverseTitle string `json:"inverseTitle,omitempty" jsonschema_description:"The title of the list of linking entities on the target's page."`
}

// Derived is a value read through relations: the entities a path reaches,
// or a property of those entities when the path ends in one.
type Derived struct {
	Title       string `json:"title,omitempty" jsonschema_description:"Defaults to the title of the property the path ends in."`
	Description string `json:"description,omitempty"`
	Path        string `json:"path" jsonschema_description:"Relation keys, declared or inverse, separated by dots, optionally ending in a property of the last blueprint reached, such as pipeline.status."`
	Display     string `json:"display,omitempty" jsonschema_description:"How the portal shows the value, as for the property the path ends in."`
	EmptyLabel  string `json:"emptyLabel,omitempty" jsonschema_description:"What the portal shows when the path reaches no value."`
}

// Display holds hints for how the portal shows one entity.
type Display struct {
	Subtitle string         `json:"subtitle,omitempty" jsonschema_description:"A string property shown under the entity title."`
	Badges   []string       `json:"badges,omitempty" jsonschema:"uniqueItems=true" jsonschema_description:"Enum or boolean properties shown as badges next to the entity title."`
	Groups   []DisplayGroup `json:"groups,omitempty" jsonschema_description:"Sections of the entity page. Properties in no group show under Details."`
}

type DisplayGroup struct {
	Title      string   `json:"title" jsonschema:"minLength=1"`
	Properties []string `json:"properties" jsonschema:"minItems=1,uniqueItems=true" jsonschema_description:"Property and derived keys, in order."`
}

// Table holds hints for the entity table of the blueprint.
type Table struct {
	Columns []string  `json:"columns,omitempty" jsonschema:"uniqueItems=true" jsonschema_description:"Property, relation, inverse and derived keys the table shows after the title. Defaults to the first five properties and every single relation."`
	Sort    TableSort `json:"sort,omitzero"`
	Filters []string  `json:"filters,omitempty" jsonschema:"uniqueItems=true" jsonschema_description:"Keys people can filter the table by: relations, inverses, enum, boolean, number and date properties, and derived values. Defaults to the single relations and the enum and boolean properties."`
}

type TableSort struct {
	By        string `json:"by" jsonschema_description:"title, updatedAt, or a string, number or boolean property or derived value."`
	Direction string `json:"direction,omitempty" jsonschema:"enum=asc,enum=desc"`
}

func (Blueprint) JSONSchemaExtend(schema *jsonschema.Schema) {
	extendHeader(schema, KindBlueprint)
	schema.Properties.Set("icon", &jsonschema.Schema{Type: "string", Enum: anyOf(Icons)})
}

func (Property) JSONSchemaExtend(schema *jsonschema.Schema) {
	display, _ := schema.Properties.Get("display")
	display.Enum = anyOf(Displays)
	colors, _ := schema.Properties.Get("enumColors")
	colors.AdditionalProperties = &jsonschema.Schema{Type: "string", Enum: anyOf(EnumColors)}
	labels, _ := schema.Properties.Get("enumLabels")
	labels.AdditionalProperties = &jsonschema.Schema{Type: "string", MinLength: new(uint64(1))}
}

func (Relation) JSONSchemaExtend(schema *jsonschema.Schema) {
	inverse, _ := schema.Properties.Get("inverse")
	inverse.Pattern = keyPattern.String()
}

func (Derived) JSONSchemaExtend(schema *jsonschema.Schema) {
	path, _ := schema.Properties.Get("path")
	key := strings.Trim(keyPattern.String(), "^$")
	path.Pattern = "^" + key + `(\.` + key + ")*$"
	display, _ := schema.Properties.Get("display")
	display.Enum = anyOf(Displays)
}

func anyOf(values []string) []any {
	items := make([]any, 0, len(values))
	for _, value := range values {
		items = append(items, value)
	}

	return items
}

// DisplayChoices are the displays the property takes; the first is the
// default.
func (p Property) DisplayChoices() []string {
	switch {
	case p.Type == TypeString && len(p.Enum) > 0:
		return []string{"badge", "text", "status"}
	case p.Type == TypeString && p.Format == FormatURL:
		return []string{"link", "text"}
	case p.Type == TypeString && p.Format == FormatEmail:
		return []string{"email", "avatar", "text"}
	case p.Type == TypeString && (p.Format == FormatDateTime || p.Format == FormatDate):
		return []string{"relative", "absolute"}
	case p.Type == TypeString:
		return []string{"text", "mono", "badge"}
	case p.Type == TypeNumber:
		return []string{"number"}
	case p.Type == TypeBoolean:
		return []string{"icon", "badge", "text"}
	case p.Type == TypeArray:
		return []string{"list", "json"}
	default:
		return []string{"json"}
	}
}

// DisplayOrDefault is how the portal shows the property.
func (p Property) DisplayOrDefault() string {
	return cmp.Or(p.Display, p.DisplayChoices()[0])
}

func (p Property) scalar() bool {
	return p.Type == TypeString || p.Type == TypeNumber || p.Type == TypeBoolean
}

// IsSortable reports whether a table can sort by the property.
func (p Property) IsSortable() bool {
	if p.Sortable != nil {
		return *p.Sortable && p.scalar()
	}

	return p.scalar()
}

// Inverse is a relation of another blueprint seen from its target, under
// the key the relation's inverse names.
type Inverse struct {
	Key      string
	Title    string
	Source   string
	Relation string
}

// Inverses are the inverse keys that relations of the set declare on the
// blueprint, in the order of the set.
func (s Set) Inverses(blueprint string) []Inverse {
	var inverses []Inverse
	for _, source := range s.Blueprints() {
		for key, relation := range source.Relations.All() {
			if relation.Target == blueprint && relation.Inverse != "" {
				inverses = append(inverses, Inverse{
					Key:      relation.Inverse,
					Title:    cmp.Or(relation.InverseTitle, source.Title),
					Source:   source.Identifier,
					Relation: key,
				})
			}
		}
	}

	return inverses
}

func (s Set) inverse(blueprint, key string) (Inverse, bool) {
	inverses := s.Inverses(blueprint)
	index := slices.IndexFunc(inverses, func(inverse Inverse) bool { return inverse.Key == key })
	if index < 0 {
		return Inverse{}, false
	}

	return inverses[index], true
}

// Hop is one step of a path: along a relation from its source, or back
// along it from its target when Inverse is set. Relation is the key on the
// source blueprint either way.
type Hop struct {
	Relation string
	Inverse  bool
	From     string
	To       string
}

// Path is where a field's values come from: the hops from the entity, then
// Property of the entities reached. An empty Property means the field lists
// the entities reached.
type Path struct {
	Hops     []Hop
	Property string
}

// Target is the blueprint whose entities hold the values.
func (p Path) Target(start string) string {
	if len(p.Hops) == 0 {
		return start
	}

	return p.Hops[len(p.Hops)-1].To
}

// ResolvePath reads a dotted path of relation and inverse keys from the
// start blueprint, which may end in a property of the last blueprint
// reached.
func (s Set) ResolvePath(start, dotted string) (Path, error) {
	segments := strings.Split(dotted, ".")
	current := start
	var resolved Path
	for index, segment := range segments {
		blueprint, ok := s.Blueprint(current)
		if !ok {
			return Path{}, fmt.Errorf("no blueprint has the identifier %q", current)
		}
		if relation, ok := blueprint.Relations.Get(segment); ok {
			resolved.Hops = append(resolved.Hops, Hop{Relation: segment, From: current, To: relation.Target})
			current = relation.Target
			continue
		}
		if inverse, ok := s.inverse(current, segment); ok {
			resolved.Hops = append(resolved.Hops, Hop{Relation: inverse.Relation, Inverse: true, From: current, To: inverse.Source})
			current = inverse.Source
			continue
		}
		if _, ok := blueprint.Properties.Get(segment); ok && index == len(segments)-1 && len(resolved.Hops) > 0 {
			resolved.Property = segment
			break
		}
		return Path{}, fmt.Errorf("%q is not a relation or inverse of %s", segment, blueprint.Title)
	}
	if len(resolved.Hops) > maxPathHops {
		return Path{}, fmt.Errorf("the path follows %d relations, at most %d are allowed", len(resolved.Hops), maxPathHops)
	}

	return resolved, nil
}

// AttributeSource says what an attribute key of a blueprint names.
type AttributeSource string

const (
	AttributeProperty AttributeSource = "property"
	AttributeRelation AttributeSource = "relation"
	AttributeInverse  AttributeSource = "inverse"
	AttributeDerived  AttributeSource = "derived"
)

// Attribute is a key a blueprint's table and entity page can show. Property is
// the property the values come from, nil when the field lists entities.
type Attribute struct {
	Key      string
	Source   AttributeSource
	Title    string
	Path     Path
	Property *Property
}

// FilterKind names the filter a field takes, or "" when it takes none.
type FilterKind string

const (
	FilterEnum     FilterKind = "enum"
	FilterBoolean  FilterKind = "boolean"
	FilterNumber   FilterKind = "number"
	FilterDate     FilterKind = "date"
	FilterRelation FilterKind = "relation"
)

func (f Attribute) FilterKind() FilterKind {
	if f.Property == nil {
		return FilterRelation
	}
	switch {
	case f.Property.Type == TypeString && len(f.Property.Enum) > 0:
		return FilterEnum
	case f.Property.Type == TypeString && (f.Property.Format == FormatDate || f.Property.Format == FormatDateTime):
		return FilterDate
	case f.Property.Type == TypeBoolean:
		return FilterBoolean
	case f.Property.Type == TypeNumber:
		return FilterNumber
	}

	return ""
}

func (f Attribute) Sortable() bool {
	return f.Property != nil && f.Property.IsSortable()
}

// Attribute resolves a key of the blueprint: a property, a relation, an inverse
// or a derived value. Hidden properties are not fields.
func (s Set) Attribute(blueprint *Blueprint, key string) (Attribute, bool) {
	if property, ok := blueprint.Properties.Get(key); ok {
		if property.Hidden {
			return Attribute{}, false
		}
		return Attribute{Key: key, Source: AttributeProperty, Title: property.Title, Path: Path{Property: key}, Property: &property}, true
	}
	if relation, ok := blueprint.Relations.Get(key); ok {
		hop := Hop{Relation: key, From: blueprint.Identifier, To: relation.Target}
		return Attribute{Key: key, Source: AttributeRelation, Title: relation.Title, Path: Path{Hops: []Hop{hop}}}, true
	}
	if inverse, ok := s.inverse(blueprint.Identifier, key); ok {
		hop := Hop{Relation: inverse.Relation, Inverse: true, From: blueprint.Identifier, To: inverse.Source}
		return Attribute{Key: key, Source: AttributeInverse, Title: inverse.Title, Path: Path{Hops: []Hop{hop}}}, true
	}
	derived, ok := blueprint.Derived.Get(key)
	if !ok {
		return Attribute{}, false
	}
	resolved, err := s.ResolvePath(blueprint.Identifier, derived.Path)
	if err != nil {
		return Attribute{}, false
	}
	field := Attribute{Key: key, Source: AttributeDerived, Title: cmp.Or(derived.Title, key), Path: resolved}
	if resolved.Property != "" {
		target, _ := s.Blueprint(resolved.Target(blueprint.Identifier))
		property, _ := target.Properties.Get(resolved.Property)
		property.Display = cmp.Or(derived.Display, property.Display)
		property.Hidden = false
		property.EmptyLabel = cmp.Or(derived.EmptyLabel, property.EmptyLabel)
		field.Title = cmp.Or(derived.Title, property.Title, key)
		field.Property = &property
	}

	return field, true
}

// TableOf is the blueprint's table with the defaults filled in.
func (s Set) TableOf(blueprint *Blueprint) Table {
	table := Table{
		Columns: slices.Clone(blueprint.Table.Columns),
		Sort:    blueprint.Table.Sort,
		Filters: slices.Clone(blueprint.Table.Filters),
	}
	if table.Columns == nil {
		table.Columns = defaultColumns(blueprint)
	}
	table.Sort.By = cmp.Or(table.Sort.By, SortTitle)
	table.Sort.Direction = cmp.Or(table.Sort.Direction, SortAscending)
	if table.Filters == nil {
		table.Filters = defaultFilters(blueprint)
	}

	return table
}

func defaultColumns(blueprint *Blueprint) []string {
	columns := []string{}
	for key, property := range blueprint.Properties.All() {
		if len(columns) < defaultColumnCount && !property.Hidden {
			columns = append(columns, key)
		}
	}
	for key, relation := range blueprint.Relations.All() {
		if !relation.Many {
			columns = append(columns, key)
		}
	}

	return columns
}

func defaultFilters(blueprint *Blueprint) []string {
	filters := []string{}
	for key, property := range blueprint.Properties.All() {
		if !property.Hidden && (property.Type == TypeString && len(property.Enum) > 0 || property.Type == TypeBoolean) {
			filters = append(filters, key)
		}
	}
	for key, relation := range blueprint.Relations.All() {
		if !relation.Many {
			filters = append(filters, key)
		}
	}

	return filters
}

func (b *Blueprint) check(c *checker) {
	for key, property := range b.Properties.All() {
		checkProperty(c, b, key, property)
	}
	for key, relation := range b.Relations.All() {
		if _, ok := b.Properties.Get(key); ok {
			c.report(b, path("relations", key), "%q is already a property of this blueprint", key)
		}
		target, ok := c.set.Blueprint(relation.Target)
		if !ok {
			c.report(b, path("relations", key, "target"), "no blueprint has the identifier %q", relation.Target)
			continue
		}
		if relation.Inverse != "" {
			b.checkInverse(c, key, relation, target)
		}
	}
	for key, derived := range b.Derived.All() {
		b.checkDerived(c, key, derived)
	}
	b.checkDisplay(c)
	b.checkTable(c)
}

func checkProperty(c *checker, b *Blueprint, key string, property Property) {
	at := path("properties", key)
	if property.Type != TypeString && property.Type != TypeArray && (property.Format != "" || len(property.Enum) > 0) {
		c.report(b, at, "format and enum apply only to string and array properties")
	}
	if property.Format != "" && len(property.Enum) > 0 {
		c.report(b, at, "a property takes either a format or an enum, not both")
	}
	if choices := property.DisplayChoices(); property.Display != "" && !slices.Contains(choices, property.Display) {
		c.report(b, append(at, "display"), "a %s takes one of %s", describeProperty(property), strings.Join(choices, ", "))
	}
	if property.LinkLabel != "" && property.Format != FormatURL {
		c.report(b, append(at, "linkLabel"), "linkLabel applies only to url properties")
	}
	for _, field := range []struct {
		name   string
		values map[string]string
	}{{"enumColors", property.EnumColors}, {"enumLabels", property.EnumLabels}} {
		for _, value := range slices.Sorted(maps.Keys(field.values)) {
			if !slices.Contains(property.Enum, value) {
				c.report(b, append(at, field.name, value), "%q is not a value of the enum", value)
			}
		}
	}
	if property.Sortable != nil && *property.Sortable && !property.scalar() {
		c.report(b, append(at, "sortable"), "only string, number and boolean properties are sortable")
	}
}

func describeProperty(property Property) string {
	switch {
	case len(property.Enum) > 0:
		return "property with an enum"
	case property.Format != "":
		return string(property.Format) + " property"
	}

	return string(property.Type) + " property"
}

func (b *Blueprint) checkInverse(c *checker, key string, relation Relation, target *Blueprint) {
	at := path("relations", key, "inverse")
	if target.owns(relation.Inverse) {
		c.report(b, at, "%s already has a property, relation or derived value %q", target.Title, relation.Inverse)
		return
	}
	for _, other := range c.set.Inverses(target.Identifier) {
		if other.Key == relation.Inverse && (other.Source != b.Identifier || other.Relation != key) {
			c.report(b, at, "%s.%s already declares the inverse %q on %s", other.Source, other.Relation, relation.Inverse, target.Title)
			return
		}
	}
}

// owns reports whether the blueprint itself declares the key.
func (b *Blueprint) owns(key string) bool {
	_, property := b.Properties.Get(key)
	_, relation := b.Relations.Get(key)
	_, derived := b.Derived.Get(key)

	return property || relation || derived
}

func (b *Blueprint) checkDerived(c *checker, key string, derived Derived) {
	at := path("derived", key)
	if _, ok := b.Properties.Get(key); ok {
		c.report(b, at, "%q is already a property of this blueprint", key)
	}
	if _, ok := b.Relations.Get(key); ok {
		c.report(b, at, "%q is already a relation of this blueprint", key)
	}
	if _, ok := c.set.inverse(b.Identifier, key); ok {
		c.report(b, at, "%q is already an inverse relation of this blueprint", key)
	}
	resolved, err := c.set.ResolvePath(b.Identifier, derived.Path)
	if err != nil {
		c.report(b, append(at, "path"), "%s", err)
		return
	}
	if derived.Display == "" {
		return
	}
	if resolved.Property == "" {
		c.report(b, append(at, "display"), "display applies only to a path that ends in a property")
		return
	}
	target, _ := c.set.Blueprint(resolved.Target(b.Identifier))
	property, _ := target.Properties.Get(resolved.Property)
	if choices := property.DisplayChoices(); !slices.Contains(choices, derived.Display) {
		c.report(b, append(at, "display"), "a %s takes one of %s", describeProperty(property), strings.Join(choices, ", "))
	}
}

func (b *Blueprint) checkDisplay(c *checker) {
	if subtitle := b.Display.Subtitle; subtitle != "" {
		if property, ok := b.Properties.Get(subtitle); !ok || property.Hidden || property.Type != TypeString {
			c.report(b, path("display", "subtitle"), "%q is not a visible string property of this blueprint", subtitle)
		}
	}
	for index, badge := range b.Display.Badges {
		property, ok := b.Properties.Get(badge)
		if !ok || property.Hidden || (len(property.Enum) == 0 && property.Type != TypeBoolean) {
			c.report(b, path("display", "badges", index), "%q is not a visible enum or boolean property of this blueprint", badge)
		}
	}
	grouped := map[string]bool{}
	for groupIndex, group := range b.Display.Groups {
		for index, key := range group.Properties {
			at := path("display", "groups", groupIndex, "properties", index)
			field, ok := c.set.Attribute(b, key)
			switch {
			case !ok || field.Source != AttributeProperty && field.Source != AttributeDerived:
				c.report(b, at, "%q is not a visible property or derived value of this blueprint", key)
			case grouped[key]:
				c.report(b, at, "%q is already in another group", key)
			}
			grouped[key] = true
		}
	}
}

func (b *Blueprint) checkTable(c *checker) {
	for index, column := range b.Table.Columns {
		if _, ok := c.set.Attribute(b, column); !ok {
			c.report(b, path("table", "columns", index), "%q is not a visible property, relation, inverse or derived value of this blueprint", column)
		}
	}
	if by := b.Table.Sort.By; by != "" && by != SortTitle && by != SortUpdatedAt {
		if field, ok := c.set.Attribute(b, by); !ok || !field.Sortable() {
			c.report(b, path("table", "sort", "by"), "sort by title, updatedAt, or a sortable string, number or boolean property or derived value, not %q", by)
		}
	}
	for index, key := range b.Table.Filters {
		if field, ok := c.set.Attribute(b, key); !ok || field.FilterKind() == "" {
			c.report(b, path("table", "filters", index), "%q is not a relation, inverse, or enum, boolean, number or date property or derived value of this blueprint", key)
		}
	}
}

func extendHeader(schema *jsonschema.Schema, kind Kind) {
	schema.Properties.Set("kind", &jsonschema.Schema{Type: "string", Const: string(kind)})
	identifier, _ := schema.Properties.Get("identifier")
	identifier.Pattern = identifierPattern.String()
}
