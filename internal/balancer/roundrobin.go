// Package balancer selects which backend should handle the next request.
package balancer

import (
	"net/url"
	"sync"
	"sync/atomic"
)

// Balancer selects the next backend to receive a request. Implementations
// must be safe for concurrent use, since net/http calls into the proxy from
// a separate goroutine per in-flight request. Next returns nil if there is
// no backend currently able to receive a request (for example, every
// backend has failed its health check).
type Balancer interface {
	Next() *url.URL
	// SetHealthy records the current health of a single backend, as
	// determined by something outside the balancer (a health checker).
	// backend must be one of the URLs the balancer was constructed with.
	// changed reports whether this call actually altered routing state
	// (a recognized backend whose health value differs from what was
	// previously recorded) -- callers that want to log state transitions,
	// rather than every individual health-check result, use this instead
	// of re-deriving "did this change" themselves.
	SetHealthy(backend *url.URL, healthy bool) (changed bool)
}

// RoundRobin cycles through the backends currently marked healthy, in the
// order they were originally given: 0, 1, 2, ..., wrapping back to 0. It
// assumes backends are roughly equal capacity and requests are roughly
// equal cost -- no weighting or load-aware selection is considered.
type RoundRobin struct {
	// all is the full, original backend list: fixed at construction,
	// never mutated afterward. It's what SetHealthy consults to rebuild
	// the healthy slice in a stable order.
	all []*url.URL

	// counter only ever moves forward by 1 per call, via atomic.Add. Next
	// uses it to pick a position in the *current* healthy slice.
	counter atomic.Uint64

	// mu guards status, which SetHealthy both reads and writes. A single
	// SetHealthy call touches two things that must change together --
	// this backend's entry in status, and the derived slice stored in
	// healthy below -- which is exactly the "more than one related field"
	// case where a mutex is the right tool instead of a lone atomic
	// integer: there's no single hardware instruction that updates a map
	// entry and rebuilds a derived slice as one indivisible step, so we
	// need a lock to make the two changes appear atomic to every other
	// goroutine. Unlike counter, status can have multiple concurrent
	// writers -- one health-check goroutine per backend -- so without mu
	// two goroutines could race on the map itself (Go maps aren't even
	// safe for concurrent read/write, let alone concurrent writes).
	mu     sync.Mutex
	status map[string]bool // keyed by backend.String(); true == healthy

	// healthy is the subset of all currently marked healthy, in all's
	// order. It's what Next() actually reads. Storing it behind an
	// atomic.Pointer means Next() never blocks on mu: it just loads
	// whatever the most recently published slice is, even if a
	// SetHealthy call is concurrently rebuilding the next one. The slice
	// itself is never mutated in place after being built -- each
	// SetHealthy call builds a brand new slice and swaps the pointer --
	// so a goroutine that loaded an old slice a moment ago is still
	// looking at something valid and internally consistent, just
	// possibly one update stale.
	healthy atomic.Pointer[[]*url.URL]
}

// NewRoundRobin builds a RoundRobin over backends, which must be non-empty.
// The slice is copied, so later mutations the caller makes to backends (a
// reorder, an append) can't reach into the balancer's internal state --
// without this, such a mutation would be an unsynchronized write racing
// against every concurrent Next() call reading the slice.
//
// Every backend starts marked healthy. This is an optimistic default: the
// balancer routes to a backend before any health check has actually run
// against it, rather than withholding traffic until the first check
// completes. The cost is a startup window where a backend that's actually
// broken from the start still receives requests until its first failed
// check marks it down.
func NewRoundRobin(backends []*url.URL) *RoundRobin {
	if len(backends) == 0 {
		panic("balancer: NewRoundRobin requires at least one backend")
	}
	owned := make([]*url.URL, len(backends))
	copy(owned, backends)

	status := make(map[string]bool, len(owned))
	for _, b := range owned {
		status[b.String()] = true
	}

	r := &RoundRobin{all: owned, status: status}
	initial := make([]*url.URL, len(owned))
	copy(initial, owned)
	r.healthy.Store(&initial)
	return r
}

// Next returns the next backend in the cycle over currently healthy
// backends, or nil if none are healthy. Safe to call concurrently from any
// number of goroutines.
func (r *RoundRobin) Next() *url.URL {
	healthy := *r.healthy.Load()
	if len(healthy) == 0 {
		return nil
	}
	// counter.Add(1) is a single atomic fetch-and-add CPU instruction: it
	// reads the current value, adds 1, and writes the result back, all as
	// one step no other goroutine can observe half-finished. It also
	// returns the *new* value, so if two goroutines call this at the same
	// instant, one gets back N and the other gets back N+1 -- never the
	// same number twice. That per-call uniqueness is what round-robin
	// depends on: without it, two concurrent requests could both land on
	// the same backend and the cycle would skip a step.
	//
	// The healthy set can change between one call and the next (a
	// SetHealthy call swapping the pointer), so the counter isn't a
	// strict "position in a fixed cycle" anymore -- it's just a source of
	// ever-increasing, unique numbers that we mod against whatever the
	// healthy set happens to be *right now*. A backend flipping
	// healthy/unhealthy can shift which backend a given counter value
	// maps to, which is a known, accepted imprecision: perfect fairness
	// across a changing set isn't the goal here, "skip known-dead
	// backends" is.
	n := r.counter.Add(1) - 1
	return healthy[n%uint64(len(healthy))]
}

// SetHealthy records backend's current health and, if that's a change,
// rebuilds the healthy slice that Next() reads. backend must be one of the
// URLs originally passed to NewRoundRobin; unrecognized backends are
// ignored, since only the health checker (which was itself constructed
// from this balancer's backend list) is expected to call this.
func (r *RoundRobin) SetHealthy(backend *url.URL, healthy bool) (changed bool) {
	key := backend.String()

	r.mu.Lock()
	defer r.mu.Unlock()

	current, ok := r.status[key]
	if !ok {
		// Not one of this balancer's backends -- nothing to update.
		return false
	}
	if current == healthy {
		// No actual change -- skip the rebuild-and-swap. This also
		// means a health checker reporting "still healthy" every
		// cycle (the common case) is nearly free: one map lookup, no
		// allocation.
		return false
	}
	r.status[key] = healthy

	rebuilt := make([]*url.URL, 0, len(r.all))
	for _, b := range r.all {
		if r.status[b.String()] {
			rebuilt = append(rebuilt, b)
		}
	}
	r.healthy.Store(&rebuilt)
	return true
}
