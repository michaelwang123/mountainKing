// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package sanitize

import (
	"fmt"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/michaelwang123/mountainKing/internal/config"
)

// Property 55: 敏感信息脱敏
// **Validates: Requirements 13.13**
//
// For any SQL statement containing string literals or 4+ digit numbers,
// the sanitized output must never contain the original sensitive values.
func TestProperty55_SensitiveDataSanitization(t *testing.T) {
	s, err := NewSanitizer(config.SanitizationConfig{Enabled: true})
	if err != nil {
		t.Fatalf("failed to create sanitizer: %v", err)
	}

	rapid.Check(t, func(t *rapid.T) {
		// Generate a random SQL-like statement with embedded sensitive values.
		table := rapid.StringMatching(`^[a-z]{3,10}$`).Draw(t, "table")
		column := rapid.StringMatching(`^[a-z]{3,10}$`).Draw(t, "column")

		// Generate a sensitive string literal (non-empty, no single quotes inside).
		sensitiveStr := rapid.StringMatching(`^[a-zA-Z0-9 ]{1,20}$`).Draw(t, "sensitiveStr")
		// Generate a sensitive number with 4+ digits.
		sensitiveNum := rapid.IntRange(1000, 9999999).Draw(t, "sensitiveNum")

		sql := fmt.Sprintf("SELECT * FROM %s WHERE %s = '%s' AND id = %d",
			table, column, sensitiveStr, sensitiveNum)

		sanitized := s.Sanitize(sql)

		// The original string literal value must not appear in the output.
		quotedOriginal := fmt.Sprintf("'%s'", sensitiveStr)
		if strings.Contains(sanitized, quotedOriginal) {
			t.Fatalf("sanitized output still contains original string literal %q:\n  input:  %s\n  output: %s",
				quotedOriginal, sql, sanitized)
		}

		// The original numeric value must not appear in the output.
		numStr := fmt.Sprintf("%d", sensitiveNum)
		if strings.Contains(sanitized, numStr) {
			t.Fatalf("sanitized output still contains original number %q:\n  input:  %s\n  output: %s",
				numStr, sql, sanitized)
		}

		// The sanitized output should still contain the non-sensitive parts.
		if !strings.Contains(sanitized, table) {
			t.Fatalf("sanitized output lost table name %q:\n  output: %s", table, sanitized)
		}
		if !strings.Contains(sanitized, column) {
			t.Fatalf("sanitized output lost column name %q:\n  output: %s", column, sanitized)
		}
	})
}
