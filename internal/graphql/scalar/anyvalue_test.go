// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package scalar

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestUnmarshalAnyValue_BoolTrue(t *testing.T) {
	result, err := UnmarshalAnyValue(true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, ok := result.(bool)
	if !ok {
		t.Fatalf("expected bool, got %T", result)
	}
	if got != true {
		t.Errorf("UnmarshalAnyValue(true) = %v, want true", got)
	}
}

func TestUnmarshalAnyValue_BoolFalse(t *testing.T) {
	result, err := UnmarshalAnyValue(false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, ok := result.(bool)
	if !ok {
		t.Fatalf("expected bool, got %T", result)
	}
	if got != false {
		t.Errorf("UnmarshalAnyValue(false) = %v, want false", got)
	}
}

func TestUnmarshalAnyValue_Null(t *testing.T) {
	result, err := UnmarshalAnyValue(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("UnmarshalAnyValue(nil) = %v, want nil", result)
	}
}

func TestMarshalAnyValue_Nil(t *testing.T) {
	m := MarshalAnyValue(nil)
	var buf bytes.Buffer
	m.MarshalGQL(&buf)
	got := buf.String()
	if got != "null" {
		t.Errorf("MarshalAnyValue(nil) = %q, want %q", got, "null")
	}
}

func TestMarshalAnyValue_BoolTrue(t *testing.T) {
	m := MarshalAnyValue(true)
	var buf bytes.Buffer
	m.MarshalGQL(&buf)
	got := buf.String()
	if got != "true" {
		t.Errorf("MarshalAnyValue(true) = %q, want %q", got, "true")
	}
}

func TestMarshalAnyValue_BoolFalse(t *testing.T) {
	m := MarshalAnyValue(false)
	var buf bytes.Buffer
	m.MarshalGQL(&buf)
	got := buf.String()
	if got != "false" {
		t.Errorf("MarshalAnyValue(false) = %q, want %q", got, "false")
	}
}

func TestUnmarshalAnyValue_DepthBoundary64(t *testing.T) {
	// Build a structure exactly 64 container levels deep (depths 0 through 63).
	// The innermost container is an empty map at depth 63.
	// This should be accepted since depth 63 < MaxAnyValueDepth (64).
	var v any = map[string]any{} // empty map at the deepest level
	for i := 0; i < 63; i++ {
		v = map[string]any{"nested": v}
	}
	result, err := UnmarshalAnyValue(v)
	if err != nil {
		t.Fatalf("expected 64-level structure to be accepted, got error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result for 64-level structure")
	}
}

func TestUnmarshalAnyValue_DepthBoundary65(t *testing.T) {
	// Build a structure exactly 65 container levels deep (depths 0 through 64).
	// The innermost container at depth 64 triggers rejection since 64 >= MaxAnyValueDepth.
	var v any = map[string]any{} // empty map that will end up at depth 64
	for i := 0; i < 64; i++ {
		v = map[string]any{"nested": v}
	}
	_, err := UnmarshalAnyValue(v)
	if err == nil {
		t.Fatal("expected error for 65-level structure, got nil")
	}
	if !strings.Contains(err.Error(), "maximum nesting depth") {
		t.Errorf("error message %q does not contain 'maximum nesting depth'", err.Error())
	}
}

func TestUnmarshalAnyValue_DepthBoundary64WithLeafChild(t *testing.T) {
	// Build 64 container levels where the deepest container has a primitive leaf child.
	// With the optimized depth check (only on containers), this should pass because:
	// - The deepest container is at depth 63 (63 < 64, accepted)
	// - Its leaf child "hello" is a primitive — no depth check applied to it.
	var v any = map[string]any{"leaf": "hello"} // container at depth 63 with a leaf
	for i := 0; i < 63; i++ {
		v = map[string]any{"nested": v}
	}
	result, err := UnmarshalAnyValue(v)
	if err != nil {
		t.Fatalf("expected 64-level structure with leaf child to be accepted, got error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestUnmarshalAnyValue_StringNotParsed(t *testing.T) {
	// String "42" should stay as string, not be parsed to float64.
	result, err := UnmarshalAnyValue("42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, ok := result.(string)
	if !ok {
		t.Fatalf("expected string, got %T", result)
	}
	if got != "42" {
		t.Errorf("UnmarshalAnyValue(\"42\") = %q, want %q", got, "42")
	}
}

func TestMarshalAnyValue_Object(t *testing.T) {
	v := map[string]any{"key": "val"}
	m := MarshalAnyValue(v)
	var buf bytes.Buffer
	m.MarshalGQL(&buf)
	got := buf.String()
	if got != `{"key":"val"}` {
		t.Errorf("MarshalAnyValue(map) = %q, want %q", got, `{"key":"val"}`)
	}
}

func TestMarshalAnyValue_Array(t *testing.T) {
	v := []any{float64(1), "two", true}
	m := MarshalAnyValue(v)
	var buf bytes.Buffer
	m.MarshalGQL(&buf)
	got := buf.String()
	if got != `[1,"two",true]` {
		t.Errorf("MarshalAnyValue(array) = %q, want %q", got, `[1,"two",true]`)
	}
}

func TestMarshalAnyValue_Number(t *testing.T) {
	m := MarshalAnyValue(float64(3.14))
	var buf bytes.Buffer
	m.MarshalGQL(&buf)
	got := buf.String()
	if got != "3.14" {
		t.Errorf("MarshalAnyValue(3.14) = %q, want %q", got, "3.14")
	}
}

func TestUnmarshalAnyValue_UnsupportedType(t *testing.T) {
	_, err := UnmarshalAnyValue(struct{}{})
	if err == nil {
		t.Fatal("expected error for unsupported type (struct{}), got nil")
	}
	if !strings.Contains(err.Error(), "unsupported AnyValue type") {
		t.Errorf("error message %q does not contain 'unsupported AnyValue type'", err.Error())
	}
}

func TestUnmarshalAnyValue_DepthExceededReturnsSentinelError(t *testing.T) {
	// Verify that ErrAnyValueDepthExceeded sentinel error is returned (errors.Is compatible)
	// even when wrapped with path info.
	var v any = map[string]any{}
	for i := 0; i < 64; i++ {
		v = map[string]any{"nested": v}
	}
	_, err := UnmarshalAnyValue(v)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrAnyValueDepthExceeded) {
		t.Errorf("expected ErrAnyValueDepthExceeded sentinel, got: %v", err)
	}
}

func TestUnmarshalAnyValue_ErrorPathInfo(t *testing.T) {
	// Verify that errors from nested structures include path information.
	// Structure: {"a": [{"b": <unsupported>}]}
	input := map[string]any{
		"a": []any{
			map[string]any{
				"b": struct{}{}, // unsupported type
			},
		},
	}

	_, err := UnmarshalAnyValue(input)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	errMsg := err.Error()
	// Should contain path segments
	if !strings.Contains(errMsg, `at key "a"`) {
		t.Errorf("error message should contain path key 'a', got: %s", errMsg)
	}
	if !strings.Contains(errMsg, `at index [0]`) {
		t.Errorf("error message should contain array index [0], got: %s", errMsg)
	}
	if !strings.Contains(errMsg, `at key "b"`) {
		t.Errorf("error message should contain path key 'b', got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "unsupported AnyValue type") {
		t.Errorf("error message should contain root cause, got: %s", errMsg)
	}
}

func TestUnmarshalAnyValue_DepthErrorPathInfo(t *testing.T) {
	// Verify depth exceeded errors also include path info.
	// Build: {"deep": {"deep": ... }} 65 levels deep
	var v any = map[string]any{}
	for i := 0; i < 64; i++ {
		v = map[string]any{"deep": v}
	}

	_, err := UnmarshalAnyValue(v)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	errMsg := err.Error()
	// Should contain multiple path wrappings of "deep"
	if !strings.Contains(errMsg, `at key "deep"`) {
		t.Errorf("depth error should contain path info, got: %s", errMsg)
	}
	// Sentinel must still be unwrappable
	if !errors.Is(err, ErrAnyValueDepthExceeded) {
		t.Errorf("path-wrapped error should still match sentinel via errors.Is, got: %v", err)
	}
}
