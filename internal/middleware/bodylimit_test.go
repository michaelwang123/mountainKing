package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	ctxkeys "github.com/example/graphql-api/internal/context"
	apierrors "github.com/example/graphql-api/internal/errors"
)

func TestParseSizeString(t *testing.T) {
	tests := []struct {
		input   string
		want    int64
		wantErr bool
	}{
		{"1MB", 1 << 20, false},
		{"1mb", 1 << 20, false},
		{"1Mb", 1 << 20, false},
		{"512KB", 512 * (1 << 10), false},
		{"2GB", 2 * (1 << 30), false},
		{"1TB", 1 << 40, false},
		{"100B", 100, false},
		{"1048576", 1048576, false},
		{"0MB", 0, false},
		{"0", 0, false},
		{"1.5MB", int64(1.5 * float64(1<<20)), false},
		{"", 0, true},
		{"-1MB", 0, true},
		{"-100", 0, true},
		{"abc", 0, true},
		{"MB", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseSizeString(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseSizeString(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Fatalf("ParseSizeString(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestBodyLimit_AllowsSmallBody(t *testing.T) {
	var bodyRead []byte
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("unexpected read error: %v", err)
		}
		bodyRead = b
		w.WriteHeader(http.StatusOK)
	})

	handler := BodyLimit("1MB")(inner)

	body := bytes.Repeat([]byte("a"), 100)
	req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !bytes.Equal(bodyRead, body) {
		t.Fatalf("body mismatch: got %d bytes, want %d", len(bodyRead), len(body))
	}
}

func TestBodyLimit_RejectsOversizedBody(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		if err != nil {
			// Body exceeded limit — write the 413 error response.
			WriteBodyLimitError(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	handler := BodyLimit("100B")(inner)

	// Send 200 bytes, exceeding the 100-byte limit.
	body := bytes.Repeat([]byte("x"), 200)
	req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d", rec.Code)
	}

	// Verify JSON error response structure.
	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatal("expected 'error' object in response")
	}
	if errObj["code"] != apierrors.ErrValidationPayloadTooLarge {
		t.Fatalf("expected code %q, got %v", apierrors.ErrValidationPayloadTooLarge, errObj["code"])
	}
	if errObj["classification"] != "VALIDATION" {
		t.Fatalf("expected classification VALIDATION, got %v", errObj["classification"])
	}
}

func TestBodyLimit_IncludesRequestID(t *testing.T) {
	const testRequestID = "test-req-id-456"

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		if err != nil {
			WriteBodyLimitError(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	handler := BodyLimit("50B")(inner)

	body := bytes.Repeat([]byte("z"), 100)
	req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
	// Inject request ID into context (as RequestID middleware would).
	ctx := context.WithValue(req.Context(), ctxkeys.CtxKeyRequestID, testRequestID)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d", rec.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["requestId"] != testRequestID {
		t.Fatalf("expected requestId %q, got %v", testRequestID, resp["requestId"])
	}
}

func TestBodyLimit_NoRequestID_OmitsField(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		if err != nil {
			WriteBodyLimitError(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	handler := BodyLimit("50B")(inner)

	body := bytes.Repeat([]byte("z"), 100)
	req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
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

func TestBodyLimit_DefaultSize(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		if err != nil {
			WriteBodyLimitError(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	// Empty string should default to 1MB.
	handler := BodyLimit("")(inner)

	// A small body should pass.
	req := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(`{"query":"{ __typename }"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for small body with default limit, got %d", rec.Code)
	}
}

func TestBodyLimit_InvalidSizeString_UsesDefault(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		if err != nil {
			WriteBodyLimitError(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	// Invalid size string should fall back to default 1MB.
	handler := BodyLimit("not-a-size")(inner)

	req := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(`{"query":"{ __typename }"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for small body with fallback default, got %d", rec.Code)
	}
}

func TestBodyLimit_ExactSizeBoundary(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		if err != nil {
			WriteBodyLimitError(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	// Limit is exactly 100 bytes.
	handler := BodyLimit("100B")(inner)

	// Body of exactly 100 bytes should pass.
	body := bytes.Repeat([]byte("a"), 100)
	req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for body at exact limit, got %d", rec.Code)
	}

	// Body of 101 bytes should fail.
	body2 := bytes.Repeat([]byte("a"), 101)
	req2 := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body2))
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 for body exceeding limit by 1 byte, got %d", rec2.Code)
	}
}

func TestBodyLimit_ContentTypeJSON(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		if err != nil {
			WriteBodyLimitError(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	handler := BodyLimit("50B")(inner)

	body := bytes.Repeat([]byte("x"), 100)
	req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %q", ct)
	}
}
