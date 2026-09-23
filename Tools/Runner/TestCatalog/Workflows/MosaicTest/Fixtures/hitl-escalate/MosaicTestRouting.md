---
mosaictest_routing: 1
---

# MosaicTest Routing Fixture: hitl-escalate

One rule. After the second SUCCESS from mosaictest-scripted (the HITL re-dispatch that also
failed approval), the Runner escalates to a deviation and consults. Stop the run.

The `#2` count selector matches because both the initial dispatch and the re-dispatch return
SUCCESS, giving exactly two occurrences of `mosaictest-scripted SUCCESS` in the log by the
time the escalation fires.

No rule for a single SUCCESS — the HITL compliance check intercepts before routing, so the
orchestrator is never consulted after the first dispatch. If it were, no rule would match
and the stub would stop, catching the bug.

## Pre-Consultation
none

## Rule: after mosaictest-scripted SUCCESS #2

### Action
stop

### Reason
~~~
MOSAICTEST-HITL-ESCALATION / HITL approval failed after re-dispatch / both invocations returned SUCCESS with human_approved: false / escalation acknowledged, stopping run
~~~
