package migrations

import "testing"

func TestRewriteIntegration(t *testing.T) {
	tests := map[string]struct{ in, want string }{
		"push": {
			in: `kind: Integration
metadata:
  name: team-directory
  title: Team Directory
spec:
  # Teams come from the directory.
  connector:
    type: push
  mappings:
    - blueprint: team
      items: .teams[]
      entity:
        identifier: .slug
    - blueprint: team
      items: .groups[] | select(.team)
      entity: {identifier: .id}
`,
			want: `kind: Integration
metadata:
  name: team-directory
  title: Team Directory
spec:
  # Teams come from the directory.
  connector:
    type: http
    config:
      resources:
        team:
          push:
            items: .body.teams[]
        team-2:
          push:
            items: .body | (.groups[] | select(.team))
  mappings:
    - resource: team
      blueprint: team
      entity:
        identifier: .slug
    - resource: team-2
      blueprint: team
      entity: {identifier: .id}
`,
		},
		"push in flow style": {
			in:   "kind: Integration\nmetadata: {name: a, title: A}\nspec:\n  connector: {type: push}\n  mappings:\n    - blueprint: service\n      entity: {identifier: .id}\n",
			want: "kind: Integration\nmetadata: {name: a, title: A}\nspec:\n  connector:\n    type: http\n    config:\n      resources:\n        service:\n          push:\n            items: .body\n  mappings:\n    - resource: service\n      blueprint: service\n      entity: {identifier: .id}\n",
		},
		"http": {
			in: `kind: Integration
metadata: {name: a, title: A}
spec:
  connector:
    type: http
    config:
      resources:
        deployments:
          url: http://example.com/1.json
          next: .next
  mappings:
    - resource: deployments
      blueprint: deployment
      items: .deployments[]
      entity: {identifier: .id}
`,
			want: `kind: Integration
metadata: {name: a, title: A}
spec:
  connector:
    type: http
    config:
      resources:
        deployments:
          url: http://example.com/1.json
          next: .next
          items: .deployments[]
  mappings:
    - resource: deployments
      blueprint: deployment
      entity: {identifier: .id}
`,
		},
		"portal unchanged": {
			in:   "kind: Integration\nmetadata: {name: a, title: A}\nspec:\n  connector: {type: portal}\n  mappings:\n    - resource: users\n      blueprint: user\n      entity: {identifier: .email}\n",
			want: "kind: Integration\nmetadata: {name: a, title: A}\nspec:\n  connector: {type: portal}\n  mappings:\n    - resource: users\n      blueprint: user\n      entity: {identifier: .email}\n",
		},
		"not yaml": {in: "{", want: "{"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := rewriteIntegration(tt.in)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("got\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}

func TestRewriteIntegrationRefusesTwoSplits(t *testing.T) {
	in := "kind: Integration\nmetadata: {name: a, title: A}\nspec:\n  connector:\n    type: http\n    config:\n      resources:\n        r: {url: http://x.com}\n  mappings:\n    - resource: r\n      blueprint: a\n      items: .a[]\n      entity: {identifier: .id}\n    - resource: r\n      blueprint: b\n      items: .b[]\n      entity: {identifier: .id}\n"
	if _, err := rewriteIntegration(in); err == nil {
		t.Fatal("rewrite of a resource split two ways succeeded")
	}
}
