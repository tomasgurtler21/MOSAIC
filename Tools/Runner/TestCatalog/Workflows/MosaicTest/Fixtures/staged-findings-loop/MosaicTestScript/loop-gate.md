---
mosaictest_script: 1
---

# MosaicTest Script: loop-gate

Marker-gated. Bound to row 2 (Test group, gate) in staged-findings-loop. First pass writes the
marker and returns COMPLETED_NEEDS_ACTION, triggering the On Findings route-back to row 1.
Second pass finds the marker and returns SUCCESS. A correct run reaches this script twice;
a run affected by the row-drift defect reaches it only once.

## Selector
marker-artifact: MosaicTestLoopGate.md
marker-content: MOSAICTEST-LOOP-GATE-SET

## Outcome: marker-absent

### Status
COMPLETED_NEEDS_ACTION

### Message
~~~
row 2 / EXECUTION.Test / gate / staged-findings-loop / marker absent, wrote MosaicTestLoopGate.md / returning COMPLETED_NEEDS_ACTION
~~~

### Write
~~~
MOSAICTEST-LOOP-GATE-SET

Gate findings placeholder. Its presence flips the gate to SUCCESS on the second pass.
~~~

## Outcome: marker-present

### Status
SUCCESS

### Message
~~~
row 2 / EXECUTION.Test / gate / staged-findings-loop / marker present, second pass / returning SUCCESS
~~~

### Write
none
