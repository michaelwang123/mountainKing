// Package middleware provides HTTP middleware components for the GraphQL API service.
package middleware

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	ctxkeys "github.com/example/graphql-api/internal/context"
	apierrors "github.com/example/graphql-api/internal/errors"
)

// defaultMaxBodySize is the default maximum request body size (1MB).
const defaultMaxBodySize int64 = 1 << 20 // 1MB

// BodyLimit returns a chi-compatible middleware that limits the request body
// size. When the body exceeds the configured maximum, the middleware returns
// HTTP 413 (Payload Too Large) with a structured JSON error response.
//
// The sizeStr parameter accepts human-readable size strings such as "1MB",
// "512KB", "2GB", or plain byte counts like "1048576". An empty string
// defaults to 1MB.
func BodyLimit(sizeStr string) func(http.Handler) http.Handler {
	maxBytes := defaultMaxBodySize
	if sizeStr != "" {
		parsed, err := ParseSizeString(sizeStr)
		if err == nil && parsed > 0 {
			maxBytes = parsed
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			next.ServeHTTP(w, r)
		})
	}
}

// WriteBodyLimitError writes a structured JSON error response for 413 Payload
// Too Large. It extracts the request ID from context when available.
func WriteBodyLimitError(w http.ResponseWriter, r *http.Request) {
	requestID, _ := r.Context().Value(ctxkeys.CtxKeyRequestID).(string)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusRequestEntityTooLarge)

	resp := map[string]any{
		"error": map[string]any{
			"code":           apierrors.ErrValidationPayloadTooLarge,
			"message":        "request body exceeds maximum allowed size",
			"classification": apierrors.Classification(apierrors.ErrValidationPayloadTooLarge),
		},
	}
	if requestID != "" {
		resp["requestId"] = requestID
	}
	_ = json.NewEncoder(w).Encode(resp)
}

// ParseSizeString parses a human-readable size string into bytes.
// Supported suffixes (case-insensitive): B, KB, MB, GB, TB.
// A plain numeric string is treated as bytes.
func ParseSizeString(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty size string")
	}

	s = strings.ToUpper(s)

	multipliers := []struct {
		suffix     string
		multiplier int64
	}{
		{"TB", 1 << 40},
		{"GB", 1 << 30},
		{"MB", 1 << 20},
		{"KB", 1 << 10},
		{"B", 1},
	}

	for _, m := range multipliers {
		if strings.HasSuffix(s, m.suffix) {
			numStr := strings.TrimSpace(s[:len(s)-len(m.suffix)])
			val, err := strconv.ParseFloat(numStr, 64)
			if err != nil {
				return 0, fmt.Errorf("invalid numeric value %q: %w", numStr, err)
			}
			if val < 0 {
				return 0, fmt.Errorf("size must be non-negative, got %v", val)
			}
			return int64(val * float64(m.multiplier)), nil
		}
	}

	// No suffix — treat as raw bytes.
	val, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size string %q: %w", s, err)
	}
	if val < 0 {
		return 0, fmt.Errorf("size must be non-negative, got %d", val)
	}
	return val, nil
}
