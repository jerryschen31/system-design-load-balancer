package balancer

import (
	"net/url"
	"sync"
	"testing"
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
