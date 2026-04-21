// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package resolver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/michaelwang123/mountainKing/internal/config"
	"github.com/michaelwang123/mountainKing/internal/datasource"
	"github.com/michaelwang123/mountainKing/internal/graphql/generated"
	"github.com/michaelwang123/mountainKing/internal/graphql/scalar"
	"github.com/michaelwang123/mountainKing/internal/template"
	"go.uber.org/zap"
)

// ---------------------------------------------------------------------------
// Unit tests for helper functions
// ---------------------------------------------------------------------------

func TestConvertJSONToMap_Nil(t *testing.T) {
	result := convertJSONToMap(nil)
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}

func TestConvertJSONToMap_NonNil(t *testing.T) {
	input := scalar.JSON{"key1": "value1", "key2": float64(42)}
	result := convertJSONToMap(input)
	if result == nil {
		t.Fatal("expected non-nil map")
	}
	if result["key1"] != "value1" {
		t.Errorf("expected key1=value1, got %v", result["key1"])
	}
	if result["key2"] != float64(42) {
		t.Errorf("expected key2=42, got %v", result["key2"])
	}
}

func TestConvertOrderBy_Empty(t *testing.T) {
	result := convertOrderBy(nil)
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}

	result = convertOrderBy([]*generated.TemplateOrderBy{})
	if result != nil {
		t.Errorf("expected nil for empty slice, got %v", result)
	}
}

func TestConvertOrderBy_NonEmpty(t *testing.T) {
	input := []*generated.TemplateOrderBy{
		{Field: "col_a", Direction: generated.SortDirectionAsc},
		{Field: "col_b", Direction: generated.SortDirectionDesc},
	}
	result := convertOrderBy(input)
	if len(result) != 2 {
		t.Fatalf("expected 2 items, got %d", len(result))
	}
	if result[0].Field != "col_a" || result[0].Direction != "ASC" {
		t.Errorf("unexpected first item: %+v", result[0])
	}
	if result[1].Field != "col_b" || result[1].Direction != "DESC" {
		t.Errorf("unexpected second item: %+v", result[1])
	}
}

func TestConvertOrderBy_SkipsNil(t *testing.T) {
	input := []*generated.TemplateOrderBy{
		nil,
		{Field: "col_a", Direction: generated.SortDirectionAsc},
		nil,
	}
	result := convertOrderBy(input)
	if len(result) != 1 {
		t.Fatalf("expected 1 item after skipping nils, got %d", len(result))
	}
}

func TestBuildTemplateQueryConnection_Empty(t *testing.T) {
	conn := buildTemplateQueryConnection(nil, 0, nil, nil, nil)
	if len(conn.Nodes) != 0 {
		t.Errorf("expected 0 nodes, got %d", len(conn.Nodes))
	}
	if conn.TotalCount != 0 {
		t.Errorf("expected totalCount=0, got %d", conn.TotalCount)
	}
	if conn.PageInfo.HasNextPage {
		t.Error("expected hasNextPage=false")
	}
	if conn.PageInfo.HasPreviousPage {
		t.Error("expected hasPreviousPage=false")
	}
}

func TestBuildTemplateQueryConnection_WithData(t *testing.T) {
	data := []map[string]interface{}{
		{"id": 1}, {"id": 2}, {"id": 3},
	}
	tc := int64(100)
	offset := 5
	first := 10

	conn := buildTemplateQueryConnection(data, 3, &tc, &offset, &first)
	if len(conn.Nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(conn.Nodes))
	}
	if conn.TotalCount != 100 {
		t.Errorf("expected totalCount=100, got %d", conn.TotalCount)
	}
	if conn.PageInfo.HasNextPage {
		t.Error("expected hasNextPage=false when originalLen <= first")
	}
	if !conn.PageInfo.HasPreviousPage {
		t.Error("expected hasPreviousPage=true when offset > 0")
	}
}

func TestBuildTemplateQueryConnection_OverFetch(t *testing.T) {
	data := []map[string]interface{}{
		{"id": 1}, {"id": 2}, {"id": 3},
	}
	first := 3
	conn := buildTemplateQueryConnection(data, 4, nil, nil, &first)
	if !conn.PageInfo.HasNextPage {
		t.Error("expected hasNextPage=true when originalLen > first")
	}
}

func TestBuildTemplateQueryConnection_NilFirst(t *testing.T) {
	data := []map[string]interface{}{{"id": 1}}
	conn := buildTemplateQueryConnection(data, 1, nil, nil, nil)
	if conn.PageInfo.HasNextPage {
		t.Error("expected hasNextPage=false when first is nil")
	}
}

func TestBuildTemplateQueryConnection_VariousSizes(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		dataSize := rapid.IntRange(0, 50).Draw(rt, "dataSize")
		data := make([]map[string]interface{}, dataSize)
		for i := range data {
			data[i] = map[string]interface{}{"id": i}
		}

		// originalLen may be larger than dataSize (over-fetch scenario).
		originalLen := rapid.IntRange(dataSize, dataSize+5).Draw(rt, "originalLen")

		useFirst := rapid.Bool().Draw(rt, "useFirst")
		useOffset := rapid.Bool().Draw(rt, "useOffset")
		useTotalCount := rapid.Bool().Draw(rt, "useTotalCount")

		var first *int
		if useFirst {
			f := rapid.IntRange(1, 100).Draw(rt, "first")
			first = &f
		}
		var offset *int
		if useOffset {
			o := rapid.IntRange(0, 100).Draw(rt, "offset")
			offset = &o
		}
		var totalCount *int64
		if useTotalCount {
			tc := int64(rapid.IntRange(0, 1000).Draw(rt, "totalCount"))
			totalCount = &tc
		}

		conn := buildTemplateQueryConnection(data, originalLen, totalCount, offset, first)

		// Verify nodes count matches data.
		if len(conn.Nodes) != dataSize {
			rt.Fatalf("expected %d nodes, got %d", dataSize, len(conn.Nodes))
		}

		// Verify hasNextPage: true iff first != nil && originalLen > *first.
		expectedHasNext := first != nil && originalLen > *first
		if conn.PageInfo.HasNextPage != expectedHasNext {
			rt.Fatalf("expected hasNextPage=%v, got %v (first=%v, originalLen=%d)",
				expectedHasNext, conn.PageInfo.HasNextPage, first, originalLen)
		}

		// Verify hasPreviousPage: true iff offset != nil && *offset > 0.
		expectedHasPrev := offset != nil && *offset > 0
		if conn.PageInfo.HasPreviousPage != expectedHasPrev {
			rt.Fatalf("expected hasPreviousPage=%v, got %v",
				expectedHasPrev, conn.PageInfo.HasPreviousPage)
		}

		// Verify totalCount.
		if useTotalCount {
			if conn.TotalCount != int(*totalCount) {
				rt.Fatalf("expected totalCount=%d, got %d", *totalCount, conn.TotalCount)
			}
		} else {
			if conn.TotalCount != 0 {
				rt.Fatalf("expected totalCount=0 when nil, got %d", conn.TotalCount)
			}
		}
	})
}

func TestConvertReloadResult_Success(t *testing.T) {
	r := &template.ReloadResult{
		SuccessCount: 5,
		Failures:     nil,
		Duration:     2 * time.Second,
	}
	result := convertReloadResult(r)
	if result.SuccessCount != 5 {
		t.Errorf("expected successCount=5, got %d", result.SuccessCount)
	}
	if len(result.Failures) != 0 {
		t.Errorf("expected 0 failures, got %d", len(result.Failures))
	}
	if result.Duration != "2s" {
		t.Errorf("expected duration=2s, got %s", result.Duration)
	}
}

func TestConvertReloadResult_WithFailures(t *testing.T) {
	r := &template.ReloadResult{
		SuccessCount: 3,
		Failures: []template.TemplateLoadFailure{
			{Name: "bad_tmpl", Error: "syntax error"},
		},
		Duration: 500 * time.Millisecond,
	}
	result := convertReloadResult(r)
	if result.SuccessCount != 3 {
		t.Errorf("expected successCount=3, got %d", result.SuccessCount)
	}
	if len(result.Failures) != 1 {
		t.Fatalf("expected 1 failure, got %d", len(result.Failures))
	}
	if result.Failures[0].Name != "bad_tmpl" {
		t.Errorf("expected failure name=bad_tmpl, got %s", result.Failures[0].Name)
	}
}

// ---------------------------------------------------------------------------
// Test helpers for creating TemplateEngine with known templates
// ---------------------------------------------------------------------------

// mockRawExecutor implements template.RawExecutor for resolver-level tests.
type mockRawExecutor struct {
	data []map[string]interface{}
}

func (m *mockRawExecutor) ExecuteRaw(_ context.Context, _ string, _ ...interface{}) (*datasource.QueryResult, error) {
	return &datasource.QueryResult{Data: m.data}, nil
}

// templateDef describes a template for test setup.
type templateDef struct {
	name         string
	file         string
	content      string
	description  string
	countEnabled *bool
	params       []config.TemplateParamConfig
}

// createResolverTestEngine creates a TemplateEngine with the given template definitions.
func createResolverTestEngine(t *testing.T, templates []templateDef) *template.TemplateEngine {
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
		Executor:       &mockRawExecutor{data: []map[string]interface{}{{"id": 1}}},
		Logger:         zap.NewNop(),
	})
	if err != nil {
		t.Fatalf("failed to create TemplateEngine: %v", err)
	}
	return te
}

func boolPtr(v bool) *bool { return &v }

// =============================================================================
// Property 13: templateList 完整性
// **Validates: Requirements 3.4**
// templateList returns all registered templates — the set of names returned
// by the resolver equals the set of templates registered in the engine.
// =============================================================================

func TestProperty13_TemplateListCompleteness(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		numTemplates := rapid.IntRange(1, 8).Draw(rt, "numTemplates")

		// Generate unique template definitions.
		defs := make([]templateDef, numTemplates)
		expectedNames := make(map[string]bool, numTemplates)
		for i := 0; i < numTemplates; i++ {
			name := fmt.Sprintf("tmpl_%d", i)
			defs[i] = templateDef{
				name:        name,
				file:        fmt.Sprintf("%s.sql.tmpl", name),
				content:     fmt.Sprintf("SELECT %d", i),
				description: fmt.Sprintf("Template %d", i),
				params:      []config.TemplateParamConfig{},
			}
			expectedNames[name] = true
		}

		te := createResolverTestEngine(t, defs)
		r := &queryResolver{&Resolver{TemplateEngine: te}}

		result, err := r.TemplateList(context.Background(), nil, nil)
		if err != nil {
			rt.Fatalf("TemplateList returned error: %v", err)
		}

		// Verify: returned count matches registered count.
		if len(result) != numTemplates {
			rt.Fatalf("expected %d templates, got %d", numTemplates, len(result))
		}

		// Verify: every registered name appears in the result.
		returnedNames := make(map[string]bool, len(result))
		for _, info := range result {
			returnedNames[info.Name] = true
		}
		for name := range expectedNames {
			if !returnedNames[name] {
				rt.Fatalf("expected template %q in result, but not found", name)
			}
		}
		// Verify: no extra names in result.
		for name := range returnedNames {
			if !expectedNames[name] {
				rt.Fatalf("unexpected template %q in result", name)
			}
		}
	})
}

// =============================================================================
// Property 14: templateList 参数一致性
// **Validates: Requirements 3.5**
// For every template, the parameters returned by templateList match the
// template's configured parameter schema.
// =============================================================================

func TestProperty14_TemplateListParameterConsistency(t *testing.T) {
	// Pre-build a set of templates with known parameters.
	// Use rapid to verify the resolver output matches for random subsets.
	type expectedParam struct {
		Name     string
		Type     string
		Required bool
	}

	// Create a fixed set of templates with varying parameter configurations.
	allDefs := []templateDef{
		{
			name: "no_params", file: "no_params.sql.tmpl",
			content: "SELECT 1", description: "No params",
			params: []config.TemplateParamConfig{},
		},
		{
			name: "one_string", file: "one_string.sql.tmpl",
			content: "SELECT 1", description: "One string param",
			params: []config.TemplateParamConfig{
				{Name: "name", Type: "string", Required: true},
			},
		},
		{
			name: "multi_types", file: "multi_types.sql.tmpl",
			content: "SELECT 1", description: "Multiple types",
			params: []config.TemplateParamConfig{
				{Name: "id", Type: "int", Required: true},
				{Name: "score", Type: "float", Required: false},
				{Name: "active", Type: "boolean", Required: false},
			},
		},
		{
			name: "with_default", file: "with_default.sql.tmpl",
			content: "SELECT 1", description: "With default",
			params: []config.TemplateParamConfig{
				{Name: "limit", Type: "int", Required: false, Default: strPtr("100")},
			},
		},
	}

	allExpected := map[string][]expectedParam{
		"no_params":  {},
		"one_string": {{Name: "name", Type: "string", Required: true}},
		"multi_types": {
			{Name: "id", Type: "int", Required: true},
			{Name: "score", Type: "float", Required: false},
			{Name: "active", Type: "boolean", Required: false},
		},
		"with_default": {{Name: "limit", Type: "int", Required: false}},
	}

	rapid.Check(t, func(rt *rapid.T) {
		// Pick a random non-empty subset of templates using a bitmask.
		mask := rapid.IntRange(1, (1<<len(allDefs))-1).Draw(rt, "mask")
		var defs []templateDef
		for i := 0; i < len(allDefs); i++ {
			if mask&(1<<i) != 0 {
				defs = append(defs, allDefs[i])
			}
		}

		te := createResolverTestEngine(t, defs)
		r := &queryResolver{&Resolver{TemplateEngine: te}}

		result, err := r.TemplateList(context.Background(), nil, nil)
		if err != nil {
			rt.Fatalf("TemplateList returned error: %v", err)
		}

		if len(result) != len(defs) {
			rt.Fatalf("expected %d templates, got %d", len(defs), len(result))
		}

		for _, info := range result {
			expected, ok := allExpected[info.Name]
			if !ok {
				rt.Fatalf("unexpected template %q in result", info.Name)
			}

			if len(info.Parameters) != len(expected) {
				rt.Fatalf("template %q: expected %d params, got %d",
					info.Name, len(expected), len(info.Parameters))
			}

			// Sort both by name for stable comparison.
			sortedGot := make([]*generated.TemplateParameterInfo, len(info.Parameters))
			copy(sortedGot, info.Parameters)
			sort.Slice(sortedGot, func(a, b int) bool {
				return sortedGot[a].Name < sortedGot[b].Name
			})
			sortedExp := make([]expectedParam, len(expected))
			copy(sortedExp, expected)
			sort.Slice(sortedExp, func(a, b int) bool {
				return sortedExp[a].Name < sortedExp[b].Name
			})

			for j, ep := range sortedExp {
				got := sortedGot[j]
				if got.Name != ep.Name {
					rt.Fatalf("template %q param %d: expected name %q, got %q",
						info.Name, j, ep.Name, got.Name)
				}
				if got.Type != ep.Type {
					rt.Fatalf("template %q param %q: expected type %q, got %q",
						info.Name, ep.Name, ep.Type, got.Type)
				}
				if got.Required != ep.Required {
					rt.Fatalf("template %q param %q: expected required=%v, got %v",
						info.Name, ep.Name, ep.Required, got.Required)
				}
			}
		}
	})
}

// =============================================================================
// Property 15: countEnabled 一致性
// **Validates: Requirements 3.5**
// The countEnabled field in templateList matches the template's configuration.
// =============================================================================

func TestProperty15_CountEnabledConsistency(t *testing.T) {
	// Create templates with different countEnabled settings.
	allDefs := []templateDef{
		{
			name: "count_default", file: "count_default.sql.tmpl",
			content: "SELECT 1", description: "Default count",
			countEnabled: nil, // default = true
			params:       []config.TemplateParamConfig{},
		},
		{
			name: "count_true", file: "count_true.sql.tmpl",
			content: "SELECT 1", description: "Count true",
			countEnabled: boolPtr(true),
			params:       []config.TemplateParamConfig{},
		},
		{
			name: "count_false", file: "count_false.sql.tmpl",
			content: "SELECT 1", description: "Count false",
			countEnabled: boolPtr(false),
			params:       []config.TemplateParamConfig{},
		},
	}

	expectedCountEnabled := map[string]bool{
		"count_default": true,
		"count_true":    true,
		"count_false":   false,
	}

	rapid.Check(t, func(rt *rapid.T) {
		// Pick a random non-empty subset using a bitmask.
		mask := rapid.IntRange(1, (1<<len(allDefs))-1).Draw(rt, "mask")
		var defs []templateDef
		for i := 0; i < len(allDefs); i++ {
			if mask&(1<<i) != 0 {
				defs = append(defs, allDefs[i])
			}
		}

		te := createResolverTestEngine(t, defs)
		r := &queryResolver{&Resolver{TemplateEngine: te}}

		result, err := r.TemplateList(context.Background(), nil, nil)
		if err != nil {
			rt.Fatalf("TemplateList returned error: %v", err)
		}

		for _, info := range result {
			expected, ok := expectedCountEnabled[info.Name]
			if !ok {
				rt.Fatalf("unexpected template %q in result", info.Name)
			}
			if info.CountEnabled != expected {
				rt.Fatalf("template %q: expected countEnabled=%v, got %v",
					info.Name, expected, info.CountEnabled)
			}
		}
	})
}

// =============================================================================
// Property 16: 功能禁用行为
// **Validates: Requirements 3.9**
// When TemplateEngine is nil, templateQuery returns error and templateList
// returns empty.
// =============================================================================

func TestProperty16_FeatureDisabledBehavior(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		templateName := rapid.StringMatching(`[a-zA-Z0-9_]{1,20}`).Draw(rt, "templateName")

		// Resolver with nil TemplateEngine (feature disabled).
		r := &queryResolver{&Resolver{
			TemplateEngine: nil,
			GraphQLConfig:  config.GraphQLConfig{MaxResultRows: 10000},
		}}

		// templateQuery should return error.
		_, err := r.TemplateQuery(context.Background(), templateName, nil, nil, nil, nil, nil)
		if err == nil {
			rt.Fatal("expected error from templateQuery when TemplateEngine is nil")
		}

		// templateList should return empty.
		list, listErr := r.TemplateList(context.Background(), nil, nil)
		if listErr != nil {
			rt.Fatalf("expected no error from templateList, got %v", listErr)
		}
		if len(list) != 0 {
			rt.Fatalf("expected empty templateList, got %d items", len(list))
		}

		// reloadTemplates should return empty result.
		mr := &mutationResolver{&Resolver{TemplateEngine: nil}}
		reloadResult, reloadErr := mr.ReloadTemplates(context.Background())
		if reloadErr != nil {
			rt.Fatalf("expected no error from reloadTemplates, got %v", reloadErr)
		}
		if reloadResult.SuccessCount != 0 {
			rt.Fatalf("expected successCount=0, got %d", reloadResult.SuccessCount)
		}
		if len(reloadResult.Failures) != 0 {
			rt.Fatalf("expected 0 failures, got %d", len(reloadResult.Failures))
		}
	})
}
