// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package template

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
	"pgregory.net/rapid"

	"github.com/michaelwang123/mountainKing/internal/config"
	"github.com/michaelwang123/mountainKing/internal/datasource"
	apierrors "github.com/michaelwang123/mountainKing/internal/errors"
)

// =============================================================================
// Feature: sql-template-engine
// Task 12.3: TemplateEngine 单元测试（使用 MockRawExecutor）
// =============================================================================

// ---------------------------------------------------------------------------
// MockRawExecutor
// ---------------------------------------------------------------------------

// MockRawExecutor implements RawExecutor for testing purposes.
// It tracks concurrent execution count and supports configurable delay and errors.
type MockRawExecutor struct {
	mu             sync.Mutex
	totalCalls     int
	currentRunning int32 // atomic: currently executing goroutines
	maxObserved    int32 // atomic: peak concurrent executions observed
	delay          time.Duration
	data           []map[string]any
	err            error
}

func (m *MockRawExecutor) ExecuteRaw(ctx context.Context, query string, args ...any) (*datasource.QueryResult, error) {
	// Track concurrency.
	current := atomic.AddInt32(&m.currentRunning, 1)
	defer atomic.AddInt32(&m.currentRunning, -1)

	// Update peak concurrency.
	for {
		old := atomic.LoadInt32(&m.maxObserved)
		if current <= old {
			break
		}
		if atomic.CompareAndSwapInt32(&m.maxObserved, old, current) {
			break
		}
	}

	m.mu.Lock()
	m.totalCalls++
	m.mu.Unlock()

	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if m.err != nil {
		return nil, m.err
	}

	return &datasource.QueryResult{Data: m.data}, nil
}

// TotalCalls returns the total number of ExecuteRaw calls.
func (m *MockRawExecutor) TotalCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.totalCalls
}

// MaxObservedConcurrency returns the peak number of concurrent ExecuteRaw calls.
func (m *MockRawExecutor) MaxObservedConcurrency() int32 {
	return atomic.LoadInt32(&m.maxObserved)
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// createTestEngine creates a TemplateEngine with a temp directory containing
// a simple template file and the given MockRawExecutor.
func createTestEngine(t *testing.T, mock *MockRawExecutor, maxConcurrent int) *TemplateEngine {
	t.Helper()
	return createTestEngineWithTemplates(t, mock, maxConcurrent, []testTemplateFile{
		{name: "test_query", file: "test_query.sql.tmpl", content: "SELECT * FROM users WHERE id = {{.Params.id | safeInt}}"},
	}, nil)
}

type testTemplateFile struct {
	name    string
	file    string
	content string
	params  []config.TemplateParamConfig
}

// createTestEngineWithTemplates creates a TemplateEngine with custom template files.
func createTestEngineWithTemplates(t *testing.T, mock *MockRawExecutor, maxConcurrent int, templates []testTemplateFile, extraCfgFn func(*config.SQLTemplatesConfig)) *TemplateEngine {
	t.Helper()

	tmpDir := t.TempDir()

	var cfgTemplates []config.TemplateConfig
	for _, tmpl := range templates {
		// Create the template file.
		filePath := filepath.Join(tmpDir, tmpl.file)
		dir := filepath.Dir(filePath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("failed to create dir %s: %v", dir, err)
		}
		if err := os.WriteFile(filePath, []byte(tmpl.content), 0o644); err != nil {
			t.Fatalf("failed to write template file %s: %v", filePath, err)
		}

		params := tmpl.params
		if params == nil {
			// Default: single "id" int param
			params = []config.TemplateParamConfig{
				{Name: "id", Type: "int", Required: true},
			}
		}

		cfgTemplates = append(cfgTemplates, config.TemplateConfig{
			Name:       tmpl.name,
			File:       tmpl.file,
			Parameters: params,
		})
	}

	sqlCfg := config.SQLTemplatesConfig{
		Enabled:              true,
		DatasourceName:       "test_ds",
		BaseDir:              tmpDir,
		RenderTimeout:        5 * time.Second,
		MaxRenderedSQLLen:    65536,
		MaxConcurrentQueries: maxConcurrent,
		Templates:            cfgTemplates,
	}

	if extraCfgFn != nil {
		extraCfgFn(&sqlCfg)
	}

	te, err := NewTemplateEngine(TemplateEngineConfig{
		Config:         sqlCfg,
		GraphQLCfg:     config.GraphQLConfig{MaxResultRows: 10000},
		DatasourceName: "test_ds",
		Executor:       mock,
		Logger:         zap.NewNop(),
	})
	if err != nil {
		t.Fatalf("failed to create TemplateEngine: %v", err)
	}
	return te
}

// =============================================================================
// Property 23: 并发限制
// **Validates: Requirements 4.9**
// No more than max_concurrent_queries execute simultaneously.
// =============================================================================

func TestProperty23_ConcurrencyLimit(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		maxConcurrent := rapid.IntRange(1, 5).Draw(rt, "maxConcurrent")

		mock := &MockRawExecutor{
			delay: 50 * time.Millisecond,
			data:  []map[string]any{{"id": float64(1)}},
		}

		te := createTestEngine(t, mock, maxConcurrent)

		// Launch more goroutines than the semaphore allows.
		numGoroutines := maxConcurrent * 3
		var wg sync.WaitGroup
		wg.Add(numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			go func() {
				defer wg.Done()
				_, _ = te.Execute(context.Background(), &TemplateQueryRequest{
					TemplateName: "test_query",
					Parameters:   map[string]any{"id": float64(1)},
				})
			}()
		}

		wg.Wait()

		observed := mock.MaxObservedConcurrency()
		if int(observed) > maxConcurrent {
			rt.Fatalf("observed %d concurrent executions, but max_concurrent_queries=%d",
				observed, maxConcurrent)
		}
	})
}

// =============================================================================
// Property 24: 结果截断 + 警告
// **Validates: Requirements 5.6**
// Engine returns data correctly from the executor.
// (Result truncation + warnings are tested at resolver level, but verify
// engine passes data through correctly.)
// =============================================================================

func TestProperty24_ResultDataPassthrough(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		numRows := rapid.IntRange(0, 20).Draw(rt, "numRows")
		data := make([]map[string]any, numRows)
		for i := 0; i < numRows; i++ {
			data[i] = map[string]any{
				"id":   float64(i + 1),
				"name": fmt.Sprintf("user_%d", i+1),
			}
		}

		mock := &MockRawExecutor{data: data}
		te := createTestEngine(t, mock, 10)

		result, err := te.Execute(context.Background(), &TemplateQueryRequest{
			TemplateName: "test_query",
			Parameters:   map[string]any{"id": float64(1)},
		})
		if err != nil {
			rt.Fatalf("unexpected error: %v", err)
		}

		if len(result.Data) != numRows {
			rt.Fatalf("expected %d rows, got %d", numRows, len(result.Data))
		}
	})
}

// =============================================================================
// Property 25: 查询超时保护
// **Validates: Requirements 5.5**
// When context is cancelled, Execute returns a context error.
// =============================================================================

func TestProperty25_QueryTimeoutProtection(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		mock := &MockRawExecutor{
			delay: 5 * time.Second, // long delay to ensure timeout
			data:  []map[string]any{{"id": float64(1)}},
		}

		te := createTestEngine(t, mock, 10)

		// Create a context that is already cancelled.
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := te.Execute(ctx, &TemplateQueryRequest{
			TemplateName: "test_query",
			Parameters:   map[string]any{"id": float64(1)},
		})

		if err == nil {
			rt.Fatal("expected error when context is cancelled, got nil")
		}

		// The error should be either a context error or a timeout/datasource error.
		// With a cancelled context, the semaphore acquisition or the executor should fail.
		apiErr, ok := err.(*apierrors.APIError)
		if ok {
			// Should be DATASOURCE_TIMEOUT (semaphore wait timeout) or context error
			validCodes := map[string]bool{
				apierrors.ErrDatasourceTimeout:            true,
				apierrors.ErrDatasourceTemplateQueryError: true,
				apierrors.ErrInternalTemplateRenderError:  true,
			}
			if !validCodes[apiErr.Code] {
				rt.Fatalf("unexpected error code %q, err: %v", apiErr.Code, err)
			}
		}
		// Non-APIError context errors are also acceptable
	})
}

// =============================================================================
// Property 27: 接口隔离
// **Validates: Requirements 5.1**
// TemplateEngine only uses RawExecutor interface (compile-time guarantee).
// This is verified at compile time: TemplateEngine.executor is of type
// RawExecutor, not *starrocks.Adapter. The test below confirms that a
// MockRawExecutor (which only implements RawExecutor) works correctly.
// =============================================================================

func TestProperty27_InterfaceIsolation(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		rowCount := rapid.IntRange(1, 10).Draw(rt, "rowCount")
		data := make([]map[string]any, rowCount)
		for i := 0; i < rowCount; i++ {
			data[i] = map[string]any{"id": float64(i + 1)}
		}

		// MockRawExecutor only implements RawExecutor, not DataSource.
		// If TemplateEngine required anything beyond RawExecutor, this would
		// fail to compile or panic at runtime.
		mock := &MockRawExecutor{data: data}
		te := createTestEngine(t, mock, 10)

		result, err := te.Execute(context.Background(), &TemplateQueryRequest{
			TemplateName: "test_query",
			Parameters:   map[string]any{"id": float64(1)},
		})
		if err != nil {
			rt.Fatalf("Execute failed with MockRawExecutor: %v", err)
		}
		if len(result.Data) != rowCount {
			rt.Fatalf("expected %d rows, got %d", rowCount, len(result.Data))
		}
	})
}

// =============================================================================
// Unit Tests
// =============================================================================

// 1. Execute with valid template and params succeeds
func TestEngine_ExecuteValidTemplate(t *testing.T) {
	mock := &MockRawExecutor{
		data: []map[string]any{
			{"id": float64(1), "name": "Alice"},
			{"id": float64(2), "name": "Bob"},
		},
	}
	te := createTestEngine(t, mock, 10)

	result, err := te.Execute(context.Background(), &TemplateQueryRequest{
		TemplateName: "test_query",
		Parameters:   map[string]any{"id": float64(42)},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Data) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(result.Data))
	}
	if mock.TotalCalls() != 1 {
		t.Fatalf("expected 1 executor call, got %d", mock.TotalCalls())
	}
}

// 2. Execute with non-existent template returns VALIDATION_TEMPLATE_NOT_FOUND
func TestEngine_ExecuteNonExistentTemplate(t *testing.T) {
	mock := &MockRawExecutor{}
	te := createTestEngine(t, mock, 10)

	_, err := te.Execute(context.Background(), &TemplateQueryRequest{
		TemplateName: "nonexistent_template",
		Parameters:   map[string]any{},
	})
	if err == nil {
		t.Fatal("expected error for non-existent template")
	}
	apiErr, ok := err.(*apierrors.APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.Code != apierrors.ErrValidationTemplateNotFound {
		t.Fatalf("expected error code %q, got %q", apierrors.ErrValidationTemplateNotFound, apiErr.Code)
	}
}

// 3. Execute with invalid params returns validation error
func TestEngine_ExecuteInvalidParams(t *testing.T) {
	mock := &MockRawExecutor{}
	te := createTestEngine(t, mock, 10)

	// "id" is required but not provided
	_, err := te.Execute(context.Background(), &TemplateQueryRequest{
		TemplateName: "test_query",
		Parameters:   map[string]any{},
	})
	if err == nil {
		t.Fatal("expected error for missing required param")
	}
	apiErr, ok := err.(*apierrors.APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.Code != apierrors.ErrValidationMissingParameter {
		t.Fatalf("expected error code %q, got %q", apierrors.ErrValidationMissingParameter, apiErr.Code)
	}
}

// 4. Execute with cancelled context returns timeout
func TestEngine_ExecuteCancelledContext(t *testing.T) {
	mock := &MockRawExecutor{
		delay: 5 * time.Second,
		data:  []map[string]any{{"id": float64(1)}},
	}
	te := createTestEngine(t, mock, 10)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := te.Execute(ctx, &TemplateQueryRequest{
		TemplateName: "test_query",
		Parameters:   map[string]any{"id": float64(1)},
	})
	if err == nil {
		t.Fatal("expected error when context is cancelled")
	}
}

// 5. Concurrent execution respects semaphore limit
func TestEngine_ConcurrencySemaphore(t *testing.T) {
	maxConcurrent := 3
	mock := &MockRawExecutor{
		delay: 100 * time.Millisecond,
		data:  []map[string]any{{"id": float64(1)}},
	}
	te := createTestEngine(t, mock, maxConcurrent)

	numGoroutines := 10
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			_, _ = te.Execute(context.Background(), &TemplateQueryRequest{
				TemplateName: "test_query",
				Parameters:   map[string]any{"id": float64(1)},
			})
		}()
	}

	wg.Wait()

	observed := mock.MaxObservedConcurrency()
	if int(observed) > maxConcurrent {
		t.Fatalf("observed %d concurrent executions, but max_concurrent_queries=%d",
			observed, maxConcurrent)
	}
	if mock.TotalCalls() != numGoroutines {
		t.Fatalf("expected %d total calls, got %d", numGoroutines, mock.TotalCalls())
	}
}

// 6. ListTemplates returns all registered templates
func TestEngine_ListTemplates(t *testing.T) {
	mock := &MockRawExecutor{}
	te := createTestEngineWithTemplates(t, mock, 10, []testTemplateFile{
		{name: "tmpl_a", file: "tmpl_a.sql.tmpl", content: "SELECT 1"},
		{name: "tmpl_b", file: "tmpl_b.sql.tmpl", content: "SELECT 2"},
		{name: "tmpl_c", file: "tmpl_c.sql.tmpl", content: "SELECT 3"},
	}, nil)

	infos := te.ListTemplates(nil, nil)
	if len(infos) != 3 {
		t.Fatalf("expected 3 templates, got %d", len(infos))
	}

	names := make(map[string]bool)
	for _, info := range infos {
		names[info.Name] = true
	}
	for _, expected := range []string{"tmpl_a", "tmpl_b", "tmpl_c"} {
		if !names[expected] {
			t.Fatalf("expected template %q in list", expected)
		}
	}
}

// 7. DatasourceName returns correct name
func TestEngine_DatasourceName(t *testing.T) {
	mock := &MockRawExecutor{}
	te := createTestEngine(t, mock, 10)

	if te.DatasourceName() != "test_ds" {
		t.Fatalf("expected datasource name %q, got %q", "test_ds", te.DatasourceName())
	}
}
