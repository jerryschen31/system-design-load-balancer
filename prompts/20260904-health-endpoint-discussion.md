 ▐▛███▛█   Claude Code v2.1.261
▝▜██████▀  Sonnet 5 · Claude Pro
  ▝▝ ▝▝    ~/gh/public/system-design-load-balancer


❯ @cmd/loadbalancer/main.go:80 why is the default health check path a separate  
  backend endpoint /health ? Would it make more sense to check the root backend 
  endpoint / ? Are there are drawbacks, tradeoffs or "gotchas" with probing     
  the root endpoint? Because what if a backend does not have a handler for the  
  /health endpoint?                                                             

  Read 1 file (ctrl+o to expand)

⏺ This is a genuinely good System Design question — dedicated health endpoints
  vs. probing / is a real production decision. Let me explain the concept first,
  then connect it to the code.

  The general problem

  A load balancer's health check has to answer one question: "can this backend 
  correctly serve real traffic right now?" But it has to answer that question
  without generating real traffic (that would defeat the purpose — you don't
  want to overload an already-struggling backend just to check on it).

  There are two very different ways to probe:

  Option A: probe "/"                    Option B: probe "/health"
  ┌─────────────────────────┐            ┌─────────────────────────┐
  │ hits real app logic:     │            │ hits a dedicated, tiny  │
  │  - DB queries?           │            │ handler that just says: │
  │  - auth checks?          │            │  "yes, I'm alive" (200) │
  │  - renders a page?       │            │  no DB, no auth, no I/O │
  │  - depends on a cache?   │            │                         │
  └─────────────────────────┘            └─────────────────────────┘

  Why a separate /health endpoint is the conventional choice

  1. / often isn't a cheap, side-effect-free operation. In a real backend, the
     root route might run a DB query, render a template, check auth, hit a
     cache, etc. If your health checker polls that every 5 seconds from the load
     balancer, you've turned health checking into a recurring load-generator
     against your most expensive endpoint — the opposite of what you want a
     lightweight liveness probe to be.
  2. / might not even return a stable, simple 200. Root routes commonly redirect
     (302 to a login page), require auth (401/403), or vary by content
     negotiation. A health checker wants a boring, deterministic signal — "200
     means good, anything else means bad" — and app routes are rarely designed
     with that contract in mind.
  3. Separation of concerns. /health (or /healthz, a convention from Kubernetes)
     is infrastructure plumbing, not a feature of the app. It lets you build a
     distinct, meaningful signal — e.g., "database connection pool is up," "disk
     isn't full," "dependent services reachable" — that goes beyond "did the
     server accept a TCP connection and return literally anything."

  Your specific worry: what if the backend has no /health handler?

  This is exactly the gotcha, and it's real. In this project, your backend is
  presumably a simple test server you wrote yourself — Go's net/http uses a
  router (http.ServeMux by default) where any path with no registered handler 
  returns a 404, not a crash. So:

  - Health checker (internal/healthcheck/healthcheck.go) requests /health.
  - If the backend never registered that route, it gets 404 Not Found.
  - The health checker treats non-2xx as unhealthy (worth confirming — let's
    check that logic) and marks the backend down — even though the backend is 
    perfectly capable of serving real traffic on /.

  That's the trade-off in one sentence: probing /health is more correct in
  production (cheap, stable, meaningful signal) but it's a contract — every
  backend registered with the load balancer must implement it, or the load
  balancer will wrongly evict a healthy backend. Probing / avoids that contract
  requirement (works on anything) but risks the load-and-instability problems
  above.

  Want me to look at how internal/healthcheck/healthcheck.go currently
  interprets the response (does it check for exactly 200, or any 2xx, or does
  404 specifically get treated as unhealthy)? That's the detail that determines
  how sharp this gotcha is in your current implementation.

✻ Crunched for 21s · done 10:39 PM

❯ yes, check how healthcheck.go interprets the response - see lines 127 - 130   
                                                                                
  healthy := resp.StatusCode >= 200 && resp.StatusCode < 300                    
  if !healthy {                                                                 
      c.logger.Printf("healthcheck: %s returned status %d", backend,            
  resp.StatusCode)                                                              
  }                                                                             
  It would be a bit of a redesign (don't make any code changes), but I'm        
  wondering if there was a more sophisticated way that health checks could be   
  done using the root endpoint, something like:                                 
  1. a backend is serving traffic normally - no need to probe (it's healthy)    
  2. a backend times out on a request - immediately do a health check probe     
  against the root endpoint                                                     
  3. a backend hasn't needed to serve traffic in a while (sat idle for a        
  certain time duration) - do a health check and make sure the backend is still 
  alive                                                                         
                                                                                
  Are there any trade-offs or caveats to this idea?                             

⏺ Confirmed — lines 127-130 treat any non-2xx as unhealthy, with no special case
  for 404. So a backend with no /health handler gets marked down on the very
  first probe, even though it's fully capable of serving real traffic on /.
  That's the gotcha in concrete terms.

  Now, your proposed redesign.

  The general shape of the idea

  There are two fundamentally different ways a load balancer can learn "is this
  backend OK":

  - Active checking (what you have now): the load balancer manufactures
    synthetic traffic on a timer, independent of real users, and uses the
    response as ground truth.
  - Passive checking (what you're proposing for cases 1 and 2): the load
    balancer doesn't manufacture anything — it just watches what happens to the
    real requests it's already sending, and treats a failure/timeout as the
    signal.

  Your scheme is a hybrid: stay passive while real traffic is flowing and
  succeeding, fall back to a quick active probe only when something surprising
  happens (a timeout) or when you've gone too long without any signal at all
  (idle).

  Diagram: what triggers what

                      ┌─────────────────────────┐
                      │   backend marked HEALTHY │
                      └───────────┬─────────────┘
                                  │
                real request routed to this backend
                                  │
                ┌─────────────────┴─────────────────┐
                │                                     │
          succeeds in time                      times out / errors
                │                                     │
     reset "last used" timer                 fire an immediate probe
     stay HEALTHY, no probe needed             against "/" right now
                │                                     │
                │                          ┌──────────┴──────────┐
                │                     probe succeeds        probe fails
                │                          │                     │
                │                    stay HEALTHY           mark UNHEALTHY
                │                                                 │
     ┌──────────┴───────────┐                       (needs its own recheck
     idle timer expires      │                        cadence to recover —
     (no traffic routed      │                        see caveat below)
     in N seconds)           │
                │
       fire an idle probe against "/"
                │
       succeeds → stay HEALTHY
       fails    → mark UNHEALTHY

  Mapping your three cases onto this: case 1 is the top-left loop (no probe,
  just watching real outcomes), case 2 is the top-right branch (timeout →
  immediate reactive probe), case 3 is the bottom branch (idle timer → active
  probe as a fallback when there's no real traffic to observe).

  Trade-offs and caveats

  1. Passive checking means a real user pays for the first failure. By
  definition, you can't detect "backend is down" passively until a real request
  has already been sent to it and failed. That user experiences the full
  backend-timeout (10s by default in this project) before the load balancer
  reacts. Your current active-only design can, in the best case, already have
  marked the backend unhealthy before that user's request was ever routed there.
  This is the core cost of passive checking — it trades synthetic-traffic
  overhead for a worse worst-case user experience.

  2. A timeout on the real route doesn't prove the root path (/) is unhealthy — 
  or vice versa. Case 2 assumes "the request to the real endpoint timed out, so
  let's check /." But / is probably cheap and fast, while the real request might
  have failed because of something narrow: a slow downstream dependency, a slow
  specific handler, a GC pause, a momentary overload. The / probe could come
  back a clean 200 milliseconds later, telling you "healthy" when the backend
  actually can't serve real traffic within your SLA. You'd be conflating
  liveness ("the process is up and can answer HTTP at all") with
  capacity/correctness ("it can serve this particular kind of work in time"),
  and these are genuinely different signals.

  3. One failure isn't proof of death — but waiting for many failures is slower 
  to react. If you evict on a single timeout, a single transient blip (one slow
  GC pause, one network hiccup) takes a backend out of rotation. Removing a
  backend concentrates its share of traffic onto the remaining ones — under real
  load this can cascade (remaining backends now closer to their own limits,
  more likely to also time out, more evictions...). Real systems handling this
  (e.g., Envoy's "outlier detection," HAProxy's passive checks) use a threshold
  — N failures in a window, or an error rate — rather than one-strike eviction.
  That's more state to track per backend, not just a boolean.

  4. You still need active polling for recovery — you don't get to delete the 
  health-check package. Once a backend is marked unhealthy from a failed probe,
  how do you learn it's back? You're not routing real traffic to it anymore
  (it's out of rotation), so there's no passive signal available. You need some
  ongoing active re-probe of unhealthy backends to notice recovery — which means
  this design doesn't eliminate active health checking, it only skips it for
  backends currently believed healthy. The existing health-check-interval
  machinery mostly survives, just scoped to the unhealthy set.

  5. Idle detection needs new, contended state on the hot path. Right now,
  internal/healthcheck runs independently of the request path — it's a clean,
  separate concern. To know "how long since this backend last served traffic,"
  every proxied request has to update a per-backend "last used" timestamp, on
  every single request, in internal/proxy. That's a new piece of shared mutable
  state touched concurrently by many goroutines on the hot path — even if it's
  just an atomic timestamp write, it's new coupling between the proxy and the
  balancer/health-check layer that doesn't exist today.

  6. Round-robin specifically makes case 3 rare. Round-robin gives every healthy
  backend roughly equal traffic, so under any real load, no backend sits idle
  for long — the idle-fallback path in your diagram mostly won't fire except at
  very low request rates. It becomes much more relevant for a future algorithm
  that can leave a backend unused for a while (e.g., least-connections when one
  backend is fast and rarely gets picked, or an algorithm that deliberately
  drains a backend).

  7. Cold-start problem for new/recovered backends. A backend that's never
  received traffic (just added to config, or just came back from unhealthy) has
  no traffic history to be passive about. You'd want to force an active probe
  before ever routing real user traffic to it the first time — otherwise the
  very first real request becomes the health check, with a real user as the
  guinea pig.

  The net shape: this is a legitimate, real pattern (it's roughly what "passive
  health checking" / "outlier detection" means in production load balancers),
  but it's additive complexity, not a simplification — you keep the active
  checker for recovery and cold-start, add new shared state on the request hot
  path, and take on a threshold/flap-control policy you don't currently need.
  It's worth doing eventually as its own phase, but it's a bigger step than the
  current health-check design.

✻ Cooked for 1m 6s · done 11:39 PM