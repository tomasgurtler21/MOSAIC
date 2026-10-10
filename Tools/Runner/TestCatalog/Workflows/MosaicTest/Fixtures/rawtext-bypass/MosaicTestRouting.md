---
mosaictest_routing: 1
---

# MosaicTest Routing Fixture: rawtext-bypass

One rule. The orchestrator is consulted once the E501 budget (original dispatch, bypass
redispatch and one engine re-dispatch) is used up. Each failed dispatch is recorded as a
workflow row with BLOCKED status, so the state is `after mosaictest-wronganswer BLOCKED #1`. Stop the run — the agent is
structurally incapable of producing a valid response, so re-dispatching would loop.

## Pre-Consultation
none

## Rule: after mosaictest-wronganswer BLOCKED #1

### Action
stop

### Reason
~~~
MOSAICTEST-RAWTEXT-BYPASS-STOP / orchestrator consulted after bypass exhaustion / agent mosaictest-wronganswer cannot produce valid protocol response / stopping run
~~~
