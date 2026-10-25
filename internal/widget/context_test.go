package widget

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dvjn/goldenportal/internal/filter"
)

func TestRelativeDates(t *testing.T) {
	values := Values{}
	SetToday(values, time.Date(2024, 3, 1, 1, 0, 0, 0, time.FixedZone("east", 19800)))
	for name, want := range map[string]string{
		"$today": "2024-02-29", "$today-1d": "2024-02-28", "$today+1d": "2024-03-01", "$today-60d": "2023-12-31",
	} {
		if err := CheckText("{{ " + name + " }}"); err != nil {
			t.Fatal(err)
		}
		if got := Render("Date: {{ "+name+" }}", values); got != "Date: "+want {
			t.Errorf("%s rendered %q", name, got)
		}
		f := &filter.Filter{Property: "deployed", Op: "gte", Value: name}
		if got := Bind(f, values); got.Value != want {
			t.Errorf("%s bound %+v", name, got)
		}
		if f.Value != name {
			t.Fatal("binding mutated the source")
		}
	}
	for _, name := range []string{"$today-7w", "$today+1", "$today--1d", "$today-365001d", "$today+999999999999999999999d"} {
		if err := CheckText("{{ " + name + " }}"); err == nil {
			t.Errorf("accepted %s", name)
		}
		if err := checkVariable(name); err == nil {
			t.Errorf("accepted filter %s", name)
		}
	}
}

func TestEntityContext(t *testing.T) {
	data := EntityValues("ledger", "Ledger", "service", map[string]any{
		"owners": []any{"payments", "search"}, "coverage": float64(72), "critical": true,
		"object": map[string]any{"ready": true},
	}, []string{"owners", "coverage", "critical", "missing", "object"})
	for _, test := range []struct{ name, op, want string }{
		{"$entity.title", "eq", `{"property":"p","op":"eq","value":"Ledger"}`},
		{"$entity.blueprint", "eq", `{"property":"p","op":"eq","value":"service"}`},
		{"$entity.coverage", "gte", `{"property":"p","op":"gte","value":72}`},
		{"$entity.critical", "eq", `{"property":"p","op":"eq","value":true}`},
		{"$entity.owners", "eq", `{"property":"p","op":"in","values":["payments","search"]}`},
		{"$entity.owners", "ne", `{"not":{"property":"p","op":"in","values":["payments","search"]}}`},
		{"$entity.missing", "eq", `{"property":"p","op":"in"}`},
	} {
		f := &filter.Filter{Property: "p", Op: test.op, Value: test.name}
		got, _ := json.Marshal(BindData(f, data))
		if string(got) != test.want {
			t.Errorf("%s %s: %s", test.name, test.op, got)
		}
	}
	list := BindData(&filter.Filter{Property: "p", Op: "in", Values: []any{"literal", "$entity.owners", "$entity.coverage"}}, data)
	if len(list.Values) != 4 || list.Values[3] != float64(72) {
		t.Fatalf("list: %+v", list)
	}
	text := "{{ $entity.title }} / {{ $entity.blueprint }} / {{ $entity.owners }} / {{ $entity.coverage }} / {{ $entity.critical }} / {{ $entity.missing }}"
	if err := CheckTextIn(text, Scope{Entity: true}); err != nil {
		t.Fatal(err)
	}
	if got := Render(text, TextValues(data)); got != "Ledger / service / payments, search / 72 / true / " {
		t.Fatal(got)
	}
	if err := CheckText(text); err == nil {
		t.Fatal("entity variables accepted on Home")
	}
	if err := CheckTextIn("{{ $entity.owner.name }}", Scope{Entity: true}); err == nil {
		t.Fatal("accepted implicit traversal")
	}
	if !Uses(&filter.Filter{Or: []filter.Filter{{Property: "p", Op: "eq", Value: "$entity.owners"}}}, "$entity") {
		t.Fatal("missed entity context")
	}
	if !strings.Contains(Render("{{ $entity.object }}", TextValues(data)), `"ready":true`) {
		t.Fatal("object not rendered as JSON")
	}
	// Entity data can contain text that resembles a variable; never expand it twice.
	data["$entity.title"] = []any{"$me.role"}
	bound := BindContext(&filter.Filter{Property: "p", Op: "eq", Value: "$entity.title"}, Values{"$me.role": {"admin"}}, data)
	if bound.Value != "$me.role" {
		t.Fatalf("expanded entity data recursively: %+v", bound)
	}
}
