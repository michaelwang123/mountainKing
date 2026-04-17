package retry

import (
	"context"
	"errors"
	"io"
	"syscall"
	"testing"
	"time"
)

func TestDo_SuccessOnFirstAttempt(t *testing.T) {
	cfg := Config{MaxRetries: 3, RetryInterval: time.Millisecond}
	calls := 0

	result, err := Do(context.Background(), cfg, func(ctx context.Context) (string, error) {
		calls++
		return "ok", nil
	})

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result != "ok" {
		t.Fatalf("expected 'ok', got %q", result)
	}
	if calls != 1 {
		t.Fatalf("expected 1 call, got %d", calls)
	}
}

func TestDo_BusinessErrorReturnsImmediately(t *testing.T) {
	cfg := Config{MaxRetries: 3, RetryInterval: time.Millisecond}
	calls := 0
	bizErr := errors.New("SQL syntax error")

	_, err := Do(context.Background(), cfg, func(ctx context.Context) (int, error) {
		calls++
		return 0, bizErr
	})

	if !errors.Is(err, bizErr) {
		t.Fatalf("expected business error, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 call (no retry for business error), got %d", calls)
	}
}

func TestDo_TransientErrorRetriesAndSucceeds(t *testing.T) {
	cfg := Config{MaxRetries: 3, RetryInterval: time.Millisecond}
	calls := 0

	result, err := Do(context.Background(), cfg, func(ctx context.Context) (string, error) {
		calls++
		if calls < 3 {
			return "", io.EOF // transient error
		}
		return "recovered", nil
	})

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result != "recovered" {
		t.Fatalf("expected 'recovered', got %q", result)
	}
	if calls != 3 {
		t.Fatalf("expected 3 calls, got %d", calls)
	}
}

func TestDo_TransientErrorExhaustsRetries(t *testing.T) {
	cfg := Config{MaxRetries: 2, RetryInterval: time.Millisecond}
	calls := 0

	_, err := Do(context.Background(), cfg, func(ctx context.Context) (string, error) {
		calls++
		return "", io.ErrUnexpectedEOF // transient error
	})

	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("expected wrapped ErrUnexpectedEOF, got %v", err)
	}
	// 1 initial + 2 retries = 3 total calls
	if calls != 3 {
		t.Fatalf("expected 3 calls (1 initial + 2 retries), got %d", calls)
	}
}

func TestDo_ContextCancellationStopsRetry(t *testing.T) {
	cfg := Config{MaxRetries: 10, RetryInterval: time.Second}
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0

	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()

	_, err := Do(ctx, cfg, func(ctx context.Context) (string, error) {
		calls++
		return "", syscall.ECONNREFUSED // transient
	})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestDo_ZeroMaxRetries(t *testing.T) {
	cfg := Config{MaxRetries: 0, RetryInterval: time.Millisecond}
	calls := 0

	_, err := Do(context.Background(), cfg, func(ctx context.Context) (string, error) {
		calls++
		return "", io.EOF
	})

	if err == nil {
		t.Fatal("expected error with 0 retries")
	}
	if calls != 1 {
		t.Fatalf("expected 1 call with 0 retries, got %d", calls)
	}
}

func TestDo_GenericTypeInt(t *testing.T) {
	cfg := Config{MaxRetries: 1, RetryInterval: time.Millisecond}

	result, err := Do(context.Background(), cfg, func(ctx context.Context) (int, error) {
		return 42, nil
	})

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result != 42 {
		t.Fatalf("expected 42, got %d", result)
	}
}
