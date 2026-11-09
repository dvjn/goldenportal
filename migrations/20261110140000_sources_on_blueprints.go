package migrations

import (
	"bytes"
	"database/sql"
	"fmt"
	"slices"

	"go.yaml.in/yaml/v3"
	"gofr.dev/pkg/gofr/migration"
)

// sourcesOnBlueprints moves each connection's mappings into the sources of
// the blueprints they write. It rewrites every stored version in place
// rather than publishing new ones, since a new version would date each
// source "now" and make the next sync read everything afresh: every version
// of a blueprint gets the sources the connections' published mappings give
// it, and every version of a connection loses its mappings.
func sourcesOnBlueprints() migration.Migrate {
	return migration.Migrate{
		UP: func(d migration.Datasource) error {
			connections, err := loadDocuments(d.SQL, "connection")
			if err != nil {
				return err
			}
			blueprints, err := loadDocuments(d.SQL, "blueprint")
			if err != nil {
				return err
			}
			if err := moveMappings(connections, blueprints); err != nil {
				return err
			}

			return saveDocuments(d.SQL, append(connections, blueprints...))
		},
	}
}

// document is a stored document's versions and draft, as text, with what
// was loaded kept to tell what changed.
type document struct {
	id         string
	identifier string
	// published is the published version, or zero when there's none.
	published int
	versions  map[int]string
	draft     string
	hasDraft  bool

	loadedVersions map[int]string
	loadedDraft    string
	hadDraft       bool
}

func loadDocuments(db migration.SQL, kind string) ([]*document, error) {
	rows, err := db.Query(`SELECT id, identifier, coalesce(published_version, 0), draft_source
		FROM config_documents WHERE kind = $1 ORDER BY identifier`, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var docs []*document
	byID := map[string]*document{}
	for rows.Next() {
		var (
			doc   document
			draft sql.NullString
		)
		if err := rows.Scan(&doc.id, &doc.identifier, &doc.published, &draft); err != nil {
			return nil, err
		}
		doc.draft, doc.hasDraft = draft.String, draft.Valid
		doc.loadedDraft, doc.hadDraft = doc.draft, doc.hasDraft
		doc.versions, doc.loadedVersions = map[int]string{}, map[int]string{}
		docs = append(docs, &doc)
		byID[doc.id] = &doc
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	versions, err := db.Query(`SELECT v.document_id, v.version, v.source FROM config_versions v
		JOIN config_documents d ON d.id = v.document_id WHERE d.kind = $1`, kind)
	if err != nil {
		return nil, err
	}
	defer versions.Close()
	for versions.Next() {
		var (
			id      string
			version int
			source  string
		)
		if err := versions.Scan(&id, &version, &source); err != nil {
			return nil, err
		}
		if doc := byID[id]; doc != nil {
			doc.versions[version], doc.loadedVersions[version] = source, source
		}
	}

	return docs, versions.Err()
}

func saveDocuments(db migration.SQL, docs []*document) error {
	for _, doc := range docs {
		for version, source := range doc.versions {
			if source == doc.loadedVersions[version] {
				continue
			}
			if _, err := db.Exec(`UPDATE config_versions SET source = $3 WHERE document_id = $1 AND version = $2`,
				doc.id, version, source); err != nil {
				return err
			}
		}
		switch {
		case doc.hasDraft && !doc.hadDraft:
			if _, err := db.Exec(`UPDATE config_documents SET draft_source = $2, draft_updated_at = now() WHERE id = $1`,
				doc.id, doc.draft); err != nil {
				return err
			}
		case doc.hasDraft && doc.draft != doc.loadedDraft:
			if _, err := db.Exec(`UPDATE config_documents SET draft_source = $2 WHERE id = $1`, doc.id, doc.draft); err != nil {
				return err
			}
		}
	}

	return nil
}

// moveMappings moves the connections' mappings into the blueprints'
// sources. Every version of a blueprint gets the published view: each
// published connection's published mappings. Its draft gets the draft view:
// each connection's draft mappings when it has a draft, else its published
// ones. A blueprint without a draft gets one, its published source with the
// draft view, only when the two views differ. Connections come in
// identifier order, and each connection's sources in mapping order.
func moveMappings(connections, blueprints []*document) error {
	published, drafted := map[string][]*yaml.Node{}, map[string][]*yaml.Node{}
	var targets []string
	for _, c := range connections {
		var publishedMappings []*yaml.Node
		if c.published != 0 {
			publishedMappings = mappingsOf(c.versions[c.published])
		}
		draftMappings := publishedMappings
		if c.hasDraft {
			draftMappings = mappingsOf(c.draft)
		}
		for _, view := range []struct {
			mappings []*yaml.Node
			into     map[string][]*yaml.Node
		}{{publishedMappings, published}, {draftMappings, drafted}} {
			for _, m := range view.mappings {
				target := field(m, "blueprint")
				if target == nil || target.Kind != yaml.ScalarNode || target.Value == "" {
					continue
				}
				if !slices.ContainsFunc(blueprints, func(b *document) bool { return b.identifier == target.Value }) {
					return fmt.Errorf("connection %s maps blueprint %s, which doesn't exist; create it or remove the mapping, then upgrade again",
						c.identifier, target.Value)
				}
				if !slices.Contains(targets, target.Value) {
					targets = append(targets, target.Value)
				}
				view.into[target.Value] = append(view.into[target.Value], toSource(c.identifier, m))
			}
		}
	}

	for _, b := range blueprints {
		if !slices.Contains(targets, b.identifier) {
			continue
		}
		publishedView, draftView := published[b.identifier], drafted[b.identifier]
		for version, source := range b.versions {
			text, err := withSources(source, publishedView)
			if err != nil {
				return fmt.Errorf("blueprint %s version %d: %w", b.identifier, version, err)
			}
			b.versions[version] = text
		}
		switch {
		case b.hasDraft:
			text, err := withSources(b.draft, draftView)
			if err != nil {
				return fmt.Errorf("blueprint %s draft: %w", b.identifier, err)
			}
			b.draft = text
		case b.published != 0 && !sameNodes(publishedView, draftView):
			text, err := withSources(b.loadedVersions[b.published], draftView)
			if err != nil {
				return fmt.Errorf("blueprint %s draft: %w", b.identifier, err)
			}
			b.draft, b.hasDraft = text, true
		}
	}

	for _, c := range connections {
		for version, source := range c.versions {
			c.versions[version] = withoutMappings(source)
		}
		if c.hasDraft {
			c.draft = withoutMappings(c.draft)
		}
	}

	return nil
}

// mappingsOf lists a connection source's mappings, or none when it doesn't
// parse.
func mappingsOf(text string) []*yaml.Node {
	root := parseRoot(text)
	mappings := field(field(root, "spec"), "mappings")
	if mappings == nil || mappings.Kind != yaml.SequenceNode {
		return nil
	}
	var out []*yaml.Node
	for _, m := range mappings.Content {
		if m.Kind == yaml.MappingNode {
			out = append(out, m)
		}
	}

	return out
}

// toSource is a copy of mapping as a source from connection: without its
// blueprint, and naming the connection first.
func toSource(connection string, mapping *yaml.Node) *yaml.Node {
	source := deepCopy(mapping)
	for i := 0; i+1 < len(source.Content); i += 2 {
		if source.Content[i].Value != "blueprint" {
			continue
		}
		key := source.Content[i]
		source.Content = append(source.Content[:i], source.Content[i+2:]...)
		// A comment above the blueprint goes to the key that took its place.
		if key.HeadComment != "" && i < len(source.Content) && source.Content[i].HeadComment == "" {
			source.Content[i].HeadComment = key.HeadComment
		}
		break
	}
	name := scalar(connection)
	key := scalar("connection")
	if len(source.Content) > 0 {
		// The mapping's first key carries the comment written above it.
		key.HeadComment, source.Content[0].HeadComment = source.Content[0].HeadComment, ""
	}
	source.Content = append([]*yaml.Node{key, name}, source.Content...)

	return source
}

func deepCopy(node *yaml.Node) *yaml.Node {
	out := *node
	out.Content = make([]*yaml.Node, len(node.Content))
	for i, child := range node.Content {
		out.Content[i] = deepCopy(child)
	}

	return &out
}

// withSources gives a blueprint source the sources, after its properties,
// or as it is when there are none or it doesn't parse.
func withSources(text string, sources []*yaml.Node) (string, error) {
	if len(sources) == 0 {
		return text, nil
	}
	doc, root := parseDocument(text)
	spec := field(root, "spec")
	if spec == nil || spec.Kind != yaml.MappingNode {
		return text, nil
	}
	if field(spec, "sources") != nil {
		return "", fmt.Errorf("it already has sources")
	}
	list := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, s := range sources {
		list.Content = append(list.Content, deepCopy(s))
	}
	pair := []*yaml.Node{scalar("sources"), list}
	at := len(spec.Content)
	for i := 0; i+1 < len(spec.Content); i += 2 {
		if spec.Content[i].Value == "properties" {
			at = i + 2
		}
	}
	spec.Content = slices.Insert(spec.Content, at, pair...)

	return encode(doc)
}

// withoutMappings is a connection source without its mappings.
func withoutMappings(text string) string {
	doc, root := parseDocument(text)
	spec := field(root, "spec")
	if field(spec, "mappings") == nil {
		return text
	}
	for i := 0; i+1 < len(spec.Content); i += 2 {
		if spec.Content[i].Value != "mappings" {
			continue
		}
		key := spec.Content[i]
		spec.Content = append(spec.Content[:i], spec.Content[i+2:]...)
		if key.HeadComment != "" && i < len(spec.Content) && spec.Content[i].HeadComment == "" {
			spec.Content[i].HeadComment = key.HeadComment
		}
		break
	}
	out, err := encode(doc)
	if err != nil {
		return text
	}

	return out
}

func sameNodes(a, b []*yaml.Node) bool {
	encoded := func(nodes []*yaml.Node) string {
		text, _ := encode(&yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Content: nodes})
		return text
	}

	return len(a) == len(b) && encoded(a) == encoded(b)
}

func parseDocument(text string) (*yaml.Node, *yaml.Node) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, nil
	}

	return &doc, doc.Content[0]
}

func parseRoot(text string) *yaml.Node {
	_, root := parseDocument(text)
	return root
}

func encode(node *yaml.Node) (string, error) {
	var out bytes.Buffer
	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(2)
	if err := encoder.Encode(node); err != nil {
		return "", err
	}

	return out.String(), nil
}
