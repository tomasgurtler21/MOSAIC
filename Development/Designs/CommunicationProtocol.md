---
id: communication-protocol
type: protocol
version: "1.10"
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

The two blocks below are the deployed protocol text. A deployment tool copies the block matching the target agent's role into that agent's `[[SECTION:CommunicationProtocol]]` slot, replacing whatever was there. Agent source files therefore do not maintain protocol wording of their own — they carry only the slot.

Everything below §1 is prose for maintainers. It is never deployed, and no tooling parses it.

Deployment mechanics — which block goes where, how the block is bounded, what the tool re-appends afterwards — are specified in §9.

### 1.1 Subagent Variant

[[SECTION:CommunicationProtocol:Subagent]]
## Communication Protocol

You operate under **Communication Protocol v1.10**. This protocol governs agent-to-agent communication, parsed programmatically by orchestration scripts. Both input and output are structured JSON - no conversational text.

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
  "constraints": "Optional restrictions",
  "include_result_summary": false,
  "human_in_the_loop": false
}
```

### Orchestration Artifacts vs Project Files
- `input_artifacts`/`output_artifacts` = **Orchestration artifacts** (STRICT: only access what's listed)
- `input_files`/`output_files` = **Hints** for project files. You have FULL autonomy over ANY file not listed as orchestration artifact.

**Rule:** You can ONLY access orchestration artifacts in your lists. You can freely access ANY other file.

### Human-in-the-Loop
When `human_in_the_loop: true`:
- You MUST present your complete output (artifacts AND project files you created/modified) to the user for review as your **final action** before returning your response
- If the user requests changes, apply them and present the updated output again — the gate re-activates on every change
- Mid-task user interactions (clarifications, questions) do NOT satisfy HITL — HITL = output review gate
- If no user contact tools are available, return BLOCKED with error_code E503

### Output Format

For SUCCESS, COMPLETED_NEEDS_ACTION, PARTIALLY_DONE, NEEDS_CLARIFICATION, CAPABILITY_EXCEEDED:
```json
{
  "agent_instance_id": "{AgentName}#{Number}",
  "run_id": "{run-identifier}",
  "status_code": "SUCCESS|COMPLETED_NEEDS_ACTION|PARTIALLY_DONE|NEEDS_CLARIFICATION|CAPABILITY_EXCEEDED",
  "status_message": "1-2 sentence description of outcome. Describe what was modified.",
  "result_data": "Only if include_result_summary was true in input"
}
```

For BLOCKED (includes error fields):
```json
{
  "agent_instance_id": "{AgentName}#{Number}",
  "run_id": "{run-identifier}",
  "status_code": "BLOCKED",
  "status_message": "1-2 sentence description of blocker",
  "error_code": "E101|E401|E501|E502|E503",
  "error_reason": "Human-readable explanation"
}
```

### Status Codes
| Status | Meaning | Orchestrator Action |
|--------|---------|---------------------|
| `SUCCESS` | Task done, proceed | Auto-advance to next phase |
| `COMPLETED_NEEDS_ACTION` | Task done, action items for another agent | Route to remediation agent |
| `PARTIALLY_DONE` | Some items done, more of same work needed | Route to successor agent (same type) |
| `NEEDS_CLARIFICATION` | Uncertain or context incomplete | Provide context or escalate |
| `CAPABILITY_EXCEEDED` | Task exceeds agent capability | Try alternative or escalate |
| `BLOCKED` | External factor preventing work | Resolve blocker or escalate |

### Error Codes (BLOCKED Only)
| Code | Name | Meaning |
|------|------|---------|
| `E101` | INPUT_NOT_FOUND | Required input file doesn't exist |
| `E401` | DEPENDENCY_MISSING | Predecessor task not complete |
| `E501` | TOOL_UNAVAILABLE | External tool/API unavailable |
| `E502` | PERMISSION_DENIED | Cannot read/write required resource |
| `E503` | USER_CONTACT_UNAVAILABLE | `human_in_the_loop: true` but no means to contact user |

### Key Rules
1. **Your entire response is the JSON object** — no prose before it, none after it, regardless of what your harness suggests
2. Echo `agent_instance_id` exactly as received
3. Echo `run_id` exactly as received
4. Always return `status_code`, `status_message`
5. Describe what you modified in `status_message`
6. Only include `result_data` if `include_result_summary: true` in input
7. Only include `error_code` and `error_reason` if status is `BLOCKED`
8. **Orchestration Artifacts (STRICT):** ONLY access orchestration artifacts listed in your `input_artifacts`/`output_artifacts`
9. **Project Files (FULL AUTONOMY):** You MAY read/modify/create ANY file NOT listed as orchestration artifact
10. **Human-in-the-loop:** If `human_in_the_loop: true`, present your complete output (artifacts + project files) to the user for review as your final action. The gate re-activates on every output change. Mid-task interactions don't satisfy HITL. (E503 if unable)
11. Use `SUCCESS` when ALL requested work is complete
12. Use `COMPLETED_NEEDS_ACTION` when your job IS to find issues (e.g., Review)
13. Use `PARTIALLY_DONE` when stopping mid-task for quality (some items done, more needed)
14. Use `NEEDS_CLARIFICATION` when uncertain or context is incomplete
15. Use `BLOCKED` + error code for external blockers
16. Use `CAPABILITY_EXCEEDED` when task is beyond your ability

### Artifact Provenance

Every file listed in `output_artifacts` must receive three frontmatter fields:

- `run_id` — copied verbatim from the task invocation's `run_id` field
- `created_by` — your own `agent_instance_id`
- `human_approved` — `false`

Files listed in `output_files` are project source files. Do not add provenance fields to them.

When rewriting an artifact that already exists, overwrite all three fields with the current writer's values.

When the artifact already has a YAML frontmatter block (`---` delimiters), merge the fields into the existing block rather than creating a second frontmatter block.

When `run_id` is absent from the task invocation, omit the `run_id` field rather than inventing one. Still stamp `created_by` and `human_approved`.

#### The `human_approved` Field

**Write `human_approved: false` every time you write an artifact.** Every write, without exception, whatever the value of `human_in_the_loop` in your invocation.

You may set it to `true` only in a separate final write that changes nothing else in the file, and only when `human_in_the_loop: true` was set, you have presented your complete output to the user, and they have asked for no further changes.

A write that changes only `human_approved` is not a content write and does not reset the field.

The full sequence when `human_in_the_loop: true`:

1. Write the artifact with `human_approved: false`.
2. Present your complete output — artifacts and project files both — to the user.
3. If the user requests changes, apply them. That rewrite returns `human_approved` to `false`. Go back to step 2.
4. Once the user asks for no further changes, set `human_approved: true` in every output artifact.
5. Return your response.

Where your invocation declares no output artifacts, there is nothing to stamp. Your review obligation is unchanged.

The orchestrator compares this field against the `human_in_the_loop` value it dispatched. An artifact stamped `false` on an invocation dispatched with `human_in_the_loop: true` is returned to you to complete the review.
[[/SECTION:CommunicationProtocol:Subagent]]

### 1.2 Orchestrator Variant

[[SECTION:CommunicationProtocol:Orchestrator]]
## Communication Protocol

You operate under **Communication Protocol v1.10**. This protocol governs agent-to-agent communication, parsed programmatically by orchestration scripts. Both input and output are structured JSON - no conversational text.

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
  "constraints": "Optional restrictions",
  "include_result_summary": false,
  "human_in_the_loop": false
}
```

### Task Response Message (Subagent → Orchestrator)
```json
{
  "agent_instance_id": "{echo from input}",
  "run_id": "{echo from run_id input}",
  "status_code": "SUCCESS|COMPLETED_NEEDS_ACTION|PARTIALLY_DONE|NEEDS_CLARIFICATION|CAPABILITY_EXCEEDED|BLOCKED",
  "status_message": "1-2 sentence outcome. Describe what was modified.",
  "result_data": "Only if include_result_summary was true in input",
  "error_code": "E101|E401|E501|E502|E503 (BLOCKED only)",
  "error_reason": "Human-readable explanation (BLOCKED only)"
}
```

### Orchestration Artifacts vs Project Files
- `input_artifacts`/`output_artifacts` = **Orchestration artifacts** (STRICT: only access what's listed)
- `input_files`/`output_files` = **Hints** for project files. Subagents have FULL autonomy over ANY file not listed as orchestration artifact.

**Rule:** Subagents can ONLY access orchestration artifacts in their lists. They can freely access ANY other project file.

### Status Codes and Routing Actions

| Status | Meaning | Your Routing Action |
|--------|---------|---------------------|
| `SUCCESS` | Task completed fully | **Auto-advance** to next subagent per workflow table |
| `COMPLETED_NEEDS_ACTION` | Task done, found issues for another subagent | **Route to fix target** (prior subagent) |
| `PARTIALLY_DONE` | Some items done, more of same work needed | **Route to successor** (same subagent type) |
| `NEEDS_CLARIFICATION` | Subagent uncertain, needs guidance | **Provide context**, callback to prior subagent, or escalate |
| `CAPABILITY_EXCEEDED` | Agent tried but couldn't do it | **Try closely matching alternative** if configured, otherwise **escalate to human** |
| `BLOCKED` | External factor preventing work | **Apply tiered error handling** based on error code |

### Error Codes (BLOCKED Only)

| Code | Name | Initial Response |
|------|------|------------------|
| `E101` | INPUT_NOT_FOUND | Check if artifact exists elsewhere, escalate if not |
| `E401` | DEPENDENCY_MISSING | Verify prerequisite task completed, escalate if not |
| `E501` | TOOL_UNAVAILABLE | Auto-retry with backoff (Tier 1) |
| `E502` | PERMISSION_DENIED | Escalate to human |
| `E503` | USER_CONTACT_UNAVAILABLE | Re-invoke without HITL flag or escalate |

### Field Obligation Semantics

**Producer obligation** is the requirement that a sender must emit a field. **Consumer enforcement** defines what happens when a receiver finds the field absent.

`run_id` is required by producer obligation: you (the orchestrator) always emit it in every Task Invocation Message, and subagents always echo it in every Task Response Message.

Consumer enforcement is tiered:
- **Core components** (runner, orchestrator): may reject the message or halt the orchestration run when `run_id` is absent or does not match expectations.
- **Auxiliary consumers** (logger, future analyzers): must degrade gracefully when `run_id` is absent or unreadable. An auxiliary consumer must never fail or crash an orchestration run because `run_id` is missing.

### Verifying the Human-in-the-Loop Gate

Subagents stamp every file they list in `output_artifacts` with `human_approved`. It is `false` on every content write, and becomes `true` only after the subagent has presented its output and the user has asked for no further changes. You stamp nothing yourself; you read this field.

**When:** immediately after any invocation you dispatched with `human_in_the_loop: true` returns, and before you route on its status code.

**What you read:** the frontmatter of each file named in that invocation's `output_artifacts`, and nothing below it. Never read further — artifact content is the subagents' business, and an orchestrator with opinions about it stops being workflow-agnostic.

**The check:** on an invocation dispatched `human_in_the_loop: true`, any output artifact carrying `human_approved: false`, or omitting the field, is a gate that was not discharged. An invocation declaring no output artifacts has nothing to check.

**The response: re-dispatch the same agent type to discharge the gate.** This is not a failure route — the work is finished, only the review is missing. Send the same artifacts as both `input_artifacts` and `output_artifacts`, with `human_in_the_loop: true` and a task description asking for exactly the missing step:

```json
{
  "agent_instance_id": "planner-tdd-soft#8",
  "run_id": "20260129T090000Z-a3f9",
  "task_description": "Present the artifacts listed in output_artifacts to the user for review. Apply any changes they request. Set human_approved: true only once the user asks for no further changes.",
  "input_artifacts": ["Orchestration-20260129T090000Z-a3f9/Plan.md"],
  "output_artifacts": ["Orchestration-20260129T090000Z-a3f9/Plan.md"],
  "human_in_the_loop": true
}
```

If the re-dispatch also returns `false`, escalate to the user. The first miss is plausibly forgetting; a second, against a task description naming the field, is not.

**What this check cannot tell you.** A `true` is self-reported and can be written without presenting anything. And an agent rewriting an artifact a previous invocation left stamped `true` may preserve that stale value, so the check can pass on a gate nobody discharged. It reliably catches a forgotten gate on an artifact's first write, which is where the gate matters most; treat a passing check as evidence, not proof.
[[/SECTION:CommunicationProtocol:Orchestrator]]

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
- File-level metadata written *into* artifacts. The provenance stamp a subagent applies to the artifacts it produces is a separate canonical section with its own contract; this document governs the JSON envelope only.

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
| `input_artifacts` | array | Yes | Orchestration artifacts the subagent may read. **Exhaustive** — nothing outside this list may be touched. `[]` when there are none. |
| `output_artifacts` | array | Yes | Orchestration artifacts the subagent is to create or update. **Exhaustive**. `[]` when there are none. |
| `input_files` | array | No | Project files worth starting from. Advisory; the subagent may read any project file it judges relevant. |
| `output_files` | array | No | Project files expected to change. Advisory; the subagent may write any project file it needs to. |
| `constraints` | string | No | Boundaries on how the work is done. Omit when there are none. |
| `include_result_summary` | boolean | No | When `true`, the response must carry `result_data`. Defaults to `false`. |
| `human_in_the_loop` | boolean | No | When `true`, activates the output review gate (§3.6). Defaults to `false`. |

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

Obligation is asymmetric by design, and the deployed text says so explicitly: producers always emit it, while consumer strictness is tiered. Core components — the orchestrator and the deterministic runner — may refuse a message or halt a run over a missing or mismatched `run_id`, because for them it is load-bearing. Auxiliary consumers — loggers, analyzers, anything in the optional tooling tier — must degrade quietly instead. An observer is not permitted to break a run it is only watching.

### 3.4 Orchestration Artifacts vs Project Files

This is the single most consequential distinction in the protocol, and the one most worth stating twice.

**Orchestration artifacts** are the files named in `input_artifacts` / `output_artifacts`.

- They exist purely to carry state between agents — findings, plans, designs, review results.
- They live in the run-scoped folder.
- Access is **strict and exhaustive**: an agent reads and writes exactly what its lists name, and nothing else. Artifacts belonging to other steps are other agents' workflow state.

**Project files** are everything else.

- Source, configuration, documentation, reports — the actual deliverables.
- Access is **fully autonomous**: any file not named as an orchestration artifact is fair game to read, modify, or create.
- `input_files` / `output_files` are hints. They point at a good starting place and an expected result; they do not fence the agent in.

**The test is positional, not semantic:** a file is an orchestration artifact because it appears in an artifact list, not because of where it lives or what it is called.

| Item | Classification | Access |
|------|---------------|--------|
| `Orchestration-{run_id}/Design.md`, listed in `output_artifacts` | Orchestration artifact | Writable — solely because it is listed |
| `Orchestration-{run_id}/Research.md`, not in either list | Orchestration artifact | **Off limits** |
| `src/UserService.ts` | Project file | Free access |
| `package.json` | Project file | Free access |
| `test-results/report.xml` | Project file | Free access — living under a results folder does not make it an artifact |

Why enforce it this way: the strict half keeps agents from reading each other's context and quietly widening their own scope, which is what preserves single-responsibility across a long run. The permissive half keeps agents from being paralysed by an incomplete file list, which is what makes them useful on real codebases. Blurring either half degrades the system in a predictable direction — the strict half toward context leakage, the permissive half toward agents refusing work they are perfectly able to do.

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

`human_in_the_loop: true` installs an **output review gate**. It is not a general instruction to be chatty.

The rules:

1. The gate fires **last**, after the work is finished — the agent presents everything it produced, artifacts and project files alike, immediately before returning its response.
2. If the user asks for changes, the agent makes them and presents again. **The gate re-arms on every change**, so the loop ends only when the user is satisfied with what they were last shown.
3. **Mid-task interaction does not discharge the gate.** Asking a clarifying question halfway through is normal agent behaviour and satisfies nothing — HITL is specifically about reviewing finished output.
4. If the agent has no way to reach a human at all, it returns `BLOCKED` with `E503` rather than silently proceeding unreviewed.

Rule 3 is the one that needs stating explicitly, because without it "I consulted the user" becomes a claim an agent can satisfy by having asked anything at all, and the gate stops meaning what it was introduced to mean.

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
| `agent_instance_id` | string | Yes | Echoed unchanged from the invocation. |
| `run_id` | string | Yes | Echoed unchanged from the invocation. |
| `status_code` | string | Yes | One of the six codes in §5. |
| `status_message` | string | Yes | One or two sentences describing the outcome, naming what was created or changed. |
| `result_data` | string | Conditional | Key findings, 200 words or fewer. Present **only** when the invocation set `include_result_summary: true`. |
| `error_code` | string | BLOCKED only | Machine-branchable blocker category, format `E{category}{number}`. Present **only** with `BLOCKED`. |
| `error_reason` | string | BLOCKED only | Plain-language explanation of the blocker. Present **only** with `BLOCKED`. |

**There is no field listing what the agent modified.** The orchestrator establishes that itself — timestamps, hashes, `git status` — because a self-reported change list is both redundant and, when an agent forgets to update it, actively misleading. The agent describes its work in prose in `status_message` and leaves detection to the mechanism that cannot be wrong about it.

### 4.2 Error Codes

Error codes accompany `BLOCKED` and nothing else. Their purpose is to let a hook or a runner branch on the *kind* of blocker without reading English.

| Prefix | Category | Meaning |
|---|---|---|
| `E1xx` | Input | Something the agent was told to read is not there |
| `E4xx` | Dependency | Prerequisite work in the workflow has not happened |
| `E5xx` | External | The environment — tools, permissions, people — is not cooperating |

| Code | Name | Condition |
|------|------|-----------|
| `E101` | INPUT_NOT_FOUND | A required input artifact does not exist |
| `E401` | DEPENDENCY_MISSING | A predecessor task has not completed |
| `E501` | TOOL_UNAVAILABLE | An external tool or service is down, timing out, or absent |
| `E502` | PERMISSION_DENIED | A required file or resource cannot be read or written |
| `E503` | USER_CONTACT_UNAVAILABLE | `human_in_the_loop: true` but there is no channel to a human |

The three-category split is what the orchestrator's tiered response keys off: `E5xx` is often worth an automatic retry, `E1xx` and `E4xx` almost never are, since retrying will not conjure a missing file.

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
  "status_message": "Cannot proceed. Required input artifact Design.md does not exist. Design phase may not have completed.",
  "error_code": "E101",
  "error_reason": "INPUT_NOT_FOUND: Orchestration-20260129T090000Z-a3f9/Design.md not found"
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

Six codes. Each describes what happened in **this** invocation, and each maps to a distinct orchestrator response.

### 5.1 `SUCCESS`

**Means:** The task is done, completely and correctly. Everything requested was produced.

**Return it when:** all acceptance criteria are met, all declared output artifacts exist in the state they should, and nothing went wrong.

**Orchestrator does:** advances to the next step in the workflow, without asking a human.

**Looks like:** research written up; implementation compiling with tests green; validation confirming a clean run.

### 5.2 `COMPLETED_NEEDS_ACTION`

**Means:** The task succeeded, and its result is work for somebody else.

**Return it when:** a review turned up issues, a test run turned up failures, an analysis turned up problems — the agent did exactly its job, and the job's output is a list of things to fix.

**Orchestrator does:** routes to whichever agent can act on the findings, typically the one that produced the material under review.

**Looks like:** a code review with five issues; a test run with three failures; a requirements analysis surfacing two contradictory requirements.

**Not a failure.** This is the expected, successful outcome for any agent whose purpose is to find things. A reviewer that returns `SUCCESS` after finding five defects has misreported.

### 5.3 `PARTIALLY_DONE`

**Means:** Some of the requested items are finished to a high standard; the rest were not attempted.

**Return it when:** a multi-item task was stopped deliberately — context is running short, or complexity is climbing and quality would suffer past this point. What is finished is finished properly. Nothing is blocked, nothing is uncertain, there is simply more of the same left.

**Orchestrator does:** dispatches a fresh instance of the same agent type to carry on, with the remaining items.

**Looks like:** two of five services implemented; three of seven documents analyzed; tests written for four of eight functions.

This is the code that makes "quality over completeness" expressible. Stopping while the work is still good is judgment, and the protocol treats it as such rather than forcing the agent to choose between overreaching and reporting failure.

### 5.4 `NEEDS_CLARIFICATION`

**Means:** The agent cannot proceed confidently without more information.

**Return it when:** the task or the requirements are ambiguous; several valid approaches exist and choosing between them is not the agent's call; the context handed over is incomplete (an unspecified interface, a missing contract); or an earlier phase left something contradictory.

**Orchestrator does:** supplies the missing context and re-invokes, or escalates to a human when the answer is a genuine decision rather than a lookup.

**Looks like:** "performance or maintainability — which wins here?"; "does 'update user' include password changes?"; "three architectures fit; which constraints should decide?"

### 5.5 `CAPABILITY_EXCEEDED`

**Means:** The agent had everything it needed and still could not do it.

**Return it when:** the attempt was made, approaches were exhausted, and the task is simply beyond what this agent can do.

**Orchestrator does:** tries a different approach or a different agent, or escalates to a human.

**Looks like:** "three attempts at the algorithm, none working"; "this needs domain expertise I don't have."

**Distinct from `BLOCKED`:** here nothing external is in the way. The inputs were all present; the agent was the limiting factor.

### 5.6 `BLOCKED`

**Means:** Something outside the agent prevents the work from starting or continuing.

**Return it when:** a required artifact is missing; a prerequisite has not run; an external service is unavailable; permissions are refused; HITL is demanded and no human is reachable.

**Orchestrator does:** consults the error code and responds by tier — retry the transient, escalate the structural.

**Carries error codes.** `BLOCKED` is the only status that does, precisely because it is the only one where the *category* of the problem determines the response.

**Looks like:** implementation dispatched before Design.md exists (`E101`); an unfinished predecessor (`E401`); a search API that is down (`E501`); a write refused (`E502`); HITL requested with no user channel (`E503`).

### 5.7 Decision Matrix

| Situation | Code | Orchestrator response |
|-----------|------|----------------------|
| Everything asked for is done | `SUCCESS` | Advance |
| Done, and the output is work for another agent | `COMPLETED_NEEDS_ACTION` | Route to the fix target |
| Some items done, stopped deliberately for quality | `PARTIALLY_DONE` | Route to a successor of the same type |
| Uncertain, needs information or a decision | `NEEDS_CLARIFICATION` | Supply context or escalate |
| Tried, cannot do it | `CAPABILITY_EXCEEDED` | Alternative agent or escalate |
| A prerequisite is missing | `BLOCKED` | Re-check dependencies |
| The environment is uncooperative | `BLOCKED` | Retry or escalate by tier |

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
  "constraints": "Must achieve 80% code coverage",
  "human_in_the_loop": false
}
```

**Response**
```json
{
  "agent_instance_id": "implementation-tdd#12",
  "run_id": "20260129T090000Z-a3f9",
  "status_code": "COMPLETED_NEEDS_ACTION",
  "status_message": "Implementation complete but 3 of 12 unit tests failing: testUpdateUser, testDeleteUser, testGetUser. Coverage 73%. Modified UserService.ts, UserService.test.ts, and types.ts."
}
```

**Orchestrator:** back to implementation for the failures, or to a review agent to diagnose them — the code says action is needed, not who must take it.

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

**Orchestrator:** dispatches `implementation-tdd#16` with the remaining three. The successor picks up continuation context from the artifact, not from the response.

### 6.5 Blocked on a missing prerequisite

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
  "status_message": "Cannot proceed. Required Design.md artifact and interface file do not exist.",
  "error_code": "E101",
  "error_reason": "INPUT_NOT_FOUND: Orchestration-20260129T090000Z-a3f9/Design.md not found, src/interfaces/IPaymentGateway.ts not found"
}
```

**Orchestrator:** checks whether the design phase actually ran; if not, dispatches the designer first.

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

**Orchestrator:** either re-dispatches without the HITL flag, or escalates so that a user channel is provided. Note what the agent did *not* do: proceed unreviewed and claim success.

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
| 7 | `input_files`, if present, is an array | No |
| 8 | `output_files`, if present, is an array | No |
| 9 | `constraints`, if present, is a string | No |
| 10 | `include_result_summary`, if present, is a boolean | No |
| 11 | `human_in_the_loop`, if present, is a boolean | No |

### 7.2 Response

| # | Check | Required |
|---|-------|----------|
| 1 | Valid JSON | Yes |
| 2 | `agent_instance_id` identical to the invocation's | Yes |
| 3 | `run_id` identical to the invocation's | Yes |
| 4 | `status_code` is one of the six | Yes |
| 5 | `status_message` present, one to two sentences, names what changed | Yes |
| 6 | `result_data` present exactly when `include_result_summary: true` was sent; 200 words or fewer | Conditional |
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
| Finished cleanly | `SUCCESS` | "This chunk is done, move on" |
| Finished, produced action items | `COMPLETED_NEEDS_ACTION` | "Someone needs to act on this" |
| Finished part of it | `PARTIALLY_DONE` | "More of the same work is needed" |
| Stalled, needs information | `NEEDS_CLARIFICATION` | "Give me an answer and I'll continue" |
| Stalled, past its limits | `CAPABILITY_EXCEEDED` | "Find another way" |
| Stalled, environment | `BLOCKED` | "Fix the world first" |

### 8.4 Why six is exhaustive

Enumerate the outcome space and it closes:

| # | Category | Code |
|---|----------|------|
| 1 | Complete, nothing outstanding | `SUCCESS` |
| 2 | Complete, with action items for others | `COMPLETED_NEEDS_ACTION` |
| 3 | Incomplete by choice, for quality | `PARTIALLY_DONE` |
| 4 | Incomplete, information blocker | `NEEDS_CLARIFICATION` |
| 5 | Incomplete, capability blocker | `CAPABILITY_EXCEEDED` |
| 6 | Incomplete, environment blocker | `BLOCKED` |

The reasoning: the work is either complete or it is not. If complete, either it left action items or it did not. If incomplete, either the agent chose to stop or something stopped it — and the something is the agent's own limits, missing information, or the environment. There is no seventh branch.

### 8.5 The two distinctions people get wrong

**`COMPLETED_NEEDS_ACTION` vs `PARTIALLY_DONE`** — the question is whether the remaining work is *the same work*.

| | `COMPLETED_NEEDS_ACTION` | `PARTIALLY_DONE` |
|---|---|---|
| The agent's own assignment | Finished | Finished as far as it went |
| Requested items | All addressed | Some addressed |
| What is left | Different work — fixes, remediation | The same work — continuation |
| Example | A review found five defects | Two of five features implemented |
| Signal | "Different agent type needed" | "Same agent type needed" |

**`CAPABILITY_EXCEEDED` vs `BLOCKED`** — the question is whether swapping the agent could help.

| | `CAPABILITY_EXCEEDED` | `BLOCKED` |
|---|---|---|
| Cause | The agent's own limits | Something external |
| Would a different agent fare better? | Possibly | No — the same wall is there |
| Example | "Can't work out the algorithm" | "Design.md doesn't exist" |
| Fix | Alternative agent, or a human | Repair the environment |

### 8.6 Resisting a seventh code

Any proposed addition must clear all three bars:

1. It names an outcome none of the six covers.
2. It requires an orchestrator response none of the six triggers.
3. It cannot be conveyed by an existing code plus detail in `status_message`.

Proposals that have failed the test:

| Proposed | Already is | Why |
|----------|-----------|-----|
| `DELEGATED` | `COMPLETED_NEEDS_ACTION` | An action item that another agent handles |
| `NEEDS_REVIEW` | `COMPLETED_NEEDS_ACTION` | An action item that a reviewer handles |
| `TIMEOUT` | `BLOCKED` | The environment prevented the work |
| `RETRY_SUGGESTED` | Not a status at all | A recommendation about routing, which is the orchestrator's decision |
| `LOW_CONFIDENCE` | `NEEDS_CLARIFICATION` | Uncertainty needing input; also see §4.4 |
| `PARTIAL_SUCCESS` | `PARTIALLY_DONE` | The same thing, renamed |

A code that maps onto an existing outcome adds vocabulary without adding meaning, and every reading site pays for it.

### 8.7 Codes report, they do not command

A status describes an outcome. It does not dictate a response. The typical mapping is a default, not a rule:

| Status | Usual | Also reasonable |
|--------|-------|-----------------|
| `SUCCESS` | Advance | Insert a review step first |
| `COMPLETED_NEEDS_ACTION` | Route to a handler | Batch findings from several sources |
| `PARTIALLY_DONE` | Same-type successor | Re-scope the remaining work |
| `NEEDS_CLARIFICATION` | Supply context | Escalate when the answer is a real decision |
| `CAPABILITY_EXCEEDED` | Escalate | Retry with a stronger model or a different agent |
| `BLOCKED` | Clear the blocker | Skip the step, or escalate |

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
| `run_id` | string | Conditional | The invocation's `run_id`, copied verbatim | Identity of the orchestration run that produced this artifact. Omitted when the invocation carried no `run_id` (§9.5). |
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

The field name is admittedly a poor fit for last-writer semantics. It is retained because it is what forty-two agents already say, and renaming it is a change with a migration cost and no functional gain (§14).

### 9.4 Merging into existing frontmatter

Where the target file already opens with a `---` delimited YAML block, the fields are merged into that block. A second frontmatter block is never created.

The failure this rule prevents is specific and easy to fall into: an agent writing its stamp by prepending `---\nrun_id: ...\n---\n` to a file that already begins with `---` produces a document whose first block contains the stamp and whose apparent body starts with a second `---`. Most frontmatter parsers read the first block and treat everything after as content, so the original frontmatter silently becomes part of the body. The artifact still looks fine to a human and has lost every field it declared.

### 9.5 Absent run identity

When the invocation carries no `run_id`, the field is omitted and `created_by` is stamped alone.

The alternatives are both worse. Minting a value locally produces an identifier that matches no run and correlates with nothing, while being indistinguishable from a real one — an invented `run_id` is not a degraded answer but a wrong one. Refusing to write the artifact escalates a missing correlation key into a halted run, which inverts the severity: provenance is an aid to a later reader, not a precondition for the work.

An absent `run_id` is therefore not an error at any layer. Consumers must treat the field as optional and degrade accordingly — the same tiering this protocol applies to `run_id` in flight, applied here to `run_id` at rest.

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

**When.** Immediately after an invocation dispatched with `human_in_the_loop: true` returns, and before routing on its status code.

**What is read.** The frontmatter of each artifact named in that invocation's `output_artifacts`, and nothing below it.

Reading only the frontmatter is a deliberate narrowing, not a new permission. The orchestrator already reads certain artifacts for routing, so artifact access is established. What is at stake is context discipline: an orchestrator that reads plans and designs in full begins forming opinions about their content, and a workflow-agnostic router with opinions about domain content is on its way to making domain decisions. A frontmatter read costs a handful of lines and cannot produce an opinion about anything.

**The check.** Dispatched `human_in_the_loop: true` and any output artifact carrying `human_approved: false` — or omitting the field — is a gate that was not discharged.

**The response: re-dispatch the same agent type to discharge the gate.** Not a failure route. The invocation's work is finished and, so far as anything indicates, finished correctly. What is missing is a review, and a review is cheap to obtain against artifacts that already exist. The follow-up invocation carries the same artifacts as both inputs and outputs, `human_in_the_loop: true`, and a task description asking for exactly the missing step. The orchestrator variant of §1 carries the worked example.

The alternatives were weighed and rejected. Routing it as a failed invocation would discard work completed correctly apart from the gate. Escalating to the user spends a human interruption on something the re-dispatch resolves by itself — and spends it to ask permission for a review the user was already meant to be given. Recording the discrepancy and advancing turns the field into an audit trail with no enforcement, surfacing skipped gates after the run rather than during it.

**A re-dispatch that comes back still `false` is a different problem.** The first miss is plausibly forgetting; the second, against a task description naming the flag explicitly, is not. That is the point to escalate, and the point at which the run has learned something about the agent rather than about the invocation.

**Where this obligation is written.** In the orchestrator variant of §1, alongside the routing table and the error-code responses.

This was previously delegated to the orchestrator's own instructions, on the reasoning that a re-dispatch is a routing decision and routing is orchestration policy. That reasoning does not survive §10.6: a field the orchestrator reads and routes on is hard interop, and both halves of a handshake belong in one deployed contract or they drift apart per deployment (§10.1). The delegation also produced a concrete defect — the subagent variant told subagents the orchestrator performs this check while nothing on the orchestrator side was instructed to perform it. The block already carries routing content, including a HITL-triggered re-dispatch under `E503`, so this is not a new kind of text for it.

The orchestrator still stamps nothing and carries no provenance obligations of its own (§9.9). Reading a field is not writing one.

### 9.8 Scope: which files are stamped

Files named in `output_artifacts` are stamped. Files named in `output_files` are not. Nothing else is touched.

**The test is positional, not semantic** — identical to the rule governing artifact access. A file is stamped because it appears in the `output_artifacts` list, never because of its location or its name.

| Item | In which list | Stamped |
|------|---------------|---------|
| `Orchestration-{run_id}/Design.md`, listed in `output_artifacts` | `output_artifacts` | Yes |
| `Orchestration-{run_id}/Stage-1/PlanProgress.md`, listed in `output_artifacts` | `output_artifacts` | Yes |
| `src/services/UserService.ts` | `output_files` | No |
| `docs/architecture.md`, written at the agent's own discretion | Neither | No |
| A markdown file the agent created under the run folder but never declared | Neither | No — the folder does not confer artifact status |

**Why project files are excluded.** They belong to the project, not to the run. A stamp in a source file is a foreign annotation with a lifetime far shorter than the file's: it goes stale the moment a human edits the file, survives into commits and releases, and names a run that will mean nothing to whoever reads it next. Worse, for many file types the stamp is not merely unwanted but invalid — there is no correct place to put YAML frontmatter in a `.ts` file, a `.json` config, or a `.go` source file, and an agent attempting it produces a syntax error.

The last row deserves emphasis. An agent working under a workflow that grants broad file autonomy may legitimately create files inside the run-scoped folder that were never declared as artifacts. Those are project files by the positional test and are not stamped. Applying the stamp by folder rather than by list would make the rule depend on where an agent happened to write, which is precisely the location-derived provenance §9.1 exists to avoid depending on.

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

Protocol text is identical across every deployed agent, and that is precisely why it must not be maintained in every agent file. Wording kept in forty places drifts in forty directions: one agent gets a fix, thirty-nine keep the defect, and the divergence is invisible until an agent misreports.

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
3. Locate the top-level `[[DEPLOYED:CommunicationProtocol]]` region in the agent file and replace its **entire** body with a version marker comment — `<!-- protocol-version: 1.9 -->` — followed by the selected block's content.

The region carries the `[[DEPLOYED:]]` marker rather than `[[SECTION:]]`, and the distinction is load-bearing: the marker in the file states who owns the region. `[[SECTION:]]` content is authored in the MOSAIC source and carried through byte-identically; `[[DEPLOYED:]]` content is written and regenerated by the tool on every deploy. A reader of an agent source file therefore knows, from the tag alone and without consulting code or documentation, that the region is not theirs to edit. In a source file the region is empty; the deployed file is the only place it has content.

The region sits at body top level rather than nested inside a section, occupying slot 2 of the canonical document order.

**There is no injection region accompanying the deployed block.** The protocol shape is not a project variable. A project that could append its own text to the protocol could contradict it, and a contradiction inside the section is strictly worse than one outside it: the agent has no way to tell which half is canonical, and the whole point of §2.4 is that precedence questions get settled once, centrally, rather than per deployment.

**A project that genuinely needs to extend the protocol mechanics** — stating how messages are delivered across network boundaries, for example — uses `[[CUSTOM:ProtocolExtension]]` as a top-level sibling of this region, never nested inside it. That region is project-invented (`[[CUSTOM:]]`) and carries no advisory parent in the MOSAIC catalogue. The guidance is unchanged: extend the mechanics (transport, delivery, environment-specific handling); do not restate or contradict what the contract fixes (message shape, status and error vocabularies, the HITL gate). `ProtocolExtension` is not a catalogued `[[INJECTION:]]` name — MOSAIC defines `[[INJECTION:]]` slots in source; this one is a project invention and belongs in `[[CUSTOM:]]` (see `AgentTemplateArchitecture.md` §6.2.1).

The absence pays a second dividend: the deployed region is exactly the source block plus one marker comment, which makes a visual diff between this file and any deployed agent meaningful without knowing what to discount.

**The division of labour with harness content.** Harness-layer injections may name a specific mechanism that competes with this protocol on a given harness — a subagent-invocation tool's metadata fields, an injected reporting convention — because those names are meaningful only on that harness. What they must not do is restate the precedence rule itself: that is canonical (§2.4), reaches every agent already, and a per-harness paraphrase of it can only drift from the original. The test for whether content belongs in a harness injection is whether it names something that exists on one harness and not another. "MOSAIC protocol takes precedence" fails that test. "The field is called `description`" passes it.

### 10.4 Version tracking

The frontmatter `version` field is the protocol version. It appears in two further places, and all three are expressions of one fact that must change together:

| Where | Form | Read by |
|---|---|---|
| This file's frontmatter | `version: "1.9"` | The deployment tool, as the source of truth |
| The deployed region's first line | `<!-- protocol-version: 1.9 -->` | Staleness checks |
| The block's opening sentence | "You operate under **Communication Protocol v1.9**" | The agent |

The marker comment is what makes staleness checkable: a deployed agent whose marker names an older version than this file's frontmatter is out of date, and the check is a string comparison against a fixed-position line — no parsing of prose, and no dependence on the wording of the sentence that follows.

The sentence exists for a different reader. It tells the agent what contract it is operating under, in the body text it actually attends to; a marker comment says nothing to a language model that a comment ever says. Two audiences, two expressions, one version string.

A protocol change is therefore always the same three steps: edit the block or blocks in §1, bump `version` in the frontmatter and in the block's opening sentence, add a changelog row. Then redeploy.

### 10.5 The frontmatter is deliberately small

The frontmatter carries four things a script genuinely needs — `id` and `type` to classify the file, `version` for the staleness check in §10.4, and `sections` for the variant mapping in §10.2 — plus `name`, `description`, `author` and `status` for display and convention. Nothing else.

In particular it **does not restate the status code or error code vocabularies**, and should not be extended to. Both lists already exist as tables inside the canonical blocks, and those blocks are the text that actually reaches an agent. A second copy in frontmatter would be the less authoritative one, which makes it worse than merely redundant: a conformance check reading it would pass while the deployed table said something different, and would report confidence in exactly the case it was built to catch. A tool needing the vocabulary parses it from the block.

The same reasoning rules out a `message_format` field or anything else describing properties of the protocol that no consumer branches on. If a future component genuinely needs a fact about this protocol in machine-readable form, the test is whether it can already get that fact from the canonical block; only when it cannot does the fact earn a frontmatter field.

### 10.6 Artifact Provenance Is Part of This Contract

The artifact provenance stamp was formerly a second contract with its own document, its own version, and its own `[[DEPLOYED:ArtifactProvenance]]` region sitting immediately after this one. It is now part of this contract, deployed inside this region, and versioned once (§12, v1.10). Its reasoning is §9.

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
| 1.10 | 2026-08-05 | **Artifact provenance merged in.** The provenance stamp — `run_id`, `created_by`, `human_approved`, written into every file named in `output_artifacts` — was a separate contract with its own document, version, and `[[DEPLOYED:ArtifactProvenance]]` region. It is now part of this contract: the text ships inside the subagent variant of §1.1, the reasoning is §9, and there is one version number where there were two. The merge is correct because the orchestrator **verifies** `human_approved` (§9.7) — a field it reads and routes on is hard interop, not an audit convenience, and it is the secondary layer under a JSON response that can otherwise claim anything. Two changes follow from the merge. The field formerly called `hitl_confirmed` is renamed **`human_approved`**: it is read from the artifact, where "HITL" is orchestration jargon a standalone reader does not hold, and "approved" is what the flip actually certifies — the user asked for no further changes. The rename is taken now because nothing yet reads the field, making this the cheapest it will ever be. And the **orchestrator variant gains a Verifying the Human-in-the-Loop Gate subsection** (§9.7), so the check the subagent variant promises is instructed on the side that must perform it; the orchestrator still stamps nothing (§9.9). Consequences: canonical document order drops from eight top-level slots to seven, and `[[DEPLOYED:ArtifactProvenance]]` and `[[INJECTION:ArtifactProvenanceExtension]]` cease to exist. |
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
| **Orchestration artifact** | A file named in `input_artifacts`/`output_artifacts`. Exists solely to carry state between agents. Strict access — only what the lists name. |
| **Project file** | Any file not named as an orchestration artifact. Source, config, docs, results. Full agent autonomy. |
| **Agent instance id** | `{AgentName}#{GlobalSequence}` — the identity of one invocation. |
| **Global sequence** | The run-wide invocation counter; supplies the numeric half of every agent instance id. |
| **Run id** | `{YYYYMMDD}T{HHMMSS}Z-{4-char-hex}` — identity of one orchestration run. Minted once, carried in both message directions. |
| **Phase** | A named stage of a workflow, tracked by name rather than by number. |
| **Human-in-the-Loop (HITL)** | The output review gate. When active, the agent must present everything it produced for approval as its final action, re-arming on every change. Mid-task interaction does not satisfy it. |
| **Provenance stamp** | The three frontmatter fields — `run_id`, `created_by`, `human_approved` — written into every file named in `output_artifacts` (§9). |
| **Producer obligation** | The requirement that a sender emit a given field. |
| **Consumer enforcement** | What a receiver is permitted or required to do when a field is missing. Tiered: core components may halt, auxiliary consumers must degrade. |

---

## 14. Open Ideas / Dead Ends

**Under consideration**

- **Enumerating the `run_id` echo's consumers.** Under single-run orchestration the response-side echo has no reader. It was added pre-emptively to avoid a second version bump later. If nothing consumes it by the time multi-run coordination is designed, the question of whether it earns its place should be reopened rather than assumed settled.
- **Whether the receiver rule needs a defined outcome, not just a prohibition.** v1.9 forbids inferring a status from a prose-only response but leaves recovery to orchestration policy. If the deterministic runner and an LLM orchestrator turn out to diverge in practice despite both obeying the prohibition, the resolution is to specify the recovery too — at which point it stops being policy and becomes protocol.
- **A machine-checkable conformance test for deployed protocol sections.** Version-string comparison (§9.4) catches staleness but not local edits that preserve the version. A hash of the canonical block, recorded at deployment, would catch both. The bundle-sourced regions carry the identical gap for the identical reason, so this is worth solving once for every `[[DEPLOYED:]]` region rather than per source.

**Rejected**

- **Confidence scores.** Uncalibrated self-assessment invites routing decisions on a number that does not mean what it looks like. `NEEDS_CLARIFICATION` plus a specific question is strictly more actionable (§4.4).
- **A self-reported list of modified artifacts.** Redundant against timestamp, hash, and `git` inspection, and wrong exactly when it matters — an agent that forgot to update the list reports a clean run it did not have (§4.1).
- **A seventh status code.** Six proposals have been tested against §8.6 and all six mapped onto existing outcomes.
- **Leaving protocol precedence to per-harness injections.** One harness already carried a version of this rule as a local constraint, which is how the gap was found: it covered the dispatch side only, said nothing to subagents, and asserted that subagents would ignore competing instructions anyway — an assumption that holds only if something tells them to, and nothing did. A universal invariant maintained in four places is maintained in none of them (§2.4).
- **Forbidding orchestrators from filling harness metadata fields at all.** Tempting, but some harnesses require those fields, so a blanket prohibition would be unfollowable. The rule instead defines their *status* — bookkeeping, carrying no task content — which is enforceable everywhere and yields the same outcome where it matters.
- **A per-project protocol extension.** An earlier design had the tool append a `ProtocolExtension` injection after the deployed block, so a project could add its own protocol notes. Rejected because a project able to append to the protocol is a project able to contradict it, and a contradiction *inside* the section is worse than one outside: nothing tells the agent which half is canonical. Precedence is settled centrally (§2.4) precisely so it is not renegotiated per deployment. Project-specific communication guidance belongs in the agent's instruction body, where it reads as guidance (§10.3).
- **A single merged protocol section for all agent roles.** Would put a routing table into every subagent and return-code rules into the orchestrator, in both cases text the reader cannot act on (§10.2).
- **Numeric status codes.** Compact on the wire, but every log line, table cell, and routing rule would then need a lookup to be readable. The wire is not the constrained resource here; attention is.
- **Declaring the status and error vocabularies in frontmatter.** Attractive because a runner could then check its own handling against a YAML list instead of parsing a markdown table. Rejected because the list would be a second copy of what the canonical block already states, and the block is what ships — so the check would validate against the copy and stay green while the deployed text diverged. Parsing the block is marginally more work and is the only version of the check that can actually fail when it should (§10.5).

---

## 15. Open Items

- **The agent instance id pattern in §7.3 is broader than earlier drafts.** Agent names are kebab-case (`checkpoint-manager-git#4`), which a letters-only pattern rejects. The pattern here admits hyphens and digits. Any validator written against the narrower form must be updated, or it will reject valid ids from most of the agent catalogue.
- **The v1.8 date in §12 should be confirmed** against when the `run_id` change actually landed in the agent files.
- **One harness injection now duplicates canonical content.** `Agents/Claude Code/HarnessInjectionsOrchestrator.md` restates protocol precedence inside a `HarnessConstraints` block. With §2.4 canonical, that injection should shrink to naming the `Task` tool's metadata fields and drop the precedence claim, per the test in §10.3.
- **The v1.10 merge is specified but not executed.** The stamp text now ships inside `[[DEPLOYED:CommunicationProtocol]]`, but nothing has been migrated: forty-two agent files still carry a separate `[[DEPLOYED:ArtifactProvenance]]` region and an `[[INJECTION:ArtifactProvenanceExtension]]`, and all three vocabulary copies — `Agents/Generic/SourceFilesFormat.md`, `Tools/Common/docformat/vocabulary.go`, `Tools/OldAgentsTransform/boundary_constants.py` — still list `ArtifactProvenance` as a canonical deployed name with eight-slot ordering. Those three must change together, along with the `Tools/Common/testdata/boundary/` fixtures that encode the old order.
- **§9.7's verification is specified but not implemented.** The orchestrator variant of §1 now carries the check, closing the gap where the subagent variant promised a verification nothing was instructed to perform. What remains is deployment: no orchestrator in the workspace has the new block, so no orchestrator yet reads the field. The verification is the reason the merge is correct at all, so this is the item that makes v1.10 more than a filing change.
- **The `hitl_confirmed` → `human_approved` rename is swept everywhere it can reach an agent.** Done: `Agents/Generic/Agents/Interface/approval-presenter.md` (v1.0.1), its row in `Agents/Generic/Agents/README.md`, `Development/Designs/DeploymentBlocks/ClosingProcedure.md`, and `Workflows/Verification/requirements-to-test-cases.md` (v1.2). The 42 agent files and the orchestrator take the new text from `[[DEPLOYED:CommunicationProtocol]]` on redeploy and need no hand edit. Two deliberate exceptions remain: the fixtures under `Tools/Deployment/testdata/golden/`, which regenerate from a redeploy and must not be hand-edited, and the analysis note `OnSuccessHITL.md`, which records the decision in the vocabulary of its date. §9.6 and §12 name the old spelling on purpose, so the rename stays traceable.
