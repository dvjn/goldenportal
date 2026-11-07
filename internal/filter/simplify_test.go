package filter

import (
	"encoding/json"
	"testing"
)

func TestAnd(t *testing.T) {
	parse := func(raw string) *Filter {
		f, err := Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	tests := []struct {
		name string
		in   []string
		want string
	}{
		{"nothing", nil, `null`},
		{"flattens nested and", []string{`{"and":[{"and":[{"property":"a","op":"exists"},{"property":"b","op":"exists"}]},{"property":"c","op":"exists"}]}`}, `{"and":[{"property":"a","op":"exists"},{"property":"b","op":"exists"},{"property":"c","op":"exists"}]}`},
		{"drops duplicates", []string{`{"property":"a","op":"eq","value":1}`, `{"property":"a","op":"eq","value":1}`}, `{"property":"a","op":"eq","value":1}`},
		{"one value in is eq", []string{`{"property":"a","op":"in","values":["x"]}`}, `{"property":"a","op":"eq","value":"x"}`},
		{"one value in duplicates eq", []string{`{"property":"a","op":"in","values":["x"]}`, `{"property":"a","op":"eq","value":"x"}`}, `{"property":"a","op":"eq","value":"x"}`},
		{"eq false implies ne true", []string{`{"property":"a","op":"ne","value":true}`, `{"property":"a","op":"eq","value":false}`}, `{"property":"a","op":"eq","value":false}`},
		{"eq implies in and exists", []string{`{"property":"a","op":"in","values":["x","y"]}`, `{"property":"a","op":"exists"}`, `{"property":"a","op":"eq","value":"y"}`}, `{"property":"a","op":"eq","value":"y"}`},
		{"eq keeps a ne of its own value", []string{`{"property":"a","op":"eq","value":"x"}`, `{"property":"a","op":"ne","value":"x"}`}, `{"and":[{"property":"a","op":"eq","value":"x"},{"property":"a","op":"ne","value":"x"}]}`},
		{"eq keeps an in without its value", []string{`{"property":"a","op":"eq","value":"x"}`, `{"property":"a","op":"in","values":["y","z"]}`}, `{"and":[{"property":"a","op":"eq","value":"x"},{"property":"a","op":"in","values":["y","z"]}]}`},
		{"other properties stay", []string{`{"property":"a","op":"eq","value":"x"}`, `{"property":"b","op":"exists"}`}, `{"and":[{"property":"a","op":"eq","value":"x"},{"property":"b","op":"exists"}]}`},
		{"not stays", []string{`{"property":"a","op":"eq","value":"x"}`, `{"not":{"property":"a","op":"exists"}}`}, `{"and":[{"property":"a","op":"eq","value":"x"},{"not":{"property":"a","op":"exists"}}]}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var filters []*Filter
			for _, raw := range test.in {
				filters = append(filters, parse(raw))
			}
			got, _ := json.Marshal(And(filters...))
			if string(got) != test.want {
				t.Errorf("And() = %s\nwant %s", got, test.want)
			}
		})
	}
}
