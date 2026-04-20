// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package cache

import (
	"strings"
	"unicode"
)

// graphqlKeywords are GraphQL keywords that get lowercased during normalization.
var graphqlKeywords = map[string]bool{
	"QUERY":        true,
	"MUTATION":     true,
	"SUBSCRIPTION": true,
	"FRAGMENT":     true,
	"ON":           true,
	"TRUE":         true,
	"FALSE":        true,
	"NULL":         true,
}

// NormalizeQuery normalizes a GraphQL query string to improve cache hit rates.
// It removes extra whitespace/newlines, strips comments, and lowercases keywords.
// Field order is preserved since it may affect semantics.
func NormalizeQuery(query string) string {
	// Remove comments first
	query = removeComments(query)

	// Tokenize, normalize whitespace, and lowercase keywords
	var b strings.Builder
	b.Grow(len(query))

	tokens := tokenize(query)
	for i, tok := range tokens {
		if i > 0 {
			b.WriteByte(' ')
		}
		if graphqlKeywords[strings.ToUpper(tok)] {
			b.WriteString(strings.ToLower(tok))
		} else {
			b.WriteString(tok)
		}
	}

	return b.String()
}

// removeComments strips single-line (#) comments from the query.
func removeComments(query string) string {
	var b strings.Builder
	b.Grow(len(query))

	inString := false
	i := 0
	for i < len(query) {
		ch := query[i]

		// Track string literals to avoid stripping # inside strings
		if ch == '"' && !isEscaped(query, i) {
			inString = !inString
			b.WriteByte(ch)
			i++
			continue
		}

		if !inString && ch == '#' {
			// Skip until end of line
			for i < len(query) && query[i] != '\n' {
				i++
			}
			continue
		}

		b.WriteByte(ch)
		i++
	}

	return b.String()
}

// isEscaped checks if the character at position i is preceded by a backslash.
func isEscaped(s string, i int) bool {
	if i == 0 {
		return false
	}
	backslashes := 0
	for j := i - 1; j >= 0 && s[j] == '\\'; j-- {
		backslashes++
	}
	return backslashes%2 == 1
}

// tokenize splits the query into whitespace-separated tokens,
// treating GraphQL punctuation ({, }, (, ), :, !, @, $, ..., =) as separate tokens.
func tokenize(query string) []string {
	var tokens []string
	var current strings.Builder

	flush := func() {
		if current.Len() > 0 {
			tokens = append(tokens, current.String())
			current.Reset()
		}
	}

	runes := []rune(query)
	for i := 0; i < len(runes); i++ {
		r := runes[i]

		if unicode.IsSpace(r) {
			flush()
			continue
		}

		// Handle spread operator (...)
		if r == '.' && i+2 < len(runes) && runes[i+1] == '.' && runes[i+2] == '.' {
			flush()
			tokens = append(tokens, "...")
			i += 2
			continue
		}

		// GraphQL punctuation characters are standalone tokens
		if isPunctuation(r) {
			flush()
			tokens = append(tokens, string(r))
			continue
		}

		current.WriteRune(r)
	}
	flush()

	return tokens
}

func isPunctuation(r rune) bool {
	switch r {
	case '{', '}', '(', ')', ':', '!', '@', '$', '=', ',', '[', ']', '|', '&':
		return true
	}
	return false
}
