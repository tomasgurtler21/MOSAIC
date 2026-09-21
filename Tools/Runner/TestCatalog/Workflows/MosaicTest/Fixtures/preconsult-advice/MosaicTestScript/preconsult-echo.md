---
mosaictest_script: 1
---

# MosaicTest Script: preconsult-echo

Unconditional SUCCESS. Echoes the task description. This script is bound to the PLANNING row,
which is auto-routed by the engine. The Runner appends pre-consultation advice to auto-routed
dispatches, so the echoed task_description should contain the PRECONSULT-ADVICE-MARKER string.

## Selector
none

## Outcome: always

### Status
SUCCESS

### Message
~~~
row 2 / PLANNING / received task_description: {task_description} / returning SUCCESS
~~~

### Write
none
