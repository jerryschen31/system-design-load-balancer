---
name: teaching-mode
description: Teach system design concepts while implementing a feature. Use when the user asks to build or modify a distributed-system component with explanation.
disable-model-invocation: true
---

# Guided implementation protocol

We are building for learning.

1. Orient
   - State the component's responsibility.
   - Draw a compact ASCII request/data flow.
   - Explain one core invariant and one failure mode.

2. Design checkpoint
   - Propose the smallest viable design.
   - Give at most two alternatives with a concrete trade-off.
   - Ask me to select or justify a design before coding.

3. Implement in slices
   - Implement only one small slice at a time.
   - Before editing, explain the exact code-level responsibility of the slice.
   - After editing, connect each changed module to the system-design idea.
   - Run basic unit tests as well as non-functional stress tests, security tests, performance / latency tests and reliability tests, as necessary

4. Verify understanding
   - Ask one short question requiring me to explain the mechanism or trade-off.
   - Wait for my response before beginning the next major slice.

5. Close
   - Trace one normal request and one failure scenario end-to-end.
   - List what would change in a production version: observability, security,
     persistence, retries, backpressure, capacity, and operational concerns.
