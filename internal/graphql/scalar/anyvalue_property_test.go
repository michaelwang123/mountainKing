// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package scalar

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// =============================================================================
// Generators for AnyValue property tests
// =============================================================================

// genAnyValueLeaf generates a leaf-level AnyValue (no containers).
func genAnyValueLeaf() *rapid.Generator[any] {
	return rapid.OneOf(
		rapid.Just[any](nil),
		rapid.Map(rapid.Bool(), func(b bool) any { return b }),
		rapid.Map(rapid.String(), func(s string) any { return s }),
		rapid.Map(rapid.Float64Range(-1e15, 1e15), func(f float64) any { return f }),
	)
}

// genAnyValueObject generates a map[string]any with leaf values.
func genAnyValueObject() *rapid.Generator[any] {
	return rapid.Custom[any](func(t *rapid.T) any {
		size := rapid.IntRange(0, 5).Draw(t, "mapSize")
		m := make(map[string]any, size)
		for i := 0; i < size; i++ {
			key := rapid.StringMatching(`[a-z]{1,8}`).Draw(t, "key")
			val := genAnyValueLeaf().Draw(t, "val")
			m[key] = val
		}
		return m
	})
}

// genAnyValueArray generates a []any with leaf values.
func genAnyValueArray() *rapid.Generator[any] {
	return rapid.Custom[any](func(t *rapid.T) any {
		size := rapid.IntRange(0, 5).Draw(t, "arrSize")
		arr := make([]any, size)
		for i := 0; i < size; i++ {
			arr[i] = genAnyValueLeaf().Draw(t, "elem")
		}
		return arr
	})
}

// genAnyValueShallow generates any valid AnyValue input including containers with leaf values.
func genAnyValueShallow() *rapid.Generator[any] {
	return rapid.OneOf(
		genAnyValueLeaf(),
		genAnyValueObject(),
		genAnyValueArray(),
	)
}

// genAnyValueRecursive generates any valid JSON value (recursive, depth-limited).
func genAnyValueRecursive(maxDepth int) *rapid.Generator[any] {
	return rapid.Custom[any](func(t *rapid.T) any {
		if maxDepth <= 0 {
			return genAnyValueLeaf().Draw(t, "leaf")
		}
		kind := rapid.IntRange(0, 5).Draw(t, "kind")
		switch kind {
		case 0:
			return nil
		case 1:
			return rapid.Bool().Draw(t, "bool")
		case 2:
			return rapid.String().Draw(t, "string")
		case 3:
			return rapid.Float64Range(-1e15, 1e15).Draw(t, "number")
		case 4:
			size := rapid.IntRange(0, 3).Draw(t, "mapSize")
			m := make(map[string]any, size)
			for i := 0; i < size; i++ {
				key := rapid.StringMatching(`[a-z]{1,8}`).Draw(t, "key")
				m[key] = genAnyValueRecursive(maxDepth-1).Draw(t, "mapValue")
			}
			return m
		case 5:
			size := rapid.IntRange(0, 3).Draw(t, "arrSize")
			arr := make([]any, size)
			for i := 0; i < size; i++ {
				arr[i] = genAnyValueRecursive(maxDepth-1).Draw(t, "arrElem")
			}
			return arr
		default:
			return nil
		}
	})
}

// buildNestedStructure creates a nested structure of exactly the given depth
// (number of container levels). The innermost container is empty.
func buildNestedStructure(depth int, startWithMap bool) any {
	var result any
	if startWithMap {
		result = map[string]any{}
	} else {
		result = []any{}
	}
	for i := 1; i < depth; i++ {
		useMap := startWithMap
		if i%2 == 1 {
			useMap = !startWithMap
		}
		if useMap {
			result = map[string]any{"nested": result}
		} else {
			result = []any{result}
		}
	}
	return result
}

// genOverDepthStructure builds a nested map structure exceeding MaxAnyValueDepth.
func genOverDepthStructure(depth int) any {
	var current any = map[string]any{}
	for i := 1; i < depth; i++ {
		current = map[string]any{"deep": current}
	}
	return current
}

// normalizeForComparison normalizes a value to account for JSON round-trip
// semantics: integers become float64 after JSON round-trip, and NaN/Inf
// become null. This allows semantic equivalence comparison.
func normalizeForComparison(v any) any {
	switch val := v.(type) {
	case map[string]any:
		m := make(map[string]any, len(val))
		for k, child := range val {
			m[k] = normalizeForComparison(child)
		}
		return m
	case []any:
		arr := make([]any, len(val))
		for i, child := range val {
			arr[i] = normalizeForComparison(child)
		}
		return arr
	case float64:
		b, err := json.Marshal(val)
		if err != nil || string(b) == "null" {
			return nil
		}
		return val
	case int:
		return float64(val)
	case int64:
		return float64(val)
	default:
		return v
	}
}

// =============================================================================
// Feature: scalar-anyvalue-type, Property 1: Round-trip preservation
// **Validates: Requirements 2.7, 8.1, 8.2, 8.3**
// =============================================================================

func TestAnyValueProperty1_RoundTripPreservation(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		original := genAnyValueRecursive(4).Draw(rt, "anyValue")

		marshaler := MarshalAnyValue(original)
		var buf bytes.Buffer
		marshaler.MarshalGQL(&buf)
		jsonBytes := buf.Bytes()

		var roundTripped any
		err := json.Unmarshal(jsonBytes, &roundTripped)
		if err != nil {
			rt.Fatalf("json.Unmarshal failed on marshaled output %q: %v", string(jsonBytes), err)
		}

		result, err := UnmarshalAnyValue(roundTripped)
		if err != nil {
			rt.Fatalf("UnmarshalAnyValue failed on round-tripped value: %v", err)
		}

		expected := normalizeForComparison(original)
		got := normalizeForComparison(result)

		if !reflect.DeepEqual(expected, got) {
			rt.Fatalf("round-trip mismatch:\n  original: %#v\n  got:      %#v\n  json:     %s",
				expected, got, string(jsonBytes))
		}
	})
}

// =============================================================================
// Feature: scalar-anyvalue-type, Property 2: Type preservation
// **Validates: Requirements 1.1-1.7, 3.1-3.6**
// =============================================================================

func TestAnyValueProperty2_TypePreservation(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		input := genAnyValueShallow().Draw(rt, "input")

		result, err := UnmarshalAnyValue(input)
		if err != nil {
			rt.Fatalf("UnmarshalAnyValue(%v) returned unexpected error: %v", input, err)
		}

		switch input.(type) {
		case nil:
			if result != nil {
				rt.Fatalf("expected nil, got %T(%v)", result, result)
			}
		case bool:
			if _, ok := result.(bool); !ok {
				rt.Fatalf("expected bool, got %T(%v)", result, result)
			}
		case string:
			if _, ok := result.(string); !ok {
				rt.Fatalf("expected string, got %T(%v)", result, result)
			}
		case float64:
			if _, ok := result.(float64); !ok {
				rt.Fatalf("expected float64, got %T(%v)", result, result)
			}
		case map[string]any:
			if _, ok := result.(map[string]any); !ok {
				rt.Fatalf("expected map[string]any, got %T(%v)", result, result)
			}
		case []any:
			if _, ok := result.([]any); !ok {
				rt.Fatalf("expected []any, got %T(%v)", result, result)
			}
		default:
			rt.Fatalf("input had unexpected type %T", input)
		}
	})
}

func TestAnyValueProperty2_TypePreservation_IntTypes(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		intVal := rapid.Int().Draw(rt, "intVal")
		result, err := UnmarshalAnyValue(intVal)
		if err != nil {
			rt.Fatalf("UnmarshalAnyValue(int(%d)) returned unexpected error: %v", intVal, err)
		}
		if _, ok := result.(int); !ok {
			rt.Fatalf("expected int, got %T(%v)", result, result)
		}
	})
}

func TestAnyValueProperty2_TypePreservation_Int64(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		int64Val := rapid.Int64().Draw(rt, "int64Val")
		result, err := UnmarshalAnyValue(int64Val)
		if err != nil {
			rt.Fatalf("UnmarshalAnyValue(int64(%d)) returned unexpected error: %v", int64Val, err)
		}
		if _, ok := result.(int64); !ok {
			rt.Fatalf("expected int64, got %T(%v)", result, result)
		}
	})
}

// =============================================================================
// Feature: scalar-anyvalue-type, Property 3: Depth acceptance
// **Validates: Requirements 1.9**
// =============================================================================

func TestAnyValueProperty3_DepthAcceptance(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		depth := rapid.IntRange(1, 64).Draw(rt, "depth")
		useMap := rapid.Bool().Draw(rt, "useMap")

		structure := buildNestedStructure(depth, useMap)

		_, err := UnmarshalAnyValue(structure)
		if err != nil {
			rt.Fatalf("UnmarshalAnyValue should accept %d container levels, got error: %v", depth, err)
		}
	})
}

// =============================================================================
// Feature: scalar-anyvalue-type, Property 4: Depth rejection
// **Validates: Requirements 1.10**
// =============================================================================

func TestAnyValueProperty4_DepthRejection(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		depth := rapid.IntRange(65, 128).Draw(rt, "depth")

		value := genOverDepthStructure(depth)

		_, err := UnmarshalAnyValue(value)
		if err == nil {
			rt.Fatalf("expected error for %d container levels, got nil", depth)
		}
		if !strings.Contains(err.Error(), "maximum nesting depth") {
			rt.Fatalf("expected error containing 'maximum nesting depth', got: %v", err)
		}
	})
}

// =============================================================================
// Feature: scalar-anyvalue-type, Property 5: Unsupported type rejection
// **Validates: Requirements 1.8**
// =============================================================================

func TestAnyValueProperty5_UnsupportedTypeRejection(t *testing.T) {
	unsupportedValues := []any{
		struct{}{},
		complex(1, 2),
		[]int{1, 2, 3},
		map[int]any{1: "one"},
		uint(42),
		uint64(999),
		float32(1.5),
	}

	gen := rapid.SampledFrom(unsupportedValues)

	rapid.Check(t, func(rt *rapid.T) {
		unsupported := gen.Draw(rt, "unsupportedValue")

		_, err := UnmarshalAnyValue(unsupported)
		if err == nil {
			rt.Fatalf("expected error for unsupported type %T, got nil", unsupported)
		}
		if !strings.Contains(err.Error(), "unsupported AnyValue type") {
			rt.Fatalf("expected error containing 'unsupported AnyValue type', got: %v", err)
		}
	})
}

// =============================================================================
// Feature: scalar-anyvalue-type, Property 6: String passthrough (no JSON parsing)
// **Validates: Requirements 3.7**
// =============================================================================

func TestAnyValueProperty6_StringPassthrough(t *testing.T) {
	jsonLookingStrings := []string{
		"42", "3.14", "true", "false", "null",
		`[1,2,3]`, `{"key":"value"}`, `""`,
		`[{"nested":true}]`, `{"a":[1,2,3]}`,
		"0", "-1", "1e10",
	}

	genString := rapid.OneOf(
		rapid.String(),
		rapid.SampledFrom(jsonLookingStrings),
		rapid.StringMatching(`[a-zA-Z0-9 _\-\.]{0,100}`),
	)

	rapid.Check(t, func(rt *rapid.T) {
		input := genString.Draw(rt, "input_string")

		result, err := UnmarshalAnyValue(input)
		if err != nil {
			rt.Fatalf("UnmarshalAnyValue(%q) returned unexpected error: %v", input, err)
		}

		resultStr, ok := result.(string)
		if !ok {
			rt.Fatalf("UnmarshalAnyValue(%q) returned type %T, want string", input, result)
		}

		if resultStr != input {
			rt.Fatalf("UnmarshalAnyValue(%q) returned %q, want exact same string", input, resultStr)
		}
	})
}
