# Claude Code Harness Analysis Report — DRAFT

> **⚠ DRAFT — Contains known factual errors. Needs review against raw capture data before treating as reliable.**

| Field | Value |
|-------|-------|
| **Harness** | Claude Code |
| **Analyzed** | 2026-09-07 |
| **Source Files** | `ClaudeCode/PrimaryAgent/SystemPrompt-clean.md`, `ClaudeCode/Subagent/SystemPrompt-clean.md`, both `BuiltInTools.json`, both `ToolOutputSchemas.json` |
| **Purpose** | Comparative analysis of primary agent vs subagent system prompts; assess MOSAIC compatibility, strengths, weaknesses, and behavioral adjustment surface area |

---

## 1. Structural Overview

### Identity Lines

| Context | Identity Statement |
|---------|--------------------|
| **Primary** | "You are Claude Code, Anthropic's official CLI for Claude." |
| **Subagent** | "You are a Claude agent, built on Anthropic's Claude Agent SDK." |

The subagent identity is deliberately generic -- it does not identify as "Claude Code." This is relevant for MOSAIC: the subagent's self-concept is more malleable than the primary's.

### Model Variants

| Context | Model |
|---------|-------|
| **Primary** | `claude-opus-4-6` |
| **Subagent** | `claude-opus-4-6[1m]` (1M context variant) |

The subagent explicitly gets the 1M-token context variant. Both see a `[total_tokens]` system-reminder showing remaining budget.

---

## 2. Tool Availability Diff

| Tool | Primary | Subagent | Notes |
|------|:-------:|:--------:|-------|
| Read | Y | Y | Identical schema and description |
| Write | Y | Y | Identical |
| Edit | Y | Y | Identical |
| Bash | Y | Y | Identical (full git instructions embedded in both) |
| Glob | Y | Y | Identical |
| Grep | Y | Y | Identical |
| Agent | Y | Y | Identical in both tiers; nested spawning supported (see Section 2.1) |
| TaskStop | Y | Y | Identical |
| AskUserQuestion | **Y** | **N** | Only primary can ask user structured questions |
| WebSearch | configurable | configurable | Present in this session but absent from the capture session; likely subscription/config dependent |
| WebFetch | configurable | configurable | Same as WebSearch |

### Key Takeaway

The only built-in tool difference: subagents **cannot** use `AskUserQuestion`. The MCP-based `ask_user_questions` (from contact-user server) is available if configured, but is NOT a built-in. This naturally supports hub-and-spoke: subagents cannot directly query the user through the built-in tool. All other tools -- including Agent -- are identical in both tiers.

### 2.1 Nested Agent Spawning: Harness Limits

Claude Code explicitly supports multi-level agent spawning with configurable depth limits. The feature has evolved rapidly:

| Date | Version | Change |
|------|---------|--------|
| June 9, 2026 | - | Nested subagent support launched, depth capped at 5 |
| July 21, 2026 | v2.1.217 | Nesting **disabled entirely**; concurrent cap set to 20 |
| July 24, 2026 | v2.1.219 | Nesting re-enabled at **default depth 3** |
| Current (Sept 2026) | - | Default depth 3, configurable up to 5 |

**Current limits (all configurable via environment variables):**

| Parameter | Default | Env Variable | Notes |
|-----------|---------|-------------|-------|
| Nesting depth | 3 | `CLAUDE_CODE_MAX_SUBAGENT_SPAWN_DEPTH` | Max 5 |
| Concurrent running | 20 | `CLAUDE_CODE_MAX_CONCURRENT_SUBAGENTS` | Any positive integer |
| Total spawns per session | 200 | - | Hard cap |

**What depth means for MOSAIC's hub-and-spoke:**

At default depth 3, counting from the primary agent:
- **Depth 0:** Primary agent (MOSAIC orchestrator)
- **Depth 1:** MOSAIC subagents dispatched by orchestrator
- **Depth 2:** A subagent could spawn its own child (bypassing orchestrator)
- **Depth 3:** That child could spawn one more

The harness caps recursive depth but does NOT enforce single-level fan-out. There is a known issue ([anthropics/claude-code#68110](https://github.com/anthropics/claude-code/issues/68110)) documenting "exponential fan-out and massive token burn" when general-purpose subagents recursively spawn unbounded children -- confirming this is a real-world problem, not just theoretical.

**MOSAIC implication:** Hub-and-spoke is a MOSAIC policy enforced through instructions. The harness provides a safety net (depth cap + concurrent cap + session cap) but does not structurally prevent a depth-1 subagent from spawning its own children. MOSAIC subagent instructions should explicitly prohibit Agent tool usage, and the depth/concurrent caps provide a backstop if instructions are ignored.

**Sources:**
- [Claude Code Subagent Depth Limits & Budget Caps (2026)](https://www.digitalapplied.com/blog/claude-code-subagent-depth-limits-budget-caps-2026)
- [Inside Claude Code's Nested Subagents - Ready Solutions AI](https://readysolutions.ai/blog/2026-06-11-claude-code-nested-subagents/)
- [Claude Code Subagents Guide 2026 - vibecoding.app](https://vibecoding.app/blog/claude-code-subagents-guide)
- [Exponential fan-out issue #68110](https://github.com/anthropics/claude-code/issues/68110)

---

## 3. System-Reminder Injection Blocks

Both primary and subagent receive the same set of dynamic system-reminder blocks, with minor ordering differences:

| Block | Primary | Subagent | Content |
|-------|:-------:|:--------:|---------|
| Environment | Y | Y | Working directory, platform, shell, OS version, scratchpad path |
| Model ID | Y | Y | Model name and knowledge cutoff |
| Agent List | Y | Y | Dynamic list of registered `.claude/agents/*.md` |
| MCP Instructions | Y | Y | Server-specific usage guidance |
| Auto Mode | Y | Y | "Bias toward working without stopping" |
| Token Budget | Y | Y | Remaining tokens |
| User Memory (MEMORY.md) | Y | Y | Persisted cross-conversation notes |
| User Context (email, gitStatus) | Y | Y | Identity + repo snapshot |
| Date | Y | Y | Current date |
| Attribution | Y | Y | Co-authored-by template + session URL |

**Significant:** User memory (`MEMORY.md`) propagates to subagents. MOSAIC project-specific notes (e.g., "use `py` not `python`") automatically reach all tiers.

---

## 4. Subagent-Specific Behavioral Rules

The subagent prompt includes a `Notes:` block (lines 109-114 of clean prompt) that the primary does NOT have:

```
- Agent threads always have their cwd reset between bash calls, as a result please only use absolute file paths.
- In your final response, share file paths (always absolute, never relative) that are relevant to the task. Include code snippets only when the exact text is load-bearing.
- For clear communication with the user the assistant MUST avoid using emojis.
- Do not use a colon before tool calls.
- Do NOT Write report/summary/findings/analysis .md files. Return findings directly as your final assistant message.
```

The subagent also gets an explicit **Scratchpad Directory** section (14 lines) directing all temp files to a session-specific path.

---

## 5. MOSAIC Conflict Analysis

### CRITICAL: Artifact File Writing Prohibition

**Conflict severity: HIGH**

Two harness instructions directly conflict with MOSAIC's artifact-based workflow:

1. **Subagent Notes:** "Do NOT Write report/summary/findings/analysis .md files. Return findings directly as your final assistant message -- the parent agent reads your text output, not files you create."

2. **Write tool description (BOTH tiers):** "NEVER create documentation files (*.md) or README files unless explicitly requested by the User."

MOSAIC subagents are designed to write artifact files (`Research.md`, `Architecture.md`, `Plan.md`, `TestStrategy.md`, etc.) as their primary deliverables. The Blackboard pattern depends on persistent artifacts.

**Mitigation:** MOSAIC agent instructions must explicitly override this by telling the agent "Write your deliverable to [path]." The harness rule says "unless explicitly requested" -- MOSAIC instructions constitute that explicit request. But the subagent-specific "Do NOT Write report..." rule has no escape clause, making it a harder override. In practice, models will follow the most specific/recent instruction (MOSAIC agent prompt) over the general harness rule, but this is a friction point that could cause sporadic refusals or artifacts returned as text instead of files.

**Recommendation:** MOSAIC subagent instructions should include an explicit counter-directive early in the prompt, e.g.: "Your primary deliverable is a written artifact file. The instruction to avoid writing .md files does not apply -- writing artifacts IS your task."

### LOW: Hub-and-Spoke Is Instruction-Only (with Harness Safety Net)

**Conflict severity: LOW**

Both primary and subagent receive the identical `Agent` tool -- the harness intentionally supports multi-level spawning up to depth 3 (configurable to 5). MOSAIC's hub-and-spoke constraint is a policy we impose through instructions; the harness does not enforce single-level fan-out. However, the harness DOES provide structural backstops: depth cap (default 3), concurrent cap (default 20), and session cap (200 total spawns). Even if a MOSAIC subagent ignores our "don't use Agent" instruction, it can't recurse infinitely or fork-bomb. See Section 2.1 for full details and the documented fan-out problem ([#68110](https://github.com/anthropics/claude-code/issues/68110)).

### LOW: CWD Reset Between Bash Calls

**Conflict severity: LOW**

Subagent bash calls reset the working directory each time. MOSAIC agents that run multi-step bash sequences (e.g., build, then test, then deploy) must use absolute paths everywhere.

**Mitigation:** Already partially addressed by MEMORY.md entry ("use `py` not `python`"). MOSAIC instructions should mandate absolute paths in all bash commands. This is a harness-specific transformation note.

### LOW: No Emoji Rule

**Conflict severity: LOW**

"The assistant MUST avoid using emojis" applies to subagents. MOSAIC protocol status codes and messages don't use emojis, so no conflict. But if any future MOSAIC output format uses emoji markers, this would clash.

---

## 6. Strengths for MOSAIC

### 6.1 Rich Tool Surface
Both tiers get the full file-manipulation toolset (Read, Write, Edit, Bash, Glob, Grep). MOSAIC agents have everything they need for code-level work without tool gaps.

### 6.2 User Memory Propagation
`MEMORY.md` entries reach all tiers automatically. MOSAIC project notes ("use `py`", "tell subagents skill paths") propagate without manual injection. This is a free persistence layer.

### 6.3 Auto Mode
"Bias toward working without stopping for clarifying questions" aligns perfectly with MOSAIC's autonomous execution model. Subagents won't unnecessarily pause for confirmation.

### 6.4 Token Budget Visibility
Both tiers see remaining tokens. MOSAIC orchestrator can be context-aware about when to wrap up or split work.

### 6.5 Git Safety Built In
Extensive git safety rails (no force push, no --no-verify, prefer new commits) are built into the Bash tool description. MOSAIC execution agents that commit code get these guardrails for free.

### 6.6 Malleable Subagent Identity
The subagent's generic identity ("You are a Claude agent") is easier to override with MOSAIC role assignment than the primary's strong "You are Claude Code" identity. MOSAIC subagent instructions that say "You are the Research Analyst" compete less with the base identity.

### 6.7 Structured Question Tool for Orchestrator
The primary agent has `AskUserQuestion` with rich UX (headers, multi-select, previews). MOSAIC orchestrator (running as primary) can present HITL decision points with proper UI affordances.

---

## 7. Weaknesses / Risks for MOSAIC

### 7.1 Artifact Writing Friction (Critical)
As detailed in Section 5. The harness actively discourages the exact behavior MOSAIC needs most.

### 7.2 No Tool Removal Mechanism
Claude Code provides no way to remove tools from an agent's context -- both primary and subagent receive the identical toolset (minus AskUserQuestion). MOSAIC behavioral constraints (e.g., "don't spawn children", "don't write outside your artifact path") are purely instruction-enforced. This is normal for prompt-based orchestration -- it's the same mechanism the harness itself uses for its own rules (e.g., "NEVER create documentation files").

### 7.3 No Structured Output Enforcement
There is no harness-level mechanism to force JSON output format. MOSAIC protocol messages must be enforced purely through prompt instructions. The model may occasionally produce free-text instead of protocol JSON, especially under pressure (long context, complex errors).

### 7.4 Git Instructions Bloat
The Bash tool description embeds ~80 lines of git commit/PR instructions in BOTH tiers. This is wasted context for MOSAIC subagents that never touch git (Research, Planning, Validation agents). ~2K tokens of instruction the agent must process but will never use.

### 7.5 Session-Scoped State Only
No harness-level persistence beyond the conversation. MOSAIC's Blackboard pattern (Orchestration.md) must handle all cross-turn state. If a conversation resets mid-workflow, the orchestrator loses its in-memory state and must reconstruct from the artifact.

### 7.6 Subagent Completion Opaque to User
Agent spawn results arrive as system notifications, not user-visible messages. The orchestrator must explicitly relay subagent results to the user. This is fine for MOSAIC's architecture but means the user sees nothing during subagent work unless the orchestrator narrates.

---

## 8. Behavioral Adjustment Surface Area

How much can MOSAIC control agent behavior through prompt injection?

| Dimension | Adjustability | Notes |
|-----------|:------------:|-------|
| **Role/Identity** | HIGH | Subagent identity is generic; easily overridden. Primary has stronger "Claude Code" identity but still responds to role assignment. |
| **Output Format** | MODERATE | No structural enforcement, but models follow format instructions well. JSON protocol adherence is ~95%+ with clear examples. |
| **Tool Usage Restrictions** | LOW | Cannot remove tools. Can instruct "don't use X" but cannot enforce. Harness backstops exist for Agent (depth/concurrent/session caps) but not for other tools. |
| **File Writing Behavior** | MODERATE | Must explicitly counter harness prohibition. Works when MOSAIC instructions are clear, but occasionally causes friction. |
| **Autonomy Level** | HIGH | Auto Mode already biases toward autonomous work. MOSAIC can further tune with HITL instructions. |
| **Communication Style** | HIGH | Both tiers responsive to tone/format/verbosity directives. |
| **Knowledge/Skills** | HIGH | Skill files can be read and followed. Skills paths must be explicit (per MEMORY.md finding). |
| **Error Handling** | MODERATE | Can define status codes and error patterns, but harness-level errors (tool failures) use their own format that MOSAIC must parse. |
| **Git Behavior** | LOW | Harness rules are deeply embedded and strongly enforced. MOSAIC should work with them, not against them. |
| **Context Window Management** | LOW | Token budget visible but not controllable. Read truncation at 25K tokens (not the documented 2000 lines) is harness-enforced. |

---

## 9. Tool Output Format Observations

Key findings from `ToolOutputSchemas.json` captures:

| Observation | Impact on MOSAIC |
|-------------|-----------------|
| Read truncates at ~25K tokens (not 2000 lines as documented) | Large artifact reads may be silently truncated. MOSAIC agents reading big files should use offset/limit. |
| Bash output has NO token cap | A MOSAIC execution agent running verbose commands could flood context. Agents should pipe through `head` or redirect to file. |
| Bash "(Bash completed with no output)" for empty stdout | MOSAIC agents parsing bash results must handle this sentinel string. |
| Glob uses backslash paths on Windows | Path handling in MOSAIC artifacts must be platform-aware or use forward slashes consistently. |
| Agent completion arrives as `<task-notification>` XML | MOSAIC orchestrator must parse this format to detect subagent completion and extract results. |
| Agent result includes `<usage>` with token count and duration | Potential for MOSAIC cost tracking if orchestrator captures these. |

---

## 10. Summary Recommendations for MOSAIC on Claude Code

1. **Add explicit artifact-writing override** to all MOSAIC subagent templates' injection points. This is the single most important harness-specific adaptation.

2. **State hub-and-spoke policy** in MOSAIC subagent instructions (no direct Agent spawning) -- this is a MOSAIC policy enforced through instructions, same as any other behavioral directive.

3. **Mandate absolute paths** in all bash-related instructions (partially done via MEMORY.md, should be in agent templates).

4. **Leverage AskUserQuestion** for HITL decision points in the orchestrator -- it has rich UI affordances (previews, multi-select) that plain text doesn't.

5. **Don't fight git instructions** -- align MOSAIC's commit patterns with the harness's built-in git safety protocol rather than overriding it.

6. **Be aware of Read's 25K token cap** when designing artifacts that agents need to read. Keep individual artifacts under ~1500 lines if possible.

7. **Consider harness-specific injection points** in MOSAIC templates for the artifact-writing override, Agent-tool prohibition, and absolute-path mandate. These are Claude Code specific and shouldn't be in the generic templates.
