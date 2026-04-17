package middleware

import (
	"context"
	"encoding/json"
	"net/http"

	ctxkeys "github.com/example/graphql-api/internal/context"
	apierrors "github.com/example/graphql-api/internal/errors"
)

// publicEndpoints lists paths that are exempt from authentication.
var publicEndpoints = map[string]bool{
	"/health":     true,
	"/ready":      true,
	"/metrics":    true,
	"/playground": true,
}

// isPublicEndpoint returns true if the request path is exempt from auth.
func isPublicEndpoint(path string) bool {
	return publicEndpoints[path]
}

// AuthMiddleware returns a chi-compatible middleware that authenticates
// requests using the provided Authenticator. Public endpoints (/health,
// /ready, /metrics, /playground) are exempt from authentication.
//
// On successful authentication the AuthIdentity is stored in the request
// context under CtxKeyAuthIdentity. On failure a JSON error response is
// written with the appropriate HTTP status code (401 or 403).
func AuthMiddleware(auth Authenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip auth for public endpoints.
			if isPublicEndpoint(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			identity, err := auth.Authenticate(r)
			if err != nil {
				writeAuthError(w, r, err)
				return
			}

			// Store identity in context and proceed.
			ctx := r.Context()
			ctx = context.WithValue(ctx, ctxkeys.CtxKeyAuthIdentity, identity)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// writeAuthError writes a structured JSON error response for authentication failures.
func writeAuthError(w http.ResponseWriter, r *http.Request, err error) {
	statusCode := http.StatusUnauthorized
	code := apierrors.ErrAuthMissing
	message := "authentication required"

	if ae, ok := err.(*AuthError); ok {
		statusCode = ae.StatusCode
		code = ae.Code
		message = ae.Message
	}

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
