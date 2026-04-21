// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package template

import (
	"strings"
	"testing"

	apierrors "github.com/michaelwang123/mountainKing/internal/errors"
	"pgregory.net/rapid"
)

// =============================================================================
// Feature: sql-template-engine
// Task 4.2: 安全检查器单元测试和属性测试
// =============================================================================

// =============================================================================
// Property 38: 多语句检测
// **Validates: Requirements 6.6**
// SQL with semicolons outside string literals returns VALIDATION_UNSAFE_SQL error.
// =============================================================================

func TestProperty38_MultiStatementDetection(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate a SQL-like prefix and suffix, then join with a semicolon outside quotes.
		prefix := rapid.StringMatching(`SELECT [a-z]{1,10} FROM [a-z]{1,10}`).Draw(rt, "prefix")
		suffix := rapid.StringMatching(` INSERT INTO [a-z]{1,10}`).Draw(rt, "suffix")
		sql := prefix + ";" + suffix

		_, err := sanitizeSQL(sql)
		if err == nil {
			rt.Fatalf("sanitizeSQL(%q) should return error for semicolon outside quotes", sql)
		}
		apiErr, ok := err.(*apierrors.APIError)
		if !ok {
			rt.Fatalf("expected *APIError, got %T", err)
		}
		if apiErr.Code != apierrors.ErrValidationUnsafeSQL {
			rt.Fatalf("expected error code %q, got %q", apierrors.ErrValidationUnsafeSQL, apiErr.Code)
		}
	})
}

// =============================================================================
// Property 39: SQL 注释检测
// **Validates: Requirements 6.6**
// Non-hint comments (-- and /* */) are removed from output.
// =============================================================================

func TestProperty39_SQLCommentRemoval(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate SQL with a line comment
		base := rapid.StringMatching(`SELECT [a-z]{1,10} FROM [a-z]{1,10}`).Draw(rt, "base")
		comment := rapid.StringMatching(`[a-zA-Z0-9 ]{1,20}`).Draw(rt, "comment")
		sqlWithLineComment := base + " --" + comment + "\n WHERE 1=1"

		result, err := sanitizeSQL(sqlWithLineComment)
		if err != nil {
			rt.Fatalf("sanitizeSQL(%q) returned error: %v", sqlWithLineComment, err)
		}
		if strings.Contains(result, "--") {
			rt.Fatalf("sanitizeSQL(%q) = %q: line comment not removed", sqlWithLineComment, result)
		}
	})
}

func TestProperty39_BlockCommentRemoval(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate SQL with a block comment (non-hint)
		base := rapid.StringMatching(`SELECT [a-z]{1,10} FROM [a-z]{1,10}`).Draw(rt, "base")
		comment := rapid.StringMatching(`[a-zA-Z0-9 ]{1,20}`).Draw(rt, "comment")
		sqlWithBlockComment := base + " /* " + comment + " */ WHERE 1=1"

		result, err := sanitizeSQL(sqlWithBlockComment)
		if err != nil {
			rt.Fatalf("sanitizeSQL(%q) returned error: %v", sqlWithBlockComment, err)
		}
		if strings.Contains(result, "/*") && !strings.Contains(result, "/*+") {
			rt.Fatalf("sanitizeSQL(%q) = %q: block comment not removed", sqlWithBlockComment, result)
		}
	})
}

// =============================================================================
// Property 40: SQL Hint 保留
// **Validates: Requirements 6.6**
// Optimizer hints (/*+ ... */) are preserved in output.
// =============================================================================

func TestProperty40_SQLHintPreservation(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		hintContent := rapid.StringMatching(`SET_VAR\([a-z_]{1,10}=[0-9]{1,5}\)`).Draw(rt, "hintContent")
		hint := "/*+ " + hintContent + " */"
		sql := hint + " SELECT a FROM b"

		result, err := sanitizeSQL(sql)
		if err != nil {
			rt.Fatalf("sanitizeSQL(%q) returned error: %v", sql, err)
		}
		if !strings.Contains(result, "/*+") || !strings.Contains(result, "*/") {
			rt.Fatalf("sanitizeSQL(%q) = %q: optimizer hint was removed", sql, result)
		}
		if !strings.Contains(result, hintContent) {
			rt.Fatalf("sanitizeSQL(%q) = %q: hint content %q was lost", sql, result, hintContent)
		}
	})
}

// =============================================================================
// Property 67: 双引号标识符安全
// **Validates: Requirements 6.6**
// Semicolons inside double-quoted identifiers don't trigger multi-statement detection.
// =============================================================================

func TestProperty67_DoubleQuoteIdentifierSafety(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate an identifier with semicolons inside double quotes
		prefix := rapid.StringMatching(`[a-z]{1,10}`).Draw(rt, "prefix")
		suffix := rapid.StringMatching(`[a-z]{1,10}`).Draw(rt, "suffix")
		identifier := `"` + prefix + ";" + suffix + `"`
		sql := "SELECT " + identifier + " FROM t"

		result, err := sanitizeSQL(sql)
		if err != nil {
			rt.Fatalf("sanitizeSQL(%q) should not error for semicolon inside double-quoted identifier, got: %v", sql, err)
		}
		if !strings.Contains(result, identifier) {
			rt.Fatalf("sanitizeSQL(%q) = %q: double-quoted identifier was modified", sql, result)
		}
	})
}

// =============================================================================
// Property 68: 反引号标识符安全
// **Validates: Requirements 6.6**
// Semicolons inside backtick-quoted identifiers don't trigger multi-statement detection.
// =============================================================================

func TestProperty68_BacktickIdentifierSafety(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate an identifier with semicolons inside backticks
		prefix := rapid.StringMatching(`[a-z]{1,10}`).Draw(rt, "prefix")
		suffix := rapid.StringMatching(`[a-z]{1,10}`).Draw(rt, "suffix")
		identifier := "`" + prefix + ";" + suffix + "`"
		sql := "SELECT " + identifier + " FROM t"

		result, err := sanitizeSQL(sql)
		if err != nil {
			rt.Fatalf("sanitizeSQL(%q) should not error for semicolon inside backtick identifier, got: %v", sql, err)
		}
		if !strings.Contains(result, identifier) {
			rt.Fatalf("sanitizeSQL(%q) = %q: backtick identifier was modified", sql, result)
		}
	})
}

// =============================================================================
// Property 69: 未闭合引号检测
// **Validates: Requirements 6.6**
// Unclosed quotes at EOF return VALIDATION_UNSAFE_SQL error.
// =============================================================================

func TestProperty69_UnclosedQuoteDetection(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Pick a random quote type and generate SQL with an unclosed quote
		quoteType := rapid.SampledFrom([]byte{'\'', '"', '`'}).Draw(rt, "quoteType")
		content := rapid.StringMatching(`[a-zA-Z0-9 ]{1,20}`).Draw(rt, "content")
		sql := "SELECT " + string(quoteType) + content

		_, err := sanitizeSQL(sql)
		if err == nil {
			rt.Fatalf("sanitizeSQL(%q) should return error for unclosed quote %c", sql, quoteType)
		}
		apiErr, ok := err.(*apierrors.APIError)
		if !ok {
			rt.Fatalf("expected *APIError, got %T", err)
		}
		if apiErr.Code != apierrors.ErrValidationUnsafeSQL {
			rt.Fatalf("expected error code %q, got %q", apierrors.ErrValidationUnsafeSQL, apiErr.Code)
		}
	})
}

// =============================================================================
// Unit Tests for sanitizeSQL
// =============================================================================

// 1. Simple SELECT passes
func TestSanitizeSQL_SimpleSelectPasses(t *testing.T) {
	sql := "SELECT id, name FROM users WHERE id = 1"
	result, err := sanitizeSQL(sql)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != sql {
		t.Fatalf("expected %q, got %q", sql, result)
	}
}

// 2. Semicolon outside quotes returns error
func TestSanitizeSQL_SemicolonOutsideQuotes(t *testing.T) {
	sql := "SELECT 1; DROP TABLE users"
	_, err := sanitizeSQL(sql)
	if err == nil {
		t.Fatal("expected error for semicolon outside quotes")
	}
	apiErr, ok := err.(*apierrors.APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.Code != apierrors.ErrValidationUnsafeSQL {
		t.Fatalf("expected code %q, got %q", apierrors.ErrValidationUnsafeSQL, apiErr.Code)
	}
}

// 3. Semicolon inside single-quoted string passes
func TestSanitizeSQL_SemicolonInsideSingleQuote(t *testing.T) {
	sql := "SELECT * FROM t WHERE name = 'hello;world'"
	result, err := sanitizeSQL(sql)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != sql {
		t.Fatalf("expected %q, got %q", sql, result)
	}
}

// 4. Semicolon inside double-quoted identifier passes
func TestSanitizeSQL_SemicolonInsideDoubleQuote(t *testing.T) {
	sql := `SELECT "col;name" FROM t`
	result, err := sanitizeSQL(sql)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != sql {
		t.Fatalf("expected %q, got %q", sql, result)
	}
}

// 5. Semicolon inside backtick identifier passes
func TestSanitizeSQL_SemicolonInsideBacktick(t *testing.T) {
	sql := "SELECT `col;name` FROM t"
	result, err := sanitizeSQL(sql)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != sql {
		t.Fatalf("expected %q, got %q", sql, result)
	}
}

// 6. Line comment (--) removed
func TestSanitizeSQL_LineCommentRemoved(t *testing.T) {
	sql := "SELECT 1 -- this is a comment\n FROM t"
	result, err := sanitizeSQL(sql)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(result, "--") {
		t.Fatalf("line comment not removed: %q", result)
	}
	if !strings.Contains(result, "SELECT 1") {
		t.Fatalf("SQL prefix lost: %q", result)
	}
	if !strings.Contains(result, "FROM t") {
		t.Fatalf("SQL suffix lost: %q", result)
	}
}

// 7. Block comment (/* */) removed
func TestSanitizeSQL_BlockCommentRemoved(t *testing.T) {
	sql := "SELECT /* comment */ 1 FROM t"
	result, err := sanitizeSQL(sql)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(result, "comment") {
		t.Fatalf("block comment not removed: %q", result)
	}
	if !strings.Contains(result, "SELECT") {
		t.Fatalf("SQL prefix lost: %q", result)
	}
	if !strings.Contains(result, "1 FROM t") {
		t.Fatalf("SQL suffix lost: %q", result)
	}
}

// 8. Optimizer hint (/*+ */) preserved
func TestSanitizeSQL_OptimizerHintPreserved(t *testing.T) {
	sql := "/*+ SET_VAR(query_timeout=30) */ SELECT 1 FROM t"
	result, err := sanitizeSQL(sql)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "/*+ SET_VAR(query_timeout=30) */") {
		t.Fatalf("optimizer hint was removed: %q", result)
	}
}

// 9. Escaped single quotes (”) handled correctly
func TestSanitizeSQL_EscapedSingleQuotes(t *testing.T) {
	sql := "SELECT * FROM t WHERE name = 'it''s'"
	result, err := sanitizeSQL(sql)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != sql {
		t.Fatalf("expected %q, got %q", sql, result)
	}
}

// 10. Backslash-escaped single quotes (\') handled correctly
func TestSanitizeSQL_BackslashEscapedSingleQuotes(t *testing.T) {
	sql := `SELECT * FROM t WHERE name = 'it\'s'`
	result, err := sanitizeSQL(sql)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != sql {
		t.Fatalf("expected %q, got %q", sql, result)
	}
}

// 11. Escaped double quotes ("") handled correctly
func TestSanitizeSQL_EscapedDoubleQuotes(t *testing.T) {
	sql := `SELECT "col""name" FROM t`
	result, err := sanitizeSQL(sql)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != sql {
		t.Fatalf("expected %q, got %q", sql, result)
	}
}

// 12. Escaped backticks (“) handled correctly
func TestSanitizeSQL_EscapedBackticks(t *testing.T) {
	sql := "SELECT `col``name` FROM t"
	result, err := sanitizeSQL(sql)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != sql {
		t.Fatalf("expected %q, got %q", sql, result)
	}
}

// 13. Unclosed single quote returns error
func TestSanitizeSQL_UnclosedSingleQuote(t *testing.T) {
	sql := "SELECT * FROM t WHERE name = 'unclosed"
	_, err := sanitizeSQL(sql)
	if err == nil {
		t.Fatal("expected error for unclosed single quote")
	}
	apiErr, ok := err.(*apierrors.APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.Code != apierrors.ErrValidationUnsafeSQL {
		t.Fatalf("expected code %q, got %q", apierrors.ErrValidationUnsafeSQL, apiErr.Code)
	}
}

// 14. Unclosed double quote returns error
func TestSanitizeSQL_UnclosedDoubleQuote(t *testing.T) {
	sql := `SELECT "unclosed FROM t`
	_, err := sanitizeSQL(sql)
	if err == nil {
		t.Fatal("expected error for unclosed double quote")
	}
	apiErr, ok := err.(*apierrors.APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.Code != apierrors.ErrValidationUnsafeSQL {
		t.Fatalf("expected code %q, got %q", apierrors.ErrValidationUnsafeSQL, apiErr.Code)
	}
}

// 15. Unclosed backtick returns error
func TestSanitizeSQL_UnclosedBacktick(t *testing.T) {
	sql := "SELECT `unclosed FROM t"
	_, err := sanitizeSQL(sql)
	if err == nil {
		t.Fatal("expected error for unclosed backtick")
	}
	apiErr, ok := err.(*apierrors.APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.Code != apierrors.ErrValidationUnsafeSQL {
		t.Fatalf("expected code %q, got %q", apierrors.ErrValidationUnsafeSQL, apiErr.Code)
	}
}

// 16. Unclosed block comment returns error
func TestSanitizeSQL_UnclosedBlockComment(t *testing.T) {
	sql := "SELECT /* unclosed comment FROM t"
	_, err := sanitizeSQL(sql)
	if err == nil {
		t.Fatal("expected error for unclosed block comment")
	}
	apiErr, ok := err.(*apierrors.APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.Code != apierrors.ErrValidationUnsafeSQL {
		t.Fatalf("expected code %q, got %q", apierrors.ErrValidationUnsafeSQL, apiErr.Code)
	}
}

// 17. Multiple comments removed
func TestSanitizeSQL_MultipleCommentsRemoved(t *testing.T) {
	sql := "SELECT -- first comment\n 1 /* second comment */ FROM t -- trailing\n"
	result, err := sanitizeSQL(sql)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(result, "first comment") {
		t.Fatalf("first line comment not removed: %q", result)
	}
	if strings.Contains(result, "second comment") {
		t.Fatalf("block comment not removed: %q", result)
	}
	if strings.Contains(result, "trailing") {
		t.Fatalf("trailing line comment not removed: %q", result)
	}
	if !strings.Contains(result, "SELECT") || !strings.Contains(result, "FROM t") {
		t.Fatalf("SQL structure lost: %q", result)
	}
}

// 18. Complex SQL with hints and strings passes
func TestSanitizeSQL_ComplexSQLWithHintsAndStrings(t *testing.T) {
	sql := `/*+ SET_VAR(query_timeout=30) */ SELECT a, b FROM t WHERE name = 'hello;world' AND "col;x" = 1`
	result, err := sanitizeSQL(sql)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "/*+ SET_VAR(query_timeout=30) */") {
		t.Fatalf("hint was removed: %q", result)
	}
	if !strings.Contains(result, "'hello;world'") {
		t.Fatalf("string literal was modified: %q", result)
	}
	if !strings.Contains(result, `"col;x"`) {
		t.Fatalf("double-quoted identifier was modified: %q", result)
	}
}
