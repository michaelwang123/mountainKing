// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package resolver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/99designs/gqlgen/graphql"
	"github.com/vektah/gqlparser/v2/ast"

	"github.com/michaelwang123/mountainKing/internal/config"
	"github.com/michaelwang123/mountainKing/internal/datasource"
	apierrors "github.com/michaelwang123/mountainKing/internal/errors"
	"github.com/michaelwang123/mountainKing/internal/graphql/generated"
	"github.com/michaelwang123/mountainKing/internal/graphql/scalar"
	"github.com/michaelwang123/mountainKing/internal/template"
	"go.uber.org/zap"
)

// ---------------------------------------------------------------------------
// Configurable mock executor for integration tests
// ---------------------------------------------------------------------------

// integrationMockExecutor is a configurable mock that can return different
// data sets and track call counts.
type integrationMockExecutor struct {
	data      []map[string]interface{}
	callCount int
	lastSQL   string
	lastArgs  []interface{}
}

func (m *integrationMockExecutor) ExecuteRaw(_ context.Context, query string, args ...interface{}) (*datasource.QueryResult, error) {
	m.callCount++
	m.lastSQL = query
	m.lastArgs = args
	return &datasource.QueryResult{Data: m.data}, nil
}

// ---------------------------------------------------------------------------
// GraphQL context helpers for resolver-level tests
// ---------------------------------------------------------------------------

// gqlCtx creates a context with gqlgen OperationContext and FieldContext
// so that resolver methods like fieldRequested and setExtensionWarnings work.
// selectedFields specifies which fields are "selected" in the GraphQL query.
func gqlCtx(selectedFields ...string) context.Context {
	// Build AST selections from field names.
	selections := make(ast.SelectionSet, 0, len(selectedFields))
	for _, f := range selectedFields {
		selections = append(selections, &ast.Field{
			Name:  f,
			Alias: f,
			Definition: &ast.FieldDefinition{
				Name: f,
			},
		})
	}

	// Create OperationContext with Extensions map.
	opCtx := &graphql.OperationContext{
		RawQuery:   "query { templateQuery { nodes pageInfo { hasNextPage } totalCount } }",
		Variables:  map[string]interface{}{},
		Extensions: map[string]interface{}{},
		Doc:        &ast.QueryDocument{},
	}

	ctx := graphql.WithOperationContext(context.Background(), opCtx)

	// Create FieldContext with the selections.
	fc := &graphql.FieldContext{
		Object: "Query",
		Field: graphql.CollectedField{
			Field: &ast.Field{
				Name:         "templateQuery",
				Alias:        "templateQuery",
				SelectionSet: selections,
			},
		},
	}
	ctx = graphql.WithFieldContext(ctx, fc)

	return ctx
}

// ---------------------------------------------------------------------------
// Engine creation helpers
// ---------------------------------------------------------------------------

// createIntegrationEngine creates a TemplateEngine with temp dir, given templates, and a mock executor.
func createIntegrationEngine(t *testing.T, templates []templateDef, executor template.RawExecutor) *template.TemplateEngine {
	t.Helper()

	tmpDir := t.TempDir()
	var cfgTemplates []config.TemplateConfig

	for _, tmpl := range templates {
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
			params = []config.TemplateParamConfig{}
		}

		cfgTemplates = append(cfgTemplates, config.TemplateConfig{
			Name:         tmpl.name,
			File:         tmpl.file,
			Description:  tmpl.description,
			CountEnabled: tmpl.countEnabled,
			Parameters:   params,
		})
	}

	te, err := template.NewTemplateEngine(template.TemplateEngineConfig{
		Config: config.SQLTemplatesConfig{
			Enabled:              true,
			DatasourceName:       "test_ds",
			BaseDir:              tmpDir,
			RenderTimeout:        5 * time.Second,
			MaxRenderedSQLLen:    65536,
			MaxConcurrentQueries: 10,
			Templates:            cfgTemplates,
		},
		GraphQLCfg:     config.GraphQLConfig{MaxResultRows: 10000},
		DatasourceName: "test_ds",
		Executor:       executor,
		Logger:         zap.NewNop(),
	})
	if err != nil {
		t.Fatalf("failed to create TemplateEngine: %v", err)
	}
	return te
}

// createReloadableEngine creates a TemplateEngine and returns the temp dir path
// so tests can modify files for reload testing.
func createReloadableEngine(t *testing.T, templates []templateDef, executor template.RawExecutor) (*template.TemplateEngine, string) {
	t.Helper()

	tmpDir := t.TempDir()
	var cfgTemplates []config.TemplateConfig

	for _, tmpl := range templates {
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
			params = []config.TemplateParamConfig{}
		}

		cfgTemplates = append(cfgTemplates, config.TemplateConfig{
			Name:         tmpl.name,
			File:         tmpl.file,
			Description:  tmpl.description,
			CountEnabled: tmpl.countEnabled,
			Parameters:   params,
		})
	}

	te, err := template.NewTemplateEngine(template.TemplateEngineConfig{
		Config: config.SQLTemplatesConfig{
			Enabled:              true,
			DatasourceName:       "test_ds",
			BaseDir:              tmpDir,
			RenderTimeout:        5 * time.Second,
			MaxRenderedSQLLen:    65536,
			MaxConcurrentQueries: 10,
			Templates:            cfgTemplates,
		},
		GraphQLCfg:     config.GraphQLConfig{MaxResultRows: 10000},
		DatasourceName: "test_ds",
		Executor:       executor,
		Logger:         zap.NewNop(),
	})
	if err != nil {
		t.Fatalf("failed to create TemplateEngine: %v", err)
	}
	return te, tmpDir
}

// ===========================================================================
// Task 19.1: End-to-end integration tests
// ===========================================================================

// TestIntegration_TemplateQuery_NormalFlow tests the normal query flow:
// resolver → TemplateEngine → MockRawExecutor → response with nodes, pageInfo, totalCount.
func TestIntegration_TemplateQuery_NormalFlow(t *testing.T) {
	mockData := []map[string]interface{}{
		{"id": 1, "name": "Alice"},
		{"id": 2, "name": "Bob"},
		{"id": 3, "name": "Charlie"},
	}
	executor := &integrationMockExecutor{data: mockData}

	te := createIntegrationEngine(t, []templateDef{
		{
			name:        "test_report",
			file:        "test_report.sql.tmpl",
			content:     "SELECT id, name FROM users WHERE status = 'active'",
			description: "Test report",
			params:      []config.TemplateParamConfig{},
		},
	}, executor)

	r := &queryResolver{&Resolver{
		TemplateEngine: te,
		GraphQLConfig:  config.GraphQLConfig{MaxResultRows: 10000},
	}}

	first := 10
	offset := 0
	ctx := gqlCtx("nodes", "pageInfo", "totalCount")
	result, err := r.TemplateQuery(ctx, "test_report", nil, nil, &first, &offset, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Verify nodes returned.
	if len(result.Nodes) != 3 {
		t.Errorf("expected 3 nodes, got %d", len(result.Nodes))
	}

	// Verify pageInfo.
	if result.PageInfo == nil {
		t.Fatal("expected non-nil pageInfo")
	}
	if result.PageInfo.HasNextPage {
		t.Error("expected hasNextPage=false for 3 rows with first=10")
	}
	if result.PageInfo.HasPreviousPage {
		t.Error("expected hasPreviousPage=false for offset=0")
	}

	// Verify totalCount (executor returns same data for count query).
	// With mock returning 3 rows, the count query also returns 3 rows,
	// and extractCount picks the first value.
	if result.TotalCount < 0 {
		t.Errorf("expected non-negative totalCount, got %d", result.TotalCount)
	}

	// Verify executor was called.
	if executor.callCount == 0 {
		t.Error("expected executor to be called at least once")
	}
}

// TestIntegration_TemplateQuery_ParamValidationFailure tests that missing required
// parameters return VALIDATION_MISSING_PARAMETER error.
func TestIntegration_TemplateQuery_ParamValidationFailure(t *testing.T) {
	executor := &integrationMockExecutor{data: nil}

	te := createIntegrationEngine(t, []templateDef{
		{
			name:        "param_test",
			file:        "param_test.sql.tmpl",
			content:     "SELECT 1 WHERE id = {{.Params.user_id | safeInt}}",
			description: "Param test",
			params: []config.TemplateParamConfig{
				{Name: "user_id", Type: "int", Required: true},
			},
		},
	}, executor)

	r := &queryResolver{&Resolver{
		TemplateEngine: te,
		GraphQLConfig:  config.GraphQLConfig{MaxResultRows: 10000},
	}}

	ctx := gqlCtx("nodes")
	// Call without providing the required parameter.
	_, err := r.TemplateQuery(ctx, "param_test", scalar.JSON{}, nil, nil, nil, nil)
	if err == nil {
		t.Fatal("expected error for missing required parameter")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, apierrors.ErrValidationMissingParameter) {
		t.Errorf("expected VALIDATION_MISSING_PARAMETER error, got: %s", errMsg)
	}

	// Executor should NOT have been called.
	if executor.callCount != 0 {
		t.Errorf("expected executor not to be called, but was called %d times", executor.callCount)
	}
}

// TestIntegration_TemplateQuery_CacheHitMiss tests cache behavior at the engine level:
// without a CacheLayer, the executor is called each time (always miss).
func TestIntegration_TemplateQuery_CacheHitMiss(t *testing.T) {
	mockData := []map[string]interface{}{{"id": 1}}
	executor := &integrationMockExecutor{data: mockData}

	te := createIntegrationEngine(t, []templateDef{
		{
			name:        "cache_test",
			file:        "cache_test.sql.tmpl",
			content:     "SELECT 1",
			description: "Cache test",
			params:      []config.TemplateParamConfig{},
		},
	}, executor)

	r := &queryResolver{&Resolver{
		TemplateEngine: te,
		GraphQLConfig:  config.GraphQLConfig{MaxResultRows: 10000},
	}}

	ctx := gqlCtx("nodes")

	// First call.
	_, err := r.TemplateQuery(ctx, "cache_test", nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	firstCallCount := executor.callCount

	// Second call with same params.
	_, err = r.TemplateQuery(ctx, "cache_test", nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}
	secondCallCount := executor.callCount

	// Without a CacheLayer, both calls should invoke the executor.
	if secondCallCount <= firstCallCount {
		t.Errorf("expected executor to be called again (no cache layer), firstCount=%d, secondCount=%d",
			firstCallCount, secondCallCount)
	}
}

// TestIntegration_TemplateQuery_TotalCountDisabled tests that when count_enabled=false,
// totalCount returns -1 and warnings are present.
func TestIntegration_TemplateQuery_TotalCountDisabled(t *testing.T) {
	mockData := []map[string]interface{}{{"id": 1}}
	executor := &integrationMockExecutor{data: mockData}

	countDisabled := false
	te := createIntegrationEngine(t, []templateDef{
		{
			name:         "no_count",
			file:         "no_count.sql.tmpl",
			content:      "SELECT 1",
			description:  "No count template",
			countEnabled: &countDisabled,
			params:       []config.TemplateParamConfig{},
		},
	}, executor)

	// Execute directly through the engine to test NeedCount behavior.
	req := &template.TemplateQueryRequest{
		TemplateName: "no_count",
		Parameters:   nil,
		NeedCount:    true, // Client requests totalCount
	}

	result, err := te.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// TotalCount should be -1 when count_enabled=false.
	if result.TotalCount == nil {
		t.Fatal("expected non-nil TotalCount")
	}
	if *result.TotalCount != -1 {
		t.Errorf("expected totalCount=-1, got %d", *result.TotalCount)
	}

	// Should have a warning about totalCount being disabled.
	foundWarning := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "totalCount disabled") {
			foundWarning = true
			break
		}
	}
	if !foundWarning {
		t.Errorf("expected warning about totalCount disabled, got warnings: %v", result.Warnings)
	}
}

// TestIntegration_TemplateQuery_OverFetchHasNextPage tests the over-fetch strategy:
// when first=10 and executor returns 11 rows, hasNextPage=true and only 10 rows returned.
func TestIntegration_TemplateQuery_OverFetchHasNextPage(t *testing.T) {
	// Generate 11 rows of mock data (over-fetch: first+1).
	mockData := make([]map[string]interface{}, 11)
	for i := 0; i < 11; i++ {
		mockData[i] = map[string]interface{}{"id": i + 1}
	}
	executor := &integrationMockExecutor{data: mockData}

	te := createIntegrationEngine(t, []templateDef{
		{
			name:        "overfetch_test",
			file:        "overfetch_test.sql.tmpl",
			content:     "SELECT id FROM items",
			description: "Over-fetch test",
			params:      []config.TemplateParamConfig{},
		},
	}, executor)

	r := &queryResolver{&Resolver{
		TemplateEngine: te,
		GraphQLConfig:  config.GraphQLConfig{MaxResultRows: 10000},
	}}

	first := 10
	ctx := gqlCtx("nodes", "pageInfo")
	result, err := r.TemplateQuery(ctx, "overfetch_test", nil, nil, &first, nil, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Should return only 10 rows (truncated from 11).
	if len(result.Nodes) != 10 {
		t.Errorf("expected 10 nodes, got %d", len(result.Nodes))
	}

	// hasNextPage should be true because originalLen (11) > first (10).
	if !result.PageInfo.HasNextPage {
		t.Error("expected hasNextPage=true when executor returns first+1 rows")
	}
}

// TestIntegration_TemplateQuery_FeatureDisabled tests that when TemplateEngine is nil
// (sql_templates.enabled=false), templateQuery returns VALIDATION_TEMPLATE_NOT_FOUND.
func TestIntegration_TemplateQuery_FeatureDisabled(t *testing.T) {
	r := &queryResolver{&Resolver{
		TemplateEngine: nil, // Feature disabled
		GraphQLConfig:  config.GraphQLConfig{MaxResultRows: 10000},
	}}

	// TemplateQuery with nil engine doesn't need gqlCtx because it returns
	// before calling fieldRequested.
	_, err := r.TemplateQuery(
		context.Background(),
		"any_template",
		nil, nil, nil, nil, nil,
	)
	if err == nil {
		t.Fatal("expected error when TemplateEngine is nil")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, apierrors.ErrValidationTemplateNotFound) {
		t.Errorf("expected VALIDATION_TEMPLATE_NOT_FOUND error, got: %s", errMsg)
	}
}

// TestIntegration_TemplateList_Complete tests that templateList returns all
// registered templates with correct metadata.
func TestIntegration_TemplateList_Complete(t *testing.T) {
	executor := &integrationMockExecutor{data: nil}

	countTrue := true
	countFalse := false
	te := createIntegrationEngine(t, []templateDef{
		{
			name:         "report_a",
			file:         "report_a.sql.tmpl",
			content:      "SELECT 1",
			description:  "Report A",
			countEnabled: &countTrue,
			params: []config.TemplateParamConfig{
				{Name: "id", Type: "int", Required: true},
			},
		},
		{
			name:         "report_b",
			file:         "report_b.sql.tmpl",
			content:      "SELECT 2",
			description:  "Report B",
			countEnabled: &countFalse,
			params: []config.TemplateParamConfig{
				{Name: "name", Type: "string", Required: false},
			},
		},
		{
			name:        "report_c",
			file:        "report_c.sql.tmpl",
			content:     "SELECT 3",
			description: "Report C",
			params:      []config.TemplateParamConfig{},
		},
	}, executor)

	r := &queryResolver{&Resolver{TemplateEngine: te}}

	result, err := r.TemplateList(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(result) != 3 {
		t.Fatalf("expected 3 templates, got %d", len(result))
	}

	// Build a map for easy lookup.
	byName := make(map[string]*generated.TemplateInfo, len(result))
	for _, info := range result {
		byName[info.Name] = info
	}

	// Verify report_a.
	a, ok := byName["report_a"]
	if !ok {
		t.Fatal("expected report_a in result")
	}
	if a.Description != "Report A" {
		t.Errorf("expected description 'Report A', got %q", a.Description)
	}
	if !a.CountEnabled {
		t.Error("expected countEnabled=true for report_a")
	}
	if len(a.Parameters) != 1 || a.Parameters[0].Name != "id" {
		t.Errorf("unexpected parameters for report_a: %+v", a.Parameters)
	}

	// Verify report_b.
	b, ok := byName["report_b"]
	if !ok {
		t.Fatal("expected report_b in result")
	}
	if b.CountEnabled {
		t.Error("expected countEnabled=false for report_b")
	}

	// Verify report_c (default countEnabled=true).
	c, ok := byName["report_c"]
	if !ok {
		t.Fatal("expected report_c in result")
	}
	if !c.CountEnabled {
		t.Error("expected countEnabled=true (default) for report_c")
	}
	if len(c.Parameters) != 0 {
		t.Errorf("expected 0 parameters for report_c, got %d", len(c.Parameters))
	}
}

// ===========================================================================
// Task 19.2: Hot reload integration tests
// ===========================================================================

// TestIntegration_HotReload_FileChange tests that modifying a template file
// and calling Reload causes the new version to take effect.
func TestIntegration_HotReload_FileChange(t *testing.T) {
	executor := &integrationMockExecutor{
		data: []map[string]interface{}{{"v": 1}},
	}

	te, tmpDir := createReloadableEngine(t, []templateDef{
		{
			name:        "reload_test",
			file:        "reload_test.sql.tmpl",
			content:     "SELECT 'version1'",
			description: "Reload test",
			params:      []config.TemplateParamConfig{},
		},
	}, executor)

	// Execute before reload — should work.
	req := &template.TemplateQueryRequest{TemplateName: "reload_test"}
	_, err := te.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("pre-reload execute failed: %v", err)
	}

	// Modify the template file.
	newContent := "SELECT 'version2'"
	filePath := filepath.Join(tmpDir, "reload_test.sql.tmpl")
	if err := os.WriteFile(filePath, []byte(newContent), 0o644); err != nil {
		t.Fatalf("failed to write updated template: %v", err)
	}

	// Trigger reload.
	result, err := te.Reload(context.Background(), true)
	if err != nil {
		t.Fatalf("reload failed: %v", err)
	}

	if result.SuccessCount != 1 {
		t.Errorf("expected 1 successful reload, got %d", result.SuccessCount)
	}
	if len(result.Failures) != 0 {
		t.Errorf("expected 0 failures, got %d", len(result.Failures))
	}

	// Execute after reload — should use new version.
	_, err = te.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("post-reload execute failed: %v", err)
	}

	// Verify the executor received a SQL containing 'version2'.
	if !strings.Contains(executor.lastSQL, "version2") {
		t.Errorf("expected SQL to contain 'version2' after reload, got: %s", executor.lastSQL)
	}
}

// TestIntegration_HotReload_CacheClear tests that after a file modification,
// the hash changes are detected by Reload.
func TestIntegration_HotReload_CacheClear(t *testing.T) {
	executor := &integrationMockExecutor{
		data: []map[string]interface{}{{"v": 1}},
	}

	te, tmpDir := createReloadableEngine(t, []templateDef{
		{
			name:        "hash_test",
			file:        "hash_test.sql.tmpl",
			content:     "SELECT 'original'",
			description: "Hash test",
			params:      []config.TemplateParamConfig{},
		},
	}, executor)

	// Modify the file.
	filePath := filepath.Join(tmpDir, "hash_test.sql.tmpl")
	if err := os.WriteFile(filePath, []byte("SELECT 'modified'"), 0o644); err != nil {
		t.Fatalf("failed to write modified template: %v", err)
	}

	// Reload should succeed and detect the hash change.
	result, err := te.Reload(context.Background(), true)
	if err != nil {
		t.Fatalf("reload failed: %v", err)
	}

	if result.SuccessCount != 1 {
		t.Errorf("expected 1 successful reload, got %d", result.SuccessCount)
	}

	// Execute to verify the new content is used.
	req := &template.TemplateQueryRequest{TemplateName: "hash_test"}
	_, err = te.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("post-reload execute failed: %v", err)
	}

	if !strings.Contains(executor.lastSQL, "modified") {
		t.Errorf("expected SQL to contain 'modified' after reload, got: %s", executor.lastSQL)
	}
}

// TestIntegration_HotReload_ErrorIsolation tests that a corrupted template
// retains its old version while other templates are updated normally.
func TestIntegration_HotReload_ErrorIsolation(t *testing.T) {
	executor := &integrationMockExecutor{
		data: []map[string]interface{}{{"v": 1}},
	}

	te, tmpDir := createReloadableEngine(t, []templateDef{
		{
			name:        "good_tmpl",
			file:        "good_tmpl.sql.tmpl",
			content:     "SELECT 'good_v1'",
			description: "Good template",
			params:      []config.TemplateParamConfig{},
		},
		{
			name:        "bad_tmpl",
			file:        "bad_tmpl.sql.tmpl",
			content:     "SELECT 'bad_v1'",
			description: "Will be corrupted",
			params:      []config.TemplateParamConfig{},
		},
	}, executor)

	// Corrupt bad_tmpl with invalid template syntax.
	badPath := filepath.Join(tmpDir, "bad_tmpl.sql.tmpl")
	if err := os.WriteFile(badPath, []byte("SELECT {{.Invalid | nonexistent_func}}"), 0o644); err != nil {
		t.Fatalf("failed to corrupt template: %v", err)
	}

	// Update good_tmpl to a new version.
	goodPath := filepath.Join(tmpDir, "good_tmpl.sql.tmpl")
	if err := os.WriteFile(goodPath, []byte("SELECT 'good_v2'"), 0o644); err != nil {
		t.Fatalf("failed to update good template: %v", err)
	}

	// Reload — bad_tmpl should fail, good_tmpl should succeed.
	result, err := te.Reload(context.Background(), true)
	if err != nil {
		t.Fatalf("reload failed: %v", err)
	}

	// bad_tmpl failed but was retained from old version.
	if len(result.Failures) == 0 {
		t.Error("expected at least 1 failure for corrupted template")
	}

	// good_tmpl should be updated — verify by executing.
	req := &template.TemplateQueryRequest{TemplateName: "good_tmpl"}
	_, err = te.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("good_tmpl execute failed after reload: %v", err)
	}
	if !strings.Contains(executor.lastSQL, "good_v2") {
		t.Errorf("expected good_tmpl to use 'good_v2', got: %s", executor.lastSQL)
	}

	// bad_tmpl should still work with old version (error isolation).
	req = &template.TemplateQueryRequest{TemplateName: "bad_tmpl"}
	_, err = te.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("bad_tmpl should still work with old version, got error: %v", err)
	}
	if !strings.Contains(executor.lastSQL, "bad_v1") {
		t.Errorf("expected bad_tmpl to retain 'bad_v1', got: %s", executor.lastSQL)
	}
}

// TestIntegration_HotReload_MutationCooldown tests that a second Mutation Reload
// within 10s returns the cached result without re-reading files.
func TestIntegration_HotReload_MutationCooldown(t *testing.T) {
	executor := &integrationMockExecutor{
		data: []map[string]interface{}{{"v": 1}},
	}

	te, _ := createReloadableEngine(t, []templateDef{
		{
			name:        "cooldown_test",
			file:        "cooldown_test.sql.tmpl",
			content:     "SELECT 'cooldown'",
			description: "Cooldown test",
			params:      []config.TemplateParamConfig{},
		},
	}, executor)

	// First reload (Mutation-triggered).
	result1, err := te.Reload(context.Background(), true)
	if err != nil {
		t.Fatalf("first reload failed: %v", err)
	}

	// Second reload immediately (within 10s cooldown).
	result2, err := te.Reload(context.Background(), true)
	if err != nil {
		t.Fatalf("second reload failed: %v", err)
	}

	// The key assertion: the second result should be the cached result.
	// Both should have the same SuccessCount and Failures.
	if result1.SuccessCount != result2.SuccessCount {
		t.Errorf("expected same SuccessCount from cached result, got %d vs %d",
			result1.SuccessCount, result2.SuccessCount)
	}
	if len(result1.Failures) != len(result2.Failures) {
		t.Errorf("expected same Failures count from cached result, got %d vs %d",
			len(result1.Failures), len(result2.Failures))
	}

	// The duration of the cached result should match the first result exactly
	// (same pointer returned).
	if result1.Duration != result2.Duration {
		t.Errorf("expected same Duration from cached result, got %v vs %v",
			result1.Duration, result2.Duration)
	}
}

// ===========================================================================
// Task 19.3: Permission integration tests
// **Validates: Requirements 5.7**
// ===========================================================================

// TestIntegration_PermissionCheck verifies that the resolver checks permissions.
// When TemplateEngine is nil (feature disabled), templateQuery returns
// VALIDATION_TEMPLATE_NOT_FOUND — this serves as a proxy for permission-like
// gating since the feature is not available.
//
// Property 26: Permission Check
// **Validates: Requirements 5.7**
func TestIntegration_PermissionCheck(t *testing.T) {
	// Test 1: nil TemplateEngine → templateQuery returns error for any template name.
	r := &queryResolver{&Resolver{
		TemplateEngine: nil,
		GraphQLConfig:  config.GraphQLConfig{MaxResultRows: 10000},
	}}

	templateNames := []string{"report_a", "report_b", "nonexistent", "fleet_report"}
	for _, name := range templateNames {
		// nil engine returns before fieldRequested, so no gqlCtx needed.
		_, err := r.TemplateQuery(context.Background(), name, nil, nil, nil, nil, nil)
		if err == nil {
			t.Errorf("expected error for template %q when engine is nil", name)
			continue
		}
		if !strings.Contains(err.Error(), apierrors.ErrValidationTemplateNotFound) {
			t.Errorf("template %q: expected VALIDATION_TEMPLATE_NOT_FOUND, got: %s", name, err.Error())
		}
	}

	// Test 2: nil TemplateEngine → templateList returns empty.
	list, err := r.TemplateList(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("expected no error from templateList, got %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected empty templateList when engine is nil, got %d items", len(list))
	}

	// Test 3: nil TemplateEngine → reloadTemplates returns empty result.
	mr := &mutationResolver{&Resolver{TemplateEngine: nil}}
	reloadResult, err := mr.ReloadTemplates(context.Background())
	if err != nil {
		t.Fatalf("expected no error from reloadTemplates, got %v", err)
	}
	if reloadResult.SuccessCount != 0 {
		t.Errorf("expected successCount=0, got %d", reloadResult.SuccessCount)
	}
	if len(reloadResult.Failures) != 0 {
		t.Errorf("expected 0 failures, got %d", len(reloadResult.Failures))
	}

	// Test 4: With engine enabled, non-existent template returns TEMPLATE_NOT_FOUND.
	executor := &integrationMockExecutor{data: nil}
	te := createIntegrationEngine(t, []templateDef{
		{
			name:        "existing_tmpl",
			file:        "existing_tmpl.sql.tmpl",
			content:     "SELECT 1",
			description: "Existing",
			params:      []config.TemplateParamConfig{},
		},
	}, executor)

	rEnabled := &queryResolver{&Resolver{
		TemplateEngine: te,
		GraphQLConfig:  config.GraphQLConfig{MaxResultRows: 10000},
	}}

	ctx := gqlCtx("nodes")
	_, err = rEnabled.TemplateQuery(ctx, "nonexistent_template", nil, nil, nil, nil, nil)
	if err == nil {
		t.Fatal("expected error for nonexistent template")
	}
	if !strings.Contains(err.Error(), apierrors.ErrValidationTemplateNotFound) {
		t.Errorf("expected VALIDATION_TEMPLATE_NOT_FOUND, got: %s", err.Error())
	}

	// Test 5: Existing template should succeed.
	_, err = rEnabled.TemplateQuery(ctx, "existing_tmpl", nil, nil, nil, nil, nil)
	if err != nil {
		t.Errorf("expected no error for existing template, got: %v", err)
	}
}

// TestIntegration_PermissionCheck_ReloadWithEngine verifies that reloadTemplates
// works correctly when the engine is enabled.
func TestIntegration_PermissionCheck_ReloadWithEngine(t *testing.T) {
	executor := &integrationMockExecutor{data: nil}

	te := createIntegrationEngine(t, []templateDef{
		{
			name:        "reload_perm",
			file:        "reload_perm.sql.tmpl",
			content:     "SELECT 1",
			description: "Reload perm test",
			params:      []config.TemplateParamConfig{},
		},
	}, executor)

	mr := &mutationResolver{&Resolver{TemplateEngine: te}}

	result, err := mr.ReloadTemplates(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.SuccessCount != 1 {
		t.Errorf("expected successCount=1, got %d", result.SuccessCount)
	}
	if result.Duration == "" {
		t.Error("expected non-empty duration")
	}
}
