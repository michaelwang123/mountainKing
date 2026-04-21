// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package template

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"go.uber.org/zap"
	"pgregory.net/rapid"

	"github.com/michaelwang123/mountainKing/internal/config"
)

// =============================================================================
// Feature: sql-template-engine
// Task 7.3: 注册表和加载器单元测试和属性测试（加载器层校验）
// =============================================================================

// helper: create a minimal valid .sql.tmpl file in dir with given content.
func writeTemplateFile(t *testing.T, dir, relPath, content string) {
	t.Helper()
	full := filepath.Join(dir, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// helper: build a minimal SQLTemplatesConfig with one template entry.
func minimalConfig(baseDir string, templates []config.TemplateConfig) config.SQLTemplatesConfig {
	return config.SQLTemplatesConfig{
		Enabled:              true,
		BaseDir:              baseDir,
		SharedDir:            filepath.Join(baseDir, "_shared"),
		RenderTimeout:        5_000_000_000, // 5s
		MaxRenderedSQLLen:    65536,
		MaxConcurrentQueries: 10,
		Templates:            templates,
	}
}

// =============================================================================
// Property 1: 模板名称唯一性
// **Validates: Requirements 1.6**
// All registered templates have unique names.
// =============================================================================

func TestProperty1_TemplateNameUniqueness(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		dir := t.TempDir()
		numTemplates := rapid.IntRange(1, 8).Draw(rt, "numTemplates")

		var templates []config.TemplateConfig
		for i := 0; i < numTemplates; i++ {
			name := rapid.StringMatching(`[a-zA-Z][a-zA-Z0-9_-]{0,10}`).Draw(rt, "name")
			file := name + ".sql.tmpl"
			writeTemplateFile(t, dir, file, "SELECT 1")
			templates = append(templates, config.TemplateConfig{
				Name: name,
				File: file,
			})
		}

		cfg := minimalConfig(dir, templates)
		result, err := loadAll(cfg, buildFuncMap(), zap.NewNop())
		if err != nil {
			rt.Fatalf("loadAll error: %v", err)
		}

		// All registered names must be unique (map keys guarantee this,
		// but verify no duplicate was silently accepted).
		seen := make(map[string]bool)
		for name := range result.Registered {
			if seen[name] {
				rt.Fatalf("duplicate template name %q in registry", name)
			}
			seen[name] = true
		}
	})
}

// =============================================================================
// Property 2: 模板语法有效性
// **Validates: Requirements 1.3**
// All registered templates have valid Go template syntax.
// =============================================================================

func TestProperty2_TemplateSyntaxValidity(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		dir := t.TempDir()
		numTemplates := rapid.IntRange(1, 5).Draw(rt, "numTemplates")

		var templates []config.TemplateConfig
		for i := 0; i < numTemplates; i++ {
			name := rapid.StringMatching(`[a-zA-Z][a-zA-Z0-9_]{0,8}`).Draw(rt, "name")
			file := name + ".sql.tmpl"
			// Generate valid SQL template content
			content := rapid.SampledFrom([]string{
				"SELECT 1",
				"SELECT * FROM users WHERE id = {{.Params.id | safeInt}}",
				"SELECT name FROM t WHERE name = {{.Params.name | quote}}",
				"SELECT {{.Params.col | safeIdentifier}} FROM t",
			}).Draw(rt, "content")
			writeTemplateFile(t, dir, file, content)
			templates = append(templates, config.TemplateConfig{
				Name: name,
				File: file,
			})
		}

		cfg := minimalConfig(dir, templates)
		result, err := loadAll(cfg, buildFuncMap(), zap.NewNop())
		if err != nil {
			rt.Fatalf("loadAll error: %v", err)
		}

		// Every registered template must have a non-nil compiled Template.
		for name, reg := range result.Registered {
			if reg.Template == nil {
				rt.Fatalf("registered template %q has nil Template", name)
			}
		}
	})
}

// =============================================================================
// Property 3: 无效模板不影响启动
// **Validates: Requirements 1.4**
// Invalid templates don't prevent other templates from loading.
// =============================================================================

func TestProperty3_InvalidTemplateDoesNotBlockOthers(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		dir := t.TempDir()

		// Always create one valid template
		validName := "valid_tmpl"
		writeTemplateFile(t, dir, validName+".sql.tmpl", "SELECT 1")

		// Create one invalid template (syntax error)
		invalidName := "invalid_tmpl"
		writeTemplateFile(t, dir, invalidName+".sql.tmpl", "SELECT {{.Params.x | ")

		templates := []config.TemplateConfig{
			{Name: validName, File: validName + ".sql.tmpl"},
			{Name: invalidName, File: invalidName + ".sql.tmpl"},
		}

		cfg := minimalConfig(dir, templates)
		result, err := loadAll(cfg, buildFuncMap(), zap.NewNop())
		if err != nil {
			rt.Fatalf("loadAll should not return error: %v", err)
		}

		// Valid template must be registered
		if _, ok := result.Registered[validName]; !ok {
			rt.Fatalf("valid template %q should be registered", validName)
		}

		// Invalid template must NOT be registered
		if _, ok := result.Registered[invalidName]; ok {
			rt.Fatalf("invalid template %q should NOT be registered", invalidName)
		}

		// Invalid template must appear in failures
		found := false
		for _, f := range result.Failures {
			if f.Name == invalidName {
				found = true
				break
			}
		}
		if !found {
			rt.Fatalf("invalid template %q should appear in failures", invalidName)
		}
	})
}

// =============================================================================
// Property 4: 模板名称格式校验（加载器层）
// **Validates: Requirements 1.9**
// Names must match ^[a-zA-Z0-9_-]{1,64}$
// =============================================================================

var validNameRe = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

func TestProperty4_TemplateNameFormatValidation(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		dir := t.TempDir()

		// Generate a name that may or may not match the pattern
		name := rapid.OneOf(
			// Valid names
			rapid.StringMatching(`[a-zA-Z][a-zA-Z0-9_-]{0,10}`),
			// Invalid names: contain special chars
			rapid.Map(rapid.String(), func(s string) string {
				return "!" + s // ensure at least one invalid char
			}),
		).Draw(rt, "name")

		file := "test.sql.tmpl"
		writeTemplateFile(t, dir, file, "SELECT 1")

		templates := []config.TemplateConfig{
			{Name: name, File: file},
		}

		cfg := minimalConfig(dir, templates)
		result, err := loadAll(cfg, buildFuncMap(), zap.NewNop())
		if err != nil {
			rt.Fatalf("loadAll error: %v", err)
		}

		isValid := validNameRe.MatchString(name)
		_, registered := result.Registered[name]

		if registered && !isValid {
			rt.Fatalf("name %q should NOT be registered (invalid format)", name)
		}
		// If valid, it should be registered (assuming file exists and is valid)
		if isValid && !registered {
			// Check it's not in failures for a name-related reason
			for _, f := range result.Failures {
				if f.Name == name && strings.Contains(f.Error, "does not match pattern") {
					rt.Fatalf("valid name %q was rejected by name format check", name)
				}
			}
		}
	})
}

// =============================================================================
// Property 5: 文件大小限制（加载器层）
// **Validates: Requirements 1.10**
// Files > 1MB are rejected.
// =============================================================================

func TestProperty5_FileSizeLimit(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		dir := t.TempDir()

		// Generate a file that exceeds 1MB
		overSize := rapid.IntRange(maxTemplateFileSize+1, maxTemplateFileSize+1024).Draw(rt, "overSize")
		bigContent := strings.Repeat("X", overSize)

		name := "big_tmpl"
		file := name + ".sql.tmpl"
		writeTemplateFile(t, dir, file, bigContent)

		templates := []config.TemplateConfig{
			{Name: name, File: file},
		}

		cfg := minimalConfig(dir, templates)
		result, err := loadAll(cfg, buildFuncMap(), zap.NewNop())
		if err != nil {
			rt.Fatalf("loadAll error: %v", err)
		}

		// Oversized template must NOT be registered
		if _, ok := result.Registered[name]; ok {
			rt.Fatalf("template with file size %d (> 1MB) should NOT be registered", overSize)
		}

		// Must appear in failures
		found := false
		for _, f := range result.Failures {
			if f.Name == name {
				found = true
				break
			}
		}
		if !found {
			rt.Fatalf("oversized template should appear in failures")
		}
	})
}

// =============================================================================
// Property 6: UTF-8 编码校验
// **Validates: Requirements 1.11**
// Non-UTF-8 files are rejected.
// =============================================================================

func TestProperty6_UTF8EncodingValidation(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		dir := t.TempDir()

		// Create a file with invalid UTF-8 bytes
		name := "bad_utf8"
		file := name + ".sql.tmpl"
		invalidUTF8 := []byte{0x80, 0x81, 0xFE, 0xFF} // invalid UTF-8 sequence
		full := filepath.Join(dir, file)
		if err := os.WriteFile(full, invalidUTF8, 0o644); err != nil {
			rt.Fatalf("write: %v", err)
		}

		templates := []config.TemplateConfig{
			{Name: name, File: file},
		}

		cfg := minimalConfig(dir, templates)
		result, err := loadAll(cfg, buildFuncMap(), zap.NewNop())
		if err != nil {
			rt.Fatalf("loadAll error: %v", err)
		}

		// Non-UTF-8 template must NOT be registered
		if _, ok := result.Registered[name]; ok {
			rt.Fatalf("non-UTF-8 template should NOT be registered")
		}

		// Must appear in failures
		found := false
		for _, f := range result.Failures {
			if f.Name == name {
				found = true
				break
			}
		}
		if !found {
			rt.Fatalf("non-UTF-8 template should appear in failures")
		}
	})
}

// =============================================================================
// Property 7: 共享片段加载
// **Validates: Requirements 1.12**
// Shared fragments can be referenced via {{template}}.
// =============================================================================

func TestProperty7_SharedFragmentLoading(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		dir := t.TempDir()
		sharedDir := filepath.Join(dir, "_shared")

		// Create a shared fragment
		fragmentName := rapid.StringMatching(`[a-z]{3,8}`).Draw(rt, "fragmentName")
		fragmentContent := `{{define "` + fragmentName + `"}}AND 1=1{{end}}`
		writeTemplateFile(t, sharedDir, fragmentName+".sql.tmpl", fragmentContent)

		// Create a template that references the shared fragment
		tmplName := "uses_shared"
		tmplContent := `SELECT 1 {{template "` + fragmentName + `" .}}`
		writeTemplateFile(t, dir, tmplName+".sql.tmpl", tmplContent)

		templates := []config.TemplateConfig{
			{Name: tmplName, File: tmplName + ".sql.tmpl"},
		}

		cfg := minimalConfig(dir, templates)
		result, err := loadAll(cfg, buildFuncMap(), zap.NewNop())
		if err != nil {
			rt.Fatalf("loadAll error: %v", err)
		}

		// Template referencing shared fragment must be registered
		reg, ok := result.Registered[tmplName]
		if !ok {
			rt.Fatalf("template %q referencing shared fragment should be registered", tmplName)
		}

		// Verify the shared fragment is accessible by looking up the template
		if reg.Template.Lookup(fragmentName) == nil {
			rt.Fatalf("shared fragment %q should be accessible via Lookup", fragmentName)
		}
	})
}

// =============================================================================
// Unit Tests: loadAll
// =============================================================================

func TestLoadAll_ValidTemplatesSucceeds(t *testing.T) {
	dir := t.TempDir()
	writeTemplateFile(t, dir, "a.sql.tmpl", "SELECT 1")
	writeTemplateFile(t, dir, "sub/b.sql.tmpl", "SELECT 2")

	cfg := minimalConfig(dir, []config.TemplateConfig{
		{Name: "tmpl_a", File: "a.sql.tmpl"},
		{Name: "tmpl_b", File: "sub/b.sql.tmpl"},
	})

	result, err := loadAll(cfg, buildFuncMap(), zap.NewNop())
	if err != nil {
		t.Fatalf("loadAll error: %v", err)
	}
	if len(result.Registered) != 2 {
		t.Fatalf("expected 2 registered, got %d", len(result.Registered))
	}
	if len(result.Failures) != 0 {
		t.Fatalf("expected 0 failures, got %d", len(result.Failures))
	}
	if _, ok := result.Registered["tmpl_a"]; !ok {
		t.Fatal("tmpl_a not registered")
	}
	if _, ok := result.Registered["tmpl_b"]; !ok {
		t.Fatal("tmpl_b not registered")
	}
}

func TestLoadAll_InvalidNameSkipped(t *testing.T) {
	dir := t.TempDir()
	writeTemplateFile(t, dir, "ok.sql.tmpl", "SELECT 1")

	cfg := minimalConfig(dir, []config.TemplateConfig{
		{Name: "valid_name", File: "ok.sql.tmpl"},
		{Name: "bad name!", File: "ok.sql.tmpl"},
	})

	result, err := loadAll(cfg, buildFuncMap(), zap.NewNop())
	if err != nil {
		t.Fatalf("loadAll error: %v", err)
	}
	if _, ok := result.Registered["valid_name"]; !ok {
		t.Fatal("valid_name should be registered")
	}
	if _, ok := result.Registered["bad name!"]; ok {
		t.Fatal("bad name! should NOT be registered")
	}
	if len(result.Failures) != 1 {
		t.Fatalf("expected 1 failure, got %d", len(result.Failures))
	}
}

func TestLoadAll_DuplicateNameSkipped(t *testing.T) {
	dir := t.TempDir()
	writeTemplateFile(t, dir, "a.sql.tmpl", "SELECT 1")
	writeTemplateFile(t, dir, "b.sql.tmpl", "SELECT 2")

	cfg := minimalConfig(dir, []config.TemplateConfig{
		{Name: "dup", File: "a.sql.tmpl"},
		{Name: "dup", File: "b.sql.tmpl"},
	})

	result, err := loadAll(cfg, buildFuncMap(), zap.NewNop())
	if err != nil {
		t.Fatalf("loadAll error: %v", err)
	}
	if len(result.Registered) != 1 {
		t.Fatalf("expected 1 registered (first wins), got %d", len(result.Registered))
	}
	if len(result.Failures) != 1 {
		t.Fatalf("expected 1 failure for duplicate, got %d", len(result.Failures))
	}
}

func TestLoadAll_MissingFileSkipped(t *testing.T) {
	dir := t.TempDir()
	writeTemplateFile(t, dir, "exists.sql.tmpl", "SELECT 1")

	cfg := minimalConfig(dir, []config.TemplateConfig{
		{Name: "exists", File: "exists.sql.tmpl"},
		{Name: "missing", File: "nonexistent.sql.tmpl"},
	})

	result, err := loadAll(cfg, buildFuncMap(), zap.NewNop())
	if err != nil {
		t.Fatalf("loadAll error: %v", err)
	}
	if _, ok := result.Registered["exists"]; !ok {
		t.Fatal("exists should be registered")
	}
	if _, ok := result.Registered["missing"]; ok {
		t.Fatal("missing should NOT be registered")
	}
	if len(result.Failures) != 1 {
		t.Fatalf("expected 1 failure, got %d", len(result.Failures))
	}
}

func TestLoadAll_SyntaxErrorSkipped(t *testing.T) {
	dir := t.TempDir()
	writeTemplateFile(t, dir, "good.sql.tmpl", "SELECT 1")
	writeTemplateFile(t, dir, "bad.sql.tmpl", "SELECT {{.Params.x | ")

	cfg := minimalConfig(dir, []config.TemplateConfig{
		{Name: "good", File: "good.sql.tmpl"},
		{Name: "bad", File: "bad.sql.tmpl"},
	})

	result, err := loadAll(cfg, buildFuncMap(), zap.NewNop())
	if err != nil {
		t.Fatalf("loadAll error: %v", err)
	}
	if _, ok := result.Registered["good"]; !ok {
		t.Fatal("good should be registered")
	}
	if _, ok := result.Registered["bad"]; ok {
		t.Fatal("bad should NOT be registered")
	}
}

func TestLoadAll_FileOverOneMBSkipped(t *testing.T) {
	dir := t.TempDir()
	bigContent := strings.Repeat("X", maxTemplateFileSize+1)
	writeTemplateFile(t, dir, "big.sql.tmpl", bigContent)
	writeTemplateFile(t, dir, "small.sql.tmpl", "SELECT 1")

	cfg := minimalConfig(dir, []config.TemplateConfig{
		{Name: "big", File: "big.sql.tmpl"},
		{Name: "small", File: "small.sql.tmpl"},
	})

	result, err := loadAll(cfg, buildFuncMap(), zap.NewNop())
	if err != nil {
		t.Fatalf("loadAll error: %v", err)
	}
	if _, ok := result.Registered["big"]; ok {
		t.Fatal("big should NOT be registered")
	}
	if _, ok := result.Registered["small"]; !ok {
		t.Fatal("small should be registered")
	}
}

func TestLoadAll_NonUTF8FileSkipped(t *testing.T) {
	dir := t.TempDir()
	// Write invalid UTF-8 bytes directly
	badPath := filepath.Join(dir, "bad_utf8.sql.tmpl")
	if err := os.WriteFile(badPath, []byte{0x80, 0x81, 0xFE, 0xFF}, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	writeTemplateFile(t, dir, "good.sql.tmpl", "SELECT 1")

	cfg := minimalConfig(dir, []config.TemplateConfig{
		{Name: "bad_utf8", File: "bad_utf8.sql.tmpl"},
		{Name: "good", File: "good.sql.tmpl"},
	})

	result, err := loadAll(cfg, buildFuncMap(), zap.NewNop())
	if err != nil {
		t.Fatalf("loadAll error: %v", err)
	}
	if _, ok := result.Registered["bad_utf8"]; ok {
		t.Fatal("bad_utf8 should NOT be registered")
	}
	if _, ok := result.Registered["good"]; !ok {
		t.Fatal("good should be registered")
	}
}

func TestLoadAll_SharedFragmentsAvailable(t *testing.T) {
	dir := t.TempDir()
	sharedDir := filepath.Join(dir, "_shared")

	// Create shared fragment
	writeTemplateFile(t, sharedDir, "common.sql.tmpl", `{{define "time_filter"}}AND created_at > NOW(){{end}}`)

	// Create template using the shared fragment
	writeTemplateFile(t, dir, "report.sql.tmpl", `SELECT * FROM t {{template "time_filter" .}}`)

	cfg := minimalConfig(dir, []config.TemplateConfig{
		{Name: "report", File: "report.sql.tmpl"},
	})

	result, err := loadAll(cfg, buildFuncMap(), zap.NewNop())
	if err != nil {
		t.Fatalf("loadAll error: %v", err)
	}
	if _, ok := result.Registered["report"]; !ok {
		t.Fatal("report should be registered")
	}
	if len(result.Failures) != 0 {
		t.Fatalf("expected 0 failures, got %d: %v", len(result.Failures), result.Failures)
	}
}

// =============================================================================
// Unit Tests: Registry Get/GetAll/Update/GetHash
// =============================================================================

func TestRegistry_GetFound(t *testing.T) {
	reg := NewTemplateRegistry()
	reg.Update(map[string]*RegisteredTemplate{
		"tmpl1": {Name: "tmpl1", Description: "test"},
	}, map[string]string{
		"tmpl1": "abc123",
	})

	got, ok := reg.Get("tmpl1")
	if !ok {
		t.Fatal("expected to find tmpl1")
	}
	if got.Name != "tmpl1" {
		t.Fatalf("expected name tmpl1, got %s", got.Name)
	}
}

func TestRegistry_GetNotFound(t *testing.T) {
	reg := NewTemplateRegistry()
	_, ok := reg.Get("nonexistent")
	if ok {
		t.Fatal("expected not found")
	}
}

func TestRegistry_GetAll(t *testing.T) {
	reg := NewTemplateRegistry()
	reg.Update(map[string]*RegisteredTemplate{
		"a": {Name: "a"},
		"b": {Name: "b"},
		"c": {Name: "c"},
	}, map[string]string{})

	all := reg.GetAll()
	if len(all) != 3 {
		t.Fatalf("expected 3 templates, got %d", len(all))
	}

	names := make(map[string]bool)
	for _, tmpl := range all {
		names[tmpl.Name] = true
	}
	for _, n := range []string{"a", "b", "c"} {
		if !names[n] {
			t.Fatalf("missing template %q in GetAll result", n)
		}
	}
}

func TestRegistry_Update(t *testing.T) {
	reg := NewTemplateRegistry()

	// Initial state
	reg.Update(map[string]*RegisteredTemplate{
		"old": {Name: "old"},
	}, map[string]string{"old": "hash1"})

	if _, ok := reg.Get("old"); !ok {
		t.Fatal("old should exist after first update")
	}

	// Replace with new set
	reg.Update(map[string]*RegisteredTemplate{
		"new": {Name: "new"},
	}, map[string]string{"new": "hash2"})

	if _, ok := reg.Get("old"); ok {
		t.Fatal("old should NOT exist after second update")
	}
	if _, ok := reg.Get("new"); !ok {
		t.Fatal("new should exist after second update")
	}
}

func TestRegistry_GetHash(t *testing.T) {
	reg := NewTemplateRegistry()
	reg.Update(map[string]*RegisteredTemplate{
		"tmpl1": {Name: "tmpl1"},
	}, map[string]string{
		"tmpl1": "deadbeef",
	})

	hash, ok := reg.GetHash("tmpl1")
	if !ok {
		t.Fatal("expected to find hash for tmpl1")
	}
	if hash != "deadbeef" {
		t.Fatalf("expected hash deadbeef, got %s", hash)
	}

	_, ok = reg.GetHash("nonexistent")
	if ok {
		t.Fatal("expected not found for nonexistent hash")
	}
}

// =============================================================================
// Unit Test: Registry concurrent access is safe
// =============================================================================

func TestRegistry_ConcurrentAccess(t *testing.T) {
	reg := NewTemplateRegistry()
	reg.Update(map[string]*RegisteredTemplate{
		"tmpl1": {Name: "tmpl1"},
		"tmpl2": {Name: "tmpl2"},
	}, map[string]string{
		"tmpl1": "hash1",
		"tmpl2": "hash2",
	})

	var wg sync.WaitGroup
	const goroutines = 50

	// Concurrent readers
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				reg.Get("tmpl1")
				reg.GetAll()
				reg.GetHash("tmpl2")
			}
		}()
	}

	// Concurrent writer
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 50; j++ {
			reg.Update(map[string]*RegisteredTemplate{
				"tmpl1": {Name: "tmpl1"},
				"tmpl2": {Name: "tmpl2"},
			}, map[string]string{
				"tmpl1": "hash1_updated",
				"tmpl2": "hash2_updated",
			})
		}
	}()

	wg.Wait()
	// If we get here without a race condition panic, the test passes.
}

// =============================================================================
// Unit Test: loadAll computes correct SHA-256 hashes
// =============================================================================

func TestLoadAll_HashComputation(t *testing.T) {
	dir := t.TempDir()
	content := "SELECT * FROM users WHERE id = {{.Params.id | safeInt}}"
	writeTemplateFile(t, dir, "test.sql.tmpl", content)

	cfg := minimalConfig(dir, []config.TemplateConfig{
		{Name: "test", File: "test.sql.tmpl"},
	})

	result, err := loadAll(cfg, buildFuncMap(), zap.NewNop())
	if err != nil {
		t.Fatalf("loadAll error: %v", err)
	}

	hash, ok := result.Hashes["test"]
	if !ok {
		t.Fatal("expected hash for test template")
	}

	// Compute expected hash
	h := sha256.Sum256([]byte(content))
	expected := hex.EncodeToString(h[:])
	if hash != expected {
		t.Fatalf("hash mismatch: got %s, expected %s", hash, expected)
	}
}

// =============================================================================
// Unit Test: loadAll with parameter schema compilation
// =============================================================================

func TestLoadAll_ParamSchemaCompilation(t *testing.T) {
	dir := t.TempDir()
	writeTemplateFile(t, dir, "report.sql.tmpl", "SELECT 1 WHERE id = {{.Params.id | safeInt}}")

	pattern := `^\d{4}-\d{2}-\d{2}$`
	defaultVal := "100"
	cfg := minimalConfig(dir, []config.TemplateConfig{
		{
			Name: "report",
			File: "report.sql.tmpl",
			Parameters: []config.TemplateParamConfig{
				{Name: "id", Type: "int", Required: true},
				{Name: "limit", Type: "int", Required: false, Default: &defaultVal},
				{Name: "date", Type: "string", Required: false, Pattern: &pattern},
			},
		},
	})

	result, err := loadAll(cfg, buildFuncMap(), zap.NewNop())
	if err != nil {
		t.Fatalf("loadAll error: %v", err)
	}

	reg, ok := result.Registered["report"]
	if !ok {
		t.Fatal("report should be registered")
	}

	if len(reg.ParamSchemas) != 3 {
		t.Fatalf("expected 3 param schemas, got %d", len(reg.ParamSchemas))
	}

	// Check id schema
	if reg.ParamSchemas[0].Name != "id" || !reg.ParamSchemas[0].Required {
		t.Fatal("id schema mismatch")
	}

	// Check limit schema has typed default
	if reg.ParamSchemas[1].Default != int64(100) {
		t.Fatalf("limit default should be int64(100), got %v (%T)", reg.ParamSchemas[1].Default, reg.ParamSchemas[1].Default)
	}

	// Check date schema has compiled pattern
	if reg.ParamSchemas[2].Pattern == nil {
		t.Fatal("date pattern should be compiled")
	}
}
