package config

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pgregory.net/rapid"
)

// =============================================================================
// Property 11: 配置校验拒绝无效值
// **Validates: Requirements 3.10**
// For any config with invalid values (negative port, empty connection address,
// invalid mode, etc.), ValidateConfig should return an error.
// =============================================================================

func TestProperty11_ConfigValidationRejectsInvalidValues(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		cfg := genValidBaseConfig(rt)

		// Pick a random invalidation strategy
		strategy := rapid.IntRange(0, 5).Draw(rt, "strategy")
		switch strategy {
		case 0:
			// Negative or zero port
			cfg.Server.Port = rapid.IntRange(-1000, 0).Draw(rt, "port")
		case 1:
			// Invalid server mode
			mode := rapid.StringMatching(`[a-z]{3,10}`).Draw(rt, "mode")
			for mode == "production" || mode == "development" {
				mode = rapid.StringMatching(`[a-z]{3,10}`).Draw(rt, "mode")
			}
			cfg.Server.Mode = mode
		case 2:
			// Invalid cache backend
			backend := rapid.StringMatching(`[a-z]{3,10}`).Draw(rt, "backend")
			for backend == "memory" || backend == "redis" {
				backend = rapid.StringMatching(`[a-z]{3,10}`).Draw(rt, "backend")
			}
			cfg.Cache.Backend = backend
		case 3:
			// Tracing sampling rate out of range
			outOfRange := rapid.Float64Range(1.01, 100.0).Draw(rt, "samplingRate")
			if rapid.Bool().Draw(rt, "negative") {
				outOfRange = -outOfRange
			}
			cfg.Tracing.SamplingRate = outOfRange
		case 4:
			// Zero or negative rate limit requests
			cfg.RateLimit.RequestsPerWindow = rapid.IntRange(-100, 0).Draw(rt, "reqPerWindow")
		case 5:
			// Zero or negative rate limit window
			cfg.RateLimit.WindowSize = time.Duration(rapid.IntRange(-100, 0).Draw(rt, "windowSize")) * time.Second
		}

		_, err := ValidateConfig(cfg)
		if err == nil {
			rt.Fatalf("expected validation error for strategy %d, but got nil", strategy)
		}
	})
}

// =============================================================================
// Property 72: 环境变量覆盖配置
// **Validates: Requirements 17.8**
// For any YAML config value, setting the corresponding GRAPHQL_ env var should
// override it. Specifically: create a temp YAML config with a known port value,
// set GRAPHQL_SERVER_PORT to a different value, LoadConfig should return the
// env var value.
// =============================================================================

func TestProperty72_EnvVarOverridesConfig(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		yamlPort := rapid.IntRange(1000, 9000).Draw(rt, "yamlPort")
		envPort := rapid.IntRange(9001, 20000).Draw(rt, "envPort")

		// Create a temp YAML config file
		yamlContent := fmt.Sprintf(`server:
  port: %d
  mode: production
rate_limit:
  requests_per_window: 100
  window_size: 60s
cache:
  backend: memory
  memory:
    max_memory_size: 256MB
tracing:
  sampling_rate: 1.0
`, yamlPort)

		tmpDir := t.TempDir()
		cfgPath := filepath.Join(tmpDir, "config.yaml")
		if err := os.WriteFile(cfgPath, []byte(yamlContent), 0644); err != nil {
			rt.Fatalf("failed to write config file: %v", err)
		}

		// Set env var to override
		os.Setenv("GRAPHQL_SERVER_PORT", fmt.Sprintf("%d", envPort))
		defer os.Unsetenv("GRAPHQL_SERVER_PORT")

		cfg, err := LoadConfig(cfgPath)
		if err != nil {
			rt.Fatalf("LoadConfig failed: %v", err)
		}

		if cfg.Server.Port != envPort {
			rt.Fatalf("expected port=%d (from env), got %d", envPort, cfg.Server.Port)
		}
	})
}

// =============================================================================
// Property 91: StarRocks 白名单必填校验
// **Validates: Design - StarRocks 白名单安全默认**
// For any StarRocks datasource config without allowed_tables (or empty),
// ValidateConfig should return error.
// =============================================================================

func TestProperty91_StarRocksWhitelistRequired(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		cfg := genValidBaseConfig(rt)

		dsName := rapid.StringMatching(`[a-z]{3,10}`).Draw(rt, "dsName")

		// Pick a strategy for missing/empty whitelist
		strategy := rapid.IntRange(0, 2).Draw(rt, "strategy")
		var opts map[string]interface{}
		switch strategy {
		case 0:
			// No allowed_tables key at all
			opts = map[string]interface{}{}
		case 1:
			// allowed_tables is nil
			opts = map[string]interface{}{"allowed_tables": nil}
		case 2:
			// allowed_tables is empty map
			opts = map[string]interface{}{"allowed_tables": map[string]interface{}{}}
		}

		cfg.Datasources = []DataSourceConfig{
			{Name: dsName, Type: "starrocks", Enabled: true, Options: opts},
		}

		_, err := ValidateConfig(cfg)
		if err == nil {
			rt.Fatalf("expected error for StarRocks without allowed_tables (strategy=%d)", strategy)
		}
		if !strings.Contains(err.Error(), "allowed_tables") {
			rt.Fatalf("expected allowed_tables error, got: %v", err)
		}
	})
}

// =============================================================================
// Property 94: JWT 配置互斥校验
// **Validates: Design - 配置校验规则**
// For any JWT config where HS256 has public_key_file set, or RS256/ES256 has
// secret set, ValidateConfig should return error.
// =============================================================================

func TestProperty94_JWTMutualExclusionValidation(t *testing.T) {
	// Pre-generate a PEM file for RS256/ES256 tests (reused across iterations)
	pemFile := createTempRSAPEM(t)

	rapid.Check(t, func(rt *rapid.T) {
		cfg := genValidBaseConfig(rt)
		cfg.Auth.Method = "jwt"

		// Pick a mutual exclusion violation
		strategy := rapid.IntRange(0, 1).Draw(rt, "strategy")
		switch strategy {
		case 0:
			// HS256 with public_key_file set
			cfg.Auth.JWT = JWTConfig{
				Algorithm:     "HS256",
				Secret:        "this-is-a-secret-that-is-at-least-32-bytes-long!!",
				PublicKeyFile: "/some/file.pem",
			}
		case 1:
			// RS256 or ES256 with secret set
			algo := rapid.SampledFrom([]string{"RS256", "ES256"}).Draw(rt, "algo")
			secret := rapid.StringMatching(`[a-zA-Z0-9]{32,64}`).Draw(rt, "secret")
			cfg.Auth.JWT = JWTConfig{
				Algorithm:     algo,
				PublicKeyFile: pemFile,
				Secret:        secret,
			}
		}

		_, err := ValidateConfig(cfg)
		if err == nil {
			rt.Fatalf("expected mutual exclusion error for strategy=%d", strategy)
		}
		errStr := err.Error()
		if strategy == 0 {
			if !strings.Contains(errStr, "public_key_file must not be set") {
				rt.Fatalf("expected public_key_file mutual exclusion error, got: %v", err)
			}
		} else {
			if !strings.Contains(errStr, "secret must not be set") {
				rt.Fatalf("expected secret mutual exclusion error, got: %v", err)
			}
		}
	})
}

// =============================================================================
// Property 95: 数据源名称唯一性
// **Validates: Design - 配置校验规则**
// For any datasource list with duplicate names, ValidateConfig should return error.
// =============================================================================

func TestProperty95_DatasourceNameUniqueness(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		cfg := genValidBaseConfig(rt)

		// Generate a random name that will be duplicated
		dupName := rapid.StringMatching(`[a-z]{3,10}`).Draw(rt, "dupName")

		// Generate 0-2 additional unique datasources
		extraCount := rapid.IntRange(0, 2).Draw(rt, "extraCount")
		datasources := []DataSourceConfig{
			{Name: dupName, Type: "prometheus", Enabled: true},
			{Name: dupName, Type: "prometheus", Enabled: true}, // duplicate
		}
		for i := 0; i < extraCount; i++ {
			uniqueName := fmt.Sprintf("unique_%d_%s", i, rapid.StringMatching(`[a-z]{3,5}`).Draw(rt, fmt.Sprintf("extra_%d", i)))
			datasources = append(datasources, DataSourceConfig{
				Name: uniqueName, Type: "prometheus", Enabled: true,
			})
		}

		cfg.Datasources = datasources

		_, err := ValidateConfig(cfg)
		if err == nil {
			rt.Fatalf("expected error for duplicate datasource name %q", dupName)
		}
		if !strings.Contains(err.Error(), "duplicate datasource name") {
			rt.Fatalf("expected duplicate name error, got: %v", err)
		}
	})
}

// =============================================================================
// Helpers
// =============================================================================

// genValidBaseConfig generates a valid base Config for property tests.
func genValidBaseConfig(rt *rapid.T) *Config {
	return &Config{
		Server: ServerConfig{
			Port: rapid.IntRange(1, 65535).Draw(rt, "basePort"),
			Mode: rapid.SampledFrom([]string{"production", "development"}).Draw(rt, "baseMode"),
		},
		RateLimit: RateLimitConfig{
			RequestsPerWindow: rapid.IntRange(1, 10000).Draw(rt, "baseReqPerWindow"),
			WindowSize:        time.Duration(rapid.IntRange(1, 3600).Draw(rt, "baseWindowSizeSec")) * time.Second,
		},
		Cache: CacheConfig{
			Backend: rapid.SampledFrom([]string{"memory", "redis"}).Draw(rt, "baseCacheBackend"),
			Memory: MemoryCacheConfig{
				MaxMemorySize: "256MB",
			},
		},
		Tracing: TracingConfig{
			SamplingRate: rapid.Float64Range(0.0, 1.0).Draw(rt, "baseSamplingRate"),
		},
	}
}

// createTempRSAPEMProp creates a temporary RSA PEM file for property tests.
// This is a duplicate of createTempRSAPEM to avoid import cycle issues.
func createTempRSAPEMProp(t *testing.T) string {
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

// createTempECPEMProp creates a temporary EC PEM file for property tests.
func createTempECPEMProp(t *testing.T) string {
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
