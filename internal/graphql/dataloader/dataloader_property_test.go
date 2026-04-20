// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package dataloader

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/michaelwang123/mountainKing/internal/config"
	"github.com/michaelwang123/mountainKing/internal/datasource"
	"github.com/michaelwang123/mountainKing/pkg/retry"
	"go.uber.org/zap"
)

// TestProperty82_DataLoaderPerRequestIsolation validates that DataLoader
// instances are per-request isolated. Each DataLoader has independent state
// and results from one DataLoader never leak to another.
//
// Feature: graphql-multi-datasource-api, Property 82: DataLoader Per-Request 隔离
// **Validates: Design - DataLoader 生命周期**
func TestProperty82_DataLoaderPerRequestIsolation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		numRequests := rapid.IntRange(2, 10).Draw(t, "numRequests")

		// Track which DataLoader instance triggered each execute call.
		// We use a per-request unique marker injected via QueryRequest.Options.
		var executeCalls atomic.Int32
		var mu sync.Mutex
		resultsByMarker := make(map[string]string) // marker →datasource response

		registry := datasource.NewAdapterRegistry()
		err := registry.Register("mock", func(name string, cfg datasource.DataSourceConfig) (datasource.DataSource, error) {
			return &datasource.MockDataSource{
				NameVal:      name,
				TypeVal:      "mock",
				AvailableVal: true,
				ConnectFunc:  func(ctx context.Context) error { return nil },
				ExecuteFunc: func(ctx context.Context, q datasource.QueryRequest) (*datasource.QueryResult, error) {
					executeCalls.Add(1)
					marker := ""
					if q.Options != nil {
						if m, ok := q.Options["marker"].(string); ok {
							marker = m
						}
					}
					response := fmt.Sprintf("response_for_%s", marker)
					mu.Lock()
					resultsByMarker[marker] = response
					mu.Unlock()
					return &datasource.QueryResult{
						Data: []map[string]interface{}{{"marker": marker, "response": response}},
					}, nil
				},
			}, nil
		})
		if err != nil {
			t.Fatalf("register failed: %v", err)
		}

		logger, _ := zap.NewDevelopment()
		mgr := datasource.NewDataSourceManager(
			registry,
			[]config.DataSourceConfig{
				{Name: "ds1", Type: "mock", Enabled: true, Connection: map[string]interface{}{}, Options: map[string]interface{}{}},
			},
			retry.Config{MaxRetries: 0, RetryInterval: time.Millisecond},
			logger,
		)
		if err := mgr.Init(context.Background()); err != nil {
			t.Fatalf("init failed: %v", err)
		}

		// Create N independent DataLoader instances (simulating N requests).
		type requestResult struct {
			marker string
			result *datasource.QueryResult
			err    error
		}

		results := make([]requestResult, numRequests)
		var wg sync.WaitGroup
		wg.Add(numRequests)

		for i := 0; i < numRequests; i++ {
			go func(idx int) {
				defer wg.Done()
				dl := New(mgr)
				defer dl.Close()

				marker := fmt.Sprintf("req_%d", idx)
				res, err := dl.Load(context.Background(), "ds1", datasource.QueryRequest{
					Options: map[string]interface{}{"marker": marker},
				})
				results[idx] = requestResult{marker: marker, result: res, err: err}
			}(i)
		}
		wg.Wait()

		// Property 1: Each DataLoader produced an independent execute call.
		if int(executeCalls.Load()) != numRequests {
			t.Fatalf("expected %d independent execute calls, got %d", numRequests, executeCalls.Load())
		}

		// Property 2: Each result corresponds to its own request marker (no cross-leak).
		seenMarkers := make(map[string]bool)
		for i, r := range results {
			if r.err != nil {
				t.Fatalf("request %d error: %v", i, r.err)
			}
			if r.result == nil || len(r.result.Data) == 0 {
				t.Fatalf("request %d returned empty result", i)
			}

			gotMarker, ok := r.result.Data[0]["marker"].(string)
			if !ok {
				t.Fatalf("request %d: marker not found in result", i)
			}

			// The result marker must match the request marker.
			if gotMarker != r.marker {
				t.Fatalf("DATA LEAK: request %d sent marker %q but got result with marker %q",
					i, r.marker, gotMarker)
			}

			if seenMarkers[gotMarker] {
				t.Fatalf("duplicate marker %q in results", gotMarker)
			}
			seenMarkers[gotMarker] = true
		}

		// Property 3: All markers are unique and accounted for.
		if len(seenMarkers) != numRequests {
			t.Fatalf("expected %d unique markers, got %d", numRequests, len(seenMarkers))
		}
	})
}
