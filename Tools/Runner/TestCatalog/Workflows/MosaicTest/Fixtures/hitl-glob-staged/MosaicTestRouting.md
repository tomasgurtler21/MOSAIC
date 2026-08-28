---
mosaictest_routing: 1
---

# MosaicTest Routing Fixture: hitl-glob-staged

Three rules. Two dispatches of the workflow's single VALIDATION row, then a stop.

Dispatch 1 uses table defaults (HITL=false from the row). The agent returns SUCCESS, which
triggers Stage-* output re-derivation and populates the session's stage set from Plan.md.

Dispatch 2 overrides HITL to true. The agent returns BLOCKED E503 (expected for
human_in_the_loop=true). The HITL check runs expandStageGlobs on the Stage-* output artifact,
reads approval from the concrete Stage-1/ and Stage-2/ files (both pre-placed as approved),
and accepts via the Status!=SUCCESS shortcut. No HITL redispatch occurs.

An unmatched state after the BLOCKED would mean the Runner consulted more than expected.

## Pre-Consultation
none

## Rule: run-start

### Action
dispatch

### Agent
mosaictest-scripted

### TaskDescription
~~~
MOSAICTEST-HITL-GLOB-DISPATCH-ONE / VALIDATION row / hitl=false / returning SUCCESS / stage re-derivation will fire after this step
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
MOSAICTEST-HITL-GLOB-DISPATCH-TWO / VALIDATION row / hitl=true override / human_in_the_loop=true / expecting BLOCKED E503 / glob expansion of Stage-* runs for approval check
~~~

### Overrides
hitl: true

## Rule: after mosaictest-scripted BLOCKED

### Action
stop

### Reason
~~~
MOSAICTEST-ROUTING-COMPLETE / two dispatches done as scripted / BLOCKED E503 from HITL dispatch confirmed no false redispatch / ending the run on fixture instruction
~~~
