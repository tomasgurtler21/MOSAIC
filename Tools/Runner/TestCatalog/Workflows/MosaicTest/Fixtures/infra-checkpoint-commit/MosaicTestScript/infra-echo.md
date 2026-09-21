---
mosaictest_script: 1
---

# MosaicTest Script: infra-echo

Unconditional SUCCESS with a self-describing message and a written output artifact.
Used in every stage of infra-checkpoint-commit. The written artifact proves the
`{StageNumber}` substitution resolved correctly for the output path.

## Selector
none

## Outcome: always

### Status
SUCCESS

### Message
~~~
row 1 / EXECUTION / infra-checkpoint-commit / wrote MosaicTestStage.md / returning SUCCESS
~~~

### Write
~~~
# MosaicTest Stage Artifact

Written by the infra-echo script. The content is fixture data and means nothing.

The file path this landed in is the assertion: it must carry the stage number of
the invocation that wrote it, substituted from the row's `{StageNumber}` token.
~~~
