// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

// Package audit provides an independent audit logger for recording
// authenticated operations. It is separate from the application logger.
package audit

import (
	"os"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/michaelwang123/mountainKing/internal/config"
)

// LogEntry represents a single audit log record.
type LogEntry struct {
	Principal   string // JWT sub or API Key ID
	Time        time.Time
	Operation   string // e.g. "query", "mutation"
	Datasource  string // target datasource name
	Success     bool
	ExtraFields map[string]string // optional extra fields (e.g. "template_name")
}

// AuditLogger writes audit entries to a dedicated output, independent
// from the application logger.
type AuditLogger struct {
	logger  *zap.Logger
	enabled bool
	closer  func() error // optional file closer
}

// NewAuditLogger creates an AuditLogger based on the provided config.
// When disabled, Log is a no-op.
func NewAuditLogger(cfg config.AuditConfig) (*AuditLogger, error) {
	if !cfg.Enabled {
		return &AuditLogger{enabled: false}, nil
	}

	var ws zapcore.WriteSyncer
	var closer func() error
	switch cfg.Output {
	case "file":
		f, err := os.OpenFile(cfg.FilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return nil, err
		}
		ws = zapcore.AddSync(f)
		closer = f.Close
	default: // "stdout" or unset
		ws = zapcore.AddSync(os.Stdout)
	}

	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.TimeKey = "timestamp"
	encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder

	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderCfg),
		ws,
		zapcore.InfoLevel,
	)

	return &AuditLogger{
		logger:  zap.New(core),
		enabled: true,
		closer:  closer,
	}, nil
}

// Log records an audit entry. It is a no-op when the logger is disabled.
func (a *AuditLogger) Log(entry LogEntry) {
	if !a.enabled || a.logger == nil {
		return
	}

	result := "failure"
	if entry.Success {
		result = "success"
	}

	fields := []zap.Field{
		zap.String("principal", entry.Principal),
		zap.Time("operation_time", entry.Time),
		zap.String("operation", entry.Operation),
		zap.String("datasource", entry.Datasource),
		zap.String("result", result),
	}
	for k, v := range entry.ExtraFields {
		fields = append(fields, zap.String(k, v))
	}
	a.logger.Info("audit", fields...)
}

// Sync flushes any buffered log entries.
func (a *AuditLogger) Sync() error {
	if a.logger != nil {
		return a.logger.Sync()
	}
	return nil
}

// Close flushes and releases resources (e.g. file handles).
func (a *AuditLogger) Close() error {
	if a.logger != nil {
		_ = a.logger.Sync()
	}
	if a.closer != nil {
		return a.closer()
	}
	return nil
}
