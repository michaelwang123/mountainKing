// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package retry

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"pgregory.net/rapid"
)

// TestProperty36_RetryDistinguishesTransientAndBusiness validates that the retry
// strategy correctly distinguishes transient errors (retried) from business errors
// (not retried), and follows exponential backoff.
//
// **Validates: Requirements 9.6, 9.7**
func TestProperty36_RetryDistinguishesTransientAndBusiness(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		maxRetries := rapid.IntRange(1, 5).Draw(t, "maxRetries")
		cfg := Config{MaxRetries: maxRetries, RetryInterval: time.Millisecond}

		// Test 1: Transient errors should be retried up to MaxRetries times
		// Total calls = 1 (initial) + MaxRetries (retries)
		transientCalls := 0
		Do(context.Background(), cfg, func(ctx context.Context) (int, error) {
			transientCalls++
			return 0, io.EOF // transient error
		})
		expectedCalls := 1 + maxRetries
		if transientCalls != expectedCalls {
			t.Fatalf("transient: expected %d calls, got %d", expectedCalls, transientCalls)
		}

		// Test 2: Business errors should NOT be retried â€?exactly 1 call
		businessCalls := 0
		Do(context.Background(), cfg, func(ctx context.Context) (int, error) {
			businessCalls++
			return 0, errors.New("SQL syntax error") // business error
		})
		if businessCalls != 1 {
			t.Fatalf("business: expected 1 call, got %d", businessCalls)
		}

		// Test 3: Verify exponential backoff pattern by measuring elapsed time
		// With RetryInterval=1ms, backoff sequence is: 1ms, 2ms, 4ms, ...
		// Total expected wait â‰?sum(2^i for i in 0..maxRetries-1) ms
		start := time.Now()
		Do(context.Background(), cfg, func(ctx context.Context) (int, error) {
			return 0, io.EOF
		})
		elapsed := time.Since(start)

		// Calculate minimum expected backoff: sum of 1ms * 2^i for i=0..maxRetries-1
		var expectedMinBackoff time.Duration
		for i := 0; i < maxRetries; i++ {
			expectedMinBackoff += time.Millisecond * time.Duration(1<<uint(i))
		}
		// Allow some tolerance â€?elapsed should be at least 50% of expected
		if elapsed < expectedMinBackoff/2 {
			t.Fatalf("backoff too fast: elapsed %v, expected at least %v (50%% of %v)",
				elapsed, expectedMinBackoff/2, expectedMinBackoff)
		}
	})
}
