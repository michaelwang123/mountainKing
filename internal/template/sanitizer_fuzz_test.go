// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package template

import (
	"testing"
)

// FuzzSanitizeSQL fuzz-tests the sanitizeSQL function with arbitrary inputs.
// Feature: project-hardening, Property 5: no panic on arbitrary input
// **Validates: Requirements 12.2, 12.4**
func FuzzSanitizeSQL(f *testing.F) {
	// Seed corpus
	f.Add("")
	f.Add("SELECT 1")
	f.Add("'")
	f.Add(`\`)
	f.Add("\x00")
	f.Add("'; DROP TABLE --")
	f.Add("' OR '1'='1")
	f.Add("SELECT * FROM t WHERE id = 1; DELETE FROM t")
	f.Add("/* comment */ SELECT 1")
	f.Add("/*+ hint */ SELECT 1")
	f.Add("SELECT 'unclosed")
	f.Add(`SELECT "unclosed`)
	f.Add("SELECT `unclosed")

	f.Fuzz(func(t *testing.T, input string) {
		result, err := sanitizeSQL(input)

		// Property: must not panic (implicit — if we reach here, no panic occurred)

		// Must return either a valid result or an error
		if err == nil && result == "" && input != "" {
			// Empty result from non-empty input is acceptable (e.g., all comments)
			// This is not an error condition
		}
		// If err != nil, that's a valid rejection (unsafe SQL detected)
	})
}
