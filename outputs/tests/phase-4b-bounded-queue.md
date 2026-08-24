# Phase 4b Test Report: Bounded, FIFO-fair wait queue for the concurrency limiter

**Source:** uncommitted work on `phase-4a-timeouts-backpressure` (phase 4b was built as a continuation of the same branch, layered on top of phase 4a), based on `e7b55f5` on `build`
**Command:** `go test -v -race -count=1 ./cmd/... ./internal/...`
**Result:** 59 passed, 0 failed, across 6 packages

| Package | Result | Duration |
|---|---|---|
| `cmd/echobackend` | ok | 1.28s |
| `cmd/loadbalancer` | ok | 2.17s |
| `internal/balancer` | ok | 1.85s |
| `internal/healthcheck` | ok | 2.87s |
| `internal/middleware` | ok | 2.40s |
| `internal/proxy` | ok | 3.49s |

Scoped to `./cmd/... ./internal/...` for the same reason as phase 4a's report: `scratch/` contains pre-existing standalone exploratory files that don't build as part of the module (confirmed via `git stash` before phase 4a work began).

The new FIFO-queue tests (`TestLimiterAcquireIsFIFO`, `TestLimiterGiveUpOnClientCancelFreesQueueSlot`, and the three `TestMaxConcurrentQueue*` tests) were additionally run 10x in a row under `-race` (`go test -race -count=10 -run 'TestLimiterAcquireIsFIFO|TestLimiterGiveUpOnClientCancelFreesQueueSlot|TestMaxConcurrentQueue'`) to check for flakiness after a real ordering bug was found and fixed in the first draft of `TestLimiterAcquireIsFIFO` (see `notes/phase-4b-bounded-queue.md` for what the bug was and why it happened) — all 10 repeats passed.

Generated on 2026-08-24 directly from the working branch. See `notes/phase-4b-bounded-queue.md` for the narrative writeup this output supports, including the manual localhost walkthrough covering all four queue outcomes (immediate grant, queued-then-admitted, queued-then-timed-out, queue-full-immediate-reject) against real processes.

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
    main_test.go:156: output: status=200 body="slept 0s\n" elapsed=32.625µs
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
ok  	github.com/jerryschen31/system-design-load-balancer/cmd/echobackend	1.279s
=== RUN   TestIntegration_ChecksRouteAroundUnhealthyBackend
    integration_test.go:102: input: 2 backends, one (http://127.0.0.1:58738) already unhealthy before the checker ever runs
    integration_test.go:121: output: good backend got 30 requests, unhealthy backend got 0
--- PASS: TestIntegration_ChecksRouteAroundUnhealthyBackend (0.12s)
=== RUN   TestStress_RecoveredBackendImmediatelyGetsFullShare
    integration_test.go:151: input: 3 backends; backend 0 (http://127.0.0.1:58792) starts unhealthy, 1 and 2 start healthy
    integration_test.go:183: step: before recovery, backend 0 got 0 of 30 requests
    integration_test.go:189: step: backend 0 flipped to healthy
    integration_test.go:192: step: checker confirmed backend 0 healthy again
    integration_test.go:201: output: on the very first burst after recovery was detected, backend 0 received 30 of 90 requests (an even share is 30)
    integration_test.go:205: KNOWN WEAKNESS: a recovered backend receives its full concurrent traffic share the instant it's marked healthy, with no gradual ramp-up. A backend still warming up after recovery (cold cache, JIT warmup, reconnecting to a database) can be knocked back down immediately.
--- PASS: TestStress_RecoveredBackendImmediatelyGetsFullShare (0.17s)
=== RUN   TestStress_ConcurrentTrafficSurvivesHealthFlapping
    integration_test.go:220: input: 2 backends, one flips healthy/unhealthy every 10ms while traffic runs
    integration_test.go:277: output: status distribution across 200 requests during flapping: map[200:200]
--- PASS: TestStress_ConcurrentTrafficSurvivesHealthFlapping (0.07s)
=== RUN   TestIntegration_MaxConcurrentProtectsFullStack
    integration_test.go:307: input: maxConcurrent=4, one backend whose handler blocks until released
    integration_test.go:339: step: 4 requests confirmed in flight at the real backend, through the full stack
    integration_test.go:367: output: 10-request burst while at capacity -> status counts map[503:10]
    integration_test.go:374: output: the 4 originally-admitted requests all completed with statuses [200 200 200 200]
--- PASS: TestIntegration_MaxConcurrentProtectsFullStack (0.00s)
=== RUN   TestIntegration_QueueAdmitsNearMissRequest
    integration_test.go:400: input: maxConcurrent=2, each holder finishes in ~150ms; queueCapacity=1, queueWaitTimeout=5s (well above holderDelay)
    integration_test.go:442: output: near-miss request -> status=200, waited 283.025083ms (holders took 304.056167ms total)
--- PASS: TestIntegration_QueueAdmitsNearMissRequest (0.30s)
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
ok  	github.com/jerryschen31/system-design-load-balancer/cmd/loadbalancer	2.169s
=== RUN   TestRoundRobinCyclesInOrder
    roundrobin_test.go:25: input: 3 backends, calling Next() 7 times in a row
    roundrobin_test.go:31: call 0: got http://backend-a (want http://backend-a)
    roundrobin_test.go:31: call 1: got http://backend-b (want http://backend-b)
    roundrobin_test.go:31: call 2: got http://backend-c (want http://backend-c)
    roundrobin_test.go:31: call 3: got http://backend-a (want http://backend-a)
    roundrobin_test.go:31: call 4: got http://backend-b (want http://backend-b)
    roundrobin_test.go:31: call 5: got http://backend-c (want http://backend-c)
    roundrobin_test.go:31: call 6: got http://backend-a (want http://backend-a)
    roundrobin_test.go:36: output: cycle wrapped correctly past the end of the backend list twice
--- PASS: TestRoundRobinCyclesInOrder (0.00s)
=== RUN   TestNewRoundRobinCopiesBackends
    roundrobin_test.go:47: input: construct RoundRobin over [http://backend-a http://backend-b], then mutate the original slice
    roundrobin_test.go:54: step: caller's original slice[0] is now http://attacker-controlled
    roundrobin_test.go:57: output: rr.Next() returned http://backend-a
--- PASS: TestNewRoundRobinCopiesBackends (0.00s)
=== RUN   TestRoundRobinConcurrentCallsStayBalanced
    roundrobin_test.go:83: input: 50 goroutines x 60 calls each = 3000 total calls across 3 backends
    roundrobin_test.go:105: distribution: map[http://backend-a:1000 http://backend-b:1000 http://backend-c:1000]
    roundrobin_test.go:114: output: every backend received exactly 1000 calls, confirming no update was lost across 50 concurrent goroutines
--- PASS: TestRoundRobinConcurrentCallsStayBalanced (0.01s)
=== RUN   TestNewRoundRobinStartsAllBackendsHealthy
    roundrobin_test.go:125: input: 2 fresh backends, no SetHealthy calls yet
    roundrobin_test.go:132: output: backends reached by Next(): map[http://backend-a:true http://backend-b:true]
--- PASS: TestNewRoundRobinStartsAllBackendsHealthy (0.00s)
=== RUN   TestRoundRobinSkipsUnhealthyBackend
    roundrobin_test.go:148: input: 3 healthy backends, then backend-b is marked unhealthy
    roundrobin_test.go:158: output: 6 calls to Next() never returned the unhealthy backend
    roundrobin_test.go:165: step: backend-b marked healthy again; backends reached: map[http://backend-a:true http://backend-b:true http://backend-c:true]
--- PASS: TestRoundRobinSkipsUnhealthyBackend (0.00s)
=== RUN   TestRoundRobinAllUnhealthyReturnsNil
    roundrobin_test.go:178: input: 2 backends, both marked unhealthy
    roundrobin_test.go:184: output: Next() returned <nil>
--- PASS: TestRoundRobinAllUnhealthyReturnsNil (0.00s)
=== RUN   TestRoundRobinSetHealthyIgnoresUnknownBackend
    roundrobin_test.go:197: input: balancer over [http://backend-a], SetHealthy called for unrelated http://not-a-backend
    roundrobin_test.go:202: output: Next() returned http://backend-a
--- PASS: TestRoundRobinSetHealthyIgnoresUnknownBackend (0.00s)
=== RUN   TestRoundRobinConcurrentSetHealthyAndNext
    roundrobin_test.go:223: input: 3 backends, concurrent SetHealthy flapping and Next() calls for 100ms
    roundrobin_test.go:265: output: no data race reported (run with -race to make this test meaningful)
--- PASS: TestRoundRobinConcurrentSetHealthyAndNext (0.15s)
=== RUN   TestRoundRobinSetHealthyReportsWhetherStateChanged
    roundrobin_test.go:277: input: fresh balancer over [backend-a], which starts healthy
    roundrobin_test.go:282: step: SetHealthy(a, true) on an already-healthy backend -> changed=false
    roundrobin_test.go:287: step: SetHealthy(a, false) -> changed=true
    roundrobin_test.go:292: step: SetHealthy(a, false) again -> changed=false
    roundrobin_test.go:297: output: SetHealthy on an unrecognized backend -> changed=false
--- PASS: TestRoundRobinSetHealthyReportsWhetherStateChanged (0.00s)
PASS
ok  	github.com/jerryschen31/system-design-load-balancer/internal/balancer	1.845s
=== RUN   TestCheckerDetectsHealthyBackend
    healthcheck_test.go:48: input: backend at http://127.0.0.1:59269 answers /health with 200
    healthcheck_test.go:60: output: first check reported healthy=true
--- PASS: TestCheckerDetectsHealthyBackend (0.00s)
=== RUN   TestCheckerDetectsUnhealthyStatus
    healthcheck_test.go:71: input: backend at http://127.0.0.1:59271 answers /health with 500
    healthcheck_test.go:83: output: first check reported healthy=false
--- PASS: TestCheckerDetectsUnhealthyStatus (0.00s)
=== RUN   TestCheckerDetectsTimeout
    healthcheck_test.go:98: input: backend at http://127.0.0.1:59273 sleeps 500ms before answering; checker timeout is 50ms
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
ok  	github.com/jerryschen31/system-design-load-balancer/internal/healthcheck	2.867s
=== RUN   TestMaxConcurrentAllowsExactlyNInFlight
    concurrency_test.go:46: input: MaxConcurrent(3, 0, 0) wrapping a handler that blocks until released
    concurrency_test.go:66: step: 3 requests confirmed in flight (holding every semaphore slot)
    concurrency_test.go:85: step: 2 additional requests while at capacity -> statuses [503 503]
    concurrency_test.go:94: output: the 3 originally-admitted requests all completed with statuses [200 200 200]
--- PASS: TestMaxConcurrentAllowsExactlyNInFlight (0.00s)
=== RUN   TestLimiterAcquireIsFIFO
    concurrency_test.go:114: input: 1 slot (held), 3 waiters joining the queue strictly in order 0, 1, 2
    concurrency_test.go:128: step: all 3 waiters confirmed enqueued (queue length reached 3)
    concurrency_test.go:147: output: admission order was [0 1 2], want [0 1 2]
--- PASS: TestLimiterAcquireIsFIFO (0.00s)
=== RUN   TestLimiterGiveUpOnClientCancelFreesQueueSlot
    concurrency_test.go:166: input: 1 slot (held), queue capacity 1
    concurrency_test.go:174: step: waiter confirmed enqueued
    concurrency_test.go:178: output: cancelled waiter's acquire returned 3, want acquireClientGaveUp
--- PASS: TestLimiterGiveUpOnClientCancelFreesQueueSlot (0.00s)
=== RUN   TestMaxConcurrentQueueAdmitsAfterWait
    concurrency_test.go:205: input: 1 slot (held), queue capacity 1, queueWaitTimeout 2s; a second request arrives while the first is in flight
    concurrency_test.go:219: step: holder confirmed in flight, holding the only slot
    concurrency_test.go:244: output: holder status=200, queued (waited then admitted) status=200
--- PASS: TestMaxConcurrentQueueAdmitsAfterWait (0.05s)
=== RUN   TestMaxConcurrentQueueTimesOut
    concurrency_test.go:272: input: 1 slot held indefinitely, queueWaitTimeout=100ms, a second request that will never be released in time
    concurrency_test.go:276: step: holder confirmed in flight, holding the only slot
    concurrency_test.go:288: output: status=503 body="timed out waiting for a free slot\n" elapsed=102.605083ms
--- PASS: TestMaxConcurrentQueueTimesOut (0.10s)
=== RUN   TestMaxConcurrentQueueFullRejectsImmediately
    concurrency_test.go:322: input: 1 slot held, queue capacity 1 also held, a 3rd (overflow) request arrives
    concurrency_test.go:331: step: 1st request confirmed holding the only slot
    concurrency_test.go:345: step: 2nd request given time to join the only queue slot
    concurrency_test.go:357: output: overflow request -> status=503 body="too many concurrent requests\n" elapsed=1.131458ms
--- PASS: TestMaxConcurrentQueueFullRejectsImmediately (0.05s)
=== RUN   TestMaxConcurrentPanicsOnNonPositiveN
    concurrency_test.go:371: input: MaxConcurrent(0, 0, 0)
    concurrency_test.go:377: output: panicked as expected: middleware: MaxConcurrent requires a positive n
--- PASS: TestMaxConcurrentPanicsOnNonPositiveN (0.00s)
=== RUN   TestMaxConcurrentPanicsOnNegativeQueueCapacity
    concurrency_test.go:383: input: MaxConcurrent(1, -1, 0)
    concurrency_test.go:389: output: panicked as expected: middleware: MaxConcurrent requires a non-negative queueCapacity
--- PASS: TestMaxConcurrentPanicsOnNegativeQueueCapacity (0.00s)
=== RUN   TestMaxConcurrentPanicsOnQueueWithoutWaitTimeout
    concurrency_test.go:395: input: MaxConcurrent(1, 5, 0) -- a real queue capacity but no positive wait timeout
    concurrency_test.go:401: output: panicked as expected: middleware: MaxConcurrent requires a positive queueWaitTimeout when queueCapacity > 0
--- PASS: TestMaxConcurrentPanicsOnQueueWithoutWaitTimeout (0.00s)
=== RUN   TestLoggingAllowsNilLogger
    logging_test.go:10: input: middleware.Logging(nil) wrapping a handler that returns 204
    logging_test.go:12: step: inner handler runs and writes HTTP 204
    logging_test.go:24: output: request completed with status 204 and no panic
--- PASS: TestLoggingAllowsNilLogger (0.00s)
PASS
ok  	github.com/jerryschen31/system-design-load-balancer/internal/middleware	2.397s
=== RUN   TestForwardsRequestToBackend
    proxy_test.go:62: input: client sends POST /widgets to load balancer http://127.0.0.1:59294; backend is http://127.0.0.1:59293
    proxy_test.go:52: backend step: received POST /widgets
    proxy_test.go:83: output: client got status 418 and body "hello from backend"
--- PASS: TestForwardsRequestToBackend (0.00s)
=== RUN   TestClientCannotSpoofForwardedFor
    proxy_test.go:100: input: client sends X-Forwarded-For="6.6.6.6" to http://127.0.0.1:59298
    proxy_test.go:89: backend step: saw X-Forwarded-For="127.0.0.1"
    proxy_test.go:114: output: backend received rewritten X-Forwarded-For="127.0.0.1"
--- PASS: TestClientCannotSpoofForwardedFor (0.00s)
=== RUN   TestHopByHopHeaderNotForwarded
    proxy_test.go:135: input: client sends Connection="Keep-Alive" and Keep-Alive="timeout=5"
    proxy_test.go:121: backend step: Connection="" Keep-Alive present=false
    proxy_test.go:149: output: backend saw Connection="" Keep-Alive present=false
--- PASS: TestHopByHopHeaderNotForwarded (0.00s)
=== RUN   TestBackendDownReturns502
    proxy_test.go:157: input: backend http://127.0.0.1:1 is down; client sends GET / through load balancer http://127.0.0.1:59305
    proxy_test.go:168: output: client got status 502
--- PASS: TestBackendDownReturns502 (0.00s)
=== RUN   TestNewAllowsNilLogger
    proxy_test.go:175: input: proxy.New(http://127.0.0.1:1, 0, nil)
    proxy_test.go:186: output: client got status 502 and the proxy did not panic
--- PASS: TestNewAllowsNilLogger (0.00s)
=== RUN   TestNoHealthyBackendsReturns502
    proxy_test.go:200: input: balancer's only backend (http://127.0.0.1:1) marked unhealthy
    proxy_test.go:216: output: client got status 502, body "no healthy backends available\n"
--- PASS: TestNoHealthyBackendsReturns502 (0.00s)
=== RUN   TestNewPanicsOnNilBalancer
    proxy_test.go:231: input: proxy.New(nil, 0, nil)
    proxy_test.go:238: output: New panicked as expected: proxy: New requires a non-nil Balancer
--- PASS: TestNewPanicsOnNilBalancer (0.00s)
=== RUN   TestBackendTimeoutReturns502
    proxy_test.go:260: input: backend sleeps 500ms, proxy backendTimeout is 50ms, client has no timeout of its own
    proxy_test.go:271: output: status=502 body="backend request timed out\n" elapsed=52.015041ms
--- PASS: TestBackendTimeoutReturns502 (0.50s)
=== RUN   TestBackendTimeoutAllowsSlowStreamedBodyWithinBudget
    proxy_test.go:311: input: backend streams 5 chunks, 20ms apart, total ~100ms; backendTimeout is 2s (well above that)
    proxy_test.go:324: output: status=200 body="chunk-0 chunk-1 chunk-2 chunk-3 chunk-4 "
--- PASS: TestBackendTimeoutAllowsSlowStreamedBodyWithinBudget (0.11s)
=== RUN   TestRewriteLogsRoutingDecision
    proxy_test.go:343: input: GET / through the load balancer, backend is http://127.0.0.1:59325
    proxy_test.go:352: output: logged "routed GET / -> http://127.0.0.1:59325\n"
--- PASS: TestRewriteLogsRoutingDecision (0.00s)
=== RUN   TestStress_DisabledTimeoutWaitsIndefinitely
    stress_test.go:21: input: backendTimeout=0 (disabled); backend sleeps for 300ms; client timeout is 50ms
    stress_test.go:24: backend step: request reached backend; backend is now sleeping
    stress_test.go:44: output: client returned after 51.378792ms with error Get "http://127.0.0.1:59330/": context deadline exceeded (Client.Timeout exceeded while awaiting headers) -- opt-out confirmed working
--- PASS: TestStress_DisabledTimeoutWaitsIndefinitely (0.30s)
=== RUN   TestStress_ConcurrentRequestsHandledConcurrently
    stress_test.go:53: input: 50 concurrent requests; backend delay per request is 100ms
    stress_test.go:96: output: burst finished in 116.985583ms (serial time would have been 5s)
--- PASS: TestStress_ConcurrentRequestsHandledConcurrently (0.12s)
=== RUN   TestStress_BurstAgainstUnreachableBackend
    stress_test.go:105: input: 50 concurrent requests against a load balancer whose only backend is down
    stress_test.go:133: output: every observed response was 502
--- PASS: TestStress_BurstAgainstUnreachableBackend (0.01s)
PASS
ok  	github.com/jerryschen31/system-design-load-balancer/internal/proxy	3.494s
```
