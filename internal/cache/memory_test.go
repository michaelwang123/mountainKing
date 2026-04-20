// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package cache

import (
	"context"
	"testing"
	"time"
)

func newTestMemoryCache(t *testing.T, maxEntries int, maxMemSize string) *MemoryCache {
	t.Helper()
	mc, err := NewMemoryCache(MemoryCacheConfig{
		MaxEntries:    maxEntries,
		MaxMemorySize: maxMemSize,
	})
	if err != nil {
		t.Fatalf("NewMemoryCache: %v", err)
	}
	return mc
}

func TestMemoryCache_InterfaceCompliance(t *testing.T) {
	var _ Cache = (*MemoryCache)(nil)
}

func TestMemoryCache_GetSetDelete(t *testing.T) {
	mc := newTestMemoryCache(t, 100, "1MB")
	ctx := context.Background()

	// Get on empty cache
	_, found, err := mc.Get(ctx, "k1")
	if err != nil || found {
		t.Fatal("expected miss on empty cache")
	}

	// Set and Get
	if err := mc.Set(ctx, "k1", []byte("v1"), time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}
	val, found, err := mc.Get(ctx, "k1")
	if err != nil || !found || string(val) != "v1" {
		t.Fatalf("expected hit with v1, got found=%v val=%q err=%v", found, val, err)
	}

	// Delete
	if err := mc.Delete(ctx, "k1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, found, _ = mc.Get(ctx, "k1")
	if found {
		t.Fatal("expected miss after delete")
	}
}

func TestMemoryCache_Expiration(t *testing.T) {
	mc := newTestMemoryCache(t, 100, "1MB")
	ctx := context.Background()

	if err := mc.Set(ctx, "k1", []byte("v1"), time.Millisecond); err != nil {
		t.Fatalf("Set: %v", err)
	}
	time.Sleep(5 * time.Millisecond)

	_, found, _ := mc.Get(ctx, "k1")
	if found {
		t.Fatal("expected miss after expiration")
	}
}

func TestMemoryCache_LRUEviction(t *testing.T) {
	mc := newTestMemoryCache(t, 3, "1MB")
	ctx := context.Background()

	for i := 0; i < 4; i++ {
		key := string(rune('a' + i))
		_ = mc.Set(ctx, key, []byte(key), time.Minute)
	}

	// First entry should have been evicted
	_, found, _ := mc.Get(ctx, "a")
	if found {
		t.Fatal("expected 'a' to be evicted by LRU")
	}

	// Last three should still be present
	for _, k := range []string{"b", "c", "d"} {
		_, found, _ := mc.Get(ctx, k)
		if !found {
			t.Fatalf("expected %q to be present", k)
		}
	}
}

func TestMemoryCache_MemoryLimitEviction(t *testing.T) {
	// 100 bytes max memory, entries of 40 bytes each
	mc := newTestMemoryCache(t, 1000, "100B")
	ctx := context.Background()

	data := make([]byte, 40)
	_ = mc.Set(ctx, "k1", data, time.Minute)
	_ = mc.Set(ctx, "k2", data, time.Minute)

	// Both fit (40+40=80 <= 100)
	if mc.Len() != 2 {
		t.Fatalf("expected 2 entries, got %d", mc.Len())
	}

	// Third entry pushes over limit, should evict oldest
	_ = mc.Set(ctx, "k3", data, time.Minute)
	if mc.Len() > 2 {
		t.Fatalf("expected at most 2 entries after memory eviction, got %d", mc.Len())
	}

	// k1 should be evicted (oldest)
	_, found, _ := mc.Get(ctx, "k1")
	if found {
		t.Fatal("expected k1 to be evicted due to memory limit")
	}
}

func TestMemoryCache_MemoryTracking(t *testing.T) {
	mc := newTestMemoryCache(t, 100, "1MB")
	ctx := context.Background()

	data := []byte("hello")
	_ = mc.Set(ctx, "k1", data, time.Minute)

	if mc.MemUsed() != int64(len(data)) {
		t.Fatalf("expected memUsed=%d, got %d", len(data), mc.MemUsed())
	}

	_ = mc.Delete(ctx, "k1")
	if mc.MemUsed() != 0 {
		t.Fatalf("expected memUsed=0 after delete, got %d", mc.MemUsed())
	}
}

func TestMemoryCache_DeleteByPrefix(t *testing.T) {
	mc := newTestMemoryCache(t, 100, "1MB")
	ctx := context.Background()

	_ = mc.Set(ctx, "cache:ds1:abc", []byte("1"), time.Minute)
	_ = mc.Set(ctx, "cache:ds1:def", []byte("2"), time.Minute)
	_ = mc.Set(ctx, "cache:ds2:ghi", []byte("3"), time.Minute)

	if err := mc.DeleteByPrefix(ctx, "cache:ds1:"); err != nil {
		t.Fatalf("DeleteByPrefix: %v", err)
	}

	if mc.Len() != 1 {
		t.Fatalf("expected 1 entry after prefix delete, got %d", mc.Len())
	}

	_, found, _ := mc.Get(ctx, "cache:ds2:ghi")
	if !found {
		t.Fatal("expected ds2 entry to survive prefix delete")
	}
}

func TestMemoryCache_Clear(t *testing.T) {
	mc := newTestMemoryCache(t, 100, "1MB")
	ctx := context.Background()

	_ = mc.Set(ctx, "k1", []byte("v1"), time.Minute)
	_ = mc.Set(ctx, "k2", []byte("v2"), time.Minute)

	if err := mc.Clear(ctx); err != nil {
		t.Fatalf("Clear: %v", err)
	}

	if mc.Len() != 0 {
		t.Fatalf("expected 0 entries after clear, got %d", mc.Len())
	}
	if mc.MemUsed() != 0 {
		t.Fatalf("expected memUsed=0 after clear, got %d", mc.MemUsed())
	}
}

func TestMemoryCache_SetOverwrite(t *testing.T) {
	mc := newTestMemoryCache(t, 100, "1MB")
	ctx := context.Background()

	_ = mc.Set(ctx, "k1", []byte("old"), time.Minute)
	_ = mc.Set(ctx, "k1", []byte("new"), time.Minute)

	val, found, _ := mc.Get(ctx, "k1")
	if !found || string(val) != "new" {
		t.Fatalf("expected overwritten value 'new', got %q found=%v", val, found)
	}
}

func TestParseMemorySize(t *testing.T) {
	tests := []struct {
		input string
		want  int64
		err   bool
	}{
		{"256MB", 256 * 1024 * 1024, false},
		{"1GB", 1024 * 1024 * 1024, false},
		{"512KB", 512 * 1024, false},
		{"100B", 100, false},
		{"", 256 * 1024 * 1024, false},
		{"invalid", 0, true},
		{"-1MB", 0, true},
		{"0MB", 0, true},
	}

	for _, tt := range tests {
		got, err := parseMemorySize(tt.input)
		if tt.err && err == nil {
			t.Errorf("parseMemorySize(%q): expected error", tt.input)
		}
		if !tt.err && err != nil {
			t.Errorf("parseMemorySize(%q): unexpected error: %v", tt.input, err)
		}
		if !tt.err && got != tt.want {
			t.Errorf("parseMemorySize(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}
