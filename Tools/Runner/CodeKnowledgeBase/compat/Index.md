---
run_id: "20260801T202027Z-ad3d"
created_by: "knowledge-base-generator#4"
---

# compat

> Responsibility: Gatekeeps which parsed workflow routing tables the runner is allowed to execute ("admission"), and — for admitted workflows — partitions the EXECUTION phase's rows into contiguous named execution groups driven by the Phase column's group segment, then carries the workflow's approach table through to the admitted workflow for downstream consumers.

## Overview

The runner does not support every shape a workflow markdown file could theoretically express — only a specific subset ("FR-18a") that its state machine (`engine`) and its stage-driven dispatch loop (`session`) know how to handle. `compat` is the single checkpoint between workflow parsing (`workflow`) and everything downstream: it either refuses a routing table outright with a precise, named reason, or it produces an `AdmittedWorkflow` that carries both the original table and the derived execution-group structure the rest of the runner relies on.

The single entry point is `Admit(table) (AdmittedWorkflow, error)`. There is no partial admission — a table is either fully admitted or refused.

## Components / Subdomains

| Component | Purpose |
|-----------|---------|
| **Admission checks** | Seven independently-checked FR-18a conditions, each producing its own refusal reason so a KB consumer/debugger can tell exactly which unsupported shape was encountered. |
| **Execution group resolution** | Partitions the EXECUTION-phase rows into contiguous named groups by reading the group segment from each row's `PhaseParsed.Group` field. Agent identifiers are never inspected. Cross-validates the declared group set against the workflow's approach table (refusals A1–A5). |

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
8. **Condition 7 — execution group resolution:** the EXECUTION rows are handed to group resolution (below); a resolution failure is surfaced as the final refusal reason. Resolution inspects each row's `PhaseParsed.Group` segment, never agent identifiers.

On success, `Admit` returns an `AdmittedWorkflow` populated with the original table, the resolved `Groups`, `GroupsDeclared`, `ApproachTable` (carried through from the routing table), `HasStagedPhase: true`, and the pre-/post-EXECUTION row ranges (`PreExecutionStartRow`/`EndRow`, `PostExecutionStartRow`/`EndRow` — zero-based, half-open `[start, end)` ranges into `Table.Rows`).

### Execution Group Resolution

Operates only on the EXECUTION-phase rows (already isolated by `Admit`). Group membership is read exclusively from each row's `PhaseParsed.Group` field — the group segment parsed from the Phase column (e.g. `EXECUTION.Test.[StageNumber]` yields group `Test`). Agent identifiers are never inspected.

**Activation rule (`GroupsDeclared`):** `GroupsDeclared` is `true` if and only if at least one EXECUTION row carries a non-empty group segment. When `false`, the workflow is a bare workflow.

**Bare workflow (no group segments declared):**
- All EXECUTION rows form a single implicit group with an empty `Name`, spanning the full EXECUTION row range.
- `GroupsDeclared = false`.
- An approach table present in the workflow alongside bare rows is refused (A3).

**Grouped workflow (at least one group segment declared):**
- Rows are partitioned by group token into contiguous half-open `[StartRow, EndRow)` ranges, ordered by first appearance. There is no upper bound on the number of groups — three or more groups resolve correctly.
- A3: if any EXECUTION row is bare (empty group segment) while an approach table is present, admission is refused.
- A2: if grouped rows appear but the workflow has no approach table, admission is refused.
- A1: if a row re-opens a group that had already ended (non-contiguous), admission is refused.
- A4: if the approach table names a group that no EXECUTION row declares, admission is refused.
- A5: if an EXECUTION row declares a group absent from every approach table row, admission is refused.

Group boundaries are expressed as zero-based, half-open `[StartRow, EndRow)` row-index ranges into the original `RoutingTable.Rows`. Duplicate agent identifiers across rows are explicitly allowed and do not affect grouping — partitioning is Phase-column-based, not agent-identity-based.

## Relationships

| Talks To | For |
|----------|-----|
| **workflow** | Consumes the `RoutingTable` produced by parsing a workflow region — this is `Admit`'s only input. |
| **domain** | Uses `RoutingTable`, `RoutingRow`, `PhaseParsed`, `AdmittedWorkflow`, `ExecutionGroup`, `GroupName`, `ApproachTable`, and `RefusalError` — compat has no types of its own. |
| **session** (run-start sequence) | `session` calls `Admit` once per run, after parsing the workflow and before resolving agents/reading stages; the resulting `AdmittedWorkflow` is threaded into `engine.Next` on every subsequent tick. `session` reads `GroupsDeclared` from the result to gate whether the Approach column is required when reading stages. |
| **engine** | Reads `AdmittedWorkflow.Groups`, `GroupsDeclared`, `ApproachTable`, `HasStagedPhase`, and the pre-/post-execution row ranges to decide dispatch order — compat does not call engine, it only produces the data engine consumes. |

## Key Concepts

| Concept | Meaning |
|---------|---------|
| **Admission (FR-18a)** | The gate that decides whether a routing table's *shape* is one the runner's state machine can drive — independent of whether individual agents/artifacts referenced in it exist. |
| **Staged phase** | A phase string of the form `EXECUTION.[StageNumber]` (bare) or `EXECUTION.{Group}.[StageNumber]` (grouped) where `PhaseParsed.IsStaged == true`; the only phase the runner allows to repeat per stage. |
| **Execution group** | A contiguous sub-range of EXECUTION rows identified by a workflow-defined name taken from the Phase column's group segment. A workflow resolves to one or more groups; bare workflows produce a single implicit group with an empty name. |
| **Groups-declared vs. bare workflow** | A workflow where at least one EXECUTION row carries a group segment is a grouped workflow (`GroupsDeclared = true`); it must also have an approach table. A workflow where no row carries a group segment is a bare workflow (`GroupsDeclared = false`); approach is ignored. |
| **Contiguity requirement** | Both the overall staged block and each named group within it must each be one unbroken run of rows — no interleaving of rows belonging to different groups is supported. |

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

None identified beyond what's captured above — the seven admission conditions and the group-resolution partition rules (including the A1–A5 cross-consistency checks) are fully described at this tier. No deeper-tier document is recommended for `compat`.
