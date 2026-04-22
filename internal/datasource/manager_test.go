// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package datasource

import (
	"context"
	"fmt"
	"testing"

	"go.uber.org/zap"

	"github.com/michaelwang123/mountainKing/internal/config"
	"github.com/michaelwang123/mountainKing/pkg/retry"
)

// newTestManager creates a DataSourceManager with a registry pre-loaded with
// a "mock" adapter factory that returns the provided MockDataSource instances.
func newTestManager(t *testing.T, configs []config.DataSourceConfig, mocks map[string]*MockDataSource) *DataSourceManager {
	t.Helper()

	registry := NewAdapterRegistry()
	_ = registry.Register("mock", func(name string, cfg DataSourceConfig) (DataSource, error) {
		m, ok := mocks[name]
		if !ok {
			return nil, fmt.Errorf("no mock configured for %q", name)
		}
		return m, nil
	})

	retryCfg := retry.Config{
		MaxRetries: 1,
	}

	return NewDataSourceManager(registry, configs, retryCfg, zap.NewNop())
}

func TestInit_PartialFailure(t *testing.T) {
	mocks := map[string]*MockDataSource{
		"ds-ok": {
			NameVal:      "ds-ok",
			TypeVal:      "mock",
			AvailableVal: true,
			ConnectFunc:  func(ctx context.Context) error { return nil },
		},
		"ds-fail": {
			NameVal:      "ds-fail",
			TypeVal:      "mock",
			AvailableVal: false,
			ConnectFunc:  func(ctx context.Context) error { return fmt.Errorf("connection refused") },
		},
	}

	configs := []config.DataSourceConfig{
		{Name: "ds-ok", Type: "mock", Enabled: true},
		{Name: "ds-fail", Type: "mock", Enabled: true},
	}

	mgr := newTestManager(t, configs, mocks)
	ctx := context.Background()

	// Init should not return error even with partial failure
	if err := mgr.Init(ctx); err != nil {
		t.Fatalf("Init() returned error: %v", err)
	}

	// ds-ok should be available
	ds, err := mgr.Get("ds-ok")
	if err != nil {
		t.Fatalf("Get(ds-ok) returned error: %v", err)
	}
	if ds.Name() != "ds-ok" {
		t.Errorf("expected name ds-ok, got %s", ds.Name())
	}

	// ds-fail should be registered but unavailable
	_, err = mgr.Get("ds-fail")
	if err == nil {
		t.Fatal("expected Get(ds-fail) to return error for unavailable datasource")
	}
}

func TestInit_DisabledSkipped(t *testing.T) {
	mocks := map[string]*MockDataSource{
		"ds-enabled": {
			NameVal:      "ds-enabled",
			TypeVal:      "mock",
			AvailableVal: true,
			ConnectFunc:  func(ctx context.Context) error { return nil },
		},
	}

	configs := []config.DataSourceConfig{
		{Name: "ds-enabled", Type: "mock", Enabled: true},
		{Name: "ds-disabled", Type: "mock", Enabled: false},
	}

	mgr := newTestManager(t, configs, mocks)
	ctx := context.Background()

	if err := mgr.Init(ctx); err != nil {
		t.Fatalf("Init() returned error: %v", err)
	}

	// Enabled datasource should be accessible
	if _, err := mgr.Get("ds-enabled"); err != nil {
		t.Fatalf("Get(ds-enabled) returned error: %v", err)
	}

	// Disabled datasource should not be registered at all
	_, err := mgr.Get("ds-disabled")
	if err == nil {
		t.Fatal("expected Get(ds-disabled) to return error for disabled datasource")
	}
}

func TestGet_Found(t *testing.T) {
	mocks := map[string]*MockDataSource{
		"my-ds": {
			NameVal:      "my-ds",
			TypeVal:      "mock",
			AvailableVal: true,
			ConnectFunc:  func(ctx context.Context) error { return nil },
		},
	}

	configs := []config.DataSourceConfig{
		{Name: "my-ds", Type: "mock", Enabled: true},
	}

	mgr := newTestManager(t, configs, mocks)
	ctx := context.Background()

	if err := mgr.Init(ctx); err != nil {
		t.Fatalf("Init() returned error: %v", err)
	}

	ds, err := mgr.Get("my-ds")
	if err != nil {
		t.Fatalf("Get() returned error: %v", err)
	}
	if ds.Name() != "my-ds" {
		t.Errorf("expected name my-ds, got %s", ds.Name())
	}
}

func TestGet_NotFound(t *testing.T) {
	mgr := newTestManager(t, nil, nil)

	_, err := mgr.Get("nonexistent")
	if err == nil {
		t.Fatal("expected Get() to return error for nonexistent datasource")
	}
}

func TestGet_Unavailable(t *testing.T) {
	mocks := map[string]*MockDataSource{
		"ds-down": {
			NameVal:      "ds-down",
			TypeVal:      "mock",
			AvailableVal: false,
			ConnectFunc:  func(ctx context.Context) error { return fmt.Errorf("down") },
		},
	}

	configs := []config.DataSourceConfig{
		{Name: "ds-down", Type: "mock", Enabled: true},
	}

	mgr := newTestManager(t, configs, mocks)
	ctx := context.Background()

	if err := mgr.Init(ctx); err != nil {
		t.Fatalf("Init() returned error: %v", err)
	}

	_, err := mgr.Get("ds-down")
	if err == nil {
		t.Fatal("expected Get() to return error for unavailable datasource")
	}
}

func TestCloseAll(t *testing.T) {
	closed := make(map[string]bool)

	mocks := map[string]*MockDataSource{
		"ds1": {
			NameVal:      "ds1",
			TypeVal:      "mock",
			AvailableVal: true,
			ConnectFunc:  func(ctx context.Context) error { return nil },
			CloseFunc:    func(ctx context.Context) error { closed["ds1"] = true; return nil },
		},
		"ds2": {
			NameVal:      "ds2",
			TypeVal:      "mock",
			AvailableVal: true,
			ConnectFunc:  func(ctx context.Context) error { return nil },
			CloseFunc:    func(ctx context.Context) error { closed["ds2"] = true; return nil },
		},
	}

	configs := []config.DataSourceConfig{
		{Name: "ds1", Type: "mock", Enabled: true},
		{Name: "ds2", Type: "mock", Enabled: true},
	}

	mgr := newTestManager(t, configs, mocks)
	ctx := context.Background()

	if err := mgr.Init(ctx); err != nil {
		t.Fatalf("Init() returned error: %v", err)
	}

	if err := mgr.CloseAll(ctx); err != nil {
		t.Fatalf("CloseAll() returned error: %v", err)
	}

	if !closed["ds1"] {
		t.Error("expected ds1 to be closed")
	}
	if !closed["ds2"] {
		t.Error("expected ds2 to be closed")
	}
}
