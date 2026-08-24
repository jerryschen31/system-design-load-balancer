# Phase 4b: Bounded, FIFO-fair wait queue for the concurrency limiter

- **Branch:** `phase-4a-timeouts-backpressure` (4b was built as a direct continuation of 4a, not a separate branch)
- **PR:** opened from this branch into `build`
- **Status:** complete

## 1. What was built

Phase 4a's `MaxConcurrent` middleware protected the load balancer from being overwhelmed, but at a cost: once every slot was full, *every* further request was rejected immediately and identically, regardless of whether a slot was about to free up in one second or ten. Phase 4b adds a bounded waiting line on top of that limiter, so a request that arrives just before a slot frees up can succeed instead of failing outright — while still keeping the total amount of work the load balancer will ever hold at once strictly bounded, which is the entire reason `MaxConcurrent` existed in the first place.

Concretely:
- `internal/middleware/concurrency.go` — `MaxConcurrent` gains two new parameters, `queueCapacity` and `queueWaitTimeout`. A request that arrives when every slot is taken now waits in a queue (bounded by `queueCapacity`, each wait capped at `queueWaitTimeout`) instead of being rejected on the spot — unless the queue itself is also full, in which case it's still rejected immediately, exactly as before. The queue is strictly first-come-first-served: whoever's been waiting longest always gets the next slot that frees up.
- `cmd/loadbalancer` — new `-queue-capacity` (default 20) and `-queue-wait-timeout` (default 2s) flags.

Deliberately out of scope: this queue only ever affects requests *at* the load balancer's own concurrency ceiling — it doesn't change backend selection, retries, or anything about phase 4a's backend-response timeout, which still applies independently to whatever request is actually running once it's admitted.

## 2. System Design concepts & considerations

- **The general problem: who gets a freed-up resource when several requests are waiting for it?** Once every slot is full, a newly arriving request has two honest options: give up immediately, or wait. If it waits, and several requests end up waiting at once, something has to decide who gets the next slot that opens up. There are two defensible answers — "whichever waiting request the computer happens to get to first" (no guarantee it's the one that's been waiting longest), or "whoever asked first, always" (a fair line). We chose the fair line:

  ```
   Slot 1: [in use]
   Slot 2: [in use]        <- both full, nothing free

   Waiting line, oldest at front:
     [ Request A ] -> [ Request B ] -> [ Request C ]
       waited longest                  just arrived

   When Slot 1 frees up:
     Request A is pulled from the FRONT and given the slot.
     B moves up to the front; C is still behind B.
  ```

- **Why not let a queued request wait forever?** Because that just relocates the exact problem `MaxConcurrent` was built to solve in phase 4a: instead of too many requests being actively handled at once, you'd have an unbounded number of requests parked waiting, each still holding a goroutine and an open client connection. Two separate caps are needed, not one: how many requests are allowed to wait at all (`queueCapacity`), and how long any one of them is allowed to wait before being told no (`queueWaitTimeout`). A queue without the second cap doesn't fail any faster than no queue at all — it just delays the same failure invisibly, after making the client wait through the whole thing with no signal anything was wrong.

- **The race between "you're being handed a slot" and "you're giving up."** This turned out to be the one genuinely hard part of building a fair (first-come-first-served) waiting line correctly, rather than a merely best-effort one. A request in the line can stop waiting two ways — it waited too long, or the client that sent it disconnected — and either can happen at almost exactly the same instant as the line handing that exact request the next available slot:

  ```
   [the line frees a slot, calls "Request A, you're up"]
                    |
                    |   <- could happen at the same instant
                    |
   [Request A separately decides "I've waited too long, I'm done"]
  ```

  Handled carelessly, this goes wrong in one of two ways: the slot is wasted forever (handed to someone who's already stopped listening, and nobody else ever gets it), or the same slot ends up double-counted. The fix: anyone about to give up must first check, at that exact moment, whether they were *just* handed a slot — and if so, since they're not going to use it, immediately pass it along to the next person in line rather than let it sit unused. That check and the "hand out the next slot" decision both have to happen under the same lock, so there's never a moment where two different parts of the system disagree about who currently holds a slot.

- **A subtlety in *testing* fairness, distinct from the algorithm itself: being granted something first is not the same as being *observed* to receive it first.** This came up directly while testing the FIFO ordering (see the design conversation below for the full story) — calling three waiting requests forward in the correct order tells you the order they were *called*, not the order in which each one will finish reacting and reporting back, if they're all free to run independently afterward. Proving strict ordering requires making sure, at each step, that only one of them is even capable of reporting in before the next one is called forward.

## 3. Design conversation summary

Phase 4a shipped with `MaxConcurrent` rejecting anything over its limit immediately, and its own notes named the sharpest weakness this created directly: a client arriving one second before a slot would free up was treated exactly the same as one arriving nine seconds before — both simply failed, because the limiter had no notion of "how close." That gap was demonstrated concretely with real numbers during phase 4a's own manual walkthrough (a client that would have succeeded in under a second was rejected instantly, identically to one that would have waited far longer), which made the case for phase 4b immediately obvious rather than theoretical.

Before writing any code, the two building blocks were laid out and confirmed: extend `MaxConcurrent` itself (rather than a separate type) with `queueCapacity` and `queueWaitTimeout` parameters, with `queueCapacity <= 0` preserving phase 4a's exact behavior — the same "0 disables it" convention `backendTimeout` already established. The one real fork in the road was whether the queue needed to guarantee strict first-come-first-served ordering, or whether a simpler best-effort version (reusing the same buffered-channel idiom `MaxConcurrent` already used) was good enough. Best-effort was flagged as meaningfully simpler; strict ordering was flagged as the real production-grade answer, at the cost of needing an actual ordered waiting list and the give-up/hand-off race described above. Asked explicitly rather than decided silently — strict FIFO was chosen.

Implementing the fair hand-off surfaced the give-up race directly: a first draft would have let a request that's timing out or whose client disconnected simply walk away, which on its own would occasionally lose a slot forever (handed to someone no longer listening, with no one else ever getting it back). The fix — re-checking under the same lock that grants slots, whether this exact request was just granted one in the moment it decided to leave — was designed before writing the code, once the race was described concretely rather than discovered by accident later.

The first version of the automated test proving strict ordering (three waiting requests, released one after another, checking the order they recorded their own admission) failed — not because the queue itself was unfair, but because the test asked a different, unguaranteed question. Calling three requests forward in the correct order (via three `release` calls in a row) only guarantees the order they were *called*; it says nothing about the order in which each one, now free to run independently, actually finishes executing its next few lines of code and reports back — that's a separate race the Go scheduler makes no promises about. The fix was to make the test wait for confirmation that each request had actually reported its admission before calling the next one forward, so at every point only one request was even capable of reporting in — closing the gap between "the algorithm decided the order" and "the test observed an order," which had been silently conflated in the first draft.

The manual walkthrough (real `echobackend` and `loadbalancer` processes on localhost) confirmed all three possible outcomes for a queued request end to end: a near-miss request queued for ~0.7s and then succeeded once a slot freed; a request that waited the full `queue-wait-timeout` (3s) and was rejected with a distinct "timed out waiting for a free slot" message; and an overflow request arriving once both the slots and the queue itself were completely full, rejected in under a millisecond with the original "too many concurrent requests" message — plus confirmation that phase 4a's independent backend-response timeout still fired correctly for requests that *did* get admitted, showing the two mechanisms compose correctly rather than interfering with each other.

## 4. Code changes

- `internal/middleware/concurrency.go` — `limiter` type (`cur`, `size`, `queueCap`, a `container/list.List` of waiters, one `sync.Mutex` guarding all of it); `acquire` (grants immediately if a slot is free, otherwise joins the queue if there's room, otherwise reports the queue is full); `release` (hands a freed slot directly to the front of the queue if anyone's waiting, otherwise just frees it); `giveUp` (the race-safe path for a queued request that times out or whose client disconnects); `MaxConcurrent`'s signature grew from `(n int)` to `(n int, queueCapacity int, queueWaitTimeout time.Duration)`, with new panics for a negative `queueCapacity` and for a positive `queueCapacity` paired with a non-positive `queueWaitTimeout`.
- `internal/middleware/concurrency_test.go` — the phase 4a boundary test updated to the new signature (`queueCapacity=0`, preserving its original meaning); new tests: strict-FIFO admission order (algorithm-level, sidestepping the scheduler-ordering pitfall described above), a queued request's slot freeing up when its client disconnects, a queued request being admitted after waiting, a queued request timing out with the distinct error message, a full queue rejecting immediately without waiting any of `queueWaitTimeout`, and panic tests for the two new invalid-parameter cases.
- `internal/middleware/logging.go` — no change this phase (already updated in 4a).
- `cmd/loadbalancer/main.go` — new `-queue-capacity` and `-queue-wait-timeout` flags, threaded into `middleware.MaxConcurrent`.
- `cmd/loadbalancer/integration_test.go` — existing `MaxConcurrent` call site updated to the new signature (queueing disabled, preserving that test's original intent); new `TestIntegration_QueueAdmitsNearMissRequest`, proving the near-miss scenario succeeds through the real composed handler chain (`Logging` → `MaxConcurrent` → `proxy` → a real backend), not just the middleware in isolation.

## 5. Testing

### Functional tests
- `internal/middleware`: a queued request is admitted once a slot frees up within `queueWaitTimeout`; a queued request that waits the full timeout without a slot freeing gets a distinct 503 body; once both the slots and the queue are full, a further request is rejected immediately (not made to wait any of `queueWaitTimeout`); construction panics on a negative queue capacity or on a positive queue capacity paired with a non-positive wait timeout.
- `cmd/loadbalancer`: `TestIntegration_QueueAdmitsNearMissRequest` proves the same near-miss success case through the real, fully composed handler chain against a real backend, not just the isolated middleware.

### Non-functional / stress tests
- **Strict FIFO admission order** (`TestLimiterAcquireIsFIFO`, `internal/middleware`) — three waiters join a queue of capacity 3 strictly in order, confirmed enqueued one at a time; the sole slot is then released three times, waiting for confirmation of each admission before releasing again (see the design conversation above for why that synchronization is required for the test to actually prove what it claims). This test failed once during development due to a race in the *test's* synchronization, not the algorithm — see above and the code comment at `concurrency_test.go`'s `TestLimiterAcquireIsFIFO` for the full explanation. Reran 10x under `-race` after the fix with no failures.
- **Client disconnect while queued frees the slot** (`TestLimiterGiveUpOnClientCancelFreesQueueSlot`, `internal/middleware`) — a queued request's context is cancelled mid-wait; confirms the queue's length drops back to zero, i.e. the abandoned request's spot doesn't stay occupied forever.
- **Manual walkthrough** (outside the automated suite; real `echobackend` + `loadbalancer` processes on localhost) — confirmed all three queue outcomes plus their interaction with phase 4a's backend timeout: (1) a near-miss request (fired 0.3s into two 1s-long holders) queued for ~0.68s and succeeded; (2) with holders sleeping 5s against a 3s `queue-wait-timeout`, a queued request was rejected at exactly 3.0s with "timed out waiting for a free slot," while the two holders themselves were separately cut off at 5.0s by the independent backend-response timeout with a 502 — demonstrating the two mechanisms operate correctly side by side; (3) once both the concurrency slots and the queue itself were full, an overflow request was rejected in under a millisecond with the original "too many concurrent requests" message, never waiting any part of the queue timeout.
- Phases 1-4a's existing functional and stress tests (slow/unreachable backends, health-check skip/recovery/flapping, backend-timeout behavior, immediate-rejection-with-no-queue) still pass unchanged.

## 6. Known weaknesses / open questions

- **The queue is a single global waiting line, not per-backend or per-route.** A burst of requests destined for one particular backend can fill the entire queue and cause requests destined for other, perfectly healthy backends to wait or be rejected too. Same shape as `MaxConcurrent`'s own known weakness from phase 4a (a single global limit, not per-backend); a natural joint follow-up once weighted or per-backend routing exists.
- **No visibility into current queue depth or slot utilization from outside the process.** An operator watching this load balancer in production would want a metric like "current queue length" or "time spent waiting" surfaced (logs, metrics endpoint) rather than only discovering queueing behavior indirectly through response latency. Deferred; ties into the broader "no observability/metrics" gap that hasn't been addressed in any phase yet.
- **`queueWaitTimeout` is a single fixed value for every request**, regardless of what kind of request it is or how long it's realistically likely to need to wait. A more adaptive design (e.g., a shorter timeout for read-only requests that can be retried elsewhere, a longer one for something that must eventually succeed) is a plausible refinement, not addressed here.
- **Still no automatic retry against a different backend, no rate limiting, no passive health checking / circuit breaker** — all carried over from phase 4a, unchanged.
- **Still no live backend membership changes, no TLS** — carried over from phases 1-3, unchanged.
