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
	"github.com/jerryschen31/system-design-load-balancer/internal/middleware"
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
	checker := healthcheck.NewChecker(backends, interval, time.Second, "/health", rr, testLogger())
	checker.Start(ctx)

	lb := httptest.NewServer(proxy.New(rr, 0, testLogger()))
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
	onResult := healthcheck.HealthReporterFunc(func(b *url.URL, healthy bool) bool {
		changed := rr.SetHealthy(b, healthy)
		if healthy && b.String() == urls[0].String() {
			once.Do(func() { close(detected) })
		}
		return changed
	})

	const interval = 15 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	checker := healthcheck.NewChecker(urls, interval, time.Second, "/health", onResult, testLogger())
	checker.Start(ctx)

	lb := httptest.NewServer(proxy.New(rr, 0, testLogger()))
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
	checker := healthcheck.NewChecker(backends, interval, time.Second, "/health", rr, testLogger())
	checker.Start(ctx)

	lb := httptest.NewServer(proxy.New(rr, 0, testLogger()))
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

// TestIntegration_MaxConcurrentProtectsFullStack proves the concurrency
// limiter works correctly composed with the rest of main()'s real handler
// chain (Logging -> MaxConcurrent -> proxy, the same order main() builds),
// not just in isolation against a bare handler the way
// TestMaxConcurrentAllowsExactlyNInFlight (internal/middleware) does. A
// single slow backend holds exactly maxConcurrent requests open; a further
// burst fired while those are in flight must all come back 503 immediately
// -- proving MaxConcurrent's admission check runs before proxy.New ever
// picks a backend or dials it, not after.
func TestIntegration_MaxConcurrentProtectsFullStack(t *testing.T) {
	const maxConcurrent = 4
	arrived := make(chan struct{}, maxConcurrent)
	release := make(chan struct{})
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		arrived <- struct{}{}
		<-release
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()
	t.Logf("input: maxConcurrent=%d, one backend whose handler blocks until released", maxConcurrent)

	target, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatalf("parse backend URL: %v", err)
	}
	rr := balancer.NewRoundRobin([]*url.URL{target})

	// Same composition order as main(): Logging outermost, MaxConcurrent
	// next, proxy innermost.
	handler := middleware.Logging(testLogger())(middleware.MaxConcurrent(maxConcurrent, 0, 0)(proxy.New(rr, 0, testLogger())))
	lb := httptest.NewServer(handler)
	defer lb.Close()

	var admittedWg sync.WaitGroup
	admittedStatuses := make([]int, maxConcurrent)
	for i := 0; i < maxConcurrent; i++ {
		admittedWg.Add(1)
		go func(i int) {
			defer admittedWg.Done()
			resp, err := http.Get(lb.URL + "/")
			if err != nil {
				t.Errorf("admitted request %d failed: %v", i, err)
				return
			}
			defer resp.Body.Close()
			admittedStatuses[i] = resp.StatusCode
		}(i)
	}
	for i := 0; i < maxConcurrent; i++ {
		<-arrived
	}
	t.Logf("step: %d requests confirmed in flight at the real backend, through the full stack", maxConcurrent)

	const burstSize = 10
	var burstWg sync.WaitGroup
	burstStatuses := make([]int, burstSize)
	burstWg.Add(burstSize)
	for i := 0; i < burstSize; i++ {
		go func(i int) {
			defer burstWg.Done()
			resp, err := http.Get(lb.URL + "/")
			if err != nil {
				t.Errorf("burst request %d failed: %v", i, err)
				return
			}
			defer resp.Body.Close()
			burstStatuses[i] = resp.StatusCode
		}(i)
	}
	// release is still open; if any of these were queued instead of
	// rejected immediately, this Wait would hang until the test's own
	// timeout, which is the point -- a hang here is a real failure, not
	// something we need to separately assert on.
	burstWg.Wait()

	counts := make(map[int]int)
	for _, status := range burstStatuses {
		counts[status]++
	}
	t.Logf("output: %d-request burst while at capacity -> status counts %v", burstSize, counts)
	if counts[http.StatusServiceUnavailable] != burstSize {
		t.Errorf("got status counts %v, want all %d requests to be %d", counts, burstSize, http.StatusServiceUnavailable)
	}

	close(release)
	admittedWg.Wait()
	t.Logf("output: the %d originally-admitted requests all completed with statuses %v", maxConcurrent, admittedStatuses)
	for i, status := range admittedStatuses {
		if status != http.StatusOK {
			t.Errorf("admitted request %d got status %d, want %d", i, status, http.StatusOK)
		}
	}
}

// TestIntegration_QueueAdmitsNearMissRequest is the resolution of the
// exact scenario discussed while designing phase 4b: with phase 4a's
// plain MaxConcurrent, a client arriving 1 second before a slot would free
// up and a client arriving 9 seconds before were treated identically --
// both rejected outright, because MaxConcurrent had no notion of "how
// close". With a queue layered on top, a near-miss client (queueing for
// well under queueWaitTimeout) succeeds instead, through the real full
// stack: Logging -> MaxConcurrent(with queue) -> proxy -> a real backend.
func TestIntegration_QueueAdmitsNearMissRequest(t *testing.T) {
	const maxConcurrent = 2
	const holderDelay = 150 * time.Millisecond
	const queueCapacity = 1
	const queueWaitTimeout = 5 * time.Second // deliberately >> holderDelay
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(holderDelay)
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()
	t.Logf("input: maxConcurrent=%d, each holder finishes in ~%s; queueCapacity=%d, queueWaitTimeout=%s (well above holderDelay)", maxConcurrent, holderDelay, queueCapacity, queueWaitTimeout)

	target, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatalf("parse backend URL: %v", err)
	}
	rr := balancer.NewRoundRobin([]*url.URL{target})
	handler := middleware.Logging(testLogger())(middleware.MaxConcurrent(maxConcurrent, queueCapacity, queueWaitTimeout)(proxy.New(rr, 0, testLogger())))
	lb := httptest.NewServer(handler)
	defer lb.Close()

	var holderWg sync.WaitGroup
	holderWg.Add(maxConcurrent)
	holderStart := time.Now()
	for i := 0; i < maxConcurrent; i++ {
		go func() {
			defer holderWg.Done()
			resp, err := http.Get(lb.URL + "/")
			if err != nil {
				t.Errorf("holder request failed: %v", err)
				return
			}
			resp.Body.Close()
		}()
	}

	// Give the holders a moment to actually be in flight before the
	// near-miss request arrives -- this only needs to land sometime
	// before holderDelay elapses, not at a precise instant, so a short
	// fixed sleep is fine here (unlike detecting an exact event).
	time.Sleep(20 * time.Millisecond)

	nearMissStart := time.Now()
	resp, err := http.Get(lb.URL + "/")
	nearMissElapsed := time.Since(nearMissStart)
	if err != nil {
		t.Fatalf("near-miss request failed: %v", err)
	}
	defer resp.Body.Close()

	holderWg.Wait()
	totalHolderElapsed := time.Since(holderStart)
	t.Logf("output: near-miss request -> status=%d, waited %s (holders took %s total)", resp.StatusCode, nearMissElapsed, totalHolderElapsed)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("near-miss request got status %d, want %d -- it should have queued and been admitted once a slot freed up, not rejected", resp.StatusCode, http.StatusOK)
	}
	if nearMissElapsed >= queueWaitTimeout {
		t.Errorf("near-miss request took %s (>= queueWaitTimeout %s); it should have been admitted well before the timeout, as soon as a holder finished", nearMissElapsed, queueWaitTimeout)
	}
}
