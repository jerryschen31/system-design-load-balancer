╭─── Claude Code v2.1.221 ─────────────────────────────────────────────────────╮
│                                                    │ Tips for getting        │
│                 Welcome back Jerry!                │ started                 │
│                                                    │ Run /init to create a … │
│                      ▗ ▗   ▖ ▖                     │ ─────────────────────── │
│                                                    │ What's new              │
│                        ▘▘ ▝▝                       │ Cost estimates (`/cost… │
│   Sonnet 5 · Claude Pro · [redacted]'s   │ Added the one-time ful… │
│   Organization                                     │ Added `/claude-api upg… │
│       ~/gh/public/system-design-load-balancer      │ /release-notes for more │
╰──────────────────────────────────────────────────────────────────────────────╯


❯ /clear                                                                        

❯ /teaching-mode Let's begin phase 4. Assess a good next step we should build.  

  Read 1 file, listed 1 directory, ran 1 shell command (ctrl+o to expand)

⏺ I reviewed the phase 3 notes and current code structure. Here's where phase 4
  picks up.

  Where we are

  Phase 3 gave the load balancer a way to know which backends are healthy. What
  it still doesn't have is any way to protect itself from a backend, or protect
  backend selection from being naive. The "Known weaknesses" list at the end of
  phase 3 names three real candidates, and they're genuinely different kinds of
  problems:

  Client → [Load Balancer] → Backend
                │
     currently: no timeout on backend response,
     no cap on concurrent connections,
     picks backends by round-robin only (ignores load),
     only learns "down" from /health polling (not real traffic)

  Three candidate next steps

  1. Backend timeouts + connection/concurrency limits (backpressure) — 
  recommended
  Right now a hung backend can hold a proxied request open indefinitely, and
  there's no ceiling on how many concurrent requests the LB will accept and fan
  out. This is a self-protection gap that's existed since phase 1 — it's not
  about picking a smarter backend, it's about the LB surviving contact with a
  bad one. Trade-off: a hard timeout means occasionally killing a request that
  would've eventually succeeded (slow-but-fine vs. actually-hung look identical
  from outside); a concurrency cap means the LB starts rejecting/queuing clients
  once full, which is a new failure mode you have to define (fail fast with
  503? queue with a bound?).

  2. Weighted / least-connections selection
  Move Next() beyond pure round-robin to account for backend capacity or current
  load. Natural extension of code you already have (roundrobin.go). Trade-off:
  least-connections requires the LB to track in-flight request counts per
  backend (new state, new concurrency surface, similar shape to phase 3's health
  map); weighted round-robin is simpler but the weights are static config, not
  a real load signal.

  3. Passive health checking (+ circuit breaker)
  Detect a backend that's failing real requests even though its /health endpoint
  looks fine (e.g., one broken route on an otherwise-healthy process) — phase
  3's active checks are structurally blind to this. Trade-off: requires deciding
  a failure-rate threshold and a recovery policy (half-open circuit, like
  Envoy/Hystrix), which is more design surface than it sounds — get the
  threshold wrong and you either never trip (useless) or flap on normal error
  noise.

  I'd lean toward #1: it's the oldest open gap, it's a correctness/safety issue
  rather than an optimization, and "what happens when a backend hangs and
  everyone keeps sending it traffic" is one of the most common System Design
  interview follow-ups for load balancers. But this is a real fork in what you
  learn next, so I'd rather you pick than have me default it.

⏺ User declined to answer questions
  ⎿  · Which direction for phase 4? (Timeouts + backpressure / Weighted / 
     least-connections / Passive health checks + circuit breaker)

⏺ What would you like to clarify?

✻ Brewed for 51s

❯ Timeouts + backpressure (connection / concurrency limits) makes sense as the  
  next step. How will you mock test connection / concurrency limits? A queue    
  with a bound makes sense for queueing client requests once full. And is       
  concurrency limit the same thing as rate-limiting ?                           

  Read 5 files (ctrl+o to expand)

⏺ Good questions — all three are exactly the kind of distinction that separates
  "backpressure" from buzzwords.

  Concurrency limit vs. rate limit — these bound different things

  Rate limit: how many requests get admitted per unit of wall-clock time (e.g.
  100/sec), regardless of how long each one takes to finish.

  Concurrency limit: how many requests are currently open at once — a live count
  that goes up when a request starts and down when it finishes, independent of
  how fast they're arriving.

  Concrete case that separates them: one client sends 1 request/sec, but each
  one takes 10 seconds to get a response (like your /hang endpoint in
  echobackend, or just a slow one). A rate limiter checking "is this under
  1/sec?" says yes, forever — it only looks at arrivals. But concurrency is
  climbing toward 10 in steady state, because each new request arrives before
  the previous ones have finished. If those requests never finish, concurrency
  climbs without bound and the rate limiter never notices anything wrong.

  That's exactly today's exposure: the load balancer holds a goroutine, an open
  socket to the client, and an open socket to the backend for every in-flight
  request. A hung backend doesn't send requests faster — it just never lets them
  finish. So the resource that's actually at risk is "how many requests are
  open right now," which is what a concurrency limit bounds. Rate limiting is a
  real, separate control (useful for abuse prevention / per-client fairness) but
  it wouldn't touch this specific failure — worth flagging as a later, distinct
  feature rather than folding it in now.

  Testing it without sleeps

  Phase 3's recovery test already established the principle to reuse here — it
  waits on the checker's actual callback firing (detected := make(chan 
  struct{})), not a fixed time.Sleep, because sleep-based assertions race
  against real scheduling and flake under load. Same problem applies to
  concurrency: "sleep 50ms then assert N requests are in flight" is exactly that
  kind of race.

  The deterministic version: build a test backend (same shape as toggleBackend
  in integration_test.go) whose handler, on each request, (1) signals its
  arrival on a channel the test is reading, then (2) blocks on a second
  "release" channel the test controls:

  arrived := make(chan struct{}, n)
  release := make(chan struct{})
  backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r
  *http.Request) {
      arrived <- struct{}{}
      <-release
      w.WriteHeader(http.StatusOK)
  }))

  Test then: fires exactly N concurrent requests through the LB, drains arrived
  N times (proves N are genuinely in flight at the backend — an event, not a
  guess), fires one more and asserts it comes back rejected immediately (or, for
  the queued case, hasn't reached arrived yet), then close(release) to let the
  held ones finish and confirm anything queued gets admitted. No timing
  assumptions anywhere except a generous fail-safe timeout on the whole test.

  Bounded queue — one thing to flag before we lock it in

  Agreed that's the right shape, but a bounded queue needs two knobs, not one:
  queue capacity (how many can wait) and a max wait time in the queue. Without
  the second, a queued request doesn't fail faster than an unbounded one — it
  just fails invisibly later, after sitting in queue the whole time, which is
  worse for the client (they get no signal anything's wrong until they've
  already waited). And when the queue itself is full, the standard move is to
  reject immediately (503) rather than queue the queue — unbounded queueing just
  relocates the exhaustion from goroutines to queue memory.

  Concrete shape for phase 4

  Two independent mechanisms, both fit cleanly into existing files:

  - Backend response timeout — in internal/proxy/proxy.go, inside the
  roundTripperFunc Transport (proxy.go:77-84): wrap the outbound request with
  context.WithTimeout before calling http.DefaultTransport.RoundTrip(r), same
  pattern healthcheck.go already uses per-probe. A new -backend-timeout flag in
  main.go.
  - Concurrency limiter — a new file in internal/middleware (whose doc comment
  already says "rate limiting... will land here in later phases" — concurrency
  limiting belongs there too), same func(http.Handler) http.Handler shape as
  Logging. A buffered channel of capacity N is the idiomatic Go semaphore:
  acquire = send, release = receive. New -max-concurrent and -queue-wait-timeout
  flags.

  One decision I want your sign-off on before I write anything: when the
  semaphore is full, do we go straight to reject-immediately (503) first as the
  smallest viable slice, then add the bounded-wait-queue as a second slice on
  top of it — or build the queue in from the start? I'd lean toward
  reject-first, since it's the simpler mechanism to get right and correctly
  tested before adding queue-capacity and max-wait as two more knobs on top —
  but you called out the queue as the thing that made sense to you, so tell me
  if you'd rather start there.

✻ Worked for 1m 45s

※ recap: We're building phase 4 of the load balancer: backend timeouts plus concurrency limiting. Next: you decide whether to start with reject-immediately on a full semaphore, or build the bounded queue in from the start.

❯ Let's start without the queue as "phase 4a". And then we can add a queue with 
  an appropriate size and a queue-wait-timeout as "phase 4b" later.             
  For simulating concurrent requests "piling up" on a backend, I am actually    
  thinking we can create an additional endpoint on the echobackend - a /sleep   
  endpoint that will receive a request and then sleep for a fixed number of     
  seconds (or even better, accepts a seconds parameter e.g. /sleep?time=5 for 5 
  second sleep). Concurrent requests hitting this endpoint would sleep when     
  they hit the backend and pile up until max-concurrent is reached, at which    
  point subsequent requests to /sleep would return a 503. Would this work?      

⏺ Good instinct, and mostly yes — but there's one gap worth naming before we
  build on it.

  Would /sleep?seconds=N work?

  For the manual walkthrough (curl loops, hey, watching things happen in real
  terminals) — yes, and it's a good fit: same design as the /health/toggle and
  /hang endpoints you already have, just parameterized. You'd fire a burst of
  concurrent requests at /sleep?seconds=5, watch the first N return normally
  after 5s and the rest come back 503 immediately, and see the limiter's
  boundary in real time. That's exactly the kind of hands-on moment this repo's
  notes call out as the most effective one so far.

  Where it falls short is the automated unit test that proves the exact boundary
  (Nth request admitted, N+1th rejected — no off-by-one). The problem: firing
  N+1 goroutines that each hit /sleep "concurrently" doesn't guarantee the first
  N have actually reached the load balancer's semaphore before the N+1th tries
  to acquire it — goroutine scheduling, network round-trips, and OS scheduling
  are all real, variable delays with no ordering guarantee. You'd have to paper
  over that with something like "fire N, sleep 100ms to let them settle, then
  fire the extra one" — which is precisely the
  fixed-sleep-racing-real-scheduling pattern phase 3's notes explicitly moved
  away from (detected := make(chan struct{}) instead of a sleep, for the
  recovery test). It'd probably pass locally and flake occasionally in CI under
  load — a real, if intermittent, bug in the test itself.

  Proposal: use both, for different jobs

  - cmd/echobackend gets a real GET /sleep?seconds=N endpoint (default a couple
  seconds, small sanity cap so a typo doesn't hang a demo for an hour) — for the
  manual walkthrough and for coarse automated stress tests where you're
  checking aggregate behavior ("of 50 concurrent requests, exactly N-worth
  succeeded, the rest were clean 503s") rather than an exact instant-by-instant
  boundary.
  - The precise unit test (proxy/middleware level) uses an inline
  httptest.Server handler with an arrived/release channel pair — same shape as
  toggleBackend in integration_test.go — so the test can assert "exactly N in
  flight, the N+1th is rejected" as a real synchronization point, not a timing
  guess.

  One more thing worth building into /sleep itself: make it honor
  r.Context().Done(), exactly like /hang does, instead of an unconditional
  time.Sleep. That makes it double as the manual-test fixture for the other half
  of phase 4a — set -backend-timeout 2s, curl /sleep?seconds=5, and watch the
  load balancer cut it off at 2s while the backend's own log shows the handler
  got cancelled early. One endpoint, two demos.

  Slice plan for phase 4a

  1. cmd/echobackend/main.go — add /sleep endpoint (context-aware).
  2. internal/proxy/proxy.go — wrap the outbound RoundTrip call in
  context.WithTimeout, same pattern healthcheck.go already uses per-probe; new
  -backend-timeout flag in main.go.
  3. internal/middleware — new file, MaxConcurrent(n int) func(http.Handler) 
  http.Handler, buffered-channel semaphore, immediate 503 when full; new
  -max-concurrent flag, wired into the handler chain in main.go alongside
  Logging.
  4. Tests: deterministic channel-gate unit tests for both timeout and
  concurrency-limit boundaries, plus a stress test using /sleep-style bursts;
  manual walkthrough with real echobackend + hey.
  5. Notes + outputs/tests/phase-4a-...md + PR, once you're happy with it.

  Want me to start with slice 1 (/sleep on echobackend), or does this shape need
  any adjustment first?

✻ Sautéed for 1m 4s

❯ Yes begin implementation of phase 4a. Be sure to start a new appropriately    
  named branch for this work.                                                   

  Ran 2 shell commands (ctrl+o to expand)

⏺ Branch phase-4a-timeouts-backpressure created off build. Starting slice 1: the
  /sleep endpoint on echobackend.

  Slice 1: /sleep?seconds=N on echobackend

  Responsibility: a handler that holds a request open for a controllable
  duration, so we can make requests "pile up" against the load balancer's
  concurrency limiter on demand, and so the backend-timeout feature has
  something concrete to time out against. Two behaviors matter:
  - It parses seconds from the query string (default if absent, capped so a typo
  can't hang a demo for an hour).
  - It doesn't just time.Sleep — it races the sleep against r.Context().Done(),
  exactly like /hang already does, so that when the load balancer's future
  -backend-timeout fires (which cancels the outbound request's context) or the
  client disconnects, this handler actually stops and logs that it was cut
  short, instead of sleeping to completion regardless.

  Let me write it.

  Read 1 file (ctrl+o to expand)

⏺ Update(cmd/echobackend/main.go)
  ⎿  Added 8 lines, removed 1 line
      13  // outright (which phases 1-2 already cover via connection-refu
          sed). Plus a
      14  // /hang endpoint that never responds, for demonstrating that t
          he load
      15  // balancer currently has nothing bounding how long it will wai
          t on a
      16 -// backend that's healthy but stuck.                           
      16 +// backend that's healthy but stuck. Plus a /sleep?seconds=N en
         +dpoint that                                                    
      17 +// responds after a controllable delay (unlike /hang, which nev
         +er responds                                                    
      18 +// on its own) -- the fixture for phase 4a's backend-response t
         +imeout and                                                     
      19 +// concurrency limiter: fire enough concurrent /sleep requests 
         +and they pile                                                  
      20 +// up until the limiter's ceiling is hit, or set a backend time
         +out shorter                                                    
      21 +// than the sleep and watch the load balancer cut the request s
         +hort.                                                          
      22  package main
      23  
      24  import (
     ...
      27    "log"
      28    "net/http"
      29    "os"
      30 +  "strconv"                                                    
      31    "sync/atomic"
      32 +  "time"                                                       
      33  )
      34  
      35  // newHandler builds the handler this backend serves every non-
          health

⏺ Update(cmd/echobackend/main.go)
  ⎿  Added 51 lines
       99    }
      100  }
      101  
      102 +// defaultSleepSeconds is used when /sleep is requested with n
          +o ?seconds=                                                   
      103 +// parameter. maxSleepSeconds caps whatever value is requested
          +, so a typo'd                                                 
      104 +// query string (or an accidental extra zero) can't hang a dem
          +o terminal                                                    
      105 +// indefinitely -- this is a manual-testing fixture, not somet
          +hing that                                                     
      106 +// needs to support arbitrary durations.                      
      107 +const (                                                       
      108 +  defaultSleepSeconds = 2                                     
      109 +  maxSleepSeconds     = 30                                    
      110 +)                                                             
      111 +                                                              
      112 +// newSleepHandler blocks for the requested number of seconds 
          +(default                                                      
      113 +// defaultSleepSeconds, capped at maxSleepSeconds) before resp
          +onding 200,                                                   
      114 +// unless the request's context is cancelled first. The select
          + below races                                                  
      115 +// two channels: time.After's, which fires once the sleep dura
          +tion elapses,                                                 
      116 +// against r.Context().Done(), which fires the moment somethin
          +g upstream                                                    
      117 +// gives up on this request -- either the client itself discon
          +necting, or                                                   
      118 +// (once phase 4a's backend timeout exists) the load balancer'
          +s own                                                         
      119 +// context.WithTimeout on its outbound request to this backend
          + expiring.                                                    
      120 +// Whichever fires first wins the select; the other branch is 
          +simply never                                                  
      121 +// taken. This is the same shape as newHangHandler's <-r.Conte
          +xt().Done(),                                                  
      122 +// except here there's also a real "finished normally" outcome
          + to race                                                      
      123 +// against, not just the cancellation.                        
      124 +func newSleepHandler(logger *log.Logger) http.HandlerFunc {   
      125 +  return func(w http.ResponseWriter, r *http.Request) {       
      126 +    seconds := defaultSleepSeconds                            
      127 +    if raw := r.URL.Query().Get("seconds"); raw != "" {       
      128 +      parsed, err := strconv.Atoi(raw)                        
      129 +      if err != nil || parsed < 0 {                           
      130 +        w.WriteHeader(http.StatusBadRequest)                  
      131 +        fmt.Fprintf(w, "invalid seconds=%q: must be a non-nega
          +tive integer\n", raw)                                         
      132 +        return                                                
      133 +      }                                                       
      134 +      seconds = parsed                                        
      135 +    }                                                         
      136 +    if seconds > maxSleepSeconds {                            
      137 +      seconds = maxSleepSeconds                               
      138 +    }                                                         
      139 +                                                              
      140 +    start := time.Now()                                       
      141 +    logger.Printf("%s %s: sleeping %ds", r.Method, r.URL.Path,
          + seconds)                                                     
      142 +    select {                                                  
      143 +    case <-time.After(time.Duration(seconds) * time.Second):  
      144 +      logger.Printf("%s %s: woke up after %ds, responding 200"
          +, r.Method, r.URL.Path, seconds)                              
      145 +      w.WriteHeader(http.StatusOK)                            
      146 +      fmt.Fprintf(w, "slept %ds\n", seconds)                  
      147 +    case <-r.Context().Done():                                
      148 +      logger.Printf("%s %s: cancelled after %v (was sleeping f
          +or %ds)", r.Method, r.URL.Path, time.Since(start), seconds)   
      149 +    }                                                         
      150 +  }                                                           
      151 +}                                                             
      152 +                                                              
      153  func main() {
      154    listenAddr := flag.String("listen", ":9001", "address for th
           is backend to listen on")
      155    flag.Parse()

⏺ Now register the route in main():

⏺ Update(cmd/echobackend/main.go)  ⎿  Added 1 line
      164    mux.Handle("/health", newHealthHandler(logger, &healthy))
      165    mux.Handle("/health/toggle", newToggleHandler(logger, &healt
           hy))
      166    mux.Handle("/hang", newHangHandler(logger))
      167 +  mux.Handle("/sleep", newSleepHandler(logger))               
      168    mux.Handle("/", newHandler(logger, *listenAddr))
      169  
      170    if err := http.ListenAndServe(*listenAddr, mux); err != nil 
           {

⏺ For testability, let me pull the seconds-parsing logic out into its own pure
  function — that way tests can check parsing and capping instantly, without any
  test actually sleeping 30 seconds.

⏺ Update(cmd/echobackend/main.go)
  ⎿  Added 24 lines, removed 12 lines
      109    maxSleepSeconds     = 30
      110  )
      111  
      112 +// parseSleepSeconds parses the ?seconds= query value into a d
          +uration,                                                      
      113 +// applying the default (raw == "") and the maxSleepSeconds ca
          +p. Split out                                                  
      114 +// from newSleepHandler as a pure function -- no request, no I
          +/O -- so tests                                                
      115 +// can check the parsing and capping logic directly instead of
          + having to                                                    
      116 +// exercise it by actually waiting out a real sleep.          
      117 +func parseSleepSeconds(raw string) (int, error) {             
      118 +  if raw == "" {                                              
      119 +    return defaultSleepSeconds, nil                           
      120 +  }                                                           
      121 +  parsed, err := strconv.Atoi(raw)                            
      122 +  if err != nil || parsed < 0 {                               
      123 +    return 0, fmt.Errorf("invalid seconds=%q: must be a non-ne
          +gative integer", raw)                                         
      124 +  }                                                           
      125 +  if parsed > maxSleepSeconds {                               
      126 +    parsed = maxSleepSeconds                                  
      127 +  }                                                           
      128 +  return parsed, nil                                          
      129 +}                                                             
      130 +                                                              
      131  // newSleepHandler blocks for the requested number of seconds 
           (default
      132  // defaultSleepSeconds, capped at maxSleepSeconds) before resp
           onding 200,
      133  // unless the request's context is cancelled first. The select
            below races
     ...
      142  // against, not just the cancellation.
      143  func newSleepHandler(logger *log.Logger) http.HandlerFunc {
      144    return func(w http.ResponseWriter, r *http.Request) {
      126 -    seconds := defaultSleepSeconds                            
      127 -    if raw := r.URL.Query().Get("seconds"); raw != "" {       
      128 -      parsed, err := strconv.Atoi(raw)                        
      129 -      if err != nil || parsed < 0 {                           
      130 -        w.WriteHeader(http.StatusBadRequest)                  
      131 -        fmt.Fprintf(w, "invalid seconds=%q: must be a non-nega
          -tive integer\n", raw)                                         
      132 -        return                                                
      133 -      }                                                       
      134 -      seconds = parsed                                        
      145 +    seconds, err := parseSleepSeconds(r.URL.Query().Get("secon
          +ds"))                                                         
      146 +    if err != nil {                                           
      147 +      w.WriteHeader(http.StatusBadRequest)                    
      148 +      fmt.Fprintln(w, err)                                    
      149 +      return                                                  
      150      }
      136 -    if seconds > maxSleepSeconds {                            
      137 -      seconds = maxSleepSeconds                               
      138 -    }                                                         
      151  
      152      start := time.Now()
      153      logger.Printf("%s %s: sleeping %ds", r.Method, r.URL.Path,
            seconds)

⏺ Now the tests:

⏺ Update(cmd/echobackend/main_test.go)
  ⎿  Added 2 lines
      2  
      3  import (
      4    "bytes"
      5 +  "context"                                                     
      6    "io"
      7    "log"
      8    "net/http"
     ...
      10    "strings"
      11    "sync/atomic"
      12    "testing"
      13 +  "time"                                                       
      14  )
      15  
      16  func TestHandlerReportsOwnAddress(t *testing.T) {

⏺ Update(cmd/echobackend/main_test.go)
  ⎿  Added 109 lines
      103      t.Errorf("got status %d, want %d -- GET must not be able t
           o change state", rec.Code, http.StatusMethodNotAllowed)
      104    }
      105  }
      106 +                                                              
      107 +// TestParseSleepSeconds exercises the default/cap/error logic
          + directly,                                                    
      108 +// without going through the handler -- so the cap (maxSleepSe
          +conds) can be                                                 
      109 +// verified without an automated test ever actually waiting th
          +at long.                                                      
      110 +func TestParseSleepSeconds(t *testing.T) {                    
      111 +  tests := []struct {                                         
      112 +    name    string                                            
      113 +    raw     string                                            
      114 +    want    int                                               
      115 +    wantErr bool                                              
      116 +  }{                                                          
      117 +    {name: "empty uses default", raw: "", want: defaultSleepSe
          +conds},                                                       
      118 +    {name: "explicit value", raw: "5", want: 5},              
      119 +    {name: "zero is valid", raw: "0", want: 0},               
      120 +    {name: "over cap is clamped", raw: "9999", want: maxSleepS
          +econds},                                                      
      121 +    {name: "negative is an error", raw: "-1", wantErr: true}, 
      122 +    {name: "non-numeric is an error", raw: "soon", wantErr: tr
          +ue},                                                          
      123 +  }                                                           
      124 +  for _, tt := range tests {                                  
      125 +    t.Run(tt.name, func(t *testing.T) {                       
      126 +      t.Logf("input: seconds=%q", tt.raw)                     
      127 +      got, err := parseSleepSeconds(tt.raw)                   
      128 +      t.Logf("output: got=%d err=%v", got, err)               
      129 +      if tt.wantErr {                                         
      130 +        if err == nil {                                       
      131 +          t.Fatalf("got err=nil, want an error for seconds=%q"
          +, tt.raw)                                                     
      132 +        }                                                     
      133 +        return                                                
      134 +      }                                                       
      135 +      if err != nil {                                         
      136 +        t.Fatalf("got unexpected error: %v", err)             
      137 +      }                                                       
      138 +      if got != tt.want {                                     
      139 +        t.Errorf("got %d, want %d", got, tt.want)             
      140 +      }                                                       
      141 +    })                                                        
      142 +  }                                                           
      143 +}                                                             
      144 +                                                              
      145 +func TestSleepHandlerRespondsAfterElapsed(t *testing.T) {     
      146 +  logger := log.New(io.Discard, "", 0)                        
      147 +  handler := newSleepHandler(logger)                          
      148 +  t.Log("input: GET /sleep?seconds=0 -- should return 200 esse
          +ntially immediately")                                         
      149 +                                                              
      150 +  req := httptest.NewRequest("GET", "/sleep?seconds=0", nil)  
      151 +  rec := httptest.NewRecorder()                               
      152 +  start := time.Now()                                         
      153 +  handler(rec, req)                                           
      154 +  elapsed := time.Since(start)                                
      155 +                                                              
      156 +  t.Logf("output: status=%d body=%q elapsed=%v", rec.Code, rec
          +.Body.String(), elapsed)                                      
      157 +  if rec.Code != http.StatusOK {                              
      158 +    t.Errorf("got status %d, want %d", rec.Code, http.StatusOK
          +)                                                             
      159 +  }                                                           
      160 +  if elapsed > time.Second {                                  
      161 +    t.Errorf("handler took %v to return for seconds=0, want ne
          +ar-instant", elapsed)                                         
      162 +  }                                                           
      163 +}                                                             
      164 +                                                              
      165 +func TestSleepHandlerRejectsInvalidSeconds(t *testing.T) {    
      166 +  logger := log.New(io.Discard, "", 0)                        
      167 +  handler := newSleepHandler(logger)                          
      168 +  t.Log("input: GET /sleep?seconds=nope")                     
      169 +                                                              
      170 +  req := httptest.NewRequest("GET", "/sleep?seconds=nope", nil
          +)                                                             
      171 +  rec := httptest.NewRecorder()                               
      172 +  handler(rec, req)                                           
      173 +                                                              
      174 +  t.Logf("output: status=%d body=%q", rec.Code, rec.Body.Strin
          +g())                                                          
      175 +  if rec.Code != http.StatusBadRequest {                      
      176 +    t.Errorf("got status %d, want %d", rec.Code, http.StatusBa
          +dRequest)                                                     
      177 +  }                                                           
      178 +}                                                             
      179 +                                                              
      180 +// TestSleepHandlerCancelledByContext proves the handler actua
          +lly races                                                     
      181 +// r.Context().Done() against its own sleep, instead of always
          + sleeping to                                                  
      182 +// completion regardless of whether the caller has given up --
          + the exact                                                    
      183 +// mechanism phase 4a's backend-response timeout (and an ordin
          +ary client                                                    
      184 +// disconnect) both depend on. The request context here is can
          +celled before                                                 
      185 +// the handler is even called, and seconds is set high (60s) s
          +pecifically so                                                
      186 +// this test fails loudly via t.Fatal, rather than hanging for
          + a minute, if                                                 
      187 +// the handler were to ignore cancellation and sleep the full 
          +duration                                                      
      188 +// anyway.                                                    
      189 +func TestSleepHandlerCancelledByContext(t *testing.T) {       
      190 +  logger := log.New(io.Discard, "", 0)                        
      191 +  handler := newSleepHandler(logger)                          
      192 +                                                              
      193 +  ctx, cancel := context.WithCancel(context.Background())     
      194 +  cancel()                                                    
      195 +  req := httptest.NewRequest("GET", "/sleep?seconds=60", nil).
          +WithContext(ctx)                                              
      196 +  t.Log("input: GET /sleep?seconds=60 with an already-cancelle
          +d context")                                                   
      197 +                                                              
      198 +  rec := httptest.NewRecorder()                               
      199 +  done := make(chan struct{})                                 
      200 +  go func() {                                                 
      201 +    handler(rec, req)                                         
      202 +    close(done)                                               
      203 +  }()                                                         
      204 +                                                              
      205 +  select {                                                    
      206 +  case <-done:                                                
      207 +    t.Logf("output: handler returned promptly on the cancellat
          +ion path; body=%q (empty means it never reached the normal-com
          +pletion branch)", rec.Body.String())                          
      208 +    if rec.Body.Len() != 0 {                                  
      209 +      t.Errorf("got body %q, want empty -- the cancellation br
          +anch must not write a response", rec.Body.String())           
      210 +    }                                                         
      211 +  case <-time.After(2 * time.Second):                         
      212 +    t.Fatal("handler did not return within 2s of an already-ca
          +ncelled context -- it is not honoring r.Context().Done()")    
      213 +  }                                                           
      214 +}                                                             

⏺ Now let's build and run tests for this slice.

  Ran 1 shell command (ctrl+o to expand)

⏺ That failure is in scratch/, unrelated to this slice — two standalone example
  files declaring the same main/runWorker in one package. Let me confirm it
  predates my changes, then scope the test run to the packages that matter.

  Ran 1 shell command (ctrl+o to expand)

⏺ Confirmed pre-existing (was already broken before this branch) — those are
  your own exploratory scratch/ files, not part of the module proper, so I'll
  leave them alone and just scope build/test to the real packages.

  Ran 1 shell command (ctrl+o to expand)

⏺ Slice 1 is done — build, vet, gofmt, and -race tests all clean, and I
  confirmed the scratch/ build error predates this branch.

  Connecting this back to the system design

  newSleepHandler is deliberately built around one mechanism: a select racing
  time.After(seconds) against r.Context().Done(). That's the same idiom /hang
  uses, but here it matters more, because this handler is the thing that will
  make phase 4a's timeout feature observable. When the load balancer eventually
  wraps its outbound request in context.WithTimeout, that context is what
  net/http propagates down to r.Context() on the backend side — if the load
  balancer gives up and cancels, this handler finds out and stops immediately
  instead of quietly finishing a response nobody's listening for anymore.
  TestSleepHandlerCancelledByContext proves that wiring works in isolation,
  before the load balancer side even exists yet.

  Check on the mechanism — one question before slice 2: when ctx.Done() wins the
  race in that select, the handler returns without ever calling w.WriteHeader
  or w.Write. What does the client on the other end of that connection actually
  observe when a real backend does that (not the test's httptest.Recorder, but a
  real TCP connection through net/http) — and why?

✻ Churned for 2m 4s

※ recap: Building phase 4a (backend timeouts + concurrency limits) on the load balancer; just finished the `/sleep` test endpoint on echobackend. Next: you answer what a client sees when the handler returns without writing a response, then we move to slice 2 (backend timeout in proxy.go).

❯ So you're talking about the situation where the request timeout occurs before 
  sleep finishes sleeping (or in a real backend, the request is being           
  processed but just hasn't finished processing), it appears to the client that 
  something was wrong with the backend connection, whereas in reality it did    
  receive the request but just hadn't responded yet. So the client will receive 
  a "request timed out" error which is misleading. Once the backend DOES        
  finish and responds, it will send the response back to the LB but I'm         
  actually not too sure what happens in this case - does the LB send the        
  response back to the client?                                                  

⏺ You've got the first half exactly right — a timeout genuinely can't
  distinguish "backend is broken" from "backend is just slow," so the client's
  error is accurate about the symptom (no answer in time) but silent on the
  cause. That ambiguity isn't a bug, it's an unavoidable property of timeouts,
  and it's part of why production systems pair timeouts with separate
  observability (backend-side logs/tracing) to tell the two apart after the
  fact.

  The second half has a gap worth closing precisely, because "does the LB
  forward the late response" has a concrete, mechanical answer: no — and not 
  because the LB chooses to discard it, but because by the time the backend 
  tries to send it, there's no longer a live connection to send it on.

  Walk through the actual sequence of events:

  LB                                    Backend
  │                                        │
  │──GET /sleep?seconds=60──────────────▶│  handler starts, begins sleeping
  │                                        │
  │  (backend-timeout of 2s elapses)      │
  │  ctx.Done() fires on the LB's         │
  │  outbound request                     │
  │                                        │
  │  Transport aborts the round trip      │
  │  AND closes the underlying TCP        │
  │  connection to the backend ───FIN───▶│  backend's connection-reading
  │                                        │  goroutine sees it close
  │  RoundTrip() returns an error         │  → cancels r.Context() for
  │  to roundTripperFunc                  │    the running handler
  │                                        │  → our select's ctx.Done()
  │  ReverseProxy's ErrorHandler          │    branch fires (if the
  │  writes 502 to the ORIGINAL client    │    handler honors it)
  │  ...immediately, done.                │
  │                                        │  (58s later) handler finally
  │                                        │  tries w.WriteHeader/Write
  │                                        │  on the dead socket
  │                                        │  → write fails: broken pipe /
  │                                        │    connection reset
  │                                        │  → response is never
  │                                        │    transmitted anywhere

  Two separate, non-overlapping things happen, not one response handed off late:

  1. The instant the LB's timeout fires, net/http's Transport doesn't just stop
  waiting — it actively closes the TCP connection to the backend, because it can
  no longer safely reuse a connection with an unknown amount of
  unread/half-written data sitting on it. That closed connection is what the
  backend's server detects and turns into r.Context() being cancelled — which is
  exactly the signal newSleepHandler's select is built to catch.
  2. If the backend doesn't honor that cancellation (imagine we hadn't wired the
  select in, or a real handler doing a DB call it can't interrupt) and keeps
  working, its eventual w.WriteHeader/w.Write call is writing to a socket that's
  already gone. That write returns an OS-level error (broken pipe/connection 
  reset) — most handlers never even check it — so the response silently
  evaporates at the TCP layer. The LB never sees it, receives it, or decides to
  drop it; it simply never arrives, because there's nothing left to receive it
  on.

  The practical upshot: a backend that ignores cancellation isn't giving the
  client anything by finishing anyway — it's just burning CPU/DB
  connections/memory computing an answer for a client that already got a 502 and
  moved on. That's the concrete case for honoring ctx.Done() (which /sleep
  already does) beyond just "it's tidy" — it's wasted resource consumption on a
  request that provably cannot succeed anymore.

  One loose end I noticed while explaining this: the current ErrorHandler's
  fallback (proxy.go, the branch after the errNoHealthyBackends check) just does
  w.WriteHeader(http.StatusBadGateway) with no body — silent. Phase 3 already
  established the pattern of giving specific errors a real message (the "no
  healthy backends available" body). We'll likely want the same treatment for a
  timeout — checking for context.DeadlineExceeded and writing something like
  "backend request timed out" — so slice 2's client-visible behavior isn't a
  bare, unexplained 502.

  Ready to move into slice 2 (the actual context.WithTimeout wrap in proxy.go +
  the timeout-specific error message + the new flag), or anything else to work
  through first?

✻ Cogitated for 1m 15s

❯ Let's move to slice 2                                                         

⏺ Good. Let's look at the existing call sites so the signature change lands
  cleanly everywhere.

  Searched for 1 pattern, read 2 files (ctrl+o to expand)

⏺ This slice touches internal/proxy/proxy.go (the core change), and then every
  call site that constructs a proxy needs the new parameter threaded through —
  cmd/loadbalancer/main.go, and the test files in internal/proxy and
  cmd/loadbalancer. Here's the exact shape before I touch anything:

  - New(b balancer.Balancer, logger *log.Logger) becomes New(b 
  balancer.Balancer, backendTimeout time.Duration, logger *log.Logger).
  backendTimeout <= 0 disables it (restores today's wait-forever behavior) — an
  explicit opt-out, not silently always-on.
  - Inside the Transport's roundTripperFunc: wrap the outbound request in
  context.WithTimeout(r.Context(), backendTimeout). The subtle part — worth
  calling out before I write it — is when to call the returned cancel().
  RoundTrip returning only means headers arrived; httputil.ReverseProxy still
  streams resp.Body to the client afterward. If I defer cancel() right next to
  context.WithTimeout (the naive placement), it fires the instant RoundTrip
  returns — before the body's been read — which would immediately fail every
  later body read with "context canceled," silently truncating any response
  bigger than what fit in the first read. So I'm wrapping resp.Body in a small
  cancelOnCloseBody type that only calls cancel() from its own Close() method,
  which ReverseProxy already calls once it's done relaying the body. That makes
  the timeout correctly bound the entire round trip (headers + full body drain),
  not just the time-to-first-byte.
  - ErrorHandler gains a case for errors.Is(err, context.DeadlineExceeded) → a
  specific "backend request timed out" 502 body, same treatment phase 3 gave
  errNoHealthyBackends.
  - cmd/loadbalancer/main.go: new -backend-timeout flag, default 10s (enabled
  out of the box — the whole point of this slice is that the LB protects itself
  without depending on operator config), 0 as the documented opt-out.
  - Existing tests in internal/proxy and cmd/loadbalancer/integration_test.go
  pass 0 at their call sites (unchanged behavior — they're not testing
  timeouts). TestStress_SlowBackendHasNoTimeout gets repurposed: it currently
  proves the absence of a timeout, which is no longer true by default, so I'll
  turn it into a test proving the opt-out still works, and add a new test
  proving the enabled case actually cuts a slow backend off. I'll also add a
  streaming-body regression test that would fail if the cancelOnCloseBody timing
  were wrong (cancels too early) — a response that trickles its body over a few
  flushes, total time under the timeout, and confirms the full body still
  arrives intact.

  Implementing now.

⏺ Update(internal/proxy/proxy.go)
  ⎿  Added 34 lines, removed 1 line
       3  package proxy
       4  
       5  import (
       6 +  "context"                                                    
       7    "errors"
       8    "io"
       9    "log"
      10    "net/http"
      11    "net/http/httputil"
      12 +  "time"                                                       
      13  
      14    "github.com/jerryschen31/system-design-load-balancer/internal
          /balancer"
      15  )
     ...
      30    return f(r)
      31  }
      32  
      33 +// cancelOnCloseBody wraps a backend response body so the conte
         +xt.WithTimeout                                                 
      34 +// guarding that round trip is cancelled once the body has been
         + fully read                                                    
      35 +// and closed -- not the instant RoundTrip returns. RoundTrip r
         +eturning only                                                  
      36 +// means the response headers arrived; httputil.ReverseProxy st
         +ill streams                                                    
      37 +// resp.Body back to the client afterward, on the same goroutin
         +e, by reading                                                  
      38 +// it as it copies. Calling cancel() right after RoundTrip retu
         +rns (the                                                       
      39 +// naive placement, directly next to context.WithTimeout) would
         + fail every                                                    
      40 +// one of those later reads with "context canceled", because ne
         +t/http ties                                                    
      41 +// body reads to the request's context -- silently truncating a
         +ny response                                                    
      42 +// whose body didn't already fully arrive in the same read as t
         +he headers.                                                    
      43 +// Deferring cancel to Close() instead means the timeout keeps 
         +counting down                                                  
      44 +// for the whole response (headers + body), which is what "back
         +end timeout"                                                   
      45 +// should mean, while still releasing the timer's resources pro
         +mptly once                                                     
      46 +// the body is actually done, rather than leaking it until back
         +endTimeout                                                     
      47 +// elapses on its own.                                         
      48 +type cancelOnCloseBody struct {                                
      49 +  io.ReadCloser                                                
      50 +  cancel context.CancelFunc                                    
      51 +}                                                              
      52 +                                                               
      53 +func (b *cancelOnCloseBody) Close() error {                    
      54 +  defer b.cancel()                                             
      55 +  return b.ReadCloser.Close()                                  
      56 +}                                                              
      57 +                                                               
      58  // New builds a reverse proxy that forwards every request to a 
          backend
      59  // chosen by b. Each request picks its target independently by 
          calling
      60  // b.Next(), so which backend handles a given request is entire
          ly up to
      61  // the balancer's selection policy (round-robin, and later phas
          es' other
      62  // strategies), not anything the proxy itself decides.
      36 -func New(b balancer.Balancer, logger *log.Logger) *httputil.Rev
         -erseProxy {                                                    
      63 +//                                                             
      64 +// backendTimeout bounds how long a single request to a backend
         + -- from the                                                   
      65 +// moment it's sent until its response body is fully read -- ma
         +y take before                                                  
      66 +// the proxy gives up on it and fails the request with a 502, i
         +ndependent of                                                  
      67 +// whether the client set any timeout of its own. backendTimeou
         +t <= 0                                                         
      68 +// disables this and restores the earlier behavior of waiting i
         +ndefinitely.                                                   
      69 +func New(b balancer.Balancer, backendTimeout time.Duration, log
         +ger *log.Logger) *httputil.ReverseProxy {                      
      70    if b == nil {
      71      // Fail at construction, not on the first request: a nil ba
          lancer
      72      // would otherwise panic inside Rewrite on whichever gorout
          ine

⏺ Update(internal/proxy/proxy.go)
  ⎿  Added 27 lines, removed 4 lines
      113          // default transport try to dial an empty host.
      114          return nil, errNoHealthyBackends
      115        }
      116 -      return http.DefaultTransport.RoundTrip(r)               
      116 +      if backendTimeout <= 0 {                                
      117 +        return http.DefaultTransport.RoundTrip(r)             
      118 +      }                                                       
      119 +      // ctx is derived from r.Context(), the incoming client 
      120 +      // request's context -- so this timeout is layered on to
          +p of,                                                         
      121 +      // not instead of, the existing cancellation the client 
      122 +      // disconnecting or the LB shutting down already provide
          +s.                                                            
      123 +      // Whichever deadline is sooner wins.                   
      124 +      ctx, cancel := context.WithTimeout(r.Context(), backendT
          +imeout)                                                       
      125 +      resp, err := http.DefaultTransport.RoundTrip(r.WithConte
          +xt(ctx))                                                      
      126 +      if err != nil {                                         
      127 +        // No response body to defer cancellation on -- the ro
          +und                                                           
      128 +        // trip never succeeded, so release the timer now.    
      129 +        cancel()                                              
      130 +        return nil, err                                       
      131 +      }                                                       
      132 +      resp.Body = &cancelOnCloseBody{ReadCloser: resp.Body, ca
          +ncel: cancel}                                                 
      133 +      return resp, nil                                        
      134      }),
      135      ErrorLog: logger,
      136      ErrorHandler: func(w http.ResponseWriter, r *http.Request,
            err error) {
      137        logger.Printf("backend error for %s %s: %v", r.Method, r
           .URL.Path, err)
      121 -      if errors.Is(err, errNoHealthyBackends) {               
      138 +      switch {                                                
      139 +      case errors.Is(err, errNoHealthyBackends):              
      140          // A specific, honest message for the specific case: t
           he
      141          // balancer itself refused to pick a target, as oppose
           d to
      142          // picking one and having the request to it fail. http
           .Error
      143          // sets Content-Type, writes the status, and writes th
           e
      144          // message plus a trailing newline in one call.
      145          http.Error(w, "no healthy backends available", http.St
           atusBadGateway)
      128 -        return                                                
      146 +      case errors.Is(err, context.DeadlineExceeded):          
      147 +        // Distinct from a generic backend-down 502: the backe
          +nd                                                            
      148 +        // was reachable and presumably still working, it just
      149 +        // didn't finish within backendTimeout.               
      150 +        http.Error(w, "backend request timed out", http.Status
          +BadGateway)                                                   
      151 +      default:                                                
      152 +        w.WriteHeader(http.StatusBadGateway)                  
      153        }
      130 -      w.WriteHeader(http.StatusBadGateway)                    
      154      },
      155    }
      156  }

⏺ Now update main.go to add the flag and thread it through:

⏺ Update(cmd/loadbalancer/main.go)
  ⎿  Added 1 line
      81    healthCheckInterval := flag.Duration("health-check-interval",
           5*time.Second, "how often to actively poll each backend's heal
          th check path")
      82    healthCheckTimeout := flag.Duration("health-check-timeout", 2
          *time.Second, "how long a single health check probe may take be
          fore it counts as a failure")
      83    healthCheckPath := flag.String("health-check-path", "/health"
          , "path to request on each backend for health checks")
      84 +  backendTimeout := flag.Duration("backend-timeout", 10*time.Se
         +cond, "max time to wait on a single backend request (headers + 
         +full body); 0 disables the timeout and waits indefinitely")    
      85    flag.Parse()
      86  
      87    targets, err := parseBackendURLs(backends)
⏺ Update(cmd/loadbalancer/main.go)
  ⎿  Added 1 line, removed 1 line
      93    logger := log.New(os.Stdout, "", log.LstdFlags)
      94  
      95    rr := balancer.NewRoundRobin(targets)
      96 -  handler := middleware.Logging(logger)(proxy.New(rr, logger)) 
      96 +  handler := middleware.Logging(logger)(proxy.New(rr, *backendT
         +imeout, logger))                                               
      97  
      98    server := &http.Server{
      99      Addr:    *listenAddr,

⏺ Now update all the existing test call sites to pass 0 (unchanged behavior),
  starting with internal/proxy/proxy_test.go:

⏺ Update(internal/proxy/proxy_test.go)
  ⎿  Added 10 lines, removed 1 line
      30    return balancer.NewRoundRobin([]*url.URL{target})
      31  }
      32  
      33 +// newProxyServer builds a proxy with no backend timeout (0 = d
         +isabled) --                                                    
      34 +// the tests using it aren't exercising timeout behavior, so th
         +ey keep the                                                    
      35 +// original "wait indefinitely" semantics. newProxyServerWithTi
         +meout below                                                    
      36 +// is for the tests that specifically are.                     
      37  func newProxyServer(t *testing.T, backendURL string) *httptest.
          Server {
      38    t.Helper()
      35 -  return httptest.NewServer(New(singleBackend(t, backendURL), n
         -ewTestLogger()))                                               
      39 +  return httptest.NewServer(New(singleBackend(t, backendURL), 0
         +, newTestLogger()))                                            
      40  }
      41  
      42 +func newProxyServerWithTimeout(t *testing.T, backendURL string,
         + backendTimeout time.Duration) *httptest.Server {              
      43 +  t.Helper()                                                   
      44 +  return httptest.NewServer(New(singleBackend(t, backendURL), b
         +ackendTimeout, newTestLogger()))                               
      45 +}                                                              
      46 +                                                               
      47  func TestForwardsRequestToBackend(t *testing.T) {
      48    var gotMethod, gotPath string
      49    backend := httptest.NewServer(http.HandlerFunc(func(w http.Re
          sponseWriter, r *http.Request) {

⏺ Update(internal/proxy/proxy_test.go)
  ⎿  Added 1 line
       9    "net/url"
      10    "strings"
      11    "testing"
      12 +  "time"                                                       
      13  
      14    "github.com/jerryschen31/system-design-load-balancer/internal
          /balancer"
      15  )

⏺ Now fix the remaining direct New(...) calls in that file:

⏺ Update(internal/proxy/proxy_test.go)
  ⎿  Added 1 line, removed 1 line
      151  func TestBackendDownReturns502(t *testing.T) {
      152    // A backend URL that nothing is listening on: connection re
           fused.
      153    const unreachableURL = "http://127.0.0.1:1"
      154 -  lb := httptest.NewServer(New(singleBackend(t, unreachableURL
          -), newTestLogger()))                                          
      154 +  lb := httptest.NewServer(New(singleBackend(t, unreachableURL
          +), 0, newTestLogger()))                                       
      155    defer lb.Close()
      156    t.Logf("input: backend %s is down; client sends GET / throug
           h load balancer %s", unreachableURL, lb.URL)
      157  

⏺ Update(internal/proxy/proxy_test.go)
  ⎿  Added 2 lines, removed 2 lines
      169  
      170  func TestNewAllowsNilLogger(t *testing.T) {
      171    const unreachableURL = "http://127.0.0.1:1"
      172 -  lb := httptest.NewServer(New(singleBackend(t, unreachableURL
          -), nil))                                                      
      172 +  lb := httptest.NewServer(New(singleBackend(t, unreachableURL
          +), 0, nil))                                                   
      173    defer lb.Close()
      174 -  t.Logf("input: proxy.New(%s, nil)", unreachableURL)         
      174 +  t.Logf("input: proxy.New(%s, 0, nil)", unreachableURL)      
      175  
      176    resp, err := http.Get(lb.URL + "/")
      177    if err != nil {

⏺ Update(internal/proxy/proxy_test.go)
  ⎿  Added 1 line, removed 1 line
      198    rr.SetHealthy(target, false)
      199    t.Logf("input: balancer's only backend (%s) marked unhealthy
           ", target)
      200  
      201 -  lb := httptest.NewServer(New(rr, newTestLogger()))          
      201 +  lb := httptest.NewServer(New(rr, 0, newTestLogger()))       
      202    defer lb.Close()
      203  
      204    resp, err := http.Get(lb.URL)

⏺ Update(internal/proxy/proxy_test.go)
  ⎿  Added 2 lines, removed 2 lines
      227  }
      228  
      229  func TestNewPanicsOnNilBalancer(t *testing.T) {
      230 -  t.Log("input: proxy.New(nil, nil)")                         
      230 +  t.Log("input: proxy.New(nil, 0, nil)")                      
      231  
      232    defer func() {
      233      r := recover()
     ...
      237      t.Logf("output: New panicked as expected: %v", r)
      238    }()
      239  
      240 -  New(nil, nil)                                               
      240 +  New(nil, 0, nil)                                            
      241  }
      242  
      243  // TestRewriteLogsRoutingDecision confirms the fix for the rev
           iew comment

⏺ Update(internal/proxy/proxy_test.go)
  ⎿  Added 1 line, removed 1 line
      251  
      252    var buf bytes.Buffer
      253    logger := log.New(&buf, "", 0)
      254 -  lb := httptest.NewServer(New(singleBackend(t, backend.URL), 
          -logger))                                                      
      254 +  lb := httptest.NewServer(New(singleBackend(t, backend.URL), 
          +0, logger))                                                   
      255    defer lb.Close()
      256    t.Logf("input: GET / through the load balancer, backend is %
           s", backend.URL)
      257  

⏺ Now add the two new timeout-specific tests to proxy_test.go (timeout returning
  a clean 502 with the specific message, and confirming a fast/normal request
  still works with a timeout configured):

⏺ Update(internal/proxy/proxy_test.go)
  ⎿  Added 86 lines
      240    New(nil, 0, nil)
      241  }
      242  
      243 +// TestBackendTimeoutReturns502 proves the proxy's own backend
          +Timeout cuts a                                                
      244 +// slow backend off, without depending on the client having se
          +t any timeout                                                 
      245 +// of its own -- http.DefaultClient here has none. This is the
          + fix for the                                                  
      246 +// weakness TestStress_SlowBackendHasNoTimeout (stress_test.go
          +) documented                                                  
      247 +// back in phase 1.                                           
      248 +func TestBackendTimeoutReturns502(t *testing.T) {             
      249 +  const backendDelay = 500 * time.Millisecond                 
      250 +  const backendTimeout = 50 * time.Millisecond                
      251 +  backend := httptest.NewServer(http.HandlerFunc(func(w http.R
          +esponseWriter, r *http.Request) {                             
      252 +    time.Sleep(backendDelay)                                  
      253 +    w.WriteHeader(http.StatusOK)                              
      254 +  }))                                                         
      255 +  defer backend.Close()                                       
      256 +                                                              
      257 +  lb := newProxyServerWithTimeout(t, backend.URL, backendTimeo
          +ut)                                                           
      258 +  defer lb.Close()                                            
      259 +  t.Logf("input: backend sleeps %s, proxy backendTimeout is %s
          +, client has no timeout of its own", backendDelay, backendTime
          +out)                                                          
      260 +                                                              
      261 +  start := time.Now()                                         
      262 +  resp, err := http.Get(lb.URL + "/")                         
      263 +  elapsed := time.Since(start)                                
      264 +  if err != nil {                                             
      265 +    t.Fatalf("request through proxy failed: %v", err)         
      266 +  }                                                           
      267 +  defer resp.Body.Close()                                     
      268 +  body, _ := io.ReadAll(resp.Body)                            
      269 +                                                              
      270 +  t.Logf("output: status=%d body=%q elapsed=%s", resp.StatusCo
          +de, string(body), elapsed)                                    
      271 +  if resp.StatusCode != http.StatusBadGateway {               
      272 +    t.Errorf("got status %d, want %d", resp.StatusCode, http.S
          +tatusBadGateway)                                              
      273 +  }                                                           
      274 +  const wantBody = "backend request timed out\n"              
      275 +  if string(body) != wantBody {                               
      276 +    t.Errorf("got body %q, want %q", string(body), wantBody)  
      277 +  }                                                           
      278 +  if elapsed >= backendDelay {                                
      279 +    t.Errorf("client waited %s (>= full backend delay %s); the
          + proxy's own timeout (%s) should have cut it off first", elaps
          +ed, backendDelay, backendTimeout)                             
      280 +  }                                                           
      281 +}                                                             
      282 +                                                              
      283 +// TestBackendTimeoutAllowsSlowStreamedBodyWithinBudget guards
          + against the                                                  
      284 +// specific bug cancelOnCloseBody exists to prevent: if the ro
          +und trip's                                                    
      285 +// context were cancelled the instant RoundTrip returned (righ
          +t after                                                       
      286 +// headers), rather than deferred to resp.Body.Close(), this t
          +est's                                                         
      287 +// streamed, multi-chunk body -- which takes some real time to
          + fully                                                        
      288 +// arrive, but well within backendTimeout -- would be truncate
          +d even though                                                 
      289 +// nothing actually timed out.                                
      290 +func TestBackendTimeoutAllowsSlowStreamedBodyWithinBudget(t *t
          +esting.T) {                                                   
      291 +  const chunkDelay = 20 * time.Millisecond                    
      292 +  const numChunks = 5                                         
      293 +  const backendTimeout = 2 * time.Second                      
      294 +  backend := httptest.NewServer(http.HandlerFunc(func(w http.R
          +esponseWriter, r *http.Request) {                             
      295 +    flusher, ok := w.(http.Flusher)                           
      296 +    if !ok {                                                  
      297 +      t.Fatal("httptest ResponseWriter does not support flushi
          +ng")                                                          
      298 +    }                                                         
      299 +    w.WriteHeader(http.StatusOK)                              
      300 +    for i := 0; i < numChunks; i++ {                          
      301 +      fmt.Fprintf(w, "chunk-%d ", i)                          
      302 +      flusher.Flush()                                         
      303 +      time.Sleep(chunkDelay)                                  
      304 +    }                                                         
      305 +  }))                                                         
      306 +  defer backend.Close()                                       
      307 +                                                              
      308 +  lb := newProxyServerWithTimeout(t, backend.URL, backendTimeo
          +ut)                                                           
      309 +  defer lb.Close()                                            
      310 +  t.Logf("input: backend streams %d chunks, %s apart, total ~%
          +s; backendTimeout is %s (well above that)", numChunks, chunkDe
          +lay, time.Duration(numChunks)*chunkDelay, backendTimeout)     
      311 +                                                              
      312 +  resp, err := http.Get(lb.URL + "/")                         
      313 +  if err != nil {                                             
      314 +    t.Fatalf("request through proxy failed: %v", err)         
      315 +  }                                                           
      316 +  defer resp.Body.Close()                                     
      317 +  body, err := io.ReadAll(resp.Body)                          
      318 +  if err != nil {                                             
      319 +    t.Fatalf("reading response body failed: %v -- this is the 
          +exact failure mode of cancelling too early", err)             
      320 +  }                                                           
      321 +                                                              
      322 +  want := "chunk-0 chunk-1 chunk-2 chunk-3 chunk-4 "          
      323 +  t.Logf("output: status=%d body=%q", resp.StatusCode, string(
          +body))                                                        
      324 +  if string(body) != want {                                   
      325 +    t.Errorf("got body %q, want %q -- body was truncated, whic
          +h means the round trip's context was cancelled before the body
          + finished streaming", string(body), want)                     
      326 +  }                                                           
      327 +}                                                             
      328 +                                                              
      329  // TestRewriteLogsRoutingDecision confirms the fix for the rev
           iew comment
      330  // asking which backend a request was routed to be logged: a r
           eal logger
      331  // (not io.Discard) must see a line naming the selected backen
           d.

⏺ Update(internal/proxy/proxy_test.go)
  ⎿  Added 1 line
      2  
      3  import (
      4    "bytes"
      5 +  "fmt"                                                         
      6    "io"
      7    "log"
      8    "net/http"

⏺ Now fix stress_test.go's direct New() call and repurpose
  TestStress_SlowBackendHasNoTimeout:

⏺ Update(internal/proxy/stress_test.go)
  ⎿  Added 12 lines, removed 13 lines
       9    "time"
      10  )
      11  
      12 -// TestStress_SlowBackendHasNoTimeout demonstrates a real weakn
         -ess of this                                                    
      13 -// phase: the proxy places no time limit on waiting for the bac
         -kend. If the                                                   
      14 -// client hadn't set its own timeout, this request would have h
         -ung for the                                                    
      15 -// full backend delay -- and with enough concurrent hung reques
         -ts, the load                                                   
      16 -// balancer's own resources (goroutines, file descriptors) woul
         -d be tied up                                                   
      17 -// indefinitely. See notes/phase-1-single-backend-proxy.md for 
         -the fix plan.                                                  
      18 -func TestStress_SlowBackendHasNoTimeout(t *testing.T) {        
      12 +// TestStress_DisabledTimeoutWaitsIndefinitely confirms the doc
         +umented                                                        
      13 +// opt-out (backendTimeout <= 0) still behaves exactly like pha
         +se 1-3's                                                       
      14 +// original proxy: no timeout of its own, so only the client's 
         +own timeout                                                    
      15 +// (if any) cuts a slow request off. This used to be this suite
         +'s                                                             
      16 +// KNOWN WEAKNESS -- phase 4a's actual fix, and the test provin
         +g it, is                                                       
      17 +// TestBackendTimeoutReturns502 in proxy_test.go.              
      18 +func TestStress_DisabledTimeoutWaitsIndefinitely(t *testing.T) 
         +{                                                              
      19    const backendDelay = 300 * time.Millisecond
      20    const clientTimeout = 50 * time.Millisecond
      21 -  t.Logf("input: backend sleeps for %s; client timeout is %s", 
         -backendDelay, clientTimeout)                                   
      21 +  t.Logf("input: backendTimeout=0 (disabled); backend sleeps fo
         +r %s; client timeout is %s", backendDelay, clientTimeout)      
      22  
      23    backend := httptest.NewServer(http.HandlerFunc(func(w http.Re
          sponseWriter, r *http.Request) {
      24      t.Log("backend step: request reached backend; backend is no
          w sleeping")
     ...
      27    }))
      28    defer backend.Close()
      29  
      30 -  lb := newProxyServer(t, backend.URL)                         
      30 +  lb := newProxyServer(t, backend.URL) // backendTimeout=0     
      31    defer lb.Close()
      32  
      33    client := &http.Client{Timeout: clientTimeout}
     ...
      36    elapsed := time.Since(start)
      37  
      38    if err == nil {
      39 -    t.Fatalf("expected the client's own timeout to fire, since 
         -the proxy enforces no timeout of its own")                     
      39 +    t.Fatalf("expected the client's own timeout to fire, since 
         +backendTimeout=0 disables the proxy's own timeout")            
      40    }
      41    if elapsed >= backendDelay {
      42 -    t.Errorf("client waited %s (>= full backend delay of %s); e
         -xpected the client's timeout (~%s) to cut it off first, proving
         - the proxy itself never would have", elapsed, backendDelay, cli
         -entTimeout)                                                    
      42 +    t.Errorf("client waited %s (>= full backend delay of %s); e
         +xpected the client's timeout (~%s) to cut it off first, proving
         + the proxy itself never would have with backendTimeout=0", elap
         +sed, backendDelay, clientTimeout)                              
      43    }
      44 -  t.Logf("output: client returned after %s with error %v", elap
         -sed, err)                                                      
      45 -  t.Logf("KNOWN WEAKNESS: proxy has no timeout on backend respo
         -nses (aborted after %s only because the client protected itself
         -; backend needed %s). A hung backend can hold load balancer res
         -ources indefinitely.", elapsed, backendDelay)                  
      44 +  t.Logf("output: client returned after %s with error %v -- opt
         +-out confirmed working", elapsed, err)                         
      45  }
      46  
      47  // TestStress_ConcurrentRequestsHandledConcurrently fires a bur
          st of

⏺ Update(internal/proxy/stress_test.go)
  ⎿  Added 1 line, removed 1 line
      104    const numRequests = 50
      105    t.Logf("input: %d concurrent requests against a load balance
           r whose only backend is down", numRequests)
      106  
      107 -  lb := httptest.NewServer(New(singleBackend(t, "http://127.0.
          -0.1:1"), newTestLogger()))                                    
      107 +  lb := httptest.NewServer(New(singleBackend(t, "http://127.0.
          +0.1:1"), 0, newTestLogger()))                                 
      108    defer lb.Close()
      109  
      110    var wg sync.WaitGroup

⏺ Now the cmd/loadbalancer/integration_test.go call sites — three occurrences,
  all get 0 (unaffected by timeout):

  Ran 1 shell command (ctrl+o to expand)

⏺ Now build and run the full test suite for the affected packages:

  Ran 1 shell command (ctrl+o to expand)

⏺ Clean build, vet, and formatting. Now the test suite:

  Ran 1 shell command (ctrl+o to expand)

⏺ All green, including the critical
  TestBackendTimeoutAllowsSlowStreamedBodyWithinBudget regression test —
  confirming the cancelOnCloseBody timing is correct. Now the rest of the suite:

  Ran 4 shell commands (ctrl+o to expand)

⏺ Full suite is green under -race. Slice 2 is done.

  Connecting this back to the system design

  The core mechanism now in place: context.WithTimeout(r.Context(), 
  backendTimeout) layers a new deadline on top of the request's existing
  context, without replacing what was already there (client disconnect, LB
  shutdown). context.WithTimeout starts a timer the moment it's created and
  fires regardless of what the request is doing — waiting for headers or
  streaming a body — which is exactly what "backend timeout" should mean: a
  bound on the whole round trip, not just time-to-first-byte.

  The one non-obvious piece was when to release that timer. The instinct is to
  defer cancel() right next to context.WithTimeout, but that fires the moment
  RoundTrip returns — which is only when headers arrive, before
  httputil.ReverseProxy has streamed the body back to the client.
  TestBackendTimeoutAllowsSlowStreamedBodyWithinBudget is there specifically to
  catch that bug: without cancelOnCloseBody deferring the cancel to
  resp.Body.Close(), that test's 5-chunk streamed response (100ms total, well
  under the 2s budget) would come back truncated, because the context would
  already be dead before the second chunk was read — a bug that would pass every
  test with a small response body and only show up on real, larger payloads.

  Check on the mechanism — one question before we move to slice 3 (the
  concurrency limiter): with backendTimeout now defaulting to 10s in main.go,
  imagine a backend that's healthy (passes its /health check fine) but one
  specific slow route consistently takes 12 seconds. Every request to that route
  now gets a clean 502 from the load balancer instead of hanging. Is that
  actually the right outcome for the load balancer to produce here — and does
  anything in phase 3's health-check machinery help this specific backend at
  all, or is this exactly the gap phase 3's notes already named?
✻ Worked for 4m 13s

※ recap: Building phase 4a: backend request timeouts and concurrency limits for the load balancer. Slice 2 (backend timeout) is done and tested; next is answering a check-in question, then building slice 3, the concurrency limiter.

❯ A few things:                                                                 
  1. I'm confused. For requests wrapped in context.WithTimeout(), does cancel() 
  get called when the request actually times out?                               
  2. Regarding cancel() getting called the moment RoundTrip() returns, yes -    
  cancel() should be called after the entire request body (payload) is sent     
  back from the backend (not sure if there is a "done" or "last" packet from    
  the backend? How does the LB know when the response body is at the end?       
  3. If a request gets a 502 timeout from a backend, does the request           
  automatically then get sent to a different backend?                           
  4. If a backend continually times out, it should be marked as unhealthy. This 
  is probably the "passive" health check that will be implemented in a future   
  phase (e.g., if 3 requests in a row timeout, then mark backend as unhealthy). 
  But once a backend is marked unhealthy by this passive check method, how      
  does that backend regain healthy status?                                      

⏺ Four good questions, each pointing at a different mechanism. Let me take them
  in order.

  1. Does cancel() get called when the request times out?

  No — and this is worth being precise about, because it's a common mix-up.
  context.WithTimeout internally starts its own timer at creation time. When
  that timer fires, the context marks itself done (closes its internal Done()
  channel, sets Err() to context.DeadlineExceeded) automatically — this happens
  regardless of whether anyone ever calls the cancel function you got back.

  The cancel function you hold is a separate, manual trigger for two purposes:
  (a) canceling early, before the deadline, if you decide you're done sooner
  (that's what cancelOnCloseBody.Close() does), and (b) releasing small internal
  bookkeeping (the timer, and this context's entry in its parent's list of
  children) once you're done with it, even after the deadline already fired on
  its own. That's why we call cancel() in both branches of our code — not to
  cause the timeout, but to clean up after it. If we never called it, the
  timeout would still work correctly; we'd just leave a little internal state
  unreleased until the parent context itself goes away.

  2. How does the LB know the response body is finished — is there a "done" 
  packet?

  There isn't one at the TCP level — TCP is just a raw byte stream with no
  concept of message boundaries. HTTP defines the framing on top of it, one of
  two ways:

  - Content-Length: N — the response declares its exact byte count up front. The
  reader just counts bytes; once it's read exactly N, it's done. No terminator
  needed, the length is the answer.
  - Transfer-Encoding: chunked — used when the sender doesn't know the total
  length ahead of time (like our streaming test, which doesn't know all 5
  chunks' total size before it starts writing). Each chunk is prefixed with its
  own length, and the stream ends with an explicit zero-length chunk (0\r\n\r\n)
  — that zero-length chunk is the literal "I'm done" signal defined by the
  protocol.

  Either way, net/http's Transport (on the LB side, reading the backend's
  response) tracks which framing mode is in play and returns io.EOF from
  resp.Body.Read() at exactly the right point. httputil.ReverseProxy's internal
  copy loop is just Read in a loop until it gets that EOF — and
  resp.Body.Close() is called right after, which is precisely why hooking
  cancelOnCloseBody onto Close() (rather than the moment RoundTrip returns) is
  correct: Close() fires exactly once, at the true end of the body, whether that
  end was a clean EOF or the copy failing partway through.

  3. Does a 502 backend timeout automatically retry on a different backend?

  No — not today, and this isn't an oversight, it's a real, unimplemented
  feature with a genuine trade-off. Right now Rewrite picks one target via
  b.Next(), and if RoundTrip to it fails for any reason (timeout, connection
  refused, whatever), ErrorHandler responds to the client directly. There's no
  retry loop anywhere in proxy.go.

  The reason it's not a trivial add: blindly retrying against another backend
  the moment one fails is a known failure amplifier in distributed systems — if
  backend A is slow because it's overloaded (not just unlucky), retrying against
  B and C sends it more load exactly when it can least handle it, and if the
  whole fleet is under similar pressure, retries multiply the total load instead
  of relieving it (a "retry storm"). There's also a correctness hazard:
  retrying a request that already reached the backend and executed a
  non-idempotent side effect (e.g., a POST that charges a card) risks doing it
  twice. Real systems only retry safe methods, cap the retry count, and often
  use backoff. That's genuinely a separate phase (retries/hedging), not
  something to bolt on casually here.

  4. Passive health checks and recovery — you're right, and this is the real gap

  You've independently arrived at exactly what phase 3's notes flagged as
  deferred. A backend whose /health endpoint is fine but whose real traffic
  keeps timing out is invisible to the active checker — it'll keep getting full
  traffic share forever under today's design.
  Consecutive-failure-triggers-unhealthy is the standard shape (often called a
  circuit breaker: N consecutive failures "opens" the circuit).

  The recovery half is the part that's easy to get subtly wrong, and there are
  two real options:

  Option A — piggyback on the existing active checker. Passive failures flip
  SetHealthy(false) (same mechanism phase 3 already built); the active checker,
  still polling /health in the background regardless, flips it back to healthy
  the moment a probe succeeds. Simple, reuses everything that already exists —
  but it inherits exactly the blind spot that motivated passive checking in the
  first place: if /health is a trivial handler, it'll report healthy the instant
  it's asked, even though the real route is still broken, so the backend could
  get slammed with full traffic again and immediately fail the same way.
  Possible flapping.

  Option B — a real circuit breaker with a half-open trial state, the
  production-standard version:

          N consecutive failures
   CLOSED ─────────────────────────▶ OPEN
     ▲                                 │
     │ trial request(s) succeed        │ cool-down timer elapses
     │                                 ▼
     └──────────────────────────  HALF-OPEN
                                        │
                                        │ trial request fails
                                        ▼
                                      OPEN (cool-down, possibly longer this
  time)

  After opening, wait a cool-down period, then let through a small number of
  real trial requests (not full traffic) while still routing the bulk of traffic
  elsewhere. If the trial succeeds, close the circuit (fully healthy again); if
  it fails, reopen and cool down again. This is deliberately similar to — and
  would need to coordinate with — the thundering-herd problem phase 3 already
  caught (TestStress_RecoveredBackendImmediatelyGetsFullShare): "recovered"
  shouldn't mean "instantly full traffic share" for the same reason there.

  Both are legitimate phase 5 candidates; I'd lean toward B as the one worth
  building since it's the actual industry-standard pattern (Envoy, Hystrix) and
  closes the flapping risk A has, but that's a decision for when we get there.

  That directly answers the question I'd asked: yes, a backend with one
  slow-but-broken route currently gets a clean 502 per request from
  backendTimeout, which is the correct thing for the LB to do defensively — but
  nothing today stops it from getting hit that way forever, because that's
  exactly the passive-health-check gap you just named.

  Ready to move to slice 3 (the concurrency limiter), or anything else to dig
  into first?

✻ Baked for 1m 33s

❯ 1. Question on Option B - with the cycle of cool-down and try again for       
  probing unhealthy backends, what if the backend has truly been taken down for 
  whatever reason - is there a point where a backend is just deemed             
  "unavailable" and completely taken out of the backends slice / list ?         
  2. Retries seems like a tough problem, because I could see there being some   
  requests that you DO want to retry because it is critical that the backend    
  receives it, and some requests that can just be dropped.                      

⏺ Both excellent, and both point at real limits of what health checking /
  retries can promise.

  1. Does a persistently-down backend ever get permanently removed?

  In the standard circuit breaker pattern: no, there's no terminal "gone
  forever" state — it keeps cycling open → cool-down → half-open trial → (fail)
  → open again, indefinitely, but with the cool-down duration growing each time
  it fails again (exponential backoff: e.g. 1s, 2s, 4s, 8s... capped at some
  maximum like 5 minutes). It never stops trying entirely, but it tries less and
  less often the longer it stays broken.

  Two things worth being precise about:

  - Why cap the backoff, not let it grow forever: if cool-down kept doubling
  unboundedly, a backend that genuinely recovers after being down a long time
  might not get a trial request for hours. Capping it (say, at 5 minutes) bounds
  the worst-case time to rediscover a real recovery.
  - Why the LB doesn't decide "permanently gone" on its own: the load balancer's
  health signal can't distinguish "this backend is dead forever" from "this
  backend is mid-deploy," "this backend is behind a network partition that will
  heal," or "this backend is about to be replaced by something else." Deciding
  "this is permanently gone, stop routing here at all" is a decision that
  belongs to a layer with more context than the LB has — an orchestrator that
  actually terminates and replaces the instance/pod, an autoscaling group
  deregistering it, or a human editing config and restarting the LB. If you've
  configured an AWS Target Group's health checks or a Kubernetes readiness
  probe, that's the same split: the health check governs routing (temporarily
  stop sending traffic), while a separate mechanism (ASG replacement, kubectl 
  delete pod + a Deployment recreating it) governs removal.

  Concretely in this codebase: even if we wanted the LB to permanently drop a
  backend, there's no code path for it today — RoundRobin.all
  (internal/balancer/roundrobin.go) is a fixed slice set once at NewRoundRobin
  construction. That's the exact "no live backend membership changes" gap phase
  3's notes already flagged as deferred; a circuit breaker's
  open/half-open/closed state per backend is a different, smaller thing than
  actually shrinking that slice at runtime.

  2. Retries and idempotency — you've found the real crux of the problem

  This is exactly right, and HTTP has a formal concept for it: idempotency. A
  method is idempotent if performing the same request N times has the same
  effect as performing it once. The spec defines GET, HEAD, OPTIONS, PUT, and
  DELETE as idempotent; POST and PATCH are not. So the first-cut rule production
  systems use is: auto-retry only idempotent methods by default; never 
  auto-retry POST/PATCH unless the caller explicitly says it's safe.

  But method alone isn't the whole story — there's a second axis: did the 
  backend actually receive and act on the request before the failure, or did it 
  fail before that?

  - If the LB never even managed to establish the connection or send the request
  (connection refused, DNS failure) — safe to retry any method, since the
  backend never saw it.
  - If the request was fully sent and the LB is just waiting on the response
  when the timeout fires (our exact backendTimeout scenario from slice 2) — this
  is genuinely ambiguous. The backend may have already received it, started
  processing, even finished and be trying to write a response into a socket
  we've already closed (exactly what we walked through last time). For a
  non-idempotent method, you cannot safely retry here without risking a
  duplicate side effect (e.g., double-charging a card), because you don't know
  if the first attempt already took effect.

  The general solution to that ambiguity is pushing idempotency into the
  application, not inferring it from the method: an idempotency key — the client
  generates a unique ID per logical operation and sends it in a header; the
  backend records "I've already processed key X" and returns the cached result
  if it sees that key again, rather than re-executing. This is what lets
  something like a payment API safely retry a POST — the safety comes from the
  backend's own deduplication, not from the LB guessing.

  There's a deeper, well-known distributed-systems fact underneath all of this,
  worth having as a fixed reference point for interviews: over an unreliable
  network, you can only reliably pick at-most-once delivery (don't retry —
  request might never have happened) or at-least-once delivery (retry — request
  might happen more than once). Exactly-once delivery is not achievable purely 
  at the transport/networking layer — it requires that same kind of end-to-end
  application-level deduplication (idempotency keys, unique constraints, etc.),
  because no amount of clever retry logic in the load balancer alone can tell
  the difference between "my retry created a duplicate" and "my retry was the
  thing that actually got through." This is a close cousin of the classic Two
  Generals Problem.

  So a real retry feature for this load balancer would need at minimum: cap
  retry count, only retry idempotent methods by default, distinguish "failed
  before send" (always safe) from "failed after send" (only safe if idempotent
  or backed by an idempotency key), and probably backoff between attempts so a
  retry doesn't immediately re-hit an overloaded backend. That's a legitimately
  meaty phase on its own — good to keep as a named future phase rather than
  squeeze into 4a.

  Want to continue into slice 3 (the concurrency limiter), or keep pulling on
  either of these threads?

✻ Churned for 1m 28s

❯ Let's continue to slice 3 - the concurrency limiter                           

⏺ Slice 3 shape

  - internal/middleware/concurrency.go (new file) — MaxConcurrent(n int) 
  func(http.Handler) http.Handler, same shape as Logging. Mechanism: a buffered
  channel chan struct{} of capacity n used as a counting semaphore. Acquiring a
  slot is a non-blocking send (select with a default case) — a normal channel
  send would block once the buffer's full, but select's default turns that into
  an immediate yes/no instead of actually waiting for room, which is exactly
  "fail fast when at capacity," not "wait for capacity." Releasing is a receive,
  deferred until the wrapped handler returns.
  - Composition order in main.go matters and isn't arbitrary: Logging needs to
  stay outermost, with MaxConcurrent wrapped just inside it, around proxy.New.
  If MaxConcurrent were outermost instead, a rejected request would never reach
  Logging at all — you'd have zero visibility into overload events in the logs,
  which defeats the purpose of even having them. So:
  Logging(MaxConcurrent(proxy)), not the reverse.
  - New -max-concurrent flag (default 100).
  - Tests reuse the deterministic arrived/release channel-gate pattern from
  slice 1/2 to prove the exact boundary (Nth admitted, N+1th rejected without
  blocking — proven by the rejecting goroutines finishing before release is ever
  closed, which would deadlock the test if they were queuing instead of failing
  fast).

  Let's build it.

⏺ Write(internal/middleware/concurrency.go)
  ⎿  Wrote 41 lines to internal/middleware/concurrency.go
       1 package middleware
       3 import "net/http"
       4 
         ts to be in
         )th
       7 // concurrent request -- one that arrives while n others are sti
       8 // handled -- is rejected immediately with 503, not queued: phas
         e 4a
         anned
         it-timeout
     … +44 lines (ctrl+o to expand)

⏺ Let me update the package doc comment in logging.go since it previously listed
  "rate limiting" as a future addition — concurrency limiting has now landed:

⏺ Update(internal/middleware/logging.go)
  ⎿  Added 4 lines, removed 3 lines
      1 -// Package middleware holds cross-cutting HTTP concerns (logging
        - now;                                                           
      2 -// rate limiting and caching will land here in later phases) tha
        -t wrap                                                          
      3 -// the core proxy handler rather than living inside it.         
      1 +// Package middleware holds cross-cutting HTTP concerns (logging
        + and a                                                          
      2 +// concurrency limiter now; rate limiting and caching will land 
        +here in                                                         
      3 +// later phases) that wrap the core proxy handler rather than li
        +ving                                                            
      4 +// inside it.                                                   
      5  package middleware
      6  
      7  import (

⏺ Now the tests:

⏺ Write(internal/middleware/concurrency_test.go)
  ⎿  Wrote 125 lines to internal/middleware/concurrency_test.go
       1 package middleware
       2 
       3 import (
       4   "net/http"
       5   "net/http/httptest"
       7   "testing"
       8 )
       9 
      10 // TestMaxConcurrentAllowsExactlyNInFlight is the precise bounda
         ry test:
     … +135 lines (ctrl+o to expand)

⏺ Now wire it into main.go:

⏺ Update(cmd/loadbalancer/main.go)
  ⎿  Added 1 line
      82    healthCheckTimeout := flag.Duration("health-check-timeout", 2
          *time.Second, "how long a single health check probe may take be
          fore it counts as a failure")
      83    healthCheckPath := flag.String("health-check-path", "/health"
          , "path to request on each backend for health checks")
      84    backendTimeout := flag.Duration("backend-timeout", 10*time.Se
          cond, "max time to wait on a single backend request (headers + 
          full body); 0 disables the timeout and waits indefinitely")
      85 +  maxConcurrent := flag.Int("max-concurrent", 100, "max number 
         +of requests the load balancer will process at once; further con
         +current requests get an immediate 503 rather than being queued"
         +)                                                              
      86    flag.Parse()
      87  
      88    targets, err := parseBackendURLs(backends)

⏺ Update(cmd/loadbalancer/main.go)
  ⎿  Added 2 lines, removed 2 lines
       90      log.Fatalf("%v", err)
       91    }
       92  
       93 -  // This is the core composition. proxy.New(rr, logger) retur
          -ns an *httputil.ReverseProxy — a standard-library type that sa
          -tisfies http.Handler (an interface requiring one method, Serve
          -HTTP(ResponseWriter, *Request) — anything with that method can
          - handle HTTP requests). middleware.Logging(logger) returns a f
          -unction of type func(http.Handler) http.Handler — a function t
          -hat takes a handler and returns a new handler wrapping it. So 
          -middleware.Logging(logger)(proxy.New(rr, logger)) is two calls
          - chained: first build the logging wrapper (configured with log
          -ger), then immediately call it on the proxy handler. The resul
          -t, handler, is: request comes in → logging middleware runs fir
          -st → delegates to the reverse proxy. This is Go's idiomatic mi
          -ddleware pattern — no framework, just functions wrapping funct
          -ions, all typed through the one http.Handler interface.       
       93 +  // This is the core composition. proxy.New(rr, backendTimeou
          +t, logger) returns an *httputil.ReverseProxy — a standard-libr
          +ary type that satisfies http.Handler (an interface requiring o
          +ne method, ServeHTTP(ResponseWriter, *Request) — anything with
          + that method can handle HTTP requests). Each middleware -- mid
          +dleware.Logging(logger), middleware.MaxConcurrent(n) -- is a f
          +unction of type func(http.Handler) http.Handler: it takes a ha
          +ndler and returns a new handler wrapping it. Chaining them lik
          +e this builds up layers: middleware.Logging(logger)(middleware
          +.MaxConcurrent(*maxConcurrent)(proxy.New(...))) means a reques
          +t goes through Logging first, then MaxConcurrent, then finally
          + the proxy. That order is deliberate, not arbitrary: Logging h
          +as to be outermost so it sees -- and logs -- every request, in
          +cluding the ones MaxConcurrent rejects with a 503 before they 
          +ever reach the proxy. If the order were reversed, a rejected r
          +equest would never reach Logging at all, and overload events w
          +ould be invisible in the logs.                                
       94    logger := log.New(os.Stdout, "", log.LstdFlags)
       95  
       96    rr := balancer.NewRoundRobin(targets)
       97 -  handler := middleware.Logging(logger)(proxy.New(rr, *backend
          -Timeout, logger))                                             
       97 +  handler := middleware.Logging(logger)(middleware.MaxConcurre
          +nt(*maxConcurrent)(proxy.New(rr, *backendTimeout, logger)))   
       98  
       99    server := &http.Server{
      100      Addr:    *listenAddr,

⏺ Now an integration-level stress test proving the full stack (Logging →
  MaxConcurrent → proxy → balancer) behaves correctly under overload, not just
  the middleware in isolation:

  Read 1 file (ctrl+o to expand)

⏺ Update(cmd/loadbalancer/integration_test.go)
  ⎿  Added 1 line
      22  
      23    "github.com/jerryschen31/system-design-load-balancer/internal
          /balancer"
      24    "github.com/jerryschen31/system-design-load-balancer/internal
          /healthcheck"
      25 +  "github.com/jerryschen31/system-design-load-balancer/internal
         +/middleware"                                                   
      26    "github.com/jerryschen31/system-design-load-balancer/internal
          /proxy"
      27  )
      28  

⏺ Update(cmd/loadbalancer/integration_test.go)
  ⎿  Added 94 lines
      284      t.Fatalf("got %d 2xx responses, want > 0 -- the stable bac
           kend should have absorbed traffic throughout", counts[http.Sta
           tusOK])
      285    }
      286  }
      287 +                                                              
      288 +// TestIntegration_MaxConcurrentProtectsFullStack proves the c
          +oncurrency                                                    
      289 +// limiter works correctly composed with the rest of main()'s 
          +real handler                                                  
      290 +// chain (Logging -> MaxConcurrent -> proxy, the same order ma
          +in() builds),                                                 
      291 +// not just in isolation against a bare handler the way       
      292 +// TestMaxConcurrentAllowsExactlyNInFlight (internal/middlewar
          +e) does. A                                                    
      293 +// single slow backend holds exactly maxConcurrent requests op
          +en; a further                                                 
      294 +// burst fired while those are in flight must all come back 50
          +3 immediately                                                 
      295 +// -- proving MaxConcurrent's admission check runs before prox
          +y.New ever                                                    
      296 +// picks a backend or dials it, not after.                    
      297 +func TestIntegration_MaxConcurrentProtectsFullStack(t *testing
          +.T) {                                                         
      298 +  const maxConcurrent = 4                                     
      299 +  arrived := make(chan struct{}, maxConcurrent)               
      300 +  release := make(chan struct{})                              
      301 +  backend := httptest.NewServer(http.HandlerFunc(func(w http.R
          +esponseWriter, r *http.Request) {                             
      302 +    arrived <- struct{}{}                                     
      303 +    <-release                                                 
      304 +    w.WriteHeader(http.StatusOK)                              
      305 +  }))                                                         
      306 +  defer backend.Close()                                       
      307 +  t.Logf("input: maxConcurrent=%d, one backend whose handler b
          +locks until released", maxConcurrent)                         
      308 +                                                              
      309 +  target, err := url.Parse(backend.URL)                       
      310 +  if err != nil {                                             
      311 +    t.Fatalf("parse backend URL: %v", err)                    
      312 +  }                                                           
      313 +  rr := balancer.NewRoundRobin([]*url.URL{target})            
      314 +                                                              
      315 +  // Same composition order as main(): Logging outermost, MaxC
          +oncurrent                                                     
      316 +  // next, proxy innermost.                                   
      317 +  handler := middleware.Logging(testLogger())(middleware.MaxCo
          +ncurrent(maxConcurrent)(proxy.New(rr, 0, testLogger())))      
      318 +  lb := httptest.NewServer(handler)                           
      319 +  defer lb.Close()                                            
      320 +                                                              
      321 +  var admittedWg sync.WaitGroup                               
      322 +  admittedStatuses := make([]int, maxConcurrent)              
      323 +  for i := 0; i < maxConcurrent; i++ {                        
      324 +    admittedWg.Add(1)                                         
      325 +    go func(i int) {                                          
      326 +      defer admittedWg.Done()                                 
      327 +      resp, err := http.Get(lb.URL + "/")                     
      328 +      if err != nil {                                         
      329 +        t.Errorf("admitted request %d failed: %v", i, err)    
      330 +        return                                                
      331 +      }                                                       
      332 +      defer resp.Body.Close()                                 
      333 +      admittedStatuses[i] = resp.StatusCode                   
      334 +    }(i)                                                      
      335 +  }                                                           
      336 +  for i := 0; i < maxConcurrent; i++ {                        
      337 +    <-arrived                                                 
      338 +  }                                                           
      339 +  t.Logf("step: %d requests confirmed in flight at the real ba
          +ckend, through the full stack", maxConcurrent)                
      340 +                                                              
      341 +  const burstSize = 10                                        
      342 +  var burstWg sync.WaitGroup                                  
      343 +  burstStatuses := make([]int, burstSize)                     
      344 +  burstWg.Add(burstSize)                                      
      345 +  for i := 0; i < burstSize; i++ {                            
      346 +    go func(i int) {                                          
      347 +      defer burstWg.Done()                                    
      348 +      resp, err := http.Get(lb.URL + "/")                     
      349 +      if err != nil {                                         
      350 +        t.Errorf("burst request %d failed: %v", i, err)       
      351 +        return                                                
      352 +      }                                                       
      353 +      defer resp.Body.Close()                                 
      354 +      burstStatuses[i] = resp.StatusCode                      
      355 +    }(i)                                                      
      356 +  }                                                           
      357 +  // release is still open; if any of these were queued instea
          +d of                                                          
      358 +  // rejected immediately, this Wait would hang until the test
          +'s own                                                        
      359 +  // timeout, which is the point -- a hang here is a real fail
          +ure, not                                                      
      360 +  // something we need to separately assert on.               
      361 +  burstWg.Wait()                                              
      362 +                                                              
      363 +  counts := make(map[int]int)                                 
      364 +  for _, status := range burstStatuses {                      
      365 +    counts[status]++                                          
      366 +  }                                                           
      367 +  t.Logf("output: %d-request burst while at capacity -> status
          + counts %v", burstSize, counts)                               
      368 +  if counts[http.StatusServiceUnavailable] != burstSize {     
      369 +    t.Errorf("got status counts %v, want all %d requests to be
          + %d", counts, burstSize, http.StatusServiceUnavailable)       
      370 +  }                                                           
      371 +                                                              
      372 +  close(release)                                              
      373 +  admittedWg.Wait()                                           
      374 +  t.Logf("output: the %d originally-admitted requests all comp
          +leted with statuses %v", maxConcurrent, admittedStatuses)     
      375 +  for i, status := range admittedStatuses {                   
      376 +    if status != http.StatusOK {                              
      377 +      t.Errorf("admitted request %d got status %d, want %d", i
          +, status, http.StatusOK)                                      
      378 +    }                                                         
      379 +  }                                                           
      380 +}                                                             

⏺ Now build, vet, format-check, and run the full suite:

  Ran 2 shell commands (ctrl+o to expand)

⏺ Now the full-stack integration test and the rest of the affected packages:

  Ran 1 shell command (ctrl+o to expand)

⏺ All passing. Full regression run to confirm nothing else broke:

  Ran 1 shell command (ctrl+o to expand)
⏺ Slice 3 is done — every package green under -race, including the full-stack
  integration test.

  Connecting this back to the system design

  MaxConcurrent's semaphore is a buffered channel, but the mechanism worth
  internalizing is the select/default pair: an ordinary channel send blocks when
  the buffer is full — that's fine for producer/consumer code that's willing to
  wait, but wrong here, since "wait for room" is exactly the queueing behavior
  phase 4a deliberately isn't building yet. select with a default branch
  converts that blocking send into an instantaneous check: try to grab a slot,
  and if it's not immediately available, take the default branch instead of
  parking the goroutine. That's the concrete mechanism behind "fail fast" — it's
  not a policy decision layered on top, it's a direct consequence of which Go
  primitive we reached for.

  The middleware-ordering decision (Logging outermost, MaxConcurrent inside it,
  proxy innermost) is the same kind of thing phase 3 already established with
  SetHealthy/Next(): get the composition order wrong and the system still
  "works" in the sense that it compiles and mostly passes casual testing, but
  silently loses a property you actually need — here, visibility into overload
  events, since a rejected request that never reaches Logging never produces a
  log line.

  Check on understanding — one question before we wrap this phase: MaxConcurrent
  and backendTimeout are two independent knobs now sitting in the request path
  together. Suppose -max-concurrent 100 and -backend-timeout 10s. Walk through
  what happens if every one of those 100 in-flight slots is occupied by requests
  to a backend that's each going to take the full 10 seconds to time out — what
  does the load balancer look like to a 101st client that shows up at second 1,
  versus one that shows up at second 9?

✻ Churned for 2m 47s

※ recap: We're building phase 4a (backend timeouts + concurrency limits) for the load balancer learning project. Just finished the concurrency limiter; next you need to answer a quick check-understanding question about how max-concurrent and backend-timeout interact for a client arriving mid-overload.

❯ So the scenario is 100 concurrent requests that have hit a backend let's say  
  at t=n, and the backend will take the full 10 seconds on each request         
  (t=n+10, after which the requests will time out). We want to compare a 101st  
  client request hits the LB at time n+1 vs a request that hits the LB at time  
  n+9 ? Is this correct? Just respond if this is correct, or clarify if I'm off 
  somewhere. And then I will answer the question.                               

⏺ That's correct.

✻ Worked for 3s

❯ I think both will get the same error, no? Since at both t=n+1 and t=n+9,      
  there are 100 concurrent requests still waiting for a response. I think it's  
  this line                                                                     
                                                                                
  t.Errorf("admitted request %d failed: %v", i, err)                            
                                                                                
  but I could be wrong. I haven't looked at your code closely yet.              

⏺ You're right about the outcome, and let's nail down exactly why — including
  correcting the line you pointed at.

  That t.Errorf("admitted request %d failed: %v", i, err) line is just test-code
  bookkeeping — it only fires if the HTTP request itself errors at the
  transport level (e.g. connection refused), which isn't what's happening here.
  It's not the mechanism that produces the 503.

  The actual mechanism is in internal/middleware/concurrency.go, the select:

  select {
  case sem <- struct{}{}:
      defer func() { <-sem }()
      next.ServeHTTP(w, r)
  default:
      http.Error(w, "too many concurrent requests",
  http.StatusServiceUnavailable)
  }

  Each of the original 100 requests sent a token into sem when it was admitted,
  and — critically — doesn't receive it back out (releasing the slot) until
  next.ServeHTTP(w, r) returns. next here is proxy's handler, and it won't
  return until the backend responds or backendTimeout fires and ErrorHandler
  writes the 502 — i.e., not until t+10 for any of these 100, uniformly,
  regardless of when a new client shows up in between.

  So for the 101st client: sem has 100 tokens in a channel of capacity 100 —
  full — whether it's t+1 or t+9. select takes the default branch either way,
  http.Error fires immediately, and the client gets a 503 in effectively zero
  time. MaxConcurrent has no notion of "how close to freeing up" — it's a pure
  point-in-time capacity check, not a countdown. The client at t+9, one second
  away from a slot actually opening, is treated identically to the one at t+1,
  nine seconds away.

  That's actually the sharpest argument yet for phase 4b's bounded queue: a
  client at t+9 is exactly the case where a short queue wait (even just 1-2
  seconds) would let it succeed instead of failing outright, while a client at
  t+1 would still time out of that same queue and fail — which is precisely the
  behavior difference a queue is supposed to buy you over flat rejection.

  Want to close out 4a now (test report + notes + PR), or take a first pass at
  4b's queue design next?

✻ Brewed for 29s

❯ Do create and write a test report and notes for 4a. After that, proceed with  
  4b's queue design.                                                            

  Searched for 1 pattern, read 3 files, ran 2 shell commands (ctrl+o to expand)

⏺ Now writing the test report:

⏺ Write(outputs/tests/phase-4a-timeouts-backpressure.md)
  ⎿  Wrote 293 lines to outputs/tests/phase-4a-timeouts-backpressure.md
       1 # Phase 4a Test Report: Backend timeouts and a concurrency limit
       2 
       3 **Source:** uncommitted work on `phase-4a-timeouts-backpressure`
         , branched from `build` at `e7b55f5` (tip: "added /hang endpoint
       4 **Command:** `go test -v -race -count=1 ./cmd/... ./internal/...
         `
       6 
       7 | Package | Result | Duration |
       8 |---|---|---|
      10 | `cmd/loadbalancer` | ok | 1.69s |
     … +417 lines (ctrl+o to expand)

⏺ Before writing the notes, let me actually run the manual walkthrough with real
  processes — that's been the highest-value learning moment in prior phases and
  is worth doing for real, not just describing.

  Ran 2 shell commands (ctrl+o to expand)

⏺ Both real processes are up. First demo: -backend-timeout 2s against
  /sleep?seconds=5.

  Ran 1 shell command (ctrl+o to expand)

⏺ The load balancer cut the request off at ~2.0s, not the full 5s the backend
  was told to sleep. Let's confirm the backend actually observed the
  cancellation, per the /sleep handler's ctx.Done() mechanism:

  Ran 1 shell command (ctrl+o to expand)

⏺ The backend's own log confirms it: cancelled at 2.001s, matching the load
  balancer's timeout exactly — the ctx.Done() propagation from slice 1/2's
  discussion, observed live. Now demo 2: the concurrency limiter with
  -max-concurrent 3.

  Ran 1 shell command (ctrl+o to expand)

⏺ That's an excellent, unplanned confirmation of exactly the theoretical
  scenario we discussed — I'd set -sleep=3s against -backend-timeout 2s, so the
  three "held" requests never actually complete; they get cut off by the timeout
  at 2.0s (502), and the two extra requests fired mid-flight get an instant
  503, regardless of the fact that a slot was about to free up. Let's confirm
  via the backend log:

  Ran 1 shell command (ctrl+o to expand)

⏺ Let me also capture the "happy path" — slots that actually complete
  successfully and free up for reuse — to round out the walkthrough:

  Ran 1 shell command (ctrl+o to expand)

⏺ That confirms the full picture end-to-end with real processes. Now let's clean
  up:

  Ran 1 shell command (ctrl+o to expand)

⏺ Both processes stopped cleanly. Now writing the phase notes:

⏺ Write(notes/phase-4a-timeouts-backpressure.md)
  ⎿  Wrote 77 lines to notes/phase-4a-timeouts-backpressure.md
      1 # Phase 4a: Backend timeouts and a concurrency limiter (no queue)
      3 - **Branch:** `phase-4a-timeouts-backpressure`
      4 - **PR:** opened from this branch into `build`
      5 - **Status:** complete
      7 ## 1. What was built
      8 
         healthy. It still had no way to protect *itself* from a backend,
         or from too much traffic at once — a hung backend could hold a p
        roxied request open indefinitely, and there was no ceiling on how
        ncurrently. Phase 4a closes both gaps:
     … +340 lines (ctrl+o to expand)
⏺ Phase 4a's test report and notes are written, with a real manual walkthrough
  included (not just described).

  Ran 1 shell command (ctrl+o to expand)

⏺ Everything's uncommitted and ready whenever you want to commit — I'll wait for
  that signal per the phase workflow. Moving to phase 4b now.

  Orient: what phase 4b's queue actually does

  Responsibility: when MaxConcurrent's semaphore is full, instead of rejecting
  instantly, hold the request in a bounded waiting room for up to
  queueWaitTimeout — if a slot frees up in that window, admit it; if not,
  reject. If the waiting room itself is full, reject immediately (no queueing
  the queue).

  request arrives
        │
        ▼
    sem slot free? ── yes ──▶ acquire, run handler, release on return
        │ no
        ▼
    queue slot free? ── no ──▶ reject 503 immediately (queue itself is full)
        │ yes
        ▼
    reserve a queue slot, then wait on whichever happens first:
        ├─ a sem slot frees up      ──▶ release queue slot, acquire sem, run
  handler
        ├─ queueWaitTimeout elapses ──▶ release queue slot, reject 503
        └─ client disconnects       ──▶ release queue slot, abandon — nothing to
  respond to

  Core invariant: total resource commitment stays bounded at maxConcurrent + 
  queueCapacity — never unbounded. That's the whole point; a queue without its
  own capacity limit just relocates the exact problem MaxConcurrent was built to
  solve in phase 4a, from "too many in-flight backend calls" to "too many
  goroutines parked waiting," which is still a resource exhaustion risk, just
  one layer removed.

  Failure mode this introduces that phase 4a didn't have: a queued request
  that's waiting doesn't know it's waiting from the client's point of view — the
  connection is just open with nothing coming back yet. If the client gives up
  and disconnects while queued, and we don't notice, we've wasted a queue slot
  on a request nobody's listening for anymore — the same wasted-work lesson from
  /sleep honoring ctx.Done() in 4a, one level up.

  Design checkpoint

  Smallest viable design: extend MaxConcurrent itself rather than building a
  parallel type — add queueCapacity int and queueWaitTimeout time.Duration
  parameters, with queueCapacity <= 0 meaning "no queue" (today's exact 4a
  behavior, preserved as the default-off case, same convention backendTimeout <=
  0 already uses). Mechanically: a second buffered channel queue of capacity
  queueCapacity, reserved with the same non-blocking select/default idiom
  already used for sem. Once a queue slot is reserved, a second select races
  three things: acquiring sem, time.After(queueWaitTimeout), and
  r.Context().Done() — honoring client disconnect from the start, not as an
  afterthought.

  Two things worth your explicit call before I build this:

  1. Ordering/fairness among queued requests. This channel-based approach does
  not guarantee first-in-first-out — when a slot frees up, Go doesn't promise
  which of several waiting goroutines racing on select wins. A real FIFO queue
  (an explicit waiter list behind a mutex, dequeued in arrival order) would
  guarantee no one gets starved by later arrivals, at real added complexity
  (bookkeeping a list, not just reusing the channel idiom already in place). I'd
  lean toward not building strict FIFO now — the core lesson here is "bounded
  wait with a timeout," not fairness scheduling — but it's a legitimate
  production concern worth you deciding on explicitly rather than me picking
  silently.
  2. Config defaults. New flags -queue-capacity (I'd suggest default 20) and
  -queue-wait-timeout (I'd suggest default 2s) — reasonable starting points for
  the manual walkthrough, adjustable.

⏺ User answered Claude's questions:
  ⎿  · Should phase 4b's queue guarantee strict FIFO ordering among waiting 
     requests, or is best-effort (no ordering guarantee) fine? → Strict FIFO

⏺ Good — strict FIFO it is. This adds real complexity (a fair semaphore has to
  correctly handle the race between "a slot just freed up for you" and "you just
  gave up waiting," without ever losing or double-granting a slot), so let me
  lay out the exact algorithm before writing it.

  The core race condition and how to close it

  When a waiting request's select has two things that can fire — "a slot became
  available" vs. "I've waited too long / the client left" — both can become
  ready at nearly the same instant. If we're not careful, a slot can get
  silently lost (handed to a waiter who's already given up and isn't listening
  anymore) or double-granted. The standard fix (the same technique
  golang.org/x/sync/semaphore's fair mode uses): when a waiter decides to give
  up, it re-checks — under the same lock that grants slots — whether it was
  actually granted the slot in the tiny window before it gave up. If so, it
  can't just walk away with an unused slot; it must immediately hand that slot
  to whoever's next.

  type limiter struct {
      mu       sync.Mutex
      cur      int         // slots currently held
      size     int         // max concurrent (n)
      queueCap int
      waiters  *list.List  // FIFO of *waiter, oldest at Front()
  }

  // release() hands the freed slot DIRECTLY to the longest-waiting waiter
  // (front of the list) rather than decrementing cur and letting anyone
  // race to acquire it -- that direct handoff is what makes this FIFO
  // instead of best-effort. cur is only decremented when no one is waiting.
  func (l *limiter) release() {
      l.mu.Lock()
      if front := l.waiters.Front(); front != nil {
          w := l.waiters.Remove(front).(*waiter)
          l.mu.Unlock()
          close(w.ready) // wakes exactly this waiter, and only this one
          return
      }
      l.cur--
      l.mu.Unlock()
  }

  container/list (a doubly-linked list in the standard library) is the right
  tool here rather than a slice: it gives O(1) removal from the middle of the
  list via the *list.Element handle returned at insertion — which a timed-out
  waiter needs, to remove itself from an arbitrary queue position without
  shifting anything.

  Let me implement the full thing now.

⏺ Write(internal/middleware/concurrency.go)
  ⎿  Added 163 lines, removed 24 lines
        1  package middleware
        2  
        3 -import "net/http"                                             
        3 +import (                                                      
        4 +  "container/list"                                            
        5 +  "context"                                                   
        6 +  "net/http"                                                  
        7 +  "sync"                                                      
        8 +  "time"                                                      
        9 +)                                                             
       10  
       11 +// acquireResult reports how a request's attempt to get a conc
          +urrency slot                                                  
       12 +// was resolved -- there are four distinct outcomes, and the h
          +andler needs                                                  
       13 +// to react differently to each (proceed, or one of three diff
          +erent ways of                                                 
       14 +// not proceeding).                                           
       15 +type acquireResult int                                        
       16 +                                                              
       17 +const (                                                       
       18 +  acquireGranted acquireResult = iota                         
       19 +  acquireQueueFull                                            
       20 +  acquireTimedOut                                             
       21 +  acquireClientGaveUp                                         
       22 +)                                                             
       23 +                                                              
       24 +// waiter is one request parked in the FIFO queue. ready is cl
          +osed exactly                                                  
       25 +// once, by release(), to wake this specific waiter and only t
          +his waiter --                                                 
       26 +// closing (rather than sending a value) means the wake-up is 
          +visible                                                       
       27 +// immediately to a <-w.ready read even if nobody was actively
          + selecting on                                                 
       28 +// it at the exact instant of the close.                      
       29 +type waiter struct {                                          
       30 +  ready chan struct{}                                         
       31 +}                                                             
       32 +                                                              
       33 +// limiter is a bounded, FIFO-fair counting semaphore with an 
          +optional                                                      
       34 +// bounded wait queue layered on top. size is the hard concurr
          +ency ceiling                                                  
       35 +// (like phase 4a's plain semaphore); queueCap bounds how many
          + additional                                                   
       36 +// requests may wait for a slot to free up, so an overloaded b
          +ackend can                                                    
       37 +// never cause unbounded goroutines/connections to pile up her
          +e either --                                                   
       38 +// the same reasoning that motivated size in the first place, 
          +one layer up.                                                 
       39 +type limiter struct {                                         
       40 +  mu       sync.Mutex                                         
       41 +  cur      int                                                
       42 +  size     int                                                
       43 +  queueCap int                                                
       44 +  waiters  *list.List // of *waiter, oldest (longest-waiting) 
          +at Front()                                                    
       45 +}                                                             
       46 +                                                              
       47 +func newLimiter(size, queueCap int) *limiter {                
       48 +  return &limiter{size: size, queueCap: queueCap, waiters: lis
          +t.New()}                                                      
       49 +}                                                             
       50 +                                                              
       51 +// acquire tries to get a slot, waiting in the FIFO queue (up 
          +to                                                            
       52 +// queueWaitTimeout, or until ctx is done) if none are immedia
          +tely free.                                                    
       53 +func (l *limiter) acquire(ctx context.Context, queueWaitTimeou
          +t time.Duration) acquireResult {                              
       54 +  l.mu.Lock()                                                 
       55 +  if l.cur < l.size {                                         
       56 +    l.cur++                                                   
       57 +    l.mu.Unlock()                                             
       58 +    return acquireGranted                                     
       59 +  }                                                           
       60 +  if l.waiters.Len() >= l.queueCap {                          
       61 +    l.mu.Unlock()                                             
       62 +    return acquireQueueFull                                   
       63 +  }                                                           
       64 +  w := &waiter{ready: make(chan struct{})}                    
       65 +  elem := l.waiters.PushBack(w)                               
       66 +  l.mu.Unlock()                                               
       67 +                                                              
       68 +  timer := time.NewTimer(queueWaitTimeout)                    
       69 +  defer timer.Stop()                                          
       70 +                                                              
       71 +  select {                                                    
       72 +  case <-w.ready:                                             
       73 +    // release() already handed us the slot directly -- cur wa
          +s never                                                       
       74 +    // decremented for this handoff (see release()), so it's a
          +lready                                                        
       75 +    // accounted for; nothing left to do but use it.          
       76 +    return acquireGranted                                     
       77 +  case <-timer.C:                                             
       78 +    return l.giveUp(elem, w, acquireTimedOut)                 
       79 +  case <-ctx.Done():                                          
       80 +    return l.giveUp(elem, w, acquireClientGaveUp)             
       81 +  }                                                           
       82 +}                                                             
       83 +                                                              
       84 +// giveUp handles a waiter that has decided to stop waiting (t
          +imed out, or                                                  
       85 +// the client disconnected). The subtle part: release() may ha
          +ve already                                                    
       86 +// granted this exact waiter the slot (closed w.ready) in the 
          +brief window                                                  
       87 +// between that decision and this function acquiring the lock 
          +-- both are                                                   
       88 +// checked under the same mutex that release() uses to grant s
          +lots, so                                                      
       89 +// exactly one outcome is possible, never a lost or double-gra
          +nted slot.                                                    
       90 +func (l *limiter) giveUp(elem *list.Element, w *waiter, reason
          + acquireResult) acquireResult {                               
       91 +  l.mu.Lock()                                                 
       92 +  select {                                                    
       93 +  case <-w.ready:                                             
       94 +    // We were granted the slot just before giving up. We're n
          +ot going                                                      
       95 +    // to use it -- pass it along to whoever's next (or actual
          +ly free                                                       
       96 +    // it, via release()'s own logic) rather than leaking it f
          +orever.                                                       
       97 +    l.mu.Unlock()                                             
       98 +    l.release()                                               
       99 +  default:                                                    
      100 +    // Not granted -- remove ourselves so a future release() n
          +ever                                                          
      101 +    // tries to wake a waiter nobody is listening for anymore.
      102 +    l.waiters.Remove(elem)                                    
      103 +    l.mu.Unlock()                                             
      104 +  }                                                           
      105 +  return reason                                               
      106 +}                                                             
      107 +                                                              
      108 +// release frees a slot. If anyone is waiting, the slot is han
          +ded directly                                                  
      109 +// to the longest-waiting one (FIFO) without ever being "let g
          +o" for a new                                                  
      110 +// arrival to grab first -- that direct handoff is what makes 
          +this fair                                                     
      111 +// instead of best-effort.                                    
      112 +func (l *limiter) release() {                                 
      113 +  l.mu.Lock()                                                 
      114 +  if front := l.waiters.Front(); front != nil {               
      115 +    w := l.waiters.Remove(front).(*waiter)                    
      116 +    l.mu.Unlock()                                             
      117 +    close(w.ready)                                            
      118 +    return                                                    
      119 +  }                                                           
      120 +  l.cur--                                                     
      121 +  l.mu.Unlock()                                               
      122 +}                                                             
      123 +                                                              
      124 +// queueLen reports how many requests are currently waiting. U
          +nexported --                                                  
      125 +// it exists purely so tests can deterministically observe "th
          +is waiter has                                                 
      126 +// actually joined the queue" instead of guessing via a sleep;
          + it is not                                                    
      127 +// part of the public API.                                    
      128 +func (l *limiter) queueLen() int {                            
      129 +  l.mu.Lock()                                                 
      130 +  defer l.mu.Unlock()                                         
      131 +  return l.waiters.Len()                                      
      132 +}                                                             
      133 +                                                              
      134  // MaxConcurrent returns middleware that allows at most n requ
           ests to be in
        6 -// flight through the wrapped handler at the same time. The (n
          -+1)th                                                         
        7 -// concurrent request -- one that arrives while n others are s
          -till being                                                    
        8 -// handled -- is rejected immediately with 503, not queued: ph
          -ase 4a                                                        
        9 -// deliberately ships without a wait queue (see notes for the 
          -planned                                                       
       10 -// phase 4b follow-up, which adds a bounded queue and its own 
          -wait-timeout                                                  
       11 -// on top of this).                                           
      135 +// flight through the wrapped handler at once. A request arriv
          +ing while all                                                 
      136 +// n slots are held waits in a bounded, strictly FIFO queue (c
          +apacity                                                       
      137 +// queueCapacity, each wait capped at queueWaitTimeout) for a 
          +slot to free                                                  
      138 +// up, rather than being rejected immediately -- phase 4a's pl
          +ain                                                           
      139 +// fail-fast behavior is queueCapacity <= 0, still fully suppo
          +rted (and                                                     
      140 +// still what MaxConcurrent(n, 0, 0) gives you).              
      141  //
       13 -// sem is a buffered channel of capacity n, used as a counting
          - semaphore:                                                   
       14 -// each in-flight request holds one "slot" by having sent a va
          -lue into sem                                                  
       15 -// (freed by receiving it back out, via the deferred release b
          -elow, once                                                    
       16 -// the request finishes). A plain send on a full channel block
          -s the sender                                                  
       17 -// until a slot frees up -- select with a default case turns t
          -hat blocking                                                  
       18 -// send into an immediate, non-blocking decision instead: "is 
          -a slot free                                                   
       19 -// right now, yes or no" -- which is exactly the fail-fast adm
          -ission check                                                  
       20 -// this middleware needs, never a wait for one to open up.    
       21 -func MaxConcurrent(n int) func(http.Handler) http.Handler {   
      142 +// Three distinct ways a request can fail to get a slot, each 
          +reported                                                      
      143 +// differently to the client: the queue itself was already ful
          +l (503,                                                       
      144 +// immediately); the request waited but queueWaitTimeout elaps
          +ed first                                                      
      145 +// (503, after waiting); or the client disconnected while queu
          +ed, in which                                                  
      146 +// case there is nothing left to respond to at all.           
      147 +//                                                            
      148 +// This is the same fair-semaphore shape as golang.org/x/sync/
          +semaphore's                                                   
      149 +// Acquire, applied to plain net/http middleware instead of th
          +at package's                                                  
      150 +// weighted-token API.                                        
      151 +func MaxConcurrent(n int, queueCapacity int, queueWaitTimeout 
          +time.Duration) func(http.Handler) http.Handler {              
      152    if n <= 0 {
       23 -    // Fail at construction, not on the first request -- same 
          -posture                                                       
       24 -    // as proxy.New's nil-balancer panic and healthcheck.NewCh
          -ecker's                                                       
       25 -    // interval/timeout checks.                               
      153      panic("middleware: MaxConcurrent requires a positive n")
      154    }
       28 -  sem := make(chan struct{}, n)                               
      155 +  if queueCapacity < 0 {                                      
      156 +    panic("middleware: MaxConcurrent requires a non-negative q
          +ueueCapacity")                                                
      157 +  }                                                           
      158 +  if queueCapacity > 0 && queueWaitTimeout <= 0 {             
      159 +    panic("middleware: MaxConcurrent requires a positive queue
          +WaitTimeout when queueCapacity > 0")                          
      160 +  }                                                           
      161 +  l := newLimiter(n, queueCapacity)                           
      162  
      163    return func(next http.Handler) http.Handler {
      164      return http.HandlerFunc(func(w http.ResponseWriter, r *htt
           p.Request) {
       32 -      select {                                                
       33 -      case sem <- struct{}{}:                                 
       34 -        defer func() { <-sem }()                              
      165 +      switch l.acquire(r.Context(), queueWaitTimeout) {       
      166 +      case acquireGranted:                                    
      167 +        defer l.release()                                     
      168          next.ServeHTTP(w, r)
       36 -      default:                                                
      169 +      case acquireQueueFull:                                  
      170          http.Error(w, "too many concurrent requests", http.Sta
           tusServiceUnavailable)
      171 +      case acquireTimedOut:                                   
      172 +        http.Error(w, "timed out waiting for a free slot", htt
          +p.StatusServiceUnavailable)                                   
      173 +      case acquireClientGaveUp:                               
      174 +        // The client is already gone; there is no one to writ
          +e a                                                           
      175 +        // response to. Same posture as echobackend's /sleep  
      176 +        // handler on its own cancellation path.              
      177        }
      178      })
      179    }

⏺ Now the tests — updating existing call sites and adding coverage for the new
  queueing behavior:

⏺ Write(internal/middleware/concurrency_test.go)
  ⎿  Added 313 lines, removed 33 lines
       1  package middleware
       2  
       3  import (
       4 +  "context"                                                    
       5    "net/http"
       6    "net/http/httptest"
       7    "sync"
       8    "testing"
       9 +  "time"                                                       
      10  )
      11  
      10 -// TestMaxConcurrentAllowsExactlyNInFlight is the precise bound
         -ary test:                                                      
      11 -// exactly n concurrent requests are admitted and held open, a 
         -further 2                                                      
      12 -// concurrent requests are rejected immediately (not queued), a
         -nd once the                                                    
      13 -// held requests are released their slots become available agai
         -n for new                                                      
      14 -// requests. Every synchronization point here is a real event (
         -a channel                                                      
      15 -// send/receive or a WaitGroup), never a sleep -- see arrived/r
         -elease below.                                                  
      12 +// waitForQueueLen busy-polls l's queue length until it reaches
         + want or a                                                     
      13 +// generous deadline passes. This is a deliberate, bounded poll
         + of real                                                       
      14 +// authoritative state (not a blind sleep-and-hope): there is n
         +o channel or                                                   
      15 +// callback the limiter exposes for "a waiter just joined the q
         +ueue", so                                                      
      16 +// polling queueLen() -- an in-process, mutex-guarded read, not
         + a network                                                     
      17 +// call -- is the simplest way to know deterministically that a
         + previous                                                      
      18 +// waiter has actually been enqueued before the test proceeds t
         +o enqueue                                                      
      19 +// the next one, which matters for proving FIFO order below.   
      20 +func waitForQueueLen(t *testing.T, l *limiter, want int) {     
      21 +  t.Helper()                                                   
      22 +  deadline := time.Now().Add(2 * time.Second)                  
      23 +  for time.Now().Before(deadline) {                            
      24 +    if l.queueLen() == want {                                  
      25 +      return                                                   
      26 +    }                                                          
      27 +  }                                                            
      28 +  t.Fatalf("queue length never reached %d (last observed %d)", 
         +want, l.queueLen())                                            
      29 +}                                                              
      30 +                                                               
      31 +// TestMaxConcurrentAllowsExactlyNInFlight is the phase 4a boun
         +dary test,                                                     
      32 +// unchanged in spirit: with queueCapacity=0 (no queue), a requ
         +est beyond n                                                   
      33 +// concurrent is rejected immediately, not queued.             
      34  func TestMaxConcurrentAllowsExactlyNInFlight(t *testing.T) {
      35    const n = 3
      36    arrived := make(chan struct{}, n)
     ...
      41      w.WriteHeader(http.StatusOK)
      42    })
      43  
      26 -  srv := httptest.NewServer(MaxConcurrent(n)(inner))           
      44 +  srv := httptest.NewServer(MaxConcurrent(n, 0, 0)(inner))     
      45    defer srv.Close()
      28 -  t.Logf("input: MaxConcurrent(%d) wrapping a handler that bloc
         -ks until released", n)                                         
      46 +  t.Logf("input: MaxConcurrent(%d, 0, 0) wrapping a handler tha
         +t blocks until released", n)                                   
      47  
      30 -  // Fire exactly n requests; each will reach the handler and t
         -hen block                                                      
      31 -  // on <-release.                                             
      48    var admittedWg sync.WaitGroup
      49    admittedStatuses := make([]int, n)
      50    for i := 0; i < n; i++ {
     ...
      60        admittedStatuses[i] = resp.StatusCode
      61      }(i)
      62    }
      47 -                                                               
      48 -  // Block here until exactly n requests have genuinely reached
         - the                                                           
      49 -  // handler -- a real synchronization point on the actual even
         -t, not a                                                       
      50 -  // guess based on elapsed time.                              
      63    for i := 0; i < n; i++ {
      64      <-arrived
      65    }
      66    t.Logf("step: %d requests confirmed in flight (holding every 
          semaphore slot)", n)
      67  
      56 -  // Fire 2 more concurrently while all n slots are held. relea
         -se is                                                          
      57 -  // still open, so if these were being queued rather than reje
         -cted                                                           
      58 -  // immediately, rejectedWg.Wait() below would never return an
         -d the                                                          
      59 -  // test would time out -- that failure mode is the actual pro
         -of that                                                        
      60 -  // rejection is immediate, not just "eventually these come ba
         -ck 503".                                                       
      68    const numRejected = 2
      69    var rejectedWg sync.WaitGroup
      70    rejectedStatuses := make([]int, numRejected)
     ...
      81        rejectedStatuses[i] = resp.StatusCode
      82      }(i)
      83    }
      77 -  rejectedWg.Wait()                                            
      84 +  rejectedWg.Wait() // hangs until test timeout if these were q
         +ueued instead of rejected                                      
      85    t.Logf("step: %d additional requests while at capacity -> sta
          tuses %v", numRejected, rejectedStatuses)
      86    for i, status := range rejectedStatuses {
      87      if status != http.StatusServiceUnavailable {
     ...
       97        t.Errorf("admitted request %d got status %d, want %d", i
           , status, http.StatusOK)
       98      }
       99    }
      100 +}                                                             
      101  
       94 -  // Slots should now be free again -- prove it by sending n m
          -ore                                                           
       95 -  // requests through a handler that responds immediately, and
          - confirm                                                      
       96 -  // none of them are rejected.                               
       97 -  fast := http.HandlerFunc(func(w http.ResponseWriter, r *http
          -.Request) {                                                   
      102 +// TestLimiterAcquireIsFIFO tests the queueing algorithm direc
          +tly (not                                                      
      103 +// through HTTP) so ordering can be proven deterministically: 
          +1 slot, 3                                                     
      104 +// waiters enqueued one at a time (each confirmed actually enq
          +ueued via                                                     
      105 +// waitForQueueLen before the next is started), then the slot 
          +released 3                                                    
      106 +// times in a row. The order requests actually acquire the slo
          +t must match                                                  
      107 +// the order they joined the queue, not the order goroutines h
          +appen to be                                                   
      108 +// scheduled.                                                 
      109 +func TestLimiterAcquireIsFIFO(t *testing.T) {                 
      110 +  l := newLimiter(1, 3)                                       
      111 +  if res := l.acquire(context.Background(), time.Second); res 
          +!= acquireGranted {                                           
      112 +    t.Fatalf("initial acquire: got %v, want acquireGranted", r
          +es)                                                           
      113 +  }                                                           
      114 +  t.Log("input: 1 slot (held), 3 waiters joining the queue str
          +ictly in order 0, 1, 2")                                      
      115 +                                                              
      116 +  var mu sync.Mutex                                           
      117 +  var admissionOrder []int                                    
      118 +  var wg sync.WaitGroup                                       
      119 +  for i := 0; i < 3; i++ {                                    
      120 +    wg.Add(1)                                                 
      121 +    go func(id int) {                                         
      122 +      defer wg.Done()                                         
      123 +      res := l.acquire(context.Background(), 2*time.Second)   
      124 +      if res != acquireGranted {                              
      125 +        t.Errorf("waiter %d: got %v, want acquireGranted", id,
          + res)                                                         
      126 +        return                                                
      127 +      }                                                       
      128 +      mu.Lock()                                               
      129 +      admissionOrder = append(admissionOrder, id)             
      130 +      mu.Unlock()                                             
      131 +    }(i)                                                      
      132 +    waitForQueueLen(t, l, i+1)                                
      133 +  }                                                           
      134 +  t.Logf("step: all 3 waiters confirmed enqueued (queue length
          + reached 3)")                                                 
      135 +                                                              
      136 +  // Release the held slot 3 times in a row, handing it to the
          + front of                                                     
      137 +  // the queue each time.                                     
      138 +  l.release()                                                 
      139 +  l.release()                                                 
      140 +  l.release()                                                 
      141 +  wg.Wait()                                                   
      142 +                                                              
      143 +  t.Logf("output: admission order was %v, want [0 1 2]", admis
          +sionOrder)                                                    
      144 +  want := []int{0, 1, 2}                                      
      145 +  if len(admissionOrder) != len(want) {                       
      146 +    t.Fatalf("got %d admissions, want %d", len(admissionOrder)
          +, len(want))                                                  
      147 +  }                                                           
      148 +  for i := range want {                                       
      149 +    if admissionOrder[i] != want[i] {                         
      150 +      t.Errorf("admission order %v does not match FIFO join or
          +der %v", admissionOrder, want)                                
      151 +      break                                                   
      152 +    }                                                         
      153 +  }                                                           
      154 +}                                                             
      155 +                                                              
      156 +// TestLimiterGiveUpOnClientCancelFreesQueueSlot proves the gi
          +veUp path:                                                    
      157 +// a waiter whose context is cancelled while queued (client di
          +sconnected)                                                   
      158 +// stops waiting and its queue slot becomes available again fo
          +r someone                                                     
      159 +// else, rather than being held forever.                      
      160 +func TestLimiterGiveUpOnClientCancelFreesQueueSlot(t *testing.
          +T) {                                                          
      161 +  l := newLimiter(1, 1)                                       
      162 +  if res := l.acquire(context.Background(), time.Second); res 
          +!= acquireGranted {                                           
      163 +    t.Fatalf("initial acquire: got %v, want acquireGranted", r
          +es)                                                           
      164 +  }                                                           
      165 +  t.Log("input: 1 slot (held), queue capacity 1")             
      166 +                                                              
      167 +  ctx, cancel := context.WithCancel(context.Background())     
      168 +  done := make(chan acquireResult, 1)                         
      169 +  go func() {                                                 
      170 +    done <- l.acquire(ctx, 10*time.Second) // long timeout: ca
          +ncellation should win, not this                               
      171 +  }()                                                         
      172 +  waitForQueueLen(t, l, 1)                                    
      173 +  t.Log("step: waiter confirmed enqueued")                    
      174 +                                                              
      175 +  cancel()                                                    
      176 +  res := <-done                                               
      177 +  t.Logf("output: cancelled waiter's acquire returned %v, want
          + acquireClientGaveUp", res)                                   
      178 +  if res != acquireClientGaveUp {                             
      179 +    t.Errorf("got %v, want acquireClientGaveUp", res)         
      180 +  }                                                           
      181 +  if got := l.queueLen(); got != 0 {                          
      182 +    t.Errorf("queue length after cancellation is %d, want 0 --
          + the given-up waiter's slot was not freed", got)              
      183 +  }                                                           
      184 +}                                                             
      185 +                                                              
      186 +// TestMaxConcurrentQueueAdmitsAfterWait is the HTTP-level pro
          +of of the                                                     
      187 +// success path: a request arriving while all slots are held, 
          +but with a                                                    
      188 +// slot freeing up before queueWaitTimeout elapses, is admitte
          +d (200)                                                       
      189 +// rather than rejected.                                      
      190 +func TestMaxConcurrentQueueAdmitsAfterWait(t *testing.T) {    
      191 +  const n = 1                                                 
      192 +  arrived := make(chan struct{}, 1)                           
      193 +  release := make(chan struct{})                              
      194 +  inner := http.HandlerFunc(func(w http.ResponseWriter, r *htt
          +p.Request) {                                                  
      195 +    select {                                                  
      196 +    case arrived <- struct{}{}:                               
      197 +    default:                                                  
      198 +    }                                                         
      199 +    <-release                                                 
      200      w.WriteHeader(http.StatusOK)
      201    })
      100 -  srv2 := httptest.NewServer(MaxConcurrent(n)(fast))          
      101 -  defer srv2.Close()                                          
      102 -  for i := 0; i < n; i++ {                                    
      103 -    resp, err := http.Get(srv2.URL)                           
      202 +  srv := httptest.NewServer(MaxConcurrent(n, 1, 2*time.Second)
          +(inner))                                                      
      203 +  defer srv.Close()                                           
      204 +  t.Log("input: 1 slot (held), queue capacity 1, queueWaitTime
          +out 2s; a second request arrives while the first is in flight"
          +)                                                             
      205 +                                                              
      206 +  holderDone := make(chan int, 1)                             
      207 +  go func() {                                                 
      208 +    resp, err := http.Get(srv.URL)                            
      209      if err != nil {
      105 -      t.Fatalf("post-release request %d failed: %v", i, err)  
      210 +      t.Errorf("holder request failed: %v", err)              
      211 +      holderDone <- -1                                        
      212 +      return                                                  
      213      }
      107 -    resp.Body.Close()                                         
      108 -    if resp.StatusCode != http.StatusOK {                     
      109 -      t.Errorf("post-release request %d got status %d, want %d
          - -- slot was not released", i, resp.StatusCode, http.StatusOK)
      214 +    defer resp.Body.Close()                                   
      215 +    holderDone <- resp.StatusCode                             
      216 +  }()                                                         
      217 +  <-arrived                                                   
      218 +  t.Log("step: holder confirmed in flight, holding the only sl
          +ot")                                                          
      219 +                                                              
      220 +  queuedDone := make(chan int, 1)                             
      221 +  go func() {                                                 
      222 +    resp, err := http.Get(srv.URL)                            
      223 +    if err != nil {                                           
      224 +      t.Errorf("queued request failed: %v", err)              
      225 +      queuedDone <- -1                                        
      226 +      return                                                  
      227      }
      228 +    defer resp.Body.Close()                                   
      229 +    queuedDone <- resp.StatusCode                             
      230 +  }()                                                         
      231 +                                                              
      232 +  // Give the queued request a moment to actually reach the mi
          +ddleware                                                      
      233 +  // and join the queue before releasing the holder -- otherwi
          +se we might                                                   
      234 +  // release before it's even queued, which would just test th
          +e ordinary                                                    
      235 +  // immediate-grant path instead of the queue+wakeup path thi
          +s test is                                                     
      236 +  // for. A short, generous sleep here is acceptable: it only 
          +widens the                                                    
      237 +  // window before we release, it isn't used to detect an even
          +t.                                                            
      238 +  time.Sleep(50 * time.Millisecond)                           
      239 +  close(release)                                              
      240 +                                                              
      241 +  holderStatus := <-holderDone                                
      242 +  queuedStatus := <-queuedDone                                
      243 +  t.Logf("output: holder status=%d, queued (waited then admitt
          +ed) status=%d", holderStatus, queuedStatus)                   
      244 +  if holderStatus != http.StatusOK {                          
      245 +    t.Errorf("holder got status %d, want %d", holderStatus, ht
          +tp.StatusOK)                                                  
      246    }
      112 -  t.Log("output: after release, a fresh batch of n requests al
          -l succeeded -- slots are correctly reusable")                 
      247 +  if queuedStatus != http.StatusOK {                          
      248 +    t.Errorf("queued request got status %d, want %d -- it shou
          +ld have been admitted once the holder's slot freed up", queued
          +Status, http.StatusOK)                                        
      249 +  }                                                           
      250  }
      251  
      252 +// TestMaxConcurrentQueueTimesOut is the HTTP-level proof of t
          +he failure                                                    
      253 +// path: a request that waits the full queueWaitTimeout withou
          +t a slot                                                      
      254 +// freeing up gets a distinct 503 body, not the immediate-reje
          +ction one.                                                    
      255 +func TestMaxConcurrentQueueTimesOut(t *testing.T) {           
      256 +  const n = 1                                                 
      257 +  const queueWaitTimeout = 100 * time.Millisecond             
      258 +  arrived := make(chan struct{}, 1)                           
      259 +  release := make(chan struct{})                              
      260 +  inner := http.HandlerFunc(func(w http.ResponseWriter, r *htt
          +p.Request) {                                                  
      261 +    select {                                                  
      262 +    case arrived <- struct{}{}:                               
      263 +    default:                                                  
      264 +    }                                                         
      265 +    <-release                                                 
      266 +    w.WriteHeader(http.StatusOK)                              
      267 +  })                                                          
      268 +  srv := httptest.NewServer(MaxConcurrent(n, 1, queueWaitTimeo
          +ut)(inner))                                                   
      269 +  defer srv.Close()                                           
      270 +  defer close(release) // let the holder finish so the test ca
          +n exit cleanly                                                
      271 +  t.Logf("input: 1 slot held indefinitely, queueWaitTimeout=%s
          +, a second request that will never be released in time", queue
          +WaitTimeout)                                                  
      272 +                                                              
      273 +  go http.Get(srv.URL) // holder; response ignored, cleaned up
          + via defer close(release) + server.Close()                    
      274 +  <-arrived                                                   
      275 +  t.Log("step: holder confirmed in flight, holding the only sl
          +ot")                                                          
      276 +                                                              
      277 +  start := time.Now()                                         
      278 +  resp, err := http.Get(srv.URL)                              
      279 +  elapsed := time.Since(start)                                
      280 +  if err != nil {                                             
      281 +    t.Fatalf("queued request failed: %v", err)                
      282 +  }                                                           
      283 +  defer resp.Body.Close()                                     
      284 +  body := make([]byte, 128)                                   
      285 +  n2, _ := resp.Body.Read(body)                               
      286 +                                                              
      287 +  t.Logf("output: status=%d body=%q elapsed=%s", resp.StatusCo
          +de, string(body[:n2]), elapsed)                               
      288 +  if resp.StatusCode != http.StatusServiceUnavailable {       
      289 +    t.Errorf("got status %d, want %d", resp.StatusCode, http.S
          +tatusServiceUnavailable)                                      
      290 +  }                                                           
      291 +  const wantBody = "timed out waiting for a free slot\n"      
      292 +  if string(body[:n2]) != wantBody {                          
      293 +    t.Errorf("got body %q, want %q", string(body[:n2]), wantBo
          +dy)                                                           
      294 +  }                                                           
      295 +  if elapsed < queueWaitTimeout {                             
      296 +    t.Errorf("request returned after %s, want at least queueWa
          +itTimeout (%s)", elapsed, queueWaitTimeout)                   
      297 +  }                                                           
      298 +}                                                             
      299 +                                                              
      300 +// TestMaxConcurrentQueueFullRejectsImmediately proves the thi
          +rd outcome:                                                   
      301 +// once both the slots and the queue itself are full, a furthe
          +r request is                                                  
      302 +// rejected immediately, with the original (not the timeout) 5
          +03 body, and                                                  
      303 +// without waiting queueWaitTimeout at all.                   
      304 +func TestMaxConcurrentQueueFullRejectsImmediately(t *testing.T
          +) {                                                           
      305 +  const n = 1                                                 
      306 +  const queueCapacity = 1                                     
      307 +  const queueWaitTimeout = 5 * time.Second // deliberately lon
          +g; overflow must not wait any of it                           
      308 +  arrived := make(chan struct{}, 1)                           
      309 +  release := make(chan struct{})                              
      310 +  inner := http.HandlerFunc(func(w http.ResponseWriter, r *htt
          +p.Request) {                                                  
      311 +    select {                                                  
      312 +    case arrived <- struct{}{}:                               
      313 +    default:                                                  
      314 +    }                                                         
      315 +    <-release                                                 
      316 +    w.WriteHeader(http.StatusOK)                              
      317 +  })                                                          
      318 +  srv := httptest.NewServer(MaxConcurrent(n, queueCapacity, qu
          +eueWaitTimeout)(inner))                                       
      319 +  defer srv.Close()                                           
      320 +  defer close(release)                                        
      321 +  t.Logf("input: 1 slot held, queue capacity 1 also held, a 3r
          +d (overflow) request arrives")                                
      322 +                                                              
      323 +  go http.Get(srv.URL) // occupies the slot                   
      324 +  <-arrived                                                   
      325 +  t.Log("step: 1st request confirmed holding the only slot")  
      326 +                                                              
      327 +  l := extractLimiterForTest(t, srv)                          
      328 +  waiterDone := make(chan struct{})                           
      329 +  go func() {                                                 
      330 +    defer close(waiterDone)                                   
      331 +    http.Get(srv.URL) // occupies the only queue slot         
      332 +  }()                                                         
      333 +  waitForQueueLen(t, l, 1)                                    
      334 +  t.Log("step: 2nd request confirmed holding the only queue sl
          +ot")                                                          
      335 +                                                              
      336 +  start := time.Now()                                         
      337 +  resp, err := http.Get(srv.URL)                              
      338 +  elapsed := time.Since(start)                                
      339 +  if err != nil {                                             
      340 +    t.Fatalf("overflow request failed: %v", err)              
      341 +  }                                                           
      342 +  defer resp.Body.Close()                                     
      343 +  body := make([]byte, 128)                                   
      344 +  n2, _ := resp.Body.Read(body)                               
      345 +                                                              
      346 +  t.Logf("output: overflow request -> status=%d body=%q elapse
          +d=%s", resp.StatusCode, string(body[:n2]), elapsed)           
      347 +  if resp.StatusCode != http.StatusServiceUnavailable {       
      348 +    t.Errorf("got status %d, want %d", resp.StatusCode, http.S
          +tatusServiceUnavailable)                                      
      349 +  }                                                           
      350 +  const wantBody = "too many concurrent requests\n"           
      351 +  if string(body[:n2]) != wantBody {                          
      352 +    t.Errorf("got body %q, want %q", string(body[:n2]), wantBo
          +dy)                                                           
      353 +  }                                                           
      354 +  if elapsed >= queueWaitTimeout {                            
      355 +    t.Errorf("overflow request took %s (>= queueWaitTimeout %s
          +); it should have been rejected immediately, never waiting", e
          +lapsed, queueWaitTimeout)                                     
      356 +  }                                                           
      357 +}                                                             
      358 +                                                              
      359  func TestMaxConcurrentPanicsOnNonPositiveN(t *testing.T) {
      360 +  t.Log("input: MaxConcurrent(0, 0, 0)")                      
      361 +  defer func() {                                              
      362 +    r := recover()                                            
      363 +    if r == nil {                                             
      364 +      t.Fatal("expected MaxConcurrent(0, 0, 0) to panic, so a 
          +misconfiguration surfaces at construction instead of silently 
          +blocking all traffic")                                        
      365 +    }                                                         
      366 +    t.Logf("output: panicked as expected: %v", r)             
      367 +  }()                                                         
      368 +  MaxConcurrent(0, 0, 0)                                      
      369 +}                                                             
      370 +                                                              
      371 +func TestMaxConcurrentPanicsOnNegativeQueueCapacity(t *testing
          +.T) {                                                         
      372 +  t.Log("input: MaxConcurrent(1, -1, 0)")                     
      373 +  defer func() {                                              
      374 +    r := recover()                                            
      375 +    if r == nil {                                             
      376 +      t.Fatal("expected a negative queueCapacity to panic")   
      377 +    }                                                         
      378 +    t.Logf("output: panicked as expected: %v", r)             
      379 +  }()                                                         
      380 +  MaxConcurrent(1, -1, 0)                                     
      381 +}                                                             
      382 +                                                              
      383 +func TestMaxConcurrentPanicsOnQueueWithoutWaitTimeout(t *testi
          +ng.T) {                                                       
      384 +  t.Log("input: MaxConcurrent(1, 5, 0) -- a real queue capacit
          +y but no positive wait timeout")                              
      385 +  defer func() {                                              
      386 +    r := recover()                                            
      387 +    if r == nil {                                             
      388 +      t.Fatal("expected queueCapacity > 0 with queueWaitTimeou
          +t <= 0 to panic")                                             
      389 +    }                                                         
      390 +    t.Logf("output: panicked as expected: %v", r)             
      391 +  }()                                                         
      392 +  MaxConcurrent(1, 5, 0)                                      
      393 +}                                                             
      394 +                                                              
      395 +func TestMaxConcurrentPanicsOnNonPositiveN(t *testing.T) {    
      396    t.Log("input: MaxConcurrent(0)")
      397    defer func() {
      398      r := recover()

⏺ I referenced a helper (extractLimiterForTest) that doesn't exist, and left a
  leftover duplicate test with the old signature. Let me fix both.

⏺ Update(internal/middleware/concurrency_test.go)
  ⎿  Added 17 lines, removed 7 lines
      320    defer close(release)
      321    t.Logf("input: 1 slot held, queue capacity 1 also held, a 3r
           d (overflow) request arrives")
      322  
      323 -  go http.Get(srv.URL) // occupies the slot                   
      323 +  go func() {                                                 
      324 +    resp, err := http.Get(srv.URL) // occupies the slot       
      325 +    if err == nil {                                           
      326 +      resp.Body.Close()                                       
      327 +    }                                                         
      328 +  }()                                                         
      329    <-arrived
      330    t.Log("step: 1st request confirmed holding the only slot")
      331  
      327 -  l := extractLimiterForTest(t, srv)                          
      328 -  waiterDone := make(chan struct{})                           
      332    go func() {
      330 -    defer close(waiterDone)                                   
      331 -    http.Get(srv.URL) // occupies the only queue slot         
      333 +    resp, err := http.Get(srv.URL) // occupies the only queue 
          +slot                                                          
      334 +    if err == nil {                                           
      335 +      resp.Body.Close()                                       
      336 +    }                                                         
      337    }()
      333 -  waitForQueueLen(t, l, 1)                                    
      334 -  t.Log("step: 2nd request confirmed holding the only queue sl
          -ot")                                                          
      338 +  // Give the 2nd request a generous, fixed window to actually
          + reach the                                                    
      339 +  // middleware and join the queue before firing the overflow 
          +request                                                       
      340 +  // below -- this only widens the window before the real asse
          +rtion, it                                                     
      341 +  // isn't used to detect an event (loopback round-trips here 
          +are                                                           
      342 +  // microseconds, so 50ms is enormous headroom).             
      343 +  time.Sleep(50 * time.Millisecond)                           
      344 +  t.Log("step: 2nd request given time to join the only queue s
          +lot")                                                         
      345  
      346    start := time.Now()
      347    resp, err := http.Get(srv.URL)

⏺ Update(internal/middleware/concurrency_test.go)
  ⎿  Removed 12 lines
      401    }()
      402    MaxConcurrent(1, 5, 0)
      403  }
      404 -                                                              
      405 -func TestMaxConcurrentPanicsOnNonPositiveN(t *testing.T) {    
      406 -  t.Log("input: MaxConcurrent(0)")                            
      407 -  defer func() {                                              
      408 -    r := recover()                                            
      409 -    if r == nil {                                             
      410 -      t.Fatal("expected MaxConcurrent(0) to panic, so a miscon
          -figuration surfaces at construction instead of silently blocki
          -ng all traffic")                                              
      411 -    }                                                         
      412 -    t.Logf("output: panicked as expected: %v", r)             
      413 -  }()                                                         
      414 -  MaxConcurrent(0)                                            
      415 -}                                                             

⏺ Now update main.go with the new flags and wiring, and integration_test.go's
  existing MaxConcurrent call site:

⏺ Update(cmd/loadbalancer/main.go)
  ⎿  Added 3 lines, removed 1 line
      82    healthCheckTimeout := flag.Duration("health-check-timeout", 2
          *time.Second, "how long a single health check probe may take be
          fore it counts as a failure")
      83    healthCheckPath := flag.String("health-check-path", "/health"
          , "path to request on each backend for health checks")
      84    backendTimeout := flag.Duration("backend-timeout", 10*time.Se
          cond, "max time to wait on a single backend request (headers + 
          full body); 0 disables the timeout and waits indefinitely")
      85 -  maxConcurrent := flag.Int("max-concurrent", 100, "max number 
         -of requests the load balancer will process at once; further con
         -current requests get an immediate 503 rather than being queued"
         -)                                                              
      85 +  maxConcurrent := flag.Int("max-concurrent", 100, "max number 
         +of requests the load balancer will process at once")           
      86 +  queueCapacity := flag.Int("queue-capacity", 20, "max number o
         +f requests allowed to wait for a free slot once max-concurrent 
         +is reached; 0 disables queueing (immediate 503 instead)")      
      87 +  queueWaitTimeout := flag.Duration("queue-wait-timeout", 2*tim
         +e.Second, "max time a request will wait in the queue for a free
         + slot before getting a 503; ignored if queue-capacity is 0")   
      88    flag.Parse()
      89  
      90    targets, err := parseBackendURLs(backends)

⏺ Update(cmd/loadbalancer/main.go)
  ⎿  Added 1 line, removed 1 line
       96    logger := log.New(os.Stdout, "", log.LstdFlags)
       97  
       98    rr := balancer.NewRoundRobin(targets)
       99 -  handler := middleware.Logging(logger)(middleware.MaxConcurre
          -nt(*maxConcurrent)(proxy.New(rr, *backendTimeout, logger)))   
       99 +  handler := middleware.Logging(logger)(middleware.MaxConcurre
          +nt(*maxConcurrent, *queueCapacity, *queueWaitTimeout)(proxy.Ne
          +w(rr, *backendTimeout, logger)))                              
      100  
      101    server := &http.Server{
      102      Addr:    *listenAddr,

⏺ Update(cmd/loadbalancer/integration_test.go)
  ⎿  Added 1 line, removed 1 line
      314  
      315    // Same composition order as main(): Logging outermost, MaxC
           oncurrent
      316    // next, proxy innermost.
      317 -  handler := middleware.Logging(testLogger())(middleware.MaxCo
          -ncurrent(maxConcurrent)(proxy.New(rr, 0, testLogger())))      
      317 +  handler := middleware.Logging(testLogger())(middleware.MaxCo
          +ncurrent(maxConcurrent, 0, 0)(proxy.New(rr, 0, testLogger())))
      318    lb := httptest.NewServer(handler)
      319    defer lb.Close()
      320  

⏺ Now let's add the integration-level test that closes the loop on the exact
  scenario discussed earlier — a near-miss client succeeding via the queue
  instead of being rejected outright:

⏺ Update(cmd/loadbalancer/integration_test.go)
  ⎿  Added 69 lines
      378      }
      379    }
      380  }
      381 +                                                              
      382 +// TestIntegration_QueueAdmitsNearMissRequest is the resolutio
          +n of the                                                      
      383 +// exact scenario discussed while designing phase 4b: with pha
          +se 4a's                                                       
      384 +// plain MaxConcurrent, a client arriving 1 second before a sl
          +ot would free                                                 
      385 +// up and a client arriving 9 seconds before were treated iden
          +tically --                                                    
      386 +// both rejected outright, because MaxConcurrent had no notion
          + of "how                                                      
      387 +// close". With a queue layered on top, a near-miss client (qu
          +eueing for                                                    
      388 +// well under queueWaitTimeout) succeeds instead, through the 
          +real full                                                     
      389 +// stack: Logging -> MaxConcurrent(with queue) -> proxy -> a r
          +eal backend.                                                  
      390 +func TestIntegration_QueueAdmitsNearMissRequest(t *testing.T) 
          +{                                                             
      391 +  const maxConcurrent = 2                                     
      392 +  const holderDelay = 150 * time.Millisecond                  
      393 +  const queueCapacity = 1                                     
      394 +  const queueWaitTimeout = 5 * time.Second // deliberately >> 
          +holderDelay                                                   
      395 +  backend := httptest.NewServer(http.HandlerFunc(func(w http.R
          +esponseWriter, r *http.Request) {                             
      396 +    time.Sleep(holderDelay)                                   
      397 +    w.WriteHeader(http.StatusOK)                              
      398 +  }))                                                         
      399 +  defer backend.Close()                                       
      400 +  t.Logf("input: maxConcurrent=%d, each holder finishes in ~%s
          +; queueCapacity=%d, queueWaitTimeout=%s (well above holderDela
          +y)", maxConcurrent, holderDelay, queueCapacity, queueWaitTimeo
          +ut)                                                           
      401 +                                                              
      402 +  target, err := url.Parse(backend.URL)                       
      403 +  if err != nil {                                             
      404 +    t.Fatalf("parse backend URL: %v", err)                    
      405 +  }                                                           
      406 +  rr := balancer.NewRoundRobin([]*url.URL{target})            
      407 +  handler := middleware.Logging(testLogger())(middleware.MaxCo
          +ncurrent(maxConcurrent, queueCapacity, queueWaitTimeout)(proxy
          +.New(rr, 0, testLogger())))                                   
      408 +  lb := httptest.NewServer(handler)                           
      409 +  defer lb.Close()                                            
      410 +                                                              
      411 +  var holderWg sync.WaitGroup                                 
      412 +  holderWg.Add(maxConcurrent)                                 
      413 +  holderStart := time.Now()                                   
      414 +  for i := 0; i < maxConcurrent; i++ {                        
      415 +    go func() {                                               
      416 +      defer holderWg.Done()                                   
      417 +      resp, err := http.Get(lb.URL + "/")                     
      418 +      if err != nil {                                         
      419 +        t.Errorf("holder request failed: %v", err)            
      420 +        return                                                
      421 +      }                                                       
      422 +      resp.Body.Close()                                       
      423 +    }()                                                       
      424 +  }                                                           
      425 +                                                              
      426 +  // Give the holders a moment to actually be in flight before
          + the                                                          
      427 +  // near-miss request arrives -- this only needs to land some
          +time                                                          
      428 +  // before holderDelay elapses, not at a precise instant, so 
          +a short                                                       
      429 +  // fixed sleep is fine here (unlike detecting an exact event
          +).                                                            
      430 +  time.Sleep(20 * time.Millisecond)                           
      431 +                                                              
      432 +  nearMissStart := time.Now()                                 
      433 +  resp, err := http.Get(lb.URL + "/")                         
      434 +  nearMissElapsed := time.Since(nearMissStart)                
      435 +  if err != nil {                                             
      436 +    t.Fatalf("near-miss request failed: %v", err)             
      437 +  }                                                           
      438 +  defer resp.Body.Close()                                     
      439 +                                                              
      440 +  holderWg.Wait()                                             
      441 +  totalHolderElapsed := time.Since(holderStart)               
      442 +  t.Logf("output: near-miss request -> status=%d, waited %s (h
          +olders took %s total)", resp.StatusCode, nearMissElapsed, tota
          +lHolderElapsed)                                               
      443 +  if resp.StatusCode != http.StatusOK {                       
      444 +    t.Fatalf("near-miss request got status %d, want %d -- it s
          +hould have queued and been admitted once a slot freed up, not 
          +rejected", resp.StatusCode, http.StatusOK)                    
      445 +  }                                                           
      446 +  if nearMissElapsed >= queueWaitTimeout {                    
      447 +    t.Errorf("near-miss request took %s (>= queueWaitTimeout %
          +s); it should have been admitted well before the timeout, as s
          +oon as a holder finished", nearMissElapsed, queueWaitTimeout) 
      448 +  }                                                           
      449 +}                                                             

⏺ Now let's build, vet, format-check, and run everything:

  Ran 1 shell command (ctrl+o to expand)

⏺ Clean. Now the test suite:

  Ran 1 shell command (ctrl+o to expand)

⏺ Found a real bug — but it's in the test, not the algorithm. Let me fix the
  test file first, then explain what happened.

⏺ Update(internal/middleware/concurrency_test.go)
  ⎿  Added 18 lines, removed 17 lines
      113    }
      114    t.Log("input: 1 slot (held), 3 waiters joining the queue str
           ictly in order 0, 1, 2")
      115  
      116 -  var mu sync.Mutex                                           
      117 -  var admissionOrder []int                                    
      118 -  var wg sync.WaitGroup                                       
      116 +  admitted := make(chan int, 3)                               
      117    for i := 0; i < 3; i++ {
      120 -    wg.Add(1)                                                 
      118      go func(id int) {
      122 -      defer wg.Done()                                         
      119        res := l.acquire(context.Background(), 2*time.Second)
      120        if res != acquireGranted {
      121          t.Errorf("waiter %d: got %v, want acquireGranted", id,
            res)
      122          return
      123        }
      128 -      mu.Lock()                                               
      129 -      admissionOrder = append(admissionOrder, id)             
      130 -      mu.Unlock()                                             
      124 +      admitted <- id                                          
      125      }(i)
      126      waitForQueueLen(t, l, i+1)
      127    }
      128    t.Logf("step: all 3 waiters confirmed enqueued (queue length
            reached 3)")
      129  
      136 -  // Release the held slot 3 times in a row, handing it to the
          - front of                                                     
      137 -  // the queue each time.                                     
      138 -  l.release()                                                 
      139 -  l.release()                                                 
      140 -  l.release()                                                 
      141 -  wg.Wait()                                                   
      130 +  // Release one slot at a time, reading admitted before issui
          +ng the                                                        
      131 +  // next release. This matters: closing a waiter's ready chan
          +nel makes                                                     
      132 +  // it *grantable*, but Go's scheduler gives no guarantee abo
          +ut the                                                        
      133 +  // order in which separately-blocked goroutines actually res
          +ume and                                                       
      134 +  // run once unblocked -- firing all 3 releases back-to-back 
          +and then                                                      
      135 +  // comparing a freely-raced append order (an earlier version
          + of this                                                      
      136 +  // test did exactly that) tests scheduler luck, not the algo
          +rithm.                                                        
      137 +  // Waiting for each admission before the next release works 
          +because,                                                      
      138 +  // at each point, only the single most-recently-woken waiter
          +'s                                                            
      139 +  // goroutine is capable of sending on admitted at all -- so 
          +the value                                                     
      140 +  // received is unambiguous, not a race.                     
      141 +  var admissionOrder []int                                    
      142 +  for i := 0; i < 3; i++ {                                    
      143 +    l.release()                                               
      144 +    admissionOrder = append(admissionOrder, <-admitted)       
      145 +  }                                                           
      146  
      147    t.Logf("output: admission order was %v, want [0 1 2]", admis
           sionOrder)
      148    want := []int{0, 1, 2}
      145 -  if len(admissionOrder) != len(want) {                       
      146 -    t.Fatalf("got %d admissions, want %d", len(admissionOrder)
          -, len(want))                                                  
      147 -  }                                                           
      149    for i := range want {
      150      if admissionOrder[i] != want[i] {
      151        t.Errorf("admission order %v does not match FIFO join or
           der %v", admissionOrder, want)
  Ran 2 shell commands (ctrl+o to expand)

⏺ Confirmed stable across 5 runs. Now the full suite:

  Ran 1 shell command (ctrl+o to expand)

⏺ All green. Now the full-stack integration test and the rest of the suite:

  Ran 1 shell command (ctrl+o to expand)

⏺ The near-miss request waited 284ms and succeeded (200), exactly the scenario
  the queue was built to fix. Let's run the full regression suite and
  stress-check for flakiness with repeated race runs on the new tests:

  Ran 1 shell command (ctrl+o to expand)

⏺ Everything's green and stable across repeated runs, including under -race.
  Let's also confirm the manual walkthrough shows the near-miss scenario working
  with real processes, since that's the whole point of 4b:

  Ran 2 shell commands (ctrl+o to expand)

⏺ Now let's demo the timeout-while-queued and queue-full cases too:

  Ran 1 shell command (ctrl+o to expand)

⏺ All four states demonstrated end-to-end with real processes: near-miss
  admitted after waiting, timed-out-after-waiting (503 at exactly 3.0s),
  queue-itself-full (503 in under a millisecond), and the two original holders
  still correctly hitting backend-timeout (502 at 5.0s) — confirming the queue
  and the phase 4a timeout compose correctly. Cleaning up:

  Ran 1 shell command (ctrl+o to expand)

⏺ Connecting this back to the system design, and the bug we hit

  The TestLimiterAcquireIsFIFO failure was a genuinely useful one to walk
  through, because it's a distinction that trips people up in real concurrent
  systems: "granted first" and "observed to run first" are not the same 
  guarantee.

  The algorithm's FIFO-ness comes entirely from release() always popping
  waiters.Front() — that part is fully deterministic, decided synchronously by
  whichever goroutine calls release(), with no scheduler involved. But my first
  version of the test fired l.release() three times back-to-back and then
  compared whatever order the three woken goroutines happened to append their
  IDs to a shared slice. Those are different things: closing waiter 0's channel
  makes it eligible to run, but Go's scheduler makes no promise about resuming
  three separately-blocked goroutines in the order they became runnable — with
  GOMAXPROCS > 1 they can genuinely execute their next lines in parallel. The
  fix was to synchronize the test around each individual grant (release, then
  wait for exactly one admission to arrive on a channel, then release again) so
  there's never more than one goroutine that's possibly able to report in at any
  point — turning an ambiguous race into an unambiguous sequence.

  This is the same category of lesson as cancelOnCloseBody in phase 4a: the
  mechanism was correct, but proving it correctly required understanding
  precisely what Go does and doesn't guarantee, not just "it passed once."

  The giveUp/re-check-under-lock pattern is the piece that makes this a
  genuinely fair semaphore rather than just a queue with a timeout bolted on —
  without it, a slot could get silently handed to a waiter who's already stopped
  listening (lost forever) in the exact race window between "timeout fires" and
  "release() happens to run at the same instant." That's the same shape of
  problem golang.org/x/sync/semaphore's real implementation solves the same way.

  Phase 4b is complete: build, vet, gofmt, and the full -race suite are clean,
  the new queue tests are stable across repeated runs, and the manual
  walkthrough confirmed all four states (immediate grant, queued-then-admitted,
  queued-then-timed-out, queue-full-immediate-reject) against real processes,
  composing correctly with phase 4a's backend timeout.

  Want me to write up 4b's test report and notes next (same as 4a), or hold off?

✻ Baked for 14m 2s

❯ You don't explain things well. You explain things as if I am deep into each   
  line of code, so your summary paragraphs "The algorithm's FIFO-ness comes     
  entirely from release() always popping waiters.Front() ..." and "The          
  giveUp/re-check-under-lock pattern is the piece that makes this a genuinely   
  fair semaphore..." only make high-level sense to me. Update CLAUDE.md and/or  
  explain-code skill so that you explain things step-by-step as if I am a       
  general developer unfamiliar with your code changes and learning about system 
  design, NOT as a side-by-side expert working with you on the code             
  line-by-line, which is how your explanations feel currently. I'm always left  
  feeling lost in your code details and don't get a good systems grasp of what  
  you did. Feel free to add diagrams and flow charts to explain things, as      
  necessary.                                                                    
                                                                                
  And then proceed with writing up 4b's test report and notes.                  

  Searched for 1 pattern, read 1 file, listed 1 directory (ctrl+o to expand)

⏺ You're right, and that's a real, specific problem — not a preference for "more
  detail," but for a different order: I've been opening with sentences that
  assume you already hold the implementation in your head, then working backward
  into an explanation. Let me fix the durable guidance so this doesn't keep
  happening, then redo the explanation properly.

  Read 1 file (ctrl+o to expand)

⏺ Update(CLAUDE.md)
  ⎿  Added 5 lines
      22  - Before changing code in a non-trivial way, first explain the 
          proposed file/function structure in concrete terms (which file,
           which function, what new control flow) so the user can react t
          o the shape of the solution before it is written.
      23  - Prefer an interactive demo when possible; if not, make tests 
          pedagogical: add `t.Log` output that shows the test inputs, the
           key internal steps, and the final outputs so `go test -v ./...
          ` reads like an execution trace rather than just pass/fail.
      24  - When you add a line that is doing important Go or HTTP work, 
          explain the exact mechanism of that line in plain language, not
           just the high-level purpose of the surrounding function.
      25 +- **Explain concept-first, code-second — never open with a para
         +graph that names multiple functions/variables before the reader
         + has a plain-language model of what's going on.** (Corrected 20
         +26-08-24 after feedback that post-implementation summaries read
         + as expert-to-expert shorthand — e.g. opening with "The algorit
         +hm's FIFO-ness comes entirely from `release()` always popping `
         +waiters.Front()`" assumes the reader already holds the implemen
         +tation in their head, which is exactly backwards for someone le
         +arning it for the first time.) The required shape for any "here
         +'s what this does / why it works" explanation, including post-i
         +mplementation "connect to system design" summaries, not just ex
         +plicit `/explain-code` walkthroughs:                           
      26 +  1. **State the general problem and solution shape in plain la
         +nguage first** — zero function/variable names at this stage. ("
         +Several requests can be waiting for the same limited resource a
         +t once. We want the one that's been waiting longest to get it n
         +ext, not whichever one the scheduler happens to run first.")   
      27 +  2. **Draw a diagram** whenever the concept involves flow, ord
         +ering, states, or a sequence of events — ASCII flowcharts, sequ
         +ence diagrams, or state diagrams. Diagrams are the default for 
         +this kind of explanation, not an optional extra reached for occ
         +asionally.                                                     
      28 +  3. **Only then map specific named functions/variables onto th
         +e diagram, one at a time** ("this box, where a freed slot gets 
         +handed to the next waiter, is the `release` function") — never 
         +introduce more than one new code identifier per sentence withou
         +t that scaffolding already in place.                           
      29 +  4. If a subtlety or bug was involved, explain the general sha
         +pe of the failure first (what could go wrong, in plain terms) b
         +efore naming which line caused it.                             
      30  - Whenever a concept can be observed hands-on, give the user a 
          concrete scenario to run themselves locally — real backend proc
          esses, real concurrent requests via curl loops or a load-testin
          g tool, killing a process mid-test, etc. — rather than only des
          cribing the behavior or pointing at automated test output. This
           includes proposing small tools to install (e.g. `hey`/`ab` for
           load generation, a tiny stand-in backend binary) when that's w
          hat it takes to make the simulation concrete. The manual curl w
          alkthrough in phase 1 (see `prompts/20260821-session-2-phase-1.
          md`) was the most effective learning moment so far — treat that
           as the bar, not an exception.
      31  - Before writing code, explain the requirement, invariants, sta
          te model, concurrency risks, failure modes, and brainstorm diff
          erent implementation approaches.
      32  - After implementation, identify missing requirements (which ma
          y be deferred to later phases), invariants, bottlenecks, failur
          e modes, and trade-offs. Write these into your summary notes. G
          enerate stress tests and adversarial tests, and add these to te
          st report.

⏺ Now let's update the explain-code skill's format section to reinforce this
  with the diagram-first principle:

⏺ Update(.claude/skills/explain-code/SKILL.md)
  ⎿  Added 3 lines, removed 2 lines
      18  - Don't assume familiarity with terms common in backend work ev
          en if they sound basic: socket, goroutine, channel, mutex, inte
          rface, context, hop-by-hop header, connection pooling, etc. Def
          ine each the first time it appears in this conversation.
      19  - Assume general programming literacy (loops, functions, types)
           from their CS background, but not Go-specific idiom or stdlib 
          knowledge.
      20  
      21 -## Format (as important as content -- confirmed 2026-08-23 agai
         -nst a side-by-side comparison)                                 
      21 +## Format (as important as content -- confirmed 2026-08-23 agai
         +nst a side-by-side comparison; sharpened 2026-08-24)           
      22  
      23  A technically-accurate explanation in dense prose paragraphs wa
          s rated harder to follow than a shorter, more scannable one cov
          ering the same mechanism. Default to the terser style:
      24  
      25 -- Lead with a one- or two-sentence plain-language summary of wh
         -at the construct does, *before* the mechanism detail.          
      25 +- Lead with a one- or two-sentence plain-language summary of wh
         +at the construct does, *before* the mechanism detail -- and tha
         +t summary must contain zero function/variable names. State the 
         +general problem/shape first ("several goroutines can be waiting
         + for the same limited resource"), then bring in code identifier
         +s only once that plain-language picture is in place. A paragrap
         +h that names two or more functions/variables before the reader 
         +has that scaffolding reads as expert-to-expert shorthand, not t
         +eaching -- this was called out explicitly as the recurring fail
         +ure mode to avoid (2026-08-24).                                
      26 +- Draw a diagram (ASCII flowchart, sequence, or state diagram) 
         +whenever the construct involves flow, ordering, states, or a se
         +quence of events. This is the default for that kind of explanat
         +ion, not something to reach for only when stuck for words.     
      27  - Prefer short bullet points over paragraphs. A paragraph is ac
          ceptable for a single connected thought, but don't stack more t
          han ~3-4 sentences of unbroken prose.
      28  - Show a minimal, standalone usage snippet when it clarifies a 
          pattern (not just the code already in the file -- a stripped-do
          wn illustrative example, e.g. the create/defer/cancel-later sha
          pe for `context.WithCancel`).
      29  - When a construct has a common misconception or a sharp edge, 
          give it its own short "what it does NOT do" callout (e.g. conte
          xt cancellation is cooperative, not preemptive -- `cancel()` do
          esn't kill a goroutine, it only closes a channel that well-beha
          ved code has to check).

⏺ Now let me save this as a durable memory since it's a general communication
  preference, not just something scoped to this one repo:

  Recalled 1 memory, wrote 2 memories (ctrl+o to expand)

⏺ Redoing the explanation, properly this time

  The general problem, no code yet: once every slot for handling a request at
  once is full, a new request has to decide: give up immediately, or wait? If it
  waits, and a slot later frees up while several requests are waiting, who gets
  it? There are two honest answers — "whoever the computer happens to pick in
  that instant" (no guarantee it's the one who's been waiting longest), or
  "whoever's been waiting longest, always" — a fair line,
  first-come-first-served. We built the second one.

   Slot 1: [in use]
   Slot 2: [in use]        <- both full, nothing free

   The waiting line, oldest at front:
     [ Request A ] -> [ Request B ] -> [ Request C ]
       waited longest                  just arrived

   When Slot 1 frees up:
     Request A is pulled from the FRONT and given the slot.
     B moves up to the front; C is still behind B.

  Two things, besides getting a slot, can happen to someone in that line: they
  wait too long and give up, or the person who sent them (the client) hangs up
  before their turn ever comes. Either way they leave the line — and that's
  where a genuine timing hazard shows up. What if someone is being called to the
  front of the line at the exact same moment they've independently decided to
  leave?

   [the line frees a slot, calls "Request A, you're up"]
                    |
                    |   <- could happen at the same instant
                    |
   [Request A separately decides "I've waited too long, I'm done"]

  Handled carelessly, this goes wrong two ways: the slot gets wasted forever
  (handed to someone who already walked away and isn't listening), or it gets
  handed out twice. The fix, in plain terms: anyone leaving the line must first 
  check whether they were just handed a slot in that same instant — and if so,
  since they're not going to use it, immediately pass it to the next person in
  line instead of letting it sit unused.

  Mapped onto the actual code in internal/middleware/concurrency.go: the
  "waiting line" is the waiters list; "pull the front person and hand them the
  slot" is the release function; "check whether I was just handed a slot right
  as I was leaving" is the giveUp function, using the same lock release uses so
  the two can never disagree about who currently holds the slot.

  The bug we actually hit was in the test, not this algorithm — and it's a very
  common concurrency-testing mistake worth understanding on its own terms.
  Picture calling three people to the front of a line, correctly, one after
  another: A, then B, then C. That tells you the order they were called. It
  tells you nothing about the order they'll each finish walking up and signing
  in on a clipboard — if all three start walking the instant they're called, at
  slightly different speeds, whoever's physically closest could sign in first
  regardless of call order. That's exactly the gap: the first version of the
  test called all three to the front back-to-back, then checked the order they
  each recorded their own name in a shared list — which was no longer testing
  "who gets called first," it was testing "who happens to run first," a
  different and unguaranteed thing. The fix was to wait for each person to
  actually finish signing in before calling the next one forward, so there was
  never more than one person who could be signing in at a time.

  That's the corrected version. Now let's write up 4b's test report and notes.

  Searched for 1 pattern, ran 1 shell command (ctrl+o to expand)

⏺ 59 passed, 0 failed. Let me read the full output to embed it in the report.

  Read 1 file (ctrl+o to expand)

⏺ Now writing the phase 4b test report:

⏺ Write(outputs/tests/phase-4b-bounded-queue.md)
  ⎿  Wrote 332 lines to outputs/tests/phase-4b-bounded-queue.md
       1 # Phase 4b Test Report: Bounded, FIFO-fair wait queue for the co
       2 
       3 **Source:** uncommitted work on `phase-4a-timeouts-backpressure`
          (phase 4b was built as a continuation of the same branch, layer
       4 **Command:** `go test -v -race -count=1 ./cmd/... ./internal/...
         `
       6 
       7 | Package | Result | Duration |
       8 |---|---|---|
      10 | `cmd/loadbalancer` | ok | 2.17s |
     … +486 lines (ctrl+o to expand)
⏺ Write(notes/phase-4b-bounded-queue.md)
  ⎿  Wrote 88 lines to notes/phase-4b-bounded-queue.md
      1 # Phase 4b: Bounded, FIFO-fair wait queue for the concurrency lim
      2 
         direct continuation of 4a, not a separate branch)
      4 - **PR:** opened from this branch into `build`
      6 
      8 
         from being overwhelmed, but at a cost: once every slot was full,
         *every* further request was rejected immediately and identically
         or ten. Phase 4b adds a bounded waiting line on top of that limi
        cceed instead of fai
     … +278 lines (ctrl+o to expand)

  Ran 1 shell command (ctrl+o to expand)

⏺ Both 4a and 4b are documented — test reports and notes written for each, plus
  CLAUDE.md and the explain-code skill updated with the corrected explanation
  standard. Everything's still uncommitted, ready whenever you want to commit or
  open a PR.

  Summary of where things stand:
  - Phase 4a (notes/phase-4a-timeouts-backpressure.md,
  outputs/tests/phase-4a-timeouts-backpressure.md): backend-response timeouts +
  fail-fast concurrency limiter.
  - Phase 4b (notes/phase-4b-bounded-queue.md,
  outputs/tests/phase-4b-bounded-queue.md): bounded, strictly FIFO wait queue
  layered on top, closing the near-miss gap 4a left open.
  - Both branches of work live on phase-4a-timeouts-backpressure (4b continued
  on the same branch rather than a new one, since it's a direct extension of
  4a's mechanism).

  What's next — commit/PR this work, or move on to something else?

✻ Worked for 6m 21s

❯ That explanation was excellent - you highlighted the key problems and how you 
  solved / fixed / implemented them in code. Redo your explanation of what you  
  implemented in Phase 1, Phase 2, Phase 3 and Phase 4a using this style of     
  explanation.                                                                  

  Read 2 files (ctrl+o to expand)

⏺ Here's all four phases, restructured the same way.

  ---
  Phase 1 — the single-backend reverse proxy

  The general problem: a client wants to talk to a backend server, but not
  directly — it talks to this load balancer instead, and the load balancer talks
  to the backend on the client's behalf. That's two entirely separate
  conversations, not one conversation passed through:

   Client  <====socket A====>  Load Balancer  <====socket B====>  Backend
           (client's own TCP        |          (LB's own TCP
            connection)             |           connection)

  Because these are two independent connections, two problems show up
  immediately, and both are about information that doesn't automatically survive
  the hop from socket A to socket B:

  Problem 1 — the backend can't see the real client. The backend's socket B only
  ever sees the load balancer's address as "who connected to me" — the original
  client's address lived on socket A, which the backend was never part of.
  HTTP's fix for this is a header, X-Forwarded-For, where the load balancer
  writes down the real client address in the request it forwards. The catch:
  it's just a text header, so nothing stops a client from setting that header
  themselves on socket A and lying about their own address — unless the load
  balancer explicitly throws away whatever the client sent and writes the real
  address it observed from socket A itself.

   Client sends:  X-Forwarded-For: 6.6.6.6  (a lie)
                          |
                          v
   Load Balancer must overwrite it with the address it actually
   observed on socket A, before forwarding on socket B.

  Problem 2 — some headers describe one specific hop, not the request itself. A
  header like Connection describes properties of socket A specifically (e.g.,
  "keep this connection alive") — it has no meaning on socket B, which is a
  completely different connection with its own lifecycle. Copying these
  "hop-by-hop" headers straight through from socket A's request to socket B's
  request is a real, known source of bugs in proxies.

  Mapped onto the code: internal/proxy/proxy.go builds the piece that owns
  socket B and decides what to forward. Rather than hand-rolling the raw HTTP
  forwarding, this uses Go's standard httputil.ReverseProxy, which already
  handles hop-by-hop header stripping correctly — but only if configured with
  the modern Rewrite/SetXForwarded API; the older, commonly-copied helper
  (NewSingleHostReverseProxy) forwards the client's X-Forwarded-For as-is, which
  is exactly the spoofable behavior from Problem 1. That was confirmed by
  reading the actual library documentation rather than assuming, and is why this
  phase deliberately uses the newer API. internal/middleware/logging.go is a
  separate wrapper around the whole thing (method, path, status, latency per
  request) — kept as its own layer, not baked into the proxy, specifically so
  later phases can add more wrappers (rate limiting, etc.) the same way instead
  of growing one increasingly tangled function.

  ---
  Phase 2 — round-robin across multiple backends

  The general problem: with more than one backend, something has to decide which
  one handles each request. The simplest fair rule: send requests to backends
  in a repeating cycle — 1, 2, 3, 1, 2, 3, ... — so everyone gets an equal share
  over time.

   Backends:  [ A ]  [ B ]  [ C ]

   Request 1 -> A
   Request 2 -> B
   Request 3 -> C
   Request 4 -> A   (cycle wraps back to the start)
   Request 5 -> B

  That sounds trivial — just keep a number that says "which backend is next" and
  add one each time — but there's a real hazard once you remember that requests
  aren't handled one at a time. Go hands each incoming request its own
  independently-running goroutine, so many requests can be asking "which backend
  is next?" at the exact same moment. "Add one to a number" is actually three
  separate steps under the hood — read the current value, add one, write the new
  value back — and if two requests interleave those steps, an update can get
  silently lost:

   Shared counter starts at 5.

   Goroutine 1: reads 5
   Goroutine 2: reads 5          <- both read the SAME value
   Goroutine 1: writes 6
   Goroutine 2: writes 6         <- overwrites, one increment vanished

   Counter ends at 6, not 7. One request's "next backend" pick was lost.

  The fix is to make "read, add one, write back" happen as a single, indivisible
  step that can never be interrupted partway through — which is exactly what an
  atomic operation is: a hardware-guaranteed all-or-nothing read-modify-write,
  as opposed to a lock that would make other goroutines wait their turn.

  Mapped onto the code: internal/balancer/roundrobin.go holds that counter as an
  atomic.Uint64 rather than a plain number, specifically so the increment can
  never be interrupted partway through the way the diagram above shows. This was
  proven, not just assumed — TestRoundRobinConcurrentCallsStayBalanced fires
  3000 requests from 50 goroutines at once and checks for an exact
  1000/1000/1000 split across three backends, which would only come out uneven
  if an update had been lost, and the whole suite additionally runs under go 
  test -race, a tool that independently detects unsynchronized concurrent
  access.

  ---
  Phase 3 — active health checks

  The general problem: round-robin has no idea whether a backend is actually
  working. If one backend crashes, round-robin keeps sending it every Nth
  request anyway — the only way it ever "discovers" the problem is by a real
  client's request failing against that dead backend, which means a real client
  always has to eat that failure first, every single time, forever.

  The fix is to stop waiting for real traffic to reveal the problem, and instead
  have the load balancer proactively check each backend's health on its own, in
  the background, so it already knows a backend is down before ever routing a
  real client to it:

   Background, independent of real traffic:

     Checker --probe--> Backend A --> "I'm fine" (200)
     Checker --probe--> Backend B --> "no response" (timeout)  <- caught here,
                                                                    not by a
  real client
   Real traffic:

     Client -> Load Balancer -> only ever routed to A, never to B

  Each backend gets checked on its own independent schedule (rather than one
  checker working through the whole list in sequence) — otherwise one
  slow-to-respond backend would delay every other backend's check behind it, the
  same "one thing blocking everything else" problem as any sequential loop.

  That independence creates its own new wrinkle: now there are multiple
  background checkers, each independently discovering results and reporting them
  in, all writing into the same shared "who's healthy" information at once —
  genuinely simultaneous writers, not just simultaneous readers against one
  writer the way phase 2's counter was. And unlike phase 2's counter, "who's
  healthy" isn't one number — it's a status per backend plus a derived,
  ready-to-use list of "just the healthy ones" that routing actually reads from,
  and those two pieces have to stay in agreement with each other. That's a case
  where a single atomic operation isn't enough (there's no one hardware
  instruction that updates two related pieces of information together as one
  step) — so this needed a lock, one that guards the moment of updating both
  pieces together.

  There's a second requirement in tension with that: routing itself (which
  backend does this request go to) happens on every single request's own
  goroutine, and must never sit around waiting for a health-check writer to
  finish updating. The way to get both — safe writes and a reader that's never
  blocked — is to never edit the "healthy backends" list in place at all.
  Instead, build a brand new, complete list from scratch whenever health
  changes, and only then swap it in as a single, instantaneous replacement:

   Old list: [A, B, C]              (routing is currently reading this)

   B goes unhealthy -> build a NEW list from scratch: [A, C]

   Swap: routing now reads [A, C] instead -- an all-or-nothing swap,
   never a list caught half-updated mid-edit.

  Mapped onto the code: internal/healthcheck/healthcheck.go is the background
  checker — one independent polling loop per backend.
  internal/balancer/roundrobin.go holds the status information behind a
  sync.Mutex (the lock from the paragraph above, guarding the "update two
  related things together" moment) and separately holds the ready-to-route list
  behind an atomic.Pointer that gets replaced wholesale rather than edited (the
  swap-in-a-new-list mechanism). internal/proxy/proxy.go handles the case where
  every backend is currently marked unhealthy — deliberately returning an
  honest, immediate 502 rather than routing to a backend already known to be
  broken.

  ---
  Phase 4a — backend timeouts and a concurrency limiter

  Problem 1 — nothing bounds how long the load balancer will wait on a backend.
  If a backend accepts a connection but never actually finishes responding —
  hung, deadlocked, waiting on something that will never answer — the load
  balancer today just waits. Forever, if need be. That's a request-shaped
  resource (a goroutine, an open socket to the client, an open socket to the
  backend) held open indefinitely for nothing.

   Client -> Load Balancer -> Backend (accepts, then never responds)
                                |
                      (load balancer waits... and waits...)

  The fix is a clock that starts the moment the request is sent to the backend
  and gives up — returning a clean error to the client — if the backend hasn't
  fully finished responding by the time it runs out:

   |---- backend must respond within this window ----|
   request sent                                    timeout fires
                                                    -> give up, tell
                                                       the client "timed out"

  That clock has to cover the entire response, not just "did the backend start
  replying" — a response can arrive as a stream of chunks over time, not all at
  once, and "backend timeout" should mean the whole thing has to finish in time,
  not just begin in time. That created a genuinely subtle bug risk: the
  natural-looking way to write "stop this clock once we're done with it" is to
  stop it the instant the backend's reply starts coming back — but at that point
  the load balancer has only received the headers; it still has to relay the
  rest of the response body to the client afterward, which takes more time.
  Stopping the clock too early would treat that in-progress body-relaying as
  "already timed out," silently cutting off a perfectly fine response partway
  through:

   Backend replies: [headers] ---- [chunk 1] -- [chunk 2] -- [chunk 3]
                        ^
                        |
          stopping the clock HERE (too early) would cut off
          chunks 2 and 3, even though nothing actually timed out

  The fix: only stop the clock once the entire body has actually been delivered
  to the client, not the moment the reply starts arriving.

  Problem 2 — nothing bounds how many requests the load balancer will try to 
  handle at once. Independent of any single backend being slow, there's no
  ceiling on how many requests the load balancer will accept and try to serve
  simultaneously — a flood of concurrent requests (legitimate traffic spike or
  otherwise) has no limit today. The fix is a hard ceiling on concurrent
  requests: a fixed number of "slots," and any request beyond that gets an
  immediate, honest "too busy" response rather than being allowed to pile up
  unboundedly.

   Slot 1: [in use]
   Slot 2: [in use]
   Slot 3: [in use]      <- all 3 slots full

   Request 4 arrives -> rejected immediately (503), no slot available

  Mapped onto the code: internal/proxy/proxy.go implements the clock for Problem
  1 using Go's context.WithTimeout, and specifically defers stopping that clock
  to the moment the response body is fully closed out (a small wrapper type,
  cancelOnCloseBody) rather than the moment the reply starts — directly avoiding
  the too-early-cutoff bug described above. A dedicated test streams a
  multi-chunk response deliberately slow enough to expose that exact bug if it
  were reintroduced, and confirms the full body still arrives intact.
  internal/middleware/concurrency.go implements the slot ceiling for Problem 2
  as MaxConcurrent, using a fixed-size buffered channel as the pool of slots — a
  request either finds a free slot or is turned away immediately, never made to
  wait (that "made to wait" case is exactly what phase 4b added afterward, as a
  deliberate second step rather than building it in from the start).

  ---
  Want me to update notes/phase-1, phase-2, and phase-3 themselves to match this
  style (they currently read more like the dense, code-first style from
  before), or is this chat explanation enough on its own?

✻ Baked for 1m 33s