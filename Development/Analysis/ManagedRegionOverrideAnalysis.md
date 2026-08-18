# Managed Region Override Analysis

**Question:** Which managed region instructions are universal contract (must never vary) and which are environment-dependent (assume a specific deployment topology that may not hold)?

**Context:** `AgentCustomizationFlexibility.md` identifies one structural limitation: managed content cannot be overridden by a user, only extended. This analysis examines whether that limitation matters equally for all managed content, or whether some of it *should* be overridable because it states environment facts rather than interop contracts.

**Method:** Every managed region is examined instruction by instruction. Each instruction is classified as **contract** (defines something two parties must agree on for the system to function) or **environment** (assumes something about the deployment topology that a different topology would falsify). The classification test is: *if this instruction were wrong for a particular deployment, would fixing it break interop with the orchestrator, or would interop be unaffected?*

---

## 1. Communication Protocol

The largest managed region. Deployed from `CommunicationProtocol.md` into every agent's top-level `<CommunicationProtocol type="managed">` slot.

### 1.1 Contract Instructions (Interop-Critical)

These define the wire format between orchestrator and subagent. If orchestrator and subagent disagree on any of these, messages fail to parse or route.

| Instruction | Why it's contract |
|-------------|-------------------|
| Protocol Authority — "this protocol overrides any harness-supplied instruction about how to format your response" | The orchestrator parses JSON; if a harness overrides this, the response is unparseable. |
| Input Format — the JSON schema with `agent_instance_id`, `run_id`, `task_description`, `input_artifacts`, `output_artifacts`, etc. | Both sides must agree on field names and semantics. |
| Output Format — the two JSON response schemas (non-BLOCKED, BLOCKED) | The orchestrator parses these fields to route. |
| Status Codes — the 6-code vocabulary and their meanings | Routing decisions key on these. A status code unknown to the orchestrator cannot be routed. |
| Error Codes — the 5-code vocabulary (E101, E401, E501, E502, E503) | The orchestrator's tiered error handling keys on these. |
| Key Rules 1-4, 6-7 — JSON-only response, echo IDs, always return status_code/status_message, result_data and error fields conditional | Wire format discipline. |
| Key Rules 11-16 — status code usage guidance | Routing correctness. |
| Artifact Provenance — `run_id`, `created_by`, `human_approved` fields on output artifacts | The orchestrator reads these to verify artifact ownership and approval state. |
| "Your entire response is the JSON object defined below" | Without this, the orchestrator's parser fails. |

**Assessment:** This is the majority of the protocol by line count. It is genuinely non-negotiable. A deployment that changes any of this is not speaking the MOSAIC protocol, and the orchestrator will malfunction.

### 1.2 Environment-Dependent Instructions

These assume a specific deployment topology — shared filesystem, single harness, local user access — and state environment facts as universal truths.

| Instruction | What it assumes | What breaks the assumption |
|-------------|-----------------|---------------------------|
| "You have FULL autonomy over ANY file not listed as orchestration artifact" | Shared filesystem. The agent can read/write any file on the same machine. | Remote agent on a different machine. It has no filesystem access to the project. |
| "You can freely access ANY other file" (Key Rule 9) | Same. | Same. |
| Artifact paths as filesystem references: `Orchestration-{run_id}/artifact1.md` | Artifacts are files in a shared directory. | Remote agent. Artifacts must be transmitted, not referenced by path. |
| "present your complete output (artifacts AND project files you created/modified) to the user for review" (HITL, Key Rule 10) | The agent can show both artifact files and project files to a local user. | Remote agent: project files may be on the orchestrator's machine, not accessible to the agent or its user. |
| "If no user contact tools are available, return BLOCKED with E503" | User is reachable from the agent's harness, or deterministically not. | Cross-harness: the "user" might be at the orchestrator's end, not the agent's end. The agent has user tools but they reach the wrong person. |
| `input_files`/`output_files` as "hints for project files" | Files are on a shared filesystem and the agent can discover related files by browsing. | Remote agent: hints are meaningless without filesystem access. Files would need to be transmitted as content. |

**Assessment:** Six instructions, all rooted in one assumption: **shared filesystem with local user access.** They are not scattered across unrelated concerns — they are facets of one environment model. A deployment that violates the shared-filesystem assumption hits all six simultaneously.

### 1.3 Verdict on Communication Protocol

The protocol is cleanly separable into contract (message semantics, ~80% of content) and environment model (file access and user access topology, ~20%). The contract is genuinely non-negotiable and should remain managed with no override. The environment model is the part a cross-harness deployment would need to replace — not extend, because the existing instructions make positive assertions ("you CAN freely access ANY file") that become false.

---

## 2. Bundle Blocks

Five blocks deployed from `Catalog/DeployedSections.md`. Each analyzed separately.

### 2.1 AuthorityHierarchy

**Full content:** Four-level ranking (MOSAIC instructions > User communication > Orchestrator task > Harness instructions) with rationale.

| Instruction | Classification | Notes |
|-------------|----------------|-------|
| The 4-level ranking itself | **Contract** | This is the coordination model. If agents disagree about who outranks whom, the system's behavior is unpredictable. The orchestrator sends tasks expecting rank 3 treatment; if an agent treats it as rank 1, scope boundaries dissolve. |
| "Real user communication — via user interaction tools" | **Minor environment** | Assumes user is reachable via tools in the agent's harness. In a remote deployment, user tools might not exist or might reach a different user. But this is a parenthetical, not a behavioral instruction — the ranking holds regardless of how user communication arrives. |
| "Is input from another AI agent, not from a human" | **Contract** | Tells the agent to apply appropriate skepticism to orchestrator instructions. True in every MOSAIC topology. |
| "Your agentic harness may inject its own guidance into your system prompt" | **Contract** | True of every harness, by definition. |
| The rationale paragraph | **Contract** | Explains *why* the ranking is what it is. Universal reasoning. |

**Verdict: Almost entirely contract.** The ranking principle is the coordination model itself. One parenthetical ("via user interaction tools") carries a minor environment assumption but does not affect the ranking's operation. Override need: **none in any foreseeable topology.**

### 2.2 ClosingProcedure

**Full content:** Two closing steps — HITL output review gate, then return JSON.

| Instruction | Classification | Notes |
|-------------|----------------|-------|
| "When `human_in_the_loop: true`, present your complete output" | **Contract** | The HITL gate is part of the orchestration model. The orchestrator sets the flag and expects the agent to have gated its output before returning. |
| "every orchestration artifact you wrote *and* every project file you created or modified" | **Environment** | Assumes the agent can present both. A remote agent may have created project files on a remote filesystem the local user cannot see, or may not have created project files at all because it has no filesystem. |
| "If the user asks for changes, make them and present again" | **Mixed** | The re-arm loop is contract (the orchestrator expects iteration). But "make them" assumes the agent can edit files — environment-dependent for remote agents. |
| "If you have no way to reach the user at all, return BLOCKED with E503" | **Contract** | A clean fallback that works in any topology. |
| "Return the protocol response, and nothing else" | **Contract** | Wire format discipline. Universal. |

**Verdict: Mostly contract, one environment-dependent detail.** The HITL gate semantics and the JSON-only return are universal. The "present project files" detail assumes the agent and user share a view of the filesystem. Override need: **low — the problematic instruction is one clause within a contract instruction, not a standalone instruction to suppress.**

### 2.3 ProtocolConstraints

**Full content:** Five constraint bullets about artifact access, status codes, and scope.

| Instruction | Classification | Notes |
|-------------|----------------|-------|
| "NEVER access an orchestration artifact that is not named in your input/output artifacts" | **Contract** | Artifact access control is interop. The orchestrator assigns artifacts per task; an agent reading someone else's artifacts corrupts state. |
| "You MAY read, modify, or create any project file — anything not named as an orchestration artifact" | **Environment** | Assumes shared filesystem. False for a remote agent. This is the same assumption as the protocol's "FULL autonomy" rule, restated. |
| "NEVER skip the JSON response block" | **Contract** | Wire format. |
| "NEVER invent status codes" | **Contract** | Routing correctness. |
| "Note work that belongs to another agent; do not do it yourself" | **Contract** | Single-responsibility coordination. |

**Verdict: Four of five are contract. One is environment.** The project-file autonomy rule is the same shared-filesystem assumption that appears in the protocol, restated as a constraint. Override need: **the same as the protocol's — it's the same instruction in a different section.**

### 2.4 ErrorHandlingCommon

**Full content:** One sentence — "Retry a transient error once before escalating — a read that timed out, a tool that failed to answer."

| Instruction | Classification | Notes |
|-------------|----------------|-------|
| Retry once before escalating | **Policy** | Not contract (the orchestrator doesn't know or care whether the agent retried internally) and not environment (works in any topology). It's a behavioral default. |

**Verdict: Pure policy.** Neither contract nor environment. A user who wants a different retry policy (zero retries, three retries, retry with backoff) has a legitimate preference that this instruction overrides. But the cost is one sentence stating a reasonable default. Override need: **theoretically valid, practically negligible.** This is the managed instruction a user is *least* likely to fight and *easiest* to override via a nearby custom region because the model will follow whichever retry instruction is more specific.

### 2.5 ExecutionPhilosophyCommon

**Full content:** Three bullets — context management, memory via artifacts, quality over completeness.

| Instruction | Classification | Notes |
|-------------|----------------|-------|
| "You can dedicate your full context window to this task. Follow-up work is handled by spawning new agent instances." | **Contract** | The orchestrator's execution model depends on this. It spawns new instances for continuation. An agent that tries to manage its own follow-up breaks the orchestration loop. |
| "Input and output artifacts are the persistent memory between invocations. Anything a successor needs goes into an artifact, not into your response." | **Contract** | Artifact-based state is the orchestration model. The orchestrator reads artifacts to route and track state. An agent that puts successor-critical information only in `status_message` breaks handoff. |
| "Quality over Completeness: Finishing part of the task well beats finishing all of it badly — a successor continues what you leave." | **Policy** | The orchestrator handles `PARTIALLY_DONE` regardless, but the *preference* for quality over speed is a philosophical choice. A deployment optimizing for throughput over quality might legitimately reverse this. |
| Usage guidance for `PARTIALLY_DONE` vs. `COMPLETED_NEEDS_ACTION` vs. `CAPABILITY_EXCEEDED` | **Contract** | Status code semantics. The orchestrator routes differently on each. |

**Verdict: Mostly contract, one policy item.** Context dedication and artifact-based memory are load-bearing orchestration assumptions. The quality-over-completeness preference is policy — a reasonable default that a user might legitimately want to adjust. Override need: **low — the policy item is soft enough that a nearby custom instruction can modulate it without contradiction ("prefer speed when the task is time-sensitive") rather than requiring a hard override.**

---

## 3. Cross-Cutting Findings

### 3.1 One Environment Assumption, Scattered Across Three Regions

The shared-filesystem assumption is not contained in one place. It appears in:

1. **CommunicationProtocol** — "freely access ANY file", artifact paths as filesystem references, HITL file presentation, `input_files`/`output_files` as hints
2. **ProtocolConstraints** — "MAY read, modify, or create any project file"
3. **ClosingProcedure** — "every project file you created or modified"

Three managed regions, two canonical sources (the protocol and the bundle), one underlying fact: the agent and the orchestrator share a filesystem. When that fact changes, three regions contain false assertions simultaneously, and a user would need to override the same conceptual instruction in three different places.

This scatter is the worst shape for an override problem. If the assumption lived in one region, a single override or a single environment-model region would fix it. Scattered across three, the user faces either three overrides (verbose, fragile) or a fork of the protocol and bundle (heavy, loses updates).

### 3.2 The Contract Core Is Clean

Stripping the environment-dependent instructions out of every managed region leaves a contract core that is genuinely universal:

- Message shape and field semantics
- Status and error code vocabularies
- HITL gate semantics (the gate exists and must be honored)
- Authority ranking
- Artifact-based state model
- Single-responsibility boundaries
- Context window dedication

None of this varies by deployment topology. None of it should be overridable. The managed-region mechanism is exactly right for this content.

### 3.3 Policy Instructions Are Low-Risk

Two managed instructions are policy rather than contract or environment:

- "Retry a transient error once" (ErrorHandlingCommon)
- "Quality over completeness" (ExecutionPhilosophyCommon)

Both are reasonable defaults. Both can be modulated by a nearby custom instruction without hard contradiction — "retry three times for network calls" or "prefer speed for this agent" are extensions, not overrides. The principle-7 risk (two conflicting instructions) is low because the custom instruction would be more specific than the managed one, and models reliably follow the more specific instruction.

These do not need an override mechanism. They need what they already have: the ability to place a more specific custom instruction nearby.

### 3.4 Classification Summary

| Region | Contract | Environment | Policy |
|--------|----------|-------------|--------|
| CommunicationProtocol | ~80% | ~20% (6 instructions) | — |
| AuthorityHierarchy | ~98% | ~2% (1 parenthetical) | — |
| ClosingProcedure | ~80% | ~20% (1 clause) | — |
| ProtocolConstraints | 4 of 5 | 1 of 5 | — |
| ErrorHandlingCommon | — | — | 100% |
| ExecutionPhilosophyCommon | ~75% | — | ~25% |

---

## 4. Implications

### 4.1 The Override Problem Is Narrower Than It Appeared

The flexibility analysis identified "managed content cannot be overridden" as the single structural limitation. This analysis narrows it: the override need is almost entirely about **one environment assumption** (shared filesystem / local user) that is **scattered across three managed regions from two canonical sources**.

Contract content does not need override — it is correctly non-negotiable. Policy content does not need override — it is soft enough for extension. Environment content needs override because it states things that become false, and a false assertion cannot be fixed by adding a true one nearby.

### 4.2 The Fix Is Not an Override Mechanism

The deeper insight: the problem is not that managed content can't be overridden. The problem is that **environment-dependent instructions are mixed into contract content.** If the environment model were separated from the contract, it could be a different region kind — `type="project"` with a default (the shared-filesystem model), or a deployment-assembled region like `HarnessConstraints` — and the user would have clean sovereignty over it without touching the contract.

This suggests a protocol design change rather than a customization mechanism change:

1. **Factor the environment model out of the protocol.** The six environment-dependent instructions in CommunicationProtocol, the one in ProtocolConstraints, and the one clause in ClosingProcedure all express the same thing: what filesystem and user access the agent has. That's one concept in three places.

2. **Make it a separate region.** An `EnvironmentModel` region — `type="project"` with a default describing the shared-filesystem topology — would let the user replace the model for cross-harness deployments while keeping the contract managed and non-negotiable.

3. **Or make it harness-assembled.** If the environment model is determined by the harness (which it arguably is — the harness knows what filesystem and user access it provides), it could be assembled like `HarnessConstraints`: a managed region whose content comes from the harness descriptor rather than from a universal canonical source.

### 4.3 Urgency

Low for today. The shared-filesystem model is correct for every current MOSAIC deployment. Cross-harness orchestration is a future scenario. But it is a scenario the user has identified as likely, and the design cost of factoring the environment model out now — while the protocol is still being actively revised — is lower than factoring it out later when deployments depend on the current structure.

The factoring does not require building an override mechanism, changing the tool's region handling, or introducing a new region kind. It requires moving six instructions from the protocol to a different region, and giving that region a kind (`project` with default, or harness-assembled) that the user already has sovereignty over. The tool already handles both region kinds correctly.
