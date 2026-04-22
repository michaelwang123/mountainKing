// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package template

import (
	"fmt"
	"math"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"text/template"
)

// identifierSegmentRe validates a single identifier segment: [a-zA-Z0-9_]+
var identifierSegmentRe = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

// buildFuncMap returns the custom template.FuncMap containing all safety and
// utility functions registered for SQL template rendering.
func buildFuncMap() template.FuncMap {
	return template.FuncMap{
		// Safety functions
		"safeString":     safeString,
		"quote":          quote,
		"safeInt":        safeInt,
		"safeFloat":      safeFloat,
		"safeIdentifier": safeIdentifier,
		"safeInList":     safeInList,
		"safeLike":       safeLike,

		// Utility functions
		"join":      join,
		"default":   defaultFn,
		"upper":     upper,
		"lower":     lower,
		"trimSpace": trimSpace,
	}
}

// ---------------------------------------------------------------------------
// Safety functions
// ---------------------------------------------------------------------------

// safeString escapes a string for safe use in SQL single-quoted literals.
// Processing order: 1) remove NULL bytes  2) escape backslash  3) escape single quote.
// It does NOT add surrounding quotes — use quote() for that.
func safeString(v any) (string, error) {
	s := fmt.Sprint(v)
	// 1. Remove NULL bytes
	s = strings.ReplaceAll(s, "\x00", "")
	// 2. Escape backslash: \ → \\
	s = strings.ReplaceAll(s, `\`, `\\`)
	// 3. Escape single quote: ' → ''
	s = strings.ReplaceAll(s, "'", "''")
	return s, nil
}

// quote calls safeString then wraps the result in single quotes.
// Example: "O'Brien" → "'O”Brien'"
func quote(v any) (string, error) {
	escaped, err := safeString(v)
	if err != nil {
		return "", err
	}
	return "'" + escaped + "'", nil
}

// safeInt validates that the input is a valid integer and returns its string
// representation. Supports int, int64, float64 (no decimal part), and string
// (parseable as integer).
func safeInt(v any) (string, error) {
	switch val := v.(type) {
	case int:
		return strconv.FormatInt(int64(val), 10), nil
	case int64:
		return strconv.FormatInt(val, 10), nil
	case float64:
		if val != math.Trunc(val) || math.IsNaN(val) || math.IsInf(val, 0) {
			return "", fmt.Errorf("safeInt: %v is not a valid integer", v)
		}
		return strconv.FormatInt(int64(val), 10), nil
	case string:
		n, err := strconv.ParseInt(val, 10, 64)
		if err != nil {
			return "", fmt.Errorf("safeInt: %q is not a valid integer", val)
		}
		return strconv.FormatInt(n, 10), nil
	default:
		return "", fmt.Errorf("safeInt: unsupported type %T", v)
	}
}

// safeFloat validates that the input is a valid finite float and returns its
// string representation. Rejects NaN and ±Inf.
func safeFloat(v any) (string, error) {
	var f float64
	switch val := v.(type) {
	case float64:
		f = val
	case float32:
		f = float64(val)
	case int:
		f = float64(val)
	case int64:
		f = float64(val)
	case string:
		var err error
		f, err = strconv.ParseFloat(val, 64)
		if err != nil {
			return "", fmt.Errorf("safeFloat: %q is not a valid float", val)
		}
	default:
		return "", fmt.Errorf("safeFloat: unsupported type %T", v)
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return "", fmt.Errorf("safeFloat: %v is not a finite number", v)
	}
	return strconv.FormatFloat(f, 'f', -1, 64), nil
}

// safeIdentifier validates that the input contains only legal SQL identifier
// characters [a-zA-Z0-9_.], splits by '.', enforces max 2 segments, validates
// each segment is 1-64 chars, and wraps each segment in backticks.
// Examples: "abc" → "`abc`", "a.b" → "`a`.`b`"
func safeIdentifier(v any) (string, error) {
	s := fmt.Sprint(v)
	if s == "" {
		return "", fmt.Errorf("safeIdentifier: empty identifier")
	}

	segments := strings.Split(s, ".")
	if len(segments) > 2 {
		return "", fmt.Errorf("safeIdentifier: too many segments (max 2): %q", s)
	}

	parts := make([]string, 0, len(segments))
	for _, seg := range segments {
		if len(seg) == 0 || len(seg) > 64 {
			return "", fmt.Errorf("safeIdentifier: segment length must be 1-64 chars, got %d: %q", len(seg), seg)
		}
		if !identifierSegmentRe.MatchString(seg) {
			return "", fmt.Errorf("safeIdentifier: illegal characters in %q", seg)
		}
		parts = append(parts, "`"+seg+"`")
	}
	return strings.Join(parts, "."), nil
}

// safeInList accepts a string slice ([]string or []any where each
// element is a string) and returns a comma-separated list of single-quoted,
// escaped values suitable for an SQL IN clause.
// Empty slices return an error because IN () is invalid SQL in StarRocks.
// Example: ["a", "b's"] → "'a','b”s'"
func safeInList(v any) (string, error) {
	var items []string

	switch val := v.(type) {
	case []string:
		items = val
	case []any:
		items = make([]string, 0, len(val))
		for i, elem := range val {
			s, ok := elem.(string)
			if !ok {
				return "", fmt.Errorf("safeInList: element %d is %T, expected string", i, elem)
			}
			items = append(items, s)
		}
	default:
		return "", fmt.Errorf("safeInList: unsupported type %T, expected []string or []any", v)
	}

	if len(items) == 0 {
		return "", fmt.Errorf("safeInList: empty slice (IN () is invalid SQL)")
	}

	parts := make([]string, 0, len(items))
	for _, item := range items {
		escaped, err := safeString(item)
		if err != nil {
			return "", err
		}
		parts = append(parts, "'"+escaped+"'")
	}
	return strings.Join(parts, ","), nil
}

// safeLike escapes LIKE wildcards in a string.
// Processing order: 1) \ → \\  2) % → \%  3) _ → \_
// Must be used with ESCAPE '\\' in the SQL LIKE clause.
func safeLike(v any) (string, error) {
	s := fmt.Sprint(v)
	// 1. Escape backslash first
	s = strings.ReplaceAll(s, `\`, `\\`)
	// 2. Escape percent
	s = strings.ReplaceAll(s, "%", `\%`)
	// 3. Escape underscore
	s = strings.ReplaceAll(s, "_", `\_`)
	return s, nil
}

// ---------------------------------------------------------------------------
// Utility functions
// ---------------------------------------------------------------------------

// join joins a string slice with comma separator.
func join(v any) (string, error) {
	switch val := v.(type) {
	case []string:
		return strings.Join(val, ","), nil
	case []any:
		parts := make([]string, 0, len(val))
		for _, elem := range val {
			parts = append(parts, fmt.Sprint(elem))
		}
		return strings.Join(parts, ","), nil
	default:
		return "", fmt.Errorf("join: unsupported type %T, expected []string or []any", v)
	}
}

// defaultFn returns defaultVal if val is a zero value, otherwise returns val.
// Note: in template pipelines, val is passed as the second argument:
//
//	{{.Params.limit | default 100}}
func defaultFn(defaultVal, val any) any {
	if val == nil {
		return defaultVal
	}
	v := reflect.ValueOf(val)
	if !v.IsValid() || v.IsZero() {
		return defaultVal
	}
	return val
}

// upper converts a string to uppercase.
func upper(v any) (string, error) {
	return strings.ToUpper(fmt.Sprint(v)), nil
}

// lower converts a string to lowercase.
func lower(v any) (string, error) {
	return strings.ToLower(fmt.Sprint(v)), nil
}

// trimSpace removes leading and trailing whitespace from a string.
func trimSpace(v any) (string, error) {
	return strings.TrimSpace(fmt.Sprint(v)), nil
}
