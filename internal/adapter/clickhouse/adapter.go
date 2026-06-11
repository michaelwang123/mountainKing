// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package clickhouse

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strconv"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/michaelwang123/mountainKing/internal/datasource"
	apierrors "github.com/michaelwang123/mountainKing/internal/errors"
)

// Adapter implements the datasource.DataSource and datasource.WritableDataSource
// interfaces for ClickHouse. It connects via the ClickHouse native TCP protocol
// using database/sql.
type Adapter struct {
	name           string
	config         datasource.DataSourceConfig
	db             *sql.DB
	queryBuilder   *SQLQueryBuilder
	typeMapper     *TypeMapper
	logger         *zap.Logger
	mu             sync.RWMutex
	available      bool
	circuitBreaker *datasource.CircuitBreaker
}

// Verify interface compliance at compile time.
var _ datasource.DataSource = (*Adapter)(nil)

// Verify WritableDataSource interface compliance at compile time.
var _ datasource.WritableDataSource = (*Adapter)(nil)

// NewAdapter creates a new ClickHouse adapter from config.
// It parses allowed_tables from config.Options to build the query builder.
func NewAdapter(name string, config datasource.DataSourceConfig, logger *zap.Logger) (*Adapter, error) {
	allowedTables, err := ParseAllowedTables(config)
	if err != nil {
		return nil, fmt.Errorf("clickhouse adapter %q: %w", name, err)
	}

	return &Adapter{
		name:         name,
		config:       config,
		queryBuilder: NewSQLQueryBuilder(allowedTables),
		typeMapper:   NewTypeMapper(logger),
		logger:       logger,
	}, nil
}

// Factory returns an AdapterFactory for registering with the AdapterRegistry.
func Factory(logger *zap.Logger) datasource.AdapterFactory {
	return func(name string, config datasource.DataSourceConfig) (datasource.DataSource, error) {
		return NewAdapter(name, config, logger)
	}
}

// Name returns the data source name.
func (a *Adapter) Name() string {
	return a.name
}

// Type returns "clickhouse".
func (a *Adapter) Type() string {
	return "clickhouse"
}

// SetCircuitBreaker sets the shared circuit breaker for this adapter.
// This allows both read and write operations to share the same breaker.
func (a *Adapter) SetCircuitBreaker(cb *datasource.CircuitBreaker) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.circuitBreaker = cb
}

// SchemaFiles returns an empty list. ClickHouse reuses the StarRocks GraphQL schema;
// query routing is handled via Options["table"] + datasource name.
func (a *Adapter) SchemaFiles() []string {
	return nil
}

// Connect establishes a ClickHouse native TCP connection.
// It builds a DSN from config.Connection and sets connection pool params.
// Connect is idempotent; calling it on an already-connected adapter is a no-op.
func (a *Adapter) Connect(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.db != nil {
		return nil
	}

	dsn := a.buildDSN()

	db, err := sql.Open("clickhouse", dsn)
	if err != nil {
		return fmt.Errorf("clickhouse adapter %q: open connection: %w", a.name, err)
	}

	// Configure connection pool with validated bounds.
	poolSize := getIntOption(a.config.Options, "pool_size", 10)
	if poolSize < 1 {
		poolSize = 1
	}
	maxIdleConns := getIntOption(a.config.Options, "max_idle_conns", 5)
	if maxIdleConns < 0 {
		maxIdleConns = 0
	}
	if maxIdleConns > poolSize {
		maxIdleConns = poolSize
	}
	connMaxLifetime := getDurationOption(a.config.Options, "conn_max_lifetime", 1*time.Hour)
	if connMaxLifetime < 0 {
		connMaxLifetime = 0 // 0 means no max lifetime
	}

	db.SetMaxOpenConns(poolSize)
	db.SetMaxIdleConns(maxIdleConns)
	db.SetConnMaxLifetime(connMaxLifetime)

	// ConnMaxIdleTime closes idle connections that have been idle longer than this duration.
	// This helps clean up connections that ClickHouse may have already closed server-side
	// (ClickHouse default idle_connection_timeout = 3600s).
	connMaxIdleTime := getDurationOption(a.config.Options, "conn_max_idle_time", 10*time.Minute)
	if connMaxIdleTime < 0 {
		connMaxIdleTime = 0
	}
	db.SetConnMaxIdleTime(connMaxIdleTime)

	// Verify the connection is alive.
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return fmt.Errorf("clickhouse adapter %q: ping failed: %w", a.name, err)
	}

	a.db = db
	a.available = true
	a.logger.Info("clickhouse adapter connected",
		zap.String("name", a.name),
		zap.String("dsn", a.sanitizedDSN()),
		zap.Int("pool_size", poolSize),
		zap.Int("max_idle_conns", maxIdleConns),
	)
	return nil
}

// buildDSN constructs a ClickHouse DSN string from the adapter's connection config.
// Format: clickhouse://username:password@host:port/database?dial_timeout=5s&read_timeout=30s&compress=lz4
func (a *Adapter) buildDSN() string {
	conn := a.config.Connection

	host, _ := conn["host"].(string)
	if host == "" {
		host = "localhost"
	}

	// Determine port and whether it was explicitly set.
	port := 9000
	portExplicit := false
	if p, ok := conn["port"]; ok {
		portExplicit = true
		switch v := p.(type) {
		case int:
			port = v
		case float64:
			port = int(v)
		case string:
			if parsed, err := strconv.Atoi(v); err == nil {
				port = parsed
			}
		}
	}

	// Validate port range; fallback to default if invalid.
	if port < 1 || port > 65535 {
		port = 9000
		portExplicit = false
	}

	username, _ := conn["username"].(string)
	if username == "" {
		username = "default"
	}

	password, _ := conn["password"].(string)

	database, _ := conn["database"].(string)
	if database == "" {
		database = "default"
	}

	// TLS port linkage: secure=true && (port not explicitly set OR port==9000) → 9440
	secure := getBoolOption(a.config.Options, "secure", false)
	if secure && (!portExplicit || port == 9000) {
		port = 9440
	}

	// Build query parameters.
	connTimeout := getDurationOption(a.config.Options, "connection_timeout", 5*time.Second)
	readTimeout := getDurationOption(a.config.Options, "read_timeout", 30*time.Second)
	compress := getStringOption(a.config.Options, "compress", "lz4")

	params := url.Values{}
	params.Set("dial_timeout", connTimeout.String())
	params.Set("read_timeout", readTimeout.String())

	// Compress: lz4/zstd/none. "none" means no compress param.
	if compress != "none" && compress != "" {
		params.Set("compress", compress)
	}

	if secure {
		params.Set("secure", "true")
	}

	// Build DSN: clickhouse://username:password@host:port/database?params
	dsn := fmt.Sprintf("clickhouse://%s:%s@%s:%d/%s?%s",
		url.PathEscape(username),
		url.PathEscape(password),
		host,
		port,
		database,
		params.Encode(),
	)

	return dsn
}

// IsAvailable returns whether the adapter is currently connected.
func (a *Adapter) IsAvailable() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.available
}

// Execute runs a query against ClickHouse.
// It extracts "table" from req.Options, builds SQL using the query builder,
// executes the query, and scans results into []map[string]any.
// If NeedCount is true, it also runs a COUNT query.
// The query is bounded by the configured read_timeout if the caller's context has no deadline.
// Read queries also respect the shared circuit breaker to fail fast when the backend is unhealthy.
func (a *Adapter) Execute(ctx context.Context, req datasource.QueryRequest) (*datasource.QueryResult, error) {
	a.mu.RLock()
	db := a.db
	cb := a.circuitBreaker
	a.mu.RUnlock()

	if db == nil {
		return nil, apierrors.DatasourceError(
			apierrors.ErrDatasourceUnavailable,
			fmt.Sprintf("clickhouse adapter %q is not connected", a.name),
		)
	}

	// Early context cancellation check — fail fast with a clear error code.
	if err := ctx.Err(); err != nil {
		return nil, a.contextError(err)
	}

	// Check circuit breaker — fail fast if backend is known unhealthy.
	if cb != nil && !cb.AllowRequest() {
		return nil, apierrors.DatasourceError(
			apierrors.ErrDatasourceCircuitOpen,
			fmt.Sprintf("clickhouse adapter %q circuit breaker is open", a.name),
		)
	}

	// Apply per-query timeout if the caller's context has no deadline.
	ctx, cancel := a.withQueryTimeout(ctx)
	defer cancel()

	// Extract table name from request options.
	table, err := extractTable(req.Options)
	if err != nil {
		return nil, err
	}

	// Build the SELECT query.
	query, params, err := a.queryBuilder.Build(req, table)
	if err != nil {
		return nil, err
	}

	// Execute the query.
	start := time.Now()
	rows, err := db.QueryContext(ctx, query, params...)
	if err != nil {
		if cb != nil {
			cb.RecordFailure()
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, a.contextError(ctxErr)
		}
		return nil, apierrors.DatasourceError(
			apierrors.ErrDatasourceQueryError,
			fmt.Sprintf("clickhouse query failed: %v", err),
		)
	}
	defer rows.Close()

	data, err := scanRows(rows)
	if err != nil {
		return nil, apierrors.DatasourceError(
			apierrors.ErrDatasourceQueryError,
			fmt.Sprintf("clickhouse scan failed: %v", err),
		)
	}

	// Record success on the circuit breaker.
	if cb != nil {
		cb.RecordSuccess()
	}

	a.logger.Debug("clickhouse query executed",
		zap.String("table", table),
		zap.Int("rows", len(data)),
		zap.Duration("duration", time.Since(start)),
	)

	result := &datasource.QueryResult{
		Data: data,
	}

	// If NeedCount, execute a COUNT query.
	if req.NeedCount {
		countQuery, countParams, err := a.queryBuilder.BuildCount(req, table)
		if err != nil {
			return nil, err
		}

		var totalCount int64
		if err := db.QueryRowContext(ctx, countQuery, countParams...).Scan(&totalCount); err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, a.contextError(ctxErr)
			}
			return nil, apierrors.DatasourceError(
				apierrors.ErrDatasourceQueryError,
				fmt.Sprintf("clickhouse count query failed: %v", err),
			)
		}
		result.TotalCount = &totalCount
	}

	return result, nil
}

// ExecuteRaw implements template.RawExecutor.
// It reuses the existing *sql.DB connection pool and scanRows function to
// execute arbitrary SQL. It does not go through SQLQueryBuilder or whitelist
// validation — the caller (TemplateEngine) is responsible for security checks.
// The query is bounded by the configured read_timeout if the caller's context has no deadline.
func (a *Adapter) ExecuteRaw(ctx context.Context, query string, args ...any) (*datasource.QueryResult, error) {
	a.mu.RLock()
	db := a.db
	a.mu.RUnlock()

	if db == nil {
		return nil, apierrors.DatasourceError(
			apierrors.ErrDatasourceUnavailable,
			fmt.Sprintf("clickhouse adapter %q is not connected", a.name),
		)
	}

	// Early context cancellation check.
	if err := ctx.Err(); err != nil {
		return nil, a.contextError(err)
	}

	// Apply per-query timeout if the caller's context has no deadline.
	ctx, cancel := a.withQueryTimeout(ctx)
	defer cancel()

	start := time.Now()
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, a.contextError(ctxErr)
		}
		return nil, apierrors.DatasourceError(
			apierrors.ErrDatasourceTemplateQueryError,
			fmt.Sprintf("template query failed: %v", err),
		)
	}
	defer rows.Close()

	data, err := scanRows(rows)
	if err != nil {
		return nil, apierrors.DatasourceError(
			apierrors.ErrDatasourceTemplateQueryError,
			fmt.Sprintf("template query scan failed: %v", err),
		)
	}

	a.logger.Debug("clickhouse raw query executed",
		zap.Int("rows", len(data)),
		zap.Duration("duration", time.Since(start)),
	)

	return &datasource.QueryResult{Data: data}, nil
}

// ExecuteWrite executes a write SQL statement and returns affected rows.
// It uses the existing connection pool and respects the shared circuit breaker.
// Write failures count toward the same circuit breaker as read queries.
// Writes are NOT retried (non-idempotent) — circuit breaker is the only protection layer.
//
// Note: ClickHouse lightweight DELETE (standard since v22.8) always returns
// RowsAffected=0 because rows are marked for deletion but not immediately
// physically removed (cleaned up by background merges). This is by design.
// INSERT operations return the actual number of inserted rows.
func (a *Adapter) ExecuteWrite(ctx context.Context, sqlStr string, params []any) (int64, error) {
	a.mu.RLock()
	db := a.db
	cb := a.circuitBreaker
	a.mu.RUnlock()

	if db == nil {
		return 0, apierrors.DatasourceError(
			apierrors.ErrDatasourceUnavailable,
			fmt.Sprintf("clickhouse adapter %q is not connected", a.name),
		)
	}

	// Early context cancellation check.
	if err := ctx.Err(); err != nil {
		return 0, a.contextError(err)
	}

	// Check circuit breaker state — if open, fail fast.
	if cb != nil && !cb.AllowRequest() {
		return 0, apierrors.DatasourceError(
			apierrors.ErrDatasourceCircuitOpen,
			fmt.Sprintf("clickhouse adapter %q circuit breaker is open", a.name),
		)
	}

	start := time.Now()
	result, err := db.ExecContext(ctx, sqlStr, params...)
	if err != nil {
		// Record failure on the shared circuit breaker.
		if cb != nil {
			cb.RecordFailure()
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return 0, a.contextError(ctxErr)
		}
		return 0, apierrors.DatasourceError(
			apierrors.ErrDatasourceQueryError,
			fmt.Sprintf("clickhouse write failed: %v", err),
		)
	}

	// Record success on the shared circuit breaker.
	if cb != nil {
		cb.RecordSuccess()
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return 0, apierrors.DatasourceError(
			apierrors.ErrDatasourceQueryError,
			fmt.Sprintf("clickhouse affected rows retrieval failed: %v", err),
		)
	}

	a.logger.Debug("clickhouse write executed",
		zap.Int64("affected_rows", affected),
		zap.Duration("duration", time.Since(start)),
	)

	return affected, nil
}

// HealthCheck pings the database to verify the connection is alive.
// It also feeds the circuit breaker: ping failures count as breaker failures,
// and ping successes count as breaker successes. This ensures that sustained
// health check failures will trip the circuit breaker, preventing query attempts
// against a known-unhealthy backend.
func (a *Adapter) HealthCheck(ctx context.Context) error {
	a.mu.RLock()
	db := a.db
	cb := a.circuitBreaker
	a.mu.RUnlock()

	if db == nil {
		return apierrors.DatasourceError(
			apierrors.ErrDatasourceUnavailable,
			fmt.Sprintf("clickhouse adapter %q is not connected", a.name),
		)
	}

	if err := db.PingContext(ctx); err != nil {
		a.mu.Lock()
		a.available = false
		a.mu.Unlock()
		// Feed health check failure to circuit breaker.
		if cb != nil {
			cb.RecordFailure()
		}
		a.logger.Warn("clickhouse health check failed",
			zap.String("name", a.name),
			zap.Error(err),
		)
		return fmt.Errorf("clickhouse adapter %q: health check failed: %w", a.name, err)
	}

	a.mu.Lock()
	a.available = true
	a.mu.Unlock()
	// Feed health check success to circuit breaker (helps recover from HALF_OPEN).
	if cb != nil {
		cb.RecordSuccess()
	}
	return nil
}

// Close closes the database connection and marks the adapter as unavailable.
func (a *Adapter) Close(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.available = false
	if a.db != nil {
		err := a.db.Close()
		a.db = nil
		if err != nil {
			return fmt.Errorf("clickhouse adapter %q: close failed: %w", a.name, err)
		}
	}
	return nil
}

// extractTable extracts the "table" value from request options.
func extractTable(options map[string]any) (string, error) {
	if options == nil {
		return "", apierrors.ValidationError(
			apierrors.ErrValidationInvalidTable,
			"request options must include a table name",
		)
	}

	table, ok := options["table"].(string)
	if !ok || table == "" {
		return "", apierrors.ValidationError(
			apierrors.ErrValidationInvalidTable,
			"request options must include a non-empty table name",
		)
	}

	return table, nil
}

// scanRows reads all rows from a sql.Rows result set into a slice of maps.
// It handles ClickHouse-specific type conversions via convertValue.
// Returns an empty (non-nil) slice when the result set has no rows,
// ensuring consistent JSON serialization as [] rather than null.
func scanRows(rows *sql.Rows) ([]map[string]any, error) {
	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("get columns: %w", err)
	}

	// Initialize as empty slice (not nil) for consistent JSON serialization.
	result := make([]map[string]any, 0)

	for rows.Next() {
		values := make([]any, len(columns))
		valuePtrs := make([]any, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}

		row := make(map[string]any, len(columns))
		for i, col := range columns {
			row[col] = convertValue(values[i])
		}
		result = append(result, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}

	return result, nil
}

// convertValue converts ClickHouse driver values to JSON-friendly Go types.
// It handles common ClickHouse-specific types that may not serialize cleanly
// to JSON via the standard encoding/json package.
func convertValue(val any) any {
	if val == nil {
		return nil
	}
	switch v := val.(type) {
	case []byte:
		return string(v)
	case time.Time:
		return v.Format(time.RFC3339Nano)
	case *time.Time:
		if v == nil {
			return nil
		}
		return v.Format(time.RFC3339Nano)
	case *string:
		if v == nil {
			return nil
		}
		return *v
	case *int64:
		if v == nil {
			return nil
		}
		return *v
	case *float64:
		if v == nil {
			return nil
		}
		return *v
	case *bool:
		if v == nil {
			return nil
		}
		return *v
	case fmt.Stringer:
		return v.String()
	default:
		return val
	}
}

// contextError maps a context error to the appropriate API error type.
// context.DeadlineExceeded → DATASOURCE_TIMEOUT
// context.Canceled → DATASOURCE_UNAVAILABLE (client disconnected)
func (a *Adapter) contextError(err error) *apierrors.APIError {
	if err == context.DeadlineExceeded {
		return apierrors.DatasourceError(
			apierrors.ErrDatasourceTimeout,
			fmt.Sprintf("clickhouse adapter %q: query timed out", a.name),
		)
	}
	return apierrors.DatasourceError(
		apierrors.ErrDatasourceUnavailable,
		fmt.Sprintf("clickhouse adapter %q: request cancelled", a.name),
	)
}

// withQueryTimeout wraps ctx with a deadline based on the configured read_timeout,
// but only if the caller's context does not already have a deadline set.
// This ensures queries don't hang indefinitely even when called without a deadline.
func (a *Adapter) withQueryTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, hasDeadline := ctx.Deadline(); hasDeadline {
		// Caller already has a deadline; don't override.
		return ctx, func() {}
	}
	readTimeout := getDurationOption(a.config.Options, "read_timeout", 30*time.Second)
	if readTimeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, readTimeout)
}

// sanitizedDSN returns the DSN with the password masked for safe logging.
func (a *Adapter) sanitizedDSN() string {
	conn := a.config.Connection
	host, _ := conn["host"].(string)
	if host == "" {
		host = "localhost"
	}
	username, _ := conn["username"].(string)
	if username == "" {
		username = "default"
	}
	database, _ := conn["database"].(string)
	if database == "" {
		database = "default"
	}
	return fmt.Sprintf("clickhouse://%s:***@%s/%s", username, host, database)
}

// getIntOption extracts an integer option from the options map with a default fallback.
func getIntOption(options map[string]any, key string, defaultVal int) int {
	if options == nil {
		return defaultVal
	}
	v, ok := options[key]
	if !ok {
		return defaultVal
	}
	switch val := v.(type) {
	case int:
		return val
	case float64:
		return int(val)
	case string:
		if parsed, err := strconv.Atoi(val); err == nil {
			return parsed
		}
	}
	return defaultVal
}

// getDurationOption extracts a duration option from the options map with a default fallback.
func getDurationOption(options map[string]any, key string, defaultVal time.Duration) time.Duration {
	if options == nil {
		return defaultVal
	}
	v, ok := options[key]
	if !ok {
		return defaultVal
	}
	switch val := v.(type) {
	case string:
		if d, err := time.ParseDuration(val); err == nil {
			return d
		}
	case time.Duration:
		return val
	case float64:
		return time.Duration(val) * time.Second
	case int:
		return time.Duration(val) * time.Second
	}
	return defaultVal
}

// getBoolOption extracts a boolean option from the options map with a default fallback.
func getBoolOption(options map[string]any, key string, defaultVal bool) bool {
	if options == nil {
		return defaultVal
	}
	v, ok := options[key]
	if !ok {
		return defaultVal
	}
	switch val := v.(type) {
	case bool:
		return val
	case string:
		if b, err := strconv.ParseBool(val); err == nil {
			return b
		}
	}
	return defaultVal
}

// getStringOption extracts a string option from the options map with a default fallback.
func getStringOption(options map[string]any, key string, defaultVal string) string {
	if options == nil {
		return defaultVal
	}
	v, ok := options[key]
	if !ok {
		return defaultVal
	}
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	return defaultVal
}
