---
mosaictest_routing: 1
---

# MosaicTest Routing Fixture: findings-loop

Used only in the Auto mode run. In Auto-review the engine auto-routes CNA and never consults.

One rule: after the reviewer (row 2) returns COMPLETED_NEEDS_ACTION, dispatch the creator
again. The Runner binds a consultant dispatch to the first row naming the agent, which is
row 1 (the creator). From there the engine routes on its own: the creator's SUCCESS goes to the
reviewer via `next`, and the reviewer's second SUCCESS ends the run via `On Success = COMPLETE`.
No second rule is needed.

If the orchestrator is consulted after any SUCCESS, no rule matches and the stub stops. That
catches a mode mix-up where the run reaches the orchestrator when it should not.

## Pre-Consultation
none

## Rule: after mosaictest-scripted COMPLETED_NEEDS_ACTION

### Action
dispatch

### Agent
mosaictest-scripted

### Row
1

### TaskDescription
~~~
MOSAICTEST-FINDINGS-REDISPATCH / orchestrator re-dispatching the creator after the reviewer's CNA deviation
~~~

### Overrides
none
