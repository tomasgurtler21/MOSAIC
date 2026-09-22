---
version: "1.0"
name: "MosaicTest HITL Escalate Workflow"
description: "Runner mode fixture — HITL approval check reads human_approved: false from the output artifact, the Runner re-dispatches once, the second attempt also fails approval, and the Runner escalates to a deviation. The orchestrator stops the run. Proves the full approval-read-to-escalation path works end to end."
hint: "Mode 2 test — HITL approval failure, re-dispatch, escalation to deviation, orchestrator stop"
author: MOSAIC
id: hitl-escalate
referenced_agents:
  - mosaictest-scripted
artifacts:
  - MosaicTestScript/hitl-escalate.md
  - MosaicTestApproval.md
modes:
  - auto
---

<Workflow type="core" name="hitl-escalate" version="1.0">
## MosaicTest HITL Escalate Workflow

**Use when:** Verifying that the Runner reads `human_approved` from output artifacts when an agent returns `SUCCESS` with HITL=true, re-dispatches once on failure, and escalates to a deviation when the second attempt also fails approval.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| RESEARCH | mosaictest-scripted | TRUE | COMPLETE | - | MosaicTestScript/hitl-escalate.md | MosaicTestApproval.md |

**Notes:**
- **Run this workflow in Auto mode.**
- The script fixture uses stub enhancements E1 (`approval`) and E3 (`hitl_behaviour`), both implemented in `mosaictest-scripted`.
- E3 (`hitl_behaviour: proceed`) causes the stub to process the fixture normally instead of auto-refusing on `human_in_the_loop: true`. Without E3, the stub would return `BLOCKED`/`E503` and the HITL approval path would never be reached.
- E1 (`approval: false`) causes the stub to write `human_approved: false` in the output artifact. The Runner reads this after `SUCCESS` with HITL=true.
- Both invocations return `SUCCESS` with `human_approved: false`. The first triggers `HITLRedispatch`; the second triggers `HITLEscalate` (deviation).
- `On Success = COMPLETE` is never reached — the approval check intervenes before the routing decision.
- Seed `Fixtures/hitl-escalate` — the whole directory, not anything inside it.

</Workflow>

---

## Design Rationale

### Why this workflow exists

`hitl-glob-staged` exercises the `Status != SUCCESS` shortcut in `DecideHITLCompliance`, where the HITL check accepts immediately because the non-SUCCESS path already deviates. That shortcut means the approval-reading path — where the Runner reads `human_approved` from the output artifact — is never exercised by any E2E test.

This workflow covers the full decision table:
- `EffectiveHITL: true` + `Status: SUCCESS` + `!IsApproved()` + `!RedispatchUsed` → `HITLRedispatch`
- `EffectiveHITL: true` + `Status: SUCCESS` + `!IsApproved()` + `RedispatchUsed` → `HITLEscalate`

### Why E1 and E3 are required

Without E3, `mosaictest-scripted` auto-refuses on `human_in_the_loop: true` with `BLOCKED`/`E503`. A `BLOCKED` response takes the `Status != SUCCESS` shortcut in `DecideHITLCompliance`, so the approval-reading path is never entered. E3 overrides this behaviour so the stub returns `SUCCESS` even with HITL=true.

Without E1, the stub cannot write `human_approved: false` in the output artifact. The Runner would find either no file (treated as non-compliant — same outcome) or no `human_approved` field. E1 makes the fixture explicit: the stub writes a file that clearly says `human_approved: false`.

### Why the fixture is written now

The workflow file, fixtures, and expected data do not change when E1 and E3 are implemented. The stub enhancement is purely in `mosaictest-scripted`'s fixture parser — it learns to read two new fields. Writing the suite now means the implementation is a single-file change (the stub), not a coordinated multi-file effort.

### Why the orchestrator stops

The escalation deviation could be resolved by re-dispatching a different agent, but that adds complexity without additional HITL coverage. A stop is the simplest response that proves the escalation reached the orchestrator. The routing fixture uses `after mosaictest-scripted SUCCESS #2` to match the state after the second approval failure.

---

## Expected Run

Four Orchestration.md log rows.

| Log `Seq` | `Agent` | Kind | `Phase` | `Status` | `Summary` shows |
|:---:|---|---|---|---|---|
| 0 | `orchestrator-script#pre_consultation#1` | consultation | — | "" | pre-run consultation response |
| 1 | `mosaictest-scripted#1` | workflow step | RESEARCH | SUCCESS | hitl_behaviour=proceed, approval=false, wrote unapproved artifact |
| 2 | `mosaictest-scripted#2` | workflow step (HITL re-dispatch) | RESEARCH | SUCCESS | hitl_behaviour=proceed, approval=false, wrote unapproved artifact again |
| 3 | `orchestrator-script#3` | consultation | — | "" | stop — HITL escalation acknowledged |

**Run outcome:** `RunStoppedByConsultant`. The orchestrator stops the run after the HITL escalation deviation.

**Key observations:**
- Rows 1 and 2 both show `SUCCESS` — the stub returned SUCCESS both times (E3 prevented the HITL auto-refusal).
- No `COMPLETE` row — `On Success = COMPLETE` was never evaluated because the approval check intercepted before routing.
- Row 2 is a re-dispatch of the same row, not a new row — the `Phase` is still `RESEARCH`, the agent is the same.
- Row 3 proves the escalation reached the orchestrator as a deviation consultation.

---

## What a Failure Means Here

| Observation | Where to look |
|---|---|
| Row 1 shows BLOCKED instead of SUCCESS | E3 not implemented or not read — the stub auto-refused on `human_in_the_loop: true` |
| Run completes with COMPLETE after row 1 | E1 not implemented — the stub did not write the artifact, or the Runner did not read `human_approved`, so the approval check found nothing and accepted |
| Only one SUCCESS row, then consultation | `DecideHITLCompliance` escalated without re-dispatching first — the `RedispatchUsed` flag may be stuck at true |
| Three or more SUCCESS rows before consultation | The re-dispatch limit is not enforced — `RedispatchUsed` is not being set after the first re-dispatch |
| Row 3 never appears, run hangs or crashes | The escalation did not produce a deviation, or the deviation-to-orchestrator path is broken for HITL escalations specifically |
| The stub stops with "no matching rule" | The routing fixture does not cover `after mosaictest-scripted SUCCESS #2` — check the fixture's selector |

---

## Changelog

| Version | Date | Author | Summary |
|---------|------|--------|---------|
| 1.0 | 2026-09-20 | MOSAIC | Initial version. HITL approval failure, re-dispatch, escalation. Requires stub enhancements E1+E3. |

---

## Open Ideas / Dead Ends

**Ideas under consideration:**
- A variant where the first invocation writes `human_approved: false` and the second writes `human_approved: true`, proving the re-dispatch path succeeds when the agent fixes the output on retry.
- A staged variant with `Stage-*` output artifacts to combine glob expansion with approval reading.

**Dead ends (tried and rejected):**
- Using a marker gate to change behaviour between invocations. The marker gate distinguishes invocations, but both invocations need the same behaviour (SUCCESS with `human_approved: false`). A marker gate that changes the second invocation's approval to true would test re-dispatch recovery, not escalation — a different test case.
