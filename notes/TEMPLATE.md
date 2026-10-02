# Phase N: <short title>

- **Branch:** `phase-N-short-description`
- **PR:** <link once opened>
- **Status:** in progress / complete

## 1. What was built

Plain description of the increment delivered in this phase — scope in, and just as importantly, scope explicitly left out (deferred to a later phase).

## 2. System Design concepts & considerations

The concepts this phase exercises, and the trade-offs weighed. E.g.:

- **Concept:** <e.g. round-robin vs. least-connections>
  - **Options considered:**
  - **Trade-offs:**
  - **What we chose and why:**

## 3. Design conversation summary

A curated narrative of the key back-and-forth for this phase — not a raw transcript. What questions came up, what alternatives were debated, where the direction changed and why. Written so it reads well on its own in an interview context.

## 4. Code changes

High-level summary of what changed (files/packages touched, key additions). The PR diff is the source of truth; this is a guide to it, not a duplicate.

## 5. Testing

### Functional tests
What behavior is verified and how.

### Non-functional / stress / chaos tests
Tests that deliberately try to break or degrade the system (e.g. backend timeouts, slow/hanging connections, backend crashes mid-request, connection floods, thundering herd on health-check recovery, resource exhaustion). For each: what was tried, what happened, and whether it's a real weakness or an accepted/expected limit for this phase.

## 6. Known weaknesses / open questions

What this phase does *not* solve yet, and what would need to change to address it — sets up the motivation for the next phase.
