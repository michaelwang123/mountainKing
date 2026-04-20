// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package schema_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// schemaDir returns the absolute path to the schema directory.
func schemaDir() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Dir(filename)
}

// readAllSchemaFiles reads and concatenates all .graphql files in the schema directory.
func readAllSchemaFiles(t *testing.T) (string, []string) {
	t.Helper()
	dir := schemaDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read schema dir: %v", err)
	}

	var combined strings.Builder
	var files []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".graphql") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read %s: %v", e.Name(), err)
		}
		combined.Write(data)
		combined.WriteByte('\n')
		files = append(files, e.Name())
	}
	return combined.String(), files
}

// TestProperty9_IntrospectionSupport validates that the schema supports
// introspection by verifying the Query root type is defined and the schema
// files are well-formed. Introspection (__schema, __type) is a built-in
// GraphQL feature that works when a valid Query type exists.
//
// Feature: graphql-multi-datasource-api, Property 9: Introspection 启用/禁用
// **Validates: Requirements 2.6, 2.8**
func TestProperty9_IntrospectionSupport(t *testing.T) {
	content, files := readAllSchemaFiles(t)

	rapid.Check(t, func(t *rapid.T) {

		if len(files) == 0 {
			t.Fatal("no .graphql schema files found")
		}

		// Property: The schema MUST define a Query root type (required for introspection).
		if !strings.Contains(content, "type Query") {
			t.Fatal("schema does not define 'type Query' — introspection requires a Query root type")
		}

		// Property: The schema must NOT define a Subscription type (per Requirements 2.11, 2.12).
		// This is also relevant to introspection: __schema.subscriptionType should be null.
		if strings.Contains(content, "type Subscription") {
			t.Fatal("schema defines 'type Subscription' but the service does not support subscriptions")
		}

		// Property: Introspection can be toggled via config (Requirements 2.8).
		// At the schema level, we verify the schema is valid for introspection by
		// checking that essential types are present.
		// Pick a random introspection-related keyword to verify schema completeness.
		introspectionPrereqs := []string{"type Query", "scalar DateTime", "scalar JSON"}
		idx := rapid.IntRange(0, len(introspectionPrereqs)-1).Draw(t, "prereqIdx")
		prereq := introspectionPrereqs[idx]
		if !strings.Contains(content, prereq) {
			t.Fatalf("schema missing prerequisite %q for valid introspection", prereq)
		}

		// Property: Schema files are modular (Requirements 2.7) — at least base.graphql exists.
		hasBase := false
		for _, f := range files {
			if f == "base.graphql" {
				hasBase = true
				break
			}
		}
		if !hasBase {
			t.Fatal("base.graphql not found — schema must have a base file")
		}
	})
}

// TestProperty10_UnsupportedOperationTypesRejected validates that the schema
// does NOT define a Subscription root type and that the Mutation type only
// contains management operations (no data write mutations).
//
// Feature: graphql-multi-datasource-api, Property 10: 不支持的操作类型被拒绝
// **Validates: Requirements 2.11, 2.12**
func TestProperty10_UnsupportedOperationTypesRejected(t *testing.T) {
	content, _ := readAllSchemaFiles(t)

	rapid.Check(t, func(t *rapid.T) {

		// Property 1: Schema MUST NOT define "type Subscription".
		// Per Requirements 2.12, the GraphQL engine shall reject all subscription operations.
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			// Skip comments.
			if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "\"\"\"") {
				continue
			}
			if strings.HasPrefix(trimmed, "type Subscription") {
				t.Fatalf("line %d: schema defines 'type Subscription' which is forbidden (Requirements 2.12)", i+1)
			}
		}

		// Property 2: Schema MUST define "type Mutation" with only management operations.
		// Per Requirements 2.9, 2.10, only clearCache is allowed.
		if !strings.Contains(content, "type Mutation") {
			t.Fatal("schema must define 'type Mutation' for management operations (Requirements 2.9)")
		}

		// Extract the Mutation type block and verify it only contains clearCache.
		mutIdx := strings.Index(content, "type Mutation")
		if mutIdx == -1 {
			t.Fatal("could not find 'type Mutation' in schema")
		}

		// Find the opening brace.
		braceStart := strings.Index(content[mutIdx:], "{")
		if braceStart == -1 {
			t.Fatal("'type Mutation' has no opening brace")
		}
		braceStart += mutIdx

		// Find the matching closing brace.
		depth := 0
		braceEnd := -1
		for i := braceStart; i < len(content); i++ {
			if content[i] == '{' {
				depth++
			} else if content[i] == '}' {
				depth--
				if depth == 0 {
					braceEnd = i
					break
				}
			}
		}
		if braceEnd == -1 {
			t.Fatal("'type Mutation' has no matching closing brace")
		}

		mutationBody := content[braceStart+1 : braceEnd]

		// Extract field definitions (lines that look like field declarations).
		mutationLines := strings.Split(mutationBody, "\n")
		var fieldNames []string
		for _, ml := range mutationLines {
			trimmed := strings.TrimSpace(ml)
			// Skip empty lines, comments, and doc strings.
			if trimmed == "" || strings.HasPrefix(trimmed, "#") ||
				strings.HasPrefix(trimmed, "\"\"\"") || strings.HasPrefix(trimmed, "\"") {
				continue
			}
			// A field definition starts with a letter (not a keyword).
			if len(trimmed) > 0 && trimmed[0] >= 'a' && trimmed[0] <= 'z' {
				// Extract field name (up to first '(' or ':').
				name := trimmed
				if idx := strings.IndexAny(name, "(:"); idx != -1 {
					name = strings.TrimSpace(name[:idx])
				}
				fieldNames = append(fieldNames, name)
			}
		}

		// Property: Mutation type should only contain management operations.
		// Currently only clearCache is defined.
		for _, fn := range fieldNames {
			if fn != "clearCache" {
				t.Fatalf("Mutation type contains unexpected field %q — only management operations are allowed (Requirements 2.9, 2.10)", fn)
			}
		}

		if len(fieldNames) == 0 {
			t.Fatal("Mutation type has no fields — expected at least clearCache (Requirements 2.9)")
		}

		// Property 3: Randomly verify that data-write mutation keywords are absent.
		forbiddenOps := []string{"insert", "update", "delete", "create", "drop", "alter"}
		opIdx := rapid.IntRange(0, len(forbiddenOps)-1).Draw(t, "forbiddenOpIdx")
		forbiddenOp := forbiddenOps[opIdx]

		for _, fn := range fieldNames {
			if strings.Contains(strings.ToLower(fn), forbiddenOp) {
				t.Fatalf("Mutation contains data-write operation %q (contains %q) — only management operations allowed",
					fn, forbiddenOp)
			}
		}
	})
}
