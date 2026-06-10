// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package resolver

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/michaelwang123/mountainKing/internal/config"
)

// setupTestTracer configures an in-memory span exporter and returns
// the exporter for assertion and a cleanup function to restore the global provider.
func setupTestTracer(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
	)
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
	})
	return exporter
}

func newTestMutationResolver() *mutationResolver {
	mutCfg := config.MutationsConfig{Enabled: true, DatasourceName: "test_ds"}
	ptr := &atomic.Pointer[config.MutationsConfig]{}
	ptr.Store(&mutCfg)
	return &mutationResolver{
		&Resolver{
			MutationConfig: ptr,
		},
	}
}

func TestTraceMutation_Success(t *testing.T) {
	exporter := setupTestTracer(t)
	r := newTestMutationResolver()

	affected, err := r.traceMutation(context.Background(), "insert", "orders", func(ctx context.Context) (int64, error) {
		return 5, nil
	})

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if affected != 5 {
		t.Fatalf("expected 5 affected rows, got %d", affected)
	}

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	span := spans[0]

	// Check span name.
	if span.Name != "mutation.insert" {
		t.Errorf("expected span name 'mutation.insert', got %q", span.Name)
	}

	// Check span status.
	if span.Status.Code != codes.Ok {
		t.Errorf("expected status Ok, got %v", span.Status.Code)
	}

	// Check attributes.
	attrs := attributeMap(span.Attributes)
	assertAttribute(t, attrs, "db.system", "starrocks")
	assertAttribute(t, attrs, "db.operation", "insert")
	assertAttribute(t, attrs, "db.table", "orders")
	assertInt64Attribute(t, attrs, "db.affected_rows", 5)
}

func TestTraceMutation_Error(t *testing.T) {
	exporter := setupTestTracer(t)
	r := newTestMutationResolver()

	testErr := errors.New("connection refused")
	affected, err := r.traceMutation(context.Background(), "delete", "events", func(ctx context.Context) (int64, error) {
		return 0, testErr
	})

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if affected != 0 {
		t.Fatalf("expected 0 affected rows, got %d", affected)
	}

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	span := spans[0]

	// Check span name.
	if span.Name != "mutation.delete" {
		t.Errorf("expected span name 'mutation.delete', got %q", span.Name)
	}

	// Check span status is Error.
	if span.Status.Code != codes.Error {
		t.Errorf("expected status Error, got %v", span.Status.Code)
	}
	if span.Status.Description != "connection refused" {
		t.Errorf("expected status description 'connection refused', got %q", span.Status.Description)
	}

	// Check error event was recorded.
	if len(span.Events) == 0 {
		t.Error("expected at least one event (error recording)")
	}

	// Check attributes (no db.affected_rows on error).
	attrs := attributeMap(span.Attributes)
	assertAttribute(t, attrs, "db.system", "starrocks")
	assertAttribute(t, attrs, "db.operation", "delete")
	assertAttribute(t, attrs, "db.table", "events")
	if _, ok := attrs["db.affected_rows"]; ok {
		t.Error("expected no db.affected_rows attribute on error")
	}
}

func TestTraceMutation_OperationNames(t *testing.T) {
	tests := []struct {
		operation string
		wantSpan  string
	}{
		{"insert", "mutation.insert"},
		{"update", "mutation.update"},
		{"delete", "mutation.delete"},
		{"insertBatch", "mutation.insertBatch"},
	}

	for _, tc := range tests {
		t.Run(tc.operation, func(t *testing.T) {
			exporter := setupTestTracer(t)
			r := newTestMutationResolver()

			_, _ = r.traceMutation(context.Background(), tc.operation, "test_table", func(ctx context.Context) (int64, error) {
				return 1, nil
			})

			spans := exporter.GetSpans()
			if len(spans) != 1 {
				t.Fatalf("expected 1 span, got %d", len(spans))
			}
			if spans[0].Name != tc.wantSpan {
				t.Errorf("expected span name %q, got %q", tc.wantSpan, spans[0].Name)
			}
		})
	}
}

func TestTraceMutation_ContextPropagation(t *testing.T) {
	_ = setupTestTracer(t)
	r := newTestMutationResolver()

	// Verify the fn receives a context with the span.
	var fnCtxHasSpan bool
	_, _ = r.traceMutation(context.Background(), "update", "users", func(ctx context.Context) (int64, error) {
		// The context should have a valid span from traceMutation.
		span := trace.SpanFromContext(ctx)
		fnCtxHasSpan = span.SpanContext().IsValid()
		return 3, nil
	})

	if !fnCtxHasSpan {
		t.Error("expected fn to receive context with valid span")
	}
}

// attributeMap converts a slice of KeyValue attributes into a map for easy lookup.
func attributeMap(attrs []attribute.KeyValue) map[string]attribute.Value {
	m := make(map[string]attribute.Value, len(attrs))
	for _, a := range attrs {
		m[string(a.Key)] = a.Value
	}
	return m
}

// assertAttribute checks that a string attribute has the expected value.
func assertAttribute(t *testing.T, attrs map[string]attribute.Value, key, expected string) {
	t.Helper()
	v, ok := attrs[key]
	if !ok {
		t.Errorf("expected attribute %q not found", key)
		return
	}
	if v.AsString() != expected {
		t.Errorf("attribute %q: expected %q, got %q", key, expected, v.AsString())
	}
}

// assertInt64Attribute checks that an int64 attribute has the expected value.
func assertInt64Attribute(t *testing.T, attrs map[string]attribute.Value, key string, expected int64) {
	t.Helper()
	v, ok := attrs[key]
	if !ok {
		t.Errorf("expected attribute %q not found", key)
		return
	}
	if v.AsInt64() != expected {
		t.Errorf("attribute %q: expected %d, got %d", key, expected, v.AsInt64())
	}
}
