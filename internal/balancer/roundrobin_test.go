package balancer

import (
	"net/url"
	"sync"
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

func TestRoundRobinCyclesInOrder(t *testing.T) {
	backends := []*url.URL{
		mustURL(t, "http://backend-a"),
		mustURL(t, "http://backend-b"),
		mustURL(t, "http://backend-c"),
	}
	t.Logf("input: %d backends, calling Next() 7 times in a row", len(backends))

	rr := NewRoundRobin(backends)
	for i := 0; i < 7; i++ {
		got := rr.Next()
		want := backends[i%len(backends)]
		t.Logf("call %d: got %s (want %s)", i, got, want)
		if got.String() != want.String() {
			t.Fatalf("call %d: got %s, want %s", i, got, want)
		}
	}
	t.Log("output: cycle wrapped correctly past the end of the backend list twice")
}

// TestNewRoundRobinCopiesBackends proves NewRoundRobin isn't aliasing the
// caller's slice: mutating the original slice after construction must not
// change what the balancer hands out.
func TestNewRoundRobinCopiesBackends(t *testing.T) {
	original := []*url.URL{
		mustURL(t, "http://backend-a"),
		mustURL(t, "http://backend-b"),
	}
	t.Logf("input: construct RoundRobin over %v, then mutate the original slice", original)

	rr := NewRoundRobin(original)

	// Mutate the caller's slice after handing it to NewRoundRobin -- this
	// simulates a caller reusing or reordering its own backend list later.
	original[0] = mustURL(t, "http://attacker-controlled")
	t.Logf("step: caller's original slice[0] is now %s", original[0])

	got := rr.Next()
	t.Logf("output: rr.Next() returned %s", got)
	if got.String() != "http://backend-a" {
		t.Fatalf("got %s, want http://backend-a -- NewRoundRobin must not alias the caller's slice", got)
	}
}

// TestRoundRobinConcurrentCallsStayBalanced fires many concurrent calls to
// Next() from many goroutines and checks that the calls split *exactly*
// evenly across backends. That exactness is the point: round-robin's
// modulo arithmetic guarantees an even split regardless of which goroutine
// ran when -- but only if every call actually gets a distinct counter
// value. If the atomic counter were replaced with a plain, unsynchronized
// int++, two goroutines could read the same value before either writes
// back, an increment gets lost, and this exact-count assertion would start
// failing -- that's the observable symptom of a lost update under
// concurrency. Run with `go test -race ./internal/balancer/...` to also
// have Go's race detector flag the underlying memory race directly.
func TestRoundRobinConcurrentCallsStayBalanced(t *testing.T) {
	backends := []*url.URL{
		mustURL(t, "http://backend-a"),
		mustURL(t, "http://backend-b"),
		mustURL(t, "http://backend-c"),
	}
	const goroutines = 50
	const callsPerGoroutine = 60 // 50*60 = 3000, divisible by len(backends)
	total := goroutines * callsPerGoroutine
	t.Logf("input: %d goroutines x %d calls each = %d total calls across %d backends", goroutines, callsPerGoroutine, total, len(backends))

	rr := NewRoundRobin(backends)
	results := make(chan string, total)

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < callsPerGoroutine; i++ {
				results <- rr.Next().String()
			}
		}()
	}
	wg.Wait()
	close(results)

	counts := make(map[string]int)
	for r := range results {
		counts[r]++
	}
	t.Logf("distribution: %v", counts)

	want := total / len(backends)
	for _, b := range backends {
		got := counts[b.String()]
		if got != want {
			t.Errorf("backend %s: got %d calls, want exactly %d (a mismatch means the counter lost an update under concurrency)", b, got, want)
		}
	}
	t.Logf("output: every backend received exactly %d calls, confirming no update was lost across %d concurrent goroutines", want, goroutines)
}

// TestNewRoundRobinStartsAllBackendsHealthy confirms the optimistic-start
// default: every backend is eligible to receive traffic immediately, before
// any health check has run against it.
func TestNewRoundRobinStartsAllBackendsHealthy(t *testing.T) {
	backends := []*url.URL{
		mustURL(t, "http://backend-a"),
		mustURL(t, "http://backend-b"),
	}
	t.Logf("input: %d fresh backends, no SetHealthy calls yet", len(backends))

	rr := NewRoundRobin(backends)
	seen := make(map[string]bool)
	for i := 0; i < 4; i++ {
		seen[rr.Next().String()] = true
	}
	t.Logf("output: backends reached by Next(): %v", seen)
	for _, b := range backends {
		if !seen[b.String()] {
			t.Fatalf("backend %s never received a call -- it should have started healthy", b)
		}
	}
}

// TestRoundRobinSkipsUnhealthyBackend proves SetHealthy actually changes
// routing: once a backend is marked unhealthy, Next() must stop returning
// it, and cycle only over the remaining healthy ones.
func TestRoundRobinSkipsUnhealthyBackend(t *testing.T) {
	a := mustURL(t, "http://backend-a")
	b := mustURL(t, "http://backend-b")
	c := mustURL(t, "http://backend-c")
	rr := NewRoundRobin([]*url.URL{a, b, c})
	t.Log("input: 3 healthy backends, then backend-b is marked unhealthy")

	rr.SetHealthy(b, false)

	for i := 0; i < 6; i++ {
		got := rr.Next()
		if got.String() == b.String() {
			t.Fatalf("call %d: got %s, which was marked unhealthy and should have been skipped", i, got)
		}
	}
	t.Log("output: 6 calls to Next() never returned the unhealthy backend")

	rr.SetHealthy(b, true)
	seen := make(map[string]bool)
	for i := 0; i < 6; i++ {
		seen[rr.Next().String()] = true
	}
	t.Logf("step: backend-b marked healthy again; backends reached: %v", seen)
	if !seen[b.String()] {
		t.Fatal("backend-b was marked healthy again but Next() never returned it")
	}
}

// TestRoundRobinAllUnhealthyReturnsNil proves the balancer fails loud
// (nil, for the proxy to turn into a 502) rather than silently routing to
// a backend it knows is down.
func TestRoundRobinAllUnhealthyReturnsNil(t *testing.T) {
	a := mustURL(t, "http://backend-a")
	b := mustURL(t, "http://backend-b")
	rr := NewRoundRobin([]*url.URL{a, b})
	t.Log("input: 2 backends, both marked unhealthy")

	rr.SetHealthy(a, false)
	rr.SetHealthy(b, false)

	got := rr.Next()
	t.Logf("output: Next() returned %v", got)
	if got != nil {
		t.Fatalf("got %s, want nil -- no backend is healthy", got)
	}
}

// TestRoundRobinSetHealthyIgnoresUnknownBackend confirms SetHealthy is a
// no-op for a URL that isn't one of this balancer's own backends, rather
// than silently growing the backend set.
func TestRoundRobinSetHealthyIgnoresUnknownBackend(t *testing.T) {
	a := mustURL(t, "http://backend-a")
	stranger := mustURL(t, "http://not-a-backend")
	rr := NewRoundRobin([]*url.URL{a})
	t.Logf("input: balancer over [%s], SetHealthy called for unrelated %s", a, stranger)

	rr.SetHealthy(stranger, false)

	got := rr.Next()
	t.Logf("output: Next() returned %s", got)
	if got.String() != a.String() {
		t.Fatalf("got %s, want %s -- an unrecognized backend must not affect routing", got, a)
	}
}

// TestRoundRobinConcurrentSetHealthyAndNext runs SetHealthy from many
// goroutines (simulating several backends' health-check loops reporting at
// once) concurrently with many Next() calls (simulating in-flight
// requests). It doesn't assert a specific distribution -- the health state
// is changing throughout -- it exists to be run under `go test -race`,
// which is the actual check: SetHealthy's map + atomic.Pointer swap must
// be race-free under concurrent writers, and Next() must never observe a
// half-updated healthy slice.
func TestRoundRobinConcurrentSetHealthyAndNext(t *testing.T) {
	backends := []*url.URL{
		mustURL(t, "http://backend-a"),
		mustURL(t, "http://backend-b"),
		mustURL(t, "http://backend-c"),
	}
	rr := NewRoundRobin(backends)
	t.Logf("input: %d backends, concurrent SetHealthy flapping and Next() calls for 100ms", len(backends))

	stop := make(chan struct{})
	var wg sync.WaitGroup

	// One flapping goroutine per backend, toggling its health repeatedly.
	for _, b := range backends {
		wg.Add(1)
		go func(b *url.URL) {
			defer wg.Done()
			healthy := true
			for {
				select {
				case <-stop:
					return
				default:
					rr.SetHealthy(b, healthy)
					healthy = !healthy
				}
			}
		}(b)
	}

	// Many goroutines hammering Next() concurrently with the flapping above.
	for g := 0; g < 20; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					rr.Next() // return value intentionally unchecked: nil is valid if all 3 happen to be unhealthy at this instant
				}
			}
		}()
	}

	time.Sleep(100 * time.Millisecond)
	close(stop)
	wg.Wait()
	t.Log("output: no data race reported (run with -race to make this test meaningful)")
}
