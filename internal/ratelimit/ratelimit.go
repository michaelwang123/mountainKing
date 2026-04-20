// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

// Package ratelimit provides rate limiting implementations for the GraphQL API service.
// It supports both local (in-process) and distributed (Redis-based) rate limiting
// using the Token Bucket algorithm.
package ratelimit

import (
	"context"
	"time"
)

// RateLimiter is the interface for rate limiting implementations.
// Implementations include KeyedRateLimiter (local) and DistributedRateLimiter (Redis).
type RateLimiter interface {
	// Allow checks whether a request identified by key should be allowed.
	// count is the number of tokens to consume (e.g., batch query count).
	// The result always contains rate limit status information.
	Allow(ctx context.Context, key string, count int) (*RateLimitResult, error)
}

// RateLimitResult contains the outcome of a rate limit check.
type RateLimitResult struct {
	Allowed   bool      // Whether the request is allowed through.
	Limit     int       // The rate limit ceiling (requests_per_window).
	Remaining int       // Number of remaining requests in the current window.
	ResetAt   time.Time // When the rate limit resets (used for X-RateLimit-Reset header).
}
