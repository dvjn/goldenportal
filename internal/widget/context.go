package widget

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"
)

var entityName = regexp.MustCompile(`^\$entity\.[a-zA-Z][a-zA-Z0-9_]{0,62}$`)

func lookup(values Values, name string) ([]string, bool) {
	value, set := values[name]
	return value, set
}

func SetToday(values Values, now time.Time) {
	values[ValueToday] = []string{now.UTC().Format(time.DateOnly)}
}

// EntityValues keeps numbers and booleans typed when used in filters.
// An absent value is set to nothing, so its condition cannot fall away.
func EntityValues(identifier, title, blueprint string, properties map[string]any, names []string) map[string][]any {
	values := map[string][]any{
		"$entity": {identifier}, "$entity.identifier": {identifier},
		"$entity.title": {title}, "$entity.blueprint": {blueprint},
	}
	for _, name := range names {
		key := "$entity." + name
		if _, reserved := values[key]; reserved {
			continue
		}
		value := properties[name]
		switch v := value.(type) {
		case nil:
			values[key] = []any{}
		case []any:
			values[key] = append([]any{}, v...)
		case []string:
			items := make([]any, len(v))
			for i, item := range v {
				items[i] = item
			}
			values[key] = items
		default:
			values[key] = []any{value}
		}
	}
	return values
}

func TextValues(data map[string][]any) Values {
	values := Values{}
	for name, items := range data {
		texts := []string{}
		for _, item := range items {
			if text, ok := item.(string); ok {
				texts = append(texts, text)
			} else if item != nil {
				if raw, err := json.Marshal(item); err == nil {
					texts = append(texts, string(raw))
				}
			}
		}
		values[name] = texts
	}
	return values
}

func EntityRefs(w Widget) []string {
	var refs []string
	add := func(name string) {
		if strings.HasPrefix(name, "$entity.") {
			refs = append(refs, strings.TrimPrefix(name, "$entity."))
		}
	}
	for _, f := range w.Filters() {
		eachText(f, add)
	}
	for _, text := range []string{w.Title, w.Description, w.Text} {
		for _, match := range tokenPattern.FindAllStringSubmatch(text, -1) {
			add(strings.TrimSpace(match[1]))
		}
	}
	return refs
}
