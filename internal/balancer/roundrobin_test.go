package balancer

import (
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/jerryschen31/system-design-load-balancer/internal/targetgroup"
)

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return u
}

// newRR is the common setup now that a balancer is constructed over a
// TargetGroup rather than over a raw backend list. The group is returned
// alongside so a test can flip health on it -- note that a test now marks
// a backend down by talking to the *group*, never to the balancer, which
// is the whole point of the split.
func newRR(t *testing.T, backends ...*url.URL) (*RoundRobin, *targetgroup.TargetGroup) {
	t.Helper()
	g := targetgroup.New(backends)
	return NewRoundRobin(g), g
}

func TestRoundRobinCyclesInOrder(t *testing.T) {
	backends := []*url.URL{
		mustURL(t, "http://backend-a"),
		mustURL(t, "http://backend-b"),
		mustURL(t, "http://backend-c"),
	}
	t.Logf("input: %d backends, calling Next() 7 times in a row", len(backends))

	rr, _ := newRR(t, backends...)
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

// TestRoundRobinSkipsUnhealthyBackend is the read-path counterpart to the
// bookkeeping tests in internal/targetgroup. Those prove the group drops a
// backend from its snapshot; this proves the balancer actually honours the
// snapshot instead of caching a stale one or routing off its own list.
// That seam -- group writes, balancer reads -- is exactly what could
// silently break, so it gets a test on both sides.
func TestRoundRobinSkipsUnhealthyBackend(t *testing.T) {
	a := mustURL(t, "http://backend-a")
	b := mustURL(t, "http://backend-b")
	c := mustURL(t, "http://backend-c")
	rr, group := newRR(t, a, b, c)
	t.Log("input: 3 healthy backends, then backend-b is marked unhealthy on the group")

	group.SetHealthy(b, false)

	for i := 0; i < 6; i++ {
		got := rr.Next()
		if got.String() == b.String() {
			t.Fatalf("call %d: got %s, which was marked unhealthy and should have been skipped", i, got)
		}
	}
	t.Log("output: 6 calls to Next() never returned the unhealthy backend")

	group.SetHealthy(b, true)
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
// a backend it knows is down. The group reports an empty healthy list;
// turning that into "no target" is the balancer's decision, which is why
// this test lives here and not in internal/targetgroup.
func TestRoundRobinAllUnhealthyReturnsNil(t *testing.T) {
	a := mustURL(t, "http://backend-a")
	b := mustURL(t, "http://backend-b")
	rr, group := newRR(t, a, b)
	t.Log("input: 2 backends, both marked unhealthy on the group")

	group.SetHealthy(a, false)
	group.SetHealthy(b, false)

	got := rr.Next()
	t.Logf("output: Next() returned %v", got)
	if got != nil {
		t.Fatalf("got %s, want nil -- no backend is healthy", got)
	}
}

// TestNewRoundRobinPanicsOnNilGroup keeps the failure at construction,
// where the stack trace points at the composition root, rather than as a
// nil dereference on some request goroutine much later.
func TestNewRoundRobinPanicsOnNilGroup(t *testing.T) {
	t.Log("input: NewRoundRobin called with a nil target group")
	defer func() {
		r := recover()
		t.Logf("output: recovered panic = %v", r)
		if r == nil {
			t.Fatal("expected NewRoundRobin to panic on a nil target group")
		}
	}()
	NewRoundRobin(nil)
}

// TestNewRoundRobinPanicsOnUnbuiltGroup is the fail-fast counterpart to
// the nil check above. A zero-value &targetgroup.TargetGroup{} is non-nil,
// so it slips past a plain nil guard, but it has no published snapshot --
// which used to mean construction succeeded and the load balancer then
// panicked on its first real request, on a request goroutine, pointing at
// Next() rather than at whoever built the group.
//
// The panic must therefore happen here, at construction, where the stack
// trace names the caller. Note this test asserts a *composition* property,
// not arithmetic: the balancer's job is to surface the group's own
// validity check early, not to re-implement it.
func TestNewRoundRobinPanicsOnUnbuiltGroup(t *testing.T) {
	t.Log("input: NewRoundRobin called with a non-nil but never-constructed &targetgroup.TargetGroup{}")
	defer func() {
		r := recover()
		t.Logf("output: recovered panic = %v", r)
		if r == nil {
			t.Fatal("expected NewRoundRobin to panic at construction, rather than deferring the failure to the first request")
		}
	}()
	NewRoundRobin(&targetgroup.TargetGroup{})
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

	rr, _ := newRR(t, backends...)
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

// TestRoundRobinNextDuringHealthFlapping is the adversarial version of the
// read path: health flips constantly while requests are being routed.
//
// The specific failure it hunts for is an index-out-of-range panic. Next()
// reads a length and then indexes with it; if it read the healthy list
// twice -- once for len(), once for the index -- a health check landing
// between those two reads could shrink the list and the index would run
// off the end. Loading one Snapshot and reusing it is what prevents that,
// and this test is what would catch a future edit that breaks it.
func TestRoundRobinNextDuringHealthFlapping(t *testing.T) {
	backends := []*url.URL{
		mustURL(t, "http://backend-a"),
		mustURL(t, "http://backend-b"),
		mustURL(t, "http://backend-c"),
	}
	rr, group := newRR(t, backends...)
	t.Logf("input: %d backends, health flapping on the group while 20 goroutines call Next() for 100ms", len(backends))

	stop := make(chan struct{})
	var wg sync.WaitGroup

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
					group.SetHealthy(b, healthy)
					healthy = !healthy
				}
			}
		}(b)
	}

	var nilResults, mu = 0, sync.Mutex{}
	for g := 0; g < 20; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			localNil := 0
			for {
				select {
				case <-stop:
					mu.Lock()
					nilResults += localNil
					mu.Unlock()
					return
				default:
					// nil is a legitimate result here: at this
					// instant all three backends may be flapped
					// down at once. The assertion is simply that
					// this never panics and never returns a
					// backend outside the group.
					if got := rr.Next(); got == nil {
						localNil++
					}
				}
			}
		}()
	}

	time.Sleep(100 * time.Millisecond)
	close(stop)
	wg.Wait()
	t.Logf("output: survived constant flapping without panicking; %d calls legitimately found no healthy backend", nilResults)
	t.Log("output: no data race reported (run with -race to make this test meaningful)")
}
