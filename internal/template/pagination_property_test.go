// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package template

import (
	"fmt"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// =============================================================================
// Feature: sql-template-engine
// Task 9.2: 分页包装器单元测试和属性测试
// =============================================================================

// =============================================================================
// Property 17: LIMIT 参数化
// **Validates: Requirements 4.7**
// For any pagination query, the LIMIT value is always passed as a parameterized
// arg (not embedded in the SQL string).
// =============================================================================

func TestProperty17_LIMITParameterized(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		first := rapid.IntRange(1, 10000).Draw(rt, "first")
		maxRows := rapid.IntRange(100, 50000).Draw(rt, "maxRows")
		baseSQL := "SELECT * FROM test_table WHERE id > 0"

		sql, args, err := wrapWithPagination(baseSQL, nil, nil, &first, nil, maxRows)
		if err != nil {
			rt.Fatalf("wrapWithPagination returned error: %v", err)
		}

		// SQL must contain "LIMIT ?" placeholder, not the actual value
		if !strings.Contains(sql, "LIMIT ?") {
			rt.Fatalf("SQL should contain 'LIMIT ?' placeholder, got: %s", sql)
		}
		// The actual LIMIT value (first+1) must be in args, not in SQL
		expectedLimit := first + 1
		if !strings.Contains(sql, fmt.Sprintf("LIMIT %d", expectedLimit)) {
			// Good — the value is not in the SQL string
		} else {
			rt.Fatalf("LIMIT value %d should not appear in SQL string: %s", expectedLimit, sql)
		}
		// Verify args[0] is the LIMIT value
		if len(args) < 1 {
			rt.Fatalf("expected at least 1 arg, got %d", len(args))
		}
		if args[0] != expectedLimit {
			rt.Fatalf("expected args[0] = %d, got %v", expectedLimit, args[0])
		}
	})
}

// =============================================================================
// Property 18: OFFSET 参数化
// **Validates: Requirements 4.7**
// For any pagination query, the OFFSET value is always passed as a parameterized
// arg (not embedded in the SQL string).
// =============================================================================

func TestProperty18_OFFSETParameterized(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		offset := rapid.IntRange(0, 100000).Draw(rt, "offset")
		maxRows := rapid.IntRange(100, 50000).Draw(rt, "maxRows")
		baseSQL := "SELECT * FROM test_table WHERE id > 0"

		sql, args, err := wrapWithPagination(baseSQL, nil, nil, nil, &offset, maxRows)
		if err != nil {
			rt.Fatalf("wrapWithPagination returned error: %v", err)
		}

		// SQL must contain "OFFSET ?" placeholder
		if !strings.Contains(sql, "OFFSET ?") {
			rt.Fatalf("SQL should contain 'OFFSET ?' placeholder, got: %s", sql)
		}
		// Verify args[1] is the OFFSET value
		if len(args) < 2 {
			rt.Fatalf("expected at least 2 args, got %d", len(args))
		}
		if args[1] != offset {
			rt.Fatalf("expected args[1] = %d, got %v", offset, args[1])
		}
	})
}

// =============================================================================
// Property 19: 默认 LIMIT 强制
// **Validates: Requirements 4.6**
// When first is nil, LIMIT = maxResultRows + 1 (over-fetch).
// =============================================================================

func TestProperty19_DefaultLIMITEnforced(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		maxRows := rapid.IntRange(100, 50000).Draw(rt, "maxRows")
		baseSQL := "SELECT * FROM test_table"

		_, args, err := wrapWithPagination(baseSQL, nil, nil, nil, nil, maxRows)
		if err != nil {
			rt.Fatalf("wrapWithPagination returned error: %v", err)
		}

		expectedLimit := maxRows + 1
		if len(args) < 1 {
			rt.Fatalf("expected at least 1 arg, got %d", len(args))
		}
		if args[0] != expectedLimit {
			rt.Fatalf("expected LIMIT = maxResultRows+1 = %d, got %v", expectedLimit, args[0])
		}
	})
}

// =============================================================================
// Property 20: 字段选择安全性
// **Validates: Requirements 4.3**
// Invalid field names return VALIDATION_INVALID_FIELD error.
// =============================================================================

func TestProperty20_FieldSelectionSafety(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate an invalid field name containing illegal characters
		illegalChar := rapid.SampledFrom([]string{";", "'", "\"", " ", "(", ")", "-", "!", "@", "#"}).Draw(rt, "illegalChar")
		prefix := rapid.StringMatching(`[a-zA-Z]{1,5}`).Draw(rt, "prefix")
		invalidField := prefix + illegalChar

		fields := []string{invalidField}
		baseSQL := "SELECT * FROM test_table"

		_, _, err := wrapWithPagination(baseSQL, fields, nil, nil, nil, 1000)
		if err == nil {
			rt.Fatalf("expected error for invalid field %q, got nil", invalidField)
		}
		if !strings.Contains(err.Error(), "VALIDATION_INVALID_FIELD") {
			rt.Fatalf("expected VALIDATION_INVALID_FIELD error for field %q, got: %v", invalidField, err)
		}
	})
}

// =============================================================================
// Property 21: OrderBy 字段安全性
// **Validates: Requirements 4.4**
// Invalid orderBy field names return VALIDATION_INVALID_FIELD error.
// =============================================================================

func TestProperty21_OrderByFieldSafety(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		illegalChar := rapid.SampledFrom([]string{";", "'", "\"", " ", "(", ")", "-", "!", "@", "#"}).Draw(rt, "illegalChar")
		prefix := rapid.StringMatching(`[a-zA-Z]{1,5}`).Draw(rt, "prefix")
		invalidField := prefix + illegalChar
		direction := rapid.SampledFrom([]string{"ASC", "DESC"}).Draw(rt, "direction")

		orderBy := []TemplateOrderByParam{{Field: invalidField, Direction: direction}}
		baseSQL := "SELECT * FROM test_table"

		_, _, err := wrapWithPagination(baseSQL, nil, orderBy, nil, nil, 1000)
		if err == nil {
			rt.Fatalf("expected error for invalid orderBy field %q, got nil", invalidField)
		}
		if !strings.Contains(err.Error(), "VALIDATION_INVALID_FIELD") {
			rt.Fatalf("expected VALIDATION_INVALID_FIELD error for orderBy field %q, got: %v", invalidField, err)
		}
	})
}

// =============================================================================
// Property 22: totalCount 独立性
// **Validates: Requirements 4.5**
// wrapWithCount produces an independent COUNT query that does not include
// LIMIT/OFFSET.
// =============================================================================

func TestProperty22_TotalCountIndependence(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		tableName := rapid.StringMatching(`[a-z]{3,10}`).Draw(rt, "tableName")
		baseSQL := fmt.Sprintf("SELECT * FROM %s WHERE id > 0", tableName)

		countSQL := wrapWithCount(baseSQL)

		// Must contain COUNT(*)
		if !strings.Contains(countSQL, "COUNT(*)") {
			rt.Fatalf("wrapWithCount should contain COUNT(*), got: %s", countSQL)
		}
		// Must contain the alias
		if !strings.Contains(countSQL, "__tq_cnt__") {
			rt.Fatalf("wrapWithCount should contain __tq_cnt__ alias, got: %s", countSQL)
		}
		// Must NOT contain LIMIT or OFFSET
		if strings.Contains(strings.ToUpper(countSQL), "LIMIT") {
			rt.Fatalf("wrapWithCount should not contain LIMIT, got: %s", countSQL)
		}
		if strings.Contains(strings.ToUpper(countSQL), "OFFSET") {
			rt.Fatalf("wrapWithCount should not contain OFFSET, got: %s", countSQL)
		}
		// Must wrap the original SQL
		if !strings.Contains(countSQL, baseSQL) {
			rt.Fatalf("wrapWithCount should wrap the original SQL, got: %s", countSQL)
		}
	})
}

// =============================================================================
// Unit Tests
// =============================================================================

// Test 1: Basic pagination with first and offset
func TestPagination_BasicFirstAndOffset(t *testing.T) {
	first := 20
	offset := 40
	sql, args, err := wrapWithPagination("SELECT * FROM users", nil, nil, &first, &offset, 10000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Over-fetch: LIMIT = first + 1 = 21
	if args[0] != 21 {
		t.Fatalf("expected LIMIT=21 (over-fetch), got %v", args[0])
	}
	if args[1] != 40 {
		t.Fatalf("expected OFFSET=40, got %v", args[1])
	}
	if !strings.Contains(sql, "LIMIT ? OFFSET ?") {
		t.Fatalf("expected parameterized LIMIT/OFFSET, got: %s", sql)
	}
	if !strings.Contains(sql, "__tq_wrapper__") {
		t.Fatalf("expected __tq_wrapper__ alias, got: %s", sql)
	}
}

// Test 2: Pagination without first (uses maxResultRows)
func TestPagination_WithoutFirst(t *testing.T) {
	offset := 10
	sql, args, err := wrapWithPagination("SELECT * FROM users", nil, nil, nil, &offset, 5000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// LIMIT = maxResultRows + 1 = 5001
	if args[0] != 5001 {
		t.Fatalf("expected LIMIT=5001 (maxResultRows+1), got %v", args[0])
	}
	if args[1] != 10 {
		t.Fatalf("expected OFFSET=10, got %v", args[1])
	}
	if !strings.Contains(sql, "LIMIT ? OFFSET ?") {
		t.Fatalf("expected parameterized LIMIT/OFFSET, got: %s", sql)
	}
}

// Test 3: Pagination without offset (defaults to 0)
func TestPagination_WithoutOffset(t *testing.T) {
	first := 10
	_, args, err := wrapWithPagination("SELECT * FROM users", nil, nil, &first, nil, 10000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if args[0] != 11 {
		t.Fatalf("expected LIMIT=11, got %v", args[0])
	}
	if args[1] != 0 {
		t.Fatalf("expected OFFSET=0 (default), got %v", args[1])
	}
}

// Test 4: Field selection with valid fields
func TestPagination_FieldSelectionValid(t *testing.T) {
	fields := []string{"name", "age", "email"}
	sql, _, err := wrapWithPagination("SELECT * FROM users", fields, nil, nil, nil, 10000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(sql, "`name`, `age`, `email`") {
		t.Fatalf("expected backtick-wrapped fields, got: %s", sql)
	}
}

// Test 5: Field selection with invalid field returns error
func TestPagination_FieldSelectionInvalid(t *testing.T) {
	fields := []string{"valid_field", "invalid;field"}
	_, _, err := wrapWithPagination("SELECT * FROM users", fields, nil, nil, nil, 10000)
	if err == nil {
		t.Fatal("expected error for invalid field name")
	}
	if !strings.Contains(err.Error(), "VALIDATION_INVALID_FIELD") {
		t.Fatalf("expected VALIDATION_INVALID_FIELD error, got: %v", err)
	}
}

// Test 6: OrderBy with valid fields
func TestPagination_OrderByValid(t *testing.T) {
	orderBy := []TemplateOrderByParam{
		{Field: "created_at", Direction: "DESC"},
		{Field: "name", Direction: "ASC"},
	}
	sql, _, err := wrapWithPagination("SELECT * FROM users", nil, orderBy, nil, nil, 10000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(sql, "ORDER BY `created_at` DESC, `name` ASC") {
		t.Fatalf("expected ORDER BY clause with backtick-wrapped fields, got: %s", sql)
	}
}

// Test 7: OrderBy with invalid field returns error
func TestPagination_OrderByInvalid(t *testing.T) {
	orderBy := []TemplateOrderByParam{
		{Field: "valid_field", Direction: "ASC"},
		{Field: "bad field!", Direction: "DESC"},
	}
	_, _, err := wrapWithPagination("SELECT * FROM users", nil, orderBy, nil, nil, 10000)
	if err == nil {
		t.Fatal("expected error for invalid orderBy field name")
	}
	if !strings.Contains(err.Error(), "VALIDATION_INVALID_FIELD") {
		t.Fatalf("expected VALIDATION_INVALID_FIELD error, got: %v", err)
	}
}

// Test 8: Over-fetch: first=10 → LIMIT=11
func TestPagination_OverFetch(t *testing.T) {
	first := 10
	_, args, err := wrapWithPagination("SELECT * FROM users", nil, nil, &first, nil, 10000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if args[0] != 11 {
		t.Fatalf("expected LIMIT=11 (first+1 over-fetch), got %v", args[0])
	}
}

// Test 9: wrapWithCount produces correct SQL
func TestPagination_WrapWithCount(t *testing.T) {
	baseSQL := "SELECT id, name FROM users WHERE active = true"
	countSQL := wrapWithCount(baseSQL)

	expected := "SELECT COUNT(*) AS total_count FROM (SELECT id, name FROM users WHERE active = true) AS __tq_cnt__"
	if countSQL != expected {
		t.Fatalf("expected:\n  %s\ngot:\n  %s", expected, countSQL)
	}
}

// Test 10: Empty fields uses SELECT *
func TestPagination_EmptyFieldsSelectStar(t *testing.T) {
	sql, _, err := wrapWithPagination("SELECT * FROM users", nil, nil, nil, nil, 10000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.HasPrefix(sql, "SELECT * FROM (") {
		t.Fatalf("expected SELECT * when no fields specified, got: %s", sql)
	}

	// Also test with empty slice
	sql2, _, err := wrapWithPagination("SELECT * FROM users", []string{}, nil, nil, nil, 10000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.HasPrefix(sql2, "SELECT * FROM (") {
		t.Fatalf("expected SELECT * when empty fields slice, got: %s", sql2)
	}
}
