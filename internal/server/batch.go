// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

// Package server provides the HTTP server for the GraphQL API service.
package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"

	ctxkeys "github.com/michaelwang123/mountainKing/internal/context"
)

// BatchHandler wraps a gqlgen HTTP handler to support GraphQL query batching.
// When the request body is a JSON array, it parses each element as an individual
// GraphQL request, enforces the max_batch_queries limit, executes all queries
// in parallel, and returns a JSON array of results in the same order.
// Single (non-array) requests are forwarded to the underlying handler as-is.
type BatchHandler struct {
	// next is the underlying gqlgen HTTP handler.
	next http.Handler
	// maxBatchQueries is the maximum number of queries allowed in a batch.
	// If <= 0, defaults to 10.
	maxBatchQueries int
}

// NewBatchHandler creates a BatchHandler that wraps the given handler.
func NewBatchHandler(next http.Handler, maxBatchQueries int) *BatchHandler {
	if maxBatchQueries <= 0 {
		maxBatchQueries = 10
	}
	return &BatchHandler{
		next:            next,
		maxBatchQueries: maxBatchQueries,
	}
}

// ServeHTTP implements http.Handler. It reads the request body, detects whether
// it is a JSON array (batch) or a single object, and dispatches accordingly.
func (bh *BatchHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "VALIDATION_INVALID_BODY", "failed to read request body")
		return
	}
	defer r.Body.Close()

	if !isBatchRequest(body) {
		// Single query: store batch count = 1 in context and forward.
		ctx := context.WithValue(r.Context(), ctxkeys.CtxKeyBatchQueryCount, 1)
		r2 := r.WithContext(ctx)
		r2.Body = io.NopCloser(bytes.NewReader(body))
		r2.ContentLength = int64(len(body))
		bh.next.ServeHTTP(w, r2)
		return
	}

	// Batch request: parse the JSON array.
	var rawQueries []json.RawMessage
	if err := json.Unmarshal(body, &rawQueries); err != nil {
		writeJSONError(w, http.StatusBadRequest, "VALIDATION_INVALID_BODY", "invalid JSON array in batch request")
		return
	}

	count := len(rawQueries)

	// Empty batch.
	if count == 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("[]"))
		return
	}

	// Enforce max batch queries limit.
	if count > bh.maxBatchQueries {
		writeJSONError(w, http.StatusBadRequest, "VALIDATION_BATCH_LIMIT_EXCEEDED",
			fmt.Sprintf("batch contains %d queries, exceeds maximum of %d", count, bh.maxBatchQueries))
		return
	}

	// Store batch query count in context for rate limiter.
	ctx := context.WithValue(r.Context(), ctxkeys.CtxKeyBatchQueryCount, count)

	// Execute all queries in parallel, collecting results in order.
	results := make([]json.RawMessage, count)
	var wg sync.WaitGroup
	wg.Add(count)

	for i, rawQuery := range rawQueries {
		go func(idx int, queryBody json.RawMessage) {
			defer wg.Done()
			results[idx] = bh.executeOne(ctx, r, queryBody)
		}(i, rawQuery)
	}

	wg.Wait()

	// Return JSON array of results.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(results)
}

// executeOne runs a single GraphQL query through the underlying handler and
// returns the raw JSON response body.
func (bh *BatchHandler) executeOne(ctx context.Context, original *http.Request, queryBody json.RawMessage) json.RawMessage {
	// Build a synthetic request for this single query.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, original.URL.String(), bytes.NewReader(queryBody))
	if err != nil {
		return errorJSON("INTERNAL_UNEXPECTED", "failed to create sub-request")
	}
	// Copy relevant headers from the original request.
	req.Header = original.Header.Clone()
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = int64(len(queryBody))

	rec := httptest.NewRecorder()
	bh.next.ServeHTTP(rec, req)

	return rec.Body.Bytes()
}

// isBatchRequest checks if the body starts with '[' (ignoring leading whitespace),
// indicating a JSON array (batch request).
func isBatchRequest(body []byte) bool {
	for _, b := range body {
		switch b {
		case ' ', '\t', '\n', '\r':
			continue
		case '[':
			return true
		default:
			return false
		}
	}
	return false
}

// writeJSONError writes a structured JSON error response.
func writeJSONError(w http.ResponseWriter, statusCode int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	resp := map[string]any{
		"errors": []map[string]any{
			{
				"message": message,
				"extensions": map[string]any{
					"code":           code,
					"classification": "VALIDATION",
				},
			},
		},
	}
	_ = json.NewEncoder(w).Encode(resp)
}

// errorJSON returns a JSON-encoded error response as raw bytes.
func errorJSON(code, message string) json.RawMessage {
	resp := map[string]any{
		"errors": []map[string]any{
			{
				"message": message,
				"extensions": map[string]any{
					"code":           code,
					"classification": "INTERNAL",
				},
			},
		},
	}
	b, _ := json.Marshal(resp)
	return b
}
