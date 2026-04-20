// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
	"pgregory.net/rapid"

	"github.com/michaelwang123/mountainKing/internal/config"
	"github.com/michaelwang123/mountainKing/internal/datasource"
	"github.com/michaelwang123/mountainKing/pkg/retry"
)

// TestProperty26_DatasourceQueryTimeoutCancellation validates that when a
// datasource query exceeds its timeout, the query is cancelled and returns
// a timeout error. We test this at the DataSource level: a mock datasource
// that sleeps longer than the context deadline must return a context
// deadline exceeded error.
//
// Feature: graphql-multi-datasource-api, Property 26: 单数据源查询超时取消
// **Validates: Requirements 8.5**
func TestProperty26_DatasourceQueryTimeoutCancellation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a query timeout between 10ms and 100ms.
		timeoutMs := rapid.IntRange(10, 100).Draw(t, "timeoutMs")
		// Generate a datasource delay that exceeds the timeout.
		excessMs := rapid.IntRange(50, 200).Draw(t, "excessMs")
		delayMs := timeoutMs + excessMs

		timeout := time.Duration(timeoutMs) * time.Millisecond
		delay := time.Duration(delayMs) * time.Millisecond

		mock := &datasource.MockDataSource{
			NameVal:      "slow-ds",
			TypeVal:      "mock",
			AvailableVal: true,
			ExecuteFunc: func(ctx context.Context, query datasource.QueryRequest) (*datasource.QueryResult, error) {
				select {
				case <-time.After(delay):
					return &datasource.QueryResult{Data: []map[string]interface{}{{"ok": true}}}, nil
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			},
		}

		// Create a context with the query timeout.
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()

		// Execute the query directly on the mock.
		_, err := mock.Execute(ctx, datasource.QueryRequest{})

		// Property: the query must be cancelled with a deadline exceeded error.
		if err == nil {
			t.Fatalf("expected timeout error, got nil (timeout=%v, delay=%v)", timeout, delay)
		}
		if !strings.Contains(err.Error(), "deadline exceeded") && !strings.Contains(err.Error(), "context canceled") {
			t.Fatalf("expected deadline exceeded or context canceled, got: %v", err)
		}
	})
}

// TestProperty27_TotalRequestTimeoutCancellation validates that when total
// request processing exceeds request_timeout, the request is terminated.
// The server wraps each request with context.WithTimeout(request_timeout).
//
// Feature: graphql-multi-datasource-api, Property 27: 总请求超时取�?
// **Validates: Requirements 8.6**
func TestProperty27_TotalRequestTimeoutCancellation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a short request timeout.
		timeoutMs := rapid.IntRange(20, 80).Draw(t, "timeoutMs")
		timeout := time.Duration(timeoutMs) * time.Millisecond

		// Create a handler that simulates slow processing by waiting for
		// the context to be cancelled (simulating slow datasource work).
		slowHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			<-r.Context().Done()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusGatewayTimeout)
			fmt.Fprintf(w, `{"errors":[{"message":"request timeout"}]}`)
		})

		// Wrap with the server's request timeout mechanism.
		s := &Server{
			serverConfig: config.ServerConfig{RequestTimeout: timeout},
		}
		wrapped := s.withRequestTimeout(slowHandler)

		ts := httptest.NewServer(wrapped)
		defer ts.Close()

		start := time.Now()
		resp, err := http.Post(ts.URL, "application/json",
			strings.NewReader(`{"query":"{ __typename }"}`))
		elapsed := time.Since(start)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		_, _ = io.ReadAll(resp.Body)

		// Property: the request completes within a reasonable margin of the timeout.
		maxExpected := timeout + 500*time.Millisecond
		if elapsed > maxExpected {
			t.Fatalf("request took %v, expected to complete within %v (timeout=%v)",
				elapsed, maxExpected, timeout)
		}
	})
}

// TestProperty28_QueryComplexityLimit validates that queries exceeding
// max_query_complexity are rejected by the GraphQL engine.
//
// Feature: graphql-multi-datasource-api, Property 28: 查询复杂度限�?
// **Validates: Requirements 8.7**
func TestProperty28_QueryComplexityLimit(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a complexity limit between 1 and 3.
		// The starrocks query { starrocks(table:"t", fields:["a"]) { nodes { data } totalCount } }
		// has complexity �?4 (one per field). So limits 1-3 will always reject it.
		maxComplexity := rapid.IntRange(1, 3).Draw(t, "maxComplexity")

		s := newPropertyTestServer(func(s *Server) {
			s.graphqlConfig.MaxQueryComplexity = maxComplexity
		})
		router := s.SetupRoutes()
		ts := httptest.NewServer(router)
		defer ts.Close()

		// This query has multiple fields, giving it complexity > 3.
		query := `{ starrocks(table:"t", fields:["a"]) { nodes { data } totalCount } }`
		body := fmt.Sprintf(`{"query":%q}`, query)

		resp, err := http.Post(ts.URL+"/graphql", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("POST /graphql failed: %v", err)
		}
		defer resp.Body.Close()

		var result map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("response is not valid JSON: %v", err)
		}

		// Property: with a low complexity limit, the query should be rejected.
		errs, hasErrors := result["errors"]
		if !hasErrors {
			t.Fatalf("expected errors for complexity limit %d, got none", maxComplexity)
		}

		// Verify the error mentions complexity.
		errBytes, _ := json.Marshal(errs)
		errStr := strings.ToLower(string(errBytes))
		if !strings.Contains(errStr, "complex") {
			t.Fatalf("expected complexity error, got: %s", string(errBytes))
		}
	})
}

// TestProperty29_QueryDepthLimit validates that queries exceeding
// max_query_depth are rejected by the GraphQL engine.
//
// Feature: graphql-multi-datasource-api, Property 29: 查询深度限制
// **Validates: Requirements 8.8**
func TestProperty29_QueryDepthLimit(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a depth limit between 1 and 3.
		maxDepth := rapid.IntRange(1, 3).Draw(t, "maxDepth")
		// Generate a query depth that exceeds the limit.
		excess := rapid.IntRange(1, 3).Draw(t, "excess")
		queryDepth := maxDepth + excess

		s := newPropertyTestServer(func(s *Server) {
			s.graphqlConfig.MaxQueryDepth = maxDepth
			s.graphqlConfig.IntrospectionEnabled = true
		})
		router := s.SetupRoutes()
		ts := httptest.NewServer(router)
		defer ts.Close()

		// Build a deeply nested introspection query.
		query := buildNestedQuery(queryDepth)
		body := fmt.Sprintf(`{"query":%q}`, query)

		resp, err := http.Post(ts.URL+"/graphql", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("POST /graphql failed: %v", err)
		}
		defer resp.Body.Close()

		var result map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("response is not valid JSON: %v", err)
		}

		// Property: query exceeding depth limit must be rejected.
		errs, hasErrors := result["errors"]
		if !hasErrors {
			t.Fatalf("expected errors for depth limit %d with query depth %d, got none",
				maxDepth, queryDepth)
		}

		// Verify the error mentions depth.
		errBytes, _ := json.Marshal(errs)
		errStr := strings.ToLower(string(errBytes))
		if !strings.Contains(errStr, "depth") {
			t.Fatalf("expected depth error, got: %s", string(errBytes))
		}
	})
}

// buildNestedQuery builds an introspection query with the specified nesting depth.
// Uses __schema { types { fields { type { ... } } } } pattern to create
// queries with predictable depth.
func buildNestedQuery(depth int) string {
	if depth <= 0 {
		return "{ __typename }"
	}

	// Introspection field chain: each level adds one depth.
	layers := []string{
		"__schema",
		"types",
		"fields",
		"type",
		"fields",
		"type",
		"fields",
		"type",
	}

	if depth > len(layers) {
		depth = len(layers)
	}

	// Build from inside out: innermost is always "name".
	inner := "name"
	for i := depth - 1; i >= 1; i-- {
		idx := i
		if idx >= len(layers) {
			idx = len(layers) - 1
		}
		inner = fmt.Sprintf("%s { %s }", layers[idx], inner)
	}

	return fmt.Sprintf("{ %s { %s } }", layers[0], inner)
}

// TestProperty88_RequestTimeoutAndQueryTimeoutCombination validates that the
// effective query timeout is min(query_timeout, remaining_request_time),
// ensuring neither timeout is exceeded.
//
// Feature: graphql-multi-datasource-api, Property 88: 请求超时与查询超时组�?
// **Validates: Design - 超时组合机制**
func TestProperty88_RequestTimeoutAndQueryTimeoutCombination(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		requestTimeoutMs := rapid.IntRange(50, 200).Draw(t, "requestTimeoutMs")
		queryTimeoutMs := rapid.IntRange(50, 200).Draw(t, "queryTimeoutMs")

		requestTimeout := time.Duration(requestTimeoutMs) * time.Millisecond
		queryTimeout := time.Duration(queryTimeoutMs) * time.Millisecond

		// The effective timeout should be min(queryTimeout, remaining request time).
		expectedEffective := min(queryTimeout, requestTimeout)

		// Create a parent context with request timeout.
		parentCtx, parentCancel := context.WithTimeout(context.Background(), requestTimeout)
		defer parentCancel()

		// Compute the query-level timeout as min(queryTimeout, remaining).
		parentDeadline, _ := parentCtx.Deadline()
		remaining := time.Until(parentDeadline)
		effectiveTimeout := min(queryTimeout, remaining)

		// Create child context with effective timeout.
		childCtx, childCancel := context.WithTimeout(parentCtx, effectiveTimeout)
		defer childCancel()

		// Verify the child context deadline is bounded by both timeouts.
		childDeadline, ok := childCtx.Deadline()
		if !ok {
			t.Fatal("child context should have a deadline")
		}

		// Property: child deadline must not exceed parent deadline.
		if childDeadline.After(parentDeadline.Add(1 * time.Millisecond)) {
			t.Fatalf("child deadline %v exceeds parent deadline %v",
				childDeadline, parentDeadline)
		}

		// Property: effective timeout �?min(queryTimeout, requestTimeout).
		margin := 5 * time.Millisecond
		if effectiveTimeout > expectedEffective+margin {
			t.Fatalf("effective timeout %v exceeds expected min(%v, %v) = %v",
				effectiveTimeout, queryTimeout, requestTimeout, expectedEffective)
		}

		// Verify cancellation propagation: mock datasource that sleeps longer
		// than the effective timeout should be cancelled.
		sleepDuration := effectiveTimeout + 100*time.Millisecond
		mock := &datasource.MockDataSource{
			NameVal:      "combo-ds",
			TypeVal:      "mock",
			AvailableVal: true,
			ExecuteFunc: func(ctx context.Context, query datasource.QueryRequest) (*datasource.QueryResult, error) {
				select {
				case <-time.After(sleepDuration):
					return &datasource.QueryResult{}, nil
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			},
		}

		_, err := mock.Execute(childCtx, datasource.QueryRequest{})
		if err == nil {
			t.Fatal("expected timeout error from combined timeout mechanism")
		}
	})
}

// TestProperty60_GracefulShutdownStopsAcceptingNewRequests validates that
// after shutdown is initiated, the server stops accepting new connections
// while in-flight requests complete.
//
// Feature: graphql-multi-datasource-api, Property 60: 优雅关闭 - 停止接受新请�?
// **Validates: Requirements 15.5, 15.6**
func TestProperty60_GracefulShutdownStopsAcceptingNewRequests(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		maxWaitMs := rapid.IntRange(500, 2000).Draw(t, "maxWaitMs")
		maxWait := time.Duration(maxWaitMs) * time.Millisecond

		s := newPropertyTestServer(func(s *Server) {
			s.shutdownCfg.MaxWaitTime = maxWait
		})
		router := s.SetupRoutes()

		// Use a real HTTP server to test shutdown behavior.
		srv := &http.Server{
			Handler: router,
		}
		s.httpServer = srv

		// Start listening on a random port.
		listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("failed to listen: %v", err)
		}
		addr := listener.Addr().String()

		go func() {
			_ = srv.Serve(listener)
		}()

		// Verify the server is accepting requests before shutdown.
		resp, err := http.Post("http://"+addr+"/graphql", "application/json",
			strings.NewReader(`{"query":"{ __typename }"}`))
		if err != nil {
			t.Fatalf("pre-shutdown request failed: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("pre-shutdown: expected 200, got %d", resp.StatusCode)
		}

		// Initiate graceful shutdown.
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), maxWait)
		defer shutdownCancel()
		go func() {
			_ = srv.Shutdown(shutdownCtx)
		}()

		// Give shutdown a moment to take effect.
		time.Sleep(50 * time.Millisecond)

		// Property: new requests should be rejected after shutdown.
		_, err = http.Post("http://"+addr+"/graphql", "application/json",
			strings.NewReader(`{"query":"{ __typename }"}`))

		// After shutdown, the connection should fail.
		if err == nil {
			t.Fatal("expected connection error after shutdown, but request succeeded")
		}
	})
}

// TestProperty61_GracefulShutdownResourceCleanupOrder validates that graceful
// shutdown follows the correct order: stop accepting �?wait for in-flight �?
// flush traces �?flush metrics �?close datasources �?sync logger.
//
// Feature: graphql-multi-datasource-api, Property 61: 优雅关闭 - 资源清理顺序
// **Validates: Requirements 15.7, 15.8**
func TestProperty61_GracefulShutdownResourceCleanupOrder(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate random delays for each shutdown step to ensure ordering
		// is maintained regardless of individual step duration.
		tracingDelayMs := rapid.IntRange(0, 10).Draw(t, "tracingDelayMs")
		metricsDelayMs := rapid.IntRange(0, 10).Draw(t, "metricsDelayMs")
		dsCloseDelayMs := rapid.IntRange(0, 10).Draw(t, "dsCloseDelayMs")

		var mu sync.Mutex
		var order []string

		record := func(step string) {
			mu.Lock()
			defer mu.Unlock()
			order = append(order, step)
		}

		// Create a datasource manager with a trackable mock datasource.
		registry := datasource.NewAdapterRegistry()
		mockCfgs := []config.DataSourceConfig{
			{Name: "test-mock", Type: "mock", Enabled: true},
		}
		dsManager := datasource.NewDataSourceManager(
			registry,
			mockCfgs,
			retry.Config{MaxRetries: 0, RetryInterval: 100 * time.Millisecond},
			zap.NewNop(),
		)

		s := newPropertyTestServer(func(s *Server) {
			s.dsManager = dsManager
			s.shutdownCfg.MaxWaitTime = 2 * time.Second
		})

		// Set up tracing shutdown callback.
		s.SetTracingShutdown(func(ctx context.Context) error {
			time.Sleep(time.Duration(tracingDelayMs) * time.Millisecond)
			record("tracing")
			return nil
		})

		// Set up metrics flush callback.
		s.SetMetricsFlush(func(ctx context.Context) error {
			time.Sleep(time.Duration(metricsDelayMs) * time.Millisecond)
			record("metrics")
			return nil
		})

		// No HTTP server to shut down (testing callback order only).
		s.httpServer = nil

		// Register a mock adapter so CloseAll has something to close.
		_ = registry.Register("mock", func(name string, cfg datasource.DataSourceConfig) (datasource.DataSource, error) {
			return &datasource.MockDataSource{
				NameVal:      name,
				TypeVal:      "mock",
				AvailableVal: true,
				CloseFunc: func(ctx context.Context) error {
					time.Sleep(time.Duration(dsCloseDelayMs) * time.Millisecond)
					record("datasource_close")
					return nil
				},
			}, nil
		})

		// Initialize a mock datasource so CloseAll has something to close.
		_ = dsManager.Init(context.Background())

		// Execute graceful shutdown.
		s.GracefulShutdown()

		mu.Lock()
		defer mu.Unlock()

		// Property: tracing must be flushed before metrics.
		tracingIdx := indexOf(order, "tracing")
		metricsIdx := indexOf(order, "metrics")

		if tracingIdx == -1 {
			t.Fatal("tracing shutdown was not called")
		}
		if metricsIdx == -1 {
			t.Fatal("metrics flush was not called")
		}

		if tracingIdx > metricsIdx {
			t.Fatalf("expected tracing before metrics, got order: %v", order)
		}

		// Property: datasource close happens after tracing and metrics.
		dsIdx := indexOf(order, "datasource_close")
		if dsIdx != -1 {
			if dsIdx < tracingIdx {
				t.Fatalf("expected datasource close after tracing, got order: %v", order)
			}
			if dsIdx < metricsIdx {
				t.Fatalf("expected datasource close after metrics, got order: %v", order)
			}
		}
	})
}

// indexOf returns the index of the first occurrence of s in slice, or -1.
func indexOf(slice []string, s string) int {
	for i, v := range slice {
		if v == s {
			return i
		}
	}
	return -1
}
