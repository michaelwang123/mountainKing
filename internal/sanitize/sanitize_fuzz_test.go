// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package sanitize

import (
	"testing"

	"github.com/michaelwang123/mountainKing/internal/config"
)

// FuzzSanitize fuzz-tests the Sanitizer.Sanitize function with arbitrary inputs.
// Feature: project-hardening, Property 5: no panic on arbitrary input
// **Validates: Requirements 12.3, 12.4**
func FuzzSanitize(f *testing.F) {
	// Seed corpus
	f.Add("")
	f.Add("'")
	f.Add(`\`)
	f.Add("\x00")
	f.Add("'; DROP TABLE --")
	f.Add("' OR '1'='1")
	f.Add("SELECT * FROM users WHERE name = 'alice'")
	f.Add("INSERT INTO t VALUES ('secret', 99999)")

	s, err := NewSanitizer(config.SanitizationConfig{Enabled: true})
	if err != nil {
		f.Fatalf("failed to create sanitizer with default rules: %v", err)
	}

	f.Fuzz(func(t *testing.T, input string) {
		// Property: must not panic (implicit — if we reach here, no panic occurred)
		_ = s.Sanitize(input)
	})
}
