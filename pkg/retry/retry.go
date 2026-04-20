// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package retry

import (
	"context"
	"fmt"
	"time"
)

// Config holds retry configuration.
type Config struct {
	MaxRetries    int           // Maximum number of retry attempts
	RetryInterval time.Duration // Initial retry interval
}

// Do executes the given function with retry logic using exponential backoff.
//   - For transient errors (IsTransient): retries up to MaxRetries with exponential backoff
//   - For business errors (IsBusiness): returns immediately without retry
//   - Respects context cancellation before each retry wait
//
// The backoff interval doubles each attempt: RetryInterval * 2^(attempt-1).
// Returns the result and the last error encountered.
func Do[T any](ctx context.Context, cfg Config, fn func(ctx context.Context) (T, error)) (T, error) {
	var zero T

	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		result, err := fn(ctx)
		if err == nil {
			return result, nil
		}

		// Business errors are not retryable — return immediately.
		if IsBusiness(err) {
			return zero, err
		}

		// If we've exhausted all retries, return the last error.
		if attempt == cfg.MaxRetries {
			return zero, fmt.Errorf("max retries (%d) exhausted: %w", cfg.MaxRetries, err)
		}

		// Calculate exponential backoff: interval * 2^attempt
		// attempt 0 → interval*1, attempt 1 → interval*2, attempt 2 → interval*4, ...
		backoff := cfg.RetryInterval * (1 << uint(attempt))

		// Check context before waiting.
		select {
		case <-ctx.Done():
			return zero, ctx.Err()
		case <-time.After(backoff):
			// Continue to next attempt.
		}
	}

	// Unreachable, but satisfies the compiler.
	return zero, fmt.Errorf("retry loop exited unexpectedly")
}
