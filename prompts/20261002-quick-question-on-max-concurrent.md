 ▐▛███▛█   Claude Code v2.1.288
▝▜██████▀  Sonnet 5.5 · Claude Pro
 ▝▝   ▝▝   ~/gh/public/system-design-load-balancer

▎ Your voice can help guide AI
▎ Take 15 min to share your experiences with Anthropic Interviewer. Start now

❯ For phase 4 of this project (backpressure), was maxConcurrent set to the      
  number of backends?                                                           

  Listed 1 directory, ran 1 shell command (ctrl+o to expand)

⏺ No. maxConcurrent is not tied to the number of backends in phase 4.

  - Production default: it's a fixed flag in cmd/loadbalancer/main.go:83,
    -max-concurrent, defaulting to 100. It is a global cap on requests the load
    balancer processes at once, whatever the backend count. Nothing in the code
    reads len(backends) to derive it.
  - Tests: they use small hand-picked constants. They are 4 in
    integration_test.go:309 and 2 in integration_test.go:402. Each runs against
    a single backend. The unit tests use a local n that is also independent of
    backend count.
  - Notes: notes/phase-4a-timeouts-backpressure.md doesn't give a rationale
    for 100. It does list the global, not per-backend, nature of the limit as a
    known weakness (line 76). A slow backend can use up the shared cap and crowd
    out requests that would have gone to healthy backends. Phase 4b's queue
    (-queue-capacity 20, -queue-wait-timeout 2s) has the same shape.

  The value is a global limit on the load balancer's own capacity, not on the
  backends'. It is a different design from a per-backend limit such as
  maxConcurrent × numBackends, where capacity scales as backends are added or
  removed. That per-backend version would handle the "one slow backend hogs the
  cap" problem the notes describe.

  I can sketch what a per-backend limit would look like and how it would
  interact with health checks, if you'd like to look at that.

✻ Churned for 14s · done 4:20 PM

❯ So if there were say 100 concurrent requests but only 5 backends (so each     
  backend can only handle 1 request at a time), without a queue, what happened  
  to the 95 waiting requests?                                                   

  Ran 1 shell command (ctrl+o to expand)

⏺ The answer depends on which of two limits is binding. In the default setup,
  the 95 requests never wait at the load balancer at all.

  The load balancer only counts its own in-flight requests

  The load balancer's admission check counts how many requests it is currently
  handling. It does not know that each backend can only serve one at a time.
  Nothing in phase 4 tells it that.

  With the default -max-concurrent 100, your scenario looks like this:

  100 clients ──► LB admission check: "100 in flight, limit is 100"
                    │
                    ├─ all 100 admitted (no queue involved, no 503)
                    ▼
                round-robin spreads them: ~20 per backend
                    │
                    ▼
          backend A  backend B  backend C  backend D  backend E
           1 working  1 working  1 working  1 working  1 working
           19 waiting 19 waiting ...  (waiting INSIDE the backend)

  The 95 requests are forwarded and wait at the backends. Where exactly they
  wait depends on the backend:

  - A typical Go or Node HTTP server accepts all the connections and runs them
    all at once, slowly. Your "1 at a time" assumption would not hold, and
    latency would degrade for everyone.
  - A backend that really serializes work leaves requests sitting in its accept
    backlog or internal queue, for a duration the load balancer has no say in.

  The load balancer's only protection here is -backend-timeout (default 10s). A
  request that hasn't finished by then gets cut off with a timeout error,
  distinct from the generic backend-down 502 (proxy.go:147). It also frees its
  slot. So the 95 requests would eventually time out if the backends are slow
  enough. That is the failure mode phase 4 doesn't prevent.

  Setting the limit to the real capacity

  Suppose you set -max-concurrent 5 and left the queue off with -queue-capacity 
  0. The behavior is then in concurrency.go:184-196:

  100 arrive ──► 5 get slots ──► forwarded, one per backend
                 95 find all slots taken ──► immediate 503
                   "too many concurrent requests"

  The rejected requests are not held anywhere. Each gets an instant 503 and the
  client decides whether to retry. That is fail-fast backpressure, which was the
  point of phase 4a.

  With -queue-capacity 20, as in 4b, up to 20 of the 95 would wait in FIFO
  order, for at most 2s each. A waiter gets a 503 if its wait times out. The
  other 75 get an immediate 503.

  What this shows

  You've found the gap the notes call out. -max-concurrent is a number the
  operator has to set by hand, and it has to match actual backend capacity
  (backends × per-backend concurrency). Nothing keeps it in sync. If a backend
  is added, removed, or marked unhealthy by the health checker, the cap stays
  the same. A per-backend limit would fix this, because capacity would then
  scale with the healthy backend count.

  If you want to see this, I can give you a curl and hey walkthrough. It would
  use a stand-in backend with a /sleep endpoint and run the LB twice, once with
  -max-concurrent 100 and once with -max-concurrent 5. You'd see slow responses
  and timeouts in the first run and instant 503s in the second.

✻ Cogitated for 18s · done 4:22 PM

❯ So the 20 requests per backend in this example get sent to the backend, and   
  its up to the backend to handle this request backlog (if processing each      
  request was slow, let's say)                                                  

⏺ Yes, mostly. Once the load balancer admits a request, it forwards it and waits
  for the response. How the backend copes with its backlog is up to the
  backend.

  The load balancer keeps three things even after handing the request off.

  1. The request still occupies one of the load balancer's slots. The slot is
  held until the backend responds or the timeout fires. With 100 slots and all
  100 requests stuck behind slow backends, no slots are free:

  slow backends ──► responses don't come back ──► slots aren't released
                                                        │
                                                        ▼
                      request #101 arrives ──► queue (if enabled), else 503

  This is how backend slowness turns into backpressure at the load balancer. It
  works only indirectly, though. The limit is on the load balancer's total
  in-flight count, not on what any one backend can take.

  2. -backend-timeout (10s) is the only bound on the wait. If a backend hasn't
  answered in 10 seconds, the proxy cancels the request, returns a timeout error
  to the client, and releases the slot. Cancelling also drops the proxy's
  connection to the backend. A well-behaved backend can notice that and stop
  working on a request nobody is waiting for. Whether it does is again up to the
  backend. Phase 4a's echobackend /sleep handler does stop, as the comment in
  concurrency.go notes.

  3. If the client disconnects first, the request is cancelled too. The proxy's
  context is derived from the client's request context.

  Two things the load balancer doesn't do about a backend backlog:

  - It doesn't route around a backlogged backend. Round-robin ignores load, so a
    backend with 19 queued requests gets the next request in turn exactly as an
    idle one would. Routing by load would need a least-connections algorithm.
    That is a natural later phase, and a common interview topic.
  - The health checker may or may not notice. If a backend processes requests
    strictly in order, its /health probe queues behind the backlog. The 2s probe
    timeout could then fail and mark it unhealthy. If the backend serves
    /health concurrently with the slow work, it looks perfectly healthy while
    its users time out. I haven't tested either case here. It's a good one to
    try with the stand-in backend.

✻ Cooked for 9s · done 4:24 PM