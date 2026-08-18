---
mosaictest_script: 1
---

# MosaicTest Script: deviation-fail

Unconditional BLOCKED with E401. The same script as blocked-fail.md in deviation-blocked,
but kept as a separate copy per the fixture convention: no two workflows share a fixture,
so drift between copies is a signal rather than a problem to prevent.

## Selector
none

## Outcome: always

### Status
BLOCKED

### Error
E401
DEPENDENCY_MISSING: fixture-declared blocker for the deviation-stop workflow

### Message
~~~
row 1 / RESEARCH / fixture-declared blocker / returning BLOCKED E401
~~~

### Write
none
