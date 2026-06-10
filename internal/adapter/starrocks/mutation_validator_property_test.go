// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package starrocks

import (
	"fmt"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/michaelwang123/mountainKing/internal/datasource"
	apierrors "github.com/michaelwang123/mountainKing/internal/errors"
)

// --- Generators ---

// genValidMutationIdentifier generates strings that match ^[a-zA-Z_][a-zA-Z0-9_]*$.
func genValidMutationIdentifier(t *rapid.T, label string) string {
	return rapid.StringMatching(`[a-zA-Z_][a-zA-Z0-9_]{0,15}`).Draw(t, label)
}

// genInvalidMutationIdentifier generates strings that do NOT match ^[a-zA-Z_][a-zA-Z0-9_]*$.
// Includes empty strings, digit-start strings, and strings with special characters.
func genInvalidMutationIdentifier(t *rapid.T, label string) string {
	category := rapid.IntRange(0, 4).Draw(t, label+"_cat")
	switch category {
	case 0:
		// Empty string
		return ""
	case 1:
		// Starts with a digit
		digit := rapid.StringMatching(`[0-9]`).Draw(t, label+"_digit")
		tail := rapid.StringMatching(`[a-zA-Z0-9_]{0,10}`).Draw(t, label+"_tail")
		return digit + tail
	case 2:
		// Contains special characters (dash, space, dot, etc.)
		prefix := rapid.StringMatching(`[a-zA-Z_][a-zA-Z0-9_]{0,5}`).Draw(t, label+"_prefix")
		special := rapid.SampledFrom([]string{"-", " ", ".", ";", "'", "\"", "(", ")", "@", "#", "!", "+"}).Draw(t, label+"_special")
		suffix := rapid.StringMatching(`[a-zA-Z0-9_]{0,5}`).Draw(t, label+"_suffix")
		return prefix + special + suffix
	case 3:
		// Only special characters
		return rapid.StringMatching(`[-@#!.;' ]{1,10}`).Draw(t, label+"_allSpecial")
	default:
		// Unicode/non-ASCII characters
		prefix := rapid.StringMatching(`[a-zA-Z_]`).Draw(t, label+"_start")
		return prefix + "é" + rapid.StringMatching(`[a-zA-Z0-9_]{0,5}`).Draw(t, label+"_rest")
	}
}

// --- Property 4: Identifier Validation (Strict) ---

// TestProperty4_IdentifierValidation_ValidMatches verifies that strings matching
// ^[a-zA-Z_][a-zA-Z0-9_]*$ produce nil error from ValidateMutationIdentifier.
//
// **Validates: Requirements 4.1, 4.2**
func TestProperty4_IdentifierValidation_ValidMatches(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		identifier := genValidMutationIdentifier(t, "validId")

		err := ValidateMutationIdentifier(identifier)

		if err != nil {
			t.Fatalf("valid identifier %q should pass validation, got error: %v", identifier, err)
		}
	})
}

// TestProperty4_IdentifierValidation_InvalidRejects verifies that strings NOT matching
// ^[a-zA-Z_][a-zA-Z0-9_]*$ produce a non-nil error from ValidateMutationIdentifier.
//
// **Validates: Requirements 4.1, 4.2**
func TestProperty4_IdentifierValidation_InvalidRejects(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		identifier := genInvalidMutationIdentifier(t, "invalidId")

		err := ValidateMutationIdentifier(identifier)

		if err == nil {
			t.Fatalf("invalid identifier %q should fail validation, but got nil error", identifier)
		}
	})
}

// --- Property 7: Batch Size Limit Enforcement ---

// TestProperty7_BatchSizeLimitEnforcement verifies that when rows > maxBatchSize,
// ValidateBatchInsertInput returns an error.
//
// **Validates: Requirements 1.9**
func TestProperty7_BatchSizeLimitEnforcement(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a maxBatchSize between 1 and 100
		maxBatchSize := rapid.IntRange(1, 100).Draw(t, "maxBatchSize")
		validator := NewMutationValidator(maxBatchSize, 1048576)

		// Generate a valid table name and at least one valid column
		table := genValidMutationIdentifier(t, "table")
		numCols := rapid.IntRange(1, 5).Draw(t, "numCols")
		columns := make([]string, numCols)
		for i := range numCols {
			columns[i] = genValidMutationIdentifier(t, fmt.Sprintf("col_%d", i))
		}

		// Generate rows exceeding maxBatchSize
		rowCount := maxBatchSize + rapid.IntRange(1, 50).Draw(t, "excess")
		rows := make([][]any, rowCount)
		for i := range rowCount {
			row := make([]any, numCols)
			for j := range numCols {
				row[j] = fmt.Sprintf("val_%d_%d", i, j)
			}
			rows[i] = row
		}

		err := validator.ValidateBatchInsertInput(table, columns, rows)

		if err == nil {
			t.Fatalf("batch size %d exceeds maxBatchSize %d, but got nil error",
				rowCount, maxBatchSize)
		}

		// Verify it's the correct error type
		apiErr, ok := err.(*apierrors.APIError)
		if !ok {
			t.Fatalf("expected *apierrors.APIError, got %T: %v", err, err)
		}
		if apiErr.Code != apierrors.ErrValidationBatchLimitExceeded {
			t.Fatalf("expected error code %q, got %q", apierrors.ErrValidationBatchLimitExceeded, apiErr.Code)
		}
	})
}

// TestProperty7_BatchSizeWithinLimit verifies that when rows <= maxBatchSize
// (and all other inputs are valid), ValidateBatchInsertInput does NOT return a batch size error.
//
// **Validates: Requirements 1.9**
func TestProperty7_BatchSizeWithinLimit(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a maxBatchSize between 1 and 100
		maxBatchSize := rapid.IntRange(1, 100).Draw(t, "maxBatchSize")
		validator := NewMutationValidator(maxBatchSize, 1048576)

		// Generate a valid table name and at least one valid column
		table := genValidMutationIdentifier(t, "table")
		numCols := rapid.IntRange(1, 5).Draw(t, "numCols")
		columns := make([]string, numCols)
		for i := range numCols {
			columns[i] = genValidMutationIdentifier(t, fmt.Sprintf("col_%d", i))
		}

		// Generate rows within the limit
		rowCount := rapid.IntRange(1, maxBatchSize).Draw(t, "rowCount")
		rows := make([][]any, rowCount)
		for i := range rowCount {
			row := make([]any, numCols)
			for j := range numCols {
				row[j] = fmt.Sprintf("val_%d_%d", i, j)
			}
			rows[i] = row
		}

		err := validator.ValidateBatchInsertInput(table, columns, rows)

		// Should not get a batch limit error (may still be nil or another error type)
		if err != nil {
			apiErr, ok := err.(*apierrors.APIError)
			if ok && apiErr.Code == apierrors.ErrValidationBatchLimitExceeded {
				t.Fatalf("batch size %d is within maxBatchSize %d, but got batch limit error",
					rowCount, maxBatchSize)
			}
		}
	})
}

// --- Property 15: SQL Length Enforcement ---

// TestProperty15_SQLLengthEnforcement verifies that SQL strings exceeding maxSQLLength
// produce an error from ValidateSQLLength.
//
// **Validates: Requirements 4.8**
func TestProperty15_SQLLengthEnforcement(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a maxSQLLength between 100 and 10000
		maxSQLLength := rapid.IntRange(100, 10000).Draw(t, "maxSQLLength")
		validator := NewMutationValidator(500, maxSQLLength)

		// Generate SQL that exceeds the limit
		excess := rapid.IntRange(1, 1000).Draw(t, "excess")
		sqlLen := maxSQLLength + excess
		sql := strings.Repeat("X", sqlLen)

		err := validator.ValidateSQLLength(sql)

		if err == nil {
			t.Fatalf("SQL length %d exceeds maxSQLLength %d, but got nil error",
				sqlLen, maxSQLLength)
		}

		// Verify it's the correct error type
		apiErr, ok := err.(*apierrors.APIError)
		if !ok {
			t.Fatalf("expected *apierrors.APIError, got %T: %v", err, err)
		}
		if apiErr.Code != apierrors.ErrValidationPayloadTooLarge {
			t.Fatalf("expected error code %q, got %q", apierrors.ErrValidationPayloadTooLarge, apiErr.Code)
		}
	})
}

// TestProperty15_SQLLengthWithinLimit verifies that SQL strings within maxSQLLength
// produce nil from ValidateSQLLength.
//
// **Validates: Requirements 4.8**
func TestProperty15_SQLLengthWithinLimit(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a maxSQLLength between 100 and 10000
		maxSQLLength := rapid.IntRange(100, 10000).Draw(t, "maxSQLLength")
		validator := NewMutationValidator(500, maxSQLLength)

		// Generate SQL within the limit
		sqlLen := rapid.IntRange(1, maxSQLLength).Draw(t, "sqlLen")
		sql := strings.Repeat("X", sqlLen)

		err := validator.ValidateSQLLength(sql)

		if err != nil {
			t.Fatalf("SQL length %d is within maxSQLLength %d, but got error: %v",
				sqlLen, maxSQLLength, err)
		}
	})
}

// --- Property 16: Validation Error Code Mapping ---

// TestProperty16_ValidationErrorCodeMapping verifies that all validation failures
// return a GraphQL error with extension code matching VALIDATION_* or MUTATION_* patterns.
//
// **Validates: Requirements 10.1**
func TestProperty16_ValidationErrorCodeMapping(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		validator := NewMutationValidator(10, 1000)

		// Choose a validation failure scenario
		scenario := rapid.IntRange(0, 7).Draw(t, "scenario")

		var err error
		switch scenario {
		case 0:
			// Invalid identifier (table name)
			invalidTable := genInvalidMutationIdentifier(t, "badTable")
			err = ValidateMutationIdentifier(invalidTable)
		case 1:
			// Empty values for insert
			table := genValidMutationIdentifier(t, "table")
			columns := []string{genValidMutationIdentifier(t, "col")}
			err = validator.ValidateInsertInput(table, columns, []any{}) // empty values
		case 2:
			// Empty filters for delete
			table := genValidMutationIdentifier(t, "table")
			err = validator.ValidateDeleteInput(table, []datasource.FilterCondition{})
		case 3:
			// Empty filters for update
			table := genValidMutationIdentifier(t, "table")
			cols := []string{genValidMutationIdentifier(t, "setCol")}
			err = validator.ValidateUpdateInput(table, cols, []datasource.FilterCondition{})
		case 4:
			// Batch size exceeded
			table := genValidMutationIdentifier(t, "table")
			columns := []string{genValidMutationIdentifier(t, "col")}
			rows := make([][]any, 11) // exceeds maxBatchSize of 10
			for i := range rows {
				rows[i] = []any{"val"}
			}
			err = validator.ValidateBatchInsertInput(table, columns, rows)
		case 5:
			// SQL too long
			sql := strings.Repeat("X", 1001) // exceeds maxSQLLength of 1000
			err = validator.ValidateSQLLength(sql)
		case 6:
			// Empty SET columns for update
			table := genValidMutationIdentifier(t, "table")
			filters := []datasource.FilterCondition{
				{Field: genValidMutationIdentifier(t, "field"), Operator: datasource.FilterOpEQ, Value: "x"},
			}
			err = validator.ValidateUpdateInput(table, []string{}, filters)
		case 7:
			// Invalid column name in insert
			table := genValidMutationIdentifier(t, "table")
			invalidCol := genInvalidMutationIdentifier(t, "badCol")
			err = validator.ValidateInsertInput(table, []string{invalidCol}, []any{"value"})
		}

		// All scenarios must produce a non-nil error
		if err == nil {
			t.Fatalf("scenario %d should produce a validation error, got nil", scenario)
		}

		// The error must be an APIError with code starting with "VALIDATION_" or "MUTATION_"
		apiErr, ok := err.(*apierrors.APIError)
		if !ok {
			t.Fatalf("scenario %d: expected *apierrors.APIError, got %T: %v", scenario, err, err)
		}

		if !strings.HasPrefix(apiErr.Code, "VALIDATION_") && !strings.HasPrefix(apiErr.Code, "MUTATION_") {
			t.Fatalf("scenario %d: error code %q does not match VALIDATION_* or MUTATION_* pattern",
				scenario, apiErr.Code)
		}
	})
}

// TestProperty16_WritableTableValidatorErrorCodes verifies that WritableTableValidator
// errors also match the VALIDATION_* or MUTATION_* code pattern.
//
// **Validates: Requirements 10.1**
func TestProperty16_WritableTableValidatorErrorCodes(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Set up a minimal writable table validator
		writable := map[string]*WritableTableConfig{
			"allowed_table": {
				Columns:           map[string]bool{"col_a": true, "col_b": true},
				AllowedOperations: map[string]bool{"insert": true},
			},
		}
		allowed := map[string]map[string]bool{
			"allowed_table": {"col_a": true, "col_b": true, "col_c": true},
		}
		wtv := NewWritableTableValidator(writable, allowed)

		// Choose a validation failure scenario
		scenario := rapid.IntRange(0, 3).Draw(t, "scenario")

		var err error
		switch scenario {
		case 0:
			// Table not in writable whitelist
			nonExistentTable := genValidMutationIdentifier(t, "badTable")
			// Ensure it doesn't coincidentally match "allowed_table"
			if nonExistentTable == "allowed_table" {
				nonExistentTable = "not_" + nonExistentTable
			}
			err = wtv.ValidateTable(nonExistentTable)
		case 1:
			// Column not in writable whitelist
			nonExistentCol := genValidMutationIdentifier(t, "badCol")
			if nonExistentCol == "col_a" || nonExistentCol == "col_b" {
				nonExistentCol = "not_" + nonExistentCol
			}
			err = wtv.ValidateWriteColumns("allowed_table", []string{nonExistentCol})
		case 2:
			// Operation not supported
			err = wtv.ValidateOperation("allowed_table", "delete") // only "insert" allowed
		case 3:
			// Filter column not in allowed_tables
			nonExistentField := genValidMutationIdentifier(t, "badField")
			if nonExistentField == "col_a" || nonExistentField == "col_b" || nonExistentField == "col_c" {
				nonExistentField = "not_" + nonExistentField
			}
			err = wtv.ValidateFilterColumns("allowed_table", []string{nonExistentField})
		}

		// All scenarios must produce a non-nil error
		if err == nil {
			t.Fatalf("scenario %d should produce a validation error, got nil", scenario)
		}

		// The error must be an APIError with code starting with "VALIDATION_" or "MUTATION_"
		apiErr, ok := err.(*apierrors.APIError)
		if !ok {
			t.Fatalf("scenario %d: expected *apierrors.APIError, got %T: %v", scenario, err, err)
		}

		if !strings.HasPrefix(apiErr.Code, "VALIDATION_") && !strings.HasPrefix(apiErr.Code, "MUTATION_") {
			t.Fatalf("scenario %d: error code %q does not match VALIDATION_* or MUTATION_* pattern",
				scenario, apiErr.Code)
		}
	})
}
