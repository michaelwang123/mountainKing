// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package resolver

import (
	"testing"

	"pgregory.net/rapid"

	"github.com/michaelwang123/mountainKing/internal/graphql/generated"
)

// Feature: scalar-anyvalue-type, Property 7: IN/NOT_IN Filter Direct Array Handling
//
// For any non-empty []any value with IN or NOT_IN operator, the filter conversion
// should succeed and the resulting FilterCondition.Value should be the SAME slice
// (the original []any, not extracted from a map).
//
// **Validates: Requirements 6.3**
func TestProperty7_INNotINFilterDirectArrayHandling(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a random operator: IN or NOT_IN
		op := rapid.SampledFrom([]generated.FilterOperator{
			generated.FilterOperatorIn,
			generated.FilterOperatorNotIn,
		}).Draw(t, "operator")

		// Generate a random []any slice with 1-10 elements of primitive types
		arrLen := rapid.IntRange(1, 10).Draw(t, "arrayLength")
		arr := make([]any, arrLen)
		for i := range arr {
			arr[i] = genPrimitiveValue().Draw(t, "element")
		}

		// Build the MutationFilterInput with []any value directly
		input := &generated.MutationFilterInput{
			Field:    "test_field",
			Operator: op,
			Value:    arr,
		}

		// Convert the filter
		result, err := convertMutationFilter(input)

		// Verify: no error returned
		if err != nil {
			t.Fatalf("expected no error for valid []any with operator %s, got: %v", op, err)
		}

		// Verify: result Value is a []any
		resultArr, ok := result.Value.([]any)
		if !ok {
			t.Fatalf("expected result.Value to be []any, got %T", result.Value)
		}

		// Verify: same length and content
		if len(resultArr) != len(arr) {
			t.Fatalf("expected result array length %d, got %d", len(arr), len(resultArr))
		}
		for i := range arr {
			if resultArr[i] != arr[i] {
				t.Fatalf("element [%d] mismatch: expected %v (%T), got %v (%T)",
					i, arr[i], arr[i], resultArr[i], resultArr[i])
			}
		}
	})
}

// TestProperty7_INNotIN_ErrorCases validates error conditions for IN/NOT_IN filters.
//
// Feature: scalar-anyvalue-type, Property 7: IN/NOT_IN Filter Direct Array Handling
// **Validates: Requirements 6.3**
func TestProperty7_INNotIN_ErrorCases(t *testing.T) {
	t.Run("nil value with IN operator returns error", func(t *testing.T) {
		input := &generated.MutationFilterInput{
			Field:    "test_field",
			Operator: generated.FilterOperatorIn,
			Value:    nil,
		}

		_, err := convertMutationFilter(input)
		if err == nil {
			t.Fatal("expected error for nil value with IN operator, got nil")
		}
	})

	t.Run("non-array value with IN operator returns error", func(t *testing.T) {
		input := &generated.MutationFilterInput{
			Field:    "test_field",
			Operator: generated.FilterOperatorIn,
			Value:    float64(42),
		}

		_, err := convertMutationFilter(input)
		if err == nil {
			t.Fatal("expected error for non-array value with IN operator, got nil")
		}
	})

	t.Run("empty array with IN operator returns error", func(t *testing.T) {
		input := &generated.MutationFilterInput{
			Field:    "test_field",
			Operator: generated.FilterOperatorIn,
			Value:    []any{},
		}

		_, err := convertMutationFilter(input)
		if err == nil {
			t.Fatal("expected error for empty array with IN operator, got nil")
		}
	})

	t.Run("nil value with NOT_IN operator returns error", func(t *testing.T) {
		input := &generated.MutationFilterInput{
			Field:    "test_field",
			Operator: generated.FilterOperatorNotIn,
			Value:    nil,
		}

		_, err := convertMutationFilter(input)
		if err == nil {
			t.Fatal("expected error for nil value with NOT_IN operator, got nil")
		}
	})

	t.Run("non-array value with NOT_IN operator returns error", func(t *testing.T) {
		input := &generated.MutationFilterInput{
			Field:    "test_field",
			Operator: generated.FilterOperatorNotIn,
			Value:    float64(99),
		}

		_, err := convertMutationFilter(input)
		if err == nil {
			t.Fatal("expected error for non-array value with NOT_IN operator, got nil")
		}
	})

	t.Run("empty array with NOT_IN operator returns error", func(t *testing.T) {
		input := &generated.MutationFilterInput{
			Field:    "test_field",
			Operator: generated.FilterOperatorNotIn,
			Value:    []any{},
		}

		_, err := convertMutationFilter(input)
		if err == nil {
			t.Fatal("expected error for empty array with NOT_IN operator, got nil")
		}
	})
}

// genPrimitiveValue generates a random primitive value (float64, string, or bool).
func genPrimitiveValue() *rapid.Generator[any] {
	return rapid.Custom[any](func(t *rapid.T) any {
		kind := rapid.IntRange(0, 2).Draw(t, "primitiveKind")
		switch kind {
		case 0:
			return rapid.Float64Range(-1e6, 1e6).Draw(t, "float64Val")
		case 1:
			return rapid.StringMatching(`[a-zA-Z0-9]{1,20}`).Draw(t, "stringVal")
		default:
			return rapid.Bool().Draw(t, "boolVal")
		}
	})
}
