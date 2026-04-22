// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package template

import (
	"strings"
	"testing"
)

// FuzzSafeString fuzz-tests the safeString function with arbitrary inputs.
// Feature: project-hardening, Property 5: no panic on arbitrary input
// Feature: project-hardening, Property 6: output safety invariants
// **Validates: Requirements 12.1, 12.4, 12.5**
func FuzzSafeString(f *testing.F) {
	// Seed corpus
	f.Add("")
	f.Add("'")
	f.Add(`\`)
	f.Add("\x00")
	f.Add("'; DROP TABLE --")
	f.Add("' OR '1'='1")
	f.Add("hello world")
	f.Add("O'Brien")
	f.Add("back\\slash")
	f.Add("\x00\x00\x00")

	f.Fuzz(func(t *testing.T, input string) {
		result, err := safeString(input)
		if err != nil {
			// safeString returning an error is acceptable
			return
		}

		// Property 6a: output must not contain NULL bytes
		if strings.Contains(result, "\x00") {
			t.Errorf("safeString(%q) output contains NULL byte: %q", input, result)
		}

		// Property 6b: output must not contain unescaped single quotes.
		// After safeString, every single quote should be doubled ('').
		// Check: no odd-length runs of consecutive single quotes.
		i := 0
		for i < len(result) {
			if result[i] == '\'' {
				count := 0
				for i < len(result) && result[i] == '\'' {
					count++
					i++
				}
				if count%2 != 0 {
					t.Errorf("safeString(%q) output has odd consecutive single quotes (count=%d): %q", input, count, result)
				}
			} else {
				i++
			}
		}
	})
}
