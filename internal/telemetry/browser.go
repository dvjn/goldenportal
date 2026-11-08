package telemetry

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	"golang.org/x/time/rate"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// The web UI's spans reach the collector through the server rather than
// straight from the browser. That way the collector needn't be reachable
// from browsers, take cross-origin requests, or hold credentials a page
// could read; the UI sends to its own origin, which a content security
// policy of connect-src 'self' allows and which works the same behind one
// origin in production; and only signed-in users can send, at a bounded
// rate, with their spans cleaned of what mustn't leave the portal. It
// costs a hop through the server, and works only with an OTLP collector,
// the one exporter whose wire format the server can pass on.

const (
	// maxBrowserBytes caps one batch, which the UI keeps far smaller.
	maxBrowserBytes = 1 << 20
	// maxBrowserSpans caps the spans one batch may carry.
	maxBrowserSpans = 2048
	// maxAttributeLength caps each string the UI sends, as span names and
	// attribute values.
	maxAttributeLength = 1024
	browserTimeout     = 10 * time.Second
	// The UI sends a batch every few seconds while it's busy, so a sender
	// gets room for a burst after a quiet spell and a steady rate well
	// above what one browser needs.
	batchesPerSecond = 2
	batchBurst       = 20
	senderIdle       = 10 * time.Minute
)

// sensitiveKey matches attribute names that may hold a credential, which
// the UI never sends and the relay drops whatever sent them.
var sensitiveKey = regexp.MustCompile(`(?i)authorization|cookie|token|secret|password|api[_.-]?key|session`)

// Settings is what Relay reads gofr's tracing settings from.
type Settings interface {
	Get(key string) string
	GetOrDefault(key, fallback string) string
}

// Relay passes the web UI's spans on to the collector the server traces
// to.
type Relay struct {
	client  otlptrace.Client
	service string
	limits  *senderLimits
	// SampleRatio is the share of traces the UI starts that it samples,
	// as the server samples those it starts.
	SampleRatio float64
}

// NewRelay is a relay to where gofr's settings send the server's spans,
// or nil when they don't go to an OTLP collector, so the UI traces
// nothing.
func NewRelay(settings Settings) *Relay {
	switch strings.ToLower(strings.TrimSpace(settings.Get("TRACE_EXPORTER"))) {
	case "otlp", "jaeger":
	default:
		return nil
	}
	endpoint := settings.Get("TRACER_URL")
	if endpoint == "" {
		return nil
	}
	// The transport is chosen as gofr chooses it for the server's spans:
	// from the URL's scheme, or else TRACER_INSECURE, which defaults to
	// plaintext.
	var opts []otlptracegrpc.Option
	if scheme := strings.ToLower(endpoint); strings.HasPrefix(scheme, "http://") || strings.HasPrefix(scheme, "https://") {
		opts = append(opts, otlptracegrpc.WithEndpointURL(endpoint))
	} else {
		opts = append(opts, otlptracegrpc.WithEndpoint(endpoint))
		if insecure, err := strconv.ParseBool(settings.GetOrDefault("TRACER_INSECURE", "true")); err != nil || insecure {
			opts = append(opts, otlptracegrpc.WithInsecure())
		}
	}
	if headers := tracerHeaders(settings); len(headers) > 0 {
		opts = append(opts, otlptracegrpc.WithHeaders(headers))
	}
	client := otlptracegrpc.NewClient(opts...)
	if err := client.Start(context.Background()); err != nil {
		return nil
	}
	ratio, err := strconv.ParseFloat(settings.GetOrDefault("TRACER_RATIO", "1"), 64)
	if err != nil {
		ratio = 1
	}

	return &Relay{
		client: client, service: settings.GetOrDefault("APP_NAME", "goldenportal") + "-ui",
		limits: newSenderLimits(batchesPerSecond, batchBurst), SampleRatio: ratio,
	}
}

// tracerHeaders are the headers gofr sends its collector, from
// TRACER_HEADERS, as Key=Value pairs, or else TRACER_AUTH_KEY.
func tracerHeaders(settings Settings) map[string]string {
	headers := map[string]string{}
	if pairs := settings.Get("TRACER_HEADERS"); pairs != "" {
		for pair := range strings.SplitSeq(pairs, ",") {
			if key, value, ok := strings.Cut(strings.TrimSpace(pair), "="); ok && strings.TrimSpace(key) != "" && strings.TrimSpace(value) != "" {
				headers[strings.TrimSpace(key)] = strings.TrimSpace(value)
			}
		}
	} else if key := settings.Get("TRACER_AUTH_KEY"); key != "" {
		headers["Authorization"] = key
	}

	return headers
}

// senderLimits caps how fast each sender passes spans on, so one busy or
// misbehaving browser can't use up what the others send.
type senderLimits struct {
	mu      sync.Mutex
	every   rate.Limit
	burst   int
	senders map[string]*senderLimit
}

type senderLimit struct {
	limit *rate.Limiter
	seen  time.Time
}

func newSenderLimits(every rate.Limit, burst int) *senderLimits {
	return &senderLimits{every: every, burst: burst, senders: map[string]*senderLimit{}}
}

func (l *senderLimits) allow(sender string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	s, ok := l.senders[sender]
	if !ok {
		// Senders whose allowance has refilled are dropped, so the map
		// only holds who's sending.
		for id, idle := range l.senders {
			if now.Sub(idle.seen) > senderIdle {
				delete(l.senders, id)
			}
		}
		s = &senderLimit{limit: rate.NewLimiter(l.every, l.burst)}
		l.senders[sender] = s
	}
	s.seen = now
	return s.limit.AllowN(now, 1)
}

// Serve takes a batch of spans from the UI, in OTLP over HTTP, and passes
// it on. sender is the signed-in user who sent it.
func (b *Relay) Serve(w http.ResponseWriter, r *http.Request, sender string) {
	if !b.limits.allow(sender) {
		w.Header().Set("Retry-After", "5")
		http.Error(w, "too many spans; try again shortly", http.StatusTooManyRequests)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBrowserBytes))
	if err != nil {
		http.Error(w, "the spans couldn't be read", http.StatusRequestEntityTooLarge)
		return
	}
	protobuf := strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-protobuf")
	var batch coltracepb.ExportTraceServiceRequest
	if protobuf {
		err = proto.Unmarshal(body, &batch)
	} else if body, err = base64IDs(body); err == nil {
		err = protojson.UnmarshalOptions{DiscardUnknown: true}.Unmarshal(body, &batch)
	}
	if err != nil {
		http.Error(w, "the spans aren't OTLP", http.StatusBadRequest)
		return
	}
	if err := b.clean(&batch, r.UserAgent()); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), browserTimeout)
	defer cancel()
	if err := b.client.UploadTraces(ctx, batch.GetResourceSpans()); err != nil {
		Warn(r.Context(), "browser spans not passed on", err)
		http.Error(w, "the spans couldn't be passed on", http.StatusServiceUnavailable)
		return
	}
	if protobuf {
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusOK)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte("{}"))
}

// idFields are the IDs OTLP's JSON writes in hex, where protobuf's JSON
// mapping, which protojson reads, takes bytes in base64.
var idFields = map[string]bool{"traceId": true, "spanId": true, "parentSpanId": true}

// base64IDs rewrites a JSON batch's IDs from hex to base64.
func base64IDs(body []byte) ([]byte, error) {
	var batch any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&batch); err != nil {
		return nil, err
	}
	var walk func(v any) error
	walk = func(v any) error {
		switch v := v.(type) {
		case map[string]any:
			for key, value := range v {
				if s, ok := value.(string); ok && idFields[key] {
					id, err := hex.DecodeString(s)
					if err != nil {
						return err
					}
					v[key] = base64.StdEncoding.EncodeToString(id)
					continue
				}
				if err := walk(value); err != nil {
					return err
				}
			}
		case []any:
			for _, item := range v {
				if err := walk(item); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(batch); err != nil {
		return nil, err
	}

	return json.Marshal(batch)
}

// clean makes a batch the server's to send: its resource says it's the
// UI, and nothing in it may carry a credential, a query string, or more
// than its share.
func (b *Relay) clean(batch *coltracepb.ExportTraceServiceRequest, userAgent string) error {
	spans := 0
	for _, rs := range batch.GetResourceSpans() {
		rs.Resource = &resourcepb.Resource{Attributes: []*commonpb.KeyValue{
			stringKV("service.name", b.service),
			stringKV("user_agent.original", truncate(userAgent)),
		}}
		rs.SchemaUrl = ""
		for _, ss := range rs.GetScopeSpans() {
			for _, span := range ss.GetSpans() {
				spans++
				span.Name = truncate(span.GetName())
				span.Attributes = cleanAttributes(span.GetAttributes())
				for _, event := range span.GetEvents() {
					event.Name = truncate(event.GetName())
					event.Attributes = cleanAttributes(event.GetAttributes())
				}
				for _, link := range span.GetLinks() {
					link.Attributes = cleanAttributes(link.GetAttributes())
				}
				if span.Status != nil {
					span.Status.Message = truncate(Scrub(span.Status.GetMessage()))
				}
			}
		}
	}
	if spans > maxBrowserSpans {
		return errors.New("too many spans in one batch")
	}

	return nil
}

func cleanAttributes(attrs []*commonpb.KeyValue) []*commonpb.KeyValue {
	kept := attrs[:0]
	for _, kv := range attrs {
		if sensitiveKey.MatchString(kv.GetKey()) {
			continue
		}
		if s, ok := kv.GetValue().GetValue().(*commonpb.AnyValue_StringValue); ok {
			s.StringValue = truncate(Scrub(s.StringValue))
		}
		kept = append(kept, kv)
	}

	return kept
}

func stringKV(key, value string) *commonpb.KeyValue {
	return &commonpb.KeyValue{Key: key, Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: value}}}
}

func truncate(s string) string {
	if len(s) <= maxAttributeLength {
		return s
	}

	return strings.ToValidUTF8(s[:maxAttributeLength], "")
}
