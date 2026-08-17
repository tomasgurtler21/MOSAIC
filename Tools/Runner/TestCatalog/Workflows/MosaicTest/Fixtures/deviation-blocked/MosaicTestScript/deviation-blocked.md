---
mosaictest_script: 1
---

# MosaicTest Script: deviation-blocked

Marker-gated. First invocation returns BLOCKED with a fixture-declared blocker and writes the
marker. Second invocation finds the marker and returns SUCCESS. The BLOCKED triggers a deviation
in Auto mode; the orchestrator re-dispatches; the second pass completes.

## Selector
marker-artifact: MosaicTestMarker.md
marker-content: MOSAICTEST-MARKER-SET

## Outcome: marker-absent

### Status
BLOCKED

### Error
E401
DEPENDENCY_MISSING: fixture-declared blocker for the deviation workflow

### Message
~~~
row 1 / RESEARCH / marker absent / fixture-declared blocker / wrote marker / returning BLOCKED E401
~~~

### Write
~~~
MOSAICTEST-MARKER-SET
~~~

## Outcome: marker-present

### Status
SUCCESS

### Message
~~~
row 1 / RESEARCH / marker present / returning SUCCESS
~~~

### Write
none
