// Package proxy builds the HTTP reverse proxy that forwards client
// requests to a backend server.
package proxy

import (
	"io"
	"log"
	"net/http"
	"net/http/httputil"

	"github.com/jerryschen31/system-design-load-balancer/internal/balancer"
)

// New builds a reverse proxy that forwards every request to a backend
// chosen by b. Each request picks its target independently by calling
// b.Next(), so which backend handles a given request is entirely up to
// the balancer's selection policy (round-robin, and later phases' other
// strategies), not anything the proxy itself decides.
func New(b balancer.Balancer, logger *log.Logger) *httputil.ReverseProxy {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}

	return &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			// Rewrite runs once per incoming request, on that request's own
			// goroutine, before it's forwarded. Calling b.Next() here --
			// rather than resolving the target once outside New -- is what
			// makes every request independently eligible for a different
			// backend.
			pr.SetURL(b.Next())
			pr.SetXForwarded()
		},
		ErrorLog: logger,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			logger.Printf("backend error for %s %s: %v", r.Method, r.URL.Path, err)
			w.WriteHeader(http.StatusBadGateway)
		},
	}
}
