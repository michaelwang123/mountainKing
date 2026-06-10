// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package resolver

import (
	"context"
	"sync/atomic"

	"github.com/michaelwang123/mountainKing/internal/adapter/starrocks"
	"github.com/michaelwang123/mountainKing/internal/config"
	ctxkeys "github.com/michaelwang123/mountainKing/internal/context"
	"github.com/michaelwang123/mountainKing/internal/datasource"
	"github.com/michaelwang123/mountainKing/internal/middleware"
	"github.com/michaelwang123/mountainKing/pkg/retry"
	"go.uber.org/zap"
)

// --- Mock helpers for property tests ---

// newPropResolver creates a mutationResolver for property testing with configurable mocks.
func newPropResolver(
	affectedRows int64,
	writeErr error,
	maxAffectedRows int,
	cacheClearer *trackingCacheClearer,
) *mutationResolver {
	wds := &mockWritableDS{
		executeWriteFunc: func(_ context.Context, _ string, _ []any) (int64, error) {
			return affectedRows, writeErr
		},
	}

	mutCfg := config.MutationsConfig{
		Enabled:         true,
		DatasourceName:  "test_ds",
		MaxAffectedRows: maxAffectedRows,
		MaxBatchSize:    500,
		MaxSQLLength:    1048576,
	}
	cfgPtr := &atomic.Pointer[config.MutationsConfig]{}
	cfgPtr.Store(&mutCfg)

	registry := datasource.NewAdapterRegistry()
	_ = registry.Register("mock_writable", func(name string, cfg datasource.DataSourceConfig) (datasource.DataSource, error) {
		wds.nameVal = name
		wds.typeVal = "mock_writable"
		return wds, nil
	})

	configs := []config.DataSourceConfig{
		{Name: "test_ds", Type: "mock_writable", Enabled: true},
	}
	retryCfg := retry.Config{MaxRetries: 0}
	mgr := datasource.NewDataSourceManager(registry, configs, retryCfg, zap.NewNop())
	_ = mgr.Init(context.Background())

	writableTables := map[string]*starrocks.WritableTableConfig{
		"orders": {
			Columns:           map[string]bool{"user_id": true, "amount": true, "status": true},
			AllowedOperations: map[string]bool{"insert": true, "update": true, "delete": true},
		},
	}
	allowedTables := map[string]map[string]bool{
		"orders": {"order_id": true, "user_id": true, "amount": true, "status": true, "created_at": true},
	}
	wtv := starrocks.NewWritableTableValidator(writableTables, allowedTables)

	var cc CacheClearer
	if cacheClearer != nil {
		cc = cacheClearer
	}

	r := &Resolver{
		DSManager:              mgr,
		MutationSQLBuilder:     &starrocks.MutationSQLBuilder{},
		MutationValidator:      starrocks.NewMutationValidator(500, 1048576),
		WritableTableValidator: wtv,
		MutationConfig:         cfgPtr,
		MutationRateLimiter:    &mockMutRateLimiter{},
		CacheClearer:           cc,
		Logger:                 zap.NewNop(),
	}

	return &mutationResolver{r}
}

// propAuthCtx creates a context with mutation-authorized identity.
func propAuthCtx(subject string) context.Context {
	identity := &middleware.AuthIdentity{
		Subject:     subject,
		Method:      "jwt",
		Operations:  []string{"query", "mutation"},
		Datasources: nil,
	}
	return context.WithValue(context.Background(), ctxkeys.CtxKeyAuthIdentity, identity)
}
