# Execution Groups

This document explains the execution-group mechanism available to workflow authors. It is the single reference you need to write a correctly grouped workflow. No runner source code reading is required.

---

## Contents

- [What execution groups are](#what-execution-groups-are)
- [Phase-column notation](#phase-column-notation)
- [Activation rule](#activation-rule)
- [The Execution Groups table](#the-execution-groups-table)
- [Why the table must sit inside the SECTION block](#why-the-table-must-sit-inside-the-section-block)
- [Vocabulary is yours to define](#vocabulary-is-yours-to-define)
- [Omission, ordering, and repetition](#omission-ordering-and-repetition)
- [Contiguity constraint](#contiguity-constraint)
- [Validation and refusal reference](#validation-and-refusal-reference)
- [How groups are recorded in `Orchestration.md`](#how-groups-are-recorded-in-orchestrationmd)
- [Worked examples](#worked-examples)

---

## What execution groups are

A grouped workflow splits its `EXECUTION` rows into named ranges — each range is one *execution group*. The plan artifact tells the runner which group sequence to use for each stage (via an `Approach` column), so the same workflow definition can run groups in different orders, skip a group entirely, or run every group for every stage — all without changing any runner code.

A workflow that does not need this flexibility keeps plain `EXECUTION.[StageNumber]` rows and no approach table. The mechanism is opt-in: bare and grouped workflows are both fully supported first-class shapes.

---

## Phase-column notation

The `Phase` column of the routing table controls group assignment. Two forms exist:

| Form | Example | Meaning |
|------|---------|---------|
| Bare staged row | `EXECUTION.[StageNumber]` | No group declared |
| Grouped staged row | `EXECUTION.Test.[StageNumber]` | Row belongs to group `Test` |

Rules:

- The phase name is exactly `EXECUTION`, uppercase, unchanged in both forms.
- `{Group}` is a workflow-defined CamelCase token. It contains no `.`, no whitespace, and is never empty. It is compared verbatim and case-sensitively everywhere.
- `[StageNumber]` is always the final segment, written literally with brackets. No numeric value is parsed from it.
- Disambiguation is unambiguous: the character immediately after `EXECUTION.` is `[` for a bare row and a group-token character for a grouped row.

Parse outcomes:

| Phase string | Base name (`Name`) | Is staged | Group |
|--------------|--------------------|-----------|-------|
| `RESEARCH` | `RESEARCH` | no | — |
| `EXECUTION.[StageNumber]` | `EXECUTION` | yes | (none) |
| `EXECUTION.Test.[StageNumber]` | `EXECUTION` | yes | `Test` |
| `EXECUTION.Implementation.[StageNumber]` | `EXECUTION` | yes | `Implementation` |

The `Name` field stays `EXECUTION` in every staged case.

---

## Activation rule

Groups are active if and only if at least one `EXECUTION` row in the workflow carries a group segment. This is the single activation signal.

A workflow where every `EXECUTION` row is bare:
- has no groups,
- requires no approach table,
- ignores any `Approach` value in the plan artifact,
- and runs its `EXECUTION` rows in the listed order.

There is no partial activation. A mix of bare and grouped rows in the same workflow is refused at admission (see [Validation and refusal reference](#validation-and-refusal-reference), refusals A2 and A3).

---

## The Execution Groups table

A grouped workflow carries an approach table under the reserved heading **`**Execution Groups:**`** (exact text, no leading or trailing spaces, full-line match required).

### Position inside the routing table section

The heading and its table must appear immediately after the routing table, inside the workflow's `[[SECTION:Workflow:{id}]]` block:

```
**Execution Groups:**

| Approach | Groups |
|----------|--------|
| TDD | Test, Implementation |
| Implementation-First | Implementation, Test |
| Implementation-Only | Implementation |
| Tests-Only | Test |
```

A blank line between the heading and the table is permitted. The heading must match a complete trimmed line — the parser reads the routing table from the start of the SECTION content and reads the approach table at or after the heading's offset. The two windows never overlap.

### Required columns

| Column | Meaning |
|--------|---------|
| `Approach` | Opaque token matched verbatim against the `Approach` column in the plan artifact's stage table. Case-sensitive. |
| `Groups` | Comma-separated, ordered list of group tokens for this approach. Each token is trimmed. Order is significant: the runner runs groups in this order. |

Additional columns are silently ignored.

### Row semantics

- Each row selects a group sequence for one approach token.
- A group omitted from a row is skipped when that approach is in effect for a stage.
- Row order in the table carries no semantics; lookup is always by `Approach` token.

---

## Why the table must sit inside the SECTION block

Only the content between `[[SECTION:Workflow:{id}]]` and `[[/SECTION:Workflow:{id}]]` is copied verbatim into a deployed orchestrator's baked instructions. Content outside the SECTION block — including the YAML frontmatter — is never copied and is therefore invisible to the orchestrator.

Placing the approach table outside the SECTION block, or inside the frontmatter, means the orchestrator cannot see it. Both the routing table and the approach table must be inside the SECTION block for the orchestrator to read them correctly.

Canonical SECTION content order for a grouped workflow:

1. `<!-- workflow-version: {version} -->` comment
2. Title and **Use when:** prose
3. Routing table (must be the first markdown table in the SECTION content)
4. `**Execution Groups:**` heading + approach table
5. **EXECUTION Stages:** sentence
6. **Notes:** block

A bare workflow omits item 4 entirely.

---

## Vocabulary is yours to define

Group names and approach values are opaque tokens defined entirely by the workflow. The runner carries no fixed list of either.

Consequences:

- Adding a new workflow, a new group name, or a new approach value requires no code change to the runner or orchestrator.
- The `Approach` cells in the approach table are matched verbatim against the `Approach` column of the plan artifact's stage table. The planner must produce tokens that match exactly. Case matters.
- CamelCase for group tokens and PascalCase or hyphenated strings for approach tokens are authoring conventions only. The runner treats them as opaque strings throughout.

---

## Omission, ordering, and repetition

- **Omission:** A group absent from an approach row is skipped for stages using that approach. A stage can run with one group, several groups, or all groups — depending on the row.
- **Ordering:** The sequence in the `Groups` cell determines the run order for that approach. An approach row with `Implementation, Test` runs the `Implementation` group before the `Test` group, which is the reverse of `Test, Implementation`.
- **Repetition:** A group that must run under every approach is listed in every row, at whatever position the author chooses. There is no always-run keyword. Position is always explicit.
- **No guaranteed order:** An approach row may order groups in any way, including an order different from another row in the same table.

---

## Contiguity constraint

Each group must be a contiguous range of `EXECUTION` rows. Interleaving is not allowed.

Valid layout (groups are contiguous):

```
EXECUTION.Test.[StageNumber]           ← Test group: rows 7–8
EXECUTION.Test.[StageNumber]
EXECUTION.Implementation.[StageNumber] ← Implementation group: rows 9–10
EXECUTION.Implementation.[StageNumber]
```

Invalid layout (groups interleave — refused at admission):

```
EXECUTION.Test.[StageNumber]
EXECUTION.Implementation.[StageNumber]
EXECUTION.Test.[StageNumber]           ← re-opens Test after Implementation started
```

---

## Validation and refusal reference

The runner validates workflows as early as the required information is available. Nothing falls back silently.

### Parse time (refused before the workflow runs at all)

| ID | Condition | When it fires |
|----|-----------|---------------|
| P1 | `EXECUTION` phase with a group token but no `[StageNumber]` segment — for example `EXECUTION.Test` | Parsing the routing table |
| P2 | `EXECUTION` phase with an empty or whitespace-bearing group token | Parsing the routing table |
| P3 | Reserved heading present but no table follows it | Parsing the workflow section |
| P4 | Approach table missing the `Approach` column | Parsing the approach table |
| P5 | Approach table missing the `Groups` column | Parsing the approach table |
| P6 | Approach table header present but no data rows | Parsing the approach table |
| P7 | Empty `Approach` cell, empty `Groups` cell, or an empty token inside a `Groups` cell | Parsing the approach table |
| P8 | Duplicate `Approach` token across rows, or duplicate group token within one `Groups` cell | Parsing the approach table |

### Admission time (refused when the runner loads the workflow before any dispatch)

| ID | Condition | When it fires |
|----|-----------|---------------|
| A1 | A group's rows are not contiguous — a row reopens a group that already ended | Resolving groups at admission |
| A2 | At least one `EXECUTION` row declares a group segment but the workflow has no `**Execution Groups:**` table | Cross-checking Phase column against the approach table at admission |
| A3 | The workflow declares an `**Execution Groups:**` table but at least one `EXECUTION` row is bare (no group segment) | Cross-checking Phase column against the approach table at admission |
| A4 | The approach table names a group that no `EXECUTION` row belongs to | Cross-checking approach table against the Phase column at admission |
| A5 | An `EXECUTION` row declares a group that appears in no approach table row | Cross-checking Phase column against the approach table at admission |

### Stage-table read time (refused when reading the plan artifact)

| ID | Condition | When it fires |
|----|-----------|---------------|
| S1 | The workflow declares groups but the plan artifact has no `Approach` column | Reading the plan artifact's stage table |
| S2 | The workflow declares groups and an `Approach` cell in the plan artifact is empty or `-` | Reading the plan artifact's stage table |

### Routing decision time (fails when the runner resolves the group order for a stage)

| ID | Condition | When it fires |
|----|-----------|---------------|
| R1 | A stage's `Approach` value has no matching row in the workflow's approach table | At first entry into `EXECUTION`, at each inter-stage advance, and at resume |

R1 is not a pre-invocation refusal. It is a typed routing-decision failure (`UnresolvableApproachError`) that fires mid-run and names the stage, the unrecognised approach value, and every declared alternative. There is no default approach and no fallback order.

---

## How groups are recorded in `Orchestration.md`

The phase notation described above is *routing-table* notation: it identifies a row of the workflow. It is never written into a run's `Orchestration.md`. That artifact records the run's position using its own vocabulary, defined normatively in `Development/Designs/OrchestrationArtifactFormat.md` §4.1. The part that matters to a workflow author is this:

| Field | Value |
|-------|-------|
| `Phase` (log column) and `current_state.phase` | Always the bare phase name `EXECUTION` — never `EXECUTION.Test`, never `EXECUTION.Test.[StageNumber]` |
| `Stage` (log column) and `current_state.stage` | `{Group}.{StageNumber}` for a grouped row (e.g. `Test.1`), a bare `{StageNumber}` for an ungrouped row (e.g. `4`) |

So a group token you define here is what appears, verbatim and case-sensitively, in the `Stage` column of every run that executes the rows carrying it — and in the `Created In` column of every artifact those rows produce (`EXECUTION.Test.1`). Group names are therefore visible to anyone reading a run artifact, which is worth weighing when you choose them.

**Why the bare phase name matters:** per-stage HITL resolution and the `EXECUTION` branch of crash recovery both compare the recorded phase against the literal string `"EXECUTION"`. A qualified form like `EXECUTION.Test` never matches, so HITL stops firing and recovery silently misroutes — neither produces an error message.

Both executors write these values identically, which is what lets a run started by an LLM orchestrator be resumed by the script runner and the reverse.

---

## Worked examples

### Grouped workflow: brownfield-tdd

This is the canonical grouped workflow. It has four `EXECUTION` rows split into two groups (`Test` and `Implementation`) and a four-row approach table.

Routing table extract (EXECUTION rows only):

```
| Phase | Subagent | ... |
|-------|----------|-----|
| EXECUTION.Test.[StageNumber] | test-writer-tdd | ... |
| EXECUTION.Test.[StageNumber] | tests-review-tdd | ... |
| EXECUTION.Implementation.[StageNumber] | implementation-tdd | ... |
| EXECUTION.Implementation.[StageNumber] | implementation-review | ... |
```

Approach table immediately after the routing table:

```
**Execution Groups:**

| Approach | Groups |
|----------|--------|
| TDD | Test, Implementation |
| Implementation-First | Implementation, Test |
| Implementation-Only | Implementation |
| Tests-Only | Test |
```

When the plan artifact marks a stage with `Approach: TDD`, the runner runs the `Test` group first, then the `Implementation` group. When `Approach: Implementation-Only`, only the `Implementation` group runs for that stage and the `Test` group is skipped.

Both the routing table and the approach table sit inside the `[[SECTION:Workflow:brownfield-tdd]]` block.

### Bare workflow: quick-fix

This workflow has one `EXECUTION` row with no group segment and no approach table. The runner ignores any `Approach` value in the plan artifact and runs the single row in the order it appears.

Routing table extract (EXECUTION row):

```
| Phase | Subagent | ... |
|-------|----------|-----|
| EXECUTION.[StageNumber] | implementation-tdd | ... |
```

No `**Execution Groups:**` heading. No approach table. This is valid and correct for a workflow that does not need group-based ordering.

---

See the full workflow files for complete examples:
- Grouped: `Workflows/Build/brownfield-tdd.md`
- Bare: `Workflows/Build/quick-fix.md`
