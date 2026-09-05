# Hotfix Test Report: Extract `TargetGroup` from the balancer, and repair the health-check → routing wire

**Source:** `hotfix/create-targetgroup`, branched from `build` at `a54b0a5`
**Command:** `go test -v -race -count=1 ./cmd/... ./internal/...`
**Result:** 67 passed, 0 failed, across 7 packages

| Package | Result | Duration |
|---|---|---|
| `cmd/echobackend` | ok | 1.18s |
| `cmd/loadbalancer` | ok | 2.19s |
| `internal/balancer` | ok | 1.61s |
| `internal/healthcheck` | ok | 2.70s |
| `internal/middleware` | ok | 8.05s |
| `internal/proxy` | ok | 3.20s |
| `internal/targetgroup` | ok | 2.10s |

Scoped to `./cmd/... ./internal/...` to stay consistent with the phase 4a/4b reports. (There is no `scratch/` directory on this branch, so `./...` would have worked equally well here.)

Generated on 2026-09-05 directly from the working branch.

## What changed

Health bookkeeping — which backends are currently up — was extracted out of `internal/balancer` into a new leaf package, `internal/targetgroup`. `RoundRobin` went from five fields to two (`group` and `counter`), and `roundrobin.go` shrank from 161 lines to 110. The health checker and the balancer no longer reference each other at all: the checker writes health into a shared `TargetGroup`, the balancer reads routing decisions out of it, and `main()` is the only place that knows both exist.

A `Generation` counter was added to the published snapshot so a later phase (consistent hashing) can cache an expensive derived structure and rebuild it only when membership actually changes, without any further change to `internal/targetgroup`.

## Regression evidence: the health-check wire was severed on `build`

This branch also repairs a real, traffic-affecting bug that was live on `build`. `healthcheck.Checker.report` logged each probe result but never called `c.reporter.SetHealthy`, so probe results never reached routing and unhealthy backends kept receiving traffic. `grep` for `c.reporter` in `internal/healthcheck/healthcheck.go` at `a54b0a5` returns nothing.

The existing integration test was already catching this. Run against `build` before the fix:

```
=== RUN   TestIntegration_ChecksRouteAroundUnhealthyBackend
    integration_test.go:102: input: 2 backends, one (http://127.0.0.1:50360) already unhealthy before the checker ever runs
    integration_test.go:121: output: good backend got 15 requests, unhealthy backend got 15
    integration_test.go:123: unhealthy backend received 15 requests, want 0 -- active health checking should have excluded it before traffic arrived
--- FAIL: TestIntegration_ChecksRouteAroundUnhealthyBackend (0.12s)
FAIL
FAIL	github.com/jerryschen31/system-design-load-balancer/cmd/loadbalancer	0.318s
FAIL
```

The unhealthy backend received exactly half the traffic — the round-robin cycle was never told to skip it. The same test passes on this branch (see the full output below).

## New tests

Eight tests were added to the new package, and two to `internal/balancer`. Three of them exist to pin down invariants that are new with this design and would otherwise be silent if broken:

- **`TestGenerationAdvancesOnlyOnRealChange`** — the contract consistent hashing will depend on. Steps through five `SetHealthy` calls (no-op, real flip down, repeat, second flip down, flip up) asserting both the `changed` return and the generation after each one. A missed bump would mean a permanently stale hash ring; a spurious bump would mean rebuilding thousands of hash positions for nothing.
- **`TestPublishedSnapshotIsNotMutatedInPlace`** — the invariant that makes lock-free reads safe. A reader holds a snapshot, a health check publishes a new one, and the held value must be completely unchanged. Without this, a request goroutine could load a 3-backend snapshot, get preempted, and return to find the slice it was indexing is now 2 elements long — an out-of-range panic on the request path.
- **`TestRoundRobinNextDuringHealthFlapping`** — the adversarial version of the same hazard, at the balancer level. 20 goroutines call `Next()` while all 3 backends flap health continuously for 100ms. It hunts specifically for an index-out-of-range panic from reading the healthy list twice (once for `len()`, once for the index) with a health check landing in between. `Next()` loading one `Snapshot` and reusing it is what prevents that.

`TestConcurrentSetHealthyAndSnapshot` is the race-detector test for the group itself: 3 flapping writers against 20 readers. It completed **277,641 snapshot reads** against constant flapping in 100ms with every read internally consistent and no race reported.

## Weaknesses and deferrals

Carried forward from earlier phases, unchanged by this work:

- **Membership is fixed at construction.** Backends can flip healthy/unhealthy but cannot join or leave a `TargetGroup`. Real service discovery would change what has to be locked in `SetHealthy` — currently `all` is never written after construction, which is what allows the rebuild loop to read it without further synchronization.
- **No flap damping.** A single failed probe marks a backend down and a single successful one brings it back. Production health checkers use N-consecutive-failures / M-consecutive-successes thresholds to avoid routing churn from one blip. `TestStress_ConcurrentTrafficSurvivesHealthFlapping` demonstrates the system survives flapping, not that it damps it.
- **No slow start on recovery.** A backend that comes back gets its full share of traffic on the very next request. `TestStress_RecoveredBackendImmediatelyGetsFullShare` asserts this current behaviour deliberately, as a marker for the phase that adds ramping.

New with this design, and accepted:

- **`Snapshot.Healthy` is a mutable slice that callers must not write to.** Go cannot express an immutable slice in the type system, and returning a defensive copy would allocate on every request — exactly the cost the atomic-pointer design exists to avoid. This is enforced by convention plus the package boundary (only `internal/targetgroup` constructs a `Snapshot`). Worth noting explicitly: **a caller writing into `Healthy` is not a data race, so `-race` will not catch it.** It is a legal write to shared memory that simply corrupts every other reader's view.
- **Round-robin fairness across a changing healthy set remains approximate.** The counter is a source of unique increasing numbers modded against whatever the healthy list is right now, so a health flip shifts which backend a given counter value maps to. Unchanged from phase 2, and still the accepted trade-off: the goal is "skip known-dead backends," not perfect fairness across a moving set.

## Full output

```
=== RUN   TestHandlerReportsOwnAddress
    main_test.go:22: input: GET /widgets against a handler configured with addr=:9001
    main_test.go:28: output: response body = "echobackend :9001 handled GET /widgets\n"
--- PASS: TestHandlerReportsOwnAddress (0.00s)
=== RUN   TestHealthHandlerReflectsState
    main_test.go:39: input: GET /health while healthy=true, then again after flipping to healthy=false
    main_test.go:43: step: healthy=true -> status 200
    main_test.go:51: output: healthy=false -> status 503
--- PASS: TestHealthHandlerReflectsState (0.00s)
=== RUN   TestHealthHandlerLogsRequest
    main_test.go:67: input: GET /health against a handler with a real (non-discard) logger
    main_test.go:72: output: logged "GET /health healthy=true\n"
--- PASS: TestHealthHandlerLogsRequest (0.00s)
=== RUN   TestToggleHandlerFlipsHealthAndRejectsGet
    main_test.go:83: input: POST /health/toggle twice, then GET /health/toggle once
    main_test.go:87: step: after 1st POST, healthy=false (body "healthy=false\n")
    main_test.go:94: step: after 2nd POST, healthy=true
    main_test.go:101: output: GET /health/toggle -> status 405
--- PASS: TestToggleHandlerFlipsHealthAndRejectsGet (0.00s)
=== RUN   TestParseSleepSeconds
=== RUN   TestParseSleepSeconds/empty_uses_default
    main_test.go:126: input: seconds=""
    main_test.go:128: output: got=2 err=<nil>
=== RUN   TestParseSleepSeconds/explicit_value
    main_test.go:126: input: seconds="5"
    main_test.go:128: output: got=5 err=<nil>
=== RUN   TestParseSleepSeconds/zero_is_valid
    main_test.go:126: input: seconds="0"
    main_test.go:128: output: got=0 err=<nil>
=== RUN   TestParseSleepSeconds/over_cap_is_clamped
    main_test.go:126: input: seconds="9999"
    main_test.go:128: output: got=30 err=<nil>
=== RUN   TestParseSleepSeconds/negative_is_an_error
    main_test.go:126: input: seconds="-1"
    main_test.go:128: output: got=0 err=invalid seconds="-1": must be a non-negative integer
=== RUN   TestParseSleepSeconds/non-numeric_is_an_error
    main_test.go:126: input: seconds="soon"
    main_test.go:128: output: got=0 err=invalid seconds="soon": must be a non-negative integer
--- PASS: TestParseSleepSeconds (0.00s)
    --- PASS: TestParseSleepSeconds/empty_uses_default (0.00s)
    --- PASS: TestParseSleepSeconds/explicit_value (0.00s)
    --- PASS: TestParseSleepSeconds/zero_is_valid (0.00s)
    --- PASS: TestParseSleepSeconds/over_cap_is_clamped (0.00s)
    --- PASS: TestParseSleepSeconds/negative_is_an_error (0.00s)
    --- PASS: TestParseSleepSeconds/non-numeric_is_an_error (0.00s)
=== RUN   TestSleepHandlerRespondsAfterElapsed
    main_test.go:148: input: GET /sleep?seconds=0 -- should return 200 essentially immediately
    main_test.go:156: output: status=200 body="slept 0s\n" elapsed=15.958µs
--- PASS: TestSleepHandlerRespondsAfterElapsed (0.00s)
=== RUN   TestSleepHandlerRejectsInvalidSeconds
    main_test.go:168: input: GET /sleep?seconds=nope
    main_test.go:174: output: status=400 body="invalid seconds=\"nope\": must be a non-negative integer\n"
--- PASS: TestSleepHandlerRejectsInvalidSeconds (0.00s)
=== RUN   TestSleepHandlerCancelledByContext
    main_test.go:196: input: GET /sleep?seconds=60 with an already-cancelled context
    main_test.go:207: output: handler returned promptly on the cancellation path; body="" (empty means it never reached the normal-completion branch)
--- PASS: TestSleepHandlerCancelledByContext (0.00s)
PASS
ok  	github.com/jerryschen31/system-design-load-balancer/cmd/echobackend	1.181s
=== RUN   TestIntegration_ChecksRouteAroundUnhealthyBackend
    integration_test.go:104: input: 2 backends, one (http://127.0.0.1:49683) already unhealthy before the checker ever runs
    integration_test.go:124: output: good backend got 30 requests, unhealthy backend got 0
--- PASS: TestIntegration_ChecksRouteAroundUnhealthyBackend (0.11s)
=== RUN   TestStress_RecoveredBackendImmediatelyGetsFullShare
    integration_test.go:154: input: 3 backends; backend 0 (http://127.0.0.1:49732) starts unhealthy, 1 and 2 start healthy
    integration_test.go:193: step: before recovery, backend 0 got 0 of 30 requests
    integration_test.go:199: step: backend 0 flipped to healthy
    integration_test.go:202: step: checker confirmed backend 0 healthy again
    integration_test.go:211: output: on the very first burst after recovery was detected, backend 0 received 30 of 90 requests (an even share is 30)
    integration_test.go:215: KNOWN WEAKNESS: a recovered backend receives its full concurrent traffic share the instant it's marked healthy, with no gradual ramp-up. A backend still warming up after recovery (cold cache, JIT warmup, reconnecting to a database) can be knocked back down immediately.
--- PASS: TestStress_RecoveredBackendImmediatelyGetsFullShare (0.11s)
=== RUN   TestStress_ConcurrentTrafficSurvivesHealthFlapping
    integration_test.go:230: input: 2 backends, one flips healthy/unhealthy every 10ms while traffic runs
    integration_test.go:288: output: status distribution across 200 requests during flapping: map[200:200]
--- PASS: TestStress_ConcurrentTrafficSurvivesHealthFlapping (0.02s)
=== RUN   TestIntegration_MaxConcurrentProtectsFullStack
    integration_test.go:318: input: maxConcurrent=4, one backend whose handler blocks until released
    integration_test.go:350: step: 4 requests confirmed in flight at the real backend, through the full stack
    integration_test.go:378: output: 10-request burst while at capacity -> status counts map[503:10]
    integration_test.go:385: output: the 4 originally-admitted requests all completed with statuses [200 200 200 200]
--- PASS: TestIntegration_MaxConcurrentProtectsFullStack (0.00s)
=== RUN   TestIntegration_QueueAdmitsNearMissRequest
    integration_test.go:411: input: maxConcurrent=2, each holder finishes in ~150ms; queueCapacity=1, queueWaitTimeout=5s (well above holderDelay)
    integration_test.go:453: output: near-miss request -> status=200, waited 287.927834ms (holders took 308.966792ms total)
--- PASS: TestIntegration_QueueAdmitsNearMissRequest (0.31s)
=== RUN   TestParseBackendURL
    main_test.go:6: input: backend URL http://localhost:9000
    main_test.go:17: output: scheme="http" host="localhost:9000"
--- PASS: TestParseBackendURL (0.00s)
=== RUN   TestParseBackendURLRejectsMissingHost
    main_test.go:21: input: backend URL localhost:9000
    main_test.go:26: output: backend URL must include a host (for example http://hostname:port)
--- PASS: TestParseBackendURLRejectsMissingHost (0.00s)
=== RUN   TestParseBackendURLRejectsUnsupportedScheme
    main_test.go:30: input: backend URL tcp://localhost:9000
    main_test.go:35: output: backend URL scheme must be http or https
--- PASS: TestParseBackendURLRejectsUnsupportedScheme (0.00s)
=== RUN   TestParseBackendURLsPreservesOrder
    main_test.go:40: input: repeated -backend flags in order [http://localhost:9001 http://localhost:9002 http://localhost:9003]
    main_test.go:54: output: [http://localhost:9001 http://localhost:9002 http://localhost:9003]
--- PASS: TestParseBackendURLsPreservesOrder (0.00s)
=== RUN   TestParseBackendURLsRejectsEmpty
    main_test.go:58: input: no -backend flags given
    main_test.go:63: output: at least one -backend is required
--- PASS: TestParseBackendURLsRejectsEmpty (0.00s)
=== RUN   TestParseBackendURLsRejectsAnyInvalidEntry
    main_test.go:68: input: [http://localhost:9001 not-a-valid-backend] (second entry has no scheme/host)
    main_test.go:74: output: invalid backend URL "not-a-valid-backend": backend URL must include a host (for example http://hostname:port)
--- PASS: TestParseBackendURLsRejectsAnyInvalidEntry (0.00s)
PASS
ok  	github.com/jerryschen31/system-design-load-balancer/cmd/loadbalancer	2.185s
=== RUN   TestRoundRobinCyclesInOrder
    roundrobin_test.go:38: input: 3 backends, calling Next() 7 times in a row
    roundrobin_test.go:44: call 0: got http://backend-a (want http://backend-a)
    roundrobin_test.go:44: call 1: got http://backend-b (want http://backend-b)
    roundrobin_test.go:44: call 2: got http://backend-c (want http://backend-c)
    roundrobin_test.go:44: call 3: got http://backend-a (want http://backend-a)
    roundrobin_test.go:44: call 4: got http://backend-b (want http://backend-b)
    roundrobin_test.go:44: call 5: got http://backend-c (want http://backend-c)
    roundrobin_test.go:44: call 6: got http://backend-a (want http://backend-a)
    roundrobin_test.go:49: output: cycle wrapped correctly past the end of the backend list twice
--- PASS: TestRoundRobinCyclesInOrder (0.00s)
=== RUN   TestRoundRobinSkipsUnhealthyBackend
    roundrobin_test.go:63: input: 3 healthy backends, then backend-b is marked unhealthy on the group
    roundrobin_test.go:73: output: 6 calls to Next() never returned the unhealthy backend
    roundrobin_test.go:80: step: backend-b marked healthy again; backends reached: map[http://backend-a:true http://backend-b:true http://backend-c:true]
--- PASS: TestRoundRobinSkipsUnhealthyBackend (0.00s)
=== RUN   TestRoundRobinAllUnhealthyReturnsNil
    roundrobin_test.go:95: input: 2 backends, both marked unhealthy on the group
    roundrobin_test.go:101: output: Next() returned <nil>
--- PASS: TestRoundRobinAllUnhealthyReturnsNil (0.00s)
=== RUN   TestNewRoundRobinPanicsOnNilGroup
    roundrobin_test.go:111: input: NewRoundRobin called with a nil target group
    roundrobin_test.go:114: output: recovered panic = balancer: NewRoundRobin requires a non-nil target group
--- PASS: TestNewRoundRobinPanicsOnNilGroup (0.00s)
=== RUN   TestRoundRobinConcurrentCallsStayBalanced
    roundrobin_test.go:142: input: 50 goroutines x 60 calls each = 3000 total calls across 3 backends
    roundrobin_test.go:164: distribution: map[http://backend-a:1000 http://backend-b:1000 http://backend-c:1000]
    roundrobin_test.go:173: output: every backend received exactly 1000 calls, confirming no update was lost across 50 concurrent goroutines
--- PASS: TestRoundRobinConcurrentCallsStayBalanced (0.00s)
=== RUN   TestRoundRobinNextDuringHealthFlapping
    roundrobin_test.go:192: input: 3 backends, health flapping on the group while 20 goroutines call Next() for 100ms
    roundrobin_test.go:244: output: survived constant flapping without panicking; 0 calls legitimately found no healthy backend
    roundrobin_test.go:245: output: no data race reported (run with -race to make this test meaningful)
--- PASS: TestRoundRobinNextDuringHealthFlapping (0.12s)
PASS
ok  	github.com/jerryschen31/system-design-load-balancer/internal/balancer	1.612s
=== RUN   TestCheckerDetectsHealthyBackend
    healthcheck_test.go:48: input: backend at http://127.0.0.1:49739 answers /health with 200
    healthcheck_test.go:60: output: first check reported healthy=true
--- PASS: TestCheckerDetectsHealthyBackend (0.00s)
=== RUN   TestCheckerDetectsUnhealthyStatus
    healthcheck_test.go:71: input: backend at http://127.0.0.1:49741 answers /health with 500
    healthcheck_test.go:83: output: first check reported healthy=false
--- PASS: TestCheckerDetectsUnhealthyStatus (0.00s)
=== RUN   TestCheckerDetectsTimeout
    healthcheck_test.go:98: input: backend at http://127.0.0.1:49743 sleeps 500ms before answering; checker timeout is 50ms
    healthcheck_test.go:110: output: first check reported healthy=false
--- PASS: TestCheckerDetectsTimeout (0.50s)
=== RUN   TestCheckerDetectsRecovery
    healthcheck_test.go:130: input: backend starts unhealthy (503), flips to healthy (200) partway through
    healthcheck_test.go:142: step: first check reported healthy=false
    healthcheck_test.go:148: step: backend flipped to answer 200
    healthcheck_test.go:155: output: a later check reported healthy=true, confirming recovery was detected
--- PASS: TestCheckerDetectsRecovery (0.03s)
=== RUN   TestCheckerStopsAfterContextCancel
    healthcheck_test.go:182: step: cancelled context after 10 checks
    healthcheck_test.go:186: output: 0 checks happened in the 200ms after cancellation
--- PASS: TestCheckerStopsAfterContextCancel (0.40s)
=== RUN   TestNewCheckerPanicsOnNonPositiveInterval
    healthcheck_test.go:203: input: NewChecker with interval=0
    healthcheck_test.go:210: output: NewChecker panicked as expected: healthcheck: NewChecker requires a positive interval
--- PASS: TestNewCheckerPanicsOnNonPositiveInterval (0.00s)
=== RUN   TestNewCheckerPanicsOnNonPositiveTimeout
    healthcheck_test.go:218: input: NewChecker with timeout=-1s
    healthcheck_test.go:225: output: NewChecker panicked as expected: healthcheck: NewChecker requires a positive timeout
--- PASS: TestNewCheckerPanicsOnNonPositiveTimeout (0.00s)
=== RUN   TestNewCheckerNormalizesPathWithoutLeadingSlash
    healthcheck_test.go:244: input: NewChecker path="health" (no leading slash) against a backend serving /health
    healthcheck_test.go:256: output: first check reported healthy=true
--- PASS: TestNewCheckerNormalizesPathWithoutLeadingSlash (0.00s)
PASS
ok  	github.com/jerryschen31/system-design-load-balancer/internal/healthcheck	2.695s
=== RUN   TestMaxConcurrentAllowsExactlyNInFlight
    concurrency_test.go:48: input: MaxConcurrent(3, 0, 0) wrapping a handler that blocks until released
    concurrency_test.go:68: step: 3 requests confirmed in flight (holding every semaphore slot)
    concurrency_test.go:87: step: 2 additional requests while at capacity -> statuses [503 503]
    concurrency_test.go:96: output: the 3 originally-admitted requests all completed with statuses [200 200 200]
--- PASS: TestMaxConcurrentAllowsExactlyNInFlight (0.00s)
=== RUN   TestLimiterAcquireIsFIFO
    concurrency_test.go:116: input: 1 slot (held), 3 waiters joining the queue strictly in order 0, 1, 2
    concurrency_test.go:130: step: all 3 waiters confirmed enqueued (queue length reached 3)
    concurrency_test.go:149: output: admission order was [0 1 2], want [0 1 2]
--- PASS: TestLimiterAcquireIsFIFO (0.00s)
=== RUN   TestLimiterGiveUpOnClientCancelFreesQueueSlot
    concurrency_test.go:168: input: 1 slot (held), queue capacity 1
    concurrency_test.go:176: step: waiter confirmed enqueued
    concurrency_test.go:180: output: cancelled waiter's acquire returned 3, want acquireClientGaveUp
--- PASS: TestLimiterGiveUpOnClientCancelFreesQueueSlot (0.00s)
=== RUN   TestLimiterReleaseGiveUpRaceDoesNotLeakSlots
    concurrency_test.go:203: input: 5000 iterations of release() raced against a waiter cancelling at the same instant
    concurrency_test.go:234: output: 5000 iterations resolved cleanly, final acquire still succeeded -- no leaked slot
--- PASS: TestLimiterReleaseGiveUpRaceDoesNotLeakSlots (5.93s)
=== RUN   TestMaxConcurrentQueueAdmitsAfterWait
    concurrency_test.go:255: input: 1 slot (held), queue capacity 1, queueWaitTimeout 2s; a second request arrives while the first is in flight
    concurrency_test.go:269: step: holder confirmed in flight, holding the only slot
    concurrency_test.go:294: output: holder status=200, queued (waited then admitted) status=200
--- PASS: TestMaxConcurrentQueueAdmitsAfterWait (0.05s)
=== RUN   TestMaxConcurrentQueueTimesOut
    concurrency_test.go:322: input: 1 slot held indefinitely, queueWaitTimeout=100ms, a second request that will never be released in time
    concurrency_test.go:326: step: holder confirmed in flight, holding the only slot
    concurrency_test.go:345: output: status=503 body="timed out waiting for a free slot\n" elapsed=102.320375ms
--- PASS: TestMaxConcurrentQueueTimesOut (0.11s)
=== RUN   TestMaxConcurrentQueueFullRejectsImmediately
    concurrency_test.go:379: input: 1 slot held, queue capacity 1 also held, a 3rd (overflow) request arrives
    concurrency_test.go:388: step: 1st request confirmed holding the only slot
    concurrency_test.go:402: step: 2nd request given time to join the only queue slot
    concurrency_test.go:416: output: overflow request -> status=503 body="too many concurrent requests\n" elapsed=1.176291ms
--- PASS: TestMaxConcurrentQueueFullRejectsImmediately (0.05s)
=== RUN   TestMaxConcurrentPanicsOnNonPositiveN
    concurrency_test.go:430: input: MaxConcurrent(0, 0, 0)
    concurrency_test.go:436: output: panicked as expected: middleware: MaxConcurrent requires a positive n
--- PASS: TestMaxConcurrentPanicsOnNonPositiveN (0.00s)
=== RUN   TestMaxConcurrentPanicsOnNegativeQueueCapacity
    concurrency_test.go:442: input: MaxConcurrent(1, -1, 0)
    concurrency_test.go:448: output: panicked as expected: middleware: MaxConcurrent requires a non-negative queueCapacity
--- PASS: TestMaxConcurrentPanicsOnNegativeQueueCapacity (0.00s)
=== RUN   TestMaxConcurrentPanicsOnQueueWithoutWaitTimeout
    concurrency_test.go:454: input: MaxConcurrent(1, 5, 0) -- a real queue capacity but no positive wait timeout
    concurrency_test.go:460: output: panicked as expected: middleware: MaxConcurrent requires a positive queueWaitTimeout when queueCapacity > 0
--- PASS: TestMaxConcurrentPanicsOnQueueWithoutWaitTimeout (0.00s)
=== RUN   TestLoggingAllowsNilLogger
    logging_test.go:10: input: middleware.Logging(nil) wrapping a handler that returns 204
    logging_test.go:12: step: inner handler runs and writes HTTP 204
    logging_test.go:24: output: request completed with status 204 and no panic
--- PASS: TestLoggingAllowsNilLogger (0.00s)
PASS
ok  	github.com/jerryschen31/system-design-load-balancer/internal/middleware	8.048s
=== RUN   TestForwardsRequestToBackend
    proxy_test.go:63: input: client sends POST /widgets to load balancer http://127.0.0.1:50105; backend is http://127.0.0.1:50104
    proxy_test.go:53: backend step: received POST /widgets
    proxy_test.go:84: output: client got status 418 and body "hello from backend"
--- PASS: TestForwardsRequestToBackend (0.00s)
=== RUN   TestClientCannotSpoofForwardedFor
    proxy_test.go:101: input: client sends X-Forwarded-For="6.6.6.6" to http://127.0.0.1:50109
    proxy_test.go:90: backend step: saw X-Forwarded-For="127.0.0.1"
    proxy_test.go:115: output: backend received rewritten X-Forwarded-For="127.0.0.1"
--- PASS: TestClientCannotSpoofForwardedFor (0.00s)
=== RUN   TestHopByHopHeaderNotForwarded
    proxy_test.go:136: input: client sends Connection="Keep-Alive" and Keep-Alive="timeout=5"
    proxy_test.go:122: backend step: Connection="" Keep-Alive present=false
    proxy_test.go:150: output: backend saw Connection="" Keep-Alive present=false
--- PASS: TestHopByHopHeaderNotForwarded (0.00s)
=== RUN   TestBackendDownReturns502
    proxy_test.go:158: input: backend http://127.0.0.1:1 is down; client sends GET / through load balancer http://127.0.0.1:50116
    proxy_test.go:169: output: client got status 502
--- PASS: TestBackendDownReturns502 (0.00s)
=== RUN   TestNewAllowsNilLogger
    proxy_test.go:176: input: proxy.New(http://127.0.0.1:1, 0, nil)
    proxy_test.go:187: output: client got status 502 and the proxy did not panic
--- PASS: TestNewAllowsNilLogger (0.00s)
=== RUN   TestNoHealthyBackendsReturns502
    proxy_test.go:202: input: balancer's only backend (http://127.0.0.1:1) marked unhealthy
    proxy_test.go:218: output: client got status 502, body "no healthy backends available\n"
--- PASS: TestNoHealthyBackendsReturns502 (0.00s)
=== RUN   TestNewPanicsOnNilBalancer
    proxy_test.go:233: input: proxy.New(nil, 0, nil)
    proxy_test.go:240: output: New panicked as expected: proxy: New requires a non-nil Balancer
--- PASS: TestNewPanicsOnNilBalancer (0.00s)
=== RUN   TestBackendTimeoutReturns502
    proxy_test.go:262: input: backend sleeps 500ms, proxy backendTimeout is 50ms, client has no timeout of its own
    proxy_test.go:273: output: status=502 body="backend request timed out\n" elapsed=51.446208ms
--- PASS: TestBackendTimeoutReturns502 (0.50s)
=== RUN   TestBackendTimeoutAllowsSlowStreamedBodyWithinBudget
    proxy_test.go:313: input: backend streams 5 chunks, 20ms apart, total ~100ms; backendTimeout is 2s (well above that)
    proxy_test.go:326: output: status=200 body="chunk-0 chunk-1 chunk-2 chunk-3 chunk-4 "
--- PASS: TestBackendTimeoutAllowsSlowStreamedBodyWithinBudget (0.11s)
=== RUN   TestRewriteLogsRoutingDecision
    proxy_test.go:345: input: GET / through the load balancer, backend is http://127.0.0.1:50138
    proxy_test.go:354: output: logged "routed GET / -> http://127.0.0.1:50138\n"
--- PASS: TestRewriteLogsRoutingDecision (0.00s)
=== RUN   TestStress_DisabledTimeoutWaitsIndefinitely
    stress_test.go:21: input: backendTimeout=0 (disabled); backend sleeps for 300ms; client timeout is 50ms
    stress_test.go:24: backend step: request reached backend; backend is now sleeping
    stress_test.go:44: output: client returned after 51.395917ms with error Get "http://127.0.0.1:50143/": context deadline exceeded (Client.Timeout exceeded while awaiting headers) -- opt-out confirmed working
--- PASS: TestStress_DisabledTimeoutWaitsIndefinitely (0.30s)
=== RUN   TestStress_ConcurrentRequestsHandledConcurrently
    stress_test.go:53: input: 50 concurrent requests; backend delay per request is 100ms
    stress_test.go:96: output: burst finished in 120.256917ms (serial time would have been 5s)
--- PASS: TestStress_ConcurrentRequestsHandledConcurrently (0.12s)
=== RUN   TestStress_BurstAgainstUnreachableBackend
    stress_test.go:105: input: 50 concurrent requests against a load balancer whose only backend is down
    stress_test.go:133: output: every observed response was 502
--- PASS: TestStress_BurstAgainstUnreachableBackend (0.01s)
PASS
ok  	github.com/jerryschen31/system-design-load-balancer/internal/proxy	3.195s
=== RUN   TestNewStartsAllBackendsHealthy
    targetgroup_test.go:37: input: 2 fresh backends, no SetHealthy calls yet
    targetgroup_test.go:41: output: snapshot healthy=[http://backend-a http://backend-b] generation=1
--- PASS: TestNewStartsAllBackendsHealthy (0.00s)
=== RUN   TestNewCopiesBackends
    targetgroup_test.go:59: input: construct a group over [http://backend-a http://backend-b], then mutate the caller's slice
    targetgroup_test.go:66: step: caller's original slice[0] is now http://attacker-controlled
    targetgroup_test.go:69: output: snapshot healthy=[http://backend-a http://backend-b]
--- PASS: TestNewCopiesBackends (0.00s)
=== RUN   TestNewPanicsOnEmptyBackends
    targetgroup_test.go:79: input: New called with an empty backend list
    targetgroup_test.go:82: output: recovered panic = targetgroup: New requires at least one backend
--- PASS: TestNewPanicsOnEmptyBackends (0.00s)
=== RUN   TestSetHealthyRemovesAndRestoresBackend
    targetgroup_test.go:99: input: 3 healthy backends [http://backend-a http://backend-b http://backend-c]
    targetgroup_test.go:103: step: SetHealthy(backend-b, false) -> healthy=[http://backend-a http://backend-c] generation=2
    targetgroup_test.go:115: output: SetHealthy(backend-b, true) -> healthy=[http://backend-a http://backend-b http://backend-c] generation=3
--- PASS: TestSetHealthyRemovesAndRestoresBackend (0.00s)
=== RUN   TestSetHealthyAllUnhealthyLeavesEmptySnapshot
    targetgroup_test.go:134: input: 2 backends, both then marked unhealthy
    targetgroup_test.go:140: output: healthy=[] (len 0) generation=3
--- PASS: TestSetHealthyAllUnhealthyLeavesEmptySnapshot (0.00s)
=== RUN   TestSetHealthyIgnoresUnknownBackend
    targetgroup_test.go:153: input: group over [http://backend-a], SetHealthy called for unrelated http://not-a-backend
    targetgroup_test.go:159: output: healthy=[http://backend-a] generation=1 (was generation=1)
--- PASS: TestSetHealthyIgnoresUnknownBackend (0.00s)
=== RUN   TestSetHealthyReportsWhetherStateChanged
    targetgroup_test.go:177: input: fresh group over [backend-a], which starts healthy
    targetgroup_test.go:182: step: SetHealthy(a, true) on an already-healthy backend -> changed=false
    targetgroup_test.go:187: step: SetHealthy(a, false) -> changed=true
    targetgroup_test.go:192: step: SetHealthy(a, false) again -> changed=false
    targetgroup_test.go:197: output: SetHealthy on an unrecognized backend -> changed=false
--- PASS: TestSetHealthyReportsWhetherStateChanged (0.00s)
=== RUN   TestGenerationAdvancesOnlyOnRealChange
    targetgroup_test.go:226: input: fresh group at generation 1
    targetgroup_test.go:236: step: SetHealthy(a, true) -- already healthy, no-op changed=false generation 1 -> 1 (want 1)
    targetgroup_test.go:236: step: SetHealthy(a, false) -- real flip down        changed=true  generation 1 -> 2 (want 2)
    targetgroup_test.go:236: step: SetHealthy(a, false) -- repeat, no-op         changed=false generation 2 -> 2 (want 2)
    targetgroup_test.go:236: step: SetHealthy(b, false) -- real flip down        changed=true  generation 2 -> 3 (want 3)
    targetgroup_test.go:236: step: SetHealthy(a, true) -- real flip up           changed=true  generation 3 -> 4 (want 4)
    targetgroup_test.go:246: output: generation advanced exactly once per real change, ending at 4
--- PASS: TestGenerationAdvancesOnlyOnRealChange (0.00s)
=== RUN   TestPublishedSnapshotIsNotMutatedInPlace
    targetgroup_test.go:265: input: a reader holds a snapshot: healthy=[http://backend-a http://backend-b http://backend-c] generation=1
    targetgroup_test.go:269: step: SetHealthy(backend-b, false) publishes a new snapshot: healthy=[http://backend-a http://backend-c] generation=2
    targetgroup_test.go:271: output: the held snapshot still reads healthy=[http://backend-a http://backend-b http://backend-c] generation=1
--- PASS: TestPublishedSnapshotIsNotMutatedInPlace (0.00s)
=== RUN   TestConcurrentSetHealthyAndSnapshot
    targetgroup_test.go:307: input: 3 backends, concurrent SetHealthy flapping and Snapshot reads for 100ms
    targetgroup_test.go:366: output: 277641 snapshot reads completed against constant flapping, every one internally consistent
    targetgroup_test.go:367: output: no data race reported (run with -race to make this test meaningful)
--- PASS: TestConcurrentSetHealthyAndSnapshot (0.10s)
PASS
ok  	github.com/jerryschen31/system-design-load-balancer/internal/targetgroup	2.098s
```
