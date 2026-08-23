package healthcheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return u
}

// result carries one onResult callback invocation onto a channel so tests
// can wait for and inspect it without a data race against the callback's
// own goroutine.
type result struct {
	backend *url.URL
	healthy bool
}

// waitForResult blocks until either a result arrives on ch or timeout
// elapses, failing the test in the latter case.
func waitForResult(t *testing.T, ch <-chan result, timeout time.Duration) result {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(timeout):
		t.Fatal("timed out waiting for a health check result")
		return result{}
	}
}

func TestCheckerDetectsHealthyBackend(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()
	t.Logf("input: backend at %s answers /health with 200", backend.URL)

	results := make(chan result, 8)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	backendURL := mustURL(t, backend.URL)
	c := NewChecker([]*url.URL{backendURL}, 50*time.Millisecond, time.Second, "/health",
		func(b *url.URL, healthy bool) { results <- result{b, healthy} }, nil)
	c.Start(ctx)

	got := waitForResult(t, results, 2*time.Second)
	t.Logf("output: first check reported healthy=%v", got.healthy)
	if !got.healthy {
		t.Fatalf("got healthy=%v, want true", got.healthy)
	}
}

func TestCheckerDetectsUnhealthyStatus(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer backend.Close()
	t.Logf("input: backend at %s answers /health with 500", backend.URL)

	results := make(chan result, 8)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	backendURL := mustURL(t, backend.URL)
	c := NewChecker([]*url.URL{backendURL}, 50*time.Millisecond, time.Second, "/health",
		func(b *url.URL, healthy bool) { results <- result{b, healthy} }, nil)
	c.Start(ctx)

	got := waitForResult(t, results, 2*time.Second)
	t.Logf("output: first check reported healthy=%v", got.healthy)
	if got.healthy {
		t.Fatalf("got healthy=%v, want false (500 is not a healthy status)", got.healthy)
	}
}

func TestCheckerDetectsTimeout(t *testing.T) {
	const checkTimeout = 50 * time.Millisecond
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Sleep well past the checker's timeout so the probe's own
		// context deadline fires before this handler ever responds.
		time.Sleep(10 * checkTimeout)
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()
	t.Logf("input: backend at %s sleeps %s before answering; checker timeout is %s", backend.URL, 10*checkTimeout, checkTimeout)

	results := make(chan result, 8)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	backendURL := mustURL(t, backend.URL)
	c := NewChecker([]*url.URL{backendURL}, time.Second, checkTimeout, "/health",
		func(b *url.URL, healthy bool) { results <- result{b, healthy} }, nil)
	c.Start(ctx)

	got := waitForResult(t, results, 2*time.Second)
	t.Logf("output: first check reported healthy=%v", got.healthy)
	if got.healthy {
		t.Fatalf("got healthy=%v, want false -- the probe should have been cut off by its own timeout", got.healthy)
	}
}

// TestCheckerDetectsRecovery proves the checker keeps reporting on the same
// backend over time, not just once at startup: it starts unhealthy, then
// the backend is flipped to answer 200, and a later check must report the
// change.
func TestCheckerDetectsRecovery(t *testing.T) {
	var healthy atomic.Bool // starts false: the backend begins unhealthy
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if healthy.Load() {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer backend.Close()
	t.Log("input: backend starts unhealthy (503), flips to healthy (200) partway through")

	results := make(chan result, 8)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	backendURL := mustURL(t, backend.URL)
	c := NewChecker([]*url.URL{backendURL}, 30*time.Millisecond, time.Second, "/health",
		func(b *url.URL, h bool) { results <- result{b, h} }, nil)
	c.Start(ctx)

	first := waitForResult(t, results, 2*time.Second)
	t.Logf("step: first check reported healthy=%v", first.healthy)
	if first.healthy {
		t.Fatalf("got healthy=%v on first check, want false", first.healthy)
	}

	healthy.Store(true)
	t.Log("step: backend flipped to answer 200")

	deadline := time.After(2 * time.Second)
	for {
		select {
		case r := <-results:
			if r.healthy {
				t.Log("output: a later check reported healthy=true, confirming recovery was detected")
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for the checker to detect recovery")
		}
	}
}

func TestCheckerStopsAfterContextCancel(t *testing.T) {
	var checks atomic.Int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		checks.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	const interval = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	backendURL := mustURL(t, backend.URL)
	c := NewChecker([]*url.URL{backendURL}, interval, time.Second, "/health",
		func(*url.URL, bool) {}, nil)
	c.Start(ctx)

	time.Sleep(10 * interval)
	cancel()
	countAtCancel := checks.Load()
	t.Logf("step: cancelled context after %d checks", countAtCancel)

	time.Sleep(10 * interval)
	countAfterWait := checks.Load()
	t.Logf("output: %d checks happened in the %s after cancellation", countAfterWait-countAtCancel, 10*interval)
	if countAfterWait > countAtCancel+1 {
		// Allow at most one in-flight check to complete after cancel
		// fires -- the ticker loop only checks ctx.Done() between
		// probes, so a probe already in progress when cancel() is
		// called is expected to finish.
		t.Fatalf("got %d checks after cancel, want at most 1 (checker should stop polling once its context is cancelled)", countAfterWait-countAtCancel)
	}
}
