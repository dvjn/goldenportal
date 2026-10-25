package configdoc

import (
	"testing"
)

func TestSplitDocuments(t *testing.T) {
	source := "# leading comment\nkind: A\n---\n\n---  # empty\nkind: B\nspec: |\n  text\n---\n"
	chunks := SplitDocuments(source)
	if len(chunks) != 2 {
		t.Fatalf("chunks = %+v, want two", chunks)
	}
	if chunks[0].Source != "# leading comment\nkind: A\n" || chunks[0].Line != 1 {
		t.Errorf("first = %+v", chunks[0])
	}
	if chunks[1].Source != "kind: B\nspec: |\n  text\n" || chunks[1].Line != 6 {
		t.Errorf("second = %+v", chunks[1])
	}
}

func TestReadDocument(t *testing.T) {
	r := &Reader{}
	doc := ReadDocument(r, "kind: Blueprint\nmetadata:\n  name: service\n  title: Service\n  icon: server\nspec: {}\n", "Blueprint")
	if len(r.Issues) != 0 || doc.Name != "service" || doc.Meta.Title != "Service" || doc.Meta.Icon != "server" || doc.Spec == nil {
		t.Fatalf("doc = %+v, issues = %+v", doc, r.Issues)
	}
}
