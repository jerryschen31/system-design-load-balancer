// Package healthcheck actively probes backends to determine whether they
// should keep receiving traffic, independent of any real client request.
package healthcheck

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Checker periodically probes a fixed set of backends and reports each
// one's health via a callback. It runs one independent polling loop per
// backend rather than checking them one at a time in sequence, so a slow
// or hanging backend can't delay how quickly the others get checked.
type Checker struct {
	backends []*url.URL
	interval time.Duration
	timeout  time.Duration
	path     string
	onResult func(backend *url.URL, healthy bool)
	client   *http.Client
	logger   *log.Logger
}

// NewChecker builds a Checker over backends (which is not copied further --
// callers should treat it as immutable after passing it in, same
// expectation as balancer.NewRoundRobin). interval is how often each
// backend is probed; timeout bounds how long a single probe is allowed to
// take before it counts as a failure. path is the request path appended to
// each backend's scheme+host (e.g. "/health"). onResult is called from
// whichever backend's polling goroutine just got a result -- it must be
// safe for concurrent use, since every backend's goroutine can call it at
// once.
func NewChecker(backends []*url.URL, interval, timeout time.Duration, path string, onResult func(backend *url.URL, healthy bool), logger *log.Logger) *Checker {
	if len(backends) == 0 {
		panic("healthcheck: NewChecker requires at least one backend")
	}
	if onResult == nil {
		panic("healthcheck: NewChecker requires a non-nil onResult callback")
	}
	// interval and timeout have no sensible default -- unlike path below,
	// there's no single "obviously right" fallback for "how often" or
	// "how long," so an invalid value here is a caller bug to panic on,
	// not something to silently paper over. A non-positive interval would
	// otherwise panic later anyway, inside time.NewTicker on whichever
	// backend's goroutine happens to start first (Start launches one
	// goroutine per backend), which is a much harder failure to trace
	// back to NewChecker having been called wrong in the first place.
	if interval <= 0 {
		panic("healthcheck: NewChecker requires a positive interval")
	}
	if timeout <= 0 {
		panic("healthcheck: NewChecker requires a positive timeout")
	}
	// path, unlike interval/timeout, does have an obvious correct
	// fallback (the root path), so a missing leading slash is normalized
	// rather than treated as a fatal caller error.
	if path == "" {
		path = "/"
	} else if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	return &Checker{
		backends: backends,
		interval: interval,
		timeout:  timeout,
		path:     path,
		onResult: onResult,
		// A dedicated client (rather than http.DefaultClient) so this
		// package's connection pool is independent of anything else in
		// the process -- health-check traffic and proxied traffic don't
		// share idle connections.
		client: &http.Client{},
		logger: logger,
	}
}

// Start launches one polling goroutine per backend and returns immediately
// -- it does not block. Each goroutine runs an initial check right away
// (so a backend's health is known well before the first interval elapses),
// then continues on a ticker until ctx is cancelled.
func (c *Checker) Start(ctx context.Context) {
	for _, backend := range c.backends {
		go c.run(ctx, backend)
	}
}

func (c *Checker) run(ctx context.Context, backend *url.URL) {
	c.probe(ctx, backend)

	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.probe(ctx, backend)
		}
	}
}

// probe runs a single health check against backend and reports the result.
func (c *Checker) probe(ctx context.Context, backend *url.URL) {
	checkCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	target := *backend
	target.Path = c.path
	target.RawQuery = ""

	req, err := http.NewRequestWithContext(checkCtx, http.MethodGet, target.String(), nil)
	if err != nil {
		c.logger.Printf("healthcheck: building request for %s: %v", backend, err)
		c.onResult(backend, false)
		return
	}

	resp, err := c.client.Do(req)
	if err != nil {
		// Covers connection refused, DNS failure, and checkCtx's
		// timeout firing mid-request -- all of these mean the backend
		// isn't answering, so they're all just "unhealthy" here.
		c.logger.Printf("healthcheck: %s: %v", backend, err)
		c.onResult(backend, false)
		return
	}
	// Drain and close the body so the underlying TCP connection can be
	// returned to the client's idle pool and reused by the next probe
	// of this backend, the same connection-reuse behavior phase 2
	// confirmed for the proxy's own backend requests.
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	healthy := resp.StatusCode >= 200 && resp.StatusCode < 300
	if !healthy {
		c.logger.Printf("healthcheck: %s returned status %d", backend, resp.StatusCode)
	}
	c.onResult(backend, healthy)
}
