---
mosaictest_script: 1
---

# MosaicTest Script: preconsult-blocked

Marker-gated. First invocation (marker absent): returns BLOCKED with E401 and writes the
marker. Second invocation (marker present): returns SUCCESS and echoes the task description.

The second invocation is dispatched by the orchestrator after a deviation consultation. Its
task_description is the orchestrator's own text and should NOT contain the pre-consultation
advice marker. The echo makes this observable.

## Selector
marker-artifact: MosaicTestMarker.md
marker-content: MOSAICTEST-PRECONSULT-MARKER-SET

## Outcome: marker-absent

### Status
BLOCKED

### Error
E401
DEPENDENCY_MISSING: fixture-declared blocker for the preconsult-advice workflow

### Message
~~~
row 1 / RESEARCH / marker absent / fixture-declared blocker / wrote marker / returning BLOCKED E401
~~~

### Write
~~~
MOSAICTEST-PRECONSULT-MARKER-SET
~~~

## Outcome: marker-present

### Status
SUCCESS

### Message
~~~
row 1 / RESEARCH / marker present / received task_description: {task_description} / returning SUCCESS
~~~

### Write
none
