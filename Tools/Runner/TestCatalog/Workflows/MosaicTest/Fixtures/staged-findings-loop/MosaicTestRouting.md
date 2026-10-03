---
mosaictest_routing: 1
---

# MosaicTest Routing Fixture: staged-findings-loop

Auto-review only. The engine routes every step itself, so after the pre-consultation the
orchestrator should never be consulted.

The one rule is a tripwire: if the gate's COMPLETED_NEEDS_ACTION reaches the orchestrator,
the engine failed to auto-route it, and the run stops with a reason saying so.

## Pre-Consultation
none

## Rule: after mosaictest-scripted COMPLETED_NEEDS_ACTION

### Action
stop

### Reason
~~~
MOSAICTEST-STAGED-FINDINGS-LOOP / orchestrator consulted after CNA / Auto-review should have routed it back via On Findings without consulting
~~~
