package sanitize

import (
	"testing"

	"github.com/example/graphql-api/internal/config"
)

func TestSanitizer_Disabled(t *testing.T) {
	s, err := NewSanitizer(config.SanitizationConfig{Enabled: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	input := "SELECT * FROM users WHERE name = 'alice'"
	if got := s.Sanitize(input); got != input {
		t.Errorf("disabled sanitizer should return input unchanged, got %q", got)
	}
}

func TestSanitizer_DefaultRules(t *testing.T) {
	s, err := NewSanitizer(config.SanitizationConfig{Enabled: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "sql string literal",
			input: "SELECT * FROM users WHERE name = 'alice'",
			want:  "SELECT * FROM users WHERE name = '***'",
		},
		{
			name:  "multiple string literals",
			input: "SELECT * FROM t WHERE a = 'foo' AND b = 'bar'",
			want:  "SELECT * FROM t WHERE a = '***' AND b = '***'",
		},
		{
			name:  "numeric 4+ digits",
			input: "SELECT * FROM orders WHERE id = 12345",
			want:  "SELECT * FROM orders WHERE id = ***",
		},
		{
			name:  "short numbers preserved",
			input: "SELECT * FROM t LIMIT 10",
			want:  "SELECT * FROM t LIMIT 10",
		},
		{
			name:  "mixed sensitive data",
			input: "INSERT INTO t VALUES ('secret', 99999)",
			want:  "INSERT INTO t VALUES ('***', ***)",
		},
		{
			name:  "no sensitive data",
			input: "SELECT id, name FROM users",
			want:  "SELECT id, name FROM users",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.Sanitize(tt.input)
			if got != tt.want {
				t.Errorf("Sanitize(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSanitizer_CustomRules(t *testing.T) {
	s, err := NewSanitizer(config.SanitizationConfig{
		Enabled: true,
		Rules: []config.SanitizationRule{
			{Pattern: `\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Z|a-z]{2,}\b`, Replacement: "[REDACTED_EMAIL]"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	input := "user email is test@example.com in the log"
	got := s.Sanitize(input)
	want := "user email is [REDACTED_EMAIL] in the log"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSanitizer_InvalidRegex(t *testing.T) {
	_, err := NewSanitizer(config.SanitizationConfig{
		Enabled: true,
		Rules: []config.SanitizationRule{
			{Pattern: `[invalid`, Replacement: "x"},
		},
	})
	if err == nil {
		t.Fatal("expected error for invalid regex")
	}
}
