---
run_id: "20260801T202027Z-ad3d"
created_by: "knowledge-base-generator#4"
---

# compat

> Responsibility: Gatekeeps which parsed workflow routing tables the runner is allowed to execute ("admission"), and — for admitted workflows — resolves the EXECUTION phase's rows into one or two contiguous execution groups that the engine and session use to drive TDD vs. non-TDD stage flow.

## Overview

The runner does not support every shape a workflow markdown file could theoretically express — only a specific subset ("FR-18a") that its state machine (`engine`) and its stage-driven dispatch loop (`session`) know how to handle. `compat` is the single checkpoint between workflow parsing (`workflow`) and everything downstream: it either refuses a routing table outright with a precise, named reason, or it produces an `AdmittedWorkflow` that carries both the original table and the derived execution-group structure the rest of the runner relies on.

The single entry point is `Admit(table) (AdmittedWorkflow, error)`. There is no partial admission — a table is either fully admitted or refused.

## Components / Subdomains

| Component | Purpose |
|-----------|---------|
| **Admission checks** | Seven independently-checked FR-18a conditions, each producing its own refusal reason so a KB consumer/debugger can tell exactly which unsupported shape was encountered. |
| **Execution group resolution** | Classifies each EXECUTION-phase row's agent identifier and partitions the EXECUTION rows into a `GroupTest`/`GroupImplementation` pair (two-group / TDD-style workflows) or a single `GroupImplementation` group (single-group / implementation-only-style workflows). |

## Key Flows

### Admission (`Admit`)

Given a `RoutingTable`, checks run in this order (not strictly the FR-18a numbering, but the actual code order — condition numbers below match the FR-18a condition list, not execution order):

1. **Condition 6 — agent-with-mode notation:** any row whose `Agent` contains `(` or `)` is refused immediately, before any structural analysis, so the error names the exact row/agent.
2. **Condition 3 — non-EXECUTION staged phase:** a staged phase (`PhaseParsed.IsStaged == true`) whose name isn't literally `"EXECUTION"` is refused (staging is only supported for the EXECUTION phase).
3. **Condition 5 — parallel dispatch:** a comma inside `OnSuccess.Value` is treated as parallel routing and refused (the runner's `DispatchDecision.Steps` currently only ever holds one element — see project Index.md invariants).
4. **Locate the staged block:** scan all rows once to find the first and last staged-row index. If there are no staged rows at all, admission short-circuits to a non-staged `AdmittedWorkflow` (`HasStagedPhase: false`) — treated as an edge case since the supported workflow set always has an EXECUTION phase.
5. **Condition 2 — multiple staged phase blocks:** within the `[firstExecIdx, lastExecIdx]` range, if a non-staged row appears before a staged row (i.e. staged rows are non-contiguous), refuse.
6. **Condition 1 — stage source not the plan artifact:** if there are any pre-EXECUTION rows, at least one of them must produce the output artifact `Stage-*/Plan.md`; otherwise the runner has no way to know stages come from `planstages` reading the Plan artifact, and admission is refused.
7. **Condition 4 — dynamic/growing stage set:** if any EXECUTION-phase row itself produces `Stage-*/Plan.md`, that implies stages can be added mid-run, which is refused (only a fixed, pre-determined stage set from `planstages` is supported).
8. **Condition 7 — execution group resolution:** the EXECUTION rows are handed to group resolution (below); a resolution failure is surfaced as the final refusal reason.

On success, `Admit` returns an `AdmittedWorkflow` populated with the original table, the resolved `Groups`, `TwoGroup`, `HasStagedPhase: true`, and the pre-/post-EXECUTION row ranges (`PreExecutionStartRow`/`EndRow`, `PostExecutionStartRow`/`EndRow` — zero-based, half-open `[start, end)` ranges into `Table.Rows`).

### Execution Group Resolution

Operates only on the EXECUTION-phase rows (already isolated by `Admit`). Each row's `Agent` string is classified into one of four classes:

| Class | Agent identifiers |
|-------|--------------------|
| Test | `test-writer-tdd`, `tests-review-tdd` |
| Implementation | `implementation-tdd`, `implementation-review` |
| Neutral | `build-review` (can legitimately appear inside either group) |
| Unknown | anything else |

Resolution logic:
- **Unknown agents are only an error when mixed with a recognized (test or implementation) agent.** If *every* EXECUTION row is unknown/neutral (no test or implementation agent detected at all), the whole block collapses to a single implementation group — this intentionally permits generic/placeholder agent names in test fixtures that exercise engine behavior without using the real MOSAIC agent roster.
- **No test agents present → single-group workflow:** all EXECUTION rows become one `GroupImplementation` group spanning the full EXECUTION row range. (`TwoGroup = false`.)
- **Test agents present → two-group workflow:** the split point is the first row classified as Implementation. Everything from the first EXECUTION row up to (but not including) that row becomes `GroupTest`; everything from that row to the end of the EXECUTION range becomes `GroupImplementation`. (`TwoGroup = true`.)
  - It is an error if test agents are found but no implementation agent exists anywhere (a two-group split needs an implementation half).
  - It is an error if any Test-classified row appears *after* the split point (test and implementation rows must each be contiguous — a test row interleaved after the first implementation row breaks the "one contiguous test block, then one contiguous implementation block" shape).

Both `GroupTest` and `GroupImplementation` group boundaries are expressed as zero-based, half-open `[StartRow, EndRow)` row-index ranges into the original `RoutingTable.Rows` — not agent lists. Duplicate agent identifiers across rows (e.g. `build-review` appearing once in the test group and again in the implementation group) are explicitly allowed and do not affect grouping.

## Relationships

| Talks To | For |
|----------|-----|
| **workflow** | Consumes the `RoutingTable` produced by parsing a workflow region — this is `Admit`'s only input. |
| **domain** | Uses `RoutingTable`, `RoutingRow`, `PhaseParsed`, `AdmittedWorkflow`, `ExecutionGroup`, `GroupKind`, and `RefusalError` — compat has no types of its own beyond an internal, unexported `agentClass` enum. |
| **session** (run-start sequence) | `session` calls `Admit` once per run, after parsing the workflow and before resolving agents/reading stages; the resulting `AdmittedWorkflow` is threaded into `engine.Next` on every subsequent tick. |
| **engine** | Reads `AdmittedWorkflow.Groups`/`TwoGroup`/`HasStagedPhase`/pre-post-execution row ranges to decide dispatch order (test group before implementation group in two-group workflows) — compat does not call engine, it only produces the data engine consumes. |

## Key Concepts

| Concept | Meaning |
|---------|---------|
| **Admission (FR-18a)** | The gate that decides whether a routing table's *shape* is one the runner's state machine can drive — independent of whether individual agents/artifacts referenced in it exist. |
| **Staged phase** | A phase string of the form `EXECUTION.[StageNumber]` (`PhaseParsed.IsStaged == true`); the only phase the runner allows to repeat per stage. |
| **Execution group** | A contiguous sub-range of EXECUTION rows sharing a purpose (writing/reviewing tests vs. implementing/reviewing code); one workflow's EXECUTION phase resolves to either one or two of these. |
| **Two-group vs. single-group workflow** | Two-group = TDD-style workflows with a distinct test-writing sub-phase before implementation (e.g. brownfield/greenfield TDD variants). Single-group = workflows that go straight to implementation (e.g. quick-fix, implementation-only). |
| **Contiguity requirement** | Both the overall staged block and, within it, the test/implementation sub-blocks must each be one unbroken run of rows — no interleaving is supported. |

## Boundaries

- **Owns:** Validating that a parsed routing table's *structural shape* (phase staging, agent notation, dispatch fan-out, stage sourcing) is inside the supported subset, and deriving execution-group row ranges from admitted EXECUTION rows.
- **Does Not Own:** Parsing workflow markdown into a `RoutingTable` (that's `workflow`); resolving stage numbers/dependencies from the Plan artifact (that's `planstages`); resolving agent identifiers to agent definition files (that's `agentresolve`); deciding what to dispatch next at runtime (that's `engine`); anything about artifact I/O.

## Invariants & Conventions

- Every refusal returns `*domain.RefusalError{Component: "compat", ...}` — never a plain error, panic, or silent fallback (see project-level "Refusal over silent fallback" pattern).
- Each of the seven FR-18a conditions is checked independently and produces a distinct, human-readable reason string naming the offending row index and/or agent — refusals are diagnosable without re-reading the routing table.
- Duplicate agent identifiers across different EXECUTION rows are explicitly permitted (FR-26a) — grouping is row-based, not agent-identity-based.
- `Groups` and the pre-/post-execution row ranges use zero-based, half-open `[start, end)` conventions consistently with `RoutingRow.Index`.
- A routing table with zero staged rows is not itself an error at the `Admit` level — it returns a non-staged `AdmittedWorkflow` — though the codebase's supported workflow set always includes a staged EXECUTION phase in practice.

## Known Complexity

None identified beyond what's captured above — the seven admission conditions and the group-resolution classification rules are fully described at this tier. No deeper-tier document is recommended for `compat`.
