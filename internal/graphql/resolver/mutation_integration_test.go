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
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/michaelwang123/mountainKing/internal/adapter/starrocks"
	"github.com/michaelwang123/mountainKing/internal/audit"
	"github.com/michaelwang123/mountainKing/internal/config"
	ctxkeys "github.com/michaelwang123/mountainKing/internal/context"
	"github.com/michaelwang123/mountainKing/internal/datasource"
	"github.com/michaelwang123/mountainKing/internal/graphql/generated"
	"github.com/michaelwang123/mountainKing/internal/middleware"
	"github.com/michaelwang123/mountainKing/internal/ratelimit"
	"github.com/michaelwang123/mountainKing/pkg/retry"
)

// errIntegCacheDown simulates a cache infrastructure failure.
var errIntegCacheDown = errors.New("redis connection refused")

// --- Integration test helpers (unique names to avoid redeclaration) ---

// integCacheClearer tracks cache clearing calls in integration tests.
type integCacheClearer struct {
	clearByDSCalled bool
	clearByDSArg    string
	clearAllCalled  bool
	err             error
}

func (c *integCacheClearer) ClearByDatasource(_ context.Context, ds string) error {
	c.clearByDSCalled = true
	c.clearByDSArg = ds
	return c.err
}

func (c *integCacheClearer) ClearAll(_ context.Context) error {
	c.clearAllCalled = true
	return c.err
}

// integWritableDS mocks a writable datasource for integration tests.
type integWritableDS struct {
	name             string
	typ              string
	executeWriteFunc func(ctx context.Context, sql string, params []any) (int64, error)
	executeCalled    bool
	lastSQL          string
	lastParams       []any
}

func (m *integWritableDS) Name() string                    { return m.name }
func (m *integWritableDS) Type() string                    { return m.typ }
func (m *integWritableDS) IsAvailable() bool               { return true }
func (m *integWritableDS) SchemaFiles() []string           { return nil }
func (m *integWritableDS) Connect(_ context.Context) error { return nil }
func (m *integWritableDS) Execute(_ context.Context, _ datasource.QueryRequest) (*datasource.QueryResult, error) {
	return &datasource.QueryResult{}, nil
}
func (m *integWritableDS) HealthCheck(_ context.Context) error { return nil }
func (m *integWritableDS) Close(_ context.Context) error       { return nil }
func (m *integWritableDS) ExecuteWrite(ctx context.Context, sql string, params []any) (int64, error) {
	m.executeCalled = true
	m.lastSQL = sql
	m.lastParams = params
	if m.executeWriteFunc != nil {
		return m.executeWriteFunc(ctx, sql, params)
	}
	return 0, nil
}

var _ datasource.WritableDataSource = (*integWritableDS)(nil)

// integRateLimiter is a configurable rate limiter for integration tests.
type integRateLimiter struct {
	allowed bool
	err     error
}

func (r *integRateLimiter) Allow(_ context.Context, _ string, _ int) (*ratelimit.RateLimitResult, error) {
	if r.err != nil {
		return nil, r.err
	}
	return &ratelimit.RateLimitResult{
		Allowed:   r.allowed,
		Limit:     20,
		Remaining: 19,
		ResetAt:   time.Now().Add(60 * time.Second),
	}, nil
}

// integTestSetup holds the full integration test setup.
type integTestSetup struct {
	resolver    *mutationResolver
	wds         *integWritableDS
	cache       *integCacheClearer
	rateLimiter *integRateLimiter
	auditFile   string
}

// newIntegrationResolver creates a full resolver with all mutation components wired for end-to-end testing.
func newIntegrationResolver(t *testing.T) *integTestSetup {
	t.Helper()

	// 1. Create audit logger writing to temp file.
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

	// 2. Create mock writable datasource.
	wds := &integWritableDS{
		name: "analytics_db",
		typ:  "integ_mock",
		executeWriteFunc: func(_ context.Context, _ string, _ []any) (int64, error) {
			return 1, nil
		},
	}

	// 3. Build DataSourceManager with the mock.
	registry := datasource.NewAdapterRegistry()
	_ = registry.Register("integ_mock", func(name string, cfg datasource.DataSourceConfig) (datasource.DataSource, error) {
		wds.name = name
		wds.typ = "integ_mock"
		return wds, nil
	})
	configs := []config.DataSourceConfig{
		{Name: "analytics_db", Type: "integ_mock", Enabled: true},
	}
	retryCfg := retry.Config{MaxRetries: 0}
	mgr := datasource.NewDataSourceManager(registry, configs, retryCfg, zap.NewNop())
	_ = mgr.Init(context.Background())

	// 4. Mutation config (enabled, targeting analytics_db).
	mutCfg := config.MutationsConfig{
		Enabled:         true,
		DatasourceName:  "analytics_db",
		MaxAffectedRows: 1000,
		MaxBatchSize:    500,
		MaxSQLLength:    1048576,
	}
	cfgPtr := &atomic.Pointer[config.MutationsConfig]{}
	cfgPtr.Store(&mutCfg)

	// 5. Writable table validator �?full whitelist setup.
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

	// 6. Cache clearer.
	cache := &integCacheClearer{}

	// 7. Rate limiter (allowed by default).
	rl := &integRateLimiter{allowed: true}

	// 8. Wire everything into the Resolver.
	r := &Resolver{
		DSManager:              mgr,
		MutationSQLBuilder:     &starrocks.MutationSQLBuilder{},
		MutationValidator:      starrocks.NewMutationValidator(500, 1048576),
		WritableTableValidator: wtv,
		MutationConfig:         cfgPtr,
		MutationRateLimiter:    rl,
		AuditLogger:            al,
		CacheClearer:           cache,
		Logger:                 zap.NewNop(),
	}

	return &integTestSetup{
		resolver:    &mutationResolver{r},
		wds:         wds,
		cache:       cache,
		rateLimiter: rl,
		auditFile:   auditFile,
	}
}

// integCtx creates a context with the given auth identity for integration tests.
func integCtx(principal string, operations []string, datasources []string) context.Context {
	identity := &middleware.AuthIdentity{
		Subject:     principal,
		Method:      "jwt",
		Operations:  operations,
		Datasources: datasources,
	}
	return context.WithValue(context.Background(), ctxkeys.CtxKeyAuthIdentity, identity)
}

// readIntegAuditEntries reads and parses the audit log file into JSON maps.
func readIntegAuditEntries(t *testing.T, filePath string) []map[string]any {
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

// --- Integration Tests: Full Mutation Flow ---

// TestIntegration_InsertStarrocks_FullFlow exercises the entire mutation pipeline:
// feature enabled �?rate limit passes �?auth passes �?validation passes �?whitelist passes
// �?SQL built �?length valid �?execution succeeds �?warning check �?audit �?cache �?result
//
// Validates: Requirements 1.1, 1.4, 5.1, 6.1, 7.1
func TestIntegration_InsertStarrocks_FullFlow(t *testing.T) {
	setup := newIntegrationResolver(t)

	// Configure the mock to return 3 affected rows.
	setup.wds.executeWriteFunc = func(_ context.Context, sql string, params []any) (int64, error) {
		// Verify the SQL is a parameterized INSERT.
		if !strings.Contains(sql, "INSERT INTO") {
			t.Errorf("expected INSERT INTO in SQL, got %q", sql)
		}
		if !strings.Contains(sql, "`orders`") {
			t.Errorf("expected backtick-quoted table name in SQL, got %q", sql)
		}
		// Verify placeholders match param count.
		placeholders := strings.Count(sql, "?")
		if placeholders != len(params) {
			t.Errorf("placeholder count (%d) != param count (%d)", placeholders, len(params))
		}
		return 3, nil
	}

	ctx := integCtx("admin-user", []string{"query", "mutation"}, nil)
	values := []*generated.ColumnValueInput{
		{Column: "user_id", Value: float64(42)},
		{Column: "amount", Value: float64(99.99)},
		{Column: "status", Value: "pending"},
	}

	result, err := setup.resolver.InsertStarrocks(ctx, "orders", values)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify result.
	if !result.Success {
		t.Error("expected success=true")
	}
	if result.AffectedRows != 3 {
		t.Errorf("expected 3 affected rows, got %d", result.AffectedRows)
	}
	if result.Warning != nil {
		t.Errorf("expected no warning (3 < 1000), got %q", *result.Warning)
	}

	// Verify execution was called.
	if !setup.wds.executeCalled {
		t.Error("expected ExecuteWrite to be called")
	}

	// Verify cache was invalidated.
	if !setup.cache.clearByDSCalled {
		t.Error("expected ClearByDatasource to be called after successful mutation")
	}
	if setup.cache.clearByDSArg != "analytics_db" {
		t.Errorf("expected cache clear for 'analytics_db', got %q", setup.cache.clearByDSArg)
	}

	// Verify audit was logged.
	_ = setup.resolver.AuditLogger.Sync()
	entries := readIntegAuditEntries(t, setup.auditFile)
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
	if entry["datasource"] != "analytics_db" {
		t.Errorf("expected datasource=analytics_db, got %v", entry["datasource"])
	}
	if entry["result"] != "success" {
		t.Errorf("expected result=success, got %v", entry["result"])
	}
	if entry["affected_rows"] != "3" {
		t.Errorf("expected affected_rows=3, got %v", entry["affected_rows"])
	}
}

// TestIntegration_FeatureDisabled_RejectsAll verifies the feature toggle rejects mutations
// when mutations.enabled=false.
//
// Validates: Requirements 9.4, 10.4
func TestIntegration_FeatureDisabled_RejectsAll(t *testing.T) {
	setup := newIntegrationResolver(t)

	// Disable mutations.
	disabledCfg := config.MutationsConfig{
		Enabled:         false,
		DatasourceName:  "analytics_db",
		MaxAffectedRows: 1000,
		MaxBatchSize:    500,
		MaxSQLLength:    1048576,
	}
	setup.resolver.MutationConfig.Store(&disabledCfg)

	ctx := integCtx("admin-user", []string{"query", "mutation"}, nil)
	values := []*generated.ColumnValueInput{
		{Column: "user_id", Value: float64(1)},
	}

	_, err := setup.resolver.InsertStarrocks(ctx, "orders", values)
	if err == nil {
		t.Fatal("expected error when feature is disabled")
	}
	if !strings.Contains(err.Error(), "disabled") {
		t.Errorf("expected 'disabled' in error message, got: %v", err)
	}

	// Verify no execution, no cache clear, no audit.
	if setup.wds.executeCalled {
		t.Error("expected ExecuteWrite NOT to be called when feature is disabled")
	}
	if setup.cache.clearByDSCalled {
		t.Error("expected cache NOT to be cleared when feature is disabled")
	}
}

// TestIntegration_RateLimitExceeded verifies the mutation-specific rate limiter rejects
// when the limit is exceeded.
//
// Validates: Requirements 9.7, 10.7
func TestIntegration_RateLimitExceeded(t *testing.T) {
	setup := newIntegrationResolver(t)

	// Configure rate limiter to deny.
	setup.rateLimiter.allowed = false

	ctx := integCtx("admin-user", []string{"query", "mutation"}, nil)
	values := []*generated.ColumnValueInput{
		{Column: "user_id", Value: float64(1)},
	}

	_, err := setup.resolver.InsertStarrocks(ctx, "orders", values)
	if err == nil {
		t.Fatal("expected rate limit error")
	}
	if !strings.Contains(err.Error(), "rate limit exceeded") {
		t.Errorf("expected rate limit error message, got: %v", err)
	}

	// Verify no execution.
	if setup.wds.executeCalled {
		t.Error("expected ExecuteWrite NOT to be called when rate limited")
	}
	if setup.cache.clearByDSCalled {
		t.Error("expected cache NOT to be cleared when rate limited")
	}
}

// TestIntegration_AuthDenied_NoMutationPermission verifies that a principal without
// "mutation" in their operations is rejected.
//
// Validates: Requirements 5.1, 6.2
func TestIntegration_AuthDenied_NoMutationPermission(t *testing.T) {
	setup := newIntegrationResolver(t)

	// Context with only "query" �?no "mutation" permission.
	ctx := integCtx("readonly-user", []string{"query"}, nil)
	values := []*generated.ColumnValueInput{
		{Column: "user_id", Value: float64(1)},
	}

	_, err := setup.resolver.InsertStarrocks(ctx, "orders", values)
	if err == nil {
		t.Fatal("expected authorization error")
	}
	if !strings.Contains(err.Error(), "mutation operation not allowed") {
		t.Errorf("expected permission error, got: %v", err)
	}

	// Verify audit logs auth denied.
	_ = setup.resolver.AuditLogger.Sync()
	entries := readIntegAuditEntries(t, setup.auditFile)
	if len(entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(entries))
	}
	if entries[0]["reason"] != "authorization_denied" {
		t.Errorf("expected reason=authorization_denied, got %v", entries[0]["reason"])
	}
	if entries[0]["result"] != "failure" {
		t.Errorf("expected result=failure, got %v", entries[0]["result"])
	}

	// No execution, no cache clear.
	if setup.wds.executeCalled {
		t.Error("expected ExecuteWrite NOT called")
	}
	if setup.cache.clearByDSCalled {
		t.Error("expected cache NOT cleared")
	}
}

// TestIntegration_AuthDenied_DatasourceRestricted verifies that a principal with mutation
// permission but restricted to a different datasource is rejected.
//
// Validates: Requirements 5.2, 6.2
func TestIntegration_AuthDenied_DatasourceRestricted(t *testing.T) {
	setup := newIntegrationResolver(t)

	// Has mutation permission but only for "other_ds".
	ctx := integCtx("restricted-user", []string{"query", "mutation"}, []string{"other_ds"})
	values := []*generated.ColumnValueInput{
		{Column: "user_id", Value: float64(1)},
	}

	_, err := setup.resolver.InsertStarrocks(ctx, "orders", values)
	if err == nil {
		t.Fatal("expected datasource access error")
	}
	if !strings.Contains(err.Error(), "no access to datasource") {
		t.Errorf("expected datasource access error, got: %v", err)
	}

	// Verify audit logs auth denied.
	_ = setup.resolver.AuditLogger.Sync()
	entries := readIntegAuditEntries(t, setup.auditFile)
	if len(entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(entries))
	}
	if entries[0]["reason"] != "authorization_denied" {
		t.Errorf("expected reason=authorization_denied, got %v", entries[0]["reason"])
	}
}

// TestIntegration_ValidationFailure_InvalidIdentifier verifies that invalid table/column
// names are rejected at the validation stage.
//
// Validates: Requirements 4.1, 6.3
func TestIntegration_ValidationFailure_InvalidIdentifier(t *testing.T) {
	setup := newIntegrationResolver(t)

	ctx := integCtx("admin-user", []string{"query", "mutation"}, nil)
	values := []*generated.ColumnValueInput{
		{Column: "user_id", Value: float64(1)},
	}

	// Invalid table name (starts with digit).
	_, err := setup.resolver.InsertStarrocks(ctx, "123invalid_table", values)
	if err == nil {
		t.Fatal("expected validation error for invalid table name")
	}
	if !strings.Contains(err.Error(), "invalid table name") {
		t.Errorf("expected invalid table name error, got: %v", err)
	}

	// Verify audit logs validation failure.
	_ = setup.resolver.AuditLogger.Sync()
	entries := readIntegAuditEntries(t, setup.auditFile)
	if len(entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(entries))
	}
	if entries[0]["reason"] != "validation_failed" {
		t.Errorf("expected reason=validation_failed, got %v", entries[0]["reason"])
	}

	// No execution, no cache clear.
	if setup.wds.executeCalled {
		t.Error("expected ExecuteWrite NOT called on validation failure")
	}
	if setup.cache.clearByDSCalled {
		t.Error("expected cache NOT cleared on validation failure")
	}
}

// TestIntegration_WhitelistFailure_TableNotWritable verifies that a table not in the
// writable_tables whitelist is rejected.
//
// Validates: Requirements 3.1
func TestIntegration_WhitelistFailure_TableNotWritable(t *testing.T) {
	setup := newIntegrationResolver(t)

	ctx := integCtx("admin-user", []string{"query", "mutation"}, nil)
	values := []*generated.ColumnValueInput{
		{Column: "email", Value: "test@example.com"},
	}

	// "users" is not in writable_tables.
	_, err := setup.resolver.InsertStarrocks(ctx, "users", values)
	if err == nil {
		t.Fatal("expected whitelist error for non-writable table")
	}
	if !strings.Contains(err.Error(), "not in the writable tables whitelist") {
		t.Errorf("expected whitelist error, got: %v", err)
	}

	// No execution.
	if setup.wds.executeCalled {
		t.Error("expected ExecuteWrite NOT called on whitelist failure")
	}
}

// TestIntegration_UpdateStarrocks_FullFlow exercises the full update mutation pipeline.
//
// Validates: Requirements 1.2, 1.5, 5.1, 6.1, 7.1
func TestIntegration_UpdateStarrocks_FullFlow(t *testing.T) {
	setup := newIntegrationResolver(t)

	setup.wds.executeWriteFunc = func(_ context.Context, sql string, params []any) (int64, error) {
		if !strings.Contains(sql, "UPDATE") {
			t.Errorf("expected UPDATE in SQL, got %q", sql)
		}
		if !strings.Contains(sql, "WHERE") {
			t.Errorf("expected WHERE in SQL, got %q", sql)
		}
		return 5, nil
	}

	ctx := integCtx("editor-user", []string{"query", "mutation"}, nil)
	set := []*generated.ColumnValueInput{
		{Column: "status", Value: "shipped"},
		{Column: "amount", Value: float64(150.0)},
	}
	filter := []*generated.MutationFilterInput{
		{Field: "order_id", Operator: generated.FilterOperatorEq, Value: float64(42)},
	}

	result, err := setup.resolver.UpdateStarrocks(ctx, "orders", set, filter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Success {
		t.Error("expected success=true")
	}
	if result.AffectedRows != 5 {
		t.Errorf("expected 5 affected rows, got %d", result.AffectedRows)
	}

	// Verify cache invalidation.
	if !setup.cache.clearByDSCalled {
		t.Error("expected cache cleared after successful update")
	}

	// Verify audit.
	_ = setup.resolver.AuditLogger.Sync()
	entries := readIntegAuditEntries(t, setup.auditFile)
	if len(entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(entries))
	}
	if entries[0]["operation"] != "update" {
		t.Errorf("expected operation=update, got %v", entries[0]["operation"])
	}
	if entries[0]["affected_rows"] != "5" {
		t.Errorf("expected affected_rows=5, got %v", entries[0]["affected_rows"])
	}
}

// TestIntegration_DeleteStarrocks_FullFlow exercises the full delete mutation pipeline.
//
// Validates: Requirements 1.3, 1.6, 5.1, 6.1, 7.1
func TestIntegration_DeleteStarrocks_FullFlow(t *testing.T) {
	setup := newIntegrationResolver(t)

	setup.wds.executeWriteFunc = func(_ context.Context, sql string, params []any) (int64, error) {
		if !strings.Contains(sql, "DELETE FROM") {
			t.Errorf("expected DELETE FROM in SQL, got %q", sql)
		}
		return 7, nil
	}

	ctx := integCtx("admin-user", []string{"query", "mutation"}, nil)
	filter := []*generated.MutationFilterInput{
		{Field: "order_id", Operator: generated.FilterOperatorLt, Value: float64(100)},
	}

	result, err := setup.resolver.DeleteStarrocks(ctx, "orders", filter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Success {
		t.Error("expected success=true")
	}
	if result.AffectedRows != 7 {
		t.Errorf("expected 7 affected rows, got %d", result.AffectedRows)
	}

	// Verify cache invalidation.
	if !setup.cache.clearByDSCalled {
		t.Error("expected cache cleared after successful delete")
	}

	// Verify audit.
	_ = setup.resolver.AuditLogger.Sync()
	entries := readIntegAuditEntries(t, setup.auditFile)
	if len(entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(entries))
	}
	if entries[0]["operation"] != "delete" {
		t.Errorf("expected operation=delete, got %v", entries[0]["operation"])
	}
	if entries[0]["affected_rows"] != "7" {
		t.Errorf("expected affected_rows=7, got %v", entries[0]["affected_rows"])
	}
}

// TestIntegration_InsertBatchStarrocks_FullFlow exercises the full batch insert pipeline.
//
// Validates: Requirements 1.8, 5.1, 6.1, 7.1
func TestIntegration_InsertBatchStarrocks_FullFlow(t *testing.T) {
	setup := newIntegrationResolver(t)

	setup.wds.executeWriteFunc = func(_ context.Context, sql string, params []any) (int64, error) {
		if !strings.Contains(sql, "INSERT INTO") {
			t.Errorf("expected INSERT INTO in SQL, got %q", sql)
		}
		// Verify batch format �?multiple value tuples.
		if !strings.Contains(sql, "), (") {
			t.Errorf("expected batch VALUES format in SQL, got %q", sql)
		}
		// Should have 3 rows × 3 columns = 9 params.
		if len(params) != 9 {
			t.Errorf("expected 9 params for 3x3 batch, got %d", len(params))
		}
		return 3, nil
	}

	ctx := integCtx("batch-admin", []string{"query", "mutation"}, nil)
	columns := []string{"user_id", "amount", "status"}
	rows := [][]any{
		{float64(1), float64(100), "pending"},
		{float64(2), float64(200), "active"},
		{float64(3), float64(300), "shipped"},
	}

	result, err := setup.resolver.InsertBatchStarrocks(ctx, "orders", columns, rows)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Success {
		t.Error("expected success=true")
	}
	if result.AffectedRows != 3 {
		t.Errorf("expected 3 affected rows, got %d", result.AffectedRows)
	}

	// Verify cache invalidation.
	if !setup.cache.clearByDSCalled {
		t.Error("expected cache cleared after successful batch insert")
	}

	// Verify audit.
	_ = setup.resolver.AuditLogger.Sync()
	entries := readIntegAuditEntries(t, setup.auditFile)
	if len(entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(entries))
	}
	if entries[0]["operation"] != "insertBatch" {
		t.Errorf("expected operation=insertBatch, got %v", entries[0]["operation"])
	}
	if entries[0]["affected_rows"] != "3" {
		t.Errorf("expected affected_rows=3, got %v", entries[0]["affected_rows"])
	}
}

// TestIntegration_AffectedRowsWarning verifies that exceeding max_affected_rows produces
// a warning but still succeeds.
//
// Validates: Requirements 4.7, 10.5
func TestIntegration_AffectedRowsWarning(t *testing.T) {
	setup := newIntegrationResolver(t)

	// Return more than max_affected_rows (1000).
	setup.wds.executeWriteFunc = func(_ context.Context, _ string, _ []any) (int64, error) {
		return 2500, nil
	}

	ctx := integCtx("admin-user", []string{"query", "mutation"}, nil)
	filter := []*generated.MutationFilterInput{
		{Field: "order_id", Operator: generated.FilterOperatorGt, Value: float64(0)},
	}

	result, err := setup.resolver.DeleteStarrocks(ctx, "orders", filter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Success {
		t.Error("expected success=true even with warning")
	}
	if result.AffectedRows != 2500 {
		t.Errorf("expected 2500 affected rows, got %d", result.AffectedRows)
	}
	if result.Warning == nil {
		t.Fatal("expected warning when affected rows exceed threshold")
	}
	if !strings.Contains(*result.Warning, "exceeded threshold") {
		t.Errorf("expected 'exceeded threshold' in warning, got: %q", *result.Warning)
	}
	if !strings.Contains(*result.Warning, "2500") {
		t.Errorf("expected actual rows in warning, got: %q", *result.Warning)
	}
}

// TestIntegration_CacheInvalidationFailure_StillSucceeds verifies that a cache clear error
// does not cause the mutation to fail.
//
// Validates: Requirements 7.3
func TestIntegration_CacheInvalidationFailure_StillSucceeds(t *testing.T) {
	setup := newIntegrationResolver(t)

	// Replace cache with one that returns an error.
	failingCache := &integCacheClearer{err: errIntegCacheDown}
	setup.resolver.CacheClearer = failingCache

	setup.wds.executeWriteFunc = func(_ context.Context, _ string, _ []any) (int64, error) {
		return 1, nil
	}

	ctx := integCtx("admin-user", []string{"query", "mutation"}, nil)
	values := []*generated.ColumnValueInput{
		{Column: "user_id", Value: float64(42)},
	}

	result, err := setup.resolver.InsertStarrocks(ctx, "orders", values)
	if err != nil {
		t.Fatalf("expected success despite cache error, got: %v", err)
	}
	if !result.Success {
		t.Error("expected success=true even when cache clear fails")
	}
	if result.AffectedRows != 1 {
		t.Errorf("expected 1 affected row, got %d", result.AffectedRows)
	}
	if !failingCache.clearByDSCalled {
		t.Error("expected ClearByDatasource to be attempted even on error")
	}
}

// TestIntegration_OperationNotAllowed verifies that disallowed operations are blocked.
//
// Validates: Requirements 3.6, 10.6
func TestIntegration_OperationNotAllowed(t *testing.T) {
	setup := newIntegrationResolver(t)

	ctx := integCtx("admin-user", []string{"query", "mutation"}, nil)

	// "events" table only allows "insert" �?delete should fail.
	filter := []*generated.MutationFilterInput{
		{Field: "event_id", Operator: generated.FilterOperatorEq, Value: float64(1)},
	}

	_, err := setup.resolver.DeleteStarrocks(ctx, "events", filter)
	if err == nil {
		t.Fatal("expected operation not allowed error")
	}
	if !strings.Contains(err.Error(), "not allowed") {
		t.Errorf("expected 'not allowed' error, got: %v", err)
	}

	if setup.wds.executeCalled {
		t.Error("expected ExecuteWrite NOT called when operation is disallowed")
	}
}

// TestIntegration_NoAuthIdentity verifies that requests without authentication are rejected.
//
// Validates: Requirements 5.1
func TestIntegration_NoAuthIdentity(t *testing.T) {
	setup := newIntegrationResolver(t)

	// Empty context �?no auth identity.
	ctx := context.Background()
	values := []*generated.ColumnValueInput{
		{Column: "user_id", Value: float64(1)},
	}

	_, err := setup.resolver.InsertStarrocks(ctx, "orders", values)
	if err == nil {
		t.Fatal("expected auth error when no identity in context")
	}
	if !strings.Contains(err.Error(), "authentication required") {
		t.Errorf("expected auth required error, got: %v", err)
	}
}

// TestIntegration_ExecutionFailure_NoCacheInvalidation verifies that failed execution
// does not trigger cache invalidation.
//
// Validates: Requirements 7.2
func TestIntegration_ExecutionFailure_NoCacheInvalidation(t *testing.T) {
	setup := newIntegrationResolver(t)

	// Simulate database failure.
	setup.wds.executeWriteFunc = func(_ context.Context, _ string, _ []any) (int64, error) {
		return 0, context.DeadlineExceeded
	}

	ctx := integCtx("admin-user", []string{"query", "mutation"}, nil)
	values := []*generated.ColumnValueInput{
		{Column: "user_id", Value: float64(1)},
	}

	_, err := setup.resolver.InsertStarrocks(ctx, "orders", values)
	if err == nil {
		t.Fatal("expected execution error")
	}

	if setup.cache.clearByDSCalled {
		t.Error("expected cache NOT cleared when execution fails")
	}
}
