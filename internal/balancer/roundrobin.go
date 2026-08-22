// Package balancer selects which backend should handle the next request.
package balancer

import (
	"net/url"
	"sync/atomic"
)

// Balancer selects the next backend to receive a request. Implementations
// must be safe for concurrent use, since net/http calls into the proxy from
// a separate goroutine per in-flight request.
type Balancer interface {
	Next() *url.URL
}

// RoundRobin cycles through a fixed list of backends in order: 0, 1, 2, ...,
// len(backends)-1, 0, 1, ... It assumes backends are roughly equal capacity
// and requests are roughly equal cost -- no weighting or live health signal
// is considered.
type RoundRobin struct {
	backends []*url.URL
	// counter only ever moves forward by 1 per call, via atomic.Add. That's
	// the entire piece of state Next() has to coordinate across goroutines,
	// which is why an atomic integer is enough here and a mutex isn't needed.
	counter atomic.Uint64
}

// NewRoundRobin builds a RoundRobin over backends, which must be non-empty.
func NewRoundRobin(backends []*url.URL) *RoundRobin {
	if len(backends) == 0 {
		panic("balancer: NewRoundRobin requires at least one backend")
	}
	return &RoundRobin{backends: backends}
}

// Next returns the next backend in the cycle. Safe to call concurrently
// from any number of goroutines.
func (r *RoundRobin) Next() *url.URL {
	// counter.Add(1) is a single atomic fetch-and-add CPU instruction: it
	// reads the current value, adds 1, and writes the result back, all as
	// one step no other goroutine can observe half-finished. It also
	// returns the *new* value, so if two goroutines call this at the same
	// instant, one gets back N and the other gets back N+1 -- never the
	// same number twice. That per-call uniqueness is what round-robin
	// depends on: without it, two concurrent requests could both land on
	// backend 0 and the cycle would skip a step.
	n := r.counter.Add(1) - 1
	return r.backends[n%uint64(len(r.backends))]
}
