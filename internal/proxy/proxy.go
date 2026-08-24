// Package proxy builds the HTTP reverse proxy that forwards client
// requests to a backend server.
package proxy

import (
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httputil"

	"github.com/jerryschen31/system-design-load-balancer/internal/balancer"
)

// errNoHealthyBackends is returned by the roundTripperFunc below when
// Rewrite couldn't set a target at all, i.e. balancer.Next() returned nil
// because every backend is currently marked unhealthy.
var errNoHealthyBackends = errors.New("proxy: no healthy backends available")

// roundTripperFunc adapts a plain function to the http.RoundTripper
// interface, the same "function implementing an interface" idiom as
// http.HandlerFunc: it lets us hand ReverseProxy.Transport a closure
// instead of having to declare a named struct type just to satisfy the
// interface's single method.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

// New builds a reverse proxy that forwards every request to a backend
// chosen by b. Each request picks its target independently by calling
// b.Next(), so which backend handles a given request is entirely up to
// the balancer's selection policy (round-robin, and later phases' other
// strategies), not anything the proxy itself decides.
func New(b balancer.Balancer, logger *log.Logger) *httputil.ReverseProxy {
	if b == nil {
		// Fail at construction, not on the first request: a nil balancer
		// would otherwise panic inside Rewrite on whichever goroutine
		// happens to handle the first incoming request, which is a much
		// harder failure to trace back to its actual cause.
		panic("proxy: New requires a non-nil Balancer")
	}
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
			target := b.Next()
			if target == nil {
				// No healthy backend to send this request to. We can't
				// call pr.SetURL(nil) -- it dereferences the URL
				// internally and would panic on this request's
				// goroutine, which net/http would recover from by just
				// closing the connection, not by giving the client a
				// clean response. Instead, leave pr.Out.URL unset (its
				// Host stays "") and let the custom Transport below
				// catch that and fail the request through the ordinary
				// ErrorHandler path.
				return
			}
			pr.SetURL(target)
			pr.SetXForwarded()
			// Which backend a given request landed on was previously only
			// inferable indirectly (cross-referencing this process's logs
			// with each backend's own). Logging it here -- the one place
			// the decision is actually made -- records it as a fact at the
			// moment it happens, rather than reconstructing it after.
			logger.Printf("routed %s %s -> %s", pr.In.Method, pr.In.URL.Path, target)
		},
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL == nil || r.URL.Host == "" {
				// Fail fast: no backend was set, so don't let the
				// default transport try to dial an empty host.
				return nil, errNoHealthyBackends
			}
			return http.DefaultTransport.RoundTrip(r)
		}),
		ErrorLog: logger,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			logger.Printf("backend error for %s %s: %v", r.Method, r.URL.Path, err)
			if errors.Is(err, errNoHealthyBackends) {
				// A specific, honest message for the specific case: the
				// balancer itself refused to pick a target, as opposed to
				// picking one and having the request to it fail. http.Error
				// sets Content-Type, writes the status, and writes the
				// message plus a trailing newline in one call.
				http.Error(w, "no healthy backends available", http.StatusBadGateway)
				return
			}
			w.WriteHeader(http.StatusBadGateway)
		},
	}
}
