package observability

import (
	"testing"
	"time"

	"go.uber.org/zap/zapcore"
)

func TestNewLogger_DefaultConfig(t *testing.T) {
	logger, err := NewLogger(LoggerConfig{
		Level:  "info",
		Format: "json",
	})
	if err != nil {
		t.Fatalf("NewLogger returned error: %v", err)
	}
	defer logger.Sync() //nolint:errcheck

	if logger.Logger == nil {
		t.Fatal("expected non-nil zap.Logger")
	}
	if got := logger.Level.Level(); got != zapcore.InfoLevel {
		t.Errorf("expected level info, got %v", got)
	}
	if logger.SlowQueryThreshold != time.Second {
		t.Errorf("expected default slow query threshold 1s, got %v", logger.SlowQueryThreshold)
	}
}

func TestNewLogger_AllLevels(t *testing.T) {
	tests := []struct {
		level    string
		expected zapcore.Level
	}{
		{"debug", zapcore.DebugLevel},
		{"info", zapcore.InfoLevel},
		{"warn", zapcore.WarnLevel},
		{"error", zapcore.ErrorLevel},
	}

	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			logger, err := NewLogger(LoggerConfig{Level: tt.level})
			if err != nil {
				t.Fatalf("NewLogger(%q) returned error: %v", tt.level, err)
			}
			defer logger.Sync() //nolint:errcheck

			if got := logger.Level.Level(); got != tt.expected {
				t.Errorf("expected level %v, got %v", tt.expected, got)
			}
		})
	}
}

func TestNewLogger_InvalidLevel(t *testing.T) {
	_, err := NewLogger(LoggerConfig{Level: "invalid"})
	if err == nil {
		t.Fatal("expected error for invalid level, got nil")
	}
}

func TestNewLogger_CustomSlowQueryThreshold(t *testing.T) {
	logger, err := NewLogger(LoggerConfig{
		Level:              "info",
		SlowQueryThreshold: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewLogger returned error: %v", err)
	}
	defer logger.Sync() //nolint:errcheck

	if logger.SlowQueryThreshold != 2*time.Second {
		t.Errorf("expected slow query threshold 2s, got %v", logger.SlowQueryThreshold)
	}
}

func TestNewLogger_ZeroSlowQueryThresholdDefaultsTo1s(t *testing.T) {
	logger, err := NewLogger(LoggerConfig{
		Level:              "info",
		SlowQueryThreshold: 0,
	})
	if err != nil {
		t.Fatalf("NewLogger returned error: %v", err)
	}
	defer logger.Sync() //nolint:errcheck

	if logger.SlowQueryThreshold != time.Second {
		t.Errorf("expected default 1s, got %v", logger.SlowQueryThreshold)
	}
}

func TestSetLevel(t *testing.T) {
	logger, err := NewLogger(LoggerConfig{Level: "info"})
	if err != nil {
		t.Fatalf("NewLogger returned error: %v", err)
	}
	defer logger.Sync() //nolint:errcheck

	if err := logger.SetLevel("debug"); err != nil {
		t.Fatalf("SetLevel(debug) returned error: %v", err)
	}
	if got := logger.Level.Level(); got != zapcore.DebugLevel {
		t.Errorf("expected debug after SetLevel, got %v", got)
	}

	if err := logger.SetLevel("error"); err != nil {
		t.Fatalf("SetLevel(error) returned error: %v", err)
	}
	if got := logger.Level.Level(); got != zapcore.ErrorLevel {
		t.Errorf("expected error after SetLevel, got %v", got)
	}
}

func TestSetLevel_Invalid(t *testing.T) {
	logger, err := NewLogger(LoggerConfig{Level: "info"})
	if err != nil {
		t.Fatalf("NewLogger returned error: %v", err)
	}
	defer logger.Sync() //nolint:errcheck

	if err := logger.SetLevel("bogus"); err == nil {
		t.Fatal("expected error for invalid level, got nil")
	}
	// Level should remain unchanged after failed SetLevel.
	if got := logger.Level.Level(); got != zapcore.InfoLevel {
		t.Errorf("expected level to remain info after failed SetLevel, got %v", got)
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input   string
		want    zapcore.Level
		wantErr bool
	}{
		{"debug", zapcore.DebugLevel, false},
		{"info", zapcore.InfoLevel, false},
		{"warn", zapcore.WarnLevel, false},
		{"error", zapcore.ErrorLevel, false},
		{"DEBUG", zapcore.DebugLevel, false},
		{"INFO", zapcore.InfoLevel, false},
		{"", zapcore.InfoLevel, false}, // zap treats empty string as InfoLevel
		{"trace", zapcore.InfoLevel, true},
		{"fatal", zapcore.FatalLevel, false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseLevel(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("parseLevel(%q) expected error, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Errorf("parseLevel(%q) unexpected error: %v", tt.input, err)
				return
			}
			if got != tt.want {
				t.Errorf("parseLevel(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
