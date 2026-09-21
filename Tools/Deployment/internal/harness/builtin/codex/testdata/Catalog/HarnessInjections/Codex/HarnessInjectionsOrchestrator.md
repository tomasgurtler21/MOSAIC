---
version: "1.0.0"
harness: codex
---

# Harness Injections Orchestrator -- Codex (module-local test fixture)

This file is a FROZEN test fixture for the codex package's own unit tests (T5.6).
It is NOT the live catalog content and must NOT be updated in step with the real
Catalog/HarnessInjections/Codex/HarnessInjectionsOrchestrator.md. If you need to
change the test assertions in codex_test.go, update both this file and the matching
constant.

<HarnessConstraints type="managed">
As an orchestrator in Codex, you may spawn subagents and perform workspace writes within the declared sandbox.
</HarnessConstraints>
