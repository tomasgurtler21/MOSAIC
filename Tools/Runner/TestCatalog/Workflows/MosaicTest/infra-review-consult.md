---
version: "1.0"
name: "MosaicTest Infrastructure Review+Consult Workflow"
description: "Runner mode fixture — Auto mode with a review-class infrastructure agent. After 3 workflow steps, INVOCATION_INTERVAL(3) fires the review agent. The Runner then does a follow-up routing consultation with the orchestrator, passing the review's status_message as last_status_message. Proves the review-to-orchestrator chain end to end."
hint: "Harness test — review trigger (INVOCATION_INTERVAL), post-review orchestrator consultation"
author: MOSAIC
id: infra-review-consult
referenced_agents:
  - mosaictest-scripted
artifacts:
  - MosaicTestScript/review-echo.md
modes:
  - auto
infrastructure_agents:
  - mosaictest-review
---

<Workflow type="core" name="infra-review-consult" version="1.0">
## MosaicTest Infrastructure Review+Consult Workflow

**Use when:** Verifying that a review-class infrastructure agent fires on its declared trigger and that the Runner follows up with an orchestrator routing consultation, passing the review's `status_message` as `last_status_message`.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| RESEARCH | mosaictest-scripted | FALSE | next | - | MosaicTestScript/review-echo.md | - |
| PLANNING | mosaictest-scripted | FALSE | next | - | MosaicTestScript/review-echo.md | - |
| DESIGN | mosaictest-scripted | FALSE | COMPLETE | - | MosaicTestScript/review-echo.md | - |

**Notes:**
- **Run this workflow in Auto mode.**
- **Infrastructure agents are not in `referenced_agents`.** The review agent (`mosaictest-review`) is deployed from the catalog based on its `infrastructure: review` frontmatter field and injected into the orchestrator's `<InfrastructureAgents>` region by the deploy tool. The workflow does not name it.
- Three rows are needed because `mosaictest-review` declares `INVOCATION_INTERVAL(3)` — the trigger fires after 3 workflow step completions since the review agent's last dispatch (or since run start).
- All three rows return unconditional SUCCESS, so no deviations occur before the trigger fires.
- After the review agent fires and returns, the Runner performs a **follow-up routing consultation** with the orchestrator. The routing fixture handles this consultation with a `stop` action.
- The `stop` proves two things: (1) the consultation happened at all, and (2) the orchestrator received the review's `status_message` as `last_status_message` in the consultation context.

</Workflow>

---

## Design Rationale

### Why three phases instead of three rows in one phase

Three distinct phases (RESEARCH, PLANNING, DESIGN) make the log easier to read than three rows in the same phase. Each row has a unique phase label, so the sequence is visually unambiguous. This is a cosmetic choice — the trigger evaluation counts workflow step completions regardless of phase.

### Why the orchestrator stops the run

After the post-review consultation, the orchestrator could dispatch another workflow step. But any additional dispatches would add noise — the test is about the review-to-consultation chain, not about what happens after. Stopping cleanly after the consultation makes the expected run short and deterministic.

### Why no routing fixture for the three SUCCESS steps

In Auto mode, SUCCESS with `On Success = next` or `COMPLETE` is handled by the engine — no orchestrator consultation needed. The routing fixture is only consulted for the post-review follow-up. If the orchestrator were consulted after a regular SUCCESS step (which would be a bug), the absence of a matching rule causes the stub to stop with "no matching rule", making the bug visible.

### What makes review different from checkpoint and commit

Checkpoint and commit classes produce markers that the Runner extracts mechanically (`[checkpoint:{sha}]`, `[branch:{name}]`). The review class produces observations — its `status_message` is passed to the orchestrator for a routing decision. This post-review consultation is the unique review-class feature, and this workflow tests it.

---

## Expected Run

Six sidecar dispatches total (what the sidecar records). The review trigger fires after the 3rd workflow step, followed by a routing consultation where the orchestrator stops.

**Important: Execution Log vs sidecar dispatches.** The Runner's Execution Log (inside `Orchestration.md`) records rows only for steps that complete the log-write path. The post-review routing consultation is handled by `consultRoute`, which returns on a stop instruction *before* writing an Execution Log row. Therefore the Execution Log has **5 rows** (Seq 0–4), not 6. The consultation still happens — it is visible as the 6th entry in the sidecar dispatch log — and the STOPPED run outcome is the evidence that it occurred.

| Log `Seq` | `Agent` | Kind | `Phase` | `Status` | `Summary` shows |
|:---:|---|---|---|---|---|
| 0 | `orchestrator-script#pre_consultation#1` | consultation | -- | "" | pre-run consultation response |
| 1 | `mosaictest-scripted#1` | workflow step | RESEARCH | SUCCESS | unconditional SUCCESS |
| 2 | `mosaictest-scripted#2` | workflow step | PLANNING | SUCCESS | unconditional SUCCESS |
| 3 | `mosaictest-scripted#3` | workflow step | DESIGN | SUCCESS | unconditional SUCCESS |
| 4 | `mosaictest-review#4` | infra trigger | -- | SUCCESS | canned review message |

**Run outcome:** STOPPED (stopped-by-consultant), exit_code 6. The orchestrator stops the run after receiving the review's observations. The stop consultation is the 6th sidecar dispatch but produces no Execution Log row.

**Key observations:**
- Seq 4 is the review infrastructure dispatch. `INVOCATION_INTERVAL(3)` fires because 3 workflow steps have completed since run start.
- After Seq 4, the Runner performs the post-review routing consultation with the orchestrator. This is the unique review-class behaviour — the Runner consulted the orchestrator with the review's `status_message` as `last_status_message`. This consultation is the 6th sidecar dispatch and is confirmed by the STOPPED run outcome, but it does not produce an Execution Log row.
- The consultation's `last_status_message` field contains the review's canned text: `MosaicTest infrastructure stub / class=review / declared trigger=INVOCATION_INTERVAL(3) / instance=mosaictest-review#4 / nothing inspected / returning SUCCESS`.
- No orchestrator consultation occurs for the three SUCCESS workflow steps — Auto mode handles those mechanically.
- The review row is flagged `IsInfrastructure=true` in the log and does not update `current_state`.

---

## What a Failure Means Here

| Observation | Where to look |
|---|---|
| Run completes with COMPLETE instead of STOPPED | The post-review consultation did not happen — the Runner did not consult the orchestrator after the review agent fired. Check ToolingGaps.md GAP-2. |
| Only 3 workflow steps in the log, no review row | `INVOCATION_INTERVAL(3)` did not fire — check that the review agent was deployed (ToolingGaps.md GAP-1) and that the trigger evaluator counts workflow step completions correctly |
| Review row appears but no consultation follows | The Runner dispatched the review agent but did not follow up with a routing consultation — the review-class special handling is missing from `evaluateTriggers` |
| Consultation appears but the run does not stop | The routing fixture's stop rule did not match — check the selector in MosaicTestRouting.md |
| Review fires after step 2 instead of step 3 | The invocation interval is counting wrong — it should fire after 3 completions, not 2. Off-by-one in the interval check. |
| Orchestrator consulted after a regular SUCCESS step | Auto mode should not consult for SUCCESS with `On Success = next` — the engine handles routing mechanically. A routing fixture rule matched unexpectedly, or the mode logic is broken. |
| Review row updates `current_state` | Infrastructure rows must not update `current_state`; the engine is treating an infra step as a workflow step |

---

## Changelog

| Version | Date | Author | Summary |
|---------|------|--------|---------|
| 1.0 | 2026-09-20 | MOSAIC | Initial version. Review trigger + post-review orchestrator consultation. |

---

## Open Ideas / Dead Ends

**Ideas under consideration:**
- A variant where the orchestrator dispatches a follow-up workflow step after the review, instead of stopping. This would test the full review-consult-continue path.
- A variant with INVOCATION_INTERVAL(1) so the review fires after every step, producing multiple review+consultation pairs.
- Combining review with checkpoint/commit in one workflow (all three infrastructure classes).

**Dead ends (tried and rejected):**
- Using PHASE_END instead of INVOCATION_INTERVAL. While §7.7 mentions PHASE_END as an option, INVOCATION_INTERVAL is what mosaictest-review actually declares in its frontmatter. The test should use the agent's declared trigger, not override it.
