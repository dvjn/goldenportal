package definition

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Problem is one reason a set of definitions cannot be applied. Document is
// the 1-based position of the document in the source it came from, and Line
// and Column point into that source; all three are 0 for a problem in a
// document the source did not include.
type Problem struct {
	Document   int    `json:"document,omitempty"`
	Kind       Kind   `json:"kind,omitempty"`
	Identifier string `json:"identifier,omitempty"`
	Path       string `json:"path,omitempty"`
	Line       int    `json:"line,omitempty"`
	Column     int    `json:"column,omitempty"`
	Message    string `json:"message"`

	segments []string
}

// String names the document and the field, leaving out the line, which
// only means something next to the source's name.
func (p Problem) String() string {
	var where []string
	if p.Kind != "" {
		where = append(where, Ref{Kind: p.Kind, Identifier: p.Identifier}.String())
	}
	if p.Path != "" {
		where = append(where, p.Path)
	}
	if len(where) == 0 {
		return p.Message
	}

	return strings.Join(where, ": ") + ": " + p.Message
}

// path builds the location of a field from keys and indexes.
func path(segments ...any) []string {
	built := make([]string, 0, len(segments))
	for _, segment := range segments {
		built = append(built, fmt.Sprint(segment))
	}

	return built
}

// dotted writes a location as people read it: mappings[0].properties.tier.
// Keys and identifiers start with a letter, so a number is a list index.
func dotted(segments []string) string {
	var out strings.Builder
	for depth, segment := range segments {
		switch {
		case isIndex(segment):
			out.WriteString("[" + segment + "]")
		case depth > 0:
			out.WriteString("." + segment)
		default:
			out.WriteString(segment)
		}
	}

	return out.String()
}

func isIndex(segment string) bool {
	_, err := strconv.Atoi(segment)
	return err == nil
}

type checker struct {
	set      Set
	problems []Problem
}

// report records a problem in a document of the set.
func (c *checker) report(document Document, at []string, format string, args ...any) {
	ref := document.Ref()
	segments := slices.Clone(at)
	c.problems = append(c.problems, Problem{
		Kind:       ref.Kind,
		Identifier: ref.Identifier,
		Path:       dotted(segments),
		Message:    fmt.Sprintf(format, args...),
		segments:   segments,
	})
}

// Check returns every problem that the schema cannot see: references to
// blueprints that do not exist, identifiers used twice, invalid jq.
func (s Set) Check() []Problem {
	c := &checker{set: s}
	seen := map[Ref]bool{}
	for _, document := range s.Documents {
		if seen[document.Ref()] {
			c.report(document, path("identifier"), "another %s already has the identifier %q", document.Ref().Kind, document.Ref().Identifier)
			continue
		}
		seen[document.Ref()] = true
		document.check(c)
	}

	return c.problems
}
