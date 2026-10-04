---
mosaictest_script: 1
---

# MosaicTest Script: findings-create

Unconditional SUCCESS. Bound to row 1 (the creator) in findings-loop. Runs twice in every
passing run: once in order, and once more after the reviewer's COMPLETED_NEEDS_ACTION, routed
back by the engine in Auto-review or by the orchestrator in Auto.

## Selector
none

## Outcome: always

### Status
SUCCESS

### Message
~~~
row 1 / RESEARCH / creator / findings-loop / wrote MosaicTestCreated.md / returning SUCCESS
~~~

### Write
~~~
# MosaicTest Created Artifact

Written by the findings-create script (row 1). The content is fixture data.
~~~
