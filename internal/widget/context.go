package widget

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	entityName  = regexp.MustCompile(`^\$entity\.[a-zA-Z][a-zA-Z0-9_]{0,62}$`)
	relativeDay = regexp.MustCompile(`^\$today([+-][0-9]+)d$`)
)

func dayOffset(name string) (int, error) {
	if name == "$today" {
		return 0, nil
	}
	match := relativeDay.FindStringSubmatch(name)
	if match != nil {
		days, err := strconv.Atoi(match[1])
		if err == nil && days >= -365000 && days <= 365000 {
			return days, nil
		}
	}
	return 0, fmt.Errorf("invalid date variable %s; use $today or $today±<n>d (up to 365000 days)", name)
}

func lookup(values Values, name string) ([]string, bool) {
	if strings.HasPrefix(name, "$today") {
		days, err := dayOffset(name)
		base := values["$today"]
		if err == nil && len(base) == 1 {
			date, err := time.Parse(time.DateOnly, base[0])
			if err == nil {
				return []string{date.AddDate(0, 0, days).Format(time.DateOnly)}, true
			}
		}
	}
	value, set := values[name]
	return value, set
}

func SetToday(values Values, now time.Time) {
	values["$today"] = []string{now.UTC().Format(time.DateOnly)}
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
	eachText(w.Filter, add)
	for _, text := range []string{w.Title, w.Description, w.Text} {
		for _, match := range tokenPattern.FindAllStringSubmatch(text, -1) {
			add(strings.TrimSpace(match[1]))
		}
	}
	return refs
}
