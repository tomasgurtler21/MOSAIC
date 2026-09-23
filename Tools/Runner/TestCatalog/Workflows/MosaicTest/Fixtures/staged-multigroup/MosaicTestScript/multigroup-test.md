---
mosaictest_script: 1
---

# MosaicTest Script: multigroup-test

Unconditional SUCCESS. Bound to the Test group row in staged-multigroup. Writes a stage
test artifact so the Implementation row can chain from it.

## Selector
none

## Outcome: always

### Status
SUCCESS

### Message
~~~
EXECUTION.Test / staged-multigroup / wrote MosaicTestStageTest.md / received task_description: {task_description} / returning SUCCESS
~~~

### Write
~~~
# MosaicTest Stage Test Artifact

Written by the multigroup-test script (Test group). The content is fixture data.

The file path this landed in carries the stage number, substituted from the row's
`{StageNumber}` token. The Implementation group's row takes this file as input,
so its existence at the expected path proves group ordering was correct.
~~~
