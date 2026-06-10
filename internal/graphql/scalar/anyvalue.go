// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package scalar

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/99designs/gqlgen/graphql"
)

// AnyValue represents any valid JSON value: objects, arrays, strings, numbers, booleans, null.
// It is the Go backing type for the GraphQL `AnyValue` scalar.
type AnyValue = any

// MaxAnyValueDepth is the maximum nesting depth allowed for AnyValue validation.
// Containers at depth 0 through 63 are accepted (64 levels total).
// Depth counting: the root container is at depth 0, its child containers at depth 1, etc.
const MaxAnyValueDepth = 64

// ErrAnyValueDepthExceeded is returned when input nesting exceeds MaxAnyValueDepth.
// Pre-allocated to avoid heap allocation per request in hot paths.
var ErrAnyValueDepthExceeded = errors.New("AnyValue exceeds maximum nesting depth of 64")

// MarshalAnyValue implements the graphql.Marshaler interface for the AnyValue scalar.
// It serializes any Go value to JSON wire format, falling back to "null" on marshal error.
func MarshalAnyValue(v AnyValue) graphql.Marshaler {
	return graphql.WriterFunc(func(w io.Writer) {
		b, err := json.Marshal(v)
		if err != nil {
			_, _ = io.WriteString(w, "null")
			return
		}
		_, _ = w.Write(b)
	})
}

// UnmarshalAnyValue implements the graphql.Unmarshaler interface for the AnyValue scalar.
// It validates the input value's type and nesting depth, returning the value as-is if valid.
// It does NOT attempt to parse string inputs as JSON.
func UnmarshalAnyValue(v any) (AnyValue, error) {
	if err := validateAnyValue(v, 0); err != nil {
		return nil, err
	}
	return v, nil
}

// validateAnyValue recursively validates that v is a supported AnyValue type
// and that container nesting depth does not exceed MaxAnyValueDepth.
//
// Depth semantics: the depth parameter represents the current container nesting level.
// The depth check applies ONLY to containers (map/slice), not to primitives, because
// primitives cannot produce further nesting. This avoids rejecting leaf values inside
// valid containers at the boundary depth.
//
// Error path info: when validation fails inside a nested structure, each recursive
// level wraps the error with the key/index that led to the failure. This produces
// messages like: `at key "config": at key "items": at index [0]: AnyValue exceeds...`
// Path wrapping is zero-cost on the success path (no allocation unless error occurs).
func validateAnyValue(v any, depth int) error {
	switch val := v.(type) {
	case nil, bool, string, float64, int, int64:
		// Primitives have no children — no depth check needed.
		return nil
	case json.Number:
		// Defensive: accept json.Number in case UseNumber() is configured upstream.
		// Note: downstream consumers (e.g., SQL builders) should handle json.Number
		// alongside float64 when processing AnyValue filter arrays.
		return nil
	case map[string]any:
		if depth >= MaxAnyValueDepth {
			return ErrAnyValueDepthExceeded
		}
		for key, child := range val {
			if err := validateAnyValue(child, depth+1); err != nil {
				return fmt.Errorf("at key %q: %w", key, err)
			}
		}
		return nil
	case []any:
		if depth >= MaxAnyValueDepth {
			return ErrAnyValueDepthExceeded
		}
		for i, child := range val {
			if err := validateAnyValue(child, depth+1); err != nil {
				return fmt.Errorf("at index [%d]: %w", i, err)
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported AnyValue type: %T", v)
	}
}
