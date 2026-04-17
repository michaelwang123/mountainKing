package retry

import (
	"errors"
	"fmt"
	"io"
	"net"
	"syscall"
	"testing"
)

// timeoutError is a net.Error that reports Timeout() == true.
type timeoutError struct{ msg string }

func (e *timeoutError) Error() string   { return e.msg }
func (e *timeoutError) Timeout() bool   { return true }
func (e *timeoutError) Temporary() bool { return true }

// Verify interface compliance.
var _ net.Error = (*timeoutError)(nil)

// nonTimeoutNetError is a net.Error that reports Timeout() == false.
type nonTimeoutNetError struct{ msg string }

func (e *nonTimeoutNetError) Error() string   { return e.msg }
func (e *nonTimeoutNetError) Timeout() bool   { return false }
func (e *nonTimeoutNetError) Temporary() bool { return false }

var _ net.Error = (*nonTimeoutNetError)(nil)

func TestIsTransient_Nil(t *testing.T) {
	if IsTransient(nil) {
		t.Error("expected nil error to be non-transient")
	}
}

func TestIsTransient_EOF(t *testing.T) {
	if !IsTransient(io.EOF) {
		t.Error("expected io.EOF to be transient")
	}
}

func TestIsTransient_UnexpectedEOF(t *testing.T) {
	if !IsTransient(io.ErrUnexpectedEOF) {
		t.Error("expected io.ErrUnexpectedEOF to be transient")
	}
}

func TestIsTransient_WrappedEOF(t *testing.T) {
	wrapped := fmt.Errorf("read failed: %w", io.EOF)
	if !IsTransient(wrapped) {
		t.Error("expected wrapped io.EOF to be transient")
	}
}

func TestIsTransient_Timeout(t *testing.T) {
	err := &timeoutError{msg: "connection timed out"}
	if !IsTransient(err) {
		t.Error("expected timeout net.Error to be transient")
	}
}

func TestIsTransient_WrappedTimeout(t *testing.T) {
	inner := &timeoutError{msg: "dial timeout"}
	wrapped := fmt.Errorf("connect: %w", inner)
	if !IsTransient(wrapped) {
		t.Error("expected wrapped timeout error to be transient")
	}
}

func TestIsTransient_ECONNREFUSED(t *testing.T) {
	if !IsTransient(syscall.ECONNREFUSED) {
		t.Error("expected ECONNREFUSED to be transient")
	}
}

func TestIsTransient_ECONNRESET(t *testing.T) {
	if !IsTransient(syscall.ECONNRESET) {
		t.Error("expected ECONNRESET to be transient")
	}
}

func TestIsTransient_WrappedECONNREFUSED(t *testing.T) {
	wrapped := fmt.Errorf("dial tcp: %w", syscall.ECONNREFUSED)
	if !IsTransient(wrapped) {
		t.Error("expected wrapped ECONNREFUSED to be transient")
	}
}

func TestIsTransient_BusinessError(t *testing.T) {
	err := errors.New("ERROR 1064 (42000): You have an error in your SQL syntax")
	if IsTransient(err) {
		t.Error("expected SQL syntax error to be non-transient")
	}
}

func TestIsTransient_NonTimeoutNetError(t *testing.T) {
	err := &nonTimeoutNetError{msg: "dns lookup failed"}
	if IsTransient(err) {
		t.Error("expected non-timeout net.Error to be non-transient")
	}
}

func TestIsBusiness_Nil(t *testing.T) {
	if IsBusiness(nil) {
		t.Error("expected nil error to not be business")
	}
}

func TestIsBusiness_SQLSyntaxError(t *testing.T) {
	err := errors.New("ERROR 1064 (42000): You have an error in your SQL syntax")
	if !IsBusiness(err) {
		t.Error("expected SQL syntax error to be business")
	}
}

func TestIsBusiness_PromQLSyntaxError(t *testing.T) {
	err := errors.New("bad_data: invalid PromQL expression")
	if !IsBusiness(err) {
		t.Error("expected PromQL syntax error to be business")
	}
}

func TestIsBusiness_TransientError(t *testing.T) {
	if IsBusiness(io.EOF) {
		t.Error("expected io.EOF to not be business")
	}
}

func TestIsTransientAndIsBusiness_Mutually_Exclusive(t *testing.T) {
	cases := []error{
		io.EOF,
		io.ErrUnexpectedEOF,
		syscall.ECONNREFUSED,
		syscall.ECONNRESET,
		&timeoutError{msg: "timeout"},
		errors.New("sql syntax error"),
		errors.New("promql error"),
	}
	for _, err := range cases {
		transient := IsTransient(err)
		business := IsBusiness(err)
		if transient == business {
			t.Errorf("error %q: IsTransient=%v IsBusiness=%v — expected exactly one to be true",
				err, transient, business)
		}
	}
}
