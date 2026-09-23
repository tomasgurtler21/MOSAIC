---
mosaictest_script: 1
---

# MosaicTest Script: chain-first

Marker-gated. First invocation returns BLOCKED with a fixture-declared blocker and writes the
marker. Second invocation finds the marker and returns SUCCESS. The BLOCKED triggers a deviation
in Auto mode; the orchestrator re-dispatches through chain-second (which also BLOCKs), then
re-dispatches back here where the marker is now present.

## Selector
marker-artifact: MosaicTestMarker.md
marker-content: MOSAICTEST-CHAIN-MARKER-SET

## Outcome: marker-absent

### Status
BLOCKED

### Error
E401
DEPENDENCY_MISSING: fixture-declared blocker for the deviation-chain workflow (link 1)

### Message
~~~
chain-first / RESEARCH / marker absent / fixture-declared blocker / wrote marker / returning BLOCKED E401
~~~

### Write
~~~
MOSAICTEST-CHAIN-MARKER-SET
~~~

## Outcome: marker-present

### Status
SUCCESS

### Message
~~~
chain-first / RESEARCH / marker present / chain completed / returning SUCCESS
~~~

### Write
none
