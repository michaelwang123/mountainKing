// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package middleware

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"pgregory.net/rapid"

	ctxkeys "github.com/michaelwang123/mountainKing/internal/context"
	"github.com/michaelwang123/mountainKing/internal/ratelimit"
)

// mockPropertyRateLimiter captures Allow calls for property testing.
type mockPropertyRateLimiter struct {
	result  *ratelimit.RateLimitResult
	err     error
	lastKey string
	lastCnt int
}

func (m *mockPropertyRateLimiter) Allow(_ context.Context, key string, count int) (*ratelimit.RateLimitResult, error) {
	m.lastKey = key
	m.lastCnt = count
	return m.result, m.err
}

// mockPropertyIPExtractor returns a fixed IP for testing.
type mockPropertyIPExtractor struct {
	ip string
}

func (m *mockPropertyIPExtractor) ExtractClientIP(_ *http.Request) string {
	return m.ip
}

// TestProperty57_RateLimitResponseHeadersAlwaysPresent validates that
// X-RateLimit-Limit, X-RateLimit-Remaining, and X-RateLimit-Reset headers
// are present on ALL non-public endpoint responses (both allowed and denied).
//
// Feature: graphql-multi-datasource-api, Property 57: 限流响应头始终存在
// **Validates: Requirements 14.4**
func TestProperty57_RateLimitResponseHeadersAlwaysPresent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		allowed := rapid.Bool().Draw(t, "allowed")
		limit := rapid.IntRange(10, 1000).Draw(t, "limit")
		remaining := rapid.IntRange(0, limit).Draw(t, "remaining")
		resetUnix := rapid.Int64Range(1700000000, 1800000000).Draw(t, "resetUnix")

		limiter := &mockPropertyRateLimiter{
			result: &ratelimit.RateLimitResult{
				Allowed:   allowed,
				Limit:     limit,
				Remaining: remaining,
				ResetAt:   time.Unix(resetUnix, 0),
			},
		}

		innerHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		handler := RateLimitMiddleware(limiter, nil)(innerHandler)

		// Test against non-public endpoints.
		nonPublicPaths := []string{"/graphql", "/api/data", "/query"}
		pathIdx := rapid.IntRange(0, len(nonPublicPaths)-1).Draw(t, "pathIdx")
		path := nonPublicPaths[pathIdx]

		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.RemoteAddr = "10.0.0.1:1234"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		// Property: X-RateLimit-Limit header is always present.
		if rec.Header().Get("X-RateLimit-Limit") == "" {
			t.Fatalf("missing X-RateLimit-Limit header (allowed=%v, path=%s)", allowed, path)
		}

		// Property: X-RateLimit-Remaining header is always present.
		if rec.Header().Get("X-RateLimit-Remaining") == "" {
			t.Fatalf("missing X-RateLimit-Remaining header (allowed=%v, path=%s)", allowed, path)
		}

		// Property: X-RateLimit-Reset header is always present.
		if rec.Header().Get("X-RateLimit-Reset") == "" {
			t.Fatalf("missing X-RateLimit-Reset header (allowed=%v, path=%s)", allowed, path)
		}

		// Property: header values match the limiter result.
		if rec.Header().Get("X-RateLimit-Limit") != fmt.Sprintf("%d", limit) {
			t.Fatalf("X-RateLimit-Limit mismatch: expected %d, got %s",
				limit, rec.Header().Get("X-RateLimit-Limit"))
		}
		if rec.Header().Get("X-RateLimit-Remaining") != fmt.Sprintf("%d", remaining) {
			t.Fatalf("X-RateLimit-Remaining mismatch: expected %d, got %s",
				remaining, rec.Header().Get("X-RateLimit-Remaining"))
		}
		if rec.Header().Get("X-RateLimit-Reset") != fmt.Sprintf("%d", resetUnix) {
			t.Fatalf("X-RateLimit-Reset mismatch: expected %d, got %s",
				resetUnix, rec.Header().Get("X-RateLimit-Reset"))
		}

		// Property: public endpoints do NOT have rate limit headers.
		publicPaths := []string{"/health", "/ready", "/metrics", "/playground"}
		pubIdx := rapid.IntRange(0, len(publicPaths)-1).Draw(t, "pubIdx")
		pubPath := publicPaths[pubIdx]

		pubReq := httptest.NewRequest(http.MethodGet, pubPath, nil)
		pubRec := httptest.NewRecorder()
		handler.ServeHTTP(pubRec, pubReq)

		if pubRec.Header().Get("X-RateLimit-Limit") != "" {
			t.Fatalf("public endpoint %s should NOT have X-RateLimit-Limit header", pubPath)
		}
	})
}

// TestProperty93_RateLimitKeyPriority validates that the rate limit key follows
// the priority: apikey:{id} > jwt:{sub} > ip:{addr}.
//
// Feature: graphql-multi-datasource-api, Property 93: 限流 Key 优先级
// **Validates: Design - 限流 Key 选择策略**
func TestProperty93_RateLimitKeyPriority(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		subject := rapid.StringMatching(`[a-zA-Z0-9_-]{3,20}`).Draw(t, "subject")
		clientIP := fmt.Sprintf("%d.%d.%d.%d",
			rapid.IntRange(1, 254).Draw(t, "ip1"),
			rapid.IntRange(0, 254).Draw(t, "ip2"),
			rapid.IntRange(0, 254).Draw(t, "ip3"),
			rapid.IntRange(1, 254).Draw(t, "ip4"),
		)

		limiter := &mockPropertyRateLimiter{
			result: &ratelimit.RateLimitResult{
				Allowed:   true,
				Limit:     100,
				Remaining: 99,
				ResetAt:   time.Now().Add(time.Minute),
			},
		}
		extractor := &mockPropertyIPExtractor{ip: clientIP}

		innerHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		handler := RateLimitMiddleware(limiter, extractor)(innerHandler)

		// Scenario 1: API Key identity → key should be "apikey:{subject}".
		{
			identity := &AuthIdentity{Subject: subject, Method: "apikey"}
			ctx := context.WithValue(context.Background(), ctxkeys.CtxKeyAuthIdentity, identity)
			req := httptest.NewRequest(http.MethodPost, "/graphql", nil).WithContext(ctx)
			req.RemoteAddr = clientIP + ":9999"
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			expectedKey := "apikey:" + subject
			if limiter.lastKey != expectedKey {
				t.Fatalf("apikey identity: expected key %q, got %q", expectedKey, limiter.lastKey)
			}
		}

		// Scenario 2: JWT identity → key should be "jwt:{subject}".
		{
			identity := &AuthIdentity{Subject: subject, Method: "jwt"}
			ctx := context.WithValue(context.Background(), ctxkeys.CtxKeyAuthIdentity, identity)
			req := httptest.NewRequest(http.MethodPost, "/graphql", nil).WithContext(ctx)
			req.RemoteAddr = clientIP + ":9999"
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			expectedKey := "jwt:" + subject
			if limiter.lastKey != expectedKey {
				t.Fatalf("jwt identity: expected key %q, got %q", expectedKey, limiter.lastKey)
			}
		}

		// Scenario 3: No identity → key should be "ip:{clientIP}".
		{
			req := httptest.NewRequest(http.MethodPost, "/graphql", nil)
			req.RemoteAddr = clientIP + ":9999"
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			expectedKey := "ip:" + clientIP
			if limiter.lastKey != expectedKey {
				t.Fatalf("no identity: expected key %q, got %q", expectedKey, limiter.lastKey)
			}
		}

		// Scenario 4: API Key takes priority over IP even when extractor is present.
		// (Already tested in scenario 1 with extractor set — the key is apikey:, not ip:)

		// Scenario 5: Nil identity in context → falls back to IP.
		{
			ctx := context.WithValue(context.Background(), ctxkeys.CtxKeyAuthIdentity, (*AuthIdentity)(nil))
			req := httptest.NewRequest(http.MethodPost, "/graphql", nil).WithContext(ctx)
			req.RemoteAddr = clientIP + ":9999"
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			expectedKey := "ip:" + clientIP
			if limiter.lastKey != expectedKey {
				t.Fatalf("nil identity: expected key %q, got %q", expectedKey, limiter.lastKey)
			}
		}
	})
}
