// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package middleware

import (
	"net/http"
	"strings"

	"github.com/michaelwang123/mountainKing/internal/config"
)

// defaultMaxAge is the default preflight cache duration in seconds.
const defaultMaxAge = "86400"

// CORS returns a chi-compatible middleware that handles Cross-Origin Resource
// Sharing based on the provided CORSConfig. When CORS is disabled
// (cfg.Enabled == false), a no-op middleware is returned that passes requests
// through without modification.
//
// When enabled, the middleware:
//   - Responds to preflight OPTIONS requests with appropriate Access-Control-Allow-* headers
//   - Sets Access-Control-Allow-Origin on simple/actual requests when the Origin matches
//   - Validates the request Origin against the configured AllowedOrigins list
func CORS(cfg config.CORSConfig) func(http.Handler) http.Handler {
	if !cfg.Enabled {
		return func(next http.Handler) http.Handler {
			return next
		}
	}

	// Pre-compute joined strings for methods and headers.
	methods := strings.Join(cfg.AllowedMethods, ", ")
	headers := strings.Join(cfg.AllowedHeaders, ", ")

	// Build a set for fast origin lookup.
	allowAll := false
	originSet := make(map[string]struct{}, len(cfg.AllowedOrigins))
	for _, o := range cfg.AllowedOrigins {
		if o == "*" {
			allowAll = true
		}
		originSet[o] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			// No Origin header â€?not a CORS request, pass through.
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}

			// Check if the origin is allowed.
			allowed := allowAll
			if !allowed {
				_, allowed = originSet[origin]
			}

			if !allowed {
				// Origin not in the allowed list â€?pass through without CORS headers.
				next.ServeHTTP(w, r)
				return
			}

			// Set the allowed origin. Use the actual origin rather than "*"
			// to be compatible with credentialed requests.
			if allowAll {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			} else {
				w.Header().Set("Access-Control-Allow-Origin", origin)
			}

			// Handle preflight OPTIONS request.
			if r.Method == http.MethodOptions {
				w.Header().Set("Access-Control-Allow-Methods", methods)
				w.Header().Set("Access-Control-Allow-Headers", headers)
				w.Header().Set("Access-Control-Max-Age", defaultMaxAge)
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
