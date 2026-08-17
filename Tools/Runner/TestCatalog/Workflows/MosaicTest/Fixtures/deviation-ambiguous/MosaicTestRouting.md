---
mosaictest_routing: 1
---

# MosaicTest Routing Fixture: deviation-ambiguous

One rule. In Auto-review mode with an ambiguous On Findings hint, the engine cannot
auto-route COMPLETED_NEEDS_ACTION and deviates instead. This rule resolves the deviation
by dispatching back to the same row. The marker file already exists from the first
invocation, so the second invocation returns SUCCESS.

## Pre-Consultation
none

## Rule: after mosaictest-scripted COMPLETED_NEEDS_ACTION

### Action
dispatch

### Agent
mosaictest-scripted

### TaskDescription
~~~
MOSAICTEST-AMBIGUOUS-REROUTE / deviation resolved by orchestrator / ambiguous On Findings forced consultation
~~~

### Overrides
none
