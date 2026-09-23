---
mosaictest_script: 1
---

# MosaicTest Script: multigroup-impl

Unconditional SUCCESS. Bound to the Implementation group row in staged-multigroup. Reads
the Test group's artifact (chained via input_artifacts) and writes an implementation artifact.

## Selector
none

## Outcome: always

### Status
SUCCESS

### Message
~~~
EXECUTION.Implementation / staged-multigroup / read MosaicTestStageTest.md from Test group / wrote MosaicTestStageImpl.md / received task_description: {task_description} / returning SUCCESS
~~~

### Write
~~~
# MosaicTest Stage Implementation Artifact

Written by the multigroup-impl script (Implementation group). The content is fixture data.

This file's existence proves the Implementation group ran after the Test group within
the same stage — if it had run first, the chained Test artifact would have been missing
and the stub would have returned BLOCKED.
~~~
