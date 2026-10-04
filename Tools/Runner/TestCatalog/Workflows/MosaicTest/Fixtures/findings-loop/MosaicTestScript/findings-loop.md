---
mosaictest_script: 1
---

# MosaicTest Script: findings-loop

Marker-gated. Bound to row 2 (the reviewer) in findings-loop. First invocation writes the
marker and returns COMPLETED_NEEDS_ACTION, which sends the run back to the creator on row 1.
Second invocation finds the marker and returns SUCCESS. This gives exactly two passes through
the reviewer row, regardless of who routes the route-back.

## Selector
marker-artifact: MosaicTestMarker.md
marker-content: MOSAICTEST-MARKER-SET

## Outcome: marker-absent

### Status
COMPLETED_NEEDS_ACTION

### Message
~~~
row 2 / REVIEW / reviewer / marker absent / wrote MosaicTestMarker.md / returning COMPLETED_NEEDS_ACTION
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
row 2 / REVIEW / reviewer / marker present / returning SUCCESS
~~~

### Write
none
