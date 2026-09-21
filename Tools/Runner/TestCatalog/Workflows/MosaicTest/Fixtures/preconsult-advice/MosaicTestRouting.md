---
mosaictest_routing: 1
---

# MosaicTest Routing Fixture: preconsult-advice

The pre-consultation section carries distinctive marker strings. The Runner appends these to
every auto-routed dispatch. Orchestrator-written dispatches do not carry them.

One rule: after BLOCKED, re-dispatch the same agent. The orchestrator's own TaskDescription
deliberately does NOT contain the preconsult markers — that absence is the proof that
orchestrator-written dispatches are not contaminated.

No rule for SUCCESS — the engine handles routing from RESEARCH to PLANNING and from PLANNING
to COMPLETE itself in Auto mode. If the orchestrator is consulted after SUCCESS, no rule
matches and the stub stops, catching the bug.

## Pre-Consultation
task_description:
~~~
PRECONSULT-ADVICE-MARKER: this text proves pre-consultation reached the dispatch
~~~
constraints:
~~~
PRECONSULT-CONSTRAINT-MARKER: constraint from pre-consultation
~~~

## Rule: after mosaictest-scripted BLOCKED

### Action
dispatch

### Agent
mosaictest-scripted

### TaskDescription
~~~
MOSAICTEST-PRECONSULT-REDISPATCH / orchestrator re-dispatching after BLOCKED deviation / this text is orchestrator-written and must NOT contain PRECONSULT-ADVICE-MARKER
~~~

### Overrides
none
