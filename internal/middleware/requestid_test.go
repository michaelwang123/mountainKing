// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package middleware

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	ctxkeys "github.com/michaelwang123/mountainKing/internal/context"
)

// uuidV4Regex matches a standard UUID v4 string.
var uuidV4Regex = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestRequestID_GeneratesNewID(t *testing.T) {
	var ctxRequestID string
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		val, _ := r.Context().Value(ctxkeys.CtxKeyRequestID).(string)
		ctxRequestID = val
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Response header should contain a UUID v4.
	headerVal := rec.Header().Get(HeaderXRequestID)
	if headerVal == "" {
		t.Fatal("expected X-Request-ID response header to be set")
	}
	if !uuidV4Regex.MatchString(headerVal) {
		t.Fatalf("expected UUID v4 format, got %q", headerVal)
	}

	// Context value should match the header.
	if ctxRequestID != headerVal {
		t.Fatalf("context request ID %q does not match header %q", ctxRequestID, headerVal)
	}
}

func TestRequestID_ReusesIncomingHeader(t *testing.T) {
	const existingID = "my-custom-request-id-123"

	var ctxRequestID string
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		val, _ := r.Context().Value(ctxkeys.CtxKeyRequestID).(string)
		ctxRequestID = val
	}))

	req := httptest.NewRequest(http.MethodPost, "/graphql", nil)
	req.Header.Set(HeaderXRequestID, existingID)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Should reuse the incoming header value.
	if got := rec.Header().Get(HeaderXRequestID); got != existingID {
		t.Fatalf("expected response header %q, got %q", existingID, got)
	}
	if ctxRequestID != existingID {
		t.Fatalf("expected context value %q, got %q", existingID, ctxRequestID)
	}
}

func TestRequestID_UniqueAcrossRequests(t *testing.T) {
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	seen := make(map[string]struct{})
	for i := 0; i < 100; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		id := rec.Header().Get(HeaderXRequestID)
		if _, exists := seen[id]; exists {
			t.Fatalf("duplicate request ID generated: %s", id)
		}
		seen[id] = struct{}{}
	}
}

func TestGenerateUUIDv4_Format(t *testing.T) {
	for i := 0; i < 50; i++ {
		id, err := generateUUIDv4()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !uuidV4Regex.MatchString(id) {
			t.Fatalf("expected UUID v4 format, got %q", id)
		}
	}
}
