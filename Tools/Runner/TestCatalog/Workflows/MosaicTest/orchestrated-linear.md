---
version: "1.0"
name: "MosaicTest Orchestrated Linear Workflow"
description: "Runner mode fixture — Orchestrated mode end to end. The orchestrator is asked before every step, dispatches the same row three times with three different task descriptions, then stops."
hint: "Mode 1 test — orchestrator consulted every step, task description delivery, stop instruction"
author: MOSAIC
id: orchestrated-linear
referenced_agents:
  - mosaictest-scripted
artifacts:
  - MosaicTestScript/linear-echo.md
---

<Workflow type="core" name="orchestrated-linear" version="1.0">
## MosaicTest Orchestrated Linear Workflow

**Use when:** Verifying that `mosaic-run` can execute a run in Orchestrated mode against a real harness — that it asks the orchestrator before every step including the first, carries the orchestrator's task description through to the subagent, and ends the run on a stop instruction.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| RESEARCH | mosaictest-scripted | ❌ | COMPLETE | - | MosaicTestScript/linear-echo.md | - |

**Notes:**
- **Run this workflow in Orchestrated mode only.** In Auto or Auto-review the engine routes the single row to `COMPLETE` without ever consulting the orchestrator, and the run proves nothing this workflow exists to prove.
- `On Success` is `COMPLETE` so that a mode confusion fails fast and harmlessly rather than looping.
- Non-staged. With no staged rows, admission short-circuits and no stage table is required.
- Seed `Fixtures/orchestrated-linear` — the whole directory, not anything inside it — as the single seed path.

</Workflow>

---

## Design Rationale

### Why one row, dispatched three times

The obvious shape for this test is three rows, one per step. It does not work, and the reason is worth recording.

When the orchestrator names an agent, the Runner resolves that name to a routing table row by taking the **first row whose agent matches**. With one stub agent in three rows, all three dispatches resolve to row 1, so every step is recorded under row 1's phase and artifacts no matter which row the orchestrator meant. The log would then show three identical rows and prove nothing about which one was dispatched.

One row removes the ambiguity entirely. Three dispatches of that row, each carrying a different orchestrator-written task description, exercise the same mechanism without depending on row addressing. Multi-row navigation belongs in `orchestrated-backjump`, which cannot be authored until row addressing for a repeated agent is settled.

### Why the subagent echoes its task description

The whole claim of Orchestrated mode is that the orchestrator writes a task description worth having and the subagent receives it. Nothing in a run shows this unless the subagent reports what it was given.

So the behaviour fixture directs the stub to echo the received `task_description` into its `status_message`. The three log rows then carry three different summaries, which is the only direct evidence that per-dispatch content crossed the harness intact. Without the echo, the three steps would be indistinguishable and the mode's central property would be assumed rather than observed.

### Why the routing fixture counts occurrences

The three dispatches differ only in their task description, so the orchestrator cannot tell them apart from the last agent and status alone — all three states are "after `mosaictest-scripted` SUCCESS". The occurrence qualifier distinguishes them, and the count comes from the execution log rather than from anything the stub remembers.

This is also where an unexpected extra consultation is caught: a fourth SUCCESS would match no rule, and the stub stops and says so instead of dispatching a fourth time.

---

## Expected Run

Six Orchestration.md log rows. The stop consultation is NOT logged in Orchestration.md — it surfaces as a `session.consult.stop` event in RunnerLogs and as the TUI/CLI exit message.

| Log `Seq` | `Agent` | Kind | `Phase` | `Status` | `Summary` shows |
|:---:|---|---|---|---|---|
| 1 | `orchestrator-script#1` | consultation | — | SUCCESS | the task description sent for step one |
| 2 | `mosaictest-scripted#1` | workflow step | RESEARCH | SUCCESS | step one's task description, echoed back |
| 3 | `orchestrator-script#3` | consultation | — | SUCCESS | the task description sent for step two |
| 4 | `mosaictest-scripted#2` | workflow step | RESEARCH | SUCCESS | step two's task description, echoed back |
| 5 | `orchestrator-script#5` | consultation | — | SUCCESS | the task description sent for step three |
| 6 | `mosaictest-scripted#3` | workflow step | RESEARCH | SUCCESS | step three's task description, echoed back |

**Run outcome:** stopped by the orchestrator (`RunStoppedByConsultant`), with the fixture's stop reason surfaced in the exit message. Not `COMPLETE` — in this mode the orchestrator ends the run, and the table's `On Success` column is never consulted.

**RunnerLogs verification:** The debug log must contain a `session.consult.stop` entry with `reason="MOSAICTEST-ROUTING-COMPLETE / three dispatches done as scripted / ending the run on fixture instruction"`.

### Two numbering observations to confirm on the first run

Both are recorded here as predictions from reading the Runner, not as settled expectations. If either differs, the difference is a finding about the Runner, not about this fixture.

1. **The consultation agent name.** Consultation rows are currently written under a hardcoded literal rather than the orchestrator actually consulted. The table above assumes that literal. Once `RUN-6` in `Requirements.md` is fixed, these rows should instead name this catalogue's stub orchestrator.

2. **`Seq` and the `#N` suffix disagree.** For consultation-routed dispatches the Runner numbers the log row from the global sequence but numbers the agent instance from a separate workflow-step counter. So `mosaictest-scripted#2` is expected on log row 4, not row 2. On the auto-routed path the two agree, which is why this only shows up in Orchestrated mode. Whether the divergence is intended is an open question — `agent_instance_id` is documented as running over a global counter, which is not what the consultation path does.

---

## What a Failure Means Here

| Observation | Where to look |
|---|---|
| Run refused at startup, "no workflow regions found" | The Runner cannot read a deployed orchestrator file (`RUN-1`) |
| First consultation fails or the run dies at its first decision | The code for asking the orchestrator what to run next is missing for this harness (`RUN-4`) |
| Consultation rejected as naming an unknown agent | The routing table is not reaching the consultant (`RUN-3`) |
| Consultation rejected as malformed, but the stub's reply looks correct | The reply is being parsed too strictly (`RUN-5`) |
| Summaries are identical across the three steps | The task description is not reaching the subagent, or the echo fixture is not being honoured |
| A fourth dispatch occurs, or the stub stops with "no matching rule" | The Runner consulted more times than the mode requires |
| Summaries differ but are mangled — wrong characters, truncation | The harness is not carrying dispatch content intact |

---

## Changelog

| Version | Date | Author | Summary |
|---------|------|--------|---------|
| 1.0 | 2026-08-17 | MOSAIC | Initial version. First workflow exercising Orchestrated mode. |

---

## Open Ideas / Dead Ends

**Ideas under consideration:**
- A variant whose fixture omits the rule for the third SUCCESS, to assert that the stub stops loudly on an unmatched state rather than improvising. Cheap, and it tests the safety property the whole fixture design rests on.

**Dead ends (tried and rejected):**
- Three rows, one per step. The Runner resolves an orchestrator-named agent to the first matching row, so all three dispatches would collapse onto row 1 and the log could not show which row was meant. See Design Rationale.
