package telemetry

import (
	"context"
	"strings"
	"unicode"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/embedded"
)

// gofr opens the database through otelsql, which makes a span for every
// statement under the caller's span. It does so with otelsql's defaults,
// though: each span is named for the database/sql method that ran it, such
// as "sql.conn.query", and transactions begin without the caller's context,
// so each begin and commit starts a trace of its own. otelsql keeps the
// tracer provider in place when the database opens, so InstallSQL puts one
// there first that renames statement spans after their statements and
// drops the spans that have no parent or say nothing.

// sqlScope is the instrumentation scope otelsql's spans come from.
const sqlScope = "github.com/XSAM/otelsql"

// sqlNoise are otelsql's spans that don't run a statement. sql.rows, the
// reading of a statement's rows, would double every query's spans; the
// work around a statement shows how long reading took.
var sqlNoise = map[string]bool{
	"sql.conn.reset_session": true, "sql.connector.connect": true, "sql.conn.prepare": true, "sql.rows": true,
}

// InstallSQL makes statement spans read as their statements. It must run
// before gofr opens the database, so before gofr.New; spans then go to
// whichever provider gofr installs.
func InstallSQL() { otel.SetTracerProvider(sqlProvider{}) }

type quietSQLKey struct{}

// QuietSQL leaves out the spans of the statements run under ctx, for work
// that runs one or more for every item, such as writing a sync's batch,
// where they'd run to tens of thousands a trace; its own span shows how
// long they took together.
func QuietSQL(ctx context.Context) context.Context {
	return context.WithValue(ctx, quietSQLKey{}, true)
}

type sqlProvider struct{ embedded.TracerProvider }

func (sqlProvider) Tracer(name string, opts ...trace.TracerOption) trace.Tracer {
	return sqlTracer{name: name, opts: opts}
}

type sqlTracer struct {
	embedded.Tracer
	name string
	opts []trace.TracerOption
}

func (t sqlTracer) Start(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	provider := otel.GetTracerProvider()
	if _, self := provider.(sqlProvider); self {
		return ctx, trace.SpanFromContext(context.Background())
	}
	if t.name != sqlScope {
		return provider.Tracer(t.name, t.opts...).Start(ctx, name, opts...)
	}
	if quiet, _ := ctx.Value(quietSQLKey{}).(bool); quiet || sqlNoise[name] || !trace.SpanContextFromContext(ctx).IsValid() {
		return ctx, trace.SpanFromContext(context.Background())
	}
	ctx, span := provider.Tracer(t.name, t.opts...).Start(ctx, name, opts...)

	return ctx, sqlSpan{span}
}

// sqlSpan names a statement's span once otelsql gives it the statement.
type sqlSpan struct{ trace.Span }

func (s sqlSpan) SetAttributes(attrs ...attribute.KeyValue) {
	for _, a := range attrs {
		if a.Key != semconv.DBQueryTextKey {
			continue
		}
		summary, operation, collection := summarize(a.Value.AsString())
		s.SetName(summary)
		attrs = append(attrs, semconv.DBSystemNamePostgreSQL, semconv.DBQuerySummary(summary), semconv.DBOperationName(operation))
		if collection != "" {
			attrs = append(attrs, semconv.DBCollectionName(collection))
		}
	}
	s.Span.SetAttributes(attrs...)
}

// summarize names a statement as the semantic conventions do, by what it
// does and to which table, such as "SELECT entities". Statements are
// parameterised, so their text holds no values, only names.
func summarize(query string) (summary, operation, collection string) {
	words := topLevelWords(query)
	i := 0
	for i < len(words) && !isOperation(words[i]) {
		// A statement that starts WITH runs the first operation after its
		// common table expressions, which sit in parentheses.
		if i > 0 && !strings.EqualFold(words[0], "WITH") {
			break
		}
		i++
	}
	if i == len(words) || !isOperation(words[i]) {
		return "SQL", "", ""
	}
	operation = strings.ToUpper(words[i])
	after := func(keyword string) string {
		for j := i + 1; j+1 < len(words); j++ {
			if strings.EqualFold(words[j], keyword) {
				return words[j+1]
			}
		}
		return ""
	}
	switch operation {
	case "SELECT", "DELETE":
		collection = after("FROM")
	case "INSERT":
		collection = after("INTO")
	case "UPDATE":
		if i+1 < len(words) {
			collection = words[i+1]
		}
	}
	collection = strings.ReplaceAll(collection, `"`, "")
	if collection == "" || !isName(collection) {
		return operation, operation, ""
	}

	return operation + " " + collection, operation, collection
}

func isOperation(word string) bool {
	switch strings.ToUpper(word) {
	case "SELECT", "INSERT", "UPDATE", "DELETE":
		return true
	}
	return false
}

func isName(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '.' }) < 0
}

// topLevelWords are a statement's words outside parentheses and string
// literals, so a subquery's FROM isn't taken for the statement's.
func topLevelWords(query string) []string {
	var words []string
	var word strings.Builder
	flush := func() {
		if word.Len() > 0 {
			words = append(words, word.String())
			word.Reset()
		}
	}
	depth := 0
	var quote rune
	for _, r := range query {
		if quote != 0 {
			if quote == '"' && depth == 0 {
				word.WriteRune(r)
			}
			if r == quote {
				quote = 0
			}
			continue
		}
		switch {
		case r == '\'':
			flush()
			quote = r
		case r == '"':
			quote = r
			if depth == 0 {
				word.WriteRune(r)
			}
		case r == '(':
			flush()
			// A subquery stands as one word, so it's never taken for a
			// table.
			if depth == 0 {
				words = append(words, "(")
			}
			depth++
		case r == ')':
			flush()
			depth--
		case depth > 0:
		case unicode.IsSpace(r) || r == ',' || r == ';':
			flush()
		default:
			word.WriteRune(r)
		}
	}
	flush()

	return words
}
