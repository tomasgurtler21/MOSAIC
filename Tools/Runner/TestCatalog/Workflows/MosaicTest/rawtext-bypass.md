---
version: "1.0"
name: "MosaicTest Raw-Text Bypass Workflow"
description: "Runner mode fixture — Auto mode raw-text subagent bypass. The agent has no Communication Protocol injection and returns raw text. The Runner bypasses orchestrator consultation and redispatches once directly; the failed bypass counts as one of the three E501 attempts, the engine re-dispatches once more, and only then is the orchestrator consulted."
hint: "Mode 2 test — raw-text harness error, direct-redispatch bypass, consultation fallback"
author: MOSAIC
id: rawtext-bypass
referenced_agents:
  - mosaictest-wronganswer
artifacts: []
modes:
  - auto
---

<Workflow type="core" name="rawtext-bypass" version="1.0">
## MosaicTest Raw-Text Bypass Workflow

**Use when:** Verifying that a subagent returning raw text (no valid Communication Protocol response) triggers the Runner's direct-redispatch bypass before falling back to orchestrator consultation.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| RESEARCH | mosaictest-wronganswer | FALSE | COMPLETE | - | - | - |

**Notes:**
- **Run this workflow in Auto mode.**
- `mosaictest-wronganswer` has no Communication Protocol injection. It returns raw text, which the harness adapter cannot parse as a protocol response.
- The harness error triggers the Runner's direct-redispatch bypass (FR-13). The same agent is redispatched once without consulting the orchestrator.
- The bypass redispatch also returns raw text (the agent is structurally incapable of producing a valid response). The failed bypass is recorded as its own `BLOCKED`/`E501` workflow row and counts as the second of the three E501 attempts.
- The engine's E501 decision then re-dispatches the agent a third time. That attempt fails too, the budget is used up, so no further bypass is allowed and the Runner falls back to orchestrator consultation (FR-16).
- The routing fixture matches `after mosaictest-wronganswer BLOCKED #1` (the first consultation after the third `BLOCKED`/`E501` workflow row) and stops the run.
- Seed `Fixtures/rawtext-bypass` — the whole directory, not anything inside it.

</Workflow>

---

## Design Rationale

### Why a separate agent instead of a scripted mode

`mosaictest-scripted` always produces valid Communication Protocol JSON because its instructions include the full protocol spec. Making it produce invalid output would mean asking an LLM to deliberately violate its own instructions — non-deterministic and unreliable. `mosaictest-wronganswer` is structurally incapable of producing a valid response because it was never given the protocol spec, making the test deterministic.

### Why no input or output artifacts

The agent cannot read artifacts meaningfully (no protocol knowledge), and it never produces a valid response from which artifacts could be written. Declaring artifacts would add nothing to test coverage and would introduce a false dependency.

### Why the routing fixture stops the run

After the bypass and the engine's last E501 attempt both fail, the orchestrator is consulted. The agent is structurally incapable of producing valid output, so re-dispatching it from the routing fixture would create an infinite loop. Stopping is the only sensible fixture response, and it still proves the full path: harness error → bypass redispatch → bypass failure → engine re-dispatch → budget used up → consultation → stop.

### Why the BLOCKED occurrence selector is the matching rule

The original dispatch, the bypass redispatch and the engine's re-dispatch all fail at the harness level before a protocol response is parsed, but each is still recorded as its own accepted `BLOCKED`/`E501` workflow row (D1-D2: harness errors are ordinary accepted workflow outcomes, built by `domain.HarnessErrorResponse`). The orchestrator therefore sees `mosaictest-wronganswer` with `BLOCKED` status after the third attempt and matches `after mosaictest-wronganswer BLOCKED #1`, the same occurrence-counting mechanism `deviation-blocked` and `deviation-chain` use.

---

## Expected Run

Five dispatch-log entries (requests and responses): a pre-run consultation, three `mosaictest-wronganswer` invocations, and a stop consultation. Both consultations dispatch to the orchestrator but allocate no `Seq` and leave no `Orchestration.md` row — each harness error is recorded via `HarnessErrorResponse` as an accepted `BLOCKED`/`E501` workflow row, its own `Seq`.

| Log `Seq` | `Agent` | Kind | `Phase` | `Status` | `Summary` shows |
|:---:|---|---|---|---|---|
| 1 | `mosaictest-wronganswer#1` | harness error | RESEARCH | BLOCKED/E501 | raw text, no protocol response extractable |
| 2 | `mosaictest-wronganswer#2` | harness error (bypass) | RESEARCH | BLOCKED/E501 | raw text again, bypass exhausted |
| 3 | `mosaictest-wronganswer#3` | harness error (engine re-dispatch) | RESEARCH | BLOCKED/E501 | raw text again, E501 budget used up |

**Run outcome:** `RunStoppedByConsultant`, exit code 6. The routing fixture stops the run because the agent is structurally unable to produce a valid response.

**Key observations:**
- Three `mosaictest-wronganswer` invocations (Seq 1 to 3) prove the budget: the first is the original auto-routed dispatch, the second is the direct redispatch (the bypass) without an intervening consultation and counts as an attempt, the third is the engine's re-dispatch while one attempt was left. Each harness error is recorded as its own accepted `BLOCKED`/`E501` row (D2), so `current_state` reflects the third attempt's failure.
- The dispatch log shows an `orchestrator-script` consultation after Seq 3, proving the fallback to orchestrator consultation once the budget is used up. It leaves no `Orchestration.md` row and consumes no `Seq`.
- No consultation appears in the dispatch log between Seq 1 and Seq 3, proving neither the bypass nor the engine re-dispatch went through the consultation round-trip.
- No fourth invocation appears: the third attempt was a raw-text error too, but the bypass is only allowed while budget remains.

---

## What a Failure Means Here

| Observation | Where to look |
|---|---|
| Only one `mosaictest-wronganswer` invocation, then consultation | The bypass did not fire and the engine did not re-dispatch — the harness error is going straight to the consultation path |
| Only two `mosaictest-wronganswer` invocations, then consultation | The failed bypass is not recorded as its own `BLOCKED`/`E501` row or the engine does not re-dispatch with budget left |
| Four or more `mosaictest-wronganswer` invocations before consultation | The bypass is not counted against the E501 budget (a bypass after the third attempt, or a failed bypass without its own row) |
| A consultation appears in the dispatch log between two `mosaictest-wronganswer` invocations | The retry is not skipping the consultation round-trip — it is going through the existing deviation path instead of the direct-redispatch or engine path |
| Run completes with exit code 0 | `mosaictest-wronganswer` somehow produced valid protocol JSON — check the agent file for accidental protocol injection |
| The stub orchestrator stops with "no matching rule" | The `run-start` selector did not match — the harness-error rows may have been recorded with a status other than `BLOCKED`/`E501`, changing the state the orchestrator sees |
| An `orchestrator-script` row appears in `Orchestration.md` | Consultations must leave no row in the artifact; a consultation wrongly called `Store.Apply` or allocated a `Seq` |

---

## Changelog

| Version | Date | Author | Summary |
|---------|------|--------|---------|
| 1.0 | 2026-09-22 | MOSAIC | Initial version. Raw-text subagent bypass and consultation fallback. |

---

## Open Ideas / Dead Ends

**Ideas under consideration:**
- A variant where the routing fixture re-dispatches `mosaictest-wronganswer` from the consultation to test whether the bypass also fires on consultant-routed dispatches (FR-14 second call site). Deferred because the resulting `run-start` loop makes the routing fixture unable to distinguish consultations — this is better tested as a Go integration test.
- A variant that combines `mosaictest-wronganswer` with `mosaictest-scripted` in a two-row workflow, where the routing fixture re-dispatches to `mosaictest-scripted` after the wronganswer consultation, proving the run can recover and complete. This would change the exit code to 0 and add coverage for the recovery-after-bypass-failure path.

**Dead ends (tried and rejected):**
- Using `mosaictest-scripted` with a script that instructs it to return raw text. The agent's Communication Protocol injection means it always knows the correct JSON format, making the "return raw text" instruction unreliable across models. A separate agent without protocol injection is the deterministic approach.
