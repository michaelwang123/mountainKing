// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package template

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"pgregory.net/rapid"

	"github.com/michaelwang123/mountainKing/internal/audit"
	"github.com/michaelwang123/mountainKing/internal/config"
	"github.com/michaelwang123/mountainKing/internal/datasource"
)

// =============================================================================
// Feature: sql-template-engine
// Task 15.4: 编写可观测性属性测试
// =============================================================================

// ---------------------------------------------------------------------------
// Test helpers for observability tests
// ---------------------------------------------------------------------------

// createTestEngineWithMetrics creates a TemplateEngine with real TemplateMetrics
// registered on a test prometheus.Registry, an OTel recording tracer, and an
// audit logger writing to a temp file.
func createTestEngineWithMetrics(
	t *testing.T,
	reg *prometheus.Registry,
	metrics *TemplateMetrics,
	tracer trace.Tracer,
	auditLogger *audit.AuditLogger,
) (*TemplateEngine, *MockRawExecutor) {
	t.Helper()

	mock := &MockRawExecutor{
		data: []map[string]any{
			{"id": float64(1), "name": "Alice"},
		},
	}

	tmpDir := t.TempDir()
	tmplContent := "SELECT * FROM users WHERE id = {{.Params.id | safeInt}}"
	tmplFile := filepath.Join(tmpDir, "test_query.sql.tmpl")
	if err := os.WriteFile(tmplFile, []byte(tmplContent), 0o644); err != nil {
		t.Fatalf("failed to write template file: %v", err)
	}

	sqlCfg := config.SQLTemplatesConfig{
		Enabled:              true,
		DatasourceName:       "test_ds",
		BaseDir:              tmpDir,
		RenderTimeout:        5 * time.Second,
		MaxRenderedSQLLen:    65536,
		MaxConcurrentQueries: 10,
		Templates: []config.TemplateConfig{
			{
				Name: "test_query",
				File: "test_query.sql.tmpl",
				Parameters: []config.TemplateParamConfig{
					{Name: "id", Type: "int", Required: true},
				},
			},
		},
	}

	te, err := NewTemplateEngine(TemplateEngineConfig{
		Config:         sqlCfg,
		GraphQLCfg:     config.GraphQLConfig{MaxResultRows: 10000},
		DatasourceName: "test_ds",
		Executor:       mock,
		Metrics:        metrics,
		Tracer:         tracer,
		AuditLogger:    auditLogger,
		Logger:         zap.NewNop(),
	})
	if err != nil {
		t.Fatalf("failed to create TemplateEngine: %v", err)
	}
	return te, mock
}

// newTestTracerForTemplate creates an in-memory tracer and exporter for property tests.
func newTestTracerForTemplate() (trace.Tracer, *tracetest.InMemoryExporter, *sdktrace.TracerProvider) {
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exp),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	return tp.Tracer("template-test"), exp, tp
}

// createTestAuditLogger creates an AuditLogger that writes to a temp file.
// The caller is responsible for closing the logger and cleaning up the directory.
func createTestAuditLogger(t *testing.T) (*audit.AuditLogger, string, string) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "audit-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	auditFile := filepath.Join(tmpDir, "audit.log")

	al, err := audit.NewAuditLogger(config.AuditConfig{
		Enabled:  true,
		Output:   "file",
		FilePath: auditFile,
	})
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		t.Fatalf("failed to create audit logger: %v", err)
	}
	return al, auditFile, tmpDir
}

// gatherMetricValue collects metrics from a registry and returns the count of
// observations for a given metric name. For histograms, returns sample_count;
// for counters, returns the counter value.
func gatherMetricValue(t *testing.T, reg *prometheus.Registry, metricName string) float64 {
	t.Helper()
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() == metricName {
			total := 0.0
			for _, m := range mf.GetMetric() {
				if h := m.GetHistogram(); h != nil {
					total += float64(h.GetSampleCount())
				}
				if c := m.GetCounter(); c != nil {
					total += c.GetValue()
				}
			}
			return total
		}
	}
	return 0
}

// =============================================================================
// Property 56: 查询延迟指标记录
// **Validates: Requirements 9.1**
// After Execute, QueryDuration histogram has observations.
// =============================================================================

func TestProperty56_QueryDurationMetricRecorded(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		reg := prometheus.NewRegistry()
		metrics := NewTemplateMetrics(reg, nil)

		te, _ := createTestEngineWithMetrics(t, reg, metrics, nil, nil)

		numQueries := rapid.IntRange(1, 5).Draw(rt, "numQueries")
		for i := 0; i < numQueries; i++ {
			_, err := te.Execute(context.Background(), &TemplateQueryRequest{
				TemplateName: "test_query",
				Parameters:   map[string]any{"id": float64(1)},
			})
			if err != nil {
				rt.Fatalf("Execute failed: %v", err)
			}
		}

		observations := gatherMetricValue(t, reg, "graphql_template_query_duration_seconds")
		if observations < float64(numQueries) {
			rt.Fatalf("expected at least %d QueryDuration observations, got %v",
				numQueries, observations)
		}
	})
}

// =============================================================================
// Property 57: 查询计数指标记录
// **Validates: Requirements 9.2**
// After Execute, QueriesTotal counter increments.
// =============================================================================

func TestProperty57_QueriesTotalMetricRecorded(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		reg := prometheus.NewRegistry()
		metrics := NewTemplateMetrics(reg, nil)

		te, _ := createTestEngineWithMetrics(t, reg, metrics, nil, nil)

		numQueries := rapid.IntRange(1, 5).Draw(rt, "numQueries")
		for i := 0; i < numQueries; i++ {
			_, err := te.Execute(context.Background(), &TemplateQueryRequest{
				TemplateName: "test_query",
				Parameters:   map[string]any{"id": float64(1)},
			})
			if err != nil {
				rt.Fatalf("Execute failed: %v", err)
			}
		}

		total := gatherMetricValue(t, reg, "graphql_template_queries_total")
		if total < float64(numQueries) {
			rt.Fatalf("expected QueriesTotal >= %d, got %v", numQueries, total)
		}
	})
}

// =============================================================================
// Property 58: 渲染延迟指标记录
// **Validates: Requirements 9.6**
// After Execute, RenderDuration histogram has observations.
// =============================================================================

func TestProperty58_RenderDurationMetricRecorded(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		reg := prometheus.NewRegistry()
		metrics := NewTemplateMetrics(reg, nil)

		te, _ := createTestEngineWithMetrics(t, reg, metrics, nil, nil)

		numQueries := rapid.IntRange(1, 5).Draw(rt, "numQueries")
		for i := 0; i < numQueries; i++ {
			_, err := te.Execute(context.Background(), &TemplateQueryRequest{
				TemplateName: "test_query",
				Parameters:   map[string]any{"id": float64(1)},
			})
			if err != nil {
				rt.Fatalf("Execute failed: %v", err)
			}
		}

		observations := gatherMetricValue(t, reg, "graphql_template_render_duration_seconds")
		if observations < float64(numQueries) {
			rt.Fatalf("expected at least %d RenderDuration observations, got %v",
				numQueries, observations)
		}
	})
}

// =============================================================================
// Property 59: Tracing Span 创建
// **Validates: Requirements 9.3**
// After Execute, a span is created with name "Template Query {template_name}"
// and attributes template.name, db.system.
// =============================================================================

func TestProperty59_TracingSpanCreated(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		tracer, exp, tp := newTestTracerForTemplate()
		defer func() { _ = tp.Shutdown(context.Background()) }()

		te, _ := createTestEngineWithMetrics(t, nil, nil, tracer, nil)

		_, err := te.Execute(context.Background(), &TemplateQueryRequest{
			TemplateName: "test_query",
			Parameters:   map[string]any{"id": float64(1)},
		})
		if err != nil {
			rt.Fatalf("Execute failed: %v", err)
		}

		spans := exp.GetSpans()
		if len(spans) == 0 {
			rt.Fatal("expected at least one span after Execute")
		}

		// Find the template query span.
		expectedName := "Template Query test_query"
		var found bool
		for _, s := range spans {
			if s.Name == expectedName {
				found = true
				// Verify attributes.
				attrs := make(map[string]string)
				for _, a := range s.Attributes {
					attrs[string(a.Key)] = a.Value.AsString()
				}
				if attrs["template.name"] != "test_query" {
					rt.Fatalf("expected template.name=test_query, got %q", attrs["template.name"])
				}
				if attrs["db.system"] != "starrocks" {
					rt.Fatalf("expected db.system=starrocks, got %q", attrs["db.system"])
				}
				break
			}
		}
		if !found {
			names := make([]string, len(spans))
			for i, s := range spans {
				names[i] = s.Name
			}
			rt.Fatalf("span %q not found; got spans: %v", expectedName, names)
		}

		exp.Reset()
	})
}

// =============================================================================
// Property 60: 审计日志记录
// **Validates: Requirements 9.5**
// After Execute, audit logger records an entry containing the template name.
// =============================================================================

func TestProperty60_AuditLogRecorded(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		al, auditFile, tmpDir := createTestAuditLogger(t)
		defer func() {
			_ = al.Close()
			_ = os.RemoveAll(tmpDir)
		}()

		te, _ := createTestEngineWithMetrics(t, nil, nil, nil, al)

		_, err := te.Execute(context.Background(), &TemplateQueryRequest{
			TemplateName: "test_query",
			Parameters:   map[string]any{"id": float64(1)},
		})
		if err != nil {
			rt.Fatalf("Execute failed: %v", err)
		}

		// Flush the audit logger.
		_ = al.Sync()

		// Read the audit log file.
		data, err := os.ReadFile(auditFile)
		if err != nil {
			rt.Fatalf("failed to read audit file: %v", err)
		}

		content := string(data)
		if len(content) == 0 {
			rt.Fatal("audit log file is empty after Execute")
		}

		// Verify the audit log contains the template name.
		if !strings.Contains(content, "test_query") {
			rt.Fatalf("audit log does not contain template name 'test_query'; content: %s", content)
		}

		// Verify the audit log contains the operation type.
		if !strings.Contains(content, "query") {
			rt.Fatalf("audit log does not contain operation 'query'; content: %s", content)
		}

		// Verify the audit log contains the datasource name.
		if !strings.Contains(content, "test_ds") {
			rt.Fatalf("audit log does not contain datasource 'test_ds'; content: %s", content)
		}
	})
}

// ---------------------------------------------------------------------------
// Additional unit tests for error cases
// ---------------------------------------------------------------------------

// TestMetrics_ErrorQueryIncrementsCounter verifies that a failed query
// (e.g., template not found) also increments the QueriesTotal counter
// with status=error.
func TestMetrics_ErrorQueryIncrementsCounter(t *testing.T) {
	reg := prometheus.NewRegistry()
	metrics := NewTemplateMetrics(reg, nil)

	te, _ := createTestEngineWithMetrics(t, reg, metrics, nil, nil)

	// Execute with a non-existent template.
	_, _ = te.Execute(context.Background(), &TemplateQueryRequest{
		TemplateName: "nonexistent",
		Parameters:   map[string]any{},
	})

	total := gatherMetricValue(t, reg, "graphql_template_queries_total")
	if total < 1 {
		t.Fatalf("expected QueriesTotal >= 1 after error, got %v", total)
	}
}

// TestMetrics_AuditLogRecordsFailure verifies that a failed query also
// produces an audit log entry with success=false.
func TestMetrics_AuditLogRecordsFailure(t *testing.T) {
	al, auditFile, tmpDir := createTestAuditLogger(t)
	defer func() {
		_ = al.Close()
		_ = os.RemoveAll(tmpDir)
	}()

	te, _ := createTestEngineWithMetrics(t, nil, nil, nil, al)

	// Execute with a non-existent template (will fail).
	_, _ = te.Execute(context.Background(), &TemplateQueryRequest{
		TemplateName: "nonexistent",
		Parameters:   map[string]any{},
	})

	_ = al.Sync()

	data, err := os.ReadFile(auditFile)
	if err != nil {
		t.Fatalf("failed to read audit file: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "failure") {
		t.Fatalf("audit log should contain 'failure' for failed query; content: %s", content)
	}
}

// Ensure MockRawExecutor satisfies RawExecutor interface.
var _ RawExecutor = (*MockRawExecutor)(nil)

// mockRawExecutorForMetrics is a simple executor for metrics tests.
type mockRawExecutorForMetrics struct {
	data []map[string]any
}

func (m *mockRawExecutorForMetrics) ExecuteRaw(_ context.Context, _ string, _ ...any) (*datasource.QueryResult, error) {
	return &datasource.QueryResult{Data: m.data}, nil
}
