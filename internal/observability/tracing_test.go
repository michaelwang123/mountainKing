package observability

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/graphql-api/internal/config"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// newTestTracer creates an in-memory tracer for testing span creation.
func newTestTracer() (trace.Tracer, *tracetest.InMemoryExporter) {
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exp),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	return tp.Tracer(tracerName), exp
}

func TestInitTracing_Disabled(t *testing.T) {
	cfg := config.TracingConfig{Enabled: false}
	tp, err := InitTracing(cfg)
	if err != nil {
		t.Fatalf("InitTracing disabled: unexpected error: %v", err)
	}
	defer func() { _ = tp.Shutdown(context.Background()) }()

	// Provider should be a noop. noop.NewTracerProvider returns a value type,
	// so we check via the concrete type.
	if _, ok := tp.Provider().(noop.TracerProvider); !ok {
		t.Errorf("expected noop.TracerProvider, got %T", tp.Provider())
	}

	// Tracer should produce non-recording spans.
	_, span := tp.Tracer().Start(context.Background(), "test")
	if span.IsRecording() {
		t.Error("expected non-recording span from noop tracer")
	}
	span.End()
}

func TestInitTracing_Enabled_GRPC(t *testing.T) {
	cfg := config.TracingConfig{
		Enabled:      true,
		SamplingRate: 0.5,
		OTLP: config.OTLPConfig{
			Endpoint: "localhost:4317",
			Protocol: "grpc",
		},
	}
	tp, err := InitTracing(cfg)
	if err != nil {
		t.Fatalf("InitTracing grpc: unexpected error: %v", err)
	}
	defer func() { _ = tp.Shutdown(context.Background()) }()

	if tp.Tracer() == nil {
		t.Error("expected non-nil tracer")
	}
	if tp.Provider() == nil {
		t.Error("expected non-nil provider")
	}
}

func TestInitTracing_Enabled_HTTP(t *testing.T) {
	cfg := config.TracingConfig{
		Enabled:      true,
		SamplingRate: 1.0,
		OTLP: config.OTLPConfig{
			Endpoint: "localhost:4318",
			Protocol: "http",
		},
	}
	tp, err := InitTracing(cfg)
	if err != nil {
		t.Fatalf("InitTracing http: unexpected error: %v", err)
	}
	defer func() { _ = tp.Shutdown(context.Background()) }()

	if tp.Tracer() == nil {
		t.Error("expected non-nil tracer")
	}
}

func TestInitTracing_DefaultSamplingRate(t *testing.T) {
	cfg := config.TracingConfig{
		Enabled:      true,
		SamplingRate: 0, // should default to 1.0
		OTLP: config.OTLPConfig{
			Endpoint: "localhost:4317",
			Protocol: "grpc",
		},
	}
	tp, err := InitTracing(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = tp.Shutdown(context.Background()) }()

	if tp.Tracer() == nil {
		t.Error("expected non-nil tracer with default sampling rate")
	}
}

func TestShutdown_NilShutdown(t *testing.T) {
	tp := &TracingProvider{}
	if err := tp.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown with nil func: unexpected error: %v", err)
	}
}

func TestStartRequestSpan(t *testing.T) {
	tracer, exp := newTestTracer()

	ctx, span := StartRequestSpan(context.Background(), tracer, "query", "GetUsers", "POST", "/graphql")
	span.End()

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	s := spans[0]
	if s.Name != "GraphQL query GetUsers" {
		t.Errorf("span name = %q, want %q", s.Name, "GraphQL query GetUsers")
	}

	assertAttr(t, s.Attributes, "graphql.operation.name", "GetUsers")
	assertAttr(t, s.Attributes, "graphql.operation.type", "query")
	assertAttr(t, s.Attributes, "http.method", "POST")
	assertAttr(t, s.Attributes, "http.url", "/graphql")

	// Verify context carries the span.
	if trace.SpanFromContext(ctx) != span {
		t.Error("context should carry the created span")
	}
}

func TestStartResolverSpan(t *testing.T) {
	tracer, exp := newTestTracer()

	_, span := StartResolverSpan(context.Background(), tracer, "users", "[User]", "analytics_db")
	span.End()

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	s := spans[0]
	if s.Name != "Resolver users" {
		t.Errorf("span name = %q, want %q", s.Name, "Resolver users")
	}

	assertAttr(t, s.Attributes, "graphql.field.name", "users")
	assertAttr(t, s.Attributes, "graphql.field.type", "[User]")
	assertAttr(t, s.Attributes, "graphql.datasource", "analytics_db")
}

func TestStartStarRocksSpan(t *testing.T) {
	tracer, exp := newTestTracer()

	_, span := StartStarRocksSpan(context.Background(), tracer, "SELECT * FROM `orders`", "analytics_db")
	span.End()

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	s := spans[0]
	if s.Name != "StarRocks Query" {
		t.Errorf("span name = %q, want %q", s.Name, "StarRocks Query")
	}

	assertAttr(t, s.Attributes, "db.system", "starrocks")
	assertAttr(t, s.Attributes, "db.statement", "SELECT * FROM `orders`")
	assertAttr(t, s.Attributes, "db.datasource", "analytics_db")
}

func TestStartPrometheusSpan(t *testing.T) {
	tracer, exp := newTestTracer()

	_, span := StartPrometheusSpan(context.Background(), tracer, `up{job="api"}`, "monitoring")
	span.End()

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	s := spans[0]
	if s.Name != "Prometheus Query" {
		t.Errorf("span name = %q, want %q", s.Name, "Prometheus Query")
	}

	assertAttr(t, s.Attributes, "db.system", "prometheus")
	assertAttr(t, s.Attributes, "db.statement", `up{job="api"}`)
	assertAttr(t, s.Attributes, "db.datasource", "monitoring")
}

func TestRecordSpanError(t *testing.T) {
	tracer, exp := newTestTracer()

	_, span := tracer.Start(context.Background(), "test-error")
	testErr := errors.New("something went wrong")
	RecordSpanError(span, testErr)
	span.End()

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	s := spans[0]
	if s.Status.Code != codes.Error {
		t.Errorf("span status = %v, want Error", s.Status.Code)
	}
	if s.Status.Description != "something went wrong" {
		t.Errorf("span status description = %q, want %q", s.Status.Description, "something went wrong")
	}

	// Should have at least one event (the recorded error).
	if len(s.Events) == 0 {
		t.Error("expected at least one span event for the recorded error")
	}
}

func TestRecordSpanError_NilInputs(t *testing.T) {
	// Should not panic with nil span or nil error.
	RecordSpanError(nil, errors.New("err"))
	tracer, _ := newTestTracer()
	_, span := tracer.Start(context.Background(), "test")
	RecordSpanError(span, nil)
	span.End()
}

func TestExtractTraceID(t *testing.T) {
	tracer, _ := newTestTracer()

	ctx, span := tracer.Start(context.Background(), "test-trace-id")
	defer span.End()

	traceID := ExtractTraceID(ctx)
	if traceID == "" {
		t.Error("expected non-empty trace ID")
	}
	if len(traceID) != 32 {
		t.Errorf("trace ID length = %d, want 32", len(traceID))
	}
}

func TestExtractTraceID_NoSpan(t *testing.T) {
	traceID := ExtractTraceID(context.Background())
	if traceID != "" {
		t.Errorf("expected empty trace ID, got %q", traceID)
	}
}

func TestExtractAndInjectTraceContext(t *testing.T) {
	// Set up W3C propagator.
	otel.SetTextMapPropagator(propagation.TraceContext{})

	tracer, _ := newTestTracer()
	ctx, span := tracer.Start(context.Background(), "parent")
	defer span.End()

	// Inject into outbound request.
	outReq := httptest.NewRequest("GET", "/downstream", nil)
	InjectTraceContext(ctx, outReq)

	traceparent := outReq.Header.Get("Traceparent")
	if traceparent == "" {
		t.Fatal("expected traceparent header to be injected")
	}

	// Extract from inbound request.
	inReq := httptest.NewRequest("GET", "/graphql", nil)
	inReq.Header.Set("Traceparent", traceparent)

	extractedCtx := ExtractTraceContext(context.Background(), inReq)
	sc := trace.SpanContextFromContext(extractedCtx)

	if !sc.HasTraceID() {
		t.Error("expected extracted context to have a trace ID")
	}
	if sc.TraceID() != span.SpanContext().TraceID() {
		t.Errorf("trace ID mismatch: got %s, want %s", sc.TraceID(), span.SpanContext().TraceID())
	}
}

func TestW3CTraceContextRoundTrip(t *testing.T) {
	otel.SetTextMapPropagator(propagation.TraceContext{})

	tracer, _ := newTestTracer()
	ctx, parentSpan := tracer.Start(context.Background(), "service-a")
	defer parentSpan.End()

	// Simulate outbound: inject traceparent.
	req, _ := http.NewRequest("POST", "http://service-b/api", nil)
	InjectTraceContext(ctx, req)

	// Simulate inbound at service-b: extract traceparent.
	extractedCtx := ExtractTraceContext(context.Background(), req)

	// Start a child span in service-b.
	_, childSpan := tracer.Start(extractedCtx, "service-b-handler")
	childSpan.End()

	// Both spans should share the same trace ID.
	if parentSpan.SpanContext().TraceID() != childSpan.SpanContext().TraceID() {
		t.Error("parent and child spans should share the same trace ID")
	}
}

// assertAttr checks that the given attribute key has the expected string value.
func assertAttr(t *testing.T, attrs []attribute.KeyValue, key, want string) {
	t.Helper()
	for _, a := range attrs {
		if string(a.Key) == key {
			if a.Value.AsString() != want {
				t.Errorf("attribute %q = %q, want %q", key, a.Value.AsString(), want)
			}
			return
		}
	}
	t.Errorf("attribute %q not found", key)
}
