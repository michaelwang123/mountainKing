// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

// Package prometheus implements the Prometheus data source adapter for the
// GraphQL multi-datasource API. It converts GraphQL query parameters into
// PromQL queries and communicates with Prometheus via its HTTP API.
package prometheus

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/michaelwang123/mountainKing/internal/datasource"
)

// LabelMatchType represents the type of label matching in PromQL.
type LabelMatchType string

const (
	// LabelMatchExact represents an exact equality match (=).
	LabelMatchExact LabelMatchType = "="
	// LabelMatchNotEqual represents a not-equal match (!=).
	LabelMatchNotEqual LabelMatchType = "!="
	// LabelMatchRegex represents a regex match (=~).
	LabelMatchRegex LabelMatchType = "=~"
	// LabelMatchNotRegex represents a negated regex match (!~).
	LabelMatchNotRegex LabelMatchType = "!~"
)

// LabelFilter represents a PromQL label filter derived from GraphQL input.
type LabelFilter struct {
	// Name is the label name to filter on.
	Name string
	// Value is the label value to match against.
	Value string
	// MatchType is the type of label matching to apply.
	MatchType LabelMatchType
}

// PromQLQueryBuilder converts GraphQL query parameters to PromQL queries
// and builds URL parameters for the Prometheus HTTP API.
type PromQLQueryBuilder struct{}

// NewPromQLQueryBuilder creates a new PromQLQueryBuilder.
func NewPromQLQueryBuilder() *PromQLQueryBuilder {
	return &PromQLQueryBuilder{}
}

// BuildInstant builds an instant query's PromQL string and URL parameters for
// the Prometheus HTTP API endpoint /api/v1/query.
// It extracts the base PromQL expression from req.Options["query"], appends
// label matchers derived from req.Filters, and optionally sets the evaluation
// timestamp from req.Options["time"].
func (b *PromQLQueryBuilder) BuildInstant(req datasource.QueryRequest) (string, url.Values, error) {
	query, err := b.extractQuery(req)
	if err != nil {
		return "", nil, err
	}

	query = b.appendLabelMatchers(query, req.Filters)

	params := url.Values{}
	params.Set("query", query)

	if t, ok := req.Options["time"]; ok {
		if ts, ok := t.(string); ok && ts != "" {
			params.Set("time", ts)
		}
	}

	return query, params, nil
}

// BuildRange builds a range query's PromQL string and URL parameters for
// the Prometheus HTTP API endpoint /api/v1/query_range.
// It extracts the base PromQL expression from req.Options["query"], appends
// label matchers derived from req.Filters, and sets the required time range
// parameters (start, end, step) from req.Options.
func (b *PromQLQueryBuilder) BuildRange(req datasource.QueryRequest) (string, url.Values, error) {
	query, err := b.extractQuery(req)
	if err != nil {
		return "", nil, err
	}

	query = b.appendLabelMatchers(query, req.Filters)

	startTime, err := b.extractRequiredOption(req, "startTime")
	if err != nil {
		return "", nil, err
	}
	endTime, err := b.extractRequiredOption(req, "endTime")
	if err != nil {
		return "", nil, err
	}
	step, err := b.extractRequiredOption(req, "step")
	if err != nil {
		return "", nil, err
	}

	params := url.Values{}
	params.Set("query", query)
	params.Set("start", startTime)
	params.Set("end", endTime)
	params.Set("step", step)

	return query, params, nil
}

// extractQuery retrieves the base PromQL expression from req.Options["query"].
func (b *PromQLQueryBuilder) extractQuery(req datasource.QueryRequest) (string, error) {
	if req.Options == nil {
		return "", fmt.Errorf("options map is nil: missing required option %q", "query")
	}
	v, ok := req.Options["query"]
	if !ok {
		return "", fmt.Errorf("missing required option %q", "query")
	}
	q, ok := v.(string)
	if !ok || q == "" {
		return "", fmt.Errorf("option %q must be a non-empty string", "query")
	}
	return q, nil
}

// extractRequiredOption retrieves a required string option from req.Options.
func (b *PromQLQueryBuilder) extractRequiredOption(req datasource.QueryRequest, key string) (string, error) {
	if req.Options == nil {
		return "", fmt.Errorf("options map is nil: missing required option %q", key)
	}
	v, ok := req.Options[key]
	if !ok {
		return "", fmt.Errorf("missing required option %q", key)
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return "", fmt.Errorf("option %q must be a non-empty string", key)
	}
	return s, nil
}

// appendLabelMatchers converts FilterCondition entries to PromQL label matchers
// and appends them to the query expression. The conversion rules are:
//   - FilterOpEQ  â†?=  (exact match)
//   - FilterOpNEQ â†?!= (not equal)
//   - FilterOpLIKE â†?=~ (regex match)
//
// Other filter operators are silently skipped as they have no PromQL equivalent.
func (b *PromQLQueryBuilder) appendLabelMatchers(query string, filters []datasource.FilterCondition) string {
	if len(filters) == 0 {
		return query
	}

	var matchers []string
	for _, f := range filters {
		matchType, ok := filterOpToLabelMatch(f.Operator)
		if !ok {
			continue
		}
		value := fmt.Sprintf("%v", f.Value)
		matchers = append(matchers, fmt.Sprintf(`%s%s"%s"`, f.Field, string(matchType), value))
	}

	if len(matchers) == 0 {
		return query
	}

	return query + "{" + strings.Join(matchers, ", ") + "}"
}

// filterOpToLabelMatch converts a datasource.FilterOperator to a PromQL LabelMatchType.
func filterOpToLabelMatch(op datasource.FilterOperator) (LabelMatchType, bool) {
	switch op {
	case datasource.FilterOpEQ:
		return LabelMatchExact, true
	case datasource.FilterOpNEQ:
		return LabelMatchNotEqual, true
	case datasource.FilterOpLIKE:
		return LabelMatchRegex, true
	default:
		return "", false
	}
}
