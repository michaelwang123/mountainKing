// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

// Package middleware provides HTTP middleware components for the GraphQL API service.
// RateLimitMiddleware enforces per-client request rate limiting using the
// Token Bucket algorithm. It supports API Key, JWT, and IP-based rate limit keys.
package middleware

import (
	"encoding/json"
	"net/http"
	"strconv"

	ctxkeys "github.com/michaelwang123/mountainKing/internal/context"
	apierrors "github.com/michaelwang123/mountainKing/internal/errors"
	"github.com/michaelwang123/mountainKing/internal/ratelimit"
)

// IPExtractor extracts the real client IP from an HTTP request.
// AuthFailureLimiter implements this interface.
type IPExtractor interface {
	ExtractClientIP(r *http.Request) string
}

// RateLimitMiddleware returns a chi-compatible middleware that enforces
// per-client rate limiting. Public endpoints (/health, /ready, /metrics,
// /playground) are exempt from rate limiting.
//
// Rate limit key priority:
//  1. API Key auth → "apikey:{id}"
//  2. JWT auth → "jwt:{sub}"
//  3. Fallback → "ip:{addr}"
//
// Batch queries consume tokens equal to the number of queries in the batch.
// Response headers X-RateLimit-Limit, X-RateLimit-Remaining, and
// X-RateLimit-Reset are set on all non-public responses.
func RateLimitMiddleware(limiter ratelimit.RateLimiter, extractor IPExtractor) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Public endpoints are exempt from rate limiting.
			if isPublicEndpoint(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			key := extractRateLimitKey(r, extractor)
			count := extractBatchCount(r)

			result, err := limiter.Allow(r.Context(), key, count)
			if err != nil {
				// On limiter error, allow the request through but skip headers.
				next.ServeHTTP(w, r)
				return
			}

			// Always set rate limit headers on non-public requests.
			setRateLimitHeaders(w, result)

			if !result.Allowed {
				writeRateLimitError(w, r)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// extractRateLimitKey determines the rate limit key from the request context.
// Priority: API Key ID > JWT sub > client IP.
func extractRateLimitKey(r *http.Request, extractor IPExtractor) string {
	if identity, ok := r.Context().Value(ctxkeys.CtxKeyAuthIdentity).(*AuthIdentity); ok && identity != nil {
		switch identity.Method {
		case "apikey":
			return "apikey:" + identity.Subject
		case "jwt":
			return "jwt:" + identity.Subject
		}
	}

	// Fallback to IP.
	if extractor != nil {
		return "ip:" + extractor.ExtractClientIP(r)
	}
	return "ip:" + stripPort(r.RemoteAddr)
}

// extractBatchCount reads the batch query count from the request context.
// Defaults to 1 if not set.
func extractBatchCount(r *http.Request) int {
	if count, ok := r.Context().Value(ctxkeys.CtxKeyBatchQueryCount).(int); ok && count > 0 {
		return count
	}
	return 1
}

// setRateLimitHeaders sets the X-RateLimit-* response headers.
func setRateLimitHeaders(w http.ResponseWriter, result *ratelimit.RateLimitResult) {
	w.Header().Set("X-RateLimit-Limit", strconv.Itoa(result.Limit))
	w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(result.Remaining))
	w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(result.ResetAt.Unix(), 10))
}

// writeRateLimitError writes a 429 JSON error response.
func writeRateLimitError(w http.ResponseWriter, r *http.Request) {
	requestID, _ := r.Context().Value(ctxkeys.CtxKeyRequestID).(string)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusTooManyRequests)

	resp := map[string]any{
		"error": map[string]any{
			"code":           apierrors.ErrRateLimitExceeded,
			"message":        "rate limit exceeded, retry after reset",
			"classification": apierrors.Classification(apierrors.ErrRateLimitExceeded),
		},
	}
	if requestID != "" {
		resp["requestId"] = requestID
	}
	_ = json.NewEncoder(w).Encode(resp)
}
