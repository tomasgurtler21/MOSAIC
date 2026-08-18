# Runner Design

> **Status:** Draft
> **Created:** 2026-08-15
> **Last Updated:** 2026-08-16
> **Scope:** The design of `mosaic-run`, the CLI tool that executes orchestration workflows without a human orchestrator in the loop. Covers execution modes (how much routing intelligence the Runner handles autonomously versus delegating to a script-mode orchestrator agent), the architectural layers that implement those modes, the dispatch loop lifecycle, and the contract boundaries between the Runner and the systems it drives (harness adapters, orchestrator agents, the orchestration artifact).

---

## 1. Purpose

The MOSAIC orchestration system was designed around a human orchestrator — an LLM-powered agent that reads a workflow table, dispatches subagents, interprets their responses, and decides what to do next. The orchestrator runs as a persistent LLM session: it creates the orchestration artifact, dispatches a subagent, records the result, reads the response, dispatches the next subagent, records that result... and the context window grows with every iteration. By step 15, the orchestrator is re-processing all prior tool calls, artifact edits, and subagent responses from steps 1–14. Cost scales roughly quadratically with run length.

The orchestrator also does two kinds of work in that loop: **mechanical** work (editing the artifact, tracking sequence numbers, resolving artifact paths, invoking harness commands) and **intelligent** work (deciding which agent runs next, crafting a task description that tells the subagent what to do, handling deviations). The mechanical work is deterministic and does not benefit from LLM reasoning — but it consumes context window and inference cost as if it did.

The Runner (`mosaic-run`) separates these concerns. It takes over all mechanical work: reading workflow tables, invoking subagents through harness adapters, writing the orchestration artifact, tracking sequences, resolving artifacts. The intelligent work — routing decisions and task descriptions — is either handled by the Runner's deterministic engine (when the workflow table prescribes the answer) or delegated to a script-mode orchestrator agent invoked as a fresh, bounded-context session.

### 1.1 The Cost Model

The Runner's three execution modes (§2) represent different points on the cost-vs-quality spectrum:

| Approach | Orchestrator Invocations | Context Growth | Task Description Quality |
|----------|------------------------|----------------|-------------------------|
| **Current (no Runner)** | Every step, persistent session | Unbounded — grows with run length | High — orchestrator crafts each one with full context |
| **Mode 1 (Orchestrated)** | Every step, fresh session each time | Bounded — always system prompt + Orchestration.md | High — orchestrator still crafts each one, reading compact artifact |
| **Mode 2 (Auto)** | Only on deviations | Bounded | Low on happy path — Runner generates generic messages |
| **Mode 3 (Auto-review)** | Only on unresolvable deviations | Bounded | Low on happy path — Runner generates generic messages |

Mode 1 is not the "expensive" option — it is already dramatically cheaper than the current approach because each orchestrator invocation starts a fresh session (no context window growth) and all mechanical work is offloaded to the Runner. Modes 2 and 3 reduce cost further by eliminating most orchestrator invocations, but at the price of losing orchestrator-crafted task descriptions on the happy path.

### 1.2 The Dispatch Intelligence Gap

The orchestrator's value beyond routing is **dispatch intelligence** — the contextual information it packs into each subagent's `task_description` and `constraints`. This intelligence has two distinct layers:

**Per-dispatch context** (what THIS invocation should focus on):
- "Re-address the interface naming inconsistencies flagged in contracts-review.md, specifically the mismatched parameter types in §3.2"
- "Focus on the authentication module — the research identified it as the highest-risk area"

**Environment context** (facts that apply to EVERY invocation):
- Skills are located at `.claude/skills/` — read the relevant skill from there by name
- Use `py` not `python` for the Python interpreter
- Harness-specific quirks subagents should be aware of

The human orchestrator synthesizes both layers for every dispatch because it holds the full context: harness injections, project knowledge (CLAUDE.md), workflow semantics, prior results. When the Runner auto-routes (Modes 2 and 3), it loses BOTH layers — the task description is generic and carries no environment guidance.

Mode 1 fully closes this gap: the orchestrator crafts every dispatch message with both layers. Modes 2/3 lose both layers on auto-routed dispatches. The per-dispatch context gap is the fundamental trade-off of those modes — subagents must derive their focus from instructions and input artifacts alone, without orchestrator guidance on what matters this time. This is the primary reason Mode 1 exists.

The environment context gap is a different class of problem: a subagent that cannot find its skills or uses the wrong Python command fails for plumbing reasons unrelated to its task, and those failures are harder to attribute than task-logic failures. Pre-Consultation (§2.8) addresses this narrow layer with a one-shot orchestrator invocation at run start. It does not close the core dispatch intelligence gap — that remains the fundamental trade-off of Modes 2/3 — but it prevents a category of failure that is difficult to diagnose and easy to avoid.

### 1.3 What the Runner Is Not

The Runner is not a replacement for the orchestrator agent. The orchestrator agent holds domain knowledge (how to recover from a failed test run, when to skip a stage, how to interpret ambiguous findings). The Runner holds workflow-table knowledge (which row comes next, how stages are ordered, how execution groups work). The Runner replaces the orchestrator's *mechanical* work; the orchestrator retains its *intelligent* work and is invoked when judgement is needed.

The Runner is also not a workflow engine in the general sense. It executes MOSAIC workflow tables — a specific format with specific semantics. It does not interpret arbitrary DAGs, BPMN diagrams, or pipeline definitions.

---

## 2. Execution Modes

The Runner supports three execution modes that control how much routing intelligence is handled autonomously versus delegated to a script-mode orchestrator agent. The modes form a spectrum from full delegation to maximum autonomy.

### 2.1 Mode Overview

| Mode | Name | Engine Routes | Orchestrator Decides | Task Descriptions | Cost vs. Current |
|------|------|---------------|---------------------|-------------------|-----------------|
| 1 | **Orchestrated** | Nothing | Everything | Orchestrator-crafted (high quality) | Much cheaper (bounded context) |
| 2 | **Auto** | SUCCESS | Everything else | Generic on happy path; orchestrator-crafted on deviation | Cheapest for deviation-heavy runs |
| 3 | **Auto-review** | SUCCESS + creator/reviewer loops | Remaining deviations only | Generic on happy path and review loops | Cheapest overall |

All three modes are cheaper than the current approach (persistent orchestrator session with unbounded context growth). The modes differ in how much further they reduce cost, and what quality they trade for it.

### 2.2 Mode 1 — Orchestrated

**Routing rule:** The Runner never decides "what next." After every subagent invocation, the Runner invokes the script-mode orchestrator agent as a fresh session. The orchestrator reads Orchestration.md, makes a routing decision, and returns both the target agent AND a task description for the next dispatch.

**Why this is cheaper than the current approach:** The current orchestrator accumulates context across the entire run — every tool call, every artifact edit, every subagent response stays in the context window. In Mode 1, each orchestrator invocation starts fresh: the context is always just the orchestrator's system prompt + the compact Orchestration.md artifact. No growth. The Runner handles all mechanical work (artifact writes, harness invocations, sequence tracking) that previously consumed orchestrator context.

**Why this is higher quality than Modes 2/3:** The orchestrator crafts the task description for every dispatch. It reads the run's history, understands what the previous subagent produced, and writes a targeted message: "Re-address the interface naming inconsistencies flagged in contracts-review.md, specifically the mismatched parameter types in §3.2." Modes 2/3 cannot do this — the Runner has no domain understanding, so auto-routed dispatches carry generic task descriptions.

**Engine role:** The engine is used only for dispatch construction (building `ProtocolRequest` objects, resolving artifact paths) — never for routing decisions. The routing decision, task description, and effective HITL all come from the orchestrator's instruction.

**When to use:**
- Default recommended mode: best cost-quality balance for most workflows
- Workflows with complex conditional routing that depend on artifact content (e.g., "skip the design phase if the research shows no architectural changes needed")
- When task description quality matters — subagents perform better when told specifically what to do
- Developing or debugging the orchestrator agent (the Runner becomes a test harness for the orchestrator's routing logic)

**Dispatch loop:** See the unified loop in §3.1. In Mode 1, step 1 always consults the orchestrator — the engine is never asked for a routing decision. The orchestrator returns a dispatch instruction (agent, task description, and optionally artifact overrides and constraints) or `stop`. The Runner applies orchestrator-provided fields and falls back to table-row defaults for anything the orchestrator omits. See `ScriptOrchestratorContract.md` for the full schema.

**Difference from Modes 2/3:** In Modes 2 and 3, the engine makes routing decisions first and only falls through to the orchestrator on deviation. In Mode 1, there is no engine routing — the orchestrator decides everything, and every dispatch carries an orchestrator-crafted task description.

### 2.3 Mode 2 — Auto

**Routing rule:** The engine handles SUCCESS routing autonomously. All non-SUCCESS responses — including COMPLETED_NEEDS_ACTION from review agents — are delegated to the orchestrator.

**Engine role:** The engine reads the workflow table, determines the On Success target, resolves execution groups and stage ordering, and produces a `DispatchDecision`. For non-SUCCESS responses, the engine produces a `DeviationDecision`, and the session invokes the deviation resolver (orchestrator delegate or stop).

**Task description trade-off:** On the happy path (SUCCESS → next row), the Runner generates a generic task description — the subagent must derive its goal from its own instructions and input artifacts. On deviations, the orchestrator is invoked and crafts a targeted task description. This means the quality of task descriptions degrades precisely on the path that needs them least (the happy path, where the subagent's standard instructions usually suffice) and remains high on the path that benefits most (deviations, where the subagent needs specific guidance about what went wrong).

**When to use:**
- When happy-path subagents perform well with generic task descriptions (their instructions and artifact inputs are sufficient)
- When deviations require orchestrator judgement — review findings sometimes warrant more than routing back to the creator (e.g., escalating to the user, adjusting the plan, or stopping the run)
- When minimizing orchestrator invocations on the happy path matters more than task description quality

**Dispatch loop:** See the unified loop in §3.1. In Mode 2, step 1 asks the engine first. SUCCESS is auto-routed (generic task description). All non-SUCCESS — including COMPLETED_NEEDS_ACTION — produces a Deviation, which triggers orchestrator consultation (crafted task description).

**Key difference from Mode 3:** COMPLETED_NEEDS_ACTION with an unambiguous On Findings target is NOT auto-routed. It produces a Deviation and goes to the orchestrator. The value is routing flexibility, not task description quality — the creator reads the review artifact regardless and derives its focus from there. What the orchestrator adds is the ability to override the On Findings target: when a reviewer's findings point to an upstream problem (e.g., "the contracts are wrong because the requirements are incomplete"), the orchestrator can route to `requirements-refinement` instead of `contracts-designer`. Mode 3 would blindly auto-route to the On Findings target, which may cause the creator to attempt a local fix when the real problem is upstream.

### 2.4 Mode 3 — Auto-review

**Routing rule:** The engine handles SUCCESS routing and creator/reviewer loop routing autonomously. Only truly ambiguous or unexpected situations are delegated to the orchestrator.

**Engine role:** Same as Mode 2, plus: when a review agent returns COMPLETED_NEEDS_ACTION and the workflow table row has an unambiguous On Findings target, the engine auto-routes back to the paired creator without invoking the orchestrator.

**Task description trade-off:** This is the mode with the weakest task descriptions. Both happy-path dispatches AND review-loop re-dispatches carry generic task descriptions. The creator being routed back after a review finding does not receive "fix the naming inconsistency in §3.2" — it receives a generic message and must read the review artifact to understand what to fix. This usually works because the review artifact is in its input_artifacts list and creators are designed to read review feedback, but it is a quality gap compared to Modes 1 and 2.

**When to use:**
- Maximum automation and minimum cost: the orchestrator is invoked only when something genuinely unexpected happens
- When review-loop routing is fully captured by the workflow table's On Findings column
- When subagents reliably derive their task from instructions + artifacts without orchestrator-crafted messages

**Auto-routed cases:**

| Subagent Status | Condition | Engine Action | Task Description | Artifacts |
|----------------|-----------|---------------|-----------------|-----------|
| SUCCESS | Always | Route to On Success target (or next EXECUTION row) | Generic | Table defaults |
| COMPLETED_NEEDS_ACTION | Row has unambiguous On Findings | Route to On Findings target | Generic | Table defaults + review artifact added to input |
| COMPLETED_NEEDS_ACTION | Row has no/ambiguous On Findings | Deviation → orchestrator | Orchestrator-crafted | Orchestrator specifies |
| Any other non-SUCCESS | Always | Deviation → orchestrator | Orchestrator-crafted | Orchestrator specifies |

**Review artifact injection (COMPLETED_NEEDS_ACTION auto-routing):** When the engine auto-routes back from a reviewer to the On Findings target (typically the paired creator), it adds the reviewer's output artifact to the target's `input_artifacts`. The table row for the creator lists the creator's normal inputs — it does not anticipate review loops. The engine knows which artifact the reviewer produced (from the table row's Output column) and adds it to the creator's input set. This ensures the creator can read the review findings without the orchestrator having to specify it manually.

**Creator/reviewer pair recognition:** The engine does not need to understand the `-review` naming convention from `OrchestrationSemantics.md`. It operates purely on the workflow table's On Findings column: if the column is present, non-empty, and contains a plain agent identifier (no spaces, no parentheses), the target is unambiguous and the engine routes to it. The semantic meaning (that this is a creator/reviewer pair) is established by the workflow author when they write the table; the engine just follows the instruction.

### 2.5 The Task Description Spectrum

The task description quality varies across modes and is further improved by pre-consultation (§2.8). To make this concrete:

**Mode 1 dispatch (orchestrator-crafted, per-dispatch + environment):**
```
The contracts-review found three issues: (1) PaymentProcessor.validate()
accepts a raw string amount but the schema defines it as Decimal, (2) the
error response type is missing the 'retryable' field. Re-address these in
ContractsDesign.md.
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

The generic message is deliberately minimal. The subagent already has its instructions (which define its role and scope), its `input_artifacts` list (which tells it what to read), and its `output_artifacts` list (which tells it what to produce) — the Runner has no domain understanding to add. The per-dispatch intelligence gap — the orchestrator's ability to direct focus based on run context ("re-address the naming issues in §3.2") — is the accepted cost of Modes 2/3.

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

The multi-step resolution model is documented as a dead end in §6.4.

### 2.7 Mode Selection

The mode is selected at run start. Both interaction surfaces — the CLI (non-interactive) and TUI (interactive) — expose the same mode selection with identical semantics. The flag/option name and the interaction with `--on-deviation` are captured in Open Items (see §7).

### 2.8 Pre-Consultation

Pre-consultation is enabled by default for Modes 2 and 3. At run start, before the dispatch loop, the Runner invokes the orchestrator agent once and receives generic environment-level strings to append to every subsequent dispatch. The default is on because a subagent in Modes 2/3 that lacks the orchestrator's environment context can fail for plumbing reasons unrelated to its task — skills not found, wrong interpreter command, harness quirks not accounted for — and those failures are difficult to attribute without the missing context. The cost of the single extra orchestrator invocation at run start is small relative to a full run with stale or absent guidance. Pre-consultation can be disabled with `--pre-consult=false`.

**Invocation context:** Pre-consultation is a distinct invocation context from routing consultation (§2.9). The orchestrator returns field-keyed strings (§2.8's response shape), not a routing instruction (`dispatch`/`stop`). The contract document defines both response schemas under their respective invocation contexts.

**What it fixes:** Environmental plumbing — project-specific conventions, tool configurations, harness quirks, and other generic facts that apply identically to all subagents. Without pre-consultation, a subagent in Modes 2/3 might fail because it doesn't know a project-specific convention that the orchestrator's deployed instructions carry. Pre-consultation does NOT address the core dispatch intelligence gap (per-dispatch task context) — that remains the fundamental trade-off of Modes 2/3.

**How it works:**

1. After the run-start sequence completes (workflow loaded, agents resolved, artifact created/resumed), the Runner invokes the orchestrator via the normal `HarnessAdapter`
2. The orchestrator — which already holds all environment context through its deployed instructions — produces structured output: string values keyed by protocol request field name
3. The Runner stores these strings in session state
4. On every subsequent dispatch, the Runner appends the stored strings to the corresponding fields of the `ProtocolRequest` (e.g., appending to `task_description`, appending to `constraints`)
5. The Runner never interprets the content — it appends mechanically

**What the orchestrator produces (example):**

```yaml
task_description: |
  Skills are located at .claude/skills/ in the project root — read the
  relevant skill from there by name. Use `py` not `python` for the
  Python interpreter.
constraints: |
  When running Python, always use `py`, never `python`.
```

**Key design decisions:**

- **Uses the real orchestrator, not a dedicated advisor agent.** The orchestrator already has all the context through deployment. A separate agent would need the same context assembled and passed explicitly — duplicated context with drift risk.
- **Generic, not per-agent.** The orchestrator doesn't know agent internals. It knows environment facts (paths, command aliases, harness quirks) that apply equally to all subagents. The output is flat strings, not a per-agent map.
- **On by default, hard failure on error.** If pre-consultation fails, the run refuses to start. Silent degradation — proceeding without environment guidance — is worse than stopping, whether the feature was explicitly requested or merely defaulted on. Pre-consultation can be disabled with `--pre-consult=false`.
- **Not cached across runs.** Project context, CLAUDE.md, and harness injections can change between runs. One extra LLM invocation at startup is cheap relative to a full run with stale guidance.

**Mode interaction:**

| Mode | Pre-consultation value |
|------|----------------------|
| 1 — Orchestrated | Unnecessary — orchestrator already includes environment context in every crafted dispatch |
| 2 — Auto | Provides environment context for auto-routed dispatches; does not close the per-dispatch intelligence gap |
| 3 — Auto-review | Provides environment context for all auto-routed dispatches; does not close the per-dispatch intelligence gap |

### 2.9 Orchestrator Consultation

There is no separate "deviation resolver" concept. Consulting the orchestrator is a single operation used identically across all modes:

- **Mode 1:** Called after every step — "what's next?"
- **Modes 2/3:** Called when the engine cannot determine the next step from the workflow table — "the engine can't decide, what's next?"

The call is the same either way: invoke the orchestrator agent as a fresh session via the harness adapter, it reads Orchestration.md, and it returns one of two instructions:

- **Dispatch:** `{agent, task_description, constraints?, input_artifacts?, output_artifacts?, hitl_override?}` — execute this routing-table agent next, with this task description. Optional fields override the table row's defaults for this dispatch. When omitted, the Runner uses the table's artifact lists, deployment constraints, and HITL resolution.
- **Stop:** `{reason}` — end the run.

This is the complete action vocabulary. The orchestrator never returns a multi-step plan, never assigns sequence numbers (that's the Runner's mechanical work), and never invokes agents itself. It names an agent, describes the task, optionally adjusts the artifact set and constraints for this specific invocation, and the Runner does the rest.

**Free table navigation:** The orchestrator can dispatch any agent in the routing table, regardless of the current position. This is normal operation, not an exception — a reviewer may find upstream problems (wrong contracts, incomplete requirements, bad plan), and the orchestrator routes back to wherever the fix belongs. The Runner looks up the named agent in the table, finds the corresponding row, and builds the `ProtocolRequest` — applying orchestrator-provided fields (task description, artifacts, constraints, HITL) where specified, falling back to the table row's defaults where not. `current_state` updates to reflect the new position, even if that means jumping backward to an earlier phase.

**Recording:** Orchestrator consultation invocations are recorded in the Execution Log as infrastructure-flagged rows — they consume `global_sequence` and appear in the log, but do not update `current_state`. This follows the same pattern as infrastructure agent invocations (§5). The audit value is significant: on resume, the orchestrator sees its own prior routing decisions in the log; `orchestration-review` can verify them. Not recording them would create invisible gaps in the sequence — `global_sequence` advances but the log doesn't show why.

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
│      HITL: orchestrator's hitl_override, else table  │
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
│ 4. RECORD                                           │
│    Store.Apply(state, completedStep)                │
│    → Execution log row appended                     │
│    → current_state updated                          │
│    → global_sequence bumped                         │
├─────────────────────────────────────────────────────┤
│ 5. INFRASTRUCTURE TRIGGERS                          │
│    Evaluate checkpoint/commit/etc. triggers          │
│    May dispatch infrastructure agents (each recorded)│
├─────────────────────────────────────────────────────┤
│ 6. STAGE REFRESH                                    │
│    If output artifacts contain Stage-*, re-read      │
│    Plan.md for refreshed stage set                   │
├─────────────────────────────────────────────────────┤
│ 7. LOOP                                             │
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
- **Deviation** → consult orchestrator (same call as Mode 1), proceed to step 2 with orchestrator-crafted task description
- **Stop** → end run (precondition failure)

The mode only affects WHICH deviations reach the orchestrator: Mode 2 sends all non-SUCCESS (including COMPLETED_NEEDS_ACTION); Mode 3 auto-routes COMPLETED_NEEDS_ACTION with unambiguous On Findings before producing a Deviation.

**Deviation chains resolve naturally:** If the orchestrator-dispatched agent's result is itself a deviation (Modes 2/3), step 1 simply consults the orchestrator again on the next iteration. No special chain logic. Each step is recorded, triggers fire, and the orchestrator reads the updated artifact each time.

### 3.3 Harness Errors

A harness-level error (timeout, crash, malformed output) at step 3 is fed back into step 1 as a deviation. The session constructs a synthetic response with `StatusCode=BLOCKED` and the error message, records it in the artifact, and the next iteration's routing decision accounts for it. The orchestrator (or engine, in Mode 3 if applicable) decides whether to retry, skip, or stop.

### 3.4 Stop Handling

When the routing decision at step 1 is `stop` (from the orchestrator or from the engine's precondition failure), the Runner's behavior depends on the interaction surface:

**CLI:** Terminal. The Runner prints the stop reason, exits with a non-zero code. The artifact is left in its current state — resumable if the underlying issue is fixed.

**TUI:** The Runner presents the stop reason and offers two actions:

| Action | What it does |
|--------|-------------|
| **Retry** | Re-invoke the orchestrator (or engine) with the same state. Useful when the user fixed something externally (environment config, missing dependency, network issue) and wants the orchestrator to reconsider. |
| **Manual dispatch** | The user takes over as deviation resolver for this one decision — picks the target agent and writes the task description. The run then continues normally (the next routing decision goes back to the configured resolver). This is the same mechanism as manual deviation resolution (§7, resolved items) but triggered on-demand rather than configured at run start. |

The stop action on the wire (`ScriptOrchestratorContract.md` §4.2) is the same regardless of surface — the Runner's reaction to it is surface-specific behavior.

---

## 4. Run-Start Sequence

The run-start sequence validates all preconditions before the first dispatch. Every step that can fail does so before any artifact is created, so a refused run leaves no trace.

| Step | What | Failure → |
|------|------|-----------|
| 1 | Load orchestrator file, extract selected workflow region | Refusal |
| 2 | Parse routing table from workflow region | Refusal |
| 3 | Read existing artifact (if resuming) or verify none exists (if new) | Refusal |
| 4 | Admit workflow (compat checks, execution group resolution) | Refusal |
| 5 | Resolve every agent identifier to a definition file | Refusal |
| 6 | Read stage set from Plan.md (if present) | Refusal if parse error; absence is normal |
| 6b | Enumerate declared infrastructure agents | Refusal |
| 6c | Validate per-class agent selections (gated classes: one active per class) | Refusal |
| 7 | Settle run configuration (checkpoints, commits, commit variant) | Refusal if precondition fails |
| 7a | Commit setup dispatch (if commits enabled) | Refusal if setup fails |
| 7b | Build and validate seed plan (new runs only) | Refusal |
| 8 | Create (new) or resume (existing) artifact | Failure |

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

### 4.2 Commit Setup Dispatch (Step 7a)

When `commits: enabled`, the Runner dispatches the commit-class agent once at run start as an out-of-band invocation. This setup dispatch establishes the target branch — for MOSAIC-owned, the agent creates it; for user's-own, the agent reports the current HEAD branch.

The setup dispatch returns the branch name in a `[branch:{name}]` marker at the end of its `status_message`. The Runner extracts this and records it as `commit_branch` in the artifact frontmatter. If the marker is missing or the dispatch fails, the run refuses to start — proceeding without a known branch destination would risk committing to the wrong place.

The setup dispatch is an ordinary invocation: it consumes `global_sequence`, gets an Execution Log row, and returns a standard protocol response. It is dispatched by explicit instruction, not by a trigger, so the `STAGE_END`-only restriction on the commit class does not apply to it.

### 4.3 Resume

On resume, the session computes a `ResumePoint` from the existing artifact's execution log. If the last logged step was interrupted mid-flight (execution log entry doesn't match `current_state`), the session rewinds `current_state` so the engine re-dispatches the interrupted row. Run configuration (checkpoints, commits, commit_branch) is read from the existing artifact's frontmatter — it was set at original run start and is never modified.

---

## 5. Infrastructure Agent Integration

Infrastructure agents (checkpoint, commit, restore) are dispatched automatically by triggers evaluated after each workflow step completion. They are not part of the workflow table — they are declared in the orchestrator file's infrastructure agent region.

**No-cascades rule:** Infrastructure agent completions do not trigger further infrastructure evaluations. Only workflow step completions trigger the evaluation pass.

**Trigger types:**

| Trigger | Fires When |
|---------|-----------|
| `INVOCATION_INTERVAL` | N workflow steps since last dispatch of this agent |
| `STAGE_END` | Current stage differs from previous workflow step's stage |
| `PHASE_END` | Current phase differs from previous workflow step's phase |
| `MANUAL` | Never fires automatically; dispatched by explicit instruction only |

**Recording:** Infrastructure steps are recorded in the execution log like any other step, but they do not update `current_state`. The artifact's recorded workflow position always names the last workflow step, ensuring the engine's row-lookup stays correct.

---

## 6. Dead Ends

Approaches considered and rejected during implementation:

### 6.1 Engine as a Stateful Object

Early designs had the engine as a struct holding mutable state (current row index, stage counter, pending deviations). This was replaced by the pure-function design because:
- Mutable state made the engine hard to test (required setup/teardown)
- Resume required serializing/deserializing engine state alongside the artifact
- The artifact already carries all state needed for routing decisions — duplicating it in the engine was redundant

The pure function receives everything as parameters and returns a decision. The session manages all mutable state.

### 6.2 Single Deviation Mode

The original `--on-deviation` flag had only `stop`. The `delegate` mode (calling the script-mode orchestrator) was added because stopping on every deviation was too disruptive for real workflows — review agents returning `COMPLETED_NEEDS_ACTION` is normal operation, not an error. The current three-mode system formalizes this spectrum further.

### 6.3 Orchestrator as HTTP Service

Briefly considered having the script-mode orchestrator run as a persistent service that the Runner calls via HTTP. Rejected because:
- The orchestrator agent runs in the same harness (Claude Code, etc.) as subagents — there is no separate service infrastructure
- Statefulness would require session management between the Runner and orchestrator
- The current approach (invoke-and-parse) is simpler and uses the same harness adapter as everything else

### 6.4 Multi-Step Deviation Resolution

The initial deviation resolver design had the orchestrator resolve an entire deviation chain in a single invocation: the Runner hands off the deviation, the orchestrator invokes however many agents it needs internally (updating Orchestration.md along the way), and returns only when it can rejoin the happy path. Rejected in favor of the single-decision principle (§2.6) because:
- **Context growth:** The orchestrator's context grows with each internal agent invocation during the chain — the same unbounded cost problem the Runner exists to solve. A complex deviation chain (research → re-design → re-test) inside one orchestrator session accumulates all intermediate tool calls and responses.
- **No infrastructure triggers:** The Runner's trigger evaluation (checkpoints, commits) runs after each step in the dispatch loop. When the orchestrator dispatches agents internally, the Runner never sees those steps, so triggers don't fire during deviation resolution — precisely when checkpoints may matter most.
- **Contract complexity:** Required `rejoin_after_custom` fields and chain-orchestration logic that the single-decision model eliminates.

Note: audit trail and resumability are NOT advantages of single-decision — the real orchestrator already maintains Orchestration.md properly during multi-step resolution. Those properties hold either way.

The single-decision model has the orchestrator make one routing decision per invocation. Complex deviation chains become a sequence of single decisions, each passing through the Runner's dispatch loop (harness → record → triggers → next decision). The orchestrator loses cached attention state between invocations, but this is minor: the execution log in the updated artifact shows what was already tried, and the orchestrator will near-certainly derive the same continuation plan.

---

## 7. Open Items

No open design items remain. All items from the initial draft have been resolved — see below.

**Resolved:**

- **Manual deviation resolution:** The user can be the deviation resolver instead of the orchestrator — making routing decisions when the engine cannot. This is a separate option from the mode, not a mode itself. When enabled, the Runner presents the deviation to the user (in TUI: interactive selection; in CLI: structured prompt) and the user names the target agent. This combines naturally with any mode: in Modes 2/3, the user handles deviations the engine can't auto-route. In Mode 1, the user makes every routing decision, effectively executing the workflow manually — useful for debugging, learning a workflow, or running without a deployed orchestrator. When both manual resolution and an orchestrator are available, the user's choice of resolver takes precedence.

- **Orchestration-review in Runner context:** The `orchestration-review` infrastructure agent is deployed optionally — injected into the orchestrator file during deployment when the user selects it. The Runner detects its presence in the `<InfrastructureAgents>` region. After orchestration-review fires, the Runner invokes the orchestrator with the review output as additional context (one extra LLM invocation per review firing). This is a known cost trade-off — the user accepts it by choosing to deploy orchestration-review. Available in all modes.

- **Mode selection flag:** Implementation detail — the mode must be explicitly selected at run start (no default), same as checkpoints and commits.

- **Orchestrator instruction schema:** The contract collapses to two actions: `dispatch {agent, task_description, hitl_override}` or `stop {reason}`. The old `rejoin` / `custom` / `stop` three-way split is eliminated — `custom` was a multi-step instruction (dispatch + rejoin point) that violates the single-decision principle (§2.6), and `rejoin` was just `dispatch` without a `task_description`. The orchestrator names an agent in the routing table and provides a task description; the Runner builds the rest of the request from the table row. Free table navigation — dispatching any agent regardless of current position — is the normal mechanism for both happy-path routing and deviation recovery. See §2.9.

- **Generic task description content:** The Runner generates a minimal generic message ("Proceed with your task"). The subagent already has its instructions, `input_artifacts`, and `output_artifacts` — the Runner has no domain understanding to add. See §2.5.

- **Mode 1 dispatch construction:** The Runner uses the engine for request construction. The orchestrator returns `{agent, task_description}` plus optional overrides (`constraints`, `input_artifacts`, `output_artifacts`, `hitl_override`). The Runner looks up the agent in the routing table; the engine builds the `ProtocolRequest` using orchestrator-provided fields where present, falling back to the table row's defaults where not. Sequence numbers are always assigned by the Runner.

- **Orchestrator invocation recording:** Orchestrator consultation invocations are recorded in the Execution Log as infrastructure-flagged rows — they consume `global_sequence` and appear in the log, but do not update `current_state`. See §2.9.

- **Mode 2 engine suppression:** Implementation concern — how Mode 2 suppresses the engine's On Findings auto-routing. Not a design decision; the engine will be rewritten to support all three modes.

- **Default mode:** No default. The user must explicitly choose a mode at run start, same as checkpoints and commits. The mode is a quality-vs-cost trade-off that should not be assumed.

- **Mode switching mid-run:** Not supported. While technically feasible (the mode affects routing decisions, not artifact format), there is no clear use case. A run starts in a mode and stays there. If a different mode is needed, start a new run or resume with the new mode as a future consideration.

- **Mode naming:** The names "orchestrated," "auto," and "auto-review" are accepted as final.

---

## 8. Changelog

| Version | Date | Summary |
|---------|------|---------|
| 0.1 | 2026-08-16 | Initial design. Three execution modes (Orchestrated, Auto, Auto-review) with cost model and dispatch intelligence gap as central tensions. Single-decision principle. Two-action orchestrator contract (dispatch + stop) with free table navigation. Dispatch instruction carries optional artifact/constraint overrides (table row defaults, orchestrator overrides on re-invocations). Mode 3 engine injects review artifact on CNA auto-route back. Pre-consultation for environment plumbing (Modes 2/3). Run-start sequence with run configuration (checkpoints, commits, branch variant, commit setup dispatch). Stop-action UX: CLI terminal, TUI offers retry + manual dispatch. Infrastructure agent triggers. All open items resolved. |
