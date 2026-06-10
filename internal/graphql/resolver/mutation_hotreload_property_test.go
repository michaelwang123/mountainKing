// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package resolver

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/michaelwang123/mountainKing/internal/adapter/starrocks"
	"github.com/michaelwang123/mountainKing/internal/config"
	ctxkeys "github.com/michaelwang123/mountainKing/internal/context"
	"github.com/michaelwang123/mountainKing/internal/datasource"
	apierrors "github.com/michaelwang123/mountainKing/internal/errors"
	"github.com/michaelwang123/mountainKing/internal/graphql/generated"
	"github.com/michaelwang123/mountainKing/internal/middleware"
	"github.com/michaelwang123/mountainKing/internal/ratelimit"
	"github.com/michaelwang123/mountainKing/pkg/retry"
	"github.com/vektah/gqlparser/v2/gqlerror"
	"go.uber.org/zap"
)

// TestProperty17_HotReloadSafety validates that when mutations.enabled is toggled
// from true to false via atomic config swap, in-flight mutation requests that have
// already passed the feature-enabled check complete successfully, while new requests
// after the swap are rejected with FEATURE_DISABLED error.
//
// **Property 17: Hot-Reload Safety** �?toggle enabled→disabled: in-flight requests
// complete, new requests rejected
//
// **Validates: Requirements 9.8**
func TestProperty17_HotReloadSafety(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a random delay duration for the in-flight mutation (10-100ms).
		delayMs := rapid.IntRange(10, 100).Draw(t, "delayMs")
		delay := time.Duration(delayMs) * time.Millisecond

		// Generate a random table name from a set of valid tables.
		table := "orders"

		// Channel to signal when the in-flight mutation has started executing.
		started := make(chan struct{})

		// Create a slow mock that signals when execution begins, then delays.
		slowWriteFunc := func(_ context.Context, _ string, _ []any) (int64, error) {
			close(started) // Signal that the in-flight mutation has started.
			time.Sleep(delay)
			return 1, nil
		}

		// Build the resolver with enabled config and the slow mock.
		cfgPtr, r := buildHotReloadResolver(slowWriteFunc)

		// Create an authorized context.
		ctx := hotReloadAuthCtx()

		// Prepare valid insert input.
		values := []*generated.ColumnValueInput{
			{Column: "user_id", Value: float64(42)},
			{Column: "amount", Value: float64(100.5)},
		}

		// --- Start in-flight mutation in a goroutine ---
		var wg sync.WaitGroup
		var inflightResult *generated.MutationResult
		var inflightErr error

		wg.Add(1)
		go func() {
			defer wg.Done()
			inflightResult, inflightErr = r.InsertStarrocks(ctx, table, values)
		}()

		// Wait until the in-flight mutation has started executing (passed the
		// feature-enabled check and is now in the slow write).
		<-started

		// --- Atomically swap config to disabled ---
		disabledCfg := &config.MutationsConfig{
			Enabled:         false, // KEY: now disabled
			DatasourceName:  "test_ds",
			MaxAffectedRows: 1000,
			MaxBatchSize:    500,
			MaxSQLLength:    1048576,
		}
		cfgPtr.Store(disabledCfg)

		// --- Verify: new requests after the swap are rejected ---
		newResult, newErr := r.InsertStarrocks(ctx, table, values)

		if newResult != nil {
			t.Fatal("expected nil result for new request after config disabled, got non-nil")
		}
		if newErr == nil {
			t.Fatal("expected error for new request after config disabled, got nil")
		}

		// Verify error code is MUTATION_FEATURE_DISABLED.
		gqlErr, ok := newErr.(*gqlerror.Error)
		if !ok {
			t.Fatalf("expected *gqlerror.Error for new request, got %T: %v", newErr, newErr)
		}
		code, _ := gqlErr.Extensions["code"].(string)
		if code != apierrors.ErrMutationFeatureDisabled {
			t.Fatalf("expected error code %q for new request, got %q", apierrors.ErrMutationFeatureDisabled, code)
		}

		// --- Wait for in-flight mutation to complete ---
		wg.Wait()

		// --- Verify: in-flight mutation completed successfully ---
		if inflightErr != nil {
			t.Fatalf("expected in-flight mutation to complete successfully, got error: %v", inflightErr)
		}
		if inflightResult == nil {
			t.Fatal("expected non-nil result for in-flight mutation")
		}
		if !inflightResult.Success {
			t.Fatal("expected in-flight mutation result to have Success=true")
		}
		if inflightResult.AffectedRows != 1 {
			t.Fatalf("expected in-flight mutation to report 1 affected row, got %d", inflightResult.AffectedRows)
		}
	})
}

// --- Hot-reload test helpers ---

// buildHotReloadResolver creates a mutationResolver with an enabled config and a custom
// write function. Returns the atomic config pointer (for swapping) and the resolver.
func buildHotReloadResolver(writeFunc func(context.Context, string, []any) (int64, error)) (*atomic.Pointer[config.MutationsConfig], *mutationResolver) {
	wds := &hotReloadMockWritableDS{
		executeWriteFunc: writeFunc,
	}

	enabledCfg := &config.MutationsConfig{
		Enabled:         true,
		DatasourceName:  "test_ds",
		MaxAffectedRows: 1000,
		MaxBatchSize:    500,
		MaxSQLLength:    1048576,
	}
	cfgPtr := &atomic.Pointer[config.MutationsConfig]{}
	cfgPtr.Store(enabledCfg)

	// Build DataSourceManager with the mock.
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

	// Build writable table validator.
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

	r := &Resolver{
		DSManager:              mgr,
		MutationSQLBuilder:     &starrocks.MutationSQLBuilder{},
		MutationValidator:      starrocks.NewMutationValidator(500, 1048576),
		WritableTableValidator: wtv,
		MutationConfig:         cfgPtr,
		MutationRateLimiter:    &hotReloadMockRateLimiter{},
		Logger:                 zap.NewNop(),
	}

	return cfgPtr, &mutationResolver{r}
}

// hotReloadAuthCtx creates a context with full mutation authorization.
func hotReloadAuthCtx() context.Context {
	identity := &middleware.AuthIdentity{
		Subject:     "hotreload-test-user",
		Method:      "jwt",
		Operations:  []string{"query", "mutation"},
		Datasources: nil, // unrestricted
	}
	return context.WithValue(context.Background(), ctxkeys.CtxKeyAuthIdentity, identity)
}

// hotReloadMockWritableDS implements datasource.WritableDataSource for hot-reload testing.
type hotReloadMockWritableDS struct {
	nameVal          string
	typeVal          string
	executeWriteFunc func(ctx context.Context, sql string, params []any) (int64, error)
}

func (m *hotReloadMockWritableDS) Name() string                    { return m.nameVal }
func (m *hotReloadMockWritableDS) Type() string                    { return m.typeVal }
func (m *hotReloadMockWritableDS) IsAvailable() bool               { return true }
func (m *hotReloadMockWritableDS) SchemaFiles() []string           { return nil }
func (m *hotReloadMockWritableDS) Connect(_ context.Context) error { return nil }
func (m *hotReloadMockWritableDS) Execute(_ context.Context, _ datasource.QueryRequest) (*datasource.QueryResult, error) {
	return &datasource.QueryResult{}, nil
}
func (m *hotReloadMockWritableDS) HealthCheck(_ context.Context) error { return nil }
func (m *hotReloadMockWritableDS) Close(_ context.Context) error       { return nil }
func (m *hotReloadMockWritableDS) ExecuteWrite(ctx context.Context, sql string, params []any) (int64, error) {
	if m.executeWriteFunc != nil {
		return m.executeWriteFunc(ctx, sql, params)
	}
	return 0, fmt.Errorf("no write func configured")
}

// Compile-time check that hotReloadMockWritableDS implements WritableDataSource.
var _ datasource.WritableDataSource = (*hotReloadMockWritableDS)(nil)

// hotReloadMockRateLimiter always allows requests through for hot-reload testing.
type hotReloadMockRateLimiter struct{}

func (m *hotReloadMockRateLimiter) Allow(_ context.Context, _ string, _ int) (*ratelimit.RateLimitResult, error) {
	return &ratelimit.RateLimitResult{Allowed: true, Limit: 100, Remaining: 99, ResetAt: time.Now().Add(60 * time.Second)}, nil
}
