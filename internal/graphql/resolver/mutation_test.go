package resolver

import (
	"context"
	"errors"
	"testing"
)

// mockCacheClearer is a test double implementing the CacheClearer interface.
type mockCacheClearer struct {
	clearByDSCalled bool
	clearByDSArg    string
	clearAllCalled  bool
	err             error
}

func (m *mockCacheClearer) ClearByDatasource(_ context.Context, datasource string) error {
	m.clearByDSCalled = true
	m.clearByDSArg = datasource
	return m.err
}

func (m *mockCacheClearer) ClearAll(_ context.Context) error {
	m.clearAllCalled = true
	return m.err
}

func TestClearCache_NilCacheClearer(t *testing.T) {
	r := &mutationResolver{&Resolver{CacheClearer: nil}}
	result, err := r.ClearCache(context.Background(), nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !result {
		t.Error("expected true when cache is disabled")
	}
}

func TestClearCache_NilCacheClearer_WithDatasource(t *testing.T) {
	r := &mutationResolver{&Resolver{CacheClearer: nil}}
	ds := "analytics_db"
	result, err := r.ClearCache(context.Background(), &ds)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !result {
		t.Error("expected true when cache is disabled")
	}
}

func TestClearCache_ClearAll(t *testing.T) {
	mock := &mockCacheClearer{}
	r := &mutationResolver{&Resolver{CacheClearer: mock}}

	result, err := r.ClearCache(context.Background(), nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !result {
		t.Error("expected true on success")
	}
	if !mock.clearAllCalled {
		t.Error("expected ClearAll to be called")
	}
	if mock.clearByDSCalled {
		t.Error("expected ClearByDatasource not to be called")
	}
}

func TestClearCache_ClearByDatasource(t *testing.T) {
	mock := &mockCacheClearer{}
	r := &mutationResolver{&Resolver{CacheClearer: mock}}
	ds := "monitoring"

	result, err := r.ClearCache(context.Background(), &ds)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !result {
		t.Error("expected true on success")
	}
	if !mock.clearByDSCalled {
		t.Error("expected ClearByDatasource to be called")
	}
	if mock.clearByDSArg != "monitoring" {
		t.Errorf("expected datasource arg 'monitoring', got %q", mock.clearByDSArg)
	}
	if mock.clearAllCalled {
		t.Error("expected ClearAll not to be called")
	}
}

func TestClearCache_ClearAll_Error(t *testing.T) {
	mock := &mockCacheClearer{err: errors.New("redis connection failed")}
	r := &mutationResolver{&Resolver{CacheClearer: mock}}

	result, err := r.ClearCache(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if result {
		t.Error("expected false on error")
	}
	if !mock.clearAllCalled {
		t.Error("expected ClearAll to be called")
	}
}

func TestClearCache_ClearByDatasource_Error(t *testing.T) {
	mock := &mockCacheClearer{err: errors.New("scan failed")}
	r := &mutationResolver{&Resolver{CacheClearer: mock}}
	ds := "analytics_db"

	result, err := r.ClearCache(context.Background(), &ds)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if result {
		t.Error("expected false on error")
	}
	if !mock.clearByDSCalled {
		t.Error("expected ClearByDatasource to be called")
	}
}
