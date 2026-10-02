 ▐▛███▛█   Claude Code v2.1.261
▝▜██████▀  Sonnet 5 · Claude Pro
  ▝▝ ▝▝    ~/gh/public/system-design-load-balancer


❯ @cmd/loadbalancer/main.go:160-164 the main goroutine calls server.Shutdown,   
  but the server is running on a separate goroutine (lines 133-138 go           
  func(){... server.ListenAndServer() ...}. So it is possible and accepted that 
  one goroutine can call a method on a variable or object that is running in a  
  different goroutine? In other words, variables are shared between             
  goroutines?                                                                   

  Read 1 file (ctrl+o to expand)

⏺ Yes — that's exactly right, and it's a fundamental (and initially surprising)
  feature of Go's concurrency model that's worth pinning down before we go
  further.

  The general idea, no code yet

  In most programming you're used to, a variable belongs to whichever function
  is currently running, and only that function touches it. Goroutines break that
  assumption: a goroutine is just an independently-scheduled function
  execution, but it doesn't get its own private copy of the variables it closes
  over. If two goroutines both have a reference to the same object in memory,
  both can read it and both can call methods on it — at the same time, from the
  runtime's perspective. There's no automatic rule in Go that says "only the
  goroutine that created this object may operate on it." The language gives you
  shared memory by default; keeping it safe is your job, not the compiler's.

  This is different from, say, message-passing-only systems (like Erlang
  processes, or channels used exclusively) where the only way to affect another
  unit of concurrency is to send it a message and let it act on its own data. Go
  supports that style too (that's what channels are for), but it doesn't force
  it. You can also just share a pointer and call methods on it from anywhere,
  which is what's happening here.

  Diagram of what's happening at shutdown

  main goroutine                         goroutine spawned at line 133
  ---------------                         ------------------------------
  server := &http.Server{...}   ────────▶  (same *http.Server value)
        │
  go func(){ server.ListenAndServe() }()
        │                                  server.ListenAndServe() blocks,
        │                                  accepting connections in a loop
  <-stop   (blocks here until signal)              │
        │                                          │
    (signal arrives)                                │
        │
  server.Shutdown(ctx)  ─────── calls a method on the SAME object ───▶ tells the
        │                                          ListenAndServe loop
        │                                          to stop, causing it
        │                                          to return

  Both arrows point at the same *http.Server value in memory. server isn't
  copied when you write go func() { server.ListenAndServe() }() — the closure
  captures the variable server, which holds a pointer, and that pointer is
  shared. So yes: one goroutine (main) is allowed to call server.Shutdown(ctx) —
  a method on line 162 — on the identical object that another goroutine (the
  one from line 133) is currently blocked inside, running
  server.ListenAndServe().

  Mapping onto the code

  - server (declared earlier, not shown in this excerpt) is a *http.Server — a
    pointer, so "the object server points to" is one single thing in memory, not
    duplicated per goroutine.
  - Line 133's go func() { ... server.ListenAndServe() ... }() captures that
    pointer and runs ListenAndServe — a call that blocks forever, looping and
    accepting connections, until something stops it.
  - Line 162's server.Shutdown(ctx) runs in the main goroutine, on that same
    pointer, after <-stop unblocks.

  Why this particular case is safe despite being shared

  This isn't safe merely by luck — it's safe because net/http.Server is
  documented and implemented to be safe for concurrent use: internally it uses
  its own locks (mutexes) to guard its state, so calling Shutdown from one
  goroutine while ListenAndServe runs in another is an intended, supported usage
  pattern. Shutdown works by signaling the server to stop accepting new
  connections and closing idle ones; that causes the blocked ListenAndServe()
  call in the other goroutine to return (with http.ErrServerClosed, which is why
  line 135 explicitly checks for and ignores that particular error).

  The general rule this teaches: sharing a variable across goroutines is legal 
  and common, but it's only safe when the thing you're sharing is designed for 
  it (documented as concurrency-safe, or you protect it yourself with a
  mutex/channel). If server were, say, a plain map or a custom struct with no
  internal locking, calling methods on it concurrently from two goroutines could
  cause a data race — a genuine bug, not just a style concern, and one that
  Go's race detector (go test -race, called out in your CLAUDE.md commands) is
  specifically built to catch. http.Server gets a pass here because its authors
  did that locking work for you; your own types generally will not, unless you
  do it explicitly.

✻ Baked for 23s · done 2:30 PM

❯ What would happen if Shutdown were called without http.Server's internal      
  locking?                                                                      

⏺ The general problem, no code yet

  When two goroutines touch the same piece of memory at the same time, and at
  least one of them is writing to it, the outcome depends on the CPU/runtime's
  exact interleaving of instructions — which is not something Go guarantees or
  makes deterministic. This is called a data race. It's not merely "you might
  read a slightly stale value" (which would be tolerable in many designs) — it's
  undefined behavior at the language level. The Go memory model does not
  promise anything about what a concurrent, unsynchronized read sees relative to
  a concurrent write. In practice this can manifest as: reading a value that's
  half-written (garbage — some old bytes, some new bytes, mashed together), a
  crash, or — worst of all — something that happens to work every single time
  you test it, then fails once in production under different timing/scheduling.

  The specific shape of danger for something like Shutdown and ListenAndServe
  sharing internal state (imagine http.Server did not have its own mutex):

  main goroutine                    server goroutine (ListenAndServe loop)
  ---------------                    ---------------------------------------
                                      reads server.listeners map
                                      to iterate + accept new conns
  Shutdown():
    writes server.listeners
    (removing/closing entries)  ──┐
                                   │  (no coordination between these —
                                   │   both touch the same map concurrently)
                                      continues iterating server.listeners
                                      ← reading a map while it's being
                                        mutated elsewhere

  Go maps in particular are documented as explicitly not safe for concurrent
  read+write — the runtime actively detects this case and panics with fatal 
  error: concurrent map read and map write (this one isn't even a "maybe you get
  unlucky" case — the runtime crashes the whole process on purpose, because
  letting it continue could corrupt memory silently). Other shared fields (plain
  ints, booleans, slices) don't get that runtime protection — they'd just
  silently produce wrong/inconsistent behavior, e.g. Shutdown thinking there are
  zero active connections when there are actually three mid-flight, and closing
  them anyone mid-response.

  Why http.Server avoids this

  Internally, http.Server has an unexported sync.Mutex field (call it mu) that
  every method — ListenAndServe, Shutdown, Close, ServeHTTP's bookkeeping —
  locks before touching shared fields like the set of active
  listeners/connections, and unlocks afterward. A mutex (mutual exclusion lock)
  enforces: only one goroutine may hold the lock at a time; anyone else calling
  Lock() blocks until it's released. So even though ListenAndServe (goroutine A)
  and Shutdown (goroutine B) both want to read/modify the same internal map of
  connections, the mutex serializes those specific critical sections — they
  never actually execute the touch-the-map instructions simultaneously, even
  though the two goroutines are running concurrently everywhere else.

  If you wrote a type without that discipline

  If you rolled your own server-like struct with a plain field like activeConns 
  map[net.Conn]struct{} and let two goroutines read/write it without a mutex
  around those accesses, you'd have written a data race — legal Go syntax,
  compiles fine, go vet won't catch it, but wrong at runtime under the memory
  model. This is exactly what go test -race (mentioned in your ## Commands
  section) is for: it instruments memory accesses at runtime and reports the
  specific unsynchronized read/write pair — which goroutine, which line — when
  it observes one during a test run, rather than you having to catch it by
  staring at logic.

  If you want, next session (or right now) we could deliberately write a tiny
  broken example — a shared map incremented by two goroutines without a mutex —
  and run it under -race to see the exact failure output. That's the kind of
  hands-on demo your CLAUDE.md flags as the most effective learning style so
  far.

✻ Cogitated for 19s · done 2:33 PM

❯ Show me a very simple basic example of two goroutines writing to the same     
  variable and potentially causing a data race.                                 

⏺ I'll write a minimal, self-contained example — a shared counter incremented by
  two goroutines with no synchronization — and run it with the race detector so
  you see Go actually catch it.

  Plain-language setup first: two goroutines will each increment the same int
  variable 1000 times in a loop. counter++ looks like one operation but is
  actually three: read the current value, add 1, write it back. If two
  goroutines interleave those three steps on the same variable, some increments
  get lost — the final count ends up less than 2000, and the pattern is
  nondeterministic (different every run).

  Made 1 scratchpad edit +35 (ctrl+o to expand)

⏺ One new piece here: sync.WaitGroup. It's a counter for "how many goroutines am
  I waiting on." wg.Add(2) sets it to 2, each goroutine calls wg.Done() (via
  defer) when it finishes, which decrements it, and wg.Wait() in main blocks
  until it hits 0. This is a different, simpler tool than <-stop from main.go —
  that channel was waiting for one external signal; this is waiting for N
  goroutines to self-report completion.

  Let's run it first without the race detector — it'll likely finish and may or
  may not print exactly 2000, since races don't always produce visibly-wrong
  output:

  Ran 1 shell command (ctrl+o to expand)

⏺ That's the trap: it printed the "correct" answer all three times, even though
  the code is genuinely broken. This is the core danger of data races — the bug
  doesn't announce itself. With only 1000 fast iterations, the two goroutines'
  counter++ operations rarely land on the exact same instant, so it looks fine.
  This is exactly why you can't verify concurrency correctness by "running it
  and checking the output" — you need a tool that inspects execution at the
  memory-access level, not the output level. That's what go build -race / go 
  test -race does. Let's run it:

  Ran 1 shell command (ctrl+o to expand)

⏺ Even with the "correct" final answer (2000) printed, the race detector flags
  the code as broken — this is the important disconnect to internalize: passing 
  output does not mean correct code, under concurrency.

  Reading the output, mapping to the diagram from before:

  goroutine 7 (func1, line 17-21)        goroutine 8 (func2, line 24-29)
  ------------------------------          ------------------------------
  line 20: counter++
     (this is really: read counter,
      add 1, write counter)        ──┐
                                      │  no lock between these two blocks
                                      │  of instructions -- runtime is free
                                      │  to interleave them at the byte level
                                      ▼
                                line 28: counter++
                                   (reads/writes the SAME memory address
                                    0x00c000114038 concurrently)

  The report's Read at ... by goroutine 8 / Previous write ... by goroutine 7 at
  the same address is the race detector literally instrumenting every memory
  access and noticing: two goroutines touched 0x00c000114038 (the address of
  counter) close enough in time, with no synchronization primitive (mutex,
  channel, atomic op) establishing which one happened first, that either
  ordering was possible. It doesn't need the bug to actually produce a wrong
  number to flag it — the lack of ordering guarantee is the violation,
  independent of whether you got unlucky on this particular run.

  The fix — introduce one of the tools that establishes ordering. The simplest
  here is sync.Mutex, the same primitive http.Server uses internally:

  Made 1 scratchpad edit +30 -22, read 1 file, ran 1 shell command (ctrl+o to expand)

⏺ Clean — no race warnings now.

  mu.Lock() before counter++ and mu.Unlock() after means: whichever goroutine
  gets there first fully completes its read-add-write before the other
  goroutine's Lock() call is even allowed to return. The mutex converts "these
  two goroutines might interleave arbitrarily" into "these two goroutines take
  turns" for that specific critical section — which is precisely the same
  mechanism http.Server's internal mutex provides for ListenAndServe vs.
  Shutdown: not preventing concurrency in general, just serializing the specific
  shared-state touches that would otherwise race.

  Takeaway to carry forward: shared variables between goroutines are legal in
  Go, but every unsynchronized concurrent access where at least one side writes
  is a data race, whether or not a given run happens to "look right" — and -race
  is the only reliable way to catch it, not eyeballing output.

✻ Baked for 52s · done 2:34 PM

※ recap: You're learning Go/System Design by building a load balancer; we just walked through why goroutines can share variables and demonstrated a data race with -race on a scratch file. Next: decide whether to try more concurrency exercises or return to main.go. (disable recaps in /config)