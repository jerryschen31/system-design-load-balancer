# Phase 2: Round-robin load balancing across multiple backends

- **Branch:** `phase-2-round-robin-balancer`
- **PR:** opened from this branch into `build`
- **Status:** complete

## 1. What was built

The proxy now forwards to one of several backends instead of a single fixed target, selected by a round-robin algorithm. Health checks, weighted/least-connections algorithms, timeouts, backpressure, and TLS are all still explicitly deferred — this phase's scope is exactly "more than one backend, with a selection rule," nothing more.

Concretely:
- `internal/balancer` — new package. A `Balancer` interface (`Next() *url.URL`) with one implementation, `RoundRobin`, which cycles through a fixed backend list using an atomically-incremented counter.
- `internal/proxy` — `New` now takes a `balancer.Balancer` instead of a single `*url.URL`. Backend selection happens inside the `Rewrite` closure, once per request, so every request independently calls `Next()`.
- `cmd/loadbalancer` — `-backend` is now a repeatable flag (`-backend http://a -backend http://b ...`) instead of a single default-valued flag; at least one is required.

## 2. System Design concepts & considerations

- **Load-balancing algorithm choice.** Considered round-robin, random, weighted round-robin, and least-connections. Round-robin was chosen as the first increment: it's stateless beyond a single counter, doesn't require a capacity/weight signal we don't have yet, and is the standard starting point before layering in weighting or live connection tracking. Least-connections was set aside specifically because it requires tracking live in-flight-request counts per backend — real shared mutable state with a different shape than round-robin's single counter — which is a reasonable follow-up once health checking exists.
- **Data races and synchronization primitives.** Round-robin's shared state (which index is next) is read and updated by many goroutines concurrently, since Go's `net/http` runs each request on its own goroutine. `index++` is not a single CPU step (read, add, write), so unsynchronized concurrent access can lose an update. Compared `sync.Mutex` (locks around an arbitrary critical section, blocking/parking contending goroutines) against `sync/atomic` (a single hardware-guaranteed read-modify-write instruction, no blocking). Chose atomic here because the protected state is exactly one integer and one operation — a mutex would be the right tool if the balancer later needs to update multiple related fields (e.g. an index alongside per-backend health) as one consistent unit.
- **Go's concurrency model vs. production reverse proxies.** nginx/HAProxy avoid most in-process synchronization by running single-threaded event loops per worker (no parallel access to per-connection state to protect in the first place). Go's goroutine-per-connection model gives genuine cross-core parallelism, which is why explicit synchronization (mutex/atomic) is needed at all here. This isn't a strike against Go for this use case — Caddy and Traefik are real production reverse proxies built the same way, and `go test -race` gives a concrete tool for verifying the synchronization is actually correct rather than assumed.
- **HTTP connection reuse (`http.Transport`'s idle pool).** Investigated hands-on (via `lsof` and `tcpdump`) whether the LB opens a fresh backend socket per request. It doesn't: `http.Transport` keeps idle, already-established connections per backend host and reuses them across requests, only dialing a new one when no idle connection exists for that host. Confirmed directly by watching a stable ephemeral port across repeated requests in `lsof`, and by capturing a single TCP three-way handshake (`SYN`/`SYN-ACK`/`ACK`) followed by multiple HTTP request/response cycles with no further handshakes in `tcpdump`.

## 3. Design conversation summary

Phase 2 started from the deferred item flagged at the end of phase 1: single backend, no failover. The choice of round-robin over the alternatives was driven by wanting the smallest correct increment — weighted and least-connections both need a signal (capacity weight, live connection count) this phase has no reason to introduce yet.

Implementing round-robin surfaced this project's first genuine concurrency-correctness question: the shared "next index" state is touched by every request's goroutine. Rather than defaulting to a mutex, walked through why an atomic counter is the narrower, sufficient tool here — the entire protected operation is a single fetch-and-add, and `sync.Mutex` would only start to matter once more than one field needs to change together. This was proven, not just asserted: `TestRoundRobinConcurrentCallsStayBalanced` fires 3000 calls from 50 goroutines and asserts an *exact* 1000/1000/1000 split, which only holds if the atomic counter never drops an update; the whole suite also runs under `go test -race`, which independently confirmed no unsynchronized memory access.

The other major thread this phase was tracing the actual request path through the standard library rather than treating `net/http`/`httputil.ReverseProxy` as a black box: found the exact accept-loop line (`net/http/server.go`) that spawns one goroutine per accepted connection, the exact line in `httputil.ReverseProxy.ServeHTTP` where our `Rewrite` closure (and therefore `balancer.Next()`) gets invoked, and the exact `Transport.dial` call that opens the LB-to-backend socket. That led directly into the connection-reuse investigation: rather than taking "the proxy reuses backend connections" as a claim, verified it by watching a single backend's socket in `lsof` stay open with a stable local port across five sequential requests, then went one level further and watched the raw packets with `tcpdump`, seeing one `SYN`/`SYN-ACK`/`ACK` handshake followed by multiple `PSH+ACK` request/response exchanges with no repeated handshake.

## 4. Code changes

- `internal/balancer/roundrobin.go` — `Balancer` interface; `RoundRobin` type with an `atomic.Uint64` counter; `NewRoundRobin` panics on an empty backend list (a caller/programming error, not a runtime condition to recover from).
- `internal/balancer/roundrobin_test.go` — cycling-order test and a 50-goroutine/3000-call concurrency test asserting an exact even split.
- `internal/proxy/proxy.go` — `New(b balancer.Balancer, logger *log.Logger)`; `Rewrite` calls `b.Next()` per request.
- `internal/proxy/proxy_test.go`, `internal/proxy/stress_test.go` — updated to wrap single test backends in a one-element `RoundRobin` via a new `singleBackend` test helper.
- `cmd/loadbalancer/main.go` — `backendFlag` (a `flag.Value` implementation) collects repeated `-backend` occurrences in order; `parseBackendURLs` validates the whole list (order-preserving, all-or-nothing).
- `cmd/loadbalancer/main_test.go` — tests for `parseBackendURLs` (order preserved, empty list rejected, any single invalid entry rejected).

## 5. Testing

### Functional tests
- `TestRoundRobinCyclesInOrder` — confirms the cycle order and wraparound over more calls than backends.
- `TestParseBackendURLsPreservesOrder` / `TestParseBackendURLsRejectsEmpty` / `TestParseBackendURLsRejectsAnyInvalidEntry` — CLI-level backend list parsing.
- All phase-1 proxy tests (forwarding, `X-Forwarded-For` spoof protection, hop-by-hop header stripping, 502 on unreachable backend) still pass against the new balancer-backed `New`.

### Non-functional / stress tests
- `TestRoundRobinConcurrentCallsStayBalanced` — 50 goroutines x 60 calls each (3000 total) against a 3-backend round-robin, asserting an exact 1000/1000/1000 split; run as part of the full suite under `go test -race`, which reported no data races. This is the concurrency-correctness proof for the atomic counter, not just an even-looking result.
- Phase 1's existing stress tests (slow backend outliving its own timeout, 50 concurrent requests handled in parallel, burst against an unreachable backend) still pass unchanged against the multi-backend proxy.
- Manually verified (outside the automated suite) that repeated requests through the LB to a single backend reuse one TCP connection: `lsof` showed one stable `ESTABLISHED` socket across 5 sequential requests (same ephemeral port before and after), and `tcpdump` on `lo0` showed exactly one SYN/SYN-ACK/ACK handshake followed by multiple HTTP request/response exchanges with no further handshakes.

## 6. Known weaknesses / open questions

- **No health checks.** Round-robin has no notion of a backend being down; a dead backend still receives every Nth request and produces a 502 for that request, rather than being skipped in favor of a healthy one. This is the main motivation for the next phase.
- **No weighting or load-aware selection.** Round-robin assumes equal-capacity backends and equal-cost requests; a slow or overloaded backend gets the same share of traffic as a fast one.
- **Still no timeout on backend responses, no backpressure/connection limits, no TLS** — all carried over unresolved from phase 1.
- **No stand-in backend that identifies itself.** Manually verifying round-robin currently requires running distinguishable backends by hand (e.g. multiple `python3 -m http.server` instances on different ports, distinguished only by which terminal logs a hit) rather than a single client-visible response. A small `cmd/echobackend` helper that echoes its own port was discussed as useful shared test infrastructure but not yet built.
