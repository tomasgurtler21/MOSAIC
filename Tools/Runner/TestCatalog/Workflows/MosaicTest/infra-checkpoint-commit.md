---
version: "1.0"
name: "MosaicTest Infrastructure Checkpoint+Commit Workflow"
description: "Runner mode fixture — Auto mode with checkpoint and commit infrastructure agents. Commit setup dispatch at run start, STAGE_END triggers at stage boundaries for both classes. Proves the trigger-and-marker path works end to end through a real harness."
hint: "Harness test — infrastructure triggers (STAGE_END), commit setup dispatch, marker extraction"
author: MOSAIC
id: infra-checkpoint-commit
referenced_agents:
  - mosaictest-scripted
artifacts:
  - MosaicTestScript/infra-echo.md
  - Stage-{StageNumber}/MosaicTestStage.md
modes:
  - auto
---

<Workflow type="core" name="infra-checkpoint-commit" version="1.0">
## MosaicTest Infrastructure Checkpoint+Commit Workflow

**Use when:** Verifying that infrastructure triggers fire through a real harness, that the commit setup dispatch runs at run start and extracts a branch marker, and that STAGE_END triggers fire at stage boundaries for both checkpoint-class and commit-class agents.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| EXECUTION.[StageNumber] | mosaictest-scripted | FALSE | - | - | MosaicTestScript/infra-echo.md | Stage-{StageNumber}/MosaicTestStage.md |

**EXECUTION Stages:** Loop per stage (stages defined in Plan.md). One row per stage.

**Notes:**
- **Run this workflow in Auto mode.**
- **Infrastructure agents are not in `referenced_agents`.** They are deployed from the catalog based on their `infrastructure:` frontmatter field and injected into the orchestrator's `<InfrastructureAgents>` region by the deploy tool. The workflow does not name them.
- **`Plan.md` is a pre-placed fixture, not produced by the run.** Seed `Fixtures/infra-checkpoint-commit` as the single seed path.
- The two infrastructure stubs (`mosaictest-checkpoint`, `mosaictest-commit`) both declare trigger `STAGE_END`. They fire at every stage boundary, in catalog order (alphabetical by key: checkpoint before commit).
- The commit setup dispatch fires once at run start, before the dispatch loop. It returns `[branch:mosaictest-run]`, which the runner records in the artifact's `commit_branch` frontmatter.
- No deviations occur, so the orchestrator is consulted only for pre-consultation (no routing fixture needed).

</Workflow>

---

## Design Rationale

### Why two infrastructure classes in one workflow

Testing checkpoint and commit triggers separately would prove each fires, but would not show that both fire at the same stage boundary without cascading. The no-cascades rule (Design.md) says infrastructure dispatches do not update `prevWorkflowStep` and do not trigger further infrastructure evaluations. Two triggers at one boundary exercises that rule.

### Why the commit setup dispatch matters

The commit setup dispatch is the only dispatch that fires *before* the dispatch loop. It establishes the branch name for the entire run. A failure here means the run refuses to start — testing that path end-to-end through a real harness proves the runner can invoke an infrastructure agent, parse its response, extract the `[branch:...]` marker, and record it, all before a single workflow step runs.

### Why two stages

One stage would fire STAGE_END only at run completion, when it is indistinguishable from PHASE_END. Two stages give a mid-run STAGE_END (after stage 1) that fires *during* the run, which is the interesting case — it proves the trigger evaluation runs between stages, not just at shutdown.

### Why no routing fixture

Auto mode with all-SUCCESS workflow steps needs no orchestrator consultation (except pre-consultation). The pre-consultation works without a routing fixture — the stub orchestrator returns `{}` when the fixture's Pre-Consultation section says `none`. If the orchestrator were consulted unexpectedly (e.g. after an infrastructure step, which would be a bug), the absence of a routing fixture causes the stub to stop with "fixture not found", making the bug visible.

---

## Expected Run

Eight dispatches total. Infrastructure agents fire in catalog order (alphabetical by key): checkpoint before commit.

| Log `Seq` | `Agent` | Kind | `Phase` | `Status` | `Summary` shows |
|:---:|---|---|---|---|---|
| 0 | `orchestrator-script#pre_consultation#1` | consultation | -- | "" | pre-run consultation response |
| 1 | `mosaictest-commit#1` | commit setup | -- | SUCCESS | `[branch:mosaictest-run]` marker |
| 2 | `mosaictest-scripted#2` | workflow step | EXECUTION.1 | SUCCESS | stage 1 row, wrote Stage-1/MosaicTestStage.md |
| 3 | `mosaictest-checkpoint#3` | infra trigger | -- | SUCCESS | STAGE_END, `[checkpoint:f00d0003]` marker |
| 4 | `mosaictest-commit#4` | infra trigger | -- | SUCCESS | STAGE_END, `[branch:mosaictest-run]` marker |
| 5 | `mosaictest-scripted#5` | workflow step | EXECUTION.2 | SUCCESS | stage 2 row, wrote Stage-2/MosaicTestStage.md |
| 6 | `mosaictest-checkpoint#6` | infra trigger | -- | SUCCESS | STAGE_END, `[checkpoint:f00d0006]` marker |
| 7 | `mosaictest-commit#7` | infra trigger | -- | SUCCESS | STAGE_END, `[branch:mosaictest-run]` marker |

**Run outcome:** COMPLETE. All workflow steps succeed, all infrastructure triggers fire.

**Key observations:**
- Seq 1 is the commit setup dispatch — before any workflow step. The artifact's `commit_branch` field should read `mosaictest-run`.
- Seq 3 and 4 fire at the stage 1/2 boundary. Seq 6 and 7 fire at run completion (stage 2 end). All four are STAGE_END triggers.
- Checkpoint markers are unique per invocation (`f00d0003`, `f00d0006`), confirming that the correct agent instance produced each row.
- Infrastructure rows are flagged `IsInfrastructure=true` in the log and do not update `current_state`.
- No cascading: checkpoint's dispatch does not trigger a re-evaluation that fires commit, and vice versa. Both fire from the same workflow step completion.

---

## What a Failure Means Here

| Observation | Where to look |
|---|---|
| Run refuses to start with "no commit-class infrastructure agent" | Infrastructure agents were not deployed — check deploy tool output for silent infra agent skip (ToolingGaps.md GAP-1) |
| Run refuses with "checkpoints requested but no checkpoint provider" | Same as above — checkpoint agent missing from deployment |
| Seq 1 missing (no commit setup row) | The commit setup dispatch did not fire; check `session.go` doCommitSetupDispatch |
| Commit setup fires but `commit_branch` is empty | The `[branch:mosaictest-run]` marker was not extracted from the stub's response |
| Only one pair of infra triggers (not two) | STAGE_END fired at run end but not at the stage 1/2 boundary — the trigger evaluator may not be running between stages |
| Infra triggers fire but in wrong order (commit before checkpoint) | The InfrastructureAgents region is not in alphabetical order, or the evaluator iterates differently from the declaration order |
| Three or more infra triggers at one stage boundary | Cascading: an infrastructure completion is triggering a re-evaluation, violating the no-cascades rule |
| Orchestrator consulted after an infrastructure step | A deviation was raised on an infrastructure completion — infrastructure steps must not produce deviations |
| Row 3 shows `current_state` updated | Infrastructure rows must not update `current_state`; the engine is treating an infra step as a workflow step |

---

## Changelog

| Version | Date | Author | Summary |
|---------|------|--------|---------|
| 1.0 | 2026-09-20 | MOSAIC | Initial version. Checkpoint + commit triggers, commit setup dispatch. |

---

## Open Ideas / Dead Ends

**Ideas under consideration:**
- A variant with `INVOCATION_INTERVAL(2)` on the checkpoint agent instead of `STAGE_END`, so both trigger types fire in one run.
- Adding a review-class agent to exercise all three infrastructure classes simultaneously.
- Testing the `on_failure: halt` path by making a stub return BLOCKED (requires changes to the infrastructure stubs).

**Dead ends (tried and rejected):**
- (none yet)
