// Package observability provides structured logging, metrics, and tracing
// initialization for the GraphQL API service.
package observability

import (
	"fmt"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// LoggerConfig holds logging configuration.
type LoggerConfig struct {
	Level              string        // "debug", "info", "warn", "error"
	Format             string        // "json" (only json supported)
	SlowQueryThreshold time.Duration // queries slower than this trigger WARN log (default 1s)
}

// Logger wraps a zap.Logger with an AtomicLevel for hot-reloading.
type Logger struct {
	*zap.Logger
	Level              zap.AtomicLevel
	SlowQueryThreshold time.Duration
}

// NewLogger creates a new structured JSON logger with the given config.
// The returned Logger has an AtomicLevel that can be changed at runtime
// for hot-reload support.
func NewLogger(cfg LoggerConfig) (*Logger, error) {
	lvl, err := parseLevel(cfg.Level)
	if err != nil {
		return nil, fmt.Errorf("invalid log level %q: %w", cfg.Level, err)
	}

	atomicLevel := zap.NewAtomicLevelAt(lvl)

	zapCfg := zap.NewProductionConfig()
	zapCfg.Encoding = "json"
	zapCfg.Level = atomicLevel
	zapCfg.EncoderConfig.TimeKey = "timestamp"
	zapCfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	zapLogger, err := zapCfg.Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build zap logger: %w", err)
	}

	threshold := cfg.SlowQueryThreshold
	if threshold <= 0 {
		threshold = time.Second
	}

	return &Logger{
		Logger:             zapLogger,
		Level:              atomicLevel,
		SlowQueryThreshold: threshold,
	}, nil
}

// SetLevel changes the log level at runtime (for hot-reload).
func (l *Logger) SetLevel(level string) error {
	lvl, err := parseLevel(level)
	if err != nil {
		return fmt.Errorf("invalid log level %q: %w", level, err)
	}
	l.Level.SetLevel(lvl)
	return nil
}

// parseLevel converts a string level to zapcore.Level.
func parseLevel(level string) (zapcore.Level, error) {
	var lvl zapcore.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		return lvl, fmt.Errorf("unsupported log level %q: %w", level, err)
	}
	return lvl, nil
}
