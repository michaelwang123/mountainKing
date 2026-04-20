// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package observability

import (
	"context"
	"net"
	"strings"

	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// RedisTracingHook implements redis.Hook to create OpenTelemetry spans
// for each Redis command. Span name format: "Redis {command}".
type RedisTracingHook struct {
	tracer   trace.Tracer
	peerName string
}

// NewRedisTracingHook creates a new tracing hook for go-redis.
// addr is the Redis server address used for the net.peer.name attribute.
func NewRedisTracingHook(tracer trace.Tracer, addr string) *RedisTracingHook {
	host := addr
	if h, _, err := net.SplitHostPort(addr); err == nil {
		host = h
	}
	return &RedisTracingHook{
		tracer:   tracer,
		peerName: host,
	}
}

// DialHook returns the default dial hook (no-op wrapper).
func (h *RedisTracingHook) DialHook(next redis.DialHook) redis.DialHook {
	return next
}

// ProcessHook wraps each Redis command with an OpenTelemetry span.
func (h *RedisTracingHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		cmdName := strings.ToUpper(cmd.Name())
		spanName := "Redis " + cmdName

		ctx, span := h.tracer.Start(ctx, spanName)
		defer span.End()

		span.SetAttributes(
			attribute.String("db.system", "redis"),
			attribute.String("db.operation", cmdName),
			attribute.String("net.peer.name", h.peerName),
		)

		err := next(ctx, cmd)
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			span.RecordError(err)
		}
		return err
	}
}

// ProcessPipelineHook wraps pipelined Redis commands with an OpenTelemetry span.
func (h *RedisTracingHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		ctx, span := h.tracer.Start(ctx, "Redis PIPELINE")
		defer span.End()

		span.SetAttributes(
			attribute.String("db.system", "redis"),
			attribute.String("db.operation", "PIPELINE"),
			attribute.String("net.peer.name", h.peerName),
			attribute.Int("db.redis.pipeline_length", len(cmds)),
		)

		err := next(ctx, cmds)
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			span.RecordError(err)
		}
		return err
	}
}
