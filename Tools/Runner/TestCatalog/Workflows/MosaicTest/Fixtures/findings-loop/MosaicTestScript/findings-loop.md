---
mosaictest_script: 1
---

# MosaicTest Script: findings-loop

Marker-gated. First invocation writes the marker and returns COMPLETED_NEEDS_ACTION. Second
invocation finds the marker and returns SUCCESS. This gives exactly two passes through the
single workflow row, regardless of who routes the second dispatch.

## Selector
marker-artifact: MosaicTestMarker.md
marker-content: MOSAICTEST-MARKER-SET

## Outcome: marker-absent

### Status
COMPLETED_NEEDS_ACTION

### Message
~~~
row 1 / RESEARCH / marker absent / wrote MosaicTestMarker.md / returning COMPLETED_NEEDS_ACTION
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
