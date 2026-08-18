# Script-Mode Orchestrator Contract

> **Status:** Draft
> **Created:** 2026-08-15
> **Last Updated:** 2026-08-16
> **Scope:** The contract between the Runner (`mosaic-run`) and the script-mode orchestrator agent. Defines when the Runner invokes the orchestrator, what it sends, what it expects back, and the constraints both sides must respect. This is not the orchestrator's internal design (how it reasons about routing) — it is the wire protocol between two systems.

---

## 1. Purpose

The Runner and the orchestrator agent collaborate to execute workflows. The Runner handles mechanical routing (reading the workflow table, building protocol requests, recording results, evaluating triggers). The orchestrator handles intelligent routing (deciding which agent runs next, crafting targeted task descriptions, handling deviations). When the Runner needs a routing decision it cannot make from the workflow table alone — or, in Mode 1, for every routing decision — it invokes the orchestrator through the same harness adapter used for subagents.

This document defines the contract for that invocation: the request the Runner sends, the response it expects, and the guarantees both sides provide.

### 1.1 Why a Contract Document

The orchestrator agent is an LLM — it produces natural language and JSON according to its system prompt. The Runner is a Go program — it parses JSON according to a struct definition. If either side's expectation drifts, the invocation fails silently or with an opaque parse error. This document is the single source of truth for what crosses the boundary.

### 1.2 Why Not the Communication Protocol

The Communication Protocol governs orchestrator↔subagent communication. The Runner↔orchestrator boundary is a different relationship: the orchestrator is not a subagent performing domain work — it is a routing advisor that reads `Orchestration.md` and returns a single instruction. Most Communication Protocol fields (`input_artifacts`, `output_artifacts`, `include_result_summary`, `human_in_the_loop`, `status_code`, `error_code`) have no meaningful value in this context. Reusing the protocol would require filling fields that are always the same (or always ignored), adding noise to both sides of the contract.

The Runner↔orchestrator contract defines its own request and response schemas, purpose-built for the routing decision boundary. The harness adapter is the transport — it delivers a JSON message and returns a JSON response, regardless of what schema that JSON follows.

### 1.3 Scope Boundary

This document covers:
- The invocation contexts (when the Runner calls the orchestrator)
- The request and response schemas for each context
- Constraints on both sides
- Error handling

This document does NOT cover:
- The orchestrator agent's internal reasoning (its system prompt design)
- The Communication Protocol itself (see `CommunicationProtocol.md`)
- The orchestration artifact format (see `OrchestrationArtifactFormat.md`)
- How the Runner decides when to invoke the orchestrator (see `Design.md` §2)

---

## 2. Invocation Contexts

The Runner invokes the orchestrator in two contexts, each with its own response schema.

### 2.1 Routing Consultation

The Runner needs a routing decision. The orchestrator reads `Orchestration.md`, decides what should happen next, and returns a routing instruction.

This context is used:
- **Mode 1 (every step):** After every subagent completion, regardless of status code. The orchestrator makes all routing decisions.
- **Modes 2/3 (deviation):** When the engine cannot determine the next step from the workflow table — non-SUCCESS status codes the engine can't auto-route, harness errors, ambiguous routing.
- **After orchestration-review:** When the review infrastructure agent fires and produces observations, the Runner follows up with a routing consultation that includes the review's `status_message`. This gives the orchestrator the chance to act on the findings.

The orchestrator does not need to know whether it is being consulted for routine routing (Mode 1) or because something went wrong (Modes 2/3). It reads the artifact, sees the current state, and decides. The `last_status_message` field (§3.2) carries the full verbatim response from the triggering agent — the only piece of context that isn't already in the artifact (the Execution Log truncates `status_message`).

### 2.2 Pre-Consultation

A one-shot invocation at run start (before the dispatch loop) for Modes 2 and 3, enabled by default. The orchestrator reads its own deployed instructions — which carry all environment context (project conventions, tool configurations, harness quirks) — and returns generic strings the Runner appends to every subsequent auto-routed dispatch. Pre-consultation can be disabled with `--pre-consult=false`.

Pre-consultation has a different response schema from routing consultation (§5 vs §4).

**When orchestration-review is not deployed** (absent from the `<InfrastructureAgents>` region), review-triggered consultations never occur.

**Note on `last_status_message` for reviews:** When orchestration-review fires, its `status_message` is passed in `last_status_message` just like any other agent's. The orchestrator does not need a separate field to know this came from a review — the Execution Log shows the review invocation.

---

## 3. Request Format

### 3.1 Schema

```json
{
  "orchestration_artifact": "Orchestration-20260815T143000Z-a3f9/Orchestration.md",
  "context": "routing",
  "last_status_message": "COMPLETED_NEEDS_ACTION: Found 3 issues — (1) PaymentProcessor.validate() accepts raw string but schema defines Decimal, (2) error response type missing 'retryable' field, (3) retry policy not specified for timeout scenarios. See contracts-review.md for details."
}
```

### 3.2 Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `orchestration_artifact` | string | Yes | Path to `Orchestration.md` for this run. The orchestrator reads this to understand the run's full state — execution log, current position, workflow notes. This is the orchestrator's single source of truth. |
| `context` | string | Yes | `"routing"` or `"pre_consultation"`. Tells the orchestrator which response schema the Runner expects. |
| `last_status_message` | string or null | Yes | The full verbatim `status_message` from the agent that triggered this consultation. This is the one piece of context NOT available in the artifact — the Execution Log truncates `status_message` for readability. `null` on the first step of a new run (no prior agent) and for pre-consultation. For harness errors, this carries the Runner-constructed error description. |

---

## 4. Response Format — Routing Instruction

For routing consultation (`context: "routing"`), the orchestrator returns a JSON object with one of two actions.

### 4.1 Action: `dispatch`

Route to a specific agent in the workflow table.

```json
{
  "action": "dispatch",
  "agent": "contracts-designer",
  "task_description": "The contracts-review found three issues: (1) PaymentProcessor.validate() accepts a raw string amount but the schema defines it as Decimal, (2) the error response type is missing the 'retryable' field. Re-address these in ContractsDesign.md.",
  "constraints": null,
  "input_artifacts": ["Requirements.md", "ContractsDesign.md", "contracts-review.md"],
  "output_artifacts": null,
  "hitl_override": null
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `action` | string | Yes | `"dispatch"` |
| `agent` | string | Yes | Agent identifier from the routing table. The Runner looks up the corresponding row for defaults. |
| `task_description` | string | Yes | The orchestrator-crafted task description for the next subagent. The Runner uses this verbatim in the protocol request's `task_description` field. This is the primary value of orchestrator involvement — targeted, context-aware instructions for the subagent. |
| `constraints` | string or null | No | If non-null, used as the `constraints` field in the protocol request. `null` or absent means the Runner uses any table-level or deployment-level constraints. |
| `input_artifacts` | array of strings or null | No | If non-null, overrides the table row's Input column for this dispatch. The orchestrator specifies exactly which artifacts this invocation should read — e.g., adding a review artifact that isn't in the table's default set. `null` or absent means the Runner uses the table row's Input column. |
| `output_artifacts` | array of strings or null | No | If non-null, overrides the table row's Output column for this dispatch. `null` or absent means the Runner uses the table row's Output column. |
| `hitl_override` | bool or null | No | If non-null, overrides the effective HITL for this dispatch. `true` forces HITL on; `false` forces it off. `null` or absent means the orchestrator defers to the workflow table and Plan artifact's HITL resolution. |

**Defaults and overrides:** The table row provides the default artifact set — what the workflow author designed for the first happy-path invocation of each agent. On re-invocations (after review loops, deviation recovery, backward jumps), the artifact set often differs: a creator routed back after review needs the review artifact as additional input; an agent re-invoked after upstream changes may need a different output scope. The orchestrator overrides only the fields that differ from table defaults — `null` means "use the table."

**Runner behavior:** The Runner looks up `agent` in the routing table. For each protocol request field, it applies the orchestrator's value if provided, otherwise falls back to the table row's default. Sequence number is always assigned by the Runner. `current_state` updates to reflect the dispatched row's position — including phase and stage changes, even if the dispatch jumps backward in the table.

**Free table navigation:** The orchestrator can name any agent in the routing table, regardless of the current position. A reviewer finding upstream problems (bad contracts, incomplete requirements, wrong plan) is a normal reason to jump backward. The Runner imposes no ordering constraint — the orchestrator's routing decision is authoritative.

### 4.2 Action: `stop`

Stop the run.

```json
{
  "action": "stop",
  "reason": "Unrecoverable test failure: the framework dependency is missing from the project"
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `action` | string | Yes | `"stop"` |
| `reason` | string | Yes | Human-readable reason for stopping. Surfaced in the Runner's exit message and recorded in the Execution Log. |

**Runner behavior:** The run ends. The artifact is left in its current state (resumable if the underlying issue is fixed).

---

## 5. Response Format — Pre-Consultation

For pre-consultation (`context: "pre_consultation"`), the orchestrator returns field-keyed strings the Runner appends to every auto-routed dispatch.

```json
{
  "task_description": "Skills are located at .claude/skills/ in the project root — read the relevant skill from there by name. Use `py` not `python` for the Python interpreter.",
  "constraints": "When running Python, always use `py`, never `python`."
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `task_description` | string | No | Appended to the generic task description on every auto-routed dispatch. Environment-level guidance that applies to all subagents. |
| `constraints` | string | No | Appended to the `constraints` field on every auto-routed dispatch. |

Both fields are optional — the orchestrator returns only the fields that carry useful content.

**Runner behavior:** The Runner stores these strings in session state. On every subsequent auto-routed dispatch (Modes 2/3), the Runner appends them to the corresponding fields of the `ProtocolRequest`. The Runner never interprets the content — it appends mechanically.

**Not applied to orchestrator-routed dispatches.** When the orchestrator crafts a dispatch instruction (§4.1), it already includes all environment context in its own `task_description`. Pre-consultation strings are only for auto-routed dispatches where the orchestrator was not involved.

---

## 6. Orchestrator-Side Constraints

### 6.1 Response Constraints

| Constraint | Rationale |
|-----------|-----------|
| Return valid JSON conforming to the expected response schema | The Runner parses the response structurally. Malformed JSON or missing required fields stops the run. |
| Use only the two defined actions (`dispatch`, `stop`) for routing consultation | Unknown actions are parse errors. |
| Use only agent identifiers from the routing table in `dispatch.agent` | The Runner resolves these to table rows. An unknown agent stops the run (§7.3). |
| Always provide `task_description` in `dispatch` | The task description is the orchestrator's primary value — an empty one wastes the invocation. |

### 6.2 Artifact Constraints

| Constraint | Rationale |
|-----------|-----------|
| May read the full artifact (execution log, current state, workflow notes) | This is how the orchestrator understands the run's history and current position. |
| May write to the Workflow Notes section | This is the orchestrator's scratchpad for recording reasoning across invocations. |
| Must NOT modify the execution log, current state, artifact registry, or frontmatter | These are managed exclusively by the Runner's artifact store. Concurrent writes would corrupt the artifact. |

### 6.3 Behavioral Constraints

| Constraint | Rationale |
|-----------|-----------|
| Must NOT invoke subagents directly | All subagent dispatch goes through the Runner. The orchestrator DECIDES what runs next; the Runner EXECUTES it. |
| Must NOT modify project files | The orchestrator is a routing agent, not an execution agent. It reads the artifact for context; it does not touch the codebase. |
| Decisions must be derivable from the artifact | The orchestrator must not require out-of-band state (environment variables, external services, prior conversation history). The artifact is the complete record. |

---

## 7. Error Handling

### 7.1 Malformed Response

The run stops. The error message includes the parse error. This covers: invalid JSON, missing required fields, unknown `action` values.

### 7.2 Unknown Agent in `dispatch.agent`

The run stops. The error message identifies the unknown agent and lists available agents from the routing table.

### 7.3 Harness Error Invoking Orchestrator

The run stops. The error message includes the harness error (timeout, crash, connection failure).

### 7.4 Pre-Consultation Failure

The run refuses to start. Pre-consultation failure is a startup error, not a runtime error — the run has not yet begun. No artifact is created, nothing to resume.

### 7.5 All Orchestrator Failures Are Terminal

Unlike subagent failures (which become deviations that the orchestrator can resolve), orchestrator failures are terminal. The Runner has no higher authority to escalate to. The run stops, the artifact is left in a resumable state, and the user must intervene.

---

## 8. Runner-Side Constraints

| Constraint | Rationale |
|-----------|-----------|
| Always provide the current artifact path in `orchestration_artifact` | The orchestrator must read the latest state. A stale or wrong path leads to decisions on wrong state. |
| Write the preceding subagent's result to the artifact BEFORE invoking the orchestrator | The orchestrator reads the artifact for context. If the artifact doesn't reflect the most recent subagent completion, the orchestrator makes decisions on stale state. |
| Always provide the full `status_message` from the triggering agent in `last_status_message` | The Execution Log truncates messages. The orchestrator needs the full response to make informed routing decisions. |
| Re-read the artifact after the orchestrator returns | The orchestrator may have updated Workflow Notes. The Runner must pick up those changes before the next dispatch. |
| Record orchestrator invocations as infrastructure-flagged Execution Log rows | Orchestrator consultations consume `global_sequence` and appear in the log but do not update `current_state`. The artifact's workflow position always reflects the last workflow step. |
| Never parse orchestrator responses beyond the defined schema | If the orchestrator returns extra fields, ignore them. Forward compatibility. |

---

## 9. Dead Ends

### 9.1 Communication Protocol as Wire Format

The initial contract reused the Communication Protocol (orchestrator↔subagent schema) for the Runner↔orchestrator boundary. This was replaced by a purpose-built schema because:
- Most CommProtocol fields (`input_artifacts`, `output_artifacts`, `include_result_summary`, `human_in_the_loop`, `status_code`, `error_code`) have fixed or meaningless values in this context — filling them adds noise without information.
- The orchestrator reads everything from `Orchestration.md`. Passing the artifact path in `input_artifacts` rather than a dedicated field obscures the one thing the orchestrator actually needs.
- The CommProtocol response requires `status_code: SUCCESS` always and `result_data` as a JSON-in-string — two layers of wrapping around the routing instruction that carries the actual content.
- The Runner↔orchestrator relationship is fundamentally different from orchestrator↔subagent: one is a routing advisory boundary, the other is a task execution boundary. Different relationships deserve different schemas.

### 9.2 Three-Action Schema (rejoin / custom / stop)

The initial contract defined three actions. `rejoin` named a table agent to route to; `custom` dispatched an off-table agent and specified a rejoin point afterward; `stop` ended the run. This was replaced by the two-action schema (`dispatch` + `stop`) because:
- `custom` encoded two decisions in one response (dispatch + rejoin point), violating the single-decision principle (`Design.md` §2.6). Under single-decision, the orchestrator is consulted again after each dispatch.
- `rejoin` was `dispatch` without a `task_description` — missing the orchestrator's primary value.
- The distinction between table and non-table agents added contract complexity for no gain.

### 9.3 Structured Deviation Payload

Early designs had the Runner send the full `DeviationInfo` as a structured JSON payload in a dedicated field. This was abandoned because:
- The orchestrator can derive everything it needs from the artifact
- Structured payloads risk the orchestrator parsing them instead of reading the artifact, which is fragile
- The `last_status_message` field carries the one thing the artifact doesn't have (the full, untruncated response); everything else is in the Execution Log

### 9.4 Context Hint Field

An intermediate design included a `context_hint` field — a Runner-constructed human-readable string describing why the orchestrator was being consulted (e.g., "contracts-review#4 returned COMPLETED_NEEDS_ACTION at phase DESIGN"). This was replaced by `last_status_message` because:
- The hint duplicated information already in the Execution Log (which agent, what status code, what phase)
- The genuinely useful information — the agent's full `status_message` — was NOT in the hint (it was a summary) and NOT in the artifact (the log truncates it)
- Sending the full verbatim `status_message` gives the orchestrator the one piece of context the artifact doesn't carry, without duplicating what it does

### 9.5 Bidirectional Orchestrator Communication

Considered a multi-turn exchange where the Runner and orchestrator go back and forth (orchestrator asks for more context, Runner provides it). Rejected because:
- The harness adapter is a single-shot invoke-and-return interface
- Multi-turn would require session management between the Runner and orchestrator
- The artifact already carries all context the orchestrator should need

### 9.6 Orchestrator as a Special Subagent

Considered treating the orchestrator as another row in the routing table (a "meta-agent" that routes to other rows). Rejected because:
- It conflates the routing level with the execution level
- The orchestrator's input/output is fundamentally different from subagents (it reads/writes the orchestration artifact, not domain artifacts)
- It would require the engine to special-case one row in the table, defeating the table's uniformity

---

## 10. Changelog

| Version | Date | Summary |
|---------|------|---------|
| 0.1 | 2026-08-16 | Initial design. Purpose-built wire schema (not Communication Protocol). Request: `orchestration_artifact` + `context` + `last_status_message`. Two-action response: `dispatch` (with optional artifact/constraint overrides) or `stop`. Pre-consultation response for environment strings. Dead ends: CommProtocol reuse, three-action schema, context hint field, structured deviation payload, bidirectional communication, orchestrator as subagent. |
