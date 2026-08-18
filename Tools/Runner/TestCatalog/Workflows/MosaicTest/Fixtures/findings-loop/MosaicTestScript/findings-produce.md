---
mosaictest_script: 1
---

# MosaicTest Script: findings-produce

Marker-gated. First invocation (no marker): returns COMPLETED_NEEDS_ACTION and writes the
findings report, setting the marker. Second invocation (marker present): returns SUCCESS
and echoes the task description to show whether it came from the engine or the orchestrator.

## Selector
marker-artifact: MosaicTestFindingsReport.md
marker-content: MOSAICTEST-FINDINGS-WRITTEN

## Outcome: marker-absent

### Status
COMPLETED_NEEDS_ACTION

### Message
~~~
row 1 / RESEARCH / findings produced / wrote MosaicTestFindingsReport.md / returning COMPLETED_NEEDS_ACTION
~~~

### Write
~~~
MOSAICTEST-FINDINGS-WRITTEN

Findings report placeholder. This content exists so the marker-content check passes on
the second invocation, transitioning the script to the marker-present branch.
~~~

## Outcome: marker-present

### Status
SUCCESS

### Message
~~~
row 1 / RESEARCH / marker present / findings already handled / received task_description: {task_description} / returning SUCCESS
~~~

### Write
none
