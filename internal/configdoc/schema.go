package configdoc

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"

	"gofr.dev/pkg/gofr"

	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/httpx"
)

// Each kind's schema file holds its own definitions; common.json holds those
// kinds share, such as widgets and filters. Each served schema gets a copy of
// those it uses, so it stands alone for clients that don't follow remote refs.
//
//go:embed schemas/*.json
var schemaFiles embed.FS

// SchemaKinds are the document kinds with a JSON Schema, in the order
// documents depend on each other.
var SchemaKinds = []string{"Blueprint", "Integration", "Scorecard", "Dashboard"}

var schemas = sync.OnceValue(func() map[string]json.RawMessage {
	out, err := loadSchemas()
	if err != nil {
		panic(err)
	}

	return out
})

// Schema returns the JSON Schema (draft 2020-12) of a whole document of a
// kind, named as documents write it, such as Blueprint, or as its API path
// segment, such as blueprints. Case doesn't matter.
func Schema(kind string) (json.RawMessage, bool) {
	for _, k := range SchemaKinds {
		if strings.EqualFold(kind, k) || strings.EqualFold(kind, k+"s") {
			return schemas()[k], true
		}
	}

	return nil, false
}

func loadSchemas() (map[string]json.RawMessage, error) {
	var common struct {
		Defs map[string]json.RawMessage `json:"$defs"`
	}
	if err := readSchemaFile("common", &common); err != nil {
		return nil, err
	}
	out := map[string]json.RawMessage{}
	for _, kind := range SchemaKinds {
		var fields map[string]json.RawMessage
		if err := readSchemaFile(strings.ToLower(kind), &fields); err != nil {
			return nil, err
		}
		defs := map[string]json.RawMessage{}
		if err := json.Unmarshal(fields["$defs"], &defs); err != nil {
			return nil, fmt.Errorf("%s schema $defs: %w", kind, err)
		}
		for name, def := range common.Defs {
			if _, ok := defs[name]; ok {
				return nil, fmt.Errorf("%s schema defines %s, which common.json defines too", kind, name)
			}
			defs[name] = def
		}
		delete(fields, "$defs")
		merged, err := json.Marshal(referenced(fields, defs))
		if err != nil {
			return nil, err
		}
		fields["$defs"] = merged
		if out[kind], err = marshalOrdered(fields, "$schema", "title", "description"); err != nil {
			return nil, err
		}
	}

	return out, nil
}

var defRef = regexp.MustCompile(`"\$ref"\s*:\s*"#/\$defs/([^"/]+)`)

// referenced keeps the definitions the schema refers to, directly or
// through others, so a kind's schema doesn't carry what only others use.
func referenced(fields, defs map[string]json.RawMessage) map[string]json.RawMessage {
	kept := map[string]json.RawMessage{}
	var visit func(raw []byte)
	visit = func(raw []byte) {
		for _, match := range defRef.FindAllSubmatch(raw, -1) {
			name := string(match[1])
			if _, done := kept[name]; done || defs[name] == nil {
				continue
			}
			kept[name] = defs[name]
			visit(defs[name])
		}
	}
	for _, raw := range fields {
		visit(raw)
	}

	return kept
}

func readSchemaFile(name string, v any) error {
	data, err := schemaFiles.ReadFile("schemas/" + name + ".json")
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("schemas/%s.json: %w", name, err)
	}

	return nil
}

// marshalOrdered puts the first keys first, so a reader meets what the
// schema is for before its definitions.
func marshalOrdered(fields map[string]json.RawMessage, first ...string) (json.RawMessage, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	write := func(key string) {
		if b.Len() > 1 {
			b.WriteByte(',')
		}
		name, _ := json.Marshal(key)
		b.Write(name)
		b.WriteByte(':')
		b.Write(fields[key])
		delete(fields, key)
	}
	for _, key := range first {
		if _, ok := fields[key]; ok {
			write(key)
		}
	}
	rest, err := json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	if len(rest) > 2 {
		if b.Len() > 1 {
			b.WriteByte(',')
		}
		b.Write(rest[1 : len(rest)-1])
	}
	b.WriteByte('}')
	var compact bytes.Buffer
	if err := json.Compact(&compact, b.Bytes()); err != nil {
		return nil, err
	}

	return compact.Bytes(), nil
}

// SchemaSummary names a kind whose schema GET /api/web/schemas/{kind} serves.
type SchemaSummary struct {
	Kind        string `json:"kind"`
	Description string `json:"description"`
}

// RegisterSchemas serves the documents' JSON Schemas, so authors, people or
// agents, can see every field a document takes before writing one.
func RegisterSchemas(app httpx.Routes) {
	app.GET("schemas", listSchemas)
	app.GET("schemas/{kind}", getSchema)
}

func listSchemas(ctx *gofr.Context) (any, error) {
	if _, err := auth.Require(ctx, auth.RoleMember); err != nil {
		return nil, err
	}
	summaries := make([]SchemaSummary, 0, len(SchemaKinds))
	for _, kind := range SchemaKinds {
		var schema struct {
			Description string `json:"description"`
		}
		raw, _ := Schema(kind)
		if err := json.Unmarshal(raw, &schema); err != nil {
			return nil, err
		}
		summaries = append(summaries, SchemaSummary{Kind: kind, Description: schema.Description})
	}

	return summaries, nil
}

func getSchema(ctx *gofr.Context) (any, error) {
	if _, err := auth.Require(ctx, auth.RoleMember); err != nil {
		return nil, err
	}
	kind := ctx.PathParam("kind")
	schema, ok := Schema(kind)
	if !ok {
		return nil, httpx.Error{
			Code:    http.StatusNotFound,
			Message: fmt.Sprintf("no document kind %q; use one of %s", kind, strings.Join(SchemaKinds, ", ")),
		}
	}

	return schema, nil
}
