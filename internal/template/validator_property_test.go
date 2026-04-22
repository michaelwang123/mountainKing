// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package template

import (
	"regexp"
	"strings"
	"testing"

	apierrors "github.com/michaelwang123/mountainKing/internal/errors"
	"pgregory.net/rapid"
)

// =============================================================================
// Feature: sql-template-engine
// Task 5.2: 参数校验器单元测试和属性测试
// =============================================================================

// helper: check if error has the expected error code.
func hasErrorCode(err error, code string) bool {
	if err == nil {
		return false
	}
	apiErr, ok := err.(*apierrors.APIError)
	if !ok {
		return false
	}
	return apiErr.Code == code
}

// =============================================================================
// Property 41: 必填参数检查
// **Validates: Requirements 7.2**
// For any required parameter not provided, validateParams returns
// VALIDATION_MISSING_PARAMETER.
// =============================================================================

func TestProperty41_MissingRequiredParams(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		paramName := rapid.StringMatching(`[a-z][a-z0-9_]{0,10}`).Draw(rt, "paramName")
		schemaType := rapid.SampledFrom([]string{"string", "int", "float", "boolean", "string[]"}).Draw(rt, "type")

		schemas := []ParamSchema{
			{
				Name:      paramName,
				Type:      schemaType,
				Required:  true,
				MaxLength: 1024,
				MaxItems:  1000,
			},
		}

		// Empty params map — required param is missing.
		_, err := validateParams(map[string]any{}, schemas)
		if err == nil {
			rt.Fatalf("expected VALIDATION_MISSING_PARAMETER for missing required param %q", paramName)
		}
		if !hasErrorCode(err, apierrors.ErrValidationMissingParameter) {
			rt.Fatalf("expected error code %s, got: %v", apierrors.ErrValidationMissingParameter, err)
		}
	})
}

// =============================================================================
// Property 42: 类型匹配检查
// **Validates: Requirements 7.4**
// For any parameter with a type mismatch, validateParams returns
// VALIDATION_INVALID_PARAMETER_TYPE.
// =============================================================================

func TestProperty42_TypeMismatch(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		paramName := rapid.StringMatching(`[a-z][a-z0-9_]{0,10}`).Draw(rt, "paramName")

		// Generate a schema type and a mismatched value.
		schemaType := rapid.SampledFrom([]string{"string", "int", "float", "boolean", "string[]"}).Draw(rt, "type")

		var mismatchedVal any
		switch schemaType {
		case "string":
			// Provide a non-string value.
			mismatchedVal = rapid.Int64().Draw(rt, "mismatch")
		case "int":
			// Provide a bool (not convertible to int).
			mismatchedVal = true
		case "float":
			// Provide a bool (not convertible to float).
			mismatchedVal = true
		case "boolean":
			// Provide a string (not a bool).
			mismatchedVal = "not_a_bool"
		case "string[]":
			// Provide a plain string (not a slice).
			mismatchedVal = "not_a_slice"
		}

		schemas := []ParamSchema{
			{
				Name:      paramName,
				Type:      schemaType,
				Required:  true,
				MaxLength: 1024,
				MaxItems:  1000,
			},
		}

		params := map[string]any{paramName: mismatchedVal}
		_, err := validateParams(params, schemas)
		if err == nil {
			rt.Fatalf("expected VALIDATION_INVALID_PARAMETER_TYPE for type %q with value %v (%T)", schemaType, mismatchedVal, mismatchedVal)
		}
		if !hasErrorCode(err, apierrors.ErrValidationInvalidParameterType) {
			rt.Fatalf("expected error code %s, got: %v", apierrors.ErrValidationInvalidParameterType, err)
		}
	})
}

// =============================================================================
// Property 43: 默认值填充
// **Validates: Requirements 7.5**
// For any optional parameter with a default, when not provided,
// the result map contains the default value.
// =============================================================================

func TestProperty43_DefaultValueFilling(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		paramName := rapid.StringMatching(`[a-z][a-z0-9_]{0,10}`).Draw(rt, "paramName")
		defaultStr := rapid.StringMatching(`[a-zA-Z0-9]{1,20}`).Draw(rt, "default")

		schemas := []ParamSchema{
			{
				Name:      paramName,
				Type:      "string",
				Required:  false,
				Default:   defaultStr,
				MaxLength: 1024,
			},
		}

		// Empty params — default should be filled.
		result, err := validateParams(map[string]any{}, schemas)
		if err != nil {
			rt.Fatalf("unexpected error: %v", err)
		}
		val, exists := result[paramName]
		if !exists {
			rt.Fatalf("expected default value for param %q to be filled", paramName)
		}
		if val != defaultStr {
			rt.Fatalf("expected default %q, got %v", defaultStr, val)
		}
	})
}

// =============================================================================
// Property 44: 枚举约束
// **Validates: Requirements 7.6**
// For any string parameter with enum constraint, values not in enum
// return VALIDATION_INVALID_PARAMETER_VALUE.
// =============================================================================

func TestProperty44_EnumConstraint(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		paramName := rapid.StringMatching(`[a-z][a-z0-9_]{0,10}`).Draw(rt, "paramName")

		// Generate a small enum set.
		enumSize := rapid.IntRange(2, 5).Draw(rt, "enumSize")
		enumVals := make([]string, enumSize)
		for i := 0; i < enumSize; i++ {
			enumVals[i] = rapid.StringMatching(`[a-z]{3,8}`).Draw(rt, "enumVal")
		}

		// Generate a value NOT in the enum.
		invalidVal := rapid.StringMatching(`[A-Z]{10,15}`).Draw(rt, "invalidVal")
		// Ensure it's truly not in the enum.
		inEnum := false
		for _, e := range enumVals {
			if e == invalidVal {
				inEnum = true
				break
			}
		}
		if inEnum {
			return // skip this iteration
		}

		schemas := []ParamSchema{
			{
				Name:      paramName,
				Type:      "string",
				Required:  true,
				Enum:      enumVals,
				MaxLength: 1024,
			},
		}

		params := map[string]any{paramName: invalidVal}
		_, err := validateParams(params, schemas)
		if err == nil {
			rt.Fatalf("expected VALIDATION_INVALID_PARAMETER_VALUE for value %q not in enum %v", invalidVal, enumVals)
		}
		if !hasErrorCode(err, apierrors.ErrValidationInvalidParameterValue) {
			rt.Fatalf("expected error code %s, got: %v", apierrors.ErrValidationInvalidParameterValue, err)
		}
	})
}

// =============================================================================
// Property 45: 字符串长度约束
// **Validates: Requirements 7.7**
// For any string parameter exceeding max_length, validateParams returns
// VALIDATION_INVALID_PARAMETER_VALUE.
// =============================================================================

func TestProperty45_StringLengthConstraint(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		paramName := rapid.StringMatching(`[a-z][a-z0-9_]{0,10}`).Draw(rt, "paramName")
		maxLen := rapid.IntRange(1, 50).Draw(rt, "maxLength")

		// Generate a string that exceeds maxLen.
		overLen := maxLen + rapid.IntRange(1, 50).Draw(rt, "excess")
		longStr := strings.Repeat("a", overLen)

		schemas := []ParamSchema{
			{
				Name:      paramName,
				Type:      "string",
				Required:  true,
				MaxLength: maxLen,
			},
		}

		params := map[string]any{paramName: longStr}
		_, err := validateParams(params, schemas)
		if err == nil {
			rt.Fatalf("expected VALIDATION_INVALID_PARAMETER_VALUE for string length %d > max_length %d", overLen, maxLen)
		}
		if !hasErrorCode(err, apierrors.ErrValidationInvalidParameterValue) {
			rt.Fatalf("expected error code %s, got: %v", apierrors.ErrValidationInvalidParameterValue, err)
		}
	})
}

// =============================================================================
// Property 46: 数组大小约束
// **Validates: Requirements 7.8**
// For any string[] parameter exceeding max_items, validateParams returns
// VALIDATION_INVALID_PARAMETER_VALUE.
// =============================================================================

func TestProperty46_ArraySizeConstraint(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		paramName := rapid.StringMatching(`[a-z][a-z0-9_]{0,10}`).Draw(rt, "paramName")
		maxItems := rapid.IntRange(1, 20).Draw(rt, "maxItems")

		// Generate a slice that exceeds maxItems.
		overSize := maxItems + rapid.IntRange(1, 10).Draw(rt, "excess")
		items := make([]string, overSize)
		for i := range items {
			items[i] = "item"
		}

		schemas := []ParamSchema{
			{
				Name:     paramName,
				Type:     "string[]",
				Required: true,
				MaxItems: maxItems,
			},
		}

		params := map[string]any{paramName: items}
		_, err := validateParams(params, schemas)
		if err == nil {
			rt.Fatalf("expected VALIDATION_INVALID_PARAMETER_VALUE for array size %d > max_items %d", overSize, maxItems)
		}
		if !hasErrorCode(err, apierrors.ErrValidationInvalidParameterValue) {
			rt.Fatalf("expected error code %s, got: %v", apierrors.ErrValidationInvalidParameterValue, err)
		}
	})
}

// =============================================================================
// Property 47: 正则约束
// **Validates: Requirements 7.9**
// For any string parameter not matching pattern, validateParams returns
// VALIDATION_INVALID_PARAMETER_VALUE.
// =============================================================================

func TestProperty47_PatternConstraint(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		paramName := rapid.StringMatching(`[a-z][a-z0-9_]{0,10}`).Draw(rt, "paramName")

		// Use a date pattern: ^\d{4}-\d{2}-\d{2}$
		pattern := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

		// Generate a string that does NOT match the date pattern.
		invalidVal := rapid.StringMatching(`[a-zA-Z]{5,15}`).Draw(rt, "invalidVal")

		schemas := []ParamSchema{
			{
				Name:      paramName,
				Type:      "string",
				Required:  true,
				MaxLength: 1024,
				Pattern:   pattern,
			},
		}

		params := map[string]any{paramName: invalidVal}
		_, err := validateParams(params, schemas)
		if err == nil {
			rt.Fatalf("expected VALIDATION_INVALID_PARAMETER_VALUE for value %q not matching pattern %q", invalidVal, pattern.String())
		}
		if !hasErrorCode(err, apierrors.ErrValidationInvalidParameterValue) {
			rt.Fatalf("expected error code %s, got: %v", apierrors.ErrValidationInvalidParameterValue, err)
		}
	})
}

// =============================================================================
// Unit Tests
// =============================================================================

// 1. Valid params pass validation.
func TestValidateParams_ValidParams(t *testing.T) {
	schemas := []ParamSchema{
		{Name: "name", Type: "string", Required: true, MaxLength: 1024},
		{Name: "age", Type: "int", Required: true},
		{Name: "score", Type: "float", Required: false, Default: float64(0.0)},
	}
	params := map[string]any{
		"name": "Alice",
		"age":  float64(30), // JSON number
	}
	result, err := validateParams(params, schemas)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["name"] != "Alice" {
		t.Fatalf("expected name=Alice, got %v", result["name"])
	}
	if result["age"] != int64(30) {
		t.Fatalf("expected age=30 (int64), got %v (%T)", result["age"], result["age"])
	}
	if result["score"] != float64(0.0) {
		t.Fatalf("expected score=0.0 (default), got %v", result["score"])
	}
}

// 2. Required param missing returns error.
func TestValidateParams_RequiredMissing(t *testing.T) {
	schemas := []ParamSchema{
		{Name: "id", Type: "string", Required: true, MaxLength: 1024},
	}
	_, err := validateParams(map[string]any{}, schemas)
	if err == nil {
		t.Fatal("expected error for missing required param")
	}
	if !hasErrorCode(err, apierrors.ErrValidationMissingParameter) {
		t.Fatalf("expected %s, got: %v", apierrors.ErrValidationMissingParameter, err)
	}
}

// 3. Optional param missing (no default) is skipped.
func TestValidateParams_OptionalMissingNoDefault(t *testing.T) {
	schemas := []ParamSchema{
		{Name: "optional_field", Type: "string", Required: false, MaxLength: 1024},
	}
	result, err := validateParams(map[string]any{}, schemas)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, exists := result["optional_field"]; exists {
		t.Fatal("optional param without default should not be in result")
	}
}

// 4. Default value filled for missing optional param.
func TestValidateParams_DefaultValueFilled(t *testing.T) {
	schemas := []ParamSchema{
		{Name: "period", Type: "string", Required: false, Default: "monthly", MaxLength: 1024},
	}
	result, err := validateParams(map[string]any{}, schemas)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["period"] != "monthly" {
		t.Fatalf("expected period=monthly, got %v", result["period"])
	}
}

// 5. String type validation.
func TestValidateParams_StringType(t *testing.T) {
	schemas := []ParamSchema{
		{Name: "s", Type: "string", Required: true, MaxLength: 1024},
	}
	// Valid string.
	result, err := validateParams(map[string]any{"s": "hello"}, schemas)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["s"] != "hello" {
		t.Fatalf("expected hello, got %v", result["s"])
	}

	// Invalid: non-string.
	_, err = validateParams(map[string]any{"s": 123}, schemas)
	if err == nil {
		t.Fatal("expected error for non-string value")
	}
	if !hasErrorCode(err, apierrors.ErrValidationInvalidParameterType) {
		t.Fatalf("expected %s, got: %v", apierrors.ErrValidationInvalidParameterType, err)
	}
}

// 6. Int type validation (int, int64, float64 whole, string).
func TestValidateParams_IntType(t *testing.T) {
	schemas := []ParamSchema{
		{Name: "n", Type: "int", Required: true},
	}

	tests := []struct {
		name     string
		input    any
		expected int64
	}{
		{"int", 42, 42},
		{"int64", int64(100), 100},
		{"float64_whole", float64(50), 50},
		{"string", "123", 123},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := validateParams(map[string]any{"n": tc.input}, schemas)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result["n"] != tc.expected {
				t.Fatalf("expected %d, got %v (%T)", tc.expected, result["n"], result["n"])
			}
		})
	}

	// Invalid: float with decimal.
	_, err := validateParams(map[string]any{"n": 3.14}, schemas)
	if err == nil {
		t.Fatal("expected error for float with decimal part")
	}
	if !hasErrorCode(err, apierrors.ErrValidationInvalidParameterType) {
		t.Fatalf("expected %s, got: %v", apierrors.ErrValidationInvalidParameterType, err)
	}
}

// 7. Float type validation.
func TestValidateParams_FloatType(t *testing.T) {
	schemas := []ParamSchema{
		{Name: "f", Type: "float", Required: true},
	}
	result, err := validateParams(map[string]any{"f": 3.14}, schemas)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["f"] != float64(3.14) {
		t.Fatalf("expected 3.14, got %v", result["f"])
	}

	// Invalid: bool is not a float.
	_, err = validateParams(map[string]any{"f": true}, schemas)
	if err == nil {
		t.Fatal("expected error for bool as float")
	}
	if !hasErrorCode(err, apierrors.ErrValidationInvalidParameterType) {
		t.Fatalf("expected %s, got: %v", apierrors.ErrValidationInvalidParameterType, err)
	}
}

// 8. Boolean type validation.
func TestValidateParams_BooleanType(t *testing.T) {
	schemas := []ParamSchema{
		{Name: "b", Type: "boolean", Required: true},
	}
	result, err := validateParams(map[string]any{"b": true}, schemas)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["b"] != true {
		t.Fatalf("expected true, got %v", result["b"])
	}

	// Invalid: string is not a bool.
	_, err = validateParams(map[string]any{"b": "true"}, schemas)
	if err == nil {
		t.Fatal("expected error for string as boolean")
	}
	if !hasErrorCode(err, apierrors.ErrValidationInvalidParameterType) {
		t.Fatalf("expected %s, got: %v", apierrors.ErrValidationInvalidParameterType, err)
	}
}

// 9. String[] type validation ([]string and []any).
func TestValidateParams_StringSliceType(t *testing.T) {
	schemas := []ParamSchema{
		{Name: "ids", Type: "string[]", Required: true, MaxItems: 1000},
	}

	// []string input.
	result, err := validateParams(map[string]any{"ids": []string{"a", "b"}}, schemas)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ids, ok := result["ids"].([]string)
	if !ok {
		t.Fatalf("expected []string, got %T", result["ids"])
	}
	if len(ids) != 2 || ids[0] != "a" || ids[1] != "b" {
		t.Fatalf("expected [a b], got %v", ids)
	}

	// []any input with string elements.
	result, err = validateParams(map[string]any{"ids": []any{"x", "y"}}, schemas)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ids, ok = result["ids"].([]string)
	if !ok {
		t.Fatalf("expected []string, got %T", result["ids"])
	}
	if len(ids) != 2 || ids[0] != "x" || ids[1] != "y" {
		t.Fatalf("expected [x y], got %v", ids)
	}

	// Invalid: plain string.
	_, err = validateParams(map[string]any{"ids": "not_a_slice"}, schemas)
	if err == nil {
		t.Fatal("expected error for non-slice value")
	}
	if !hasErrorCode(err, apierrors.ErrValidationInvalidParameterType) {
		t.Fatalf("expected %s, got: %v", apierrors.ErrValidationInvalidParameterType, err)
	}
}

// 10. Enum constraint.
func TestValidateParams_EnumConstraint(t *testing.T) {
	schemas := []ParamSchema{
		{Name: "period", Type: "string", Required: true, Enum: []string{"daily", "weekly", "monthly"}, MaxLength: 1024},
	}

	// Valid enum value.
	result, err := validateParams(map[string]any{"period": "daily"}, schemas)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["period"] != "daily" {
		t.Fatalf("expected daily, got %v", result["period"])
	}

	// Invalid enum value.
	_, err = validateParams(map[string]any{"period": "yearly"}, schemas)
	if err == nil {
		t.Fatal("expected error for invalid enum value")
	}
	if !hasErrorCode(err, apierrors.ErrValidationInvalidParameterValue) {
		t.Fatalf("expected %s, got: %v", apierrors.ErrValidationInvalidParameterValue, err)
	}
}

// 11. MaxLength constraint.
func TestValidateParams_MaxLengthConstraint(t *testing.T) {
	schemas := []ParamSchema{
		{Name: "name", Type: "string", Required: true, MaxLength: 10},
	}

	// Within limit.
	_, err := validateParams(map[string]any{"name": "short"}, schemas)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Exceeds limit.
	_, err = validateParams(map[string]any{"name": "this_is_too_long"}, schemas)
	if err == nil {
		t.Fatal("expected error for string exceeding max_length")
	}
	if !hasErrorCode(err, apierrors.ErrValidationInvalidParameterValue) {
		t.Fatalf("expected %s, got: %v", apierrors.ErrValidationInvalidParameterValue, err)
	}
}

// 12. MaxItems constraint.
func TestValidateParams_MaxItemsConstraint(t *testing.T) {
	schemas := []ParamSchema{
		{Name: "ids", Type: "string[]", Required: true, MaxItems: 3},
	}

	// Within limit.
	_, err := validateParams(map[string]any{"ids": []string{"a", "b"}}, schemas)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Exceeds limit.
	_, err = validateParams(map[string]any{"ids": []string{"a", "b", "c", "d"}}, schemas)
	if err == nil {
		t.Fatal("expected error for array exceeding max_items")
	}
	if !hasErrorCode(err, apierrors.ErrValidationInvalidParameterValue) {
		t.Fatalf("expected %s, got: %v", apierrors.ErrValidationInvalidParameterValue, err)
	}
}

// 13. Pattern constraint.
func TestValidateParams_PatternConstraint(t *testing.T) {
	datePattern := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	schemas := []ParamSchema{
		{Name: "date", Type: "string", Required: true, MaxLength: 1024, Pattern: datePattern},
	}

	// Valid pattern match.
	_, err := validateParams(map[string]any{"date": "2024-01-15"}, schemas)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Invalid pattern.
	_, err = validateParams(map[string]any{"date": "not-a-date"}, schemas)
	if err == nil {
		t.Fatal("expected error for pattern mismatch")
	}
	if !hasErrorCode(err, apierrors.ErrValidationInvalidParameterValue) {
		t.Fatalf("expected %s, got: %v", apierrors.ErrValidationInvalidParameterValue, err)
	}
}

// 14. Multiple schemas validated.
func TestValidateParams_MultipleSchemas(t *testing.T) {
	schemas := []ParamSchema{
		{Name: "eerid", Type: "string", Required: true, MaxLength: 64},
		{Name: "period", Type: "string", Required: false, Default: "monthly", Enum: []string{"daily", "weekly", "monthly"}, MaxLength: 1024},
		{Name: "limit", Type: "int", Required: false, Default: int64(100)},
	}

	params := map[string]any{
		"eerid": "EER001",
	}

	result, err := validateParams(params, schemas)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["eerid"] != "EER001" {
		t.Fatalf("expected eerid=EER001, got %v", result["eerid"])
	}
	if result["period"] != "monthly" {
		t.Fatalf("expected period=monthly (default), got %v", result["period"])
	}
	if result["limit"] != int64(100) {
		t.Fatalf("expected limit=100 (default), got %v (%T)", result["limit"], result["limit"])
	}
}

// 15. Original params map not modified.
func TestValidateParams_OriginalNotModified(t *testing.T) {
	schemas := []ParamSchema{
		{Name: "name", Type: "string", Required: true, MaxLength: 1024},
		{Name: "opt", Type: "string", Required: false, Default: "default_val", MaxLength: 1024},
	}

	original := map[string]any{
		"name": "Alice",
	}

	result, err := validateParams(original, schemas)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Result should have the default filled.
	if result["opt"] != "default_val" {
		t.Fatalf("expected opt=default_val in result, got %v", result["opt"])
	}

	// Original should NOT have the default.
	if _, exists := original["opt"]; exists {
		t.Fatal("original params map should not be modified")
	}
}
