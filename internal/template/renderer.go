// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package template

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	apierrors "github.com/michaelwang123/mountainKing/internal/errors"
)

// renderContext is the data object passed to template.Execute.
// Templates access parameters via {{.Params.xxx}}.
type renderContext struct {
	Params map[string]interface{}
}

// render executes the template with the given parameters and performs
// post-render validation (trim, non-empty, length check, SQL sanitisation).
//
// Flow:
//  1. Build renderContext{Params: params}
//  2. Execute template inside a goroutine with render_timeout
//  3. Trim result and validate non-empty
//  4. Check length ≤ maxRenderedSQLLen
//  5. Run sanitizeSQL security checks
func (te *TemplateEngine) render(ctx context.Context, tmpl *RegisteredTemplate, params map[string]interface{}, renderTimeout time.Duration, maxRenderedSQLLen int) (string, error) {
	renderCtx := renderContext{Params: params}

	// Create a timeout context for the render phase.
	timeoutCtx, cancel := context.WithTimeout(ctx, renderTimeout)
	defer cancel()

	type renderResult struct {
		sql string
		err error
	}
	ch := make(chan renderResult, 1)

	// Execute template in a separate goroutine so we can enforce the timeout.
	go func() {
		var buf bytes.Buffer
		err := tmpl.Template.Execute(&buf, renderCtx)
		ch <- renderResult{sql: buf.String(), err: err}
	}()

	// Wait for either the render result or timeout.
	var result renderResult
	select {
	case <-timeoutCtx.Done():
		// Track the leaked goroutine (template.Execute is synchronous and can't be cancelled).
		if te.metrics != nil && te.metrics.RenderGoroutineLeaks != nil {
			te.metrics.RenderGoroutineLeaks.Inc()
			// Spawn a goroutine to wait for the leaked render goroutine to finish,
			// then decrement the gauge.
			go func() {
				<-ch // wait for the leaked goroutine to complete
				te.metrics.RenderGoroutineLeaks.Dec()
			}()
		}
		return "", apierrors.NewAPIError(
			apierrors.ErrInternalTemplateRenderError,
			fmt.Sprintf("template %q render timed out after %s", tmpl.Name, renderTimeout),
			500,
		)
	case result = <-ch:
	}

	if result.err != nil {
		return "", apierrors.NewAPIError(
			apierrors.ErrInternalTemplateRenderError,
			fmt.Sprintf("template %q render failed: %v", tmpl.Name, result.err),
			500,
		)
	}

	// Trim whitespace and validate non-empty.
	rendered := strings.TrimSpace(result.sql)
	if rendered == "" {
		return "", apierrors.NewAPIError(
			apierrors.ErrInternalTemplateRenderError,
			fmt.Sprintf("template %q rendered to empty SQL", tmpl.Name),
			500,
		)
	}

	// Check rendered SQL length.
	if len(rendered) > maxRenderedSQLLen {
		return "", apierrors.ValidationError(
			apierrors.ErrValidationUnsafeSQL,
			fmt.Sprintf("rendered SQL length %d exceeds maximum %d bytes", len(rendered), maxRenderedSQLLen),
		)
	}

	// Run SQL security checks (semicolon detection, comment removal, unclosed quotes).
	sanitized, err := sanitizeSQL(rendered)
	if err != nil {
		return "", err
	}

	return sanitized, nil
}
