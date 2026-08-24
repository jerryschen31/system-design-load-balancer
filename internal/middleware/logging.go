// Package middleware holds cross-cutting HTTP concerns (logging and a
// concurrency limiter now; rate limiting and caching will land here in
// later phases) that wrap the core proxy handler rather than living
// inside it.
package middleware

import (
	"io"
	"log"
	"net/http"
	"time"
)

// statusRecorder wraps http.ResponseWriter to capture the status code
// written by the inner handler, since the standard interface has no way
// to read it back afterward.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// Logging returns middleware that logs method, path, status, and latency
// for every request that passes through the handler it wraps.
func Logging(logger *log.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			start := time.Now()
			next.ServeHTTP(rec, r)
			logger.Printf("%s %s %d %s", r.Method, r.URL.Path, rec.status, time.Since(start))
		})
	}
}
