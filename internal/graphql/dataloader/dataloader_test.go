package dataloader

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/example/graphql-api/internal/config"
	"github.com/example/graphql-api/internal/datasource"
	"github.com/example/graphql-api/pkg/retry"
	"go.uber.org/zap"
)

// newTestManager creates a DataSourceManager with a single MockDataSource.
func newTestManager(t *testing.T, dsName string, execFn func(ctx context.Context, q datasource.QueryRequest) (*datasource.QueryResult, error)) *datasource.DataSourceManager {
	t.Helper()
	registry := datasource.NewAdapterRegistry()
	err := registry.Register("mock", func(name string, cfg datasource.DataSourceConfig) (datasource.DataSource, error) {
		return &datasource.MockDataSource{
			NameVal:      name,
			TypeVal:      "mock",
			AvailableVal: true,
			ConnectFunc:  func(ctx context.Context) error { return nil },
			ExecuteFunc:  execFn,
		}, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	logger, _ := zap.NewDevelopment()
	mgr := datasource.NewDataSourceManager(
		registry,
		[]config.DataSourceConfig{
			{Name: dsName, Type: "mock", Enabled: true, Connection: map[string]interface{}{}, Options: map[string]interface{}{}},
		},
		retry.Config{MaxRetries: 0, RetryInterval: time.Millisecond},
		logger,
	)
	if err := mgr.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	return mgr
}

func TestLoad_SingleQuery(t *testing.T) {
	mgr := newTestManager(t, "ds1", func(_ context.Context, q datasource.QueryRequest) (*datasource.QueryResult, error) {
		return &datasource.QueryResult{
			Data: []map[string]interface{}{{"id": 1}},
		}, nil
	})

	dl := New(mgr)
	defer dl.Close()

	res, err := dl.Load(context.Background(), "ds1", datasource.QueryRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Data) != 1 {
		t.Fatalf("expected 1 row, got %d", len(res.Data))
	}
}

func TestLoad_BatchCoalescing(t *testing.T) {
	var callCount atomic.Int32
	mgr := newTestManager(t, "ds1", func(_ context.Context, q datasource.QueryRequest) (*datasource.QueryResult, error) {
		callCount.Add(1)
		return &datasource.QueryResult{
			Data: []map[string]interface{}{{"ok": true}},
		}, nil
	})

	// Use a longer batch window so all goroutines queue before the timer fires.
	dl := New(mgr, WithBatchWindow(50*time.Millisecond))
	defer dl.Close()

	const n = 5
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			res, err := dl.Load(context.Background(), "ds1", datasource.QueryRequest{})
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if len(res.Data) != 1 {
				t.Errorf("expected 1 row, got %d", len(res.Data))
			}
		}()
	}
	wg.Wait()

	// Each request is executed individually against the datasource manager,
	// but they are batched within the same timer window.
	if callCount.Load() != int32(n) {
		t.Fatalf("expected %d execute calls, got %d", n, callCount.Load())
	}
}

func TestLoad_MaxBatchFlush(t *testing.T) {
	var callCount atomic.Int32
	mgr := newTestManager(t, "ds1", func(_ context.Context, q datasource.QueryRequest) (*datasource.QueryResult, error) {
		callCount.Add(1)
		return &datasource.QueryResult{}, nil
	})

	// Very long window — flush should be triggered by max batch size.
	dl := New(mgr, WithBatchWindow(10*time.Second), WithMaxBatch(3))
	defer dl.Close()

	var wg sync.WaitGroup
	wg.Add(3)
	for i := 0; i < 3; i++ {
		go func() {
			defer wg.Done()
			_, err := dl.Load(context.Background(), "ds1", datasource.QueryRequest{})
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()

	if callCount.Load() != 3 {
		t.Fatalf("expected 3 calls, got %d", callCount.Load())
	}
}

func TestLoad_ContextCancellation(t *testing.T) {
	// Block the execute call so we can test cancellation.
	mgr := newTestManager(t, "ds1", func(ctx context.Context, q datasource.QueryRequest) (*datasource.QueryResult, error) {
		time.Sleep(5 * time.Second)
		return &datasource.QueryResult{}, nil
	})

	dl := New(mgr, WithBatchWindow(100*time.Millisecond))
	defer dl.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := dl.Load(ctx, "ds1", datasource.QueryRequest{})
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
	if err != context.DeadlineExceeded {
		t.Fatalf("expected DeadlineExceeded, got %v", err)
	}
}

func TestLoad_DatasourceError(t *testing.T) {
	mgr := newTestManager(t, "ds1", func(_ context.Context, q datasource.QueryRequest) (*datasource.QueryResult, error) {
		return nil, fmt.Errorf("query failed")
	})

	dl := New(mgr)
	defer dl.Close()

	_, err := dl.Load(context.Background(), "ds1", datasource.QueryRequest{})
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "query failed" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoad_PerRequestIsolation(t *testing.T) {
	var callCount atomic.Int32
	mgr := newTestManager(t, "ds1", func(_ context.Context, q datasource.QueryRequest) (*datasource.QueryResult, error) {
		callCount.Add(1)
		return &datasource.QueryResult{
			Data: []map[string]interface{}{{"req": "ok"}},
		}, nil
	})

	// Two independent DataLoader instances (simulating two requests).
	dl1 := New(mgr)
	dl2 := New(mgr)

	res1, err := dl1.Load(context.Background(), "ds1", datasource.QueryRequest{})
	if err != nil {
		t.Fatalf("dl1 error: %v", err)
	}
	dl1.Close()

	res2, err := dl2.Load(context.Background(), "ds1", datasource.QueryRequest{})
	if err != nil {
		t.Fatalf("dl2 error: %v", err)
	}
	dl2.Close()

	// Both should have gotten results independently.
	if len(res1.Data) != 1 || len(res2.Data) != 1 {
		t.Fatal("expected independent results from each DataLoader")
	}
	if callCount.Load() != 2 {
		t.Fatalf("expected 2 independent execute calls, got %d", callCount.Load())
	}
}

func TestLoad_AfterClose(t *testing.T) {
	mgr := newTestManager(t, "ds1", func(_ context.Context, q datasource.QueryRequest) (*datasource.QueryResult, error) {
		return &datasource.QueryResult{}, nil
	})

	dl := New(mgr)
	dl.Close()

	_, err := dl.Load(context.Background(), "ds1", datasource.QueryRequest{})
	if err != context.Canceled {
		t.Fatalf("expected context.Canceled after Close, got %v", err)
	}
}

func TestMiddleware_InjectsDataLoader(t *testing.T) {
	mgr := newTestManager(t, "ds1", func(_ context.Context, q datasource.QueryRequest) (*datasource.QueryResult, error) {
		return &datasource.QueryResult{Data: []map[string]interface{}{{"v": 42}}}, nil
	})

	var captured *DataLoader
	handler := NewMiddleware(mgr)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = ForContext(r.Context())
		if captured == nil {
			t.Error("DataLoader not found in context")
			return
		}
		res, err := captured.Load(r.Context(), "ds1", datasource.QueryRequest{})
		if err != nil {
			t.Errorf("Load error: %v", err)
			return
		}
		if len(res.Data) != 1 {
			t.Errorf("expected 1 row, got %d", len(res.Data))
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/graphql", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestForContext_NilWhenMissing(t *testing.T) {
	dl := ForContext(context.Background())
	if dl != nil {
		t.Fatal("expected nil DataLoader from empty context")
	}
}

func TestLoad_MultipleDatasources(t *testing.T) {
	// Set up a manager with two datasources.
	registry := datasource.NewAdapterRegistry()
	_ = registry.Register("mock", func(name string, cfg datasource.DataSourceConfig) (datasource.DataSource, error) {
		return &datasource.MockDataSource{
			NameVal:      name,
			TypeVal:      "mock",
			AvailableVal: true,
			ConnectFunc:  func(ctx context.Context) error { return nil },
			ExecuteFunc: func(_ context.Context, q datasource.QueryRequest) (*datasource.QueryResult, error) {
				return &datasource.QueryResult{
					Data: []map[string]interface{}{{"source": name}},
				}, nil
			},
		}, nil
	})

	logger, _ := zap.NewDevelopment()
	mgr := datasource.NewDataSourceManager(
		registry,
		[]config.DataSourceConfig{
			{Name: "ds_a", Type: "mock", Enabled: true, Connection: map[string]interface{}{}, Options: map[string]interface{}{}},
			{Name: "ds_b", Type: "mock", Enabled: true, Connection: map[string]interface{}{}, Options: map[string]interface{}{}},
		},
		retry.Config{MaxRetries: 0, RetryInterval: time.Millisecond},
		logger,
	)
	if err := mgr.Init(context.Background()); err != nil {
		t.Fatal(err)
	}

	dl := New(mgr)
	defer dl.Close()

	var wg sync.WaitGroup
	wg.Add(2)

	var resA, resB *datasource.QueryResult
	var errA, errB error

	go func() {
		defer wg.Done()
		resA, errA = dl.Load(context.Background(), "ds_a", datasource.QueryRequest{})
	}()
	go func() {
		defer wg.Done()
		resB, errB = dl.Load(context.Background(), "ds_b", datasource.QueryRequest{})
	}()
	wg.Wait()

	if errA != nil || errB != nil {
		t.Fatalf("errors: a=%v b=%v", errA, errB)
	}
	if resA.Data[0]["source"] != "ds_a" || resB.Data[0]["source"] != "ds_b" {
		t.Fatal("results mixed between datasources")
	}
}
