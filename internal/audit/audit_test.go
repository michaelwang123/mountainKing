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

	"github.com/michaelwang123/mountainKing/internal/config"
)

func TestNewAuditLogger_Disabled(t *testing.T) {
	al, err := NewAuditLogger(config.AuditConfig{Enabled: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if al.enabled {
		t.Fatal("expected disabled logger")
	}
	// Log should be a no-op.
	al.Log(LogEntry{Principal: "test", Time: time.Now(), Operation: "query", Datasource: "ds1", Success: true})
}

func TestNewAuditLogger_Stdout(t *testing.T) {
	al, err := NewAuditLogger(config.AuditConfig{Enabled: true, Output: "stdout"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !al.enabled {
		t.Fatal("expected enabled logger")
	}
}

func TestNewAuditLogger_File(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "audit.log")

	al, err := NewAuditLogger(config.AuditConfig{Enabled: true, Output: "file", FilePath: fp})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	now := time.Date(2024, 6, 15, 10, 30, 0, 0, time.UTC)
	al.Log(LogEntry{
		Principal:  "user-123",
		Time:       now,
		Operation:  "query",
		Datasource: "starrocks_main",
		Success:    true,
	})
	al.Log(LogEntry{
		Principal:  "apikey-abc",
		Time:       now,
		Operation:  "mutation",
		Datasource: "cache",
		Success:    false,
	})
	_ = al.Close()

	data, err := os.ReadFile(fp)
	if err != nil {
		t.Fatalf("failed to read audit log file: %v", err)
	}

	// Each line should be valid JSON with required fields.
	lines := splitLines(data)
	if len(lines) != 2 {
		t.Fatalf("expected 2 log lines, got %d", len(lines))
	}

	for i, line := range lines {
		var m map[string]any
		if err := json.Unmarshal(line, &m); err != nil {
			t.Fatalf("line %d: invalid JSON: %v", i, err)
		}
		for _, key := range []string{"principal", "operation_time", "operation", "datasource", "result"} {
			if _, ok := m[key]; !ok {
				t.Errorf("line %d: missing field %q", i, key)
			}
		}
	}

	// Verify first entry values.
	var first map[string]any
	_ = json.Unmarshal(lines[0], &first)
	if first["principal"] != "user-123" {
		t.Errorf("expected principal user-123, got %v", first["principal"])
	}
	if first["result"] != "success" {
		t.Errorf("expected result success, got %v", first["result"])
	}

	// Verify second entry values.
	var second map[string]any
	_ = json.Unmarshal(lines[1], &second)
	if second["result"] != "failure" {
		t.Errorf("expected result failure, got %v", second["result"])
	}
}

func TestNewAuditLogger_FileError(t *testing.T) {
	_, err := NewAuditLogger(config.AuditConfig{Enabled: true, Output: "file", FilePath: "/nonexistent/dir/audit.log"})
	if err == nil {
		t.Fatal("expected error for invalid file path")
	}
}

func TestAuditLogger_ExtraFields(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "audit_extra.log")

	al, err := NewAuditLogger(config.AuditConfig{Enabled: true, Output: "file", FilePath: fp})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	now := time.Date(2024, 6, 15, 10, 30, 0, 0, time.UTC)

	// Log with ExtraFields.
	al.Log(LogEntry{
		Principal:   "user-456",
		Time:        now,
		Operation:   "query",
		Datasource:  "analytics_db",
		Success:     true,
		ExtraFields: map[string]string{"template_name": "fleet_report", "custom_key": "custom_val"},
	})

	// Log without ExtraFields (nil) — backward compatible.
	al.Log(LogEntry{
		Principal:  "user-789",
		Time:       now,
		Operation:  "mutation",
		Datasource: "cache",
		Success:    false,
	})

	// Log with empty ExtraFields map — backward compatible.
	al.Log(LogEntry{
		Principal:   "user-000",
		Time:        now,
		Operation:   "query",
		Datasource:  "ds1",
		Success:     true,
		ExtraFields: map[string]string{},
	})
	_ = al.Close()

	data, err := os.ReadFile(fp)
	if err != nil {
		t.Fatalf("failed to read audit log file: %v", err)
	}

	lines := splitLines(data)
	if len(lines) != 3 {
		t.Fatalf("expected 3 log lines, got %d", len(lines))
	}

	// First entry: ExtraFields present.
	var first map[string]any
	if err := json.Unmarshal(lines[0], &first); err != nil {
		t.Fatalf("line 0: invalid JSON: %v", err)
	}
	if first["template_name"] != "fleet_report" {
		t.Errorf("expected template_name=fleet_report, got %v", first["template_name"])
	}
	if first["custom_key"] != "custom_val" {
		t.Errorf("expected custom_key=custom_val, got %v", first["custom_key"])
	}

	// Second entry: no ExtraFields — should not have template_name.
	var second map[string]any
	if err := json.Unmarshal(lines[1], &second); err != nil {
		t.Fatalf("line 1: invalid JSON: %v", err)
	}
	if _, ok := second["template_name"]; ok {
		t.Errorf("expected no template_name field in entry without ExtraFields")
	}

	// Third entry: empty ExtraFields — should not have extra keys.
	var third map[string]any
	if err := json.Unmarshal(lines[2], &third); err != nil {
		t.Fatalf("line 2: invalid JSON: %v", err)
	}
	if _, ok := third["template_name"]; ok {
		t.Errorf("expected no template_name field in entry with empty ExtraFields")
	}
}

// splitLines splits data by newlines, ignoring trailing empty line.
func splitLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i := 0; i < len(data); i++ {
		if data[i] == '\n' {
			line := data[start:i]
			if len(line) > 0 {
				lines = append(lines, line)
			}
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, data[start:])
	}
	return lines
}
