---
run_id: "20260801T202027Z-ad3d"
created_by: "knowledge-base-generator#2"
---

# session

> Responsibility: The imperative shell that drives a complete orchestration run — from resolving all run-start inputs, through the dispatch loop that repeatedly asks the pure `engine` what happens next and carries out that decision, to a terminal outcome (completed, stopped, refused, or failed).

## Overview

`session` is the only package that performs I/O in service of running a workflow: it invokes the harness, reads/writes the artifact, resolves deviations, and reports progress. It exists to keep `engine` pure — `engine.Next` only *decides*, `session` *does*. Both frontends (`cli` and `tui`) call the same `Session.Start` entry point and never touch `engine`, `orchfile`, `workflow`, `compat`, `agentresolve`, or `planstages` directly; `session` owns the sequencing of all of those packages.

A run has two phases: a fixed-order **run-start sequence** that validates every input and produces (or resumes) the artifact, and a **dispatch loop** that repeatedly calls `engine.Next` and acts on whichever decision comes back until the run reaches a terminal state.

## Components / Subdomains

| Component | Purpose |
|-----------|---------|
| **Run-start sequence** | Loads and validates every input needed before the first dispatch: workflow region, routing table, existing artifact (if any), admission, agent resolution, stage set, declared infrastructure agents, per-class agent selection, and checkpoint availability. Produces a fresh or resumed artifact state. |
| **Dispatch loop** | The core `for` loop: calls `engine.Next`, then branches on which decision field is populated (Dispatch / Complete / Deviation / Stop) and performs the corresponding action. |
| **Deviation & rejoin handling** | Invokes the injected `DeviationResolver` when the engine cannot decide or when harness invocation fails, then applies the returned `RejoinInstruction` (stop / rejoin at a row / custom dispatch) to resume the loop. |
| **Infrastructure-agent trigger evaluation** | After each *workflow* step completes (never after infrastructure steps — "no-cascades"), checks every declared infrastructure agent's triggers and synchronously dispatches any that fire, applying per-class gating, activation, and failure policy. |
| **Selection & override validation** (`selection.go`) | Enforces at-most-one-active-agent-per-gated-class semantics (checkpoint, commit, restore) and applies `infrastructure_overrides` from the artifact to declared agents' trigger lists. |
| **Stage-* re-derivation** | Detects when a completed step's output artifacts reference the `Stage-*` wildcard pattern and re-reads `Plan.md` via `planstages` so the next `engine.Next` call sees a refreshed stage set. |

## Key Flows

### Run-start sequence

Executed once, in this fixed order, entirely inside `Start`:

1. Load the orchestrator file and select the requested workflow region (`orchfile`).
2. Parse the region's raw content into a routing table (`workflow`).
3. Read the existing artifact, if any. A non-canonical-format artifact is always a refusal, regardless of new-vs-resume. A missing artifact is expected for new runs and an error for resume.
4. Apply the new-vs-resume contract: new runs refuse if an artifact already exists (race guard); resumes refuse if none exists (stale scan guard). Resumes also refuse on workflow-version mismatch unless version drift is explicitly allowed.
5. Admit the workflow (`compat`) — validates the FR-18a subset and resolves execution groups.
6. Resolve every agent identifier referenced in the routing table to a definition file (`agentresolve`).
7. If the admitted workflow has a staged phase, read the stage set from `Plan.md` (`planstages`), passing `admitted.GroupsDeclared` to indicate whether the `Approach` column is required. When `GroupsDeclared` is true, every stage must carry a non-empty `Approach` value; when false, the column is not read even if present.
8. Enumerate declared infrastructure agents from the orchestrator file, then validate per-class agent selection (refusing non-interactive runs that have multiple agents in a gated class but no `--infra-class` selection).
9. Settle checkpoints: refuse only when checkpoints were requested for the run **and** no checkpoint-class infrastructure agent is declared.
10. Create a fresh artifact (new run) or determine the resume point via `engine.ResumePoint` (resume). A resume whose last step was interrupted mid-invocation (FR-33) has its `CurrentState` rewound so the interrupted row is re-dispatched rather than skipped.
11. Validate and apply any `infrastructure_overrides` recorded in the artifact state against the declared agents (unknown agent names refuse; disallowed trigger names per agent class refuse).

Any failure at steps 1–9 or step 11 returns a **refusal** outcome (`RunRefused`) with no error — refusals are expected, pre-invocation validation failures, not infrastructure faults. Infrastructure-level failures (e.g. artifact store I/O errors) return `RunFailed` with a non-nil error instead.

### Dispatch loop

Each iteration calls `engine.Next` with the admitted workflow, stage set, current artifact state, the previous response, resolved agents, the running sequence number, the current time, and any one-shot refreshed stage set. Exactly one of four decision fields comes back:

- **Dispatch** — Build the request for `Steps[0]` (only one step is ever populated today; the slice shape reserves room for future parallel dispatch): stamp `RunID` from artifact state (not from `RunConfig`, so resumed runs keep the RunID minted at creation), resolve artifact paths to run-scoped form, apply and clear any pending HITL override from a prior deviation rejoin, notify progress, then invoke the harness.
  - On invocation success: append a `CompletedStep` to the artifact via `Store.Apply`, notify per-step completion, then — only for non-infrastructure steps — evaluate infrastructure-agent triggers (see below) and check for `Stage-*` re-derivation.
  - On invocation failure (that isn't context cancellation): treated as a deviation of kind `DeviationHarnessError`, never a run crash — the deviation resolver decides whether to rejoin, dispatch a custom agent, or stop.
  - On context cancellation: returns `RunStopped` immediately (graceful stop).
- **Complete** — Returns `RunCompleted`.
- **Deviation** — Invokes the deviation resolver with `decision.Deviation.Info`, then applies the returned rejoin instruction.
- **Stop** — Returns `RunStopped` with the engine's stop reason.
- No field populated — `RunFailed` (should not occur; a defensive fallback).

### Deviation resolution and rejoin

Whenever the deviation resolver is invoked (either because the engine returned a Deviation decision, or because a harness invocation errored), `applyRejoinInstruction` does the following:

1. Re-reads the artifact from disk (FR-23) — the orchestrator delegate strategy may have updated it out-of-band while resolving.
2. Branches on the returned `RejoinInstruction`:
   - **Stop** — terminates the run with `RunDeviationUnresolved`.
   - **Rejoin** — carries an optional HITL override forward to the next dispatch (applied once, then cleared) and repositions `CurrentState` to the target row via `applyRejoinAtRow`, clearing `lastResponse` so the engine doesn't misinterpret stale data.
   - **Custom** — performs a one-off harness invocation for an agent outside the routing table (refusing if no agent identifier is supplied — a schema gap), then rejoins at the specified row the same way.
   - Empty instruction — `RunDeviationUnresolved`.

`applyRejoinAtRow` repositions the artifact's `CurrentState` so `engine.Next` dispatches from an arbitrary target row: row 0 clears `CurrentState` entirely (restart); row N>0 searches the execution log backward for the last entry produced by row N-1's agent and reconstructs `CurrentState` from it, or clears it if no such entry exists. This is what lets a deviation resolver direct arbitrary row jumps, not just "retry the last row."

### Infrastructure-agent trigger evaluation

Runs once per *workflow* step completion (never after an infrastructure step completes — the "no-cascades" rule), after the workflow step's own `Store.Apply` has already happened:

1. `state.CurrentState` is saved before evaluation and restored afterward, because infrastructure-agent dispatches also call `Store.Apply`, which would otherwise leave `CurrentState` pointing at the infra agent instead of the workflow step the engine needs to route from next.
2. For each declared infrastructure agent, in declaration order:
   - `restore`-class agents are always skipped — they only ever act on an explicit `MANUAL` trigger via out-of-band instruction, never automatically.
   - Agents excluded by the active-agents filter (per-class selection) are skipped.
   - `checkpoint`-class agents are skipped entirely when the run did not request checkpoints.
   - The agent's declared triggers are checked (`INVOCATION_INTERVAL` — fires on sequence-number interval arithmetic against the agent's last dispatch; `STAGE_END` / `PHASE_END` — fire when the completed step's stage/phase differs from the previous workflow step's; `MANUAL` — never fires automatically). An agent fires at most once per evaluation pass even if multiple triggers match.
   - A firing agent is dispatched synchronously (its invocation, including the resulting Execution Log row, completes before the next agent's triggers are evaluated). Checkpoint-class responses have a `[checkpoint:{sha}]` marker extracted from `status_message` and recorded on the log entry.
   - Non-`SUCCESS` outcomes apply the agent's `on_failure` policy: `halt` stops the run immediately (`haltRun=true`); any other policy records the failure and continues.
3. After all agents are evaluated for this workflow step, the named no-op hook `onInfrastructureAgentTrigger` is called (a discoverable anchor point for FR-40), followed by the test-injected `Deps.OnInfrastructureTrigger` hook if set.

### Per-class agent selection (`selection.go`)

Three infrastructure agent classes — `checkpoint`, `commit`, `restore` — are "gated": at most one agent of each may be *active* for a given run. If a gated class has more than one declared agent, run start requires an explicit `--infra-class {class}={agent}` selection (`validateClassSelections`), otherwise the run refuses before dispatch begins. `buildActiveAgentsFilter` turns the resolved selections into a `map[string]bool` of active agent names (`nil` when no gated class has more than one agent, meaning "no filtering needed"); non-gated classes (e.g. `review`) are always active. `commit`-class agents are further restricted to only the `STAGE_END` trigger, whether declared directly or via an `infrastructure_overrides` replacement (`allowedTriggersForClass`).

### Stage-* output re-derivation

After a completed *workflow* step's output artifacts include a `Stage-*` wildcard, `Plan.md` is re-read via `planstages` and the resulting stage set is stashed as `refreshedStages`, consumed exactly once by the next `engine.Next` call (then cleared). A re-read failure is logged as a warning notice and does not fail the run — the engine simply proceeds with the stage set it already had.

### Graceful stop

Context cancellation is checked at two points: immediately after a harness invocation error, and (implicitly, via the same helper pattern) during trigger evaluation and custom dispatch. In every case a cancelled context produces `RunStopped`, distinct from `RunFailed`, so callers can distinguish "the run was asked to stop" from "the run broke."

## Relationships

| Talks To | For |
|----------|-----|
| **domain** | All port interfaces (`HarnessAdapter`, `ArtifactStore`, `DeviationResolver`, `Clock`, `Interaction`) and every shared value type; session imports domain but constructs none of the concrete implementations (that's `cmd/mosaic-run`'s job). |
| **engine** | The single source of "what happens next" (`Next`) and resume-point calculation (`ResumePoint`); session never re-implements routing logic. |
| **orchfile** | Loading the workflow region and enumerating declared infrastructure agents from the orchestrator file. |
| **workflow** | Parsing the selected region into a routing table. |
| **compat** | Admitting the routing table before the dispatch loop can begin. |
| **agentresolve** | Resolving every agent identifier in the routing table to a definition file. |
| **planstages** | Reading (and re-reading, on Stage-* output) the stage set from `Plan.md`. |
| **cli / tui** | Both frontends drive `Session.Start` as their sole entry point into run execution; session has no knowledge of either. |
| **mosaic-common/interaction** | The `Notice` type used for progress reporting through the `Interaction` port. |

## Key Concepts

| Concept | Meaning |
|---------|---------|
| **Refusal vs. Failure** | A refusal (`RunRefused`) is an expected, pre-invocation validation rejection (bad input, mismatched state) returned with a nil error. A failure (`RunFailed`) is an unexpected infrastructure fault, returned with a non-nil error. Session is careful to route each condition to the correct outcome. |
| **CurrentState** | The artifact's pointer to "where the engine should route from next" — a (phase, stage, last-agent, last-status) tuple. Every state-repositioning operation in session (rejoin, rewind-for-rerun, infra-trigger save/restore) exists to keep this pointer accurate for the engine's next call. |
| **No-cascades rule** | Infrastructure agent completions never themselves trigger further infrastructure-agent evaluation — only workflow step completions do. This bounds the trigger evaluation to one pass per workflow step. |
| **HITL override** | A one-shot human-in-the-loop flag carried from a deviation rejoin instruction to exactly the next dispatch's request; session never originates this value itself, only carries it. |
| **RunID scoping** | `RunID` for a dispatch always comes from the artifact state (set at creation), not from `RunConfig`, so resumed runs never drift to a caller-supplied value; artifact paths are prefixed with the run-scoped folder derived from that same RunID. |

## Boundaries

- **Owns:** the full run lifecycle sequencing (start validation, dispatch loop, deviation/rejoin handling, infrastructure-agent trigger evaluation, stage re-derivation, graceful stop) and all I/O performed in service of a run.
- **Does Not Own:** deciding what happens next given a state (that's `engine`), parsing/validating any individual input format (`orchfile`, `workflow`, `compat`, `agentresolve`, `planstages` each own their own domain), or the concrete mechanics of invoking a harness / persisting an artifact / resolving a deviation (those are behind ports and implemented in leaf packages).

## Invariants & Conventions

- Every port dependency in `Deps` is an interface; `sessionImpl` holds no concrete adapter types.
- `CurrentState` is always restored after infrastructure-agent trigger evaluation so the engine's next call sees the workflow step's position, not the last-dispatched infra agent's.
- A harness invocation failure is always routed through the deviation resolver, never returned directly as `RunFailed` — per the `HarnessAdapter` port contract ("never a crash").
- `hitlOverride` is applied to exactly one dispatch request and cleared immediately after, whether or not that dispatch succeeds.
- Infrastructure agent dispatch within `evaluateTriggers` is strictly synchronous and sequential in declaration order — no agent's triggers are evaluated until the previous firing agent's invocation and artifact write have completed.
- `restore`-class infrastructure agents are excluded from automatic trigger evaluation unconditionally, by class, so future restore-class agents need no code change to inherit the exclusion.

## Known Complexity

None identified beyond what this document already covers — the run-start sequence and dispatch loop are now documented at the depth needed to navigate them. Should the deviation-resolution strategies (`OrchestratorDelegate` / `ManualResolver`) themselves prove to have non-obvious internal branching, that would be a candidate for a dedicated `deviation` package document, but that is outside this package's scope.
