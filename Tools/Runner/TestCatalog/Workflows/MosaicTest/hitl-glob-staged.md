---
version: "1.0"
name: "MosaicTest HITL Glob Staged Workflow"
description: "Harness conformance fixture — HITL approval check with Stage-* glob output artifacts. Exercises the glob-expansion path so that Stage-* wildcard patterns in output artifacts are resolved to concrete Stage-N/ paths before approval is read."
hint: "Harness test — HITL glob expansion, Stage-* output artifacts, no false redispatch"
author: MOSAIC
id: hitl-glob-staged
referenced_agents:
  - mosaictest-scripted
artifacts:
  - MosaicTestScript/hitl-glob-check.md
  - Stage-*/HITLGlobStage.md
---

<Workflow type="core" name="hitl-glob-staged" version="1.0">
## MosaicTest HITL Glob Staged Workflow

**Use when:** Verifying that the Runner correctly expands `Stage-*` glob patterns in output artifact paths when performing HITL approval checks, rather than attempting to read approval from a literal path containing `*`.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| VALIDATION | mosaictest-scripted | FALSE | - | - | MosaicTestScript/hitl-glob-check.md | Stage-*/HITLGlobStage.md |

**Notes:**
- **Run this workflow in Orchestrated mode only.** The routing fixture drives two dispatches of the single VALIDATION row: one with default HITL (false, from the table), then one with `hitl: true` override. Auto mode would route the SUCCESS on the first dispatch to `COMPLETE`, preventing the second dispatch from running.
- **Dispatch 1 (HITL=false):** `mosaictest-scripted` receives `human_in_the_loop: false`, reads `hitl-glob-check.md`, returns `SUCCESS` with `Write: none`. The `Stage-*` output artifact triggers stage re-derivation; `Plan.md` is read and the stage set `{1, 2}` is established for subsequent glob expansion.
- **Dispatch 2 (HITL=true override):** The orchestrator overrides `hitl: true`. `mosaictest-scripted` receives `human_in_the_loop: true` and returns `BLOCKED E503` (expected, asserted behaviour — the stub holds no user-contact tool). The HITL check runs: `expandStageGlobs` resolves `Stage-*/HITLGlobStage.md` to `Stage-1/HITLGlobStage.md` and `Stage-2/HITLGlobStage.md` and reads approval from both. Both files are pre-placed with `human_approved: true`. `DecideHITLCompliance` returns `HITLAccept` via the `Status != SUCCESS` shortcut — no false HITL redispatch occurs. The orchestrator then stops the run.
- **`Plan.md` is pre-placed**, not produced by a planner. It carries a two-stage table so that `expandStageGlobs` knows which concrete paths to generate when `Stage-*` is present in output artifacts.
- **`Stage-1/HITLGlobStage.md` and `Stage-2/HITLGlobStage.md` are pre-placed** with `human_approved: true`. They are the files that the HITL approval check reads after glob expansion. They are not written during the run.
- Seed `Fixtures/hitl-glob-staged` — the whole directory, not anything inside it.

</Workflow>

---

## Design Rationale

### Why two dispatches of one row rather than two rows

In Orchestrated mode the Runner resolves an agent name to the **first matching row**. With two rows both naming `mosaictest-scripted`, both dispatches would collapse onto row 1 and produce indistinguishable log entries. One row dispatched twice — with different orchestrator-controlled `hitl` overrides — avoids the ambiguity and keeps the fixture self-consistent with the `orchestrated-linear` and `orchestrated-backjump` patterns.

### Why the row's HITL column is FALSE

The table's `HITL` flag controls the default `human_in_the_loop` value sent on every auto-mode or default-override dispatch. Marking the row `FALSE` means the first dispatch (no override) sends `human_in_the_loop: false` and `mosaictest-scripted` returns `SUCCESS`, allowing the stage re-derivation path to fire. The orchestrator then overrides `hitl: true` for the second dispatch to trigger the HITL approval-check path.

### Why the stage set must be established before the second dispatch

`expandStageGlobs` expands `Stage-*` patterns using the currently known stage set. The stage set is nil until a step with a `Stage-*` output artifact is applied and Plan.md is re-derived. The first dispatch (SUCCESS with `Stage-*/HITLGlobStage.md` output) triggers this re-derivation. By the time the second dispatch's HITL check runs, `stages = {1, 2}` and glob expansion produces concrete paths.

### Why pre-placed Stage-N files with human_approved: true

The HITL approval check reads `human_approved` from the concrete Stage-N files after glob expansion. No agent in this fixture writes to Stage-* paths (wildcards are skipped by `mosaictest-scripted`). Pre-placing the files with `human_approved: true` means that if the approval check path were taken (i.e., if a real agent returned SUCCESS with HITL=true), the runner would correctly accept without false redispatch. The pre-placed files also serve as the input artifacts for the second dispatch, satisfying the runner's input resolution.

### What the glob expansion fix prevents

Before the fix, the HITL check loop iterated over the raw output-artifact list and called `ReadApproval` on the literal `Stage-*/HITLGlobStage.md` path. A file with `*` in its name cannot exist, so every HITL=true + Stage-* scenario produced `ApprovalFileMissing` for every approval entry. For a `SUCCESS` response this triggered an HITL redispatch (false positive). After the fix, `expandStageGlobs` resolves the glob to concrete paths and reads approval from the actual per-stage files.

---

## Expected Run

Five Orchestration.md log rows.

| Log `Seq` | `Agent` | Kind | `Phase` | `Status` | `Summary` shows |
|:---:|---|---|---|---|---|
| 1 | `orchestrator-script#1` | consultation | - | SUCCESS | step-one task description |
| 2 | `mosaictest-scripted#1` | workflow step | VALIDATION | SUCCESS | dispatch 1 / hitl=false / stage glob paths ready |
| 3 | `orchestrator-script#3` | consultation | - | SUCCESS | step-two task description with hitl override |
| 4 | `mosaictest-scripted#2` | workflow step | VALIDATION | BLOCKED | E503 / human_in_the_loop=true as designed |
| 5 | `orchestrator-script#5` | consultation | - | SUCCESS | stop reason |

**Run outcome:** `RunStoppedByConsultant`. The orchestrator ends the run after the BLOCKED from dispatch 2.

**Key observation:** Row 4 shows exactly one BLOCKED entry for the HITL row — no additional redispatch row appears. This is the assertion: the glob expansion ran (expanding `Stage-*/HITLGlobStage.md` to `Stage-1/` and `Stage-2/`) and both pre-placed files were found as approved. `DecideHITLCompliance` returned `HITLAccept` via the `Status != SUCCESS` shortcut without redispatching.

**RunnerLogs verification:** The debug log must contain a `session.hitl.accept` or equivalent event after seq 4, with no `session.hitl.redispatch` event. A `session.consult.stop` entry must follow naming the fixture's stop reason.

---

## What a Failure Means Here

| Observation | Where to look |
|---|---|
| A sixth log row appears showing a second BLOCKED from `mosaictest-scripted` | HITL false-redispatch occurred: the glob was not expanded and `ApprovalFileMissing` triggered a redispatch even for a BLOCKED response |
| Seq 2 shows BLOCKED instead of SUCCESS | The first dispatch incorrectly received `human_in_the_loop: true` — the routing fixture's `hitl: true` override may have leaked into dispatch 1 |
| Seq 4 shows SUCCESS instead of BLOCKED | The `hitl: true` override did not reach the agent — check the routing fixture override parsing |
| Run fails with `stage set not available` or similar | Plan.md was not re-derived after dispatch 1 — check that `Stage-*` in output artifacts triggers re-derivation |
| Approval check finds `ApprovalFileMissing` for Stage-1 or Stage-2 | The pre-placed `Stage-N/HITLGlobStage.md` files were not seeded correctly, or `expandStageGlobs` did not produce the expected concrete paths |

---

## Changelog

| Version | Date | Author | Summary |
|---------|------|--------|---------|
| 1.0 | 2026-08-28 | MOSAIC | Initial version. HITL glob expansion fixture. |

---

## Open Ideas / Dead Ends

**Ideas under consideration:**
- A variant where the agent returns SUCCESS with HITL=true (would require mosaictest-scripted to support a mode where it bypasses the human_in_the_loop check, or a different stub agent). This would directly exercise the approval-reading path rather than taking the Status!=SUCCESS shortcut.
- A three-stage variant to verify that expandStageGlobs produces the correct number of expanded paths.

**Dead ends (tried and rejected):**
- Using EXECUTION.[StageNumber] rows for the HITL row. Per-stage EXECUTION rows receive `Stage-{StageNumber}` substituted to a concrete path (e.g., `Stage-1/HITLGlobStage.md`), so no glob expansion is needed in the HITL check. The bug only manifests for non-EXECUTION rows that carry a literal `Stage-*` pattern in their output artifacts.
- Designing for Auto mode. Without a routing consultant, a BLOCKED from a HITL row in Auto mode resolves to `RunDeviationUnresolved`. Auto mode cannot complete this fixture cleanly.
