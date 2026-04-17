package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	ctxkeys "github.com/example/graphql-api/internal/context"
)

// okHandler is a simple handler that returns 200 OK.
var okHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
})

func TestCSRF_ProductionMode_BlocksGET(t *testing.T) {
	handler := CSRFProtection(false, "production")(okHandler)

	req := httptest.NewRequest(http.MethodGet, "/graphql", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatal("expected 'error' object in response")
	}
	if errObj["code"] != "VALIDATION_GET_QUERY_DISABLED" {
		t.Fatalf("expected code VALIDATION_GET_QUERY_DISABLED, got %v", errObj["code"])
	}
	if errObj["classification"] != "VALIDATION" {
		t.Fatalf("expected classification VALIDATION, got %v", errObj["classification"])
	}
}

func TestCSRF_ProductionMode_AllowsGET_WhenEnabled(t *testing.T) {
	handler := CSRFProtection(true, "production")(okHandler)

	req := httptest.NewRequest(http.MethodGet, "/graphql", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestCSRF_DevelopmentMode_AllowsGET(t *testing.T) {
	handler := CSRFProtection(false, "development")(okHandler)

	req := httptest.NewRequest(http.MethodGet, "/graphql", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestCSRF_POST_ValidContentType(t *testing.T) {
	handler := CSRFProtection(false, "production")(okHandler)

	req := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(`{"query":"{ __typename }"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestCSRF_POST_ContentTypeWithCharset(t *testing.T) {
	handler := CSRFProtection(false, "production")(okHandler)

	req := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(`{"query":"{ __typename }"}`))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestCSRF_POST_InvalidContentType(t *testing.T) {
	handler := CSRFProtection(false, "production")(okHandler)

	req := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader("query=%7B+__typename+%7D"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected 415, got %d", rec.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatal("expected 'error' object in response")
	}
	if errObj["code"] != "VALIDATION_INVALID_CONTENT_TYPE" {
		t.Fatalf("expected code VALIDATION_INVALID_CONTENT_TYPE, got %v", errObj["code"])
	}
}

func TestCSRF_POST_MissingContentType(t *testing.T) {
	handler := CSRFProtection(false, "production")(okHandler)

	req := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(`{"query":"{ __typename }"}`))
	// No Content-Type header set.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected 415, got %d", rec.Code)
	}
}

func TestCSRF_NonGraphQLPath_PassThrough(t *testing.T) {
	handler := CSRFProtection(false, "production")(okHandler)

	paths := []string{"/health", "/ready", "/metrics", "/playground"}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200 for %s, got %d", path, rec.Code)
			}
		})
	}
}

func TestCSRF_GraphQLTrailingSlash(t *testing.T) {
	handler := CSRFProtection(false, "production")(okHandler)

	req := httptest.NewRequest(http.MethodGet, "/graphql/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for /graphql/ GET in production, got %d", rec.Code)
	}
}

func TestCSRF_IncludesRequestID(t *testing.T) {
	const testRequestID = "csrf-test-req-id"
	handler := CSRFProtection(false, "production")(okHandler)

	req := httptest.NewRequest(http.MethodGet, "/graphql", nil)
	ctx := context.WithValue(req.Context(), ctxkeys.CtxKeyRequestID, testRequestID)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["requestId"] != testRequestID {
		t.Fatalf("expected requestId %q, got %v", testRequestID, resp["requestId"])
	}
}

func TestCSRF_NoRequestID_OmitsField(t *testing.T) {
	handler := CSRFProtection(false, "production")(okHandler)

	req := httptest.NewRequest(http.MethodGet, "/graphql", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if _, exists := resp["requestId"]; exists {
		t.Fatal("expected requestId to be absent when no request ID in context")
	}
}

func TestCSRF_ContentTypeJSON_ResponseHeader(t *testing.T) {
	handler := CSRFProtection(false, "production")(okHandler)

	req := httptest.NewRequest(http.MethodGet, "/graphql", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %q", ct)
	}
}
