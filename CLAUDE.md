# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Purpose of this repo

This is a learning project, not a production build. The end goal is a Go-based load balancer, but the actual goal is for the user to learn System Design fundamentals by building it — the code is a vehicle, not the deliverable. The user is preparing for System Design interviews.

## Who you're working with

The user is a computational biologist / bioinformatics engineer (Caltech CS background, ~20 years removed from formal CS work) who runs bioinformatics pipelines on AWS and builds simple frontends for them. They are an experienced "applied" engineer comfortable with cloud infrastructure and daily tool use, but they have not built real software systems at scale and are new to Go and to core System Design concepts (load balancing algorithms, concurrency models, failure handling, scalability trade-offs, etc.).

## How to collaborate on this repo

- **Do not just implement features on request.** Treat every step as a teaching opportunity: explain the relevant System Design concept, the trade-offs between approaches, and why a particular approach was chosen, before or alongside writing code.
- Prefer working in small, incremental steps (e.g., a naive round-robin balancer before health checks, before weighted algorithms, before concurrency concerns) so each step maps to a learnable concept, rather than delivering a complete/polished system in one pass.
- When introducing a Go idiom or standard library feature the user may not know (goroutines, channels, `net/http`, interfaces, context cancellation, etc.), briefly explain what it does and why it's the idiomatic choice here — don't assume prior Go experience.
- When a design decision has real System Design weight (e.g., load balancing algorithm choice, health-check strategy, connection handling, consistent hashing, statelessness vs. sticky sessions), surface it explicitly and explain the trade-offs rather than silently picking one.
- It's fine to write code, but check that the user understands the "why" before moving to the next step — favor discussion and incremental review over large autonomous implementation passes.

## Phase workflow

Work happens in phases, each building toward a more production-like load balancer, so the user can reference this history back in System Design interviews. `build` is the integration branch (not `master`) — every phase branches off `build` and PRs back into it.

For each phase:

1. Branch off `build`, named `phase-N-short-description` (e.g. `phase-1-single-backend-proxy`).
2. Build the increment for that phase (see "How to collaborate" above — teach as you go, keep it incremental).
3. Write functional tests, and also non-functional/stress tests that deliberately try to break or degrade the load balancer (e.g. backend timeouts, slow/hanging connections, backend crashes mid-request, connection floods, thundering herd on health-check recovery) to surface real weaknesses.
4. Write up `notes/phase-N-short-description.md` using `notes/TEMPLATE.md` — capture what was built, the System Design concepts/trade-offs learned, a curated summary of the key design discussion (not a raw transcript dump — a distilled narrative of the decisions and why), and the test results including weaknesses/vulnerabilities found and what's deferred to a later phase.
5. Open a PR from the phase branch into `build`. The PR description should summarize the same points (what/why/tests/known weaknesses) so the PR itself is a standalone artifact; link the notes file from it.
6. Only commit or open the PR when the user asks — don't do this proactively at the end of a phase without confirmation.

## Commands

No source code exists yet. Once the Go module is initialized, the standard commands will apply:
- `go build ./...` — build
- `go test ./...` — run all tests (`go test ./... -run TestName` for a single test)
- `go vet ./...` — static checks
- `gofmt -l .` — check formatting
