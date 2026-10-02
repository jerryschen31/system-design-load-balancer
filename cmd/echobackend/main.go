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
// outright (which phases 1-2 already cover via connection-refused). Plus a
// /hang endpoint that never responds, for demonstrating that the load
// balancer currently has nothing bounding how long it will wait on a
// backend that's healthy but stuck. Plus a /sleep?seconds=N endpoint that
// responds after a controllable delay (unlike /hang, which never responds
// on its own) -- the fixture for phase 4a's backend-response timeout and
// concurrency limiter: fire enough concurrent /sleep requests and they pile
// up until the limiter's ceiling is hit, or set a backend timeout shorter
// than the sleep and watch the load balancer cut the request short.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync/atomic"
	"time"
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
		cur := healthy.Load()
		logger.Printf("%s %s healthy=%v", r.Method, r.URL.Path, cur)
		if cur {
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

// newHangHandler never writes a response -- it blocks until the request's
// context is done (the client gives up and closes the connection, or the
// process exits). It exists purely as a manual test fixture: routing a
// request here simulates a backend that has accepted the TCP connection but
// is stuck (deadlocked, waiting on a downstream dependency that never
// answers, etc.) rather than one that's down. Unlike /health/toggle, this
// doesn't change any state a health check would ever observe -- /health
// keeps answering normally on its own goroutine, so a backend serving a
// hung /hang request still reports healthy, which is the point: it shows
// the load balancer has nothing today that bounds how long it will wait on
// a backend that accepted the request but never responds.
func newHangHandler(logger *log.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger.Printf("%s %s: hanging until the connection is dropped", r.Method, r.URL.Path)
		<-r.Context().Done()
	}
}

// defaultSleepSeconds is used when /sleep is requested with no ?seconds=
// parameter. maxSleepSeconds caps whatever value is requested, so a typo'd
// query string (or an accidental extra zero) can't hang a demo terminal
// indefinitely -- this is a manual-testing fixture, not something that
// needs to support arbitrary durations.
const (
	defaultSleepSeconds = 2
	maxSleepSeconds     = 30
)

// parseSleepSeconds parses the ?seconds= query value into a duration,
// applying the default (raw == "") and the maxSleepSeconds cap. Split out
// from newSleepHandler as a pure function -- no request, no I/O -- so tests
// can check the parsing and capping logic directly instead of having to
// exercise it by actually waiting out a real sleep.
func parseSleepSeconds(raw string) (int, error) {
	if raw == "" {
		return defaultSleepSeconds, nil
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed < 0 {
		return 0, fmt.Errorf("invalid seconds=%q: must be a non-negative integer", raw)
	}
	if parsed > maxSleepSeconds {
		parsed = maxSleepSeconds
	}
	return parsed, nil
}

// newSleepHandler blocks for the requested number of seconds (default
// defaultSleepSeconds, capped at maxSleepSeconds) before responding 200,
// unless the request's context is cancelled first. The select below races
// two channels: time.After's, which fires once the sleep duration elapses,
// against r.Context().Done(), which fires the moment something upstream
// gives up on this request -- either the client itself disconnecting, or
// (once phase 4a's backend timeout exists) the load balancer's own
// context.WithTimeout on its outbound request to this backend expiring.
// Whichever fires first wins the select; the other branch is simply never
// taken. This is the same shape as newHangHandler's <-r.Context().Done(),
// except here there's also a real "finished normally" outcome to race
// against, not just the cancellation.
func newSleepHandler(logger *log.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		seconds, err := parseSleepSeconds(r.URL.Query().Get("seconds"))
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintln(w, err)
			return
		}

		start := time.Now()
		logger.Printf("%s %s: sleeping %ds", r.Method, r.URL.Path, seconds)
		select {
		case <-time.After(time.Duration(seconds) * time.Second):
			logger.Printf("%s %s: woke up after %ds, responding 200", r.Method, r.URL.Path, seconds)
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "slept %ds\n", seconds)
		case <-r.Context().Done():
			logger.Printf("%s %s: cancelled after %v (was sleeping for %ds)", r.Method, r.URL.Path, time.Since(start), seconds)
		}
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
	mux.Handle("/hang", newHangHandler(logger))
	mux.Handle("/sleep", newSleepHandler(logger))
	mux.Handle("/", newHandler(logger, *listenAddr))

	if err := http.ListenAndServe(*listenAddr, mux); err != nil {
		logger.Fatalf("server error: %v", err)
	}
}
