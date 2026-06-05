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

		// StarRocks does not support parameterized LIMIT/OFFSET.
		// LIMIT value (first+1) must be inlined in SQL as an integer.
		expectedLimit := first + 1
		expected := fmt.Sprintf("LIMIT %d OFFSET 0", expectedLimit)
		if !strings.Contains(sql, expected) {
			rt.Fatalf("SQL should contain '%s', got: %s", expected, sql)
		}
		// args should be nil (no parameterized values)
		if len(args) != 0 {
			rt.Fatalf("expected 0 args (inline LIMIT/OFFSET), got %d", len(args))
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

		// StarRocks does not support parameterized LIMIT/OFFSET.
		// OFFSET value must be inlined in SQL as an integer.
		expected := fmt.Sprintf("OFFSET %d", offset)
		if !strings.Contains(sql, expected) {
			rt.Fatalf("SQL should contain '%s', got: %s", expected, sql)
		}
		// args should be nil
		if len(args) != 0 {
			rt.Fatalf("expected 0 args (inline LIMIT/OFFSET), got %d", len(args))
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

		sql, args, err := wrapWithPagination(baseSQL, nil, nil, nil, nil, maxRows)
		if err != nil {
			rt.Fatalf("wrapWithPagination returned error: %v", err)
		}

		expectedLimit := maxRows + 1
		// LIMIT must be inlined in SQL
		expected := fmt.Sprintf("LIMIT %d", expectedLimit)
		if !strings.Contains(sql, expected) {
			rt.Fatalf("expected LIMIT = maxResultRows+1 = %d in SQL, got: %s", expectedLimit, sql)
		}
		// args should be nil
		if len(args) != 0 {
			rt.Fatalf("expected 0 args, got %d", len(args))
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

	// Over-fetch: LIMIT = first + 1 = 21, inlined in SQL
	if !strings.Contains(sql, "LIMIT 21 OFFSET 40") {
		t.Fatalf("expected inlined LIMIT 21 OFFSET 40, got: %s", sql)
	}
	// args should be nil (StarRocks doesn't support parameterized LIMIT/OFFSET)
	if len(args) != 0 {
		t.Fatalf("expected 0 args, got %d", len(args))
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

	// LIMIT = maxResultRows + 1 = 5001, inlined
	if !strings.Contains(sql, "LIMIT 5001 OFFSET 10") {
		t.Fatalf("expected inlined LIMIT 5001 OFFSET 10, got: %s", sql)
	}
	if len(args) != 0 {
		t.Fatalf("expected 0 args, got %d", len(args))
	}
}

// Test 3: Pagination without offset (defaults to 0)
func TestPagination_WithoutOffset(t *testing.T) {
	first := 10
	sql, args, err := wrapWithPagination("SELECT * FROM users", nil, nil, &first, nil, 10000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(sql, "LIMIT 11 OFFSET 0") {
		t.Fatalf("expected inlined LIMIT 11 OFFSET 0, got: %s", sql)
	}
	if len(args) != 0 {
		t.Fatalf("expected 0 args, got %d", len(args))
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
	sql, args, err := wrapWithPagination("SELECT * FROM users", nil, nil, &first, nil, 10000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(sql, "LIMIT 11") {
		t.Fatalf("expected LIMIT=11 (first+1 over-fetch), got: %s", sql)
	}
	if len(args) != 0 {
		t.Fatalf("expected 0 args, got %d", len(args))
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
