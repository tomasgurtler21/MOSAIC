---
version: "1.2"
name: "MosaicTest Infrastructure Checkpoint+Commit Workflow"
description: "Runner mode fixture — Auto mode with checkpoint and commit infrastructure agents. Commit setup dispatch at run start, STAGE_END firing for both classes at each stage boundary. Proves the trigger-and-marker path works end to end through a real harness. Currently RED: exposes the STAGE_END timing gap."
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
infrastructure_agents:
  - mosaictest-checkpoint
  - mosaictest-commit
checkpoints: enabled
commits: enabled
---

<Workflow type="core" name="infra-checkpoint-commit" version="1.2">
## MosaicTest Infrastructure Checkpoint+Commit Workflow

**Use when:** Verifying that infrastructure triggers fire through a real harness, that the commit setup dispatch runs at run start and extracts a branch marker, and that STAGE_END triggers fire on a stage transition for both checkpoint-class and commit-class agents.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| EXECUTION.[StageNumber] | mosaictest-scripted | FALSE | - | - | MosaicTestScript/infra-echo.md | Stage-{StageNumber}/MosaicTestStage.md |

**EXECUTION Stages:** Loop per stage (stages defined in Plan.md). One row per stage.

**Notes:**
- **Run this workflow in Auto mode.**
- **Infrastructure agents are not in `referenced_agents`.** They are deployed from the catalog based on their `infrastructure:` frontmatter field and injected into the orchestrator's `<InfrastructureAgents>` region by the deploy tool. The workflow does not name them.
- **`Plan.md` is a pre-placed fixture, not produced by the run.** Seed `Fixtures/infra-checkpoint-commit` as the single seed path.
- The two infrastructure stubs (`mosaictest-checkpoint`, `mosaictest-commit`) both declare trigger `STAGE_END`. They fire on each observed stage transition, in catalog order (alphabetical by key: checkpoint before commit).
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

Two stages give two stage boundaries, which is the minimum needed to tell a correctly-timed
trigger from a late one. With one stage you cannot distinguish "fires at the end of the stage"
from "fires at the end of the run" — they are the same moment. With two, a correct
implementation puts an infra pair *between* the two workflow steps, and any late implementation
visibly bunches them afterwards.

It also covers the final-stage case: stage 2 has no successor step, so it catches implementations
that can only detect a boundary by looking backwards from the next stage.

### Why no routing fixture

Auto mode with all-SUCCESS workflow steps needs no orchestrator consultation (except pre-consultation). The pre-consultation works without a routing fixture — the stub orchestrator returns `{}` when the fixture's Pre-Consultation section says `none`. If the orchestrator were consulted unexpectedly (e.g. after an infrastructure step, which would be a bug), the absence of a routing fixture causes the stub to stop with "fixture not found", making the bug visible.

---

## Expected Run

> **This fixture is currently RED on purpose.** It describes the intended behaviour after the
> `STAGE_END` timing fix (`Issue-Runner-TerminalStageEndTrigger.md`), not what the Runner does
> today. Today the two infra pairs do not straddle the stage 2 step — the first pair fires late
> (after stage 2's step) and the second never fires at all. Do not "fix" this file to match the
> current output; the gap it exposes is the point.

Eight dispatches total. Infrastructure agents fire in catalog order (alphabetical by key):
checkpoint before commit.

| Log `Seq` | `Agent` | Kind | `Phase` | `Status` | `Summary` shows |
|:---:|---|---|---|---|---|
| 0 | `mosaictest-commit#1` | commit setup | -- | SUCCESS | `[branch:mosaictest-run]` marker |
| 1 | `orchestrator-script#pre_consultation#1` | consultation | -- | "" | pre-run consultation response |
| 2 | `mosaictest-scripted#2` | workflow step | EXECUTION.1 | SUCCESS | stage 1 row, wrote Stage-1/MosaicTestStage.md |
| 3 | `mosaictest-checkpoint#3` | infra trigger | -- | SUCCESS | STAGE_END at end of stage 1, `[checkpoint:...]` marker |
| 4 | `mosaictest-commit#4` | infra trigger | -- | SUCCESS | STAGE_END at end of stage 1, `[branch:mosaictest-run]` marker |
| 5 | `mosaictest-scripted#5` | workflow step | EXECUTION.2 | SUCCESS | stage 2 row, wrote Stage-2/MosaicTestStage.md |
| 6 | `mosaictest-checkpoint#6` | infra trigger | -- | SUCCESS | STAGE_END at end of stage 2, `[checkpoint:...]` marker |
| 7 | `mosaictest-commit#7` | infra trigger | -- | SUCCESS | STAGE_END at end of stage 2, `[branch:mosaictest-run]` marker |

**Run outcome:** COMPLETE. All workflow steps succeed, both STAGE_END boundaries fire.

**Key observations:**
- Seq 0 is the commit setup dispatch — before any workflow step *and* before pre-consultation.
  Per Design.md §4.2 the setup dispatch is run-start step 7a, while pre-consultation follows
  artifact creation (step 8) because its request carries the artifact path. This ordering is
  correct today and is **not** part of the pending fix. The artifact's `commit_branch` field
  should read `mosaictest-run`.
- **Each infra pair straddles a stage boundary.** Seq 3/4 fire after stage 1's step and before
  stage 2's step; Seq 6/7 fire after stage 2's step. This is what makes per-stage commits
  meaningful: the commit at Seq 4 sees only stage 1's files, and the commit at Seq 7 sees only
  stage 2's.
- **Why this is RED today.** `STAGE_END` is currently evaluated by comparing the completed step's
  stage against the *previous* step's stage, so it cannot fire until a step from the next stage
  has already completed — the Seq 3/4 pair lands after Seq 5's work instead of before it, and
  sweeps both stages' files into one commit. The final stage has no successor step, so Seq 6/7
  never fire. Both symptoms have the same root cause.
- Infrastructure rows are flagged `IsInfrastructure=true` in the log and do not update
  `current_state`.
- No cascading: checkpoint's dispatch does not trigger a re-evaluation that fires commit, and
  vice versa. Both fire from the same workflow step completion.

---

## What a Failure Means Here

| Observation | Where to look |
|---|---|
| Run refuses to start with "no commit-class infrastructure agent" | Infrastructure agents were not deployed — check deploy tool output for silent infra agent skip (ToolingGaps.md GAP-1) |
| Run refuses with "checkpoints requested but no checkpoint provider" | Same as above — checkpoint agent missing from deployment |
| Seq 0 missing (no commit setup row) | The commit setup dispatch did not fire; check `session.go` doCommitSetupDispatch |
| Commit setup fires but `commit_branch` is empty | The `[branch:mosaictest-run]` marker was not extracted from the stub's response |
| Only one infra pair, firing after stage 2's step | **The known gap this fixture exposes.** `STAGE_END` still compares backwards against the previous step's stage. See `Issue-Runner-TerminalStageEndTrigger.md` |
| Two pairs, but both after stage 2's step | A terminal evaluation was added without fixing the timing — the last stage now fires, but the first commit still sweeps up both stages' files. Half a fix |
| No infra triggers at all | No stage boundary was observed at all — the trigger evaluator may not be running between stages, or `prevWorkflowStep` is not being carried across steps |
| Infra triggers fire but in wrong order (commit before checkpoint) | The InfrastructureAgents region is not in alphabetical order, or the evaluator iterates differently from the declaration order |
| Three or more infra triggers at one stage boundary | Cascading: an infrastructure completion is triggering a re-evaluation, violating the no-cascades rule |
| Orchestrator consulted after an infrastructure step | A deviation was raised on an infrastructure completion — infrastructure steps must not produce deviations |
| Row 3 shows `current_state` updated | Infrastructure rows must not update `current_state`; the engine is treating an infra step as a workflow step |

---

## Changelog

| Version | Date | Author | Summary |
|---------|------|--------|---------|
| 1.0 | 2026-09-20 | MOSAIC | Initial version. Checkpoint + commit triggers, commit setup dispatch. |
| 1.1 | 2026-09-22 | MOSAIC | Corrected the startup ordering: commit setup precedes pre-consultation (Design.md §4.2 step 7a). |
| 1.2 | 2026-09-22 | MOSAIC | Expected Run restored to eight dispatches with each infra pair straddling a stage boundary. Deliberately RED pending the STAGE_END timing fix (`Issue-Runner-TerminalStageEndTrigger.md`); the v1.1 six-dispatch expectation documented the bug rather than the requirement. |

---

## Open Ideas / Dead Ends

**Ideas under consideration:**
- (moved to `Issue-Runner-TerminalStageEndTrigger.md` — the STAGE_END timing fix is now committed work, and this fixture is RED until it lands.)
- A variant with `INVOCATION_INTERVAL(2)` on the checkpoint agent instead of `STAGE_END`, so both trigger types fire in one run.
- Adding a review-class agent to exercise all three infrastructure classes simultaneously.
- Testing the `on_failure: halt` path by making a stub return BLOCKED (requires changes to the infrastructure stubs).

**Dead ends (tried and rejected):**
- (none yet)
