╭─── Claude Code v2.1.221 ─────────────────────────────────────────────────────╮
│                                                    │ Tips for getting        │
│                 Welcome back Jerry!                │ started                 │
│                                                    │ Run /init to create a … │
│                      ▗ ▗   ▖ ▖                     │ ─────────────────────── │
│                                                    │ What's new              │
│                        ▘▘ ▝▝                       │ Cost estimates (`/cost… │
│   Sonnet 5 · Claude Pro · jerryschen@gmail.com's   │ Added the one-time ful… │
│   Organization                                     │ Added `/claude-api upg… │
│       ~/gh/public/system-design-load-balancer      │ /release-notes for more │
╰──────────────────────────────────────────────────────────────────────────────╯


❯ /model                                                                        
  ⎿  Set model to Sonnet 5 and saved as your default for new sessions

❯ /init                                                                         

  Searched for 1 pattern, read 1 file, listed 1 directory (ctrl+o to expand)

⏺ This repository is essentially empty — just a README.md (one-line description)
  and LICENSE (MIT). There's no code, no build system, no tests, and no
  Cursor/Copilot rules to import. A CLAUDE.md with commands/architecture would
  be fabricated, which goes against the instructions not to make things up.

  Read 1 file (ctrl+o to expand)

⏺ Write(CLAUDE.md)
  ⎿  User rejected write to CLAUDE.md
      1 # CLAUDE.md
      2 
      3 This file provides guidance to Claude Code (claude.ai/code) when 
        working with code in this repository.
      4 
      5 ## Project status
      6 
      7 This repository currently contains no code — only a `README.md` a
        nd `LICENSE` (MIT). Per the README, the goal is to build a load b
        alancer as a system-design learning exercise.
      8 
      9 There are no build, lint, or test commands yet because no source 
        files, language, or tooling have been chosen. When implementation
         begins, update this file with the actual commands and architectu
        re notes once they exist — don't guess at them in advance.

✻ Worked for 40s

❯ /init                                                                         

⏺ Understood — I won't create the CLAUDE.md. Let me know if you'd like to
  proceed differently (e.g., wait until there's actual code, or write a more
  minimal placeholder).

✻ Baked for 1s

❯ /init Create the CLAUDE.md now. I have filled in details of this repo and     
what I want to get out of you as a coding agent, within README.md               

  Read 1 file (ctrl+o to expand)

⏺ Write(CLAUDE.md)
  ⎿  Wrote 27 lines to CLAUDE.md
      1 # CLAUDE.md
      3 This file provides guidance to Claude Code (claude.ai/code) when 
        working with code in this repository.
      4 
      6 
      7 This is a learning project, not a production build. The end goal 
        to learn System Design fundamentals by building it — the code is 
        a vehicle, not the deliverable. The user is preparing for System 
        Design interviews.
      9 ## Who you're working with
     … +46 lines (ctrl+o to expand)

  Listed 1 directory (ctrl+o to expand)

⏺ No existing memory for this project — I'll create it now.

  Wrote 4 memories (ctrl+o to expand)

⏺ Created CLAUDE.md, which captures the repo's real purpose: a Go load balancer
  built as a vehicle for learning System Design, not a production deliverable.
  It documents your background, and sets the collaboration mode explicitly —
  explain concepts and trade-offs as we go, build incrementally, don't just hand
  over finished code. I also saved this context to memory so future sessions
  pick up the same approach automatically.

  Ready to start whenever you are — I'd suggest we begin with the most basic
  version (single backend, no algorithm) and build up from there, discussing
  each addition as a System Design concept. Want to start there?

✻ Cogitated for 45s

❯ Forgot to add - I would like to document this process cleanly, so I can       
  easily reference back to this in interviews. What I am envisioning is, for    
  each phase of this as we incrementally build towards a production-like load   
  balancer, we create a new branch for each phase and capture the following:    
  1. What was built in the phase                                                
  2. What are the learnings and System Design concepts and considerations       
  3. Our back and forth conversation, since I doubt each phase will be one-shot 
  4. The code changes of course                                                 
  5. All of the various tests, including non-functional tests that stress test  
  (or break) the load balancer and show weaknesses, vulnerabilities, areas to   
  improve etc                                                                   
                                                                                
  We end each phase with a PR back to build, with notes on these things in the  
  PR as well as in appropriate, well-organized notes markdown files.            

  Read 1 file (ctrl+o to expand)

⏺ Update(CLAUDE.md)
  ⎿  Added 13 lines
      18  - When a design decision has real System Design weight (e.g., l
          oad balancing algorithm choice, health-check strategy, connecti
          on handling, consistent hashing, statelessness vs. sticky sessi
          ons), surface it explicitly and explain the trade-offs rather t
          han silently picking one.
      19  - It's fine to write code, but check that the user understands 
          the "why" before moving to the next step — favor discussion and
           incremental review over large autonomous implementation passes
          .
      20  
      21 +## Phase workflow                                              
      22 +                                                               
      23 +Work happens in phases, each building toward a more production-
         +like load balancer, so the user can reference this history back
         + in System Design interviews. `build` is the integration branch
         + (not `master`) — every phase branches off `build` and PRs back
         + into it.                                                      
      24 +                                                               
      25 +For each phase:                                                
      26 +                                                               
      27 +1. Branch off `build`, named `phase-N-short-description` (e.g. 
         +`phase-1-single-backend-proxy`).                               
      28 +2. Build the increment for that phase (see "How to collaborate"
         + above — teach as you go, keep it incremental).                
      29 +3. Write functional tests, and also non-functional/stress tests
         + that deliberately try to break or degrade the load balancer (e
         +.g. backend timeouts, slow/hanging connections, backend crashes
         + mid-request, connection floods, thundering herd on health-chec
         +k recovery) to surface real weaknesses.                        
      30 +4. Write up `notes/phase-N-short-description.md` using `notes/T
         +EMPLATE.md` — capture what was built, the System Design concept
         +s/trade-offs learned, a curated summary of the key design discu
         +ssion (not a raw transcript dump — a distilled narrative of the
         + decisions and why), and the test results including weaknesses/
         +vulnerabilities found and what's deferred to a later phase.    
      31 +5. Open a PR from the phase branch into `build`. The PR descrip
         +tion should summarize the same points (what/why/tests/known wea
         +knesses) so the PR itself is a standalone artifact; link the no
         +tes file from it.                                              
      32 +6. Only commit or open the PR when the user asks — don't do thi
         +s proactively at the end of a phase without confirmation.      
      33 +                                                               
      34  ## Commands
      35  
      36  No source code exists yet. Once the Go module is initialized, t
          he standard commands will apply:

⏺ Now let's create the notes template.

  Ran 1 shell command (ctrl+o to expand)

⏺ Write(notes/TEMPLATE.md)
  ⎿  Wrote 38 lines to notes/TEMPLATE.md
      1 # Phase N: <short title>
      2 
      3 - **Branch:** `phase-N-short-description`
      4 - **PR:** <link once opened>
      5 - **Status:** in progress / complete
      7 ## 1. What was built
      8 
      9 Plain description of the increment delivered in this phase — scop
        e in, and just as importantly, scope explicitly left out (deferre
        d to a later phase).
     … +40 lines (ctrl+o to expand)
  Wrote 2 memories (ctrl+o to expand)

⏺ Set up and saved to memory. Summary of what's in place now:

  - CLAUDE.md — added a "Phase workflow" section: branch off build as
  phase-N-short-description, build the increment, write functional +
  stress/chaos tests, write notes/phase-N-....md, PR back into build with the
  same summary. Commits/PRs only happen when you ask.
  - notes/TEMPLATE.md — the six-section template (what was built, SD
  concepts/trade-offs, curated design-discussion narrative, code changes,
  functional + non-functional/stress tests, known weaknesses/open questions).

  One judgment call worth flagging: for "our back and forth conversation," I'm
  planning to write a curated narrative of the key decisions and debates per
  phase rather than pasting a raw transcript — reads better for interview
  reference and stays honest about what actually mattered. Let me know if you'd
  rather have raw transcript excerpts instead.

  These files are created but not committed. Ready to start Phase 1 (I'd
  suggest: a bare TCP/HTTP reverse proxy to a single backend, no algorithm yet —
  the simplest possible starting point) whenever you want — just say go.

✻ Crunched for 53s

❯ Yes, let's start Phase 1 as "a bare TCP/HTTP reverse proxy to a single        
  backend, no algorithm yet". Remember to teach me as we go along - don't       
  implement and code without me understanding what you are doing. I don't need  
  to know about each and every line of code, but I need to understand the       
  concepts and thought process and considerations behind what you're building.  

⏺ Good — before I write any code, let's settle one foundational decision, since
  it shapes everything downstream: what layer does this proxy operate at?

  Layer 4 (TCP) proxying: The load balancer just shuffles raw TCP bytes between
  client and backend without looking inside them. It doesn't know it's HTTP —
  could be anything (a database connection, gRPC, raw sockets). Fast, low
  overhead, but "dumb": it can't route based on URL path, can't read HTTP
  headers, can't do things like sticky sessions via cookies, and typically
  load-balances at the connection level rather than the request level (so one
  long-lived TCP connection with many HTTP requests on it — keep-alive — all go
  to the same backend, whether you want that or not).

  Layer 7 (HTTP) proxying: The load balancer parses the HTTP request, so it can
  route by path/host/header, inspect and rewrite requests, balance at the
  request level even over a single kept-alive connection, and do smarter health
  checks (e.g. "does this backend return 200 on /healthz" vs just "is the TCP
  port open"). More overhead, more complexity, but it's what almost all
  real-world "load balancer" system-design questions (and things like nginx,
  HAProxy in HTTP mode, or an ALB) actually mean.

  Most interview-style "design a load balancer" discussions live at L7, and it's
  the layer where the interesting distributed-systems concepts (health checks,
  retries, request-level algorithms) actually show up — so that's my
  recommendation for Phase 1. But L4 is the simpler mental model and worth
  knowing too.

⏺ User declined to answer questions
  ⎿  · Which layer should Phase 1's reverse proxy operate at? (Layer 7 (HTTP) 
     (Recommended) / Layer 4 (TCP) / Build both, L4 first)

⏺ What would you like to clarify about the L4 vs. L7 choice?

✻ Cooked for 21s

❯ One consideration is that I do need to learn about rate-limiting of           
  individual IPs as well as request caching, for situations where there is a    
  sudden spike in incoming requests or data into the load balancer. I'm not     
  sure if starting with just an L4 load balancer would be simpler to learn      
  about these things? Or should we just go ahead and implement an L7 load       
  balancer? I'll leave it to you to decide.                                     

⏺ That resolves it — go with L7 (HTTP).

  Here's why those two goals settle it:

  - Request caching is inherently an L7 concept. To cache a response you need to
  know what was requested — method, path, query params, headers like
  Cache-Control/ETag — so you can decide if two requests are "the same" and
  whether a cached response is still valid. At L4 you're just watching a stream
  of bytes with no notion of "a request" at all; caching isn't expressible
  there.
  - Per-IP rate limiting works at either layer, but L7 gives you more useful 
  control. At L4 you can only throttle by connection/byte-rate per source IP. At
  L7 you can throttle per-request, and later get more precise (e.g. cheap
  endpoints get a higher limit than expensive ones) — a distinction that shows
  up constantly in real system-design interviews.

  If we started at L4 and moved to L7 later, the L4 work wouldn't really carry
  forward for either goal — it'd be a separate detour rather than a foundation.
  So we'd be spending Phase 1 on a layer we'd have to leave behind anyway.

  Confirming: Phase 1 = L7 HTTP reverse proxy to a single backend, no algorithm 
  yet (that comes in Phase 2 with multiple backends). Rate limiting and caching
  will be later phases once the basic request path exists. Sound good — should I
  go ahead and set up the branch and start building?

✻ Cogitated for 14s

❯ Yes                                                                           

⏺ Let's get the mechanics straight before I write anything.

  What a reverse proxy actually has to do, at minimum:
  1. Accept an incoming HTTP request from a client.
  2. Open (or reuse) a connection to the backend, and forward an equivalent
  request.
  3. Get the backend's response and relay it back to the client.
  4. Correctly handle the parts of HTTP that are easy to get subtly wrong — e.g.
  hop-by-hop headers (Connection, Keep-Alive, Transfer-Encoding, etc., per
  RFC 7230) must be stripped and regenerated between hops, not blindly copied,
  or you get protocol bugs and even request-smuggling vulnerabilities in
  production systems. Streaming request/response bodies (not just buffering
  everything in memory) also matters once bodies get large.

  A build decision worth naming: I could have us hand-write the raw socket-level
  forwarding to fully see the mechanics, or use Go's standard library
  net/http/httputil.ReverseProxy, which already implements point 4 correctly.
  Real load balancers (nginx, HAProxy, Envoy) don't hand-roll this either — it's
  exactly the kind of "easy to get subtly wrong" plumbing you delegate. My
  call: we use httputil.ReverseProxy as the transport primitive (so we're not
  debugging chunked-encoding edge cases instead of learning system design), but
  we wrap it in our own package and I'll walk through what it's doing for us, so
  it isn't a black box.

  Setting up now — branch, Go module, and a first working version.

  Ran 3 shell commands (ctrl+o to expand)

⏺ Good — those docs are already committed on build (commit 724e67b). Now
  creating the Phase 1 branch and Go module.

  Ran 4 shell commands (ctrl+o to expand)

⏺ Let me verify the exact stdlib behavior before I write tests that assert on
  it, rather than relying on memory.

  Ran 2 shell commands (ctrl+o to expand)

⏺ One more thing worth flagging before I write the code — I checked the actual
  stdlib docs rather than relying on memory, and it changes the design:

  The convenience helper httputil.NewSingleHostReverseProxy uses an older,
  deprecated code path that preserves whatever X-Forwarded-For header the client
  sent. That's a real vulnerability: a client could fake X-Forwarded-For: 
  1.2.3.4 to spoof its source IP — which would directly undermine the per-IP
  rate limiting we know is coming in a later phase. Go's newer Rewrite-based API
  (via ProxyRequest.SetXForwarded()) strips any client-supplied forwarding
  headers first and then sets them correctly from the real connection. So I'm
  building on the modern API, not the convenience helper — small difference in
  code, meaningfully different security property.

  Now writing the code.

  Ran 1 shell command (ctrl+o to expand)

⏺ Write(internal/proxy/proxy.go)
  ⎿  Wrote 29 lines to internal/proxy/proxy.go
       1 // Package proxy builds the HTTP reverse proxy that forwards cli
         ent
       2 // requests to a backend server.
       3 package proxy
       4 
       6   "log"
       7   "net/http"
       8   "net/http/httputil"
       9   "net/url"
      10 )
     … +24 lines (ctrl+o to expand)

⏺ Write(internal/middleware/logging.go)
  ⎿  Wrote 36 lines to internal/middleware/logging.go
       1 // Package middleware holds cross-cutting HTTP concerns (logging
          now;
       2 // rate limiting and caching will land here in later phases) tha
       3 // the core proxy handler rather than living inside it.
       4 package middleware
       5 
       6 import (
       8   "net/http"
       9   "time"
      10 )
     … +33 lines (ctrl+o to expand)

⏺ Write(cmd/loadbalancer/main.go)
  ⎿  Wrote 56 lines to cmd/loadbalancer/main.go
       1 // Command loadbalancer runs the reverse proxy.
       2 package main
       3 
       4 import (
       5   "context"
       6   "errors"
       7   "flag"
       8   "log"
       9   "net/http"
      10   "net/url"
     … +52 lines (ctrl+o to expand)

  Ran 1 shell command (ctrl+o to expand)
⏺ It builds. Let me pause and explain the three pieces before moving to tests:

  - internal/proxy/proxy.go — the actual forwarding logic: rewrite the request
  to point at the backend, strip/reset forwarding headers securely, return 502
  with a logged reason if the backend can't be reached.
  - internal/middleware/logging.go — wraps any handler to log
  method/path/status/latency. I split this out deliberately: it's not
  proxy-specific, and rate limiting / caching (coming later) will plug in the
  same way — as middleware wrapping the proxy, not code jammed inside it.
  - cmd/loadbalancer/main.go — wires it together, plus one thing I added without
  flagging it yet: graceful shutdown. On SIGINT/SIGTERM it stops accepting new
  connections but gives in-flight requests up to 10s to finish before exiting,
  via http.Server.Shutdown. Worth understanding even at this stage — without it,
  a kill or container restart just severs connections mid-response.

  Now let's write the tests — functional first, then the stress tests that try
  to break it.

⏺ Write(internal/proxy/proxy_test.go)
  ⎿  Wrote 137 lines to internal/proxy/proxy_test.go
       1 package proxy
       2 
       3 import (
       4   "io"
       5   "log"
       6   "net/http"
       7   "net/http/httptest"
       8   "net/url"
       9   "testing"
      10 )
     … +137 lines (ctrl+o to expand)

  Ran 1 shell command (ctrl+o to expand)

⏺ Now the stress tests — designed to actually find weaknesses, not just confirm
  the happy path:

⏺ Write(internal/proxy/stress_test.go)
  ⎿  Wrote 133 lines to internal/proxy/stress_test.go
       1 package proxy
       2 
       3 import (
       4   "fmt"
       5   "net/http"
       6   "net/http/httptest"
       7   "net/url"
       8   "sync"
       9   "testing"
      10   "time"
     … +147 lines (ctrl+o to expand)

  Ran 1 shell command (ctrl+o to expand)

⏺ All tests pass, and the timeout test explicitly surfaced the weakness it was
  designed to find. Let me run gofmt to confirm formatting, then quickly
  smoke-test the actual binary end-to-end (not just go test).

  Ran 3 shell commands (ctrl+o to expand)

⏺ The smoke test itself worked — the log line shows the real end-to-end path:
  GET / 200 4.491375ms, and curl got status: 200. The timeout was just my
  cleanup command (wait hung on backgrounded jobs), not the load balancer; I've
  killed the leftover processes.

  Phase 1 is functionally done. Before I write it up in notes/, let's make sure
  the concepts landed. Quick check:

⏺ User declined to answer questions
  ⎿  · Before I write up the notes doc, want to walk through anything again? 
     (e.g. why X-Forwarded-For spoofing matters, hop-by-hop headers, why 
     middleware is separate from the proxy, or the no-timeout weakness we found)
     (All clear, write the notes / Explain X-Forwarded-For spoofing more / 
     Explain hop-by-hop headers more / Explain the no-timeout weakness more)

⏺ What would you like to clarify?

✻ Worked for 5m 54s

❯ Quick note - the way you are explaining things is a little too advanced for   
  my current level. I don't know what "raw socket-level forwarding" means (the  
  idea of a "socket" is still not even crystal clear to me). And "hop-by-hop    
  headers", "X-Forwarded-For header" are foreign to me too. I am also curious   
  how load balancers actually are - are they usually just a process that runs   
  on a machine ? Like when I deploy an ALB on AWS, what am I actually           
  deploying?                                                                    

⏺ Good catch — let me back up and build this from the ground up. No code in this
  message, just concepts.

  What's a socket?

  When you use AWS every day, you're used to thinking in terms of "services"
  (S3, EC2, an API). Underneath almost all of them, at the OS level, network
  communication happens through a socket: the operating system's handle for "one
  end of a connection between two programs." Think of it like a phone handset —
  your program picks it up, "dials" an address (an IP + port number, e.g.
  10.0.1.5:8080), and once connected, it can write bytes in and read bytes out,
  without your program needing to know anything about how those bytes actually
  cross the network (packet routing, retransmission on loss, etc. — the
  OS/kernel handles all of that beneath the socket).

  Concretely, in our load balancer: our process opens a listening socket on port
  8080. When a client connects, the OS hands us a new socket dedicated to that
  one client. Separately, our process opens another socket to the backend on
  port 9000. So one incoming HTTP request actually involves two independent
  socket connections stitched together: client↔LB and LB↔backend.

  "Raw socket-level forwarding" (the L4 option I mentioned earlier) means: just
  copy bytes from one socket to the other, blindly, with zero understanding that
  those bytes happen to form an HTTP request. That's simple but "dumb." Layer 7
  means: actually parse those bytes as HTTP first.

  What's an HTTP header, and what's "hop-by-hop"?

  An HTTP request is just structured text sent over that socket: a first line
  (GET /widgets HTTP/1.1), then a series of Key: Value header lines (metadata
  about the request), a blank line, then an optional body. E.g. Content-Type: 
  application/json tells the receiver how to interpret the body.

  Most headers describe the request itself and should survive the whole trip
  from browser → load balancer → backend unchanged (e.g. Content-Type,
  Authorization).

  A few headers instead describe properties of one specific socket connection —
  one "hop" — not the request as a whole. Connection: keep-alive is the client
  telling whichever program it's directly talking to — "please don't close this
  particular socket, I want to reuse it for more requests." That's meaningful
  for the client↔LB socket. But our LB↔backend socket is a completely separate 
  connection with its own independent lifecycle — blindly copying that header
  onto it doesn't make sense and can actually cause real bugs (misinterpreting
  where one HTTP message ends and the next begins, in the chunked-encoding case
  — this is the class of bug historically behind "request smuggling" attacks).
  So those specific headers must be stripped/regenerated at each hop rather than
  copied through — that's what "hop-by-hop" means.

  What's X-Forwarded-For?

  Because the load balancer sits in the middle, the backend's socket only ever
  sees connections arriving from the load balancer's IP — it has no built-in way
  to know the original client's real IP address. But often you want that (e.g.
  "block requests from this abusive IP," or just logging). So the LB adds a
  custom header, X-Forwarded-For: <real client IP>, as a side-channel note to
  the backend: "FYI, this didn't originate from me — it originated from this
  address."

  The spoofing risk: that header is just text. Nothing stops a client from
  setting X-Forwarded-For: 1.2.3.4 in their own request before it even reaches
  us, lying about their identity. If we naively forwarded whatever arrived, and
  a later system (e.g. a per-IP rate limiter — exactly what you want to build
  later) trusted that header, an attacker could claim any IP they want and dodge
  the limit entirely. That's why our code discards whatever the client sent and
  sets it fresh from the actual socket's real source address, which the client
  cannot fake — it's determined by the TCP connection itself, not by anything
  the client wrote in a header.

  What is a load balancer, physically? What are you actually deploying with an 
  ALB?

  Our Go program, once built, is just an ordinary OS process — same category of
  thing as python3, nginx, or any bioinformatics pipeline binary you run. It
  opens a listening socket and sits there handling connections until killed.
  That's literally what we ran a minute ago.

  A self-managed load balancer (say, someone running nginx or HAProxy, or this
  very program) is that same idea: an EC2 instance running that process 24/7 in
  front of your other EC2 instances. Problem: if that one instance or process
  dies, everything behind it becomes unreachable — so now that single point of
  failure needs its own redundancy, which is a recursive problem (who
  load-balances the load balancer?).

  An AWS ALB is AWS's answer: they run that same conceptual thing — many
  redundant instances of proxying software — across multiple physical machines
  and multiple Availability Zones, and give you one stable DNS name in front of
  all of it. When you "deploy an ALB," you're not uploading a binary or picking
  a VM size — you're just handing AWS a config (which backends/"target group" to
  forward to, health-check rules, routing rules) and they operate the actual
  fleet of processes: scaling it, patching it, keeping it redundant across AZs,
  terminating TLS, etc.

  So: what we're building here is, conceptually, the same kind of engine an ALB
  runs internally — just a single unscaled, unmanaged instance of it. Everything
  we add in later phases (multiple backends, health checks, algorithms) is
  exactly the kind of logic that engine needs; ALB just also solves the "run
  thousands of copies of this reliably across a whole cloud region" problem on
  top, which is a separate, much bigger infrastructure problem we're not
  tackling here.

  Does this land better? Happy to go deeper on any one of these before I write
  up the Phase 1 notes.

✻ Churned for 55s

❯ Make a note in CLAUDE.md to adjust your teaching level, based on my comment   
  here.                                                                         

  Read 1 file (ctrl+o to expand)

⏺ Update(CLAUDE.md)
  ⎿  Added 2 lines
      10  
      11  The user is a computational biologist / bioinformatics engineer
           (Caltech CS background, ~20 years removed from formal CS work)
           who runs bioinformatics pipelines on AWS and builds simple fro
          ntends for them. They are an experienced "applied" engineer com
          fortable with cloud infrastructure and daily tool use, but they
           have not built real software systems at scale and are new to G
          o and to core System Design concepts (load balancing algorithms
          , concurrency models, failure handling, scalability trade-offs,
           etc.).
      12  
      13 +**Calibrate explanations lower than you'd guess from "20 years 
         +of CS background."** In practice, foundational networking/OS te
         +rminology is not solid — e.g. what a socket is, HTTP header bas
         +ics, terms like "hop-by-hop header" or "X-Forwarded-For" landed
         + as unfamiliar jargon, not refreshers. Don't assume familiarity
         + with a term just because it's common in backend/networking wor
         +k — define it in plain language the first time it comes up, ide
         +ally anchored to something from their AWS/cloud-infra day job (
         +e.g. "a socket is like the OS-level version of the connection y
         +ou're used to thinking about when curling an ALB endpoint"). It
         +'s fine (expected) to build up from smaller building blocks rat
         +her than assuming the standard vocabulary.                     
      14 +                                                               
      15  ## How to collaborate on this repo
      16  
      17  - **Do not just implement features on request.** Treat every st
          ep as a teaching opportunity: explain the relevant System Desig
          n concept, the trade-offs between approaches, and why a particu
          lar approach was chosen, before or alongside writing code.

⏺ Update(CLAUDE.md)
  ⎿  Removed 1 line
      35  
      36  ## Commands
      37  
      38 -No source code exists yet. Once the Go module is initialized, t
         -he standard commands will apply:                               
      38  - `go build ./...` — build
      39  - `go test ./...` — run all tests (`go test ./... -run TestName
          ` for a single test)
      40  - `go vet ./...` — static checks

⏺ Now updating the persistent memory so this recalibration carries into future
  sessions too.

  Recalled 1 memory, wrote 1 memory (ctrl+o to expand)

❯ And don't explain things in terms of analogies. Just explain things in terms  
  of first principles. I have a technical background but its REALLY outdated -  
  from over 20 years ago at Caltech!                                            

  Wrote 1 memory (ctrl+o to expand)

⏺ Now let's update CLAUDE.md's calibration note the same way and add the memory
  pointer to the index.

⏺ Update(CLAUDE.md)
  ⎿  Added 1 line, removed 1 line
      10  
      11  The user is a computational biologist / bioinformatics engineer
           (Caltech CS background, ~20 years removed from formal CS work)
           who runs bioinformatics pipelines on AWS and builds simple fro
          ntends for them. They are an experienced "applied" engineer com
          fortable with cloud infrastructure and daily tool use, but they
           have not built real software systems at scale and are new to G
          o and to core System Design concepts (load balancing algorithms
          , concurrency models, failure handling, scalability trade-offs,
           etc.).
      12  
      13 -**Calibrate explanations lower than you'd guess from "20 years 
         -of CS background."** In practice, foundational networking/OS te
         -rminology is not solid — e.g. what a socket is, HTTP header bas
         -ics, terms like "hop-by-hop header" or "X-Forwarded-For" landed
         - as unfamiliar jargon, not refreshers. Don't assume familiarity
         - with a term just because it's common in backend/networking wor
         -k — define it in plain language the first time it comes up, ide
         -ally anchored to something from their AWS/cloud-infra day job (
         -e.g. "a socket is like the OS-level version of the connection y
         -ou're used to thinking about when curling an ALB endpoint"). It
         -'s fine (expected) to build up from smaller building blocks rat
         -her than assuming the standard vocabulary.                     
      13 +**Calibrate explanations lower than you'd guess from "20 years 
         +of CS background," and explain from first principles, not analo
         +gies.** Their CS background (Caltech) is real but over 20 years
         + stale — treat it as needing to be rebuilt from fundamentals, n
         +ot lightly refreshed. Foundational networking/OS terminology is
         + not solid — e.g. what a socket is, HTTP header basics, terms l
         +ike "hop-by-hop header" or "X-Forwarded-For" landed as unfamili
         +ar jargon, not refreshers. Don't assume familiarity with a term
         + just because it's common in backend/networking work — define i
         +t plainly the first time it comes up. The user explicitly asked
         + to skip analogies/metaphors (e.g. "a socket is like a phone ha
         +ndset") in favor of explaining actual mechanism — build up from
         + what's really happening at the OS/network level rather than re
         +aching for a comparison.                                       
      14  
      15  ## How to collaborate on this repo
      16  

  Recalled 1 memory, wrote 1 memory (ctrl+o to expand)
⏺ Updated both CLAUDE.md and persistent memory: explanations should be
  first-principles (actual mechanism — what's really happening at the OS/network
  level), not analogies, and I should treat your CS background as needing to be
  rebuilt from fundamentals rather than lightly refreshed.

  Want me to redo the socket/header/X-Forwarded-For explanation from earlier in
  that style, or should we move on to writing up the Phase 1 notes?

✻ Crunched for 44s

❯ Yes, redo the explanation from before in the first-principles style I asked   
  for. And just as a quick aside - given my outdated background, how realistic  
  is it that I can learn everything I need to learn to pass a Senior-level      
  System Design interview ? Or, how long will it take to build the right        
  foundation, realistically? I am aiming for AI agent developer and AI          
  infrastructure positions.                                                     

⏺ Sockets, from the OS up

  Two computers exchange data over a network as small chunks called packets —
  each carrying a bit of data plus addressing information (source IP:port,
  destination IP:port). Managing this directly — splitting data into packets,
  handling lost or out-of-order packets, retransmitting, reassembling — is
  genuinely hard and every network-using program would need to do it. So this
  logic is written exactly once, inside the operating system kernel (the "TCP/IP
  stack"), and the kernel exposes a much simpler interface to programs on top
  of it: the socket.

  Mechanically: a socket is an entry the kernel keeps in its own internal table,
  representing one specific ongoing connection between two IP:port pairs. Your
  program never touches that table directly — the kernel hands your program a
  small integer (a "file descriptor") that refers to that entry. From then on,
  your program calls generic OS functions like write(fd, data) and read(fd, 
  buffer) on that integer. The kernel is doing everything below "clean stream of
  bytes in, clean stream of bytes out" — chopping into packets, adding IP/port
  headers, retransmitting lost ones, putting arriving packets back in order.

  For our load balancer specifically: our process calls something equivalent to
  listen() on port 8080. That creates one socket whose only job is waiting for
  incoming connection attempts — it never carries request data itself. Each time
  a client connects, the kernel creates a brand-new, separate socket dedicated
  to that one client and hands our program its file descriptor. Go's net package
  wraps these exact OS calls; nothing magic is happening underneath it.

  HTTP headers and "hop-by-hop," from the byte level up

  A connected socket carries raw bytes — the kernel has no concept of what they
  mean. HTTP is just an agreed-upon text format both sides parse the same way. A
  request is literally bytes like:

  GET /widgets HTTP/1.1\r\n
  Host: example.com\r\n
  Content-Type: application/json\r\n
  Content-Length: 42\r\n
  \r\n
  {...body...}

  \r\n (carriage return + newline) separates lines, a blank line separates the
  headers from the body, and each Key: Value line is a header — plain text
  metadata about the request.

  In our load balancer there are two entirely separate sockets chained together:
  client↔LB, and LB↔backend. Each is its own independent TCP connection with
  its own state.

  Some headers describe the request/resource itself (Content-Type describes the
  body — true regardless of how many hops it crosses) and should pass through
  unchanged. But Connection: keep-alive means something different: originally,
  HTTP opened a new TCP connection per request and closed it afterward, which is
  wasteful (establishing a TCP connection requires a multi-step handshake
  before any data moves). Connection: keep-alive is the client telling whoever 
  it's directly talking to on this socket: "don't close this connection after
  responding — I'll send more requests on it." That's a statement about the
  client↔LB socket specifically.

  If we copied that header verbatim onto the LB↔backend request, we'd be
  asserting something about a socket it has nothing to do with — whether the LB
  wants to keep its own connection to the backend open is a completely
  independent decision. Headers that describe one specific socket hop rather
  than the request's actual content are called "hop-by-hop." HTTP names a short
  specific list of them (Connection, Keep-Alive, Transfer-Encoding, Upgrade)
  precisely because anything relaying HTTP across multiple hops needs to
  regenerate these fresh at each hop instead of copying them through — copying
  them through incorrectly is a real source of bugs, including a known attack
  class (request smuggling) around disagreements over where one HTTP message
  ends and the next begins.

  X-Forwarded-For, from the connection metadata up

  When the backend's kernel accepts a socket connection, it gets exactly one
  piece of ground-truth identity data about the other end: the source IP
  address, taken from the actual incoming packets by the kernel's own network
  stack — not read from any HTTP text, and not something the far side can fake
  by typing different bytes into a header, because it's observed at the
  packet/handshake level.

  But since our LB opens its own separate socket to the backend, the source IP
  the backend's kernel observes is the LB's IP, not the original client's. The
  real client IP did exist — the LB's own kernel saw it on the client↔LB socket
  — but that fact has no path to the backend except by being written explicitly
  into the HTTP text, since kernel-level connection metadata doesn't
  automatically carry across to a second, unrelated socket.

  Hence the convention: the LB writes X-Forwarded-For: <the real client IP its 
  own kernel observed> into the text it sends the backend. Here's the exact
  vulnerability: at the HTTP layer, there is no way to distinguish "this header
  was added by the LB based on real kernel-observed data" from "this header was
  typed by the original client, claiming to be some other IP" — both produce
  identical bytes. If the LB doesn't overwrite whatever arrived, a client can
  simply include a fabricated X-Forwarded-For in its own original request, and
  it passes straight through disguised as trustworthy. Our code closes this by
  discarding any client-supplied value and setting it fresh from the one thing
  the client genuinely cannot fake: the real source IP the LB's own kernel
  observed at the socket level.

  ---
  On the timeline question

  Honest answer, not a pep talk: the vocabulary gap is real but narrower than it
  feels right now, and it's closable — what's actually missing is mostly
  terminology and mechanism (sockets, protocol-level details, concurrency
  primitives), which is exactly what rebuilds fast once you're doing it
  hands-on, because you already have the underlying engineering instincts (you
  debug distributed-ish failures in production pipelines already — that's real
  judgment, just not labeled in system-design vocabulary yet).

  What "Senior" actually tests isn't "do you know more patterns than a mid-level
  candidate" — it's whether you can reason about trade-offs out loud, justify a
  specific design under specific constraints, and push back or ask clarifying
  questions like someone who's actually operated something in production. Your
  AWS/pipeline experience is a genuine asset there, not a gap, once you can talk
  about it in these terms.

  Realistic timeline, doing focused work a few hours a week (this project plus
  deliberately covering the other classic scenarios — rate limiter, URL
  shortener, chat system, cache, queue-backed pipeline — and doing some mock
  interviews near the end to practice articulating under time pressure): roughly
  3–6 months to go from where you are now to genuinely competent, not just
  pattern-memorized, for mid-to-senior-level interviews. Faster with more hours,
  and it compounds — the first couple of concepts (like today's) take the
  longest because there's no scaffolding yet; by phase 4 or 5 you'll be
  recognizing patterns instead of learning them cold.

  For AI agent/infra roles specifically: that target is good news for this
  project, because a lot of the actual infra concerns in that space — request
  queuing under burst load, per-client rate limiting, retry/idempotency, caching
  expensive calls — are exactly what we're building toward in later phases, not
  a separate curriculum you'd need to bolt on afterward.

  Ready to write up the Phase 1 notes whenever you are.

✻ Crunched for 1m 17s

※ recap: Building a Go load balancer to learn System Design for AI infra interviews. Phase 1 (single-backend HTTP proxy) is coded and tested; next step is writing up notes/phase-1-single-backend-proxy.md before opening the PR. (disable recaps in /config)

❯ Tell me more about a socket being an entry the kernel keeps in its own        
  internal table. Describe more about the contents of this table. That might    
  help me understand better exactly what a socket is. I am familiar with the    
  socket library in Python, from which I think you can instantiate an instance  
  of socket and call methods like bind, open, listen, etc.                      

⏺ Good instinct to ground this in something you've actually used. Let's open up
  what's really behind socket.socket(), .bind(), .listen(), .accept().

  Two separate tables, not one

  There are actually two levels of bookkeeping here, and conflating them is a
  common source of confusion:

  1. A per-process file descriptor table. Every running process has its own
  small table mapping small integers (0, 1, 2 are conventionally
  stdin/stdout/stderr; anything you open gets the next free number) to "things
  the kernel is tracking for you" — open files, pipes, or sockets. When you call
  socket.socket(), the Python object you get back is a thin wrapper around one
  of these integers. The integer itself means nothing outside your process —
  descriptor 5 in your program and descriptor 5 in some other process refer to
  completely unrelated kernel objects.
  2. A kernel-wide table of actual connection state, which is what the file
  descriptor really points to. This is the part worth digging into, since it's
  what a socket is.

  What's actually in an entry of that second table

  For a TCP socket, the entry the kernel maintains holds, at minimum:

  - Local address: local IP + local port (e.g. 0.0.0.0:8080 for our listening
  socket — 0.0.0.0 meaning "any network interface on this machine").
  - Remote address: remote IP + remote port. A listening socket has this unset —
  it isn't connected to anyone. A connection that's actually carrying data has
  both filled in.
  - State: TCP is a state machine. Key states you'll actually encounter: LISTEN
  (waiting for connection attempts), SYN_SENT/SYN_RECEIVED (mid-handshake),
  ESTABLISHED (connected, can send/receive), FIN_WAIT/CLOSE_WAIT/TIME_WAIT
  (mid-teardown). If you've ever hit "address already in use" restarting a
  server quickly, that's TIME_WAIT — the kernel holds the entry around briefly
  after close so any stray late-arriving packets from the old connection don't
  get misdelivered to a new, unrelated connection that happens to reuse the same
  port.
  - Receive buffer and send buffer: actual kernel-space memory holding bytes
  that have arrived over the network but that your program hasn't called recv()
  to consume yet, and bytes your program handed to send() that the kernel hasn't
  finished transmitting yet. This is why send() can return before the remote
  side has actually received anything — it only guarantees the kernel has queued
  the bytes into its own buffer.
  - Lower-level TCP bookkeeping (sequence numbers, acknowledgment numbers,
  window size for flow control) that you never touch directly — this is what
  makes TCP reliable and ordered despite the network underneath only delivering
  unordered, sometimes-lost packets.

  Mapping your Python calls onto this exactly

  - socket.socket(AF_INET, SOCK_STREAM) — asks the kernel to allocate a new
  entry in that table: no local address, no remote address, no state yet.
  Returns a file descriptor referencing it.
  - .bind((host, port)) — writes into that entry's local address field. Nothing
  about a remote peer yet.
  - .listen(backlog) — moves that entry's state to LISTEN, and tells the kernel
  to also allocate a small queue (backlog) for incoming connection attempts that
  have finished their handshake but that your program hasn't picked up yet.
  - .accept() — blocks until there's something in that queue. When a client's
  handshake completes, the kernel does not reuse the listening entry — it
  creates a brand-new, separate entry with local address = your server's, remote
  address = that specific client's, state = ESTABLISHED, and returns a new file
  descriptor for it. The original listening socket is untouched and keeps
  accepting more connections. This is exactly why one listening socket on port
  8080 can have thousands of simultaneous clients: each gets its own separate
  table entry, distinguished by the (local IP, local port, remote IP, remote
  port) 4-tuple even though the local IP:port is identical across all of them —
  it's the remote side of the tuple that makes each one unique.
  - .connect((host, port)) — the client-side equivalent: perform the handshake,
  and on success fill in the entry's remote address fields and move it to
  ESTABLISHED.
  - .send() / .recv() — write into / read out of that specific entry's
  send/receive buffers. Your program deals only with bytes in and bytes out; the
  kernel is doing the packet-level work in the background, asynchronously from
  your program's execution.

  So concretely, in our Go load balancer: there's one table entry in LISTEN
  state (port 8080), and for every simultaneous client connection, one more
  entry in ESTABLISHED state — plus a second ESTABLISHED entry for the
  corresponding LB→backend connection we open to service it. Two client requests
  in flight at once means at least four ESTABLISHED entries (two per request),
  all independently tracked, none of them aware of each other's existence except
  through our Go code explicitly wiring them together.

  Want to keep going deeper here, or move on to writing up the Phase 1 notes?

✻ Worked for 41s

❯ Describe the important code you implemented, and the tests you implemented.   

  Read 1 file (ctrl+o to expand)

⏺ internal/proxy/proxy.go — the actual forwarding logic

  func New(target *url.URL, logger *log.Logger) *httputil.ReverseProxy {
        return &httputil.ReverseProxy{
                Rewrite: func(pr *httputil.ProxyRequest) {
                        pr.SetURL(target)
                        pr.SetXForwarded()
                },
                ErrorLog: logger,
                ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
                        logger.Printf("backend error for %s %s: %v", r.Method, r.URL.Path, err)
                        w.WriteHeader(http.StatusBadGateway)
                },
        }
  }

  httputil.ReverseProxy is a Go standard library type that does the actual
  mechanical work: read the incoming request off the client↔LB socket, open (or
  reuse) a connection on the LB↔backend socket, write an outgoing request there,
  read the backend's response, write it back to the client. We don't hand-write
  that byte-copying loop — the standard library already implements it
  correctly, including the hop-by-hop header stripping we walked through
  earlier.

  What our code supplies is the two things specific to our proxy:

  - Rewrite — this function runs on every incoming request, right after the
  standard library has already stripped hop-by-hop and forwarding headers from
  the outbound copy. pr.SetURL(target) rewrites the outbound request's
  destination to our one backend's address. pr.SetXForwarded() is exactly the
  function we discussed — it writes X-Forwarded-For using the real source IP the
  kernel observed on the client↔LB socket, discarding anything the client tried
  to set itself.
  - ErrorHandler — runs only when the LB↔backend connection attempt itself fails
  (e.g. connection refused, because nothing is listening on that port) — as
  opposed to the backend accepting the connection and returning an HTTP error
  status, which just flows through as a normal response. We log why, then
  explicitly write 502 Bad Gateway to the client. Without this, the standard
  library still returns 502 by default, but we wouldn't get our own log line
  explaining why.

  internal/middleware/logging.go — wrapping, not modifying, the proxy

  type statusRecorder struct {
        http.ResponseWriter
        status int
  }

  func (r *statusRecorder) WriteHeader(status int) {
        r.status = status
        r.ResponseWriter.WriteHeader(status)
  }

  func Logging(logger *log.Logger) func(http.Handler) http.Handler {
        return func(next http.Handler) http.Handler {
                return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                        rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
                        start := time.Now()
                        next.ServeHTTP(rec, r)
                        logger.Printf("%s %s %d %s", r.Method, r.URL.Path, rec.status,
  time.Since(start))
                })
        }
  }

  The mechanism worth understanding here: Go's http.ResponseWriter is a
  write-only interface — once the proxy calls WriteHeader(200) on it, there's no
  method to ask "what status code did you just write?" So statusRecorder sits
  between the proxy and the real response writer: it's a struct that is a
  ResponseWriter (via the embedded field) but intercepts the one call we care
  about (WriteHeader) to also stash the value in a field, before passing it
  through to the real writer underneath. This is the standard shape for HTTP
  middleware in Go: a function that takes a handler and returns a new handler
  that does something extra before/after calling the original.

  This is deliberately a separate package from proxy, not a feature bolted onto
  it — because rate limiting and caching, when we get to those phases, will be
  additional middleware functions wrapping the same proxy handler, not more
  logic crammed inside proxy.New.

  cmd/loadbalancer/main.go — wiring, plus graceful shutdown

  The two lines that matter most:

  handler := middleware.Logging(logger)(proxy.New(target, logger))
  server := &http.Server{Addr: *listenAddr, Handler: handler}

  proxy.New(...) returns the ReverseProxy, which itself satisfies Go's
  http.Handler interface. middleware.Logging(logger) returns a function, and we
  immediately call that function on the proxy — wrapping it. server.Handler ends
  up being: logging middleware on the outside, proxy on the inside, for every
  request that arrives on the listening socket.

  The other piece is shutdown handling:

  stop := make(chan os.Signal, 1)
  signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
  <-stop

  ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
  defer cancel()
  server.Shutdown(ctx)

  signal.Notify tells the OS "instead of killing this process outright on Ctrl-C
  or SIGTERM, deliver that signal onto this channel instead, and let my code
  decide what to do." <-stop blocks the main goroutine until that happens.
  server.Shutdown(ctx) then closes the listening socket immediately (no new
  connections accepted) but leaves already-ESTABLISHED connections alone until
  their in-flight requests finish or the 10-second context deadline passes,
  whichever comes first. Without this, a process kill just severs every open
  socket mid-response, including the LB↔backend ones — any request in progress
  gets cut off with no response at all.

  Tests

  proxy_test.go — functional, one assertion each:

  - TestForwardsRequestToBackend — spins up a fake backend with
  httptest.NewServer, records what method/path it actually received, points our
  proxy at it, and checks the client gets back exactly what the backend sent.
  This is the baseline: does forwarding actually work.
  - TestClientCannotSpoofForwardedFor — sends a request through the proxy with a
  hand-set X-Forwarded-For: 6.6.6.6 header already attached, and asserts the
  backend does not see that value. This is a direct test of the security
  property we discussed — it would fail if we'd used the deprecated
  NewSingleHostReverseProxy helper instead of SetXForwarded().
  - TestHopByHopHeaderNotForwarded — sends Connection: Keep-Alive and
  Keep-Alive: timeout=5 through the proxy and asserts the backend receives
  neither. Directly verifies the hop-by-hop stripping we walked through
  conceptually.
  - TestBackendDownReturns502 — points the proxy at 127.0.0.1:1, a port nothing
  is listening on, so the connection attempt fails at the kernel level
  (connection refused) rather than timing out. Confirms our ErrorHandler path
  actually fires and returns 502 rather than, say, hanging or crashing.

  stress_test.go — deliberately trying to break it, not just confirm happy 
  paths:

  - TestStress_SlowBackendHasNoTimeout — the backend sleeps 300ms before
  responding; the client has a 50ms timeout. The test asserts the client's own
  timeout is what cuts the request off (elapsed < backendDelay), and logs it
  explicitly as a known weakness: our proxy places no time limit of its own on
  waiting for the backend. If a real client didn't set a timeout, that request —
  and the goroutine, socket, and kernel buffers backing it — would sit there
  for as long as the backend takes, however long that is. Under enough
  concurrent hung requests, that's how a single slow backend exhausts the load
  balancer's own resources.
  - TestStress_ConcurrentRequestsHandledConcurrently — fires 50 concurrent
  requests at a backend that takes 100ms each, and checks the whole burst
  finishes in well under 50 × 100ms. This confirms Go's http.Server really is
  handling each connection on its own goroutine in parallel, rather than
  serializing them — a fact I was asserting, not just assuming, before this test
  existed.
  - TestStress_BurstAgainstUnreachableBackend — fires 50 concurrent requests at
  a dead backend and asserts every single one comes back as a clean 502, none
  hang, none crash the process. This checks that failure handling holds up under
  concurrent load, not just for one request in isolation.

  All 7 tests pass; the timeout test is the one genuine weakness this phase
  surfaced, and it's exactly the kind of thing that becomes the "known
  weaknesses" motivation for a later phase.

  Ready to write up notes/phase-1-single-backend-proxy.md, or is there a piece
  of this you want to sit with longer first?





✻ Cooked for 40s

※ recap: Building a Go load balancer to learn system design for AI infra interviews; Phase 1 (single-backend HTTP proxy) is coded and tested. Next: write up notes/phase-1-single-backend-proxy.md, then decide on committing and opening the PR. (disable recaps in /config)

❯ Yes write up the notes, then go ahead and commit and open a PR for merging to 
  build.                                                                        

  Searched for 1 pattern, read 1 file, ran 1 shell command (ctrl+o to expand)

⏺ Write(notes/phase-1-single-backend-proxy.md)
  ⎿  Wrote 66 lines to notes/phase-1-single-backend-proxy.md
      1 # Phase 1: Single-backend HTTP reverse proxy
      3 - **Branch:** `phase-1-single-backend-proxy`
      4 - **PR:** opened from this branch into `build`
      5 - **Status:** complete
      7 ## 1. What was built
      8 
        it receives to a single, fixed backend. No load-balancing algorit
        hm, no multiple backends, no health checks, no rate limiting, no 
        caching, no TLS — all explicitly deferred to later phases. The go
        done carefully rather than done big.
     … +181 lines (ctrl+o to expand)

⏺ Now let's run the full check one more time before committing, then commit and
  open the PR.

  Ran 2 shell commands (ctrl+o to expand)

⏺ Caught a stray vim swap file (cmd/loadbalancer/.main.go.swp) that got staged —
  that shouldn't be committed. Let me remove it and add a .gitignore so this
  doesn't happen again.

  Ran 1 shell command (ctrl+o to expand)

⏺ Write(.gitignore)
  ⎿  Wrote 4 lines to .gitignore
      1 *.swp
      2 *.swo
      3 *~
      4 .DS_Store

  Ran 1 shell command (ctrl+o to expand)

⏺ Staged set looks clean now. Committing.

  Committed 14c6c6c (ctrl+o to expand)

⏺ Now pushing the branch and opening the PR into build.

  Pushed to phase-1-single-backend-proxy, created PR #1 (ctrl+o to expand)

⏺ Phase 1 is committed and up for review: PR #1 
  (https://github.com/jerryschen31/system-design-load-balancer/pull/1) —
  phase-1-single-backend-proxy → build.

  Along the way I caught a stray vim swap file that had gotten staged
  (cmd/loadbalancer/.main.go.swp) and added a .gitignore so junk like that, and
  .DS_Store, won't get committed in future phases.

  Everything's in place per the workflow we set up: code, functional + stress
  tests, notes/phase-1-single-backend-proxy.md, and a PR description mirroring
  it. Whenever you're ready, say the word and we'll start Phase 2 — multiple
  backends and a first load-balancing algorithm.

✻ Crunched for 2m 6s