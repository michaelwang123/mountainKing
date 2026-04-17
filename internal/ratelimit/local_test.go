package ratelimit

import (
	"context"
	"testing"
	"time"
)

func TestNewKeyedRateLimiter(t *testing.T) {
	krl := NewKeyedRateLimiter(100, 60*time.Second, 100000)
	defer krl.Stop()

	if krl.burst != 100 {
		t.Errorf("expected burst=100, got %d", krl.burst)
	}
	if krl.maxEntries != 100000 {
		t.Errorf("expected maxEntries=100000, got %d", krl.maxEntries)
	}
}

func TestAllow_BasicAllow(t *testing.T) {
	krl := NewKeyedRateLimiter(10, time.Second, 100000)
	defer krl.Stop()

	ctx := context.Background()
	result, err := krl.Allow(ctx, "ip:127.0.0.1", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Allowed {
		t.Error("expected request to be allowed")
	}
	if result.Limit != 10 {
		t.Errorf("expected Limit=10, got %d", result.Limit)
	}
}

func TestAllow_ExceedsBurst(t *testing.T) {
	krl := NewKeyedRateLimiter(5, time.Second, 100000)
	defer krl.Stop()

	ctx := context.Background()

	// Consume all 5 tokens at once.
	result, err := krl.Allow(ctx, "ip:test", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Allowed {
		t.Error("expected first batch to be allowed")
	}

	// Next request should be denied (no tokens left).
	result, err = krl.Allow(ctx, "ip:test", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Allowed {
		t.Error("expected request to be denied after burst exhausted")
	}
	if result.Remaining != 0 {
		t.Errorf("expected Remaining=0, got %d", result.Remaining)
	}
}

func TestAllow_CountExceedsBurst(t *testing.T) {
	krl := NewKeyedRateLimiter(5, time.Second, 100000)
	defer krl.Stop()

	ctx := context.Background()

	// Requesting more tokens than burst should fail.
	result, err := krl.Allow(ctx, "ip:test", 6)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Allowed {
		t.Error("expected request exceeding burst to be denied")
	}
}

func TestAllow_IndependentKeys(t *testing.T) {
	krl := NewKeyedRateLimiter(2, time.Second, 100000)
	defer krl.Stop()

	ctx := context.Background()

	// Exhaust key1.
	krl.Allow(ctx, "key1", 2)
	result1, _ := krl.Allow(ctx, "key1", 1)
	if result1.Allowed {
		t.Error("expected key1 to be denied")
	}

	// key2 should still be allowed.
	result2, _ := krl.Allow(ctx, "key2", 1)
	if !result2.Allowed {
		t.Error("expected key2 to be allowed (independent bucket)")
	}
}

func TestAllow_MaxEntriesProtection(t *testing.T) {
	krl := NewKeyedRateLimiter(10, time.Second, 3)
	defer krl.Stop()

	ctx := context.Background()

	// Fill up to maxEntries.
	for i := 0; i < 3; i++ {
		result, err := krl.Allow(ctx, string(rune('a'+i)), 1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.Allowed {
			t.Errorf("expected key %d to be allowed", i)
		}
	}

	// New key should be denied due to maxEntries.
	result, err := krl.Allow(ctx, "new-key", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Allowed {
		t.Error("expected new key to be denied when maxEntries reached")
	}
	if result.Remaining != 0 {
		t.Errorf("expected Remaining=0 for denied key, got %d", result.Remaining)
	}
}

func TestAllow_ExistingKeyStillWorksAtMaxEntries(t *testing.T) {
	krl := NewKeyedRateLimiter(10, time.Second, 2)
	defer krl.Stop()

	ctx := context.Background()

	// Create 2 keys (at max).
	krl.Allow(ctx, "key1", 1)
	krl.Allow(ctx, "key2", 1)

	// Existing key should still work.
	result, err := krl.Allow(ctx, "key1", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Allowed {
		t.Error("expected existing key to still be allowed at maxEntries")
	}
}

func TestAllow_ResultFields(t *testing.T) {
	krl := NewKeyedRateLimiter(100, 60*time.Second, 100000)
	defer krl.Stop()

	ctx := context.Background()
	before := time.Now()
	result, err := krl.Allow(ctx, "ip:test", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Limit != 100 {
		t.Errorf("expected Limit=100, got %d", result.Limit)
	}
	if result.ResetAt.Before(before) {
		t.Error("expected ResetAt to be in the future")
	}
	if result.Remaining < 0 {
		t.Error("expected non-negative Remaining")
	}
}

func TestStop(t *testing.T) {
	krl := NewKeyedRateLimiter(10, time.Second, 100000)
	// Should not panic on double stop or after stop.
	krl.Stop()
}

func TestRateLimiterInterface(t *testing.T) {
	// Verify KeyedRateLimiter satisfies the RateLimiter interface.
	var _ RateLimiter = (*KeyedRateLimiter)(nil)
}
