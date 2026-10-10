---
run_id: "20260801T202027Z-ad3d"
created_by: "knowledge-base-generator#2"
last_updated: "2026-10-08"
---

# session

> Responsibility: The imperative shell that drives a complete orchestration run — from resolving all run-start inputs, through the dispatch loop that repeatedly asks the pure `engine` what happens next and carries out that decision (dispatching, gating on human approval, consulting the routing consultant), to a terminal outcome (completed, stopped, refused, start-failed, or failed).

## Overview

`session` is the only package that performs I/O in service of running a workflow: it invokes the harness, reads/writes the artifact, consults the routing consultant, and reports progress. It exists to keep `engine` pure — `engine.Next` only *decides*, `session` *does*. Both frontends (`cli` and `tui`) call the same `Session.Start` entry point and never touch `engine`, `orchfile`, `workflow`, `compat`, `agentresolve`, or `planstages` directly; `session` owns the sequencing of all of those packages.

A run has two phases: a fixed-order **run-start sequence** that validates every input and produces (or resumes) the artifact, and a **dispatch loop** that repeatedly calls `engine.Next` and acts on whichever decision comes back until the run reaches a terminal state. The run's execution mode (`orchestrated`, `auto`, `auto-review`) is required, immutable, persisted in the artifact, and passed to every `engine.Next` call.

## Components / Subdomains

| Component | Purpose |
|-----------|---------|
| **Run-start sequence** (`start_prepare.go`, `start_harness.go`) | Loads and validates every input needed before the first dispatch and produces a fresh or resumed artifact state. |
| **Dispatch loop** (`dispatch_loop.go`) | The core `for` loop: calls `engine.Next`, then branches on which decision field is populated (Dispatch / Complete / Consult / Deviation / Stop) and performs the corresponding action. |
| **HITL gate** (`dispatch_hitl.go`, `consult_route_hitl.go`, `written_outputs.go`) | Verifies `human_approved` on the outputs an attempt actually wrote and drives the single re-dispatch or the escalation. |
| **Routing consultation** (`consult_route.go`, `consult_target.go`, `consult_defaults.go`, `consult_manual.go`) | One cycle that asks the consultant (or the manual resolver) where to go next, validates the answer, and dispatches the chosen row. |
| **Mechanical-retry integration** (`retry_dispatch.go`) | Connects harness errors and the raw-text bypass to the engine's E501 budget. |
| **Anti-loop guard** (`antiloop.go`, `antiloop_escalation.go`) | Bounds consultant-requested repeat dispatches. |
| **Graceful stop** (`stopsignal.go`, `stopcheckpoints.go`, `consult_stop.go`) | The stop flag, the six places it is observed, and the visible discard of a routing decision completed after a stop request. |
| **Infrastructure-agent trigger evaluation** (`triggers.go`) | After each *workflow* step, checks every declared infrastructure agent's triggers and synchronously dispatches those that fire. |
| **Selection & override validation** (`selection.go`, `deps_policy.go`) | At-most-one-active-agent-per-gated-class, `infrastructure_overrides`, and the dependency policy that refuses a run needing an unwired port. |
| **Stage-* re-derivation** | Re-reads `Plan.md` via `planstages` when a completed step's outputs reference the `Stage-*` wildcard. |

## Key Flows

### Run-start sequence

Executed once, in this fixed order, inside `Start`:

0. Refuse when an always-required port (Harness, Store, Clock, Interact) is nil (`Deps.CheckRequired`).
1. Load the orchestrator file and select the requested workflow region (`orchfile`); a resume whose workflow the file no longer declares is a refusal naming both the workflow and the run.
2. Parse the region into a routing table (`workflow`); resolve the orchestrator agent; bind the run context (orchestrator, table) to the routing consultant, manual resolver and pre-consultant; refuse when the workflow has human-review rows and the approval reader cannot read approvals.
3. Read the existing artifact. A non-canonical artifact is always a refusal (the underlying parse error stays the refusal's cause). New runs refuse if an artifact exists; resumes refuse if none exists, if the artifact's `run_id` does not match its folder, or on workflow-version mismatch unless drift is allowed.
4. Admit the workflow (`compat.Admit(table, mode)`); recovery check (CLI harnesses only): `snapshot.RecoveryCheck` restores originals from an orphaned `.agents-backup/` if no run is active, and refuses on a corrupt manifest.
5. Resolve every agent identifier to a definition file (`agentresolve`).
5b. Snapshot / backup-and-transform (CLI harnesses only): for `LoadingMechanismPath` (Claude Code) copy agents to a run-scoped `agents-runner-{run_id}/` directory and transform the copies; for `LoadingMechanismName` (OpenCode, GHCP CLI) back originals up to `.agents-backup/`, transform in place and write a recovery marker. Cleanup is deferred to run exit.
6. On resume, read the stage set from `Plan.md` (`planstages`, with `admitted.GroupsDeclared` deciding whether the `Approach` column is required); enumerate declared infrastructure agents and apply the infrastructure filter; on resume settle the run settings against the artifact (`domain.ReconcileResumeSettings`: supplied values are compared with recorded ones); validate per-class agent selection; refuse checkpoints requested with no checkpoint-class agent; refuse an unset mode; refuse commits enabled with no commit-class agent; refuse when the effective settings need a port that is not wired (`Deps.CheckForSettings`: PreConsult for pre-consultation in auto modes, Manual for manual resolution); record adopted runner settings once for a native-created artifact.
7. Create the artifact (new run; apply seed inputs, then read the stage set) or take the existing one (resume). Commit setup runs here when commits are enabled and no `commit_branch` is recorded: an ordinary out-of-band invocation that always gets an Execution Log row without moving `current_state`; only SUCCESS with a `[branch:{name}]` marker records the branch. On resume `engine.ResumePoint` finds the resume point, and an interrupted last step has `current_state` rewound to the last completed workflow step so it is re-dispatched.
8. Validate and apply `infrastructure_overrides` against the declared agents; in `auto` and `auto-review` modes with pre-consultation enabled, run the one-shot pre-consultation (it never touches the artifact; its advice is appended to later auto-routed task descriptions and constraints).

Steps 0 through 6 and the overrides step refuse with `RunRefused` and no error — refusals are expected, pre-invocation rejections. A failed commit setup or pre-consultation, after the artifact exists, ends with `RunStartFailed`: the artifact and any setup row are kept, nothing is dispatched, and a resume retries the step. Infrastructure faults (artifact store I/O) return `RunFailed` with a non-nil error.

### Dispatch loop

Each iteration calls `engine.Next` with the admitted workflow, stages, state, previous response, resolved agents, sequence number, time, one-shot refreshed stage set, stage source, the run mode and the output artifacts of the previous step (`rs.lastOutputArtifacts`; a consultation-routed step hands its outputs over through `consultOutputs`). Exactly one decision field comes back:

- **Dispatch** (`handleEngineDispatch`) — Fill in the request (generic task description plus any pre-consultation advice, `RunID` from artifact state, run-scoped artifact paths), count the dispatch for the anti-loop guard (mechanical retries are not counted), observe the stop flag, record the output baseline, invoke the harness, and run the HITL gate. The accepted result is applied through `Store.Apply` (written outputs registered, status and error code recorded on the log row), progress is reported, infrastructure triggers are evaluated (workflow steps only) and `Stage-*` outputs re-derive the stage set.
- **Complete** — `RunCompleted`.
- **Consult** — orchestrated mode: `consultRoute` with no deviation info.
- **Deviation** — `consultRoute` with the engine's `DeviationInfo`. With no routing consultant wired (and no pending manual dispatch) the outcome is `RunDeviationUnresolved`.
- **Stop** — `RunStopped` with the engine's stop reason (a position-resolution error is carried typed in the decision).

### Harness errors and mechanical retries

A harness-level error on an auto-routed dispatch (other than context cancellation) is never a run crash. It is recorded as a BLOCKED row with error code E501 under the failed instance; with no routing consultant wired the outcome is then `RunDeviationUnresolved`, otherwise:

1. If the error is a raw-text protocol failure (output received but no protocol JSON extractable) and the bypass is permitted, one direct re-dispatch under a fresh instance runs. In `auto` and `auto-review` the bypass is one attempt of the E501 budget (`engine.E501BudgetRemaining`, 3 attempts per row and stage since the last SUCCESS), so the budget owns the bound; in `orchestrated` the anti-loop guard owns it. A failed bypass is recorded as its own BLOCKED/E501 row.
2. If the budget still has attempts left in `auto` or `auto-review`, the loop continues and `engine.Next` re-dispatches the same row and stage.
3. Otherwise the session consults (`consultRoute`) with the harness response as the deviation.

PARTIALLY_DONE re-dispatches (bounded at 3 by the engine) need no session logic beyond the dispatch itself.

### HITL gate

For a dispatch whose effective HITL is on, after every attempt (whatever its status) the session reads `human_approved` on the declared outputs that attempt created or modified — found by the `OutputWriteDetector` against a baseline taken before the first attempt and reused for the re-dispatch; without a wired detector every declared concrete path counts (`Stage-*` expanded through the stage set), so the gate fails closed. `domain.DecideHITLCompliance` decides: accept; re-dispatch the same agent once (same task, constraints and artifacts); or escalate. The rejected attempt is logged as an infrastructure-flagged row (HITL-rejected) that does not move `current_state`. Escalation after the spent re-dispatch is a routing consultation as a deviation. BLOCKED/E503 (the agent could not reach the user) is accepted without a re-dispatch. When a gate-discharging re-dispatch returns SUCCESS, its row records the re-dispatch's agent instance, but `current_state`, routing and STAGE_END/PHASE_END evaluation follow the original attempt's status and error code (the `Routed` outcome). Only written outputs are registered in the Artifacts table. The same gate runs for consultation-routed dispatches.

### Routing consultation (`consultRoute`)

One cycle for every routing choice the engine hands over (orchestrated Consult, deviations, HITL escalations, harness errors, review-class triggers, guard escalations):

1. If a stop was requested, stop before starting; the consultation writes nothing, so a resume derives the same decision again.
2. Build the `ConsultationRequest`: the artifact path, the last status message and error reason, plus session-only context that never reaches the wire — the deviation, the current stage set, the Artifacts registry in recorded (unprefixed) form, and a row-defaults function bound to the stage sets in force.
3. Pick the resolver: the one-shot manual resolver when the stop screen requested a manual dispatch; otherwise `Routing.ConsultRouting`, and — when manual resolution is enabled — the manual resolver as fallback if that consultation fails. A consultation error ends the run with `RunStoppedByConsultant` (resumable; for user cancel, unavailable interaction or exceeded manual bound the stop reason names the case). A `stop` instruction also ends with `RunStoppedByConsultant`.
4. Validate the dispatch target against the routing table and stage set (`domain.ValidateDispatchTarget`: row, agent, stage rules, plus the check that the row's group is part of the stage's approach). An invalid target ends the run unrecorded.
5. If a stop was requested during the consultation, discard the decision visibly (warning notice, debug event, outcome message naming it).
6. Re-read the artifact (so Workflow Notes the orchestrator appended are preserved) and follow its `global_sequence`. The consultation itself writes no Execution Log row, no sequence number and no `current_state` change.
7. Resolve the payload: artifacts and HITL default to the row defaults the engine would use; explicit instruction fields pass through verbatim.
8. Anti-loop guard, dispatch, harness-error handling, HITL gate and apply, the same as for an engine dispatch.

**Anti-loop guard:** the same agent at the same row may be dispatched at most 4 consecutive times (`maxConsecutiveSameAgentDispatches`); the fifth is blocked and escalated to the consultant as a deviation. If the consultant keeps requesting the blocked dispatch, after 2 consecutive guard escalations the run stops resumably. Engine dispatches are counted but never blocked.

### Infrastructure-agent trigger evaluation

Runs once per *workflow* step completion after its `Store.Apply`, and only for HITL-accepted steps; never after an infrastructure step (the "no-cascades" rule):

1. For each declared agent in declaration order: `restore`-class agents are always skipped (they act only on an explicit manual instruction); agents outside the active-agents filter are skipped; `checkpoint`-class agents are skipped unless the run enabled checkpoints.
2. Triggers: `INVOCATION_INTERVAL` fires on sequence arithmetic against the agent's last log row; `STAGE_END` fires when the completed step is the last row of its stage and `PHASE_END` when it is the last row of its phase (look-ahead via `engine.IsLastRowOfStage` / `IsLastRowOfPhase`), both only when the step's routed status is SUCCESS and it passed the HITL gate; `MANUAL` never fires automatically. An agent fires at most once per pass.
3. A firing agent is dispatched synchronously and recorded as an infrastructure row (`current_state` is not moved by infrastructure rows). Checkpoint responses have their content reference extracted onto the log entry. Non-SUCCESS applies the agent's `on_failure` policy: `halt` stops the run, otherwise the failure is recorded and evaluation continues. A graceful stop is checked before each infrastructure dispatch.
4. A successful `review`-class agent makes the session perform a routing consultation after the pass, supplying the review's status message as the last status message (when a consultant is wired).
5. The named no-op hook `onInfrastructureAgentTrigger` is called after the pass, then the test-injected `Deps.OnInfrastructureTrigger`.

### Per-class agent selection (`selection.go`)

Three infrastructure classes — `checkpoint`, `commit`, `restore` — are "gated": at most one agent of each is active. If a gated class has more than one declared agent, run start requires an explicit selection (`--infra-class {class}={agent}`, persisted as `infrastructure_selections`), otherwise it refuses. `buildActiveAgentsFilter` returns the active names (`nil` when no filtering is needed); non-gated classes (e.g. `review`) are always active. `commit`-class agents are restricted to the `STAGE_END` trigger, declared directly or via an override.

### Stage-* output re-derivation

After a completed workflow step's outputs include a `Stage-*` wildcard (and, before the gate, for a self-referential row when no stage set exists yet), `Plan.md` is re-read via `planstages` and the result becomes both the session's stage set and the one-shot `refreshedStages`. A re-read failure is a warning notice and does not fail the run.

### Graceful stop

`Deps.StopRequested` (backed by `StopSignal`: `Request`, `Reset`, `Requested`, safe across goroutines) is polled only at six checkpoints, never mid-invocation: `engine.step`, `engine.hitl_redispatch`, `consult.entry`, `consult.dispatch`, `consult.hitl_redispatch`, `infra.dispatch`. Each observation is logged with its checkpoint name and produces `RunStopped`. Context cancellation is the separate hard-cancel path and also yields `RunStopped`, distinct from `RunFailed`.

## Relationships

| Talks To | For |
|----------|-----|
| **domain** | All ports (`HarnessAdapter`, `ArtifactStore`, `Clock`, `Interaction`, `RoutingConsultant`, `PreConsultant`, `ApprovalReader`, `OutputWriteDetector`) and every shared value type; session imports domain but constructs none of the concrete implementations (that's `cmd/mosaic-run`'s job). |
| **engine** | `Next`, `ResumePoint`, `ResolveRowDefaults`, `E501BudgetRemaining`, the last-row helpers; session never re-implements routing logic. |
| **orchfile / workflow / compat / agentresolve / planstages** | Loading and parsing the workflow, admitting it, resolving agents, reading and re-reading the stage set. |
| **deviation** (via ports) | The routing consultant and manual resolver sit behind `RoutingConsultant`; session never imports them. |
| **cli / tui** | Both frontends drive `Session.Start` as their sole entry point; session has no knowledge of either. |
| **mosaic-common/interaction** | The `Notice` type for progress reporting and the `Interaction` port. |

## Key Concepts

| Concept | Meaning |
|---------|---------|
| **Refusal vs. Start-failure vs. Failure** | `RunRefused`: expected pre-invocation rejection, nil error. `RunStartFailed`: commit setup or pre-consultation failed after the artifact exists; resumable. `RunFailed`: unexpected infrastructure fault, non-nil error. |
| **RunStoppedByConsultant** | The consultant stopped the run, a consultation failed, or its dispatch target was invalid. The artifact stays resumable; the outcome carries `StopReason` and `Cause`. |
| **CurrentState** | The artifact's pointer to the last completed workflow step. Only workflow steps move it; infrastructure rows (triggers, commit setup, HITL-rejected attempts) never do. |
| **Routed outcome** | When a gate-discharging re-dispatch succeeds, routing follows the original attempt's status and error code. |
| **RunID scoping** | `RunID` for a dispatch comes from the artifact state, so resumed runs keep the ID minted at creation; artifact paths are prefixed with the run-scoped folder derived from it. |

## Boundaries

- **Owns:** run lifecycle sequencing, the dispatch loop, the HITL gate, the routing-consultation cycle, harness-error and bypass handling, the anti-loop guard, infrastructure-agent trigger evaluation, stage re-derivation, graceful stop, and all I/O performed in service of a run.
- **Does Not Own:** deciding what happens next given a state (`engine`), parsing any individual input format, the mechanics of invoking a harness, persisting an artifact, or producing a consultation answer (leaf packages behind ports).

## Invariants & Conventions

- Every port dependency in `Deps` is an interface; `sessionImpl` holds no concrete adapter types. Optional ports are normalised in `New`.
- A consultation never writes to the artifact; the dispatch it triggers is recorded like any other.
- A harness invocation failure is always recorded and routed (bounded retry or consultation), never returned as `RunFailed`.
- A one-shot HITL override or manual dispatch is consumed by exactly one dispatch.
- Infrastructure dispatch is strictly synchronous and sequential in declaration order.
- `restore`-class infrastructure agents are excluded from automatic trigger evaluation unconditionally, by class.

## Known Complexity

None beyond what this document covers; the consultation cycle and the HITL gate are the densest paths and are described step by step above.
