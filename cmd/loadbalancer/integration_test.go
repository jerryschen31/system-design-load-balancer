package main

// These tests wire together the same three pieces main() wires together --
// balancer.RoundRobin, healthcheck.Checker, and proxy.New -- rather than
// exercising any one package in isolation. That's deliberate: the
// interesting failure modes of active health checking only show up once a
// real checker is actually flipping a real balancer's health state while
// real HTTP traffic is flowing through a real proxy, which no single
// package's own tests can reproduce on their own.

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jerryschen31/system-design-load-balancer/internal/balancer"
	"github.com/jerryschen31/system-design-load-balancer/internal/healthcheck"
	"github.com/jerryschen31/system-design-load-balancer/internal/proxy"
)

func testLogger() *log.Logger {
	return log.New(io.Discard, "", 0)
}

// toggleBackend is a test backend whose /health and / responses both
// follow one shared atomic flag, so a test can flip its real-world health
// with a single Store call and have both the health checker and normal
// traffic see the change.
type toggleBackend struct {
	server  *httptest.Server
	healthy atomic.Bool
	hits    atomic.Int64 // count of non-health requests served, i.e. real traffic
}

func newToggleBackend() *toggleBackend {
	b := &toggleBackend{}
	b.healthy.Store(true)
	b.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			if b.healthy.Load() {
				w.WriteHeader(http.StatusOK)
			} else {
				w.WriteHeader(http.StatusServiceUnavailable)
			}
			return
		}
		b.hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	return b
}

func (b *toggleBackend) url(t *testing.T) *url.URL {
	t.Helper()
	u, err := url.Parse(b.server.URL)
	if err != nil {
		t.Fatalf("parse backend URL: %v", err)
	}
	return u
}

// fireBurst sends n concurrent GET requests to lbURL and returns once all
// have completed. It doesn't assert anything itself -- callers inspect
// each backend's hit counter afterward.
func fireBurst(t *testing.T, lbURL string, n int) {
	t.Helper()
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := http.Get(lbURL + "/")
			if err != nil {
				return
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}()
	}
	wg.Wait()
}

// TestIntegration_ChecksRouteAroundUnhealthyBackend is the core end-to-end
// proof for this phase: a backend that fails its health check stops
// receiving real traffic, without the load balancer needing to actually
// fail a request against it first (that's what phase 2's plain round-robin
// could already do reactively via 502s -- this is the proactive version).
func TestIntegration_ChecksRouteAroundUnhealthyBackend(t *testing.T) {
	good := newToggleBackend()
	defer good.server.Close()
	bad := newToggleBackend()
	defer bad.server.Close()
	bad.healthy.Store(false)
	t.Logf("input: 2 backends, one (%s) already unhealthy before the checker ever runs", bad.server.URL)

	backends := []*url.URL{good.url(t), bad.url(t)}
	rr := balancer.NewRoundRobin(backends)

	const interval = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	checker := healthcheck.NewChecker(backends, interval, time.Second, "/health", func(b *url.URL, h bool) { rr.SetHealthy(b, h) }, testLogger())
	checker.Start(ctx)

	lb := httptest.NewServer(proxy.New(rr, testLogger()))
	defer lb.Close()

	// Give the checker a few intervals to run its first probe and mark
	// "bad" down before sending any real traffic.
	time.Sleep(5 * interval)

	fireBurst(t, lb.URL, 30)
	t.Logf("output: good backend got %d requests, unhealthy backend got %d", good.hits.Load(), bad.hits.Load())
	if bad.hits.Load() != 0 {
		t.Fatalf("unhealthy backend received %d requests, want 0 -- active health checking should have excluded it before traffic arrived", bad.hits.Load())
	}
	if good.hits.Load() != 30 {
		t.Fatalf("healthy backend received %d requests, want all 30", good.hits.Load())
	}
}

// TestStress_RecoveredBackendImmediatelyGetsFullShare demonstrates a real
// weakness in this phase's design: there is no gradual ramp-up ("slow
// start") after a backend is marked healthy again. The instant the checker
// detects recovery, round-robin treats that backend exactly like any other
// healthy backend and hands it its full 1/N share of concurrent traffic --
// which is the classic thundering-herd-on-recovery problem: a backend that
// just came back (e.g. from restarting with a cold cache) can be
// immediately slammed at full production concurrency and fail again
// before it's actually ready. Production balancers address this with a
// slow-start window that ramps a recovered backend's share up gradually;
// this phase deliberately doesn't build that yet.
func TestStress_RecoveredBackendImmediatelyGetsFullShare(t *testing.T) {
	backends := make([]*toggleBackend, 3)
	urls := make([]*url.URL, 3)
	for i := range backends {
		backends[i] = newToggleBackend()
		defer backends[i].server.Close()
		urls[i] = backends[i].url(t)
	}
	recovering := backends[0]
	recovering.healthy.Store(false)
	t.Logf("input: 3 backends; backend 0 (%s) starts unhealthy, 1 and 2 start healthy", recovering.server.URL)

	rr := balancer.NewRoundRobin(urls)

	// detected fires exactly once, the moment the checker's own callback
	// (not a fixed sleep) confirms backend 0 has been marked healthy
	// again -- so the test fires its post-recovery burst at the earliest
	// real moment it could, with no slack that would let a slow-start
	// mechanism (if one existed) hide behind extra elapsed time.
	detected := make(chan struct{})
	var once sync.Once
	onResult := func(b *url.URL, healthy bool) {
		rr.SetHealthy(b, healthy)
		if healthy && b.String() == urls[0].String() {
			once.Do(func() { close(detected) })
		}
	}

	const interval = 15 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	checker := healthcheck.NewChecker(urls, interval, time.Second, "/health", onResult, testLogger())
	checker.Start(ctx)

	lb := httptest.NewServer(proxy.New(rr, testLogger()))
	defer lb.Close()

	// Confirm backend 0 is actually excluded first, same as the previous
	// test, so the "immediately gets full share" result below is a
	// change from a real zero, not a fluke of timing.
	time.Sleep(5 * interval)
	fireBurst(t, lb.URL, 30)
	t.Logf("step: before recovery, backend 0 got %d of 30 requests", recovering.hits.Load())
	if recovering.hits.Load() != 0 {
		t.Fatalf("backend 0 got %d requests before recovery, want 0", recovering.hits.Load())
	}

	recovering.healthy.Store(true)
	t.Log("step: backend 0 flipped to healthy")
	select {
	case <-detected:
		t.Log("step: checker confirmed backend 0 healthy again")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the checker to detect backend 0's recovery")
	}

	const burstSize = 90 // multiple of 3, large enough for an even split to be meaningful
	fireBurst(t, lb.URL, burstSize)
	got := recovering.hits.Load()
	want := int64(burstSize / len(backends))
	t.Logf("output: on the very first burst after recovery was detected, backend 0 received %d of %d requests (an even share is %d)", got, burstSize, want)
	if got != want {
		t.Fatalf("backend 0 received %d requests immediately after recovery, want exactly %d -- if this ever fails because the count is *lower*, a slow-start ramp has been added and this test (and its doc comment) should be updated to reflect that improvement", got, want)
	}
	t.Log("KNOWN WEAKNESS: a recovered backend receives its full concurrent traffic share the instant it's marked healthy, with no gradual ramp-up. A backend still warming up after recovery (cold cache, JIT warmup, reconnecting to a database) can be knocked back down immediately.")
}

// TestStress_ConcurrentTrafficSurvivesHealthFlapping fires continuous
// concurrent load at the load balancer while one backend's health flips
// repeatedly in the background, and checks the load balancer itself never
// produces anything other than a clean 200 or 502 -- no hangs, no panics,
// no connection resets -- while a real health checker is concurrently
// mutating the balancer's routing state.
func TestStress_ConcurrentTrafficSurvivesHealthFlapping(t *testing.T) {
	stable := newToggleBackend()
	defer stable.server.Close()
	flapping := newToggleBackend()
	defer flapping.server.Close()
	backends := []*url.URL{stable.url(t), flapping.url(t)}
	t.Logf("input: 2 backends, one flips healthy/unhealthy every 10ms while traffic runs")

	rr := balancer.NewRoundRobin(backends)
	const interval = 15 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	checker := healthcheck.NewChecker(backends, interval, time.Second, "/health", func(b *url.URL, h bool) { rr.SetHealthy(b, h) }, testLogger())
	checker.Start(ctx)

	lb := httptest.NewServer(proxy.New(rr, testLogger()))
	defer lb.Close()

	stopFlapping := make(chan struct{})
	var flapWg sync.WaitGroup
	flapWg.Add(1)
	go func() {
		defer flapWg.Done()
		healthy := true
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopFlapping:
				return
			case <-ticker.C:
				healthy = !healthy
				flapping.healthy.Store(healthy)
			}
		}
	}()

	const numRequests = 200
	var wg sync.WaitGroup
	statusCh := make(chan int, numRequests)
	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := http.Get(lb.URL + "/")
			if err != nil {
				statusCh <- -1
				return
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			statusCh <- resp.StatusCode
		}()
	}
	wg.Wait()
	close(stopFlapping)
	flapWg.Wait()
	close(statusCh)

	counts := make(map[int]int)
	for status := range statusCh {
		counts[status]++
	}
	t.Logf("output: status distribution across %d requests during flapping: %v", numRequests, counts)
	for status := range counts {
		if status != http.StatusOK && status != http.StatusBadGateway {
			t.Errorf("saw unexpected status %d -- every response should be a clean 200 (routed to the currently-healthy backend) or 502 (both backends unhealthy at that instant), never a hang, reset, or other error", status)
		}
	}
	if counts[http.StatusOK] == 0 {
		t.Fatalf("got %d 2xx responses, want > 0 -- the stable backend should have absorbed traffic throughout", counts[http.StatusOK])
	}
}
