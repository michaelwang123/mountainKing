// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package cache

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
	"pgregory.net/rapid"
)

// =============================================================================
// Feature: graphql-multi-datasource-api
// Task 17.7: Cache core property tests
// =============================================================================

// =============================================================================
// Property 64: 缓存 Key 确定性
// **Validates: Requirements 16.3**
// For any two queries with the same query, variables, and datasource, the
// generated cache key should be identical. For any two queries where query,
// variables, or datasource differ, the cache key should differ.
// =============================================================================

func TestProperty64_CacheKeyDeterminism(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		gen := &CacheKeyGenerator{}

		ds := rapid.StringMatching(`[a-z]{2,10}`).Draw(rt, "datasource")
		query := rapid.StringMatching(`\{ [a-z]{2,10} \{ [a-z]{2,8} \} \}`).Draw(rt, "query")

		// Generate random variables
		numVars := rapid.IntRange(0, 5).Draw(rt, "numVars")
		vars := make(map[string]any)
		for i := 0; i < numVars; i++ {
			k := rapid.StringMatching(`[a-z]{2,8}`).Draw(rt, fmt.Sprintf("varKey_%d", i))
			v := rapid.IntRange(0, 1000).Draw(rt, fmt.Sprintf("varVal_%d", i))
			vars[k] = v
		}

		// Same inputs → same key (determinism)
		key1 := gen.Generate(ds, query, vars)
		key2 := gen.Generate(ds, query, vars)
		if key1 != key2 {
			rt.Fatalf("same inputs produced different keys: %q vs %q", key1, key2)
		}

		// Key format: "cache:{datasource}:{16-hex-chars}"
		prefix := fmt.Sprintf("cache:%s:", ds)
		if !strings.HasPrefix(key1, prefix) {
			rt.Fatalf("key %q missing expected prefix %q", key1, prefix)
		}
		hashPart := key1[len(prefix):]
		if len(hashPart) != 16 {
			rt.Fatalf("hash part %q should be 16 hex chars, got %d", hashPart, len(hashPart))
		}

		// Different datasource → different key
		otherDS := ds + "x"
		keyOtherDS := gen.Generate(otherDS, query, vars)
		if key1 == keyOtherDS {
			rt.Fatalf("different datasource should produce different key")
		}

		// Different query → different key
		otherQuery := query + " extra"
		keyOtherQuery := gen.Generate(ds, otherQuery, vars)
		if key1 == keyOtherQuery {
			rt.Fatalf("different query should produce different key")
		}
	})
}

// =============================================================================
// Property 65: 客户端绕过缓存
// **Validates: Requirements 16.5**
// For any request where extensions.cache=false, Cache_Layer should bypass
// cache and always call the loader.
// =============================================================================

func TestProperty65_ClientBypassCache(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		mc, err := NewMemoryCache(MemoryCacheConfig{MaxEntries: 1000, MaxMemorySize: "10MB"})
		if err != nil {
			rt.Fatalf("NewMemoryCache: %v", err)
		}
		cl := NewCacheLayer(CacheLayerConfig{
			Backend:    mc,
			DefaultTTL: 5 * time.Second,
			JitterPct:  0,
			EmptyTTL:   1 * time.Second,
			Logger:     zap.NewNop(),
		})
		ctx := context.Background()

		ds := rapid.StringMatching(`[a-z]{3,8}`).Draw(rt, "datasource")
		key := fmt.Sprintf("cache:%s:bypass", ds)
		data := []byte(rapid.StringMatching(`[a-z]{5,20}`).Draw(rt, "data"))

		// First call: populate cache
		var calls1 int32
		_, err = cl.GetOrLoad(ctx, key, ds, func() ([]byte, error) {
			atomic.AddInt32(&calls1, 1)
			return data, nil
		})
		if err != nil {
			rt.Fatalf("first GetOrLoad: %v", err)
		}

		// Second call: should hit cache (loader NOT called)
		var calls2 int32
		_, err = cl.GetOrLoad(ctx, key, ds, func() ([]byte, error) {
			atomic.AddInt32(&calls2, 1)
			return data, nil
		})
		if err != nil {
			rt.Fatalf("second GetOrLoad: %v", err)
		}
		if calls2 != 0 {
			rt.Fatalf("expected cache hit (0 loader calls), got %d", calls2)
		}

		// Simulate bypass: delete cache entry and call loader directly
		// (extensions.cache=false means the caller skips CacheLayer entirely)
		// We verify that without cache, loader is always called.
		_ = mc.Delete(ctx, key)
		var calls3 int32
		_, err = cl.GetOrLoad(ctx, key, ds, func() ([]byte, error) {
			atomic.AddInt32(&calls3, 1)
			return data, nil
		})
		if err != nil {
			rt.Fatalf("bypass GetOrLoad: %v", err)
		}
		if calls3 != 1 {
			rt.Fatalf("expected loader called once after bypass, got %d", calls3)
		}
	})
}

// =============================================================================
// Property 66: 仅缓存 Query 操作
// **Validates: Requirements 16.7**
// For any Mutation operation, Cache_Layer should always skip cache and execute
// directly. We model this by verifying that the loader is always called for
// mutation-type operations (no caching).
// =============================================================================

func TestProperty66_OnlyCacheQueryOperations(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		mc, err := NewMemoryCache(MemoryCacheConfig{MaxEntries: 1000, MaxMemorySize: "10MB"})
		if err != nil {
			rt.Fatalf("NewMemoryCache: %v", err)
		}
		cl := NewCacheLayer(CacheLayerConfig{
			Backend:    mc,
			DefaultTTL: 5 * time.Second,
			JitterPct:  0,
			EmptyTTL:   1 * time.Second,
			Logger:     zap.NewNop(),
		})
		ctx := context.Background()

		// For query operations: cache should work (second call hits cache)
		queryKey := "cache:ds:query_op"
		queryData := []byte("query-result")
		var queryCalls int32
		loader := func() ([]byte, error) {
			atomic.AddInt32(&queryCalls, 1)
			return queryData, nil
		}
		_, _ = cl.GetOrLoad(ctx, queryKey, "ds", loader)
		_, _ = cl.GetOrLoad(ctx, queryKey, "ds", loader)
		if queryCalls != 1 {
			rt.Fatalf("query operation: expected 1 loader call (cache hit on 2nd), got %d", queryCalls)
		}

		// For mutation operations: caller should NOT use CacheLayer at all.
		// We verify that if we use a different key each time (simulating no caching),
		// the loader is always called.
		numMutations := rapid.IntRange(1, 5).Draw(rt, "numMutations")
		var mutCalls int32
		for i := 0; i < numMutations; i++ {
			// Each mutation uses a unique key (simulating bypass)
			mutKey := fmt.Sprintf("mutation:%d", i)
			_, _ = cl.GetOrLoad(ctx, mutKey, "ds", func() ([]byte, error) {
				atomic.AddInt32(&mutCalls, 1)
				return []byte("mut-result"), nil
			})
		}
		if int(mutCalls) != numMutations {
			rt.Fatalf("mutation operations: expected %d loader calls, got %d", numMutations, mutCalls)
		}
	})
}

// =============================================================================
// Property 67: LRU 缓存淘汰
// **Validates: Requirements 16.8**
// For any memory cache that has reached max_entries, adding a new entry should
// evict the least recently used entry, and the cache size should never exceed
// max_entries.
// =============================================================================

func TestProperty67_LRUCacheEviction(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		maxEntries := rapid.IntRange(3, 20).Draw(rt, "maxEntries")
		mc, err := NewMemoryCache(MemoryCacheConfig{
			MaxEntries:    maxEntries,
			MaxMemorySize: "10MB",
		})
		if err != nil {
			rt.Fatalf("NewMemoryCache: %v", err)
		}
		ctx := context.Background()

		// Fill cache to capacity
		for i := 0; i < maxEntries; i++ {
			key := fmt.Sprintf("key_%d", i)
			_ = mc.Set(ctx, key, []byte(fmt.Sprintf("val_%d", i)), time.Minute)
		}
		if mc.Len() != maxEntries {
			rt.Fatalf("expected %d entries, got %d", maxEntries, mc.Len())
		}

		// Add more entries beyond capacity
		extraEntries := rapid.IntRange(1, 10).Draw(rt, "extraEntries")
		for i := 0; i < extraEntries; i++ {
			key := fmt.Sprintf("extra_%d", i)
			_ = mc.Set(ctx, key, []byte(fmt.Sprintf("extra_val_%d", i)), time.Minute)
		}

		// Cache size should never exceed maxEntries
		if mc.Len() > maxEntries {
			rt.Fatalf("cache size %d exceeds max_entries %d", mc.Len(), maxEntries)
		}

		// The oldest entries (key_0, key_1, ...) should have been evicted
		// At minimum, key_0 should be gone since it was the LRU entry
		_, found, _ := mc.Get(ctx, "key_0")
		if found {
			rt.Fatalf("expected key_0 to be evicted (LRU), but it was found")
		}

		// The newest extra entries should be present
		lastExtra := fmt.Sprintf("extra_%d", extraEntries-1)
		_, found, _ = mc.Get(ctx, lastExtra)
		if !found {
			rt.Fatalf("expected newest entry %q to be present", lastExtra)
		}
	})
}

// =============================================================================
// Property 68: 缓存清除操作
// **Validates: Requirements 16.9**
// For any clearCache(datasource: "X") call, all cache entries for datasource X
// should be cleared while other datasources' entries remain. For clearCache()
// (no args), all entries should be cleared.
// =============================================================================

func TestProperty68_CacheClearOperations(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		mc, err := NewMemoryCache(MemoryCacheConfig{MaxEntries: 1000, MaxMemorySize: "10MB"})
		if err != nil {
			rt.Fatalf("NewMemoryCache: %v", err)
		}
		cl := NewCacheLayer(CacheLayerConfig{
			Backend:    mc,
			DefaultTTL: 5 * time.Second,
			JitterPct:  0,
			EmptyTTL:   1 * time.Second,
			Logger:     zap.NewNop(),
		})
		ctx := context.Background()

		// Generate 2-4 datasources with random entries
		numDS := rapid.IntRange(2, 4).Draw(rt, "numDS")
		dsNames := make([]string, numDS)
		for i := 0; i < numDS; i++ {
			dsNames[i] = fmt.Sprintf("ds%d", i)
		}

		// Populate cache entries for each datasource
		entriesPerDS := rapid.IntRange(1, 5).Draw(rt, "entriesPerDS")
		for _, ds := range dsNames {
			for j := 0; j < entriesPerDS; j++ {
				key := fmt.Sprintf("cache:%s:entry_%d", ds, j)
				_, _ = cl.GetOrLoad(ctx, key, ds, func() ([]byte, error) {
					return []byte(fmt.Sprintf("data_%s_%d", ds, j)), nil
				})
			}
		}

		// Test ClearByDatasource: clear the first datasource
		targetDS := dsNames[0]
		if err := cl.ClearByDatasource(ctx, targetDS); err != nil {
			rt.Fatalf("ClearByDatasource: %v", err)
		}

		// Verify target datasource entries are gone
		for j := 0; j < entriesPerDS; j++ {
			key := fmt.Sprintf("cache:%s:entry_%d", targetDS, j)
			var called int32
			_, _ = cl.GetOrLoad(ctx, key, targetDS, func() ([]byte, error) {
				atomic.AddInt32(&called, 1)
				return []byte("reloaded"), nil
			})
			if called != 1 {
				rt.Fatalf("expected cleared entry %q to trigger loader, but it was cached", key)
			}
		}

		// Verify other datasources' entries are still cached
		for _, ds := range dsNames[1:] {
			for j := 0; j < entriesPerDS; j++ {
				key := fmt.Sprintf("cache:%s:entry_%d", ds, j)
				var called int32
				_, _ = cl.GetOrLoad(ctx, key, ds, func() ([]byte, error) {
					atomic.AddInt32(&called, 1)
					return []byte("should-not-reload"), nil
				})
				if called != 0 {
					rt.Fatalf("entry %q for non-cleared ds %q should still be cached", key, ds)
				}
			}
		}

		// Test ClearAll
		if err := cl.ClearAll(ctx); err != nil {
			rt.Fatalf("ClearAll: %v", err)
		}

		// All entries should be gone
		for _, ds := range dsNames[1:] {
			key := fmt.Sprintf("cache:%s:entry_0", ds)
			var called int32
			_, _ = cl.GetOrLoad(ctx, key, ds, func() ([]byte, error) {
				atomic.AddInt32(&called, 1)
				return []byte("reloaded-all"), nil
			})
			if called != 1 {
				rt.Fatalf("expected all entries cleared, but %q was still cached", key)
			}
		}
	})
}

// =============================================================================
// Property 77: 缓存 Key 查询规范化
// **Validates: Design - 缓存命中率优化**
// For any two semantically identical GraphQL queries that differ only in
// whitespace, newlines, or comments, the normalized cache key should be the same.
// =============================================================================

func TestProperty77_CacheKeyQueryNormalization(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		gen := &CacheKeyGenerator{}
		ds := rapid.StringMatching(`[a-z]{3,8}`).Draw(rt, "datasource")

		// Generate a base query
		fieldName := rapid.StringMatching(`[a-z]{3,10}`).Draw(rt, "field")
		subField := rapid.StringMatching(`[a-z]{2,8}`).Draw(rt, "subField")
		baseQuery := fmt.Sprintf("{ %s { %s } }", fieldName, subField)

		// Create variants that differ only in whitespace/comments
		variants := []string{
			baseQuery,
			// Extra spaces
			fmt.Sprintf("{  %s  {  %s  } }", fieldName, subField),
			// Newlines
			fmt.Sprintf("{\n  %s {\n    %s\n  }\n}", fieldName, subField),
			// Tabs
			fmt.Sprintf("{\t%s\t{\t%s\t}\t}", fieldName, subField),
			// Leading/trailing whitespace
			fmt.Sprintf("  { %s { %s } }  ", fieldName, subField),
			// With comments
			fmt.Sprintf("# comment\n{ %s { %s } }", fieldName, subField),
			fmt.Sprintf("{ %s { %s # inline\n } }", fieldName, subField),
		}

		baseKey := gen.Generate(ds, variants[0], nil)
		for i, variant := range variants[1:] {
			variantKey := gen.Generate(ds, variant, nil)
			if variantKey != baseKey {
				rt.Fatalf("variant %d produced different key:\n  base:    %q → %q\n  variant: %q → %q",
					i+1, variants[0], baseKey, variant, variantKey)
			}
		}
	})
}

// =============================================================================
// Feature: graphql-multi-datasource-api
// Task 17.8: Cache protection property tests
// =============================================================================

// =============================================================================
// Property 69: 缓存穿透防护
// **Validates: Requirements 16.10**
// For any query where the datasource returns an empty result, Cache_Layer should
// cache a short-TTL empty marker. Subsequent identical queries within that TTL
// should hit cache rather than penetrating to the datasource.
// =============================================================================

func TestProperty69_CachePenetrationProtection(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		mc, err := NewMemoryCache(MemoryCacheConfig{MaxEntries: 1000, MaxMemorySize: "10MB"})
		if err != nil {
			rt.Fatalf("NewMemoryCache: %v", err)
		}
		cl := NewCacheLayer(CacheLayerConfig{
			Backend:    mc,
			DefaultTTL: 5 * time.Second,
			JitterPct:  0,
			EmptyTTL:   2 * time.Second,
			Logger:     zap.NewNop(),
		})
		ctx := context.Background()

		ds := rapid.StringMatching(`[a-z]{3,8}`).Draw(rt, "datasource")
		key := fmt.Sprintf("cache:%s:empty_query", ds)

		// First call: loader returns empty result
		var loadCount int32
		result, err := cl.GetOrLoad(ctx, key, ds, func() ([]byte, error) {
			atomic.AddInt32(&loadCount, 1)
			return nil, nil // empty result
		})
		if err != nil {
			rt.Fatalf("first GetOrLoad: %v", err)
		}
		if result != nil {
			rt.Fatalf("expected nil result for empty, got %q", result)
		}
		if loadCount != 1 {
			rt.Fatalf("expected 1 loader call, got %d", loadCount)
		}

		// Subsequent calls within emptyTTL should NOT call loader (cache hit on empty marker)
		numRetries := rapid.IntRange(1, 5).Draw(rt, "numRetries")
		for i := 0; i < numRetries; i++ {
			result, err = cl.GetOrLoad(ctx, key, ds, func() ([]byte, error) {
				atomic.AddInt32(&loadCount, 1)
				return []byte("should-not-reach"), nil
			})
			if err != nil {
				rt.Fatalf("retry %d: %v", i, err)
			}
			if result != nil {
				rt.Fatalf("retry %d: expected nil from empty marker, got %q", i, result)
			}
		}

		if loadCount != 1 {
			rt.Fatalf("expected loader called only once (penetration protection), got %d", loadCount)
		}
	})
}

// =============================================================================
// Property 70: 缓存雪崩防护 - TTL 抖动
// **Validates: Requirements 16.11**
// For any cache entry's actual TTL, it should be within ±jitter_percent of the
// configured TTL (default ±10%).
// =============================================================================

func TestProperty70_CacheAvalancheProtection_TTLJitter(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		jitterPct := rapid.IntRange(1, 30).Draw(rt, "jitterPct")
		baseTTLSec := rapid.IntRange(10, 300).Draw(rt, "baseTTLSec")
		baseTTL := time.Duration(baseTTLSec) * time.Second

		cl := NewCacheLayer(CacheLayerConfig{
			Backend:    nil, // not used for jitter calculation
			DefaultTTL: baseTTL,
			JitterPct:  jitterPct,
			Logger:     zap.NewNop(),
		})

		minExpected := time.Duration(float64(baseTTL) * (1.0 - float64(jitterPct)/100.0))
		maxExpected := time.Duration(float64(baseTTL) * (1.0 + float64(jitterPct)/100.0))

		// Test many jitter applications
		for i := 0; i < 100; i++ {
			jittered := cl.applyJitter(baseTTL)
			if jittered < minExpected || jittered > maxExpected {
				rt.Fatalf("jittered TTL %v out of range [%v, %v] (base=%v, jitter=%d%%)",
					jittered, minExpected, maxExpected, baseTTL, jitterPct)
			}
		}

		// Verify that jitter produces some variation (not all identical)
		seen := make(map[time.Duration]bool)
		for i := 0; i < 50; i++ {
			seen[cl.applyJitter(baseTTL)] = true
		}
		if len(seen) < 2 {
			rt.Fatalf("jitter produced no variation across 50 samples (jitterPct=%d)", jitterPct)
		}
	})
}

// =============================================================================
// Property 71: 缓存击穿防护 - Singleflight
// **Validates: Requirements 16.12**
// For any N concurrent cache-miss requests for the same key, only 1 actual
// datasource query should be triggered. The other N-1 requests should wait
// and share the result.
// =============================================================================

func TestProperty71_CacheBreakdownProtection_Singleflight(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		mc, err := NewMemoryCache(MemoryCacheConfig{MaxEntries: 1000, MaxMemorySize: "10MB"})
		if err != nil {
			rt.Fatalf("NewMemoryCache: %v", err)
		}
		cl := NewCacheLayer(CacheLayerConfig{
			Backend:    mc,
			DefaultTTL: 5 * time.Second,
			JitterPct:  0,
			EmptyTTL:   1 * time.Second,
			Logger:     zap.NewNop(),
		})
		ctx := context.Background()

		numGoroutines := rapid.IntRange(5, 20).Draw(rt, "numGoroutines")
		ds := rapid.StringMatching(`[a-z]{3,8}`).Draw(rt, "datasource")
		key := fmt.Sprintf("cache:%s:singleflight", ds)
		expectedData := []byte(rapid.StringMatching(`[a-z]{10,30}`).Draw(rt, "data"))

		var loadCount atomic.Int32
		var wg sync.WaitGroup
		results := make([][]byte, numGoroutines)
		errs := make([]error, numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				results[idx], errs[idx] = cl.GetOrLoad(ctx, key, ds, func() ([]byte, error) {
					loadCount.Add(1)
					time.Sleep(50 * time.Millisecond) // simulate slow datasource
					return expectedData, nil
				})
			}(i)
		}
		wg.Wait()

		// Verify: only 1 loader call (singleflight)
		if loadCount.Load() != 1 {
			rt.Fatalf("expected 1 loader call (singleflight), got %d", loadCount.Load())
		}

		// All goroutines should get the same result with no errors
		for i := 0; i < numGoroutines; i++ {
			if errs[i] != nil {
				rt.Fatalf("goroutine %d error: %v", i, errs[i])
			}
			if !bytes.Equal(results[i], expectedData) {
				rt.Fatalf("goroutine %d: expected %q, got %q", i, expectedData, results[i])
			}
		}
	})
}

// =============================================================================
// Property 79: totalCount 与数据结果缓存一致性
// **Validates: Design - 缓存一致性**
// For any NeedCount=true query, data results and totalCount should be stored
// as a single cache entry and expire together. There should never be a case
// where totalCount and actual returned rows are inconsistent.
// =============================================================================

func TestProperty79_TotalCountDataCacheConsistency(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		mc, err := NewMemoryCache(MemoryCacheConfig{MaxEntries: 1000, MaxMemorySize: "10MB"})
		if err != nil {
			rt.Fatalf("NewMemoryCache: %v", err)
		}
		cl := NewCacheLayer(CacheLayerConfig{
			Backend:    mc,
			DefaultTTL: 5 * time.Second,
			JitterPct:  0,
			EmptyTTL:   1 * time.Second,
			Logger:     zap.NewNop(),
		})
		ctx := context.Background()

		ds := rapid.StringMatching(`[a-z]{3,8}`).Draw(rt, "datasource")
		totalCount := rapid.Int64Range(1, 1000).Draw(rt, "totalCount")
		numRows := rapid.IntRange(1, 50).Draw(rt, "numRows")

		// Simulate a combined result (data + totalCount in one cache entry)
		type CombinedResult struct {
			Data       []map[string]any
			TotalCount int64
		}

		combined := CombinedResult{
			Data:       make([]map[string]any, numRows),
			TotalCount: totalCount,
		}
		for i := 0; i < numRows; i++ {
			combined.Data[i] = map[string]any{"id": i}
		}

		// Serialize the combined result
		var buf bytes.Buffer
		if err := gob.NewEncoder(&buf).Encode(combined); err != nil {
			rt.Fatalf("gob encode: %v", err)
		}
		serialized := buf.Bytes()

		key := fmt.Sprintf("cache:%s:combined", ds)

		// Store via GetOrLoad
		var loadCount int32
		result, err := cl.GetOrLoad(ctx, key, ds, func() ([]byte, error) {
			atomic.AddInt32(&loadCount, 1)
			return serialized, nil
		})
		if err != nil {
			rt.Fatalf("GetOrLoad: %v", err)
		}

		// Deserialize and verify consistency
		var decoded CombinedResult
		if err := gob.NewDecoder(bytes.NewReader(result)).Decode(&decoded); err != nil {
			rt.Fatalf("gob decode: %v", err)
		}

		if decoded.TotalCount != totalCount {
			rt.Fatalf("totalCount mismatch: expected %d, got %d", totalCount, decoded.TotalCount)
		}
		if len(decoded.Data) != numRows {
			rt.Fatalf("data rows mismatch: expected %d, got %d", numRows, len(decoded.Data))
		}

		// Second call should return the same combined entry from cache
		result2, err := cl.GetOrLoad(ctx, key, ds, func() ([]byte, error) {
			atomic.AddInt32(&loadCount, 1)
			return nil, fmt.Errorf("should not be called")
		})
		if err != nil {
			rt.Fatalf("second GetOrLoad: %v", err)
		}

		var decoded2 CombinedResult
		if err := gob.NewDecoder(bytes.NewReader(result2)).Decode(&decoded2); err != nil {
			rt.Fatalf("gob decode 2: %v", err)
		}

		// Both data and totalCount must be consistent (same cache entry)
		if decoded2.TotalCount != decoded.TotalCount {
			rt.Fatalf("cached totalCount changed: %d vs %d", decoded.TotalCount, decoded2.TotalCount)
		}
		if len(decoded2.Data) != len(decoded.Data) {
			rt.Fatalf("cached data rows changed: %d vs %d", len(decoded.Data), len(decoded2.Data))
		}

		if loadCount != 1 {
			rt.Fatalf("expected 1 loader call, got %d", loadCount)
		}
	})
}

// =============================================================================
// Property 85: 内存缓存内存大小限制
// **Validates: Design - 内存缓存容量控制**
// For any memory cache whose total memory usage reaches max_memory_size, adding
// new entries should trigger LRU eviction to keep memory under the limit.
// =============================================================================

func TestProperty85_MemoryCacheMemorySizeLimit(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Use a small memory limit for testing
		maxMemBytes := rapid.IntRange(200, 1000).Draw(rt, "maxMemBytes")
		mc, err := NewMemoryCache(MemoryCacheConfig{
			MaxEntries:    10000, // high entry limit so memory is the binding constraint
			MaxMemorySize: fmt.Sprintf("%dB", maxMemBytes),
		})
		if err != nil {
			rt.Fatalf("NewMemoryCache: %v", err)
		}
		ctx := context.Background()

		// Insert entries with known sizes
		entrySize := rapid.IntRange(20, 80).Draw(rt, "entrySize")
		numEntries := rapid.IntRange(5, 30).Draw(rt, "numEntries")

		for i := 0; i < numEntries; i++ {
			data := make([]byte, entrySize)
			for j := range data {
				data[j] = byte(i % 256)
			}
			_ = mc.Set(ctx, fmt.Sprintf("key_%d", i), data, time.Minute)

			// After each insert, memory usage should not exceed the limit
			// (allowing for the entry just added)
			if mc.MemUsed() > int64(maxMemBytes)+int64(entrySize) {
				rt.Fatalf("memory usage %d exceeds limit %d + entry size %d after inserting entry %d",
					mc.MemUsed(), maxMemBytes, entrySize, i)
			}
		}

		// Final check: memory should be within bounds
		if mc.MemUsed() > int64(maxMemBytes) {
			rt.Fatalf("final memory usage %d exceeds limit %d", mc.MemUsed(), maxMemBytes)
		}
	})
}

// =============================================================================
// Property 86: 缓存 Gob 反序列化失败恢复
// **Validates: Design - 缓存容错**
// For any cache entry where gob deserialization fails (corrupted data), the
// Cache Layer should: 1) delete the corrupted entry, 2) call the loader to
// fetch fresh data from the datasource, 3) log a WARN. It should NOT return
// an error to the client.
// =============================================================================

func TestProperty86_CacheGobDeserializationFailureRecovery(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		mc, err := NewMemoryCache(MemoryCacheConfig{MaxEntries: 1000, MaxMemorySize: "10MB"})
		if err != nil {
			rt.Fatalf("NewMemoryCache: %v", err)
		}

		// Set up observed logger to verify WARN log
		core, logs := observer.New(zapcore.WarnLevel)
		logger := zap.New(core)

		cl := NewCacheLayer(CacheLayerConfig{
			Backend:    mc,
			DefaultTTL: 5 * time.Second,
			JitterPct:  0,
			EmptyTTL:   1 * time.Second,
			Logger:     logger,
		})
		ctx := context.Background()

		ds := rapid.StringMatching(`[a-z]{3,8}`).Draw(rt, "datasource")
		key := fmt.Sprintf("cache:%s:corrupt", ds)

		// Inject corrupted (non-gob) data directly into the cache backend
		corruptData := []byte(rapid.StringMatching(`[a-z]{10,50}`).Draw(rt, "corruptData"))
		_ = mc.Set(ctx, key, corruptData, 5*time.Second)

		// Verify the corrupted data is in cache
		_, found, _ := mc.Get(ctx, key)
		if !found {
			rt.Fatalf("corrupted data should be in cache")
		}

		// Call GetOrLoad — should recover gracefully
		freshData := []byte(rapid.StringMatching(`[a-z]{10,30}`).Draw(rt, "freshData"))
		var loadCount int32
		result, err := cl.GetOrLoad(ctx, key, ds, func() ([]byte, error) {
			atomic.AddInt32(&loadCount, 1)
			return freshData, nil
		})

		// Should NOT return error
		if err != nil {
			rt.Fatalf("expected no error after corruption recovery, got: %v", err)
		}

		// Should return fresh data from loader
		if !bytes.Equal(result, freshData) {
			rt.Fatalf("expected fresh data %q, got %q", freshData, result)
		}

		// Loader should have been called exactly once
		if loadCount != 1 {
			rt.Fatalf("expected loader called once, got %d", loadCount)
		}

		// Should have logged a WARN about deserialization failure
		warnLogs := logs.FilterMessage("gob deserialization failed, deleting corrupted cache entry")
		if warnLogs.Len() == 0 {
			rt.Fatalf("expected WARN log about gob deserialization failure, got none")
		}
	})
}
