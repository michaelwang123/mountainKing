package cache

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
)

// memoryCacheEntry holds a cached value along with its expiration time.
type memoryCacheEntry struct {
	Value     []byte
	ExpiresAt time.Time
}

// MemoryCache is an in-memory Cache implementation backed by an LRU cache.
// It enforces dual limits: max entries and max memory size.
// Memory tracking uses the stored []byte length as an approximation.
type MemoryCache struct {
	cache     *lru.Cache[string, memoryCacheEntry]
	memUsed   atomic.Int64
	maxMemory int64
}

// MemoryCacheConfig holds configuration for the in-memory cache backend.
type MemoryCacheConfig struct {
	MaxEntries    int
	MaxMemorySize string
}

// NewMemoryCache creates a new MemoryCache with the given configuration.
// MaxEntries controls the LRU entry limit. MaxMemorySize (e.g. "256MB")
// controls the approximate memory limit. When either limit is reached,
// the LRU eviction policy removes the least recently used entry.
func NewMemoryCache(cfg MemoryCacheConfig) (*MemoryCache, error) {
	maxMem, err := parseMemorySize(cfg.MaxMemorySize)
	if err != nil {
		return nil, fmt.Errorf("invalid max_memory_size %q: %w", cfg.MaxMemorySize, err)
	}

	maxEntries := cfg.MaxEntries
	if maxEntries <= 0 {
		maxEntries = 10000
	}

	mc := &MemoryCache{
		maxMemory: maxMem,
	}

	cache, err := lru.NewWithEvict(maxEntries, mc.onEvict)
	if err != nil {
		return nil, fmt.Errorf("failed to create LRU cache: %w", err)
	}
	mc.cache = cache

	return mc, nil
}

// onEvict is called when an entry is evicted from the LRU cache.
// It decrements the tracked memory usage.
func (mc *MemoryCache) onEvict(_ string, entry memoryCacheEntry) {
	mc.memUsed.Add(-int64(len(entry.Value)))
}

// Get retrieves a cached value by key. Returns the value, whether it was found,
// and any error. Expired entries are removed on access.
func (mc *MemoryCache) Get(_ context.Context, key string) ([]byte, bool, error) {
	entry, ok := mc.cache.Get(key)
	if !ok {
		return nil, false, nil
	}

	if time.Now().After(entry.ExpiresAt) {
		mc.cache.Remove(key)
		return nil, false, nil
	}

	return entry.Value, true, nil
}

// Set stores a value with the given key and TTL. If adding the entry would
// exceed the memory limit, older entries are evicted until there is room.
func (mc *MemoryCache) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	entrySize := int64(len(value))

	// If the entry already exists, remove it first to reclaim memory.
	if _, ok := mc.cache.Peek(key); ok {
		mc.cache.Remove(key)
	}

	// Evict entries until we have room for the new entry.
	for mc.memUsed.Load()+entrySize > mc.maxMemory && mc.cache.Len() > 0 {
		mc.cache.RemoveOldest()
	}

	entry := memoryCacheEntry{
		Value:     value,
		ExpiresAt: time.Now().Add(ttl),
	}

	mc.memUsed.Add(entrySize)
	mc.cache.Add(key, entry)

	return nil
}

// Delete removes a single cache entry by key.
func (mc *MemoryCache) Delete(_ context.Context, key string) error {
	mc.cache.Remove(key)
	return nil
}

// DeleteByPrefix removes all cache entries whose keys start with the given prefix.
// Time complexity is O(N) where N is the number of entries.
func (mc *MemoryCache) DeleteByPrefix(_ context.Context, prefix string) error {
	keys := mc.cache.Keys()
	for _, k := range keys {
		if strings.HasPrefix(k, prefix) {
			mc.cache.Remove(k)
		}
	}
	return nil
}

// Clear removes all cache entries and resets memory tracking.
func (mc *MemoryCache) Clear(_ context.Context) error {
	mc.cache.Purge()
	mc.memUsed.Store(0)
	return nil
}

// MemUsed returns the current approximate memory usage in bytes.
func (mc *MemoryCache) MemUsed() int64 {
	return mc.memUsed.Load()
}

// Len returns the number of entries in the cache.
func (mc *MemoryCache) Len() int {
	return mc.cache.Len()
}

// parseMemorySize parses a human-readable memory size string (e.g. "256MB", "1GB")
// into bytes. Supported suffixes: B, KB, MB, GB.
func parseMemorySize(s string) (int64, error) {
	if s == "" {
		return 256 * 1024 * 1024, nil // default 256MB
	}

	s = strings.TrimSpace(strings.ToUpper(s))

	multipliers := []struct {
		suffix string
		mult   int64
	}{
		{"GB", 1024 * 1024 * 1024},
		{"MB", 1024 * 1024},
		{"KB", 1024},
		{"B", 1},
	}

	for _, m := range multipliers {
		if strings.HasSuffix(s, m.suffix) {
			numStr := strings.TrimSpace(s[:len(s)-len(m.suffix)])
			n, err := strconv.ParseInt(numStr, 10, 64)
			if err != nil {
				return 0, fmt.Errorf("invalid number %q: %w", numStr, err)
			}
			if n <= 0 {
				return 0, fmt.Errorf("memory size must be positive, got %d", n)
			}
			return n * m.mult, nil
		}
	}

	// Try parsing as raw bytes
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("unsupported memory size format %q", s)
	}
	if n <= 0 {
		return 0, fmt.Errorf("memory size must be positive, got %d", n)
	}
	return n, nil
}
