package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	ctxkeys "github.com/example/graphql-api/internal/context"
)

// echoHandler is a simple handler that echoes back the request body as a GraphQL-like
// JSON response. It also records the batch query count from context if present.
func echoHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		defer r.Body.Close()

		var req map[string]any
		if err := json.Unmarshal(body, &req); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]any{
				"errors": []map[string]any{{"message": "bad json"}},
			})
			return
		}

		query, _ := req["query"].(string)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"echo": query},
		})
	}
}

func TestBatchHandler_SingleQueryPassthrough(t *testing.T) {
	bh := NewBatchHandler(echoHandler(), 10)
	ts := httptest.NewServer(bh)
	defer ts.Close()

	body := `{"query":"{ __typename }"}`
	resp, err := http.Post(ts.URL, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	data, ok := result["data"].(map[string]any)
	if !ok {
		t.Fatal("expected 'data' field in response")
	}
	if data["echo"] != "{ __typename }" {
		t.Errorf("expected echo of query, got %v", data["echo"])
	}
}

func TestBatchHandler_ValidBatch(t *testing.T) {
	bh := NewBatchHandler(echoHandler(), 10)
	ts := httptest.NewServer(bh)
	defer ts.Close()

	body := `[{"query":"query1"},{"query":"query2"},{"query":"query3"}]`
	resp, err := http.Post(ts.URL, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var results []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// Verify results are in order.
	for i, r := range results {
		data, ok := r["data"].(map[string]any)
		if !ok {
			t.Errorf("result[%d]: expected 'data' field", i)
			continue
		}
		expected := "query" + string(rune('1'+i))
		if data["echo"] != expected {
			t.Errorf("result[%d]: expected echo=%q, got %v", i, expected, data["echo"])
		}
	}
}

func TestBatchHandler_ExceedsMaxLimit(t *testing.T) {
	bh := NewBatchHandler(echoHandler(), 2)
	ts := httptest.NewServer(bh)
	defer ts.Close()

	body := `[{"query":"q1"},{"query":"q2"},{"query":"q3"}]`
	resp, err := http.Post(ts.URL, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	errs, ok := result["errors"].([]any)
	if !ok || len(errs) == 0 {
		t.Fatal("expected errors in response")
	}

	errObj, ok := errs[0].(map[string]any)
	if !ok {
		t.Fatal("expected error object")
	}

	ext, ok := errObj["extensions"].(map[string]any)
	if !ok {
		t.Fatal("expected extensions in error")
	}
	if ext["code"] != "VALIDATION_BATCH_LIMIT_EXCEEDED" {
		t.Errorf("expected VALIDATION_BATCH_LIMIT_EXCEEDED, got %v", ext["code"])
	}
}

func TestBatchHandler_EmptyBatchArray(t *testing.T) {
	bh := NewBatchHandler(echoHandler(), 10)
	ts := httptest.NewServer(bh)
	defer ts.Close()

	body := `[]`
	resp, err := http.Post(ts.URL, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var results []any
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(results) != 0 {
		t.Errorf("expected empty array, got %d elements", len(results))
	}
}

func TestBatchHandler_InvalidJSON(t *testing.T) {
	bh := NewBatchHandler(echoHandler(), 10)
	ts := httptest.NewServer(bh)
	defer ts.Close()

	body := `not valid json at all`
	resp, err := http.Post(ts.URL, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	// Non-array body is treated as single query and forwarded to the handler.
	// The echo handler will return 400 for invalid JSON.
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestBatchHandler_InvalidJSONArray(t *testing.T) {
	bh := NewBatchHandler(echoHandler(), 10)
	ts := httptest.NewServer(bh)
	defer ts.Close()

	// Starts with '[' but is not valid JSON.
	body := `[invalid json`
	resp, err := http.Post(ts.URL, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestBatchHandler_ContextBatchCount_Single(t *testing.T) {
	var capturedCount int
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if v, ok := r.Context().Value(ctxkeys.CtxKeyBatchQueryCount).(int); ok {
			capturedCount = v
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"data":{}}`))
	})

	bh := NewBatchHandler(handler, 10)
	req := httptest.NewRequest("POST", "/graphql", strings.NewReader(`{"query":"{ __typename }"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	bh.ServeHTTP(w, req)

	if capturedCount != 1 {
		t.Errorf("expected batch count 1 for single query, got %d", capturedCount)
	}
}

func TestBatchHandler_ContextBatchCount_Batch(t *testing.T) {
	var capturedCounts []int
	var mu = &sync.Mutex{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if v, ok := r.Context().Value(ctxkeys.CtxKeyBatchQueryCount).(int); ok {
			mu.Lock()
			capturedCounts = append(capturedCounts, v)
			mu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"data":{}}`))
	})

	bh := NewBatchHandler(handler, 10)
	req := httptest.NewRequest("POST", "/graphql", strings.NewReader(`[{"query":"q1"},{"query":"q2"}]`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	bh.ServeHTTP(w, req)

	if len(capturedCounts) != 2 {
		t.Fatalf("expected 2 handler calls, got %d", len(capturedCounts))
	}
	for i, c := range capturedCounts {
		if c != 2 {
			t.Errorf("handler call %d: expected batch count 2, got %d", i, c)
		}
	}
}

func TestBatchHandler_DefaultMaxBatchQueries(t *testing.T) {
	// Passing 0 should default to 10.
	bh := NewBatchHandler(echoHandler(), 0)
	if bh.maxBatchQueries != 10 {
		t.Errorf("expected default maxBatchQueries=10, got %d", bh.maxBatchQueries)
	}
}

func TestIsBatchRequest(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		expected bool
	}{
		{"json object", `{"query":"test"}`, false},
		{"json array", `[{"query":"test"}]`, false},
		{"json array with leading space", `  [{"query":"test"}]`, false},
		{"empty string", ``, false},
		{"just whitespace", `   `, false},
		{"number", `123`, false},
		{"string", `"hello"`, false},
	}

	// isBatchRequest is tested indirectly through the handler tests above.
	// These are direct unit tests for the helper.
	tests[1].expected = true
	tests[2].expected = true

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isBatchRequest([]byte(tt.body))
			if got != tt.expected {
				t.Errorf("isBatchRequest(%q) = %v, want %v", tt.body, got, tt.expected)
			}
		})
	}
}
