// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package template

import (
	"fmt"
	"math"
	"regexp"
	"strconv"

	apierrors "github.com/michaelwang123/mountainKing/internal/errors"
)

// ParamSchema defines the runtime validation schema for a single template parameter.
// It contains pre-compiled regexp and typed default values, converted from the
// configuration-level TemplateParamConfig during template loading.
type ParamSchema struct {
	Name      string
	Type      string // "string", "int", "float", "boolean", "string[]"
	Required  bool
	Default   interface{} // typed default value (string/int64/float64/bool/[]string)
	Enum      []string
	MaxLength int            // default 1024
	MaxItems  int            // default 1000
	Pattern   *regexp.Regexp // pre-compiled pattern (nil = no constraint)
}

// validateParams validates and converts input parameters according to the given schemas.
// It returns a new map with type-converted values (the original map is not modified).
//
// Processing per schema entry:
//  1. Check if param exists in input
//  2. If missing and required → VALIDATION_MISSING_PARAMETER
//  3. If missing and has default → fill with default value
//  4. If missing, not required, no default → skip
//  5. Type validation and conversion
//  6. Constraint validation (enum, max_length, max_items, pattern)
func validateParams(params map[string]interface{}, schemas []ParamSchema) (map[string]interface{}, error) {
	// Create a copy so we don't modify the original.
	result := make(map[string]interface{}, len(params))
	for k, v := range params {
		result[k] = v
	}

	for _, schema := range schemas {
		val, exists := result[schema.Name]

		// Missing parameter handling.
		if !exists {
			if schema.Required {
				return nil, apierrors.ValidationError(
					apierrors.ErrValidationMissingParameter,
					fmt.Sprintf("required parameter %q is missing", schema.Name),
				)
			}
			if schema.Default != nil {
				result[schema.Name] = schema.Default
			}
			continue
		}

		// Type validation and conversion.
		converted, err := convertParamType(schema.Name, val, schema.Type)
		if err != nil {
			return nil, err
		}
		result[schema.Name] = converted

		// Constraint validation.
		if err := validateConstraints(schema, converted); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// convertParamType validates and converts a parameter value to the expected Go type.
func convertParamType(name string, val interface{}, schemaType string) (interface{}, error) {
	switch schemaType {
	case "string":
		return convertString(name, val)
	case "int":
		return convertInt(name, val)
	case "float":
		return convertFloat(name, val)
	case "boolean":
		return convertBoolean(name, val)
	case "string[]":
		return convertStringSlice(name, val)
	default:
		return nil, apierrors.ValidationError(
			apierrors.ErrValidationInvalidParameterType,
			fmt.Sprintf("parameter %q has unsupported type %q", name, schemaType),
		)
	}
}

// convertString validates that val is a string.
func convertString(name string, val interface{}) (string, error) {
	s, ok := val.(string)
	if !ok {
		return "", apierrors.ValidationError(
			apierrors.ErrValidationInvalidParameterType,
			fmt.Sprintf("parameter %q must be string, got %T", name, val),
		)
	}
	return s, nil
}

// convertInt validates and converts val to int64.
// Accepts: int, int64, float64 (no decimal part), string (parseable).
func convertInt(name string, val interface{}) (int64, error) {
	switch v := val.(type) {
	case int:
		return int64(v), nil
	case int64:
		return v, nil
	case float64:
		if v != math.Trunc(v) || math.IsNaN(v) || math.IsInf(v, 0) {
			return 0, apierrors.ValidationError(
				apierrors.ErrValidationInvalidParameterType,
				fmt.Sprintf("parameter %q must be int, got float64 with decimal part", name),
			)
		}
		return int64(v), nil
	case string:
		i, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return 0, apierrors.ValidationError(
				apierrors.ErrValidationInvalidParameterType,
				fmt.Sprintf("parameter %q must be int, got unparseable string %q", name, v),
			)
		}
		return i, nil
	default:
		return 0, apierrors.ValidationError(
			apierrors.ErrValidationInvalidParameterType,
			fmt.Sprintf("parameter %q must be int, got %T", name, val),
		)
	}
}

// convertFloat validates and converts val to float64.
// Accepts: float64, float32, int, int64, string (parseable).
func convertFloat(name string, val interface{}) (float64, error) {
	switch v := val.(type) {
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return 0, apierrors.ValidationError(
				apierrors.ErrValidationInvalidParameterType,
				fmt.Sprintf("parameter %q must be a finite float, got %v", name, v),
			)
		}
		return v, nil
	case float32:
		f := float64(v)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, apierrors.ValidationError(
				apierrors.ErrValidationInvalidParameterType,
				fmt.Sprintf("parameter %q must be a finite float, got %v", name, v),
			)
		}
		return f, nil
	case int:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case string:
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return 0, apierrors.ValidationError(
				apierrors.ErrValidationInvalidParameterType,
				fmt.Sprintf("parameter %q must be float, got unparseable string %q", name, v),
			)
		}
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, apierrors.ValidationError(
				apierrors.ErrValidationInvalidParameterType,
				fmt.Sprintf("parameter %q must be a finite float, got %v", name, f),
			)
		}
		return f, nil
	default:
		return 0, apierrors.ValidationError(
			apierrors.ErrValidationInvalidParameterType,
			fmt.Sprintf("parameter %q must be float, got %T", name, val),
		)
	}
}

// convertBoolean validates that val is a bool.
func convertBoolean(name string, val interface{}) (bool, error) {
	b, ok := val.(bool)
	if !ok {
		return false, apierrors.ValidationError(
			apierrors.ErrValidationInvalidParameterType,
			fmt.Sprintf("parameter %q must be boolean, got %T", name, val),
		)
	}
	return b, nil
}

// convertStringSlice validates and converts val to []string.
// Accepts: []string directly, or []interface{} where each element must be a string.
func convertStringSlice(name string, val interface{}) ([]string, error) {
	switch v := val.(type) {
	case []string:
		return v, nil
	case []interface{}:
		ss := make([]string, len(v))
		for i, elem := range v {
			s, ok := elem.(string)
			if !ok {
				return nil, apierrors.ValidationError(
					apierrors.ErrValidationInvalidParameterType,
					fmt.Sprintf("parameter %q[%d] must be string, got %T", name, i, elem),
				)
			}
			ss[i] = s
		}
		return ss, nil
	default:
		return nil, apierrors.ValidationError(
			apierrors.ErrValidationInvalidParameterType,
			fmt.Sprintf("parameter %q must be string[], got %T", name, val),
		)
	}
}

// validateConstraints checks enum, max_length, max_items, and pattern constraints.
func validateConstraints(schema ParamSchema, val interface{}) error {
	// Enum check (applies to all types that have enum defined).
	if len(schema.Enum) > 0 {
		strVal := fmt.Sprintf("%v", val)
		found := false
		for _, e := range schema.Enum {
			if strVal == e {
				found = true
				break
			}
		}
		if !found {
			return apierrors.ValidationError(
				apierrors.ErrValidationInvalidParameterValue,
				fmt.Sprintf("parameter %q value %q is not in allowed values %v", schema.Name, strVal, schema.Enum),
			)
		}
	}

	// MaxLength check for "string" type.
	if schema.Type == "string" {
		s, ok := val.(string)
		if ok && schema.MaxLength > 0 && len(s) > schema.MaxLength {
			return apierrors.ValidationError(
				apierrors.ErrValidationInvalidParameterValue,
				fmt.Sprintf("parameter %q length %d exceeds max_length %d", schema.Name, len(s), schema.MaxLength),
			)
		}
	}

	// MaxItems check for "string[]" type.
	if schema.Type == "string[]" {
		ss, ok := val.([]string)
		if ok && schema.MaxItems > 0 && len(ss) > schema.MaxItems {
			return apierrors.ValidationError(
				apierrors.ErrValidationInvalidParameterValue,
				fmt.Sprintf("parameter %q has %d items, exceeds max_items %d", schema.Name, len(ss), schema.MaxItems),
			)
		}
	}

	// Pattern check for "string" type.
	if schema.Type == "string" && schema.Pattern != nil {
		s, ok := val.(string)
		if ok && !schema.Pattern.MatchString(s) {
			return apierrors.ValidationError(
				apierrors.ErrValidationInvalidParameterValue,
				fmt.Sprintf("parameter %q value %q does not match pattern %q", schema.Name, s, schema.Pattern.String()),
			)
		}
	}

	return nil
}
