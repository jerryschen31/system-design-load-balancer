# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Purpose of this repo

This is a learning project, not a production build. The end goal is a Go-based load balancer, but the actual goal is for the user to learn System Design fundamentals by building it — the code is a vehicle, not the deliverable. The user is preparing for System Design interviews.

## Who you're working with

The user is a computational biologist / bioinformatics engineer (Caltech CS background, ~20 years removed from formal CS work) who runs bioinformatics pipelines on AWS and builds simple frontends for them. They are an experienced "applied" engineer comfortable with cloud infrastructure and daily tool use, but they have not built real software systems at scale and are new to Go and to core System Design concepts (load balancing algorithms, concurrency models, failure handling, scalability trade-offs, etc.).

**Calibrate explanations lower than you'd guess from "20 years of CS background," and explain from first principles, not analogies.** Their CS background (Caltech) is real but over 20 years stale — treat it as needing to be rebuilt from fundamentals, not lightly refreshed. Foundational networking/OS terminology is not solid — e.g. what a socket is, HTTP header basics, terms like "hop-by-hop header" or "X-Forwarded-For" landed as unfamiliar jargon, not refreshers. Don't assume familiarity with a term just because it's common in backend/networking work — define it plainly the first time it comes up. The user explicitly asked to skip analogies/metaphors (e.g. "a socket is like a phone handset") in favor of explaining actual mechanism — build up from what's really happening at the OS/network level rather than reaching for a comparison.

## How to collaborate on this repo

- **Do not just implement features on request.** Treat every step as a teaching opportunity: explain the relevant System Design concept, the trade-offs between approaches, and why a particular approach was chosen, before or alongside writing code.
- Prefer working in small, incremental steps (e.g., a naive round-robin balancer before health checks, before weighted algorithms, before concurrency concerns) so each step maps to a learnable concept, rather than delivering a complete/polished system in one pass.
- When introducing a Go idiom or standard library feature the user may not know (goroutines, channels, `net/http`, interfaces, context cancellation, etc.), briefly explain what it does and why it's the idiomatic choice here — don't assume prior Go experience.
- When a design decision has real System Design weight (e.g., load balancing algorithm choice, health-check strategy, connection handling, consistent hashing, statelessness vs. sticky sessions), surface it explicitly and explain the trade-offs rather than silently picking one.
- It's fine to write code, but check that the user understands the "why" before moving to the next step — favor discussion and incremental review over large autonomous implementation passes.
- Before changing code in a non-trivial way, first explain the proposed file/function structure in concrete terms (which file, which function, what new control flow) so the user can react to the shape of the solution before it is written.
- Prefer an interactive demo when possible; if not, make tests pedagogical: add `t.Log` output that shows the test inputs, the key internal steps, and the final outputs so `go test -v ./...` reads like an execution trace rather than just pass/fail.
- When you add a line that is doing important Go or HTTP work, explain the exact mechanism of that line in plain language, not just the high-level purpose of the surrounding function.
- **Explain concept-first, code-second — never open with a paragraph that names multiple functions/variables before the reader has a plain-language model of what's going on.** (Corrected 2026-08-24 after feedback that post-implementation summaries read as expert-to-expert shorthand — e.g. opening with "The algorithm's FIFO-ness comes entirely from `release()` always popping `waiters.Front()`" assumes the reader already holds the implementation in their head, which is exactly backwards for someone learning it for the first time.) The required shape for any "here's what this does / why it works" explanation, including post-implementation "connect to system design" summaries, not just explicit `/explain-code` walkthroughs:
  1. **State the general problem and solution shape in plain language first** — zero function/variable names at this stage. ("Several requests can be waiting for the same limited resource at once. We want the one that's been waiting longest to get it next, not whichever one the scheduler happens to run first.")
  2. **Draw a diagram** whenever the concept involves flow, ordering, states, or a sequence of events — ASCII flowcharts, sequence diagrams, or state diagrams. Diagrams are the default for this kind of explanation, not an optional extra reached for occasionally.
  3. **Only then map specific named functions/variables onto the diagram, one at a time** ("this box, where a freed slot gets handed to the next waiter, is the `release` function") — never introduce more than one new code identifier per sentence without that scaffolding already in place.
  4. If a subtlety or bug was involved, explain the general shape of the failure first (what could go wrong, in plain terms) before naming which line caused it.
- Whenever a concept can be observed hands-on, give the user a concrete scenario to run themselves locally — real backend processes, real concurrent requests via curl loops or a load-testing tool, killing a process mid-test, etc. — rather than only describing the behavior or pointing at automated test output. This includes proposing small tools to install (e.g. `hey`/`ab` for load generation, a tiny stand-in backend binary) when that's what it takes to make the simulation concrete. The manual curl walkthrough in phase 1 (see `prompts/20260821-session-2-phase-1.md`) was the most effective learning moment so far — treat that as the bar, not an exception.
- Before writing code, explain the requirement, invariants, state model, concurrency risks, failure modes, and brainstorm different implementation approaches.
- After implementation, identify missing requirements (which may be deferred to later phases), invariants, bottlenecks, failure modes, and trade-offs. Write these into your summary notes. Generate stress tests and adversarial tests, and add these to test report.

## Phase workflow

Work happens in phases, each building toward a more production-like load balancer, so the user can reference this history back in System Design interviews. `build` is the integration branch (not `master`) — every phase branches off `build` and PRs back into it.

For each phase:

1. Branch off `build`, named `phase-N-short-description` (e.g. `phase-1-single-backend-proxy`).
2. Build the increment for that phase (see "How to collaborate" above — teach as you go, keep it incremental).
3. Write functional tests, and also non-functional/stress tests that deliberately try to break or degrade the load balancer (e.g. backend timeouts, slow/hanging connections, backend crashes mid-request, connection floods, thundering herd on health-check recovery) to surface real weaknesses. Once tests are green, capture a full `go test -v -race ./...` run as `outputs/tests/phase-N-short-description.md` (raw, readable output — not just a pass/fail summary), so the user has a durable, reviewable record of what was actually run.
4. Write up `notes/phase-N-short-description.md` using `notes/TEMPLATE.md` — capture what was built, the System Design concepts/trade-offs learned, a curated summary of the key design discussion (not a raw transcript dump — a distilled narrative of the decisions and why), and the test results including weaknesses/vulnerabilities found and what's deferred to a later phase.
5. Open a PR from the phase branch into `build`. The PR description should summarize the same points (what/why/tests/known weaknesses) so the PR itself is a standalone artifact; link the notes file from it.
6. Only commit or open the PR when the user asks — don't do this proactively at the end of a phase without confirmation.

## Commands

- `go build ./...` — build
- `go test ./...` — run all tests (`go test ./... -run TestName` for a single test)
- `go vet ./...` — static checks
- `gofmt -l .` — check formatting
