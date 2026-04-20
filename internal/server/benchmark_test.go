// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/michaelwang123/mountainKing/internal/cache"
	"github.com/michaelwang123/mountainKing/internal/config"
	"github.com/michaelwang123/mountainKing/internal/datasource"
	"github.com/michaelwang123/mountainKing/internal/graphql/generated"
	"github.com/michaelwang123/mountainKing/internal/graphql/resolver"
	"github.com/michaelwang123/mountainKing/pkg/retry"
)

// newBenchmarkServer creates a test server for benchmarking.
func newBenchmarkServer() (*httptest.Server, func()) {
	dsManager := datasource.NewDataSourceManager(
		datasource.NewAdapterRegistry(),
		nil,
		retry.Config{MaxRetries: 0, RetryInterval: 100 * time.Millisecond},
		zap.NewNop(),
	)

	res := &resolver.Resolver{
		DSManager:     dsManager,
		GraphQLConfig: config.GraphQLConfig{MaxResultRows: 10000},
	}
	schema := generated.NewExecutableSchema(generated.Config{Resolvers: res})

	s := NewServer(
		config.ServerConfig{
			Port:            0,
			Mode:            "production",
			RequestTimeout:  5 * time.Second,
			AllowGetQueries: false,
			MaxBatchQueries: 10,
		},
		config.GraphQLConfig{
			IntrospectionEnabled: false,
			MaxQueryComplexity:   100,
			MaxQueryDepth:        10,
			MaxResultRows:        10000,
		},
		config.ShutdownConfig{MaxWaitTime: 5 * time.Second},
		dsManager,
		res,
		schema,
		zap.NewNop(),
	)

	router := s.SetupRoutes()
	ts := httptest.NewServer(router)
	return ts, ts.Close
}

// BenchmarkSingleDatasourceQuery measures single datasource simple query latency.
// Target: P95 â‰?200ms (excluding datasource time).
func BenchmarkSingleDatasourceQuery(b *testing.B) {
	ts, cleanup := newBenchmarkServer()
	defer cleanup()

	body := `{"query":"{ __typename }"}`

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			resp, err := http.Post(ts.URL+"/graphql", "application/json", strings.NewReader(body))
			if err != nil {
				b.Fatalf("POST /graphql failed: %v", err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				b.Fatalf("expected 200, got %d", resp.StatusCode)
			}
		}
	})
}

// BenchmarkMixedDatasourceQuery measures cross-datasource mixed query latency.
// Simulates the overhead of processing queries that would span multiple datasources.
// Uses two sequential GraphQL queries in a single benchmark iteration to model
// the mixed-source pattern. Target: P95 â‰?500ms (excluding datasource time).
func BenchmarkMixedDatasourceQuery(b *testing.B) {
	ts, cleanup := newBenchmarkServer()
	defer cleanup()

	// Two queries simulating a mixed-datasource request pattern.
	body1 := `{"query":"{ a1: __typename }"}`
	body2 := `{"query":"{ b1: __typename }"}`

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			resp1, err := http.Post(ts.URL+"/graphql", "application/json", strings.NewReader(body1))
			if err != nil {
				b.Fatalf("POST /graphql failed: %v", err)
			}
			resp1.Body.Close()

			resp2, err := http.Post(ts.URL+"/graphql", "application/json", strings.NewReader(body2))
			if err != nil {
				b.Fatalf("POST /graphql failed: %v", err)
			}
			resp2.Body.Close()
		}
	})
}

// BenchmarkConcurrentQueries measures concurrent query throughput by running
// multiple goroutines issuing queries simultaneously.
func BenchmarkConcurrentQueries(b *testing.B) {
	ts, cleanup := newBenchmarkServer()
	defer cleanup()

	body := `{"query":"{ __typename }"}`
	concurrency := []int{1, 10, 50}

	for _, c := range concurrency {
		b.Run(fmt.Sprintf("concurrency-%d", c), func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				var wg sync.WaitGroup
				wg.Add(c)
				for j := 0; j < c; j++ {
					go func() {
						defer wg.Done()
						resp, err := http.Post(ts.URL+"/graphql", "application/json", strings.NewReader(body))
						if err != nil {
							return
						}
						resp.Body.Close()
					}()
				}
				wg.Wait()
			}
		})
	}
}

// BenchmarkCacheHitVsMiss compares cache hit vs miss performance using the
// CacheLayer with an in-memory backend.
func BenchmarkCacheHitVsMiss(b *testing.B) {
	memCache, err := cache.NewMemoryCache(cache.MemoryCacheConfig{
		MaxEntries:    10000,
		MaxMemorySize: "64MB",
	})
	if err != nil {
		b.Fatalf("failed to create memory cache: %v", err)
	}

	layer := cache.NewCacheLayer(cache.CacheLayerConfig{
		Backend:    memCache,
		DefaultTTL: 60 * time.Second,
		JitterPct:  10,
		EmptyTTL:   30 * time.Second,
		Logger:     zap.NewNop(),
	})

	ctx := context.Background()
	testData := []byte(`{"data":[{"id":1,"value":"cached-result"}]}`)

	b.Run("cache-miss", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("cache:bench:%d", i)
			_, _ = layer.GetOrLoad(ctx, key, "bench", func() ([]byte, error) {
				return testData, nil
			})
		}
	})

	// Pre-populate cache for hit benchmark.
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("cache:benchhit:%d", i)
		_, _ = layer.GetOrLoad(ctx, key, "bench", func() ([]byte, error) {
			return testData, nil
		})
	}

	b.Run("cache-hit", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("cache:benchhit:%d", i%1000)
			_, _ = layer.GetOrLoad(ctx, key, "bench", func() ([]byte, error) {
				b.Fatal("loader should not be called on cache hit")
				return nil, nil
			})
		}
	})
}
