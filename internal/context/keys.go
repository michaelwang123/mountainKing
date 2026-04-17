// Package context defines context keys used for propagating request-scoped
// values (such as request IDs, authentication identities, and trace IDs)
// across middleware and resolver boundaries.
package context

// contextKey is an unexported type used for context keys to avoid collisions
// with keys defined in other packages.
type contextKey string

const (
	// CtxKeyRequestID is the context key for the unique request ID (string).
	CtxKeyRequestID contextKey = "requestId"
	// CtxKeyAuthIdentity is the context key for the authenticated identity (*AuthIdentity).
	CtxKeyAuthIdentity contextKey = "authIdentity"
	// CtxKeyTraceID is the context key for the current trace ID (string).
	CtxKeyTraceID contextKey = "traceId"
	// CtxKeyBatchQueryCount is the context key for the number of queries in a batch request (int).
	// The rate limiter uses this to consume the correct number of tokens.
	CtxKeyBatchQueryCount contextKey = "batchQueryCount"
)
