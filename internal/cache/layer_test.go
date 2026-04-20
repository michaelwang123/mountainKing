// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package cache

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// --- helpers ---

func newTestCacheLayer(t *testing.T, opts ...func(*CacheLayerConfig)) *CacheLayer {
	t.Helper()
	mc, err := NewMemoryCache(MemoryCacheConfig{MaxEntries: 1000, MaxMemorySize: "10MB"})
	if err != nil {
		t.Fatal(err)
	}
	cfg := CacheLayerConfig{
		Backend:    mc,
		DefaultTTL: 5 * time.Second,
		JitterPct:  0, // disable jitter for deterministic tests
		EmptyTTL:   1 * time.Second,
		Logger:     zaptest.NewLogger(t),
	}
	for _, o := range opts {
		o(&cfg)
	}
	return NewCacheLayer(cfg)
}

// --- GetOrLoad tests ---

func TestGetOrLoad_CacheMiss_LoadsAndCaches(t *testing.T) {
	cl := newTestCacheLayer(t)
	ctx := context.Background()

	calls := 0
	data := []byte(`{"rows":[1,2,3]}`)

	result, err := cl.GetOrLoad(ctx, "cache:ds1:abc", "ds1", func() ([]byte, error) {
		calls++
		return data, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(result) != string(data) {
		t.Fatalf("expected %q, got %q", data, result)
	}
	if calls != 1 {
		t.Fatalf("expected loader called once, got %d", calls)
	}

	// Second call should hit cache
	result2, err := cl.GetOrLoad(ctx, "cache:ds1:abc", "ds1", func() ([]byte, error) {
		calls++
		return data, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(result2) != string(data) {
		t.Fatalf("expected %q, got %q", data, result2)
	}
	if calls != 1 {
		t.Fatalf("expected loader called once total, got %d", calls)
	}
}

func TestGetOrLoad_EmptyResult_CachesWithShortTTL(t *testing.T) {
	cl := newTestCacheLayer(t)
	ctx := context.Background()

	calls := 0
	result, err := cl.GetOrLoad(ctx, "cache:ds1:empty", "ds1", func() ([]byte, error) {
		calls++
		return nil, nil // empty result
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Fatalf("expected nil result, got %q", result)
	}
	if calls != 1 {
		t.Fatalf("expected 1 call, got %d", calls)
	}

	// Second call should hit the empty marker in cache
	result2, err := cl.GetOrLoad(ctx, "cache:ds1:empty", "ds1", func() ([]byte, error) {
		calls++
		return []byte("should not reach"), nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result2 != nil {
		t.Fatalf("expected nil from empty marker, got %q", result2)
	}
	if calls != 1 {
		t.Fatalf("expected loader not called again, got %d", calls)
	}
}

func TestGetOrLoad_LoaderError_ReturnsError(t *testing.T) {
	cl := newTestCacheLayer(t)
	ctx := context.Background()

	expectedErr := errors.New("datasource timeout")
	_, err := cl.GetOrLoad(ctx, "cache:ds1:err", "ds1", func() ([]byte, error) {
		return nil, expectedErr
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() != expectedErr.Error() {
		t.Fatalf("expected %q, got %q", expectedErr, err)
	}
}

func TestGetOrLoad_Singleflight_DeduplicatesConcurrentLoads(t *testing.T) {
	cl := newTestCacheLayer(t)
	ctx := context.Background()

	var loadCount atomic.Int32
	data := []byte(`concurrent-data`)

	var wg sync.WaitGroup
	const goroutines = 10
	results := make([][]byte, goroutines)
	errs := make([]error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx], errs[idx] = cl.GetOrLoad(ctx, "cache:ds1:sf", "ds1", func() ([]byte, error) {
				loadCount.Add(1)
				time.Sleep(50 * time.Millisecond) // simulate slow load
				return data, nil
			})
		}(i)
	}
	wg.Wait()

	for i := 0; i < goroutines; i++ {
		if errs[i] != nil {
			t.Fatalf("goroutine %d error: %v", i, errs[i])
		}
		if string(results[i]) != string(data) {
			t.Fatalf("goroutine %d: expected %q, got %q", i, data, results[i])
		}
	}

	if loadCount.Load() != 1 {
		t.Fatalf("expected singleflight to call loader once, got %d", loadCount.Load())
	}
}

func TestGetOrLoad_CorruptedCacheEntry_DeletesAndReloads(t *testing.T) {
	mc, err := NewMemoryCache(MemoryCacheConfig{MaxEntries: 100, MaxMemorySize: "1MB"})
	if err != nil {
		t.Fatal(err)
	}

	cl := NewCacheLayer(CacheLayerConfig{
		Backend:    mc,
		DefaultTTL: 5 * time.Second,
		JitterPct:  0,
		EmptyTTL:   1 * time.Second,
		Logger:     zap.NewNop(),
	})
	ctx := context.Background()

	// Manually inject corrupted (non-gob) data into cache
	_ = mc.Set(ctx, "cache:ds1:corrupt", []byte("not-valid-gob"), 5*time.Second)

	calls := 0
	freshData := []byte(`fresh-from-source`)
	result, err := cl.GetOrLoad(ctx, "cache:ds1:corrupt", "ds1", func() ([]byte, error) {
		calls++
		return freshData, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(result) != string(freshData) {
		t.Fatalf("expected fresh data %q, got %q", freshData, result)
	}
	if calls != 1 {
		t.Fatalf("expected loader called once after corruption, got %d", calls)
	}
}

func TestGetOrLoad_PerDatasourceTTL(t *testing.T) {
	cl := newTestCacheLayer(t, func(cfg *CacheLayerConfig) {
		cfg.TTLConfig = map[string]time.Duration{
			"fast_ds": 100 * time.Millisecond,
		}
	})
	ctx := context.Background()

	calls := 0
	data := []byte(`ttl-test`)

	_, err := cl.GetOrLoad(ctx, "cache:fast_ds:ttl", "fast_ds", func() ([]byte, error) {
		calls++
		return data, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 call, got %d", calls)
	}

	// Wait for the short TTL to expire
	time.Sleep(150 * time.Millisecond)

	_, err = cl.GetOrLoad(ctx, "cache:fast_ds:ttl", "fast_ds", func() ([]byte, error) {
		calls++
		return data, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("expected 2 calls after TTL expiry, got %d", calls)
	}
}

// --- ClearByDatasource tests ---

func TestClearByDatasource(t *testing.T) {
	cl := newTestCacheLayer(t)
	ctx := context.Background()

	// Populate cache for two datasources
	_, _ = cl.GetOrLoad(ctx, "cache:ds1:a", "ds1", func() ([]byte, error) { return []byte("a"), nil })
	_, _ = cl.GetOrLoad(ctx, "cache:ds1:b", "ds1", func() ([]byte, error) { return []byte("b"), nil })
	_, _ = cl.GetOrLoad(ctx, "cache:ds2:c", "ds2", func() ([]byte, error) { return []byte("c"), nil })

	// Clear ds1 only
	if err := cl.ClearByDatasource(ctx, "ds1"); err != nil {
		t.Fatalf("ClearByDatasource error: %v", err)
	}

	// ds1 entries should be gone (loader called again)
	ds1Calls := 0
	_, _ = cl.GetOrLoad(ctx, "cache:ds1:a", "ds1", func() ([]byte, error) {
		ds1Calls++
		return []byte("a-new"), nil
	})
	if ds1Calls != 1 {
		t.Fatalf("expected ds1 entry cleared, loader should be called, got %d", ds1Calls)
	}

	// ds2 entry should still be cached
	ds2Calls := 0
	_, _ = cl.GetOrLoad(ctx, "cache:ds2:c", "ds2", func() ([]byte, error) {
		ds2Calls++
		return []byte("c-new"), nil
	})
	if ds2Calls != 0 {
		t.Fatalf("expected ds2 entry still cached, loader should not be called, got %d", ds2Calls)
	}
}

// --- ClearAll tests ---

func TestClearAll(t *testing.T) {
	cl := newTestCacheLayer(t)
	ctx := context.Background()

	_, _ = cl.GetOrLoad(ctx, "cache:ds1:x", "ds1", func() ([]byte, error) { return []byte("x"), nil })
	_, _ = cl.GetOrLoad(ctx, "cache:ds2:y", "ds2", func() ([]byte, error) { return []byte("y"), nil })

	if err := cl.ClearAll(ctx); err != nil {
		t.Fatalf("ClearAll error: %v", err)
	}

	// Both should be cleared
	calls := 0
	_, _ = cl.GetOrLoad(ctx, "cache:ds1:x", "ds1", func() ([]byte, error) {
		calls++
		return []byte("x-new"), nil
	})
	_, _ = cl.GetOrLoad(ctx, "cache:ds2:y", "ds2", func() ([]byte, error) {
		calls++
		return []byte("y-new"), nil
	})
	if calls != 2 {
		t.Fatalf("expected both entries cleared, got %d loader calls", calls)
	}
}

// --- Jitter tests ---

func TestApplyJitter_ProducesValueInRange(t *testing.T) {
	cl := NewCacheLayer(CacheLayerConfig{
		Backend:    nil, // not used for this test
		DefaultTTL: 100 * time.Second,
		JitterPct:  10,
		Logger:     zap.NewNop(),
	})

	baseTTL := 100 * time.Second
	minExpected := time.Duration(float64(baseTTL) * 0.90)
	maxExpected := time.Duration(float64(baseTTL) * 1.10)

	for i := 0; i < 100; i++ {
		jittered := cl.applyJitter(baseTTL)
		if jittered < minExpected || jittered > maxExpected {
			t.Fatalf("jittered TTL %v out of range [%v, %v]", jittered, minExpected, maxExpected)
		}
	}
}

func TestApplyJitter_ZeroPercent_NoChange(t *testing.T) {
	cl := NewCacheLayer(CacheLayerConfig{
		Backend:    nil,
		DefaultTTL: 60 * time.Second,
		JitterPct:  0,
		Logger:     zap.NewNop(),
	})

	ttl := 60 * time.Second
	result := cl.applyJitter(ttl)
	if result != ttl {
		t.Fatalf("expected no jitter, got %v", result)
	}
}

// --- NewCacheLayer defaults ---

func TestNewCacheLayer_Defaults(t *testing.T) {
	cl := NewCacheLayer(CacheLayerConfig{})

	if cl.defaultTTL != 60*time.Second {
		t.Fatalf("expected default TTL 60s, got %v", cl.defaultTTL)
	}
	// JitterPct defaults to 0 in CacheLayer; the config layer (config.go) provides 10.
	if cl.jitterPct != 0 {
		t.Fatalf("expected default jitter 0%%, got %d", cl.jitterPct)
	}
	if cl.emptyTTL != 30*time.Second {
		t.Fatalf("expected default empty TTL 30s, got %v", cl.emptyTTL)
	}
}
