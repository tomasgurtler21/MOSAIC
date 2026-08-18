---
mosaictest_script: 1
---

# MosaicTest Script: deviation-stop-fail

Unconditional BLOCKED. The run deviates, the orchestrator stops, and the artifact is left
in a resumable state. There is no second pass — the orchestrator ends the run.

## Selector
none

## Outcome: always

### Status
BLOCKED

### Error
E501
TOOL_UNAVAILABLE: fixture-declared tool failure for the deviation-stop workflow

### Message
~~~
row 1 / RESEARCH / fixture-declared blocker / returning BLOCKED E501
~~~

### Write
none
