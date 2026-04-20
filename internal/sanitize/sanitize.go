// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

// Package sanitize provides sensitive data masking for SQL statements
// and other strings recorded in logs and trace spans.
package sanitize

import (
	"regexp"

	"github.com/michaelwang123/mountainKing/internal/config"
)

// DefaultRules returns the built-in sanitization rules:
//   - SQL string literals ('...') â†?'***'
//   - 4+ digit numbers â†?***
func DefaultRules() []config.SanitizationRule {
	return []config.SanitizationRule{
		{Pattern: `'[^']*'`, Replacement: "'***'"},
		{Pattern: `\b\d{4,}\b`, Replacement: "***"},
	}
}

// compiledRule pairs a compiled regex with its replacement string.
type compiledRule struct {
	re          *regexp.Regexp
	replacement string
}

// Sanitizer applies regex-based sanitization rules to input strings.
type Sanitizer struct {
	enabled bool
	rules   []compiledRule
}

// NewSanitizer creates a Sanitizer from the given config.
// When disabled, Sanitize returns the input unchanged.
// If no rules are configured but sanitization is enabled, default rules are used.
func NewSanitizer(cfg config.SanitizationConfig) (*Sanitizer, error) {
	if !cfg.Enabled {
		return &Sanitizer{enabled: false}, nil
	}

	rules := cfg.Rules
	if len(rules) == 0 {
		rules = DefaultRules()
	}

	compiled := make([]compiledRule, 0, len(rules))
	for _, r := range rules {
		re, err := regexp.Compile(r.Pattern)
		if err != nil {
			return nil, err
		}
		compiled = append(compiled, compiledRule{re: re, replacement: r.Replacement})
	}

	return &Sanitizer{enabled: true, rules: compiled}, nil
}

// Sanitize applies all configured rules sequentially to the input string.
// Returns the input unchanged when the sanitizer is disabled.
func (s *Sanitizer) Sanitize(input string) string {
	if !s.enabled {
		return input
	}
	result := input
	for _, r := range s.rules {
		result = r.re.ReplaceAllString(result, r.replacement)
	}
	return result
}
