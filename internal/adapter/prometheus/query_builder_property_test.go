// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package prometheus

import (
	"fmt"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/michaelwang123/mountainKing/internal/datasource"
)

// genPromQLExpression generates a random valid PromQL metric name.
func genPromQLExpression(t *rapid.T) string {
	metrics := []string{
		"up",
		"http_requests_total",
		"node_cpu_seconds_total",
		"process_resident_memory_bytes",
		"go_goroutines",
		"rate(http_requests_total[5m])",
		"sum(rate(http_requests_total[5m]))",
		"histogram_quantile(0.99, rate(http_request_duration_seconds_bucket[5m]))",
	}
	return metrics[rapid.IntRange(0, len(metrics)-1).Draw(t, "metricIdx")]
}

// genLabelName generates a random valid PromQL label name.
func genLabelName(t *rapid.T) string {
	labels := []string{
		"job", "instance", "status", "method", "handler",
		"env", "cluster", "namespace", "pod", "container",
	}
	return labels[rapid.IntRange(0, len(labels)-1).Draw(t, "labelIdx")]
}

// genSafeLabelValue generates a label value that does NOT contain PromQL special chars.
func genSafeLabelValue(t *rapid.T) string {
	return rapid.StringMatching(`[a-zA-Z0-9_\-\./:]+`).Draw(t, "safeLabelValue")
}

// genSupportedFilterOp generates a filter operator that maps to a PromQL label match type.
func genSupportedFilterOp(t *rapid.T) datasource.FilterOperator {
	ops := []datasource.FilterOperator{
		datasource.FilterOpEQ,
		datasource.FilterOpNEQ,
		datasource.FilterOpLIKE,
	}
	return ops[rapid.IntRange(0, len(ops)-1).Draw(t, "filterOpIdx")]
}

// expectedMatchOp returns the expected PromQL operator string for a given FilterOperator.
func expectedMatchOp(op datasource.FilterOperator) string {
	switch op {
	case datasource.FilterOpEQ:
		return "="
	case datasource.FilterOpNEQ:
		return "!="
	case datasource.FilterOpLIKE:
		return "=~"
	default:
		return ""
	}
}

// genTimeString generates a random time string for Prometheus queries.
func genTimeString(t *rapid.T) string {
	formats := []string{
		"2024-01-01T00:00:00Z",
		"2024-06-15T12:30:00Z",
		"1704067200",
		"1718451000.123",
	}
	return formats[rapid.IntRange(0, len(formats)-1).Draw(t, "timeIdx")]
}

// genStepString generates a random step duration string.
func genStepString(t *rapid.T) string {
	steps := []string{"15s", "30s", "1m", "5m", "1h", "60"}
	return steps[rapid.IntRange(0, len(steps)-1).Draw(t, "stepIdx")]
}

// TestProperty19_PrometheusPromQLQueryBuild validates that for any valid Prometheus
// query request with filters and time params:
// - BuildInstant produces a query with correct label matchers
// - BuildRange produces a query with correct label matchers and time params (start, end, step)
// - Label matchers use correct operators (=, !=, =~)
//
// Feature: graphql-multi-datasource-api, Property 19: Prometheus PromQL 查询构建
// **Validates: Requirements 5.2, 5.4, 5.5, 7.3**
func TestProperty19_PrometheusPromQLQueryBuild(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		builder := NewPromQLQueryBuilder()
		baseExpr := genPromQLExpression(t)

		// Generate 0-4 supported filters with safe label values.
		numFilters := rapid.IntRange(0, 4).Draw(t, "numFilters")
		var filters []datasource.FilterCondition
		for i := 0; i < numFilters; i++ {
			filters = append(filters, datasource.FilterCondition{
				Field:    genLabelName(t),
				Operator: genSupportedFilterOp(t),
				Value:    genSafeLabelValue(t),
			})
		}

		// === Sub-property A: BuildInstant ===
		instantReq := datasource.QueryRequest{
			Options: map[string]interface{}{
				"query": baseExpr,
			},
			Filters: filters,
		}

		// Optionally add a time parameter.
		hasTime := rapid.Bool().Draw(t, "hasTime")
		if hasTime {
			instantReq.Options["time"] = genTimeString(t)
		}

		query, params, err := builder.BuildInstant(instantReq)
		if err != nil {
			t.Fatalf("BuildInstant failed: %v", err)
		}

		// The query should start with the base expression.
		if !strings.HasPrefix(query, baseExpr) {
			t.Fatalf("BuildInstant query should start with %q, got %q", baseExpr, query)
		}

		// The params should contain the query.
		if params.Get("query") != query {
			t.Fatalf("BuildInstant params[query] = %q, want %q", params.Get("query"), query)
		}

		// If time was set, params should contain it.
		if hasTime {
			if params.Get("time") != instantReq.Options["time"].(string) {
				t.Fatalf("BuildInstant params[time] = %q, want %q",
					params.Get("time"), instantReq.Options["time"].(string))
			}
		}

		// Verify label matchers in the query.
		if numFilters > 0 {
			// Query should contain { and }.
			if !strings.Contains(query, "{") || !strings.Contains(query, "}") {
				t.Fatalf("BuildInstant query with %d filters should contain label matchers: %q", numFilters, query)
			}

			// Each filter should produce a correct label matcher.
			for _, f := range filters {
				matchOp := expectedMatchOp(f.Operator)
				expectedMatcher := fmt.Sprintf(`%s%s"%s"`, f.Field, matchOp, f.Value)
				if !strings.Contains(query, expectedMatcher) {
					t.Fatalf("BuildInstant query missing matcher %q in: %q", expectedMatcher, query)
				}
			}
		}

		// === Sub-property B: BuildRange ===
		startTime := genTimeString(t)
		endTime := genTimeString(t)
		step := genStepString(t)

		rangeReq := datasource.QueryRequest{
			Options: map[string]interface{}{
				"query":     baseExpr,
				"startTime": startTime,
				"endTime":   endTime,
				"step":      step,
			},
			Filters: filters,
		}

		rangeQuery, rangeParams, err := builder.BuildRange(rangeReq)
		if err != nil {
			t.Fatalf("BuildRange failed: %v", err)
		}

		// The range query should start with the base expression.
		if !strings.HasPrefix(rangeQuery, baseExpr) {
			t.Fatalf("BuildRange query should start with %q, got %q", baseExpr, rangeQuery)
		}

		// The range params should contain query, start, end, step.
		if rangeParams.Get("query") != rangeQuery {
			t.Fatalf("BuildRange params[query] = %q, want %q", rangeParams.Get("query"), rangeQuery)
		}
		if rangeParams.Get("start") != startTime {
			t.Fatalf("BuildRange params[start] = %q, want %q", rangeParams.Get("start"), startTime)
		}
		if rangeParams.Get("end") != endTime {
			t.Fatalf("BuildRange params[end] = %q, want %q", rangeParams.Get("end"), endTime)
		}
		if rangeParams.Get("step") != step {
			t.Fatalf("BuildRange params[step] = %q, want %q", rangeParams.Get("step"), step)
		}

		// Verify label matchers in the range query (same as instant).
		if numFilters > 0 {
			if !strings.Contains(rangeQuery, "{") || !strings.Contains(rangeQuery, "}") {
				t.Fatalf("BuildRange query with %d filters should contain label matchers: %q", numFilters, rangeQuery)
			}
			for _, f := range filters {
				matchOp := expectedMatchOp(f.Operator)
				expectedMatcher := fmt.Sprintf(`%s%s"%s"`, f.Field, matchOp, f.Value)
				if !strings.Contains(rangeQuery, expectedMatcher) {
					t.Fatalf("BuildRange query missing matcher %q in: %q", expectedMatcher, rangeQuery)
				}
			}
		}

		// === Sub-property C: Unsupported filter operators are silently skipped ===
		unsupportedOps := []datasource.FilterOperator{
			datasource.FilterOpGT,
			datasource.FilterOpGTE,
			datasource.FilterOpLT,
			datasource.FilterOpLTE,
			datasource.FilterOpIN,
			datasource.FilterOpNOT_IN,
			datasource.FilterOpIS_NULL,
			datasource.FilterOpIS_NOT_NULL,
		}
		unsupportedOp := unsupportedOps[rapid.IntRange(0, len(unsupportedOps)-1).Draw(t, "unsupportedOpIdx")]
		unsupportedReq := datasource.QueryRequest{
			Options: map[string]interface{}{
				"query": baseExpr,
			},
			Filters: []datasource.FilterCondition{
				{Field: "label", Operator: unsupportedOp, Value: "val"},
			},
		}
		unsupportedQuery, _, err := builder.BuildInstant(unsupportedReq)
		if err != nil {
			t.Fatalf("BuildInstant with unsupported filter should not error: %v", err)
		}
		// The query should be unchanged (no label matchers appended).
		if unsupportedQuery != baseExpr {
			t.Fatalf("BuildInstant with unsupported filter should return base expression %q, got %q",
				baseExpr, unsupportedQuery)
		}
	})
}

// TestProperty20_PromQLInjectionProtection validates that for any label value
// containing PromQL special characters (}{|~"), ValidateLabelValue returns an error.
//
// Feature: graphql-multi-datasource-api, Property 20: PromQL 注入防护
// **Validates: Requirements 5.7**
func TestProperty20_PromQLInjectionProtection(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Each PromQL special character that must be rejected.
		specialChars := []string{"}", "{", "|", "~", `"`}
		charIdx := rapid.IntRange(0, len(specialChars)-1).Draw(t, "specialCharIdx")
		specialChar := specialChars[charIdx]

		// Generate a label value that contains the special character at a random position.
		prefix := rapid.StringMatching(`[a-zA-Z0-9_\-\.]{0,20}`).Draw(t, "prefix")
		suffix := rapid.StringMatching(`[a-zA-Z0-9_\-\.]{0,20}`).Draw(t, "suffix")
		maliciousValue := prefix + specialChar + suffix

		err := ValidateLabelValue(maliciousValue)
		if err == nil {
			t.Fatalf("ValidateLabelValue(%q) should return error for special char %q, got nil",
				maliciousValue, specialChar)
		}

		// Verify the error message mentions PromQL special characters.
		if !strings.Contains(err.Error(), "PromQL special characters") {
			t.Fatalf("ValidateLabelValue(%q) error should mention PromQL special characters, got: %v",
				maliciousValue, err)
		}

		// Sub-property: safe values without special chars should pass.
		safeValue := rapid.StringMatching(`[a-zA-Z0-9_\-\./: ]{0,50}`).Draw(t, "safeValue")
		if err := ValidateLabelValue(safeValue); err != nil {
			t.Fatalf("ValidateLabelValue(%q) should pass for safe value, got: %v", safeValue, err)
		}
	})
}
