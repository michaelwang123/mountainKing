// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package prometheus

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"

	"github.com/michaelwang123/mountainKing/internal/datasource"
)

// newTestServer creates an httptest.Server that routes Prometheus API paths.
func newTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(handler)
}

// newTestAdapter creates a Prometheus Adapter pointing at the given test server URL.
func newTestAdapter(t *testing.T, baseURL string) *Adapter {
	t.Helper()
	cfg := datasource.DataSourceConfig{
		Name:    "test-prom",
		Type:    "prometheus",
		Enabled: true,
		Connection: map[string]any{
			"url": baseURL,
		},
		Options: map[string]any{},
	}
	adapter, err := NewAdapter("test-prom", cfg, zap.NewNop())
	if err != nil {
		t.Fatalf("NewAdapter() error: %v", err)
	}
	return adapter
}

func TestConnect_Success(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/status/buildinfo" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"success","data":{"version":"2.45.0"}}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	defer ts.Close()

	adapter := newTestAdapter(t, ts.URL)
	ctx := context.Background()

	if err := adapter.Connect(ctx); err != nil {
		t.Fatalf("Connect() error: %v", err)
	}

	if !adapter.IsAvailable() {
		t.Error("expected adapter to be available after Connect")
	}
}

func TestConnect_Fail(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	})
	defer ts.Close()

	adapter := newTestAdapter(t, ts.URL)
	ctx := context.Background()

	err := adapter.Connect(ctx)
	if err == nil {
		t.Fatal("expected Connect to fail with 500 response")
	}

	if adapter.IsAvailable() {
		t.Error("expected adapter to be unavailable after failed Connect")
	}
}

func TestExecute_InstantQuery(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/status/buildinfo":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"success","data":{"version":"2.45.0"}}`))
		case "/api/v1/query":
			resp := map[string]any{
				"status": "success",
				"data": map[string]any{
					"resultType": "vector",
					"result": []map[string]any{
						{
							"metric": map[string]string{"__name__": "up", "instance": "localhost:9090"},
							"value":  []any{1234567890.0, "1"},
						},
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	defer ts.Close()

	adapter := newTestAdapter(t, ts.URL)
	ctx := context.Background()

	if err := adapter.Connect(ctx); err != nil {
		t.Fatalf("Connect() error: %v", err)
	}

	req := datasource.QueryRequest{
		Options: map[string]any{
			"query": "up",
		},
	}

	result, err := adapter.Execute(ctx, req)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	if len(result.Data) != 1 {
		t.Errorf("expected 1 result row, got %d", len(result.Data))
	}
}

func TestExecute_RangeQuery(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/status/buildinfo":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"success","data":{"version":"2.45.0"}}`))
		case "/api/v1/query_range":
			resp := map[string]any{
				"status": "success",
				"data": map[string]any{
					"resultType": "matrix",
					"result": []map[string]any{
						{
							"metric": map[string]string{"__name__": "up"},
							"values": [][]any{
								{1234567890.0, "1"},
								{1234567900.0, "1"},
							},
						},
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	defer ts.Close()

	adapter := newTestAdapter(t, ts.URL)
	ctx := context.Background()

	if err := adapter.Connect(ctx); err != nil {
		t.Fatalf("Connect() error: %v", err)
	}

	req := datasource.QueryRequest{
		Options: map[string]any{
			"query":     "up",
			"startTime": "2024-01-01T00:00:00Z",
			"endTime":   "2024-01-01T01:00:00Z",
			"step":      "15s",
		},
	}

	result, err := adapter.Execute(ctx, req)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	if len(result.Data) != 1 {
		t.Errorf("expected 1 series, got %d", len(result.Data))
	}
}

func TestExecute_Error(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/status/buildinfo":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"success","data":{"version":"2.45.0"}}`))
		case "/api/v1/query":
			resp := map[string]any{
				"status":    "error",
				"errorType": "bad_data",
				"error":     "invalid query",
			}
			w.Header().Set("Content-Type", "application/json")
			// Prometheus returns 422 for bad queries but sometimes 200 with error status
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	defer ts.Close()

	adapter := newTestAdapter(t, ts.URL)
	ctx := context.Background()

	if err := adapter.Connect(ctx); err != nil {
		t.Fatalf("Connect() error: %v", err)
	}

	req := datasource.QueryRequest{
		Options: map[string]any{
			"query": "invalid{",
		},
	}

	_, err := adapter.Execute(ctx, req)
	if err == nil {
		t.Fatal("expected Execute to return error for invalid query response")
	}
}

func TestHealthCheck_Success(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/status/buildinfo" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"success","data":{"version":"2.45.0"}}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	defer ts.Close()

	adapter := newTestAdapter(t, ts.URL)
	ctx := context.Background()

	// Connect first
	if err := adapter.Connect(ctx); err != nil {
		t.Fatalf("Connect() error: %v", err)
	}

	if err := adapter.HealthCheck(ctx); err != nil {
		t.Fatalf("HealthCheck() error: %v", err)
	}

	if !adapter.IsAvailable() {
		t.Error("expected adapter to be available after successful health check")
	}
}

func TestHealthCheck_Fail(t *testing.T) {
	callCount := 0
	ts := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			// First call (Connect) succeeds
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"success","data":{"version":"2.45.0"}}`))
			return
		}
		// Subsequent calls (HealthCheck) fail
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	defer ts.Close()

	adapter := newTestAdapter(t, ts.URL)
	ctx := context.Background()

	if err := adapter.Connect(ctx); err != nil {
		t.Fatalf("Connect() error: %v", err)
	}

	err := adapter.HealthCheck(ctx)
	if err == nil {
		t.Fatal("expected HealthCheck to return error")
	}

	if adapter.IsAvailable() {
		t.Error("expected adapter to be unavailable after failed health check")
	}
}

func TestClose(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"success","data":{"version":"2.45.0"}}`))
	})
	defer ts.Close()

	adapter := newTestAdapter(t, ts.URL)
	ctx := context.Background()

	if err := adapter.Connect(ctx); err != nil {
		t.Fatalf("Connect() error: %v", err)
	}

	if err := adapter.Close(ctx); err != nil {
		t.Fatalf("Close() error: %v", err)
	}

	if adapter.IsAvailable() {
		t.Error("expected adapter to be unavailable after Close")
	}
}
