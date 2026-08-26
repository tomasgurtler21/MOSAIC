---
version: "1.0"
name: "MosaicTest Deviation Blocked Workflow"
description: "Runner mode fixture — Auto mode deviation handling. The stub returns BLOCKED, the engine deviates, the orchestrator re-dispatches, and the run completes. Proves the deviation-to-consultation path works end to end."
hint: "Mode 2 test — BLOCKED deviation, orchestrator re-dispatch, run completion"
author: MOSAIC
id: deviation-blocked
referenced_agents:
  - mosaictest-scripted
artifacts:
  - MosaicTestScript/deviation-blocked.md
  - MosaicTestMarker.md
---

<Workflow type="core" name="deviation-blocked" version="1.0">
## MosaicTest Deviation Blocked Workflow

**Use when:** Verifying that a `BLOCKED` result in Auto mode becomes a deviation, the orchestrator is consulted, and the run resumes when the orchestrator re-dispatches.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| RESEARCH | mosaictest-scripted | FALSE | COMPLETE | - | MosaicTestScript/deviation-blocked.md | MosaicTestMarker.md |

**Notes:**
- **Run this workflow in Auto mode.**
- The script is marker-gated. First invocation (marker absent): returns `BLOCKED`/`E401` with a fixture-declared blocker message and writes the marker. Second invocation (marker present): returns `SUCCESS`.
- The BLOCKED triggers a `DeviationNonSuccess` in the engine. The session consults the orchestrator, which re-dispatches the same agent.
- `On Success = COMPLETE` terminates the run after the second invocation succeeds.
- Seed `Fixtures/deviation-blocked` — the whole directory, not anything inside it.

</Workflow>

---

## Design Rationale

### Why BLOCKED specifically

`BLOCKED` is the clearest deviation trigger: it has an error code and an error reason, so the stub's response is unambiguous and the Runner's deviation log entry is diagnosable. `PARTIALLY_DONE` or `NEEDS_CLARIFICATION` would also deviate, but their messages are less distinctive and provide no additional coverage.

### Why the marker gate

The first pass must return BLOCKED (deviation trigger). The second pass must return SUCCESS (run completion). The marker is the mechanism that distinguishes the two passes: absent on first, present on second.

The stub writes the marker on the BLOCKED pass even though BLOCKED normally means "could not work." This is fixture behaviour, not real agent behaviour — the marker exists to control the test flow, not to represent meaningful progress.

### Why the orchestrator re-dispatches the same agent

The orchestrator could dispatch a different agent or stop. Re-dispatching the same agent is the most interesting case: it proves the deviation resolution loop returns control to the workflow engine, which then re-evaluates the row.

---

## Expected Run

Three Orchestration.md log rows.

| Log `Seq` | `Agent` | Kind | `Phase` | `Status` | `Summary` shows |
|:---:|---|---|---|---|---|
| 1 | `mosaictest-scripted#1` | workflow step | RESEARCH | BLOCKED | fixture-declared blocker, E401 |
| 2 | `orchestrator-script#2` | consultation | — | SUCCESS | dispatch instruction for re-dispatch |
| 3 | `mosaictest-scripted#3` | workflow step | RESEARCH | SUCCESS | marker present, returning SUCCESS |

**Run outcome:** COMPLETE. The engine routes `SUCCESS` via `On Success = COMPLETE`.

**Key observation:** The consultation row (Seq 2) proves the deviation-to-orchestrator path fired. The SUCCESS on row 3 proves the re-dispatch worked and the run completed normally.

---

## What a Failure Means Here

| Observation | Where to look |
|---|---|
| Run stops after row 1 with `RunDeviationUnresolved` | No routing consultant configured — the harness adapter's `InvokeRaw` path is missing (RUN-4) |
| Consultation row appears but the re-dispatch fails | The orchestrator's dispatch instruction could not be parsed, or the named agent is not in the routing table |
| Row 3 shows BLOCKED again instead of SUCCESS | The marker was not written on the first pass — check that the stub writes on BLOCKED outcomes |
| No BLOCKED row at all, run completes in one step | The script's marker-absent branch was not taken — fixture seeding may have pre-placed the marker |
| The stub stops with "no matching rule" | The routing fixture does not cover `after mosaictest-scripted BLOCKED` |

---

## Changelog

| Version | Date | Author | Summary |
|---------|------|--------|---------|
| 1.0 | 2026-08-17 | MOSAIC | Initial version. BLOCKED deviation and orchestrator re-dispatch. |

---

## Open Ideas / Dead Ends

**Ideas under consideration:**
- A variant where the orchestrator stops instead of re-dispatching, to test that a stopped-after-deviation run is resumable.
- Testing `PARTIALLY_DONE` and `NEEDS_CLARIFICATION` deviations (same path, different status codes).

**Dead ends (tried and rejected):**
- Using `CAPABILITY_EXCEEDED` as the deviation trigger. Same path as BLOCKED but less observable — no error code to verify in the log.
