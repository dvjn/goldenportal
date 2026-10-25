// Package configdoc stores the portal's YAML configuration documents, such as
// blueprints and integrations. Each document has a draft that builders edit
// and numbered versions they publish, with an impact preview in between.
package configdoc

import (
	"context"
	"slices"
)

const (
	SeverityError   = "error"
	SeverityWarning = "warning"
)

// Issue is a problem found in a document. Errors block saving and
// publishing; warnings don't.
type Issue struct {
	Severity string `json:"severity"`
	Path     string `json:"path,omitempty"`
	Line     int    `json:"line,omitempty"`
	Message  string `json:"message"`
}

// HasErrors reports whether any issue is an error.
func HasErrors(issues []Issue) bool {
	return slices.ContainsFunc(issues, func(issue Issue) bool { return issue.Severity == SeverityError })
}

// Change is one difference between two versions of a document. A breaking
// change can invalidate or drop data that depends on the document.
type Change struct {
	Section  string `json:"section"`
	ID       string `json:"identifier,omitempty"`
	Change   string `json:"change"`
	Breaking bool   `json:"breaking"`
	Detail   string `json:"detail"`
}

const (
	ChangeAdded   = "added"
	ChangeRemoved = "removed"
	ChangeChanged = "changed"
)

// Breaking reports whether any change is breaking.
func Breaking(changes []Change) bool {
	return slices.ContainsFunc(changes, func(c Change) bool { return c.Breaking })
}

// Spec is a parsed document.
type Spec interface {
	Key() string
	Meta() Meta
	// Stats are counts shown when listing documents, such as properties.
	Stats() map[string]int
}

// Meta is what every document shows about itself.
type Meta struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Icon        string `json:"icon,omitempty"`
}

// Kind describes one type of document and how to handle it.
type Kind[S Spec] struct {
	// Name is singular and used in messages and audit actions, e.g. "blueprint".
	Name string
	// Document is the kind as documents write it, e.g. "Blueprint".
	Document string
	// Plural is the API path segment, e.g. "blueprints".
	Plural string
	// Parse reads a source and checks everything that doesn't depend on other documents.
	Parse func(source string) (S, []Issue)
	// Check finds problems that depend on other documents or data.
	Check func(ctx context.Context, db DB, spec S) ([]Issue, error)
	// Diff lists what publishing next over previous changes.
	Diff func(previous, next S) []Change
	// Affected counts the data a publish would touch, for the impact preview.
	Affected func(ctx context.Context, db DB, identifier string) (int, error)
	// Published runs in the publish transaction, e.g. to drop data of removed fields.
	Published func(ctx context.Context, tx DB, previous *S, next S) error
	// Deletable returns an error when the document can't be deleted yet.
	Deletable func(ctx context.Context, db DB, identifier string) error
}

// Action names the audit action for an operation on this kind.
func (k Kind[S]) Action(operation string) string { return k.Name + "." + operation }

// Audit operations, recorded as "<kind>.<operation>".
const (
	OpDraftSave    = "draft_save"
	OpDraftDiscard = "draft_discard"
	OpPublish      = "publish"
	OpDelete       = "delete"
)

// Actions lists the audit actions of a kind.
func Actions(name string) []string {
	return []string{name + "." + OpDraftSave, name + "." + OpDraftDiscard, name + "." + OpPublish, name + "." + OpDelete}
}

// Colors are the colors the UI knows, for tiers and property values.
var Colors = []string{
	"gray", "red", "orange", "amber", "yellow", "lime", "green", "emerald", "teal", "cyan",
	"sky", "blue", "indigo", "violet", "purple", "fuchsia", "pink", "rose",
}
