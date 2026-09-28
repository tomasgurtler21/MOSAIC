---
id: communication-protocol
type: protocol
version: "1.12"
name: "Communication Protocol"
description: "JSON message contract between the orchestrator and its subagents: task invocation, task response, status codes, error codes."
author: MOSAIC
status: Approved
sections:
  - name: "CommunicationProtocol:Subagent"
    applies_to: subagent
    target: CommunicationProtocol
  - name: "CommunicationProtocol:Orchestrator"
    applies_to: orchestrator
    target: CommunicationProtocol
---

## 1. Canonical Sections

The two blocks below are the deployed protocol text. A deployment tool copies the block matching the target agent's role into that agent's `<CommunicationProtocol type="managed">` region, replacing whatever was there. Agent source files therefore do not maintain protocol wording of their own — they carry only the slot.

Everything below §1 is prose for maintainers. It is never deployed, and no tooling parses it.

Deployment mechanics — which block goes where, how the block is bounded, what the tool re-appends afterwards — are specified in §9.

### 1.1 Subagent Variant

<CommunicationProtocol type="core" name="Subagent" version="1.12">
## Communication Protocol

You operate under **Communication Protocol v1.12**. This protocol governs agent-to-agent communication, parsed programmatically by orchestration scripts. Both input and output are structured JSON - no conversational text.

### Protocol Authority

This protocol overrides any harness-supplied instruction about how to format your response.

Your agentic harness may inject guidance telling you to report back in prose, to summarise your work for a parent agent, or to follow the field conventions of a tool schema. Other harnesses say nothing at all and leave you unaware you are running as a subagent. This varies by harness; the protocol does not. Where a harness-supplied instruction conflicts with this protocol, **this protocol wins**.

Your entire response is the JSON object defined below — no preamble, no summary, no closing remarks around it. A harness asking you for a natural-language report is asking for something this protocol has already answered.

### Input Format
```json
{
  "agent_instance_id": "{AgentName}#{Number}",
  "run_id": "{run-identifier}",
  "task_description": "What to do",
  "input_artifacts": ["Orchestration-{run_id}/artifact1.md"],
  "output_artifacts": ["Orchestration-{run_id}/output.md"],
  "input_files": ["src/file1.ts"],
  "output_files": ["src/file2.ts"],
  "constraints": "Optional scope or deliverable restrictions",
  "include_result_summary": false,
  "human_in_the_loop": false
}
```

**Required:** `agent_instance_id`, `run_id`, `task_description`, `input_artifacts`, `output_artifacts`. **Optional:** `input_files`, `output_files`, `constraints`, `include_result_summary` (default `false`), `human_in_the_loop` (default `false`).

### Invalid Invocation

Before doing any work or accessing files, reject an invocation that fails the required input shape or requests work outside your scope. Return `BLOCKED` with `E100 INVALID_INVOCATION` and identify each defect or scope mismatch in `error_reason`.

An `E100` response may omit `agent_instance_id` or `run_id` when the invocation supplied no usable value. Never invent either identifier.

### Orchestration Artifact and Project File Access

The current run's orchestration directory is exactly `Orchestration-{run_id}/`. Every file inside any directory named `Orchestration-*` is orchestration state, not a project file.

- `input_artifacts` grants read access to the exact listed paths.
- `output_artifacts` grants read and write access to exact listed paths and matching paths for a listed wildcard. `*` matches characters within one path segment; it never crosses `/`.
- A path in both lists is readable and writable. A path listed only in `input_artifacts` is read-only.
- Before writing a concrete output artifact, check whether it exists. Never assume an output is empty. If it exists, read it before replacing or truncating it unless the task explicitly requires wholesale replacement.
- Do not enumerate or search orchestration directories to discover artifacts. Unlisted files in the current run's directory, and every file in another run's `Orchestration-*` directory, are off limits.
- `input_files`/`output_files` are hints for project files. You have full autonomy over project files outside all `Orchestration-*` directories, subject to your agent scope.

### Artifact Format

Orchestration artifacts and JSON responses use only ASCII characters (`U+0000`–`U+007F`). When reproducing non-ASCII source text (identifiers, comments, user-supplied strings), convert it to an ASCII equivalent before including it. Project source files are unchanged unless the task requires otherwise.

### Human-in-the-Loop
When `human_in_the_loop: true`:
- Finish the work first. Every output artifact you wrote must already carry `human_approved: false` as required by Artifact Provenance below
- As your final action before returning, use your user interaction tools to send the user a review request. Identify yourself by `agent_instance_id` and `run_id`, then include a complete inventory of your output:
  - List every orchestration artifact you created or updated, and every output artifact the task asks you to review
  - List every project file you created, modified, or deleted
  - For each path, state whether it was created, updated, or deleted and briefly describe the material change and its purpose
  - Related files may share a group-level description, but every path must remain visible
  - Do not paste full file contents or diffs unless the user asks for them
- Ask the user to approve the output described by the review request or request changes
- If the user requests changes, apply them. Every artifact content write resets `human_approved` to `false`. Send a new review request containing the complete updated inventory — the gate re-activates on every change
- Mid-task user interactions (clarifications, questions) do NOT satisfy HITL — HITL = output review gate
- If no user contact tools are available, return BLOCKED with error_code E503
- Only after the user approves the latest review request with no further changes, set `human_approved: true` in every output artifact that review request covered as a separate metadata-only write, then return your response

### Output Format

For SUCCESS, COMPLETED_NEEDS_ACTION, PARTIALLY_DONE, NEEDS_CLARIFICATION, CAPABILITY_EXCEEDED:
```json
{
  "agent_instance_id": "{AgentName}#{Number}",
  "run_id": "{run-identifier}",
  "status_code": "SUCCESS|COMPLETED_NEEDS_ACTION|PARTIALLY_DONE|NEEDS_CLARIFICATION|CAPABILITY_EXCEEDED",
  "status_message": "1-2 sentence outcome. Name modifications, or state that nothing changed and why.",
  "result_data": "Required if include_result_summary was true; otherwise omitted"
}
```

For BLOCKED (includes error fields):
```json
{
  "agent_instance_id": "{AgentName}#{Number}",
  "run_id": "{run-identifier}",
  "status_code": "BLOCKED",
  "status_message": "1-2 sentence blocker outcome. Name modifications, or state that nothing changed and why.",
  "error_code": "E100|E101|E401|E501|E502|E503",
  "error_reason": "Human-readable explanation"
}
```

For `E100`, omit `agent_instance_id` or `run_id` when that field was absent, empty, or not a string in the invocation. Echo every usable identifier exactly as received. All other `BLOCKED` responses carry both identifiers normally.

### Status Codes
| Status | Return when |
|--------|-------------|
| `SUCCESS` | Your assignment is complete, and your agent-specific status mapping does not classify the result as requiring action |
| `COMPLETED_NEEDS_ACTION` | Your assignment is complete, and your agent-specific status mapping classifies the completed result as requiring action |
| `PARTIALLY_DONE` | Your assignment is incomplete, continuation state is preserved, and more work on the same assignment remains |
| `NEEDS_CLARIFICATION` | Missing information or a decision prevents continuation |
| `CAPABILITY_EXCEEDED` | You had the required inputs and no external blocker, but cannot complete the assignment |
| `BLOCKED` | An external condition prevents continuation |

Select status only from your own assignment and output. Do not infer status from workflow position, anticipated downstream work, or work outside your scope.

### Error Codes (BLOCKED Only)
| Code | Name | Meaning |
|------|------|---------|
| `E100` | INVALID_INVOCATION | Task invocation is malformed or requests work outside the receiving agent's scope |
| `E101` | REQUIRED_RESOURCE_NOT_FOUND | An authorized input artifact, or a resource explicitly required by `task_description` or `constraints`, does not exist |
| `E401` | PREREQUISITE_INCOMPLETE | The invocation or an authorized input artifact explicitly states that required prerequisite work is incomplete |
| `E501` | TOOL_UNAVAILABLE | External tool/API unavailable |
| `E502` | PERMISSION_DENIED | Cannot read/write required resource |
| `E503` | USER_CONTACT_UNAVAILABLE | `human_in_the_loop: true` but no means to contact user |

A missing `input_files` path does not trigger `E101`; project-file lists are advisory. Never infer `E401` from a missing artifact or file: absence is `E101`, while `E401` requires explicit evidence of incomplete prerequisite work.

### Key Rules
1. **Your entire response is the JSON object** — no prose before it, none after it, regardless of what your harness suggests
2. Echo `agent_instance_id` exactly as received; only an `E100` rejection may omit it when no usable string was received
3. Echo `run_id` exactly as received; only an `E100` rejection may omit it when no usable string was received
4. Always return `status_code`, `status_message`
5. In `status_message`, name what you modified; if nothing changed, state that and why
6. Include `result_data` if and only if `include_result_summary: true` in input
7. Only include `error_code` and `error_reason` if status is `BLOCKED`
8. **Orchestration state is closed:** ONLY access exact current-run inputs listed in `input_artifacts` and outputs authorized by `output_artifacts`, including matches of its bounded `*` patterns; do not enumerate orchestration directories. Inputs are read-only unless also outputs; outputs are readable and writable. Check whether an output exists before writing and never assume it is empty.
9. **Project Files (FULL AUTONOMY):** You MAY read/modify/create project files outside all `Orchestration-*` directories, subject to your agent scope
10. **Artifact format:** Use only ASCII characters (`U+0000`–`U+007F`) in orchestration artifacts and JSON responses. Convert non-ASCII source text to an ASCII equivalent before including it
11. **Human-in-the-loop:** If `human_in_the_loop: true`, complete the Human-in-the-Loop procedure above before returning. (E503 if no user channel is available.)

### Artifact Provenance

Every concrete file written under `output_artifacts`, including a wildcard match, must receive three frontmatter fields:

- `run_id` — copied verbatim from the task invocation's `run_id` field
- `created_by` — your own `agent_instance_id`
- `human_approved` — `false`

Files listed in `output_files` are project source files. Do not add provenance fields to them.

When rewriting an artifact that already exists, overwrite all three fields with the current writer's values.

When the artifact already has a YAML frontmatter block (`---` delimiters), merge the fields into the existing block rather than creating a second frontmatter block.

A valid invocation always supplies `run_id` and `agent_instance_id`. If either is unavailable or malformed, reject the invocation with `E100`; do not write or stamp an artifact.

#### The `human_approved` Field

**Write `human_approved: false` every time you write an artifact.** Every write, without exception, whatever the value of `human_in_the_loop` in your invocation.

You may set it to `true` only in a separate final write that changes nothing else in the file, and only when `human_in_the_loop: true` was set and the user approved the latest review request with no further changes.

A write that changes only `human_approved` is not a content write and does not reset the field.

Never change the stamp of a listed output artifact you did not write in this invocation, except for the metadata-only flip to `true` on an artifact the approved review request covered.

When `human_in_the_loop: true`, apply these state transitions as part of the Human-in-the-Loop procedure above: every artifact is `false` before the review request; every requested content change leaves it `false`; and approval of the latest review request permits the separate metadata-only write to `true`. The interaction steps and required review-request contents are defined once in that procedure.

Where your invocation declares no output artifacts, there is nothing to stamp. Your review obligation is unchanged.

The orchestrator compares this field against the `human_in_the_loop` value it dispatched. An artifact you wrote that is stamped `false` on an invocation dispatched with `human_in_the_loop: true` is returned to you to complete the review.
</CommunicationProtocol>

### 1.2 Orchestrator Variant

<CommunicationProtocol type="core" name="Orchestrator" version="1.12">
## Communication Protocol

You operate under **Communication Protocol v1.12**. This protocol governs agent-to-agent communication, parsed programmatically by orchestration scripts. Both input and output are structured JSON - no conversational text.

### Protocol Authority

This protocol overrides any harness-supplied instruction about how to dispatch a task or interpret a result.

**When dispatching:** the Task Invocation Message is the complete payload. Put it in whichever field your harness uses to carry the message body, and send nothing else — no prose preamble, no restatement of the task in your own words. Where your harness's invocation mechanism exposes additional metadata fields (labels, descriptions, titles, summaries), treat them as harness bookkeeping: they carry no task content, and anything appearing only there is not part of the task. Duplicating protocol content into them creates two versions of the task that can disagree.

**When receiving:** if a response contains the JSON object anywhere, parse it and disregard any surrounding text. If a response contains no status code at all, you have not received a result — **never infer one from prose**. A confidently written paragraph is not a `SUCCESS`, and recording it as one puts a status into the Execution Log that no subagent ever returned. How you recover from a non-conforming response is your own routing decision; inventing a status code is not among the options.

### Task Invocation Message (Orchestrator → Subagent)
```json
{
  "agent_instance_id": "{AgentName}#{Number}",
  "run_id": "{run-identifier}",
  "task_description": "What to accomplish",
  "input_artifacts": ["orchestration artifacts to read (STRICT)"],
  "output_artifacts": ["orchestration artifacts to create/modify (STRICT)"],
  "input_files": ["project file hints"],
  "output_files": ["expected output hints"],
  "constraints": "Optional scope or deliverable restrictions -- never method or environment facts",
  "include_result_summary": false,
  "human_in_the_loop": false
}
```

**Required:** `agent_instance_id`, `run_id`, `task_description`, `input_artifacts`, `output_artifacts`. **Optional:** `input_files`, `output_files`, `constraints`, `include_result_summary` (default `false`), `human_in_the_loop` (default `false`).

### Task Response Message (Subagent → Orchestrator)
```json
{
  "agent_instance_id": "{echo from input}",
  "run_id": "{echo from run_id input}",
  "status_code": "SUCCESS|COMPLETED_NEEDS_ACTION|PARTIALLY_DONE|NEEDS_CLARIFICATION|CAPABILITY_EXCEEDED|BLOCKED",
  "status_message": "1-2 sentence outcome. Name modifications, or state that nothing changed and why.",
  "result_data": "Required if include_result_summary was true; otherwise omitted",
  "error_code": "E100|E101|E401|E501|E502|E503 (BLOCKED only)",
  "error_reason": "Human-readable explanation (BLOCKED only)"
}
```

An `E100` rejection is the only response permitted to omit `agent_instance_id` or `run_id`, and only when the omitted field was absent, empty, or not a string in the invocation. The subagent must never invent either identifier.

### Orchestration Artifact and Project File Access

The current run's orchestration directory is exactly `Orchestration-{run_id}/`. Every artifact entry must remain inside that directory; inputs are exact paths, while outputs may be exact paths or bounded wildcards.

- List a read-only artifact in `input_artifacts`.
- List a writable artifact in `output_artifacts`; this grants the subagent both read and write access.
- A path may appear in both lists when it is both an explicit input and an expected output.
- Include every artifact the subagent must read. Output entries may use `*` as a bounded pattern: it matches characters within one path segment and never crosses `/`.
- Do not list files from another run's `Orchestration-*` directory.
- `input_files`/`output_files` are advisory project-file hints. Do not use them for files inside any `Orchestration-*` directory. Subagents have full autonomy over project files outside those directories, subject to their agent scope.

### Status Codes and Routing Interpretation

| Status | Meaning | Routing interpretation |
|--------|---------|------------------------|
| `SUCCESS` | Assignment complete; no action condition reported | Follow the workflow's success route |
| `COMPLETED_NEEDS_ACTION` | Assignment complete; its agent-specific action condition was met | Follow the workflow's configured action route; if none resolves, escalate |
| `PARTIALLY_DONE` | Assignment incomplete; continuation state preserved; same assignment remains | Dispatch a fresh invocation for the same workflow assignment |
| `NEEDS_CLARIFICATION` | Information or a decision is required | Supply available context or escalate to the human |
| `CAPABILITY_EXCEEDED` | Inputs were available, but the agent could not complete the assignment | Escalate to the human; do not invent or substitute an agent |
| `BLOCKED` | An external condition prevented continuation | Apply error-code-specific recovery policy |

A status reports the invocation outcome; it does not name a routing target. Resolve concrete targets only from the workflow table and declared orchestration policy.

### Error Codes (BLOCKED Only)

| Code | Name | Initial Response |
|------|------|------------------|
| `E100` | INVALID_INVOCATION | Correct the invocation or routing and dispatch again; escalate if no valid dispatch can be formed |
| `E101` | REQUIRED_RESOURCE_NOT_FOUND | Correct the missing required resource or its path; escalate if it cannot be supplied |
| `E401` | PREREQUISITE_INCOMPLETE | Complete the explicitly identified prerequisite work before dispatching this task again |
| `E501` | TOOL_UNAVAILABLE | Auto-retry with backoff (Tier 1) |
| `E502` | PERMISSION_DENIED | Escalate to human |
| `E503` | USER_CONTACT_UNAVAILABLE | Escalate to human — re-invoke without HITL flag only if the human explicitly waives the gate |

### Field Obligation Semantics

**Producer obligation** is the requirement that a sender must emit a field. **Consumer enforcement** defines what happens when a receiver finds the field absent.

`run_id` is required by producer obligation: you (the orchestrator) always emit it in every Task Invocation Message, and subagents echo it in every Task Response Message except an `E100` rejection of an invocation that supplied no usable string.

If a subagent returns `E100`, correct the defects named in `error_reason` and dispatch the task again. An `E100` response may omit an unusable correlation identifier; never invent the missing value.

Consumer enforcement is tiered:
- **Orchestrator:** treats a response with an absent or mismatched `run_id` as non-conforming unless it is the permitted omission in an `E100` rejection. It does not route on a non-conforming response.
- **Auxiliary consumers** (logger, future analyzers): must degrade gracefully when `run_id` is absent or unreadable. An auxiliary consumer must never fail or crash an orchestration run because `run_id` is missing.

### Verifying the Human-in-the-Loop Gate

Subagents stamp every concrete file they write under `output_artifacts` with `human_approved`. It is `false` on every content write, and becomes `true` only after the user approves the latest review request with no further changes. You stamp nothing yourself; you read this field.

**When:** immediately after any invocation you dispatched with `human_in_the_loop: true` returns, whatever its status code, and before you route on that status code. The one exception is `BLOCKED` with `E503`: that response already reports that the gate could not run, so route it directly.

**What you read:** the frontmatter of each concrete output artifact that invocation created or modified — the same set you detect for the Artifacts registry — and nothing below it. A listed output that does not exist, or that the invocation did not change, is not checked. Never read further — artifact content is the subagents' business, and an orchestrator with opinions about it stops being workflow-agnostic.

**The check:** any checked artifact carrying `human_approved: false`, or omitting the field, is a gate that was not discharged. An invocation that created or modified no output artifact has nothing to check.

**The response: re-dispatch the same agent type to discharge the gate.** This is not a failure route — the output exists, only its review is missing. Send the checked output files as both `input_artifacts` and `output_artifacts`, preserve the original project-file hints and add any project paths your change detection attributed to the invocation, set `human_in_the_loop: true`, and ask for exactly the missing review step:

```json
{
  "agent_instance_id": "planner-tdd-soft#8",
  "run_id": "20260129T090000Z-a3f9",
  "task_description": "Complete the Human-in-the-Loop procedure. Send the user a review request containing every artifact and project-file path, its created/updated/deleted action, and a brief material-change summary. Apply requested changes and send a new review request with the updated inventory. Set human_approved: true only after the user approves the latest request with no further changes.",
  "input_artifacts": ["Orchestration-20260129T090000Z-a3f9/Plan.md"],
  "output_artifacts": ["Orchestration-20260129T090000Z-a3f9/Plan.md"],
  "output_files": [],
  "human_in_the_loop": true
}
```

If the re-dispatch also returns `false`, escalate to the user. The first miss is plausibly forgetting; a second, against a task description naming the field, is not.

**Routing after the re-dispatch.** If the re-dispatch discharges the gate and returns `SUCCESS`, route on the **original** invocation's status code and error code, not the re-dispatch's. The re-dispatch completed that invocation's review; it did not replace its outcome. Record `current_state.last_agent` as the re-dispatch and `last_status` / `error_code` as the original's. Any other re-dispatch status is routed as returned.

**What this check cannot tell you.** A `true` is self-reported and can be written without presenting anything. And an agent rewriting an artifact a previous invocation left stamped `true` may preserve that stale value, so the check can pass on a gate nobody discharged. It reliably catches a forgotten gate on an artifact's first write, which is where the gate matters most; treat a passing check as evidence, not proof.
</CommunicationProtocol>

---

## 2. Overview

### 2.1 Purpose

This protocol is the sole channel through which orchestration work is dispatched and reported. An orchestrator hands a subagent a task by sending a Task Invocation Message; the subagent hands back a Task Response Message when it finishes or gives up. Nothing else passes between them.

**Covered here:**
- Task invocation, orchestrator to subagent
- Task response, subagent to orchestrator
- The status vocabulary and the error vocabulary

**Deliberately outside:**
- Subagent-to-subagent messaging. There is none — the topology is hub-and-spoke, and every exchange goes through the orchestrator.
- Human conversation. When an agent needs to talk to a person it uses whatever user-interaction tool its harness provides; that traffic never appears in protocol messages.
- Artifact content below the provenance frontmatter. This protocol governs the message envelope and provenance stamp, not an artifact's substantive content.

### 2.2 Machine-to-Machine, Not Conversational

Both directions are structured JSON. No human is expected to read a protocol message, and no protocol message should read as if one might — no greetings, no narration, no hedging. A hook, a runner, or a log adapter must be able to lift every field out with a parser and no heuristics.

This is why the protocol is deliberately thin. Anything a downstream tool needs to *branch* on has a dedicated field; anything it merely needs to *display* goes into `status_message`.

### 2.3 Design Principles

| # | Principle | What it buys |
|---|---|---|
| 1 | **JSON envelope** | Trivially parseable by tooling, and a shape language models produce reliably. |
| 2 | **Word-shaped status codes** | `SUCCESS` reads correctly in a log line, a table cell, and a routing rule alike. Numeric codes would need a lookup table at every reading site. |
| 3 | **Minimal required surface** | A short mandatory field list keeps compliance high. Optional fields exist only where a real consumer needs them. |
| 4 | **Traceability by construction** | Every invocation carries an identity (`agent_instance_id`) and a run identity (`run_id`); the full history is reconstructible from the orchestration artifact. |
| 5 | **Autonomy-enabling** | `SUCCESS` is unambiguous enough that the orchestrator can advance without asking a human, which is what makes long unattended runs possible. |
| 6 | **Errors are the exception, not the frame** | Only one status carries error codes. The other five need no error machinery at all. |

### 2.4 Protocol Authority Over Harness Conventions

An agentic harness is not a neutral pipe. Several of them hold opinions about how agents should talk to each other, and express those opinions in places an agent cannot ignore: a subagent-invocation tool whose parameter schema describes what to put where, and instructions injected into a subagent's system prompt telling it how to report back to whatever called it.

Those opinions overlap this protocol, and they do not agree with it. The observed consequences, in both directions:

- **Dispatch side.** A tool schema offering a `description` field alongside the message body invites the orchestrator to fill both, producing a task statement in the metadata and a second one in the payload — two versions of the same instruction, free to disagree. Worse, an orchestrator that treats the schema as the authority may write the task in prose and never assemble the protocol message at all.
- **Response side.** A harness that tells subagents to summarise their work for a parent agent gets exactly that: a well-written report, no status code, and nothing for a routing decision to bind to.

Crucially, **this varies by harness and cannot be reasoned about from inside an agent.** Some harnesses inject nothing and leave a subagent entirely unaware it is running as one — which is why the protocol appeared to work flawlessly under one harness while degrading under another. The protocol did not change. The competing instructions did.

So v1.9 states the precedence explicitly, in both variants: **MOSAIC-authored instructions outrank harness-authored ones on anything concerning message shape.** The protocol message is the whole of what passes between agents; harness guidance about response formatting is subordinate, and the JSON object is the entire response no matter what else was suggested.

**Why this belongs in the protocol and not in per-harness content.** The rule is harness-independent — it holds under a harness that injects nothing, where it is merely satisfied trivially. Placing it in the protocol means it reaches every agent on every harness, including harnesses that do not exist yet: whoever adds the fifth harness inherits the rule without having to notice they need it. Per-harness content would instead have each harness author rediscover a system-wide invariant and restate it in their own words, which is how four subtly different versions of one rule come to exist.

**What does stay harness-specific:** the identity of the competing mechanism. "The `Task` tool's `description` field" is a fact about one harness and meaningless in another. Naming it is legitimate harness-layer content and belongs in that harness's injections — but the injection now only *names the field*, because the precedence rule it used to restate is canonical (§10.3).

**Why the receiver rule is narrow.** The response-side rule says only what an orchestrator must not do: infer a status code from prose. It deliberately stops short of prescribing recovery. Retry and escalation are orchestration policy (§11), and the same reasoning that rules out confidence scores (§4.4) rules out routing on how assured a paragraph sounds — both let an uncalibrated judgment of tone drive a decision that should rest on a declared value. Pinning this down is not merely tidiness: a deterministic runner and an LLM orchestrator are required to produce identical execution records for identical runs, and unspecified receiver behaviour is precisely where two implementations would diverge.

---

## 3. Task Invocation Message (Orchestrator → Subagent)

### 3.1 Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `agent_instance_id` | string | Yes | Identity of this specific invocation. Format `{AgentName}#{GlobalSequence}` (§3.2). Correlates the response back to the request. |
| `run_id` | string | Yes | Identity of the orchestration run this invocation belongs to (§3.3). |
| `task_description` | string | Yes | What the subagent is to accomplish, stated concretely enough to act on. |
| `input_artifacts` | array | Yes | Exact paths inside `Orchestration-{run_id}/` that the subagent may read. A path listed only here is read-only. `[]` when there are none. |
| `output_artifacts` | array | Yes | Exact paths or bounded `*` patterns inside `Orchestration-{run_id}/` that the subagent may read, create, or update. `*` stays within one path segment. `[]` when there are none. |
| `input_files` | array | No | Project files outside all `Orchestration-*` directories worth starting from. Advisory; the subagent may read any project file it judges relevant. |
| `output_files` | array | No | Project files outside all `Orchestration-*` directories expected to change. Advisory; the subagent may write any project file it needs to. |
| `constraints` | string | No | Restrictions on this assignment's scope or deliverable that neither the agent's own instructions nor its input artifacts state — e.g. "Phase 1 requirements only". Not for method, and not for environment facts such as paths, interpreter names, or harness quirks; those are appended to `task_description`. Omit when there are none. |
| `include_result_summary` | boolean | No | When `true`, the response must carry `result_data`. Defaults to `false`. |
| `human_in_the_loop` | boolean | No | When `true`, activates the output review gate (§3.6). Defaults to `false`. |

### 3.1.1 Invocation Acceptance and Rejection

The receiving subagent applies the acceptance check before doing work. There is no separate transport validator: in harness-native mode the orchestrator's dispatch is a tool call that executes as it is produced.

An invocation is rejected when any §7.1 check fails or when the requested work falls outside the receiving agent's scope. The subagent performs no task work and accesses no files, then returns `BLOCKED` with `E100 INVALID_INVOCATION`. `error_reason` identifies the failed fields, rules, or scope boundary so the orchestrator can correct the invocation or routing.

The ordinary response envelope cannot always describe this condition: its two correlation fields are values the invalid invocation may not contain. `E100` therefore has one narrow exception. The rejection echoes each usable non-empty string identifier exactly as received and omits an absent, empty, or wrongly typed identifier. It never manufactures a placeholder. The orchestrator already owns the dispatch interaction and correlates the rejection to that interaction when an identifier is unavailable.

This is rejection by the receiving subagent, not a transport feature. A harness may carry the message without understanding any field in it. The orchestrator's responsibility begins when it receives `E100`: correct the named defects in the next dispatch.

### 3.2 Agent Instance ID

Format: `{AgentName}#{GlobalSequence}`

- **AgentName** — the agent's own name, exactly as the workflow table and the agent file spell it (`codebase-research`, `plan-review`, `checkpoint-manager-git`).
- **GlobalSequence** — a single counter that increments across *all* invocations in the run, not per agent type.

The counter being global rather than per-agent is a deliberate choice, and it pays for itself three ways:

1. **Uniqueness is free.** No pair of invocations in a run can ever collide, regardless of type.
2. **The orchestrator's bookkeeping is one integer.** Increment, use, record.
3. **Hooks can filter at two granularities without extra state.** `implementation-tdd#*` selects every invocation of that agent; `implementation-tdd#5` selects exactly one.

It also gives every invocation a natural join key: the sequence number is the primary key of the orchestration artifact's execution log, so phase, stage, timing and outcome are all recoverable from the id alone.

Examples within one run:

| Id | Reading |
|---|---|
| `codebase-research#1` | First invocation of the run |
| `planner-tdd-soft#2` | Second invocation of the run |
| `implementation-tdd#3` | Third |
| `codebase-research#7` | Seventh — research running again, new instance, new number |

### 3.3 Run Identity

`run_id` names the orchestration run. It is minted once, when the run's orchestration artifact is created, and is never regenerated mid-run.

Format: `{YYYYMMDD}T{HHMMSS}Z-{4-char-hex}` — for example `20260129T090000Z-a3f9`.

Its consumers:

- **Subagents** need it to stamp the artifacts they produce, so an artifact can name the run that created it rather than depending on which folder it happens to sit in.
- **Storage** derives from it — a run's artifacts live in `Orchestration-{run_id}/`, which is what lets several runs proceed in one workspace without overwriting each other's blackboard.
- **Observers** (log adapters, analyzers) use it to group events by run without inferring the grouping from timing.

The response echoes `run_id` back unchanged, exactly as it echoes `agent_instance_id`. Under single-run orchestration nothing routes on the echoed value; it is carried anyway so that an orchestrator coordinating several concurrent runs becomes possible later without a second protocol revision. Cheap now, expensive to retrofit.

Obligation is asymmetric by design, and the deployed text says so explicitly: the orchestrator always emits `run_id`, a receiving subagent rejects an invocation without a usable value, and the orchestrator does not route on a non-conforming response that omits or changes it outside the `E100` exception. Auxiliary consumers — loggers, analyzers, anything in the optional tooling tier — must degrade quietly instead. An observer is not permitted to break a run it is only watching.

### 3.4 Orchestration Artifact and Project File Access

This is the single most consequential boundary in the protocol. Classification and authorization are separate questions.

**Classification comes from location.** Every file inside a directory named `Orchestration-*` is orchestration state. The directory for this invocation is exactly `Orchestration-{run_id}/`; directories carrying another run id belong to other runs. A file does not become a project file merely because the invocation omitted it.

**Authorization comes from list membership.** Inputs are exact paths. An output entry is either exact or contains `*`, which matches characters within one path segment and never crosses `/`:

| List membership | Read | Write |
|---|---:|---:|
| `input_artifacts` only | Yes | No |
| `output_artifacts` only | Yes | Yes |
| Both lists | Yes | Yes |
| Neither list | No | No |

Output artifacts are readable because an output path may already contain state from an earlier pass, such as a prior review round or continuation. Before its first write to an output artifact, the subagent checks whether the file exists. It never assumes a listed output is empty. When the file exists, replacing or truncating it requires reading it first unless the task explicitly calls for wholesale replacement. This prevents a destination-shaped path from silently discarding workflow state while leaving genuinely fresh-output tasks free to replace by instruction.

A wildcard output authorizes each matching concrete path; it does not authorize directory discovery or non-matching paths.

**No discovery.** A subagent does not enumerate or search the current run's directory for additional context. Unlisted files are other workflow state, and files in another run's directory are stale or unrelated state. Both are off limits even when their names look relevant.

**Project files are everything outside all `Orchestration-*` directories.** Source, configuration, documentation, and test results remain fully autonomous within the agent's scope. `input_files` and `output_files` point at useful starting places and expected results; they grant no special permission and cannot be used to reclassify orchestration state as project files.

| Item | Classification | Access |
|------|---------------|--------|
| `Orchestration-{run_id}/Design.md`, in `input_artifacts` only | Current-run orchestration artifact | Read-only |
| `Orchestration-{run_id}/Plan.md`, in `output_artifacts` | Current-run orchestration artifact | Read and write; check for existing content first |
| `Orchestration-{run_id}/Research.md`, unlisted | Current-run orchestration artifact | **Off limits** |
| `Orchestration-{other-run-id}/Research.md` | Other-run orchestration state | **Off limits** |
| `src/UserService.ts` | Project file | Autonomous within agent scope |
| `test-results/report.xml` | Project file | Autonomous — a results folder is not an orchestration directory |

The closed half prevents agents from absorbing another step's or another run's context and quietly widening their responsibility. The open half prevents incomplete project-file hints from paralysing implementation on a real codebase.

### 3.5 Context Discipline

Each subagent starts with a fresh context window. The invocation should spend that window on the task, not on history.

**Send:**
- The task description
- Artifact paths (strict) and file hints (advisory)
- Constraints
- `include_result_summary` when an inline summary is genuinely needed
- `human_in_the_loop` when the output requires human sign-off

**Do not send:**
- Conversation history
- Prior agents' outputs pasted inline — the agent reads them from artifacts
- Background reasoning about why this step exists

Restating a previous agent's `status_message` inside the next task description is the most common violation and the most damaging one: it substitutes a lossy summary for the artifact the receiving agent would otherwise read in full, and it biases that agent toward the previous agent's framing. Pass the artifact; trust the artifact.

Leaving `include_result_summary` at its default is likewise the right call whenever the orchestrator intends to read the artifact anyway — a summary it does not use is pure context cost.

### 3.6 Human-in-the-Loop

`human_in_the_loop: true` installs an **output review gate**. It is not a general instruction to be chatty, and "complete output" means a complete inventory rather than a reproduction of every file.

The rules:

1. The gate fires **last**, after the work is finished and after every written output artifact carries `human_approved: false`.
2. The agent uses its user interaction tools and begins by stating its `agent_instance_id` and `run_id`.
3. The agent sends a review request that inventories every orchestration artifact created or updated, every output artifact the task asks it to review, and every project file created, modified, or deleted. Every path remains visible and carries its action plus a brief description of the material change and its purpose. Related paths may share a group-level description. There is no fixed length limit: the description is as detailed as the user needs to understand what changed without reproducing the file.
4. Full contents and diffs are omitted unless the user asks for them. File size, encoding, and format do not change the review-request rule.
5. The review request asks the user to approve the described output or request changes.
6. If the user asks for changes, the agent makes them and sends a new review request containing the complete updated inventory. **The gate re-arms on every change**, and each artifact content write resets `human_approved` to `false`.
7. **Mid-task interaction does not discharge the gate.** Asking a clarifying question halfway through is normal agent behaviour and satisfies nothing — HITL is specifically about reviewing finished output.
8. If the agent has no way to reach a human at all, it returns `BLOCKED` with `E503` rather than silently proceeding unreviewed.
9. Only after the user approves the latest review request with no further changes does the agent set `human_approved: true` in every output artifact the approved review request covered, through a metadata-only write, and return its response. Coverage, not authorship, is the test: a gate-discharge re-dispatch (§9.7) writes no content, yet its review covers the artifacts it was sent to review. A listed output the request did not cover keeps whatever stamp its last writer left; flipping it would certify a review that never happened.

Rule 7 is the one that needs stating explicitly, because without it "I consulted the user" becomes a claim an agent can satisfy by having asked anything at all, and the gate stops meaning what it was introduced to mean.

### 3.7 Example Invocation

```json
{
  "agent_instance_id": "codebase-research#1",
  "run_id": "20260129T090000Z-a3f9",
  "task_description": "Analyze the requirements document and identify key functional requirements, risks, and dependencies.",
  "input_artifacts": [],
  "output_artifacts": ["Orchestration-20260129T090000Z-a3f9/Research.md"],
  "input_files": ["docs/requirements.md", "docs/project-brief.md"],
  "output_files": [],
  "constraints": "Focus only on Phase 1 requirements; maximum 500 words for summary",
  "include_result_summary": true,
  "human_in_the_loop": false
}
```

---

## 4. Task Response Message (Subagent → Orchestrator)

### 4.1 Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `agent_instance_id` | string | Yes, except unusable in an `E100` rejection | Echoed unchanged from the invocation. Omitted rather than invented when absent, empty, or wrongly typed in an invalid invocation. |
| `run_id` | string | Yes, except unusable in an `E100` rejection | Echoed unchanged from the invocation. Omitted rather than invented when absent, empty, or wrongly typed in an invalid invocation. |
| `status_code` | string | Yes | One of the six codes in §5. |
| `status_message` | string | Yes | One or two sentences describing the outcome and naming modifications, or stating that nothing changed and why. |
| `result_data` | string | Conditional | Key findings. Present if and only if the invocation set `include_result_summary: true`. |
| `error_code` | string | BLOCKED only | Machine-branchable blocker category, format `E{category}{number}`. Present **only** with `BLOCKED`. |
| `error_reason` | string | BLOCKED only | Plain-language explanation of the blocker. Present **only** with `BLOCKED`. |

**There is no field listing what the agent modified.** The orchestrator establishes that itself — timestamps, hashes, `git status` — because a self-reported change list is both redundant and, when an agent forgets to update it, actively misleading. The agent describes its work in prose in `status_message` and leaves detection to the mechanism that cannot be wrong about it.

### 4.2 Error Codes

Error codes accompany `BLOCKED` and nothing else. Their purpose is to let a hook or a runner branch on the *kind* of blocker without reading English.

| Prefix | Category | Meaning |
|---|---|---|
| `E1xx` | Input | The invocation is invalid or a required task resource is absent |
| `E4xx` | Dependency | Supplied context explicitly establishes that prerequisite work is incomplete |
| `E5xx` | External | The environment — tools, permissions, people — is not cooperating |

| Code | Name | Condition |
|------|------|-----------|
| `E100` | INVALID_INVOCATION | The Task Invocation Message fails one or more §7.1 checks or requests work outside the receiving agent's scope |
| `E101` | REQUIRED_RESOURCE_NOT_FOUND | An authorized input artifact, or a resource explicitly required by `task_description` or `constraints`, does not exist |
| `E401` | PREREQUISITE_INCOMPLETE | The invocation or an authorized input artifact explicitly states that required prerequisite work is incomplete |
| `E501` | TOOL_UNAVAILABLE | An external tool or service is down, timing out, or absent |
| `E502` | PERMISSION_DENIED | A required file or resource cannot be read or written |
| `E503` | USER_CONTACT_UNAVAILABLE | `human_in_the_loop: true` but there is no channel to a human |

`input_files` and `output_files` remain advisory hints. A missing hinted path does not become an `E101` condition merely because it appears in either list; a project resource is required only when `task_description` or `constraints` says so. Likewise, a missing resource never proves that predecessor work is incomplete. `E401` requires explicit evidence in the invocation or an authorized input artifact.

The three-category split is what the orchestrator's tiered response keys off: `E5xx` is often worth an automatic retry, while `E1xx` and `E4xx` require changing the invocation or repairing prerequisite state. Repeating the same message will not make an invalid invocation valid, conjure a missing resource, or complete prerequisite work.

### 4.3 Response Examples

**`SUCCESS`, summary requested:**
```json
{
  "agent_instance_id": "codebase-research#1",
  "run_id": "20260129T090000Z-a3f9",
  "status_code": "SUCCESS",
  "status_message": "Requirements analysis completed. Created Research.md with 12 functional requirements and 3 risks identified.",
  "result_data": "Analyzed requirements.md. Key findings: (1) Core functionality requires 12 features across 3 modules. (2) NFRs include <2s response time. (3) Identified risks: agent coordination complexity, state management, testing challenges. Full details in Research.md."
}
```

**`SUCCESS`, no summary requested (the default):**
```json
{
  "agent_instance_id": "codebase-research#1",
  "run_id": "20260129T090000Z-a3f9",
  "status_code": "SUCCESS",
  "status_message": "Requirements analysis completed. Created Research.md with 12 functional requirements and 3 risks identified."
}
```

**`COMPLETED_NEEDS_ACTION`:**
```json
{
  "agent_instance_id": "plan-review#5",
  "run_id": "20260129T090000Z-a3f9",
  "status_code": "COMPLETED_NEEDS_ACTION",
  "status_message": "Review complete. Found 3 critical issues requiring fixes. Details written to plan-review.md."
}
```
`result_data` is absent because the findings are in the artifact and the orchestrator will read them there.

**`BLOCKED`:**
```json
{
  "agent_instance_id": "implementation-tdd#10",
  "run_id": "20260129T090000Z-a3f9",
  "status_code": "BLOCKED",
  "status_message": "Cannot proceed. Required input artifact Design.md does not exist.",
  "error_code": "E101",
  "error_reason": "REQUIRED_RESOURCE_NOT_FOUND: Orchestration-20260129T090000Z-a3f9/Design.md not found"
}
```

**`NEEDS_CLARIFICATION`:**
```json
{
  "agent_instance_id": "implementation-tdd#15",
  "run_id": "20260129T090000Z-a3f9",
  "status_code": "NEEDS_CLARIFICATION",
  "status_message": "Requirements ambiguous. Design specifies 'secure authentication' but doesn't specify OAuth vs JWT vs session-based. Please clarify which approach to use."
}
```

**`CAPABILITY_EXCEEDED`:**
```json
{
  "agent_instance_id": "implementation-tdd#20",
  "run_id": "20260129T090000Z-a3f9",
  "status_code": "CAPABILITY_EXCEEDED",
  "status_message": "Unable to implement distributed consensus algorithm. Attempted 3 approaches but couldn't get it working. Task requires specialized domain knowledge - recommend human expert review."
}
```

### 4.4 No Confidence Scores

Agents do not report a confidence number, and the protocol has no field for one. Self-assessed confidence from a language model is not calibrated, so a number would invite routing decisions on a value that does not mean what it appears to mean.

An uncertain agent has two honest options instead:

1. Ask the user directly, through whatever user-interaction tool it has, and resolve the uncertainty; or
2. Return `NEEDS_CLARIFICATION` and describe precisely what it could not decide.

Both produce something actionable. A `0.6` does not.

---

## 5. Status Codes

Six codes. Each describes what happened in **this** invocation. The workflow table and orchestration policy determine the concrete route.

### 5.1 `SUCCESS`

**Means:** The task is done, completely and correctly. Everything requested was produced.

**Return it when:** all acceptance criteria are met, all declared output artifacts exist in the state they should, and nothing went wrong.

**Routing implication:** follow the workflow's success route.

**Looks like:** research written up; implementation compiling with tests green; validation confirming a clean run.

### 5.2 `COMPLETED_NEEDS_ACTION`

**Means:** The assignment is complete, and the agent-specific status mapping classifies its result as requiring action.

**Return it when:** the assignment's own action condition was met. That condition is defined in the agent's ErrorHandling section, not inferred from workflow position or from unfinished work outside the assignment.

**Routing implication:** follow the workflow's configured action route. If no target resolves from the workflow table, escalate rather than inventing one.

**Looks like:** a completed assignment whose agent-specific mapping says its recorded outcome requires follow-up.

**Not incomplete work.** The assignment itself is finished. Ordinary downstream work, anticipated workflow steps, and missing deliverables outside the agent's scope do not trigger this status.

### 5.3 `PARTIALLY_DONE`

**Means:** The assignment is incomplete, useful continuation state is preserved, and more work on the same assignment remains.

**Return it when:** requested items remain unattempted, attempted work still fails acceptance criteria, or the invocation stops before completion to preserve quality. Nothing external blocks continuation and no missing decision is required.

**Routing implication:** dispatch a fresh invocation for the same workflow assignment. The successor continues from the recorded state rather than restarting.

**Looks like:** two of five services implemented; three of seven documents analyzed; implementation written but acceptance tests still failing.

This is the code that makes incomplete but continuable work expressible. It covers both a deliberate stopping point and attempted work whose acceptance criteria are not yet met.

### 5.4 `NEEDS_CLARIFICATION`

**Means:** The agent cannot proceed confidently without more information.

**Return it when:** the task or the requirements are ambiguous; several valid approaches exist and choosing between them is not the agent's call; the context handed over is incomplete (an unspecified interface, a missing contract); or an earlier phase left something contradictory.

**Routing implication:** supply available context and re-invoke, or escalate when the answer requires a human decision.

**Looks like:** "performance or maintainability — which wins here?"; "does 'update user' include password changes?"; "three architectures fit; which constraints should decide?"

### 5.5 `CAPABILITY_EXCEEDED`

**Means:** The agent had everything it needed and still could not do it.

**Return it when:** the attempt was made, approaches were exhausted, and the task is simply beyond what this agent can do.

**Routing implication:** escalate to the human. The orchestrator does not invent or substitute an agent.

**Looks like:** "three attempts at the algorithm, none working"; "this needs domain expertise I don't have."

**Distinct from `BLOCKED`:** here nothing external is in the way. The inputs were all present; the agent was the limiting factor.

### 5.6 `BLOCKED`

**Means:** Something outside the agent prevents the work from starting or continuing.

**Return it when:** the invocation is invalid; a required artifact is missing; a prerequisite has not run; an external service is unavailable; permissions are refused; HITL is demanded and no human is reachable.

**Orchestrator does:** consults the error code and responds by tier — retry the transient, escalate the structural.

**Carries error codes.** `BLOCKED` is the only status that does, precisely because it is the only one where the *category* of the problem determines the response.

**Looks like:** a dispatch missing `run_id` (`E100`); a required Design.md artifact that does not exist (`E101`); an authorized progress artifact explicitly recording an unfinished prerequisite (`E401`); a search API that is down (`E501`); a write refused (`E502`); HITL requested with no user channel (`E503`).

### 5.7 Decision Matrix

| Situation | Code | Routing interpretation |
|-----------|------|------------------------|
| Assignment complete; no action condition | `SUCCESS` | Workflow success route |
| Assignment complete; agent-specific action condition met | `COMPLETED_NEEDS_ACTION` | Workflow action route, or escalate if unresolved |
| Assignment incomplete; continuation state preserved | `PARTIALLY_DONE` | Fresh invocation of the same workflow assignment |
| Missing information or decision | `NEEDS_CLARIFICATION` | Supply context or escalate |
| Inputs available, but agent cannot complete | `CAPABILITY_EXCEEDED` | Escalate; do not substitute an agent |
| An explicitly required resource is absent | `BLOCKED` (`E101`) | Supply the resource or route to its producer |
| Explicit evidence that prerequisite work is incomplete | `BLOCKED` (`E401`) | Route to the prerequisite work |
| The environment is uncooperative | `BLOCKED` | Retry or escalate by error policy |

---

## 6. Worked Exchanges

### 6.1 Research completes, with human review

**Invocation**
```json
{
  "agent_instance_id": "codebase-research#1",
  "run_id": "20260129T090000Z-a3f9",
  "task_description": "Analyze the user story document and extract all acceptance criteria. Identify ambiguities or missing information.",
  "input_artifacts": [],
  "output_artifacts": ["Orchestration-20260129T090000Z-a3f9/Research.md"],
  "input_files": ["docs/user-stories.md", "docs/product-vision.md"],
  "output_files": [],
  "constraints": "Focus on MVP features only",
  "human_in_the_loop": true
}
```

**Response**
```json
{
  "agent_instance_id": "codebase-research#1",
  "run_id": "20260129T090000Z-a3f9",
  "status_code": "SUCCESS",
  "status_message": "Successfully extracted 24 acceptance criteria from 8 user stories. User reviewed and approved Research.md. Created Research.md."
}
```

**Orchestrator:** advances to planning.

### 6.2 Review finds issues

**Invocation**
```json
{
  "agent_instance_id": "implementation-review#8",
  "run_id": "20260129T090000Z-a3f9",
  "task_description": "Review the UserService implementation against the design contract. Identify any deviations or issues.",
  "input_artifacts": ["Orchestration-20260129T090000Z-a3f9/Design.md"],
  "output_artifacts": ["Orchestration-20260129T090000Z-a3f9/implementation-review.md"],
  "input_files": ["src/services/UserService.ts"],
  "output_files": [],
  "human_in_the_loop": false
}
```

**Response**
```json
{
  "agent_instance_id": "implementation-review#8",
  "run_id": "20260129T090000Z-a3f9",
  "status_code": "COMPLETED_NEEDS_ACTION",
  "status_message": "Review complete. Found 5 issues requiring implementation fixes. Details in implementation-review.md."
}
```

**Orchestrator:** routes back to the implementation agent to address the five issues.

### 6.3 Implementation lands, tests do not

**Invocation**
```json
{
  "agent_instance_id": "implementation-tdd#12",
  "run_id": "20260129T090000Z-a3f9",
  "task_description": "Implement UserService according to design. Ensure all unit tests pass.",
  "input_artifacts": ["Orchestration-20260129T090000Z-a3f9/Design.md"],
  "output_artifacts": ["Orchestration-20260129T090000Z-a3f9/Stage-1/PlanProgress.md"],
  "input_files": ["src/interfaces/IUserService.ts"],
  "output_files": ["src/services/UserService.ts", "src/services/UserService.test.ts"],
  "constraints": "Read operations only; account deletion is out of scope for this stage",
  "human_in_the_loop": false
}
```

**Response**
```json
{
  "agent_instance_id": "implementation-tdd#12",
  "run_id": "20260129T090000Z-a3f9",
  "status_code": "PARTIALLY_DONE",
  "status_message": "Implemented UserService; 9 of 12 tests pass and coverage is 73%. Three failing behaviors and the remaining work are recorded in PlanProgress.md."
}
```

**Orchestrator:** dispatches a fresh implementation invocation to continue the same assignment from PlanProgress.md.

### 6.4 Deliberate partial completion

**Invocation**
```json
{
  "agent_instance_id": "implementation-tdd#15",
  "run_id": "20260129T090000Z-a3f9",
  "task_description": "Implement all 5 service modules: UserService, AuthService, PaymentService, NotificationService, ReportingService.",
  "input_artifacts": ["Orchestration-20260129T090000Z-a3f9/Design.md"],
  "output_artifacts": ["Orchestration-20260129T090000Z-a3f9/Stage-1/PlanProgress.md"],
  "input_files": ["src/interfaces/"],
  "output_files": ["src/services/"],
  "human_in_the_loop": false
}
```

**Response**
```json
{
  "agent_instance_id": "implementation-tdd#15",
  "run_id": "20260129T090000Z-a3f9",
  "status_code": "PARTIALLY_DONE",
  "status_message": "Implemented 2 of 5 services (UserService, AuthService) with full test coverage. Stopping due to context limits. Remaining: PaymentService, NotificationService, ReportingService. Continuation context in PlanProgress.md."
}
```

**Orchestrator:** dispatches `implementation-tdd#16`, a fresh invocation of the same assignment; the successor finds the remaining three in the artifact. The successor picks up continuation context from the artifact, not from the response.

### 6.5 Blocked on a missing required resource

**Invocation**
```json
{
  "agent_instance_id": "implementation-tdd#20",
  "run_id": "20260129T090000Z-a3f9",
  "task_description": "Implement PaymentGateway module according to design specification.",
  "input_artifacts": ["Orchestration-20260129T090000Z-a3f9/Design.md"],
  "output_artifacts": ["Orchestration-20260129T090000Z-a3f9/Stage-2/PlanProgress.md"],
  "input_files": ["src/interfaces/IPaymentGateway.ts"],
  "output_files": ["src/integrations/PaymentGateway.ts"],
  "human_in_the_loop": false
}
```

**Response**
```json
{
  "agent_instance_id": "implementation-tdd#20",
  "run_id": "20260129T090000Z-a3f9",
  "status_code": "BLOCKED",
  "status_message": "Cannot proceed. Required Design.md artifact does not exist.",
  "error_code": "E101",
  "error_reason": "REQUIRED_RESOURCE_NOT_FOUND: Orchestration-20260129T090000Z-a3f9/Design.md not found"
}
```

**Orchestrator:** supplies the missing required artifact or corrects its path before dispatching the implementation task again. The absent advisory `input_files` path does not contribute to `E101`.

### 6.6 Blocked with no human reachable

**Invocation**
```json
{
  "agent_instance_id": "library-research#25",
  "run_id": "20260129T090000Z-a3f9",
  "task_description": "Research database options and recommend approach.",
  "input_artifacts": [],
  "output_artifacts": ["Orchestration-20260129T090000Z-a3f9/LibraryResearch.md"],
  "input_files": ["docs/requirements.md"],
  "output_files": [],
  "human_in_the_loop": true
}
```

**Response**
```json
{
  "agent_instance_id": "library-research#25",
  "run_id": "20260129T090000Z-a3f9",
  "status_code": "BLOCKED",
  "status_message": "Cannot complete. Task requires human-in-the-loop output review but no user contact tools available.",
  "error_code": "E503",
  "error_reason": "USER_CONTACT_UNAVAILABLE: human_in_the_loop is true but agent has no means to contact user"
}
```

**Orchestrator:** escalates to the user. It re-dispatches without HITL only if the user explicitly waives the gate.

### 6.7 Invalid invocation is rejected before work

**Invocation**
```json
{
  "agent_instance_id": "codebase-research#30",
  "task_description": "Analyze the requirements.",
  "input_artifacts": [],
  "output_artifacts": []
}
```

**Response**
```json
{
  "agent_instance_id": "codebase-research#30",
  "status_code": "BLOCKED",
  "status_message": "Rejected the invocation before starting work because a required correlation field is missing.",
  "error_code": "E100",
  "error_reason": "INVALID_INVOCATION: run_id is absent"
}
```

**Orchestrator:** corrects the invocation, including a valid `run_id`, and dispatches it again. The response omits `run_id` because none was available to echo; it does not invent one.

---

## 7. Validation

### 7.1 Invocation

| # | Check | Required |
|---|-------|----------|
| 1 | Valid JSON | Yes |
| 2 | `agent_instance_id` present, matching `{AgentName}#{Number}` | Yes |
| 3 | `run_id` present and non-empty | Yes |
| 4 | `task_description` present and non-empty | Yes |
| 5 | `input_artifacts` present, an array (may be empty) | Yes |
| 6 | `output_artifacts` present, an array (may be empty) | Yes |
| 6a | Every artifact path or output wildcard pattern is inside exactly `Orchestration-{run_id}/` and does not escape it through `..`; input paths contain no wildcard | Yes |
| 6b | No `input_files` or `output_files` path is inside any `Orchestration-*` directory | Yes |
| 7 | `input_files`, if present, is an array | No |
| 8 | `output_files`, if present, is an array | No |
| 9 | `constraints`, if present, is a string | No |
| 10 | `include_result_summary`, if present, is a boolean | No |
| 11 | `human_in_the_loop`, if present, is a boolean | No |

### 7.2 Response

| # | Check | Required |
|---|-------|----------|
| 1 | Valid JSON | Yes |
| 2 | `agent_instance_id` identical to the invocation's; omitted only for `E100` when the invocation supplied no usable string | Conditional |
| 3 | `run_id` identical to the invocation's; omitted only for `E100` when the invocation supplied no usable string | Conditional |
| 4 | `status_code` is one of the six | Yes |
| 5 | `status_message` present, one to two sentences, names modifications or states that nothing changed and why | Yes |
| 6 | `result_data` present exactly when `include_result_summary: true` was sent | Conditional |
| 7 | `error_code` present when `BLOCKED` | Yes |
| 8 | `error_reason` present when `BLOCKED` | Yes |
| 9 | `error_code` and `error_reason` **absent** when not `BLOCKED` | Yes |

Check 9 matters as much as checks 7 and 8. An error code attached to a non-`BLOCKED` response will be picked up by anything branching on the presence of the field, and will send the run down a recovery path for a blocker that does not exist.

### 7.3 Patterns

```
# Agent instance id — agent names are kebab-case, so hyphens and digits are legal
^[A-Za-z][A-Za-z0-9-]*#\d+$

# Run id
^\d{8}T\d{6}Z-[0-9a-f]{4}$

# Status code
^(SUCCESS|COMPLETED_NEEDS_ACTION|PARTIALLY_DONE|NEEDS_CLARIFICATION|CAPABILITY_EXCEEDED|BLOCKED)$

# Error code — categories E1xx, E4xx, E5xx
^E[145]\d{2}$
```

---

## 8. Why Six Status Codes

This section exists to be argued with. Its purpose is to give any future proposal to add a seventh code something concrete to fail against.

### 8.1 Which "task" a status describes

"Task" is ambiguous across five nested scopes:

| Layer | Scope | Example | Complete when |
|-------|-------|---------|---------------|
| **L0** | One agent invocation | `implementation-tdd#3`'s work | The agent returns |
| **L1** | A chain of same-type invocations | `#1 → #2 → #3` | All the code is written |
| **L2** | A feedback loop | implement → review → implement → review | Review finds nothing |
| **L3** | A deliverable | "user authentication" | The feature works |
| **L4** | A workflow phase | EXECUTION | Every feature is done |

An agent only ever perceives L0. A review agent cannot know whether it is the only review step or the first of three; it has no view of the loop it sits inside. It does its work and reports.

### 8.2 Status codes are strictly L0

A status code says what happened during **this invocation**. Nothing more.

L1 through L4 are the orchestrator's problem, tracked through the orchestration artifact, the workflow table, and the history of status codes received. Keeping the codes at L0 is what allows an agent to be reused in any workflow without knowing anything about it — the moment a code tries to describe higher-layer state, the agent needs to know its position in a workflow, and workflow-agnostic agents become impossible.

### 8.3 The taxonomy

| Outcome at L0 | Code | What the orchestrator hears |
|---------------|------|------------------------------|
| Assignment complete, no action condition | `SUCCESS` | "This assignment is done" |
| Assignment complete, mapped action condition met | `COMPLETED_NEEDS_ACTION` | "Use the workflow's action route" |
| Assignment incomplete, continuable | `PARTIALLY_DONE` | "More of this assignment remains" |
| Stalled, needs information | `NEEDS_CLARIFICATION` | "Give me an answer and I'll continue" |
| Stalled, past its limits | `CAPABILITY_EXCEEDED` | "Find another way" |
| Stalled, environment | `BLOCKED` | "Fix the world first" |

### 8.4 Why six is exhaustive

Enumerate the outcome space and it closes:

| # | Category | Code |
|---|----------|------|
| 1 | Complete, agent-specific action condition not met | `SUCCESS` |
| 2 | Complete, agent-specific action condition met | `COMPLETED_NEEDS_ACTION` |
| 3 | Incomplete, continuation state preserved | `PARTIALLY_DONE` |
| 4 | Incomplete, information blocker | `NEEDS_CLARIFICATION` |
| 5 | Incomplete, capability blocker | `CAPABILITY_EXCEEDED` |
| 6 | Incomplete, environment blocker | `BLOCKED` |

The reasoning: the assignment is either complete or it is not. If complete, its agent-specific action condition either applies or it does not. If incomplete, either the same assignment can continue from preserved state, information is missing, the agent's capability is exhausted, or the environment prevents work. There is no seventh branch.

### 8.5 The two distinctions people get wrong

**`COMPLETED_NEEDS_ACTION` vs `PARTIALLY_DONE`** — the first question is whether the current assignment is complete.

| | `COMPLETED_NEEDS_ACTION` | `PARTIALLY_DONE` |
|---|---|---|
| The agent's own assignment | Complete | Incomplete |
| Status basis | Agent-specific action condition was met | Continuation state exists and the same assignment remains |
| Workflow knowledge required | None | None |
| Outside-scope future work | Does not trigger the status | Does not trigger the status |
| Signal | "Use the configured action route" | "Continue this assignment" |

**`CAPABILITY_EXCEEDED` vs `BLOCKED`** — the question is whether the limiting condition is internal or external.

| | `CAPABILITY_EXCEEDED` | `BLOCKED` |
|---|---|---|
| Cause | The agent's own limits | Something external |
| Example | "Can't work out the algorithm" | "Design.md doesn't exist" |
| Response | Escalate to the human; do not substitute an agent | Repair the environment or escalate by error policy |

### 8.6 Resisting a seventh code

Any proposed addition must clear all three bars:

1. It names an outcome none of the six covers.
2. It requires an orchestrator response none of the six triggers.
3. It cannot be conveyed by an existing code plus detail in `status_message`.

Proposals that have failed the test:

| Proposed | Already is | Why |
|----------|-----------|-----|
| `DELEGATED` | `COMPLETED_NEEDS_ACTION` | A completed assignment whose mapped outcome uses an action route |
| `NEEDS_REVIEW` | `COMPLETED_NEEDS_ACTION` | A completed assignment whose agent-specific mapping requires review |
| `TIMEOUT` | `BLOCKED` | The environment prevented the work |
| `RETRY_SUGGESTED` | Not a status at all | A recommendation about routing, which is the orchestrator's decision |
| `LOW_CONFIDENCE` | `NEEDS_CLARIFICATION` | Uncertainty needing input; also see §4.4 |
| `PARTIAL_SUCCESS` | `PARTIALLY_DONE` | The same thing, renamed |

A code that maps onto an existing outcome adds vocabulary without adding meaning, and every reading site pays for it.

### 8.7 Codes report, they do not command

A status describes an outcome. It does not name a target. The workflow table and orchestration policy supply the concrete route:

| Status | Usual | Also reasonable |
|--------|-------|-----------------|
| `SUCCESS` | Follow workflow success route | Workflow may place a review there |
| `COMPLETED_NEEDS_ACTION` | Follow configured action route | Escalate when no target resolves |
| `PARTIALLY_DONE` | Continue the same workflow assignment | Re-scope only through workflow or human decision |
| `NEEDS_CLARIFICATION` | Supply available context | Escalate when the answer is a human decision |
| `CAPABILITY_EXCEEDED` | Escalate to the human | Stop with the capability reason when no interactive escalation channel exists |
| `BLOCKED` | Apply error-code recovery | Escalate when recovery cannot resolve it |

The agent says **what happened**. The orchestrator decides **what to do next**. Keeping that boundary sharp is what allows routing policy to change without touching a single agent.

---

## 9. The Artifact Provenance Stamp

Every artifact an agent produces carries three frontmatter fields naming the run and the invocation that wrote it, and recording whether the human review gate was discharged. The deployed text is in §1.1; this section is why it says what it says.

The stamp was a separate contract with its own document and version until v1.10. §10.6 states why it merged.

### 9.1 Why the stamp exists at all

The information is, strictly speaking, recoverable elsewhere. The run's folder name encodes `run_id`; the orchestration artifact's Artifacts registry records a creator per artifact path. So the stamp is redundant — and it earns its place regardless, for three reasons.

**A file that is moved keeps its identity.** Folder-derived provenance survives exactly as long as the folder does. Artifacts get copied into issue trackers, attached to reviews, pasted into a report, archived out of the run folder. The stamp travels with the bytes; the path does not.

**The reader may not have the orchestration artifact.** The registry answers the question only for someone holding the blackboard and willing to parse a table in it. A person opening `Design.md` on its own, or a tool ingesting a directory of artifacts, has the frontmatter and nothing else.

**Two independent records disagree usefully.** When the registry says one thing and the stamp says another, something went wrong — a stale file, an artifact written by an agent that never declared it, a folder assembled by hand. A single record can be wrong silently; two cannot.

### 9.2 Fields

| Field | Type | Required | Source | Description |
|-------|------|----------|--------|-------------|
| `run_id` | string | Yes | The invocation's `run_id`, copied verbatim | Identity of the orchestration run that produced this artifact. An invocation without a usable `run_id` is rejected before artifact writes (§9.5). |
| `created_by` | string | Yes | The invocation's `agent_instance_id`, copied verbatim | Identity of the invocation that most recently wrote this file. Format `{AgentName}#{GlobalSequence}`. |
| `human_approved` | boolean | Yes | The constant `false` on every content write; `true` only via the flip described in §9.6 | Whether the human-in-the-loop output review gate was discharged for the write that produced this file's current content. |

The first two values arrive in the task invocation message and are written out unchanged — not parsed, reformatted, shortened, or validated by the writing agent, since an agent that reformats a value it did not mint introduces a second spelling of one fact. The third is a constant on write and a state thereafter.

Example, on an artifact that had no frontmatter before:

```yaml
---
run_id: "20260129T090000Z-a3f9"
created_by: "codebase-research#1"
human_approved: false
---

# Research: Authentication
...
```

**Three fields, no more.** Each is a value the agent already holds or a constant. Nothing must be derived, computed, or judged — an agent that must *decide* a stamp value can get it wrong; an agent that copies or constants one cannot.

**`created_by` names an invocation, not an agent.** The value carries the global sequence number: `codebase-research#7`, not `codebase-research`. Within one run the same agent type may run several times, and "which of those seven invocations wrote this file" is a materially different question from "what kind of agent wrote this file" — the sequence number joins the artifact to a specific row of the orchestration artifact's execution log, recovering phase, stage, timing, and returned status. A bare agent name would join to a set of rows and answer none of it.

### 9.3 Rewrites overwrite

When an agent writes an artifact that already exists — a successor continuing partially-done work, a planner revising a plan after review — it overwrites all three fields with its own values. `human_approved` is among them, so a rewrite returns it to `false` regardless of what a previous writer left there. The stamp names the **most recent** writer, not the original creator.

This costs something: original-creation provenance is lost. It is still right. The question a reader actually asks of a file is "is this current, and who is answerable for what I am reading now" — and the current writer answers that. The full write history is not lost; it is in the execution log, where a full history belongs. Keeping a `created_by` alongside a `modified_by` in the frontmatter would replicate a chronology in the one place least equipped to hold it: two fields cannot record three writes.

The field name is admittedly a poor fit for last-writer semantics. It is retained because it is what every agent in the catalogue already says, and renaming it is a change with a migration cost and no functional gain (§14).

### 9.4 Merging into existing frontmatter

Where the target file already opens with a `---` delimited YAML block, the fields are merged into that block. A second frontmatter block is never created.

The failure this rule prevents is specific and easy to fall into: an agent writing its stamp by prepending `---\nrun_id: ...\n---\n` to a file that already begins with `---` produces a document whose first block contains the stamp and whose apparent body starts with a second `---`. Most frontmatter parsers read the first block and treat everything after as content, so the original frontmatter silently becomes part of the body. The artifact still looks fine to a human and has lost every field it declared.

### 9.5 Absent run identity

When the invocation carries no usable `run_id`, the subagent rejects it with `E100` before doing work or accessing files. It does not write an artifact and therefore has nothing to stamp.

This is stricter than treating provenance as optional because `run_id` no longer serves provenance alone. It identifies the current orchestration directory, bounds artifact authorization, correlates the response, and separates concurrent runs. Without it, a subagent cannot determine which orchestration state it may access. Minting a value locally would be worse: the invented identifier would match no dispatch and could authorize the wrong directory while appearing valid.

Older artifacts may legitimately lack a `run_id`; readers of artifacts at rest may degrade gracefully for that historical case. That compatibility rule does not permit a current subagent to accept a new invocation without one.

### 9.6 The human approval flag

`human_approved` reports whether the output review gate was discharged for the write that produced the file's current content.

**On the name.** The field was `hitl_confirmed` through v1.9. Two things were wrong with it. It named the field after the *dispatch flag* rather than after what is true of the file, which reads oddly in a stamp whose other two fields answer "which run" and "who wrote it" — and it does so in vocabulary only an orchestration reader holds, in the one place §9.1 justifies precisely by the reader who holds nothing else. And "confirmed" is weaker than the condition that produces the flip: the value goes `true` when the user has asked for no further changes, which is approval, not merely having looked. A name that understates the bar invites a flip that clears it.

This is the same trade §9.3 declines for `created_by`, and it resolves the other way for a specific reason: `created_by` has live readers and this field has none yet (§15), so the migration cost is a sweep of text with no run, artifact, or tool depending on the old spelling. That window closes the moment §9.7's verification ships.

**Why a gate flag belongs in a provenance stamp.** It is not provenance in the strict sense — it records whether a process obligation was discharged, not where the file came from. It earns its place by naming a consumer that cannot do its job without it. The orchestrator dispatches `human_in_the_loop: true` and otherwise has no way whatsoever to tell whether the gate was honoured: it receives a status code and a prose sentence, and a subagent that skipped the review returns exactly what one that honoured it returns. And the flag shares the stamp's scope exactly — it describes what happened during the write of this file, by the invocation named in `created_by`, for the run named in `run_id`. Same subject, same lifetime, same reader.

#### The problem it addresses

The gate is a prose obligation with no mechanism behind it. In practice agents skip it, or ask something in the middle of the work, finish, and never return. Nothing detects this. Adding more emphatic wording has a ceiling, and that ceiling has been reached. The flag takes a different route — not more insistence, but a different kind of obligation.

#### Why writing `false` first is the point

The instruction is not "record whether you did the review." It is "write `false`, then earn `true`." Those produce very different behaviour.

Recording after the fact asks the agent to remember an obligation it formed some time ago, at the end of a long stretch of work that displaced it. Writing `false` first converts that obligation into **file state**: there is now a field in a file saying the gate is not discharged, and an explicit rule for what closes it. An open loop written down outlasts one held in attention. This is the whole of the mechanism, and it is why the ordering is not negotiable — a flag written once at the end would be a self-report and nothing more.

#### Rewrite as mechanical re-arm

The hardest-to-enforce clause of the gate is that it **re-arms on every output change**: present, absorb feedback, present again, until the user stops asking. It is the clause agents most often drop, because after two rounds the work feels finished.

The rewrite rule enforces it without asking anyone to remember it. Applying requested changes rewrites the artifact; rewriting stamps `human_approved: false`; the gate is armed again as a property of the file. The loop is closable only by a write that changes nothing but the flag — which, by construction, cannot happen while the user is still requesting changes.

A prose rule became a state transition.

#### The regress carve-out

The flip to `true` is itself a write, so a literal reading of "every write stamps `false`" never terminates. §1.1 therefore states the exception outright: **a write that changes only `human_approved` is not a content write and does not reset the field.** This is stated rather than left to inference, because an agent reasoning its way to the exception is an agent that might instead reason its way into a loop, or into treating the whole rule as unworkable and abandoning it.

#### What it does not catch

Three limits, stated plainly, because a check believed to be stronger than it is is worse than a weak check known to be weak.

**It is still self-reported.** An agent can write `true` without presenting anything. The mechanism catches *forgetting* — the observed failure — because an agent that loses the gate also loses the flip. It does not catch *fabrication*. The gain is that a skipped gate becomes an explicit false claim in a durable file rather than a silent omission in a discarded context.

**Re-arm across invocations is weaker than within one.** Within an invocation the reset is mechanical. Across invocations it is not: a planner invoked to revise `Plan.md` after review opens a file a predecessor left stamped `true`, and the file's own state now argues against the rule rather than for it. If that agent skips the gate *and* preserves the stale `true`, the orchestrator's check passes on a gate nobody discharged — a false negative. Two things blunt it, neither decisive: the rule is unconditional and does not depend on the field's current value, and the three fields are written as one act, so the failure requires an agent that updates `created_by` to its own id while selectively preserving `human_approved` from another.

**Invocations producing no artifacts are invisible to it.** With `output_artifacts: []` there is nothing to stamp, while the gate still applies — it covers project files too. Every agent writing only source files is outside the check.

#### Why the limits are acceptable

They fall where the gate matters least. HITL's value concentrates overwhelmingly on the **first version of an artifact** — that is when direction is set, when a wrong turn is cheapest to correct, and when leaving it uncorrected is most expensive. Re-invocations after a review are correcting against findings already established.

The mechanism is at **full strength on exactly that first write**: no prior value in the file, nothing arguing against `false`, the reset purely mechanical. It weakens only on rewrites, where the gate matters less. The same reasoning covers the no-artifact hole: an invocation producing no orchestration artifact usually produced no durable deliverable for a human to review.

The alignment is the argument: **the mechanism is strongest where the gate is most valuable, and weakest where it is least.** A verification scheme with the opposite profile would be worth more work.

### 9.7 The orchestrator's verification

The flag has no effect unless something reads it. **The orchestrator verifies it**, and that verification is what makes the stamp part of this contract rather than an audit convenience (§10.6).

**When.** Immediately after an invocation dispatched with `human_in_the_loop: true` returns, whatever its status, and before routing on its status code. `BLOCKED` with `E503` is exempt. That response is the agent reporting that the gate could not run. Verifying it would only re-dispatch a review to an agent that has already said it cannot reach a human, and would delay the immediate escalation `E503` requires.

**What is read.** The frontmatter of each concrete output artifact the invocation created or modified — including wildcard matches — and nothing below it. This is the same set the orchestrator already detects for the Artifacts registry. Expanding wildcards matters: a wildcard output is exactly where an unstamped artifact would otherwise go unchecked. A listed output that does not exist, or that the invocation left unchanged, is not checked: the subagent had nothing of its own to stamp there, and treating absence as a missed gate would re-dispatch a review of a file nobody wrote.

Reading only the frontmatter is a deliberate narrowing, not a new permission. The orchestrator already reads certain artifacts for routing, so artifact access is established. What is at stake is context discipline: an orchestrator that reads plans and designs in full begins forming opinions about their content, and a workflow-agnostic router with opinions about domain content is on its way to making domain decisions. A frontmatter read costs a handful of lines and cannot produce an opinion about anything.

**The check.** Dispatched `human_in_the_loop: true` and any output artifact carrying `human_approved: false` — or omitting the field — is a gate that was not discharged.

**The response: re-dispatch the same agent type to discharge the gate.** Not a failure route. The invocation's work is finished and, so far as anything indicates, finished correctly. What is missing is a review, and a review is cheap to obtain against output that already exists. The follow-up invocation carries the concrete output files as both inputs and outputs, preserves the original project-file hints plus any project paths attributed by change detection, sets `human_in_the_loop: true`, and asks for the complete inventory-and-approval procedure from §3.6. The orchestrator variant of §1 carries the worked example.

The alternatives were weighed and rejected. Routing it as a failed invocation would discard work completed correctly apart from the gate. Escalating to the user spends a human interruption on something the re-dispatch resolves by itself — and spends it to ask permission for a review the user was already meant to be given. Recording the discrepancy and advancing turns the field into an audit trail with no enforcement, surfacing skipped gates after the run rather than during it.

**A re-dispatch that comes back still `false` is a different problem.** The first miss is plausibly forgetting; the second, against a task description naming the flag explicitly, is not. That is the point to escalate, and the point at which the run has learned something about the agent rather than about the invocation.

**Why routing uses the original status after a repaired gate.** The re-dispatch's assignment is only the review, so a clean review correctly returns `SUCCESS`. Routing on it would erase what the original invocation reported: a reviewer's `COMPLETED_NEEDS_ACTION` would open the quality gate on findings nobody fixed, and a `PARTIALLY_DONE` would skip its continuation. `last_agent` names the re-dispatch because it is the trailing workflow row, and recovery's row-versus-`last_agent` check must agree with it. `last_status` carries the original's code because that is what recovery routes on. The Execution Log keeps both rows exactly as returned. A non-`SUCCESS` re-dispatch reports something new about the review itself (no user channel, a question raised in review), so it is routed as returned.

**Where this obligation is written.** In the orchestrator variant of §1, alongside the routing table and the error-code responses.

This was previously delegated to the orchestrator's own instructions, on the reasoning that a re-dispatch is a routing decision and routing is orchestration policy. That reasoning does not survive §10.6: a field the orchestrator reads and routes on is hard interop, and both halves of a handshake belong in one deployed contract or they drift apart per deployment (§10.1). The delegation also produced a concrete defect — the subagent variant told subagents the orchestrator performs this check while nothing on the orchestrator side was instructed to perform it. The block already carries routing content, including a HITL-triggered re-dispatch under `E503`, so this is not a new kind of text for it.

The orchestrator still stamps nothing and carries no provenance obligations of its own (§9.9). Reading a field is not writing one.

### 9.8 Scope: which files are stamped

Concrete files written under `output_artifacts`, including wildcard matches, are stamped. Files named in `output_files` are not. Nothing else is touched.

Classification and stamping use different tests. Location classifies every file under an `Orchestration-*` directory as orchestration state (§3.4). Membership in `output_artifacts` determines which current-run artifacts this invocation may write and therefore must stamp. An unlisted orchestration file remains orchestration state, but is off limits and receives no stamp from this invocation.

| Item | Classification and permission | Stamped |
|------|-------------------------------|---------|
| `Orchestration-{run_id}/Design.md`, listed in `output_artifacts` | Writable current-run artifact | Yes |
| `Orchestration-{run_id}/Stage-1/PlanProgress.md`, listed in `output_artifacts` | Writable current-run artifact | Yes |
| `Orchestration-{run_id}/Research.md`, unlisted | Orchestration state, off limits | No — it must not be touched |
| `Orchestration-{other-run-id}/Research.md` | Other-run orchestration state, off limits | No — it must not be touched |
| `src/services/UserService.ts` | Project file listed in `output_files` | No |
| `docs/architecture.md`, written at the agent's own discretion | Project file | No |

**Why project files are excluded.** They belong to the project, not to the run. A stamp in a source file is a foreign annotation with a lifetime far shorter than the file's: it goes stale the moment a human edits the file, survives into commits and releases, and names a run that will mean nothing to whoever reads it next. Worse, for many file types the stamp is not merely unwanted but invalid — there is no correct place to put YAML frontmatter in a `.ts` file, a `.json` config, or a `.go` source file, and an agent attempting it produces a syntax error.

The two unlisted rows deserve emphasis. Broad project-file autonomy never extends into an orchestration directory. Creating an undeclared file there would write workflow state the orchestrator did not authorize and no later invocation could lawfully discover. If the workflow needs another artifact, the orchestrator must name it in a later invocation rather than the subagent inventing it in place.

### 9.9 Consumers, and why the orchestrator stamps nothing

The stamp has readers, and enumerating them is what keeps the field list from growing on speculation.

| Consumer | Reads | For |
|----------|-------|-----|
| **A resuming orchestrator** | `run_id` | Confirming that artifacts found in a run folder belong to the run being resumed, rather than to a previous run whose output was left in place. |
| **A human auditor** | Both | Answering "what produced this" from the file alone, including after the file has been moved out of the run folder. |
| **Log and artifact correlation tooling** | Both | Joining an artifact to its invocation's log events and execution-log row via `created_by`, and to the run via `run_id`. |
| **Review and audit agents** | `created_by` | Attributing a finding to the invocation responsible for the material under review. |
| **The dispatching orchestrator** | `human_approved` | Verifying that a gate it requested was actually discharged, immediately after the invocation returns (§9.7). |

A proposal to add a fourth field should name a consumer that cannot do its job with these three, in the same way a proposed status code must name an orchestrator response none of the existing six triggers (§8.6). `human_approved` was itself admitted against that bar, not around it.

**The orchestrator carries no stamp obligations.** It writes exactly one file, the orchestration artifact, and the stamp text appears only in the subagent variant of §1. `run_id` is already a set-once frontmatter field of that artifact, minted at creation — a stamp rule would restate an obligation the artifact's own schema imposes, in a second document free to drift from the first. And `created_by` has no value it could carry: instance ids are minted *by* the orchestrator *for* subagents, so any orchestrator variant would have to invent a sentinel that exists to satisfy a schema rather than to inform a reader.

---

## 10. Deployment Model

### 10.1 One source, many copies

Protocol text is identical across every deployed agent, and that is precisely why it must not be maintained in every agent file. Wording kept in one place per agent drifts in as many directions: one agent gets a fix, the rest keep the defect, and the divergence is invisible until an agent misreports.

So the arrangement inverts. This file holds the text; agent source files hold an empty, named slot. At deployment the tool copies the appropriate block from §1 into the slot, overwriting whatever is there. Protocol wording never has to be edited in an agent file, and a protocol change is a one-file edit followed by a redeploy.

### 10.2 The two variants

Two blocks exist because the orchestrator and its subagents genuinely need different text — this is not duplication that should be collapsed.

| | Subagent block | Orchestrator block |
|---|---|---|
| Message it composes | The response | The invocation |
| Message it receives | The invocation | The response |
| Status codes framed as | Which one to return, and when | How to route each one on arrival |
| Error codes framed as | When to raise each | How to respond to each |
| Carries the HITL obligation | Yes — it is the party that must present output | No — it only sets the flag |
| Carries field obligation semantics | No | Yes — it is the producer of `run_id` |
| Protocol authority framed as | "Ignore harness instructions about how to report" | "Don't duplicate the task into metadata fields; don't infer a status from prose" |

Merging them would hand every subagent a routing table it can never act on, and hand the orchestrator a set of "return this when" rules for messages it never returns. Both halves would be dead weight in a system prompt, and dead weight in a system prompt is not free — it competes for attention with the instructions that matter.

The compound section names (`CommunicationProtocol:Subagent`, `CommunicationProtocol:Orchestrator`) follow the same convention as workflow sections, so a tool can enumerate every variant by prefix rather than knowing their names ahead of time. A third variant — should some future agent class need one — is added here and picked up without a code change.

### 10.3 What the tool does

1. Read this file; parse the frontmatter's `sections` list to learn which block serves which role.
2. For each agent being deployed, determine its role and select the matching block.
3. Locate the top-level `<CommunicationProtocol type="managed">` region in the agent file and replace its **entire** body with the selected block's content. Set the `version` attribute on the region's opening tag to the frontmatter's `version` value.

The region carries `type="managed"` rather than `type="core"`, and the distinction is load-bearing: the type attribute in the file states who owns the region. Content with `type="core"` is authored in the MOSAIC source and carried through byte-identically; content with `type="managed"` is written and regenerated by the tool on every deploy. A reader of an agent source file therefore knows, from the tag alone and without consulting code or documentation, that the region is not theirs to edit. In a source file the region is empty; the deployed file is the only place it has content.

The region sits at body top level rather than nested inside a section, occupying slot 2 of the canonical document order.

**There is no injection region accompanying the deployed block.** The protocol shape is not a project variable. A project that could append its own text to the protocol could contradict it, and a contradiction inside the section is strictly worse than one outside it: the agent has no way to tell which half is canonical, and the whole point of §2.4 is that precedence questions get settled once, centrally, rather than per deployment.

**A project that genuinely needs to extend the protocol mechanics** — stating how messages are delivered across network boundaries, for example — uses `<ProtocolExtension type="custom">` as a top-level sibling of this region, never nested inside it. That region is project-invented (type `custom`) and carries no advisory parent in the MOSAIC catalogue. The guidance is unchanged: extend the mechanics (transport, delivery, environment-specific handling); do not restate or contradict what the contract fixes (message shape, status and error vocabularies, the HITL gate). `ProtocolExtension` is not a catalogued project-type region name — MOSAIC defines project-type (`type="project"`) slots in source; this one is a project invention and belongs in a custom-type region (see `AgentTemplateArchitecture.md` §6.2.1).

The absence pays a second dividend: the deployed region's canonical body is exactly the source block, which makes a visual diff against a deployed agent meaningful.

**The division of labour with harness content.** Harness-layer injections may name a specific mechanism that competes with this protocol on a given harness — a subagent-invocation tool's metadata fields, an injected reporting convention — because those names are meaningful only on that harness. What they must not do is restate the precedence rule itself: that is canonical (§2.4), reaches every agent already, and a per-harness paraphrase of it can only drift from the original. The test for whether content belongs in a harness injection is whether it names something that exists on one harness and not another. "MOSAIC protocol takes precedence" fails that test. "The field is called `description`" passes it.

### 10.4 Version tracking

The frontmatter `version` field is the protocol version. It appears in two further places, and all three are expressions of one fact that must change together:

| Where | Form | Read by |
|---|---|---|
| This file's frontmatter | `version: "1.12"` | The deployment tool, as the source of truth |
| The region's opening tag `version` attribute | `version="1.12"` | Staleness checks |
| The block's opening sentence | "You operate under **Communication Protocol v1.12**" | The agent |

The `version` attribute is what makes staleness checkable: a deployed agent whose region tag names an older version than this file's frontmatter is out of date, and the check is a string comparison against the attribute value — no parsing of prose, and no dependence on the wording of the sentence that follows.

The sentence exists for a different reader. It tells the agent what contract it is operating under, in the body text it actually attends to; an attribute on a tag says nothing to a language model that a tag ever says. Two audiences, two expressions, one version string.

A protocol change is therefore always the same three steps: edit the block or blocks in §1, bump `version` in the frontmatter and in the block's opening sentence, add a changelog row. Then redeploy.

### 10.5 The frontmatter is deliberately small

The frontmatter carries four things a script genuinely needs — `id` and `type` to classify the file, `version` for the staleness check in §10.4, and `sections` for the variant mapping in §10.2 — plus `name`, `description`, `author` and `status` for display and convention. Nothing else.

In particular it **does not restate the status code or error code vocabularies**, and should not be extended to. Both lists already exist as tables inside the canonical blocks, and those blocks are the text that actually reaches an agent. A second copy in frontmatter would be the less authoritative one, which makes it worse than merely redundant: a conformance check reading it would pass while the deployed table said something different, and would report confidence in exactly the case it was built to catch. A tool needing the vocabulary parses it from the block.

The same reasoning rules out a `message_format` field or anything else describing properties of the protocol that no consumer branches on. If a future component genuinely needs a fact about this protocol in machine-readable form, the test is whether it can already get that fact from the canonical block; only when it cannot does the fact earn a frontmatter field.

### 10.6 Artifact Provenance Is Part of This Contract

The artifact provenance stamp was formerly a second contract with its own document, its own version, and its own `<ArtifactProvenance type="managed">` region sitting immediately after this one. It is now part of this contract, deployed inside this region, and versioned once (§12, v1.10). Its reasoning is §9.

**Why it merged.** The two were separated on the grounds that they govern different media — this protocol the JSON envelope in flight, the stamp the frontmatter at rest — with different lifetimes and different readers. That reasoning was sound while the stamp was an audit convenience that nothing in the run consulted. It stopped being sound when the orchestrator began verifying `human_approved` (§9.7): a field the orchestrator reads and routes on is hard interop, not documentation.

That makes the stamp the **secondary layer beneath the JSON response**. The response can claim anything, and the orchestrator otherwise has to take it on trust. With the stamp verified, the orchestrator can establish that an artifact was produced, correctly attributed, and reviewed by the user — three facts the envelope alone cannot carry credibly, because the party making the claim is the party being checked.

Two things follow, and both are the point of merging rather than side effects. One contract means **one version number**: a subagent and an orchestrator either agree about message shape *and* stamp obligations or they do not, and there is no longer a combination where they agree about one and disagree about the other. And `run_id` stops being a value shared across a boundary — it is transported and recorded under a single contract, which is where a rule about copying it verbatim belongs.

---

## 11. Non-Goals

- **Subagent-to-subagent messaging.** The topology is hub-and-spoke by design; there is no channel to specify.
- **Human conversation.** Protocol messages carry no user-facing content. HITL sets an obligation (§3.6); the mechanics of talking to a person belong to the harness.
- **The content of an artifact below its frontmatter.** The stamp governs three keys; what the file says is the producing agent's business.
- **The orchestration artifact's schema.** How the execution log records an invocation is that artifact's contract, not this one — even though several of its columns are copied straight from protocol fields.
- **Retry policy, backoff timing, and escalation thresholds.** The protocol supplies the error code; what an orchestrator or runner does with `E501` versus `E101` is orchestration policy.
- **Transport.** How a message physically reaches an agent — CLI invocation, harness subagent call, anything else — is a harness concern. This document specifies content only.
- **Multi-run routing.** `run_id` is carried in both directions so that a coordinator handling several concurrent runs is possible later. The routing logic for that is not designed here.

---

## 12. Changelog

| Version | Date | Summary |
|---------|------|---------|
| 1.12 | 2026-09-26 | **Artifact access, HITL review, status semantics, and input failure handling made operational.** Every file under any `Orchestration-*` directory is orchestration state; only authorized paths in the current run's directory are accessible. Inputs use exact paths; outputs may use bounded `*` patterns within one path segment. Inputs grant read access, outputs grant read/write access, and existing outputs are protected from blind replacement. The deployed invocation examples identify required fields, optional fields, and boolean defaults. `result_data` is present if and only if requested, with no arbitrary word limit. A `status_message` names modifications or states that nothing changed and why. The HITL review request inventories every changed path with its action and material-change summary while preserving the false-before-review and true-after-approval stamp sequence. `E503` escalates immediately; HITL is removed only after explicit human waiver. Status is now strictly assignment-local: `COMPLETED_NEEDS_ACTION` is a completed assignment whose agent-specific action condition was met; `PARTIALLY_DONE` is an incomplete but continuable assignment, including unresolved acceptance failures. The status table is the sole deployed status-selection guidance; duplicate Key Rules were removed. Subagents no longer receive routing actions, concrete targets come only from workflow configuration, and `CAPABILITY_EXCEEDED` escalates without agent substitution. A subagent rejects a malformed or out-of-scope invocation before work with `BLOCKED` and `E100`; an `E100` response omits an unusable correlation identifier rather than inventing one, and the orchestrator corrects the invocation or routing in its next dispatch. `E101` now means an explicitly required resource is absent and never follows from an advisory project-file hint alone; `E401` requires explicit evidence that prerequisite work is incomplete and is never inferred from absence. Stale maintainer references to separate provenance, a marker comment, and v1.9 were corrected. The `human_approved: true` flip covers exactly the output artifacts the approved review request covered: those the invocation wrote, plus any the task asked it to review, which is what lets a gate-discharge re-dispatch that writes no content complete the gate. Any other listed output keeps its prior stamp. Gate verification runs whatever status was returned, except `BLOCKED` with `E503`, which already reports that the gate could not run and is routed directly to escalation. It reads only output artifacts the invocation created or modified, so a listed but absent or unchanged output is not a gate miss. `constraints` is defined once: scope or deliverable restrictions the agent's instructions and inputs do not state — never method or environment facts, which are appended to `task_description`. The deployed input examples say so, and the coverage-threshold example was replaced with a scope restriction. §6.4's `PARTIALLY_DONE` example dispatches a fresh invocation of the same assignment rather than implying re-scoping, and §5.7's "prerequisite is missing" row is split into `E101` (explicitly required resource absent) and `E401` (explicit evidence of incomplete prerequisite work). After a HITL re-dispatch discharges the gate with `SUCCESS`, the orchestrator routes on and records the original invocation's status and error code, with `last_agent` naming the re-dispatch; any other re-dispatch status is routed as returned. Maintainer-text corrections: §3.6 rules 1 and 2 are separate list items again, and §14's rejected per-project extension now points to the custom `ProtocolExtension` sibling region that §10.3 specifies rather than to the agent's instruction body. |
| 1.11 | 2026-09-03 | **E503 escalation priority corrected.** The orchestrator variant's error-code table previously listed E503 (USER_CONTACT_UNAVAILABLE) with "Re-invoke without HITL flag or escalate", making silent HITL bypass the primary option. Changed to "Escalate to human — re-invoke without HITL flag only if the human explicitly waives the gate." The old wording caused the script-mode orchestrator to resolve every E503 by dropping HITL, completing runs with zero human review despite the workflow declaring HITL on critical steps. The subagent variant is unchanged — its E503 row describes the condition, not the response. |
| 1.10 | 2026-08-05 | **Artifact provenance merged in.** The provenance stamp — `run_id`, `created_by`, `human_approved`, written into every file named in `output_artifacts` — was a separate contract with its own document, version, and `<ArtifactProvenance type="managed">` region. It is now part of this contract: the text ships inside the subagent variant of §1.1, the reasoning is §9, and there is one version number where there were two. The merge is correct because the orchestrator **verifies** `human_approved` (§9.7) — a field it reads and routes on is hard interop, not an audit convenience, and it is the secondary layer under a JSON response that can otherwise claim anything. Two changes follow from the merge. The field formerly called `hitl_confirmed` is renamed **`human_approved`**: it is read from the artifact, where "HITL" is orchestration jargon a standalone reader does not hold, and "approved" is what the flip actually certifies — the user asked for no further changes. The rename is taken now because nothing yet reads the field, making this the cheapest it will ever be. And the **orchestrator variant gains a Verifying the Human-in-the-Loop Gate subsection** (§9.7), so the check the subagent variant promises is instructed on the side that must perform it; the orchestrator still stamps nothing (§9.9). Consequences: canonical document order drops from eight top-level slots to seven, and `<ArtifactProvenance type="managed">` and `<ArtifactProvenanceExtension type="project">` cease to exist. |
| 1.9 | 2026-08-03 | **Protocol authority over harness conventions.** Added a Protocol Authority subsection to both variants, establishing that MOSAIC-authored instructions outrank harness-authored ones on message shape. Subagents return the JSON object as their entire response regardless of harness guidance requesting a prose report or summary. Orchestrators put the whole protocol message in the payload field and treat harness metadata fields as bookkeeping carrying no task content, and must never infer a status code from prose when a response contains none. Added as Key Rule 1 in the subagent variant, renumbering the remaining rules. Motivated by harnesses whose subagent-invocation tool schema and injected reporting conventions partially duplicate — and contradict — this protocol. |
| 1.8 | 2026-08-01 | **Run identity in the envelope.** Added `run_id` to both the Task Invocation and Task Response messages, echoed the same way `agent_instance_id` is. Introduced Field Obligation Semantics in the orchestrator variant: producers always emit `run_id`; core consumers may enforce it, auxiliary consumers must degrade gracefully. Artifact paths throughout now use the run-scoped `Orchestration-{run_id}/` prefix. |
| 1.7 | 2026-04-05 | **HITL redefined as an output review gate.** `human_in_the_loop` now explicitly gates the agent's produced output — artifacts and project files both — which must be presented for review as the final action before returning. The gate re-arms on every output change. Mid-task interaction explicitly does not satisfy it. |
| 1.6 | 2026-02-17 | **Machine-to-machine nature made explicit.** Added the statement that this protocol is agent-to-agent, parsed programmatically, with no conversational text in either direction. |
| 1.5 | 2026-01-29 | **Status codes renamed for accuracy.** `COMPLETED_WITH_FINDINGS` → `COMPLETED_NEEDS_ACTION`, `PARTIAL_COMPLETION` → `PARTIALLY_DONE`, `UNABLE_TO_COMPLETE` → `CAPABILITY_EXCEEDED`. The new names state what the orchestrator must do about them. |
| 1.4 | 2026-01-29 | **Sixth status code added** for quality-driven partial completion. Added the layer model (L0–L4), the exhaustiveness argument for the taxonomy, and the slippery-slope test for proposed codes. |
| 1.3 | 2026-01-29 | **Status code rationale documented.** Recorded the reasoning behind the taxonomy so future changes could be evaluated rather than negotiated. |
| 1.2 | 2026-01-29 | **Artifacts separated from project files.** `input_artifacts`/`output_artifacts` became strict orchestration-artifact lists; `input_files`/`output_files` became advisory hints over project files with full agent autonomy. Added `human_in_the_loop` and `E503`. Removed the self-reported modified-artifacts field — the orchestrator detects changes itself. |
| 1.1 | 2026-01-28 | Status codes refined to five. Error codes reduced and confined to `BLOCKED`. Dropped redundant task and phase fields. Added `include_result_summary`. Introduced the compact extract for system instructions. |
| 1.0 | 2026-01-28 | Initial specification. |

---

## 13. Glossary

| Term | Meaning |
|------|---------|
| **Orchestrator** | The central coordinator: interprets the workflow, dispatches subagents, routes on status codes, maintains the orchestration artifact. |
| **Subagent** | A specialised agent performing one kind of work, invoked by the orchestrator and returning to it. |
| **Task Invocation Message** | The JSON message dispatching work from orchestrator to subagent. |
| **Task Response Message** | The JSON message reporting the outcome from subagent to orchestrator. |
| **Orchestration artifact** | A file inside any `Orchestration-*` directory. For the current run, exact list membership grants access: input means read; output means read/write; unlisted means off limits. Other runs are entirely off limits. |
| **Project file** | Any workspace file outside all `Orchestration-*` directories. Source, config, docs, results. Autonomous within the agent's scope. |
| **Agent instance id** | `{AgentName}#{GlobalSequence}` — the identity of one invocation. |
| **Global sequence** | The run-wide invocation counter; supplies the numeric half of every agent instance id. |
| **Run id** | `{YYYYMMDD}T{HHMMSS}Z-{4-char-hex}` — identity of one orchestration run. Minted once, carried in both message directions. |
| **Phase** | A named stage of a workflow, tracked by name rather than by number. |
| **Human-in-the-Loop (HITL)** | The output review gate. When active, the agent sends a final review request inventorying everything it changed, repeats the request after changes, and returns only after approval. Mid-task interaction does not satisfy it. |
| **Provenance stamp** | The three frontmatter fields — `run_id`, `created_by`, `human_approved` — written into every file named in `output_artifacts` (§9). |
| **Producer obligation** | The requirement that a sender emit a given field. |
| **Consumer enforcement** | What a receiver does when a field is missing. The receiving subagent rejects an invalid invocation, the orchestrator refuses to route on a non-conforming response, and auxiliary observers degrade. |

---

## 14. Open Ideas / Dead Ends

**Under consideration**

- **Enumerating the `run_id` echo's consumers.** Under single-run orchestration the response-side echo has no reader. It was added pre-emptively to avoid a second version bump later. If nothing consumes it by the time multi-run coordination is designed, the question of whether it earns its place should be reopened rather than assumed settled.
- **Whether the receiver rule needs a defined outcome, not just a prohibition.** v1.9 forbids inferring a status from a prose-only response but leaves recovery to orchestration policy. If the deterministic runner and an LLM orchestrator turn out to diverge in practice despite both obeying the prohibition, the resolution is to specify the recovery too — at which point it stops being policy and becomes protocol.
- **A machine-checkable conformance test for deployed protocol sections.** Version-string comparison (§10.4) catches staleness but not local edits that preserve the version. A hash of the canonical block, recorded at deployment, would catch both. The bundle-sourced regions carry the identical gap for the identical reason, so this is worth solving once for every managed-type region rather than per source.

**Rejected**

- **Confidence scores.** Uncalibrated self-assessment invites routing decisions on a number that does not mean what it looks like. `NEEDS_CLARIFICATION` plus a specific question is strictly more actionable (§4.4).
- **A self-reported list of modified artifacts.** Redundant against timestamp, hash, and `git` inspection, and wrong exactly when it matters — an agent that forgot to update the list reports a clean run it did not have (§4.1).
- **A seventh status code.** Six proposals have been tested against §8.6 and all six mapped onto existing outcomes.
- **Leaving protocol precedence to per-harness injections.** One harness already carried a version of this rule as a local constraint, which is how the gap was found: it covered the dispatch side only, said nothing to subagents, and asserted that subagents would ignore competing instructions anyway — an assumption that holds only if something tells them to, and nothing did. A universal invariant maintained in four places is maintained in none of them (§2.4).
- **Forbidding orchestrators from filling harness metadata fields at all.** Tempting, but some harnesses require those fields, so a blanket prohibition would be unfollowable. The rule instead defines their *status* — bookkeeping, carrying no task content — which is enforceable everywhere and yields the same outcome where it matters.
- **A per-project protocol extension.** An earlier design had the tool append a `ProtocolExtension` injection after the deployed block, so a project could add its own protocol notes. Rejected because a project able to append to the protocol is a project able to contradict it, and a contradiction *inside* the section is worse than one outside: nothing tells the agent which half is canonical. Precedence is settled centrally (§2.4) precisely so it is not renegotiated per deployment. A project that needs to extend protocol mechanics uses a `<ProtocolExtension type="custom">` region as a top-level sibling of the contract region, never inside it (§10.3).
- **A single merged protocol section for all agent roles.** Would put a routing table into every subagent and return-code rules into the orchestrator, in both cases text the reader cannot act on (§10.2).
- **Numeric status codes.** Compact on the wire, but every log line, table cell, and routing rule would then need a lookup to be readable. The wire is not the constrained resource here; attention is.
- **Declaring the status and error vocabularies in frontmatter.** Attractive because a runner could then check its own handling against a YAML list instead of parsing a markdown table. Rejected because the list would be a second copy of what the canonical block already states, and the block is what ships — so the check would validate against the copy and stay green while the deployed text diverged. Parsing the block is marginally more work and is the only version of the check that can actually fail when it should (§10.5).

---

## 15. Open Items

- **The agent instance id pattern in §7.3 is broader than earlier drafts.** Agent names are kebab-case (`checkpoint-manager-git#4`), which a letters-only pattern rejects. The pattern here admits hyphens and digits. Any validator written against the narrower form must be updated, or it will reject valid ids from most of the agent catalogue.
- **The v1.8 date in §12 should be confirmed** against when the `run_id` change actually landed in the agent files.
- ~~**One harness injection now duplicates canonical content.**~~ **Resolved 2026-09-27.** `Catalog/HarnessInjections/Claude Code/HarnessInjectionsOrchestrator.md` no longer restates protocol precedence. The `Task`-tool field bullet went with it, so that injection currently names no competing mechanism at all — permitted, since §10.3 makes per-harness naming optional rather than required, but worth knowing if the two-versions-of-the-task failure in §2.4 is ever observed on that harness again. Its Design Rationale now records what the block deliberately omits and why.
- **The `hitl_confirmed` → `human_approved` rename is swept everywhere it can reach an agent.** Done: `Catalog/Subagents/Interface/approval-presenter.md` (v1.0.1), its row in `Catalog/Subagents/Interface/README.md`, `Development/Designs/DeploymentBlocks/ClosingProcedure.md`, and `Workflows/Verification/requirements-to-test-cases.md` (v1.2). The subagent files and both orchestrators take the new text from `<CommunicationProtocol type="managed">` on redeploy and need no hand edit. Two deliberate exceptions remain: the fixtures under `Tools/Deployment/testdata/golden/`, which regenerate from a redeploy and must not be hand-edited, and the analysis note `OnSuccessHITL.md`, which records the decision in the vocabulary of its date. §9.6 and §12 name the old spelling on purpose, so the rename stays traceable.
