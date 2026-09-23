---
mosaictest_routing: 1
---

# MosaicTest Routing Fixture: deviation-chain

Two rules. Two consecutive BLOCKEDs produce two consecutive consultations.

Rule 1 fires on the first BLOCKED (from chain-first.md) and re-dispatches with input_artifacts
overridden to chain-second.md — the unconditional BLOCKED script. Rule 2 fires on the second
BLOCKED (#2, from chain-second.md) and re-dispatches with input_artifacts overridden back to
chain-first.md. The marker is now present, so chain-first returns SUCCESS, and the engine
routes it to COMPLETE.

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
MOSAICTEST-CHAIN-REDISPATCH-1 / first deviation resolved / re-dispatching with chain-second script / expecting unconditional BLOCKED
~~~

### Overrides
input_artifacts: MosaicTestScript/chain-second.md

## Rule: after mosaictest-scripted BLOCKED #2

### Action
dispatch

### Agent
mosaictest-scripted

### TaskDescription
~~~
MOSAICTEST-CHAIN-REDISPATCH-2 / second deviation resolved / re-dispatching with chain-first script / marker now present, expecting SUCCESS
~~~

### Overrides
input_artifacts: MosaicTestScript/chain-first.md
