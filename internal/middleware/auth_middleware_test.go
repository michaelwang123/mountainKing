// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	ctxkeys "github.com/michaelwang123/mountainKing/internal/context"
	apierrors "github.com/michaelwang123/mountainKing/internal/errors"
)

// --- mock authenticator ---

type mockAuthenticator struct {
	identity *AuthIdentity
	err      error
}

func (m *mockAuthenticator) Authenticate(_ *http.Request) (*AuthIdentity, error) {
	return m.identity, m.err
}

// --- helpers ---

func callMiddleware(t *testing.T, auth Authenticator, path string, ctx context.Context) *httptest.ResponseRecorder {
	t.Helper()
	handler := AuthMiddleware(auth)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Echo back identity subject if present.
		identity, _ := r.Context().Value(ctxkeys.CtxKeyAuthIdentity).(*AuthIdentity)
		if identity != nil {
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "subject=%s", identity.Subject)
		} else {
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "no-identity")
		}
	}))

	r := httptest.NewRequest(http.MethodGet, path, nil)
	if ctx != nil {
		r = r.WithContext(ctx)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func parseErrorResponse(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var resp map[string]any
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("failed to parse JSON response: %v", err)
	}
	return resp
}

// --- public endpoint tests ---

func TestAuthMiddleware_PublicEndpoints_SkipAuth(t *testing.T) {
	publicPaths := []string{"/health", "/ready", "/metrics", "/playground"}

	// Use an authenticator that always fails →public endpoints should still pass.
	failAuth := &mockAuthenticator{
		err: &AuthError{Code: apierrors.ErrAuthMissing, StatusCode: 401, Message: "no creds"},
	}

	for _, path := range publicPaths {
		t.Run(path, func(t *testing.T) {
			w := callMiddleware(t, failAuth, path, nil)
			if w.Code != http.StatusOK {
				t.Errorf("path %s: got status %d, want 200", path, w.Code)
			}
			if w.Body.String() != "no-identity" {
				t.Errorf("path %s: got body %q, want %q", path, w.Body.String(), "no-identity")
			}
		})
	}
}

// --- non-public endpoint: successful auth ---

func TestAuthMiddleware_Success_StoresIdentity(t *testing.T) {
	auth := &mockAuthenticator{
		identity: &AuthIdentity{
			Subject:     "user-42",
			Method:      "jwt",
			Datasources: []string{"starrocks"},
			Operations:  []string{"query"},
		},
	}

	w := callMiddleware(t, auth, "/graphql", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200", w.Code)
	}
	if w.Body.String() != "subject=user-42" {
		t.Errorf("got body %q, want %q", w.Body.String(), "subject=user-42")
	}
}

// --- non-public endpoint: AuthError 401 ---

func TestAuthMiddleware_AuthError_401(t *testing.T) {
	auth := &mockAuthenticator{
		err: &AuthError{
			Code:       apierrors.ErrAuthMissing,
			StatusCode: 401,
			Message:    "missing Authorization header",
		},
	}

	w := callMiddleware(t, auth, "/graphql", nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("got status %d, want 401", w.Code)
	}

	resp := parseErrorResponse(t, w.Body.Bytes())
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatal("missing error object in response")
	}
	if errObj["code"] != apierrors.ErrAuthMissing {
		t.Errorf("error code = %v, want %s", errObj["code"], apierrors.ErrAuthMissing)
	}
	if errObj["classification"] != "AUTH" {
		t.Errorf("classification = %v, want AUTH", errObj["classification"])
	}
	if errObj["message"] != "missing Authorization header" {
		t.Errorf("message = %v, want %q", errObj["message"], "missing Authorization header")
	}
}

// --- non-public endpoint: AuthError 403 ---

func TestAuthMiddleware_AuthError_403(t *testing.T) {
	auth := &mockAuthenticator{
		err: &AuthError{
			Code:       apierrors.ErrAuthInsufficientPermission,
			StatusCode: 403,
			Message:    "access denied",
		},
	}

	w := callMiddleware(t, auth, "/graphql", nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("got status %d, want 403", w.Code)
	}

	resp := parseErrorResponse(t, w.Body.Bytes())
	errObj := resp["error"].(map[string]any)
	if errObj["code"] != apierrors.ErrAuthInsufficientPermission {
		t.Errorf("error code = %v, want %s", errObj["code"], apierrors.ErrAuthInsufficientPermission)
	}
}

// --- non-public endpoint: generic error (non-AuthError) returns 401 ---

func TestAuthMiddleware_GenericError_Returns401(t *testing.T) {
	auth := &mockAuthenticator{
		err: fmt.Errorf("something went wrong"),
	}

	w := callMiddleware(t, auth, "/graphql", nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("got status %d, want 401", w.Code)
	}

	resp := parseErrorResponse(t, w.Body.Bytes())
	errObj := resp["error"].(map[string]any)
	if errObj["code"] != apierrors.ErrAuthMissing {
		t.Errorf("error code = %v, want %s", errObj["code"], apierrors.ErrAuthMissing)
	}
}

// --- requestId propagation ---

func TestAuthMiddleware_IncludesRequestID(t *testing.T) {
	auth := &mockAuthenticator{
		err: &AuthError{
			Code:       apierrors.ErrAuthTokenInvalid,
			StatusCode: 401,
			Message:    "bad token",
		},
	}

	ctx := context.WithValue(context.Background(), ctxkeys.CtxKeyRequestID, "req-abc-123")
	w := callMiddleware(t, auth, "/graphql", ctx)

	resp := parseErrorResponse(t, w.Body.Bytes())
	if resp["requestId"] != "req-abc-123" {
		t.Errorf("requestId = %v, want %q", resp["requestId"], "req-abc-123")
	}
}

func TestAuthMiddleware_NoRequestID_OmitsField(t *testing.T) {
	auth := &mockAuthenticator{
		err: &AuthError{
			Code:       apierrors.ErrAuthMissing,
			StatusCode: 401,
			Message:    "no creds",
		},
	}

	w := callMiddleware(t, auth, "/graphql", nil)

	resp := parseErrorResponse(t, w.Body.Bytes())
	if _, exists := resp["requestId"]; exists {
		t.Error("requestId should not be present when not in context")
	}
}

// --- Content-Type header ---

func TestAuthMiddleware_ErrorResponse_ContentType(t *testing.T) {
	auth := &mockAuthenticator{
		err: &AuthError{
			Code:       apierrors.ErrAuthMissing,
			StatusCode: 401,
			Message:    "no creds",
		},
	}

	w := callMiddleware(t, auth, "/graphql", nil)
	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}
}

// --- non-public path that is not /graphql ---

func TestAuthMiddleware_NonPublicNonGraphQL_RequiresAuth(t *testing.T) {
	auth := &mockAuthenticator{
		err: &AuthError{
			Code:       apierrors.ErrAuthMissing,
			StatusCode: 401,
			Message:    "no creds",
		},
	}

	w := callMiddleware(t, auth, "/some-other-path", nil)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("got status %d, want 401 for non-public path", w.Code)
	}
}

// --- expired token error ---

func TestAuthMiddleware_ExpiredToken(t *testing.T) {
	auth := &mockAuthenticator{
		err: &AuthError{
			Code:       apierrors.ErrAuthTokenExpired,
			StatusCode: 401,
			Message:    "token has expired",
		},
	}

	w := callMiddleware(t, auth, "/graphql", nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("got status %d, want 401", w.Code)
	}

	resp := parseErrorResponse(t, w.Body.Bytes())
	errObj := resp["error"].(map[string]any)
	if errObj["code"] != apierrors.ErrAuthTokenExpired {
		t.Errorf("error code = %v, want %s", errObj["code"], apierrors.ErrAuthTokenExpired)
	}
	if errObj["classification"] != "AUTH" {
		t.Errorf("classification = %v, want AUTH", errObj["classification"])
	}
}

// --- isPublicEndpoint ---

func TestIsPublicEndpoint(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/health", true},
		{"/ready", true},
		{"/metrics", true},
		{"/playground", true},
		{"/graphql", false},
		{"/graphql/", false},
		{"/", false},
		{"/health/extra", false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := isPublicEndpoint(tt.path); got != tt.want {
				t.Errorf("isPublicEndpoint(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}
