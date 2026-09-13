# OpenCode Harness Analysis Report — DRAFT

> **⚠ DRAFT — Contains known factual errors. Needs review against raw capture data before treating as reliable.**

| Field | Value |
|-------|-------|
| **Harness** | OpenCode |
| **Model observed** | openai/gpt-5.6-sol |
| **Captured** | 2026-09-06 |
| **Analyzed** | 2026-09-07 |
| **Source files** | PrimaryAgent/ and Subagent/ in this folder |

---

## 1. Primary Agent vs Subagent: Observed Differences

The two contexts are **remarkably similar** -- far more so than Claude Code's primary/subagent split. The differences are minor and surgical:

| Aspect | Primary Agent | Subagent | Impact |
|--------|---------------|----------|--------|
| **Juice** | 16.855 | 16 | Unclear internal metric; slightly different budget/priority weighting |
| **Missing built-in tools** | `edit`, `write` listed as missing in capture notes | None missing | Capture artifact -- both contexts appear to have the same tool set in the actual prompt |
| **Glob description** | Extra sentence: "You have the capability to call multiple tools in a single response. It is always better to speculatively perform multiple searches as a batch that are potentially useful." | Absent | Primary agent gets a nudge toward speculative parallel search; subagent doesn't |
| **Todowrite schema** | `status` and `priority` are plain `string` type | `status` and `priority` have `enum` constraints (`pending`/`in_progress`/`completed`/`cancelled`, `high`/`medium`/`low`) | Subagent gets stricter validation; primary relies on prose description only |
| **ToolOutputSchemas (verbatim)** | Minor wording differences in `tool_uses.parameters.description`: "...tool's own specification." | "...tool's own specifications." (plural) | Inconsequential |

**Everything else is identical:** same tool set, same tool descriptions, same environment block, same skills, same `# Instructions` injection point, same channel system.

### What this means for MOSAIC

OpenCode gives subagents **nearly full parity** with the primary agent. Unlike Claude Code (which strips many tools from subagents), an OpenCode subagent can do everything the primary agent can: read, write, search, run commands, launch nested subagents, fetch web content. This is a significant advantage for orchestration -- subagents are not capability-restricted.

---

## 2. Harness Architecture

### 2.1 Prompt Structure

```
[System-level preamble]
  "You are an AI assistant accessed via an API."
  Oververbosity setting (default 3 -- concise)
  Channel declarations (analysis, commentary, final, summary)
  Juice value

[# Instructions]
  <-- THIS IS WHERE AGENT .md BODY GETS INJECTED -->
  (Empty in the harness default; filled by the agent file)

[# Tools]
  Tool definitions in TypeScript-like format
  Grouped by namespace (functions, multi_tool_use)

[Post-instructions footer]
  Model identity line
  [env] block (working directory, platform, date, git status)
  [available_skills] block
```

### 2.2 Channel System

OpenCode requires every message to specify a channel: `analysis`, `commentary`, `final`, or `summary`. Tool calls target `commentary` channel. This is unique to OpenCode and has **no equivalent in Claude Code or GHCP-CLI**.

**MOSAIC implication:** Our agent instructions don't reference channels. This is fine -- the model handles channel routing internally. But if we ever need to control output routing (e.g., "think silently, then respond"), the channel system is the mechanism.

### 2.3 Tool Namespace System

Tools live in namespaces:
- `functions` -- all regular tools (read, bash, glob, grep, etc.)
- `multi_tool_use` -- the `parallel` wrapper for concurrent tool calls

The `parallel` tool explicitly states: "only tools defined in developer messages are allowed." System tools cannot be called through the parallel wrapper.

---

## 3. Tool Inventory

### 3.1 Complete Tool Set (identical Primary & Subagent)

| Tool | Category | Notes |
|------|----------|-------|
| `apply_patch` | Edit | **Patch-based only** -- no separate edit/write tools |
| `bash` | Shell | Actually PowerShell 5.1 despite the name |
| `read` | File I/O | Files and directories; supports offset/limit |
| `glob` | Search | File pattern matching |
| `grep` | Search | Content search with regex |
| `skill` | Meta | Dynamic skill injection |
| `task` | Orchestration | Subagent dispatch with typed `subagent_type` |
| `question` | User I/O | Structured multiple-choice questions |
| `todowrite` | Tracking | Task list management |
| `webfetch` | Web | URL content fetching |
| `websearch` | Web | Real-time web search with configurable depth |
| `parallel` | Meta | Concurrent tool execution wrapper |

### 3.2 Key Tool Differences from Claude Code

| Feature | OpenCode | Claude Code |
|---------|----------|-------------|
| **File editing** | `apply_patch` (patch format) | Separate `Edit` + `Write` tools |
| **Shell** | PowerShell 5.1 (called `bash`) | Git Bash (actual POSIX sh) |
| **Subagent dispatch** | `task` with `subagent_type` param | `Task` with `description` only |
| **User interaction** | `question` (structured choices) | MCP `ask_user_questions` |
| **Task tracking** | `todowrite` (built-in) | No equivalent |
| **Parallel calls** | Explicit `parallel` wrapper tool | Native parallel tool_use |
| **Web search** | `websearch` with search type/crawl modes | `WebSearch` (simpler) |
| **Skill loading** | `skill` tool (dynamic) | No direct equivalent |

---

## 4. Strengths

### 4.1 Subagent Capability Parity
Subagents get the **full tool set** including `task` (can launch nested subagents), `bash`, `apply_patch`, web tools, and `question`. No tool stripping. This means our orchestrator subagent can truly delegate work to MOSAIC subagents without worrying about missing capabilities.

### 4.2 Typed Subagent Dispatch
The `task` tool has an explicit `subagent_type` parameter. The available agent types are dynamically injected from the workspace's agent directory. This maps cleanly to MOSAIC's agent registry -- each MOSAIC subagent becomes a `subagent_type` value.

### 4.3 Session Continuity
`task` supports `task_id` for resuming a previous subagent session. This enables multi-turn subagent interactions without re-establishing context -- valuable for iterative review cycles in MOSAIC workflows.

### 4.4 Structured User Interaction
The `question` tool provides structured multiple-choice UI. This is cleaner than free-text for HITL checkpoints. Options can be labeled with recommendations.

### 4.5 Task Tracking Built-In
`todowrite` provides native task list management. The orchestrator can use this to show the user a structured view of workflow progress without custom artifact management.

### 4.6 Dynamic Skill Loading
Skills can be injected mid-conversation. Our MOSAIC skills (lean-tdd, efficient-file-reading) are already visible and loadable.

### 4.7 Low Verbosity Default
Oververbosity 3 (out of 10) encourages concise responses. Good for MOSAIC subagents that should return structured data rather than essays.

---

## 5. Weaknesses & Risks

### 5.1 Patch-Based Editing Only
`apply_patch` is the only file modification tool. No simple "replace this string with that string" edit and no "write this entire file" operation. For MOSAIC agents that need to:
- Create new artifacts (Orchestration.md, plans, etc.) -- must use `*** Add File` syntax
- Update existing artifacts -- must use `*** Update File` with diff hunks
- Large file rewrites -- must still go through patch format

**Risk:** Patch format is more error-prone than direct write, especially for large markdown artifacts. The model must correctly construct `+` prefixed lines for every new line. A single formatting mistake corrupts the file.

**Mitigation:** Our agent instructions should emphasize using `*** Add File` for new artifacts (simpler) and keeping updates surgical.

### 5.2 PowerShell Masquerading as Bash
The tool is called `bash` but runs PowerShell 5.1. This creates confusion:
- Our MOSAIC agent instructions reference shell commands (e.g., `py` for Python)
- Any POSIX syntax in agent instructions will fail
- `&&` chaining doesn't work -- must use `cmd1; if ($?) { cmd2 }`

**Risk:** Generic MOSAIC templates that include shell examples will break if they assume POSIX syntax.

**Mitigation:** Already partially handled by our `project_python_command_alias.md` memory note. Transformation to OpenCode harness must rewrite shell examples.

### 5.3 No Direct Edit/Write Equivalent
Claude Code's `Edit` tool is surgical: "find this exact string, replace with that." OpenCode's `apply_patch` requires context lines and `@@` anchors. This means:
- Agent instructions that say "edit file X to change Y" may produce different behavior
- The model must locate the right context anchor for each change

### 5.4 Opaque "Juice" and Oververbosity Systems
These are internal OpenCode control mechanisms we can't influence:
- **Juice** (16.855/16) -- unclear what it controls. Possibly token budget, possibly priority scoring.
- **Oververbosity** (3) -- affects response length. Can be overridden by developer instructions, but we can't set it directly.

### 5.5 Channel System Complexity
Every message must declare a channel. While the model handles this internally, if our MOSAIC instructions accidentally create confusion about what channel to use, the model's output may be misrouted (e.g., analysis intended for the user going to `commentary` instead of `final`).

---

## 6. Potential Conflicts with MOSAIC Instructions

### 6.1 Communication Protocol JSON Output
MOSAIC expects subagents to return structured JSON (`status_code`, `status_message`, `deliverables`). OpenCode's channel system adds a layer: the JSON response needs to go through the right channel. In practice, since agent instructions say "return JSON," the model should comply. But the channel routing adds an implicit wrapping layer.

**Conflict level: LOW** -- the model follows explicit instructions over defaults.

### 6.2 File Operations in Agent Instructions
MOSAIC agents reference writing artifacts (Orchestration.md, plans, reviews). Our generic templates use `{model-identifier}` and assume a write/edit tool. OpenCode only has `apply_patch`.

**Conflict level: MEDIUM** -- transformation must ensure any "write file" instructions translate to patch syntax expectations. The model will likely figure it out, but explicit guidance helps.

### 6.3 Shell Command Syntax
Any agent instructions that include shell examples (running tests, git commands, build commands) must be PowerShell-compatible.

**Conflict level: MEDIUM** -- transformation must catch all POSIX-isms.

### 6.4 Parallel Tool Execution
MOSAIC doesn't explicitly manage parallel vs sequential tool calls. OpenCode has an explicit `parallel` wrapper. The model decides when to use it. If our agent instructions say "do X then Y then Z" the model may parallelize incorrectly.

**Conflict level: LOW** -- sequential instructions are generally respected.

### 6.5 Todowrite vs Orchestration.md
OpenCode encourages using `todowrite` for task tracking. MOSAIC uses Orchestration.md as the blackboard. These could compete -- the orchestrator might use `todowrite` AND maintain Orchestration.md, or confuse which one to update.

**Conflict level: MEDIUM** -- our agent instructions should be explicit that Orchestration.md is the primary tracking mechanism. Consider whether to leverage `todowrite` as a complementary UI feature or suppress it.

### 6.6 Skill Auto-Loading
OpenCode may proactively load skills when it thinks the task matches. If a MOSAIC subagent's task description matches a skill description (e.g., a test-writing agent matching `lean-tdd`), the skill may auto-load and add instructions that weren't planned.

**Conflict level: LOW-MEDIUM** -- our skills are designed to be additive, but unplanned skill injection could change behavior.

---

## 7. Behavior Adjustability

### What We Can Control

| Mechanism | Scope | How |
|-----------|-------|-----|
| **Agent instructions** | Full | The `# Instructions` section is where our agent .md body is injected. We have complete control over this content. |
| **Skills** | Additive | We can create custom skills that get injected on demand via the `skill` tool. |
| **Agent types** | Full | We define the `subagent_type` registry -- what agents exist and what they do. |
| **Workspace config** | Moderate | `opencode.json` / `.opencode/` config files control some harness behavior. |
| **MCP tools** | Additive | We can add MCP servers for additional capabilities. |
| **Prompt content** | Full | Everything in the agent .md file is injected verbatim into `# Instructions`. |

### What We Cannot Control

| Mechanism | Why |
|-----------|-----|
| **Oververbosity** | Hardcoded per session; can be overridden by explicit instructions but not set programmatically |
| **Juice** | Internal metric, no user control |
| **Channel system** | Baked into the harness; cannot be disabled or reconfigured |
| **Tool definitions** | Built-in tools are fixed; we can only add MCP tools, not modify built-ins |
| **Parallel wrapper** | The `multi_tool_use.parallel` tool is system-provided |
| **Patch format** | The `apply_patch` format is fixed; we can't switch to direct write |

### Effective Adjustability: HIGH

OpenCode gives us the `# Instructions` section as a clean injection point. The harness itself is lightweight -- almost no behavioral instructions beyond tool usage patterns. Our MOSAIC agent instructions will dominate the model's behavior. The main constraints are tool mechanics (patch-based editing, PowerShell shell) rather than behavioral restrictions.

---

## 8. Transformation Implications

When transforming generic MOSAIC templates to OpenCode:

1. **Tool references**: `edit` / `write` -> guidance for `apply_patch` usage
2. **Shell syntax**: POSIX -> PowerShell 5.1 (no `&&`, use `; if ($?) {}`)
3. **Python command**: `python` -> `py` (per workspace memory)
4. **Subagent dispatch**: Generic dispatch -> `task` with `subagent_type`
5. **User interaction**: Generic HITL -> `question` tool structured choices
6. **Model identifier**: `{model-identifier}` -> `openai/gpt-5.6-sol` (or configurable)
7. **File operations note**: Consider adding explicit guidance that file creation uses `apply_patch` with `*** Add File` syntax

---

## 9. Summary

OpenCode is a **strong harness for MOSAIC orchestration**. Its key advantage is subagent capability parity -- subagents get the full tool set, making the hub-and-spoke delegation model work without capability gaps. The typed `subagent_type` dispatch maps naturally to our agent registry. The main friction points are the patch-based editing model (more error-prone than direct write) and the PowerShell shell (requires syntax translation). The behavioral control surface is large -- our agent instructions dominate the prompt, with the harness adding only lightweight operational guidance.
