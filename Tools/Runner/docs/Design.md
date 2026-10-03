# Runner Design

> **Status:** Draft
> **Created:** 2026-08-15
> **Last Updated:** 2026-09-27
> **Scope:** The design of `mosaic-run`, the CLI tool that executes orchestration workflows without a human orchestrator in the loop. Covers execution modes (how much routing intelligence the Runner handles autonomously versus delegating to a script-mode orchestrator agent), the architectural layers that implement those modes, the dispatch loop lifecycle, and the contract boundaries between the Runner and the systems it drives (harness adapters, orchestrator agents, the orchestration artifact).

---

## 1. Purpose

The MOSAIC orchestration system was designed around a human orchestrator — an LLM-powered agent that reads a workflow table, dispatches subagents, interprets their responses, and decides what to do next. The orchestrator runs as a persistent LLM session: it creates the orchestration artifact, dispatches a subagent, records the result, reads the response, dispatches the next subagent, records that result... and the context window grows with every iteration. By step 15, the orchestrator is re-processing all prior tool calls, artifact edits, and subagent responses from steps 1–14. Cost scales roughly quadratically with run length.

The orchestrator also does two kinds of work in that loop: **mechanical** work (editing the artifact, tracking sequence numbers, resolving artifact paths, invoking harness commands) and **intelligent** work (deciding which agent runs next, crafting a task description that tells the subagent what to do, handling deviations). The mechanical work is deterministic and does not benefit from LLM reasoning — but it consumes context window and inference cost as if it did.

The Runner (`mosaic-run`) separates these concerns. It takes over all mechanical work: reading workflow tables, invoking subagents through harness adapters, writing the orchestration artifact, tracking sequences, resolving artifacts. The intelligent work — routing decisions and task descriptions — is either handled by the Runner's deterministic engine (when the workflow table prescribes the answer) or delegated to a script-mode orchestrator agent invoked as a fresh, bounded-context session.

### 1.1 The Cost Model

The Runner's three execution modes (§2) represent different points on the cost-vs-quality spectrum:

| Approach | Orchestrator Invocations | Context Growth | Routing Judgment |
|----------|------------------------|----------------|-------------------------|
| **Current (no Runner)** | Every step, persistent session | Unbounded — grows with run length | Every step, in one long session |
| **Mode 1 (Orchestrated)** | Every step, fresh session each time | Bounded — always system prompt + Orchestration.md | Every step |
| **Mode 2 (Auto)** | Only on deviations | Bounded | Non-SUCCESS only; the engine follows the table on SUCCESS |
| **Mode 3 (Auto-review)** | Only on unresolvable deviations | Bounded | Unresolvable deviations only |

Mode 1 is not the "expensive" option — it is already dramatically cheaper than the current approach because each orchestrator invocation starts a fresh session (no context window growth) and all mechanical work is offloaded to the Runner. Modes 2 and 3 reduce cost further by eliminating most orchestrator invocations. The price is the orchestrator's judgment on the happy path, and they run only the workflow shapes the engine supports.

**Direction.** Modes 2 and 3 are the intended primary modes. Mode 1 is the fallback: it covers any workflow the script orchestrator can interpret, including table features the engine does not support yet (§1.3), and it remains useful beyond that.

### 1.2 The Dispatch Intelligence Gap

What the orchestrator adds beyond mechanical work has two layers.

**Routing judgment** (where the run must go when the table alone does not say):
- A reviewer's findings implicate upstream work, so the fix belongs to `requirements-refinement` rather than the `On Findings` creator
- A `NEEDS_CLARIFICATION` asks for codebase facts, so a research agent runs before the asking agent is re-dispatched
- A repeated failure or an exhausted review-loop limit calls for a stop, not another attempt

**Environment context** (facts that apply to EVERY invocation):
- Skills are located at `.claude/skills/` — read the relevant skill from there by name
- Use `py` not `python` for the Python interpreter
- Harness-specific quirks subagents should be aware of

Task descriptions are not a third layer. Both orchestrators follow a shared minimal rule: state what to accomplish, never how, and never reshape scope from domain content (the Routing Policy in `orchestrator.md` / `orchestrator-script.md`). An orchestrator-stated task therefore differs little from the Runner's generic one. Earlier drafts treated targeted, findings-quoting descriptions as Mode 1's main value, but in practice they over-directed subagents.

Mode 1 supplies both layers on every step. Modes 2/3 lose routing judgment on auto-routed steps and follow the table instead. That is their fundamental trade-off, and it is acceptable wherever the table already says the right thing.

The environment context gap is a different class of problem: a subagent that cannot find its skills or uses the wrong Python command fails for plumbing reasons unrelated to its task, and those failures are harder to attribute than task-logic failures. Pre-Consultation (§2.8) addresses this narrow layer with a one-shot orchestrator invocation at run start. It does not close the core dispatch intelligence gap — that remains the fundamental trade-off of Modes 2/3 — but it prevents a category of failure that is difficult to diagnose and easy to avoid.

### 1.3 What the Runner Is Not

The Runner is not a replacement for the orchestrator agent. The orchestrator agent holds domain knowledge (how to recover from a failed test run, when to skip a stage, how to interpret ambiguous findings). The Runner holds workflow-table knowledge (which row comes next, how stages are ordered, how execution groups work). The Runner replaces the orchestrator's *mechanical* work; the orchestrator retains its *intelligent* work and is invoked when judgement is needed.

The Runner is also not a workflow engine in the general sense. It executes MOSAIC workflow tables — a specific format with specific semantics. It does not interpret arbitrary DAGs, BPMN diagrams, or pipeline definitions.

The Runner currently invokes one workflow agent at a time. For an admitted staged workflow, work that a native orchestrator may dispatch concurrently is executed sequentially; this changes scheduling, not which admitted stages run. True concurrent workflow dispatch is deferred and tracked in `ROADMAP.md`.

**Admission is mode-dependent.** The engine routes in Modes 2 and 3, so admission there refuses table shapes the engine cannot interpret — currently including comma-separated multi-target `On Success` (parallel forks). In Mode 1 the engine never routes; it only builds requests for the agent the orchestrator names. Mode 1 therefore admits any table the script orchestrator can interpret, and refuses only what request construction itself needs, such as unresolvable agents or artifact templates. A Mode 1 fork runs its branches one after another: each branch is one routing decision, and the orchestrator judges the join (every `Waits For` branch `SUCCESS`) from the Execution Log. New workflow-table features therefore work in Mode 1 first; Modes 2/3 refuse them until the engine learns them. Mode 1 still refuses a fork branch that is the last row of its phase or execution group, because the last-row helpers answer by table position and such a branch has no join to end on; the refusal names the branch and its row.

---

## 2. Execution Modes

The Runner supports three execution modes that control how much routing intelligence is handled autonomously versus delegated to a script-mode orchestrator agent. The modes form a spectrum from full delegation to maximum autonomy.

### 2.1 Mode Overview

| Mode | Name | Engine Routes | Orchestrator Decides | Workflow Shapes | Cost vs. Current |
|------|------|---------------|---------------------|-------------------|-----------------|
| 1 | **Orchestrated** | Nothing | Everything | Any the orchestrator can interpret (§1.3) | Much cheaper (bounded context) |
| 2 | **Auto** | SUCCESS | Everything else | Engine-supported only | Cheapest for deviation-heavy runs |
| 3 | **Auto-review** | SUCCESS + creator/reviewer loops | Remaining deviations only | Engine-supported only | Cheapest overall |

All three modes are cheaper than the current approach (persistent orchestrator session with unbounded context growth). The modes differ in how much further they reduce cost, and what quality they trade for it.

### 2.2 Mode 1 — Orchestrated

**Routing rule:** The Runner never decides "what next." After every subagent invocation, the Runner invokes the script-mode orchestrator agent as a fresh session. The orchestrator reads Orchestration.md, makes a routing decision, and returns both the target agent AND a task description for the next dispatch.

**Why this is cheaper than the current approach:** The current orchestrator accumulates context across the entire run — every tool call, every artifact edit, every subagent response stays in the context window. In Mode 1, each orchestrator invocation starts fresh: the context is always just the orchestrator's system prompt + the compact Orchestration.md artifact. No growth. The Runner handles all mechanical work (artifact writes, harness invocations, sequence tracking) that previously consumed orchestrator context.

**What it adds over Modes 2/3:** The orchestrator judges every step. It can send work upstream when a finding implicates it, pick the agent that can answer a clarification, or stop. It states each task minimally, with the environment facts its instructions carry, and that remains useful guidance for the subagent. Because the engine never routes in Mode 1, it also runs workflow shapes Modes 2/3 do not support yet (§1.3).

**Engine role:** The engine is used only for dispatch construction (building `ProtocolRequest` objects, resolving artifact paths) — never for routing decisions. The routing decision, task description, and effective HITL all come from the orchestrator's instruction.

**When to use:**
- Fallback: workflows using table features the engine does not support yet (e.g., parallel forks), which Modes 2/3 refuse at admission
- Workflows with complex conditional routing that depend on artifact content (e.g., "skip the design phase if the research shows no architectural changes needed")
- When per-step routing judgment matters more than cost
- Developing or debugging the orchestrator agent (the Runner becomes a test harness for the orchestrator's routing logic)

**Dispatch loop:** See the unified loop in §3.1. In Mode 1, step 1 always consults the orchestrator — the engine is never asked for a routing decision. The orchestrator returns a dispatch instruction (agent, task description, and optionally artifact overrides and constraints) or `stop`. The Runner applies orchestrator-provided fields and falls back to table-row defaults for anything the orchestrator omits. See `ScriptOrchestratorContract.md` for the full schema.

**Difference from Modes 2/3:** In Modes 2 and 3, the engine makes routing decisions first and only falls through to the orchestrator on deviation. In Mode 1, there is no engine routing — the orchestrator decides everything and states every dispatch's task.

### 2.3 Mode 2 — Auto

**Routing rule:** The engine handles SUCCESS routing autonomously. All non-SUCCESS responses — including COMPLETED_NEEDS_ACTION from review agents — are delegated to the orchestrator.

**Engine role:** The engine reads the workflow table, determines the On Success target, resolves execution groups and stage ordering, and produces a `DispatchDecision`. For non-SUCCESS responses, the engine produces a `DeviationDecision`, and the session invokes the deviation resolver (orchestrator delegate or stop).

**Trade-off:** On the happy path (SUCCESS → next row), the Runner sends a generic task description and follows the table's On Success target without judgment. Orchestrator-stated descriptions are minimal by policy, so the description itself loses little. What is lost is the chance to route differently after a SUCCESS. Deviations still reach the orchestrator.

**When to use:**
- When happy-path subagents perform well with generic task descriptions (their instructions and artifact inputs are sufficient)
- When deviations require orchestrator judgement — review findings sometimes warrant more than routing back to the creator (e.g., escalating to the user, adjusting the plan, or stopping the run)
- When minimizing orchestrator invocations on the happy path matters more than per-step routing judgment

**Dispatch loop:** See the unified loop in §3.1. In Mode 2, step 1 asks the engine first. SUCCESS is auto-routed (generic task description). All non-SUCCESS — including COMPLETED_NEEDS_ACTION — produces a Deviation, which triggers orchestrator consultation (orchestrator-stated task description).

**Key difference from Mode 3:** COMPLETED_NEEDS_ACTION with an unambiguous On Findings target is NOT auto-routed. It produces a Deviation and goes to the orchestrator. The value is routing flexibility — the creator reads the review artifact regardless and derives its focus from there. What the orchestrator adds is the ability to override the On Findings target: when a reviewer's findings point to an upstream problem (e.g., "the contracts are wrong because the requirements are incomplete"), the orchestrator can route to `requirements-refinement` instead of `contracts-designer`. Mode 3 would blindly auto-route to the On Findings target, which may cause the creator to attempt a local fix when the real problem is upstream.

### 2.4 Mode 3 — Auto-review

**Routing rule:** The engine handles SUCCESS routing and creator/reviewer loop routing autonomously. Only truly ambiguous or unexpected situations are delegated to the orchestrator.

**Engine role:** Same as Mode 2, plus: when a review agent returns COMPLETED_NEEDS_ACTION and the workflow table row has an unambiguous On Findings target, the engine auto-routes back to the paired creator without invoking the orchestrator.

**Trade-off:** Review-loop re-dispatches are auto-routed too, with generic descriptions and table targets. The creator reads the review artifact, which is added to its inputs, to learn what to fix; creators are designed to read review feedback, so this usually works. What Mode 3 gives up relative to Mode 2 is the orchestrator's chance to route a finding upstream instead of back to the paired creator. The review loop limit (`review_loop_limit`) still applies: before auto-routing, the engine counts that reviewer's COMPLETED_NEEDS_ACTION iterations at the current phase and stage (a CNA row directly after a CNA row of the same reviewer is a re-dispatch and counts once). When the count reaches the limit, the engine produces a deviation instead of auto-routing. With no limit it never stops.

**When to use:**
- Maximum automation and minimum cost: the orchestrator is invoked only when something genuinely unexpected happens
- When review-loop routing is fully captured by the workflow table's On Findings column
- When subagents reliably derive their task from instructions + artifacts without orchestrator routing

**Auto-routed cases:**

| Subagent Status | Condition | Engine Action | Task Description | Artifacts |
|----------------|-----------|---------------|-----------------|-----------|
| SUCCESS | Always | Route to On Success target (or next EXECUTION row) | Generic | Table defaults |
| COMPLETED_NEEDS_ACTION | Row has unambiguous On Findings, review loop limit not reached | Route to On Findings target | Generic | Table defaults + review artifact added to input |
| COMPLETED_NEEDS_ACTION | Row has no/ambiguous On Findings, or review loop limit reached | Deviation → orchestrator | Orchestrator-stated | Orchestrator specifies |
| Any other non-SUCCESS | Always | Deviation → orchestrator | Orchestrator-stated | Orchestrator specifies |

**Row identification and the nearest-preceding rule:** The engine identifies the row that ran from the `WorkflowRow` value recorded in the Execution Log (the column directly after `Stage`, matching the table's `Row` column), not from seq arithmetic or invocation counts. Infrastructure, out-of-band and ad-hoc steps record `-`. Live routing and resume read the last row from this value and stop and report when the agent or group at that row no longer matches. The table's `Row` column is added by mosaic-deploy; the Runner accepts tables with or without it and refuses one whose `Row` numbers do not match row positions. The On Findings target resolves to the nearest row above the row that ran whose agent is the target: the row that ran is not a candidate, and group and stage boundaries are ignored. A target with no preceding row is treated as no target, which produces a deviation. A run is pinned to its workflow only by `workflow_version`, and the table is re-read on every start including resume, so authors bump the version on every table edit; a mismatch refuses resume unless version drift is allowed.

**Review artifact injection (COMPLETED_NEEDS_ACTION auto-routing):** When the engine auto-routes back from a reviewer to the On Findings target (typically the paired creator), it adds the reviewer's output artifact to the target's `input_artifacts`. The table row for the creator lists the creator's normal inputs — it does not anticipate review loops. The engine knows which artifact the reviewer produced (from the table row's Output column) and adds it to the creator's input set. This ensures the creator can read the review findings without the orchestrator having to specify it manually.

**Creator/reviewer pair recognition:** The engine does not need to understand the `-review` naming convention from `OrchestrationSemantics.md`. It operates purely on the workflow table's On Findings column: if the column is present, non-empty, and contains a plain agent identifier (no spaces, no parentheses), the target is unambiguous and the engine routes to it. The semantic meaning (that this is a creator/reviewer pair) is established by the workflow author when they write the table; the engine just follows the instruction.

### 2.5 The Task Description Spectrum

Task descriptions differ little across modes, because orchestrator-stated descriptions are minimal by policy. Pre-consultation (§2.8) adds environment facts to auto-routed ones. To make this concrete:

**Mode 1 dispatch (orchestrator-stated, minimal + environment):**
```
Revise ContractsDesign.md to resolve the findings recorded in
contracts-review.md.
Skills are at .claude/skills/ — read the relevant skill by name. Use `py`
not `python`.
```

**Mode 2/3 dispatch WITH pre-consultation (generic + environment, default):**
```
Proceed with your task.
Skills are at .claude/skills/ — read the relevant skill by name. Use `py`
not `python`.
```

**Mode 2/3 dispatch WITHOUT pre-consultation (--pre-consult=false):**
```
Proceed with your task.
```

The generic message is deliberately minimal. The subagent already has its instructions (which define its role and scope), its `input_artifacts` list (which tells it what to read), and its `output_artifacts` list (which tells it what to produce) — the Runner has no domain understanding to add. What Modes 2/3 give up is routing judgment, not description content.

Pre-consultation adds environment plumbing on top of the generic content.

### 2.6 The Single-Decision Principle

Regardless of mode, the orchestrator makes exactly ONE routing decision per invocation. It reads Orchestration.md, decides what should happen next, and returns. The Runner executes that decision, records the result in the artifact, and — if needed — invokes the orchestrator again for the NEXT decision.

This applies uniformly:
- **Mode 1 happy path:** Orchestrator reads artifact → "dispatch contracts-designer with this task" → Runner executes + records → orchestrator reads updated artifact → "dispatch contracts-review" → ...
- **Mode 2/3 deviation:** Engine can't route → orchestrator reads artifact → "dispatch contracts-designer to fix the issues" → Runner executes + records → engine can now route (SUCCESS) → continues autonomously
- **Complex deviation chain:** Engine can't route → orchestrator reads artifact → "try codebase-research first" → Runner executes + records → orchestrator reads artifact → "now dispatch test-writer-tdd" → Runner executes + records → engine can route → continues

The orchestrator never resolves a full deviation chain internally. It never invokes agents itself. It makes one decision, returns, and is consulted again if needed. Every intermediate step is executed by the Runner and recorded in the execution log.

**Why this matters:**

| Property | Single-decision | Multi-step resolution |
|----------|-----------------|----------------------|
| Orchestrator context | Bounded: system prompt + Orchestration.md, always | Grows within the resolution chain (same cost problem the Runner exists to solve) |
| Infrastructure triggers | Fire after each step, even during deviation resolution | Don't fire: orchestrator dispatches agents directly, Runner's trigger evaluation is bypassed |
| Contract complexity | Simple: orchestrator returns ONE instruction | Complex: needs `rejoin_after_custom`, chain orchestration logic |

Note: the real orchestrator already maintains Orchestration.md in real time during deviation resolution — it logs every dispatch and updates current_state properly. The single-decision model does NOT improve audit trail or resumability over the current approach. Its value is cost (bounded context) and infrastructure integration (triggers fire).

The orchestrator does lose cached attention state between invocations — a multi-step recovery plan held in KV cache is discarded. But this is minor: when the orchestrator reads the updated artifact next time, the execution log shows what it already tried, and it will near-certainly derive the same continuation. If it doesn't, it's because the intermediate result genuinely changed the picture.

The multi-step resolution model is documented as a dead end in §7.4.

### 2.7 Mode Selection

The mode is selected at run start. Both interaction surfaces — the CLI (non-interactive) and TUI (interactive) — expose the same mode selection with identical semantics. The flag/option name and the interaction with `--on-deviation` are captured in Open Items (see §7).

### 2.8 Pre-Consultation

Pre-consultation is enabled by default for Modes 2 and 3. At run start, before the dispatch loop, the Runner invokes the orchestrator agent once and receives generic environment-level strings to append to every subsequent dispatch. The default is on because a subagent in Modes 2/3 that lacks the orchestrator's environment context can fail for plumbing reasons unrelated to its task — skills not found, wrong interpreter command, harness quirks not accounted for — and those failures are difficult to attribute without the missing context. The cost of the single extra orchestrator invocation at run start is small relative to a full run with stale or absent guidance. Pre-consultation can be disabled with `--pre-consult=false`.

**Invocation context:** Pre-consultation is a distinct invocation context from routing consultation (§2.9). The orchestrator returns field-keyed strings (§2.8's response shape), not a routing instruction (`dispatch`/`stop`). The contract document defines both response schemas under their respective invocation contexts.

**What it fixes:** Environmental plumbing — project-specific conventions, tool configurations, harness quirks, and other generic facts that apply identically to all subagents. Without pre-consultation, a subagent in Modes 2/3 might fail because it doesn't know a project-specific convention that the orchestrator's deployed instructions carry. Pre-consultation does NOT add routing judgment — that remains the fundamental trade-off of Modes 2/3.

**How it works:**

1. After the artifact is created or resumed and any required commit setup completes, but before the workflow dispatch loop, the Runner invokes the orchestrator via the normal `HarnessAdapter`
2. The orchestrator extracts explicit environment facts from its deployed instructions that apply to every auto-routed subagent, without rationale or duplication; it returns an empty object when none exist
3. The Runner stores these strings in session state
4. On every subsequent dispatch, the Runner appends the stored strings to the corresponding fields of the `ProtocolRequest` (e.g., appending to `task_description`, appending to `constraints`)
5. The Runner never interprets the content — it appends mechanically

**What the orchestrator produces (example):**

```yaml
task_description: |
  Skills are located at .claude/skills/ in the project root.
  When running Python, always use `py`, never `python`.
```

**Key design decisions:**

- **Uses the real orchestrator, not a dedicated advisor agent.** The orchestrator already has all the context through deployment. A separate agent would need the same context assembled and passed explicitly — duplicated context with drift risk.
- **Generic, not per-agent.** The orchestrator doesn't know agent internals. It knows environment facts (paths, command aliases, harness quirks) that apply equally to all subagents. The output is flat strings, not a per-agent map.
- **On by default, hard failure on error.** If pre-consultation fails, the Runner stops before any workflow dispatch and retains the already-created or resumed artifact for inspection and resume; resuming retries pre-consultation. The coordinator call remains in Runner diagnostic/MOSAIC logs but creates no Execution Log row and changes neither `global_sequence` nor `current_state`. Silent degradation — proceeding without environment guidance — is worse than stopping, whether the feature was explicitly requested or merely defaulted on. Pre-consultation can be disabled with `--pre-consult=false`.
- **Not cached across runs.** Project context, CLAUDE.md, and harness injections can change between runs. One extra LLM invocation at startup is cheap relative to a full run with stale guidance.

**Mode interaction:**

| Mode | Pre-consultation value |
|------|----------------------|
| 1 — Orchestrated | Unnecessary — orchestrator already includes environment context in every dispatch it states |
| 2 — Auto | Provides environment context for auto-routed dispatches; adds no routing judgment |
| 3 — Auto-review | Provides environment context for all auto-routed dispatches; adds no routing judgment |

### 2.9 Orchestrator Consultation

There is no separate "deviation resolver" concept. Consulting the orchestrator is a single operation used identically across all modes:

- **Mode 1:** Called after every step — "what's next?"
- **Modes 2/3:** Called when the engine cannot determine the next step from the workflow table — "the engine can't decide, what's next?"

The call is the same either way: invoke the orchestrator agent as a fresh session via the harness adapter, it reads Orchestration.md, and it returns one of two instructions:

- **Dispatch:** `{agent, task_description, constraints?, input_artifacts?, output_artifacts?, hitl_override?}` — execute this routing-table agent next, with this task description. Optional fields may override artifact and constraint defaults; `hitl_override: true` adds HITL, while `false` applies an explicit user waiver recorded in Workflow Notes. When omitted, the Runner uses table defaults and Plan resolution.
- **Stop:** `{reason}` — end the run.

This is the complete action vocabulary. The orchestrator never returns a multi-step plan, never assigns sequence numbers (that's the Runner's mechanical work), and never invokes agents itself. It names an agent, describes the task, optionally adjusts the artifact set and constraints for this specific invocation, and the Runner does the rest.

**Free table navigation:** The orchestrator can dispatch any agent in the routing table, regardless of the current position. This is normal operation, not an exception — a reviewer may find upstream problems (wrong contracts, incomplete requirements, bad plan), and the orchestrator routes back to wherever the fix belongs. The Runner looks up the named agent in the table, finds the corresponding row, and builds the `ProtocolRequest` — applying orchestrator-provided fields (task description, artifacts, constraints, HITL) where specified, falling back to the table row's defaults where not. Once that invocation's outcome is accepted, `current_state` takes the dispatched row's position, even if that means jumping backward to an earlier phase.

**Recording:** Orchestrator consultations are turns of the run's coordinator, not protocol subagent invocations. They do not consume `global_sequence`, create Execution Log rows, or update `current_state`. Runner diagnostic/MOSAIC logs record each consultation call for troubleshooting. Conclusions and decisions a later consultation needs are appended by the script orchestrator to Workflow Notes, using the artifact's current `global_sequence` to associate the note with the most recent completed recorded invocation (`0` during run initialization).

The orchestrator never needs to know whether it's being consulted for routine routing (Mode 1) or because something went wrong (Modes 2/3). It reads the artifact, sees the current state, and decides. The distinction is the Runner's concern, not the orchestrator's.

---

## 3. Dispatch Loop Lifecycle

The Runner has ONE dispatch loop. The mode determines who makes the routing decision at step 1 — the engine or the orchestrator — but everything else is identical.

### 3.1 The Unified Loop

```
┌─────────────────────────────────────────────────────┐
│ 1. DECIDE NEXT STEP                                 │
│                                                     │
│    Mode 1:  Always consult orchestrator              │
│    Mode 2:  Engine first; orchestrator if Deviation  │
│    Mode 3:  Engine first (incl. On Findings);        │
│             orchestrator if Deviation                │
│                                                     │
│    Result: {agent, task_description} or stop/complete│
├─────────────────────────────────────────────────────┤
│ 2. BUILD REQUEST                                    │
│    Engine resolves artifacts, assigns sequence       │
│    number. Fields come from:                         │
│    Orchestrator-routed:                              │
│      task_description, constraints from orchestrator │
│      input/output artifacts: orchestrator if         │
│        specified, else table row defaults            │
│      HITL: table/Plan, plus add or recorded waiver   │
│    Auto-routed (Modes 2/3):                          │
│      task_description: generic + pre-consultation    │
│      input/output: table defaults (+ review artifact │
│        on CNA auto-route back)                       │
│      HITL: table + Plan resolution                   │
├─────────────────────────────────────────────────────┤
│ 3. INVOKE SUBAGENT                                  │
│    Harness.Invoke(agent, request) → response        │
│    Harness error → treat as deviation, back to 1    │
├─────────────────────────────────────────────────────┤
│ 4. VERIFY HITL GATE                                 │
│    Only when this invocation was dispatched with     │
│    human_in_the_loop: true. Read human_approved      │
│    from each output the invocation wrote (§3.5)     │
│    Rejected attempt: log it without current_state     │
│    or Artifacts updates, then re-dispatch             │
├─────────────────────────────────────────────────────┤
│ 5. RECORD ACCEPTED OUTCOME                          │
│    Store.Apply(state, completedStep)                │
│    → Execution log row appended                     │
│    → current_state updated                          │
│    → global_sequence bumped                         │
├─────────────────────────────────────────────────────┤
│ 6. INFRASTRUCTURE TRIGGERS                          │
│    Evaluate checkpoint/commit/etc. triggers          │
│    May dispatch infrastructure agents (each recorded)│
├─────────────────────────────────────────────────────┤
│ 7. STAGE REFRESH                                    │
│    If output artifacts contain Stage-*, re-read      │
│    Plan.md for refreshed stage set                   │
├─────────────────────────────────────────────────────┤
│ 8. LOOP                                             │
│    Back to step 1 with updated state                │
└─────────────────────────────────────────────────────┘
```

### 3.2 Step 1 in Detail: Routing Decision

Step 1 is the ONLY step that varies by mode. Everything else is identical.

**Mode 1 — Always orchestrator:**
The orchestrator is invoked as a fresh session. It reads Orchestration.md, decides what's next, and returns `{agent, task_description}` or `stop`. On the first iteration of a new run, the artifact has no prior subagent result — the orchestrator reads the fresh artifact and decides the first step.

**Modes 2/3 — Engine first, orchestrator on deviation:**
The engine is called with the current state and last response. Three outcomes:
- **Dispatch** → proceed to step 2 (with generic task description)
- **Complete** → end run
- **Deviation** → consult orchestrator (same call as Mode 1), proceed to step 2 with orchestrator-stated task description
- **Stop** → end run (precondition failure)

The mode only affects WHICH deviations reach the orchestrator: Mode 2 sends all non-SUCCESS (including COMPLETED_NEEDS_ACTION); Mode 3 auto-routes COMPLETED_NEEDS_ACTION with unambiguous On Findings before producing a Deviation.

**Deviation chains resolve naturally:** If the orchestrator-dispatched agent's result is itself a deviation (Modes 2/3), step 1 simply consults the orchestrator again on the next iteration. No special chain logic. Each step is recorded, triggers fire, and the orchestrator reads the updated artifact each time.

### 3.3 Harness Errors

A harness-level error (timeout, crash, malformed output) at step 3 is fed back into step 1 as a deviation. The session constructs a synthetic response with `StatusCode=BLOCKED`, `ErrorCode=E501` (`TOOL_UNAVAILABLE`: the harness is the external tool that failed), and the error message as both status message and error reason. It records the response in the artifact, and the next iteration's routing decision accounts for it. The code matters because the shared Routing Policy picks its recovery tier by `error_code`: `E501` retries the same agent within the Repeated Failures cap and then escalates. The consultation request carries the description in `last_error_reason` (`ScriptOrchestratorContract.md` §3.2). *Not yet implemented: the synthetic response currently carries no error code.*

### 3.4 Stop Handling

When the routing decision at step 1 is `stop` (from the orchestrator or from the engine's precondition failure), the Runner's behavior depends on the interaction surface:

**CLI:** Terminal. The Runner prints the stop reason, exits with a non-zero code. The artifact is left in its current state — resumable if the underlying issue is fixed.

**TUI:** The Runner presents the stop reason and offers two actions:

| Action | What it does |
|--------|-------------|
| **Retry** | Re-invoke the orchestrator (or engine) with the same state. Useful when the user fixed something externally (environment config, missing dependency, network issue) and wants the orchestrator to reconsider. |
| **Manual dispatch** | The user takes over as deviation resolver for this one decision — picks the target agent and writes the task description. The run then continues normally (the next routing decision goes back to the configured resolver). This is the same mechanism as manual deviation resolution (§7, resolved items) but triggered on-demand rather than configured at run start. |

The stop action on the wire (`ScriptOrchestratorContract.md` §4.2) is the same regardless of surface — the Runner's reaction to it is surface-specific behavior.

### 3.5 HITL Gate Verification (Step 4)

`CommunicationProtocol.md` §9.7 makes verifying the human-in-the-loop gate an obligation of the party that dispatched the invocation. In Runner mode that party is **the Runner**, not the orchestrator agent: the orchestrator returns a routing instruction and cannot re-dispatch, so leaving the check to it would leave it undone. The check's semantics and the response to a gate that was not discharged are the protocol's — this section records only that the Runner owns them here, so the obligation is not read as unassigned.

What the Runner does:

- **Reads `human_approved`** from the frontmatter of each output artifact that an invocation dispatched with `human_in_the_loop: true` created or modified, whatever status it returned except `BLOCKED` with `E503`, which is routed directly because it already reports that the gate could not run — the same set recorded in the Artifacts registry, with wildcards expanded. A listed output that does not exist or that the invocation left unchanged is not checked. The read distinguishes the failure conditions — unreadable, no frontmatter, malformed value, field absent — rather than collapsing them into "not approved", and it never errors the run itself.
- **Refuses to start** a run whose workflow requires HITL when the interaction surface cannot read approvals at all. A surface that cannot verify the gate cannot honour it, and discovering that mid-run would leave a run that believed it was gated and was not.
- **Verifies before accepting the workflow outcome.** A discharged gate proceeds to the ordinary record step, which updates `current_state` and the Artifacts registry. An undischarged attempt is appended to the Execution Log and advances sequence/time, but leaves both structures unchanged before the protocol-required re-dispatch. Infrastructure triggers do not run for that rejected attempt.
- **Routes on the original outcome after a repaired gate.** When the re-dispatch discharges the gate and returns `SUCCESS`, the Runner routes on the original invocation's status and error code and records them as `current_state.last_status` / `error_code`, with `last_agent` naming the re-dispatch (protocol "Routing after the re-dispatch"). Any other re-dispatch status is routed as returned. *Not yet implemented: the Runner currently routes on the re-dispatch's response.*

This is the only place the Runner inspects artifact content beyond the orchestration artifact, and it reads frontmatter only — the same narrowing the protocol applies to an LLM orchestrator, for the same context-discipline reason.

---

## 4. Run-Start Sequence

The run-start sequence validates configuration and compatibility preconditions before creating the artifact. When commits are enabled, commit setup is the first invocation after creation so its outcome is recorded like every other invocation. A setup failure stops the run but leaves an inspectable artifact rather than erasing the failure from the shared run record.

| Step | What | Failure → |
|------|------|-----------|
| 1 | Load orchestrator file, extract selected workflow region | Refusal |
| 2 | Parse routing table from workflow region | Refusal |
| 3 | Read existing artifact (if resuming) or verify none exists (if new) | Refusal |
| 4 | Admit workflow for the selected mode (compat checks, execution group resolution; engine-routing shape checks in Modes 2/3 only — §1.3) | Refusal |
| **4b** | **Recovery check: scan for orphaned `.agents-backup/` directory next to the agents directory; restore originals if found and no active runs are detected** | **Refusal if restore fails** |
| 5 | Resolve every agent identifier to a definition file | Refusal |
| **5b** | **Create snapshot (copy-and-invoke) or backup and transform originals (backup-and-transform), per harness strategy** | **Refusal** |
| 6 | Read stage set from Plan.md (if present) | Refusal if parse error; absence is normal |
| 6b | Enumerate declared infrastructure agents | Refusal |
| 6c | Validate per-class agent selections (gated classes: one active per class) | Refusal |
| 7 | Settle run configuration (checkpoints, commits, commit variant) | Refusal if precondition fails |
| 7a | Build and validate seed plan (new runs only) | Refusal |
| 8 | Create (new) or resume (existing) artifact | Failure |
| 8a | Commit setup dispatch when commits are enabled and `commit_branch` is absent | Stop with the attempt recorded if setup fails |
| 8b | Pre-consultation in Modes 2/3 when enabled | Stop before workflow dispatch; retain the artifact for inspection and resume |

### 4.1 Run Configuration (Step 7)

The Runner collects three configuration decisions that mirror what the human-driven orchestrator asks the user at run start. Both interaction surfaces (CLI and TUI) must support all three; the CLI uses flags, the TUI uses interactive prompts.

**Checkpoints** (`enabled` / `disabled`):
- Asked unconditionally — even when no checkpoint-class agent is declared. A user who wanted rollback capability and is told this deployment cannot provide it has learned something useful while they can still choose a different deployment.
- Precondition: `checkpoints: enabled` requires at least one `Class = checkpoint` agent in the infrastructure declaration region. `Class = commit` and `Class = restore` do not satisfy this — a commit agent writes to the user's history (not restorable), and a restore agent only consumes checkpoints (doesn't create them).

**Commits** (`enabled` / `disabled`):
- Asked only when a `Class = commit` agent is declared in the infrastructure region. If no commit-class agent exists, commits default to `disabled` silently — the capability doesn't exist in this deployment, so there is no choice to make.
- Precondition: `commits: enabled` requires at least one `Class = commit` agent.

**Commit branch variant** (when commits enabled):

| Variant | Where stage commits go |
|---------|----------------------|
| **MOSAIC-owned** (recommended) | A branch created for this run (`mosaic/run/{run_id}`), merged by the user when satisfied |
| **User's own** | The branch the user is already on |

MOSAIC-owned is recommended because an abandoned stage on a run-owned branch can be discarded cleanly, while on the user's own branch the failed attempt and its undo both stay in history permanently.

**Review loop limit** (positive integer or no limit):
- Asked at run start in both surfaces, suggesting `3`, and recorded as the set-once `review_loop_limit` frontmatter field; no limit omits the field. Resume reads it and never asks again. Both orchestrators' shared Routing Policy and the Mode 3 engine escalate instead of routing a reviewer's `COMPLETED_NEEDS_ACTION` back once that reviewer has hit the limit at the current phase and stage.

**Gated infrastructure selections** (`checkpoint`, `commit`, `restore`):
- When a gated class has more than one declared agent, the user selects exactly one at run start. A class with exactly one declaration is auto-selected and needs no stored entry.
- The Runner records each required class-to-agent choice in the artifact's set-once `infrastructure_selections` map. On resume it reads and validates that map instead of asking again; an absent, unavailable, or reclassified required selection refuses the resume rather than choosing by declaration order.
- Non-gated classes such as `review` are not selected; all their declarations remain active.

**Runner-owned settings** use optional `runner_*` frontmatter fields in the shared orchestration artifact: `runner_mode`, `runner_pre_consultation`, and `runner_manual_resolution`. A Runner-created run writes them at creation. On first Runner resume of a native-created artifact that omits them, the user supplies the settings and the Runner records them once; later Runner resumes reuse them. Native orchestration preserves these fields but does not act on them. `commit_branch_variant` is not stored: the artifact schema defines the variant from `commit_branch`, so a second field would be free to disagree with the destination it describes.

### 4.2 Commit Setup Dispatch (Step 8a)

When `commits: enabled` and the artifact has no `commit_branch`, the Runner dispatches the commit-class agent as an out-of-band invocation after creating or loading the artifact. This setup dispatch establishes the target branch — for MOSAIC-owned, the agent creates it; for user's-own, the agent reports the current HEAD branch. The same condition applies on resume, so an artifact retained after failed setup retries setup before any workflow agent can run.

The setup dispatch returns the branch name in a `[branch:{name}]` marker at the end of its `status_message`. The Runner extracts this and records it as `commit_branch` in the artifact frontmatter. If the marker is missing or the dispatch fails, the run stops before its first workflow invocation and retains the setup attempt in the artifact — proceeding without a known branch destination would risk committing to the wrong place.

The setup dispatch is an ordinary invocation: it receives the next sequence number, returns a standard protocol response, and gets an Execution Log row whether it succeeds or fails. On a new run it is sequence 1. Append the row before writing `commit_branch`, following the artifact's normal log-first ordering. A successful response with a readable branch marker sets `commit_branch` and permits the workflow loop to begin. A non-success response or missing marker leaves `commit_branch` absent, keeps the setup row and advanced sequence in the artifact, and stops the run with the setup reason. `current_state` remains unchanged because setup is an out-of-band non-workflow invocation.

This ordering matches native orchestration and preserves one audit rule across executors: once a protocol invocation has occurred, its outcome exists in `Orchestration.md`. Configuration or compatibility refusal still happens before artifact creation; commit setup is not a precondition check but an agent invocation whose failure belongs in the run record.

Setup is dispatched by explicit instruction, not by a trigger, so the `STAGE_END`-only restriction on the commit class does not apply to it.

### 4.3 Resume

On resume, the session first requires a valid artifact `run_id` matching the enclosing `Orchestration-{run_id}/` folder. An absent, empty, malformed, or mismatched value refuses the resume; the Runner never mints identity for an existing artifact. It then computes a `ResumePoint` from the execution log. If the trailing workflow row does not match `current_state.last_agent`, the row is an unaccepted or interrupted attempt: the session preserves the prior accepted `current_state` and re-dispatches that workflow assignment rather than routing on its recorded status. Run configuration — including checkpoints, commits, commit branch, and any gated `infrastructure_selections` — is read from the existing artifact's frontmatter; it was set at original run start and is never modified.

---

## 5. Infrastructure Agent Integration

Infrastructure agents are declared outside workflow tables and reached either by automatic trigger evaluation or explicit dispatch. Checkpoint, commit, and review agents may fire automatically according to their declarations; restore agents are manual and never participate in automatic trigger evaluation. All are declared in the orchestrator file's infrastructure agent region.

**No-cascades rule:** Infrastructure agent completions do not trigger further infrastructure evaluations. Only workflow step completions trigger the evaluation pass.

**Trigger types:**

| Trigger | Fires When |
|---------|-----------|
| `INVOCATION_INTERVAL` | Updated `global_sequence` minus this agent's most recent Execution Log `Seq` is at least N; when it has no prior row, `global_sequence` is at least N. Counts globally allocated invocations, not only workflow steps |
| `STAGE_END` | The completed step is the last step of its stage and returned `SUCCESS`, accepted by HITL verification where it applies. Any other status, including a reviewer's `COMPLETED_NEEDS_ACTION` loop-back, does not fire |
| `PHASE_END` | The completed step is the last step of its phase and returned `SUCCESS`, accepted by HITL verification where it applies; for the EXECUTION phase this means the last step of the last stage (phase-wide scope, not per-stage) |
| `MANUAL` | Never fires automatically; dispatched by explicit instruction only |

**Recording:** Infrastructure steps are recorded in the execution log like any other step, but they do not update `current_state`. The artifact's recorded workflow position always names the last workflow step, ensuring the engine's row-lookup stays correct.

---

## 6. Runner Agent Snapshot

Regular orchestration and Runner execution have different deployment requirements. In regular orchestration, the orchestrator agent (`mode: primary`) spawns subagents via the harness's task/subagent tool, and the subagent's `mode: subagent` frontmatter enforces isolation. The Runner invokes every agent via the harness CLI (`opencode run --agent <id>`, `claude --agent <path>`, etc.), and some harnesses block CLI invocation of agents marked `mode: subagent`. OpenCode is the first harness with this constraint, but others may follow.

### 6.1 The Problem

The deployed agents directory (e.g., `.opencode/agents/`) serves both interactive orchestration and the Runner. These two execution models have conflicting requirements for the same agent files. The Runner needs transformed copies (e.g., `mode: primary` instead of `mode: subagent`), but the regular agents must remain unchanged for interactive orchestration.

Harnesses differ fundamentally in how they load agent definitions:

| Harness | Agent loading | Mechanism |
|---------|-------------|-----------|
| Claude Code | **Path-based** | `--append-system-prompt-file <path>` — loads the file at the given path directly |
| OpenCode | **Name-based** | `--agent <name>` — resolves the name from its canonical agents directory |
| GitHub Copilot CLI | **Name-based** | `--agent <name>` — resolves the name from its scoped agent directories |

Path-based harnesses can be pointed at an arbitrary file — a snapshot copy works naturally. Name-based harnesses always read from their canonical directory regardless of what path the Runner has resolved internally. A snapshot sitting in a sibling directory is invisible to them.

### 6.2 Dual-Strategy Snapshot

The Runner selects a snapshot strategy per harness based on its agent loading mechanism:

| Strategy | How it works | Used by |
|----------|-------------|---------|
| **Copy-and-invoke** | Copy agents to a run-scoped snapshot dir, apply transforms, invoke from snapshot path. Originals untouched. | Path-based harnesses (Claude Code) |
| **Backup-and-transform** | Copy originals to a shared backup dir, transform originals in-place, restore from backup on completion. | Name-based harnesses (OpenCode, GHCP CLI) |

Both strategies create sibling directories next to the regular agents directory:

| Strategy | Harness | Regular directory | Sibling directory | Role |
|----------|---------|------------------|-------------------|------|
| Copy-and-invoke | Claude Code | `.claude/agents/` | `.claude/agents-runner-{run_id}/` | Working snapshot (invoked from here); one per run |
| Backup-and-transform | OpenCode | `.opencode/agents/` | `.opencode/.agents-backup/` | Shared safety backup (restore source); one per workspace |
| Backup-and-transform | GHCP CLI | `.github/agents/` | `.github/.agents-backup/` | Shared safety backup (restore source); one per workspace |

Copy-and-invoke creates a **per-run** directory (scoped by run ID) because each run needs its own independent copy. Backup-and-transform creates a **shared** backup directory because all concurrent runs share the same transformed originals and need the same original-state backup to restore from.

#### 6.2.1 Copy-and-Invoke (Path-Based Harnesses)

This is the current mechanism, unchanged:

1. Copy agents directory to `agents-runner-{run_id}/`
2. Apply harness-specific transformations to the copies
3. Re-resolve all agent paths to point into the snapshot
4. Invoke agents using the snapshot paths
5. On completion: delete snapshot directory

**Crash safety:** Inherent. Originals are never modified. An orphaned snapshot directory is harmless.

**Concurrency:** Naturally concurrent — each run has its own snapshot directory. No coordination needed.

#### 6.2.2 Backup-and-Transform (Name-Based Harnesses)

For harnesses that resolve agents by name from a canonical directory, the Runner must transform the originals in-place so the harness naturally reads the correct content. Multiple concurrent runs share one backup and coordinate via per-run lock files (§6.3).

**First run to start (backup directory does not exist):**

1. **Recovery check:** Scan for orphaned backup state from prior crashes (§6.3.2). Restore if found.
2. **Create backup directory:** Attempt `os.Mkdir(".agents-backup", 0755)`. This is atomic on both Windows and Linux — exactly one caller succeeds when multiple runs race. If the call fails with "already exists," this run is not first — fall through to the concurrent join protocol below.

   **Creator failure handling:** If lock creation (step 3) fails because the directory was concurrently deleted (by a recovery check that saw the partial backup), the creator must not proceed with copying or transforming — it retries from step 2 (re-attempt `os.Mkdir`). Similarly, if copy (step 4) or manifest write (step 5) fails because the directory vanished mid-operation, the creator must not transform. The invariant is: a creator never transforms unless its own lock is held AND the manifest write succeeded.
3. **Acquire lock:** Create `.lock-{run_id}` inside the backup directory and hold an exclusive OS-level file lock on it for the duration of the run. This must happen **before** copying files, so that a concurrent recovery check or joiner sees "lock held" and does not treat the in-progress backup as orphaned.
4. **Copy originals:** Copy all agent files into the backup directory.
5. **Write manifest:** Write `recovery-manifest.json` into the backup directory. The manifest lists the files that will be transformed, computed in memory by a dry-run of the transformation rules (no disk writes yet). The manifest's presence is the completion signal for backup creation — a concurrent joiner that sees the backup directory but no manifest knows the creator is still copying and must wait.
6. **Drop recovery marker:** Write a human-readable recovery file into the agents directory itself (§6.3.3).
7. **Transform in-place:** Apply harness-specific transformations to the original agent files.
8. **Write setup-complete signal:** Write a `.setup-complete` file into the backup directory after all transforms have been applied. A concurrent joiner that sees the manifest but no `.setup-complete` file knows the creator is still transforming and must wait for it before proceeding to run. This prevents a joiner from running against untransformed agents between manifest write and transform completion.
9. **Run.**

**Subsequent concurrent runs (backup directory already exists):**

1. **Wait for manifest:** If `recovery-manifest.json` does not yet exist in the backup directory, the creator is still copying. Poll with a bounded timeout (refuse the run if exceeded) until the manifest appears. During each poll iteration, also check: (a) if the backup directory has disappeared, start fresh as a first run (go to step 2 of the first-run protocol above); (b) if `.restoring` becomes held, enter the `.restoring` re-poll loop described in step 3 below instead of continuing to wait for the manifest.
2. **Wait for setup-complete:** If `.setup-complete` does not yet exist in the backup directory, the creator has finished copying but is still transforming the originals. Poll with a bounded timeout (refuse the run if exceeded) until `.setup-complete` appears. This prevents a joiner from running against untransformed agents. During each poll iteration, also check: (a) if the backup directory has disappeared, start fresh as a first run; (b) if `.restoring` becomes held, enter the `.restoring` re-poll loop described in step 3 below instead of continuing to wait for setup-complete. This prevents a joiner from timing out and refusing the run when a last-out teardown deletes the manifest or backup directory mid-wait.
3. **Check for active restore:** Attempt to acquire an exclusive lock on `.restoring` inside the backup directory. If the lock is held, a restore or exit serialization is in progress — poll in a loop until either: (i) `.restoring` becomes acquirable (acquire it briefly, then re-check backup directory existence: if the backup directory is gone, release `.restoring` and start fresh as a first run; if the backup directory still exists with its manifest, release `.restoring` and proceed to step 4), or (ii) the backup directory disappears (meaning restore completed and the directory was deleted — start fresh as a first run). If the lock is acquirable on the first attempt (or the file does not exist), acquire it briefly, re-check backup directory existence, release it, and proceed.
4. **Acquire lock:** Create `.lock-{run_id}` inside the backup directory and hold an exclusive lock on it.
5. **Post-lock re-verification:** After acquiring the lock, re-check that `.restoring` is not held and that the backup directory and manifest still exist. This closes the check-then-act window between step 3 and step 4: a last-out run could have acquired `.restoring` and begun restoring in that interval. If re-verification fails (`.restoring` is now held, or the backup directory/manifest no longer exists), release and delete the lock file, wait for the backup directory to disappear (bounded timeout), then start fresh as a first run (go to step 2 of the first-run protocol above).
6. No backup or transform needed — originals are already transformed (transforms are idempotent) and the backup already holds the true originals.
7. **Run.**

**On clean completion (any run):**

1. **Acquire restore sentinel:** Create `.restoring` inside the backup directory and acquire an exclusive **blocking Lock** (not TryLock) on it. This serializes all exits: when multiple runs exit simultaneously, they queue on `.restoring` — the second exit blocks until the first completes. This prevents new runs from joining mid-restore (they will see the held `.restoring` lock and wait). This must happen **before** releasing the run's own lock, so that no window exists where a joiner sees "no `.restoring` held, backup exists" and joins between this run's lock release and `.restoring` acquisition.
2. Release the lock on `.lock-{run_id}` and delete the lock file.
3. **Last-out check:** List remaining `.lock-*` files in the backup directory. For each, attempt to acquire an exclusive lock:
   - Lock acquired → owner crashed → delete the lock file.
   - Lock held → another run is still active → **release `.restoring`** (do NOT delete it), leave everything in place, done. Deleting `.restoring` while another run is blocked on it breaks mutual exclusion: on Unix the waiter holds a lock on the unlinked inode while the next arrival creates a fresh file (two holders simultaneously); on Windows the delete fails because the waiter has the file open.
4. If no lock files remain → this is the last active run → restore originals from backup, remove recovery marker, then perform **Windows-safe backup directory deletion**: (a) delete `recovery-manifest.json` while `.restoring` is still held (a joiner's post-lock re-verification checks manifest existence and will detect its absence), (b) delete `.setup-complete`, (c) close and release the `.restoring` file handle, (d) call `os.RemoveAll` on the backup directory. This ordering is necessary because Go opens files on Windows without `FILE_SHARE_DELETE`, so `os.RemoveAll` would fail if `.restoring` were still held. On Linux the ordering is harmless.

### 6.3 Concurrency and Crash Recovery

The backup-and-transform strategy modifies user files in-place. Multiple concurrent runs share the same transformed originals and the same backup. Coordination uses **per-run lock files** inside the backup directory — OS-level file locks (`flock` on Linux, `LockFileEx` on Windows) that are released automatically when a process exits or crashes. No heartbeats, no PID checks, no staleness thresholds.

#### 6.3.1 Race Resolution

Two concurrency races require explicit handling:

**Race (a): Simultaneous first-run backup creation.** Multiple runs start when no backup directory exists. Each attempts `os.Mkdir(".agents-backup", 0755)` — this is atomic on both Windows and Linux: exactly one succeeds, others receive "already exists." The winner creates the backup; losers fall through to the concurrent-join protocol (§6.2.2, "Subsequent concurrent runs"). The winner acquires its `.lock-{run_id}` immediately after `os.Mkdir` and before copying files, so that any concurrent recovery check or joiner sees "lock held" rather than treating the in-progress backup as orphaned. The manifest (`recovery-manifest.json`) is written **last** and serves as the completion signal: a joiner that sees the backup directory but no manifest knows the creator is still copying and polls until the manifest appears before joining.

**Race (a) — partial backup from crash:** If the creator crashes after `os.Mkdir` but before writing the manifest, the backup directory contains files (or is empty) with no manifest and no held lock (the OS released it on crash). This is a partial backup; originals are still untouched because transforms have not been applied (transforms follow the manifest write). The recovery check (§6.3.2) detects this state — backup exists, no manifest, no held locks — and safely deletes the partial backup directory.

**Race (a) — creator failure before lock acquisition:** If N runs start simultaneously and the `os.Mkdir` winner crashes or has the directory removed between `os.Mkdir` and lock acquisition, a recoverer may delete the partial backup. A retrying creator re-attempts `os.Mkdir`. The invariant is that exactly one creator advances to file copying; all others join or retry.

**Race (b): A run joining while a last-out run is restoring.** The last-out run acquires an exclusive **blocking Lock** on a `.restoring` sentinel file inside the backup directory **before** releasing its own `.lock-{run_id}` and before probing other locks or starting the restore (§6.2.2, "On clean completion," step 1). A joining run checks for `.restoring` before creating its own lock: if `.restoring` is held, the joiner polls until either `.restoring` becomes acquirable or the backup directory disappears (see §6.2.2 step 3). After acquiring its own lock, the joiner performs a **post-lock re-verification**: it re-checks that `.restoring` is not held and that the backup directory and manifest still exist. If re-verification fails, the joiner releases its lock, deletes its lock file, and re-enters the `.restoring` poll loop or waits for the backup directory to disappear before starting fresh. This two-step check (pre-lock + post-lock) closes the check-then-act window and ensures a joiner never creates a lock inside a backup directory that is being deleted, and never runs against agents mid-restore.

**Race (b) — two simultaneous exits:** When two runs exit at the same time, both attempt to acquire `.restoring` as a blocking Lock. Exactly one acquires it first; the other blocks. The first performs the last-out check — if it detects the second run's `.lock-{run_id}` as still held (because the second run has not yet had a chance to release it), it releases `.restoring` without restoring. The second run then unblocks, acquires `.restoring`, re-runs the last-out check, finds no remaining locks, and performs the restore. Exactly one of the two exits performs the restore.

**Race (c): A run joining while the creator is still transforming.** After the manifest is written (step 5) but before `.setup-complete` is written (step 8), the originals are not yet transformed. A joiner that sees the manifest but no `.setup-complete` must wait for it before proceeding. The `.setup-complete` file is the signal that transforms are complete and it is safe to run.

#### 6.3.2 Automatic Recovery (Runner Startup)

Every Runner start begins with a recovery check — before any other run-start logic:

1. Scan the agents directory's sibling for the `.agents-backup/` directory. If not found → no recovery needed → done.
2. List all `.lock-*` files inside the backup directory. For each, attempt to acquire an exclusive lock:
   - Lock held → another Runner is actively running → **do not recover**. Leave the backup in place and proceed (this run will join as a concurrent run per §6.2.2).
   - Lock acquired → owner crashed → delete the lock file (release the lock first).
3. If no held locks remain (all lock files were orphaned or none existed), acquire `.restoring` as a **blocking Lock**. Two simultaneous recoverers serialize on `.restoring` — the second blocks until the first finishes, then finds no backup directory and no-ops.
4. **Re-probe all `.lock-*` files under `.restoring`:** A joiner may have acquired a lock between the initial probe (step 2) and `.restoring` acquisition (step 3). For each `.lock-*` file found now, attempt to acquire an exclusive lock:
   - Lock held → a run became active between steps 2 and 3 → release `.restoring` and **skip recovery** (leave the backup in place; this run will join per §6.2.2).
   - Lock acquired → orphaned → delete the lock file.
5. If any held lock was found in step 4 → recovery skipped (released `.restoring` in that step). Done.
6. If still no held locks → no active runs → check the manifest:
   - **Manifest present and readable** → **recover:** restore modified files from backup copies, remove the recovery marker from the agents directory, delete the backup directory, release `.restoring`.
   - **Manifest absent** → **partial backup** from a crash during backup creation (§6.3.1, Race (a) — partial backup). Originals are still untouched (transforms follow the manifest write). Delete the partial backup directory, release `.restoring`, and proceed as if no backup existed.
   - **Manifest present but corrupt/unreadable** → release `.restoring`, **refuse the run** with a clear error. The backup may contain the only copy of original field values, and restoring from a corrupt manifest risks data loss.

This is idempotent: running it when no recovery is needed is a no-op. Running it when another run is active correctly detects the live run and skips recovery.

#### 6.3.3 Manual Recovery (User Self-Service)

If the user abandons the Runner and returns to native harness usage, they need to be able to recover without Runner knowledge. The recovery marker file placed in the agents directory serves this purpose:

- **Location:** Inside the agents directory (e.g., `.opencode/agents/RUNNER-RECOVERY.txt`)
- **Visibility:** Not hidden, not dot-prefixed — visible to anyone who lists the directory
- **Content:** Plain-language explanation of what happened, where the backup is, and step-by-step restore instructions: (1) copy the `.md` files from the `.agents-backup/` directory back into the agents directory (overwrite the modified files), (2) delete the `.agents-backup/` directory, (3) delete this marker file. The instructions must NOT tell the user to rename or replace the agents directory with the backup directory, because the backup contains only `.md` files and renaming would destroy any subdirectories or non-`.md` files the user has in the agents directory.
- **Format:** `.txt` — empirical testing shows that a stray `.md` file in the agents directory is listed as an available agent by OpenCode, while `.txt` files are ignored by all harnesses (Claude Code, OpenCode, GHCP CLI)

#### 6.3.4 What "Corrupted" Means in Practice

Today's only transformation is `mode: subagent → mode: primary` for OpenCode. If left unreversed:

- The agents become directly invocable as primary agents — **more permissive, not broken**
- Interactive orchestration still works (the orchestrator can still spawn them)
- The semantic meaning is wrong but the functional impact is minimal

This is a fortunate property of the current transformation set, not a design guarantee. Future transformations could be more destructive, which is why the recovery mechanism exists regardless of current severity.

### 6.4 Snapshot Transformations

Both strategies apply the same harness-specific transformation rules to make agent files compatible with CLI invocation:

| Harness | Field | Regular value | Runner value | Reason |
|---------|-------|---------------|--------------|--------|
| OpenCode | `mode` | `subagent` | `primary` | `mode: subagent` blocks `opencode run --agent` CLI invocation |

This table is expected to grow as new harness-specific constraints are discovered. The transformation set is hardcoded per harness. The transformation mechanism is currently limited to frontmatter field rewrites; broader file transforms can be introduced when needed without changing the snapshot strategy selection.

### 6.5 Orchestrator Resolution

The Runner derives the script-mode orchestrator path from the harness convention -- it looks for `orchestrator-script` in the regular agents directory (e.g., `.opencode/agents/orchestrator-script.md`). The `--orchestrator-file` flag is removed; the path is fully determined by harness selection. If the orchestrator is not found at the expected path, the Runner refuses to start with a clear error indicating the workspace is not properly deployed.

This requires the deploy tool to place the script-mode orchestrator in the regular agents directory alongside all other agents. The regular orchestrator and script-mode orchestrator coexist in the same directory -- the regular orchestrator is used by interactive orchestration, the script-mode orchestrator is used by the Runner.

### 6.6 Run-Start Integration

The snapshot step is inserted into the Run-Start Sequence (§4). Recovery runs before agent resolution (step 5) so that agent files are in their original state when ResolveAll reads them:

| Step | What | Failure |
|------|------|---------|
| **4b** | **Recovery check: scan for the `.agents-backup/` directory next to the current harness's agents directory, restore if found** | **Refusal if restore fails** |
| 5 | Resolve every agent identifier to a definition file | Refusal |
| **5b** | **Create snapshot (copy-and-invoke) or backup + transform (backup-and-transform), per harness strategy** | **Refusal** |
| 6 | Read stage set from Plan.md (if present) | Refusal if parse error |

Step 4b runs unconditionally for CLI harnesses — it is a no-op when no recovery is needed (and correctly detects active concurrent runs via lock files, skipping recovery in that case). Step 5b creates the snapshot or backup depending on the harness's agent loading mechanism. For copy-and-invoke, resolved paths are rewritten to point into the snapshot. For backup-and-transform, resolved paths remain unchanged (originals are now transformed).

**Cleanup:** On run completion (any terminal outcome):
- **Copy-and-invoke:** Delete the snapshot directory. Cleanup failure is non-fatal.
- **Backup-and-transform:** Release lock, delete own lock file, perform last-out check (§6.2.2). If last out: restore originals, remove marker, delete backup directory. If not last out: leave everything for the remaining active runs. Restore failure is logged as an error but does not change the run's exit code — the run itself succeeded, and the next Runner start will retry recovery automatically.

---

## 7. Dead Ends

Approaches considered and rejected during implementation:

### 7.1 Engine as a Stateful Object

Early designs had the engine as a struct holding mutable state (current row index, stage counter, pending deviations). This was replaced by the pure-function design because:
- Mutable state made the engine hard to test (required setup/teardown)
- Resume required serializing/deserializing engine state alongside the artifact
- The artifact already carries all state needed for routing decisions — duplicating it in the engine was redundant

The pure function receives everything as parameters and returns a decision. The session manages all mutable state.

### 7.2 Single Deviation Mode

The original `--on-deviation` flag had only `stop`. The `delegate` mode (calling the script-mode orchestrator) was added because stopping on every deviation was too disruptive for real workflows — review agents returning `COMPLETED_NEEDS_ACTION` is normal operation, not an error. The current three-mode system formalizes this spectrum further.

### 7.3 Orchestrator as HTTP Service

Briefly considered having the script-mode orchestrator run as a persistent service that the Runner calls via HTTP. Rejected because:
- The orchestrator agent runs in the same harness (Claude Code, etc.) as subagents — there is no separate service infrastructure
- Statefulness would require session management between the Runner and orchestrator
- The current approach (invoke-and-parse) is simpler and uses the same harness adapter as everything else

### 7.4 Multi-Step Deviation Resolution

The initial deviation resolver design had the orchestrator resolve an entire deviation chain in a single invocation: the Runner hands off the deviation, the orchestrator invokes however many agents it needs internally (updating Orchestration.md along the way), and returns only when it can rejoin the happy path. Rejected in favor of the single-decision principle (§2.6) because:
- **Context growth:** The orchestrator's context grows with each internal agent invocation during the chain — the same unbounded cost problem the Runner exists to solve. A complex deviation chain (research → re-design → re-test) inside one orchestrator session accumulates all intermediate tool calls and responses.
- **No infrastructure triggers:** The Runner's trigger evaluation (checkpoints, commits) runs after each step in the dispatch loop. When the orchestrator dispatches agents internally, the Runner never sees those steps, so triggers don't fire during deviation resolution — precisely when checkpoints may matter most.
- **Contract complexity:** Required `rejoin_after_custom` fields and chain-orchestration logic that the single-decision model eliminates.

Note: audit trail and resumability are NOT advantages of single-decision — the real orchestrator already maintains Orchestration.md properly during multi-step resolution. Those properties hold either way.

The single-decision model has the orchestrator make one routing decision per invocation. Complex deviation chains become a sequence of single decisions, each passing through the Runner's dispatch loop (harness → record → triggers → next decision). The orchestrator loses cached attention state between invocations, but this is minor: the execution log in the updated artifact shows what was already tried, and the orchestrator will near-certainly derive the same continuation plan.

---

## 8. Open Items

No open design items remain. All items from the initial draft have been resolved — see below.

**Resolved:**

- **Manual deviation resolution:** The user can be the deviation resolver instead of the orchestrator — making routing decisions when the engine cannot. This is a separate option from the mode, not a mode itself. When enabled, the Runner presents the deviation to the user (in TUI: interactive selection; in CLI: structured prompt) and the user names the target agent. This combines naturally with any mode: in Modes 2/3, the user handles deviations the engine can't auto-route. In Mode 1, the user makes every routing decision, effectively executing the workflow manually — useful for debugging, learning a workflow, or running without a deployed orchestrator. When both manual resolution and an orchestrator are available, the user's choice of resolver takes precedence.

- **Orchestration-review in Runner context:** The `orchestration-review` infrastructure agent is deployed optionally — injected into the orchestrator file during deployment when the user selects it. The Runner detects its presence in the `<InfrastructureAgents>` region. After orchestration-review fires, the Runner invokes the orchestrator with the review output as additional context (one extra LLM invocation per review firing). This is a known cost trade-off — the user accepts it by choosing to deploy orchestration-review. Available in all modes.

- **Mode selection flag:** Implementation detail — the mode must be explicitly selected at run start (no default), same as checkpoints and commits.

- **Orchestrator instruction schema:** The contract collapses to two actions: `dispatch {agent, task_description, hitl_override}` or `stop {reason}`. The old `rejoin` / `custom` / `stop` three-way split is eliminated — `custom` was a multi-step instruction (dispatch + rejoin point) that violates the single-decision principle (§2.6), and `rejoin` was just `dispatch` without a `task_description`. The orchestrator names an agent in the routing table and provides a task description; the Runner builds the rest of the request from the table row. Free table navigation — dispatching any agent regardless of current position — is the normal mechanism for both happy-path routing and deviation recovery. See §2.9.

- **Generic task description content:** The Runner generates a minimal generic message ("Proceed with your task"). The subagent already has its instructions, `input_artifacts`, and `output_artifacts` — the Runner has no domain understanding to add. See §2.5.

- **Mode 1 dispatch construction:** The Runner uses the engine for request construction. The orchestrator returns `{agent, task_description}` plus optional overrides (`constraints`, `input_artifacts`, `output_artifacts`, `hitl_override`). The Runner looks up the agent in the routing table; the engine builds the `ProtocolRequest` using orchestrator-provided fields where present, falling back to the table row's defaults where not. Sequence numbers are always assigned by the Runner.

- **Orchestrator consultation recording:** Individual calls remain in Runner diagnostic/MOSAIC logs rather than the orchestration artifact. They are coordinator turns, so they consume no `global_sequence` value and create no Execution Log row. Workflow Notes preserve conclusions needed by later consultations. See §2.9.

- **Mode 2 engine suppression:** Implementation concern — how Mode 2 suppresses the engine's On Findings auto-routing. Not a design decision; the engine will be rewritten to support all three modes.

- **Default mode:** No default. The user must explicitly choose a mode at run start, same as checkpoints and commits. The mode is a quality-vs-cost trade-off that should not be assumed.

- **Mode switching mid-run:** Not supported. While technically feasible (the mode affects routing decisions, not artifact format), there is no clear use case. A run starts in a mode and stays there. If a different mode is needed, start a new run or resume with the new mode as a future consideration.

- **Mode naming:** The names "orchestrated," "auto," and "auto-review" are accepted as final.

---

## 9. Changelog

| Version | Date | Summary |
|---------|------|---------|
| 0.1 | 2026-08-16 | Initial design. Three execution modes (Orchestrated, Auto, Auto-review) with cost model and dispatch intelligence gap as central tensions. Single-decision principle. Two-action orchestrator contract (dispatch + stop) with free table navigation. Dispatch instruction carries optional artifact/constraint overrides (table row defaults, orchestrator overrides on re-invocations). Mode 3 engine injects review artifact on CNA auto-route back. Pre-consultation for environment plumbing (Modes 2/3). Run-start sequence with run configuration (checkpoints, commits, branch variant, commit setup dispatch). Stop-action UX: CLI terminal, TUI offers retry + manual dispatch. Infrastructure agent triggers. All open items resolved. |
| 0.2 | 2026-08-26 | Runner Agent Snapshot (§6). Runner creates a run-ID-scoped snapshot of deployed agents (`agents-runner-{run_id}/`) at every run start, with harness-specific transformations applied (e.g., `mode: primary` for OpenCode). Run-scoped directories enable safe parallel execution. Snapshot cleaned up on run completion; orphaned snapshots from crashes are harmless. Orchestrator file auto-discovered from harness convention, `--orchestrator-file` flag removed. Snapshot step inserted into Run-Start Sequence as step 5a. |
| 0.4 | 2026-09-26 | **HITL verification, Runner run-state corrections, and current sequential scheduling documented.** §3.5 and the unified loop put HITL verification before acceptance: a rejected attempt is logged without changing `current_state` or the Artifacts registry and cannot fire infrastructure triggers. §4.2 aligns commit setup with native orchestration: create the artifact first, record every setup outcome as an ordinary out-of-band invocation, and retain a failed attempt for inspection and resume. §4.3 rejects an existing artifact whose `run_id` is absent, malformed, or inconsistent with its run folder rather than preserving obsolete pre-identity behavior. §5 corrects `INVOCATION_INTERVAL` to its implemented global-sequence threshold and distinguishes automatically trigger-fired checkpoint/commit/review agents from manual restore agents. §4.1 and §4.3 persist gated-agent selections across resume. Runner-only execution policy is namespaced as `runner_mode`, `runner_pre_consultation`, and `runner_manual_resolution`; redundant `commit_branch_variant` storage is rejected. Consultation rows are described by their persisted form rather than an internal flag absent from the artifact. Admitted staged workflows currently execute one invocation at a time; concurrent workflow dispatch remains deferred in `ROADMAP.md`, while existing compatibility admission continues to reject workflow shapes the Runner cannot interpret. Setup ordering, legacy identity rejection, selection, and Runner-field persistence require the implementation follow-ups recorded by the review. §3.3 records harness errors as `BLOCKED`/`E501`, so the Routing Policy's error tiers apply (implementation follow-up). HITL verification applies to every returned status except `BLOCKED`/`E503`, which is routed directly because it already reports that the gate could not run, and checks only output artifacts the invocation created or modified; a listed but absent or unchanged output is not a gate miss. Mode rationale rewritten after both orchestrators adopted a shared minimal task-description rule. Mode 1's value is per-step routing judgment plus coverage of any workflow the orchestrator can interpret; it is no longer "targeted task descriptions". Modes 2/3 are the intended primary modes, and Mode 1 is the fallback. Admission is mode-dependent: engine-routing shape checks such as the parallel `On Success` refusal apply only in Modes 2/3, and Mode 1 runs forks sequentially with the orchestrator judging joins. The review loop limit is added to run configuration and to Mode 3 auto-routing. §5 `STAGE_END`/`PHASE_END` fire only when the last step of the stage or phase returns HITL-accepted `SUCCESS`, matching `InfrastructureAgentConcept.md` §4. §2.9 moves `current_state` to the dispatched row's position once the outcome is accepted, not at dispatch. The pre-consultation example carries every environment fact in `task_description`; `constraints` is for scope or deliverable restrictions only. §3.5: after a gate-discharging HITL re-dispatch that returns `SUCCESS`, the Runner routes on and records the original invocation's status (implementation follow-up). |
| 0.3 | 2026-09-20 | Dual-strategy snapshot (§6 rewrite). Harnesses that resolve agents by name (OpenCode, GHCP CLI) cannot read from a snapshot directory — the copy-and-invoke mechanism only works for path-based harnesses (Claude Code). New backup-and-transform strategy for name-based harnesses: backup originals, transform in-place, restore on completion. Concurrent runs coordinate via per-run OS-level file locks in a shared backup directory — no heartbeats or PID checks. Crash recovery via startup reconciliation (automatic, lock-aware) and user-visible recovery marker file (manual self-service). Run-start sequence gains step 4b (recovery check, before ResolveAll at step 5) and step 5b (snapshot/backup creation, after ResolveAll). |
