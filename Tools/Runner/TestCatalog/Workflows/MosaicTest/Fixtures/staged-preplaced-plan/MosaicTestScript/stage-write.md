---
mosaictest_script: 1
---

# MosaicTest Script: stage-write

## Selector
none

## Outcome: always

### Status
SUCCESS

### Message
~~~
row 1 / EXECUTION / staged-preplaced-plan / wrote MosaicTestStage.md / returning SUCCESS
~~~

### Write
~~~
# MosaicTest Stage Artifact

Written by the stage-write script. The content is fixture data and means nothing.

The file path this landed in is the assertion: it must carry the stage number of
the invocation that wrote it, substituted from the row's `{StageNumber}` token.
~~~
