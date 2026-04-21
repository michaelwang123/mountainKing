// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package starrocks

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"go.uber.org/zap"

	"github.com/michaelwang123/mountainKing/internal/datasource"
	apierrors "github.com/michaelwang123/mountainKing/internal/errors"
)

// Adapter implements the datasource.DataSource interface for StarRocks.
// It connects via the MySQL protocol using database/sql.
type Adapter struct {
	name         string
	config       datasource.DataSourceConfig
	db           *sql.DB
	queryBuilder *SQLQueryBuilder
	typeMapper   *TypeMapper
	logger       *zap.Logger
	mu           sync.RWMutex
	available    bool
}

// Verify interface compliance at compile time.
var _ datasource.DataSource = (*Adapter)(nil)

// NewAdapter creates a new StarRocks adapter from config.
// It parses allowed_tables from config.Options to build the query builder.
func NewAdapter(name string, config datasource.DataSourceConfig, logger *zap.Logger) (*Adapter, error) {
	allowedTables, err := ParseAllowedTables(config)
	if err != nil {
		return nil, fmt.Errorf("starrocks adapter %q: %w", name, err)
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

// Type returns "starrocks".
func (a *Adapter) Type() string {
	return "starrocks"
}

// Connect establishes a MySQL connection to StarRocks.
// It builds a DSN from config.Connection (host, port, username, password, database)
// and sets connection pool params from config.Options (pool_size, connection_timeout).
// Connect is idempotent; calling it on an already-connected adapter is a no-op.
func (a *Adapter) Connect(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.db != nil {
		return nil
	}

	dsn, err := a.buildDSN()
	if err != nil {
		return fmt.Errorf("starrocks adapter %q: build DSN: %w", a.name, err)
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("starrocks adapter %q: open connection: %w", a.name, err)
	}

	// Configure connection pool.
	poolSize := getIntOption(a.config.Options, "pool_size", 10)
	db.SetMaxOpenConns(poolSize)
	db.SetMaxIdleConns(poolSize)

	connTimeout := getDurationOption(a.config.Options, "connection_timeout", 5*time.Second)
	db.SetConnMaxLifetime(connTimeout * 10)

	// Verify the connection is alive.
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return fmt.Errorf("starrocks adapter %q: ping failed: %w", a.name, err)
	}

	a.db = db
	a.available = true
	a.logger.Info("starrocks adapter connected",
		zap.String("name", a.name),
		zap.Int("pool_size", poolSize),
	)
	return nil
}

// IsAvailable returns whether the adapter is currently connected.
func (a *Adapter) IsAvailable() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.available
}

// Execute runs a query against StarRocks.
// It extracts "table" from req.Options, builds SQL using the query builder,
// executes the query, and scans results into []map[string]interface{}.
// If NeedCount is true, it also runs a COUNT query.
func (a *Adapter) Execute(ctx context.Context, req datasource.QueryRequest) (*datasource.QueryResult, error) {
	a.mu.RLock()
	db := a.db
	a.mu.RUnlock()

	if db == nil {
		return nil, apierrors.DatasourceError(
			apierrors.ErrDatasourceUnavailable,
			fmt.Sprintf("starrocks adapter %q is not connected", a.name),
		)
	}

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
	rows, err := db.QueryContext(ctx, query, params...)
	if err != nil {
		return nil, apierrors.DatasourceError(
			apierrors.ErrDatasourceQueryError,
			fmt.Sprintf("starrocks query failed: %v", err),
		)
	}
	defer rows.Close()

	data, err := scanRows(rows)
	if err != nil {
		return nil, apierrors.DatasourceError(
			apierrors.ErrDatasourceQueryError,
			fmt.Sprintf("starrocks scan failed: %v", err),
		)
	}

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
			return nil, apierrors.DatasourceError(
				apierrors.ErrDatasourceQueryError,
				fmt.Sprintf("starrocks count query failed: %v", err),
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
func (a *Adapter) ExecuteRaw(ctx context.Context, query string, args ...interface{}) (*datasource.QueryResult, error) {
	a.mu.RLock()
	db := a.db
	a.mu.RUnlock()

	if db == nil {
		return nil, apierrors.DatasourceError(
			apierrors.ErrDatasourceUnavailable,
			fmt.Sprintf("starrocks adapter %q is not connected", a.name),
		)
	}

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
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

	return &datasource.QueryResult{Data: data}, nil
}

// HealthCheck pings the database to verify the connection is alive.
func (a *Adapter) HealthCheck(ctx context.Context) error {
	a.mu.RLock()
	db := a.db
	a.mu.RUnlock()

	if db == nil {
		return apierrors.DatasourceError(
			apierrors.ErrDatasourceUnavailable,
			fmt.Sprintf("starrocks adapter %q is not connected", a.name),
		)
	}

	if err := db.PingContext(ctx); err != nil {
		a.mu.Lock()
		a.available = false
		a.mu.Unlock()
		return fmt.Errorf("starrocks adapter %q: health check failed: %w", a.name, err)
	}

	a.mu.Lock()
	a.available = true
	a.mu.Unlock()
	return nil
}

// SchemaFiles returns the StarRocks GraphQL schema file paths.
func (a *Adapter) SchemaFiles() []string {
	return []string{"internal/graphql/schema/starrocks.graphql"}
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
			return fmt.Errorf("starrocks adapter %q: close failed: %w", a.name, err)
		}
	}
	return nil
}

// buildDSN constructs a MySQL DSN string from the adapter's connection config.
// Format: username:password@tcp(host:port)/database
func (a *Adapter) buildDSN() (string, error) {
	conn := a.config.Connection

	host, _ := conn["host"].(string)
	if host == "" {
		return "", fmt.Errorf("missing connection.host")
	}

	port := 9030
	if p, ok := conn["port"]; ok {
		switch v := p.(type) {
		case int:
			port = v
		case float64:
			port = int(v)
		case string:
			parsed, err := strconv.Atoi(v)
			if err != nil {
				return "", fmt.Errorf("invalid connection.port: %w", err)
			}
			port = parsed
		}
	}

	username, _ := conn["username"].(string)
	password, _ := conn["password"].(string)
	database, _ := conn["database"].(string)

	connTimeout := getDurationOption(a.config.Options, "connection_timeout", 5*time.Second)
	timeoutParam := fmt.Sprintf("timeout=%s", connTimeout)

	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?%s&parseTime=true",
		username, password, host, port, database, timeoutParam)

	return dsn, nil
}

// extractTable extracts the "table" value from request options.
func extractTable(options map[string]interface{}) (string, error) {
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
func scanRows(rows *sql.Rows) ([]map[string]interface{}, error) {
	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("get columns: %w", err)
	}

	var result []map[string]interface{}

	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}

		row := make(map[string]interface{}, len(columns))
		for i, col := range columns {
			val := values[i]
			// Convert []byte to string for readability.
			if b, ok := val.([]byte); ok {
				row[col] = string(b)
			} else {
				row[col] = val
			}
		}
		result = append(result, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}

	return result, nil
}

// getIntOption extracts an integer option from the options map with a default fallback.
func getIntOption(options map[string]interface{}, key string, defaultVal int) int {
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
func getDurationOption(options map[string]interface{}, key string, defaultVal time.Duration) time.Duration {
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
