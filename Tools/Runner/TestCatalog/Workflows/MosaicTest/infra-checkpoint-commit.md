---
version: "1.2"
name: "MosaicTest Infrastructure Checkpoint+Commit Workflow"
description: "Runner mode fixture — Auto mode with checkpoint and commit infrastructure agents. Commit setup dispatch at run start, STAGE_END firing for both classes at each stage boundary. Proves the trigger-and-marker path works end to end through a real harness. Also guards the STAGE_END timing fix: each infra pair must fire between stages, including after the final stage."
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
- No deviations occur, so the orchestrator is consulted only for pre-consultation. The routing fixture declares `Pre-Consultation: none` and no rules.

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

### Why a routing fixture with no rules

Auto mode with all-SUCCESS workflow steps needs no orchestrator consultation except the pre-consultation. The pre-consultation still needs a fixture: the stub returns `{}` only when it reads `Pre-Consultation: none`. With no fixture at all, the stub's only correct answer is a "fixture not found" stop. Whether the model writes that stop as JSON or as prose varies from run to run, so the run passed or failed at random (exit 7, "reply contains no JSON object").

The fixture declares no rules. If the orchestrator is consulted unexpectedly (e.g. after an infrastructure step, which would be a bug), no rule matches and the stub stops, naming the state it saw. The bug stays visible.

---

## Expected Run

Seven Orchestration.md log rows plus one pre-run consultation. Infrastructure agents fire in
catalog order (alphabetical by key): checkpoint before commit. Pre-consultation dispatches to the
orchestrator but allocates no `Seq` and leaves no row — it surfaces only in the dispatch log.

| Log `Seq` | `Agent` | Kind | `Phase` | `Status` | `Summary` shows |
|:---:|---|---|---|---|---|
| 1 | `mosaictest-commit#1` | commit setup | -- | SUCCESS | `[branch:mosaictest-run]` marker |
| 2 | `mosaictest-scripted#2` | workflow step | EXECUTION.1 | SUCCESS | stage 1 row, wrote Stage-1/MosaicTestStage.md |
| 3 | `mosaictest-checkpoint#3` | infra trigger | -- | SUCCESS | STAGE_END at end of stage 1, `[checkpoint:...]` marker |
| 4 | `mosaictest-commit#4` | infra trigger | -- | SUCCESS | STAGE_END at end of stage 1, `[branch:mosaictest-run]` marker |
| 5 | `mosaictest-scripted#5` | workflow step | EXECUTION.2 | SUCCESS | stage 2 row, wrote Stage-2/MosaicTestStage.md |
| 6 | `mosaictest-checkpoint#6` | infra trigger | -- | SUCCESS | STAGE_END at end of stage 2, `[checkpoint:...]` marker |
| 7 | `mosaictest-commit#7` | infra trigger | -- | SUCCESS | STAGE_END at end of stage 2, `[branch:mosaictest-run]` marker |

**Run outcome:** COMPLETE. All workflow steps succeed, both STAGE_END boundaries fire.

**Key observations:**
- Seq 1 is the commit setup dispatch — before any workflow step *and* before pre-consultation.
  Per Design.md §4.2 the setup dispatch is run-start step 7a, while pre-consultation follows
  artifact creation (step 8) because its request carries the artifact path. This ordering is
  is separate from `STAGE_END` timing. The artifact's `commit_branch` field
  should read `mosaictest-run`. Pre-consultation itself allocates no `Seq` and leaves no row, so
  the first workflow step is `mosaictest-scripted#2`, immediately following the commit setup row.
- **Each infra pair straddles a stage boundary.** Seq 3/4 fire after stage 1's step and before
  stage 2's step; Seq 6/7 fire after stage 2's step. This is what makes per-stage commits
  meaningful: the commit at Seq 4 sees only stage 1's files, and the commit at Seq 7 sees only
  stage 2's.
- **Regression guard for the `STAGE_END` timing fix.** `STAGE_END` used to be evaluated by
  comparing the completed step's stage against the *previous* step's stage, so it could not fire
  until a step from the next stage had already completed. The Seq 3/4 pair landed after Seq 5's
  work instead of before it and swept both stages' files into one commit, and because the final
  stage has no successor step, Seq 6/7 never fired. The fix evaluates `STAGE_END` when the last
  step of a stage returns HITL-accepted `SUCCESS` (Runner `Design.md` §5). If either symptom
  comes back, the fix has regressed.
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
| Seq 1 missing (no commit setup row) | The commit setup dispatch did not fire; check `session.go` doCommitSetupDispatch |
| An `orchestrator-script` row appears in `Orchestration.md` | Consultations must leave no row in the artifact; pre-consultation wrongly called `Store.Apply` or allocated a `Seq` |
| Commit setup fires but `commit_branch` is empty | The `[branch:mosaictest-run]` marker was not extracted from the stub's response |
| Only one infra pair, firing after stage 2's step | The `STAGE_END` timing fix has regressed: the trigger again compares backwards against the previous step's stage, so it fires late and never for the final stage |
| Two pairs, but both after stage 2's step | Partial regression: the final stage still fires, but the stage 1 trigger is late again, so the first commit sweeps up both stages' files |
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
| 1.3 | 2026-10-04 | MOSAIC | Added a rule-less `MosaicTestRouting.md` (`Pre-Consultation: none`). Without it the pre-consultation answer depended on how the stub happened to phrase "fixture not found", which made the run flaky. The `STAGE_END` timing fix has landed: the "RED on purpose" notes are replaced by regression notes, and the Expected Run is unchanged. |

---

## Open Ideas / Dead Ends

**Ideas under consideration:**
- A variant with `INVOCATION_INTERVAL(2)` on the checkpoint agent instead of `STAGE_END`, so both trigger types fire in one run.
- Adding a review-class agent to exercise all three infrastructure classes simultaneously.
- Testing the `on_failure: halt` path by making a stub return BLOCKED (requires changes to the infrastructure stubs).

**Dead ends (tried and rejected):**
- (none yet)
