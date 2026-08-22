# AgentTest — Design

**Status:** Active
**Scope:** What AgentTest is, how its pieces connect, and what full-control orchestrator testing requires. All external dependencies (deploy tool `--catalog-folder`) are resolved; remaining work is AgentTest-internal wiring and test catalogue creation.

---

## 1. What AgentTest Is

AgentTest is a Go CLI/TUI tool that answers one question:

> Does this orchestrator, run under this harness, actually make the routing decisions its workflow says it should?

It exercises a **real LLM orchestrator** through a **real agent harness** (Claude Code, OpenCode, etc.), replacing every collaborator invocation with a **declarative stub** at the harness's interception layer, then evaluates the resulting invocation sequence and orchestration state against declarative assertions.

The orchestrator under test is a real LLM making real judgement calls. The collaborators it dispatches are intercepted before they reach any model — the interception pipeline catches each dispatch, consults a stub registry, and returns the declared response. The orchestrator never knows its collaborators are stubbed. This is what makes the test measure the orchestrator's routing behaviour in isolation.

### 1.1 What It Does Not Do (Yet)

- **Test individual subagents.** The architecture does not prevent this (the `layer: subagent` test mode exists and works), but the design focus and the open questions in this document concern orchestrator testing. Subagent testing is a planned future capability, not a current design concern.
- **Test the Runner.** `Tools/Runner` has its own test strategy: a `MosaicTest` workflow catalogue (`MosaicTestCatalog/`) with cheap-model stub agents, deployed manually into a real harness via `mosaic-deploy`, run via `mosaic-run`'s TUI, evaluated by watching the output. That is a separate, partially-manual testing tier. The two tools share no test infrastructure beyond the deploy tool itself. The MosaicTest design is documented in `MosaicTestHarnessSuiteBrainstorm.md`.

### 1.2 Core Use Cases

**Routing correctness.** A developer suspects the orchestrator makes a wrong routing decision — say, it misroutes a `COMPLETED_NEEDS_ACTION` response when `On Findings` is ambiguous. They prepare one or more **orchestrator variants** with different fixes, define a **test workflow** whose routing table sets up the exact condition, declare **stub responses** that trigger the behaviour, and run all variants against the same test to see which one routes correctly.

**Model and harness comparison.** Run the same test scenario 100 times against each of several models and each available harness. Compare models on routing accuracy (does the orchestrator make the right decision N% of the time?). Compare harnesses for negative influence (does one harness shape interfere with correct behaviour more than another?). The tool already supports this via `repetitions` and `pass_rate` in suite definitions, and `--harness` for harness selection. Model selection is per-subject via `subject.model`.

**Regression testing.** Once a routing fix ships into the production orchestrator, the test scenario that proved it works becomes a regression test. The test definition switches from pointing at the variant to pointing at the production orchestrator. The test workflow, stubs, and assertions stay the same.

These use cases all require full control over every variable: which orchestrator, which workflow, what stubs return, which model, which harness, and what the pass criteria are.

---

## 2. Piece Inventory

### 2.1 What Exists and Works

| Piece | Status | Where |
|-------|--------|-------|
| **Interception Pipeline** | Complete | `internal/intercept`, `internal/interceptor`, `internal/stubmatch`, `internal/runstate`, `internal/invlog`, `internal/sideeffects` |
| **Harness Adapters** | `claudecode` complete, `opencode` directory exists, `fake` (scripted, no-LLM) complete | `internal/harness/` |
| **Conformance Suite** | Complete — every adapter must pass it unchanged | `internal/harness/contract/` |
| **Authoring & Preflight** | Complete — `.test.yaml`, `.stubs.json`, `.suite.yaml` parsing, `$ref` fixture resolution, cross-validation | `internal/authoring/`, `internal/fixtures/`, `internal/preflight/` |
| **Execution & Evaluation** | Complete — sandbox lifecycle, evidence assembly, pure verdict engine, repetitions, pass-rate aggregation | `internal/runner/`, `internal/suite/`, `internal/evaluate/`, `internal/concurrency/`, `internal/protocolcheck/`, `internal/orchstate/`, `internal/cost/` |
| **Reporting** | Complete — single `report.Result` model, text + JSON renderings | `internal/report/` |
| **Frontends** | CLI + TUI (bubbletea) | `internal/cli/`, `internal/tui/` |
| **Deploy Integration** | Partial — `domain.AgentDeployer` port delegates to `mosaic-deploy render` subprocess (works for single-agent rendering). Port needs `--catalog-folder` wiring and a `Deploy` method for single-call catalogue deployment (§7) | `internal/agentdeploy/` |
| **Architecture Enforcement** | Complete — static import-layer checker | `tools/importcheck/` |
| **Stub Agent Definitions** | 4 generic-form stubs | `agents/` |
| **Example Suites** | `examples/` (fake harness, exercised by Go e2e tests), `tests/first-run/` (real claude-code harness) | `examples/`, `tests/first-run/` |

### 2.2 Wiring That Is Complete

These connections are implemented, tested, and working end-to-end:

**Subject agent rendering.** The `.test.yaml` field `subject.agent` names a catalogue agent key. During setup, the runner calls `Deploy.Render` with that key, the harness ID, and the sandbox as workspace. The deploy tool renders the generic-form agent into the harness's expected location inside the sandbox. The runner records the rendered path in the setup ledger for teardown.

**Workflow selection.** The `.test.yaml` field `subject.workflows` carries a list of workflow IDs. It is parsed by `authoring`, passed through `runner.setup` into `Deploy.Render` as `RenderAgentRequest.Workflows`, and forwarded to the deploy tool as `--workflows <ids>`. The deploy tool injects exactly those workflows into the orchestrator's `<AvailableWorkflows type="managed">` region. Three-way nil/empty/populated semantics are preserved: nil = unspecified (deploy tool injects all catalogue workflows), `[]` = explicitly none, `[id, ...]` = exactly these.

**Stub agent rendering (current path).** The `.test.yaml` field `stub_agents` lists collaborator identities and source paths to generic-form definitions in `agents/`. During setup, the runner calls `Deploy.Render` for each, with `SourcePath` set. The deploy tool renders them into the sandbox's agent directory. This makes the dispatch legal from the harness's perspective — the actual stub *response* comes from the interception layer, not from these files. Under the test catalogue design (§3), this per-stub rendering is superseded by the `deploy` subcommand, which deploys all referenced agents in one call. The `stub_agents` mechanism remains for subagent-layer tests and edge cases.

**Seed files.** The `.test.yaml` field `seed_files` places files into the sandbox before the subject runs, with `{run_id}` expansion in paths and `$ref` resolution for content.

**MOSAIC root override.** AgentTest's CLI accepts `--mosaic-root <dir>`, wired through `WiringConfig.MosaicRoot` into `agentdeploy.Options.MosaicRoot`, forwarded as `--mosaic-root <dir>` to the deploy tool.

### 2.3 What Is Not Yet Wired

**Test catalogue and deployment integration.** No test-specific catalogue tree exists inside `Tools/AgentTest/`. The current deployment path uses `render` (single-agent rendering) for each agent individually. The deploy tool now supports `--catalog-folder` on all subcommands, so the remaining work is AgentTest-side: wiring `--catalog-folder` into the `agentdeploy` port's `Options`, adding a `Deploy` method that calls the `deploy` subcommand, creating the test catalogue tree, and updating the runner's setup phase to use the single-call deployment path.

---

## 3. The Test Catalogue

Test workflows and test orchestrator variants do not belong in the product catalogue (`Catalog/`, `Catalog/Workflows/`). They would pollute real deployments, and they exist to test specific conditions that may not correspond to any shipped workflow shape.

### 3.1 The Problem with `--mosaic-root`

`--mosaic-root` replaces the **entire** MOSAIC root, not just the catalogue. The deploy tool reads non-catalogue resources from the root too:

- `Development/Designs/CommunicationProtocol.md` — the protocol document injected into every agent
- `Catalog/SourceFilesFormat.md` — the bundle spec
- Skill folders under `Catalog/Skills/`

A test catalogue used as `--mosaic-root` would need copies of all of these, or protocol loading and bundle loading would fail. This is why `MosaicTestCatalog/` at the repo root does not follow the standard catalogue structure (its agents live under `Agents/MosaicTest/`, not `Subagents/MosaicTest/`) — it was built for manual deployment, not as a `--mosaic-root` target for the `render` subcommand.

### 3.2 `--catalog-folder`

The deploy tool supports `--catalog-folder <dir>` as a persistent flag on all subcommands. It redirects only the **catalogue source directories** — where agents and workflows are scanned from — while keeping everything else (protocol, bundles, skills, harness descriptors) resolved from the real MOSAIC root.

`catalog.Load(mosaicRoot, catalogFolder)` takes the two roots as separate arguments. When `--catalog-folder` is absent, the default `{mosaicRoot}/Catalog` is used. When present, agents and workflows resolve against the supplied folder while protocol, bundles, and other root-relative resources stay at the real root.

For AgentTest, this means the test catalogue is a simple directory:

```
Tools/AgentTest/
├── catalog/
│   ├── Orchestrator/
│   │   └── orchestrator.md             # production orchestrator (or a variant)
│   ├── Subagents/
│   │   └── TestStubs/
│   │       ├── researcher.md
│   │       └── planner.md
│   └── Workflows/
│       ├── Index.md                     # workflow index for this catalogue
│       └── AgentTest/
│           ├── ambiguous-findings.md
│           └── three-agent-linear.md
```

The test author puts the orchestrator under test into the catalogue tree as the orchestrator. The deploy tool resolves it by its standard path, renders it with the test workflows, and the result is a properly transformed orchestrator with exactly the workflows the test needs. No `--source`, no `--mosaic-root` — just `--catalog-folder` pointing at the test tree.

**This dissolves the `subject.agent` vs `subject.source` question (§5.3 in the previous draft).** There is no need for an arbitrary source path in the test definition. The orchestrator variant goes into the test catalogue as `Orchestrator/orchestrator.md`. The test definition uses `subject.agent: orchestrator` as normal. If the test author wants to compare five variants, they either:
- Run the same test five times, swapping the orchestrator file in the catalogue between runs
- Or maintain five catalogue trees, one per variant (cheap — the only file that differs is the orchestrator)

The second is heavier but fully parallel and automatable. The first is the common workflow for interactive use.

### 3.3 What This Means for AgentTest

AgentTest's `agentdeploy` port needs two additions: a `CatalogFolder` field in `Options` (emitting `--catalog-folder` to the subprocess), and a `Deploy` method that calls the `deploy` subcommand instead of `render`. The wiring through `WiringConfig` is identical to the existing `MosaicRoot` pattern.

The net effect: the test author recreates just the agents/workflows directory structure, puts their orchestrator and test workflows there, and points the tool at it. Everything else — protocol injection, bundle loading, harness transformation — comes from the real MOSAIC root as it always has.

### 3.4 Why Not Reuse the Product Catalogue

Three reasons, each sufficient alone:

1. **Pollution.** Test workflows would appear in `mosaic-deploy`'s workflow selection for real deployments.
2. **Variant testing.** The core use case involves testing orchestrator variants that don't exist in the catalogue and may never ship.
3. **Decoupling.** A test suite should be self-contained. Its orchestrator, workflows, stubs, and assertions form a unit.

### 3.5 Relationship to `MosaicTestCatalog/`

`MosaicTestCatalog/` at the repo root serves `Tools/Runner`'s harness conformance tests. It is a manually-deployed catalogue tree with test-only workflows and cheap-model stub agents. MosaicTest deploys manually via `mosaic-deploy` TUI/CLI.

Now that `--catalog-folder` exists, `MosaicTestCatalog/` could also use it instead of its current informal deployment process. The two testing tools share the same catalogue-redirection mechanism.

---

## 4. How a Test Works End-to-End

### 4.1 What the Test Author Creates

To test whether an orchestrator handles a specific routing condition correctly:

1. **An orchestrator** (`catalog/Orchestrator/orchestrator.md`) — the production orchestrator, or a variant with a specific fix. This is a generic-form file, placed at the standard orchestrator path within the test catalogue.

2. **A test workflow** (`catalog/Workflows/AgentTest/<id>.md`) — a workflow definition whose routing table sets up the condition. Agent names in the routing table must match the stub agent definitions and the stub registry entries. Standard workflow schema (frontmatter with `id`, `referenced_agents`, `<Workflow type="core" name="<id>">` block).

3. **Stub agent definitions** (`catalog/Subagents/TestStubs/<name>.md`) — generic-form placeholder files for each collaborator the workflow references. These are empty stubs (content is just a `# stub: <name>` comment) — all actual behaviour comes from the stub registry. They exist solely to pass the deploy tool's agent-exists validation. Common stubs are shared across tests.

4. **A stub registry** (`tests/<suite>/<test>.stubs.json`) — declares what each collaborator returns when intercepted. This is where the routing condition is created.

5. **A test definition** (`tests/<suite>/<test>.test.yaml`) — ties everything together: names the subject, the workflow(s), the stub registry, seed files, and assertions.

6. **A suite file** (`tests/<suite>/<suite>.suite.yaml`) — groups test definitions with shared defaults (timeout, repetitions, pass rate).

### 4.2 What Happens at Runtime

```
                        preflight (dry-run validation)
                                    │
                    ┌───────────────┴───────────────┐
                    ▼                               ▼
            deploy orchestrator              resolve fixtures
            + all referenced agents          (seed files,
            (mosaic-deploy deploy             $ref expansion)
             --catalog-folder <test-cat>)         │
                    │                               │
                    ▼                               │
            provision sandbox                       │
            (hooks, bridge,                         │
             logger bundle)                         │
                    │                               │
                    ├───────────────────────────────┘
                    ▼
            launch subject
            (harness CLI invocation)
                    │
            ┌───────┴───────┐
            │               │
            ▼               ▼
    orchestrator        interception
    runs (LLM)         pipeline catches
        │               each dispatch
        │                   │
        ├──── dispatch ────►│
        │                   ├── match stub
        │◄── stub resp ────┤   registry
        │                   ├── append to
        ├──── dispatch ────►│   invocation log
        │                   │
        │◄── stub resp ────┤
        │                   │
        ▼                   ▼
    subject completes   evidence snapshot
                        (invlog, orchstate,
                         protocol check,
                         concurrency, cost)
                            │
                            ▼
                    evaluate (pure verdict)
                            │
                            ▼
                      report / render
```

1. **Preflight** dry-runs the deploy call to catch catalogue misses, bad workflow IDs, and missing agent definitions before any sandbox is created.
2. **Setup** creates a fresh sandbox, deploys the orchestrator and all referenced agents into it via a single `mosaic-deploy deploy --catalog-folder <test-catalogue>` call (or via per-agent `render` calls when using the `stub_agents` path), seeds declared files, provisions the interception hooks. The deploy tool resolves agents and workflows from the test catalogue while loading protocol, bundles, and skills from the real MOSAIC root.
3. **Launch** starts the orchestrator through the harness CLI. The orchestrator sees a real harness environment with its workflow(s) baked into its system prompt.
4. **Interception** — every time the orchestrator dispatches a collaborator, the harness routes the call through the interception pipeline. The pipeline matches the collaborator identity against the stub registry and returns the declared response. The orchestrator receives it as if a real collaborator produced it.
5. **Snapshot** captures the invocation log, the orchestration document the orchestrator wrote, and any side effects, while the sandbox still exists.
6. **Evaluate** is a pure function from evidence to verdict: did the orchestrator dispatch the right agents in the right order? Did it reach the expected final state? Did the invocation sequence match? Did echo fidelity hold?
7. **Teardown** removes exactly the sandbox the setup ledger records.

### 4.3 What the Test Author Asserts

Available assertion classes, all evaluated from the evidence snapshot:

| Assertion | What It Checks |
|-----------|---------------|
| `invocation_sequence` | The order of collaborator dispatches (exact or subsequence) |
| `final_state` | The orchestration document's final phase and last status |
| `execution_log` | Agent instance IDs and/or uniform status across the log |
| `protocol_violations` | Count of Communication Protocol violations in intercepted messages |
| `artifact_created` | Named files exist in the sandbox after the run |
| `artifact_not_created` | Named files do not exist |
| `min_concurrency` | Minimum observed simultaneous invocations per parallel group |
| `task_messages` | Content assertions on individual task invocation messages |

Plus **echo fidelity** — evaluated unconditionally on every run, never declared, never invertible. Every stubbed collaborator's observed response must match the declared stub response exactly.

---

## 5. Cutoff Semantics and Evidence Boundaries

### 5.1 What N Counts

`stop_after_invocations: N` counts **completed collaborator dispatches** — specifically, the number of times the interception pipeline has both started and finished handling a collaborator call, delivering the stub reply to the subject. Each call the subject makes and receives a response for increments the counter by one. A call the subject begins but that terminates before reply delivery (because the subject process was already stopped) is not counted.

### 5.2 The Termination Moment

Termination is **immediate on delivery of the Nth reply**. The interception pipeline delivers the Nth stub reply to the subject, then writes the early-exit sentinel file. The supervisor observes the sentinel on its next poll (every 250 ms) and cancels the subject's launch context, ending the subject's process.

There is no grace period. The subject process ends as soon as the supervisor observes the sentinel. It does not wait for the subject to perform any further action — the subject is terminated in the middle of whatever it was doing next.

### 5.3 What the Evidence Contains

When a run exits via `stop_after_invocations: N`, the evidence snapshot contains:

- **Exactly N invocation-log entries** (one start record and one end record per dispatch). Because termination fires after the Nth reply is delivered, not before, no phantom (N+1)th entry is ever present. The interception pipeline writes its records synchronously before delivering the reply, so the Nth records are always present.
- **The orchestration document as it existed when the subject process was terminated** — which may be mid-update if the subject was writing it when the supervisor cancelled it.
- **Any files the stub side effects created** — these are written by the interception pipeline before the reply is delivered, so they are always present regardless of the cutoff.
- **No events the subject would have emitted after receiving the Nth reply** — the subject is terminated before it can write output artifacts for that dispatch, log the Nth agent in its execution log, or dispatch a subsequent collaborator.

### 5.4 Post-Reply Bookkeeping Caveat

Because the subject is terminated immediately after the Nth reply is delivered, any **bookkeeping the subject performs after receiving dispatch N** — writing the Nth dispatch's result to the orchestration document, recording the Nth agent in the execution log, writing the Nth output artifact — may be absent from the evidence. This is expected behaviour, not evidence corruption.

**Practical consequence for test authors:** Do not assert on the subject's own post-reply state for the final dispatch (dispatch N). For a single-dispatch test (`stop_after_invocations: 1`), do not assert `final_state`, `execution_log`, or `artifact_created` for artifacts the subject writes after receiving the reply — these assertions observe the subject's own bookkeeping, which may be incomplete when the cutoff fires. The `invocation_sequence` assertion observes the interception pipeline's own log and is always reliable regardless of cutoff.

### 5.5 What Each Assertion Class Observes

| Assertion | Source | Reliable after cutoff? |
|-----------|--------|------------------------|
| `invocation_sequence` | Interception pipeline log | Always reliable — written by the pipeline before the reply is delivered |
| `task_messages` | Interception pipeline log | Always reliable — messages are recorded when intercepted, before the reply |
| `echo_fidelity` | Interception pipeline log | Always reliable |
| `min_concurrency` | Interception pipeline log | Always reliable |
| `final_state` | Subject's orchestration document | Unreliable for dispatch N — the subject may not have updated it yet |
| `execution_log` | Subject's orchestration document | Unreliable for dispatch N — same reason |
| `artifact_created` / `artifact_not_created` | Files in the sandbox | Reliable for files stub side effects create; unreliable for files the subject writes after receiving dispatch N |

The reliable group all observe data the interception pipeline records independently of the subject's own activity. The unreliable group observe the subject's own output, which the cutoff may have interrupted mid-write.

### 5.6 Protocol-Envelope Precondition for Run Identity and Cost Attribution

AgentTest's evidence bucketing and per-run cost attribution depend on the subject being identifiable by the MOSAIC run ID that AgentTest injects at launch. The logger bundle resolves this identity by two mechanisms:

1. Reading the `run_id` field from the first collaborator dispatch the subject makes using the documented protocol envelope.
2. Falling back to the `MOSAIC_RUN_ID` environment variable that AgentTest injects into the subject's environment before launch.

A subject that **never dispatches a collaborator using the documented protocol envelope** — a misconfigured subject, or a subject that aborts before its first dispatch — produces no extractable run ID from mechanism (1). Mechanism (2) provides the binding via the environment variable for logger-bundle versions that include this fallback.

**Pre-dispatch events:** Any events the subject emits before its first collaborator dispatch may be attributed to the `unknown-run` fallback bucket rather than to the per-run log folder, depending on the timing of the logger bundle's identity resolution. Events emitted before the first dispatch are outside the window where the bundle has confirmed the run identity.

**Practical consequences:**
- A cost report of `unknown_bucket` means the subject's events landed in the fallback bucket because run identity was not resolved before those events were emitted. This is a run-identity binding failure, not a pricing configuration failure.
- A cost report of `unavailable` (with "no usage data found") means the logger bundle either did not run or the subject terminated before emitting any events.
- A subject that dispatches at least one collaborator before the cutoff will generally have its events attributed correctly, because the protocol-envelope binding fires on the first dispatch.

---

## 6. Design Decisions

### 6.1 Test Workflow ↔ Stub Consistency

A test workflow references agent names in its routing table. Those same names must appear in the stub registry (what they return) and in the stub agent definitions (the files that make the dispatch legal). This is a three-way coupling the test author maintains manually.

Preflight validates the deploy calls (agent key exists, workflow ID resolves) but does not cross-check that every agent named in the workflow has a stub entry. A missing stub entry surfaces at runtime as an unmatched invocation, handled per the configured policy (halt, passthrough, or generic response).

**Current position:** Manual consistency is acceptable. The error paths are well-defined and diagnosable. Cross-validation could be added later without changing the authoring format.

### 6.2 Orchestrator Variant Lifecycle

Orchestrator variants are experimental files placed in the test catalogue. They have no lifecycle beyond git. After a fix ships, the test should switch to the production orchestrator. Keeping stale variants is the same forking problem the transformation architecture exists to prevent.

**Position:** Variants are disposable. The test catalogue's orchestrator slot is a workbench, not an archive. Once a fix ships, the test points at the production orchestrator and becomes a regression test.

**Deferred: workbench folder concept.** A possible future enhancement: a `workbench/` folder alongside the test catalogue, holding named orchestrator variants. The test tool could offer a picker dialog letting the user select which variant to test. This would avoid manual file-swapping for interactive variant comparison. Not designed — noted as a direction.

### 6.3 Multiple Variants in One Test Run — Resolved

The core use case (§1.2) involves comparing variants. Running the same test against five variants means five test runs, each with a different orchestrator in the catalogue.

**Decision: manual swap.** The test author replaces `catalog/.../orchestrator.md` between runs. Simple, interactive, no tooling needed. Parallelisation is achievable via workspace copies — duplicate the catalogue, put a different variant in each, run simultaneously. Not elegant, but variant comparison is an infrequent interactive activity, not a hot path.

### 6.4 Stub Agents — Resolved

**Decision: stubs live in the test catalogue.** Placeholder stub agent definitions live at `catalog/Subagents/TestStubs/<name>.md` within the test catalogue. This lets the deploy tool's `deploy` subcommand resolve all agents referenced by the test workflow in a single call — no separate `--source` rendering needed per stub.

Stub files are empty placeholders (a `# stub: <name>` comment and minimal frontmatter). All actual stub behaviour comes from the stub registry (`.stubs.json`). This means the existing `stub_agents` field in `.test.yaml` becomes unnecessary for catalogue-deployed tests — the deploy tool handles stub agent rendering as part of the full deployment.

The existing `agents/` folder and `stub_agents` mechanism remain available for edge cases or subagent-layer tests that don't use the test catalogue path.

---

## 7. Deploy Tool Dependency

AgentTest uses the deploy tool as a subprocess, through the `domain.AgentDeployer` port. This is a binary dependency, not a library import — the harness isolation boundary forbids importing `mosaic-deploy` directly.

### 7.1 Primary Interface: `deploy` Subcommand

The primary deployment mechanism is the `deploy` subcommand, which deploys an orchestrator and all agents referenced by selected workflows into a workspace in a single call. This matches the test setup need exactly: one call produces a fully-wired sandbox with the orchestrator and all its stub collaborators.

All required deploy tool capabilities exist:

| Capability | Flag | Status |
|-----------|------|--------|
| Redirect catalogue resolution | `deploy --catalog-folder` | Implemented |
| Deploy orchestrator + agents for workflows | `deploy --workflows` | Implemented |
| Workspace destination | `deploy --workspace` | Implemented |
| Dry-run validation | `deploy --dry-run` | Implemented |
| JSON output | `deploy --output json` | Implemented |

The remaining work is AgentTest-side: wiring `--catalog-folder` into the `agentdeploy` port and adding a `Deploy` method that calls the `deploy` subcommand.

### 7.2 Secondary Interface: `render` Subcommand

The `render` subcommand (single-agent rendering) remains available for edge cases: subagent-layer tests, rendering from an arbitrary source path, or any test that needs an agent not covered by the workflow-driven deployment. AgentTest's existing `agentdeploy` port already uses `render` — the migration to `deploy` is additive, not a replacement.

| Capability | Flag | Status |
|-----------|------|--------|
| Render arbitrary source file | `render --source` | Exists |
| Render catalogue agent | `render --source-agent` | Exists |
| Workflow selection | `render --workflows` | Exists |
| Workspace destination | `render --workspace` | Exists |
| Overwrite existing | `render --overwrite` | Exists |
| Dry-run validation | `render --dry-run` | Exists |
| JSON output | `render --output json` | Exists |

### 7.3 Implementation Path

The deploy tool's `--catalog-folder` is implemented. `catalog.Load(mosaicRoot, catalogFolder)` already separates catalogue loading from protocol/bundle loading.

AgentTest's `agentdeploy` port needs:
1. A `CatalogFolder` field in `Options`, emitting `--catalog-folder` when non-empty.
2. A `Deploy` method on the `AgentDeployer` interface that calls the `deploy` subcommand with `--catalog-folder`, `--workspace`, `--workflows`, `--harness`, `--dry-run`, and `--output json`.
3. The runner's setup phase must call `Deploy` (single call) for catalogue-based tests, falling back to per-agent `Render` for `stub_agents`-based tests.

See `Requirements-agentTest.md` for the full requirement set.

---

## Related Documents

- `Tools/AgentTest/docs/LaunchGuide.md` — where to run the binary from, what to stage beside it, the frontend selection rules, configuration overrides, and a worked example of the "wrong launch location" pre-flight failure
