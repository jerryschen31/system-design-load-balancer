// Package balancer selects which backend should handle the next request.
//
// A balancer answers one question -- "whose turn is it?" -- and reads, but
// never owns, the answer to "who is up right now?". That second question
// belongs to internal/targetgroup, which every algorithm in this package
// shares. Keeping health bookkeeping out of here means adding
// least-connections or consistent hashing later is a matter of writing
// selection logic and nothing else.
package balancer

import (
	"net/url"
	"sync/atomic"

	"github.com/jerryschen31/system-design-load-balancer/internal/targetgroup"
)

// Balancer selects the next backend to receive a request. Implementations
// must be safe for concurrent use, since net/http calls into the proxy from
// a separate goroutine per in-flight request. Next returns nil if there is
// no backend currently able to receive a request (for example, every
// backend has failed its health check).
//
// The interface is deliberately this narrow. It used to also carry
// SetHealthy, which meant its one consumer (internal/proxy) declared a
// dependency on a method it never calls. Health reporting now goes to the
// TargetGroup directly, so the interface describes exactly what a caller
// needs from a balancer and nothing more.
type Balancer interface {
	Next() *url.URL
}

// RoundRobin hands out the backends currently marked healthy in the order
// they were originally given: 0, 1, 2, ..., wrapping back to 0. It assumes
// backends are roughly equal capacity and requests are roughly equal cost
// -- no weighting or load-aware selection is considered.
//
// Note how little state this needs. Round-robin genuinely is just a
// counter; everything else it used to hold was health bookkeeping, which
// now lives in the shared TargetGroup.
type RoundRobin struct {
	// group is the shared source of truth for which backends are up.
	// RoundRobin only ever reads from it -- the health checker is what
	// writes to it -- so the two never call into each other.
	group *targetgroup.TargetGroup

	// counter only ever moves forward by 1 per call, via atomic.Add.
	// Next uses it to pick a position in the healthy list.
	counter atomic.Uint64
}

// NewRoundRobin builds a RoundRobin that routes over group's healthy
// backends. group must be non-nil, and is typically shared with the health
// checker so that probe results reach routing without either component
// knowing about the other.
//
// group is taken as a concrete *targetgroup.TargetGroup rather than an
// interface. Go's "accept interfaces, return structs" guidance applies
// when a caller might plausibly supply a different implementation; here
// there is exactly one, and any interface declared for it would still
// mention targetgroup.Snapshot in its signature -- so it would buy the
// same import dependency with extra indirection.
func NewRoundRobin(group *targetgroup.TargetGroup) *RoundRobin {
	if group == nil {
		panic("balancer: NewRoundRobin requires a non-nil target group")
	}
	return &RoundRobin{group: group}
}

// Next returns the next backend in the cycle over currently healthy
// backends, or nil if none are healthy. Safe to call concurrently from any
// number of goroutines.
func (r *RoundRobin) Next() *url.URL {
	// One Snapshot load, reused for the whole decision. Calling
	// r.group.Snapshot() twice here would be a bug waiting to happen: a
	// health check could land between the two calls and the length used
	// for the modulo would no longer describe the slice being indexed.
	// Reading a consistent view once and working from it is the entire
	// point of the Snapshot type.
	//
	// Round-robin ignores snap.Generation. Indexing a slice is cheap
	// enough that there is nothing worth caching between requests -- the
	// generation exists for algorithms whose derived structure is
	// expensive to rebuild, like consistent hashing's ring.
	healthy := r.group.Snapshot().Healthy
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
	// SetHealthy call on the group publishing a new snapshot), so the
	// counter isn't a strict "position in a fixed cycle" anymore -- it's
	// just a source of ever-increasing, unique numbers that we mod
	// against whatever the healthy set happens to be *right now*. A
	// backend flipping healthy/unhealthy can shift which backend a given
	// counter value maps to, which is a known, accepted imprecision:
	// perfect fairness across a changing set isn't the goal here, "skip
	// known-dead backends" is.
	n := r.counter.Add(1) - 1
	return healthy[n%uint64(len(healthy))]
}
