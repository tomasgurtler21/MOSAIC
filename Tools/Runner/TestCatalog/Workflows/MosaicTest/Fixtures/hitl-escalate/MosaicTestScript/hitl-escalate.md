---
mosaictest_script: 1
---

# MosaicTest Script: hitl-escalate

Unconditional SUCCESS with E1 and E3 fixture fields. Both invocations (initial dispatch and
HITL re-dispatch) return the same response: SUCCESS with `human_approved: false` in the
output artifact.

**Requires stub enhancements E1 and E3.** Without E3 (`hitl_behaviour: proceed`) the stub
auto-refuses on `human_in_the_loop: true` with BLOCKED/E503. Without E1 (`HitlApproval: false`)
the stub cannot write `human_approved: false` in the output artifact.

## Selector
none

## Outcome: always

### Status
SUCCESS

### HitlBehaviour
proceed

### HitlApproval
false

### Message
~~~
row 1 / RESEARCH / hitl_behaviour=proceed / hitl_approval=false / returning SUCCESS with unapproved artifact
~~~

### Write
~~~
human_approved: false
This artifact was deliberately not approved by the fixture.
~~~
