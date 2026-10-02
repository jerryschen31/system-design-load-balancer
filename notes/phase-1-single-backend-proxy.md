# Phase 1: Single-backend HTTP reverse proxy

- **Branch:** `phase-1-single-backend-proxy`
- **PR:** opened from this branch into `build`
- **Status:** complete

## 1. What was built

A Layer 7 (HTTP) reverse proxy in Go that forwards every request it receives to a single, fixed backend. No load-balancing algorithm, no multiple backends, no health checks, no rate limiting, no caching, no TLS — all explicitly deferred to later phases. The goal of this phase was the smallest possible correct request path, done carefully rather than done big.

Concretely:
- `cmd/loadbalancer` — the binary entrypoint: reads `-listen` and `-backend` flags, wires the proxy and middleware together, and handles graceful shutdown on `SIGINT`/`SIGTERM`.
- `internal/proxy` — builds an `httputil.ReverseProxy` configured to forward to the one backend, using Go's modern `Rewrite`/`SetXForwarded` API (not the deprecated `NewSingleHostReverseProxy` helper) so client-supplied forwarding headers can't be spoofed.
- `internal/middleware` — a `Logging` middleware (method, path, status, latency) wrapping the proxy, deliberately kept separate from the proxy itself so rate limiting and caching can be added the same way in later phases.

## 2. System Design concepts & considerations

- **Layer 4 vs. Layer 7 proxying.** L4 forwards raw TCP bytes with no knowledge of HTTP; L7 parses the request. Decided L7 specifically because request caching is only expressible with HTTP-level semantics (you cache a response keyed by method/path/headers — meaningless at the byte-stream level), and per-IP rate limiting benefits from request-level (not just connection-level) granularity. Both are planned for later phases, so L4 would have been a dead end requiring a rewrite.
- **Sockets as the actual mechanism underneath `net/http`.** A socket is a kernel-maintained table entry (4-tuple: local IP:port, remote IP:port, plus TCP state and send/receive buffers), referenced from a process via a file descriptor. Listening sockets (`LISTEN` state, no remote address) are distinct from per-connection sockets (`ESTABLISHED`, both addresses filled in) — `accept()` creates a new entry per connection rather than reusing the listening one. This matters for reasoning about what "one request" actually costs the process: two sockets per request through this proxy (client↔LB, LB↔backend), each with its own kernel-side state and buffers.
- **Hop-by-hop vs. end-to-end headers.** Headers like `Connection`/`Keep-Alive` describe properties of one specific socket hop, not the request content, and must be regenerated per hop rather than copied through — copying them through incorrectly is a known source of protocol bugs and request-smuggling vulnerabilities in real proxies. `httputil.ReverseProxy` handles this correctly by default.
- **The `X-Forwarded-For` trust problem.** The backend only ever sees the LB's IP as the connection's real source, since it's a separate socket from the client's. `X-Forwarded-For` is how the original client IP crosses that gap — but it's just a text header, so a client can set it on their own request to lie about their IP unless the proxy explicitly overwrites it using the actual source address its own kernel observed. This is directly load-bearing for the per-IP rate limiting planned in a later phase: if this weren't fixed now, that entire feature would be trivially bypassable.
- **Standard library API surface as a real design decision.** `httputil.NewSingleHostReverseProxy` (the common example code found online) uses the deprecated `Director` path and preserves client-supplied `X-Forwarded-*` headers by default — confirmed by reading `go doc`, not assumed from memory. Built against the modern `ReverseProxy{Rewrite: ...}` API and `ProxyRequest.SetXForwarded()` instead, which strips those headers from the client and sets them fresh.
- **Graceful shutdown.** `http.Server.Shutdown` closes the listening socket immediately (no new connections) but lets in-flight requests finish, up to a deadline, instead of severing every open connection on process kill.
- **Middleware as the extension point.** Cross-cutting concerns (logging now; rate limiting and caching later) are implemented as `func(http.Handler) http.Handler` wrappers around the proxy, not logic embedded inside it.

## 3. Design conversation summary

Started by deciding the proxy's layer. The deciding factor wasn't proxy mechanics but two features already known to be coming later: per-IP rate limiting and request caching. Caching is only meaningful with HTTP semantics, which settled it toward L7 over L4.

Next question was build-vs-delegate: hand-write the socket-level forwarding to see the mechanics directly, or use `httputil.ReverseProxy` and treat correctness-sensitive HTTP plumbing (hop-by-hop headers, streaming, chunked encoding) as something to delegate, the way real proxies (nginx, HAProxy, Envoy) do rather than hand-roll. Chose to delegate the transport mechanics but keep full control of policy (target selection, error handling, header rewriting) in code we wrote and understand.

While implementing, checked the actual `go doc` output for `httputil.ReverseProxy` rather than relying on the commonly-copied `NewSingleHostReverseProxy` pattern, and found it uses the deprecated, spoofable header-handling path. Switched to the modern `Rewrite`/`SetXForwarded()` API as a result — an unplanned finding that turned into one of the more concrete lessons of this phase (verify library behavior against docs, especially for anything with a security property, rather than trusting the first example you remember).

After the implementation and tests were in place, went through several rounds of deeper first-principles explanation on request: what a socket actually is at the kernel level (the file-descriptor table vs. the kernel's own connection-state table, and what fields live in the latter), how HTTP frames a request as text over that socket, why hop-by-hop headers can't be copied between hops, and the exact mechanics of the `X-Forwarded-For` spoofing risk — all explicitly without analogies, given a 20+-year gap since formal CS coursework. That calibration (first-principles, no metaphors, don't assume standard networking vocabulary) is now recorded in `CLAUDE.md` for future phases.

## 4. Code changes

- `go.mod` — module `github.com/jerryschen31/system-design-load-balancer`.
- `cmd/loadbalancer/main.go` — entrypoint: flag parsing, wiring proxy + logging middleware, `SIGINT`/`SIGTERM` handling with a 10s graceful-shutdown deadline.
- `internal/proxy/proxy.go` — `New(target *url.URL, logger *log.Logger) *httputil.ReverseProxy`: rewrites requests to the backend via `SetURL`/`SetXForwarded`, and a custom `ErrorHandler` that logs and returns 502 when the backend is unreachable.
- `internal/middleware/logging.go` — `Logging(logger) func(http.Handler) http.Handler`: logs method/path/status/latency per request; includes a `statusRecorder` wrapper since `http.ResponseWriter` doesn't expose the status code it was given after the fact.
- `internal/proxy/proxy_test.go`, `internal/proxy/stress_test.go` — see below.

## 5. Testing

### Functional tests
- `TestForwardsRequestToBackend` — confirms method, path, status, and body all pass through the proxy unchanged.
- `TestClientCannotSpoofForwardedFor` — sends a request with a forged `X-Forwarded-For` already set and confirms the backend never sees the forged value.
- `TestHopByHopHeaderNotForwarded` — sends `Connection`/`Keep-Alive` headers and confirms the backend receives neither.
- `TestBackendDownReturns502` — points the proxy at a port nothing is listening on and confirms a clean 502, not a hang or crash.

### Non-functional / stress tests
- `TestStress_SlowBackendHasNoTimeout` — backend sleeps 300ms, client times out at 50ms; confirms the *client's* timeout is what ends the request, not any limit imposed by the proxy. This is the one genuine weakness this phase surfaced (see below).
- `TestStress_ConcurrentRequestsHandledConcurrently` — 50 concurrent requests against a backend with a 100ms per-request delay complete in well under the serial-time bound, confirming Go's per-connection-goroutine model is actually providing concurrency here, not just assumed to.
- `TestStress_BurstAgainstUnreachableBackend` — 50 concurrent requests against a dead backend all return a clean 502; the proxy itself stays healthy under concurrent failure, not just single-request failure.

All 7 tests pass (`go test ./...`); `go vet ./...` and `gofmt -l .` are clean.

## 6. Known weaknesses / open questions

- **No timeout on backend responses** (confirmed by `TestStress_SlowBackendHasNoTimeout`). A hung or slow backend can hold a load-balancer-side goroutine, socket, and buffers open indefinitely; with enough concurrent hung requests this becomes a resource-exhaustion problem on the load balancer itself, not just a slow-response problem for one client. Deferred fix: request/response timeouts via `context` deadlines or a configured `http.Transport`.
- **No backpressure or connection limits.** Nothing currently caps concurrent in-flight requests or backend connections; a legitimate traffic spike and a malicious flood look identical to this proxy today. This is exactly the gap the planned rate-limiting phase addresses.
- **Single backend, no failover.** The entire service depends on one backend instance; there's no notion of "this backend is down, try elsewhere" because there's nowhere else to try yet. Addressed in Phase 2 (multiple backends + selection algorithm).
- **No health checks.** The proxy only discovers a backend is unhealthy by attempting a real request and failing — no proactive checking.
- **No TLS.** Both the client-facing and backend-facing connections are plaintext HTTP.
- **Logging only, no metrics.** Line-based stdout logs exist; no structured metrics/counters yet (e.g. request rate, error rate, latency percentiles) that a later phase could use to drive load-balancing or rate-limiting decisions.
