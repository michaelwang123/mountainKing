// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package template

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// =============================================================================
// Feature: sql-template-engine
// Task 3.3: 安全函数单元测试和属性测试
// =============================================================================

// =============================================================================
// Property 28: safeString 转义正确性
// **Validates: Requirements 6.1**
// For any string, safeString output contains no unescaped single quotes
// (every ' is preceded by another ').
// =============================================================================

func TestProperty28_SafeStringEscapeCorrectness(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		input := rapid.String().Draw(rt, "input")
		result, err := safeString(input)
		if err != nil {
			rt.Fatalf("safeString(%q) returned error: %v", input, err)
		}

		// Every single quote in the output must be doubled.
		// Walk through the result: whenever we see a ', the next char must also be '.
		i := 0
		for i < len(result) {
			if result[i] == '\'' {
				if i+1 >= len(result) || result[i+1] != '\'' {
					rt.Fatalf("safeString(%q) = %q: unescaped single quote at position %d", input, result, i)
				}
				i += 2 // skip the pair
			} else {
				i++
			}
		}
	})
}

// =============================================================================
// Property 29: safeString 反斜杠安全
// **Validates: Requirements 6.1**
// For any string, safeString output contains no unescaped backslashes
// (every \ is followed by another \).
// =============================================================================

func TestProperty29_SafeStringBackslashSafety(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		input := rapid.String().Draw(rt, "input")
		result, err := safeString(input)
		if err != nil {
			rt.Fatalf("safeString(%q) returned error: %v", input, err)
		}

		// Every backslash in the output must be doubled.
		i := 0
		for i < len(result) {
			if result[i] == '\\' {
				if i+1 >= len(result) || result[i+1] != '\\' {
					rt.Fatalf("safeString(%q) = %q: unescaped backslash at position %d", input, result, i)
				}
				i += 2 // skip the pair
			} else {
				i++
			}
		}
	})
}

// =============================================================================
// Property 30: safeInt 类型安全
// **Validates: Requirements 6.2**
// For any valid int input, safeInt returns a string parseable as int64.
// For non-integer float64, returns error.
// =============================================================================

func TestProperty30_SafeIntTypeSafety(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Test with valid int values
		intVal := rapid.Int64().Draw(rt, "intVal")
		result, err := safeInt(intVal)
		if err != nil {
			rt.Fatalf("safeInt(%d) returned error: %v", intVal, err)
		}
		parsed, err := strconv.ParseInt(result, 10, 64)
		if err != nil {
			rt.Fatalf("safeInt(%d) = %q: not parseable as int64: %v", intVal, result, err)
		}
		if parsed != intVal {
			rt.Fatalf("safeInt(%d) = %q: parsed as %d, expected %d", intVal, result, parsed, intVal)
		}
	})
}

func TestProperty30_SafeIntRejectsNonIntegerFloat(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate a float64 with a fractional part
		intPart := rapid.Float64Range(-1e15, 1e15).Draw(rt, "intPart")
		frac := rapid.Float64Range(0.01, 0.99).Draw(rt, "frac")
		floatVal := intPart + frac

		if floatVal == math.Trunc(floatVal) {
			return // skip if it happens to be an integer
		}

		_, err := safeInt(floatVal)
		if err == nil {
			rt.Fatalf("safeInt(%v) should return error for non-integer float64", floatVal)
		}
	})
}

// =============================================================================
// Property 31: safeFloat 类型安全
// **Validates: Requirements 6.3**
// For any valid float input, safeFloat returns a finite float64 string.
// NaN/±Inf are rejected.
// =============================================================================

func TestProperty31_SafeFloatTypeSafety(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Test with valid finite float values
		floatVal := rapid.Float64Range(-1e18, 1e18).Draw(rt, "floatVal")
		result, err := safeFloat(floatVal)
		if err != nil {
			rt.Fatalf("safeFloat(%v) returned error: %v", floatVal, err)
		}
		parsed, err := strconv.ParseFloat(result, 64)
		if err != nil {
			rt.Fatalf("safeFloat(%v) = %q: not parseable as float64: %v", floatVal, result, err)
		}
		if math.IsNaN(parsed) || math.IsInf(parsed, 0) {
			rt.Fatalf("safeFloat(%v) = %q: parsed as non-finite value", floatVal, result)
		}
	})
}

func TestProperty31_SafeFloatRejectsNaNInf(t *testing.T) {
	for _, val := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		_, err := safeFloat(val)
		if err == nil {
			t.Errorf("safeFloat(%v) should return error for NaN/Inf", val)
		}
	}
}

// =============================================================================
// Property 32: safeIdentifier 字符安全
// **Validates: Requirements 6.4**
// For any successful safeIdentifier call, the output only contains
// [a-zA-Z0-9_`.] characters.
// =============================================================================

var safeIdentifierOutputRe = regexp.MustCompile(`^[a-zA-Z0-9_` + "`" + `\.]+$`)

func TestProperty32_SafeIdentifierCharSafety(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate valid identifier inputs (1 or 2 segments)
		numSegments := rapid.IntRange(1, 2).Draw(rt, "numSegments")
		segments := make([]string, numSegments)
		for i := 0; i < numSegments; i++ {
			seg := rapid.StringMatching(`[a-zA-Z][a-zA-Z0-9_]{0,10}`).Draw(rt, "segment")
			segments[i] = seg
		}
		input := strings.Join(segments, ".")

		result, err := safeIdentifier(input)
		if err != nil {
			rt.Fatalf("safeIdentifier(%q) returned error: %v", input, err)
		}

		if !safeIdentifierOutputRe.MatchString(result) {
			rt.Fatalf("safeIdentifier(%q) = %q: contains illegal characters", input, result)
		}
	})
}

// =============================================================================
// Property 33: safeIdentifier 反引号包裹
// **Validates: Requirements 6.4**
// For any successful safeIdentifier call, each segment is wrapped in backticks.
// =============================================================================

func TestProperty33_SafeIdentifierBacktickWrapping(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		numSegments := rapid.IntRange(1, 2).Draw(rt, "numSegments")
		segments := make([]string, numSegments)
		for i := 0; i < numSegments; i++ {
			seg := rapid.StringMatching(`[a-zA-Z][a-zA-Z0-9_]{0,10}`).Draw(rt, "segment")
			segments[i] = seg
		}
		input := strings.Join(segments, ".")

		result, err := safeIdentifier(input)
		if err != nil {
			rt.Fatalf("safeIdentifier(%q) returned error: %v", input, err)
		}

		// Split by "." and verify each part is backtick-wrapped
		parts := strings.Split(result, ".")
		if len(parts) != numSegments {
			rt.Fatalf("safeIdentifier(%q) = %q: expected %d segments, got %d", input, result, numSegments, len(parts))
		}
		for i, part := range parts {
			if !strings.HasPrefix(part, "`") || !strings.HasSuffix(part, "`") {
				rt.Fatalf("safeIdentifier(%q) = %q: segment %d (%q) not wrapped in backticks", input, result, i, part)
			}
		}
	})
}

// =============================================================================
// Property 34: safeIdentifier 段数限制
// **Validates: Requirements 6.4**
// Input with >2 dots returns error.
// =============================================================================

func TestProperty34_SafeIdentifierSegmentLimit(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		numSegments := rapid.IntRange(3, 6).Draw(rt, "numSegments")
		segments := make([]string, numSegments)
		for i := 0; i < numSegments; i++ {
			seg := rapid.StringMatching(`[a-zA-Z][a-zA-Z0-9_]{0,5}`).Draw(rt, "segment")
			segments[i] = seg
		}
		input := strings.Join(segments, ".")

		_, err := safeIdentifier(input)
		if err == nil {
			rt.Fatalf("safeIdentifier(%q) should return error for >2 segments", input)
		}
	})
}

// =============================================================================
// Property 35: safeInList 元素独立转义
// **Validates: Requirements 6.5**
// For any string slice, each element in safeInList output is independently escaped.
// =============================================================================

func TestProperty35_SafeInListIndependentEscape(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		numItems := rapid.IntRange(1, 10).Draw(rt, "numItems")
		items := make([]string, numItems)
		for i := 0; i < numItems; i++ {
			items[i] = rapid.String().Draw(rt, "item")
		}

		result, err := safeInList(items)
		if err != nil {
			rt.Fatalf("safeInList(%v) returned error: %v", items, err)
		}

		// Each element should be independently escaped via safeString and quoted.
		// Verify by escaping each element individually and comparing.
		expectedParts := make([]string, numItems)
		for i, item := range items {
			escaped, err := safeString(item)
			if err != nil {
				rt.Fatalf("safeString(%q) returned error: %v", item, err)
			}
			expectedParts[i] = "'" + escaped + "'"
		}
		expected := strings.Join(expectedParts, ",")

		if result != expected {
			rt.Fatalf("safeInList result mismatch:\n  got:      %q\n  expected: %q", result, expected)
		}
	})
}

// =============================================================================
// Property 36: safeInList 空数组拒绝
// **Validates: Requirements 6.5**
// Empty slice always returns error.
// =============================================================================

func TestProperty36_SafeInListEmptySliceRejection(t *testing.T) {
	// Test with empty []string
	_, err := safeInList([]string{})
	if err == nil {
		t.Fatal("safeInList([]string{}) should return error for empty slice")
	}

	// Test with empty []any
	_, err = safeInList([]any{})
	if err == nil {
		t.Fatal("safeInList([]any{}) should return error for empty slice")
	}
}

// =============================================================================
// Property 37: safeLike 通配符转义
// **Validates: Requirements 6.8**
// For any string, safeLike output contains no unescaped % or _ characters.
// =============================================================================

func TestProperty37_SafeLikeWildcardEscape(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		input := rapid.String().Draw(rt, "input")
		result, err := safeLike(input)
		if err != nil {
			rt.Fatalf("safeLike(%q) returned error: %v", input, err)
		}

		// Walk through the result: % and _ must be preceded by \.
		i := 0
		for i < len(result) {
			if result[i] == '\\' {
				// Skip the escaped character
				i += 2
				continue
			}
			if result[i] == '%' {
				rt.Fatalf("safeLike(%q) = %q: unescaped '%%' at position %d", input, result, i)
			}
			if result[i] == '_' {
				rt.Fatalf("safeLike(%q) = %q: unescaped '_' at position %d", input, result, i)
			}
			i++
		}
	})
}

// =============================================================================
// Unit Tests: safeString edge cases
// =============================================================================

func TestSafeString_NullBytes(t *testing.T) {
	result, err := safeString("hello\x00world")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(result, "\x00") {
		t.Fatalf("result should not contain NULL bytes: %q", result)
	}
	if result != "helloworld" {
		t.Fatalf("expected %q, got %q", "helloworld", result)
	}
}

func TestSafeString_BackslashQuoteCombo(t *testing.T) {
	// Input: \' should become \\'' (backslash escaped, then quote escaped)
	result, err := safeString(`\'`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != `\\''` {
		t.Fatalf("expected %q, got %q", `\\''`, result)
	}
}

func TestSafeString_MultipleBackslashesBeforeQuote(t *testing.T) {
	// Input: \\' should become \\\\''
	result, err := safeString(`\\'`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != `\\\\''` {
		t.Fatalf("expected %q, got %q", `\\\\''`, result)
	}
}

// =============================================================================
// Unit Tests: quote wrapping
// =============================================================================

func TestQuote_Basic(t *testing.T) {
	result, err := quote("hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "'hello'" {
		t.Fatalf("expected %q, got %q", "'hello'", result)
	}
}

func TestQuote_WithSingleQuote(t *testing.T) {
	result, err := quote("O'Brien")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "'O''Brien'" {
		t.Fatalf("expected %q, got %q", "'O''Brien'", result)
	}
}

func TestQuote_EmptyString(t *testing.T) {
	result, err := quote("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "''" {
		t.Fatalf("expected %q, got %q", "''", result)
	}
}

// =============================================================================
// Unit Tests: safeInt type conversions
// =============================================================================

func TestSafeInt_Int(t *testing.T) {
	result, err := safeInt(42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "42" {
		t.Fatalf("expected %q, got %q", "42", result)
	}
}

func TestSafeInt_Int64(t *testing.T) {
	result, err := safeInt(int64(9223372036854775807))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "9223372036854775807" {
		t.Fatalf("expected %q, got %q", "9223372036854775807", result)
	}
}

func TestSafeInt_Float64Whole(t *testing.T) {
	result, err := safeInt(float64(100))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "100" {
		t.Fatalf("expected %q, got %q", "100", result)
	}
}

func TestSafeInt_Float64Fractional(t *testing.T) {
	_, err := safeInt(3.14)
	if err == nil {
		t.Fatal("expected error for fractional float64")
	}
}

func TestSafeInt_String(t *testing.T) {
	result, err := safeInt("123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "123" {
		t.Fatalf("expected %q, got %q", "123", result)
	}
}

func TestSafeInt_InvalidString(t *testing.T) {
	_, err := safeInt("abc")
	if err == nil {
		t.Fatal("expected error for non-numeric string")
	}
}

func TestSafeInt_UnsupportedType(t *testing.T) {
	_, err := safeInt([]int{1, 2})
	if err == nil {
		t.Fatal("expected error for unsupported type")
	}
}

// =============================================================================
// Unit Tests: safeFloat NaN/Inf rejection
// =============================================================================

func TestSafeFloat_NaN(t *testing.T) {
	_, err := safeFloat(math.NaN())
	if err == nil {
		t.Fatal("expected error for NaN")
	}
}

func TestSafeFloat_PosInf(t *testing.T) {
	_, err := safeFloat(math.Inf(1))
	if err == nil {
		t.Fatal("expected error for +Inf")
	}
}

func TestSafeFloat_NegInf(t *testing.T) {
	_, err := safeFloat(math.Inf(-1))
	if err == nil {
		t.Fatal("expected error for -Inf")
	}
}

func TestSafeFloat_ValidFloat(t *testing.T) {
	result, err := safeFloat(3.14)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "3.14" {
		t.Fatalf("expected %q, got %q", "3.14", result)
	}
}

func TestSafeFloat_StringNaN(t *testing.T) {
	_, err := safeFloat("NaN")
	if err == nil {
		t.Fatal("expected error for string NaN")
	}
}

func TestSafeFloat_StringInf(t *testing.T) {
	_, err := safeFloat("+Inf")
	if err == nil {
		t.Fatal("expected error for string +Inf")
	}
}

// =============================================================================
// Unit Tests: safeIdentifier segment validation
// =============================================================================

func TestSafeIdentifier_SingleSegment(t *testing.T) {
	result, err := safeIdentifier("users")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "`users`" {
		t.Fatalf("expected %q, got %q", "`users`", result)
	}
}

func TestSafeIdentifier_TwoSegments(t *testing.T) {
	result, err := safeIdentifier("db.users")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "`db`.`users`" {
		t.Fatalf("expected %q, got %q", "`db`.`users`", result)
	}
}

func TestSafeIdentifier_ThreeSegments(t *testing.T) {
	_, err := safeIdentifier("a.b.c")
	if err == nil {
		t.Fatal("expected error for 3 segments")
	}
}

func TestSafeIdentifier_Empty(t *testing.T) {
	_, err := safeIdentifier("")
	if err == nil {
		t.Fatal("expected error for empty identifier")
	}
}

func TestSafeIdentifier_IllegalChars(t *testing.T) {
	_, err := safeIdentifier("users; DROP TABLE")
	if err == nil {
		t.Fatal("expected error for illegal characters")
	}
}

func TestSafeIdentifier_EmptySegment(t *testing.T) {
	_, err := safeIdentifier("db.")
	if err == nil {
		t.Fatal("expected error for empty segment")
	}
}

// =============================================================================
// Unit Tests: safeInList with []any and []string
// =============================================================================

func TestSafeInList_StringSlice(t *testing.T) {
	result, err := safeInList([]string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "'a','b','c'" {
		t.Fatalf("expected %q, got %q", "'a','b','c'", result)
	}
}

func TestSafeInList_InterfaceSlice(t *testing.T) {
	result, err := safeInList([]any{"x", "y"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "'x','y'" {
		t.Fatalf("expected %q, got %q", "'x','y'", result)
	}
}

func TestSafeInList_InterfaceSliceNonString(t *testing.T) {
	_, err := safeInList([]any{1, 2})
	if err == nil {
		t.Fatal("expected error for non-string elements in []any")
	}
}

func TestSafeInList_WithEscaping(t *testing.T) {
	result, err := safeInList([]string{"it's", `back\slash`})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != `'it''s','back\\slash'` {
		t.Fatalf("expected %q, got %q", `'it''s','back\\slash'`, result)
	}
}

func TestSafeInList_UnsupportedType(t *testing.T) {
	_, err := safeInList("not a slice")
	if err == nil {
		t.Fatal("expected error for unsupported type")
	}
}

// =============================================================================
// Unit Tests: safeLike escape ordering
// =============================================================================

func TestSafeLike_PercentEscape(t *testing.T) {
	result, err := safeLike("100%")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != `100\%` {
		t.Fatalf("expected %q, got %q", `100\%`, result)
	}
}

func TestSafeLike_UnderscoreEscape(t *testing.T) {
	result, err := safeLike("user_name")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != `user\_name` {
		t.Fatalf("expected %q, got %q", `user\_name`, result)
	}
}

func TestSafeLike_BackslashEscape(t *testing.T) {
	result, err := safeLike(`path\to`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != `path\\to` {
		t.Fatalf("expected %q, got %q", `path\\to`, result)
	}
}

func TestSafeLike_CombinedEscape(t *testing.T) {
	// Input: \% should become \\% → then % escaped → \\\%
	result, err := safeLike(`\%`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != `\\\%` {
		t.Fatalf("expected %q, got %q", `\\\%`, result)
	}
}

// =============================================================================
// Unit Tests: join, defaultFn, upper, lower, trimSpace
// =============================================================================

func TestJoin_StringSlice(t *testing.T) {
	result, err := join([]string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "a,b,c" {
		t.Fatalf("expected %q, got %q", "a,b,c", result)
	}
}

func TestJoin_InterfaceSlice(t *testing.T) {
	result, err := join([]any{"x", 1, true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "x,1,true" {
		t.Fatalf("expected %q, got %q", "x,1,true", result)
	}
}

func TestJoin_UnsupportedType(t *testing.T) {
	_, err := join("not a slice")
	if err == nil {
		t.Fatal("expected error for unsupported type")
	}
}

func TestDefaultFn_NilValue(t *testing.T) {
	result := defaultFn(100, nil)
	if result != 100 {
		t.Fatalf("expected 100, got %v", result)
	}
}

func TestDefaultFn_ZeroValue(t *testing.T) {
	result := defaultFn(100, 0)
	if result != 100 {
		t.Fatalf("expected 100, got %v", result)
	}
}

func TestDefaultFn_NonZeroValue(t *testing.T) {
	result := defaultFn(100, 42)
	if result != 42 {
		t.Fatalf("expected 42, got %v", result)
	}
}

func TestDefaultFn_EmptyString(t *testing.T) {
	result := defaultFn("default", "")
	if result != "default" {
		t.Fatalf("expected %q, got %v", "default", result)
	}
}

func TestUpper(t *testing.T) {
	result, err := upper("hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "HELLO" {
		t.Fatalf("expected %q, got %q", "HELLO", result)
	}
}

func TestLower(t *testing.T) {
	result, err := lower("HELLO")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "hello" {
		t.Fatalf("expected %q, got %q", "hello", result)
	}
}

func TestTrimSpace(t *testing.T) {
	result, err := trimSpace("  hello  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "hello" {
		t.Fatalf("expected %q, got %q", "hello", result)
	}
}

func TestTrimSpace_TabsAndNewlines(t *testing.T) {
	result, err := trimSpace("\t\n hello \n\t")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "hello" {
		t.Fatalf("expected %q, got %q", "hello", result)
	}
}
