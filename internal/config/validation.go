// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

// Package config provides configuration loading, validation, and management
// for the GraphQL API service.
package config

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// ValidationWarning represents a non-fatal configuration warning.
type ValidationWarning struct {
	Message string
}

// ValidateConfig performs comprehensive validation of the configuration.
// It checks all required fields, mutual exclusion rules, format constraints,
// and security-related settings. Returns an error combining all validation
// failures, or nil if the configuration is valid.
// Warnings (e.g., CSRF risk) are returned separately and do not cause failure.
func ValidateConfig(cfg *Config) ([]ValidationWarning, error) {
	var errs []string
	var warnings []ValidationWarning

	// === Server config validation ===
	if cfg.Server.Port <= 0 {
		errs = append(errs, "server.port must be > 0")
	}
	if cfg.Server.Mode != "production" && cfg.Server.Mode != "development" {
		errs = append(errs, fmt.Sprintf("server.mode must be \"production\" or \"development\", got %q", cfg.Server.Mode))
	}

	// WARN: CSRF risk if production + allow_get_queries
	if cfg.Server.Mode == "production" && cfg.Server.AllowGetQueries {
		warnings = append(warnings, ValidationWarning{
			Message: "CSRF risk: allow_get_queries is enabled in production mode; GET queries may be vulnerable to CSRF attacks",
		})
	}

	// === Datasource validation ===
	dsNames := make(map[string]bool)
	for i, ds := range cfg.Datasources {
		prefix := fmt.Sprintf("datasources[%d]", i)

		if !ds.Enabled {
			continue
		}

		if strings.TrimSpace(ds.Name) == "" {
			errs = append(errs, fmt.Sprintf("%s.name must not be empty", prefix))
		}
		if strings.TrimSpace(ds.Type) == "" {
			errs = append(errs, fmt.Sprintf("%s.type must not be empty", prefix))
		}

		// Datasource name uniqueness
		if ds.Name != "" {
			if dsNames[ds.Name] {
				errs = append(errs, fmt.Sprintf("%s: duplicate datasource name %q", prefix, ds.Name))
			}
			dsNames[ds.Name] = true
		}

		// Negative pool sizes
		if poolSize, ok := getIntOption(ds.Options, "pool_size"); ok && poolSize < 0 {
			errs = append(errs, fmt.Sprintf("%s.options.pool_size must not be negative", prefix))
		}

		// StarRocks whitelist required
		if ds.Type == "starrocks" {
			errs = append(errs, validateStarRocksWhitelist(ds, prefix)...)
		}
	}

	// === Auth validation ===
	// When auth.method is empty (e.g., disabled via env var or not configured),
	// skip all auth-related validation to allow running without authentication.
	authMethod := strings.TrimSpace(cfg.Auth.Method)
	if authMethod != "" {
		if authMethod != "jwt" && authMethod != "apikey" {
			errs = append(errs, fmt.Sprintf("auth.method must be \"jwt\" or \"apikey\", got %q", authMethod))
		}

		if authMethod == "jwt" {
			errs = append(errs, validateJWTConfig(cfg.Auth.JWT)...)
		}
	}

	// === Trusted proxies CIDR validation ===
	for i, cidr := range cfg.Auth.TrustedProxies {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			errs = append(errs, fmt.Sprintf("auth.trusted_proxies[%d] %q is not valid CIDR: %v", i, cidr, err))
		}
	}

	// === Rate limit validation ===
	if cfg.RateLimit.RequestsPerWindow <= 0 {
		errs = append(errs, "rate_limit.requests_per_window must be > 0")
	}
	if cfg.RateLimit.WindowSize <= 0 {
		errs = append(errs, "rate_limit.window_size must be > 0")
	}

	// === Cache validation ===
	if cfg.Cache.Backend != "memory" && cfg.Cache.Backend != "redis" {
		errs = append(errs, fmt.Sprintf("cache.backend must be \"memory\" or \"redis\", got %q", cfg.Cache.Backend))
	}

	// max_memory_size format validation
	if cfg.Cache.Memory.MaxMemorySize != "" {
		bytes, err := ParseSizeString(cfg.Cache.Memory.MaxMemorySize)
		if err != nil {
			errs = append(errs, fmt.Sprintf("cache.memory.max_memory_size %q is invalid: %v", cfg.Cache.Memory.MaxMemorySize, err))
		} else if bytes <= 0 {
			errs = append(errs, "cache.memory.max_memory_size must be > 0")
		}
	}

	// === Tracing validation ===
	if cfg.Tracing.SamplingRate < 0.0 || cfg.Tracing.SamplingRate > 1.0 {
		errs = append(errs, fmt.Sprintf("tracing.sampling_rate must be between 0.0 and 1.0, got %f", cfg.Tracing.SamplingRate))
	}

	if len(errs) > 0 {
		return warnings, fmt.Errorf("configuration validation failed:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return warnings, nil
}

// validateJWTConfig validates JWT-specific configuration including algorithm
// and mutual exclusion between secret and public_key_file.
func validateJWTConfig(jwt JWTConfig) []string {
	var errs []string

	algo := jwt.Algorithm
	if algo == "" {
		algo = "HS256" // default
	}

	if algo != "HS256" && algo != "RS256" && algo != "ES256" {
		errs = append(errs, fmt.Sprintf("auth.jwt.algorithm must be \"HS256\", \"RS256\", or \"ES256\", got %q", algo))
		return errs
	}

	switch algo {
	case "HS256":
		if jwt.Secret == "" {
			errs = append(errs, "auth.jwt.secret is required when algorithm is HS256")
		} else if len(jwt.Secret) < 32 {
			errs = append(errs, fmt.Sprintf("auth.jwt.secret must be at least 32 bytes, got %d", len(jwt.Secret)))
		}
		if jwt.PublicKeyFile != "" {
			errs = append(errs, "auth.jwt.public_key_file must not be set when algorithm is HS256")
		}

	case "RS256", "ES256":
		if jwt.PublicKeyFile == "" {
			errs = append(errs, fmt.Sprintf("auth.jwt.public_key_file is required when algorithm is %s", algo))
		} else {
			errs = append(errs, validatePEMFile(jwt.PublicKeyFile, algo)...)
		}
		if jwt.Secret != "" {
			errs = append(errs, fmt.Sprintf("auth.jwt.secret must not be set when algorithm is %s", algo))
		}
	}

	return errs
}

// validatePEMFile checks that the given file exists and contains a valid PEM block.
func validatePEMFile(path string, algo string) []string {
	var errs []string

	data, err := os.ReadFile(path)
	if err != nil {
		errs = append(errs, fmt.Sprintf("auth.jwt.public_key_file %q: %v", path, err))
		return errs
	}

	block, _ := pem.Decode(data)
	if block == nil {
		errs = append(errs, fmt.Sprintf("auth.jwt.public_key_file %q: not a valid PEM file", path))
		return errs
	}

	// Try to parse as a public key to validate the content
	_, err = x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		errs = append(errs, fmt.Sprintf("auth.jwt.public_key_file %q: invalid public key: %v", path, err))
	}

	return errs
}

// validateStarRocksWhitelist validates that a StarRocks datasource has a
// non-empty allowed_tables configuration with valid column names.
func validateStarRocksWhitelist(ds DataSourceConfig, prefix string) []string {
	var errs []string

	allowedTables, ok := ds.Options["allowed_tables"]
	if !ok || allowedTables == nil {
		errs = append(errs, fmt.Sprintf("%s (type=starrocks): options.allowed_tables is required and must not be empty", prefix))
		return errs
	}

	tablesMap, ok := allowedTables.(map[string]interface{})
	if !ok {
		errs = append(errs, fmt.Sprintf("%s (type=starrocks): options.allowed_tables must be a map of table definitions", prefix))
		return errs
	}

	if len(tablesMap) == 0 {
		errs = append(errs, fmt.Sprintf("%s (type=starrocks): options.allowed_tables must not be empty", prefix))
		return errs
	}

	identifierRegex := regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

	for tableName, tableDef := range tablesMap {
		tablePrefix := fmt.Sprintf("%s.options.allowed_tables[%s]", prefix, tableName)

		tableMap, ok := tableDef.(map[string]interface{})
		if !ok {
			errs = append(errs, fmt.Sprintf("%s: table definition must be a map", tablePrefix))
			continue
		}

		columnsRaw, ok := tableMap["columns"]
		if !ok {
			errs = append(errs, fmt.Sprintf("%s: must define at least one column", tablePrefix))
			continue
		}

		columns, ok := columnsRaw.([]interface{})
		if !ok {
			errs = append(errs, fmt.Sprintf("%s.columns: must be an array", tablePrefix))
			continue
		}

		if len(columns) == 0 {
			errs = append(errs, fmt.Sprintf("%s: must define at least one column", tablePrefix))
			continue
		}

		for j, col := range columns {
			colStr, ok := col.(string)
			if !ok {
				errs = append(errs, fmt.Sprintf("%s.columns[%d]: must be a string", tablePrefix, j))
				continue
			}
			if !identifierRegex.MatchString(colStr) {
				errs = append(errs, fmt.Sprintf("%s.columns[%d] %q: must match [a-zA-Z0-9_]", tablePrefix, j, colStr))
			}
		}
	}

	return errs
}

// getIntOption extracts an integer value from an options map.
// Returns the value and true if found, or 0 and false otherwise.
func getIntOption(opts map[string]interface{}, key string) (int, bool) {
	v, ok := opts[key]
	if !ok {
		return 0, false
	}
	switch val := v.(type) {
	case int:
		return val, true
	case int64:
		return int(val), true
	case float64:
		return int(val), true
	default:
		return 0, false
	}
}

// sizePattern matches size strings like "256MB", "1GB", "512KB".
var sizePattern = regexp.MustCompile(`(?i)^(\d+(?:\.\d+)?)\s*(KB|MB|GB)$`)

// ParseSizeString parses a human-readable size string (e.g., "256MB", "1GB")
// and returns the size in bytes. Supports KB, MB, GB units.
func ParseSizeString(s string) (int64, error) {
	s = strings.TrimSpace(s)
	matches := sizePattern.FindStringSubmatch(s)
	if matches == nil {
		return 0, fmt.Errorf("invalid size format %q: must be a number followed by KB, MB, or GB", s)
	}

	value, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size value %q: %w", matches[1], err)
	}

	unit := strings.ToUpper(matches[2])
	var multiplier int64
	switch unit {
	case "KB":
		multiplier = 1024
	case "MB":
		multiplier = 1024 * 1024
	case "GB":
		multiplier = 1024 * 1024 * 1024
	}

	result := int64(value * float64(multiplier))
	if result <= 0 {
		return 0, fmt.Errorf("parsed size must be > 0")
	}

	return result, nil
}
