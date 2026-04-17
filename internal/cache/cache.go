// Package cache provides caching infrastructure for GraphQL query results.
// It defines the Cache interface for pluggable backends (memory, Redis) and
// utilities for cache key generation and query normalization.
package cache

import (
	"context"
	"time"
)

// Cache defines the interface for cache backends.
// Implementations must be safe for concurrent use.
type Cache interface {
	// Get retrieves a cached value by key. Returns the value, whether it was found, and any error.
	Get(ctx context.Context, key string) ([]byte, bool, error)

	// Set stores a value with the given key and TTL.
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error

	// Delete removes a single cache entry by key.
	Delete(ctx context.Context, key string) error

	// DeleteByPrefix removes all cache entries whose keys start with the given prefix.
	DeleteByPrefix(ctx context.Context, prefix string) error

	// Clear removes all cache entries.
	Clear(ctx context.Context) error
}
