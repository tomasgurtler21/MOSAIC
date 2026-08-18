---
mosaictest_routing: 1
---

# MosaicTest Routing Fixture: findings-loop

Used only in the Auto mode run. In Auto-review the engine auto-routes CNA and never consults.

One rule: after the stub returns COMPLETED_NEEDS_ACTION, dispatch the same agent again. The
re-dispatch's SUCCESS is routed by the engine (On Success = COMPLETE), so no second rule is
needed.

If the orchestrator is consulted after SUCCESS, no rule matches and the stub stops — catching
a mode confusion where the run reaches the orchestrator when it should not.

## Pre-Consultation
none

## Rule: after mosaictest-scripted COMPLETED_NEEDS_ACTION

### Action
dispatch

### Agent
mosaictest-scripted

### TaskDescription
~~~
MOSAICTEST-FINDINGS-REDISPATCH / orchestrator re-dispatching after CNA deviation / marker should now be present
~~~

### Overrides
none
