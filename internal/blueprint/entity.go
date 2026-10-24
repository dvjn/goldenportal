package blueprint

import (
	"fmt"
	"math"
	"net/mail"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/itchyny/gojq"
)

// Entity is an entity's data. Integrations write relations among the
// properties; ValidateEntity moves them to Relations, which are stored apart
// so lookups across entities stay cheap. Values are as decoded from JSON:
// numbers are float64.
type Entity struct {
	Identifier string         `json:"identifier"`
	Title      string         `json:"title"`
	Properties map[string]any `json:"properties"`
	Relations  map[string]any `json:"relations"`
}

// MaxEntityIdentifier caps entity identifiers, which come from other systems.
const MaxEntityIdentifier = 255

// ValidateEntity checks an entity against the blueprint and returns it
// cleaned up: nulls dropped, the title defaulted and relations moved to
// Relations as a string or a list of strings. It returns every problem it finds.
func (s Spec) ValidateEntity(in Entity) (Entity, []string) {
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }

	out := Entity{Identifier: in.Identifier, Title: strings.TrimSpace(in.Title),
		Properties: map[string]any{}, Relations: map[string]any{}}
	switch {
	case strings.TrimSpace(in.Identifier) == "":
		add("identifier is missing")
	case len(in.Identifier) > MaxEntityIdentifier:
		add("identifier is longer than %d characters", MaxEntityIdentifier)
	case strings.ContainsFunc(in.Identifier, unicode.IsControl):
		add("identifier contains control characters")
	}
	if out.Title == "" {
		out.Title = in.Identifier
	}

	for key, value := range in.Properties {
		if value == nil {
			continue
		}
		prop, ok := s.property(key)
		if !ok {
			add("%s has no property %q", s.Identifier, key)
			continue
		}
		if prop.Derived() {
			add("properties.%s is worked out by the portal, so it can't be written", key)
			continue
		}
		if prop.IsRelation() {
			targets, ok := relationTargets(value, prop.Many)
			switch {
			case !ok && prop.Many:
				add("properties.%s must be a list of %s identifiers", key, prop.Relation)
			case !ok:
				add("properties.%s must be a %s identifier", key, prop.Relation)
			case len(targets) == 0:
			case prop.Many:
				out.Relations[key] = targets
			default:
				out.Relations[key] = targets[0]
			}
			continue
		}
		for _, problem := range checkValue(prop.Type, prop.Format, prop.Enum, prop.Items, value) {
			add("properties.%s %s", key, problem)
		}
		if text, ok := value.(string); ok && prop.Type == "string" {
			if prop.Pattern != "" && !regexp.MustCompile(prop.Pattern).MatchString(text) {
				add("properties.%s doesn't match the pattern %s", key, prop.Pattern)
			}
			length := len([]rune(text))
			if prop.MinLength != nil && length < *prop.MinLength {
				add("properties.%s is shorter than %d characters", key, *prop.MinLength)
			}
			if prop.MaxLength != nil && length > *prop.MaxLength {
				add("properties.%s is longer than %d characters", key, *prop.MaxLength)
			}
		}
		if number, ok := value.(float64); ok {
			if prop.Minimum != nil && number < *prop.Minimum {
				add("properties.%s is less than %v", key, *prop.Minimum)
			}
			if prop.Maximum != nil && number > *prop.Maximum {
				add("properties.%s is more than %v", key, *prop.Maximum)
			}
		}
		out.Properties[key] = value
	}
	for _, prop := range s.Properties {
		_, stored := out.Properties[prop.ID]
		_, related := out.Relations[prop.ID]
		if prop.Required && !prop.Derived() && !stored && !related {
			add("properties.%s is required", prop.ID)
		}
	}

	slices.Sort(problems)

	return out, problems
}

// Compute evaluates the computed properties against an entity. A value
// that fails or doesn't match its type is left out, with the reason.
func (s Spec) Compute(entity Entity) (map[string]any, []string) {
	values := map[string]any{}
	var problems []string

	properties := make(map[string]any, len(entity.Properties)+len(entity.Relations))
	for key, value := range entity.Properties {
		properties[key] = value
	}
	for key, value := range relationsForJQ(entity.Relations) {
		properties[key] = value
	}
	input := map[string]any{"identifier": entity.Identifier, "title": entity.Title, "properties": properties}
	for _, prop := range s.Properties {
		if prop.Compute == "" {
			continue
		}
		value, err := evaluate(prop.Compute, input)
		if err != nil {
			problems = append(problems, fmt.Sprintf("properties.%s: %v", prop.ID, err))
			continue
		}
		if value == nil {
			continue
		}
		if wrong := checkValue(prop.Type, prop.Format, nil, prop.Items, value); len(wrong) > 0 {
			problems = append(problems, fmt.Sprintf("properties.%s %s", prop.ID, wrong[0]))
			continue
		}
		values[prop.ID] = value
	}

	return values, problems
}

// evaluate runs a jq expression and returns its first result.
func evaluate(expression string, input any) (any, error) {
	query, err := gojq.Parse(expression)
	if err != nil {
		return nil, err
	}
	iter := query.Run(input)
	value, ok := iter.Next()
	if !ok {
		return nil, nil
	}
	if err, isErr := value.(error); isErr {
		return nil, err
	}

	return normalise(value), nil
}

// normalise turns gojq's ints into float64, as JSON decoding gives.
func normalise(value any) any {
	switch v := value.(type) {
	case int:
		return float64(v)
	case []any:
		for i := range v {
			v[i] = normalise(v[i])
		}
	case map[string]any:
		for key := range v {
			v[key] = normalise(v[key])
		}
	}

	return value
}

// relationsForJQ gives gojq values it understands: []any, not []string.
func relationsForJQ(relations map[string]any) map[string]any {
	out := make(map[string]any, len(relations))
	for key, value := range relations {
		if targets, ok := value.([]string); ok {
			list := make([]any, len(targets))
			for i, target := range targets {
				list[i] = target
			}
			value = list
		}
		out[key] = value
	}

	return out
}

func relationTargets(value any, many bool) ([]string, bool) {
	if !many {
		target, ok := value.(string)
		if !ok {
			return nil, false
		}
		if target == "" {
			return nil, true
		}
		return []string{target}, true
	}

	var items []any
	switch v := value.(type) {
	case []any:
		items = v
	case []string:
		for _, target := range v {
			items = append(items, target)
		}
	default:
		return nil, false
	}
	targets := []string{}
	for _, item := range items {
		target, ok := item.(string)
		if !ok {
			return nil, false
		}
		if target != "" && !slices.Contains(targets, target) {
			targets = append(targets, target)
		}
	}

	return targets, true
}

// checkValue checks a value against a type, format and enum, returning what's wrong.
func checkValue(valueType, format string, enum []any, items *Items, value any) []string {
	switch valueType {
	case "string":
		text, ok := value.(string)
		if !ok {
			return []string{"must be a string"}
		}
		if problem := checkFormat(format, text); problem != "" {
			return []string{problem}
		}
	case "number", "integer":
		number, ok := value.(float64)
		if !ok {
			return []string{"must be a number"}
		}
		if valueType == "integer" && number != math.Trunc(number) {
			return []string{"must be a whole number"}
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return []string{"must be true or false"}
		}
	case "object":
		if _, ok := value.(map[string]any); !ok {
			return []string{"must be an object"}
		}
	case "array":
		list, ok := value.([]any)
		if !ok {
			return []string{"must be a list"}
		}
		if items == nil {
			return nil
		}
		var problems []string
		for i, item := range list {
			for _, problem := range checkValue(items.Type, items.Format, items.Enum, nil, item) {
				problems = append(problems, fmt.Sprintf("item %d %s", i+1, problem))
			}
		}
		return problems
	}
	if len(enum) > 0 && !slices.Contains(enum, value) {
		return []string{fmt.Sprintf("must be one of %v", enum)}
	}

	return nil
}

func checkFormat(format, text string) string {
	switch format {
	case "date":
		if _, err := time.Parse(time.DateOnly, text); err != nil {
			return "must be a date like 2026-10-25"
		}
	case "date-time":
		if _, err := time.Parse(time.RFC3339, text); err != nil {
			return "must be a date and time like 2026-10-25T14:30:00Z"
		}
	case "url":
		if u, err := url.Parse(text); err != nil || u.Scheme == "" || u.Host == "" {
			return "must be an absolute URL"
		}
	case "email":
		if _, err := mail.ParseAddress(text); err != nil {
			return "must be an email address"
		}
	}

	return ""
}
