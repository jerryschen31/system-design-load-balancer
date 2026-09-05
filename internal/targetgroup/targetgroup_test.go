package targetgroup

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

// names renders a backend list as plain strings so t.Log output reads as a
// list of hostnames rather than a row of pointer addresses.
func names(backends []*url.URL) []string {
	out := make([]string, 0, len(backends))
	for _, b := range backends {
		out = append(out, b.String())
	}
	return out
}

// TestNewStartsAllBackendsHealthy confirms the optimistic-start default:
// every backend is eligible to receive traffic immediately, before any
// health check has run against it.
func TestNewStartsAllBackendsHealthy(t *testing.T) {
	backends := []*url.URL{
		mustURL(t, "http://backend-a"),
		mustURL(t, "http://backend-b"),
	}
	t.Logf("input: %d fresh backends, no SetHealthy calls yet", len(backends))

	g := New(backends)
	snap := g.Snapshot()
	t.Logf("output: snapshot healthy=%v generation=%d", names(snap.Healthy), snap.Generation)

	if len(snap.Healthy) != len(backends) {
		t.Fatalf("got %d healthy backends, want %d -- all backends should start healthy", len(snap.Healthy), len(backends))
	}
	if snap.Generation != 1 {
		t.Fatalf("got generation %d on a fresh group, want 1 -- generation must never start at 0, so a consumer's zero-valued cache field can't accidentally match it", snap.Generation)
	}
}

// TestNewCopiesBackends proves New isn't aliasing the caller's slice:
// mutating the original slice after construction must not change what the
// group reports.
func TestNewCopiesBackends(t *testing.T) {
	original := []*url.URL{
		mustURL(t, "http://backend-a"),
		mustURL(t, "http://backend-b"),
	}
	t.Logf("input: construct a group over %v, then mutate the caller's slice", names(original))

	g := New(original)

	// Simulates a caller reusing or reordering its own backend list after
	// handing it over.
	original[0] = mustURL(t, "http://attacker-controlled")
	t.Logf("step: caller's original slice[0] is now %s", original[0])

	got := names(g.Snapshot().Healthy)
	t.Logf("output: snapshot healthy=%v", got)
	if got[0] != "http://backend-a" {
		t.Fatalf("got %s, want http://backend-a -- New must copy the caller's slice", got[0])
	}
}

// TestNewPanicsOnEmptyBackends confirms the failure is immediate and loud
// at construction rather than surfacing much later as a load balancer that
// mysteriously 502s every request.
func TestNewPanicsOnEmptyBackends(t *testing.T) {
	t.Log("input: New called with an empty backend list")
	defer func() {
		r := recover()
		t.Logf("output: recovered panic = %v", r)
		if r == nil {
			t.Fatal("expected New to panic on an empty backend list")
		}
	}()
	New(nil)
}

// TestZeroValueTargetGroupPanicsOnUse covers the one construction path the
// package cannot forbid: &TargetGroup{} is legal in any package, because
// an empty composite literal is allowed even when every field is
// unexported. Since the type can't stop such a group being built, it has
// to make using one fail immediately and say why.
//
// Both entry points are covered, and the SetHealthy case is the important
// one. Snapshot would blow up on its own regardless -- returning nil for a
// caller to dereference -- so the guard there only improves the message.
// SetHealthy would NOT: reading from a nil map is legal Go and yields the
// zero value, so an unguarded unbuilt group accepts every health report,
// discards it, and returns changed=false forever. Backends would never be
// marked down, and nothing would be logged to say so. Converting that
// silent failure into a panic is the actual fix.
func TestZeroValueTargetGroupPanicsOnUse(t *testing.T) {
	t.Run("Snapshot", func(t *testing.T) {
		t.Log("input: Snapshot() called on a zero-value &TargetGroup{}")
		defer func() {
			r := recover()
			t.Logf("output: recovered panic = %v", r)
			if r == nil {
				t.Fatal("expected Snapshot to panic on a group that was never built by New")
			}
		}()
		(&TargetGroup{}).Snapshot()
	})

	t.Run("SetHealthy", func(t *testing.T) {
		t.Log("input: SetHealthy() called on a zero-value &TargetGroup{}")
		defer func() {
			r := recover()
			t.Logf("output: recovered panic = %v", r)
			if r == nil {
				t.Fatal("expected SetHealthy to panic rather than silently no-op on a nil map -- a health checker wired to such a group would discard every probe result without a word")
			}
		}()
		(&TargetGroup{}).SetHealthy(mustURL(t, "http://backend-a"), false)
	})
}

// TestSetHealthyRemovesAndRestoresBackend is the core bookkeeping
// behaviour: a backend marked unhealthy drops out of the published
// snapshot, and marking it healthy again puts it back -- in its original
// position, not appended at the end.
func TestSetHealthyRemovesAndRestoresBackend(t *testing.T) {
	a := mustURL(t, "http://backend-a")
	b := mustURL(t, "http://backend-b")
	c := mustURL(t, "http://backend-c")
	g := New([]*url.URL{a, b, c})
	t.Logf("input: 3 healthy backends %v", names(g.Snapshot().Healthy))

	g.SetHealthy(b, false)
	afterDown := g.Snapshot()
	t.Logf("step: SetHealthy(backend-b, false) -> healthy=%v generation=%d", names(afterDown.Healthy), afterDown.Generation)
	for _, got := range afterDown.Healthy {
		if got.String() == b.String() {
			t.Fatalf("backend-b is still in the healthy list after being marked unhealthy")
		}
	}
	if len(afterDown.Healthy) != 2 {
		t.Fatalf("got %d healthy backends, want 2", len(afterDown.Healthy))
	}

	g.SetHealthy(b, true)
	afterUp := g.Snapshot()
	t.Logf("output: SetHealthy(backend-b, true) -> healthy=%v generation=%d", names(afterUp.Healthy), afterUp.Generation)

	want := []string{"http://backend-a", "http://backend-b", "http://backend-c"}
	got := names(afterUp.Healthy)
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got healthy=%v, want %v -- a recovered backend must return to its original position, since round-robin's cycle order depends on it", got, want)
		}
	}
}

// TestSetHealthyAllUnhealthyLeavesEmptySnapshot confirms the group reports
// "nobody is up" honestly rather than falling back to handing out a
// backend it knows is down. Turning that into a 502 is the balancer's and
// the proxy's job, not the group's.
func TestSetHealthyAllUnhealthyLeavesEmptySnapshot(t *testing.T) {
	a := mustURL(t, "http://backend-a")
	b := mustURL(t, "http://backend-b")
	g := New([]*url.URL{a, b})
	t.Log("input: 2 backends, both then marked unhealthy")

	g.SetHealthy(a, false)
	g.SetHealthy(b, false)

	snap := g.Snapshot()
	t.Logf("output: healthy=%v (len %d) generation=%d", names(snap.Healthy), len(snap.Healthy), snap.Generation)
	if len(snap.Healthy) != 0 {
		t.Fatalf("got %d healthy backends, want 0", len(snap.Healthy))
	}
}

// TestSetHealthyIgnoresUnknownBackend confirms SetHealthy is a no-op for a
// URL that isn't one of this group's backends, rather than silently
// growing the backend set. Membership is fixed at construction.
func TestSetHealthyIgnoresUnknownBackend(t *testing.T) {
	a := mustURL(t, "http://backend-a")
	stranger := mustURL(t, "http://not-a-backend")
	g := New([]*url.URL{a})
	t.Logf("input: group over [%s], SetHealthy called for unrelated %s", a, stranger)

	before := g.Snapshot()
	g.SetHealthy(stranger, false)
	after := g.Snapshot()

	t.Logf("output: healthy=%v generation=%d (was generation=%d)", names(after.Healthy), after.Generation, before.Generation)
	if len(after.Healthy) != 1 || after.Healthy[0].String() != a.String() {
		t.Fatalf("got healthy=%v, want [%s] -- an unrecognized backend must not affect the group", names(after.Healthy), a)
	}
	if after.Generation != before.Generation {
		t.Fatalf("generation moved from %d to %d for an unrecognized backend; it must not", before.Generation, after.Generation)
	}
}

// TestSetHealthyReportsWhetherStateChanged locks in the changed return
// value's contract: true only when a recognized backend's health actually
// differs from what was previously recorded. internal/healthcheck uses
// this to log real transitions instead of re-logging "still healthy" every
// interval.
func TestSetHealthyReportsWhetherStateChanged(t *testing.T) {
	a := mustURL(t, "http://backend-a")
	stranger := mustURL(t, "http://not-a-backend")
	g := New([]*url.URL{a})
	t.Log("input: fresh group over [backend-a], which starts healthy")

	if changed := g.SetHealthy(a, true); changed {
		t.Error("got changed=true for a no-op (already healthy) call, want false")
	}
	t.Log("step: SetHealthy(a, true) on an already-healthy backend -> changed=false")

	if changed := g.SetHealthy(a, false); !changed {
		t.Error("got changed=false for an actual flip healthy->unhealthy, want true")
	}
	t.Log("step: SetHealthy(a, false) -> changed=true")

	if changed := g.SetHealthy(a, false); changed {
		t.Error("got changed=true for a repeated identical call, want false")
	}
	t.Log("step: SetHealthy(a, false) again -> changed=false")

	if changed := g.SetHealthy(stranger, false); changed {
		t.Error("got changed=true for an unrecognized backend, want false")
	}
	t.Log("output: SetHealthy on an unrecognized backend -> changed=false")
}

// TestGenerationAdvancesOnlyOnRealChange is the contract that consistent
// hashing will depend on in a later phase: a cached derived structure
// (there, the hash ring) is rebuilt if and only if the generation moved.
// So the generation must advance on every real membership change and stay
// completely still otherwise -- a missed bump means a permanently stale
// ring, and a spurious bump means rebuilding thousands of hash positions
// for nothing.
func TestGenerationAdvancesOnlyOnRealChange(t *testing.T) {
	a := mustURL(t, "http://backend-a")
	b := mustURL(t, "http://backend-b")
	g := New([]*url.URL{a, b})

	type step struct {
		desc       string
		call       func() bool
		wantChange bool
	}
	steps := []step{
		{"SetHealthy(a, true) -- already healthy, no-op", func() bool { return g.SetHealthy(a, true) }, false},
		{"SetHealthy(a, false) -- real flip down", func() bool { return g.SetHealthy(a, false) }, true},
		{"SetHealthy(a, false) -- repeat, no-op", func() bool { return g.SetHealthy(a, false) }, false},
		{"SetHealthy(b, false) -- real flip down", func() bool { return g.SetHealthy(b, false) }, true},
		{"SetHealthy(a, true) -- real flip up", func() bool { return g.SetHealthy(a, true) }, true},
	}

	gen := g.Snapshot().Generation
	t.Logf("input: fresh group at generation %d", gen)

	for _, s := range steps {
		changed := s.call()
		next := g.Snapshot().Generation

		want := gen
		if s.wantChange {
			want = gen + 1
		}
		t.Logf("step: %-45s changed=%-5v generation %d -> %d (want %d)", s.desc, changed, gen, next, want)

		if changed != s.wantChange {
			t.Errorf("%s: got changed=%v, want %v", s.desc, changed, s.wantChange)
		}
		if next != want {
			t.Errorf("%s: got generation %d, want %d", s.desc, next, want)
		}
		gen = next
	}
	t.Logf("output: generation advanced exactly once per real change, ending at %d", gen)
}

// TestPublishedSnapshotIsNotMutatedInPlace guards the invariant that makes
// lock-free reads safe: a reader that grabbed a Snapshot and is still
// working with it must not see that value change underneath it when a
// health check fires. SetHealthy has to build a whole new Snapshot and
// swap the pointer, never edit the one already published.
//
// Without this property, a request goroutine could load a 3-backend
// snapshot, get preempted, and come back to find the slice it was indexing
// is now 2 elements long -- an out-of-range panic on the request path.
func TestPublishedSnapshotIsNotMutatedInPlace(t *testing.T) {
	a := mustURL(t, "http://backend-a")
	b := mustURL(t, "http://backend-b")
	c := mustURL(t, "http://backend-c")
	g := New([]*url.URL{a, b, c})

	held := g.Snapshot()
	t.Logf("input: a reader holds a snapshot: healthy=%v generation=%d", names(held.Healthy), held.Generation)

	g.SetHealthy(b, false)
	fresh := g.Snapshot()
	t.Logf("step: SetHealthy(backend-b, false) publishes a new snapshot: healthy=%v generation=%d", names(fresh.Healthy), fresh.Generation)

	t.Logf("output: the held snapshot still reads healthy=%v generation=%d", names(held.Healthy), held.Generation)
	if len(held.Healthy) != 3 {
		t.Fatalf("the previously-published snapshot now has %d backends, want 3 -- SetHealthy mutated a published Snapshot instead of replacing it", len(held.Healthy))
	}
	if held.Generation != fresh.Generation-1 {
		t.Fatalf("held generation %d, fresh generation %d -- the held snapshot's generation changed underneath its reader", held.Generation, fresh.Generation)
	}
	if held == fresh {
		t.Fatal("Snapshot returned the same pointer before and after a change -- a new Snapshot must be published, not the old one edited")
	}
}

// TestConcurrentSetHealthyAndSnapshot runs SetHealthy from several
// goroutines (simulating each backend's health-check loop reporting at
// once) concurrently with many Snapshot readers (simulating in-flight
// requests). It deliberately asserts almost nothing about the values --
// health is flapping throughout, so any particular result is valid. It
// exists to be run under `go test -race`, which is the actual check: the
// mutex-guarded map writes must not race with each other, and the atomic
// pointer swap must never let a reader observe a half-built Snapshot.
//
// It does assert one thing that must hold at every instant regardless of
// timing: every backend in a snapshot is one of the group's own backends,
// and the list never exceeds the group's size. A torn read would show up
// here as a nil entry or a stranger URL.
func TestConcurrentSetHealthyAndSnapshot(t *testing.T) {
	backends := []*url.URL{
		mustURL(t, "http://backend-a"),
		mustURL(t, "http://backend-b"),
		mustURL(t, "http://backend-c"),
	}
	known := make(map[string]bool, len(backends))
	for _, b := range backends {
		known[b.String()] = true
	}
	g := New(backends)
	t.Logf("input: %d backends, concurrent SetHealthy flapping and Snapshot reads for 100ms", len(backends))

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
					g.SetHealthy(b, healthy)
					healthy = !healthy
				}
			}
		}(b)
	}

	// Many reader goroutines, as the request path would be.
	var reads int64
	var readMu sync.Mutex
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			local := int64(0)
			for {
				select {
				case <-stop:
					readMu.Lock()
					reads += local
					readMu.Unlock()
					return
				default:
					snap := g.Snapshot()
					if len(snap.Healthy) > len(backends) {
						t.Errorf("snapshot has %d backends, more than the group's %d -- torn read", len(snap.Healthy), len(backends))
						return
					}
					for _, b := range snap.Healthy {
						if b == nil || !known[b.String()] {
							t.Errorf("snapshot contains unexpected backend %v -- torn read", b)
							return
						}
					}
					local++
				}
			}
		}()
	}

	time.Sleep(100 * time.Millisecond)
	close(stop)
	wg.Wait()
	t.Logf("output: %d snapshot reads completed against constant flapping, every one internally consistent", reads)
	t.Log("output: no data race reported (run with -race to make this test meaningful)")
}
