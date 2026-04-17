package prometheus

import (
	"testing"

	apierrors "github.com/example/graphql-api/internal/errors"
)

func TestValidateLabelValue_Clean(t *testing.T) {
	clean := []string{
		"my-service",
		"production",
		"node_cpu_seconds_total",
		"hello world",
		"123",
		"",
	}
	for _, v := range clean {
		if err := ValidateLabelValue(v); err != nil {
			t.Errorf("ValidateLabelValue(%q) = %v, want nil", v, err)
		}
	}
}

func TestValidateLabelValue_SpecialChars(t *testing.T) {
	cases := []string{
		`value"injected`,
		"value{injected",
		"value}injected",
		"value|injected",
		"value~injected",
	}
	for _, v := range cases {
		err := ValidateLabelValue(v)
		if err == nil {
			t.Errorf("ValidateLabelValue(%q) = nil, want error", v)
			continue
		}
		apiErr, ok := err.(*apierrors.APIError)
		if !ok {
			t.Errorf("ValidateLabelValue(%q) error type = %T, want *apierrors.APIError", v, err)
			continue
		}
		if apiErr.Code != apierrors.ErrValidationPromQLInjection {
			t.Errorf("ValidateLabelValue(%q) code = %q, want %q", v, apiErr.Code, apierrors.ErrValidationPromQLInjection)
		}
	}
}

func TestValidateQueryExpression_Valid(t *testing.T) {
	valid := []string{
		"up",
		`rate(http_requests_total[5m])`,
		`rate(http_requests_total[5m])[1h:5m]`, // depth 2
		`sum(rate(http_requests_total{job="api"}[5m])) by (instance)`, // depth 1 (curly braces don't count)
	}
	for _, q := range valid {
		if err := ValidateQueryExpression(q); err != nil {
			t.Errorf("ValidateQueryExpression(%q) = %v, want nil", q, err)
		}
	}
}

func TestValidateQueryExpression_ExcessiveNesting(t *testing.T) {
	// For true nesting we need [ [ [ ] ] ]
	nestedQuery := `foo[bar[baz[5m]]]`
	err := ValidateQueryExpression(nestedQuery)
	if err == nil {
		t.Fatal("ValidateQueryExpression with depth 3 = nil, want error")
	}
	apiErr, ok := err.(*apierrors.APIError)
	if !ok {
		t.Fatalf("error type = %T, want *apierrors.APIError", err)
	}
	if apiErr.Code != apierrors.ErrValidationPromQLInjection {
		t.Errorf("code = %q, want %q", apiErr.Code, apierrors.ErrValidationPromQLInjection)
	}
}

func TestValidateQueryExpression_HighCostOps(t *testing.T) {
	cases := []string{
		`metric1 / on(instance) group_left(job) metric2`,
		`metric1 * on(instance) group_right(job) metric2`,
		`metric1 / on(instance) GROUP_LEFT(job) metric2`, // case insensitive
	}
	for _, q := range cases {
		err := ValidateQueryExpression(q)
		if err == nil {
			t.Errorf("ValidateQueryExpression(%q) = nil, want error", q)
			continue
		}
		apiErr, ok := err.(*apierrors.APIError)
		if !ok {
			t.Errorf("error type = %T, want *apierrors.APIError", err)
			continue
		}
		if apiErr.Code != apierrors.ErrValidationPromQLInjection {
			t.Errorf("code = %q, want %q", apiErr.Code, apierrors.ErrValidationPromQLInjection)
		}
	}
}

func TestValidateQueryExpression_SequentialBracketsOK(t *testing.T) {
	// Sequential brackets should not trigger depth limit.
	// rate(x[5m]) is depth 1, then [1h:5m] is also depth 1 (sequential).
	query := `rate(http_requests_total[5m])[1h:5m]`
	if err := ValidateQueryExpression(query); err != nil {
		t.Errorf("ValidateQueryExpression(%q) = %v, want nil", query, err)
	}
}
