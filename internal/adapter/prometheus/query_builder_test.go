// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package prometheus

import (
	"testing"

	"github.com/michaelwang123/mountainKing/internal/datasource"
)

func TestBuildInstant_BasicQuery(t *testing.T) {
	b := NewPromQLQueryBuilder()
	req := datasource.QueryRequest{
		Options: map[string]any{
			"query": "up",
		},
	}

	query, params, err := b.BuildInstant(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if query != "up" {
		t.Errorf("expected query %q, got %q", "up", query)
	}
	if params.Get("query") != "up" {
		t.Errorf("expected params query %q, got %q", "up", params.Get("query"))
	}
	if params.Get("time") != "" {
		t.Errorf("expected no time param, got %q", params.Get("time"))
	}
}

func TestBuildInstant_WithTime(t *testing.T) {
	b := NewPromQLQueryBuilder()
	req := datasource.QueryRequest{
		Options: map[string]any{
			"query": "up",
			"time":  "1234567890",
		},
	}

	_, params, err := b.BuildInstant(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if params.Get("time") != "1234567890" {
		t.Errorf("expected time %q, got %q", "1234567890", params.Get("time"))
	}
}

func TestBuildInstant_MissingQuery(t *testing.T) {
	b := NewPromQLQueryBuilder()
	req := datasource.QueryRequest{
		Options: map[string]any{},
	}

	_, _, err := b.BuildInstant(req)
	if err == nil {
		t.Fatal("expected error for missing query option")
	}
}

func TestBuildInstant_NilOptions(t *testing.T) {
	b := NewPromQLQueryBuilder()
	req := datasource.QueryRequest{}

	_, _, err := b.BuildInstant(req)
	if err == nil {
		t.Fatal("expected error for nil options")
	}
}

func TestBuildInstant_WithFilters(t *testing.T) {
	b := NewPromQLQueryBuilder()
	req := datasource.QueryRequest{
		Options: map[string]any{
			"query": "http_requests_total",
		},
		Filters: []datasource.FilterCondition{
			{Field: "job", Operator: datasource.FilterOpEQ, Value: "api"},
			{Field: "status", Operator: datasource.FilterOpNEQ, Value: "500"},
		},
	}

	query, params, err := b.BuildInstant(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := `http_requests_total{job="api", status!="500"}`
	if query != expected {
		t.Errorf("expected query %q, got %q", expected, query)
	}
	if params.Get("query") != expected {
		t.Errorf("expected params query %q, got %q", expected, params.Get("query"))
	}
}

func TestBuildInstant_LIKEFilterBecomesRegex(t *testing.T) {
	b := NewPromQLQueryBuilder()
	req := datasource.QueryRequest{
		Options: map[string]any{
			"query": "up",
		},
		Filters: []datasource.FilterCondition{
			{Field: "instance", Operator: datasource.FilterOpLIKE, Value: ".*:9090"},
		},
	}

	query, _, err := b.BuildInstant(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := `up{instance=~".*:9090"}`
	if query != expected {
		t.Errorf("expected query %q, got %q", expected, query)
	}
}

func TestBuildInstant_UnsupportedFilterSkipped(t *testing.T) {
	b := NewPromQLQueryBuilder()
	req := datasource.QueryRequest{
		Options: map[string]any{
			"query": "up",
		},
		Filters: []datasource.FilterCondition{
			{Field: "value", Operator: datasource.FilterOpGT, Value: "100"},
		},
	}

	query, _, err := b.BuildInstant(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if query != "up" {
		t.Errorf("expected query %q (no matchers appended), got %q", "up", query)
	}
}

func TestBuildRange_BasicQuery(t *testing.T) {
	b := NewPromQLQueryBuilder()
	req := datasource.QueryRequest{
		Options: map[string]any{
			"query":     "rate(http_requests_total[5m])",
			"startTime": "2024-01-01T00:00:00Z",
			"endTime":   "2024-01-01T01:00:00Z",
			"step":      "15s",
		},
	}

	query, params, err := b.BuildRange(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if query != "rate(http_requests_total[5m])" {
		t.Errorf("unexpected query: %q", query)
	}
	if params.Get("start") != "2024-01-01T00:00:00Z" {
		t.Errorf("unexpected start: %q", params.Get("start"))
	}
	if params.Get("end") != "2024-01-01T01:00:00Z" {
		t.Errorf("unexpected end: %q", params.Get("end"))
	}
	if params.Get("step") != "15s" {
		t.Errorf("unexpected step: %q", params.Get("step"))
	}
}

func TestBuildRange_MissingStartTime(t *testing.T) {
	b := NewPromQLQueryBuilder()
	req := datasource.QueryRequest{
		Options: map[string]any{
			"query":   "up",
			"endTime": "2024-01-01T01:00:00Z",
			"step":    "15s",
		},
	}

	_, _, err := b.BuildRange(req)
	if err == nil {
		t.Fatal("expected error for missing startTime")
	}
}

func TestBuildRange_MissingEndTime(t *testing.T) {
	b := NewPromQLQueryBuilder()
	req := datasource.QueryRequest{
		Options: map[string]any{
			"query":     "up",
			"startTime": "2024-01-01T00:00:00Z",
			"step":      "15s",
		},
	}

	_, _, err := b.BuildRange(req)
	if err == nil {
		t.Fatal("expected error for missing endTime")
	}
}

func TestBuildRange_MissingStep(t *testing.T) {
	b := NewPromQLQueryBuilder()
	req := datasource.QueryRequest{
		Options: map[string]any{
			"query":     "up",
			"startTime": "2024-01-01T00:00:00Z",
			"endTime":   "2024-01-01T01:00:00Z",
		},
	}

	_, _, err := b.BuildRange(req)
	if err == nil {
		t.Fatal("expected error for missing step")
	}
}

func TestBuildRange_WithFilters(t *testing.T) {
	b := NewPromQLQueryBuilder()
	req := datasource.QueryRequest{
		Options: map[string]any{
			"query":     "http_requests_total",
			"startTime": "2024-01-01T00:00:00Z",
			"endTime":   "2024-01-01T01:00:00Z",
			"step":      "15s",
		},
		Filters: []datasource.FilterCondition{
			{Field: "job", Operator: datasource.FilterOpEQ, Value: "api"},
		},
	}

	query, _, err := b.BuildRange(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := `http_requests_total{job="api"}`
	if query != expected {
		t.Errorf("expected query %q, got %q", expected, query)
	}
}

func TestFilterOpToLabelMatch(t *testing.T) {
	tests := []struct {
		op       datasource.FilterOperator
		expected LabelMatchType
		ok       bool
	}{
		{datasource.FilterOpEQ, LabelMatchExact, true},
		{datasource.FilterOpNEQ, LabelMatchNotEqual, true},
		{datasource.FilterOpLIKE, LabelMatchRegex, true},
		{datasource.FilterOpGT, "", false},
		{datasource.FilterOpGTE, "", false},
		{datasource.FilterOpLT, "", false},
		{datasource.FilterOpLTE, "", false},
		{datasource.FilterOpIN, "", false},
		{datasource.FilterOpNOT_IN, "", false},
		{datasource.FilterOpIS_NULL, "", false},
		{datasource.FilterOpIS_NOT_NULL, "", false},
	}

	for _, tt := range tests {
		got, ok := filterOpToLabelMatch(tt.op)
		if ok != tt.ok {
			t.Errorf("filterOpToLabelMatch(%d): expected ok=%v, got ok=%v", tt.op, tt.ok, ok)
		}
		if got != tt.expected {
			t.Errorf("filterOpToLabelMatch(%d): expected %q, got %q", tt.op, tt.expected, got)
		}
	}
}

func TestBuildInstant_EmptyQueryString(t *testing.T) {
	b := NewPromQLQueryBuilder()
	req := datasource.QueryRequest{
		Options: map[string]any{
			"query": "",
		},
	}

	_, _, err := b.BuildInstant(req)
	if err == nil {
		t.Fatal("expected error for empty query string")
	}
}

func TestBuildInstant_NonStringQuery(t *testing.T) {
	b := NewPromQLQueryBuilder()
	req := datasource.QueryRequest{
		Options: map[string]any{
			"query": 12345,
		},
	}

	_, _, err := b.BuildInstant(req)
	if err == nil {
		t.Fatal("expected error for non-string query")
	}
}
