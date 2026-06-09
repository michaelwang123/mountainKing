// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package mock

import (
	"context"
	"testing"

	"github.com/michaelwang123/mountainKing/internal/datasource"
)

func newTestAdapter() *Adapter {
	return NewAdapter("test-mock", defaultTables())
}

func intPtr(v int) *int { return &v }

func TestExecute_QueryByTable(t *testing.T) {
	adapter := newTestAdapter()
	ctx := context.Background()

	result, err := adapter.Execute(ctx, datasource.QueryRequest{
		Options: map[string]any{"table": "demo_users"},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if len(result.Data) != 5 {
		t.Fatalf("expected 5 rows for demo_users, got %d", len(result.Data))
	}
}

func TestExecute_Pagination(t *testing.T) {
	adapter := newTestAdapter()
	ctx := context.Background()

	result, err := adapter.Execute(ctx, datasource.QueryRequest{
		Options: map[string]any{"table": "demo_users"},
		Pagination: &datasource.PaginationParams{
			Limit:  intPtr(3),
			Offset: intPtr(2),
		},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if len(result.Data) != 3 {
		t.Fatalf("expected 3 rows with Limit=3 Offset=2, got %d", len(result.Data))
	}
	// After offset=2, first row should be Carol Wang (id=3)
	if name, ok := result.Data[0]["name"].(string); !ok || name != "Carol Wang" {
		t.Fatalf("expected first row name 'Carol Wang', got %v", result.Data[0]["name"])
	}
}

func TestExecute_EmptyUnknownTable(t *testing.T) {
	adapter := newTestAdapter()
	ctx := context.Background()

	result, err := adapter.Execute(ctx, datasource.QueryRequest{
		Options: map[string]any{"table": "nonexistent"},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.Data != nil {
		t.Fatalf("expected nil data for unknown table, got %v", result.Data)
	}
}

func TestExecute_NeedCount(t *testing.T) {
	adapter := newTestAdapter()
	ctx := context.Background()

	result, err := adapter.Execute(ctx, datasource.QueryRequest{
		Options:   map[string]any{"table": "demo_users"},
		NeedCount: true,
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.TotalCount == nil {
		t.Fatal("expected TotalCount to be set when NeedCount=true")
	}
	if *result.TotalCount != 5 {
		t.Fatalf("expected TotalCount=5, got %d", *result.TotalCount)
	}
}

func TestHealthCheck(t *testing.T) {
	adapter := newTestAdapter()
	if err := adapter.HealthCheck(context.Background()); err != nil {
		t.Fatalf("HealthCheck should return nil, got %v", err)
	}
}

func TestConnectClose(t *testing.T) {
	adapter := newTestAdapter()
	ctx := context.Background()

	if err := adapter.Connect(ctx); err != nil {
		t.Fatalf("Connect should return nil, got %v", err)
	}
	if err := adapter.Close(ctx); err != nil {
		t.Fatalf("Close should return nil, got %v", err)
	}
}

func TestIsAvailable(t *testing.T) {
	adapter := newTestAdapter()
	if !adapter.IsAvailable() {
		t.Fatal("IsAvailable should return true")
	}
}

func TestNameType(t *testing.T) {
	adapter := newTestAdapter()

	if adapter.Name() != "test-mock" {
		t.Fatalf("expected Name()='test-mock', got %q", adapter.Name())
	}
	if adapter.Type() != "mock" {
		t.Fatalf("expected Type()='mock', got %q", adapter.Type())
	}
}

func TestExecute_OffsetBeyondData(t *testing.T) {
	adapter := newTestAdapter()
	ctx := context.Background()

	result, err := adapter.Execute(ctx, datasource.QueryRequest{
		Options: map[string]any{"table": "demo_users"},
		Pagination: &datasource.PaginationParams{
			Offset: intPtr(100), // far beyond 5 rows
		},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.Data != nil {
		t.Fatalf("expected nil data when offset exceeds rows, got %d rows", len(result.Data))
	}
}

func TestExecute_NegativeOffset(t *testing.T) {
	adapter := newTestAdapter()
	ctx := context.Background()

	// Negative offset should not panic; treated as 0
	result, err := adapter.Execute(ctx, datasource.QueryRequest{
		Options: map[string]any{"table": "demo_users"},
		Pagination: &datasource.PaginationParams{
			Offset: intPtr(-5),
		},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if len(result.Data) != 5 {
		t.Fatalf("expected 5 rows with negative offset (treated as 0), got %d", len(result.Data))
	}
}

func TestExecute_NegativeLimit(t *testing.T) {
	adapter := newTestAdapter()
	ctx := context.Background()

	// Negative limit should not panic; treated as 0
	result, err := adapter.Execute(ctx, datasource.QueryRequest{
		Options: map[string]any{"table": "demo_users"},
		Pagination: &datasource.PaginationParams{
			Limit: intPtr(-1),
		},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if len(result.Data) != 0 {
		t.Fatalf("expected 0 rows with negative limit (treated as 0), got %d", len(result.Data))
	}
}

func TestExecute_LimitZero(t *testing.T) {
	adapter := newTestAdapter()
	ctx := context.Background()

	result, err := adapter.Execute(ctx, datasource.QueryRequest{
		Options: map[string]any{"table": "demo_users"},
		Pagination: &datasource.PaginationParams{
			Limit: intPtr(0),
		},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if len(result.Data) != 0 {
		t.Fatalf("expected 0 rows with Limit=0, got %d", len(result.Data))
	}
}

func TestExecute_NilOptions(t *testing.T) {
	adapter := newTestAdapter()
	ctx := context.Background()

	// nil Options map should not panic
	result, err := adapter.Execute(ctx, datasource.QueryRequest{
		Options: nil,
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.Data != nil {
		t.Fatalf("expected nil data with nil Options, got %v", result.Data)
	}
}

func TestFactory(t *testing.T) {
	factory := Factory()
	ds, err := factory("test-factory", datasource.DataSourceConfig{
		Name: "test-factory",
		Type: "mock",
	})
	if err != nil {
		t.Fatalf("Factory returned error: %v", err)
	}
	if ds.Name() != "test-factory" {
		t.Fatalf("expected Name()='test-factory', got %q", ds.Name())
	}
	if ds.Type() != "mock" {
		t.Fatalf("expected Type()='mock', got %q", ds.Type())
	}
	if !ds.IsAvailable() {
		t.Fatal("expected IsAvailable()=true")
	}
}
