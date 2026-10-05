---
mosaictest_routing: 1
---

# MosaicTest Routing Fixture: infra-checkpoint-commit

Pre-consultation only. Every workflow step returns SUCCESS and the engine routes it, so after
the pre-consultation the orchestrator should never be consulted.

This file exists so the pre-consultation has a defined answer (`{}`). Without it the stub
orchestrator can only report "fixture not found", and whether it phrases that as JSON or prose
is up to the model, so the run's result would depend on chance.

It declares no rules on purpose. If the orchestrator is consulted after any step, no rule
matches and the stub stops, naming the state it saw. That keeps an unexpected consultation
visible as a failure.

## Pre-Consultation
none
