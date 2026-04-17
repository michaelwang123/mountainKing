// Package retry provides error classification and retry logic with
// exponential backoff for the GraphQL Multi-DataSource API service.
package retry

import (
	"errors"
	"io"
	"net"
	"syscall"
)

// IsTransient returns true if the error is a transient (retryable) error.
// Transient errors include:
//   - Connection timeout (net.Error with Timeout())
//   - Connection refused (syscall.ECONNREFUSED)
//   - Connection reset (syscall.ECONNRESET)
//   - Unexpected EOF (io.EOF, io.ErrUnexpectedEOF)
func IsTransient(err error) bool {
	if err == nil {
		return false
	}

	// Check for io.EOF or io.ErrUnexpectedEOF.
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}

	// Check for syscall-level connection errors (ECONNREFUSED, ECONNRESET).
	var errno syscall.Errno
	if errors.As(err, &errno) {
		if errno == syscall.ECONNREFUSED || errno == syscall.ECONNRESET {
			return true
		}
	}

	// Check for net.Error with Timeout().
	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return true
		}
	}

	return false
}

// IsBusiness returns true if the error is a business (non-retryable) error.
// Business errors include SQL syntax errors, PromQL syntax errors, etc.
// This is the inverse of IsTransient — if an error is not transient and
// not nil, it is considered a business error.
func IsBusiness(err error) bool {
	if err == nil {
		return false
	}
	return !IsTransient(err)
}
