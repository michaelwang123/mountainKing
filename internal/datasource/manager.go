// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package datasource

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/michaelwang123/mountainKing/internal/config"
	apierrors "github.com/michaelwang123/mountainKing/internal/errors"
	"github.com/michaelwang123/mountainKing/pkg/retry"
)

// DataSourceStatus tracks the runtime status of a data source.
type DataSourceStatus struct {
	// Available indicates whether the data source is currently reachable.
	Available bool
	// LastError holds the most recent error encountered during connection or health check.
	LastError error
	// ReconnectCount tracks the number of reconnection attempts made.
	ReconnectCount int
	// NextReconnectAt is the earliest time the next reconnection attempt will be made.
	NextReconnectAt time.Time
}

// DataSourceManager manages all data source lifecycles including initialization,
// lookup, query execution with retry, health checking, and graceful shutdown.
type DataSourceManager struct {
	registry    *AdapterRegistry
	datasources map[string]DataSource
	status      map[string]*DataSourceStatus
	configs     []config.DataSourceConfig
	retryConfig retry.Config
	mu          sync.RWMutex
	logger      *zap.Logger
	stopCh      chan struct{}
}

// NewDataSourceManager creates a new DataSourceManager with the given registry,
// data source configurations, retry configuration, and logger.
func NewDataSourceManager(registry *AdapterRegistry, configs []config.DataSourceConfig, retryCfg retry.Config, logger *zap.Logger) *DataSourceManager {
	return &DataSourceManager{
		registry:    registry,
		datasources: make(map[string]DataSource),
		status:      make(map[string]*DataSourceStatus),
		configs:     configs,
		retryConfig: retryCfg,
		logger:      logger,
		stopCh:      make(chan struct{}),
	}
}

// toDataSourceConfig converts a config.DataSourceConfig to a datasource.DataSourceConfig
// for passing to adapter factories.
func toDataSourceConfig(cfg config.DataSourceConfig) DataSourceConfig {
	return DataSourceConfig{
		Name:       cfg.Name,
		Type:       cfg.Type,
		Enabled:    cfg.Enabled,
		Connection: cfg.Connection,
		Options:    cfg.Options,
	}
}

// Init initializes all enabled data sources from configuration.
// It skips disabled datasources (enabled=false), looks up adapter factories
// from the registry by type name, and attempts to connect each data source.
// Unregistered types are skipped with an error log. Connection failures mark
// the data source as unavailable but do not prevent startup.
func (m *DataSourceManager) Init(ctx context.Context) error {
	for _, cfg := range m.configs {
		if !cfg.Enabled {
			m.logger.Info("skipping disabled datasource", zap.String("name", cfg.Name))
			continue
		}

		factory, ok := m.registry.Get(cfg.Type)
		if !ok {
			m.logger.Error("adapter type not registered, skipping datasource",
				zap.String("name", cfg.Name),
				zap.String("type", cfg.Type),
			)
			continue
		}

		ds, err := factory(cfg.Name, toDataSourceConfig(cfg))
		if err != nil {
			m.logger.Error("failed to create datasource adapter",
				zap.String("name", cfg.Name),
				zap.String("type", cfg.Type),
				zap.Error(err),
			)
			continue
		}

		status := &DataSourceStatus{Available: false}

		// Attempt to connect; failure marks the datasource as unavailable
		// and starts a background reconnect loop.
		if err := ds.Connect(ctx); err != nil {
			m.logger.Error("datasource connection failed, marking unavailable",
				zap.String("name", cfg.Name),
				zap.Error(err),
			)
			status.LastError = err

			m.mu.Lock()
			m.datasources[cfg.Name] = ds
			m.status[cfg.Name] = status
			m.mu.Unlock()

			// Start background reconnect with exponential backoff.
			initialInterval, maxInterval := parseReconnectIntervals(cfg.Options)
			m.startReconnectLoop(cfg.Name, initialInterval, maxInterval)
		} else {
			status.Available = true

			m.mu.Lock()
			m.datasources[cfg.Name] = ds
			m.status[cfg.Name] = status
			m.mu.Unlock()
		}
	}

	return nil
}

// Get returns a data source by name. It returns an error if the data source
// is not found or is currently unavailable.
func (m *DataSourceManager) Get(name string) (DataSource, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ds, ok := m.datasources[name]
	if !ok {
		return nil, apierrors.DatasourceError(
			apierrors.ErrDatasourceUnavailable,
			fmt.Sprintf("datasource %q not found", name),
		)
	}

	st := m.status[name]
	if st != nil && !st.Available {
		return nil, apierrors.DatasourceError(
			apierrors.ErrDatasourceUnavailable,
			fmt.Sprintf("datasource %q is currently unavailable", name),
		)
	}

	return ds, nil
}

// ExecuteWithRetry executes a query against the named data source with retry
// logic for transient errors. It uses retry.Do with the manager's configured
// retry parameters. Circuit breaker integration will be added in a later task.
func (m *DataSourceManager) ExecuteWithRetry(ctx context.Context, dsName string, query QueryRequest) (*QueryResult, error) {
	ds, err := m.Get(dsName)
	if err != nil {
		return nil, err
	}

	return retry.Do(ctx, m.retryConfig, func(ctx context.Context) (*QueryResult, error) {
		return ds.Execute(ctx, query)
	})
}

// HealthCheckAll checks all registered data sources and returns a map of
// data source name to error. A nil error value indicates a healthy data source.
func (m *DataSourceManager) HealthCheckAll(ctx context.Context) map[string]error {
	m.mu.RLock()
	names := make([]string, 0, len(m.datasources))
	for name := range m.datasources {
		names = append(names, name)
	}
	m.mu.RUnlock()

	results := make(map[string]error, len(names))
	for _, name := range names {
		m.mu.RLock()
		ds := m.datasources[name]
		m.mu.RUnlock()

		err := ds.HealthCheck(ctx)
		results[name] = err

		// Update status based on health check result.
		m.mu.Lock()
		if st, ok := m.status[name]; ok {
			if err != nil {
				st.Available = false
				st.LastError = err
			} else {
				st.Available = true
				st.LastError = nil
			}
		}
		m.mu.Unlock()
	}

	return results
}

// CloseAll signals background goroutines to stop and closes all data source
// connections. It collects any errors encountered during shutdown.
func (m *DataSourceManager) CloseAll(ctx context.Context) error {
	// Signal background goroutines (e.g., reconnect loops) to stop.
	close(m.stopCh)

	m.mu.RLock()
	names := make([]string, 0, len(m.datasources))
	for name := range m.datasources {
		names = append(names, name)
	}
	m.mu.RUnlock()

	var errs []error
	for _, name := range names {
		m.mu.RLock()
		ds := m.datasources[name]
		m.mu.RUnlock()

		if err := ds.Close(ctx); err != nil {
			m.logger.Error("failed to close datasource",
				zap.String("name", name),
				zap.Error(err),
			)
			errs = append(errs, fmt.Errorf("close %s: %w", name, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors closing datasources: %v", errs)
	}
	return nil
}

// GetWritable returns a WritableDataSource by name.
// Returns DATASOURCE_UNAVAILABLE if the datasource is not found or not writable.
func (m *DataSourceManager) GetWritable(name string) (WritableDataSource, error) {
	ds, err := m.Get(name)
	if err != nil {
		return nil, err
	}
	wds, ok := ds.(WritableDataSource)
	if !ok {
		return nil, apierrors.DatasourceError(
			apierrors.ErrDatasourceUnavailable,
			fmt.Sprintf("datasource %q does not support write operations", name),
		)
	}
	return wds, nil
}

// Status returns the current status of a data source by name.
// Returns nil if the data source is not found.
func (m *DataSourceManager) Status(name string) *DataSourceStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.status[name]
}
