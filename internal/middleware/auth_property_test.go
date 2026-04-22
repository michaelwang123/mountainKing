// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package middleware

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/michaelwang123/mountainKing/internal/config"
	apierrors "github.com/michaelwang123/mountainKing/internal/errors"
	"github.com/golang-jwt/jwt/v5"
	"pgregory.net/rapid"
)

// --- helpers for property tests ---

// generateHS256Token creates a signed HS256 JWT token with the given claims.
func generateHS256Token(secret string, claims jwt.MapClaims) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// generateRS256Token creates a signed RS256 JWT token with the given claims.
func generateRS256Token(key *rsa.PrivateKey, claims jwt.MapClaims) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	return token.SignedString(key)
}

// generateES256Token creates a signed ES256 JWT token with the given claims.
func generateES256Token(key *ecdsa.PrivateKey, claims jwt.MapClaims) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	return token.SignedString(key)
}

// writePEM writes a PEM-encoded public key to a temp file and returns the path.
func writePEM(dir string, pub any) (string, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "pub.pem")
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := pem.Encode(f, &pem.Block{Type: "PUBLIC KEY", Bytes: der}); err != nil {
		return "", err
	}
	return path, nil
}

// TestProperty48_MissingAuthCredentialsReturns401 validates that any request
// without valid auth credentials to non-public endpoints returns 401.
//
// Feature: graphql-multi-datasource-api, Property 48: 缺失认证凭据返回 401
// **Validates: Requirements 13.3**
func TestProperty48_MissingAuthCredentialsReturns401(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate random non-public paths.
		pathSuffix := rapid.StringMatching(`[a-z]{3,15}`).Draw(t, "pathSuffix")
		nonPublicPath := "/" + pathSuffix
		// Ensure it's not a public endpoint.
		for isPublicEndpoint(nonPublicPath) {
			pathSuffix = rapid.StringMatching(`[a-z]{3,15}`).Draw(t, "pathSuffix_retry")
			nonPublicPath = "/" + pathSuffix
		}

		// Generate random invalid auth header scenarios.
		scenario := rapid.IntRange(0, 3).Draw(t, "scenario")
		var authHeader string
		switch scenario {
		case 0:
			// No Authorization header at all.
			authHeader = ""
		case 1:
			// Empty Bearer token.
			authHeader = "Bearer "
		case 2:
			// Non-Bearer scheme.
			authHeader = "Basic " + rapid.StringMatching(`[a-zA-Z0-9]{5,20}`).Draw(t, "basicCred")
		case 3:
			// Garbage token that won't parse as JWT.
			authHeader = "Bearer " + rapid.StringMatching(`[a-zA-Z0-9]{5,30}`).Draw(t, "garbageToken")
		}

		// Use a real HS256 authenticator so we test actual auth logic.
		secret := "property-test-secret-key-32bytes!"
		auth, err := NewJWTAuthenticator(config.JWTConfig{Secret: secret}, "HS256")
		if err != nil {
			t.Fatalf("NewJWTAuthenticator: %v", err)
		}

		handler := AuthMiddleware(auth)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodPost, nonPublicPath, nil)
		if authHeader != "" {
			req.Header.Set("Authorization", authHeader)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		// Property: non-public endpoint without valid credentials returns 401.
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 for scenario %d on path %s, got %d", scenario, nonPublicPath, rec.Code)
		}

		// Property: response is valid JSON with error code.
		var resp map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("response is not valid JSON: %v", err)
		}
		errObj, ok := resp["error"].(map[string]any)
		if !ok {
			t.Fatal("response missing 'error' object")
		}
		code, ok := errObj["code"].(string)
		if !ok || code == "" {
			t.Fatal("error missing 'code' field")
		}

		// Property: error code is an AUTH_* code.
		classification := apierrors.Classification(code)
		if classification != "AUTH" {
			t.Fatalf("expected AUTH classification, got %q for code %q", classification, code)
		}

		// Property: Content-Type is application/json.
		ct := rec.Header().Get("Content-Type")
		if ct != "application/json" {
			t.Fatalf("expected Content-Type application/json, got %q", ct)
		}
	})
}

// TestProperty49_InsufficientPermissionsReturns403 validates that authenticated
// requests with insufficient permissions return 403.
//
// Feature: graphql-multi-datasource-api, Property 49: 权限不足返回 403
// **Validates: Requirements 13.4**
func TestProperty49_InsufficientPermissionsReturns403(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a restricted identity with specific datasource/operation permissions.
		allowedDS := rapid.StringMatching(`[a-z]{3,10}`).Draw(t, "allowedDS")
		allowedOp := rapid.SampledFrom([]string{"query", "mutation"}).Draw(t, "allowedOp")

		identity := &AuthIdentity{
			Subject:     "test-user",
			Method:      "apikey",
			Datasources: []string{allowedDS},
			Operations:  []string{allowedOp},
		}

		authz := &DefaultAuthorizer{}

		// Generate a disallowed datasource (different from allowed).
		disallowedDS := rapid.StringMatching(`[a-z]{3,10}`).Draw(t, "disallowedDS")
		for disallowedDS == allowedDS {
			disallowedDS = rapid.StringMatching(`[a-z]{3,10}`).Draw(t, "disallowedDS_retry")
		}

		// Generate a disallowed operation (different from allowed).
		disallowedOp := "mutation"
		if allowedOp == "mutation" {
			disallowedOp = "query"
		}

		// Test datasource restriction.
		testCase := rapid.IntRange(0, 1).Draw(t, "testCase")
		var targetDS, targetOp string
		switch testCase {
		case 0:
			// Disallowed datasource.
			targetDS = disallowedDS
			targetOp = allowedOp
		case 1:
			// Disallowed operation.
			targetDS = allowedDS
			targetOp = disallowedOp
		}

		err := authz.Authorize(identity, targetDS, targetOp)

		// Property: insufficient permissions return an error.
		if err == nil {
			t.Fatalf("expected authorization error for ds=%q op=%q (allowed: ds=%q op=%q)",
				targetDS, targetOp, allowedDS, allowedOp)
		}

		ae, ok := err.(*AuthError)
		if !ok {
			t.Fatalf("expected *AuthError, got %T", err)
		}

		// Property: error status code is 403.
		if ae.StatusCode != 403 {
			t.Fatalf("expected status 403, got %d", ae.StatusCode)
		}

		// Property: error code is AUTH_INSUFFICIENT_PERMISSION.
		if ae.Code != apierrors.ErrAuthInsufficientPermission {
			t.Fatalf("expected code %s, got %s", apierrors.ErrAuthInsufficientPermission, ae.Code)
		}
	})
}

// TestProperty50_PublicEndpointsExemptFromAuth validates that public endpoints
// (/health, /ready, /metrics, /playground) always return 200 regardless of auth state.
//
// Feature: graphql-multi-datasource-api, Property 50: 公共端点豁免认证和限→
// **Validates: Requirements 13.6, 14.6**
func TestProperty50_PublicEndpointsExemptFromAuth(t *testing.T) {
	publicPaths := []string{"/health", "/ready", "/metrics", "/playground"}

	rapid.Check(t, func(t *rapid.T) {
		// Pick a random public endpoint.
		pathIdx := rapid.IntRange(0, len(publicPaths)-1).Draw(t, "pathIdx")
		path := publicPaths[pathIdx]

		// Generate random auth state scenarios.
		authScenario := rapid.IntRange(0, 3).Draw(t, "authScenario")

		var authHeader string
		switch authScenario {
		case 0:
			// No auth header.
			authHeader = ""
		case 1:
			// Invalid Bearer token.
			authHeader = "Bearer invalid-garbage-token"
		case 2:
			// Expired token (simulated via header).
			authHeader = "Bearer expired.token.here"
		case 3:
			// Random garbage auth.
			authHeader = rapid.StringMatching(`[a-zA-Z]{3,10} [a-zA-Z0-9]{5,20}`).Draw(t, "randomAuth")
		}

		// Use an authenticator that always fails.
		failAuth := &mockAuthenticator{
			err: &AuthError{
				Code:       apierrors.ErrAuthMissing,
				StatusCode: 401,
				Message:    "no credentials",
			},
		}

		innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "ok")
		})
		handler := AuthMiddleware(failAuth)(innerHandler)

		req := httptest.NewRequest(http.MethodGet, path, nil)
		if authHeader != "" {
			req.Header.Set("Authorization", authHeader)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		// Property: public endpoints always return 200 regardless of auth state.
		if rec.Code != http.StatusOK {
			t.Fatalf("public endpoint %s with auth scenario %d: expected 200, got %d",
				path, authScenario, rec.Code)
		}

		// Property: the inner handler was actually called (response body is "ok").
		if rec.Body.String() != "ok" {
			t.Fatalf("public endpoint %s: inner handler not called, body=%q", path, rec.Body.String())
		}
	})
}

// TestProperty51_JWTExpiredTokenReturns401WithTokenExpired validates that JWT
// tokens with expired exp claim always return 401 with AUTH_TOKEN_EXPIRED code.
//
// Feature: graphql-multi-datasource-api, Property 51: JWT 过期 Token 返回 401 + token_expired
// **Validates: Requirements 13.8**
func TestProperty51_JWTExpiredTokenReturns401WithTokenExpired(t *testing.T) {
	secret := "property-test-secret-key-32bytes!"
	auth, err := NewJWTAuthenticator(config.JWTConfig{Secret: secret}, "HS256")
	if err != nil {
		t.Fatalf("NewJWTAuthenticator: %v", err)
	}

	rapid.Check(t, func(t *rapid.T) {
		// Generate a random expiration time in the past (1 second to 1 year ago).
		secondsAgo := rapid.IntRange(1, 365*24*3600).Draw(t, "secondsAgo")
		expTime := time.Now().Add(-time.Duration(secondsAgo) * time.Second)

		// Generate random subject.
		subject := rapid.StringMatching(`[a-zA-Z0-9]{3,20}`).Draw(t, "subject")

		claims := jwt.MapClaims{
			"sub": subject,
			"exp": jwt.NewNumericDate(expTime),
			"iat": jwt.NewNumericDate(expTime.Add(-time.Hour)),
		}

		tokenStr, err := generateHS256Token(secret, claims)
		if err != nil {
			t.Fatalf("failed to generate token: %v", err)
		}

		handler := AuthMiddleware(auth)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodPost, "/graphql", nil)
		req.Header.Set("Authorization", "Bearer "+tokenStr)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		// Property: expired token returns 401.
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 for expired token (exp=%v), got %d", expTime, rec.Code)
		}

		// Property: response contains AUTH_TOKEN_EXPIRED code.
		var resp map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("response is not valid JSON: %v", err)
		}
		errObj, ok := resp["error"].(map[string]any)
		if !ok {
			t.Fatal("response missing 'error' object")
		}
		code, ok := errObj["code"].(string)
		if !ok {
			t.Fatal("error missing 'code' field")
		}

		// Property: error code is specifically AUTH_TOKEN_EXPIRED.
		if code != apierrors.ErrAuthTokenExpired {
			t.Fatalf("expected error code %s, got %s", apierrors.ErrAuthTokenExpired, code)
		}

		// Property: classification is AUTH.
		classif, _ := errObj["classification"].(string)
		if classif != "AUTH" {
			t.Fatalf("expected classification AUTH, got %q", classif)
		}
	})
}

// TestProperty81_JWTAsymmetricSignatureVerification validates that RS256/ES256
// signed tokens are correctly verified with the corresponding public key.
//
// Feature: graphql-multi-datasource-api, Property 81: JWT 非对称签名验→
// **Validates: Design - JWT 非对称签名支→*
func TestProperty81_JWTAsymmetricSignatureVerification(t *testing.T) {
	// Pre-generate key pairs (expensive, do once).
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ECDSA key: %v", err)
	}

	// Write public keys to temp files.
	dir := t.TempDir()
	rsaPubPath, err := writePEM(dir, &rsaKey.PublicKey)
	if err != nil {
		t.Fatalf("write RSA pub: %v", err)
	}
	// Write EC public key to a separate file.
	ecPubPath := filepath.Join(dir, "ec_pub.pem")
	ecDer, err := x509.MarshalPKIXPublicKey(&ecKey.PublicKey)
	if err != nil {
		t.Fatalf("marshal EC pub: %v", err)
	}
	ecFile, err := os.Create(ecPubPath)
	if err != nil {
		t.Fatalf("create EC pub file: %v", err)
	}
	if err := pem.Encode(ecFile, &pem.Block{Type: "PUBLIC KEY", Bytes: ecDer}); err != nil {
		ecFile.Close()
		t.Fatalf("encode EC pub: %v", err)
	}
	ecFile.Close()

	// Generate a second RSA key for wrong-key tests.
	rsaKey2, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key2: %v", err)
	}
	// Generate a second EC key for wrong-key tests.
	ecKey2, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ECDSA key2: %v", err)
	}

	rsaAuth, err := NewJWTAuthenticator(config.JWTConfig{PublicKeyFile: rsaPubPath}, "RS256")
	if err != nil {
		t.Fatalf("NewJWTAuthenticator RS256: %v", err)
	}
	ecAuth, err := NewJWTAuthenticator(config.JWTConfig{PublicKeyFile: ecPubPath}, "ES256")
	if err != nil {
		t.Fatalf("NewJWTAuthenticator ES256: %v", err)
	}

	rapid.Check(t, func(t *rapid.T) {
		algo := rapid.SampledFrom([]string{"RS256", "ES256"}).Draw(t, "algo")
		subject := rapid.StringMatching(`[a-zA-Z0-9]{3,20}`).Draw(t, "subject")
		useCorrectKey := rapid.Bool().Draw(t, "useCorrectKey")

		claims := jwt.MapClaims{
			"sub": subject,
			"exp": jwt.NewNumericDate(time.Now().Add(time.Hour)),
			"iat": jwt.NewNumericDate(time.Now()),
		}

		var tokenStr string
		var auth *JWTAuthenticator

		switch algo {
		case "RS256":
			auth = rsaAuth
			if useCorrectKey {
				tokenStr, err = generateRS256Token(rsaKey, claims)
			} else {
				tokenStr, err = generateRS256Token(rsaKey2, claims)
			}
		case "ES256":
			auth = ecAuth
			if useCorrectKey {
				tokenStr, err = generateES256Token(ecKey, claims)
			} else {
				tokenStr, err = generateES256Token(ecKey2, claims)
			}
		}
		if err != nil {
			t.Fatalf("generate %s token: %v", algo, err)
		}

		req := httptest.NewRequest(http.MethodPost, "/graphql", nil)
		req.Header.Set("Authorization", "Bearer "+tokenStr)

		identity, authErr := auth.Authenticate(req)

		if useCorrectKey {
			// Property: valid token signed with correct key authenticates successfully.
			if authErr != nil {
				t.Fatalf("%s correct key: expected success, got error: %v", algo, authErr)
			}
			if identity == nil {
				t.Fatalf("%s correct key: expected identity, got nil", algo)
			}
			// Property: subject is correctly extracted.
			if identity.Subject != subject {
				t.Fatalf("%s correct key: expected subject %q, got %q", algo, subject, identity.Subject)
			}
			// Property: method is jwt.
			if identity.Method != "jwt" {
				t.Fatalf("%s correct key: expected method jwt, got %q", algo, identity.Method)
			}
		} else {
			// Property: token signed with wrong key is rejected.
			if authErr == nil {
				t.Fatalf("%s wrong key: expected error, got success", algo)
			}
			ae, ok := authErr.(*AuthError)
			if !ok {
				t.Fatalf("%s wrong key: expected *AuthError, got %T", algo, authErr)
			}
			// Property: wrong key returns 401 with AUTH_TOKEN_INVALID.
			if ae.StatusCode != 401 {
				t.Fatalf("%s wrong key: expected status 401, got %d", algo, ae.StatusCode)
			}
			if ae.Code != apierrors.ErrAuthTokenInvalid {
				t.Fatalf("%s wrong key: expected code %s, got %s", algo, apierrors.ErrAuthTokenInvalid, ae.Code)
			}
		}
	})
}
