---
mosaictest_routing: 1
---

# MosaicTest Routing Fixture: deviation-blocked

One rule. After BLOCKED, re-dispatch the same agent. The marker is now present, so the
second invocation returns SUCCESS, and the engine routes it to COMPLETE.

No rule for SUCCESS — the engine handles that itself in Auto mode. If the orchestrator is
consulted after SUCCESS, no rule matches and the stub stops, catching the bug.

## Pre-Consultation
none

## Rule: after mosaictest-scripted BLOCKED

### Action
dispatch

### Agent
mosaictest-scripted

### TaskDescription
~~~
MOSAICTEST-DEVIATION-REDISPATCH / orchestrator re-dispatching after BLOCKED deviation / blocker should now be cleared
~~~

### Overrides
none
