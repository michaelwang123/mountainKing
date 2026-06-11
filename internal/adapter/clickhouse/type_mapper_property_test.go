// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package clickhouse

import (
	"fmt"
	"strings"
	"testing"

	"go.uber.org/zap"
	"pgregory.net/rapid"
)

// validGraphQLTypes is the set of all valid GraphQL type constants returned by MapType.
var validGraphQLTypes = map[GraphQLType]bool{
	GraphQLInt:      true,
	GraphQLFloat:    true,
	GraphQLString:   true,
	GraphQLBoolean:  true,
	GraphQLDateTime: true,
	GraphQLJSON:     true,
}

// TestPropertyTypeMappingCompleteness validates that for ANY random string input
// (arbitrary bytes, unicode, empty, very long), calling MapType(s) never panics
// and always returns one of the valid GraphQL type constants.
//
// **Validates: Requirements 7.1, 7.2, 7.3, 17.7**
func TestPropertyTypeMappingCompleteness(t *testing.T) {
	mapper := NewTypeMapper(zap.NewNop())

	rapid.Check(t, func(t *rapid.T) {
		// Generate arbitrary strings: empty, unicode, very long, random bytes
		sqlType := rapid.String().Draw(t, "sqlType")

		// MapType must not panic (rapid.Check will catch panics)
		result := mapper.MapType(sqlType)

		// Result must be one of the valid GraphQL types
		if !validGraphQLTypes[result] {
			t.Fatalf("MapType(%q) returned invalid GraphQL type %q", sqlType, result)
		}
	})
}

// TestPropertyTypeMappingCompleteness_LongStrings validates that very long type strings
// do not panic and return a valid GraphQL type.
//
// **Validates: Requirements 7.1, 7.2, 7.3, 17.7**
func TestPropertyTypeMappingCompleteness_LongStrings(t *testing.T) {
	mapper := NewTypeMapper(zap.NewNop())

	rapid.Check(t, func(t *rapid.T) {
		// Generate long strings by repeating random segments
		length := rapid.IntRange(100, 10000).Draw(t, "length")
		segment := rapid.String().Draw(t, "segment")
		if len(segment) == 0 {
			segment = "X"
		}
		// Repeat the segment to reach desired length
		sqlType := strings.Repeat(segment, (length/len(segment))+1)
		if len(sqlType) > length {
			sqlType = sqlType[:length]
		}

		result := mapper.MapType(sqlType)

		if !validGraphQLTypes[result] {
			t.Fatalf("MapType(string of len %d) returned invalid GraphQL type %q", len(sqlType), result)
		}
	})
}

// TestPropertyRecursionDepthSafety validates that deeply nested types
// (Nullable/LowCardinality wrapped more than 10 layers) never cause a stack overflow
// and always return GraphQLString.
//
// **Validates: Requirements 7.3, 17.8**
func TestPropertyRecursionDepthSafety(t *testing.T) {
	mapper := NewTypeMapper(zap.NewNop())

	rapid.Check(t, func(t *rapid.T) {
		// Generate a depth between 11 and 50 (always exceeds maxTypeRecursionDepth of 10)
		depth := rapid.IntRange(11, 50).Draw(t, "depth")

		// Choose a random wrapper type for each level
		wrappers := []string{"Nullable", "LowCardinality"}

		// Build nested type string like Nullable(LowCardinality(Nullable(...(String)...)))
		innerType := rapid.SampledFrom([]string{"String", "Int32", "Float64", "Boolean"}).Draw(t, "innerType")
		nested := innerType
		for i := 0; i < depth; i++ {
			wrapper := wrappers[rapid.IntRange(0, len(wrappers)-1).Draw(t, fmt.Sprintf("wrapper_%d", i))]
			nested = wrapper + "(" + nested + ")"
		}

		// MapType must not panic (stack overflow would be caught by rapid)
		result := mapper.MapType(nested)

		// At depth > 10, MapType should return GraphQLString (recursion depth exceeded)
		if result != GraphQLString {
			t.Fatalf("MapType with depth %d returned %q, want %q", depth, result, GraphQLString)
		}
	})
}

// TestPropertyRecursionDepthSafety_PureNullable validates that deeply nested
// pure Nullable(...) wrappers do not cause stack overflow.
//
// **Validates: Requirements 7.3, 17.8**
func TestPropertyRecursionDepthSafety_PureNullable(t *testing.T) {
	mapper := NewTypeMapper(zap.NewNop())

	rapid.Check(t, func(t *rapid.T) {
		depth := rapid.IntRange(11, 100).Draw(t, "depth")

		// Build Nullable(Nullable(...Nullable(String)...))
		nested := "String"
		for i := 0; i < depth; i++ {
			nested = "Nullable(" + nested + ")"
		}

		result := mapper.MapType(nested)

		if result != GraphQLString {
			t.Fatalf("MapType with %d Nullable wraps returned %q, want %q", depth, result, GraphQLString)
		}
	})
}

// TestPropertyRecursionDepthSafety_PureLowCardinality validates that deeply nested
// pure LowCardinality(...) wrappers do not cause stack overflow.
//
// **Validates: Requirements 7.3, 17.8**
func TestPropertyRecursionDepthSafety_PureLowCardinality(t *testing.T) {
	mapper := NewTypeMapper(zap.NewNop())

	rapid.Check(t, func(t *rapid.T) {
		depth := rapid.IntRange(11, 100).Draw(t, "depth")

		// Build LowCardinality(LowCardinality(...LowCardinality(String)...))
		nested := "String"
		for i := 0; i < depth; i++ {
			nested = "LowCardinality(" + nested + ")"
		}

		result := mapper.MapType(nested)

		if result != GraphQLString {
			t.Fatalf("MapType with %d LowCardinality wraps returned %q, want %q", depth, result, GraphQLString)
		}
	})
}

// TestPropertyRecursionDepthSafety_MixedNesting validates mixed Nullable/LowCardinality
// nesting at extreme depths with varied case does not panic.
//
// **Validates: Requirements 7.3, 17.8**
func TestPropertyRecursionDepthSafety_MixedNesting(t *testing.T) {
	mapper := NewTypeMapper(zap.NewNop())

	rapid.Check(t, func(t *rapid.T) {
		depth := rapid.IntRange(11, 50).Draw(t, "depth")

		// Use random case variants of the wrappers
		wrapperVariants := []string{
			"Nullable", "nullable", "NULLABLE",
			"LowCardinality", "lowcardinality", "LOWCARDINALITY",
		}

		innerType := "Int32"
		nested := innerType
		for i := 0; i < depth; i++ {
			idx := rapid.IntRange(0, len(wrapperVariants)-1).Draw(t, fmt.Sprintf("wrapIdx_%d", i))
			nested = wrapperVariants[idx] + "(" + nested + ")"
		}

		result := mapper.MapType(nested)

		// Must be a valid GraphQL type (should be String due to depth limit)
		if !validGraphQLTypes[result] {
			t.Fatalf("MapType with mixed nesting depth %d returned invalid type %q", depth, result)
		}

		// At depth > maxTypeRecursionDepth, result should be String
		if result != GraphQLString {
			t.Fatalf("MapType with depth %d returned %q, want %q", depth, result, GraphQLString)
		}
	})
}

// TestPropertyTypeMappingCompleteness_SpecialChars validates that types with
// special characters (parens, brackets, unicode, control chars) do not panic.
//
// **Validates: Requirements 7.1, 7.3, 17.7**
func TestPropertyTypeMappingCompleteness_SpecialChars(t *testing.T) {
	mapper := NewTypeMapper(zap.NewNop())

	rapid.Check(t, func(t *rapid.T) {
		// Generate strings that look vaguely like type declarations with random inner content
		prefix := rapid.SampledFrom([]string{
			"Nullable(", "LowCardinality(", "Array(",
			"SimpleAggregateFunction(", "", "(", ")",
		}).Draw(t, "prefix")

		middle := rapid.String().Draw(t, "middle")

		suffix := rapid.SampledFrom([]string{")", "", "((", "))"}).Draw(t, "suffix")

		sqlType := prefix + middle + suffix

		result := mapper.MapType(sqlType)

		if !validGraphQLTypes[result] {
			t.Fatalf("MapType(%q) returned invalid GraphQL type %q", sqlType, result)
		}
	})
}

// TestPropertyTypeMappingCompleteness_EmptyAndWhitespace validates that empty strings
// and whitespace-only strings do not panic and return a valid type.
//
// **Validates: Requirements 7.1, 17.7**
func TestPropertyTypeMappingCompleteness_EmptyAndWhitespace(t *testing.T) {
	mapper := NewTypeMapper(zap.NewNop())

	// Deterministic edge cases
	edgeCases := []string{"", " ", "\t", "\n", "  \t\n  ", "\x00", "\xff"}
	for _, tc := range edgeCases {
		result := mapper.MapType(tc)
		if !validGraphQLTypes[result] {
			t.Fatalf("MapType(%q) returned invalid GraphQL type %q", tc, result)
		}
	}

	// Property: any whitespace-padded string is valid
	rapid.Check(t, func(t *rapid.T) {
		padding := strings.Repeat(" ", rapid.IntRange(0, 50).Draw(t, "padLeft"))
		core := rapid.String().Draw(t, "core")
		paddingRight := strings.Repeat("\t", rapid.IntRange(0, 50).Draw(t, "padRight"))

		sqlType := padding + core + paddingRight

		result := mapper.MapType(sqlType)
		if !validGraphQLTypes[result] {
			t.Fatalf("MapType(%q) returned invalid GraphQL type %q", sqlType, result)
		}
	})
}
