// Package targetgroup tracks a fixed set of backends and which of them are
// currently able to receive traffic.
//
// It answers exactly one question -- "who is up right now?" -- and knows
// nothing about how a backend gets chosen from among those that are up.
// That second question ("whose turn is it?") is the load-balancing
// algorithm's job, and lives in internal/balancer.
//
// Splitting the two matters because the first question has the same answer
// for every algorithm. Round-robin, weighted round-robin,
// least-connections and consistent hashing all need identical health
// bookkeeping; only their selection logic differs. Keeping that
// bookkeeping here means a new algorithm implements selection and nothing
// else.
//
// The name follows AWS's term for the same concept. Other load balancers
// call it a pool (F5), an upstream (nginx), or a backend (HAProxy) -- every
// serious load balancer has a name for this exact seam, which is decent
// evidence it is the right place to cut.
//
// A TargetGroup has two kinds of user, and they are why it is shaped the
// way it is:
//
//   - Writers (internal/healthcheck's probe goroutines, one per backend)
//     call SetHealthy. There are few of them and they call rarely -- once
//     per backend per health-check interval.
//   - Readers (one goroutine per in-flight HTTP request) call Snapshot.
//     There can be thousands of them and they call constantly.
//
// So the design pays for writes to make reads free: a write takes a mutex
// and rebuilds a derived list, while a read is a single atomic pointer
// load that never blocks and never allocates.
package targetgroup

import (
	"net/url"
	"sync"
	"sync/atomic"
)

// Snapshot is a consistent, point-in-time view of a TargetGroup, published
// as a single immutable value.
//
// Both fields are published together by one atomic pointer store, which is
// the whole reason this is a struct rather than two separate fields on
// TargetGroup. If Generation and Healthy could be read separately, a
// reader could see a new Generation next to an older Healthy list, cache a
// stale derived structure against the new number, and never rebuild it
// again. Bundling them makes that impossible: you either see both old
// values or both new ones.
//
// IMPORTANT INVARIANT: a published Snapshot is never modified. SetHealthy
// builds an entirely new Snapshot and swaps the pointer rather than
// touching the old one, so a reader holding an older Snapshot is looking
// at something still valid and internally consistent -- just possibly one
// update behind.
//
// Callers must not write to Healthy. Go has no way to express "immutable
// slice" in the type system -- returning a defensive copy would allocate
// on every request, which is exactly the cost this design exists to avoid
// -- so this is enforced by convention and by the package boundary: only
// this package ever constructs a Snapshot. Note that a caller writing into
// Healthy is not a data race, so `go test -race` will NOT catch it; it is
// a perfectly legal write to shared memory that simply corrupts every
// other reader's view.
type Snapshot struct {
	// Healthy is the subset of the group's backends currently marked
	// healthy, in the stable order the group was constructed with. It is
	// empty (not nil-checked for you) when every backend is down.
	Healthy []*url.URL

	// Generation increases by one every time membership actually
	// changes, and is otherwise stable. It exists for consumers that
	// derive an expensive structure from Healthy and want to rebuild it
	// only when it is genuinely out of date.
	//
	// Round-robin ignores this: indexing a slice is so cheap that it
	// just reads Healthy fresh on every request. Consistent hashing
	// cannot -- its ring costs hundreds or thousands of hashes plus a
	// sort to build, so it caches one and compares the Generation it was
	// built from against the current Generation to decide whether to
	// rebuild. That is the same trick as an HTTP ETag with
	// If-None-Match, etcd's revision numbers, or a Kubernetes
	// resourceVersion: publish a cheap version stamp beside expensive
	// data and let each consumer decide when recomputing is worth it.
	//
	// Generation starts at 1, never 0, so that a consumer whose cache
	// field is still at its zero value can never accidentally match a
	// real generation and skip its first build.
	Generation uint64
}

// TargetGroup records the health of a fixed set of backends. It is safe for
// concurrent use by any number of goroutines.
//
// The backend set itself is fixed at construction: backends never join or
// leave a TargetGroup, they only flip between healthy and unhealthy.
// (Dynamic membership -- service discovery -- would be a later phase, and
// would change what has to be locked here.)
type TargetGroup struct {
	// all is the full, original backend list: fixed at construction,
	// never mutated afterward. It is what SetHealthy walks to rebuild
	// the healthy list in a stable order, which is what makes
	// round-robin's cycle deterministic rather than depending on Go's
	// randomized map iteration order.
	all []*url.URL

	// mu guards status and generation together. A single SetHealthy call
	// touches three things that must change as one indivisible step from
	// every other goroutine's point of view -- this backend's entry in
	// status, the generation counter, and the derived Snapshot published
	// below -- which is exactly the "more than one related field" case
	// where a mutex is the right tool rather than a lone atomic integer.
	// There is no single hardware instruction that updates a map entry,
	// bumps a counter and rebuilds a derived slice at once, so a lock is
	// what makes the three changes appear simultaneous.
	//
	// There can be several concurrent writers -- one health-check
	// goroutine per backend -- so without mu two of them could race on
	// the map itself. Go maps are not safe for concurrent read/write,
	// let alone concurrent writes; the runtime detects this and
	// deliberately crashes the process rather than corrupting the map
	// silently.
	mu         sync.Mutex
	status     map[string]bool // keyed by backend.String(); true == healthy
	generation uint64

	// current holds the most recently published Snapshot. Readers load
	// it without ever touching mu: an atomic pointer load is a single
	// instruction that always returns some complete, previously-stored
	// pointer, never a half-written one. That is what keeps the request
	// path free of any contention with health checks -- a thousand
	// concurrent requests reading this do not block each other or block
	// a health check trying to write.
	current atomic.Pointer[Snapshot]
}

// New builds a TargetGroup over backends, which must be non-empty.
//
// The slice is copied, so later mutations the caller makes to backends (a
// reorder, an append) cannot reach into the group's internal state.
// Without the copy, such a mutation would be an unsynchronized write
// racing against every concurrent Snapshot reader.
//
// Every backend starts marked healthy. This is an optimistic default: a
// backend receives traffic before any health check has actually run
// against it, rather than being withheld until the first probe completes.
// The cost is a startup window in which a backend that is broken from the
// start still receives requests until its first failed check marks it
// down. The alternative (pessimistic start) trades that for a window at
// startup where the load balancer has no backends at all and rejects
// everything, which is usually worse.
func New(backends []*url.URL) *TargetGroup {
	if len(backends) == 0 {
		panic("targetgroup: New requires at least one backend")
	}

	owned := make([]*url.URL, len(backends))
	copy(owned, backends)

	status := make(map[string]bool, len(owned))
	for _, b := range owned {
		status[b.String()] = true
	}

	g := &TargetGroup{all: owned, status: status, generation: 1}

	// The initial Snapshot needs its own copy of the list: publishing
	// `owned` directly would hand readers a slice that shares its
	// backing array with g.all, and a later rebuild could then be
	// observed mid-write by a reader still holding this Snapshot.
	initial := make([]*url.URL, len(owned))
	copy(initial, owned)
	g.current.Store(&Snapshot{Healthy: initial, Generation: g.generation})

	return g
}

// Snapshot returns the current view of the group. It never blocks, never
// allocates, and is safe to call from any number of goroutines at once --
// it is a single atomic pointer load.
//
// The returned Snapshot must be treated as read-only (see the Snapshot
// type's invariant). Callers should load it once and reuse that value for
// the duration of a decision rather than calling Snapshot repeatedly,
// since two separate calls can return two different generations.
func (g *TargetGroup) Snapshot() *Snapshot {
	return g.current.Load()
}

// SetHealthy records backend's current health and, if that is an actual
// change, publishes a new Snapshot with a bumped Generation.
//
// backend must be one of the URLs the group was constructed with;
// unrecognized backends are ignored rather than silently joining the
// group, since membership is fixed at construction.
//
// changed reports whether this call actually altered the group's state. A
// health checker reporting "still healthy" every interval -- the common
// case by far -- returns false and costs one map lookup with no
// allocation and no pointer swap. Callers use the returned bool to log
// real transitions instead of re-logging the same state every interval,
// without having to track the previous value themselves.
//
// The method is deliberately named SetHealthy rather than Set so that
// *TargetGroup satisfies healthcheck.HealthReporter structurally, with no
// adapter and without either package importing the other.
func (g *TargetGroup) SetHealthy(backend *url.URL, healthy bool) (changed bool) {
	key := backend.String()

	g.mu.Lock()
	defer g.mu.Unlock()

	current, ok := g.status[key]
	if !ok {
		// Not one of this group's backends -- nothing to update.
		return false
	}
	if current == healthy {
		// No actual change -- skip the rebuild-and-publish entirely.
		return false
	}
	g.status[key] = healthy
	g.generation++

	// Rebuild from g.all rather than from the map so the result keeps
	// the original construction order. Iterating the map instead would
	// produce a different order on every rebuild, because Go randomizes
	// map iteration order on purpose to stop code from depending on it.
	rebuilt := make([]*url.URL, 0, len(g.all))
	for _, b := range g.all {
		if g.status[b.String()] {
			rebuilt = append(rebuilt, b)
		}
	}

	// Publish both fields in one store. Until this line executes,
	// readers still see the previous Snapshot in full; after it, they
	// see the new one in full. There is no in-between state.
	g.current.Store(&Snapshot{Healthy: rebuilt, Generation: g.generation})

	return true
}
