---
mosaictest_script: 1
---

# MosaicTest Script: chain-second

Unconditional BLOCKED. The second link in the deviation chain: it always fails, forcing a
second orchestrator consultation. The recovery path is back in chain-first.md (where the
marker is now present) delivered via input_artifacts override on the second consultation.

## Selector
none

## Outcome: always

### Status
BLOCKED

### Error
E401
DEPENDENCY_MISSING: fixture-declared blocker for the deviation-chain workflow (link 2)

### Message
~~~
chain-second / RESEARCH / unconditional blocker / second link in chain / returning BLOCKED E401
~~~

### Write
none
