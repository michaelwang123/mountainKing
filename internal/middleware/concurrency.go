// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package middleware

import (
	"encoding/json"
	"net/http"

	apierrors "github.com/michaelwang123/mountainKing/internal/errors"
)

// MaxConcurrentRequests returns a middleware that limits the number of
// in-flight requests. When the limit is reached, new requests receive
// a 503 Service Unavailable response. If maxConcurrent <= 0, the
// middleware is a no-op (unlimited).
//
// This provides global backpressure at the HTTP layer, preventing
// goroutine and memory accumulation when downstream systems (DB,
// cache) are saturated. It complements per-client rate limiting
// which does not protect against aggregate traffic from many clients.
func MaxConcurrentRequests(maxConcurrent int) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if maxConcurrent <= 0 {
			return next
		}

		sem := make(chan struct{}, maxConcurrent)

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Public endpoints are exempt from concurrency limiting.
			if isPublicEndpoint(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
				next.ServeHTTP(w, r)
			default:
				writeServiceUnavailable(w)
			}
		})
	}
}

// writeServiceUnavailable writes a 503 JSON error response.
func writeServiceUnavailable(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Retry-After", "5")
	w.WriteHeader(http.StatusServiceUnavailable)

	resp := map[string]any{
		"errors": []map[string]any{
			{
				"message": "server is at capacity, please retry later",
				"extensions": map[string]any{
					"code":           apierrors.ErrServiceUnavailable,
					"classification": "INTERNAL",
				},
			},
		},
	}
	_ = json.NewEncoder(w).Encode(resp)
}
