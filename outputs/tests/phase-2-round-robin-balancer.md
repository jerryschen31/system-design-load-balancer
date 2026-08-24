# Phase 2 Test Report: Round-robin load balancing across multiple backends

**Source commit:** `1c10997` (merge of PR #2, `phase-2-round-robin-balancer` into `build`)
**Command:** `go test -v -race -count=1 ./...`
**Result:** 19 passed, 0 failed, across 4 packages

| Package | Result | Duration |
|---|---|---|
| `cmd/loadbalancer` | ok | 1.25s |
| `internal/balancer` | ok | 1.15s |
| `internal/middleware` | ok | 1.36s |
| `internal/proxy` | ok | 1.90s |

Regenerated retroactively on 2026-08-23 by checking out the phase's actual merge commit in an isolated worktree, so this reflects what the code did at the end of phase 2, not phase 2's code re-run against later changes (`cmd/echobackend`, added afterward, correctly does not appear here). See `notes/phase-2-round-robin-balancer.md` for the narrative writeup this output supports.

## Full output

```
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
ok  	github.com/jerryschen31/system-design-load-balancer/cmd/loadbalancer	1.251s
=== RUN   TestRoundRobinCyclesInOrder
    roundrobin_test.go:24: input: 3 backends, calling Next() 7 times in a row
    roundrobin_test.go:30: call 0: got http://backend-a (want http://backend-a)
    roundrobin_test.go:30: call 1: got http://backend-b (want http://backend-b)
    roundrobin_test.go:30: call 2: got http://backend-c (want http://backend-c)
    roundrobin_test.go:30: call 3: got http://backend-a (want http://backend-a)
    roundrobin_test.go:30: call 4: got http://backend-b (want http://backend-b)
    roundrobin_test.go:30: call 5: got http://backend-c (want http://backend-c)
    roundrobin_test.go:30: call 6: got http://backend-a (want http://backend-a)
    roundrobin_test.go:35: output: cycle wrapped correctly past the end of the backend list twice
--- PASS: TestRoundRobinCyclesInOrder (0.00s)
=== RUN   TestNewRoundRobinCopiesBackends
    roundrobin_test.go:46: input: construct RoundRobin over [http://backend-a http://backend-b], then mutate the original slice
    roundrobin_test.go:53: step: caller's original slice[0] is now http://attacker-controlled
    roundrobin_test.go:56: output: rr.Next() returned http://backend-a
--- PASS: TestNewRoundRobinCopiesBackends (0.00s)
=== RUN   TestRoundRobinConcurrentCallsStayBalanced
    roundrobin_test.go:82: input: 50 goroutines x 60 calls each = 3000 total calls across 3 backends
    roundrobin_test.go:104: distribution: map[http://backend-a:1000 http://backend-b:1000 http://backend-c:1000]
    roundrobin_test.go:113: output: every backend received exactly 1000 calls, confirming no update was lost across 50 concurrent goroutines
--- PASS: TestRoundRobinConcurrentCallsStayBalanced (0.00s)
PASS
ok  	github.com/jerryschen31/system-design-load-balancer/internal/balancer	1.151s
=== RUN   TestLoggingAllowsNilLogger
    logging_test.go:10: input: middleware.Logging(nil) wrapping a handler that returns 204
    logging_test.go:12: step: inner handler runs and writes HTTP 204
    logging_test.go:24: output: request completed with status 204 and no panic
--- PASS: TestLoggingAllowsNilLogger (0.00s)
PASS
ok  	github.com/jerryschen31/system-design-load-balancer/internal/middleware	1.363s
=== RUN   TestForwardsRequestToBackend
    proxy_test.go:49: input: client sends POST /widgets to load balancer http://127.0.0.1:57950; backend is http://127.0.0.1:57949
    proxy_test.go:39: backend step: received POST /widgets
    proxy_test.go:70: output: client got status 418 and body "hello from backend"
--- PASS: TestForwardsRequestToBackend (0.00s)
=== RUN   TestClientCannotSpoofForwardedFor
    proxy_test.go:87: input: client sends X-Forwarded-For="6.6.6.6" to http://127.0.0.1:57954
    proxy_test.go:76: backend step: saw X-Forwarded-For="127.0.0.1"
    proxy_test.go:101: output: backend received rewritten X-Forwarded-For="127.0.0.1"
--- PASS: TestClientCannotSpoofForwardedFor (0.00s)
=== RUN   TestHopByHopHeaderNotForwarded
    proxy_test.go:122: input: client sends Connection="Keep-Alive" and Keep-Alive="timeout=5"
    proxy_test.go:108: backend step: Connection="" Keep-Alive present=false
    proxy_test.go:136: output: backend saw Connection="" Keep-Alive present=false
--- PASS: TestHopByHopHeaderNotForwarded (0.00s)
=== RUN   TestBackendDownReturns502
    proxy_test.go:144: input: backend http://127.0.0.1:1 is down; client sends GET / through load balancer http://127.0.0.1:57961
    proxy_test.go:155: output: client got status 502
--- PASS: TestBackendDownReturns502 (0.00s)
=== RUN   TestNewAllowsNilLogger
    proxy_test.go:162: input: proxy.New(http://127.0.0.1:1, nil)
    proxy_test.go:173: output: client got status 502 and the proxy did not panic
--- PASS: TestNewAllowsNilLogger (0.00s)
=== RUN   TestNewPanicsOnNilBalancer
    proxy_test.go:177: input: proxy.New(nil, nil)
    proxy_test.go:184: output: New panicked as expected: proxy: New requires a non-nil Balancer
--- PASS: TestNewPanicsOnNilBalancer (0.00s)
=== RUN   TestStress_SlowBackendHasNoTimeout
    stress_test.go:21: input: backend sleeps for 300ms; client timeout is 50ms
    stress_test.go:24: backend step: request reached backend; backend is now sleeping
    stress_test.go:44: output: client returned after 51.173333ms with error Get "http://127.0.0.1:57968/": context deadline exceeded (Client.Timeout exceeded while awaiting headers)
    stress_test.go:45: KNOWN WEAKNESS: proxy has no timeout on backend responses (aborted after 51.173333ms only because the client protected itself; backend needed 300ms). A hung backend can hold load balancer resources indefinitely.
--- PASS: TestStress_SlowBackendHasNoTimeout (0.30s)
=== RUN   TestStress_ConcurrentRequestsHandledConcurrently
    stress_test.go:54: input: 50 concurrent requests; backend delay per request is 100ms
    stress_test.go:97: output: burst finished in 115.23625ms (serial time would have been 5s)
--- PASS: TestStress_ConcurrentRequestsHandledConcurrently (0.12s)
=== RUN   TestStress_BurstAgainstUnreachableBackend
    stress_test.go:106: input: 50 concurrent requests against a load balancer whose only backend is down
    stress_test.go:134: output: every observed response was 502
--- PASS: TestStress_BurstAgainstUnreachableBackend (0.01s)
PASS
ok  	github.com/jerryschen31/system-design-load-balancer/internal/proxy	1.904s
```
