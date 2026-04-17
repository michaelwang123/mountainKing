package middleware

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/example/graphql-api/internal/config"
)

// defaultMinCompressSize is the default minimum response size for compression (1KB).
const defaultMinCompressSize int64 = 1 << 10

// gzipWriterPool reuses gzip writers to reduce allocations.
var gzipWriterPool = sync.Pool{
	New: func() any {
		w, _ := gzip.NewWriterLevel(io.Discard, gzip.DefaultCompression)
		return w
	},
}

// Compression returns a chi-compatible middleware that conditionally applies
// gzip compression to response bodies. When compression is disabled via config,
// a no-op middleware is returned.
//
// When enabled the middleware:
//   - Checks the Accept-Encoding header for "gzip"
//   - Buffers the response to compare its size against the minimum threshold
//   - Compresses with gzip and sets Content-Encoding: gzip when the threshold is exceeded
//   - Sends the response uncompressed when below the threshold
func Compression(cfg config.CompressionConfig) func(http.Handler) http.Handler {
	if !cfg.Enabled {
		return func(next http.Handler) http.Handler {
			return next
		}
	}

	minSize := defaultMinCompressSize
	if cfg.MinSize != "" {
		parsed, err := ParseSizeString(cfg.MinSize)
		if err == nil && parsed > 0 {
			minSize = parsed
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Only compress when the client accepts gzip.
			if !acceptsGzip(r) {
				next.ServeHTTP(w, r)
				return
			}

			// Use a buffered response writer to capture the response.
			bw := &bufferedResponseWriter{
				ResponseWriter: w,
				buf:            &bytes.Buffer{},
			}

			next.ServeHTTP(bw, r)

			body := bw.buf.Bytes()

			if int64(len(body)) >= minSize {
				w.Header().Set("Content-Encoding", "gzip")
				w.Header().Del("Content-Length")
				if bw.statusCode != 0 {
					w.WriteHeader(bw.statusCode)
				}

				gz := gzipWriterPool.Get().(*gzip.Writer)
				gz.Reset(w)
				_, _ = gz.Write(body)
				_ = gz.Close()
				gzipWriterPool.Put(gz)
			} else {
				// Below threshold — send uncompressed.
				if bw.statusCode != 0 {
					w.WriteHeader(bw.statusCode)
				}
				_, _ = w.Write(body)
			}
		})
	}
}

// acceptsGzip returns true when the request's Accept-Encoding header
// contains "gzip".
func acceptsGzip(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept-Encoding"), "gzip")
}

// bufferedResponseWriter captures the response body and status code so the
// compression middleware can decide whether to compress after the full
// response has been written.
type bufferedResponseWriter struct {
	http.ResponseWriter
	buf        *bytes.Buffer
	statusCode int
}

func (bw *bufferedResponseWriter) WriteHeader(code int) {
	bw.statusCode = code
}

func (bw *bufferedResponseWriter) Write(b []byte) (int, error) {
	return bw.buf.Write(b)
}
