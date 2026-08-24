---
name: explain-code
description: Explain a file or code block in this repo, chunk by chunk, teaching Go syntax/conventions/stdlib and how the piece fits into the load balancer as a whole. Use when the user asks to explain, walk through, or break down a file, function, or line range -- without writing or changing any code.
disable-model-invocation: true
---

# Explain code, chunk by chunk

Purpose: let the user say something short like "explain internal/proxy/proxy.go" or "explain roundrobin.go lines 20-45" or "explain the ServeHTTP function in main.go" and get a teaching-oriented walkthrough -- no code changes, ever.

This skill is pure explanation. Do not edit, refactor, or write any code while it's active, even if an improvement is obvious -- note it verbally at most, but leave the file untouched.

## Audience calibration (read `CLAUDE.md` if not already in context)

The user is a computational biologist / bioinformatics engineer, Caltech CS background but ~20 years stale, new to Go and to core System Design and networking/OS concepts. Calibrate low:

- Avoid generic real-world metaphors ("a socket is like a phone handset"). Concrete cross-language comparisons to C/C++/Python constructs the user already knows (e.g. "this pointer receiver is like passing `struct*` in C so the callee can mutate the caller's copy") are welcome, especially when asked for.
- Don't assume familiarity with terms common in backend work even if they sound basic: socket, goroutine, channel, mutex, interface, context, hop-by-hop header, connection pooling, etc. Define each the first time it appears in this conversation.
- Assume general programming literacy (loops, functions, types) from their CS background, but not Go-specific idiom or stdlib knowledge.

## Format (as important as content -- confirmed 2026-08-23 against a side-by-side comparison)

A technically-accurate explanation in dense prose paragraphs was rated harder to follow than a shorter, more scannable one covering the same mechanism. Default to the terser style:

- Lead with a one- or two-sentence plain-language summary of what the construct does, *before* the mechanism detail.
- Prefer short bullet points over paragraphs. A paragraph is acceptable for a single connected thought, but don't stack more than ~3-4 sentences of unbroken prose.
- Show a minimal, standalone usage snippet when it clarifies a pattern (not just the code already in the file -- a stripped-down illustrative example, e.g. the create/defer/cancel-later shape for `context.WithCancel`).
- When a construct has a common misconception or a sharp edge, give it its own short "what it does NOT do" callout (e.g. context cancellation is cooperative, not preemptive -- `cancel()` doesn't kill a goroutine, it only closes a channel that well-behaved code has to check).
- Use sub-headers to break a multi-part explanation into scannable sections rather than one long flowing narrative.
- Use simple, plain, accurate technical language and avoid unnecessary jargon

## Steps

1. **Resolve the target.** Figure out exactly what the user means: a whole file, a named function/type, or a line range. If ambiguous (e.g. multiple files could match, or "the health check part" doesn't map to one obvious span), ask a short clarifying question rather than guessing broadly.

2. **Read the target plus enough surrounding context** to explain it accurately: the file's imports, any types/functions it calls into elsewhere in the repo, and its callers. Use Grep/Read to trace those connections -- don't explain a function in isolation if it's called from somewhere non-obvious.

3. **Break the target into logical chunks** -- not line-by-line, but by meaningful unit (e.g. "imports and package declaration", "the Backend struct and its fields", "the health-check goroutine loop", "the error-handling branch"). For each chunk, in order:
   - Show or reference the code (quote the relevant lines with file:line so the user can jump to it).
   - Explain *what it does* in plain terms.
   - Explain *any Go syntax/convention/stdlib feature* present that a Go beginner wouldn't know (e.g. `defer`, multiple return values with an `error`, struct embedding, pointer receivers vs. value receivers, `net/http.Handler` interface, `sync.Mutex`, channels, `select`, zero values, named return values, blank identifier `_`). Explain the mechanism, not just "this is idiomatic."
   - Explain *why it's written this way* here specifically -- what it's for in this load balancer, and any System Design concept it embodies (e.g. why a mutex guards the backend list, why round-robin state needs to be thread-safe, why health checks run on a separate goroutine).

4. **Connect the chunks.** After going through them, give a short synthesis of how this file/function fits into the overall system: what calls it, what it calls, what data flows through it, and where it sits in the request lifecycle (e.g. incoming HTTP request -> middleware -> proxy -> backend selection -> health check state).

5. **Check understanding, lightly.** End with one short, specific question the user could answer to confirm the mechanism landed (not a quiz dump -- one question). This matches how this repo's teaching sessions normally work, per `CLAUDE.md`.

## Explicit non-goals

- Do not propose or make code changes, even small "obvious" fixes -- if you notice something worth changing, mention it in one sentence at the end and stop there.
- Do not dump the entire file's contents with a wall of text; the value is in the chunk-by-chunk breakdown with explanations interleaved.
- Don't over-scope: if the user asked about one function, don't walk through the whole file unless they ask, though you may.
