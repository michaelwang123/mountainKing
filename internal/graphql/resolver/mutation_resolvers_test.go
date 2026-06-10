// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package resolver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"

	ctxkeys "github.com/michaelwang123/mountainKing/internal/context"
	"github.com/michaelwang123/mountainKing/internal/datasource"
	"github.com/michaelwang123/mountainKing/pkg/retry"

	"github.com/michaelwang123/mountainKing/internal/adapter/starrocks"
	"github.com/michaelwang123/mountainKing/internal/config"
	apierrors "github.com/michaelwang123/mountainKing/internal/errors"
	"github.com/michaelwang123/mountainKing/internal/graphql/generated"
	"github.com/michaelwang123/mountainKing/internal/middleware"
	"github.com/michaelwang123/mountainKing/internal/ratelimit"
)

// --- Test helpers ---

// mockWritableDS implements datasource.WritableDataSource for testing.
type mockWritableDS struct {
	nameVal          string
	typeVal          string
	executeWriteFunc func(ctx context.Context, sql string, params []any) (int64, error)
}

func (m *mockWritableDS) Name() string                    { return m.nameVal }
func (m *mockWritableDS) Type() string                    { return m.typeVal }
func (m *mockWritableDS) IsAvailable() bool               { return true }
func (m *mockWritableDS) SchemaFiles() []string           { return nil }
func (m *mockWritableDS) Connect(_ context.Context) error { return nil }
func (m *mockWritableDS) Execute(_ context.Context, _ datasource.QueryRequest) (*datasource.QueryResult, error) {
	return &datasource.QueryResult{}, nil
}
func (m *mockWritableDS) HealthCheck(_ context.Context) error { return nil }
func (m *mockWritableDS) Close(_ context.Context) error       { return nil }
func (m *mockWritableDS) ExecuteWrite(ctx context.Context, sql string, params []any) (int64, error) {
	if m.executeWriteFunc != nil {
		return m.executeWriteFunc(ctx, sql, params)
	}
	return 0, nil
}

// Compile-time check that mockWritableDS implements WritableDataSource.
var _ datasource.WritableDataSource = (*mockWritableDS)(nil)

// mockMutRateLimiter implements ratelimit.RateLimiter for testing.
type mockMutRateLimiter struct {
	allowFunc func(ctx context.Context, key string, count int) (*ratelimit.RateLimitResult, error)
}

func (m *mockMutRateLimiter) Allow(ctx context.Context, key string, count int) (*ratelimit.RateLimitResult, error) {
	if m.allowFunc != nil {
		return m.allowFunc(ctx, key, count)
	}
	return &ratelimit.RateLimitResult{Allowed: true, Limit: 20, Remaining: 19, ResetAt: time.Now().Add(60 * time.Second)}, nil
}

// newMutTestResolver creates a resolver configured for mutation testing with mocked dependencies.
func newMutTestResolver(t *testing.T, wds *mockWritableDS, opts ...func(*Resolver)) *mutationResolver {
	t.Helper()

	mutCfg := config.MutationsConfig{
		Enabled:         true,
		DatasourceName:  "test_ds",
		MaxAffectedRows: 1000,
		MaxBatchSize:    500,
		MaxSQLLength:    1048576,
	}
	cfgPtr := &atomic.Pointer[config.MutationsConfig]{}
	cfgPtr.Store(&mutCfg)

	// Build a DataSourceManager with the mock writable datasource via registry + Init.
	registry := datasource.NewAdapterRegistry()
	_ = registry.Register("mock_writable", func(name string, cfg datasource.DataSourceConfig) (datasource.DataSource, error) {
		if wds == nil {
			return nil, fmt.Errorf("no mock configured for %q", name)
		}
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
		"events": {
			Columns:           map[string]bool{"event_type": true, "payload": true, "created_at": true},
			AllowedOperations: map[string]bool{"insert": true},
		},
	}
	allowedTables := map[string]map[string]bool{
		"orders": {"order_id": true, "user_id": true, "amount": true, "status": true, "created_at": true},
		"events": {"event_id": true, "event_type": true, "payload": true, "created_at": true},
	}
	wtv := starrocks.NewWritableTableValidator(writableTables, allowedTables)

	r := &Resolver{
		DSManager:              mgr,
		MutationSQLBuilder:     &starrocks.MutationSQLBuilder{},
		MutationValidator:      starrocks.NewMutationValidator(500, 1048576),
		WritableTableValidator: wtv,
		MutationConfig:         cfgPtr,
		MutationRateLimiter:    &mockMutRateLimiter{},
	}

	for _, o := range opts {
		o(r)
	}

	return &mutationResolver{r}
}

// ctxWithMutAuth returns a context with the given auth identity.
func ctxWithMutAuth(operations []string, datasources []string) context.Context {
	identity := &middleware.AuthIdentity{
		Subject:     "test-user",
		Method:      "jwt",
		Operations:  operations,
		Datasources: datasources,
	}
	return context.WithValue(context.Background(), ctxkeys.CtxKeyAuthIdentity, identity)
}

// --- InsertStarrocks Tests ---

func TestInsertStarrocks_Success(t *testing.T) {
	wds := &mockWritableDS{
		executeWriteFunc: func(_ context.Context, sql string, params []any) (int64, error) {
			if !strings.Contains(sql, "INSERT INTO") {
				t.Errorf("expected INSERT INTO in SQL, got %q", sql)
			}
			return 1, nil
		},
	}

	r := newMutTestResolver(t, wds)
	ctx := ctxWithMutAuth([]string{"query", "mutation"}, nil)

	values := []*generated.ColumnValueInput{
		{Column: "user_id", Value: float64(42)},
		{Column: "amount", Value: float64(99.99)},
	}

	result, err := r.InsertStarrocks(ctx, "orders", values)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success=true")
	}
	if result.AffectedRows != 1 {
		t.Errorf("expected 1 affected row, got %d", result.AffectedRows)
	}
	if result.Warning != nil {
		t.Errorf("expected no warning, got %q", *result.Warning)
	}
}

func TestInsertStarrocks_FeatureDisabled(t *testing.T) {
	wds := &mockWritableDS{}
	r := newMutTestResolver(t, wds, func(res *Resolver) {
		disabledCfg := config.MutationsConfig{
			Enabled:        false,
			DatasourceName: "test_ds",
		}
		res.MutationConfig.Store(&disabledCfg)
	})
	ctx := ctxWithMutAuth([]string{"query", "mutation"}, nil)

	values := []*generated.ColumnValueInput{
		{Column: "user_id", Value: float64(1)},
	}

	_, err := r.InsertStarrocks(ctx, "orders", values)
	if err == nil {
		t.Fatal("expected error when feature is disabled")
	}
	if !strings.Contains(err.Error(), "disabled") {
		t.Errorf("expected FEATURE_DISABLED error, got: %v", err)
	}
}

func TestInsertStarrocks_Unauthorized_NoMutationPermission(t *testing.T) {
	wds := &mockWritableDS{}
	r := newMutTestResolver(t, wds)
	// Context with only "query" operations — no "mutation"
	ctx := ctxWithMutAuth([]string{"query"}, nil)

	values := []*generated.ColumnValueInput{
		{Column: "user_id", Value: float64(1)},
	}

	_, err := r.InsertStarrocks(ctx, "orders", values)
	if err == nil {
		t.Fatal("expected authorization error")
	}
	if !strings.Contains(err.Error(), "mutation operation not allowed") {
		t.Errorf("expected AUTH permission error, got: %v", err)
	}
}

func TestInsertStarrocks_InvalidTableName(t *testing.T) {
	wds := &mockWritableDS{}
	r := newMutTestResolver(t, wds)
	ctx := ctxWithMutAuth([]string{"query", "mutation"}, nil)

	values := []*generated.ColumnValueInput{
		{Column: "user_id", Value: float64(1)},
	}

	// Invalid table name starting with digit
	_, err := r.InsertStarrocks(ctx, "123invalid", values)
	if err == nil {
		t.Fatal("expected validation error for invalid table name")
	}
	if !strings.Contains(err.Error(), "invalid table name") {
		t.Errorf("expected invalid table name error, got: %v", err)
	}
}

func TestInsertStarrocks_TableNotInWhitelist(t *testing.T) {
	wds := &mockWritableDS{}
	r := newMutTestResolver(t, wds)
	ctx := ctxWithMutAuth([]string{"query", "mutation"}, nil)

	values := []*generated.ColumnValueInput{
		{Column: "user_id", Value: float64(1)},
	}

	// Valid identifier but not in writable_tables
	_, err := r.InsertStarrocks(ctx, "unknown_table", values)
	if err == nil {
		t.Fatal("expected whitelist validation error")
	}
	if !strings.Contains(err.Error(), "not in the writable tables whitelist") {
		t.Errorf("expected whitelist error, got: %v", err)
	}
}

func TestInsertStarrocks_RateLimited(t *testing.T) {
	wds := &mockWritableDS{}
	r := newMutTestResolver(t, wds, func(res *Resolver) {
		res.MutationRateLimiter = &mockMutRateLimiter{
			allowFunc: func(_ context.Context, _ string, _ int) (*ratelimit.RateLimitResult, error) {
				return &ratelimit.RateLimitResult{
					Allowed:   false,
					Limit:     20,
					Remaining: 0,
					ResetAt:   time.Now().Add(60 * time.Second),
				}, nil
			},
		}
	})
	ctx := ctxWithMutAuth([]string{"query", "mutation"}, nil)

	values := []*generated.ColumnValueInput{
		{Column: "user_id", Value: float64(1)},
	}

	_, err := r.InsertStarrocks(ctx, "orders", values)
	if err == nil {
		t.Fatal("expected rate limit error")
	}
	if !strings.Contains(err.Error(), "rate limit exceeded") {
		t.Errorf("expected rate limit error, got: %v", err)
	}
}

// --- UpdateStarrocks Tests ---

func TestUpdateStarrocks_Success(t *testing.T) {
	wds := &mockWritableDS{
		executeWriteFunc: func(_ context.Context, sql string, params []any) (int64, error) {
			if !strings.Contains(sql, "UPDATE") {
				t.Errorf("expected UPDATE in SQL, got %q", sql)
			}
			return 3, nil
		},
	}

	r := newMutTestResolver(t, wds)
	ctx := ctxWithMutAuth([]string{"query", "mutation"}, nil)

	set := []*generated.ColumnValueInput{
		{Column: "status", Value: "shipped"},
	}
	filter := []*generated.MutationFilterInput{
		{Field: "order_id", Operator: generated.FilterOperatorEq, Value: float64(100)},
	}

	result, err := r.UpdateStarrocks(ctx, "orders", set, filter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success=true")
	}
	if result.AffectedRows != 3 {
		t.Errorf("expected 3 affected rows, got %d", result.AffectedRows)
	}
}

func TestUpdateStarrocks_FeatureDisabled(t *testing.T) {
	wds := &mockWritableDS{}
	r := newMutTestResolver(t, wds, func(res *Resolver) {
		disabledCfg := config.MutationsConfig{
			Enabled:        false,
			DatasourceName: "test_ds",
		}
		res.MutationConfig.Store(&disabledCfg)
	})
	ctx := ctxWithMutAuth([]string{"query", "mutation"}, nil)

	set := []*generated.ColumnValueInput{
		{Column: "status", Value: "shipped"},
	}
	filter := []*generated.MutationFilterInput{
		{Field: "order_id", Operator: generated.FilterOperatorEq, Value: float64(1)},
	}

	_, err := r.UpdateStarrocks(ctx, "orders", set, filter)
	if err == nil {
		t.Fatal("expected error when feature is disabled")
	}
	if !strings.Contains(err.Error(), "disabled") {
		t.Errorf("expected FEATURE_DISABLED error, got: %v", err)
	}
}

// --- DeleteStarrocks Tests ---

func TestDeleteStarrocks_Success(t *testing.T) {
	wds := &mockWritableDS{
		executeWriteFunc: func(_ context.Context, sql string, params []any) (int64, error) {
			if !strings.Contains(sql, "DELETE FROM") {
				t.Errorf("expected DELETE FROM in SQL, got %q", sql)
			}
			return 5, nil
		},
	}

	r := newMutTestResolver(t, wds)
	ctx := ctxWithMutAuth([]string{"query", "mutation"}, nil)

	filter := []*generated.MutationFilterInput{
		{Field: "order_id", Operator: generated.FilterOperatorEq, Value: float64(42)},
	}

	result, err := r.DeleteStarrocks(ctx, "orders", filter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success=true")
	}
	if result.AffectedRows != 5 {
		t.Errorf("expected 5 affected rows, got %d", result.AffectedRows)
	}
}

func TestDeleteStarrocks_FeatureDisabled(t *testing.T) {
	wds := &mockWritableDS{}
	r := newMutTestResolver(t, wds, func(res *Resolver) {
		disabledCfg := config.MutationsConfig{
			Enabled:        false,
			DatasourceName: "test_ds",
		}
		res.MutationConfig.Store(&disabledCfg)
	})
	ctx := ctxWithMutAuth([]string{"query", "mutation"}, nil)

	filter := []*generated.MutationFilterInput{
		{Field: "order_id", Operator: generated.FilterOperatorEq, Value: float64(1)},
	}

	_, err := r.DeleteStarrocks(ctx, "orders", filter)
	if err == nil {
		t.Fatal("expected error when feature is disabled")
	}
	if !strings.Contains(err.Error(), "disabled") {
		t.Errorf("expected FEATURE_DISABLED error, got: %v", err)
	}
}

// --- InsertBatchStarrocks Tests ---

func TestInsertBatchStarrocks_Success(t *testing.T) {
	wds := &mockWritableDS{
		executeWriteFunc: func(_ context.Context, sql string, params []any) (int64, error) {
			if !strings.Contains(sql, "INSERT INTO") {
				t.Errorf("expected INSERT INTO in SQL, got %q", sql)
			}
			// Expect batch insert with multiple VALUES tuples
			if !strings.Contains(sql, "), (") {
				t.Errorf("expected batch format in SQL, got %q", sql)
			}
			return 3, nil
		},
	}

	r := newMutTestResolver(t, wds)
	ctx := ctxWithMutAuth([]string{"query", "mutation"}, nil)

	columns := []string{"user_id", "amount", "status"}
	rows := [][]any{
		{float64(1), float64(100), "pending"},
		{float64(2), float64(200), "active"},
		{float64(3), float64(300), "shipped"},
	}

	result, err := r.InsertBatchStarrocks(ctx, "orders", columns, rows)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success=true")
	}
	if result.AffectedRows != 3 {
		t.Errorf("expected 3 affected rows, got %d", result.AffectedRows)
	}
}

func TestInsertBatchStarrocks_BatchSizeExceeded(t *testing.T) {
	wds := &mockWritableDS{}
	r := newMutTestResolver(t, wds, func(res *Resolver) {
		// Set very small batch size for testing
		smallCfg := config.MutationsConfig{
			Enabled:         true,
			DatasourceName:  "test_ds",
			MaxAffectedRows: 1000,
			MaxBatchSize:    2, // Only allow 2 rows
			MaxSQLLength:    1048576,
		}
		res.MutationConfig.Store(&smallCfg)
	})
	ctx := ctxWithMutAuth([]string{"query", "mutation"}, nil)

	columns := []string{"user_id", "amount", "status"}
	rows := [][]any{
		{float64(1), float64(100), "a"},
		{float64(2), float64(200), "b"},
		{float64(3), float64(300), "c"},
	}

	_, err := r.InsertBatchStarrocks(ctx, "orders", columns, rows)
	if err == nil {
		t.Fatal("expected batch size exceeded error")
	}
	if !strings.Contains(err.Error(), "batch size") {
		t.Errorf("expected batch size error, got: %v", err)
	}
}

func TestInsertBatchStarrocks_SQLLengthExceeded(t *testing.T) {
	wds := &mockWritableDS{}
	r := newMutTestResolver(t, wds, func(res *Resolver) {
		// Set very small SQL length limit for testing
		smallCfg := config.MutationsConfig{
			Enabled:         true,
			DatasourceName:  "test_ds",
			MaxAffectedRows: 1000,
			MaxBatchSize:    500,
			MaxSQLLength:    10, // Extremely small limit
		}
		res.MutationConfig.Store(&smallCfg)
		res.MutationValidator = starrocks.NewMutationValidator(500, 10)
	})
	ctx := ctxWithMutAuth([]string{"query", "mutation"}, nil)

	columns := []string{"user_id", "amount", "status"}
	rows := [][]any{
		{float64(1), float64(100), "pending"},
	}

	_, err := r.InsertBatchStarrocks(ctx, "orders", columns, rows)
	if err == nil {
		t.Fatal("expected SQL length exceeded error")
	}
	if !strings.Contains(err.Error(), "SQL statement length") {
		t.Errorf("expected SQL length error, got: %v", err)
	}
}

// --- Cross-cutting error tests ---

func TestMutation_NoAuthIdentity(t *testing.T) {
	wds := &mockWritableDS{}
	r := newMutTestResolver(t, wds)
	// Context without any auth identity
	ctx := context.Background()

	values := []*generated.ColumnValueInput{
		{Column: "user_id", Value: float64(1)},
	}

	_, err := r.InsertStarrocks(ctx, "orders", values)
	if err == nil {
		t.Fatal("expected auth error when no identity in context")
	}
	if !strings.Contains(err.Error(), "authentication required") {
		t.Errorf("expected auth required error, got: %v", err)
	}
}

func TestMutation_DatasourceAccessDenied(t *testing.T) {
	wds := &mockWritableDS{}
	r := newMutTestResolver(t, wds)
	// Context with mutation permission but restricted to different datasource
	ctx := ctxWithMutAuth([]string{"query", "mutation"}, []string{"other_ds"})

	values := []*generated.ColumnValueInput{
		{Column: "user_id", Value: float64(1)},
	}

	_, err := r.InsertStarrocks(ctx, "orders", values)
	if err == nil {
		t.Fatal("expected auth error when datasource not in allowed list")
	}
	if !strings.Contains(err.Error(), "no access to datasource") {
		t.Errorf("expected datasource access error, got: %v", err)
	}
}

func TestMutation_ExecuteWriteError(t *testing.T) {
	wds := &mockWritableDS{
		executeWriteFunc: func(_ context.Context, _ string, _ []any) (int64, error) {
			return 0, apierrors.DatasourceError(apierrors.ErrDatasourceQueryError, "connection reset")
		},
	}

	r := newMutTestResolver(t, wds)
	ctx := ctxWithMutAuth([]string{"query", "mutation"}, nil)

	values := []*generated.ColumnValueInput{
		{Column: "user_id", Value: float64(1)},
	}

	_, err := r.InsertStarrocks(ctx, "orders", values)
	if err == nil {
		t.Fatal("expected error from ExecuteWrite failure")
	}
	if !strings.Contains(err.Error(), "connection reset") {
		t.Errorf("expected connection reset error, got: %v", err)
	}
}

func TestMutation_AffectedRowsWarning(t *testing.T) {
	wds := &mockWritableDS{
		executeWriteFunc: func(_ context.Context, _ string, _ []any) (int64, error) {
			return 1500, nil // Exceeds default max_affected_rows of 1000
		},
	}

	r := newMutTestResolver(t, wds)
	ctx := ctxWithMutAuth([]string{"query", "mutation"}, nil)

	filter := []*generated.MutationFilterInput{
		{Field: "order_id", Operator: generated.FilterOperatorGt, Value: float64(0)},
	}

	result, err := r.DeleteStarrocks(ctx, "orders", filter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success=true even with warning")
	}
	if result.AffectedRows != 1500 {
		t.Errorf("expected 1500 affected rows, got %d", result.AffectedRows)
	}
	if result.Warning == nil {
		t.Fatal("expected warning for affected rows exceeding threshold")
	}
	if !strings.Contains(*result.Warning, "exceeded threshold") {
		t.Errorf("expected exceeded threshold warning, got: %q", *result.Warning)
	}
}

func TestMutation_OperationNotAllowed(t *testing.T) {
	wds := &mockWritableDS{}
	r := newMutTestResolver(t, wds)
	ctx := ctxWithMutAuth([]string{"query", "mutation"}, nil)

	// "events" table only allows "insert" operation — delete should fail
	filter := []*generated.MutationFilterInput{
		{Field: "event_id", Operator: generated.FilterOperatorEq, Value: float64(1)},
	}

	_, err := r.DeleteStarrocks(ctx, "events", filter)
	if err == nil {
		t.Fatal("expected operation not supported error")
	}
	if !strings.Contains(err.Error(), "not allowed") {
		t.Errorf("expected operation not allowed error, got: %v", err)
	}
}

func TestMutation_RateLimiterError_FailsOpen(t *testing.T) {
	wds := &mockWritableDS{
		executeWriteFunc: func(_ context.Context, _ string, _ []any) (int64, error) {
			return 1, nil
		},
	}

	r := newMutTestResolver(t, wds, func(res *Resolver) {
		res.MutationRateLimiter = &mockMutRateLimiter{
			allowFunc: func(_ context.Context, _ string, _ int) (*ratelimit.RateLimitResult, error) {
				// Simulate rate limiter internal error
				return nil, errors.New("redis connection failed")
			},
		}
	})
	ctx := ctxWithMutAuth([]string{"query", "mutation"}, nil)

	values := []*generated.ColumnValueInput{
		{Column: "user_id", Value: float64(1)},
	}

	// Should succeed because rate limiter fails open
	result, err := r.InsertStarrocks(ctx, "orders", values)
	if err != nil {
		t.Fatalf("expected fail-open on rate limiter error, got: %v", err)
	}
	if !result.Success {
		t.Error("expected success=true when rate limiter fails open")
	}
}

func TestInsertStarrocks_ColumnNotInWhitelist(t *testing.T) {
	wds := &mockWritableDS{}
	r := newMutTestResolver(t, wds)
	ctx := ctxWithMutAuth([]string{"query", "mutation"}, nil)

	// "order_id" is not in writable columns for "orders" table
	values := []*generated.ColumnValueInput{
		{Column: "order_id", Value: float64(1)},
	}

	_, err := r.InsertStarrocks(ctx, "orders", values)
	if err == nil {
		t.Fatal("expected error for column not in writable whitelist")
	}
	if !strings.Contains(err.Error(), "not in the writable whitelist") {
		t.Errorf("expected column whitelist error, got: %v", err)
	}
}
