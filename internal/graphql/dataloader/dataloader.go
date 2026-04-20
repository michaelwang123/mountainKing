// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

// Package dataloader implements a per-request DataLoader that batches and
// coalesces queries targeting the same data source within a configurable
// time window. Each HTTP request gets its own DataLoader instance â€?sharing
// across requests is forbidden to prevent data leakage.
package dataloader

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/michaelwang123/mountainKing/internal/datasource"
)

// Default batching parameters.
const (
	DefaultBatchWindow = 1 * time.Millisecond
	DefaultMaxBatch    = 100
)

// ctxKey is an unexported type used as the context key for the DataLoader.
type ctxKey struct{}

// LoadResult carries the outcome of a single batched query.
type LoadResult struct {
	Result *datasource.QueryResult
	Err    error
}

// loadRequest is an internal representation of a pending query.
type loadRequest struct {
	dsName   string
	query    datasource.QueryRequest
	resultCh chan *LoadResult
}

// DataLoader collects queries per data source and flushes them either when
// the batch window expires or the batch reaches its maximum size.
type DataLoader struct {
	dsManager   *datasource.DataSourceManager
	batchWindow time.Duration
	maxBatch    int

	mu      sync.Mutex
	pending map[string][]*loadRequest // datasource name â†?pending requests
	timers  map[string]*time.Timer    // datasource name â†?flush timer
	closed  bool
}

// New creates a DataLoader bound to the given DataSourceManager.
func New(dsManager *datasource.DataSourceManager, opts ...Option) *DataLoader {
	dl := &DataLoader{
		dsManager:   dsManager,
		batchWindow: DefaultBatchWindow,
		maxBatch:    DefaultMaxBatch,
		pending:     make(map[string][]*loadRequest),
		timers:      make(map[string]*time.Timer),
	}
	for _, o := range opts {
		o(dl)
	}
	return dl
}

// Option configures a DataLoader.
type Option func(*DataLoader)

// WithBatchWindow overrides the default batch window duration.
func WithBatchWindow(d time.Duration) Option {
	return func(dl *DataLoader) { dl.batchWindow = d }
}

// WithMaxBatch overrides the default maximum batch size.
func WithMaxBatch(n int) Option {
	return func(dl *DataLoader) { dl.maxBatch = n }
}

// Load submits a query for the named data source and blocks until the result
// is available. It respects context cancellation â€?if the context is cancelled
// before the result arrives the caller receives the context error.
func (dl *DataLoader) Load(ctx context.Context, dsName string, query datasource.QueryRequest) (*datasource.QueryResult, error) {
	ch := make(chan *LoadResult, 1)

	dl.mu.Lock()
	if dl.closed {
		dl.mu.Unlock()
		return nil, context.Canceled
	}

	req := &loadRequest{dsName: dsName, query: query, resultCh: ch}
	dl.pending[dsName] = append(dl.pending[dsName], req)
	batch := dl.pending[dsName]

	// If the batch is full, flush immediately.
	if len(batch) >= dl.maxBatch {
		dl.stopTimer(dsName)
		reqs := dl.takePending(dsName)
		dl.mu.Unlock()
		dl.executeBatch(context.Background(), dsName, reqs)
	} else {
		// Start or reset the batch window timer.
		dl.ensureTimer(dsName)
		dl.mu.Unlock()
	}

	// Wait for result or context cancellation.
	select {
	case res := <-ch:
		return res.Result, res.Err
	case <-ctx.Done():
		// Context cancelled â€?trigger an immediate async flush so other
		// waiters in the same batch are not stuck forever.
		dl.mu.Lock()
		if reqs := dl.takePending(dsName); len(reqs) > 0 {
			dl.stopTimer(dsName)
			dl.mu.Unlock()
			go dl.executeBatch(context.Background(), dsName, reqs)
		} else {
			dl.mu.Unlock()
		}
		// Try to drain our result one more time in case the flush already
		// completed before we got here.
		select {
		case res := <-ch:
			return res.Result, res.Err
		default:
			return nil, ctx.Err()
		}
	}
}

// Close flushes all pending batches and marks the DataLoader as closed.
// After Close returns, subsequent Load calls return context.Canceled.
func (dl *DataLoader) Close() {
	dl.mu.Lock()
	dl.closed = true
	// Collect all pending batches.
	allPending := make(map[string][]*loadRequest, len(dl.pending))
	for ds, reqs := range dl.pending {
		allPending[ds] = reqs
		dl.stopTimer(ds)
	}
	dl.pending = make(map[string][]*loadRequest)
	dl.mu.Unlock()

	for ds, reqs := range allPending {
		if len(reqs) > 0 {
			dl.executeBatch(context.Background(), ds, reqs)
		}
	}
}

// executeBatch runs each pending request against the DataSourceManager and
// delivers results to the waiting callers.
func (dl *DataLoader) executeBatch(ctx context.Context, dsName string, reqs []*loadRequest) {
	for _, r := range reqs {
		result, err := dl.dsManager.ExecuteWithRetry(ctx, r.dsName, r.query)
		r.resultCh <- &LoadResult{Result: result, Err: err}
	}
}

// ensureTimer starts a flush timer for the given data source if one is not
// already running. Must be called with dl.mu held.
func (dl *DataLoader) ensureTimer(dsName string) {
	if _, ok := dl.timers[dsName]; ok {
		return
	}
	dl.timers[dsName] = time.AfterFunc(dl.batchWindow, func() {
		dl.mu.Lock()
		reqs := dl.takePending(dsName)
		delete(dl.timers, dsName)
		dl.mu.Unlock()
		if len(reqs) > 0 {
			dl.executeBatch(context.Background(), dsName, reqs)
		}
	})
}

// stopTimer stops and removes the flush timer for the given data source.
// Must be called with dl.mu held.
func (dl *DataLoader) stopTimer(dsName string) {
	if t, ok := dl.timers[dsName]; ok {
		t.Stop()
		delete(dl.timers, dsName)
	}
}

// takePending removes and returns all pending requests for the given data
// source. Must be called with dl.mu held.
func (dl *DataLoader) takePending(dsName string) []*loadRequest {
	reqs := dl.pending[dsName]
	delete(dl.pending, dsName)
	return reqs
}

// NewMiddleware returns an HTTP middleware that creates a per-request
// DataLoader and injects it into the request context. The DataLoader is
// closed when the request handler returns.
func NewMiddleware(dsManager *datasource.DataSourceManager, opts ...Option) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			dl := New(dsManager, opts...)
			ctx := context.WithValue(r.Context(), ctxKey{}, dl)
			defer dl.Close()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ForContext retrieves the DataLoader from the request context. It returns
// nil if no DataLoader is present.
func ForContext(ctx context.Context) *DataLoader {
	dl, _ := ctx.Value(ctxKey{}).(*DataLoader)
	return dl
}
