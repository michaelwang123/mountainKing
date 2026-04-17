// Package middleware provides HTTP middleware components for the GraphQL API service.
package middleware

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"

	ctxkeys "github.com/example/graphql-api/internal/context"
)

const (
	// HeaderXRequestID is the HTTP header name for the request ID.
	HeaderXRequestID = "X-Request-ID"
)

// generateUUIDv4 generates a UUID v4 string using crypto/rand.
func generateUUIDv4() (string, error) {
	var uuid [16]byte
	if _, err := rand.Read(uuid[:]); err != nil {
		return "", fmt.Errorf("failed to generate UUID: %w", err)
	}
	// Set version 4 (bits 12-15 of time_hi_and_version)
	uuid[6] = (uuid[6] & 0x0f) | 0x40
	// Set variant bits (bits 6-7 of clock_seq_hi_and_reserved)
	uuid[8] = (uuid[8] & 0x3f) | 0x80

	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		uuid[0:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:16]), nil
}

// RequestID returns a chi-compatible middleware that assigns a unique request ID
// to each incoming request. If the request already carries an X-Request-ID header,
// that value is reused; otherwise a new UUID v4 is generated.
//
// The request ID is stored in the request context under CtxKeyRequestID and
// written to the X-Request-ID response header.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get(HeaderXRequestID)
		if requestID == "" {
			id, err := generateUUIDv4()
			if err != nil {
				// Fallback: proceed without a request ID rather than failing the request.
				next.ServeHTTP(w, r)
				return
			}
			requestID = id
		}

		// Set the response header.
		w.Header().Set(HeaderXRequestID, requestID)

		// Inject into context.
		ctx := context.WithValue(r.Context(), ctxkeys.CtxKeyRequestID, requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
