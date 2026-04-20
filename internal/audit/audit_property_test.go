// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/michaelwang123/mountainKing/internal/config"
)

// Property 54: 审计日志完整性
// **Validates: Requirements 13.12**
//
// For any authenticated request, the audit log entry must contain:
// principal, operation_time, operation, datasource, and result.
func TestProperty54_AuditLogCompleteness(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		dir := os.TempDir()
		fp := filepath.Join(dir, "audit_prop54_"+rapid.StringMatching(`^[a-z0-9]{8}$`).Draw(t, "suffix")+".log")
		defer os.Remove(fp)

		al, err := NewAuditLogger(config.AuditConfig{
			Enabled:  true,
			Output:   "file",
			FilePath: fp,
		})
		if err != nil {
			t.Fatalf("failed to create audit logger: %v", err)
		}

		// Generate random audit entry fields.
		principal := rapid.StringMatching(`^[a-zA-Z0-9_-]{1,32}$`).Draw(t, "principal")
		operation := rapid.SampledFrom([]string{"query", "mutation"}).Draw(t, "operation")
		datasource := rapid.StringMatching(`^[a-z][a-z0-9_]{0,15}$`).Draw(t, "datasource")
		success := rapid.Bool().Draw(t, "success")

		opTime := time.Date(
			rapid.IntRange(2020, 2030).Draw(t, "year"),
			time.Month(rapid.IntRange(1, 12).Draw(t, "month")),
			rapid.IntRange(1, 28).Draw(t, "day"),
			rapid.IntRange(0, 23).Draw(t, "hour"),
			rapid.IntRange(0, 59).Draw(t, "minute"),
			rapid.IntRange(0, 59).Draw(t, "second"),
			0, time.UTC,
		)

		al.Log(LogEntry{
			Principal:  principal,
			Time:       opTime,
			Operation:  operation,
			Datasource: datasource,
			Success:    success,
		})
		_ = al.Close()

		data, err := os.ReadFile(fp)
		if err != nil {
			t.Fatalf("failed to read audit log: %v", err)
		}

		var m map[string]interface{}
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("audit log is not valid JSON: %v", err)
		}

		// All required fields must be present.
		requiredFields := []string{"principal", "operation_time", "operation", "datasource", "result"}
		for _, field := range requiredFields {
			val, ok := m[field]
			if !ok {
				t.Fatalf("audit log missing required field %q", field)
			}
			if val == nil || val == "" {
				t.Fatalf("audit log field %q is empty", field)
			}
		}

		// Verify field values match input.
		if m["principal"] != principal {
			t.Fatalf("principal mismatch: got %v, want %v", m["principal"], principal)
		}
		if m["operation"] != operation {
			t.Fatalf("operation mismatch: got %v, want %v", m["operation"], operation)
		}
		if m["datasource"] != datasource {
			t.Fatalf("datasource mismatch: got %v, want %v", m["datasource"], datasource)
		}

		expectedResult := "failure"
		if success {
			expectedResult = "success"
		}
		if m["result"] != expectedResult {
			t.Fatalf("result mismatch: got %v, want %v", m["result"], expectedResult)
		}
	})
}
