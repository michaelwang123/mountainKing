// Package observability provides structured logging, metrics, and tracing
// initialization for the GraphQL API service.
package observability

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/example/graphql-api/internal/config"
)

const (
	// tracerName is the instrumentation name used for all spans created by this package.
	tracerName = "github.com/example/graphql-api"
	// shutdownTimeout is the maximum time allowed for flushing trace data on shutdown.
	shutdownTimeout = 5 * time.Second
)

// TracingProvider manages the OpenTelemetry TracerProvider lifecycle.
type TracingProvider struct {
	provider trace.TracerProvider
	tracer   trace.Tracer
	shutdown func(ctx context.Context) error
}

// InitTracing creates and initializes a TracingProvider based on the given config.
// When cfg.Enabled is false, a NoopTracerProvider is used (zero overhead).
// When enabled, an OTLP exporter (gRPC or HTTP) is configured with the specified
// sampling rate.
func InitTracing(cfg config.TracingConfig) (*TracingProvider, error) {
	tp := &TracingProvider{}

	if !cfg.Enabled {
		noopProvider := noop.NewTracerProvider()
		tp.provider = noopProvider
		tp.tracer = noopProvider.Tracer(tracerName)
		tp.shutdown = func(_ context.Context) error { return nil }

		otel.SetTracerProvider(noopProvider)
		return tp, nil
	}

	ctx := context.Background()

	exporter, err := createExporter(ctx, cfg.OTLP)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP exporter: %w", err)
	}

	samplingRate := cfg.SamplingRate
	if samplingRate <= 0 {
		samplingRate = 1.0
	}

	sdkProvider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithSampler(sdktrace.TraceIDRatioBased(samplingRate)),
		sdktrace.WithResource(nil),
	)

	tp.provider = sdkProvider
	tp.tracer = sdkProvider.Tracer(tracerName)
	tp.shutdown = sdkProvider.Shutdown

	// Register as global provider and set W3C Trace Context propagator.
	otel.SetTracerProvider(sdkProvider)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	return tp, nil
}

// createExporter creates an OTLP trace exporter based on the protocol config.
func createExporter(ctx context.Context, cfg config.OTLPConfig) (sdktrace.SpanExporter, error) {
	switch cfg.Protocol {
	case "http":
		return otlptracehttp.New(ctx,
			otlptracehttp.WithEndpoint(cfg.Endpoint),
			otlptracehttp.WithInsecure(),
		)
	default: // "grpc" or unspecified
		return otlptracegrpc.New(ctx,
			otlptracegrpc.WithEndpoint(cfg.Endpoint),
			otlptracegrpc.WithInsecure(),
		)
	}
}

// Shutdown flushes all pending trace data and shuts down the exporter.
// Uses an independent 5s timeout to prevent blocking the overall graceful
// shutdown if the OTLP endpoint is unreachable.
func (tp *TracingProvider) Shutdown(_ context.Context) error {
	if tp.shutdown == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	return tp.shutdown(ctx)
}

// Tracer returns the underlying tracer for creating spans.
func (tp *TracingProvider) Tracer() trace.Tracer {
	return tp.tracer
}

// Provider returns the underlying TracerProvider.
func (tp *TracingProvider) Provider() trace.TracerProvider {
	return tp.provider
}

// StartRequestSpan creates a root span for a GraphQL request.
// Name format: "GraphQL {operationType} {operationName}"
// Attributes: graphql.operation.name, graphql.operation.type, http.method, http.url
func StartRequestSpan(ctx context.Context, tracer trace.Tracer, operationType, operationName, httpMethod, httpURL string) (context.Context, trace.Span) {
	spanName := fmt.Sprintf("GraphQL %s %s", operationType, operationName)
	ctx, span := tracer.Start(ctx, spanName)
	span.SetAttributes(
		attribute.String("graphql.operation.name", operationName),
		attribute.String("graphql.operation.type", operationType),
		attribute.String("http.method", httpMethod),
		attribute.String("http.url", httpURL),
	)
	return ctx, span
}

// StartResolverSpan creates a child span for a resolver execution.
// Name format: "Resolver {fieldName}"
// Attributes: graphql.field.name, graphql.field.type, graphql.datasource
func StartResolverSpan(ctx context.Context, tracer trace.Tracer, fieldName, fieldType, datasource string) (context.Context, trace.Span) {
	spanName := fmt.Sprintf("Resolver %s", fieldName)
	ctx, span := tracer.Start(ctx, spanName)
	span.SetAttributes(
		attribute.String("graphql.field.name", fieldName),
		attribute.String("graphql.field.type", fieldType),
		attribute.String("graphql.datasource", datasource),
	)
	return ctx, span
}

// StartStarRocksSpan creates a child span for a StarRocks SQL query.
// Attributes: db.system=starrocks, db.statement (sanitized), db.datasource
func StartStarRocksSpan(ctx context.Context, tracer trace.Tracer, sanitizedSQL, datasource string) (context.Context, trace.Span) {
	ctx, span := tracer.Start(ctx, "StarRocks Query")
	span.SetAttributes(
		attribute.String("db.system", "starrocks"),
		attribute.String("db.statement", sanitizedSQL),
		attribute.String("db.datasource", datasource),
	)
	return ctx, span
}

// StartPrometheusSpan creates a child span for a Prometheus PromQL query.
// Attributes: db.system=prometheus, db.statement, db.datasource
func StartPrometheusSpan(ctx context.Context, tracer trace.Tracer, promQL, datasource string) (context.Context, trace.Span) {
	ctx, span := tracer.Start(ctx, "Prometheus Query")
	span.SetAttributes(
		attribute.String("db.system", "prometheus"),
		attribute.String("db.statement", promQL),
		attribute.String("db.datasource", datasource),
	)
	return ctx, span
}

// RecordSpanError sets the span status to Error and records an error event.
func RecordSpanError(span trace.Span, err error) {
	if span == nil || err == nil {
		return
	}
	span.SetStatus(codes.Error, err.Error())
	span.RecordError(err)
}

// ExtractTraceID returns the trace ID string from the current span context.
// Returns an empty string if no valid trace ID is present.
func ExtractTraceID(ctx context.Context) string {
	sc := trace.SpanContextFromContext(ctx)
	if sc.HasTraceID() {
		return sc.TraceID().String()
	}
	return ""
}

// ExtractTraceContext extracts W3C traceparent from an inbound HTTP request
// and returns a context with the extracted span context.
func ExtractTraceContext(ctx context.Context, r *http.Request) context.Context {
	propagator := otel.GetTextMapPropagator()
	return propagator.Extract(ctx, propagation.HeaderCarrier(r.Header))
}

// InjectTraceContext injects the current trace context (W3C traceparent)
// into outbound HTTP request headers.
func InjectTraceContext(ctx context.Context, r *http.Request) {
	propagator := otel.GetTextMapPropagator()
	propagator.Inject(ctx, propagation.HeaderCarrier(r.Header))
}
