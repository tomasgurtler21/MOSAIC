---
run_id: "20260801T202027Z-ad3d"
created_by: "knowledge-base-generator#1"
---

# mosaic-run (MOSAIC Script-Driven Orchestration Runner)

> Purpose: A Go CLI/TUI tool that drives a MOSAIC multi-agent orchestration run to completion — reading a workflow definition and an in-progress orchestration artifact, deciding which subagent to dispatch next, invoking it through a harness, and recording the outcome — without requiring an LLM orchestrator agent to manually run the loop.

## Areas / Domains

| Area | Responsibility | Key Relationships |
|------|---------------|-------------------|
| **domain** | Defines all value types, enums, and port interfaces (`HarnessAdapter`, `ArtifactStore`, `DeviationResolver`, `Clock`) shared by every other package. Zero dependency on other internal packages. | Depended on by everything; depends on nothing internal. |
| **orchfile** | Reads an orchestrator agent file and enumerates its embedded `[[SECTION:Workflow:{id}]]` regions (workflow definitions), each carrying an id, version, and raw content. | Feeds raw workflow content to **workflow**. |
| **workflow** | Parses a workflow region's raw markdown into a flat, ordered, index-identified routing table (`RoutingTable`). | Consumes **orchfile** output; feeds **compat**. |
| [**compat**](./compat/Index.md) | Validates that a parsed routing table is within the supported workflow subset ("admission") and resolves its EXECUTION rows into contiguous named execution groups partitioned by the Phase column's group segment. | Consumes **workflow** output; feeds **engine** (`AdmittedWorkflow`). |
| **planstages** | Reads the stage table from a Plan.md artifact and returns a validated, ordered `StageSet` (consecutive numbering from 1, no forward dependencies, `Approach` column required when the workflow declares execution groups and ignored for bare workflows). | Feeds **engine**; re-invoked mid-run by **session** when stage output is re-derived. |
| **agentresolve** | Resolves every agent identifier referenced in a workflow to its agent definition file in the directory containing the orchestrator file, by exact filename-stem match. | Feeds **engine** (agent references) and **session**. |
| **artifact** | Implements the `ArtifactStore` port: reads, constructs, and atomically (write-temp-then-rename) rewrites `Orchestration.md` in the canonical format. Also provides a narrow `SetPhase` path for writing the final COMPLETED marker. | Read/written by **session**; format understood by **engine** indirectly via `ArtifactState`. |
| [**engine**](./engine/Index.md) | The pure, side-effect-free decision core. Given the admitted workflow, stage set, and current artifact state, `Next` answers "what happens next": Dispatch, Complete, Deviation, or Stop. No I/O, no clock reads except via injected `time.Time`. | Consumed exclusively by **session**; imports only **domain**. |
| [**session**](./session/Index.md) | The use-case loop connecting the pure engine to the outside world. Drives the full run lifecycle: run-start sequence (load orchfile → parse workflow → read/create artifact → admit via compat → resolve agents → read stages → settle checkpoint) then the dispatch loop (ask engine → dispatch via harness → apply to artifact → repeat), including deviation handling, stage re-derivation, and graceful stop. | Orchestrates nearly every other package; driven by **cli** and **tui**. |
| **harness** | Implements the `HarnessAdapter` port. Includes a `FakeHarness` for tests (scripted responses) and `ClaudeCodeAdapter`, which spawns the Claude Code CLI as a subprocess per invocation and parses its `--output-format json` envelope into a Communication Protocol response. | Invoked only by **session**, always through the `domain.HarnessAdapter` port (import-boundary enforced). |
| **deviation** | Implements the `DeviationResolver` port with two strategies: `OrchestratorDelegate` (delegates to a script-mode orchestrator agent via the harness, parsing a JSON `RejoinInstruction` from its `result_data`) and `ManualResolver` (drives the `Interaction` port for user input). Never originates a HITL override on its own. | Invoked by **session** when the engine returns a Deviation decision. |
| **runscan** | Scans the working directory for `Orchestration-{run_id}/` folders and classifies each as resumable or completed, for run-selection UX. | Used by **tui** (and CLI run-selection) at startup. |
| **cli** | The non-interactive frontend. Resolves all setup questions from flags, writes per-step progress to stdout, and defines the tool's exit codes. | Drives **session**; never imports **tui**. |
| **tui** (+ **tui/screens**) | The interactive frontend, built on Bubble Tea, sharing theme/keys/scaffold from the shared `mosaic-common/tui` library. Collects run setup interactively, starts the session in a background goroutine, shows live progress, handles deviation resolution, and lets the user inspect the artifact. | Drives **session**; never imports **cli**. |
| **cmd/mosaic-run** | The single binary entry point. Decides CLI vs. TUI mode (`--tui` flag, explicit subcommand, or TTY detection), pre-scans flags needed for dependency wiring, resolves run identity, and wires all infrastructure (harness, artifact store, deviation resolver, clock) before handing off to the chosen frontend. | Constructs and injects concrete implementations of every domain port; the only place that does so. |
| **tools/importcheck** | A standalone static-analysis command that enforces the module's import-boundary rules (see System-Wide Patterns) by scanning non-test source files. Run via `task check:imports` as part of `build-checked`. | Governs, but is not depended on by, the rest of the module. |

## System-Wide Patterns

- **Ports and adapters (hexagonal architecture):** `domain` defines interfaces (`HarnessAdapter`, `ArtifactStore`, `DeviationResolver`, `Clock`); concrete implementations live in leaf packages (`harness`, `artifact`, `deviation`) and are wired together only in `cmd/mosaic-run`. No use-case or adapter package outside `harness` itself may import `internal/harness` directly — all access goes through the `domain.HarnessAdapter` port.
- **Layered import direction, enforced by `tools/importcheck`:** `domain` imports nothing internal → `engine` imports only `domain` → `session` imports the core packages (`domain`, `engine`, `orchfile`, `workflow`, `compat`, `agentresolve`, `planstages`) but never `tui` or `cli` → `tui`/`cli` import `session` and `domain` but never each other. This boundary is a build-time gate (`task check:imports`), not just a convention.
- **Engine purity:** The `engine` package must contain no filesystem, network, randomness, or `time.Now()` imports — all time-varying data arrives as parameters. This is what makes deterministic golden-file testing of full runs possible.
- **Refusal over silent fallback:** Every package that validates input (orchfile, workflow admission via compat, planstages, agentresolve, artifact parsing) reports invalid input as a `*domain.RefusalError` naming the component, the resource, and the specific condition — never a silent default or best-effort guess.
- **Pure decision core / imperative shell:** `engine.Next` is a pure function that decides what happens next; `session` is the imperative shell that performs all I/O (harness invocation, artifact read/write, deviation resolution) and drives the loop.
- **Communication Protocol v1.8 vocabulary:** Status codes (SUCCESS, COMPLETED_NEEDS_ACTION, PARTIALLY_DONE, NEEDS_CLARIFICATION, CAPABILITY_EXCEEDED, BLOCKED) and error codes (E101, E401, E501, E502, E503) defined in `domain` are the shared vocabulary that `engine` routes on and that `harness`/`deviation` parse from subagent responses.
- **Atomic artifact writes:** `Orchestration.md` (the run's persistent state) is always rewritten via write-to-temp-then-rename, never in place, so a crash mid-write cannot corrupt run state.
- **Golden-file / fixture-driven testing:** Nearly every package pairs with a `testdata/` fixture directory (`orchfile`, `planstages`, `agentresolve`, `artifact`, `session`, `integration/golden`) containing hand-crafted markdown fixtures exercising valid and invalid shapes; integration tests compare produced `Orchestration.md` output byte-exactly against golden files.
- **External shared library dependency:** Several packages (`orchfile`, `artifact`, `session`, `deviation`, `tui`) depend on a sibling `mosaic-common` module (`mdtable`, `docformat`, `interaction`, `tui` helpers) that is outside this KB's scope — treated here as a fixed external dependency, not documented further.

## Key Invariants

- No package outside `domain` may be imported by `domain`; no package outside `harness` (and its consumers via the port) may import `internal/harness` directly.
- `engine` remains side-effect-free: no I/O, no direct clock/random reads.
- Every parsing/validation refusal produces a `*domain.RefusalError` — never a panic, never a silently-substituted default.
- `Orchestration.md` writes are always atomic (write-temp-then-rename); direct in-place writes are not used anywhere in the artifact package.
- `cli` and `tui` never import each other; both depend on `session` as their sole entry point into run execution.
- A `DispatchDecision.Steps` slice currently always contains exactly one element — parallel dispatch is not yet implemented but the shape reserves the seam for it.
