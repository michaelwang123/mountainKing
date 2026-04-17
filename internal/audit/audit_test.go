package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/example/graphql-api/internal/config"
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
		var m map[string]interface{}
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
	var first map[string]interface{}
	_ = json.Unmarshal(lines[0], &first)
	if first["principal"] != "user-123" {
		t.Errorf("expected principal user-123, got %v", first["principal"])
	}
	if first["result"] != "success" {
		t.Errorf("expected result success, got %v", first["result"])
	}

	// Verify second entry values.
	var second map[string]interface{}
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
