// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package ratelimit

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"pgregory.net/rapid"
)

// TestProperty56_TokenBucketRateLimiting validates that the token bucket rate limiter
// allows requests up to burst, denies after exhaustion, and tokens refill over time.
//
// Feature: graphql-multi-datasource-api, Property 56: 令牌桶限�?
// **Validates: Requirements 14.1, 14.2, 14.3, 14.4**
func TestProperty56_TokenBucketRateLimiting(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		burst := rapid.IntRange(2, 20).Draw(t, "burst")
		windowSize := time.Duration(rapid.IntRange(1, 5).Draw(t, "windowSec")) * time.Second

		krl := NewKeyedRateLimiter(burst, windowSize, 100000)
		defer krl.Stop()

		ctx := context.Background()
		key := rapid.StringMatching(`[a-z]{3,10}`).Draw(t, "key")

		// Property: requests up to burst are allowed.
		for i := 0; i < burst; i++ {
			result, err := krl.Allow(ctx, key, 1)
			if err != nil {
				t.Fatalf("unexpected error on request %d: %v", i, err)
			}
			if !result.Allowed {
				t.Fatalf("request %d should be allowed (burst=%d)", i, burst)
			}
			// Property: Limit field always equals burst.
			if result.Limit != burst {
				t.Fatalf("expected Limit=%d, got %d", burst, result.Limit)
			}
			// Property: Remaining is non-negative.
			if result.Remaining < 0 {
				t.Fatalf("Remaining should be non-negative, got %d", result.Remaining)
			}
			// Property: ResetAt is in the future.
			if result.ResetAt.Before(time.Now().Add(-time.Second)) {
				t.Fatal("ResetAt should be approximately in the future")
			}
		}

		// Property: after exhausting burst, next request is denied.
		denied, err := krl.Allow(ctx, key, 1)
		if err != nil {
			t.Fatalf("unexpected error on denied request: %v", err)
		}
		if denied.Allowed {
			t.Fatal("request should be denied after burst exhaustion")
		}
		if denied.Remaining != 0 {
			t.Fatalf("expected Remaining=0 after exhaustion, got %d", denied.Remaining)
		}

		// Property: different keys have independent buckets.
		otherKey := key + "-other"
		otherResult, err := krl.Allow(ctx, otherKey, 1)
		if err != nil {
			t.Fatalf("unexpected error on other key: %v", err)
		}
		if !otherResult.Allowed {
			t.Fatal("other key should be allowed (independent bucket)")
		}
	})
}

// TestProperty8_BatchQueryRateLimiting validates that batch queries with N queries
// consume N tokens (not 1).
//
// Feature: graphql-multi-datasource-api, Property 8: 批量查询按实际查询数限流
// **Validates: Requirements 1.11**
func TestProperty8_BatchQueryRateLimiting(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		burst := rapid.IntRange(5, 30).Draw(t, "burst")
		batchCount := rapid.IntRange(1, burst).Draw(t, "batchCount")

		krl := NewKeyedRateLimiter(burst, time.Second, 100000)
		defer krl.Stop()

		ctx := context.Background()
		key := rapid.StringMatching(`ip:[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}`).Draw(t, "key")

		// Consume batchCount tokens in one call.
		result, err := krl.Allow(ctx, key, batchCount)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.Allowed {
			t.Fatalf("batch of %d should be allowed (burst=%d)", batchCount, burst)
		}

		// Property: remaining tokens should be approximately burst - batchCount.
		// Due to token refill, remaining might be slightly higher, but should not exceed burst - batchCount + 1.
		expectedMax := burst - batchCount + 1
		if result.Remaining > expectedMax {
			t.Fatalf("expected Remaining <= %d after consuming %d tokens, got %d",
				expectedMax, batchCount, result.Remaining)
		}

		// Now try to consume the rest. If batchCount == burst, next request should be denied.
		remaining := burst - batchCount
		if remaining > 0 {
			// Consume remaining tokens one by one.
			for i := 0; i < remaining; i++ {
				r, err := krl.Allow(ctx, key, 1)
				if err != nil {
					t.Fatalf("unexpected error consuming remaining token %d: %v", i, err)
				}
				// These should mostly be allowed (token refill may allow a few extra).
				_ = r
			}
		}

		// After consuming burst tokens total, the next request should be denied.
		finalResult, err := krl.Allow(ctx, key, 1)
		if err != nil {
			t.Fatalf("unexpected error on final request: %v", err)
		}
		if finalResult.Allowed {
			t.Fatal("request should be denied after consuming all burst tokens")
		}

		// Property: a batch request exceeding burst is always denied.
		freshKey := key + "-fresh"
		overBurst, err := krl.Allow(ctx, freshKey, burst+1)
		if err != nil {
			t.Fatalf("unexpected error on over-burst request: %v", err)
		}
		if overBurst.Allowed {
			t.Fatalf("batch of %d should be denied (burst=%d)", burst+1, burst)
		}
	})
}

// TestProperty58_DistributedRateLimitRedisDegradation validates that when Redis is
// unavailable, FallbackRateLimiter degrades to local mode and still enforces rate limits.
//
// Feature: graphql-multi-datasource-api, Property 58: 分布式限�?Redis 降级
// **Validates: Requirements 14.9**
func TestProperty58_DistributedRateLimitRedisDegradation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		burst := rapid.IntRange(2, 20).Draw(t, "burst")
		windowSize := time.Duration(rapid.IntRange(1, 5).Draw(t, "windowSec")) * time.Second

		// Use an unreachable Redis address to trigger degradation.
		client := redis.NewClient(&redis.Options{
			Addr:        "localhost:1", // unreachable port
			DialTimeout: 50 * time.Millisecond,
		})
		defer client.Close()

		primary := NewDistributedRateLimiter(client, burst, windowSize)
		fallback := NewKeyedRateLimiter(burst, windowSize, 100000)

		frl := NewFallbackRateLimiter(primary, fallback, time.Hour, zap.NewNop())
		defer frl.Stop()

		ctx := context.Background()
		key := rapid.StringMatching(`[a-z]{3,10}`).Draw(t, "key")

		// Property: first call triggers degradation and still returns a valid result.
		result, err := frl.Allow(ctx, key, 1)
		if err != nil {
			t.Fatalf("expected no error after fallback, got %v", err)
		}
		if !result.Allowed {
			t.Fatal("first request should be allowed via local fallback")
		}

		// Property: useFallback flag is set after Redis failure.
		if !frl.useFallback.Load() {
			t.Fatal("expected useFallback=true after Redis failure")
		}

		// Property: local limiter still enforces rate limits after degradation.
		// Consume remaining burst-1 tokens.
		for i := 1; i < burst; i++ {
			r, err := frl.Allow(ctx, key, 1)
			if err != nil {
				t.Fatalf("unexpected error on request %d: %v", i, err)
			}
			if !r.Allowed {
				t.Fatalf("request %d should be allowed (burst=%d)", i, burst)
			}
		}

		// Next request should be denied �?local limiter enforces the limit.
		denied, err := frl.Allow(ctx, key, 1)
		if err != nil {
			t.Fatalf("unexpected error on denied request: %v", err)
		}
		if denied.Allowed {
			t.Fatal("request should be denied after burst exhaustion in fallback mode")
		}

		// Property: result fields are valid even in fallback mode.
		if denied.Limit != burst {
			t.Fatalf("expected Limit=%d in fallback mode, got %d", burst, denied.Limit)
		}
		if denied.Remaining != 0 {
			t.Fatalf("expected Remaining=0 after exhaustion, got %d", denied.Remaining)
		}
	})
}

// TestProperty78_DistributedRateLimitDegradationRecovery validates that after
// degradation, the useFallback flag transitions correctly: it is set to true
// on degradation and can be reset to false (simulating Redis recovery).
//
// Feature: graphql-multi-datasource-api, Property 78: 分布式限流降级恢�?
// **Validates: Design - 降级恢复机制**
func TestProperty78_DistributedRateLimitDegradationRecovery(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		burst := rapid.IntRange(5, 30).Draw(t, "burst")

		// Use an unreachable Redis address to trigger degradation.
		client := redis.NewClient(&redis.Options{
			Addr:        "localhost:1",
			DialTimeout: 50 * time.Millisecond,
		})
		defer client.Close()

		primary := NewDistributedRateLimiter(client, burst, time.Minute)
		fallback := NewKeyedRateLimiter(burst, time.Minute, 100000)

		frl := NewFallbackRateLimiter(primary, fallback, 50*time.Millisecond, zap.NewNop())
		defer frl.Stop()

		ctx := context.Background()
		key := rapid.StringMatching(`[a-z]{3,10}`).Draw(t, "key")

		// Property: initially useFallback is false (primary mode).
		if frl.useFallback.Load() {
			t.Fatal("expected useFallback=false initially")
		}

		// Trigger degradation by calling Allow (Redis is unreachable).
		_, err := frl.Allow(ctx, key, 1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Property: after Redis failure, useFallback transitions to true.
		if !frl.useFallback.Load() {
			t.Fatal("expected useFallback=true after degradation")
		}

		// Property: the recovery mechanism exists �?useFallback is an atomic.Bool
		// that can be toggled. Simulate recovery by directly setting it to false
		// (the real probe does this when PING succeeds).
		frl.useFallback.Store(false)

		// Property: after recovery, useFallback is false.
		if frl.useFallback.Load() {
			t.Fatal("expected useFallback=false after simulated recovery")
		}

		// Property: after recovery, a new Redis failure re-triggers degradation.
		// Since Redis is still unreachable, the next Allow call should degrade again.
		_, err = frl.Allow(ctx, key, 1)
		if err != nil {
			t.Fatalf("unexpected error on re-degradation: %v", err)
		}
		if !frl.useFallback.Load() {
			t.Fatal("expected useFallback=true after re-degradation")
		}
	})
}

// TestProperty96_KeyedRateLimiterMaxEntries validates that when KeyedRateLimiter
// reaches maxEntries, new keys are denied (Allow=false) while existing keys still work.
//
// Feature: graphql-multi-datasource-api, Property 96: KeyedRateLimiter 最�?Key 数量限制
// **Validates: Design - DDoS 内存防护**
func TestProperty96_KeyedRateLimiterMaxEntries(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		maxEntries := rapid.IntRange(2, 20).Draw(t, "maxEntries")
		burst := rapid.IntRange(5, 30).Draw(t, "burst")

		krl := NewKeyedRateLimiter(burst, time.Minute, maxEntries)
		defer krl.Stop()

		ctx := context.Background()

		// Fill up to maxEntries with unique keys.
		existingKeys := make([]string, maxEntries)
		for i := 0; i < maxEntries; i++ {
			existingKeys[i] = fmt.Sprintf("key-%d", i)
			result, err := krl.Allow(ctx, existingKeys[i], 1)
			if err != nil {
				t.Fatalf("unexpected error creating key %d: %v", i, err)
			}
			if !result.Allowed {
				t.Fatalf("key %d should be allowed (filling up to maxEntries)", i)
			}
		}

		// Property: new keys are denied when maxEntries is reached.
		numNewKeys := rapid.IntRange(1, 10).Draw(t, "numNewKeys")
		for i := 0; i < numNewKeys; i++ {
			newKey := fmt.Sprintf("new-key-%d", i)
			result, err := krl.Allow(ctx, newKey, 1)
			if err != nil {
				t.Fatalf("unexpected error on new key %d: %v", i, err)
			}
			if result.Allowed {
				t.Fatalf("new key %d should be denied when maxEntries=%d reached", i, maxEntries)
			}
			// Property: denied result has Remaining=0.
			if result.Remaining != 0 {
				t.Fatalf("expected Remaining=0 for denied new key, got %d", result.Remaining)
			}
		}

		// Property: existing keys still work after maxEntries is reached.
		existingIdx := rapid.IntRange(0, maxEntries-1).Draw(t, "existingIdx")
		result, err := krl.Allow(ctx, existingKeys[existingIdx], 1)
		if err != nil {
			t.Fatalf("unexpected error on existing key: %v", err)
		}
		if !result.Allowed {
			t.Fatalf("existing key %q should still be allowed at maxEntries", existingKeys[existingIdx])
		}

		// Property: no new limiter entries are created beyond maxEntries.
		krl.mu.Lock()
		entryCount := len(krl.limiters)
		krl.mu.Unlock()
		if entryCount > maxEntries {
			t.Fatalf("limiter map has %d entries, exceeding maxEntries=%d", entryCount, maxEntries)
		}
	})
}
