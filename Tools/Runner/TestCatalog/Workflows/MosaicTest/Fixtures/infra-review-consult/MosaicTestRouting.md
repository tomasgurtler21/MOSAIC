---
mosaictest_routing: 1
---

# MosaicTest Routing Fixture: infra-review-consult

One rule. After the review infrastructure agent fires, the Runner does a post-review routing
consultation with the orchestrator. This fixture handles that consultation by stopping the run.

The three SUCCESS workflow steps are routed by the engine in Auto mode (On Success = next / COMPLETE),
so no rules are needed for them. If the orchestrator is consulted after a regular SUCCESS step,
no rule matches and the stub stops with "no matching rule" — catching the bug.

## Pre-Consultation
none

## Rule: after mosaictest-review SUCCESS

### Action
stop

### Reason
~~~
MOSAICTEST-REVIEW-CONSULT-STOP / post-review consultation / orchestrator received review observations / stopping run as designed
~~~
