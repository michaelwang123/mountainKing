// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package middleware

import (
	apierrors "github.com/michaelwang123/mountainKing/internal/errors"
)

// Authorizer checks whether an authenticated identity has permission
// to access a specific datasource with a given operation type.
type Authorizer interface {
	Authorize(identity *AuthIdentity, datasource string, operation string) error
}

// DefaultAuthorizer implements Authorizer with list-based permission checks.
// Empty Datasources or Operations slices grant full access (typical for JWT tokens).
// Non-empty slices restrict access to the listed values (typical for API Key tokens).
type DefaultAuthorizer struct{}

// Authorize checks that identity is allowed to access datasource with operation.
// Returns nil if permitted, or an AuthError with 403 status if denied.
func (a *DefaultAuthorizer) Authorize(identity *AuthIdentity, datasource string, operation string) error {
	if identity == nil {
		return &AuthError{
			Code:       apierrors.ErrAuthInsufficientPermission,
			StatusCode: 403,
			Message:    "no identity provided",
		}
	}

	if len(identity.Datasources) > 0 && datasource != "" {
		if !contains(identity.Datasources, datasource) {
			return &AuthError{
				Code:       apierrors.ErrAuthInsufficientPermission,
				StatusCode: 403,
				Message:    "access denied for datasource: " + datasource,
			}
		}
	}

	if len(identity.Operations) > 0 && operation != "" {
		if !contains(identity.Operations, operation) {
			return &AuthError{
				Code:       apierrors.ErrAuthInsufficientPermission,
				StatusCode: 403,
				Message:    "operation not permitted: " + operation,
			}
		}
	}

	return nil
}

// contains checks whether s is present in the slice.
func contains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
