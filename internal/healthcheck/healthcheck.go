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

// HealthReporter receives the outcome of each probe and decides what it
// means for routing -- for example, a load-balancing algorithm that stops
// sending traffic to a backend once it's reported unhealthy. healthcheck
// depends on this interface rather than on any specific balancer
// implementation, so swapping load-balancing algorithms never requires
// changing this package: the new algorithm just needs a SetHealthy method
// with this signature. This is deliberately narrower than balancer.Balancer
// (which also has Next()) -- healthcheck never calls Next(), so it doesn't
// declare a dependency on it.
type HealthReporter interface {
	// SetHealthy records backend's current health. changed reports
	// whether this call actually altered the reporter's routing state (as
	// opposed to reporting the same health value it already had), so the
	// caller can log real transitions without re-deriving that fact
	// itself.
	SetHealthy(backend *url.URL, healthy bool) (changed bool)
}

// HealthReporterFunc adapts a plain function to HealthReporter, the same
// pattern the standard library uses for http.HandlerFunc: it lets a caller
// (typically a test, or a composition root that needs to do a little extra
// work alongside reporting) pass a closure directly wherever a
// HealthReporter is expected, without declaring a named type for it.
type HealthReporterFunc func(backend *url.URL, healthy bool) (changed bool)

// SetHealthy calls f, satisfying HealthReporter.
func (f HealthReporterFunc) SetHealthy(backend *url.URL, healthy bool) (changed bool) {
	return f(backend, healthy)
}

// Checker periodically probes a fixed set of backends and reports each one's health to a HealthReporter.
// It runs one independent polling loop per backend (separate go routines) rather than checking them one at a time in sequence, so a slow or hanging backend can't delay how quickly the others get checked.
type Checker struct {
	backends []*url.URL
	interval time.Duration
	timeout  time.Duration
	path     string
	reporter HealthReporter
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
//   - reporter: receives each probe's health result (concurrency considerations must be taken into account, since one goroutine per backend calls into it)
//   - logger: logger for recording health check events. If nil, logging is discarded.
func NewChecker(backends []*url.URL, interval, timeout time.Duration, path string, reporter HealthReporter, logger *log.Logger) *Checker {
	if len(backends) == 0 {
		panic("healthcheck: NewChecker requires at least one backend")
	}
	if reporter == nil {
		panic("healthcheck: NewChecker requires a non-nil reporter")
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
		reporter: reporter,
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

// run: runs a continuous health check loop for the given backend URL, performing an initial probe immediately and then repeating at each interval until the context is cancelled.
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

// probe: runs a single health check against backend URL and reports the result.
func (c *Checker) probe(ctx context.Context, backend *url.URL) {
	checkCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	target := *backend
	target.Path = c.path
	target.RawQuery = ""

	// Builds the HTTP GET request for the health check using the target URL. This only errors on malformed target URLs or context issues, still worth catching.
	req, err := http.NewRequestWithContext(checkCtx, http.MethodGet, target.String(), nil)
	if err != nil {
		c.logger.Printf("healthcheck: building request for %s: %v", backend, err)
		c.report(backend, false)
		return
	}

	// this performs the actual HTTP health-check request to the backend using the dedicated client for health checks
	resp, err := c.client.Do(req)
	if err != nil {
		// Covers connection refused, DNS failure, and checkCtx's timeout firing mid-request -- all of these mean the backend isn't answering, so they're all labeled as just "unhealthy" here.
		c.logger.Printf("healthcheck: %s: %v", backend, err)
		c.report(backend, false)
		return
	}

	// Drain and close the response body to allow this probe connection to be recycled to the client's idle pool and reused by the next probe.
	// Copy commands simply copies the entire contents of the response body to the specified destination (in this case, io.Discard).
	io.Copy(io.Discard, resp.Body)
	defer resp.Body.Close()

	healthy := resp.StatusCode >= 200 && resp.StatusCode < 300
	if !healthy {
		c.logger.Printf("healthcheck: %s returned status %d", backend, resp.StatusCode)
	}
	c.report(backend, healthy)
}

// report forwards a probe's outcome to the reporter and logs an actual
// health transition (as opposed to every individual probe result, which
// would mostly just repeat "still healthy" every interval).
func (c *Checker) report(backend *url.URL, healthy bool) {
	// The reporter's return value is the whole reason this method exists.
	// A probe fires every interval and almost always finds the same
	// answer as last time, so logging every result would bury real events
	// under a steady stream of "still healthy". The reporter already has
	// to compare the new value against the recorded one to decide whether
	// to republish its state, so it hands that comparison back as
	// changed, and only a genuine transition gets logged here.
	if c.reporter.SetHealthy(backend, healthy) {
		c.logger.Printf("healthcheck: backend %s health changed: healthy=%v", backend, healthy)
	}
}
