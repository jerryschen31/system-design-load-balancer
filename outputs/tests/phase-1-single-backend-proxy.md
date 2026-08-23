# Phase 1 Test Report: Single-backend reverse proxy

**Source commit:** `732912d` (merge of PR #1, `phase-1-single-backend-proxy` into `build`)
**Command:** `go test -v -race -count=1 ./...`
**Result:** 12 passed, 0 failed, across 3 packages

| Package | Result | Duration |
|---|---|---|
| `cmd/loadbalancer` | ok | 1.36s |
| `internal/middleware` | ok | 1.25s |
| `internal/proxy` | ok | 1.92s |

Regenerated retroactively on 2026-08-23 by checking out the phase's actual merge commit in an isolated worktree, so this reflects what the code did at the end of phase 1, not phase 1's code re-run against later changes. See `notes/phase-1-single-backend-proxy.md` for the narrative writeup this output supports.

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
PASS
ok  	github.com/jerryschen31/system-design-load-balancer/cmd/loadbalancer	1.360s
=== RUN   TestLoggingAllowsNilLogger
    logging_test.go:10: input: middleware.Logging(nil) wrapping a handler that returns 204
    logging_test.go:12: step: inner handler runs and writes HTTP 204
    logging_test.go:24: output: request completed with status 204 and no panic
--- PASS: TestLoggingAllowsNilLogger (0.00s)
PASS
ok  	github.com/jerryschen31/system-design-load-balancer/internal/middleware	1.254s
=== RUN   TestForwardsRequestToBackend
    proxy_test.go:38: input: client sends POST /widgets to load balancer http://127.0.0.1:57509; backend is http://127.0.0.1:57508
    proxy_test.go:28: backend step: received POST /widgets
    proxy_test.go:59: output: client got status 418 and body "hello from backend"
--- PASS: TestForwardsRequestToBackend (0.00s)
=== RUN   TestClientCannotSpoofForwardedFor
    proxy_test.go:76: input: client sends X-Forwarded-For="6.6.6.6" to http://127.0.0.1:57513
    proxy_test.go:65: backend step: saw X-Forwarded-For="127.0.0.1"
    proxy_test.go:90: output: backend received rewritten X-Forwarded-For="127.0.0.1"
--- PASS: TestClientCannotSpoofForwardedFor (0.00s)
=== RUN   TestHopByHopHeaderNotForwarded
    proxy_test.go:111: input: client sends Connection="Keep-Alive" and Keep-Alive="timeout=5"
    proxy_test.go:97: backend step: Connection="" Keep-Alive present=false
    proxy_test.go:125: output: backend saw Connection="" Keep-Alive present=false
--- PASS: TestHopByHopHeaderNotForwarded (0.00s)
=== RUN   TestBackendDownReturns502
    proxy_test.go:136: input: backend http://127.0.0.1:1 is down; client sends GET / through load balancer http://127.0.0.1:57520
    proxy_test.go:147: output: client got status 502
--- PASS: TestBackendDownReturns502 (0.00s)
=== RUN   TestNewAllowsNilLogger
    proxy_test.go:158: input: proxy.New(http://127.0.0.1:1, nil)
    proxy_test.go:169: output: client got status 502 and the proxy did not panic
--- PASS: TestNewAllowsNilLogger (0.00s)
=== RUN   TestStress_SlowBackendHasNoTimeout
    stress_test.go:22: input: backend sleeps for 300ms; client timeout is 50ms
    stress_test.go:25: backend step: request reached backend; backend is now sleeping
    stress_test.go:45: output: client returned after 51.264625ms with error Get "http://127.0.0.1:57527/": context deadline exceeded (Client.Timeout exceeded while awaiting headers)
    stress_test.go:46: KNOWN WEAKNESS: proxy has no timeout on backend responses (aborted after 51.264625ms only because the client protected itself; backend needed 300ms). A hung backend can hold load balancer resources indefinitely.
--- PASS: TestStress_SlowBackendHasNoTimeout (0.30s)
=== RUN   TestStress_ConcurrentRequestsHandledConcurrently
    stress_test.go:55: input: 50 concurrent requests; backend delay per request is 100ms
    stress_test.go:98: output: burst finished in 117.723666ms (serial time would have been 5s)
--- PASS: TestStress_ConcurrentRequestsHandledConcurrently (0.12s)
=== RUN   TestStress_BurstAgainstUnreachableBackend
    stress_test.go:107: input: 50 concurrent requests against a load balancer whose only backend is down
    stress_test.go:139: output: every observed response was 502
--- PASS: TestStress_BurstAgainstUnreachableBackend (0.01s)
PASS
ok  	github.com/jerryschen31/system-design-load-balancer/internal/proxy	1.920s
```
