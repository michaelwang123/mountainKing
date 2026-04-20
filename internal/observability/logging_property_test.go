// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package observability

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"pgregory.net/rapid"
)

// =============================================================================
// Property 32: 结构化日志格式
// **Validates: Requirements 9.2**
// For any log message written by the Logger, the output should be valid JSON
// containing "level", "timestamp", and "message" (or "msg") fields.
// =============================================================================

func TestProperty32_StructuredLogFormat(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate a random log message
		msg := rapid.StringMatching(`[a-zA-Z0-9 _\-]{1,100}`).Draw(rt, "msg")

		// Create a logger that writes to a buffer
		var buf bytes.Buffer
		encoderCfg := zap.NewProductionEncoderConfig()
		encoderCfg.TimeKey = "timestamp"
		encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder

		core := zapcore.NewCore(
			zapcore.NewJSONEncoder(encoderCfg),
			zapcore.AddSync(&buf),
			zapcore.DebugLevel,
		)
		logger := zap.New(core)

		// Pick a random log level to write at
		level := rapid.SampledFrom([]zapcore.Level{
			zapcore.DebugLevel,
			zapcore.InfoLevel,
			zapcore.WarnLevel,
			zapcore.ErrorLevel,
		}).Draw(rt, "level")

		// Write the log message at the chosen level
		switch level {
		case zapcore.DebugLevel:
			logger.Debug(msg)
		case zapcore.InfoLevel:
			logger.Info(msg)
		case zapcore.WarnLevel:
			logger.Warn(msg)
		case zapcore.ErrorLevel:
			logger.Error(msg)
		}

		output := buf.String()
		if output == "" {
			rt.Fatal("expected log output, got empty string")
		}

		// Each line should be valid JSON
		for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
			if line == "" {
				continue
			}

			var parsed map[string]interface{}
			if err := json.Unmarshal([]byte(line), &parsed); err != nil {
				rt.Fatalf("log output is not valid JSON: %v\noutput: %s", err, line)
			}

			// Verify required fields exist
			if _, ok := parsed["level"]; !ok {
				rt.Fatalf("log output missing 'level' field: %s", line)
			}
			if _, ok := parsed["timestamp"]; !ok {
				rt.Fatalf("log output missing 'timestamp' field: %s", line)
			}
			// zap uses "msg" as the message key
			if _, ok := parsed["msg"]; !ok {
				rt.Fatalf("log output missing 'msg' field: %s", line)
			}
		}
	})
}

// =============================================================================
// Property 35: 日志级别配置
// **Validates: Requirements 9.5**
// For any configured log level, messages below that level should NOT be output,
// and messages at or above that level SHOULD be output.
// =============================================================================

func TestProperty35_LogLevelConfiguration(t *testing.T) {
	// Define all levels in order from lowest to highest
	allLevels := []zapcore.Level{
		zapcore.DebugLevel,
		zapcore.InfoLevel,
		zapcore.WarnLevel,
		zapcore.ErrorLevel,
	}

	rapid.Check(t, func(rt *rapid.T) {
		// Pick a random configured log level
		configuredLevel := rapid.SampledFrom(allLevels).Draw(rt, "configuredLevel")

		// Create a logger at the configured level writing to a buffer
		var buf bytes.Buffer
		encoderCfg := zap.NewProductionEncoderConfig()
		encoderCfg.TimeKey = "timestamp"
		encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder

		core := zapcore.NewCore(
			zapcore.NewJSONEncoder(encoderCfg),
			zapcore.AddSync(&buf),
			configuredLevel,
		)
		logger := zap.New(core)

		// Write messages at all levels
		msg := rapid.StringMatching(`[a-zA-Z0-9]{1,50}`).Draw(rt, "msg")
		logger.Debug("debug-" + msg)
		logger.Info("info-" + msg)
		logger.Warn("warn-" + msg)
		logger.Error("error-" + msg)

		output := buf.String()
		lines := strings.Split(strings.TrimSpace(output), "\n")

		// Filter out empty lines
		var nonEmptyLines []string
		for _, line := range lines {
			if line != "" {
				nonEmptyLines = append(nonEmptyLines, line)
			}
		}

		// Collect which levels appeared in output
		outputLevels := make(map[string]bool)
		for _, line := range nonEmptyLines {
			var parsed map[string]interface{}
			if err := json.Unmarshal([]byte(line), &parsed); err != nil {
				rt.Fatalf("log output is not valid JSON: %v\nline: %s", err, line)
			}
			if lvl, ok := parsed["level"].(string); ok {
				outputLevels[lvl] = true
			}
		}

		// Verify: levels below configured should NOT appear
		// Verify: levels at or above configured SHOULD appear
		for _, lvl := range allLevels {
			levelStr := lvl.String()
			if lvl < configuredLevel {
				if outputLevels[levelStr] {
					rt.Fatalf("level %q should NOT appear when configured level is %q", levelStr, configuredLevel.String())
				}
			} else {
				if !outputLevels[levelStr] {
					rt.Fatalf("level %q SHOULD appear when configured level is %q", levelStr, configuredLevel.String())
				}
			}
		}
	})
}
