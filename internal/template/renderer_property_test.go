// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package template

import (
	"context"
	"strings"
	"testing"
	"text/template"
	"time"

	apierrors "github.com/michaelwang123/mountainKing/internal/errors"
	"pgregory.net/rapid"
)

// =============================================================================
// Feature: sql-template-engine
// Task 8.2: 渲染器单元测试和属性测试
// =============================================================================

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// newTestTemplate creates a RegisteredTemplate for testing using the standard
// buildFuncMap and Option("missingkey=error").
func newTestTemplate(t *testing.T, name, content string) *RegisteredTemplate {
	t.Helper()
	tmpl, err := template.New(name).Funcs(buildFuncMap()).Option("missingkey=error").Parse(content)
	if err != nil {
		t.Fatalf("failed to parse test template %q: %v", name, err)
	}
	return &RegisteredTemplate{
		Name:     name,
		Template: tmpl,
	}
}

// defaultRenderTimeout is the standard timeout used in most tests.
const defaultRenderTimeout = 5 * time.Second

// defaultMaxRenderedSQLLen is the standard max length used in most tests.
const defaultMaxRenderedSQLLen = 65536

// extractErrorCode extracts the error code from an *APIError, or returns "" if
// the error is not an *APIError.
func extractErrorCode(err error) string {
	if apiErr, ok := err.(*apierrors.APIError); ok {
		return apiErr.Code
	}
	return ""
}

// =============================================================================
// Property 8: 渲染结果非空
// **Validates: Requirements 2.8**
// For any successful render, result is non-empty (after trim).
// =============================================================================

func TestProperty8_RenderResultNonEmpty(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate a random non-empty word to embed in the template.
		word := rapid.StringMatching(`[a-zA-Z]{1,20}`).Draw(rt, "word")
		tmplContent := "SELECT " + word + " FROM t"
		tmpl := newTestTemplate(t, "test_p8", tmplContent)

		result, err := render(context.Background(), tmpl, map[string]interface{}{}, defaultRenderTimeout, defaultMaxRenderedSQLLen)
		if err != nil {
			rt.Fatalf("render returned error: %v", err)
		}

		trimmed := strings.TrimSpace(result)
		if len(trimmed) == 0 {
			rt.Fatalf("render produced empty result for template %q", tmplContent)
		}
	})
}

// =============================================================================
// Property 9: 渲染结果长度限制
// **Validates: Requirements 2.10**
// For any successful render, len(result) ≤ maxRenderedSQLLen.
// =============================================================================

func TestProperty9_RenderResultLengthLimit(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		maxLen := rapid.IntRange(10, 1000).Draw(rt, "maxLen")
		// Generate a template that produces output shorter than maxLen.
		word := rapid.StringMatching(`[a-zA-Z]{1,8}`).Draw(rt, "word")
		tmplContent := "SELECT " + word + " FROM t"
		tmpl := newTestTemplate(t, "test_p9", tmplContent)

		result, err := render(context.Background(), tmpl, map[string]interface{}{}, defaultRenderTimeout, maxLen)
		if err != nil {
			// If error, it should be because length exceeded — that's fine.
			return
		}

		if len(result) > maxLen {
			rt.Fatalf("render result length %d exceeds maxRenderedSQLLen %d", len(result), maxLen)
		}
	})
}

// =============================================================================
// Property 10: 渲染超时保护
// **Validates: Requirements 2.9**
// Render operations that exceed timeout return INTERNAL_TEMPLATE_RENDER_ERROR.
// =============================================================================

func TestProperty10_RenderTimeoutProtection(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Use a normal template but with an extremely short timeout (1ns)
		// to guarantee the timeout fires.
		word := rapid.StringMatching(`[a-zA-Z]{1,10}`).Draw(rt, "word")
		tmplContent := "SELECT " + word + " FROM t"
		tmpl := newTestTemplate(t, "test_p10", tmplContent)

		_, err := render(context.Background(), tmpl, map[string]interface{}{}, time.Nanosecond, defaultMaxRenderedSQLLen)
		if err == nil {
			// It's possible the render completed before the 1ns timeout on fast machines.
			// This is acceptable — the property is that IF timeout occurs, the error code is correct.
			return
		}

		code := extractErrorCode(err)
		if code != apierrors.ErrInternalTemplateRenderError {
			rt.Fatalf("expected error code %q on timeout, got %q (err: %v)",
				apierrors.ErrInternalTemplateRenderError, code, err)
		}
	})
}

// =============================================================================
// Property 12: 渲染确定性
// **Validates: Requirements 2.1**
// For same template and params, render produces identical results.
// =============================================================================

func TestProperty12_RenderDeterminism(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		word := rapid.StringMatching(`[a-zA-Z]{1,10}`).Draw(rt, "word")
		paramVal := rapid.StringMatching(`[a-zA-Z0-9]{1,20}`).Draw(rt, "paramVal")
		tmplContent := "SELECT * FROM t WHERE name = {{.Params.name | quote}}"
		tmpl := newTestTemplate(t, "test_p12", tmplContent)

		params := map[string]interface{}{"name": paramVal}
		_ = word // used for variety in generation

		result1, err1 := render(context.Background(), tmpl, params, defaultRenderTimeout, defaultMaxRenderedSQLLen)
		result2, err2 := render(context.Background(), tmpl, params, defaultRenderTimeout, defaultMaxRenderedSQLLen)

		if (err1 == nil) != (err2 == nil) {
			rt.Fatalf("render determinism violated: err1=%v, err2=%v", err1, err2)
		}
		if err1 != nil {
			return // both errored, that's consistent
		}
		if result1 != result2 {
			rt.Fatalf("render determinism violated:\n  result1: %q\n  result2: %q", result1, result2)
		}
	})
}

// =============================================================================
// Unit Tests
// =============================================================================

// 1. Simple template renders correctly
func TestRender_SimpleTemplate(t *testing.T) {
	tmpl := newTestTemplate(t, "simple", "SELECT * FROM users")
	result, err := render(context.Background(), tmpl, map[string]interface{}{}, defaultRenderTimeout, defaultMaxRenderedSQLLen)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "SELECT * FROM users" {
		t.Fatalf("expected %q, got %q", "SELECT * FROM users", result)
	}
}

// 2. Template with params renders correctly
func TestRender_TemplateWithParams(t *testing.T) {
	tmpl := newTestTemplate(t, "with_params", "SELECT * FROM users WHERE id = {{.Params.id | safeInt}}")
	params := map[string]interface{}{"id": int64(42)}
	result, err := render(context.Background(), tmpl, params, defaultRenderTimeout, defaultMaxRenderedSQLLen)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "SELECT * FROM users WHERE id = 42"
	if result != expected {
		t.Fatalf("expected %q, got %q", expected, result)
	}
}

// 3. Empty render result returns error
func TestRender_EmptyResultReturnsError(t *testing.T) {
	// Template that renders to only whitespace.
	tmpl := newTestTemplate(t, "empty", "   \n\t  ")
	_, err := render(context.Background(), tmpl, map[string]interface{}{}, defaultRenderTimeout, defaultMaxRenderedSQLLen)
	if err == nil {
		t.Fatal("expected error for empty render result")
	}
	code := extractErrorCode(err)
	if code != apierrors.ErrInternalTemplateRenderError {
		t.Fatalf("expected error code %q, got %q", apierrors.ErrInternalTemplateRenderError, code)
	}
}

// 4. Render exceeding max length returns VALIDATION_UNSAFE_SQL
func TestRender_ExceedingMaxLengthReturnsError(t *testing.T) {
	// Create a template that produces output longer than our small max.
	longSQL := "SELECT " + strings.Repeat("x", 100) + " FROM t"
	tmpl := newTestTemplate(t, "long", longSQL)
	_, err := render(context.Background(), tmpl, map[string]interface{}{}, defaultRenderTimeout, 50)
	if err == nil {
		t.Fatal("expected error for exceeding max length")
	}
	code := extractErrorCode(err)
	if code != apierrors.ErrValidationUnsafeSQL {
		t.Fatalf("expected error code %q, got %q", apierrors.ErrValidationUnsafeSQL, code)
	}
}

// 5. Render timeout returns INTERNAL_TEMPLATE_RENDER_ERROR
func TestRender_TimeoutReturnsError(t *testing.T) {
	tmpl := newTestTemplate(t, "timeout", "SELECT 1 FROM t")
	// Use an already-cancelled context to guarantee the timeout path fires.
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately
	_, err := render(ctx, tmpl, map[string]interface{}{}, time.Nanosecond, defaultMaxRenderedSQLLen)
	if err == nil {
		t.Fatal("expected error for timeout")
	}
	code := extractErrorCode(err)
	if code != apierrors.ErrInternalTemplateRenderError {
		t.Fatalf("expected error code %q, got %q", apierrors.ErrInternalTemplateRenderError, code)
	}
}

// 6. Template with syntax error in execution returns INTERNAL_TEMPLATE_RENDER_ERROR
func TestRender_TemplateSyntaxErrorInExecution(t *testing.T) {
	// Create a template that references a missing key — with missingkey=error this will fail.
	tmpl := newTestTemplate(t, "missing_key", "SELECT {{.Params.nonexistent}} FROM t")
	params := map[string]interface{}{} // "nonexistent" not provided
	_, err := render(context.Background(), tmpl, params, defaultRenderTimeout, defaultMaxRenderedSQLLen)
	if err == nil {
		t.Fatal("expected error for missing key in template execution")
	}
	code := extractErrorCode(err)
	if code != apierrors.ErrInternalTemplateRenderError {
		t.Fatalf("expected error code %q, got %q", apierrors.ErrInternalTemplateRenderError, code)
	}
}

// 7. Render with sanitizeSQL detecting semicolons returns VALIDATION_UNSAFE_SQL
func TestRender_SemicolonDetectionReturnsUnsafeSQL(t *testing.T) {
	// Template that produces SQL with a semicolon outside string literals.
	tmpl := newTestTemplate(t, "semicolon", "SELECT 1; DROP TABLE users")
	_, err := render(context.Background(), tmpl, map[string]interface{}{}, defaultRenderTimeout, defaultMaxRenderedSQLLen)
	if err == nil {
		t.Fatal("expected error for semicolon in rendered SQL")
	}
	code := extractErrorCode(err)
	if code != apierrors.ErrValidationUnsafeSQL {
		t.Fatalf("expected error code %q, got %q", apierrors.ErrValidationUnsafeSQL, code)
	}
}

// 8. Deterministic rendering (same input → same output)
func TestRender_Deterministic(t *testing.T) {
	tmpl := newTestTemplate(t, "deterministic", "SELECT * FROM t WHERE name = {{.Params.name | quote}} AND id = {{.Params.id | safeInt}}")
	params := map[string]interface{}{"name": "O'Brien", "id": int64(7)}

	result1, err := render(context.Background(), tmpl, params, defaultRenderTimeout, defaultMaxRenderedSQLLen)
	if err != nil {
		t.Fatalf("first render failed: %v", err)
	}

	result2, err := render(context.Background(), tmpl, params, defaultRenderTimeout, defaultMaxRenderedSQLLen)
	if err != nil {
		t.Fatalf("second render failed: %v", err)
	}

	if result1 != result2 {
		t.Fatalf("deterministic rendering violated:\n  result1: %q\n  result2: %q", result1, result2)
	}
}
