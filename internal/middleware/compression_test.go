package middleware

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/graphql-api/internal/config"
)

func TestCompression_Disabled_NoCompression(t *testing.T) {
	cfg := config.CompressionConfig{Enabled: false}
	body := bytes.Repeat([]byte("a"), 2048)

	handler := Compression(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))

	req := httptest.NewRequest(http.MethodPost, "/graphql", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("expected no Content-Encoding when disabled, got %q", got)
	}
	if !bytes.Equal(rec.Body.Bytes(), body) {
		t.Fatal("body should be uncompressed when disabled")
	}
}

func TestCompression_NoAcceptEncoding_NoCompression(t *testing.T) {
	cfg := config.CompressionConfig{Enabled: true, MinSize: "100B"}
	body := bytes.Repeat([]byte("x"), 200)

	handler := Compression(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))

	req := httptest.NewRequest(http.MethodPost, "/graphql", nil)
	// No Accept-Encoding header.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("expected no Content-Encoding without Accept-Encoding, got %q", got)
	}
	if !bytes.Equal(rec.Body.Bytes(), body) {
		t.Fatal("body should be uncompressed without Accept-Encoding")
	}
}

func TestCompression_BelowThreshold_NoCompression(t *testing.T) {
	cfg := config.CompressionConfig{Enabled: true, MinSize: "1KB"}
	body := []byte(`{"data":{"hello":"world"}}`) // well below 1KB

	handler := Compression(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))

	req := httptest.NewRequest(http.MethodPost, "/graphql", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("expected no Content-Encoding below threshold, got %q", got)
	}
	if !bytes.Equal(rec.Body.Bytes(), body) {
		t.Fatal("body should be uncompressed below threshold")
	}
}

func TestCompression_AboveThreshold_Compressed(t *testing.T) {
	cfg := config.CompressionConfig{Enabled: true, MinSize: "100B"}
	body := bytes.Repeat([]byte("z"), 500)

	handler := Compression(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))

	req := httptest.NewRequest(http.MethodPost, "/graphql", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("expected Content-Encoding gzip, got %q", got)
	}

	// Decompress and verify content.
	gr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("failed to create gzip reader: %v", err)
	}
	defer gr.Close()

	decompressed, err := io.ReadAll(gr)
	if err != nil {
		t.Fatalf("failed to decompress: %v", err)
	}
	if !bytes.Equal(decompressed, body) {
		t.Fatalf("decompressed body mismatch: got %d bytes, want %d", len(decompressed), len(body))
	}
}

func TestCompression_ExactThreshold_Compressed(t *testing.T) {
	cfg := config.CompressionConfig{Enabled: true, MinSize: "100B"}
	body := bytes.Repeat([]byte("a"), 100) // exactly at threshold

	handler := Compression(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))

	req := httptest.NewRequest(http.MethodPost, "/graphql", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("expected Content-Encoding gzip at exact threshold, got %q", got)
	}

	gr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("failed to create gzip reader: %v", err)
	}
	defer gr.Close()

	decompressed, err := io.ReadAll(gr)
	if err != nil {
		t.Fatalf("failed to decompress: %v", err)
	}
	if !bytes.Equal(decompressed, body) {
		t.Fatal("decompressed body mismatch at exact threshold")
	}
}

func TestCompression_PreservesStatusCode(t *testing.T) {
	cfg := config.CompressionConfig{Enabled: true, MinSize: "10B"}
	body := bytes.Repeat([]byte("x"), 100)

	handler := Compression(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(body)
	}))

	req := httptest.NewRequest(http.MethodPost, "/graphql", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("expected gzip, got %q", got)
	}
}

func TestCompression_InvalidMinSize_UsesDefault(t *testing.T) {
	cfg := config.CompressionConfig{Enabled: true, MinSize: "not-a-size"}
	// Default is 1KB. A body of 500 bytes should not be compressed.
	body := bytes.Repeat([]byte("a"), 500)

	handler := Compression(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))

	req := httptest.NewRequest(http.MethodPost, "/graphql", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("expected no compression with invalid min size (default 1KB), got %q", got)
	}
}

func TestCompression_AcceptEncodingWithMultipleValues(t *testing.T) {
	cfg := config.CompressionConfig{Enabled: true, MinSize: "10B"}
	body := bytes.Repeat([]byte("b"), 100)

	handler := Compression(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))

	req := httptest.NewRequest(http.MethodPost, "/graphql", nil)
	req.Header.Set("Accept-Encoding", "deflate, gzip, br")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("expected gzip with multi-value Accept-Encoding, got %q", got)
	}
}

func TestCompression_EmptyBody_NoCompression(t *testing.T) {
	cfg := config.CompressionConfig{Enabled: true, MinSize: "1B"}

	handler := Compression(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodPost, "/graphql", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("expected no Content-Encoding for empty body, got %q", got)
	}
}
