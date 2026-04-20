// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package cache

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	"math/rand/v2"
	"time"

	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"
)

// CacheLayer provides a caching layer with protection against cache penetration,
// avalanche, and breakdown. It wraps a Cache backend with singleflight deduplication,
// TTL jitter, and empty-result short TTL.
type CacheLayer struct {
	backend    Cache
	sfGroup    singleflight.Group
	ttlConfig  map[string]time.Duration // per-datasource TTL overrides
	defaultTTL time.Duration
	jitterPct  int           // TTL jitter percentage (default 10 → ±10%)
	emptyTTL   time.Duration // short TTL for empty results (default 30s)
	keyGen     *CacheKeyGenerator
	logger     *zap.Logger
}

// CacheLayerConfig holds configuration for creating a CacheLayer.
type CacheLayerConfig struct {
	Backend    Cache
	TTLConfig  map[string]time.Duration
	DefaultTTL time.Duration
	JitterPct  int
	EmptyTTL   time.Duration
	Logger     *zap.Logger
}

// emptyMarker is a sentinel value stored in cache to represent empty results.
// It prevents cache penetration by caching "no data" with a short TTL.
var emptyMarker = []byte("__empty__")

// NewCacheLayer creates a new CacheLayer with the given configuration.
func NewCacheLayer(cfg CacheLayerConfig) *CacheLayer {
	if cfg.DefaultTTL == 0 {
		cfg.DefaultTTL = 60 * time.Second
	}
	if cfg.JitterPct < 0 {
		cfg.JitterPct = 0
	}
	if cfg.EmptyTTL == 0 {
		cfg.EmptyTTL = 30 * time.Second
	}
	if cfg.Logger == nil {
		cfg.Logger = zap.NewNop()
	}
	if cfg.TTLConfig == nil {
		cfg.TTLConfig = make(map[string]time.Duration)
	}

	return &CacheLayer{
		backend:    cfg.Backend,
		ttlConfig:  cfg.TTLConfig,
		defaultTTL: cfg.DefaultTTL,
		jitterPct:  cfg.JitterPct,
		emptyTTL:   cfg.EmptyTTL,
		keyGen:     &CacheKeyGenerator{},
		logger:     cfg.Logger,
	}
}

// GetOrLoad checks the cache for the given key. On a miss, it uses singleflight
// to ensure only one goroutine loads from the source, then caches the result.
//
// Flow:
// 1. Check cache → hit → return (deserialize via gob)
// 2. Miss → singleflight ensures single loader execution
// 3. Empty result → cache with short TTL (emptyTTL)
// 4. Non-empty result → cache with TTL + jitter
//
// If gob deserialization fails on a cache hit, the corrupted entry is deleted,
// the loader is called to fetch fresh data, and a WARN log is emitted.
func (cl *CacheLayer) GetOrLoad(ctx context.Context, key string, datasource string, loader func() ([]byte, error)) ([]byte, error) {
	// Step 1: Check cache
	cached, found, err := cl.backend.Get(ctx, key)
	if err != nil {
		cl.logger.Warn("cache get error, falling through to loader",
			zap.String("key", key),
			zap.Error(err),
		)
	} else if found {
		// Check for empty marker
		if bytes.Equal(cached, emptyMarker) {
			return nil, nil
		}

		// Try gob deserialization to validate the entry
		var decoded []byte
		if gobErr := gobDecode(cached, &decoded); gobErr != nil {
			// Corrupted entry: delete it, log warning, and fall through to loader
			cl.logger.Warn("gob deserialization failed, deleting corrupted cache entry",
				zap.String("key", key),
				zap.Error(gobErr),
			)
			_ = cl.backend.Delete(ctx, key)
		} else {
			return decoded, nil
		}
	}

	// Step 2: Cache miss → singleflight
	val, sfErr, _ := cl.sfGroup.Do(key, func() (interface{}, error) {
		result, loadErr := loader()
		if loadErr != nil {
			return nil, loadErr
		}

		ttl := cl.ttlForDatasource(datasource)

		if len(result) == 0 {
			// Step 3: Empty result → short TTL
			_ = cl.backend.Set(ctx, key, emptyMarker, cl.emptyTTL)
			return nil, nil
		}

		// Step 4: Non-empty → gob encode and cache with jittered TTL
		encoded, encErr := gobEncode(result)
		if encErr != nil {
			cl.logger.Warn("gob encode failed, returning result without caching",
				zap.String("key", key),
				zap.Error(encErr),
			)
			return result, nil
		}

		jitteredTTL := cl.applyJitter(ttl)
		_ = cl.backend.Set(ctx, key, encoded, jitteredTTL)

		return result, nil
	})

	if sfErr != nil {
		return nil, sfErr
	}

	if val == nil {
		return nil, nil
	}

	return val.([]byte), nil
}

// ClearByDatasource clears all cache entries for the given datasource.
// Cache keys use the format "cache:{datasource}:{hash}", so we delete by prefix.
func (cl *CacheLayer) ClearByDatasource(ctx context.Context, datasource string) error {
	prefix := fmt.Sprintf("cache:%s:", datasource)
	return cl.backend.DeleteByPrefix(ctx, prefix)
}

// ClearAll clears all cache entries.
func (cl *CacheLayer) ClearAll(ctx context.Context) error {
	return cl.backend.Clear(ctx)
}

// ttlForDatasource returns the TTL for the given datasource, falling back to defaultTTL.
func (cl *CacheLayer) ttlForDatasource(datasource string) time.Duration {
	if ttl, ok := cl.ttlConfig[datasource]; ok {
		return ttl
	}
	return cl.defaultTTL
}

// applyJitter adds ±jitterPct% random jitter to the given TTL to prevent cache avalanche.
func (cl *CacheLayer) applyJitter(ttl time.Duration) time.Duration {
	if cl.jitterPct <= 0 {
		return ttl
	}
	// jitter range: [-jitterPct%, +jitterPct%]
	jitterFraction := (rand.Float64()*2 - 1) * float64(cl.jitterPct) / 100.0
	jittered := time.Duration(float64(ttl) * (1 + jitterFraction))
	if jittered <= 0 {
		return ttl
	}
	return jittered
}

// gobEncode encodes data using gob serialization.
func gobEncode(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	if err := enc.Encode(data); err != nil {
		return nil, fmt.Errorf("gob encode: %w", err)
	}
	return buf.Bytes(), nil
}

// gobDecode decodes gob-serialized data.
func gobDecode(data []byte, out *[]byte) error {
	buf := bytes.NewReader(data)
	dec := gob.NewDecoder(buf)
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("gob decode: %w", err)
	}
	return nil
}
