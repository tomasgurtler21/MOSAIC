---
version: "1.0"
name: "MosaicTest Deviation Ambiguous Workflow"
description: "Runner mode fixture — Auto-review mode with an ambiguous On Findings hint. Proves that an unresolvable hint deviates even in Auto-review, where an unambiguous hint would auto-route."
hint: "Mode 3 test — ambiguous On Findings forces deviation even in auto-review"
author: MOSAIC
id: deviation-ambiguous
referenced_agents:
  - mosaictest-scripted
artifacts:
  - MosaicTestScript/ambiguous-produce.md
  - MosaicTestAmbiguousReport.md
---

<Workflow type="core" name="deviation-ambiguous" version="1.0">
## MosaicTest Deviation Ambiguous Workflow

**Use when:** Verifying that Auto-review mode does NOT auto-route `COMPLETED_NEEDS_ACTION` when the On Findings column is ambiguous (contains spaces or parentheses). The engine's `isUnambiguousHint` check must reject it, and the result must become a deviation exactly as it would in Auto mode.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| RESEARCH | mosaictest-scripted | FALSE | COMPLETE | mosaictest-scripted (re-check) | MosaicTestScript/ambiguous-produce.md | MosaicTestAmbiguousReport.md |

**Notes:**
- **Run in Auto-review mode.** The whole point is to show that Auto-review does NOT auto-route when the hint is ambiguous. Running in Auto mode would produce the same log (Auto always deviates on CNA) and prove nothing specific.
- The On Findings value `mosaictest-scripted (re-check)` is deliberately ambiguous — it contains a space and parentheses. The engine's `isUnambiguousHint` check rejects any value containing ` `, `(`, or `)`.
- The script is marker-gated: first invocation returns `COMPLETED_NEEDS_ACTION` and writes the marker; second invocation returns `SUCCESS`.
- Non-staged. No `Plan.md` required.
- Seed `Fixtures/deviation-ambiguous` as the single seed path.

</Workflow>

---

## Design Rationale

### Why this needs its own workflow

`findings-loop` proves that Auto-review auto-routes CNA with an unambiguous On Findings. This workflow proves the converse: that Auto-review does NOT auto-route when the hint is ambiguous. Without both, the suite cannot distinguish "Auto-review auto-routes everything" from "Auto-review auto-routes only unambiguous hints."

### Why the On Findings column is `mosaictest-scripted (re-check)`

The engine's `isUnambiguousHint` function rejects any value containing a space, `(`, or `)`. The parenthesised form `agent (qualifier)` is plausibly what someone might write to annotate a findings target with a hint, so it is a realistic ambiguity rather than an artificial one.

### Why marker-gated instead of override

Unlike `deviation-blocked`, this workflow's first invocation returns `COMPLETED_NEEDS_ACTION` (not `BLOCKED`), and a CNA-returning agent legitimately writes output artifacts. The marker file is the output artifact `MosaicTestAmbiguousReport.md`, which doubles as the fixture's state transition. No script override is needed because the second invocation reads the same script but takes a different branch.

---

## Expected Run

Three Orchestration.md log rows. A consultation appears even though the mode is Auto-review — because the hint is ambiguous.

| Log `Seq` | `Agent` | Kind | `Phase` | `Status` | `Summary` shows |
|:---:|---|---|---|---|---|
| 1 | `mosaictest-scripted#1` | workflow step | RESEARCH | COMPLETED_NEEDS_ACTION | findings produced, marker written |
| 2 | `orchestrator-script#2` | consultation | — | SUCCESS | the re-dispatch task description |
| 3 | `mosaictest-scripted#3` | workflow step | RESEARCH | SUCCESS | marker present, echoed task description |

**Run outcome:** completed normally (`RunCompleted`). The engine routed the recovery dispatch's SUCCESS to COMPLETE via On Success.

**The proof:** A consultation row appears at Seq 2, even though the mode is Auto-review. The ambiguous On Findings hint forced the engine to deviate.

**Contrast with `findings-loop` Auto-review run:** That run has the same mode, the same status code, but an unambiguous On Findings — and produces NO consultation row. Together, the two workflows prove the engine distinguishes ambiguous from unambiguous.

**Note:** `mosaictest-review` (interval 3) may fire after Seq 3 if deployed. Account for an additional infrastructure row.

---

## What a Failure Means Here

| Observation | Where to look |
|---|---|
| No consultation row — run completes in 2 steps like `findings-loop` Auto-review | The engine is treating the ambiguous hint as unambiguous — `isUnambiguousHint` is broken |
| Consultation happens but names an unknown agent | The routing table is not reaching the consultant |
| Second dispatch returns COMPLETED_NEEDS_ACTION instead of SUCCESS | The marker file was not written by the first invocation |
| Run stops after Seq 1 with deviation unresolved | The orchestrator consultation path is not wired for this harness |

---

## Changelog

| Version | Date | Author | Summary |
|---------|------|--------|---------|
| 1.0 | 2026-08-17 | MOSAIC | Initial version. Proves ambiguous On Findings forces deviation in Auto-review. |

---

## Open Ideas / Dead Ends

**Ideas under consideration:**
- A variant with On Findings set to `-` (column present but empty value) to test `isUnambiguousHint`'s empty-value rejection path separately from the space/parens path.

**Dead ends (tried and rejected):**
- (none yet)
