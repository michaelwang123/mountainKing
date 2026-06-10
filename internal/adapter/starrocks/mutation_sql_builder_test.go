// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package starrocks

import (
	"reflect"
	"testing"

	"github.com/michaelwang123/mountainKing/internal/datasource"
)

func TestBuildInsert_SingleColumn(t *testing.T) {
	builder := &MutationSQLBuilder{}

	result := builder.BuildInsert("users", []string{"name"}, []any{"alice"})

	expectedSQL := "INSERT INTO `users` (`name`) VALUES (?)"
	if result.SQL != expectedSQL {
		t.Errorf("SQL mismatch\n  got:  %s\n  want: %s", result.SQL, expectedSQL)
	}

	expectedParams := []any{"alice"}
	if !reflect.DeepEqual(result.Params, expectedParams) {
		t.Errorf("Params mismatch\n  got:  %v\n  want: %v", result.Params, expectedParams)
	}
}

func TestBuildInsert_MultipleColumns(t *testing.T) {
	builder := &MutationSQLBuilder{}

	result := builder.BuildInsert("orders", []string{"user_id", "amount", "status"}, []any{42, 99.95, "pending"})

	expectedSQL := "INSERT INTO `orders` (`user_id`, `amount`, `status`) VALUES (?, ?, ?)"
	if result.SQL != expectedSQL {
		t.Errorf("SQL mismatch\n  got:  %s\n  want: %s", result.SQL, expectedSQL)
	}

	expectedParams := []any{42, 99.95, "pending"}
	if !reflect.DeepEqual(result.Params, expectedParams) {
		t.Errorf("Params mismatch\n  got:  %v\n  want: %v", result.Params, expectedParams)
	}
}

func TestBuildUpdate_SetWithEQFilter(t *testing.T) {
	builder := &MutationSQLBuilder{}

	filters := []datasource.FilterCondition{
		{Field: "id", Operator: datasource.FilterOpEQ, Value: 1},
	}

	result := builder.BuildUpdate("users", []string{"name", "email"}, []any{"bob", "bob@example.com"}, filters)

	expectedSQL := "UPDATE `users` SET `name` = ?, `email` = ? WHERE `id` = ?"
	if result.SQL != expectedSQL {
		t.Errorf("SQL mismatch\n  got:  %s\n  want: %s", result.SQL, expectedSQL)
	}

	expectedParams := []any{"bob", "bob@example.com", 1}
	if !reflect.DeepEqual(result.Params, expectedParams) {
		t.Errorf("Params mismatch\n  got:  %v\n  want: %v", result.Params, expectedParams)
	}
}

func TestBuildUpdate_WithINFilter(t *testing.T) {
	builder := &MutationSQLBuilder{}

	filters := []datasource.FilterCondition{
		{Field: "status", Operator: datasource.FilterOpIN, Value: []any{"active", "pending", "review"}},
	}

	result := builder.BuildUpdate("orders", []string{"amount"}, []any{100}, filters)

	expectedSQL := "UPDATE `orders` SET `amount` = ? WHERE `status` IN (?, ?, ?)"
	if result.SQL != expectedSQL {
		t.Errorf("SQL mismatch\n  got:  %s\n  want: %s", result.SQL, expectedSQL)
	}

	expectedParams := []any{100, "active", "pending", "review"}
	if !reflect.DeepEqual(result.Params, expectedParams) {
		t.Errorf("Params mismatch\n  got:  %v\n  want: %v", result.Params, expectedParams)
	}
}

func TestBuildUpdate_WithISNULLFilter(t *testing.T) {
	builder := &MutationSQLBuilder{}

	filters := []datasource.FilterCondition{
		{Field: "deleted_at", Operator: datasource.FilterOpIS_NULL},
	}

	result := builder.BuildUpdate("users", []string{"status"}, []any{"inactive"}, filters)

	expectedSQL := "UPDATE `users` SET `status` = ? WHERE `deleted_at` IS NULL"
	if result.SQL != expectedSQL {
		t.Errorf("SQL mismatch\n  got:  %s\n  want: %s", result.SQL, expectedSQL)
	}

	// IS_NULL produces no params for the WHERE clause.
	expectedParams := []any{"inactive"}
	if !reflect.DeepEqual(result.Params, expectedParams) {
		t.Errorf("Params mismatch\n  got:  %v\n  want: %v", result.Params, expectedParams)
	}
}

func TestBuildDelete_SingleEQFilter(t *testing.T) {
	builder := &MutationSQLBuilder{}

	filters := []datasource.FilterCondition{
		{Field: "id", Operator: datasource.FilterOpEQ, Value: 42},
	}

	result := builder.BuildDelete("events", filters)

	expectedSQL := "DELETE FROM `events` WHERE `id` = ?"
	if result.SQL != expectedSQL {
		t.Errorf("SQL mismatch\n  got:  %s\n  want: %s", result.SQL, expectedSQL)
	}

	expectedParams := []any{42}
	if !reflect.DeepEqual(result.Params, expectedParams) {
		t.Errorf("Params mismatch\n  got:  %v\n  want: %v", result.Params, expectedParams)
	}
}

func TestBuildDelete_MultipleFiltersAND(t *testing.T) {
	builder := &MutationSQLBuilder{}

	filters := []datasource.FilterCondition{
		{Field: "user_id", Operator: datasource.FilterOpEQ, Value: 7},
		{Field: "status", Operator: datasource.FilterOpNEQ, Value: "archived"},
		{Field: "amount", Operator: datasource.FilterOpGT, Value: 50},
	}

	result := builder.BuildDelete("orders", filters)

	expectedSQL := "DELETE FROM `orders` WHERE `user_id` = ? AND `status` != ? AND `amount` > ?"
	if result.SQL != expectedSQL {
		t.Errorf("SQL mismatch\n  got:  %s\n  want: %s", result.SQL, expectedSQL)
	}

	expectedParams := []any{7, "archived", 50}
	if !reflect.DeepEqual(result.Params, expectedParams) {
		t.Errorf("Params mismatch\n  got:  %v\n  want: %v", result.Params, expectedParams)
	}
}

func TestBuildDelete_WithNOTINFilter(t *testing.T) {
	builder := &MutationSQLBuilder{}

	filters := []datasource.FilterCondition{
		{Field: "role", Operator: datasource.FilterOpNOT_IN, Value: []any{"admin", "superadmin"}},
	}

	result := builder.BuildDelete("users", filters)

	expectedSQL := "DELETE FROM `users` WHERE `role` NOT IN (?, ?)"
	if result.SQL != expectedSQL {
		t.Errorf("SQL mismatch\n  got:  %s\n  want: %s", result.SQL, expectedSQL)
	}

	expectedParams := []any{"admin", "superadmin"}
	if !reflect.DeepEqual(result.Params, expectedParams) {
		t.Errorf("Params mismatch\n  got:  %v\n  want: %v", result.Params, expectedParams)
	}
}

func TestBuildDelete_WithISNOTNULLFilter(t *testing.T) {
	builder := &MutationSQLBuilder{}

	filters := []datasource.FilterCondition{
		{Field: "email", Operator: datasource.FilterOpIS_NOT_NULL},
	}

	result := builder.BuildDelete("users", filters)

	expectedSQL := "DELETE FROM `users` WHERE `email` IS NOT NULL"
	if result.SQL != expectedSQL {
		t.Errorf("SQL mismatch\n  got:  %s\n  want: %s", result.SQL, expectedSQL)
	}

	// IS_NOT_NULL produces no params.
	if len(result.Params) != 0 {
		t.Errorf("expected empty params for IS_NOT_NULL, got %v", result.Params)
	}
}

func TestBuildBatchInsert_SingleRow(t *testing.T) {
	builder := &MutationSQLBuilder{}

	rows := [][]any{
		{"alice", "alice@example.com"},
	}

	result := builder.BuildBatchInsert("users", []string{"name", "email"}, rows)

	expectedSQL := "INSERT INTO `users` (`name`, `email`) VALUES (?, ?)"
	if result.SQL != expectedSQL {
		t.Errorf("SQL mismatch\n  got:  %s\n  want: %s", result.SQL, expectedSQL)
	}

	expectedParams := []any{"alice", "alice@example.com"}
	if !reflect.DeepEqual(result.Params, expectedParams) {
		t.Errorf("Params mismatch\n  got:  %v\n  want: %v", result.Params, expectedParams)
	}
}

func TestBuildBatchInsert_MultipleRows(t *testing.T) {
	builder := &MutationSQLBuilder{}

	rows := [][]any{
		{"alice", "alice@example.com", 25},
		{"bob", "bob@example.com", 30},
		{"carol", "carol@example.com", 28},
	}

	result := builder.BuildBatchInsert("users", []string{"name", "email", "age"}, rows)

	expectedSQL := "INSERT INTO `users` (`name`, `email`, `age`) VALUES (?, ?, ?), (?, ?, ?), (?, ?, ?)"
	if result.SQL != expectedSQL {
		t.Errorf("SQL mismatch\n  got:  %s\n  want: %s", result.SQL, expectedSQL)
	}

	expectedParams := []any{
		"alice", "alice@example.com", 25,
		"bob", "bob@example.com", 30,
		"carol", "carol@example.com", 28,
	}
	if !reflect.DeepEqual(result.Params, expectedParams) {
		t.Errorf("Params mismatch\n  got:  %v\n  want: %v", result.Params, expectedParams)
	}
}
