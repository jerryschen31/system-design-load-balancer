# Hotfix: Extract `TargetGroup` from the balancer

- **Branch:** `hotfix/create-targetgroup` (off `build` at `a54b0a5`)
- **PR:** https://github.com/jerryschen31/system-design-load-balancer/pull/7
- **Status:** complete
- **Test report:** [`outputs/tests/hotfix-create-targetgroup.md`](../outputs/tests/hotfix-create-targetgroup.md)

Not a numbered phase — a structural cleanup between phase 4b and phase 5, prompted by a question about the composition root that turned out to have a real design answer behind it (and to be sitting next to a real bug).

## 1. What was built

**In scope:**

- A new leaf package, `internal/targetgroup`, that owns the answer to "which backends are up right now?" — the health map, the mutex guarding it, and the published list of healthy backends.
- `internal/balancer` rewritten to read from a `TargetGroup` rather than own that state. `RoundRobin` drops from five fields to two.
- A `Generation` counter on the published snapshot, so a later algorithm can cache an expensive derived structure and rebuild it only when membership actually moves.
- Repair of a live bug on `build`: the health checker was logging probe results but never reporting them, so unhealthy backends kept receiving traffic.

**Explicitly out of scope:**

- Dynamic membership (backends joining/leaving at runtime). Membership is still fixed at construction.
- Flap damping, slow-start on recovery, per-backend weights. All still deferred.
- Consistent hashing itself — only the hook it will need was added.

## 2. System Design concepts & considerations

### Concept: where health state belongs

- **Options considered:** (a) leave it on `RoundRobin`, as it was; (b) extract it into a shared object both the checker and the balancer use; (c) have each algorithm keep its own copy.
- **Trade-offs:** Leaving it in place costs nothing today but means least-connections, weighted round-robin and consistent hashing each reimplement identical bookkeeping — three more chances to get a lock wrong. Extracting it costs one indirection and one more package.
- **What we chose and why:** (b). The decisive argument was that the two questions have different shapes. "Who is up?" has the *same* answer for every algorithm; "whose turn is it?" is the algorithm. Those don't belong in one type. The naming is also evidence: F5 calls this a pool, AWS a target group, nginx an upstream, HAProxy a backend — every serious load balancer has a word for exactly this seam, which is a good sign it's a real one and not an arbitrary cut. We used AWS's term.

### Concept: which direction a dependency points, and who declares the interface

- **Options considered:** the balancer implements an interface the health checker declares (status quo); the health checker imports the balancer; both depend on a third thing.
- **Trade-offs:** Go satisfies interfaces structurally — a type implements an interface merely by having the right methods, with no `implements` keyword and no import. That means the *consumer* can declare the narrowest interface it needs and any producer satisfies it accidentally. `healthcheck.HealthReporter` was already written this way, which is why `healthcheck` never imported `balancer` even when it was calling into it.
- **What we chose and why:** both depend on `targetgroup`, which depends on nothing but the standard library. This inverts the last remaining arrow. Before, the checker called into the balancer; now the checker writes to a group and the balancer reads from one, and swapping the algorithm changes nothing about health checking.

### Concept: shared state vs. message passing

- **Options considered:** the direct call we had; a channel of health events consumed in `main`; a channel consumed by a goroutine inside the balancer (the actor model); a shared, lock-protected object.
- **Trade-offs:** A channel doesn't remove the coupling, it relocates it — something still has to receive and apply the update, and now you also own a buffer policy, a drop policy, and a shutdown path. Consuming in `main` puts *more* wiring at the composition root, not less. Consuming inside the balancer removes the mutex but adds a goroutine and a lifecycle to a type that is currently a plain value, and the read path still needs an atomic anyway, because thousands of concurrent request goroutines can't each do a channel round-trip to ask "who's next" without serialising the entire load balancer through one goroutine.
- **What we chose and why:** shared state behind a lock. The Go proverb "share memory by communicating" is real, but the Go team's own follow-up is the operative half here: **channels transfer ownership and orchestrate; mutexes protect shared state.** The healthy list is shared state read by every request. The deciding variable is the number of consumers — with exactly one, a direct write to shared state is right; a channel or subscription earns its complexity only when several parties care.

One property worth recording, because it decides how much the transport matters: this health signal is **level-triggered, not edge-triggered**. Every probe re-reports the backend's current state each interval rather than announcing a change. So a lost or stale update self-heals within one interval. Systems that resend full state tolerate lossy transport; systems that send deltas do not. It's the same reason Kubernetes controllers re-list and reconcile instead of trusting a watch stream.

### Concept: publishing expensive derived state with a version stamp

- **Options considered:** consumers recompute from the healthy list on every request; the group pushes a callback to registered subscribers on change; the group publishes a version number and consumers pull.
- **Trade-offs:** Recompute-per-request is free for round-robin (a slice index) and unaffordable for consistent hashing (hundreds of hashes plus a sort). Push means the group holds a subscriber list and calls into foreign code — potentially while holding its own lock, which is a classic deadlock setup — and runs the rebuild on a health-check goroutine, so a slow rebuild stalls health checking. Pull-with-a-version means the consumer that cares does the work, on a goroutine that actually needs the answer.
- **What we chose and why:** pull. `Generation` increments on every real membership change and is otherwise still. This is the same mechanism as an HTTP `ETag` with `If-None-Match`, etcd revisions, or a Kubernetes `resourceVersion`: publish a cheap stamp beside expensive data and let each consumer decide when recomputing is worth it.

The subtle part is that the stamp and the data must be published **together**:

```
  WRONG                                RIGHT

  atomic Healthy    ─┐                 atomic Snapshot ─┬─ Healthy
  atomic Generation ─┘                                  └─ Generation

  A reader can load Generation=7       One pointer store publishes both.
  next to Generation=6's list, cache   A reader sees either the whole old
  a ring against the new number, and   value or the whole new one. There is
  never rebuild it again.              no in-between.
```

That is the entire reason `Snapshot` is a struct rather than two fields on `TargetGroup`. `Generation` also starts at 1, never 0, so a consumer whose cache field is still at its zero value cannot accidentally match a real generation and skip its first build.

### Concept: an unusable zero value, and where to enforce it

Added during PR review (see §7).

- **Options considered:** (a) prevent `&TargetGroup{}` from being constructible at all; (b) tolerate it by having `Snapshot` return an empty snapshot; (c) check it at each consumer, e.g. in `NewRoundRobin`; (d) make the type itself reject it at the first use of either entry point, and have consumers trigger that check at construction.
- **Trade-offs:** (a) is impossible in Go -- an empty composite literal is legal from any package even when every field is unexported, so a type cannot force callers through its constructor. (b) is the worst option available: an empty snapshot means "no healthy backends", so the load balancer would 502 every request forever with no indication why. (c) fixes the one call site under review while leaving every other consumer exposed, and duplicates the definition of a valid group into each of them. (d) puts one definition in one place.
- **What we chose and why:** (d). `Snapshot` and `SetHealthy` both panic with a single shared message, and `NewRoundRobin` calls `Snapshot` at construction purely to trigger that check early.

The general principle worth carrying out of this: **when a type's zero value cannot be valid, the failure belongs to the type, not to its callers.** A check written into each consumer is a rule that has to be remembered N times.

Which entry point matters more is counterintuitive, and is the part worth remembering. `Snapshot` on an unbuilt group would fail loudly anyway -- returning nil for the caller to dereference -- so guarding it only improves the error message. `SetHealthy` would not fail at all: **reading from a nil map is legal in Go and returns the zero value**, so an unbuilt group accepts every health report, silently discards it, and answers `changed=false` forever. Backends never get marked down and nothing is logged. Turning a silent failure into a loud one is a real fix; improving a panic message is a nicety.

### Concept: making reads cheap by paying on writes

`TargetGroup` has two very different populations of user:

```
  writers: one health-check goroutine per backend
           a few, calling once per health-check interval
                       │
                       ▼  take mutex, rebuild list, swap pointer
              ┌──────────────────┐
              │   TargetGroup    │
              └──────────────────┘
                       │  single atomic load, no lock, no allocation
                       ▼
  readers: one goroutine per in-flight request
           potentially thousands, calling constantly
```

So the design deliberately makes writes more expensive (allocate a whole new list, never edit in place) to make reads free. The stress test measured 277,641 reads in 100ms against three continuously flapping writers with no contention and no race.

The invariant that makes this safe is that a published `Snapshot` is **never modified**. A reader holding an older one is looking at something still valid and internally consistent, just possibly one update behind.

## 3. Design conversation summary

The trigger was a question about one line in `main()`:

```go
checker := healthcheck.NewChecker(targets, ..., rr, logger)
```

— "why does the health checker need the whole round-robin balancer, when all it uses is `SetHealthy`?"

The first answer was that it doesn't, and never did. `NewChecker`'s parameter is typed as `HealthReporter`, a one-method interface, and a Go interface value at runtime is two words: a pointer to the data and a pointer to a method table containing **only the interface's methods**. `Next` is not in that table. The checker literally could not call it. So the visibility concern wasn't real, and the packages were already independent in both directions.

But the question was pointing at something real, just one level down: *why does every load-balancing algorithm have to implement health bookkeeping at all?* That's duplication rather than coupling, and channels — the next idea raised — don't fix duplication. Working through the three channel shapes made that concrete: each one still ended with `SetHealthy` defined on `RoundRobin`, and each one added a buffer policy, a drop policy and a shutdown path to own.

That reframing is what produced the registry. Once "who is up" is a separate object, the checker and the balancer stop being connected at all — they share a value instead of calling each other.

The last turn of the conversation was about whether phase 7 would force a second refactor. Consistent hashing can't filter a list per request the way round-robin does; it needs its ring rebuilt when membership changes. Rather than add a subscription mechanism for a single subscriber, we added a version stamp — one field and one increment — which keeps phase 7 purely additive. The naming also changed here: "pool" was rejected in favour of "target group" to avoid collision with *connection* pool, which this repo will plausibly grow later.

The bug surfaced while reading the code to answer the original question: `healthcheck`'s `report` logged each probe result and never called the reporter. It had been that way on `build` since `b3f8c11`.

## 4. Code changes

| File | Change |
|---|---|
| `internal/targetgroup/targetgroup.go` | **New.** `TargetGroup` (fixed backend list, health map + mutex, generation, published snapshot) and the immutable `Snapshot{Healthy, Generation}`. Methods: `New`, `Snapshot`, `SetHealthy`. |
| `internal/balancer/roundrobin.go` | 161 → 110 lines. `RoundRobin` is now `{group, counter}`. `SetHealthy` deleted; `Next` reads one snapshot from the group. `Balancer` interface reduced to `Next() *url.URL`. |
| `internal/healthcheck/healthcheck.go` | `report` now actually calls `c.reporter.SetHealthy` and logs only when it returns `changed`. |
| `cmd/loadbalancer/main.go` | Constructs the group and hands the same value to both the balancer and the checker. |
| test files | Call sites updated; health tests relocated (see below). |

`SetHealthy` is deliberately named that rather than `Set` so `*TargetGroup` satisfies `healthcheck.HealthReporter` structurally, with no adapter and no import either way.

The `Balancer` interface losing `SetHealthy` is a small independent win: its only consumer, `internal/proxy`, calls `Next()` and nothing else, so the interface had been advertising a dependency it didn't have.

## 5. Testing

`go test -v -race -count=1 ./cmd/... ./internal/...` — **67 passed, 0 failed, 7 packages.**

### Functional tests

Tests were split along **bookkeeping vs. routing**, which is a slightly different line than "health tests move to the new package":

- `internal/targetgroup` — does the map, the list, the generation and the `changed` return behave? This is where the four relocated health tests live, plus `TestNewCopiesBackends`, which *had* to move because `NewRoundRobin` no longer takes a backend slice at all.
- `internal/balancer` — does routing actually *honour* the group? `TestRoundRobinSkipsUnhealthyBackend` and `TestRoundRobinAllUnhealthyReturnsNil` stayed here on purpose. The group→balancer seam is the thing that can now break silently across a package boundary (a balancer caching a stale snapshot would still pass every test in `targetgroup`), so it is covered from both sides. `AllUnhealthyReturnsNil` in particular is a balancer decision: the group just reports an empty list; turning that into "no target, let the proxy 502" is the balancer's call.

`TestGenerationAdvancesOnlyOnRealChange` is new and pins the phase-7 contract directly: five `SetHealthy` calls (no-op, real flip down, repeat, second flip down, flip up) asserting both the `changed` return and the generation after each. A missed bump would mean a permanently stale ring; a spurious bump would mean rebuilding thousands of hash positions for nothing.

### Non-functional / stress tests

**Regression evidence for the severed wire.** The bug was traffic-affecting, and an existing test already caught it. Against `build` before the fix:

```
integration_test.go:121: output: good backend got 15 requests, unhealthy backend got 15
integration_test.go:123: unhealthy backend received 15 requests, want 0
--- FAIL: TestIntegration_ChecksRouteAroundUnhealthyBackend (0.12s)
```

Exactly half the traffic went to a backend that was already answering 503 before the checker ever started — the cycle was never told to skip it. Passes on this branch.

**`TestPublishedSnapshotIsNotMutatedInPlace`** guards the invariant that makes lock-free reads safe. A reader holds a snapshot, a health check publishes a new one, and the held value must be untouched. Its log reads as the proof:

```
input:  a reader holds a snapshot: healthy=[a b c] generation=1
step:   SetHealthy(b, false) publishes a new one: healthy=[a c] generation=2
output: the held snapshot still reads healthy=[a b c] generation=1
```

Without this property, a request goroutine could load a 3-backend snapshot, get preempted, and come back to find the slice it was indexing is now 2 elements long.

**`TestRoundRobinNextDuringHealthFlapping`** is the adversarial version of that same hazard at the balancer level: 20 goroutines calling `Next()` while all 3 backends flap health continuously for 100ms. It hunts specifically for an index-out-of-range panic — which is what would happen if `Next()` read the healthy list twice, once for `len()` and once for the index, with a health check landing in between. Loading one snapshot and reusing it is what prevents it, and this test is what would catch a future edit that breaks that.

**`TestConcurrentSetHealthyAndSnapshot`** is the race-detector test for the group: 3 flapping writers against 20 readers. 277,641 reads completed in 100ms, every one internally consistent (no nil entries, no unknown backends, never longer than the group), no race reported.

## 6. Known weaknesses / open questions

Carried forward from earlier phases, unchanged by this work:

- **Membership is fixed at construction.** Backends flip healthy/unhealthy but cannot join or leave. Adding service discovery would change what has to be locked: `all` is currently never written after construction, which is exactly what lets the rebuild loop read it safely.
- **No flap damping.** One failed probe marks a backend down; one success brings it back. Production health checkers use N-consecutive-failures / M-consecutive-successes thresholds so a single blip doesn't churn routing.
- **No slow start on recovery.** A recovered backend gets its full share on the very next request, which can immediately re-overwhelm whatever was struggling.

New with this design, and accepted:

- **`Snapshot.Healthy` is a mutable slice that callers must not write to.** Go cannot express an immutable slice, and a defensive copy would allocate on every request — precisely the cost this design exists to avoid. Enforced by convention plus the package boundary, since only `internal/targetgroup` constructs a `Snapshot`. Worth stating loudly: **a caller writing into it is not a data race, so `-race` will not catch it.** It's a legal write to shared memory that silently corrupts every other reader's view.
- **Round-robin fairness across a changing healthy set stays approximate.** The counter is a source of unique increasing numbers modded against whatever the list is right now, so a health flip shifts which backend a given value maps to. Unchanged from phase 2 and still the accepted trade-off — the goal is "skip known-dead backends", not perfect fairness across a moving set.
- **`Generation` currently has no consumer.** It is speculative, added on the explicit judgement that one field now is cheaper than refactoring this package during phase 7. If consistent hashing ends up not needing it, it should be deleted rather than left as decoration.

**Open question for phase 7:** the pull-with-a-version design means the first request after a health change pays the ring rebuild, and concurrent requests block behind it on the rebuild mutex. For a ring of ~100 backends × ~150 virtual nodes that's a sort of 15,000 elements on the request path. If that latency spike turns out to matter, the alternative is rebuilding on the health-check goroutine instead — which trades the spike for the callback-under-lock problem this design was written to avoid.

## 7. PR review follow-up

GitHub Copilot's automated review on PR #7 produced one finding, on `NewRoundRobin`: it guarded against a nil `*TargetGroup` but not against a non-nil zero-valued one, so `&targetgroup.TargetGroup{}` constructed fine and then panicked on the first request -- on a request goroutine, with a stack trace pointing at `Next()` rather than at whoever built the group. Given that the surrounding code's stated posture is fail-loud-at-construction, the finding was valid.

Probing it before fixing turned up a second path Copilot had not flagged, and a worse one. Empirically, on a zero-value group:

```
Snapshot()   -> nil, then Next() panics: "invalid memory address or nil pointer dereference"
SetHealthy() -> returns changed=false and does NOT panic
```

The `SetHealthy` case is the health checker's path. It doesn't crash because reading from a nil map is legal Go, so an unbuilt group would swallow every probe result in silence — no panic, no log, backends never marked down. That is exactly the class of bug this hotfix was already fixing once (the severed `report` wire), arriving by a different route.

So the fix went into `internal/targetgroup` rather than only into the call site named in the review: both entry points panic with one shared `errNotBuilt` message, and `NewRoundRobin` calls `group.Snapshot()` at construction to trigger that check at the composition root. Three regression tests were added and confirmed to fail against the pre-fix source before passing against the fixed one.

Because the new guard sits on the hot read path, its cost was measured rather than assumed: 39.14 ns/op with it against 38.82 ns/op without, across 10 parallel goroutines — a difference inside run-to-run noise, as expected for a compare-and-branch on a value already in a register. The benchmark was throwaway and not committed.

**Method note for future phases:** the useful habit here was reproducing the reported failure first, in a throwaway test that printed what each entry point actually did, instead of going straight to the suggested patch. The report was accurate but described one symptom of a broader problem, and applying it literally would have left the silent-failure path in place. Automated review findings are evidence about where to look, not a specification of the fix.
