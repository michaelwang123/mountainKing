package prometheus

import (
	"strings"

	apierrors "github.com/example/graphql-api/internal/errors"
)

// promqlSpecialChars contains the set of characters that are considered
// special in PromQL and must be rejected in label values to prevent
// PromQL injection attacks.
const promqlSpecialChars = `}{|~"`

// maxSubqueryDepth is the maximum allowed nesting depth of subqueries
// (counted by [ ] pairs) in a PromQL expression.
const maxSubqueryDepth = 2

// highCostOps lists PromQL operations that are considered high-cost
// and are rejected by ValidateQueryExpression.
var highCostOps = []string{"group_left", "group_right"}

// ValidateLabelValue validates a PromQL label value, rejecting values that
// contain PromQL special characters: } { | ~ "
// These characters could be used for PromQL injection.
func ValidateLabelValue(value string) error {
	if strings.ContainsAny(value, promqlSpecialChars) {
		return apierrors.ValidationError(
			apierrors.ErrValidationPromQLInjection,
			"label value contains PromQL special characters",
		)
	}
	return nil
}

// ValidateQueryExpression validates a PromQL expression for basic safety:
//   - Rejects subquery nesting deeper than 2 levels (counting [ ] pairs)
//   - Rejects high-cost operations: group_left, group_right
func ValidateQueryExpression(query string) error {
	// Check subquery nesting depth by counting nested [ ] pairs.
	maxDepth := 0
	depth := 0
	for _, ch := range query {
		switch ch {
		case '[':
			depth++
			if depth > maxDepth {
				maxDepth = depth
			}
		case ']':
			if depth > 0 {
				depth--
			}
		}
	}
	if maxDepth > maxSubqueryDepth {
		return apierrors.ValidationError(
			apierrors.ErrValidationPromQLInjection,
			"query expression exceeds maximum subquery nesting depth",
		)
	}

	// Check for high-cost operations.
	lower := strings.ToLower(query)
	for _, op := range highCostOps {
		if strings.Contains(lower, op) {
			return apierrors.ValidationError(
				apierrors.ErrValidationPromQLInjection,
				"query expression contains high-cost operation: "+op,
			)
		}
	}

	return nil
}
