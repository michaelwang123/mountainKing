// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package ratelimit

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

func TestFallbackRateLimiter_InterfaceCompliance(t *testing.T) {
	var _ RateLimiter = (*FallbackRateLimiter)(nil)
}

func TestNewFallbackRateLimiter_Defaults(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	defer client.Close()

	primary := NewDistributedRateLimiter(client, 100, time.Minute)
	fallback := NewKeyedRateLimiter(100, time.Minute, 1000)

	frl := NewFallbackRateLimiter(primary, fallback, 0, nil)
	defer frl.Stop()

	if frl.probeInterval != 30*time.Second {
		t.Errorf("expected default probeInterval=30s, got %v", frl.probeInterval)
	}
	if frl.logger == nil {
		t.Error("expected non-nil logger when nil is passed")
	}
	if frl.useFallback.Load() {
		t.Error("expected useFallback to start as false")
	}
}

func TestNewFallbackRateLimiter_CustomProbeInterval(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	defer client.Close()

	primary := NewDistributedRateLimiter(client, 100, time.Minute)
	fallback := NewKeyedRateLimiter(100, time.Minute, 1000)

	frl := NewFallbackRateLimiter(primary, fallback, 10*time.Second, zaptest.NewLogger(t))
	defer frl.Stop()

	if frl.probeInterval != 10*time.Second {
		t.Errorf("expected probeInterval=10s, got %v", frl.probeInterval)
	}
}

// TestFallbackRateLimiter_DegradesToLocal verifies that when the primary
// (distributed) limiter fails, Allow falls back to the local limiter and
// returns a valid result without error.
func TestFallbackRateLimiter_DegradesToLocal(t *testing.T) {
	// Use an unreachable Redis address so the primary always fails.
	client := redis.NewClient(&redis.Options{
		Addr:        "localhost:1", // unreachable
		DialTimeout: 50 * time.Millisecond,
	})
	defer client.Close()

	primary := NewDistributedRateLimiter(client, 100, time.Minute)
	fallback := NewKeyedRateLimiter(100, time.Minute, 1000)

	logger := zaptest.NewLogger(t)
	frl := NewFallbackRateLimiter(primary, fallback, time.Hour, logger)
	defer frl.Stop() // Stop also stops the fallback

	ctx := context.Background()
	result, err := frl.Allow(ctx, "test-key", 1)
	if err != nil {
		t.Fatalf("expected no error after fallback, got %v", err)
	}
	if !result.Allowed {
		t.Error("expected request to be allowed via local fallback")
	}
	if !frl.useFallback.Load() {
		t.Error("expected useFallback to be true after degradation")
	}
}

// TestFallbackRateLimiter_UsesLocalWhenDegraded verifies that subsequent
// calls use the local limiter once degraded.
func TestFallbackRateLimiter_UsesLocalWhenDegraded(t *testing.T) {
	client := redis.NewClient(&redis.Options{
		Addr:        "localhost:1",
		DialTimeout: 50 * time.Millisecond,
	})
	defer client.Close()

	primary := NewDistributedRateLimiter(client, 5, time.Minute)
	fallback := NewKeyedRateLimiter(5, time.Minute, 1000)

	frl := NewFallbackRateLimiter(primary, fallback, time.Hour, zaptest.NewLogger(t))
	defer frl.Stop()

	ctx := context.Background()

	// First call triggers degradation.
	_, _ = frl.Allow(ctx, "key", 1)

	// Subsequent calls should use local limiter and succeed.
	for i := 0; i < 4; i++ {
		result, err := frl.Allow(ctx, "key", 1)
		if err != nil {
			t.Fatalf("call %d: unexpected error: %v", i, err)
		}
		if !result.Allowed {
			t.Errorf("call %d: expected allowed", i)
		}
	}
}

// TestFallbackRateLimiter_LocalLimiterEnforcesLimit verifies that the
// fallback local limiter still enforces rate limits after degradation.
func TestFallbackRateLimiter_LocalLimiterEnforcesLimit(t *testing.T) {
	client := redis.NewClient(&redis.Options{
		Addr:        "localhost:1",
		DialTimeout: 50 * time.Millisecond,
	})
	defer client.Close()

	primary := NewDistributedRateLimiter(client, 2, time.Minute)
	fallback := NewKeyedRateLimiter(2, time.Minute, 1000)

	frl := NewFallbackRateLimiter(primary, fallback, time.Hour, zaptest.NewLogger(t))
	defer frl.Stop()

	ctx := context.Background()

	// Consume all tokens via fallback.
	_, _ = frl.Allow(ctx, "key", 1) // triggers degradation + consumes 1
	_, _ = frl.Allow(ctx, "key", 1) // consumes 1

	// Third request should be denied.
	result, err := frl.Allow(ctx, "key", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Allowed {
		t.Error("expected request to be denied after tokens exhausted")
	}
}

// TestFallbackRateLimiter_RecoveryProbe verifies that the background probe
// switches back to primary when Redis becomes available.
func TestFallbackRateLimiter_RecoveryProbe(t *testing.T) {
	// Start with an unreachable address to trigger degradation.
	client := redis.NewClient(&redis.Options{
		Addr:        "localhost:1",
		DialTimeout: 50 * time.Millisecond,
	})
	defer client.Close()

	primary := NewDistributedRateLimiter(client, 100, time.Minute)
	fallback := NewKeyedRateLimiter(100, time.Minute, 1000)

	// Use a very short probe interval for testing.
	frl := NewFallbackRateLimiter(primary, fallback, 50*time.Millisecond, zaptest.NewLogger(t))
	defer frl.Stop()

	ctx := context.Background()

	// Trigger degradation.
	_, _ = frl.Allow(ctx, "key", 1)
	if !frl.useFallback.Load() {
		t.Fatal("expected useFallback=true after degradation")
	}

	// The probe is running but Redis is still unreachable, so useFallback
	// should remain true after a few probe cycles.
	time.Sleep(200 * time.Millisecond)
	if !frl.useFallback.Load() {
		t.Error("expected useFallback to remain true while Redis is unreachable")
	}
}

// TestFallbackRateLimiter_Stop verifies that Stop can be called safely.
func TestFallbackRateLimiter_Stop(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	defer client.Close()

	primary := NewDistributedRateLimiter(client, 100, time.Minute)
	fallback := NewKeyedRateLimiter(100, time.Minute, 1000)

	frl := NewFallbackRateLimiter(primary, fallback, time.Second, zap.NewNop())

	// Stop should not panic even without any Allow calls.
	frl.Stop()
}

// TestFallbackRateLimiter_StopDuringProbe verifies that Stop terminates
// a running recovery probe.
func TestFallbackRateLimiter_StopDuringProbe(t *testing.T) {
	client := redis.NewClient(&redis.Options{
		Addr:        "localhost:1",
		DialTimeout: 50 * time.Millisecond,
	})
	defer client.Close()

	primary := NewDistributedRateLimiter(client, 100, time.Minute)
	fallback := NewKeyedRateLimiter(100, time.Minute, 1000)

	frl := NewFallbackRateLimiter(primary, fallback, 50*time.Millisecond, zaptest.NewLogger(t))

	ctx := context.Background()
	_, _ = frl.Allow(ctx, "key", 1) // trigger degradation + probe

	// Give the probe goroutine time to start.
	time.Sleep(30 * time.Millisecond)

	// Stop should terminate cleanly.
	frl.Stop()

	// Brief wait to ensure goroutine exits.
	time.Sleep(100 * time.Millisecond)
}

// TestFallbackRateLimiter_ResultFields verifies the RateLimitResult fields
// returned via the fallback path.
func TestFallbackRateLimiter_ResultFields(t *testing.T) {
	client := redis.NewClient(&redis.Options{
		Addr:        "localhost:1",
		DialTimeout: 50 * time.Millisecond,
	})
	defer client.Close()

	primary := NewDistributedRateLimiter(client, 10, time.Minute)
	fallback := NewKeyedRateLimiter(10, time.Minute, 1000)

	frl := NewFallbackRateLimiter(primary, fallback, time.Hour, zaptest.NewLogger(t))
	defer frl.Stop()

	ctx := context.Background()
	result, err := frl.Allow(ctx, "fields-key", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Limit != 10 {
		t.Errorf("expected Limit=10, got %d", result.Limit)
	}
	if result.ResetAt.IsZero() {
		t.Error("expected non-zero ResetAt")
	}
}
