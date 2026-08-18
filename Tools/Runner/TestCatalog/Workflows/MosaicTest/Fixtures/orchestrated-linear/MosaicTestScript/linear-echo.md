---
mosaictest_script: 1
---

# MosaicTest Script: linear-echo

Unconditional success whose message echoes the task description the dispatch carried.

The `{task_description}` placeholder is the whole point of this fixture. Every one of the three
dispatches in `orchestrated-linear` uses this same script, so the echoed text is the only thing
that distinguishes the three log rows from one another — and therefore the only direct evidence
that the orchestrator's per-dispatch content reached the subagent intact.

## Selector
none

## Outcome: always

### Status
SUCCESS

### Message
~~~
row 1 / RESEARCH / received task_description: {task_description} / returning SUCCESS
~~~

### Write
none
