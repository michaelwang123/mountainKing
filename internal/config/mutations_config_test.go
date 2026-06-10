// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package config

import (
	"strings"
	"testing"
)

// validMutationsBaseConfig returns a Config with mutations enabled and a valid
// StarRocks datasource referenced by mutations.datasource_name.
func validMutationsBaseConfig() *Config {
	cfg := validBaseConfig()
	cfg.Datasources = []DataSourceConfig{
		{
			Name:    "analytics_db",
			Type:    "starrocks",
			Enabled: true,
			Options: map[string]any{
				"allowed_tables": map[string]any{
					"orders": map[string]any{
						"columns": []any{"order_id", "amount", "status"},
					},
				},
			},
		},
	}
	cfg.Mutations = MutationsConfig{
		Enabled:         true,
		DatasourceName:  "analytics_db",
		MaxAffectedRows: 1000,
		MaxBatchSize:    500,
		MaxSQLLength:    1048576,
	}
	return cfg
}

func TestValidateMutationsConfig_DisabledSkipsValidation(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Mutations = MutationsConfig{
		Enabled:        false,
		DatasourceName: "", // would fail if enabled, but disabled skips all checks
	}
	err := ValidateMutationsConfig(cfg)
	if err != nil {
		t.Fatalf("disabled mutations should skip validation, got: %v", err)
	}
}

func TestValidateMutationsConfig_ValidConfig(t *testing.T) {
	cfg := validMutationsBaseConfig()
	err := ValidateMutationsConfig(cfg)
	if err != nil {
		t.Fatalf("expected no error for valid config, got: %v", err)
	}
}

func TestValidateMutationsConfig_EmptyDatasourceName(t *testing.T) {
	cfg := validMutationsBaseConfig()
	cfg.Mutations.DatasourceName = ""
	err := ValidateMutationsConfig(cfg)
	if err == nil {
		t.Fatal("expected error for empty datasource_name")
	}
	if !strings.Contains(err.Error(), "datasource_name is required") {
		t.Fatalf("expected datasource_name required error, got: %v", err)
	}
}

func TestValidateMutationsConfig_NonExistentDatasource(t *testing.T) {
	cfg := validMutationsBaseConfig()
	cfg.Mutations.DatasourceName = "nonexistent_ds"
	err := ValidateMutationsConfig(cfg)
	if err == nil {
		t.Fatal("expected error for non-existent datasource")
	}
	if !strings.Contains(err.Error(), "does not reference an existing datasource") {
		t.Fatalf("expected non-existent datasource error, got: %v", err)
	}
}

func TestValidateMutationsConfig_DisabledDatasource(t *testing.T) {
	cfg := validMutationsBaseConfig()
	cfg.Datasources[0].Enabled = false
	err := ValidateMutationsConfig(cfg)
	if err == nil {
		t.Fatal("expected error for disabled datasource")
	}
	if !strings.Contains(err.Error(), "disabled datasource") {
		t.Fatalf("expected disabled datasource error, got: %v", err)
	}
}

func TestValidateMutationsConfig_NonStarrocksType(t *testing.T) {
	cfg := validMutationsBaseConfig()
	cfg.Datasources[0].Type = "prometheus"
	err := ValidateMutationsConfig(cfg)
	if err == nil {
		t.Fatal("expected error for non-starrocks type")
	}
	if !strings.Contains(err.Error(), "must reference a starrocks type datasource") {
		t.Fatalf("expected starrocks type error, got: %v", err)
	}
}

func TestValidateMutationsConfig_MaxBatchSizeExceedsMaxAffectedRows(t *testing.T) {
	cfg := validMutationsBaseConfig()
	cfg.Mutations.MaxBatchSize = 2000
	cfg.Mutations.MaxAffectedRows = 1000
	err := ValidateMutationsConfig(cfg)
	if err == nil {
		t.Fatal("expected error for max_batch_size > max_affected_rows")
	}
	if !strings.Contains(err.Error(), "max_batch_size") && !strings.Contains(err.Error(), "max_affected_rows") {
		t.Fatalf("expected max_batch_size/max_affected_rows error, got: %v", err)
	}
}

func TestValidateMutationsConfig_MaxBatchSizeEqualsMaxAffectedRows(t *testing.T) {
	cfg := validMutationsBaseConfig()
	cfg.Mutations.MaxBatchSize = 1000
	cfg.Mutations.MaxAffectedRows = 1000
	err := ValidateMutationsConfig(cfg)
	if err != nil {
		t.Fatalf("max_batch_size == max_affected_rows should be valid, got: %v", err)
	}
}
