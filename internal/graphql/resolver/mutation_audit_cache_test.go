// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package resolver

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/michaelwang123/mountainKing/internal/adapter/starrocks"
	"github.com/michaelwang123/mountainKing/internal/audit"
	"github.com/michaelwang123/mountainKing/internal/config"
	ctxkeys "github.com/michaelwang123/mountainKing/internal/context"
	"github.com/michaelwang123/mountainKing/internal/datasource"
	"github.com/michaelwang123/mountainKing/internal/graphql/generated"
	"github.com/michaelwang123/mountainKing/internal/middleware"
	"github.com/michaelwang123/mountainKing/pkg/retry"
	"go.uber.org/zap"
)

// --- Audit/Cache test helpers ---

// trackingCacheClearer tracks calls to ClearByDatasource and ClearAll.
// Thread-safe for use in property tests with concurrent access.
type trackingCacheClearer struct {
	mu              sync.Mutex
	clearByDSCalled bool
	clearByDSArg    string
	clearAllCalled  bool
	err             error
}

func (tc *trackingCacheClearer) ClearByDatasource(_ context.Context, ds string) error {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.clearByDSCalled = true
	tc.clearByDSArg = ds
	return tc.err
}

func (tc *trackingCacheClearer) ClearAll(_ context.Context) error {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.clearAllCalled = true
	return tc.err
}

func (tc *trackingCacheClearer) wasClearByDSCalled() bool {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	return tc.clearByDSCalled
}

func (tc *trackingCacheClearer) getClearByDSArg() string {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	return tc.clearByDSArg
}

// newAuditTestResolver creates a resolver with a file-based audit logger and tracking cache clearer.
// Returns the resolver and the path to the audit log file.
func newAuditTestResolver(t *testing.T, wds *mockWritableDS, cacheClearer *trackingCacheClearer) (*mutationResolver, string) {
	t.Helper()

	dir := t.TempDir()
	auditFile := filepath.Join(dir, "audit.log")

	al, err := audit.NewAuditLogger(config.AuditConfig{
		Enabled:  true,
		Output:   "file",
		FilePath: auditFile,
	})
	if err != nil {
		t.Fatalf("failed to create audit logger: %v", err)
	}
	t.Cleanup(func() { _ = al.Close() })

	mutCfg := config.MutationsConfig{
		Enabled:         true,
		DatasourceName:  "test_ds",
		MaxAffectedRows: 1000,
		MaxBatchSize:    500,
		MaxSQLLength:    1048576,
	}
	cfgPtr := &atomic.Pointer[config.MutationsConfig]{}
	cfgPtr.Store(&mutCfg)

	registry := datasource.NewAdapterRegistry()
	_ = registry.Register("mock_writable", func(name string, cfg datasource.DataSourceConfig) (datasource.DataSource, error) {
		if wds == nil {
			return nil, errors.New("no mock configured")
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
		MutationRateLimiter:    &mockMutRateLimiter{},
		AuditLogger:            al,
		CacheClearer:           cacheClearer,
		Logger:                 zap.NewNop(),
	}

	return &mutationResolver{r}, auditFile
}

// readAuditEntries reads and parses the audit log file into JSON maps.
func readAuditEntries(t *testing.T, filePath string) []map[string]any {
	t.Helper()

	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read audit file: %v", err)
	}

	var entries []map[string]any
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("failed to parse audit line: %v\nline: %s", err, line)
		}
		entries = append(entries, m)
	}
	return entries
}

// ctxWithMutAuthPrincipal returns a context with auth identity using a specific principal.
func ctxWithMutAuthPrincipal(principal string, operations []string, datasources []string) context.Context {
	identity := &middleware.AuthIdentity{
		Subject:     principal,
		Method:      "jwt",
		Operations:  operations,
		Datasources: datasources,
	}
	return context.WithValue(context.Background(), ctxkeys.CtxKeyAuthIdentity, identity)
}

// --- Audit Tests: Success Cases ---

// Validates: Requirements 6.1, 6.4, 6.5
func TestAudit_InsertSuccess_LogsCorrectFields(t *testing.T) {
	wds := &mockWritableDS{
		executeWriteFunc: func(_ context.Context, _ string, _ []any) (int64, error) {
			return 1, nil
		},
	}
	cache := &trackingCacheClearer{}
	r, auditFile := newAuditTestResolver(t, wds, cache)

	ctx := ctxWithMutAuthPrincipal("admin-user", []string{"query", "mutation"}, nil)
	values := []*generated.ColumnValueInput{
		{Column: "user_id", Value: float64(42)},
	}

	result, err := r.InsertStarrocks(ctx, "orders", values)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatal("expected success")
	}

	_ = r.AuditLogger.Sync()

	entries := readAuditEntries(t, auditFile)
	if len(entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(entries))
	}

	entry := entries[0]
	if entry["principal"] != "admin-user" {
		t.Errorf("expected principal=admin-user, got %v", entry["principal"])
	}
	if entry["operation"] != "insert" {
		t.Errorf("expected operation=insert, got %v", entry["operation"])
	}
	if entry["datasource"] != "test_ds" {
		t.Errorf("expected datasource=test_ds, got %v", entry["datasource"])
	}
	if entry["result"] != "success" {
		t.Errorf("expected result=success, got %v", entry["result"])
	}
	if entry["affected_rows"] != "1" {
		t.Errorf("expected affected_rows=1, got %v", entry["affected_rows"])
	}
	if entry["table"] != "orders" {
		t.Errorf("expected table=orders, got %v", entry["table"])
	}
}

// Validates: Requirements 6.1, 6.4
func TestAudit_UpdateSuccess_LogsAffectedRows(t *testing.T) {
	wds := &mockWritableDS{
		executeWriteFunc: func(_ context.Context, _ string, _ []any) (int64, error) {
			return 5, nil
		},
	}
	cache := &trackingCacheClearer{}
	r, auditFile := newAuditTestResolver(t, wds, cache)

	ctx := ctxWithMutAuthPrincipal("updater-user", []string{"query", "mutation"}, nil)
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
		t.Fatal("expected success")
	}

	_ = r.AuditLogger.Sync()

	entries := readAuditEntries(t, auditFile)
	if len(entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(entries))
	}

	entry := entries[0]
	if entry["principal"] != "updater-user" {
		t.Errorf("expected principal=updater-user, got %v", entry["principal"])
	}
	if entry["operation"] != "update" {
		t.Errorf("expected operation=update, got %v", entry["operation"])
	}
	if entry["result"] != "success" {
		t.Errorf("expected result=success, got %v", entry["result"])
	}
	if entry["affected_rows"] != "5" {
		t.Errorf("expected affected_rows=5, got %v", entry["affected_rows"])
	}
}

// Validates: Requirements 6.1, 6.4
func TestAudit_DeleteSuccess_LogsAffectedRows(t *testing.T) {
	wds := &mockWritableDS{
		executeWriteFunc: func(_ context.Context, _ string, _ []any) (int64, error) {
			return 10, nil
		},
	}
	cache := &trackingCacheClearer{}
	r, auditFile := newAuditTestResolver(t, wds, cache)

	ctx := ctxWithMutAuthPrincipal("delete-user", []string{"query", "mutation"}, nil)
	filter := []*generated.MutationFilterInput{
		{Field: "order_id", Operator: generated.FilterOperatorGt, Value: float64(0)},
	}

	result, err := r.DeleteStarrocks(ctx, "orders", filter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatal("expected success")
	}

	_ = r.AuditLogger.Sync()

	entries := readAuditEntries(t, auditFile)
	if len(entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(entries))
	}

	entry := entries[0]
	if entry["principal"] != "delete-user" {
		t.Errorf("expected principal=delete-user, got %v", entry["principal"])
	}
	if entry["operation"] != "delete" {
		t.Errorf("expected operation=delete, got %v", entry["operation"])
	}
	if entry["result"] != "success" {
		t.Errorf("expected result=success, got %v", entry["result"])
	}
	if entry["affected_rows"] != "10" {
		t.Errorf("expected affected_rows=10, got %v", entry["affected_rows"])
	}
}

// --- Audit Tests: Failure Cases ---

// Validates: Requirements 6.2
func TestAudit_AuthFailure_LogsAuthorizationDenied(t *testing.T) {
	wds := &mockWritableDS{}
	cache := &trackingCacheClearer{}
	r, auditFile := newAuditTestResolver(t, wds, cache)

	// Context with only "query" permissions �?no "mutation"
	ctx := ctxWithMutAuthPrincipal("readonly-user", []string{"query"}, nil)
	values := []*generated.ColumnValueInput{
		{Column: "user_id", Value: float64(1)},
	}

	_, err := r.InsertStarrocks(ctx, "orders", values)
	if err == nil {
		t.Fatal("expected authorization error")
	}

	_ = r.AuditLogger.Sync()

	entries := readAuditEntries(t, auditFile)
	if len(entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(entries))
	}

	entry := entries[0]
	if entry["principal"] != "readonly-user" {
		t.Errorf("expected principal=readonly-user, got %v", entry["principal"])
	}
	if entry["result"] != "failure" {
		t.Errorf("expected result=failure, got %v", entry["result"])
	}
	if entry["reason"] != "authorization_denied" {
		t.Errorf("expected reason=authorization_denied, got %v", entry["reason"])
	}
	if entry["operation"] != "insert" {
		t.Errorf("expected operation=insert, got %v", entry["operation"])
	}
}

// Validates: Requirements 6.2
func TestAudit_DatasourceAccessDenied_LogsAuthorizationDenied(t *testing.T) {
	wds := &mockWritableDS{}
	cache := &trackingCacheClearer{}
	r, auditFile := newAuditTestResolver(t, wds, cache)

	// Has mutation permission but restricted to a different datasource.
	ctx := ctxWithMutAuthPrincipal("restricted-user", []string{"query", "mutation"}, []string{"other_ds"})
	set := []*generated.ColumnValueInput{
		{Column: "status", Value: "cancelled"},
	}
	filter := []*generated.MutationFilterInput{
		{Field: "order_id", Operator: generated.FilterOperatorEq, Value: float64(5)},
	}

	_, err := r.UpdateStarrocks(ctx, "orders", set, filter)
	if err == nil {
		t.Fatal("expected authorization error")
	}

	_ = r.AuditLogger.Sync()

	entries := readAuditEntries(t, auditFile)
	if len(entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(entries))
	}

	entry := entries[0]
	if entry["result"] != "failure" {
		t.Errorf("expected result=failure, got %v", entry["result"])
	}
	if entry["reason"] != "authorization_denied" {
		t.Errorf("expected reason=authorization_denied, got %v", entry["reason"])
	}
}

// Validates: Requirements 6.3
func TestAudit_ValidationFailure_LogsValidationFailed(t *testing.T) {
	wds := &mockWritableDS{}
	cache := &trackingCacheClearer{}
	r, auditFile := newAuditTestResolver(t, wds, cache)

	ctx := ctxWithMutAuthPrincipal("user-xyz", []string{"query", "mutation"}, nil)

	// Invalid table name (starts with digit) �?validation failure.
	values := []*generated.ColumnValueInput{
		{Column: "user_id", Value: float64(1)},
	}

	_, err := r.InsertStarrocks(ctx, "123bad_table", values)
	if err == nil {
		t.Fatal("expected validation error")
	}

	_ = r.AuditLogger.Sync()

	entries := readAuditEntries(t, auditFile)
	if len(entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(entries))
	}

	entry := entries[0]
	if entry["result"] != "failure" {
		t.Errorf("expected result=failure, got %v", entry["result"])
	}
	if entry["reason"] != "validation_failed" {
		t.Errorf("expected reason=validation_failed, got %v", entry["reason"])
	}
	if entry["principal"] != "user-xyz" {
		t.Errorf("expected principal=user-xyz, got %v", entry["principal"])
	}
}

// Validates: Requirements 6.3
func TestAudit_WhitelistFailure_LogsValidationFailed(t *testing.T) {
	wds := &mockWritableDS{}
	cache := &trackingCacheClearer{}
	r, auditFile := newAuditTestResolver(t, wds, cache)

	ctx := ctxWithMutAuthPrincipal("user-abc", []string{"query", "mutation"}, nil)

	// Table is valid identifier but not in writable_tables �?whitelist failure.
	values := []*generated.ColumnValueInput{
		{Column: "user_id", Value: float64(1)},
	}

	_, err := r.InsertStarrocks(ctx, "nonexistent_table", values)
	if err == nil {
		t.Fatal("expected whitelist error")
	}

	_ = r.AuditLogger.Sync()

	entries := readAuditEntries(t, auditFile)
	if len(entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(entries))
	}

	entry := entries[0]
	if entry["result"] != "failure" {
		t.Errorf("expected result=failure, got %v", entry["result"])
	}
	if entry["reason"] != "validation_failed" {
		t.Errorf("expected reason=validation_failed, got %v", entry["reason"])
	}
}

// --- Cache Invalidation Tests ---

// Validates: Requirements 7.1
func TestCache_SuccessfulInsert_ClearByDatasourceCalled(t *testing.T) {
	wds := &mockWritableDS{
		executeWriteFunc: func(_ context.Context, _ string, _ []any) (int64, error) {
			return 1, nil
		},
	}
	cache := &trackingCacheClearer{}
	r, _ := newAuditTestResolver(t, wds, cache)

	ctx := ctxWithMutAuthPrincipal("test-user", []string{"query", "mutation"}, nil)
	values := []*generated.ColumnValueInput{
		{Column: "user_id", Value: float64(1)},
	}

	_, err := r.InsertStarrocks(ctx, "orders", values)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !cache.clearByDSCalled {
		t.Error("expected ClearByDatasource to be called on successful mutation")
	}
	if cache.clearByDSArg != "test_ds" {
		t.Errorf("expected ClearByDatasource called with 'test_ds', got %q", cache.clearByDSArg)
	}
}

// Validates: Requirements 7.1
func TestCache_SuccessfulUpdate_ClearByDatasourceCalled(t *testing.T) {
	wds := &mockWritableDS{
		executeWriteFunc: func(_ context.Context, _ string, _ []any) (int64, error) {
			return 2, nil
		},
	}
	cache := &trackingCacheClearer{}
	r, _ := newAuditTestResolver(t, wds, cache)

	ctx := ctxWithMutAuthPrincipal("test-user", []string{"query", "mutation"}, nil)
	set := []*generated.ColumnValueInput{
		{Column: "status", Value: "completed"},
	}
	filter := []*generated.MutationFilterInput{
		{Field: "order_id", Operator: generated.FilterOperatorEq, Value: float64(10)},
	}

	_, err := r.UpdateStarrocks(ctx, "orders", set, filter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !cache.clearByDSCalled {
		t.Error("expected ClearByDatasource to be called on successful update")
	}
	if cache.clearByDSArg != "test_ds" {
		t.Errorf("expected ClearByDatasource called with 'test_ds', got %q", cache.clearByDSArg)
	}
}

// Validates: Requirements 7.1
func TestCache_SuccessfulDelete_ClearByDatasourceCalled(t *testing.T) {
	wds := &mockWritableDS{
		executeWriteFunc: func(_ context.Context, _ string, _ []any) (int64, error) {
			return 3, nil
		},
	}
	cache := &trackingCacheClearer{}
	r, _ := newAuditTestResolver(t, wds, cache)

	ctx := ctxWithMutAuthPrincipal("test-user", []string{"query", "mutation"}, nil)
	filter := []*generated.MutationFilterInput{
		{Field: "order_id", Operator: generated.FilterOperatorLt, Value: float64(100)},
	}

	_, err := r.DeleteStarrocks(ctx, "orders", filter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !cache.clearByDSCalled {
		t.Error("expected ClearByDatasource to be called on successful delete")
	}
	if cache.clearByDSArg != "test_ds" {
		t.Errorf("expected ClearByDatasource called with 'test_ds', got %q", cache.clearByDSArg)
	}
}

// Validates: Requirements 7.2
func TestCache_FailedMutation_CacheNotCleared(t *testing.T) {
	wds := &mockWritableDS{}
	cache := &trackingCacheClearer{}
	r, _ := newAuditTestResolver(t, wds, cache)

	// Auth failure �?no "mutation" permission.
	ctx := ctxWithMutAuthPrincipal("readonly-user", []string{"query"}, nil)
	values := []*generated.ColumnValueInput{
		{Column: "user_id", Value: float64(1)},
	}

	_, err := r.InsertStarrocks(ctx, "orders", values)
	if err == nil {
		t.Fatal("expected error")
	}

	if cache.clearByDSCalled {
		t.Error("expected ClearByDatasource NOT to be called on failed mutation")
	}
	if cache.clearAllCalled {
		t.Error("expected ClearAll NOT to be called on failed mutation")
	}
}

// Validates: Requirements 7.2
func TestCache_ValidationFailure_CacheNotCleared(t *testing.T) {
	wds := &mockWritableDS{}
	cache := &trackingCacheClearer{}
	r, _ := newAuditTestResolver(t, wds, cache)

	ctx := ctxWithMutAuthPrincipal("test-user", []string{"query", "mutation"}, nil)
	values := []*generated.ColumnValueInput{
		{Column: "user_id", Value: float64(1)},
	}

	_, err := r.InsertStarrocks(ctx, "123invalid", values)
	if err == nil {
		t.Fatal("expected validation error")
	}

	if cache.clearByDSCalled {
		t.Error("expected ClearByDatasource NOT to be called on validation failure")
	}
}

// Validates: Requirements 7.3
func TestCache_ClearError_MutationStillSucceeds(t *testing.T) {
	wds := &mockWritableDS{
		executeWriteFunc: func(_ context.Context, _ string, _ []any) (int64, error) {
			return 1, nil
		},
	}
	cache := &trackingCacheClearer{err: errors.New("redis connection refused")}
	r, _ := newAuditTestResolver(t, wds, cache)

	ctx := ctxWithMutAuthPrincipal("test-user", []string{"query", "mutation"}, nil)
	values := []*generated.ColumnValueInput{
		{Column: "user_id", Value: float64(42)},
	}

	result, err := r.InsertStarrocks(ctx, "orders", values)
	if err != nil {
		t.Fatalf("expected success despite cache clear error, got: %v", err)
	}
	if !result.Success {
		t.Error("expected success=true even when cache clear fails")
	}
	if result.AffectedRows != 1 {
		t.Errorf("expected 1 affected row, got %d", result.AffectedRows)
	}
	if !cache.clearByDSCalled {
		t.Error("expected ClearByDatasource to be attempted")
	}
}

// Validates: Requirements 7.3
func TestCache_ClearError_UpdateStillSucceeds(t *testing.T) {
	wds := &mockWritableDS{
		executeWriteFunc: func(_ context.Context, _ string, _ []any) (int64, error) {
			return 2, nil
		},
	}
	cache := &trackingCacheClearer{err: errors.New("cache timeout")}
	r, _ := newAuditTestResolver(t, wds, cache)

	ctx := ctxWithMutAuthPrincipal("test-user", []string{"query", "mutation"}, nil)
	set := []*generated.ColumnValueInput{
		{Column: "status", Value: "delivered"},
	}
	filter := []*generated.MutationFilterInput{
		{Field: "order_id", Operator: generated.FilterOperatorEq, Value: float64(5)},
	}

	result, err := r.UpdateStarrocks(ctx, "orders", set, filter)
	if err != nil {
		t.Fatalf("expected success despite cache clear error, got: %v", err)
	}
	if !result.Success {
		t.Error("expected success=true")
	}
	if result.AffectedRows != 2 {
		t.Errorf("expected 2 affected rows, got %d", result.AffectedRows)
	}
}

// Validates: Requirements 7.2
func TestCache_ExecuteWriteError_CacheNotCleared(t *testing.T) {
	wds := &mockWritableDS{
		executeWriteFunc: func(_ context.Context, _ string, _ []any) (int64, error) {
			return 0, errors.New("database unavailable")
		},
	}
	cache := &trackingCacheClearer{}
	r, _ := newAuditTestResolver(t, wds, cache)

	ctx := ctxWithMutAuthPrincipal("test-user", []string{"query", "mutation"}, nil)
	values := []*generated.ColumnValueInput{
		{Column: "user_id", Value: float64(1)},
	}

	_, err := r.InsertStarrocks(ctx, "orders", values)
	if err == nil {
		t.Fatal("expected error")
	}

	if cache.clearByDSCalled {
		t.Error("expected ClearByDatasource NOT to be called when write fails")
	}
}

// --- Nil AuditLogger / Nil CacheClearer tests ---

func TestAudit_NilAuditLogger_MutationStillSucceeds(t *testing.T) {
	wds := &mockWritableDS{
		executeWriteFunc: func(_ context.Context, _ string, _ []any) (int64, error) {
			return 1, nil
		},
	}

	mutCfg := config.MutationsConfig{
		Enabled:         true,
		DatasourceName:  "test_ds",
		MaxAffectedRows: 1000,
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
		"orders": {"order_id": true, "user_id": true, "amount": true, "status": true},
	}
	wtv := starrocks.NewWritableTableValidator(writableTables, allowedTables)

	r := &mutationResolver{&Resolver{
		DSManager:              mgr,
		MutationSQLBuilder:     &starrocks.MutationSQLBuilder{},
		MutationValidator:      starrocks.NewMutationValidator(500, 1048576),
		WritableTableValidator: wtv,
		MutationConfig:         cfgPtr,
		MutationRateLimiter:    &mockMutRateLimiter{},
		AuditLogger:            nil,
		CacheClearer:           &trackingCacheClearer{},
	}}

	ctx := ctxWithMutAuthPrincipal("user", []string{"query", "mutation"}, nil)
	values := []*generated.ColumnValueInput{
		{Column: "user_id", Value: float64(1)},
	}

	result, err := r.InsertStarrocks(ctx, "orders", values)
	if err != nil {
		t.Fatalf("unexpected error with nil audit logger: %v", err)
	}
	if !result.Success {
		t.Error("expected success=true")
	}
}

func TestCache_NilCacheClearer_MutationStillSucceeds(t *testing.T) {
	wds := &mockWritableDS{
		executeWriteFunc: func(_ context.Context, _ string, _ []any) (int64, error) {
			return 1, nil
		},
	}
	r, _ := newAuditTestResolver(t, wds, nil)
	r.CacheClearer = nil

	ctx := ctxWithMutAuthPrincipal("test-user", []string{"query", "mutation"}, nil)
	values := []*generated.ColumnValueInput{
		{Column: "user_id", Value: float64(1)},
	}

	result, err := r.InsertStarrocks(ctx, "orders", values)
	if err != nil {
		t.Fatalf("unexpected error with nil cache clearer: %v", err)
	}
	if !result.Success {
		t.Error("expected success=true")
	}
}

// --- Audit Tests: BatchInsert ---

// Validates: Requirements 6.1, 6.4
func TestAudit_BatchInsertSuccess_LogsCorrectFields(t *testing.T) {
	wds := &mockWritableDS{
		executeWriteFunc: func(_ context.Context, _ string, _ []any) (int64, error) {
			return 3, nil
		},
	}
	cache := &trackingCacheClearer{}
	r, auditFile := newAuditTestResolver(t, wds, cache)

	ctx := ctxWithMutAuthPrincipal("batch-user", []string{"query", "mutation"}, nil)
	columns := []string{"user_id", "amount", "status"}
	rows := [][]any{
		{float64(1), float64(100), "a"},
		{float64(2), float64(200), "b"},
		{float64(3), float64(300), "c"},
	}

	result, err := r.InsertBatchStarrocks(ctx, "orders", columns, rows)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatal("expected success")
	}

	_ = r.AuditLogger.Sync()

	entries := readAuditEntries(t, auditFile)
	if len(entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(entries))
	}

	entry := entries[0]
	if entry["principal"] != "batch-user" {
		t.Errorf("expected principal=batch-user, got %v", entry["principal"])
	}
	if entry["operation"] != "insertBatch" {
		t.Errorf("expected operation=insertBatch, got %v", entry["operation"])
	}
	if entry["result"] != "success" {
		t.Errorf("expected result=success, got %v", entry["result"])
	}
	if entry["affected_rows"] != "3" {
		t.Errorf("expected affected_rows=3, got %v", entry["affected_rows"])
	}
	if entry["datasource"] != "test_ds" {
		t.Errorf("expected datasource=test_ds, got %v", entry["datasource"])
	}
}

// Validates: Requirements 7.1
func TestCache_BatchInsertSuccess_ClearByDatasourceCalled(t *testing.T) {
	wds := &mockWritableDS{
		executeWriteFunc: func(_ context.Context, _ string, _ []any) (int64, error) {
			return 3, nil
		},
	}
	cache := &trackingCacheClearer{}
	r, _ := newAuditTestResolver(t, wds, cache)

	ctx := ctxWithMutAuthPrincipal("test-user", []string{"query", "mutation"}, nil)
	columns := []string{"user_id", "amount", "status"}
	rows := [][]any{
		{float64(1), float64(100), "a"},
		{float64(2), float64(200), "b"},
		{float64(3), float64(300), "c"},
	}

	_, err := r.InsertBatchStarrocks(ctx, "orders", columns, rows)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !cache.clearByDSCalled {
		t.Error("expected ClearByDatasource to be called on successful batch insert")
	}
	if cache.clearByDSArg != "test_ds" {
		t.Errorf("expected ClearByDatasource called with 'test_ds', got %q", cache.clearByDSArg)
	}
}
