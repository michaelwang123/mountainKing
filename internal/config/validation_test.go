// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package config

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"pgregory.net/rapid"
)

// validBaseConfig returns a minimal valid Config for testing.
// Tests modify specific fields to trigger validation errors.
func validBaseConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Port: 8080,
			Mode: "production",
		},
		RateLimit: RateLimitConfig{
			RequestsPerWindow: 100,
			WindowSize:        60 * time.Second,
		},
		Cache: CacheConfig{
			Backend: "memory",
			Memory: MemoryCacheConfig{
				MaxMemorySize: "256MB",
			},
		},
		Tracing: TracingConfig{
			SamplingRate: 1.0,
		},
	}
}

func TestValidateConfig_ValidMinimal(t *testing.T) {
	cfg := validBaseConfig()
	warnings, err := ValidateConfig(cfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(warnings) > 0 {
		t.Fatalf("expected no warnings, got: %v", warnings)
	}
}

func TestValidateConfig_ServerPortZero(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Server.Port = 0
	_, err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected error for port=0")
	}
	if !strings.Contains(err.Error(), "server.port") {
		t.Fatalf("expected server.port error, got: %v", err)
	}
}

func TestValidateConfig_ServerPortNegative(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Server.Port = -1
	_, err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected error for negative port")
	}
}

func TestValidateConfig_InvalidServerMode(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Server.Mode = "staging"
	_, err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected error for invalid mode")
	}
	if !strings.Contains(err.Error(), "server.mode") {
		t.Fatalf("expected server.mode error, got: %v", err)
	}
}

func TestValidateConfig_CSRFWarning(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Server.Mode = "production"
	cfg.Server.AllowGetQueries = true
	warnings, err := ValidateConfig(cfg)
	if err != nil {
		t.Fatalf("CSRF warning should not cause error, got: %v", err)
	}
	if len(warnings) == 0 {
		t.Fatal("expected CSRF warning")
	}
	if !strings.Contains(warnings[0].Message, "CSRF") {
		t.Fatalf("expected CSRF warning message, got: %s", warnings[0].Message)
	}
}

func TestValidateConfig_NoCSRFWarningInDev(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Server.Mode = "development"
	cfg.Server.AllowGetQueries = true
	warnings, err := ValidateConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(warnings) > 0 {
		t.Fatal("should not warn about CSRF in development mode")
	}
}

func TestValidateConfig_DatasourceEmptyName(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Datasources = []DataSourceConfig{
		{Name: "", Type: "starrocks", Enabled: true, Options: map[string]any{
			"allowed_tables": map[string]any{
				"t": map[string]any{"columns": []any{"id"}},
			},
		}},
	}
	_, err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected error for empty datasource name")
	}
	if !strings.Contains(err.Error(), "name must not be empty") {
		t.Fatalf("expected name error, got: %v", err)
	}
}

func TestValidateConfig_DatasourceEmptyType(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Datasources = []DataSourceConfig{
		{Name: "ds1", Type: "", Enabled: true},
	}
	_, err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected error for empty datasource type")
	}
	if !strings.Contains(err.Error(), "type must not be empty") {
		t.Fatalf("expected type error, got: %v", err)
	}
}

func TestValidateConfig_DatasourceDuplicateNames(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Datasources = []DataSourceConfig{
		{Name: "myds", Type: "prometheus", Enabled: true},
		{Name: "myds", Type: "prometheus", Enabled: true},
	}
	_, err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected error for duplicate datasource names")
	}
	if !strings.Contains(err.Error(), "duplicate datasource name") {
		t.Fatalf("expected duplicate name error, got: %v", err)
	}
}

func TestValidateConfig_DisabledDatasourceSkipped(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Datasources = []DataSourceConfig{
		{Name: "", Type: "", Enabled: false}, // invalid but disabled
	}
	_, err := ValidateConfig(cfg)
	if err != nil {
		t.Fatalf("disabled datasource should be skipped, got: %v", err)
	}
}

func TestValidateConfig_NegativePoolSize(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Datasources = []DataSourceConfig{
		{Name: "ds1", Type: "prometheus", Enabled: true, Options: map[string]any{
			"pool_size": -5,
		}},
	}
	_, err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected error for negative pool_size")
	}
	if !strings.Contains(err.Error(), "pool_size must not be negative") {
		t.Fatalf("expected pool_size error, got: %v", err)
	}
}

func TestValidateConfig_StarRocksMissingWhitelist(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Datasources = []DataSourceConfig{
		{Name: "sr", Type: "starrocks", Enabled: true, Options: map[string]any{}},
	}
	_, err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected error for missing allowed_tables")
	}
	if !strings.Contains(err.Error(), "allowed_tables") {
		t.Fatalf("expected allowed_tables error, got: %v", err)
	}
}

func TestValidateConfig_StarRocksEmptyWhitelist(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Datasources = []DataSourceConfig{
		{Name: "sr", Type: "starrocks", Enabled: true, Options: map[string]any{
			"allowed_tables": map[string]any{},
		}},
	}
	_, err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected error for empty allowed_tables")
	}
}

func TestValidateConfig_StarRocksInvalidColumnName(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Datasources = []DataSourceConfig{
		{Name: "sr", Type: "starrocks", Enabled: true, Options: map[string]any{
			"allowed_tables": map[string]any{
				"orders": map[string]any{
					"columns": []any{"valid_col", "invalid-col!"},
				},
			},
		}},
	}
	_, err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected error for invalid column name")
	}
	if !strings.Contains(err.Error(), "must match [a-zA-Z0-9_]") {
		t.Fatalf("expected column name error, got: %v", err)
	}
}

func TestValidateConfig_StarRocksValidWhitelist(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Datasources = []DataSourceConfig{
		{Name: "sr", Type: "starrocks", Enabled: true, Options: map[string]any{
			"allowed_tables": map[string]any{
				"orders": map[string]any{
					"columns": []any{"order_id", "amount"},
				},
			},
		}},
	}
	_, err := ValidateConfig(cfg)
	if err != nil {
		t.Fatalf("expected no error for valid starrocks config, got: %v", err)
	}
}

func TestValidateConfig_AuthMethodInvalid(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Auth.Method = "oauth"
	_, err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected error for invalid auth method")
	}
	if !strings.Contains(err.Error(), "auth.method") {
		t.Fatalf("expected auth.method error, got: %v", err)
	}
}

func TestValidateConfig_JWTHS256Valid(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Auth.Method = "jwt"
	cfg.Auth.JWT = JWTConfig{
		Algorithm: "HS256",
		Secret:    "this-is-a-secret-that-is-at-least-32-bytes-long!!",
	}
	_, err := ValidateConfig(cfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidateConfig_JWTHS256SecretTooShort(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Auth.Method = "jwt"
	cfg.Auth.JWT = JWTConfig{
		Algorithm: "HS256",
		Secret:    "short",
	}
	_, err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected error for short secret")
	}
	if !strings.Contains(err.Error(), "at least 32 bytes") {
		t.Fatalf("expected secret length error, got: %v", err)
	}
}

func TestValidateConfig_JWTHS256MissingSecret(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Auth.Method = "jwt"
	cfg.Auth.JWT = JWTConfig{
		Algorithm: "HS256",
	}
	_, err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected error for missing secret")
	}
}

func TestValidateConfig_JWTHS256WithPublicKey(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Auth.Method = "jwt"
	cfg.Auth.JWT = JWTConfig{
		Algorithm:     "HS256",
		Secret:        "this-is-a-secret-that-is-at-least-32-bytes-long!!",
		PublicKeyFile: "/some/file.pem",
	}
	_, err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected error for HS256 with public_key_file")
	}
	if !strings.Contains(err.Error(), "public_key_file must not be set") {
		t.Fatalf("expected mutual exclusion error, got: %v", err)
	}
}

func TestValidateConfig_JWTRS256WithSecret(t *testing.T) {
	// Create a temp PEM file
	pemFile := createTempRSAPEM(t)
	cfg := validBaseConfig()
	cfg.Auth.Method = "jwt"
	cfg.Auth.JWT = JWTConfig{
		Algorithm:     "RS256",
		PublicKeyFile: pemFile,
		Secret:        "should-not-be-set",
	}
	_, err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected error for RS256 with secret")
	}
	if !strings.Contains(err.Error(), "secret must not be set") {
		t.Fatalf("expected mutual exclusion error, got: %v", err)
	}
}

func TestValidateConfig_JWTRS256Valid(t *testing.T) {
	pemFile := createTempRSAPEM(t)
	cfg := validBaseConfig()
	cfg.Auth.Method = "jwt"
	cfg.Auth.JWT = JWTConfig{
		Algorithm:     "RS256",
		PublicKeyFile: pemFile,
	}
	_, err := ValidateConfig(cfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidateConfig_JWTES256Valid(t *testing.T) {
	pemFile := createTempECPEM(t)
	cfg := validBaseConfig()
	cfg.Auth.Method = "jwt"
	cfg.Auth.JWT = JWTConfig{
		Algorithm:     "ES256",
		PublicKeyFile: pemFile,
	}
	_, err := ValidateConfig(cfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidateConfig_JWTInvalidAlgorithm(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Auth.Method = "jwt"
	cfg.Auth.JWT = JWTConfig{
		Algorithm: "PS256",
	}
	_, err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected error for invalid algorithm")
	}
	if !strings.Contains(err.Error(), "auth.jwt.algorithm") {
		t.Fatalf("expected algorithm error, got: %v", err)
	}
}

func TestValidateConfig_JWTMissingPublicKeyFile(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Auth.Method = "jwt"
	cfg.Auth.JWT = JWTConfig{
		Algorithm: "RS256",
	}
	_, err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected error for missing public_key_file")
	}
	if !strings.Contains(err.Error(), "public_key_file is required") {
		t.Fatalf("expected public_key_file error, got: %v", err)
	}
}

func TestValidateConfig_JWTPublicKeyFileNotExist(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Auth.Method = "jwt"
	cfg.Auth.JWT = JWTConfig{
		Algorithm:     "RS256",
		PublicKeyFile: "/nonexistent/path.pem",
	}
	_, err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected error for nonexistent PEM file")
	}
}

func TestValidateConfig_JWTInvalidPEMContent(t *testing.T) {
	tmpDir := t.TempDir()
	pemPath := filepath.Join(tmpDir, "bad.pem")
	if err := os.WriteFile(pemPath, []byte("not a pem file"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := validBaseConfig()
	cfg.Auth.Method = "jwt"
	cfg.Auth.JWT = JWTConfig{
		Algorithm:     "RS256",
		PublicKeyFile: pemPath,
	}
	_, err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected error for invalid PEM content")
	}
	if !strings.Contains(err.Error(), "not a valid PEM") {
		t.Fatalf("expected PEM error, got: %v", err)
	}
}

func TestValidateConfig_TrustedProxiesValid(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Auth.TrustedProxies = []string{"10.0.0.0/8", "172.16.0.0/12"}
	_, err := ValidateConfig(cfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidateConfig_TrustedProxiesInvalidCIDR(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Auth.TrustedProxies = []string{"10.0.0.0/8", "not-a-cidr"}
	_, err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected error for invalid CIDR")
	}
	if !strings.Contains(err.Error(), "not valid CIDR") {
		t.Fatalf("expected CIDR error, got: %v", err)
	}
}

func TestValidateConfig_RateLimitZeroRequests(t *testing.T) {
	cfg := validBaseConfig()
	cfg.RateLimit.RequestsPerWindow = 0
	_, err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected error for zero requests_per_window")
	}
}

func TestValidateConfig_RateLimitZeroWindowSize(t *testing.T) {
	cfg := validBaseConfig()
	cfg.RateLimit.WindowSize = 0
	_, err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected error for zero window_size")
	}
}

func TestValidateConfig_CacheBackendInvalid(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Cache.Backend = "memcached"
	_, err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected error for invalid cache backend")
	}
	if !strings.Contains(err.Error(), "cache.backend") {
		t.Fatalf("expected cache.backend error, got: %v", err)
	}
}

func TestValidateConfig_TracingSamplingRateOutOfRange(t *testing.T) {
	tests := []struct {
		name string
		rate float64
	}{
		{"negative", -0.1},
		{"above_one", 1.1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validBaseConfig()
			cfg.Tracing.SamplingRate = tt.rate
			_, err := ValidateConfig(cfg)
			if err == nil {
				t.Fatal("expected error for out-of-range sampling rate")
			}
			if !strings.Contains(err.Error(), "sampling_rate") {
				t.Fatalf("expected sampling_rate error, got: %v", err)
			}
		})
	}
}

func TestValidateConfig_AuthMethodEmpty(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Auth.Method = ""
	_, err := ValidateConfig(cfg)
	if err != nil {
		t.Fatalf("empty auth method should be valid (auth not configured), got: %v", err)
	}
}

// === ParseSizeString tests ===

func TestParseSizeString_Valid(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
	}{
		{"1KB", 1024},
		{"256MB", 256 * 1024 * 1024},
		{"1GB", 1024 * 1024 * 1024},
		{"2GB", 2 * 1024 * 1024 * 1024},
		{"512kb", 512 * 1024},
		{"  128MB  ", 128 * 1024 * 1024},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := ParseSizeString(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tt.expected {
				t.Fatalf("expected %d, got %d", tt.expected, result)
			}
		})
	}
}

func TestParseSizeString_Invalid(t *testing.T) {
	tests := []string{
		"",
		"256",
		"MB",
		"256TB",
		"abc",
		"-1MB",
		"0MB",
	}
	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			_, err := ParseSizeString(input)
			if err == nil {
				t.Fatalf("expected error for input %q", input)
			}
		})
	}
}

func TestValidateConfig_MaxMemorySizeInvalid(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Cache.Memory.MaxMemorySize = "notasize"
	_, err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected error for invalid max_memory_size")
	}
	if !strings.Contains(err.Error(), "max_memory_size") {
		t.Fatalf("expected max_memory_size error, got: %v", err)
	}
}

func TestValidateConfig_MultipleErrors(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{
			Port: -1,
			Mode: "invalid",
		},
		RateLimit: RateLimitConfig{
			RequestsPerWindow: 0,
			WindowSize:        0,
		},
		Cache: CacheConfig{
			Backend: "invalid",
		},
		Tracing: TracingConfig{
			SamplingRate: 2.0,
		},
	}
	_, err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected multiple errors")
	}
	errStr := err.Error()
	// Should contain multiple error messages
	if !strings.Contains(errStr, "server.port") {
		t.Error("missing server.port error")
	}
	if !strings.Contains(errStr, "server.mode") {
		t.Error("missing server.mode error")
	}
	if !strings.Contains(errStr, "rate_limit") {
		t.Error("missing rate_limit error")
	}
}

// === Helper functions ===

func createTempRSAPEM(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pubBytes, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	pemBlock := &pem.Block{Type: "PUBLIC KEY", Bytes: pubBytes}
	tmpDir := t.TempDir()
	pemPath := filepath.Join(tmpDir, "rsa_pub.pem")
	if err := os.WriteFile(pemPath, pem.EncodeToMemory(pemBlock), 0644); err != nil {
		t.Fatal(err)
	}
	return pemPath
}

func createTempECPEM(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pubBytes, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	pemBlock := &pem.Block{Type: "PUBLIC KEY", Bytes: pubBytes}
	tmpDir := t.TempDir()
	pemPath := filepath.Join(tmpDir, "ec_pub.pem")
	if err := os.WriteFile(pemPath, pem.EncodeToMemory(pemBlock), 0644); err != nil {
		t.Fatal(err)
	}
	return pemPath
}

// =============================================================================
// SQL Templates validation tests (validateSQLTemplates)
// =============================================================================

// validSQLTemplatesConfig returns a valid enabled SQLTemplatesConfig for testing.
func validSQLTemplatesConfig() SQLTemplatesConfig {
	return SQLTemplatesConfig{
		Enabled:              true,
		DatasourceName:       "analytics_db",
		BaseDir:              "./templates",
		RenderTimeout:        5 * time.Second,
		MaxRenderedSQLLen:    65536,
		MaxConcurrentQueries: 10,
		Templates: []TemplateConfig{
			{
				Name: "fleet_report",
				File: "fleet/fleet_report.sql.tmpl",
				Parameters: []TemplateParamConfig{
					{Name: "eerid", Type: "string", Required: true},
				},
			},
		},
	}
}

func TestValidateSQLTemplates_DisabledPassesValidation(t *testing.T) {
	cfg := validBaseConfig()
	cfg.SQLTemplates = SQLTemplatesConfig{
		Enabled: false,
		// All other fields invalid/empty — should not matter
	}
	_, err := ValidateConfig(cfg)
	if err != nil {
		t.Fatalf("disabled sql_templates should pass validation, got: %v", err)
	}
}

func TestValidateSQLTemplates_EnabledEmptyDatasourceName(t *testing.T) {
	cfg := validBaseConfig()
	sqlCfg := validSQLTemplatesConfig()
	sqlCfg.DatasourceName = ""
	cfg.SQLTemplates = sqlCfg
	_, err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected error for empty datasource_name")
	}
	if !strings.Contains(err.Error(), "datasource_name") {
		t.Fatalf("expected datasource_name error, got: %v", err)
	}
}

func TestValidateSQLTemplates_EnabledEmptyBaseDir(t *testing.T) {
	cfg := validBaseConfig()
	sqlCfg := validSQLTemplatesConfig()
	sqlCfg.BaseDir = ""
	cfg.SQLTemplates = sqlCfg
	_, err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected error for empty base_dir")
	}
	if !strings.Contains(err.Error(), "base_dir") {
		t.Fatalf("expected base_dir error, got: %v", err)
	}
}

func TestValidateSQLTemplates_TableDriven(t *testing.T) {
	tests := []struct {
		name      string
		modify    func(*SQLTemplatesConfig)
		wantErr   bool
		errSubstr string
	}{
		{
			name: "render_timeout_zero",
			modify: func(c *SQLTemplatesConfig) {
				c.RenderTimeout = 0
			},
			wantErr:   true,
			errSubstr: "render_timeout",
		},
		{
			name: "render_timeout_negative",
			modify: func(c *SQLTemplatesConfig) {
				c.RenderTimeout = -1 * time.Second
			},
			wantErr:   true,
			errSubstr: "render_timeout",
		},
		{
			name: "max_rendered_sql_length_zero",
			modify: func(c *SQLTemplatesConfig) {
				c.MaxRenderedSQLLen = 0
			},
			wantErr:   true,
			errSubstr: "max_rendered_sql_length",
		},
		{
			name: "max_rendered_sql_length_negative",
			modify: func(c *SQLTemplatesConfig) {
				c.MaxRenderedSQLLen = -100
			},
			wantErr:   true,
			errSubstr: "max_rendered_sql_length",
		},
		{
			name: "max_concurrent_queries_zero",
			modify: func(c *SQLTemplatesConfig) {
				c.MaxConcurrentQueries = 0
			},
			wantErr:   true,
			errSubstr: "max_concurrent_queries",
		},
		{
			name: "max_concurrent_queries_negative",
			modify: func(c *SQLTemplatesConfig) {
				c.MaxConcurrentQueries = -5
			},
			wantErr:   true,
			errSubstr: "max_concurrent_queries",
		},
		{
			name: "template_name_invalid_chars",
			modify: func(c *SQLTemplatesConfig) {
				c.Templates = []TemplateConfig{
					{Name: "invalid name!", File: "test.sql.tmpl"},
				}
			},
			wantErr:   true,
			errSubstr: "invalid format",
		},
		{
			name: "template_name_too_long",
			modify: func(c *SQLTemplatesConfig) {
				longName := strings.Repeat("a", 65)
				c.Templates = []TemplateConfig{
					{Name: longName, File: "test.sql.tmpl"},
				}
			},
			wantErr:   true,
			errSubstr: "invalid format",
		},
		{
			name: "template_name_empty",
			modify: func(c *SQLTemplatesConfig) {
				c.Templates = []TemplateConfig{
					{Name: "", File: "test.sql.tmpl"},
				}
			},
			wantErr:   true,
			errSubstr: "invalid format",
		},
		{
			name: "duplicate_template_names",
			modify: func(c *SQLTemplatesConfig) {
				c.Templates = []TemplateConfig{
					{Name: "report", File: "a.sql.tmpl"},
					{Name: "report", File: "b.sql.tmpl"},
				}
			},
			wantErr:   true,
			errSubstr: "duplicate",
		},
		{
			name: "empty_template_file",
			modify: func(c *SQLTemplatesConfig) {
				c.Templates = []TemplateConfig{
					{Name: "report", File: ""},
				}
			},
			wantErr:   true,
			errSubstr: "file must not be empty",
		},
		{
			name: "invalid_parameter_type",
			modify: func(c *SQLTemplatesConfig) {
				c.Templates = []TemplateConfig{
					{
						Name: "report",
						File: "test.sql.tmpl",
						Parameters: []TemplateParamConfig{
							{Name: "p1", Type: "date"},
						},
					},
				}
			},
			wantErr:   true,
			errSubstr: "unsupported",
		},
		{
			name: "invalid_pattern_regex",
			modify: func(c *SQLTemplatesConfig) {
				badPattern := "[invalid"
				c.Templates = []TemplateConfig{
					{
						Name: "report",
						File: "test.sql.tmpl",
						Parameters: []TemplateParamConfig{
							{Name: "p1", Type: "string", Pattern: &badPattern},
						},
					},
				}
			},
			wantErr:   true,
			errSubstr: "invalid regex",
		},
		{
			name: "valid_configuration",
			modify: func(c *SQLTemplatesConfig) {
				validPattern := `^\d{4}-\d{2}-\d{2}$`
				c.Templates = []TemplateConfig{
					{
						Name: "fleet_report",
						File: "fleet/fleet_report.sql.tmpl",
						Parameters: []TemplateParamConfig{
							{Name: "eerid", Type: "string", Required: true},
							{Name: "limit", Type: "int"},
							{Name: "threshold", Type: "float"},
							{Name: "active", Type: "boolean"},
							{Name: "ids", Type: "string[]"},
							{Name: "date", Type: "string", Pattern: &validPattern},
						},
					},
					{
						Name: "driver-score",
						File: "driver/driver_score.sql.tmpl",
					},
				}
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validBaseConfig()
			sqlCfg := validSQLTemplatesConfig()
			tt.modify(&sqlCfg)
			cfg.SQLTemplates = sqlCfg

			_, err := ValidateConfig(cfg)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected validation error, got nil")
				}
				if tt.errSubstr != "" && !strings.Contains(err.Error(), tt.errSubstr) {
					t.Fatalf("expected error containing %q, got: %v", tt.errSubstr, err)
				}
			} else {
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
			}
		})
	}
}

// =============================================================================
// Property 4: 模板名称格式校验（配置层）
// **Validates: Requirements 1.9**
// For any template name, validateSQLTemplates accepts it iff it matches
// ^[a-zA-Z0-9_-]{1,64}$
// =============================================================================

func TestProperty4_TemplateNameFormatValidation_Config(t *testing.T) {
	nameRegex := regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

	t.Run("valid_names_accepted", func(t *testing.T) {
		rapid.Check(t, func(rt *rapid.T) {
			name := rapid.StringMatching(`[a-zA-Z0-9_-]{1,64}`).Draw(rt, "validName")

			errs := validateSQLTemplates(&SQLTemplatesConfig{
				Enabled:              true,
				DatasourceName:       "db",
				BaseDir:              "./templates",
				RenderTimeout:        5 * time.Second,
				MaxRenderedSQLLen:    65536,
				MaxConcurrentQueries: 10,
				Templates: []TemplateConfig{
					{Name: name, File: "test.sql.tmpl"},
				},
			})

			for _, e := range errs {
				if strings.Contains(e, "invalid format") {
					rt.Fatalf("valid name %q rejected: %s", name, e)
				}
			}

			if !nameRegex.MatchString(name) {
				rt.Fatalf("generated name %q should match regex", name)
			}
		})
	})

	t.Run("invalid_names_rejected", func(t *testing.T) {
		rapid.Check(t, func(rt *rapid.T) {
			// Generate names that should fail: either contain invalid chars or are too long
			strategy := rapid.IntRange(0, 2).Draw(rt, "strategy")
			var name string
			switch strategy {
			case 0:
				// Name with invalid characters (spaces, special chars)
				name = rapid.StringMatching(`[a-zA-Z0-9_-]{0,10}[^a-zA-Z0-9_-]+[a-zA-Z0-9_-]{0,10}`).Draw(rt, "invalidCharsName")
			case 1:
				// Name too long (>64 chars)
				name = rapid.StringMatching(`[a-zA-Z0-9_-]{65,100}`).Draw(rt, "tooLongName")
			case 2:
				// Empty name
				name = ""
			}

			errs := validateSQLTemplates(&SQLTemplatesConfig{
				Enabled:              true,
				DatasourceName:       "db",
				BaseDir:              "./templates",
				RenderTimeout:        5 * time.Second,
				MaxRenderedSQLLen:    65536,
				MaxConcurrentQueries: 10,
				Templates: []TemplateConfig{
					{Name: name, File: "test.sql.tmpl"},
				},
			})

			hasFormatErr := false
			for _, e := range errs {
				if strings.Contains(e, "invalid format") {
					hasFormatErr = true
					break
				}
			}

			if nameRegex.MatchString(name) {
				// If the name actually matches the regex, it should be accepted
				if hasFormatErr {
					rt.Fatalf("name %q matches regex but was rejected", name)
				}
			} else {
				if !hasFormatErr {
					rt.Fatalf("name %q does not match regex but was accepted", name)
				}
			}
		})
	})
}

// =============================================================================
// Property 5: 文件大小限制（配置层）
// **Validates: Requirements 1.10**
// Config-level validation: positive value checks for render_timeout,
// max_rendered_sql_length, and max_concurrent_queries.
// =============================================================================

func TestProperty5_ConfigPositiveValueValidation(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate non-positive values for the three fields
		field := rapid.IntRange(0, 2).Draw(rt, "field")
		nonPositive := rapid.IntRange(-1000, 0).Draw(rt, "nonPositive")

		sqlCfg := SQLTemplatesConfig{
			Enabled:              true,
			DatasourceName:       "db",
			BaseDir:              "./templates",
			RenderTimeout:        5 * time.Second,
			MaxRenderedSQLLen:    65536,
			MaxConcurrentQueries: 10,
			Templates:            []TemplateConfig{},
		}

		var expectedSubstr string
		switch field {
		case 0:
			sqlCfg.RenderTimeout = time.Duration(nonPositive) * time.Second
			expectedSubstr = "render_timeout"
		case 1:
			sqlCfg.MaxRenderedSQLLen = nonPositive
			expectedSubstr = "max_rendered_sql_length"
		case 2:
			sqlCfg.MaxConcurrentQueries = nonPositive
			expectedSubstr = "max_concurrent_queries"
		}

		errs := validateSQLTemplates(&sqlCfg)

		hasExpectedErr := false
		for _, e := range errs {
			if strings.Contains(e, expectedSubstr) {
				hasExpectedErr = true
				break
			}
		}
		if !hasExpectedErr {
			rt.Fatalf("expected error containing %q for non-positive value %d (field=%d), got: %v",
				expectedSubstr, nonPositive, field, errs)
		}
	})
}

// =============================================================================
// Property 48: 正则预编译
// **Validates: Requirements 7.9**
// For any pattern string, validateSQLTemplates catches invalid regex patterns
// and accepts valid ones.
// =============================================================================

func TestProperty48_PatternRegexValidation(t *testing.T) {
	t.Run("valid_patterns_accepted", func(t *testing.T) {
		rapid.Check(t, func(rt *rapid.T) {
			// Generate known-valid regex patterns
			pattern := rapid.SampledFrom([]string{
				`^\d{4}-\d{2}-\d{2}$`,
				`^[a-zA-Z]+$`,
				`^\w+@\w+\.\w+$`,
				`^[0-9]{1,10}$`,
				`^(daily|weekly|monthly)$`,
				`.*`,
				`^.{1,100}$`,
				`^[A-Z]{2,3}\d{3,6}$`,
			}).Draw(rt, "validPattern")

			errs := validateSQLTemplates(&SQLTemplatesConfig{
				Enabled:              true,
				DatasourceName:       "db",
				BaseDir:              "./templates",
				RenderTimeout:        5 * time.Second,
				MaxRenderedSQLLen:    65536,
				MaxConcurrentQueries: 10,
				Templates: []TemplateConfig{
					{
						Name: "test_tmpl",
						File: "test.sql.tmpl",
						Parameters: []TemplateParamConfig{
							{Name: "p1", Type: "string", Pattern: &pattern},
						},
					},
				},
			})

			for _, e := range errs {
				if strings.Contains(e, "invalid regex") {
					rt.Fatalf("valid pattern %q was rejected: %s", pattern, e)
				}
			}
		})
	})

	t.Run("invalid_patterns_rejected", func(t *testing.T) {
		rapid.Check(t, func(rt *rapid.T) {
			// Generate known-invalid regex patterns
			pattern := rapid.SampledFrom([]string{
				`[invalid`,
				`(unclosed`,
				`*bad`,
				`+bad`,
				`?bad`,
				`[z-a]`,
				`(?P<name>unclosed`,
				`\p{Invalid}`,
			}).Draw(rt, "invalidPattern")

			// Verify it's actually invalid
			if _, err := regexp.Compile(pattern); err == nil {
				return // skip if it happens to be valid
			}

			errs := validateSQLTemplates(&SQLTemplatesConfig{
				Enabled:              true,
				DatasourceName:       "db",
				BaseDir:              "./templates",
				RenderTimeout:        5 * time.Second,
				MaxRenderedSQLLen:    65536,
				MaxConcurrentQueries: 10,
				Templates: []TemplateConfig{
					{
						Name: "test_tmpl",
						File: "test.sql.tmpl",
						Parameters: []TemplateParamConfig{
							{Name: "p1", Type: "string", Pattern: &pattern},
						},
					},
				},
			})

			hasRegexErr := false
			for _, e := range errs {
				if strings.Contains(e, "invalid regex") {
					hasRegexErr = true
					break
				}
			}
			if !hasRegexErr {
				rt.Fatalf("invalid pattern %q was accepted, expected 'invalid regex' error", pattern)
			}
		})
	})
}
