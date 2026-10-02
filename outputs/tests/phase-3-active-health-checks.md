# Phase 3 Test Report: Active health checks

**Source commit:** `e82a23e` (tip of `phase-3-active-health-checks`, PR #4 into `build`, not yet merged)
**Command:** `go test -v -race -count=1 ./...`
**Result:** 36 passed, 0 failed, across 6 packages

| Package | Result | Duration |
|---|---|---|
| `cmd/echobackend` | ok | 1.15s |
| `cmd/loadbalancer` | ok | 1.49s |
| `internal/balancer` | ok | 1.46s |
| `internal/healthcheck` | ok | 2.53s |
| `internal/middleware` | ok | 1.45s |
| `internal/proxy` | ok | 2.14s |

Generated on 2026-08-23 directly from the phase-3 branch (no historical checkout needed, since this phase's own tip *is* the current code). See `notes/phase-3-active-health-checks.md` for the narrative writeup this output supports, including the manual localhost walkthrough (killing/restarting and `/health/toggle`-ing real backend processes) that isn't captured in `go test` output.

## Full output

```
=== RUN   TestHandlerReportsOwnAddress
    main_test.go:18: input: GET /widgets against a handler configured with addr=:9001
    main_test.go:24: output: response body = "echobackend :9001 handled GET /widgets\n"
--- PASS: TestHandlerReportsOwnAddress (0.00s)
=== RUN   TestHealthHandlerReflectsState
    main_test.go:35: input: GET /health while healthy=true, then again after flipping to healthy=false
    main_test.go:39: step: healthy=true -> status 200
    main_test.go:47: output: healthy=false -> status 503
--- PASS: TestHealthHandlerReflectsState (0.00s)
=== RUN   TestToggleHandlerFlipsHealthAndRejectsGet
    main_test.go:58: input: POST /health/toggle twice, then GET /health/toggle once
    main_test.go:62: step: after 1st POST, healthy=false (body "healthy=false\n")
    main_test.go:69: step: after 2nd POST, healthy=true
    main_test.go:76: output: GET /health/toggle -> status 405
--- PASS: TestToggleHandlerFlipsHealthAndRejectsGet (0.00s)
PASS
ok  	github.com/jerryschen31/system-design-load-balancer/cmd/echobackend	1.148s
=== RUN   TestIntegration_ChecksRouteAroundUnhealthyBackend
    integration_test.go:101: input: 2 backends, one (http://127.0.0.1:59983) already unhealthy before the checker ever runs
    integration_test.go:120: output: good backend got 30 requests, unhealthy backend got 0
--- PASS: TestIntegration_ChecksRouteAroundUnhealthyBackend (0.11s)
=== RUN   TestStress_RecoveredBackendImmediatelyGetsFullShare
    integration_test.go:150: input: 3 backends; backend 0 (http://127.0.0.1:60031) starts unhealthy, 1 and 2 start healthy
    integration_test.go:182: step: before recovery, backend 0 got 0 of 30 requests
    integration_test.go:188: step: backend 0 flipped to healthy
    integration_test.go:191: step: checker confirmed backend 0 healthy again
    integration_test.go:200: output: on the very first burst after recovery was detected, backend 0 received 30 of 90 requests (an even share is 30)
    integration_test.go:204: KNOWN WEAKNESS: a recovered backend receives its full concurrent traffic share the instant it's marked healthy, with no gradual ramp-up. A backend still warming up after recovery (cold cache, JIT warmup, reconnecting to a database) can be knocked back down immediately.
--- PASS: TestStress_RecoveredBackendImmediatelyGetsFullShare (0.11s)
=== RUN   TestStress_ConcurrentTrafficSurvivesHealthFlapping
    integration_test.go:219: input: 2 backends, one flips healthy/unhealthy every 10ms while traffic runs
    integration_test.go:276: output: status distribution across 200 requests during flapping: map[200:200]
--- PASS: TestStress_ConcurrentTrafficSurvivesHealthFlapping (0.02s)
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
ok  	github.com/jerryschen31/system-design-load-balancer/cmd/loadbalancer	1.489s
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
--- PASS: TestRoundRobinConcurrentCallsStayBalanced (0.00s)
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
--- PASS: TestRoundRobinConcurrentSetHealthyAndNext (0.12s)
PASS
ok  	github.com/jerryschen31/system-design-load-balancer/internal/balancer	1.464s
=== RUN   TestCheckerDetectsHealthyBackend
    healthcheck_test.go:48: input: backend at http://127.0.0.1:60370 answers /health with 200
    healthcheck_test.go:60: output: first check reported healthy=true
--- PASS: TestCheckerDetectsHealthyBackend (0.00s)
=== RUN   TestCheckerDetectsUnhealthyStatus
    healthcheck_test.go:71: input: backend at http://127.0.0.1:60372 answers /health with 500
    healthcheck_test.go:83: output: first check reported healthy=false
--- PASS: TestCheckerDetectsUnhealthyStatus (0.00s)
=== RUN   TestCheckerDetectsTimeout
    healthcheck_test.go:98: input: backend at http://127.0.0.1:60374 sleeps 500ms before answering; checker timeout is 50ms
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
PASS
ok  	github.com/jerryschen31/system-design-load-balancer/internal/healthcheck	2.525s
=== RUN   TestLoggingAllowsNilLogger
    logging_test.go:10: input: middleware.Logging(nil) wrapping a handler that returns 204
    logging_test.go:12: step: inner handler runs and writes HTTP 204
    logging_test.go:24: output: request completed with status 204 and no panic
--- PASS: TestLoggingAllowsNilLogger (0.00s)
PASS
ok  	github.com/jerryschen31/system-design-load-balancer/internal/middleware	1.449s
=== RUN   TestForwardsRequestToBackend
    proxy_test.go:49: input: client sends POST /widgets to load balancer http://127.0.0.1:60377; backend is http://127.0.0.1:60376
    proxy_test.go:39: backend step: received POST /widgets
    proxy_test.go:70: output: client got status 418 and body "hello from backend"
--- PASS: TestForwardsRequestToBackend (0.00s)
=== RUN   TestClientCannotSpoofForwardedFor
    proxy_test.go:87: input: client sends X-Forwarded-For="6.6.6.6" to http://127.0.0.1:60381
    proxy_test.go:76: backend step: saw X-Forwarded-For="127.0.0.1"
    proxy_test.go:101: output: backend received rewritten X-Forwarded-For="127.0.0.1"
--- PASS: TestClientCannotSpoofForwardedFor (0.00s)
=== RUN   TestHopByHopHeaderNotForwarded
    proxy_test.go:122: input: client sends Connection="Keep-Alive" and Keep-Alive="timeout=5"
    proxy_test.go:108: backend step: Connection="" Keep-Alive present=false
    proxy_test.go:136: output: backend saw Connection="" Keep-Alive present=false
--- PASS: TestHopByHopHeaderNotForwarded (0.00s)
=== RUN   TestBackendDownReturns502
    proxy_test.go:144: input: backend http://127.0.0.1:1 is down; client sends GET / through load balancer http://127.0.0.1:60388
    proxy_test.go:155: output: client got status 502
--- PASS: TestBackendDownReturns502 (0.00s)
=== RUN   TestNewAllowsNilLogger
    proxy_test.go:162: input: proxy.New(http://127.0.0.1:1, nil)
    proxy_test.go:173: output: client got status 502 and the proxy did not panic
--- PASS: TestNewAllowsNilLogger (0.00s)
=== RUN   TestNoHealthyBackendsReturns502
    proxy_test.go:187: input: balancer's only backend (http://127.0.0.1:1) marked unhealthy
    proxy_test.go:198: output: client got status 502
--- PASS: TestNoHealthyBackendsReturns502 (0.00s)
=== RUN   TestNewPanicsOnNilBalancer
    proxy_test.go:205: input: proxy.New(nil, nil)
    proxy_test.go:212: output: New panicked as expected: proxy: New requires a non-nil Balancer
--- PASS: TestNewPanicsOnNilBalancer (0.00s)
=== RUN   TestStress_SlowBackendHasNoTimeout
    stress_test.go:21: input: backend sleeps for 300ms; client timeout is 50ms
    stress_test.go:24: backend step: request reached backend; backend is now sleeping
    stress_test.go:44: output: client returned after 51.297334ms with error Get "http://127.0.0.1:60397/": context deadline exceeded (Client.Timeout exceeded while awaiting headers)
    stress_test.go:45: KNOWN WEAKNESS: proxy has no timeout on backend responses (aborted after 51.297334ms only because the client protected itself; backend needed 300ms). A hung backend can hold load balancer resources indefinitely.
--- PASS: TestStress_SlowBackendHasNoTimeout (0.30s)
=== RUN   TestStress_ConcurrentRequestsHandledConcurrently
    stress_test.go:54: input: 50 concurrent requests; backend delay per request is 100ms
    stress_test.go:97: output: burst finished in 116.242583ms (serial time would have been 5s)
--- PASS: TestStress_ConcurrentRequestsHandledConcurrently (0.12s)
=== RUN   TestStress_BurstAgainstUnreachableBackend
    stress_test.go:106: input: 50 concurrent requests against a load balancer whose only backend is down
    stress_test.go:134: output: every observed response was 502
--- PASS: TestStress_BurstAgainstUnreachableBackend (0.01s)
PASS
ok  	github.com/jerryschen31/system-design-load-balancer/internal/proxy	2.138s
```
