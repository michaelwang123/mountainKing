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
		introspectionPrereqs := []string{"type Query", "scalar DateTime", "scalar JSON", "scalar AnyValue"}
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
// does NOT define a Subscription root type and that the Mutation type contains
// only allowed operations (management + CRUD mutations).
//
// Feature: graphql-multi-datasource-api, Property 10: 不支持的操作类型被拒绝
// **Validates: Requirements 2.11, 2.12**
//
// Updated: Now allows CRUD mutations (insertStarrocks, updateStarrocks,
// deleteStarrocks, insertBatchStarrocks) per graphql-mutations-crud spec.
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

		// Property 2: Schema MUST define "type Mutation" with allowed operations.
		// Use "type Mutation " (with trailing space) to avoid matching "type MutationResult".
		if !strings.Contains(content, "type Mutation {") && !strings.Contains(content, "type Mutation\n") {
			t.Fatal("schema must define 'type Mutation' for management operations (Requirements 2.9)")
		}

		// Extract the Mutation root type block (not MutationResult or other types).
		mutIdx := -1
		searchFrom := 0
		for {
			idx := strings.Index(content[searchFrom:], "type Mutation")
			if idx == -1 {
				break
			}
			absIdx := searchFrom + idx
			// Check that the character after "type Mutation" is a space or '{' (not a letter).
			afterIdx := absIdx + len("type Mutation")
			if afterIdx < len(content) && (content[afterIdx] == ' ' || content[afterIdx] == '{' || content[afterIdx] == '\n') {
				mutIdx = absIdx
				break
			}
			searchFrom = absIdx + 1
		}
		if mutIdx == -1 {
			t.Fatal("could not find 'type Mutation' root type in schema")
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
		inDocString := false
		for _, ml := range mutationLines {
			trimmed := strings.TrimSpace(ml)
			// Handle multi-line doc strings (""" ... """).
			if !inDocString && strings.HasPrefix(trimmed, "\"\"\"") {
				// Check if it's a single-line doc string (opens and closes on same line).
				if strings.Count(trimmed, "\"\"\"") >= 2 {
					continue
				}
				inDocString = true
				continue
			}
			if inDocString {
				if strings.Contains(trimmed, "\"\"\"") {
					inDocString = false
				}
				continue
			}
			// Skip empty lines, comments, and single-line strings.
			if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "\"") {
				continue
			}
			// A field definition starts with a lowercase letter.
			if len(trimmed) > 0 && trimmed[0] >= 'a' && trimmed[0] <= 'z' {
				// Extract field name (up to first '(' or ':').
				name := trimmed
				if idx := strings.IndexAny(name, "(:"); idx != -1 {
					name = strings.TrimSpace(name[:idx])
				}
				fieldNames = append(fieldNames, name)
			}
		}

		// Allowed mutation fields: management ops + CRUD mutations (per graphql-mutations-crud spec).
		allowedFields := map[string]bool{
			"clearCache":           true,
			"insertStarrocks":      true,
			"updateStarrocks":      true,
			"deleteStarrocks":      true,
			"insertBatchStarrocks": true,
		}

		// Property: Mutation type should only contain allowed operations.
		for _, fn := range fieldNames {
			if !allowedFields[fn] {
				t.Fatalf("Mutation type contains unexpected field %q — only allowed operations are: %v", fn, allowedFields)
			}
		}

		if len(fieldNames) == 0 {
			t.Fatal("Mutation type has no fields — expected at least clearCache (Requirements 2.9)")
		}

		// Property: clearCache must always be present (management operation).
		hasClearCache := false
		for _, fn := range fieldNames {
			if fn == "clearCache" {
				hasClearCache = true
				break
			}
		}
		if !hasClearCache {
			t.Fatal("Mutation type must include clearCache management operation")
		}

		// Property 3: Randomly verify that DDL operations are absent (create, drop, alter).
		forbiddenOps := []string{"create", "drop", "alter"}
		opIdx := rapid.IntRange(0, len(forbiddenOps)-1).Draw(t, "forbiddenOpIdx")
		forbiddenOp := forbiddenOps[opIdx]

		for _, fn := range fieldNames {
			if strings.Contains(strings.ToLower(fn), forbiddenOp) {
				t.Fatalf("Mutation contains DDL operation %q (contains %q) — DDL operations are not allowed",
					fn, forbiddenOp)
			}
		}
	})
}
