---
mosaictest_routing: 1
---

# MosaicTest Routing Fixture: orchestrated-backjump

Four rules. Three dispatches of the workflow's single row with escalating overrides, then a stop
after the BLOCKED that the third dispatch's HITL override produces.

## Pre-Consultation
none

## Rule: run-start

### Action
dispatch

### Agent
mosaictest-scripted

### TaskDescription
~~~
MOSAICTEST-BACKJUMP-ONE / first dispatch, no overrides / table defaults apply / echo it back verbatim
~~~

### Overrides
none

## Rule: after mosaictest-scripted SUCCESS #1

### Action
dispatch

### Agent
mosaictest-scripted

### TaskDescription
~~~
MOSAICTEST-BACKJUMP-TWO / second dispatch / input_artifacts overridden to include MosaicTestExtraInput.md / echo it back verbatim
~~~

### Overrides
input_artifacts: MosaicTestScript/backjump-echo.md, MosaicTestExtraInput.md

## Rule: after mosaictest-scripted SUCCESS #2

### Action
dispatch

### Agent
mosaictest-scripted

### TaskDescription
~~~
MOSAICTEST-BACKJUMP-THREE / third dispatch / hitl_override true / stub will return BLOCKED E503
~~~

### Overrides
hitl: true

## Rule: after mosaictest-scripted BLOCKED

### Action
stop

### Reason
~~~
MOSAICTEST-ROUTING-COMPLETE / three dispatches done / BLOCKED from HITL override confirmed the override reached the agent / ending the run
~~~
