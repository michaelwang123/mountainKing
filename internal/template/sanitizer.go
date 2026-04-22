// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package template

import (
	"strings"

	apierrors "github.com/michaelwang123/mountainKing/internal/errors"
)

// scannerState represents the current state of the SQL lexical scanner.
type scannerState int

const (
	stateNormal         scannerState = iota // default state
	stateInSingleQuote                      // inside '...'
	stateInDoubleQuote                      // inside "..."
	stateInBacktick                         // inside `...`
	stateInLineComment                      // inside -- comment
	stateInBlockComment                     // inside /* */ comment (non-hint)
	stateInHint                             // inside /*+ */ optimizer hint
)

// sanitizeSQL performs security checks on rendered SQL using a linear O(n)
// lexical scanner with 7 states. It:
//   - Detects semicolons outside string literals / quoted identifiers (multi-statement injection)
//   - Removes non-hint SQL comments (-- and /* */) while preserving optimizer hints (/*+ ... */)
//   - Detects unclosed string literals / quoted identifiers at EOF
//
// Returns the cleaned SQL or a VALIDATION_UNSAFE_SQL error.
func sanitizeSQL(sql string) (string, error) {
	var out strings.Builder
	out.Grow(len(sql))

	state := stateNormal
	i := 0
	n := len(sql)

	for i < n {
		ch := sql[i]

		switch state {
		case stateNormal:
			switch {
			case ch == '\'':
				out.WriteByte(ch)
				state = stateInSingleQuote
				i++

			case ch == '"':
				out.WriteByte(ch)
				state = stateInDoubleQuote
				i++

			case ch == '`':
				out.WriteByte(ch)
				state = stateInBacktick
				i++

			case ch == '-' && i+1 < n && sql[i+1] == '-':
				// Start of line comment -- skip, don't write to output
				state = stateInLineComment
				i += 2

			case ch == '/' && i+1 < n && sql[i+1] == '*':
				// Check if this is an optimizer hint /*+ ... */
				if i+2 < n && sql[i+2] == '+' {
					// Optimizer hint: preserve in output
					out.WriteString("/*+")
					state = stateInHint
					i += 3
				} else {
					// Block comment: skip, don't write to output
					state = stateInBlockComment
					i += 2
				}

			case ch == ';':
				return "", apierrors.ValidationError(
					apierrors.ErrValidationUnsafeSQL,
					"multi-statement SQL detected: semicolon found outside string literal",
				)

			default:
				out.WriteByte(ch)
				i++
			}

		case stateInSingleQuote:
			if ch == '\\' && i+1 < n && sql[i+1] == '\\' {
				// Escaped backslash \\ — write both and stay in state
				out.WriteByte('\\')
				out.WriteByte('\\')
				i += 2
			} else if ch == '\\' && i+1 < n && sql[i+1] == '\'' {
				// Backslash-escaped quote \' — write both and stay in state
				out.WriteByte('\\')
				out.WriteByte('\'')
				i += 2
			} else if ch == '\'' && i+1 < n && sql[i+1] == '\'' {
				// Doubled quote '' — write both and stay in state
				out.WriteByte('\'')
				out.WriteByte('\'')
				i += 2
			} else if ch == '\'' {
				// Closing quote — back to NORMAL
				out.WriteByte(ch)
				state = stateNormal
				i++
			} else {
				out.WriteByte(ch)
				i++
			}

		case stateInDoubleQuote:
			if ch == '"' && i+1 < n && sql[i+1] == '"' {
				// Doubled double-quote "" — write both and stay in state
				out.WriteByte('"')
				out.WriteByte('"')
				i += 2
			} else if ch == '"' {
				// Closing double-quote — back to NORMAL
				out.WriteByte(ch)
				state = stateNormal
				i++
			} else {
				out.WriteByte(ch)
				i++
			}

		case stateInBacktick:
			if ch == '`' && i+1 < n && sql[i+1] == '`' {
				// Doubled backtick `` — write both and stay in state
				out.WriteByte('`')
				out.WriteByte('`')
				i += 2
			} else if ch == '`' {
				// Closing backtick — back to NORMAL
				out.WriteByte(ch)
				state = stateNormal
				i++
			} else {
				out.WriteByte(ch)
				i++
			}

		case stateInLineComment:
			if ch == '\n' {
				// End of line comment — emit the newline and return to NORMAL
				out.WriteByte('\n')
				state = stateNormal
			}
			// Skip all comment content (including the newline advance)
			i++

		case stateInBlockComment:
			if ch == '*' && i+1 < n && sql[i+1] == '/' {
				// End of block comment — return to NORMAL, replace with space
				out.WriteByte(' ')
				state = stateNormal
				i += 2
			} else {
				// Skip comment content
				i++
			}

		case stateInHint:
			if ch == '*' && i+1 < n && sql[i+1] == '/' {
				// End of hint — preserve closing */
				out.WriteString("*/")
				state = stateNormal
				i += 2
			} else {
				// Preserve hint content
				out.WriteByte(ch)
				i++
			}
		}
	}

	// Check for unclosed states at EOF
	switch state {
	case stateInSingleQuote:
		return "", apierrors.ValidationError(
			apierrors.ErrValidationUnsafeSQL,
			"unclosed single-quoted string literal",
		)
	case stateInDoubleQuote:
		return "", apierrors.ValidationError(
			apierrors.ErrValidationUnsafeSQL,
			"unclosed double-quoted identifier",
		)
	case stateInBacktick:
		return "", apierrors.ValidationError(
			apierrors.ErrValidationUnsafeSQL,
			"unclosed backtick-quoted identifier",
		)
	case stateInBlockComment:
		return "", apierrors.ValidationError(
			apierrors.ErrValidationUnsafeSQL,
			"unclosed block comment",
		)
	case stateInHint:
		return "", apierrors.ValidationError(
			apierrors.ErrValidationUnsafeSQL,
			"unclosed optimizer hint",
		)
	}

	return out.String(), nil
}
