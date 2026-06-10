// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

// Package errors defines unified error codes and structured error types
// for the GraphQL Multi-DataSource API service. Error codes follow the
// {CATEGORY}_{ERROR_NAME} naming convention as specified in Requirements 9.8.
package errors

const (
	// AUTH - Authentication/Authorization errors.

	// ErrAuthTokenExpired indicates the JWT token has expired.
	ErrAuthTokenExpired = "AUTH_TOKEN_EXPIRED"
	// ErrAuthTokenInvalid indicates the JWT signature verification failed.
	ErrAuthTokenInvalid = "AUTH_TOKEN_INVALID"
	// ErrAuthInsufficientPermission indicates the caller lacks permission for the requested resource.
	ErrAuthInsufficientPermission = "AUTH_INSUFFICIENT_PERMISSION"
	// ErrAuthMissing indicates no authentication credentials were provided.
	ErrAuthMissing = "AUTH_MISSING"
	// ErrAuthKeyExpired indicates the API key has passed its expires_at time.
	ErrAuthKeyExpired = "AUTH_KEY_EXPIRED"
	// ErrAuthBruteForceBlocked indicates the IP is blocked due to excessive authentication failures.
	ErrAuthBruteForceBlocked = "AUTH_BRUTE_FORCE_BLOCKED"

	// VALIDATION - Request validation errors.

	// ErrValidationSyntaxError indicates a GraphQL syntax error in the query.
	ErrValidationSyntaxError = "VALIDATION_SYNTAX_ERROR"
	// ErrValidationComplexityExceeded indicates the query complexity exceeds the configured threshold.
	ErrValidationComplexityExceeded = "VALIDATION_COMPLEXITY_EXCEEDED"
	// ErrValidationDepthExceeded indicates the query depth exceeds the configured threshold.
	ErrValidationDepthExceeded = "VALIDATION_DEPTH_EXCEEDED"
	// ErrValidationPayloadTooLarge indicates the request body exceeds the configured max size.
	ErrValidationPayloadTooLarge = "VALIDATION_PAYLOAD_TOO_LARGE"
	// ErrValidationBatchLimitExceeded indicates the batch query count exceeds the configured limit.
	ErrValidationBatchLimitExceeded = "VALIDATION_BATCH_LIMIT_EXCEEDED"
	// ErrValidationInvalidTable indicates the table name is not in the allowed whitelist.
	ErrValidationInvalidTable = "VALIDATION_INVALID_TABLE"
	// ErrValidationInvalidField indicates the field name is not in the allowed whitelist.
	ErrValidationInvalidField = "VALIDATION_INVALID_FIELD"
	// ErrValidationPromQLInjection indicates a PromQL injection attempt was detected.
	ErrValidationPromQLInjection = "VALIDATION_PROMQL_INJECTION"
	// ErrValidationTemplateNotFound indicates the requested template name is not registered.
	ErrValidationTemplateNotFound = "VALIDATION_TEMPLATE_NOT_FOUND"
	// ErrValidationUnsafeSQL indicates the rendered SQL failed security checks.
	ErrValidationUnsafeSQL = "VALIDATION_UNSAFE_SQL"
	// ErrValidationMissingParameter indicates a required template parameter is missing.
	ErrValidationMissingParameter = "VALIDATION_MISSING_PARAMETER"
	// ErrValidationInvalidParameterType indicates a parameter type mismatch.
	ErrValidationInvalidParameterType = "VALIDATION_INVALID_PARAMETER_TYPE"
	// ErrValidationInvalidParameterValue indicates a parameter value constraint violation.
	ErrValidationInvalidParameterValue = "VALIDATION_INVALID_PARAMETER_VALUE"

	// DATASOURCE - Data source errors.

	// ErrDatasourceTimeout indicates a data source query exceeded the configured timeout.
	ErrDatasourceTimeout = "DATASOURCE_TIMEOUT"
	// ErrDatasourceUnavailable indicates the data source is currently unavailable.
	ErrDatasourceUnavailable = "DATASOURCE_UNAVAILABLE"
	// ErrDatasourceCircuitOpen indicates the circuit breaker is open for the data source.
	ErrDatasourceCircuitOpen = "DATASOURCE_CIRCUIT_OPEN"
	// ErrDatasourcePoolExhausted indicates all connections in the pool are occupied.
	ErrDatasourcePoolExhausted = "DATASOURCE_POOL_EXHAUSTED"
	// ErrDatasourceQueryError indicates a query execution error from the data source.
	ErrDatasourceQueryError = "DATASOURCE_QUERY_ERROR"
	// ErrDatasourceMaxDataPoints indicates the query result exceeds the max data points limit.
	ErrDatasourceMaxDataPoints = "DATASOURCE_MAX_DATA_POINTS"
	// ErrDatasourceTemplateQueryError indicates a StarRocks execution error for template SQL.
	ErrDatasourceTemplateQueryError = "DATASOURCE_TEMPLATE_QUERY_ERROR"

	// RATELIMIT - Rate limiting errors.

	// ErrRateLimitExceeded indicates the client has exceeded the request rate limit.
	ErrRateLimitExceeded = "RATELIMIT_EXCEEDED"

	// INTERNAL - Internal errors.

	// ErrInternalUnexpected indicates an unexpected internal server error.
	ErrInternalUnexpected = "INTERNAL_UNEXPECTED"
	// ErrInternalTemplateRenderError indicates a template rendering failure.
	ErrInternalTemplateRenderError = "INTERNAL_TEMPLATE_RENDER_ERROR"

	// ErrServiceUnavailable indicates the server is at capacity and cannot accept new requests.
	ErrServiceUnavailable = "SERVICE_UNAVAILABLE"

	// MUTATION - Mutation operation errors.

	// ErrMutationFeatureDisabled indicates the mutations feature is globally disabled via configuration.
	ErrMutationFeatureDisabled = "MUTATION_FEATURE_DISABLED"
	// ErrMutationOperationNotSupported indicates the requested operation is not in the table's allowed_operations list.
	ErrMutationOperationNotSupported = "MUTATION_OPERATION_NOT_SUPPORTED"
	// ErrMutationLimitExceeded indicates the mutation affected more rows than the configured max_affected_rows threshold.
	ErrMutationLimitExceeded = "MUTATION_LIMIT_EXCEEDED"
	// ErrMutationRateLimitExceeded indicates the client has exceeded the mutation-specific rate limit.
	ErrMutationRateLimitExceeded = "MUTATION_RATELIMIT_EXCEEDED"
)
