// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package clickhouse

import (
	"strings"
	"testing"

	"github.com/michaelwang123/mountainKing/internal/datasource"
)

// testWhitelist defines the allowed tables and columns used across all query builder tests.
var testWhitelist = map[string]map[string]bool{
	"events":  {"event_id": true, "user_id": true, "event_type": true, "created_at": true, "payload": true},
	"metrics": {"timestamp": true, "metric_name": true, "value": true, "tags": true},
}

// --- Build: Simple SELECT ---

func TestQueryBuilder_Build_SimpleSelect(t *testing.T) {
	builder := NewSQLQueryBuilder(testWhitelist)
	req := datasource.QueryRequest{
		Fields: []string{"event_id", "user_id", "event_type"},
	}

	sql, params, err := builder.Build(req, "events")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "SELECT `event_id`, `user_id`, `event_type` FROM `events`"
	if sql != expected {
		t.Errorf("expected SQL:\n  %s\ngot:\n  %s", expected, sql)
	}
	if len(params) != 0 {
		t.Errorf("expected 0 params, got %d", len(params))
	}
}

func TestQueryBuilder_Build_SelectStar(t *testing.T) {
	builder := NewSQLQueryBuilder(testWhitelist)
	req := datasource.QueryRequest{
		Fields: nil, // no fields → SELECT *
	}

	sql, params, err := builder.Build(req, "events")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "SELECT * FROM `events`"
	if sql != expected {
		t.Errorf("expected SQL:\n  %s\ngot:\n  %s", expected, sql)
	}
	if len(params) != 0 {
		t.Errorf("expected 0 params, got %d", len(params))
	}
}

// --- Build: All FilterOperators ---

func TestQueryBuilder_Build_FilterEQ(t *testing.T) {
	builder := NewSQLQueryBuilder(testWhitelist)
	req := datasource.QueryRequest{
		Fields: []string{"event_id"},
		Filters: []datasource.FilterCondition{
			{Field: "user_id", Operator: datasource.FilterOpEQ, Value: "abc123"},
		},
	}

	sql, params, err := builder.Build(req, "events")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(sql, "`user_id` = ?") {
		t.Errorf("expected EQ clause, got: %s", sql)
	}
	if len(params) != 1 || params[0] != "abc123" {
		t.Errorf("expected params [abc123], got %v", params)
	}
}

func TestQueryBuilder_Build_FilterNEQ(t *testing.T) {
	builder := NewSQLQueryBuilder(testWhitelist)
	req := datasource.QueryRequest{
		Fields: []string{"event_id"},
		Filters: []datasource.FilterCondition{
			{Field: "event_type", Operator: datasource.FilterOpNEQ, Value: "error"},
		},
	}

	sql, params, err := builder.Build(req, "events")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(sql, "`event_type` != ?") {
		t.Errorf("expected NEQ clause, got: %s", sql)
	}
	if len(params) != 1 || params[0] != "error" {
		t.Errorf("expected params [error], got %v", params)
	}
}

func TestQueryBuilder_Build_FilterGT(t *testing.T) {
	builder := NewSQLQueryBuilder(testWhitelist)
	req := datasource.QueryRequest{
		Fields: []string{"event_id"},
		Filters: []datasource.FilterCondition{
			{Field: "event_id", Operator: datasource.FilterOpGT, Value: 100},
		},
	}

	sql, params, err := builder.Build(req, "events")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(sql, "`event_id` > ?") {
		t.Errorf("expected GT clause, got: %s", sql)
	}
	if len(params) != 1 || params[0] != 100 {
		t.Errorf("expected params [100], got %v", params)
	}
}

func TestQueryBuilder_Build_FilterGTE(t *testing.T) {
	builder := NewSQLQueryBuilder(testWhitelist)
	req := datasource.QueryRequest{
		Fields: []string{"event_id"},
		Filters: []datasource.FilterCondition{
			{Field: "event_id", Operator: datasource.FilterOpGTE, Value: 50},
		},
	}

	sql, params, err := builder.Build(req, "events")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(sql, "`event_id` >= ?") {
		t.Errorf("expected GTE clause, got: %s", sql)
	}
	if len(params) != 1 || params[0] != 50 {
		t.Errorf("expected params [50], got %v", params)
	}
}

func TestQueryBuilder_Build_FilterLT(t *testing.T) {
	builder := NewSQLQueryBuilder(testWhitelist)
	req := datasource.QueryRequest{
		Fields: []string{"event_id"},
		Filters: []datasource.FilterCondition{
			{Field: "event_id", Operator: datasource.FilterOpLT, Value: 200},
		},
	}

	sql, params, err := builder.Build(req, "events")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(sql, "`event_id` < ?") {
		t.Errorf("expected LT clause, got: %s", sql)
	}
	if len(params) != 1 || params[0] != 200 {
		t.Errorf("expected params [200], got %v", params)
	}
}

func TestQueryBuilder_Build_FilterLTE(t *testing.T) {
	builder := NewSQLQueryBuilder(testWhitelist)
	req := datasource.QueryRequest{
		Fields: []string{"event_id"},
		Filters: []datasource.FilterCondition{
			{Field: "event_id", Operator: datasource.FilterOpLTE, Value: 300},
		},
	}

	sql, params, err := builder.Build(req, "events")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(sql, "`event_id` <= ?") {
		t.Errorf("expected LTE clause, got: %s", sql)
	}
	if len(params) != 1 || params[0] != 300 {
		t.Errorf("expected params [300], got %v", params)
	}
}

func TestQueryBuilder_Build_FilterLIKE(t *testing.T) {
	builder := NewSQLQueryBuilder(testWhitelist)
	req := datasource.QueryRequest{
		Fields: []string{"event_id"},
		Filters: []datasource.FilterCondition{
			{Field: "event_type", Operator: datasource.FilterOpLIKE, Value: "%click%"},
		},
	}

	sql, params, err := builder.Build(req, "events")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(sql, "`event_type` LIKE ?") {
		t.Errorf("expected LIKE clause, got: %s", sql)
	}
	if len(params) != 1 || params[0] != "%click%" {
		t.Errorf("expected params [%%click%%], got %v", params)
	}
}

func TestQueryBuilder_Build_FilterIN(t *testing.T) {
	builder := NewSQLQueryBuilder(testWhitelist)
	req := datasource.QueryRequest{
		Fields: []string{"event_id"},
		Filters: []datasource.FilterCondition{
			{Field: "event_type", Operator: datasource.FilterOpIN, Value: []any{"click", "view", "purchase"}},
		},
	}

	sql, params, err := builder.Build(req, "events")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(sql, "`event_type` IN (?, ?, ?)") {
		t.Errorf("expected IN clause with 3 placeholders, got: %s", sql)
	}
	if len(params) != 3 {
		t.Errorf("expected 3 params, got %d", len(params))
	}
	if params[0] != "click" || params[1] != "view" || params[2] != "purchase" {
		t.Errorf("unexpected params: %v", params)
	}
}

func TestQueryBuilder_Build_FilterNOT_IN(t *testing.T) {
	builder := NewSQLQueryBuilder(testWhitelist)
	req := datasource.QueryRequest{
		Fields: []string{"event_id"},
		Filters: []datasource.FilterCondition{
			{Field: "event_type", Operator: datasource.FilterOpNOT_IN, Value: []any{"spam", "bot"}},
		},
	}

	sql, params, err := builder.Build(req, "events")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(sql, "`event_type` NOT IN (?, ?)") {
		t.Errorf("expected NOT IN clause with 2 placeholders, got: %s", sql)
	}
	if len(params) != 2 {
		t.Errorf("expected 2 params, got %d", len(params))
	}
	if params[0] != "spam" || params[1] != "bot" {
		t.Errorf("unexpected params: %v", params)
	}
}

func TestQueryBuilder_Build_FilterIS_NULL(t *testing.T) {
	builder := NewSQLQueryBuilder(testWhitelist)
	req := datasource.QueryRequest{
		Fields: []string{"event_id"},
		Filters: []datasource.FilterCondition{
			{Field: "payload", Operator: datasource.FilterOpIS_NULL},
		},
	}

	sql, params, err := builder.Build(req, "events")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(sql, "`payload` IS NULL") {
		t.Errorf("expected IS NULL clause, got: %s", sql)
	}
	if len(params) != 0 {
		t.Errorf("expected 0 params for IS NULL, got %d", len(params))
	}
}

func TestQueryBuilder_Build_FilterIS_NOT_NULL(t *testing.T) {
	builder := NewSQLQueryBuilder(testWhitelist)
	req := datasource.QueryRequest{
		Fields: []string{"event_id"},
		Filters: []datasource.FilterCondition{
			{Field: "payload", Operator: datasource.FilterOpIS_NOT_NULL},
		},
	}

	sql, params, err := builder.Build(req, "events")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(sql, "`payload` IS NOT NULL") {
		t.Errorf("expected IS NOT NULL clause, got: %s", sql)
	}
	if len(params) != 0 {
		t.Errorf("expected 0 params for IS NOT NULL, got %d", len(params))
	}
}

// --- Build: ORDER BY ---

func TestQueryBuilder_Build_OrderByASC(t *testing.T) {
	builder := NewSQLQueryBuilder(testWhitelist)
	req := datasource.QueryRequest{
		Fields: []string{"event_id", "created_at"},
		OrderBy: []datasource.OrderByClause{
			{Field: "created_at", Direction: datasource.SortASC},
		},
	}

	sql, params, err := builder.Build(req, "events")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(sql, "ORDER BY `created_at` ASC") {
		t.Errorf("expected ORDER BY ASC, got: %s", sql)
	}
	if len(params) != 0 {
		t.Errorf("expected 0 params, got %d", len(params))
	}
}

func TestQueryBuilder_Build_OrderByDESC(t *testing.T) {
	builder := NewSQLQueryBuilder(testWhitelist)
	req := datasource.QueryRequest{
		Fields: []string{"event_id", "created_at"},
		OrderBy: []datasource.OrderByClause{
			{Field: "created_at", Direction: datasource.SortDESC},
		},
	}

	sql, params, err := builder.Build(req, "events")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(sql, "ORDER BY `created_at` DESC") {
		t.Errorf("expected ORDER BY DESC, got: %s", sql)
	}
	if len(params) != 0 {
		t.Errorf("expected 0 params, got %d", len(params))
	}
}

func TestQueryBuilder_Build_OrderByMultiple(t *testing.T) {
	builder := NewSQLQueryBuilder(testWhitelist)
	req := datasource.QueryRequest{
		Fields: []string{"event_id", "created_at", "user_id"},
		OrderBy: []datasource.OrderByClause{
			{Field: "created_at", Direction: datasource.SortDESC},
			{Field: "user_id", Direction: datasource.SortASC},
		},
	}

	sql, _, err := builder.Build(req, "events")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(sql, "ORDER BY `created_at` DESC, `user_id` ASC") {
		t.Errorf("expected multiple ORDER BY, got: %s", sql)
	}
}

// --- Build: LIMIT and OFFSET (inline integers) ---

func TestQueryBuilder_Build_LimitOffset(t *testing.T) {
	builder := NewSQLQueryBuilder(testWhitelist)
	limit := 10
	offset := 20
	req := datasource.QueryRequest{
		Fields: []string{"event_id"},
		Pagination: &datasource.PaginationParams{
			Limit:  &limit,
			Offset: &offset,
		},
	}

	sql, params, err := builder.Build(req, "events")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(sql, "LIMIT 10") {
		t.Errorf("expected LIMIT 10 (inline integer), got: %s", sql)
	}
	if !strings.Contains(sql, "OFFSET 20") {
		t.Errorf("expected OFFSET 20 (inline integer), got: %s", sql)
	}
	// LIMIT/OFFSET are inlined, not parameterized
	if len(params) != 0 {
		t.Errorf("expected 0 params (LIMIT/OFFSET are inlined), got %d", len(params))
	}
}

func TestQueryBuilder_Build_LimitOnly(t *testing.T) {
	builder := NewSQLQueryBuilder(testWhitelist)
	limit := 50
	req := datasource.QueryRequest{
		Fields: []string{"event_id"},
		Pagination: &datasource.PaginationParams{
			Limit: &limit,
		},
	}

	sql, _, err := builder.Build(req, "events")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(sql, "LIMIT 50") {
		t.Errorf("expected LIMIT 50, got: %s", sql)
	}
	if strings.Contains(sql, "OFFSET") {
		t.Errorf("expected no OFFSET, got: %s", sql)
	}
}

func TestQueryBuilder_Build_FirstTakesPrecedenceOverLimit(t *testing.T) {
	builder := NewSQLQueryBuilder(testWhitelist)
	first := 5
	limit := 100
	req := datasource.QueryRequest{
		Fields: []string{"event_id"},
		Pagination: &datasource.PaginationParams{
			First: &first,
			Limit: &limit,
		},
	}

	sql, _, err := builder.Build(req, "events")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// First takes precedence over Limit
	if !strings.Contains(sql, "LIMIT 5") {
		t.Errorf("expected LIMIT 5 (First takes precedence), got: %s", sql)
	}
}

// --- Build: Cursor Pagination (After decoding) ---

func TestQueryBuilder_Build_CursorAfter(t *testing.T) {
	builder := NewSQLQueryBuilder(testWhitelist)
	first := 10
	cursor := "25" // cursor is a numeric string representing offset
	req := datasource.QueryRequest{
		Fields: []string{"event_id"},
		Pagination: &datasource.PaginationParams{
			First: &first,
			After: &cursor,
		},
	}

	sql, params, err := builder.Build(req, "events")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(sql, "LIMIT 10") {
		t.Errorf("expected LIMIT 10, got: %s", sql)
	}
	if !strings.Contains(sql, "OFFSET 25") {
		t.Errorf("expected OFFSET 25 (decoded from cursor '25'), got: %s", sql)
	}
	if len(params) != 0 {
		t.Errorf("expected 0 params (pagination is inlined), got %d", len(params))
	}
}

func TestQueryBuilder_Build_CursorAfterTakesPrecedenceOverOffset(t *testing.T) {
	builder := NewSQLQueryBuilder(testWhitelist)
	first := 10
	cursor := "30"
	offset := 999
	req := datasource.QueryRequest{
		Fields: []string{"event_id"},
		Pagination: &datasource.PaginationParams{
			First:  &first,
			After:  &cursor,
			Offset: &offset,
		},
	}

	sql, _, err := builder.Build(req, "events")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// After takes precedence over Offset
	if !strings.Contains(sql, "OFFSET 30") {
		t.Errorf("expected OFFSET 30 (After takes precedence over Offset), got: %s", sql)
	}
}

func TestQueryBuilder_Build_InvalidCursorIgnored(t *testing.T) {
	builder := NewSQLQueryBuilder(testWhitelist)
	first := 10
	cursor := "invalid_cursor"
	req := datasource.QueryRequest{
		Fields: []string{"event_id"},
		Pagination: &datasource.PaginationParams{
			First: &first,
			After: &cursor,
		},
	}

	sql, _, err := builder.Build(req, "events")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Invalid cursor should be silently ignored (no OFFSET)
	if !strings.Contains(sql, "LIMIT 10") {
		t.Errorf("expected LIMIT 10, got: %s", sql)
	}
	if strings.Contains(sql, "OFFSET") {
		t.Errorf("expected no OFFSET for invalid cursor, got: %s", sql)
	}
}

func TestQueryBuilder_Build_NegativeCursorIgnored(t *testing.T) {
	builder := NewSQLQueryBuilder(testWhitelist)
	first := 10
	cursor := "-5"
	req := datasource.QueryRequest{
		Fields: []string{"event_id"},
		Pagination: &datasource.PaginationParams{
			First: &first,
			After: &cursor,
		},
	}

	sql, _, err := builder.Build(req, "events")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(sql, "OFFSET") {
		t.Errorf("expected no OFFSET for negative cursor, got: %s", sql)
	}
}

// --- Build: Combined (filters + ordering + pagination) ---

func TestQueryBuilder_Build_Combined(t *testing.T) {
	builder := NewSQLQueryBuilder(testWhitelist)
	limit := 20
	offset := 40
	req := datasource.QueryRequest{
		Fields: []string{"event_id", "user_id", "created_at"},
		Filters: []datasource.FilterCondition{
			{Field: "event_type", Operator: datasource.FilterOpEQ, Value: "click"},
			{Field: "user_id", Operator: datasource.FilterOpGT, Value: 0},
		},
		OrderBy: []datasource.OrderByClause{
			{Field: "created_at", Direction: datasource.SortDESC},
		},
		Pagination: &datasource.PaginationParams{
			Limit:  &limit,
			Offset: &offset,
		},
	}

	sql, params, err := builder.Build(req, "events")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify SELECT
	if !strings.Contains(sql, "SELECT `event_id`, `user_id`, `created_at`") {
		t.Errorf("expected SELECT with backtick-quoted fields, got: %s", sql)
	}
	// Verify FROM
	if !strings.Contains(sql, "FROM `events`") {
		t.Errorf("expected FROM `events`, got: %s", sql)
	}
	// Verify WHERE
	if !strings.Contains(sql, "WHERE `event_type` = ? AND `user_id` > ?") {
		t.Errorf("expected WHERE clause, got: %s", sql)
	}
	// Verify ORDER BY
	if !strings.Contains(sql, "ORDER BY `created_at` DESC") {
		t.Errorf("expected ORDER BY, got: %s", sql)
	}
	// Verify LIMIT/OFFSET
	if !strings.Contains(sql, "LIMIT 20 OFFSET 40") {
		t.Errorf("expected LIMIT 20 OFFSET 40, got: %s", sql)
	}
	// Verify params
	if len(params) != 2 {
		t.Errorf("expected 2 params, got %d", len(params))
	}
	if params[0] != "click" || params[1] != 0 {
		t.Errorf("expected params [click, 0], got %v", params)
	}
}

// --- BuildCount ---

func TestQueryBuilder_BuildCount_NoFilters(t *testing.T) {
	builder := NewSQLQueryBuilder(testWhitelist)
	req := datasource.QueryRequest{}

	sql, params, err := builder.BuildCount(req, "events")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "SELECT COUNT(*) FROM `events`"
	if sql != expected {
		t.Errorf("expected SQL:\n  %s\ngot:\n  %s", expected, sql)
	}
	if len(params) != 0 {
		t.Errorf("expected 0 params, got %d", len(params))
	}
}

func TestQueryBuilder_BuildCount_WithFilters(t *testing.T) {
	builder := NewSQLQueryBuilder(testWhitelist)
	req := datasource.QueryRequest{
		Filters: []datasource.FilterCondition{
			{Field: "event_type", Operator: datasource.FilterOpEQ, Value: "purchase"},
			{Field: "user_id", Operator: datasource.FilterOpIS_NOT_NULL},
		},
	}

	sql, params, err := builder.BuildCount(req, "events")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.HasPrefix(sql, "SELECT COUNT(*) FROM `events` WHERE") {
		t.Errorf("expected COUNT with WHERE, got: %s", sql)
	}
	if !strings.Contains(sql, "`event_type` = ?") {
		t.Errorf("expected EQ filter in COUNT, got: %s", sql)
	}
	if !strings.Contains(sql, "`user_id` IS NOT NULL") {
		t.Errorf("expected IS NOT NULL filter in COUNT, got: %s", sql)
	}
	if len(params) != 1 || params[0] != "purchase" {
		t.Errorf("expected params [purchase], got %v", params)
	}
}

// --- Whitelist Enforcement ---

func TestQueryBuilder_Build_NonWhitelistedTable(t *testing.T) {
	builder := NewSQLQueryBuilder(testWhitelist)
	req := datasource.QueryRequest{
		Fields: []string{"event_id"},
	}

	_, _, err := builder.Build(req, "unknown_table")
	if err == nil {
		t.Fatal("expected error for non-whitelisted table, got nil")
	}
	if !strings.Contains(err.Error(), "not in the allowed whitelist") {
		t.Errorf("expected whitelist error, got: %v", err)
	}
}

func TestQueryBuilder_Build_NonWhitelistedColumn(t *testing.T) {
	builder := NewSQLQueryBuilder(testWhitelist)
	req := datasource.QueryRequest{
		Fields: []string{"event_id", "nonexistent_column"},
	}

	_, _, err := builder.Build(req, "events")
	if err == nil {
		t.Fatal("expected error for non-whitelisted column, got nil")
	}
	if !strings.Contains(err.Error(), "not in the allowed columns") {
		t.Errorf("expected column whitelist error, got: %v", err)
	}
}

func TestQueryBuilder_Build_NonWhitelistedFilterColumn(t *testing.T) {
	builder := NewSQLQueryBuilder(testWhitelist)
	req := datasource.QueryRequest{
		Fields: []string{"event_id"},
		Filters: []datasource.FilterCondition{
			{Field: "secret_col", Operator: datasource.FilterOpEQ, Value: "x"},
		},
	}

	_, _, err := builder.Build(req, "events")
	if err == nil {
		t.Fatal("expected error for non-whitelisted filter column, got nil")
	}
	if !strings.Contains(err.Error(), "not in the allowed columns") {
		t.Errorf("expected filter column whitelist error, got: %v", err)
	}
}

func TestQueryBuilder_Build_NonWhitelistedOrderByColumn(t *testing.T) {
	builder := NewSQLQueryBuilder(testWhitelist)
	req := datasource.QueryRequest{
		Fields: []string{"event_id"},
		OrderBy: []datasource.OrderByClause{
			{Field: "hacked_col", Direction: datasource.SortASC},
		},
	}

	_, _, err := builder.Build(req, "events")
	if err == nil {
		t.Fatal("expected error for non-whitelisted order by column, got nil")
	}
	if !strings.Contains(err.Error(), "not in the allowed columns") {
		t.Errorf("expected order by whitelist error, got: %v", err)
	}
}

func TestQueryBuilder_BuildCount_NonWhitelistedTable(t *testing.T) {
	builder := NewSQLQueryBuilder(testWhitelist)
	req := datasource.QueryRequest{}

	_, _, err := builder.BuildCount(req, "bad_table")
	if err == nil {
		t.Fatal("expected error for non-whitelisted table in BuildCount, got nil")
	}
	if !strings.Contains(err.Error(), "not in the allowed whitelist") {
		t.Errorf("expected whitelist error, got: %v", err)
	}
}

// --- IN/NOT_IN expansion ---

func TestQueryBuilder_Build_INSingleValue(t *testing.T) {
	builder := NewSQLQueryBuilder(testWhitelist)
	req := datasource.QueryRequest{
		Fields: []string{"event_id"},
		Filters: []datasource.FilterCondition{
			{Field: "event_type", Operator: datasource.FilterOpIN, Value: []any{"click"}},
		},
	}

	sql, params, err := builder.Build(req, "events")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(sql, "`event_type` IN (?)") {
		t.Errorf("expected IN with 1 placeholder, got: %s", sql)
	}
	if len(params) != 1 || params[0] != "click" {
		t.Errorf("expected params [click], got %v", params)
	}
}

func TestQueryBuilder_Build_INMultipleValues(t *testing.T) {
	builder := NewSQLQueryBuilder(testWhitelist)
	req := datasource.QueryRequest{
		Fields: []string{"event_id"},
		Filters: []datasource.FilterCondition{
			{Field: "user_id", Operator: datasource.FilterOpIN, Value: []any{1, 2, 3, 4, 5}},
		},
	}

	sql, params, err := builder.Build(req, "events")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(sql, "`user_id` IN (?, ?, ?, ?, ?)") {
		t.Errorf("expected IN with 5 placeholders, got: %s", sql)
	}
	if len(params) != 5 {
		t.Errorf("expected 5 params, got %d", len(params))
	}
}

func TestQueryBuilder_Build_NOT_INMultipleValues(t *testing.T) {
	builder := NewSQLQueryBuilder(testWhitelist)
	req := datasource.QueryRequest{
		Fields: []string{"event_id"},
		Filters: []datasource.FilterCondition{
			{Field: "event_type", Operator: datasource.FilterOpNOT_IN, Value: []any{"spam", "bot", "test"}},
		},
	}

	sql, params, err := builder.Build(req, "events")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(sql, "`event_type` NOT IN (?, ?, ?)") {
		t.Errorf("expected NOT IN with 3 placeholders, got: %s", sql)
	}
	if len(params) != 3 {
		t.Errorf("expected 3 params, got %d", len(params))
	}
}

func TestQueryBuilder_Build_INEmptySlice(t *testing.T) {
	builder := NewSQLQueryBuilder(testWhitelist)
	req := datasource.QueryRequest{
		Fields: []string{"event_id"},
		Filters: []datasource.FilterCondition{
			{Field: "event_type", Operator: datasource.FilterOpIN, Value: []any{}},
		},
	}

	_, _, err := builder.Build(req, "events")
	if err == nil {
		t.Fatal("expected error for IN with empty slice, got nil")
	}
	if !strings.Contains(err.Error(), "at least one value") {
		t.Errorf("expected 'at least one value' error, got: %v", err)
	}
}

func TestQueryBuilder_Build_INNonSliceValue(t *testing.T) {
	builder := NewSQLQueryBuilder(testWhitelist)
	req := datasource.QueryRequest{
		Fields: []string{"event_id"},
		Filters: []datasource.FilterCondition{
			{Field: "event_type", Operator: datasource.FilterOpIN, Value: "not_a_slice"},
		},
	}

	_, _, err := builder.Build(req, "events")
	if err == nil {
		t.Fatal("expected error for IN with non-slice value, got nil")
	}
	if !strings.Contains(err.Error(), "requires a slice value") {
		t.Errorf("expected 'requires a slice value' error, got: %v", err)
	}
}

// --- Invalid Identifier ---

func TestQueryBuilder_Build_InvalidIdentifierTable(t *testing.T) {
	// Table with special characters (SQL injection attempt)
	whitelist := map[string]map[string]bool{
		"valid_table": {"col": true},
	}
	builder := NewSQLQueryBuilder(whitelist)
	req := datasource.QueryRequest{Fields: []string{"col"}}

	_, _, err := builder.Build(req, "drop;--")
	if err == nil {
		t.Fatal("expected error for invalid table identifier, got nil")
	}
	if !strings.Contains(err.Error(), "invalid characters") {
		t.Errorf("expected 'invalid characters' error, got: %v", err)
	}
}

// --- NoPagination ---

func TestQueryBuilder_Build_NoPagination(t *testing.T) {
	builder := NewSQLQueryBuilder(testWhitelist)
	req := datasource.QueryRequest{
		Fields:     []string{"event_id"},
		Pagination: nil,
	}

	sql, _, err := builder.Build(req, "events")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(sql, "LIMIT") || strings.Contains(sql, "OFFSET") {
		t.Errorf("expected no LIMIT/OFFSET with nil pagination, got: %s", sql)
	}
}

// --- Multiple filters combined ---

func TestQueryBuilder_Build_MultipleFiltersCombined(t *testing.T) {
	builder := NewSQLQueryBuilder(testWhitelist)
	req := datasource.QueryRequest{
		Fields: []string{"event_id", "user_id"},
		Filters: []datasource.FilterCondition{
			{Field: "event_type", Operator: datasource.FilterOpEQ, Value: "click"},
			{Field: "user_id", Operator: datasource.FilterOpIN, Value: []any{1, 2, 3}},
			{Field: "payload", Operator: datasource.FilterOpIS_NOT_NULL},
		},
	}

	sql, params, err := builder.Build(req, "events")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Filters are joined with AND
	if !strings.Contains(sql, "WHERE `event_type` = ? AND `user_id` IN (?, ?, ?) AND `payload` IS NOT NULL") {
		t.Errorf("expected combined WHERE clause, got: %s", sql)
	}
	// 1 EQ param + 3 IN params = 4 total
	if len(params) != 4 {
		t.Errorf("expected 4 params, got %d", len(params))
	}
}

// --- Using the metrics table (second table in whitelist) ---

func TestQueryBuilder_Build_MetricsTable(t *testing.T) {
	builder := NewSQLQueryBuilder(testWhitelist)
	req := datasource.QueryRequest{
		Fields: []string{"metric_name", "value"},
		Filters: []datasource.FilterCondition{
			{Field: "metric_name", Operator: datasource.FilterOpLIKE, Value: "cpu%"},
		},
		OrderBy: []datasource.OrderByClause{
			{Field: "timestamp", Direction: datasource.SortDESC},
		},
	}

	sql, params, err := builder.Build(req, "metrics")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(sql, "FROM `metrics`") {
		t.Errorf("expected FROM `metrics`, got: %s", sql)
	}
	if !strings.Contains(sql, "`metric_name` LIKE ?") {
		t.Errorf("expected LIKE filter, got: %s", sql)
	}
	if !strings.Contains(sql, "ORDER BY `timestamp` DESC") {
		t.Errorf("expected ORDER BY timestamp DESC, got: %s", sql)
	}
	if len(params) != 1 || params[0] != "cpu%" {
		t.Errorf("expected params [cpu%%], got %v", params)
	}
}
