// Command echobackend is a minimal stand-in backend for manually testing
// the load balancer. It does nothing but report its own listen address in
// every response body, so a request routed through the load balancer's
// round-robin (or, later, any other selection policy) can be traced back
// to the specific backend that handled it just by reading the response --
// no log-watching across terminals required.
//
// It also serves a /health endpoint, so the load balancer's active health
// checker has something to poll, plus a POST /health/toggle endpoint for
// manually flipping this backend's reported health during a demo --
// letting you watch the load balancer route around a backend that's still
// running but failing its health check, distinct from killing the process
// outright (which phases 1-2 already cover via connection-refused).
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync/atomic"
)

// newHandler builds the handler this backend serves every non-health
// request with. addr is baked in at construction rather than read from the
// request, since what we want to prove (in the round-robin/hey
// simulations) is "which backend process answered this," and a backend
// can't discover its own bind address from an incoming request.
func newHandler(logger *log.Logger, addr string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger.Printf("%s %s", r.Method, r.URL.Path)
		fmt.Fprintf(w, "echobackend %s handled %s %s\n", addr, r.Method, r.URL.Path)
	}
}

// newHealthHandler reports the current value of healthy: 200 if true, 503
// if false. healthy is an *atomic.Bool rather than a plain bool because
// this handler and the toggle handler below both run per-request, on
// whichever goroutine net/http assigns that request to -- concurrent
// requests to /health and /health/toggle are a normal, expected case, not
// an edge case to special-case around.
func newHealthHandler(logger *log.Logger, healthy *atomic.Bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if healthy.Load() {
			w.WriteHeader(http.StatusOK)
			fmt.Fprintln(w, "ok")
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprintln(w, "unhealthy")
	}
}

// newToggleHandler flips healthy's value and reports the new state. Only
// POST is accepted -- a GET is what a browser or a curl-without-flags
// issues by default, and a state-changing action shouldn't be reachable by
// something that easy to trigger by accident (e.g. a link preview fetcher).
func newToggleHandler(logger *log.Logger, healthy *atomic.Bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		newState := !healthy.Load()
		healthy.Store(newState)
		logger.Printf("health toggled to healthy=%v", newState)
		fmt.Fprintf(w, "healthy=%v\n", newState)
	}
}

func main() {
	listenAddr := flag.String("listen", ":9001", "address for this backend to listen on")
	flag.Parse()

	logger := log.New(os.Stdout, "", log.LstdFlags)
	logger.Printf("echobackend listening on %s", *listenAddr)

	var healthy atomic.Bool
	healthy.Store(true) // starts healthy; toggle it down with POST /health/toggle

	mux := http.NewServeMux()
	mux.Handle("/health", newHealthHandler(logger, &healthy))
	mux.Handle("/health/toggle", newToggleHandler(logger, &healthy))
	mux.Handle("/", newHandler(logger, *listenAddr))

	if err := http.ListenAndServe(*listenAddr, mux); err != nil {
		logger.Fatalf("server error: %v", err)
	}
}
