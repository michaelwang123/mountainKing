// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package errors

import (
	"fmt"
	"strings"
)

// Classification extracts the category prefix from an error code.
// For example, "AUTH_TOKEN_EXPIRED" returns "AUTH" and
// "DATASOURCE_TIMEOUT" returns "DATASOURCE".
// If the code contains no underscore, the entire code is returned.
func Classification(code string) string {
	if idx := strings.Index(code, "_"); idx >= 0 {
		return code[:idx]
	}
	return code
}

// APIError represents a structured API error with an error code, a
// human-readable message, and the associated HTTP status code.
// It implements the error interface.
type APIError struct {
	// Code is the machine-readable error code (e.g. "AUTH_TOKEN_EXPIRED").
	Code string
	// Message is a human-readable description of the error.
	Message string
	// StatusCode is the HTTP status code associated with this error.
	StatusCode int
}

// Error returns a formatted string containing the error code and message.
func (e *APIError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// NewAPIError creates a new APIError with the given code, message, and HTTP status code.
func NewAPIError(code string, message string, statusCode int) *APIError {
	return &APIError{
		Code:       code,
		Message:    message,
		StatusCode: statusCode,
	}
}

// AuthError creates an authentication/authorization error. The HTTP status
// code is determined by the error code: AUTH_INSUFFICIENT_PERMISSION maps
// to 403 (Forbidden); all other AUTH_* codes map to 401 (Unauthorized).
func AuthError(code string, message string) *APIError {
	statusCode := 401
	if code == ErrAuthInsufficientPermission {
		statusCode = 403
	}
	return NewAPIError(code, message, statusCode)
}

// ValidationError creates a validation error with HTTP status 400 (Bad Request).
func ValidationError(code string, message string) *APIError {
	return NewAPIError(code, message, 400)
}

// DatasourceError creates a data source error with HTTP status 200.
// Data source errors are returned inside the GraphQL errors array
// per the GraphQL specification, so the HTTP status remains 200.
func DatasourceError(code string, message string) *APIError {
	return NewAPIError(code, message, 200)
}

// RateLimitError creates a rate limit error with HTTP status 429 (Too Many Requests).
func RateLimitError(message string) *APIError {
	return NewAPIError(ErrRateLimitExceeded, message, 429)
}

// InternalError creates an internal error with HTTP status 500 (Internal Server Error).
func InternalError(message string) *APIError {
	return NewAPIError(ErrInternalUnexpected, message, 500)
}
