---
mosaictest_routing: 1
---

# MosaicTest Routing Fixture: deviation-stop

One rule. After BLOCKED, stop the run. The artifact is left as-is and remains resumable.

## Pre-Consultation
none

## Rule: after mosaictest-scripted BLOCKED

### Action
stop

### Reason
~~~
MOSAICTEST-DEVIATION-STOP / orchestrator stopping the run after BLOCKED deviation / artifact left resumable
~~~
