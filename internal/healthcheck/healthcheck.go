// Package healthcheck actively probes backends to determine whether they should keep receiving traffic, independent of any real client request.
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

// Checker periodically probes a fixed set of backends and reports each one's health via a callback.
// It runs one independent polling loop per backend (separate go routines) rather than checking them one at a time in sequence, so a slow or hanging backend can't delay how quickly the others get checked.
type Checker struct {
	backends []*url.URL
	interval time.Duration
	timeout  time.Duration
	path     string
	onResult func(backend *url.URL, healthy bool)
	client   *http.Client
	logger   *log.Logger
}

// NewChecker constructs a new health checker for each of the given backends.
//
// Inputs:
//   - backends: the list of backend URLs to be health-checked.
//   - interval: how often each backend should be probed.
//   - timeout: the maximum time duration to wait for a single probe response before it counts as a failure.
//   - path: the relative health check endpoint - i.e., request path appended to each backend's scheme+host (e.g., /health)
//   - onResult: callback function invoked once ANY backend responds with its health status (concurrency considerations must be taken into account)
//   - logger: logger for recording health check events. If nil, logging is discarded.
func NewChecker(backends []*url.URL, interval, timeout time.Duration, path string, onResult func(backend *url.URL, healthy bool), logger *log.Logger) *Checker {
	if len(backends) == 0 {
		panic("healthcheck: NewChecker requires at least one backend")
	}
	if onResult == nil {
		panic("healthcheck: NewChecker requires a non-nil onResult callback")
	}
	if interval <= 0 {
		panic("healthcheck: NewChecker requires a positive interval")
	}
	if timeout <= 0 {
		panic("healthcheck: NewChecker requires a positive timeout")
	}

	// if health endpoint relative path is missing a "/", just add it
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
		// Creates a dedicated client for managing health check connections. This is for isolation, so that health-check traffic and reverse-proxy traffic (from real requests to the actual load balancer) don't share the same pool of connections.
		client: &http.Client{},
		logger: logger,
	}
}

// Start launches one polling goroutine per backend and returns immediately - it does not block.
// Each goroutine runs an initial check right away (so a backend's health is known well before the first interval elapses), then continues on a ticker until context (ctx) is cancelled.
func (c *Checker) Start(ctx context.Context) {
	for _, backend := range c.backends {
		go c.run(ctx, backend)
	}
}

func (c *Checker) run(ctx context.Context, backend *url.URL) {
	c.probe(ctx, backend)

	ticker := time.NewTicker(c.interval)
	// this makes sure the ticker is stopped when the run() function exits, preventing resource leaks.
	defer ticker.Stop()
	// this for loop repeats indefinitely, performing health checks at each tick until the context is cancelled (a Done signal is received)
	for {
		// this select waits for receiving either a cancellation signal on the Done channel of the context, or a tick timestamp from the ticker.C Channel
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
