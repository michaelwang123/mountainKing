// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	ctxkeys "github.com/michaelwang123/mountainKing/internal/context"
	apierrors "github.com/michaelwang123/mountainKing/internal/errors"
	"github.com/michaelwang123/mountainKing/internal/ratelimit"
)

// mockRateLimiter is a test double for ratelimit.RateLimiter.
type mockRateLimiter struct {
	result  *ratelimit.RateLimitResult
	err     error
	lastKey string
	lastCnt int
}

func (m *mockRateLimiter) Allow(_ context.Context, key string, count int) (*ratelimit.RateLimitResult, error) {
	m.lastKey = key
	m.lastCnt = count
	return m.result, m.err
}

// mockIPExtractor is a test double for IPExtractor.
type mockIPExtractor struct {
	ip string
}

func (m *mockIPExtractor) ExtractClientIP(_ *http.Request) string {
	return m.ip
}

func allowedResult() *ratelimit.RateLimitResult {
	return &ratelimit.RateLimitResult{
		Allowed:   true,
		Limit:     100,
		Remaining: 99,
		ResetAt:   time.Unix(1700000000, 0),
	}
}

func deniedResult() *ratelimit.RateLimitResult {
	return &ratelimit.RateLimitResult{
		Allowed:   false,
		Limit:     100,
		Remaining: 0,
		ResetAt:   time.Unix(1700000000, 0),
	}
}

func rlOKHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestRateLimitMiddleware_AllowedRequest(t *testing.T) {
	limiter := &mockRateLimiter{result: allowedResult()}
	handler := RateLimitMiddleware(limiter, nil)(rlOKHandler())

	req := httptest.NewRequest(http.MethodPost, "/graphql", nil)
	req.RemoteAddr = "1.2.3.4:5678"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Header().Get("X-RateLimit-Limit") != "100" {
		t.Fatalf("expected X-RateLimit-Limit=100, got %q", rec.Header().Get("X-RateLimit-Limit"))
	}
	if rec.Header().Get("X-RateLimit-Remaining") != "99" {
		t.Fatalf("expected X-RateLimit-Remaining=99, got %q", rec.Header().Get("X-RateLimit-Remaining"))
	}
	if rec.Header().Get("X-RateLimit-Reset") != strconv.FormatInt(1700000000, 10) {
		t.Fatalf("expected X-RateLimit-Reset=1700000000, got %q", rec.Header().Get("X-RateLimit-Reset"))
	}
}

func TestRateLimitMiddleware_DeniedRequest(t *testing.T) {
	limiter := &mockRateLimiter{result: deniedResult()}
	handler := RateLimitMiddleware(limiter, nil)(rlOKHandler())

	req := httptest.NewRequest(http.MethodPost, "/graphql", nil)
	req.RemoteAddr = "1.2.3.4:5678"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rec.Code)
	}

	// Verify rate limit headers are still set.
	if rec.Header().Get("X-RateLimit-Limit") != "100" {
		t.Fatalf("expected X-RateLimit-Limit=100, got %q", rec.Header().Get("X-RateLimit-Limit"))
	}
	if rec.Header().Get("X-RateLimit-Remaining") != "0" {
		t.Fatalf("expected X-RateLimit-Remaining=0, got %q", rec.Header().Get("X-RateLimit-Remaining"))
	}

	// Verify JSON error body.
	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatal("expected 'error' object in response")
	}
	if errObj["code"] != apierrors.ErrRateLimitExceeded {
		t.Fatalf("expected code %q, got %v", apierrors.ErrRateLimitExceeded, errObj["code"])
	}
	if errObj["classification"] != "RATELIMIT" {
		t.Fatalf("expected classification RATELIMIT, got %v", errObj["classification"])
	}
}

func TestRateLimitMiddleware_PublicEndpointsExempt(t *testing.T) {
	paths := []string{"/health", "/ready", "/metrics", "/playground"}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			limiter := &mockRateLimiter{result: deniedResult()}
			handler := RateLimitMiddleware(limiter, nil)(rlOKHandler())

			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200 for public endpoint %s, got %d", path, rec.Code)
			}
			// Public endpoints should NOT have rate limit headers.
			if rec.Header().Get("X-RateLimit-Limit") != "" {
				t.Fatalf("expected no X-RateLimit-Limit header for %s", path)
			}
		})
	}
}

func TestRateLimitMiddleware_KeyPriority_APIKey(t *testing.T) {
	limiter := &mockRateLimiter{result: allowedResult()}
	handler := RateLimitMiddleware(limiter, nil)(rlOKHandler())

	identity := &AuthIdentity{Subject: "key-123", Method: "apikey"}
	ctx := context.WithValue(context.Background(), ctxkeys.CtxKeyAuthIdentity, identity)
	req := httptest.NewRequest(http.MethodPost, "/graphql", nil).WithContext(ctx)
	req.RemoteAddr = "1.2.3.4:5678"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if limiter.lastKey != "apikey:key-123" {
		t.Fatalf("expected key 'apikey:key-123', got %q", limiter.lastKey)
	}
}

func TestRateLimitMiddleware_KeyPriority_JWT(t *testing.T) {
	limiter := &mockRateLimiter{result: allowedResult()}
	handler := RateLimitMiddleware(limiter, nil)(rlOKHandler())

	identity := &AuthIdentity{Subject: "user-456", Method: "jwt"}
	ctx := context.WithValue(context.Background(), ctxkeys.CtxKeyAuthIdentity, identity)
	req := httptest.NewRequest(http.MethodPost, "/graphql", nil).WithContext(ctx)
	req.RemoteAddr = "1.2.3.4:5678"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if limiter.lastKey != "jwt:user-456" {
		t.Fatalf("expected key 'jwt:user-456', got %q", limiter.lastKey)
	}
}

func TestRateLimitMiddleware_KeyPriority_IP_WithExtractor(t *testing.T) {
	limiter := &mockRateLimiter{result: allowedResult()}
	extractor := &mockIPExtractor{ip: "10.0.0.1"}
	handler := RateLimitMiddleware(limiter, extractor)(rlOKHandler())

	req := httptest.NewRequest(http.MethodPost, "/graphql", nil)
	req.RemoteAddr = "192.168.1.1:9999"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if limiter.lastKey != "ip:10.0.0.1" {
		t.Fatalf("expected key 'ip:10.0.0.1', got %q", limiter.lastKey)
	}
}

func TestRateLimitMiddleware_KeyPriority_IP_NoExtractor(t *testing.T) {
	limiter := &mockRateLimiter{result: allowedResult()}
	handler := RateLimitMiddleware(limiter, nil)(rlOKHandler())

	req := httptest.NewRequest(http.MethodPost, "/graphql", nil)
	req.RemoteAddr = "192.168.1.1:9999"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if limiter.lastKey != "ip:192.168.1.1" {
		t.Fatalf("expected key 'ip:192.168.1.1', got %q", limiter.lastKey)
	}
}

func TestRateLimitMiddleware_BatchCount(t *testing.T) {
	limiter := &mockRateLimiter{result: allowedResult()}
	handler := RateLimitMiddleware(limiter, nil)(rlOKHandler())

	ctx := context.WithValue(context.Background(), ctxkeys.CtxKeyBatchQueryCount, 5)
	req := httptest.NewRequest(http.MethodPost, "/graphql", nil).WithContext(ctx)
	req.RemoteAddr = "1.2.3.4:5678"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if limiter.lastCnt != 5 {
		t.Fatalf("expected count 5, got %d", limiter.lastCnt)
	}
}

func TestRateLimitMiddleware_BatchCount_Default(t *testing.T) {
	limiter := &mockRateLimiter{result: allowedResult()}
	handler := RateLimitMiddleware(limiter, nil)(rlOKHandler())

	req := httptest.NewRequest(http.MethodPost, "/graphql", nil)
	req.RemoteAddr = "1.2.3.4:5678"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if limiter.lastCnt != 1 {
		t.Fatalf("expected default count 1, got %d", limiter.lastCnt)
	}
}

func TestRateLimitMiddleware_IncludesRequestID(t *testing.T) {
	limiter := &mockRateLimiter{result: deniedResult()}
	handler := RateLimitMiddleware(limiter, nil)(rlOKHandler())

	ctx := context.WithValue(context.Background(), ctxkeys.CtxKeyRequestID, "req-abc")
	req := httptest.NewRequest(http.MethodPost, "/graphql", nil).WithContext(ctx)
	req.RemoteAddr = "1.2.3.4:5678"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["requestId"] != "req-abc" {
		t.Fatalf("expected requestId 'req-abc', got %v", resp["requestId"])
	}
}

func TestRateLimitMiddleware_NoRequestID_OmitsField(t *testing.T) {
	limiter := &mockRateLimiter{result: deniedResult()}
	handler := RateLimitMiddleware(limiter, nil)(rlOKHandler())

	req := httptest.NewRequest(http.MethodPost, "/graphql", nil)
	req.RemoteAddr = "1.2.3.4:5678"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if _, exists := resp["requestId"]; exists {
		t.Fatal("expected requestId to be absent")
	}
}

func TestRateLimitMiddleware_LimiterError_AllowsThrough(t *testing.T) {
	limiter := &mockRateLimiter{
		result: nil,
		err:    context.DeadlineExceeded,
	}
	handler := RateLimitMiddleware(limiter, nil)(rlOKHandler())

	req := httptest.NewRequest(http.MethodPost, "/graphql", nil)
	req.RemoteAddr = "1.2.3.4:5678"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on limiter error, got %d", rec.Code)
	}
	// No rate limit headers when limiter errors.
	if rec.Header().Get("X-RateLimit-Limit") != "" {
		t.Fatal("expected no rate limit headers on limiter error")
	}
}

func TestRateLimitMiddleware_ContentTypeJSON(t *testing.T) {
	limiter := &mockRateLimiter{result: deniedResult()}
	handler := RateLimitMiddleware(limiter, nil)(rlOKHandler())

	req := httptest.NewRequest(http.MethodPost, "/graphql", nil)
	req.RemoteAddr = "1.2.3.4:5678"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %q", ct)
	}
}
