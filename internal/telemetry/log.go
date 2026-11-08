package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync/atomic"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"gofr.dev/pkg/gofr/logging"
)

// gofr's logger writes one message per line, with the trace ID beside it
// when handed gofr's own trace ID marker. Logging a fields value as that
// message is how a line gets fields rather than a sentence with values
// spliced in: JSON logs nest them under "message", and a terminal shows them
// as key=value after the text.

// traceIDMarker is the key gofr's logger lifts a trace ID out of the
// arguments by, to write it as the line's trace_id.
const traceIDMarker = "__trace_id__"

var logger atomic.Pointer[logging.Logger]

// SetLogger is where Debug, Info, Warn and Error write: the portal's one
// gofr logger, whose level they follow. Until it's set they write nothing.
func SetLogger(l logging.Logger) { logger.Store(&l) }

// Debug logs what's only worth seeing while looking into a problem, such
// as each item or page.
func Debug(ctx context.Context, msg string, attrs ...attribute.KeyValue) {
	write(ctx, logging.DEBUG, msg, nil, attrs)
}

// Info logs how a unit of work went, once per unit.
func Info(ctx context.Context, msg string, attrs ...attribute.KeyValue) {
	write(ctx, logging.INFO, msg, nil, attrs)
}

// Warn logs what went wrong without stopping the work, such as a request
// tried again or a scope that couldn't be read. err may be nil.
func Warn(ctx context.Context, msg string, err error, attrs ...attribute.KeyValue) {
	write(ctx, logging.WARN, msg, err, attrs)
}

// Error logs what failed.
func Error(ctx context.Context, msg string, err error, attrs ...attribute.KeyValue) {
	write(ctx, logging.ERROR, msg, err, attrs)
}

func write(ctx context.Context, level logging.Level, msg string, err error, attrs []attribute.KeyValue) {
	l := logger.Load()
	if l == nil {
		return
	}
	line := Fields{Msg: msg, Attrs: fieldsOf(withFields(ctx, attrs)), Err: err}
	args := []any{line}
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		line.SpanID = sc.SpanID().String()
		args = []any{line, map[string]any{traceIDMarker: sc.TraceID().String()}}
	}
	switch level {
	case logging.DEBUG:
		(*l).Debug(args...)
	case logging.INFO:
		(*l).Info(args...)
	case logging.WARN:
		(*l).Warn(args...)
	default:
		(*l).Error(args...)
	}
}

// Fields is one log line's message: what happened, the fields of the work
// it happened in, and the error, if any.
type Fields struct {
	Msg    string
	Attrs  []attribute.KeyValue
	SpanID string
	Err    error
}

func (f Fields) pairs() []pair {
	pairs := []pair{{"msg", f.Msg}}
	for _, a := range f.Attrs {
		pairs = append(pairs, pair{string(a.Key), a.Value.AsInterface()})
	}
	if f.Err != nil {
		pairs = append(pairs, pair{"error", Scrub(f.Err.Error())})
	}
	if f.SpanID != "" {
		pairs = append(pairs, pair{"span_id", f.SpanID})
	}

	return pairs
}

type pair struct {
	key   string
	value any
}

// MarshalJSON writes the fields in the order they were given, msg first,
// so lines read alike.
func (f Fields) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, p := range f.pairs() {
		if i > 0 {
			b.WriteByte(',')
		}
		key, _ := json.Marshal(p.key)
		value, err := json.Marshal(p.value)
		if err != nil {
			return nil, err
		}
		b.Write(key)
		b.WriteByte(':')
		b.Write(value)
	}
	b.WriteByte('}')

	return b.Bytes(), nil
}

// PrettyPrint is how gofr shows the line in a terminal.
func (f Fields) PrettyPrint(w io.Writer) {
	pairs := f.pairs()
	fmt.Fprint(w, f.Msg)
	for _, p := range pairs[1:] {
		if p.key == "span_id" {
			continue
		}
		if s, ok := p.value.(string); ok {
			fmt.Fprintf(w, " %s=%q", p.key, s)
		} else {
			fmt.Fprintf(w, " %s=%v", p.key, p.value)
		}
	}
	fmt.Fprintln(w)
}
