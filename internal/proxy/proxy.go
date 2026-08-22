// Package proxy builds the HTTP reverse proxy that forwards client
// requests to a backend server.
package proxy

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
)

// New builds a reverse proxy that forwards every request to target.
//
// It has no load-balancing logic yet: there is exactly one backend, so
// there is nothing to choose between. That arrives once Phase 2 introduces
// more than one backend.
func New(target *url.URL, logger *log.Logger) *httputil.ReverseProxy {
	return &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.SetXForwarded()
		},
		ErrorLog: logger,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			logger.Printf("backend error for %s %s: %v", r.Method, r.URL.Path, err)
			w.WriteHeader(http.StatusBadGateway)
		},
	}
}
