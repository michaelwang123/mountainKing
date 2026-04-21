// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

// Package config provides configuration loading and management for the GraphQL API service.
// It uses Viper to load YAML configuration files with environment variable overrides
// using the GRAPHQL_ prefix, following the 12-Factor App convention.
package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config is the root configuration structure for the GraphQL API service.
// It aggregates all sub-configurations for server, GraphQL engine, datasources,
// authentication, rate limiting, caching, and observability.
type Config struct {
	Server         ServerConfig         `mapstructure:"server"`
	GraphQL        GraphQLConfig        `mapstructure:"graphql"`
	Datasources    []DataSourceConfig   `mapstructure:"datasources"`
	Auth           AuthConfig           `mapstructure:"auth"`
	RateLimit      RateLimitConfig      `mapstructure:"rate_limit"`
	Cache          CacheConfig          `mapstructure:"cache"`
	CORS           CORSConfig           `mapstructure:"cors"`
	Compression    CompressionConfig    `mapstructure:"compression"`
	Logging        LoggingConfig        `mapstructure:"logging"`
	Sanitization   SanitizationConfig   `mapstructure:"sanitization"`
	Metrics        MetricsConfig        `mapstructure:"metrics"`
	Tracing        TracingConfig        `mapstructure:"tracing"`
	Retry          RetryConfig          `mapstructure:"retry"`
	CircuitBreaker CircuitBreakerConfig `mapstructure:"circuit_breaker"`
	AuthFailure    AuthFailureConfig    `mapstructure:"auth_failure"`
	Shutdown       ShutdownConfig       `mapstructure:"shutdown"`
	SQLTemplates   SQLTemplatesConfig   `mapstructure:"sql_templates"`
}

// ServerConfig holds HTTP server settings including port, mode, body size limits,
// request timeout, connection timeouts, concurrency limits, and batch query constraints.
type ServerConfig struct {
	Port                  int           `mapstructure:"port"`
	Mode                  string        `mapstructure:"mode"`
	MaxRequestBodySize    string        `mapstructure:"max_request_body_size"`
	RequestTimeout        time.Duration `mapstructure:"request_timeout"`
	ReadHeaderTimeout     time.Duration `mapstructure:"read_header_timeout"`     // Slowloris protection: max time to read request headers
	ReadTimeout           time.Duration `mapstructure:"read_timeout"`            // Max time to read entire request (headers + body)
	WriteTimeout          time.Duration `mapstructure:"write_timeout"`           // Max time to write response
	IdleTimeout           time.Duration `mapstructure:"idle_timeout"`            // Max time for keep-alive connections to stay idle
	MaxConcurrentRequests int           `mapstructure:"max_concurrent_requests"` // Global in-flight request limit; 0 = unlimited
	MaxBatchQueries       int           `mapstructure:"max_batch_queries"`
	AllowGetQueries       bool          `mapstructure:"allow_get_queries"`
}

// GraphQLConfig holds GraphQL engine settings including introspection control,
// query complexity/depth limits, result row limits, and APQ support.
type GraphQLConfig struct {
	IntrospectionEnabled bool `mapstructure:"introspection_enabled"`
	MaxQueryComplexity   int  `mapstructure:"max_query_complexity"`
	MaxQueryDepth        int  `mapstructure:"max_query_depth"`
	MaxResultRows        int  `mapstructure:"max_result_rows"`
	APQEnabled           bool `mapstructure:"apq_enabled"`
}

// DataSourceConfig represents the configuration for a single data source.
// Each data source has a type identifier, connection parameters, and custom options
// that are passed to the corresponding adapter factory.
type DataSourceConfig struct {
	Name       string                 `mapstructure:"name"`
	Type       string                 `mapstructure:"type"`
	Enabled    bool                   `mapstructure:"enabled"`
	Connection map[string]interface{} `mapstructure:"connection"`
	Options    map[string]interface{} `mapstructure:"options"`
}

// AuthConfig holds authentication settings including the authentication method
// (jwt or apikey), JWT/API Key specific configuration, and trusted proxy CIDRs.
type AuthConfig struct {
	Method         string       `mapstructure:"method"`
	JWT            JWTConfig    `mapstructure:"jwt"`
	APIKey         APIKeyConfig `mapstructure:"apikey"`
	TrustedProxies []string     `mapstructure:"trusted_proxies"`
}

// JWTConfig holds JWT authentication parameters including the signing algorithm,
// secret (for symmetric signing), public key file (for asymmetric signing), and issuer.
type JWTConfig struct {
	Algorithm     string `mapstructure:"algorithm"`
	Secret        string `mapstructure:"secret"`
	PublicKeyFile string `mapstructure:"public_key_file"`
	Issuer        string `mapstructure:"issuer"`
}

// APIKeyConfig holds API Key authentication parameters.
type APIKeyConfig struct {
	Keys []APIKeyConfigEntry `mapstructure:"keys"`
}

// APIKeyConfigEntry represents a single API Key with its ID, hashed key value,
// optional expiration time, and permission scope.
type APIKeyConfigEntry struct {
	ID          string `mapstructure:"id"`
	Key         string `mapstructure:"key"`
	ExpiresAt   string `mapstructure:"expires_at"`
	Permissions struct {
		Datasources []string `mapstructure:"datasources"`
		Operations  []string `mapstructure:"operations"`
	} `mapstructure:"permissions"`
}

// CacheConfig holds caching layer settings including backend type (memory/redis),
// TTL values, jitter for avalanche protection, and per-datasource TTL overrides.
type CacheConfig struct {
	Enabled          bool                             `mapstructure:"enabled"`
	Backend          string                           `mapstructure:"backend"`
	DefaultTTL       time.Duration                    `mapstructure:"default_ttl"`
	EmptyResultTTL   time.Duration                    `mapstructure:"empty_result_ttl"`
	TTLJitterPercent int                              `mapstructure:"ttl_jitter_percent"`
	Memory           MemoryCacheConfig                `mapstructure:"memory"`
	Redis            RedisCacheConfig                 `mapstructure:"redis"`
	PerDatasource    map[string]DatasourceCacheConfig `mapstructure:"per_datasource"`
}

// MemoryCacheConfig holds in-memory cache backend settings including
// maximum entry count and maximum memory size for dual-limit eviction.
type MemoryCacheConfig struct {
	MaxEntries    int    `mapstructure:"max_entries"`
	MaxMemorySize string `mapstructure:"max_memory_size"`
}

// RedisCacheConfig holds Redis cache backend connection settings.
type RedisCacheConfig struct {
	Addr     string `mapstructure:"addr"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

// DatasourceCacheConfig holds per-datasource cache TTL override.
type DatasourceCacheConfig struct {
	TTL time.Duration `mapstructure:"ttl"`
}

// RateLimitConfig holds rate limiting settings including mode (local/distributed),
// token bucket parameters, and Redis connection for distributed mode.
type RateLimitConfig struct {
	Mode              string        `mapstructure:"mode"`
	RequestsPerWindow int           `mapstructure:"requests_per_window"`
	WindowSize        time.Duration `mapstructure:"window_size"`
	Redis             RedisConfig   `mapstructure:"redis"`
}

// RedisConfig holds generic Redis connection settings used by non-cache components
// such as distributed rate limiting, Redis cache backend, and Redis tracing hook.
type RedisConfig struct {
	Addr     string `mapstructure:"addr"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

// CORSConfig holds Cross-Origin Resource Sharing settings.
type CORSConfig struct {
	Enabled        bool     `mapstructure:"enabled"`
	AllowedOrigins []string `mapstructure:"allowed_origins"`
	AllowedMethods []string `mapstructure:"allowed_methods"`
	AllowedHeaders []string `mapstructure:"allowed_headers"`
}

// CompressionConfig holds response compression settings.
type CompressionConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	MinSize string `mapstructure:"min_size"`
}

// LoggingConfig holds structured logging settings including level, format,
// and audit log configuration.
type LoggingConfig struct {
	Level  string      `mapstructure:"level"`
	Format string      `mapstructure:"format"`
	Audit  AuditConfig `mapstructure:"audit"`
}

// AuditConfig holds audit logging settings. Audit logs are independent from
// application logs and can be directed to stdout or a file.
type AuditConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	Output   string `mapstructure:"output"`
	FilePath string `mapstructure:"file_path"`
}

// SanitizationConfig holds sensitive data masking settings.
// Rules define regex patterns and their replacements for sanitizing
// SQL statements and other sensitive data in logs and trace spans.
type SanitizationConfig struct {
	Enabled bool               `mapstructure:"enabled"`
	Rules   []SanitizationRule `mapstructure:"rules"`
}

// SanitizationRule defines a single sanitization rule with a regex pattern
// and its replacement text.
type SanitizationRule struct {
	Pattern     string `mapstructure:"pattern"`
	Replacement string `mapstructure:"replacement"`
}

// MetricsConfig holds Prometheus metrics settings including custom labels
// that are appended to all metrics (e.g., env, cluster, instance).
type MetricsConfig struct {
	CustomLabels map[string]string `mapstructure:"custom_labels"`
}

// TracingConfig holds OpenTelemetry distributed tracing settings including
// enable/disable toggle, sampling rate, and OTLP exporter configuration.
type TracingConfig struct {
	Enabled      bool       `mapstructure:"enabled"`
	SamplingRate float64    `mapstructure:"sampling_rate"`
	OTLP         OTLPConfig `mapstructure:"otlp"`
}

// OTLPConfig holds OTLP exporter settings for trace data export.
type OTLPConfig struct {
	Endpoint string `mapstructure:"endpoint"`
	Protocol string `mapstructure:"protocol"`
}

// RetryConfig holds retry strategy settings for transient error recovery.
type RetryConfig struct {
	MaxRetries    int           `mapstructure:"max_retries"`
	RetryInterval time.Duration `mapstructure:"retry_interval"`
	Backoff       string        `mapstructure:"backoff"`
}

// CircuitBreakerConfig holds circuit breaker settings for datasource resilience.
type CircuitBreakerConfig struct {
	FailureThreshold    int           `mapstructure:"failure_threshold"`
	OpenDuration        time.Duration `mapstructure:"open_duration"`
	HalfOpenMaxRequests int           `mapstructure:"half_open_max_requests"`
	SuccessThreshold    int           `mapstructure:"success_threshold"`
}

// AuthFailureConfig holds brute-force protection settings for authentication failures.
// When enabled, IPs exceeding the failure threshold within the window are banned.
type AuthFailureConfig struct {
	Enabled     bool          `mapstructure:"enabled"`
	Threshold   int           `mapstructure:"threshold"`
	Window      time.Duration `mapstructure:"window"`
	BanDuration time.Duration `mapstructure:"ban_duration"`
}

// ShutdownConfig holds graceful shutdown settings.
type ShutdownConfig struct {
	MaxWaitTime time.Duration `mapstructure:"max_wait_time"`
}

// SQLTemplatesConfig holds SQL template engine configuration.
type SQLTemplatesConfig struct {
	Enabled              bool             `mapstructure:"enabled"`
	DatasourceName       string           `mapstructure:"datasource_name"` // Associated StarRocks datasource name (required when enabled)
	BaseDir              string           `mapstructure:"base_dir"`
	SharedDir            string           `mapstructure:"shared_dir"`
	RenderTimeout        time.Duration    `mapstructure:"render_timeout"`
	MaxRenderedSQLLen    int              `mapstructure:"max_rendered_sql_length"`
	MaxConcurrentQueries int              `mapstructure:"max_concurrent_queries"`
	Templates            []TemplateConfig `mapstructure:"templates"`
}

// TemplateConfig holds the configuration for a single SQL template.
type TemplateConfig struct {
	Name         string                `mapstructure:"name"`
	File         string                `mapstructure:"file"`
	Description  string                `mapstructure:"description"`
	CacheEnabled *bool                 `mapstructure:"cache_enabled"` // nil = true (default enabled)
	CacheTTL     *time.Duration        `mapstructure:"cache_ttl"`     // nil = use datasource default TTL
	CountEnabled *bool                 `mapstructure:"count_enabled"` // nil = true (default enabled)
	Parameters   []TemplateParamConfig `mapstructure:"parameters"`
}

// TemplateParamConfig holds the parameter schema for a template parameter.
type TemplateParamConfig struct {
	Name      string   `mapstructure:"name"`
	Type      string   `mapstructure:"type"` // string, int, float, boolean, string[]
	Required  bool     `mapstructure:"required"`
	Default   *string  `mapstructure:"default"`
	Enum      []string `mapstructure:"enum"`
	MaxLength *int     `mapstructure:"max_length"` // string type, default 1024
	MaxItems  *int     `mapstructure:"max_items"`  // string[] type, default 1000
	Pattern   *string  `mapstructure:"pattern"`    // RE2 regex constraint
}

// LoadConfig loads configuration from a YAML file and environment variables.
// Environment variables use the GRAPHQL_ prefix (e.g., GRAPHQL_SERVER_PORT)
// and override values from the YAML file, following the 12-Factor App convention.
// If configPath is empty, only defaults and environment variables are used.
func LoadConfig(configPath string) (*Config, error) {
	v := viper.New()

	setDefaults(v)

	if configPath != "" {
		v.SetConfigFile(configPath)
	}

	v.SetEnvPrefix("GRAPHQL")
	v.AutomaticEnv()
	// Replace dots with underscores so nested keys like "server.port"
	// map to environment variables like GRAPHQL_SERVER_PORT.
	v.SetEnvKeyReplacer(underscoreReplacer())

	// Explicitly bind nested keys that are commonly overridden via env vars.
	// Viper's AutomaticEnv only works for keys that have been accessed via Get(),
	// Set(), or SetDefault(). For nested keys loaded from YAML, we need explicit
	// BindEnv calls to ensure env var overrides work with Unmarshal().
	_ = v.BindEnv("auth.method", "GRAPHQL_AUTH_METHOD")
	_ = v.BindEnv("server.mode", "GRAPHQL_SERVER_MODE")
	_ = v.BindEnv("graphql.introspection_enabled", "GRAPHQL_GRAPHQL_INTROSPECTION_ENABLED")

	if configPath != "" {
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Post-unmarshal env var overrides for keys where Viper's AutomaticEnv
	// doesn't work reliably (e.g., empty string overrides, nested keys).
	// Use GRAPHQL_AUTH_METHOD="none" to disable authentication.
	if envVal, ok := os.LookupEnv("GRAPHQL_AUTH_METHOD"); ok {
		cfg.Auth.Method = envVal
	}
	// Treat "none" as disabled (empty).
	if cfg.Auth.Method == "none" {
		cfg.Auth.Method = ""
	}

	return &cfg, nil
}

// underscoreReplacer returns a strings.Replacer that converts dots and hyphens
// to underscores for environment variable key mapping.
func underscoreReplacer() *strings.Replacer {
	return strings.NewReplacer(".", "_", "-", "_")
}

// setDefaults sets default values for all configuration fields.
// These defaults match the requirements appendix specification.
func setDefaults(v *viper.Viper) {
	// Server defaults
	v.SetDefault("server.port", 8080)
	v.SetDefault("server.mode", "production")
	v.SetDefault("server.max_request_body_size", "1MB")
	v.SetDefault("server.request_timeout", 30*time.Second)
	v.SetDefault("server.read_header_timeout", 10*time.Second)
	v.SetDefault("server.read_timeout", 30*time.Second)
	v.SetDefault("server.write_timeout", 60*time.Second)
	v.SetDefault("server.idle_timeout", 120*time.Second)
	v.SetDefault("server.max_concurrent_requests", 0) // 0 = unlimited (use rate limiting + HPA instead)
	v.SetDefault("server.max_batch_queries", 10)
	v.SetDefault("server.allow_get_queries", false)

	// GraphQL defaults
	v.SetDefault("graphql.introspection_enabled", false)
	v.SetDefault("graphql.max_query_complexity", 100)
	v.SetDefault("graphql.max_query_depth", 10)
	v.SetDefault("graphql.max_result_rows", 10000)
	v.SetDefault("graphql.apq_enabled", false)

	// Cache defaults
	v.SetDefault("cache.enabled", false)
	v.SetDefault("cache.backend", "memory")
	v.SetDefault("cache.default_ttl", 60*time.Second)
	v.SetDefault("cache.empty_result_ttl", 30*time.Second)
	v.SetDefault("cache.ttl_jitter_percent", 10)
	v.SetDefault("cache.memory.max_entries", 10000)
	v.SetDefault("cache.memory.max_memory_size", "256MB")

	// Rate limit defaults
	v.SetDefault("rate_limit.mode", "local")
	v.SetDefault("rate_limit.requests_per_window", 100)
	v.SetDefault("rate_limit.window_size", 60*time.Second)

	// Tracing defaults
	v.SetDefault("tracing.enabled", false)
	v.SetDefault("tracing.sampling_rate", 1.0)

	// Retry defaults
	v.SetDefault("retry.max_retries", 3)
	v.SetDefault("retry.retry_interval", 100*time.Millisecond)
	v.SetDefault("retry.backoff", "exponential")

	// Circuit breaker defaults
	v.SetDefault("circuit_breaker.failure_threshold", 5)
	v.SetDefault("circuit_breaker.open_duration", 30*time.Second)
	v.SetDefault("circuit_breaker.half_open_max_requests", 1)
	v.SetDefault("circuit_breaker.success_threshold", 2)

	// Auth failure (brute-force protection) defaults
	v.SetDefault("auth_failure.enabled", false)
	v.SetDefault("auth_failure.threshold", 10)
	v.SetDefault("auth_failure.window", 5*time.Minute)
	v.SetDefault("auth_failure.ban_duration", 15*time.Minute)

	// Logging defaults
	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.format", "json")

	// CORS defaults
	v.SetDefault("cors.enabled", false)

	// Compression defaults
	v.SetDefault("compression.enabled", false)
	v.SetDefault("compression.min_size", "1KB")

	// Sanitization defaults
	v.SetDefault("sanitization.enabled", false)

	// Shutdown defaults
	v.SetDefault("shutdown.max_wait_time", 30*time.Second)

	// SQL templates defaults
	v.SetDefault("sql_templates.enabled", false)
	v.SetDefault("sql_templates.datasource_name", "")
	v.SetDefault("sql_templates.base_dir", "./templates")
	v.SetDefault("sql_templates.render_timeout", 5*time.Second)
	v.SetDefault("sql_templates.max_rendered_sql_length", 65536)
	v.SetDefault("sql_templates.max_concurrent_queries", 10)
}
