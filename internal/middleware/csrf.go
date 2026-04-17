package middleware

import (
	"encoding/json"
	"net/http"
	"strings"

	ctxkeys "github.com/example/graphql-api/internal/context"
	apierrors "github.com/example/graphql-api/internal/errors"
)

// CSRFProtection returns a chi-compatible middleware that provides CSRF
// protection for the GraphQL endpoint.
//
// In production mode with allowGetQueries=false (the default), HTTP GET
// requests to the /graphql path are rejected with 403 Forbidden. This
// prevents CSRF attacks via browser-initiated GET requests (e.g. <img>
// or <script> tags).
//
// For POST requests, the middleware verifies that the Content-Type header
// contains "application/json". Browser form submissions use
// application/x-www-form-urlencoded, so requiring JSON provides natural
// CSRF protection.
//
// Parameters:
//   - allowGetQueries: whether GET queries are permitted on /graphql
//   - mode: server mode ("production" or "development")
func CSRFProtection(allowGetQueries bool, mode string) func(http.Handler) http.Handler {
	isProduction := mode == "production"

	// In development mode with GET queries allowed, or when explicitly
	// enabled, the middleware is effectively a Content-Type check only.
	blockGet := isProduction && !allowGetQueries

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Only apply CSRF checks to the /graphql endpoint.
			if !isGraphQLPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			// Block GET requests when configured.
			if blockGet && r.Method == http.MethodGet {
				writeCSRFError(w, r, http.StatusForbidden,
					"VALIDATION_GET_QUERY_DISABLED",
					"GET queries are disabled in production mode")
				return
			}

			// For POST requests, require Content-Type: application/json.
			if r.Method == http.MethodPost {
				ct := r.Header.Get("Content-Type")
				if !strings.Contains(ct, "application/json") {
					writeCSRFError(w, r, http.StatusUnsupportedMediaType,
						"VALIDATION_INVALID_CONTENT_TYPE",
						"Content-Type must be application/json")
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// isGraphQLPath checks whether the request path matches the GraphQL endpoint.
func isGraphQLPath(path string) bool {
	return path == "/graphql" || path == "/graphql/"
}

// writeCSRFError writes a structured JSON error response for CSRF violations.
func writeCSRFError(w http.ResponseWriter, r *http.Request, statusCode int, code, message string) {
	requestID, _ := r.Context().Value(ctxkeys.CtxKeyRequestID).(string)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	resp := map[string]any{
		"error": map[string]any{
			"code":           code,
			"message":        message,
			"classification": apierrors.Classification(code),
		},
	}
	if requestID != "" {
		resp["requestId"] = requestID
	}
	_ = json.NewEncoder(w).Encode(resp)
}
