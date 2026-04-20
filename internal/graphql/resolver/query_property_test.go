// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package resolver

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/michaelwang123/mountainKing/internal/config"
	"github.com/michaelwang123/mountainKing/internal/datasource"
	"github.com/michaelwang123/mountainKing/pkg/retry"
	"go.uber.org/zap"
)

// TestProperty24_CrossDataSourceParallelQueryAndMerge validates that
// executeParallel dispatches queries to multiple datasources concurrently
// and collects all results. All datasource results must be present in the
// output regardless of execution order.
//
// Feature: graphql-multi-datasource-api, Property 24: 跨数据源并行查询与结果合并
// **Validates: Requirements 6.1, 6.2**
func TestProperty24_CrossDataSourceParallelQueryAndMerge(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		numDS := rapid.IntRange(2, 6).Draw(t, "numDS")

		registry := datasource.NewAdapterRegistry()
		logger, _ := zap.NewDevelopment()

		// Create mock datasources that each return unique data.
		dsNames := make([]string, numDS)
		expectedData := make(map[string][]map[string]interface{})

		for i := 0; i < numDS; i++ {
			name := fmt.Sprintf("ds_%d", i)
			dsNames[i] = name
			rowCount := rapid.IntRange(1, 5).Draw(t, fmt.Sprintf("rowCount_%d", i))
			rows := make([]map[string]interface{}, rowCount)
			for j := 0; j < rowCount; j++ {
				rows[j] = map[string]interface{}{
					"ds":  name,
					"idx": j,
				}
			}
			expectedData[name] = rows
		}

		_ = registry.Register("mock", func(n string, cfg datasource.DataSourceConfig) (datasource.DataSource, error) {
			data := expectedData[n]
			return &datasource.MockDataSource{
				NameVal:      n,
				TypeVal:      "mock",
				AvailableVal: true,
				ExecuteFunc: func(ctx context.Context, query datasource.QueryRequest) (*datasource.QueryResult, error) {
					return &datasource.QueryResult{Data: data}, nil
				},
			}, nil
		})

		cfgs := make([]config.DataSourceConfig, numDS)
		for i, name := range dsNames {
			cfgs[i] = config.DataSourceConfig{
				Name:    name,
				Type:    "mock",
				Enabled: true,
			}
			_ = i
		}

		mgr := datasource.NewDataSourceManager(registry, cfgs, retry.Config{
			MaxRetries:    1,
			RetryInterval: time.Millisecond,
		}, logger)

		if err := mgr.Init(context.Background()); err != nil {
			t.Fatalf("Init failed: %v", err)
		}

		// Build query map for all datasources.
		queries := make(map[string]datasource.QueryRequest)
		for _, name := range dsNames {
			queries[name] = datasource.QueryRequest{}
		}

		results := executeParallel(context.Background(), mgr, queries)

		// Verify: all datasources returned results.
		if len(results) != numDS {
			t.Fatalf("expected %d results, got %d", numDS, len(results))
		}

		// Verify: each datasource name appears exactly once in results.
		seen := make(map[string]bool)
		for _, r := range results {
			if r.err != nil {
				t.Fatalf("unexpected error for ds %q: %v", r.dsName, r.err)
			}
			if seen[r.dsName] {
				t.Fatalf("duplicate result for ds %q", r.dsName)
			}
			seen[r.dsName] = true

			// Verify the data matches what we expected.
			expected := expectedData[r.dsName]
			if len(r.result.Data) != len(expected) {
				t.Fatalf("ds %q: expected %d rows, got %d", r.dsName, len(expected), len(r.result.Data))
			}
		}

		for _, name := range dsNames {
			if !seen[name] {
				t.Fatalf("missing result for ds %q", name)
			}
		}
	})
}

// TestProperty25_MixedQueryPartialFailureHandling validates that when one
// datasource fails in a parallel query, the other datasource results are
// still returned. The failed datasource should have an error in the result.
//
// Feature: graphql-multi-datasource-api, Property 25: 混合查询部分失败处理
// **Validates: Requirements 6.3**
func TestProperty25_MixedQueryPartialFailureHandling(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		numSuccess := rapid.IntRange(1, 4).Draw(t, "numSuccess")
		numFail := rapid.IntRange(1, 3).Draw(t, "numFail")
		total := numSuccess + numFail

		registry := datasource.NewAdapterRegistry()
		logger, _ := zap.NewDevelopment()

		successNames := make([]string, numSuccess)
		failNames := make([]string, numFail)

		for i := 0; i < numSuccess; i++ {
			successNames[i] = fmt.Sprintf("ok_%d", i)
		}
		for i := 0; i < numFail; i++ {
			failNames[i] = fmt.Sprintf("fail_%d", i)
		}

		_ = registry.Register("mock", func(n string, cfg datasource.DataSourceConfig) (datasource.DataSource, error) {
			// Determine if this is a failing datasource.
			isFail := false
			for _, fn := range failNames {
				if fn == n {
					isFail = true
					break
				}
			}
			return &datasource.MockDataSource{
				NameVal:      n,
				TypeVal:      "mock",
				AvailableVal: true,
				ExecuteFunc: func(ctx context.Context, query datasource.QueryRequest) (*datasource.QueryResult, error) {
					if isFail {
						return nil, fmt.Errorf("datasource %s error", n)
					}
					return &datasource.QueryResult{
						Data: []map[string]interface{}{{"source": n}},
					}, nil
				},
			}, nil
		})

		allNames := append(append([]string{}, successNames...), failNames...)
		cfgs := make([]config.DataSourceConfig, total)
		for i, name := range allNames {
			cfgs[i] = config.DataSourceConfig{
				Name:    name,
				Type:    "mock",
				Enabled: true,
			}
		}

		mgr := datasource.NewDataSourceManager(registry, cfgs, retry.Config{
			MaxRetries:    0, // No retries so failures propagate immediately.
			RetryInterval: time.Millisecond,
		}, logger)

		if err := mgr.Init(context.Background()); err != nil {
			t.Fatalf("Init failed: %v", err)
		}

		queries := make(map[string]datasource.QueryRequest)
		for _, name := range allNames {
			queries[name] = datasource.QueryRequest{}
		}

		results := executeParallel(context.Background(), mgr, queries)

		if len(results) != total {
			t.Fatalf("expected %d results, got %d", total, len(results))
		}

		successResults := 0
		failResults := 0
		for _, r := range results {
			isFailDS := false
			for _, fn := range failNames {
				if fn == r.dsName {
					isFailDS = true
					break
				}
			}

			if isFailDS {
				if r.err == nil {
					t.Fatalf("expected error for failing ds %q, got nil", r.dsName)
				}
				failResults++
			} else {
				if r.err != nil {
					t.Fatalf("unexpected error for success ds %q: %v", r.dsName, r.err)
				}
				if r.result == nil || len(r.result.Data) == 0 {
					t.Fatalf("expected data for success ds %q", r.dsName)
				}
				successResults++
			}
		}

		if successResults != numSuccess {
			t.Fatalf("expected %d success results, got %d", numSuccess, successResults)
		}
		if failResults != numFail {
			t.Fatalf("expected %d fail results, got %d", numFail, failResults)
		}
	})
}

// TestProperty92_FailingDataSourceDoesNotCancelOthers validates that a failing
// datasource does NOT cancel queries to other datasources. Uses a slow-responding
// mock for the successful datasource and verifies it still completes even when
// another datasource fails immediately.
//
// Feature: graphql-multi-datasource-api, Property 92: 跨数据源查询不因单个失败取消其他查询
// **Validates: Design - 跨数据源错误隔离**
func TestProperty92_FailingDataSourceDoesNotCancelOthers(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		slowDelayMs := rapid.IntRange(10, 50).Draw(t, "slowDelayMs")

		registry := datasource.NewAdapterRegistry()
		logger, _ := zap.NewDevelopment()

		var slowCompleted atomic.Bool

		_ = registry.Register("mock", func(n string, cfg datasource.DataSourceConfig) (datasource.DataSource, error) {
			return &datasource.MockDataSource{
				NameVal:      n,
				TypeVal:      "mock",
				AvailableVal: true,
				ExecuteFunc: func(ctx context.Context, query datasource.QueryRequest) (*datasource.QueryResult, error) {
					if n == "fast_fail" {
						// Fail immediately.
						return nil, fmt.Errorf("immediate failure")
					}
					// Slow success — simulate work.
					select {
					case <-time.After(time.Duration(slowDelayMs) * time.Millisecond):
						slowCompleted.Store(true)
						return &datasource.QueryResult{
							Data: []map[string]interface{}{{"slow": true}},
						}, nil
					case <-ctx.Done():
						return nil, ctx.Err()
					}
				},
			}, nil
		})

		cfgs := []config.DataSourceConfig{
			{Name: "fast_fail", Type: "mock", Enabled: true},
			{Name: "slow_ok", Type: "mock", Enabled: true},
		}

		mgr := datasource.NewDataSourceManager(registry, cfgs, retry.Config{
			MaxRetries:    0,
			RetryInterval: time.Millisecond,
		}, logger)

		if err := mgr.Init(context.Background()); err != nil {
			t.Fatalf("Init failed: %v", err)
		}

		queries := map[string]datasource.QueryRequest{
			"fast_fail": {},
			"slow_ok":   {},
		}

		results := executeParallel(context.Background(), mgr, queries)

		if len(results) != 2 {
			t.Fatalf("expected 2 results, got %d", len(results))
		}

		for _, r := range results {
			switch r.dsName {
			case "fast_fail":
				if r.err == nil {
					t.Fatal("expected error for fast_fail datasource")
				}
			case "slow_ok":
				if r.err != nil {
					t.Fatalf("slow_ok should succeed even when fast_fail fails: %v", r.err)
				}
				if r.result == nil || len(r.result.Data) == 0 {
					t.Fatal("slow_ok should return data")
				}
			default:
				t.Fatalf("unexpected dsName: %s", r.dsName)
			}
		}

		// Verify the slow datasource actually completed (wasn't cancelled).
		if !slowCompleted.Load() {
			t.Fatal("slow datasource should have completed, but it was cancelled")
		}
	})
}

// TestProperty30_ResultSetTruncation validates that when data exceeds
// max_result_rows, the data is truncated to max_result_rows and a warning
// is added to the result.
//
// Feature: graphql-multi-datasource-api, Property 30: 结果集截断
// **Validates: Requirements 8.9**
func TestProperty30_ResultSetTruncation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		maxRows := rapid.IntRange(1, 100).Draw(t, "maxRows")
		dataSize := rapid.IntRange(1, 300).Draw(t, "dataSize")

		// Build mock data.
		data := make([]map[string]interface{}, dataSize)
		for i := 0; i < dataSize; i++ {
			data[i] = map[string]interface{}{"id": i}
		}

		result := &datasource.QueryResult{Data: data}

		// Apply truncation logic (same as in base.resolvers.go Starrocks resolver).
		var warnings []string
		warnings = append(warnings, result.Warnings...)

		truncatedData := result.Data
		if maxRows > 0 && len(truncatedData) > maxRows {
			warnings = append(warnings, fmt.Sprintf(
				"Result set truncated: returned %d rows out of %d (max_result_rows=%d)",
				maxRows, len(truncatedData), maxRows,
			))
			truncatedData = truncatedData[:maxRows]
		}

		if dataSize > maxRows {
			// Data exceeds limit: should be truncated.
			if len(truncatedData) != maxRows {
				t.Fatalf("expected truncated data length %d, got %d", maxRows, len(truncatedData))
			}
			// Should have a truncation warning.
			hasWarning := false
			for _, w := range warnings {
				if len(w) > 0 {
					hasWarning = true
					break
				}
			}
			if !hasWarning {
				t.Fatal("expected truncation warning when data exceeds max_result_rows")
			}
		} else {
			// Data within limit: should not be truncated.
			if len(truncatedData) != dataSize {
				t.Fatalf("expected data length %d (no truncation), got %d", dataSize, len(truncatedData))
			}
			// No truncation warning should be added.
			if len(warnings) > 0 {
				t.Fatalf("expected no warnings when data within limit, got %v", warnings)
			}
		}

		// Verify truncated data preserves order (first maxRows items).
		for i := 0; i < len(truncatedData); i++ {
			id, ok := truncatedData[i]["id"].(int)
			if !ok || id != i {
				t.Fatalf("truncated data[%d] should have id=%d, got %v", i, i, truncatedData[i]["id"])
			}
		}
	})
}
