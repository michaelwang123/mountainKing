// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package scalar

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/99designs/gqlgen/graphql"
)

// JSON is a custom GraphQL scalar for arbitrary JSON values.
type JSON map[string]any

// MarshalJSON implements the graphql.Marshaler interface for JSON scalar.
func MarshalJSON(v JSON) graphql.Marshaler {
	return graphql.WriterFunc(func(w io.Writer) {
		b, err := json.Marshal(v)
		if err != nil {
			// Write null on marshal failure
			_, _ = io.WriteString(w, "null")
			return
		}
		_, _ = w.Write(b)
	})
}

// UnmarshalJSON implements the graphql.Unmarshaler interface for JSON scalar.
func UnmarshalJSON(v any) (JSON, error) {
	switch v := v.(type) {
	case map[string]any:
		return JSON(v), nil
	case string:
		var result JSON
		if err := json.Unmarshal([]byte(v), &result); err != nil {
			return nil, fmt.Errorf("JSON scalar must be a valid JSON object: %w", err)
		}
		return result, nil
	default:
		return nil, fmt.Errorf("JSON scalar must be a map or string, got %T", v)
	}
}
