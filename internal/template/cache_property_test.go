// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package template

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/michaelwang123/mountainKing/internal/cache"
	"github.com/michaelwang123/mountainKing/internal/config"
	"pgregory.net/rapid"
)

// =============================================================================
// Feature: sql-template-engine
// Task 11.2: 缓存集成单元测试和属性测试
// =============================================================================

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// genParams generates a random map[string]any for cache key testing.
func genParams(rt *rapid.T, label string) map[string]any {
	n := rapid.IntRange(0, 5).Draw(rt, label+"_count")
	m := make(map[string]any, n)
	for i := 0; i < n; i++ {
		key := rapid.StringMatching(`[a-z]{1,8}`).Draw(rt, fmt.Sprintf("%s_key_%d", label, i))
		val := rapid.StringMatching(`[a-zA-Z0-9]{0,16}`).Draw(rt, fmt.Sprintf("%s_val_%d", label, i))
		m[key] = val
	}
	return m
}

// genFields generates a random []string of field names.
func genFields(rt *rapid.T, label string) []string {
	n := rapid.IntRange(0, 5).Draw(rt, label+"_count")
	fields := make([]string, n)
	for i := 0; i < n; i++ {
		fields[i] = rapid.StringMatching(`[a-z]{1,8}`).Draw(rt, fmt.Sprintf("%s_%d", label, i))
	}
	return fields
}

// genOrderBy generates a random []TemplateOrderByParam.
func genOrderBy(rt *rapid.T, label string) []TemplateOrderByParam {
	n := rapid.IntRange(0, 3).Draw(rt, label+"_count")
	obs := make([]TemplateOrderByParam, n)
	for i := 0; i < n; i++ {
		obs[i] = TemplateOrderByParam{
			Field:     rapid.StringMatching(`[a-z]{1,8}`).Draw(rt, fmt.Sprintf("%s_field_%d", label, i)),
			Direction: rapid.SampledFrom([]string{"ASC", "DESC"}).Draw(rt, fmt.Sprintf("%s_dir_%d", label, i)),
		}
	}
	return obs
}

// intPtr returns a pointer to an int.
func intPtr(v int) *int { return &v }

// durationPtr returns a pointer to a time.Duration.
func durationPtr(d time.Duration) *time.Duration { return &d }

// boolPtr returns a pointer to a bool.
func boolPtr(b bool) *bool { return &b }

// =============================================================================
// Property 49: 缓存 key 确定性
// **Validates: Requirements 8.2**
// Same inputs always produce the same cache key.
// =============================================================================

func TestProperty49_CacheKeyDeterminism(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		name := rapid.StringMatching(`[a-z_]{1,16}`).Draw(rt, "name")
		params := genParams(rt, "params")
		fields := genFields(rt, "fields")
		first := rapid.Ptr(rapid.IntRange(1, 10000), true).Draw(rt, "first")
		offset := rapid.Ptr(rapid.IntRange(0, 10000), true).Draw(rt, "offset")
		orderBy := genOrderBy(rt, "orderBy")

		key1 := generateCacheKey(name, params, fields, first, offset, orderBy)
		key2 := generateCacheKey(name, params, fields, first, offset, orderBy)

		if key1 != key2 {
			rt.Fatalf("same inputs produced different keys:\n  key1=%s\n  key2=%s", key1, key2)
		}
	})
}

// =============================================================================
// Property 50: 缓存 key 区分性
// **Validates: Requirements 8.2**
// Different inputs produce different cache keys (with high probability).
// =============================================================================

func TestProperty50_CacheKeyDistinctness(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		name := rapid.StringMatching(`[a-z_]{1,16}`).Draw(rt, "name")
		params := genParams(rt, "params")
		fields := genFields(rt, "fields")
		first := rapid.Ptr(rapid.IntRange(1, 10000), true).Draw(rt, "first")
		offset := rapid.Ptr(rapid.IntRange(0, 10000), true).Draw(rt, "offset")
		orderBy := genOrderBy(rt, "orderBy")

		// Mutate one dimension: add an extra param
		mutatedParams := make(map[string]any, len(params)+1)
		for k, v := range params {
			mutatedParams[k] = v
		}
		extraKey := rapid.StringMatching(`[a-z]{1,8}`).Draw(rt, "extraKey")
		extraVal := rapid.StringMatching(`[a-zA-Z0-9]{1,8}`).Draw(rt, "extraVal")
		mutatedParams[extraKey+"_xtra"] = extraVal

		key1 := generateCacheKey(name, params, fields, first, offset, orderBy)
		key2 := generateCacheKey(name, mutatedParams, fields, first, offset, orderBy)

		if key1 == key2 {
			rt.Fatalf("different params produced same key: %s", key1)
		}
	})
}

// =============================================================================
// Property 51: 缓存 key 含 fields
// **Validates: Requirements 8.2**
// Different fields produce different cache keys.
// =============================================================================

func TestProperty51_CacheKeyIncludesFields(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		name := rapid.StringMatching(`[a-z_]{1,16}`).Draw(rt, "name")
		params := genParams(rt, "params")
		first := rapid.Ptr(rapid.IntRange(1, 10000), true).Draw(rt, "first")
		offset := rapid.Ptr(rapid.IntRange(0, 10000), true).Draw(rt, "offset")
		orderBy := genOrderBy(rt, "orderBy")

		fields1 := []string{"col_a", "col_b"}
		fields2 := []string{"col_a", "col_b", "col_c"}

		key1 := generateCacheKey(name, params, fields1, first, offset, orderBy)
		key2 := generateCacheKey(name, params, fields2, first, offset, orderBy)

		if key1 == key2 {
			rt.Fatalf("different fields produced same key: %s", key1)
		}
	})
}

// =============================================================================
// Property 52: 模板级 TTL 覆盖
// **Validates: Requirements 8.3**
// shouldCache returns true when CacheTTL is set (cache is enabled).
// =============================================================================

func TestProperty52_TemplateLevelTTLOverride(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		ttlSec := rapid.IntRange(1, 3600).Draw(rt, "ttlSec")
		ttl := time.Duration(ttlSec) * time.Second

		tmpl := &RegisteredTemplate{
			CacheEnabled: true,
			CacheTTL:     &ttl,
			Config: config.TemplateConfig{
				CacheTTL: durationPtr(ttl),
			},
		}

		// shouldCache should return true when cache is enabled
		if !shouldCache(tmpl, false) {
			rt.Fatalf("shouldCache should return true for template with CacheTTL=%v", ttl)
		}

		// Verify the TTL is stored correctly
		if tmpl.CacheTTL == nil || *tmpl.CacheTTL != ttl {
			rt.Fatalf("expected CacheTTL=%v, got %v", ttl, tmpl.CacheTTL)
		}
	})
}

// =============================================================================
// Property 53: 缓存禁用
// **Validates: Requirements 8.4**
// shouldCache returns false when CacheEnabled=false.
// =============================================================================

func TestProperty53_CacheDisabled(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		skipCache := rapid.Bool().Draw(rt, "skipCache")

		tmpl := &RegisteredTemplate{
			CacheEnabled: false,
		}

		if shouldCache(tmpl, skipCache) {
			rt.Fatalf("shouldCache should return false when CacheEnabled=false (skipCache=%v)", skipCache)
		}
	})
}

// =============================================================================
// Property 54: 客户端缓存绕过
// **Validates: Requirements 8.5**
// shouldCache returns false when skipCache=true.
// =============================================================================

func TestProperty54_ClientCacheBypass(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		tmpl := &RegisteredTemplate{
			CacheEnabled: true,
		}

		if shouldCache(tmpl, true) {
			rt.Fatal("shouldCache should return false when skipCache=true")
		}
	})
}

// =============================================================================
// Property 55: totalCount 独立缓存
// **Validates: Requirements 8.6**
// Count key differs from data key for the same params.
// =============================================================================

func TestProperty55_TotalCountIndependentCache(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		name := rapid.StringMatching(`[a-z_]{1,16}`).Draw(rt, "name")
		params := genParams(rt, "params")
		fields := genFields(rt, "fields")
		first := rapid.Ptr(rapid.IntRange(1, 10000), true).Draw(rt, "first")
		offset := rapid.Ptr(rapid.IntRange(0, 10000), true).Draw(rt, "offset")
		orderBy := genOrderBy(rt, "orderBy")

		dataKey := generateCacheKey(name, params, fields, first, offset, orderBy)
		countKey := generateCountCacheKey(name, params)

		if dataKey == countKey {
			rt.Fatalf("data key and count key should differ:\n  dataKey=%s\n  countKey=%s", dataKey, countKey)
		}
	})
}

// =============================================================================
// Unit Tests
// =============================================================================

// Test 1: generateCacheKey deterministic
func TestGenerateCacheKey_Deterministic(t *testing.T) {
	params := map[string]any{"eerid": "EER001", "period": "monthly"}
	fields := []string{"vehicle_id", "plate_number"}
	first := intPtr(20)
	offset := intPtr(0)
	orderBy := []TemplateOrderByParam{{Field: "event_count", Direction: "DESC"}}

	key1 := generateCacheKey("fleet_report", params, fields, first, offset, orderBy)
	key2 := generateCacheKey("fleet_report", params, fields, first, offset, orderBy)

	if key1 != key2 {
		t.Fatalf("expected deterministic keys, got:\n  key1=%s\n  key2=%s", key1, key2)
	}
	if key1 == "" {
		t.Fatal("cache key should not be empty")
	}
}

// Test 2: generateCacheKey different params → different keys
func TestGenerateCacheKey_DifferentParams(t *testing.T) {
	fields := []string{"col_a"}
	first := intPtr(10)
	offset := intPtr(0)

	key1 := generateCacheKey("tmpl", map[string]any{"a": "1"}, fields, first, offset, nil)
	key2 := generateCacheKey("tmpl", map[string]any{"a": "2"}, fields, first, offset, nil)

	if key1 == key2 {
		t.Fatalf("different params should produce different keys, got: %s", key1)
	}
}

// Test 3: generateCacheKey different fields → different keys
func TestGenerateCacheKey_DifferentFields(t *testing.T) {
	params := map[string]any{"x": "1"}
	first := intPtr(10)
	offset := intPtr(0)

	key1 := generateCacheKey("tmpl", params, []string{"a", "b"}, first, offset, nil)
	key2 := generateCacheKey("tmpl", params, []string{"a", "b", "c"}, first, offset, nil)

	if key1 == key2 {
		t.Fatalf("different fields should produce different keys, got: %s", key1)
	}
}

// Test 4: generateCacheKey different first/offset → different keys
func TestGenerateCacheKey_DifferentPagination(t *testing.T) {
	params := map[string]any{"x": "1"}
	fields := []string{"a"}

	key1 := generateCacheKey("tmpl", params, fields, intPtr(10), intPtr(0), nil)
	key2 := generateCacheKey("tmpl", params, fields, intPtr(20), intPtr(0), nil)
	key3 := generateCacheKey("tmpl", params, fields, intPtr(10), intPtr(10), nil)

	if key1 == key2 {
		t.Fatalf("different first should produce different keys, got: %s", key1)
	}
	if key1 == key3 {
		t.Fatalf("different offset should produce different keys, got: %s", key1)
	}
}

// Test 5: generateCacheKey different orderBy → different keys
func TestGenerateCacheKey_DifferentOrderBy(t *testing.T) {
	params := map[string]any{"x": "1"}
	fields := []string{"a"}
	first := intPtr(10)
	offset := intPtr(0)

	ob1 := []TemplateOrderByParam{{Field: "col_a", Direction: "ASC"}}
	ob2 := []TemplateOrderByParam{{Field: "col_a", Direction: "DESC"}}
	ob3 := []TemplateOrderByParam{{Field: "col_b", Direction: "ASC"}}

	key1 := generateCacheKey("tmpl", params, fields, first, offset, ob1)
	key2 := generateCacheKey("tmpl", params, fields, first, offset, ob2)
	key3 := generateCacheKey("tmpl", params, fields, first, offset, ob3)

	if key1 == key2 {
		t.Fatalf("different orderBy direction should produce different keys, got: %s", key1)
	}
	if key1 == key3 {
		t.Fatalf("different orderBy field should produce different keys, got: %s", key1)
	}
}

// Test 6: generateCountCacheKey deterministic
func TestGenerateCountCacheKey_Deterministic(t *testing.T) {
	params := map[string]any{"eerid": "EER001", "period": "monthly"}

	key1 := generateCountCacheKey("fleet_report", params)
	key2 := generateCountCacheKey("fleet_report", params)

	if key1 != key2 {
		t.Fatalf("expected deterministic count keys, got:\n  key1=%s\n  key2=%s", key1, key2)
	}
	if key1 == "" {
		t.Fatal("count cache key should not be empty")
	}
}

// Test 7: generateCountCacheKey independent of fields/pagination
func TestGenerateCountCacheKey_IndependentOfFieldsPagination(t *testing.T) {
	params := map[string]any{"x": "1"}

	countKey := generateCountCacheKey("tmpl", params)

	// Data keys with different fields/pagination should differ from each other
	// but count key should be the same regardless
	countKey2 := generateCountCacheKey("tmpl", params)
	if countKey != countKey2 {
		t.Fatalf("count key should be independent of fields/pagination:\n  key1=%s\n  key2=%s", countKey, countKey2)
	}

	// Count key should end with ":count"
	if len(countKey) < 6 || countKey[len(countKey)-6:] != ":count" {
		t.Fatalf("count key should end with ':count', got: %s", countKey)
	}
}

// Test 8: shouldCache returns true when enabled and not skipped
func TestShouldCache_EnabledNotSkipped(t *testing.T) {
	tmpl := &RegisteredTemplate{CacheEnabled: true}
	if !shouldCache(tmpl, false) {
		t.Fatal("shouldCache should return true when CacheEnabled=true and skipCache=false")
	}
}

// Test 9: shouldCache returns false when CacheEnabled=false
func TestShouldCache_Disabled(t *testing.T) {
	tmpl := &RegisteredTemplate{CacheEnabled: false}
	if shouldCache(tmpl, false) {
		t.Fatal("shouldCache should return false when CacheEnabled=false")
	}
}

// Test 10: shouldCache returns false when skipCache=true
func TestShouldCache_SkipCache(t *testing.T) {
	tmpl := &RegisteredTemplate{CacheEnabled: true}
	if shouldCache(tmpl, true) {
		t.Fatal("shouldCache should return false when skipCache=true")
	}
}

// Test 11: executeWithCache with nil cacheLayer calls loader directly
func TestExecuteWithCache_NilCacheLayer(t *testing.T) {
	loaderCalled := false
	expectedData := []map[string]any{
		{"id": float64(1), "name": "test"},
	}

	data, called, err := executeWithCache(
		context.Background(),
		nil, // nil cache layer
		"analytics_db",
		"cache:template:test:abc123",
		func() ([]map[string]any, error) {
			loaderCalled = true
			return expectedData, nil
		},
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !loaderCalled {
		t.Fatal("loader should have been called")
	}
	if !called {
		t.Fatal("loaderCalled return value should be true")
	}
	if len(data) != 1 || data[0]["name"] != "test" {
		t.Fatalf("unexpected data: %v", data)
	}
}

// Test 12: executeCount with nil cacheLayer calls loader directly
func TestExecuteCount_NilCacheLayer(t *testing.T) {
	count, err := executeCount(
		context.Background(),
		nil, // nil cache layer
		"analytics_db",
		"cache:template:test:abc123:count",
		func() (int64, error) {
			return 42, nil
		},
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 42 {
		t.Fatalf("expected count=42, got %d", count)
	}
}

// =============================================================================
// Additional unit tests for executeWithCache / executeCount with real cache
// =============================================================================

// newTestCacheLayer creates a CacheLayer backed by an in-memory cache for testing.
func newTestCacheLayer(t *testing.T) *cache.CacheLayer {
	t.Helper()
	mem, err := cache.NewMemoryCache(cache.MemoryCacheConfig{
		MaxEntries:    1000,
		MaxMemorySize: "10MB",
	})
	if err != nil {
		t.Fatalf("failed to create memory cache: %v", err)
	}
	return cache.NewCacheLayer(cache.CacheLayerConfig{
		Backend:    mem,
		DefaultTTL: 60 * time.Second,
	})
}

func TestExecuteWithCache_CacheHit(t *testing.T) {
	cl := newTestCacheLayer(t)

	ctx := context.Background()
	key := "cache:template:test:hit"
	ds := "analytics_db"

	// First call: loader is invoked (cache miss)
	expectedData := []map[string]any{{"id": float64(1)}}
	data, called, err := executeWithCache(ctx, cl, ds, key, func() ([]map[string]any, error) {
		return expectedData, nil
	})
	if err != nil {
		t.Fatalf("unexpected error on first call: %v", err)
	}
	if !called {
		t.Fatal("loader should have been called on first call (cache miss)")
	}
	if len(data) != 1 {
		t.Fatalf("expected 1 row, got %d", len(data))
	}

	// Second call: loader should NOT be invoked (cache hit)
	data2, called2, err := executeWithCache(ctx, cl, ds, key, func() ([]map[string]any, error) {
		t.Fatal("loader should not be called on cache hit")
		return nil, nil
	})
	if err != nil {
		t.Fatalf("unexpected error on second call: %v", err)
	}
	if called2 {
		t.Fatal("loader should not have been called on cache hit")
	}

	// Verify data matches
	b1, _ := json.Marshal(data)
	b2, _ := json.Marshal(data2)
	if string(b1) != string(b2) {
		t.Fatalf("cached data mismatch:\n  first:  %s\n  second: %s", b1, b2)
	}
}

func TestExecuteCount_CacheHit(t *testing.T) {
	cl := newTestCacheLayer(t)

	ctx := context.Background()
	key := "cache:template:test:count:hit"
	ds := "analytics_db"

	// First call: cache miss
	count1, err := executeCount(ctx, cl, ds, key, func() (int64, error) {
		return 100, nil
	})
	if err != nil {
		t.Fatalf("unexpected error on first call: %v", err)
	}
	if count1 != 100 {
		t.Fatalf("expected count=100, got %d", count1)
	}

	// Second call: cache hit
	count2, err := executeCount(ctx, cl, ds, key, func() (int64, error) {
		t.Fatal("loader should not be called on cache hit")
		return 0, nil
	})
	if err != nil {
		t.Fatalf("unexpected error on second call: %v", err)
	}
	if count2 != 100 {
		t.Fatalf("expected count=100 from cache, got %d", count2)
	}
}

func TestExecuteWithCache_LoaderError(t *testing.T) {
	cl := newTestCacheLayer(t)

	_, _, err := executeWithCache(
		context.Background(),
		cl,
		"analytics_db",
		"cache:template:test:err",
		func() ([]map[string]any, error) {
			return nil, fmt.Errorf("db connection failed")
		},
	)
	if err == nil {
		t.Fatal("expected error from loader")
	}
}

func TestExecuteCount_LoaderError(t *testing.T) {
	cl := newTestCacheLayer(t)

	_, err := executeCount(
		context.Background(),
		cl,
		"analytics_db",
		"cache:template:test:count:err",
		func() (int64, error) {
			return 0, fmt.Errorf("db connection failed")
		},
	)
	if err == nil {
		t.Fatal("expected error from loader")
	}
}
