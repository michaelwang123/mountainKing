// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package observability

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"pgregory.net/rapid"
)

// newPropertyTestTracer creates an in-memory tracer and exporter for property tests.
func newPropertyTestTracer() (trace.Tracer, *tracetest.InMemoryExporter, *sdktrace.TracerProvider) {
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exp),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	return tp.Tracer(tracerName), exp, tp
}

// findAttr looks up a string attribute by key in a span's attributes.
func findAttr(attrs []attribute.KeyValue, key string) (string, bool) {
	for _, a := range attrs {
		if string(a.Key) == key {
			return a.Value.AsString(), true
		}
	}
	return "", false
}

// =============================================================================
// Property 42: Root Span 创建与属→
// **Validates: Requirements 12.3, 12.4**
// For any GraphQL request (tracing enabled), a Root Span should be created with
// name format "GraphQL {operation_type} {operation_name}" and attributes:
// graphql.operation.name, graphql.operation.type, http.method, http.url.
// =============================================================================

func TestProperty42_RootSpanCreationAndAttributes(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		tracer, exp, tp := newPropertyTestTracer()
		defer func() { _ = tp.Shutdown(context.Background()) }()

		opType := rapid.SampledFrom([]string{"query", "mutation"}).Draw(rt, "opType")
		opName := rapid.StringMatching(`[A-Z][a-zA-Z0-9]{2,20}`).Draw(rt, "opName")
		httpMethod := rapid.SampledFrom([]string{"POST", "GET"}).Draw(rt, "httpMethod")
		httpURL := rapid.StringMatching(`/[a-z]{3,10}`).Draw(rt, "httpURL")

		ctx, span := StartRequestSpan(context.Background(), tracer, opType, opName, httpMethod, httpURL)
		span.End()

		// Verify context carries the span.
		if trace.SpanFromContext(ctx) != span {
			rt.Fatal("context should carry the created span")
		}

		spans := exp.GetSpans()
		if len(spans) != 1 {
			rt.Fatalf("expected 1 span, got %d", len(spans))
		}

		s := spans[0]

		// Verify span name format.
		expectedName := fmt.Sprintf("GraphQL %s %s", opType, opName)
		if s.Name != expectedName {
			rt.Fatalf("span name = %q, want %q", s.Name, expectedName)
		}

		// Verify required attributes.
		for _, tc := range []struct {
			key  string
			want string
		}{
			{"graphql.operation.name", opName},
			{"graphql.operation.type", opType},
			{"http.method", httpMethod},
			{"http.url", httpURL},
		} {
			got, ok := findAttr(s.Attributes, tc.key)
			if !ok {
				rt.Fatalf("attribute %q not found on root span", tc.key)
			}
			if got != tc.want {
				rt.Fatalf("attribute %q = %q, want %q", tc.key, got, tc.want)
			}
		}

		exp.Reset()
	})
}

// =============================================================================
// Property 43: Resolver Span 创建与属→
// **Validates: Requirements 12.5**
// For any Resolver execution, a child Span should be created under the Root Span
// with name format "Resolver {field_name}" and attributes: graphql.field.name,
// graphql.field.type, graphql.datasource.
// =============================================================================

func TestProperty43_ResolverSpanCreationAndAttributes(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		tracer, exp, tp := newPropertyTestTracer()
		defer func() { _ = tp.Shutdown(context.Background()) }()

		fieldName := rapid.StringMatching(`[a-z][a-zA-Z0-9]{2,15}`).Draw(rt, "fieldName")
		fieldType := rapid.SampledFrom([]string{"String", "[User]", "Int", "PrometheusVector", "[StarRocksRow]"}).Draw(rt, "fieldType")
		datasource := rapid.StringMatching(`[a-z_]{3,15}`).Draw(rt, "datasource")

		// Create a parent root span first.
		parentCtx, parentSpan := tracer.Start(context.Background(), "root")

		// Create resolver span as child.
		_, resolverSpan := StartResolverSpan(parentCtx, tracer, fieldName, fieldType, datasource)
		resolverSpan.End()
		parentSpan.End()

		spans := exp.GetSpans()
		if len(spans) != 2 {
			rt.Fatalf("expected 2 spans, got %d", len(spans))
		}

		// The resolver span is ended first, so it appears first in the exporter.
		rs := spans[0]

		expectedName := fmt.Sprintf("Resolver %s", fieldName)
		if rs.Name != expectedName {
			rt.Fatalf("resolver span name = %q, want %q", rs.Name, expectedName)
		}

		// Verify parent-child relationship.
		if rs.Parent.SpanID() != parentSpan.SpanContext().SpanID() {
			rt.Fatal("resolver span should be a child of the root span")
		}

		// Verify required attributes.
		for _, tc := range []struct {
			key  string
			want string
		}{
			{"graphql.field.name", fieldName},
			{"graphql.field.type", fieldType},
			{"graphql.datasource", datasource},
		} {
			got, ok := findAttr(rs.Attributes, tc.key)
			if !ok {
				rt.Fatalf("attribute %q not found on resolver span", tc.key)
			}
			if got != tc.want {
				rt.Fatalf("attribute %q = %q, want %q", tc.key, got, tc.want)
			}
		}

		exp.Reset()
	})
}

// =============================================================================
// Property 44: 数据源查→Span 创建与属→
// **Validates: Requirements 12.6, 12.7**
// For any datasource query, a child Span should be created under the Resolver
// Span (StarRocks: "StarRocks Query", Prometheus: "Prometheus Query") with
// attributes: db.system, db.statement, db.datasource.
// =============================================================================

func TestProperty44_DataSourceQuerySpanCreationAndAttributes(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		tracer, exp, tp := newPropertyTestTracer()
		defer func() { _ = tp.Shutdown(context.Background()) }()

		dsType := rapid.SampledFrom([]string{"starrocks", "prometheus"}).Draw(rt, "dsType")
		statement := rapid.StringMatching(`[A-Za-z0-9 _\*\.\{\}="]{5,50}`).Draw(rt, "statement")
		datasource := rapid.StringMatching(`[a-z_]{3,15}`).Draw(rt, "datasource")

		// Create parent resolver span.
		parentCtx, parentSpan := tracer.Start(context.Background(), "Resolver test")

		var dsSpan trace.Span
		switch dsType {
		case "starrocks":
			_, dsSpan = StartStarRocksSpan(parentCtx, tracer, statement, datasource)
		case "prometheus":
			_, dsSpan = StartPrometheusSpan(parentCtx, tracer, statement, datasource)
		}
		dsSpan.End()
		parentSpan.End()

		spans := exp.GetSpans()
		if len(spans) != 2 {
			rt.Fatalf("expected 2 spans, got %d", len(spans))
		}

		ds := spans[0] // datasource span ended first

		// Verify span name.
		var expectedName string
		switch dsType {
		case "starrocks":
			expectedName = "StarRocks Query"
		case "prometheus":
			expectedName = "Prometheus Query"
		}
		if ds.Name != expectedName {
			rt.Fatalf("datasource span name = %q, want %q", ds.Name, expectedName)
		}

		// Verify parent-child relationship.
		if ds.Parent.SpanID() != parentSpan.SpanContext().SpanID() {
			rt.Fatal("datasource span should be a child of the resolver span")
		}

		// Verify required attributes.
		for _, tc := range []struct {
			key  string
			want string
		}{
			{"db.system", dsType},
			{"db.statement", statement},
			{"db.datasource", datasource},
		} {
			got, ok := findAttr(ds.Attributes, tc.key)
			if !ok {
				rt.Fatalf("attribute %q not found on datasource span", tc.key)
			}
			if got != tc.want {
				rt.Fatalf("attribute %q = %q, want %q", tc.key, got, tc.want)
			}
		}

		exp.Reset()
	})
}

// =============================================================================
// Property 45: W3C Trace Context 传播
// **Validates: Requirements 12.8, 12.9**
// For any inbound request with a traceparent header, the Root Span should use
// that trace context as parent; for any outbound request, the current trace
// context should be injected into the traceparent header.
// =============================================================================

func TestProperty45_W3CTraceContextPropagation(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		tracer, _, tp := newPropertyTestTracer()
		defer func() { _ = tp.Shutdown(context.Background()) }()

		// Set W3C propagator globally.
		otel.SetTextMapPropagator(propagation.TraceContext{})

		// Create a parent span to simulate an upstream service.
		upstreamCtx, upstreamSpan := tracer.Start(context.Background(), "upstream")
		defer upstreamSpan.End()

		// Inject trace context into a simulated inbound request.
		inboundReq := httptest.NewRequest("POST", "/graphql", nil)
		InjectTraceContext(upstreamCtx, inboundReq)

		traceparentHeader := inboundReq.Header.Get("Traceparent")
		if traceparentHeader == "" {
			rt.Fatal("expected traceparent header to be injected into outbound request")
		}

		// Extract trace context from the inbound request.
		extractedCtx := ExtractTraceContext(context.Background(), inboundReq)
		sc := trace.SpanContextFromContext(extractedCtx)

		if !sc.HasTraceID() {
			rt.Fatal("extracted context should have a trace ID")
		}
		if sc.TraceID() != upstreamSpan.SpanContext().TraceID() {
			rt.Fatalf("trace ID mismatch: got %s, want %s", sc.TraceID(), upstreamSpan.SpanContext().TraceID())
		}

		// Create a child span from extracted context and verify trace ID propagation.
		_, childSpan := tracer.Start(extractedCtx, "downstream")
		childSpan.End()

		if childSpan.SpanContext().TraceID() != upstreamSpan.SpanContext().TraceID() {
			rt.Fatal("child span should share the same trace ID as the upstream span")
		}

		// Verify outbound injection: inject child context into a new request.
		childCtx := trace.ContextWithSpan(context.Background(), childSpan)
		outboundReq := httptest.NewRequest("GET", "/downstream", nil)
		InjectTraceContext(childCtx, outboundReq)

		outTraceparent := outboundReq.Header.Get("Traceparent")
		if outTraceparent == "" {
			rt.Fatal("expected traceparent header on outbound request")
		}
	})
}

// =============================================================================
// Property 46: 错误 Span 状→
// **Validates: Requirements 12.14, 12.15**
// For any datasource query error or uncaught exception, the corresponding Span
// status should be set to Error, and a Span Event should record the error info.
// =============================================================================

func TestProperty46_ErrorSpanStatus(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		tracer, exp, tp := newPropertyTestTracer()
		defer func() { _ = tp.Shutdown(context.Background()) }()

		errMsg := rapid.StringMatching(`[a-zA-Z0-9 _\-]{5,50}`).Draw(rt, "errMsg")
		testErr := errors.New(errMsg)

		_, span := tracer.Start(context.Background(), "test-error-span")
		RecordSpanError(span, testErr)
		span.End()

		spans := exp.GetSpans()
		if len(spans) != 1 {
			rt.Fatalf("expected 1 span, got %d", len(spans))
		}

		s := spans[0]

		// Verify span status is Error.
		if s.Status.Code != codes.Error {
			rt.Fatalf("span status = %v, want Error", s.Status.Code)
		}

		// Verify status description contains the error message.
		if s.Status.Description != errMsg {
			rt.Fatalf("span status description = %q, want %q", s.Status.Description, errMsg)
		}

		// Verify at least one span event was recorded (the error event).
		if len(s.Events) == 0 {
			rt.Fatal("expected at least one span event for the recorded error")
		}

		// Verify the event is an "exception" event (OTel convention).
		foundException := false
		for _, ev := range s.Events {
			if ev.Name == "exception" {
				foundException = true
				break
			}
		}
		if !foundException {
			rt.Fatal("expected an 'exception' event on the error span")
		}

		exp.Reset()
	})
}

// =============================================================================
// Property 47: Trace ID 关联
// **Validates: Requirements 12.16, 12.17**
// For any request with tracing enabled, the trace_id should be extractable
// from the context and be a valid 32-character hex string.
// =============================================================================

func TestProperty47_TraceIDCorrelation(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		tracer, _, tp := newPropertyTestTracer()
		defer func() { _ = tp.Shutdown(context.Background()) }()

		opName := rapid.StringMatching(`[A-Z][a-zA-Z0-9]{2,20}`).Draw(rt, "opName")

		ctx, span := tracer.Start(context.Background(), opName)
		defer span.End()

		// Extract trace ID from context.
		traceID := ExtractTraceID(ctx)

		// Trace ID should be non-empty.
		if traceID == "" {
			rt.Fatal("expected non-empty trace ID from active span context")
		}

		// Trace ID should be a 32-character hex string.
		if len(traceID) != 32 {
			rt.Fatalf("trace ID length = %d, want 32", len(traceID))
		}

		// Verify all characters are valid hex.
		for _, c := range traceID {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
				rt.Fatalf("trace ID contains non-hex character: %c", c)
			}
		}

		// Verify trace ID matches the span's trace ID.
		expectedTraceID := span.SpanContext().TraceID().String()
		if traceID != expectedTraceID {
			rt.Fatalf("trace ID = %q, want %q", traceID, expectedTraceID)
		}
	})
}

// Verify ExtractTraceID returns empty string when no span is in context.
func TestProperty47_TraceIDCorrelation_NoSpan(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		traceID := ExtractTraceID(context.Background())
		if traceID != "" {
			rt.Fatalf("expected empty trace ID for context without span, got %q", traceID)
		}
	})
}

// =============================================================================
// Property 89: Redis 操作 Span 创建
// **Validates: Design - Redis 可观测→*
// For any Redis cache or distributed rate-limiting operation (tracing enabled),
// an independent Span should be created with name format "Redis {command}" and
// attributes: db.system (redis), db.operation, net.peer.name.
// =============================================================================

func TestProperty89_RedisOperationSpanCreation(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		tracer, exp, tp := newPropertyTestTracer()
		defer func() { _ = tp.Shutdown(context.Background()) }()

		addr := rapid.SampledFrom([]string{
			"redis:6379",
			"localhost:6379",
			"10.0.0.1:6380",
			"cache.internal:6379",
		}).Draw(rt, "addr")

		hook := NewRedisTracingHook(tracer, addr)

		// Simulate a Redis command by calling ProcessHook directly.
		cmdName := rapid.SampledFrom([]string{"GET", "SET", "DEL", "HGET", "SCAN", "PING", "EXPIRE"}).Draw(rt, "cmd")

		// Create a mock redis.Cmd to pass through the hook.
		cmd := redis.NewCmd(context.Background(), cmdName, "test-key")

		// Build the wrapped process function.
		processHook := hook.ProcessHook(func(ctx context.Context, c redis.Cmder) error {
			return nil // simulate successful command
		})

		// Execute the hook.
		err := processHook(context.Background(), cmd)
		if err != nil {
			rt.Fatalf("unexpected error from process hook: %v", err)
		}

		spans := exp.GetSpans()
		if len(spans) != 1 {
			rt.Fatalf("expected 1 span, got %d", len(spans))
		}

		s := spans[0]

		// Verify span name format: "Redis {COMMAND}".
		expectedName := fmt.Sprintf("Redis %s", cmdName)
		if s.Name != expectedName {
			rt.Fatalf("redis span name = %q, want %q", s.Name, expectedName)
		}

		// Verify db.system attribute.
		dbSystem, ok := findAttr(s.Attributes, "db.system")
		if !ok {
			rt.Fatal("attribute 'db.system' not found on redis span")
		}
		if dbSystem != "redis" {
			rt.Fatalf("db.system = %q, want %q", dbSystem, "redis")
		}

		// Verify db.operation attribute.
		dbOp, ok := findAttr(s.Attributes, "db.operation")
		if !ok {
			rt.Fatal("attribute 'db.operation' not found on redis span")
		}
		if dbOp != cmdName {
			rt.Fatalf("db.operation = %q, want %q", dbOp, cmdName)
		}

		// Verify net.peer.name attribute is present and non-empty.
		peerName, ok := findAttr(s.Attributes, "net.peer.name")
		if !ok {
			rt.Fatal("attribute 'net.peer.name' not found on redis span")
		}
		if peerName == "" {
			rt.Fatal("net.peer.name should not be empty")
		}

		exp.Reset()
	})
}

// TestProperty89_RedisOperationSpanError verifies that Redis command errors
// are properly recorded on the span.
func TestProperty89_RedisOperationSpanError(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		tracer, exp, tp := newPropertyTestTracer()
		defer func() { _ = tp.Shutdown(context.Background()) }()

		hook := NewRedisTracingHook(tracer, "redis:6379")

		errMsg := rapid.StringMatching(`[a-zA-Z0-9 ]{5,30}`).Draw(rt, "errMsg")
		simulatedErr := errors.New(errMsg)

		cmd := redis.NewCmd(context.Background(), "GET", "key")

		processHook := hook.ProcessHook(func(ctx context.Context, c redis.Cmder) error {
			return simulatedErr
		})

		err := processHook(context.Background(), cmd)
		if err == nil {
			rt.Fatal("expected error from process hook")
		}

		spans := exp.GetSpans()
		if len(spans) != 1 {
			rt.Fatalf("expected 1 span, got %d", len(spans))
		}

		s := spans[0]

		// Verify span status is Error.
		if s.Status.Code != codes.Error {
			rt.Fatalf("redis error span status = %v, want Error", s.Status.Code)
		}

		// Verify error event was recorded.
		if len(s.Events) == 0 {
			rt.Fatal("expected at least one span event for the redis error")
		}

		exp.Reset()
	})
}
