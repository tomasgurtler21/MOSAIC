---
mosaictest_routing: 1
---

# MosaicTest Routing Fixture: orchestrated-linear

Four rules. Three dispatches of the workflow's single row, each with a different task
description, then a stop. Nothing is overridden, so every dispatch uses the routing table's
own artifact list — which is what binds the stub subagent to its behaviour fixture.

A fifth consultation would match no rule. That is deliberate: it is how this fixture catches a
Runner that consults more often than Orchestrated mode requires.

## Pre-Consultation
none

## Rule: run-start

### Action
dispatch

### Agent
mosaictest-scripted

### TaskDescription
~~~
MOSAICTEST-STEP-ONE / this text was written by the orchestrator for the first dispatch / echo it back verbatim
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
MOSAICTEST-STEP-TWO / second dispatch / this text differs from step one so the log can tell them apart / echo it back verbatim
~~~

### Overrides
none

## Rule: after mosaictest-scripted SUCCESS #2

### Action
dispatch

### Agent
mosaictest-scripted

### TaskDescription
~~~
MOSAICTEST-STEP-THREE / third and final dispatch / echo it back verbatim
~~~

### Overrides
none

## Rule: after mosaictest-scripted SUCCESS #3

### Action
stop

### Reason
~~~
MOSAICTEST-ROUTING-COMPLETE / three dispatches done as scripted / ending the run on fixture instruction
~~~
