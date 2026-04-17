package middleware

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/example/graphql-api/internal/config"
	apierrors "github.com/example/graphql-api/internal/errors"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// --- helpers ---

func makeHS256Token(t *testing.T, secret string, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign HS256 token: %v", err)
	}
	return s
}

func makeRS256Token(t *testing.T, key *rsa.PrivateKey, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	s, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("sign RS256 token: %v", err)
	}
	return s
}

func makeES256Token(t *testing.T, key *ecdsa.PrivateKey, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	s, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("sign ES256 token: %v", err)
	}
	return s
}

func writePEMPublicKey(t *testing.T, dir string, pub interface{}) string {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	path := filepath.Join(dir, "pub.pem")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create pem file: %v", err)
	}
	defer f.Close()
	if err := pem.Encode(f, &pem.Block{Type: "PUBLIC KEY", Bytes: der}); err != nil {
		t.Fatalf("encode pem: %v", err)
	}
	return path
}

func newRequest(t *testing.T, authHeader string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/graphql", nil)
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	return r
}

func assertAuthError(t *testing.T, err error, wantCode string, wantStatus int) {
	t.Helper()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	ae, ok := err.(*AuthError)
	if !ok {
		t.Fatalf("expected *AuthError, got %T: %v", err, err)
	}
	if ae.Code != wantCode {
		t.Errorf("error code = %q, want %q", ae.Code, wantCode)
	}
	if ae.StatusCode != wantStatus {
		t.Errorf("status code = %d, want %d", ae.StatusCode, wantStatus)
	}
}

// --- HS256 tests ---

func TestJWT_HS256_ValidToken(t *testing.T) {
	secret := "test-secret-key-at-least-32-bytes!"
	auth, err := NewJWTAuthenticator(config.JWTConfig{
		Secret: secret,
		Issuer: "test-issuer",
	}, "HS256")
	if err != nil {
		t.Fatalf("NewJWTAuthenticator: %v", err)
	}

	tokenStr := makeHS256Token(t, secret, jwt.MapClaims{
		"sub": "user-123",
		"iss": "test-issuer",
		"exp": jwt.NewNumericDate(time.Now().Add(time.Hour)),
	})

	identity, err := auth.Authenticate(newRequest(t, "Bearer "+tokenStr))
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if identity.Subject != "user-123" {
		t.Errorf("Subject = %q, want %q", identity.Subject, "user-123")
	}
	if identity.Method != "jwt" {
		t.Errorf("Method = %q, want %q", identity.Method, "jwt")
	}
}

func TestJWT_HS256_ExpiredToken(t *testing.T) {
	secret := "test-secret-key-at-least-32-bytes!"
	auth, err := NewJWTAuthenticator(config.JWTConfig{
		Secret: secret,
	}, "HS256")
	if err != nil {
		t.Fatalf("NewJWTAuthenticator: %v", err)
	}

	tokenStr := makeHS256Token(t, secret, jwt.MapClaims{
		"sub": "user-123",
		"exp": jwt.NewNumericDate(time.Now().Add(-time.Hour)),
	})

	_, err = auth.Authenticate(newRequest(t, "Bearer "+tokenStr))
	assertAuthError(t, err, apierrors.ErrAuthTokenExpired, 401)
}

func TestJWT_HS256_InvalidSignature(t *testing.T) {
	auth, err := NewJWTAuthenticator(config.JWTConfig{
		Secret: "correct-secret-key-at-least-32-bytes!",
	}, "HS256")
	if err != nil {
		t.Fatalf("NewJWTAuthenticator: %v", err)
	}

	tokenStr := makeHS256Token(t, "wrong-secret-key-at-least-32-bytes!", jwt.MapClaims{
		"sub": "user-123",
		"exp": jwt.NewNumericDate(time.Now().Add(time.Hour)),
	})

	_, err = auth.Authenticate(newRequest(t, "Bearer "+tokenStr))
	assertAuthError(t, err, apierrors.ErrAuthTokenInvalid, 401)
}

func TestJWT_HS256_WrongIssuer(t *testing.T) {
	secret := "test-secret-key-at-least-32-bytes!"
	auth, err := NewJWTAuthenticator(config.JWTConfig{
		Secret: secret,
		Issuer: "expected-issuer",
	}, "HS256")
	if err != nil {
		t.Fatalf("NewJWTAuthenticator: %v", err)
	}

	tokenStr := makeHS256Token(t, secret, jwt.MapClaims{
		"sub": "user-123",
		"iss": "wrong-issuer",
		"exp": jwt.NewNumericDate(time.Now().Add(time.Hour)),
	})

	_, err = auth.Authenticate(newRequest(t, "Bearer "+tokenStr))
	assertAuthError(t, err, apierrors.ErrAuthTokenInvalid, 401)
}

func TestJWT_MissingAuthHeader(t *testing.T) {
	auth, err := NewJWTAuthenticator(config.JWTConfig{
		Secret: "test-secret",
	}, "HS256")
	if err != nil {
		t.Fatalf("NewJWTAuthenticator: %v", err)
	}

	_, err = auth.Authenticate(newRequest(t, ""))
	assertAuthError(t, err, apierrors.ErrAuthMissing, 401)
}

func TestJWT_NonBearerScheme(t *testing.T) {
	auth, err := NewJWTAuthenticator(config.JWTConfig{
		Secret: "test-secret",
	}, "HS256")
	if err != nil {
		t.Fatalf("NewJWTAuthenticator: %v", err)
	}

	_, err = auth.Authenticate(newRequest(t, "Basic dXNlcjpwYXNz"))
	assertAuthError(t, err, apierrors.ErrAuthTokenInvalid, 401)
}

func TestJWT_EmptyBearerToken(t *testing.T) {
	auth, err := NewJWTAuthenticator(config.JWTConfig{
		Secret: "test-secret",
	}, "HS256")
	if err != nil {
		t.Fatalf("NewJWTAuthenticator: %v", err)
	}

	_, err = auth.Authenticate(newRequest(t, "Bearer "))
	assertAuthError(t, err, apierrors.ErrAuthMissing, 401)
}

// --- RS256 tests ---

func TestJWT_RS256_ValidToken(t *testing.T) {
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}

	dir := t.TempDir()
	pubPath := writePEMPublicKey(t, dir, &privKey.PublicKey)

	auth, err := NewJWTAuthenticator(config.JWTConfig{
		PublicKeyFile: pubPath,
		Issuer:        "test-issuer",
	}, "RS256")
	if err != nil {
		t.Fatalf("NewJWTAuthenticator: %v", err)
	}

	tokenStr := makeRS256Token(t, privKey, jwt.MapClaims{
		"sub": "user-rs256",
		"iss": "test-issuer",
		"exp": jwt.NewNumericDate(time.Now().Add(time.Hour)),
	})

	identity, err := auth.Authenticate(newRequest(t, "Bearer "+tokenStr))
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if identity.Subject != "user-rs256" {
		t.Errorf("Subject = %q, want %q", identity.Subject, "user-rs256")
	}
	if identity.Method != "jwt" {
		t.Errorf("Method = %q, want %q", identity.Method, "jwt")
	}
}

func TestJWT_RS256_ExpiredToken(t *testing.T) {
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}

	dir := t.TempDir()
	pubPath := writePEMPublicKey(t, dir, &privKey.PublicKey)

	auth, err := NewJWTAuthenticator(config.JWTConfig{
		PublicKeyFile: pubPath,
	}, "RS256")
	if err != nil {
		t.Fatalf("NewJWTAuthenticator: %v", err)
	}

	tokenStr := makeRS256Token(t, privKey, jwt.MapClaims{
		"sub": "user-rs256",
		"exp": jwt.NewNumericDate(time.Now().Add(-time.Hour)),
	})

	_, err = auth.Authenticate(newRequest(t, "Bearer "+tokenStr))
	assertAuthError(t, err, apierrors.ErrAuthTokenExpired, 401)
}

func TestJWT_RS256_InvalidSignature(t *testing.T) {
	// Sign with one key, verify with another.
	privKey1, _ := rsa.GenerateKey(rand.Reader, 2048)
	privKey2, _ := rsa.GenerateKey(rand.Reader, 2048)

	dir := t.TempDir()
	pubPath := writePEMPublicKey(t, dir, &privKey2.PublicKey) // verify with key2

	auth, err := NewJWTAuthenticator(config.JWTConfig{
		PublicKeyFile: pubPath,
	}, "RS256")
	if err != nil {
		t.Fatalf("NewJWTAuthenticator: %v", err)
	}

	tokenStr := makeRS256Token(t, privKey1, jwt.MapClaims{ // sign with key1
		"sub": "user-rs256",
		"exp": jwt.NewNumericDate(time.Now().Add(time.Hour)),
	})

	_, err = auth.Authenticate(newRequest(t, "Bearer "+tokenStr))
	assertAuthError(t, err, apierrors.ErrAuthTokenInvalid, 401)
}

// --- ES256 tests ---

func TestJWT_ES256_ValidToken(t *testing.T) {
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ECDSA key: %v", err)
	}

	dir := t.TempDir()
	pubPath := writePEMPublicKey(t, dir, &privKey.PublicKey)

	auth, err := NewJWTAuthenticator(config.JWTConfig{
		PublicKeyFile: pubPath,
		Issuer:        "test-issuer",
	}, "ES256")
	if err != nil {
		t.Fatalf("NewJWTAuthenticator: %v", err)
	}

	tokenStr := makeES256Token(t, privKey, jwt.MapClaims{
		"sub": "user-es256",
		"iss": "test-issuer",
		"exp": jwt.NewNumericDate(time.Now().Add(time.Hour)),
	})

	identity, err := auth.Authenticate(newRequest(t, "Bearer "+tokenStr))
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if identity.Subject != "user-es256" {
		t.Errorf("Subject = %q, want %q", identity.Subject, "user-es256")
	}
}

func TestJWT_ES256_ExpiredToken(t *testing.T) {
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ECDSA key: %v", err)
	}

	dir := t.TempDir()
	pubPath := writePEMPublicKey(t, dir, &privKey.PublicKey)

	auth, err := NewJWTAuthenticator(config.JWTConfig{
		PublicKeyFile: pubPath,
	}, "ES256")
	if err != nil {
		t.Fatalf("NewJWTAuthenticator: %v", err)
	}

	tokenStr := makeES256Token(t, privKey, jwt.MapClaims{
		"sub": "user-es256",
		"exp": jwt.NewNumericDate(time.Now().Add(-time.Hour)),
	})

	_, err = auth.Authenticate(newRequest(t, "Bearer "+tokenStr))
	assertAuthError(t, err, apierrors.ErrAuthTokenExpired, 401)
}

// --- Constructor error tests ---

func TestNewJWTAuthenticator_HS256_EmptySecret(t *testing.T) {
	_, err := NewJWTAuthenticator(config.JWTConfig{}, "HS256")
	if err == nil {
		t.Fatal("expected error for empty secret")
	}
}

func TestNewJWTAuthenticator_UnsupportedAlgorithm(t *testing.T) {
	_, err := NewJWTAuthenticator(config.JWTConfig{}, "PS256")
	if err == nil {
		t.Fatal("expected error for unsupported algorithm")
	}
}

func TestNewJWTAuthenticator_RS256_MissingKeyFile(t *testing.T) {
	_, err := NewJWTAuthenticator(config.JWTConfig{
		PublicKeyFile: "/nonexistent/path.pem",
	}, "RS256")
	if err == nil {
		t.Fatal("expected error for missing key file")
	}
}

func TestNewJWTAuthenticator_RS256_WrongKeyType(t *testing.T) {
	// Write an ECDSA key but try to use it as RS256.
	privKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	dir := t.TempDir()
	pubPath := writePEMPublicKey(t, dir, &privKey.PublicKey)

	_, err := NewJWTAuthenticator(config.JWTConfig{
		PublicKeyFile: pubPath,
	}, "RS256")
	if err == nil {
		t.Fatal("expected error for wrong key type")
	}
}

func TestNewJWTAuthenticator_ES256_WrongKeyType(t *testing.T) {
	// Write an RSA key but try to use it as ES256.
	privKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	dir := t.TempDir()
	pubPath := writePEMPublicKey(t, dir, &privKey.PublicKey)

	_, err := NewJWTAuthenticator(config.JWTConfig{
		PublicKeyFile: pubPath,
	}, "ES256")
	if err == nil {
		t.Fatal("expected error for wrong key type")
	}
}

// --- AuthError tests ---

func TestAuthError_Error(t *testing.T) {
	e := &AuthError{Code: "AUTH_MISSING", StatusCode: 401, Message: "no creds"}
	want := "AUTH_MISSING: no creds"
	if got := e.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

// --- Token without sub claim ---

func TestJWT_HS256_NoSubClaim(t *testing.T) {
	secret := "test-secret-key-at-least-32-bytes!"
	auth, err := NewJWTAuthenticator(config.JWTConfig{
		Secret: secret,
	}, "HS256")
	if err != nil {
		t.Fatalf("NewJWTAuthenticator: %v", err)
	}

	tokenStr := makeHS256Token(t, secret, jwt.MapClaims{
		"exp": jwt.NewNumericDate(time.Now().Add(time.Hour)),
	})

	identity, err := auth.Authenticate(newRequest(t, "Bearer "+tokenStr))
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if identity.Subject != "" {
		t.Errorf("Subject = %q, want empty", identity.Subject)
	}
}

// --- API Key Authenticator tests ---

func hashAPIKey(t *testing.T, key string) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(key), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("bcrypt hash: %v", err)
	}
	return string(hash)
}

func newAPIKeyRequest(t *testing.T, apiKey string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/graphql", nil)
	if apiKey != "" {
		r.Header.Set("X-API-Key", apiKey)
	}
	return r
}

func TestAPIKey_ValidKey(t *testing.T) {
	rawKey := "my-secret-api-key-123"
	hash := hashAPIKey(t, rawKey)

	auth, err := NewAPIKeyAuthenticator(config.APIKeyConfig{
		Keys: []config.APIKeyConfigEntry{
			{
				ID:  "client-a",
				Key: hash,
				Permissions: struct {
					Datasources []string `mapstructure:"datasources"`
					Operations  []string `mapstructure:"operations"`
				}{
					Datasources: []string{"starrocks", "prometheus"},
					Operations:  []string{"query"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("NewAPIKeyAuthenticator: %v", err)
	}

	identity, err := auth.Authenticate(newAPIKeyRequest(t, rawKey))
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if identity.Subject != "client-a" {
		t.Errorf("Subject = %q, want %q", identity.Subject, "client-a")
	}
	if identity.Method != "apikey" {
		t.Errorf("Method = %q, want %q", identity.Method, "apikey")
	}
	if len(identity.Datasources) != 2 || identity.Datasources[0] != "starrocks" {
		t.Errorf("Datasources = %v, want [starrocks prometheus]", identity.Datasources)
	}
	if len(identity.Operations) != 1 || identity.Operations[0] != "query" {
		t.Errorf("Operations = %v, want [query]", identity.Operations)
	}
}

func TestAPIKey_MissingHeader(t *testing.T) {
	auth, err := NewAPIKeyAuthenticator(config.APIKeyConfig{
		Keys: []config.APIKeyConfigEntry{
			{ID: "k1", Key: hashAPIKey(t, "key1")},
		},
	})
	if err != nil {
		t.Fatalf("NewAPIKeyAuthenticator: %v", err)
	}

	_, err = auth.Authenticate(newAPIKeyRequest(t, ""))
	assertAuthError(t, err, apierrors.ErrAuthMissing, 401)
}

func TestAPIKey_InvalidKey(t *testing.T) {
	auth, err := NewAPIKeyAuthenticator(config.APIKeyConfig{
		Keys: []config.APIKeyConfigEntry{
			{ID: "k1", Key: hashAPIKey(t, "correct-key")},
		},
	})
	if err != nil {
		t.Fatalf("NewAPIKeyAuthenticator: %v", err)
	}

	_, err = auth.Authenticate(newAPIKeyRequest(t, "wrong-key"))
	assertAuthError(t, err, apierrors.ErrAuthTokenInvalid, 401)
}

func TestAPIKey_ExpiredKey(t *testing.T) {
	rawKey := "my-expired-key"
	hash := hashAPIKey(t, rawKey)
	expired := time.Now().Add(-time.Hour).Format(time.RFC3339)

	auth, err := NewAPIKeyAuthenticator(config.APIKeyConfig{
		Keys: []config.APIKeyConfigEntry{
			{ID: "k1", Key: hash, ExpiresAt: expired},
		},
	})
	if err != nil {
		t.Fatalf("NewAPIKeyAuthenticator: %v", err)
	}

	_, err = auth.Authenticate(newAPIKeyRequest(t, rawKey))
	assertAuthError(t, err, apierrors.ErrAuthTokenExpired, 401)
}

func TestAPIKey_NotYetExpired(t *testing.T) {
	rawKey := "my-valid-key"
	hash := hashAPIKey(t, rawKey)
	future := time.Now().Add(time.Hour).Format(time.RFC3339)

	auth, err := NewAPIKeyAuthenticator(config.APIKeyConfig{
		Keys: []config.APIKeyConfigEntry{
			{ID: "k1", Key: hash, ExpiresAt: future},
		},
	})
	if err != nil {
		t.Fatalf("NewAPIKeyAuthenticator: %v", err)
	}

	identity, err := auth.Authenticate(newAPIKeyRequest(t, rawKey))
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if identity.Subject != "k1" {
		t.Errorf("Subject = %q, want %q", identity.Subject, "k1")
	}
}

func TestAPIKey_MultipleKeys(t *testing.T) {
	key1 := "first-api-key"
	key2 := "second-api-key"

	auth, err := NewAPIKeyAuthenticator(config.APIKeyConfig{
		Keys: []config.APIKeyConfigEntry{
			{
				ID:  "client-a",
				Key: hashAPIKey(t, key1),
				Permissions: struct {
					Datasources []string `mapstructure:"datasources"`
					Operations  []string `mapstructure:"operations"`
				}{
					Datasources: []string{"starrocks"},
					Operations:  []string{"query"},
				},
			},
			{
				ID:  "client-b",
				Key: hashAPIKey(t, key2),
				Permissions: struct {
					Datasources []string `mapstructure:"datasources"`
					Operations  []string `mapstructure:"operations"`
				}{
					Datasources: []string{"prometheus"},
					Operations:  []string{"query", "mutation"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("NewAPIKeyAuthenticator: %v", err)
	}

	// Authenticate with second key.
	identity, err := auth.Authenticate(newAPIKeyRequest(t, key2))
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if identity.Subject != "client-b" {
		t.Errorf("Subject = %q, want %q", identity.Subject, "client-b")
	}
	if len(identity.Datasources) != 1 || identity.Datasources[0] != "prometheus" {
		t.Errorf("Datasources = %v, want [prometheus]", identity.Datasources)
	}
}

func TestNewAPIKeyAuthenticator_MissingID(t *testing.T) {
	_, err := NewAPIKeyAuthenticator(config.APIKeyConfig{
		Keys: []config.APIKeyConfigEntry{
			{Key: "some-hash"},
		},
	})
	if err == nil {
		t.Fatal("expected error for missing ID")
	}
}

func TestNewAPIKeyAuthenticator_MissingKeyHash(t *testing.T) {
	_, err := NewAPIKeyAuthenticator(config.APIKeyConfig{
		Keys: []config.APIKeyConfigEntry{
			{ID: "k1"},
		},
	})
	if err == nil {
		t.Fatal("expected error for missing key hash")
	}
}

func TestNewAPIKeyAuthenticator_InvalidExpiresAt(t *testing.T) {
	_, err := NewAPIKeyAuthenticator(config.APIKeyConfig{
		Keys: []config.APIKeyConfigEntry{
			{ID: "k1", Key: hashAPIKey(t, "key"), ExpiresAt: "not-a-date"},
		},
	})
	if err == nil {
		t.Fatal("expected error for invalid expires_at")
	}
}

func TestNewAPIKeyAuthenticator_EmptyKeys(t *testing.T) {
	auth, err := NewAPIKeyAuthenticator(config.APIKeyConfig{})
	if err != nil {
		t.Fatalf("NewAPIKeyAuthenticator: %v", err)
	}

	_, err = auth.Authenticate(newAPIKeyRequest(t, "any-key"))
	assertAuthError(t, err, apierrors.ErrAuthTokenInvalid, 401)
}
