---
version: "1.0"
name: "MosaicTest Smoke Single Workflow"
description: "Harness conformance fixture — one agent, one invocation, SUCCESS to COMPLETE. The simplest run mosaic-run can perform."
hint: "Harness smoke test — single invocation, envelope parse and identifier echo"
author: MOSAIC
id: smoke-single
referenced_agents:
  - mosaictest-scripted
artifacts:
  - MosaicTestScript/smoke-success.md
---

[[SECTION:Workflow:smoke-single]]
<!-- workflow-version: 1.0 -->
## MosaicTest Smoke Single Workflow

**Use when:** Verifying that `mosaic-run` can drive a harness at all. This is the first workflow to run against any new or changed harness — if it fails, nothing else in the suite is worth running.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| RESEARCH | mosaictest-scripted | ❌ | COMPLETE | - | MosaicTestScript/smoke-success.md | - |

**Notes:**
- Non-staged. With zero staged rows, workflow admission short-circuits, so none of the staged-shape conditions apply.
- No `Plan.md` is read and no stage table is required.
- The script fixture must be seeded into the run folder at run creation. Seed `Fixtures/smoke-single` — the whole directory, not anything inside it — as the single seed path. See `Fixtures/README.md`.

[[/SECTION:Workflow:smoke-single]]

---

## Design Rationale

This workflow exists to isolate the harness invocation path from everything else. One row, one invocation, no stages, no artifacts written, no plan read. When it passes, the following are all known good for this harness: the agent was located and invoked from the command line; the model was selected; the prompt was delivered; a JSON response envelope came back and was parsed; the protocol response was extracted from it; and `run_id` and `agent_instance_id` survived the round trip intact.

That last property is why the row declares an input artifact but no output. The script fixture is the only thing that tells the stub what to do, so reading it also proves the harness resolved the working directory correctly and that file-read tooling functions. Writing an output artifact would add provenance stamping to the surface under test, which belongs in a later fixture where a failure can be attributed.

`On Success` is `COMPLETE` rather than another agent because the whole point is to terminate immediately. A run that reaches `COMPLETE` has exercised the runner's completion path as well.

---

## Changelog

| Version | Date | Author | Summary |
|---------|------|--------|---------|
| 1.0 | 2026-08-03 | MOSAIC | Initial version |

---

## Open Ideas / Dead Ends

**Ideas under consideration:**
- A variant with `HITL ✅` to assert the `BLOCKED`/`E503` path, which `mosaictest-scripted` returns by design since it declares no user-interaction tool.

**Dead ends (tried and rejected):**
- (none yet)
