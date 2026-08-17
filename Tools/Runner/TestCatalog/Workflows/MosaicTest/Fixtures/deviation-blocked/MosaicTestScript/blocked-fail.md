---
mosaictest_script: 1
---

# MosaicTest Script: blocked-fail

Unconditional BLOCKED with E401. Simulates an external dependency blocker that the
orchestrator must resolve. The recovery path is in a separate script (blocked-recover.md)
delivered via input_artifacts override.

## Selector
none

## Outcome: always

### Status
BLOCKED

### Error
E401
DEPENDENCY_MISSING: fixture-declared blocker for the deviation-blocked workflow

### Message
~~~
row 1 / RESEARCH / fixture-declared blocker / returning BLOCKED E401
~~~

### Write
none
