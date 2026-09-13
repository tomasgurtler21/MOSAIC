# Cross-Harness Comparison — DRAFT

> **⚠ DRAFT — Contains known factual errors. Needs review against individual harness reports before treating as reliable.**

| Field | Value |
|-------|-------|
| **Harnesses Compared** | Claude Code, GHCP-CLI, OpenCode |
| **Created** | 2026-09-07 |
| **Source** | Individual `AnalysisReport.md` files in each harness folder |
| **Purpose** | Side-by-side comparison to inform MOSAIC transformation strategy and identify harness-agnostic vs harness-specific design decisions |

---

## 1. Harness Identity Summary

| Dimension | Claude Code | GHCP-CLI | OpenCode |
|-----------|-------------|----------|----------|
| **Default Model** | claude-opus-4-6 | claude-sonnet-5 | openai/gpt-5.6-sol |
| **Shell** | Git Bash (POSIX sh) | PowerShell 5.x only | PowerShell 5.1 (named `bash`) |
| **Primary Identity** | "You are Claude Code, Anthropic's official CLI" | "You are the GitHub Copilot CLI" | "You are an AI assistant accessed via an API" |
| **Subagent Identity** | "You are a Claude agent" (generic) | No identity statement | Same as primary (generic) |
| **Agent Dispatch Tool** | `Agent` | `task` (custom agent types) | `task` (typed `subagent_type`) |
| **File Editing** | `Edit` (string replace) + `Write` | `edit` (string replace) + `create` | `apply_patch` only (patch format) |
| **User Interaction** | `AskUserQuestion` (primary only) | `ask_user` (primary only) | `question` (both tiers) |

---

## 2. Subagent Capability Parity
HarnessKnowledge\SystemPromptCapture
This is the single most consequential architectural difference between harnesses. It determines how much MOSAIC subagents can accomplish autonomously.

| Tool / Capability | Claude Code | GHCP-CLI | OpenCode |
|-------------------|:-----------:|:--------:|:--------:|
| File read | Both | Both | Both |
| File write/edit | Both | Both | Both |
| Shell execution | Both | Both | Both |
| Content search (grep) | Both | **Primary only** | Both |
| File search (glob) | Both | **Primary only** | Both |
| Nested agent spawning | Both (depth-capped) | **Primary only** | Both |
| Web fetch | Configurable | Both | Both |
| Web search | Configurable | **Primary only** | Both |
| User interaction | **Primary only** | **Primary only** | Both |
| Task/todo tracking | None built-in | **Primary only** | Both |
| Skills | Both | Both | Both |
| Memory/persistence | Both | Both | N/A |
| Sibling communication | No | **Subagent only** | No |

### Parity Rating

| Harness | Subagent Parity | Summary |
|---------|:-----------:|---------|
| **OpenCode** | **Highest** | Near-identical tool sets. Subagents are full peers. |
| **Claude Code** | High | Only `AskUserQuestion` missing from subagents. All core tools present. |
| **GHCP-CLI** | **Lowest** | Subagents lose 7 tools including `grep`, `glob`, `task`. Severely handicapped for autonomous work. |

**MOSAIC Impact:** GHCP-CLI subagents require explicit PowerShell fallback instructions for code search and file discovery. OpenCode and Claude Code subagents work out of the box.

---

## 3. File Writing Restrictions

Most harnesses have some form of anti-file-writing guidance — a common friction point for MOSAIC's artifact-based Blackboard pattern. OpenCode is the exception.

| Harness | Restriction | Scope | Severity | Escape Clause |
|---------|------------|-------|----------|---------------|
| **Claude Code** | "Do NOT Write report/summary/findings/analysis .md files" | Subagent only | HIGH | None explicit — must override via agent instructions |
| **Claude Code** | "NEVER create documentation files (*.md) or README files" | Both tiers (Write tool) | MEDIUM | "unless explicitly requested by the User" |
| **GHCP-CLI** | "Do NOT write output to files" + no temp files, no redirection | Subagent only | **CRITICAL** | None — hard prohibition in system prompt |
| **OpenCode** | None observed | — | None | N/A |

### Recommended Override Strategy (per harness)

| Harness | Strategy |
|---------|----------|
| **Claude Code** | Add explicit counter-directive in agent instructions: "Writing artifact files IS your task. The general prohibition does not apply." |
| **GHCP-CLI** | Needs empirical testing — if prompt-level only, same override works. If runtime-enforced, must relay artifacts through orchestrator response text. |
| **OpenCode** | No override needed. |

---

## 4. System Prompt Architecture

How each harness structures its system prompt, and where MOSAIC agent instructions get injected.

| Aspect | Claude Code | GHCP-CLI | OpenCode |
|--------|-------------|----------|----------|
| **Injection point** | Agent `.md` file becomes subagent system prompt via Agent tool | Agent body injected as system prompt section | `# Instructions` section — clean dedicated block |
| **Harness behavioral rules** | Embedded in tool descriptions + system-reminders | Extensive `[code_change_instructions]`, `[tips_and_tricks]`, etc. | Minimal — oververbosity setting + channel declarations |
| **Bloat in subagent prompt** | ~80 lines git instructions in Bash tool (both tiers) | Phantom tool references (grep/glob/task guidance for tools subagent doesn't have) | Clean — no phantom references |
| **Dynamic context blocks** | MEMORY.md, env, git status, model ID, token budget | Environment, version, model info | `[env]` block, `[available_skills]` |
| **Context propagation to subagents** | Full — MEMORY.md + all system-reminders | Partial — some sections stripped | Full — identical prompt structure |

### Prompt Cleanliness Ranking

1. **OpenCode** — Minimal harness instructions, clean injection point, no phantom references
2. **Claude Code** — Some bloat (git instructions) but no contradictions in tool references
3. **GHCP-CLI** — Phantom tool references in subagent prompt create model confusion

---

## 5. Shell & Platform Considerations

| Aspect | Claude Code | GHCP-CLI | OpenCode |
|--------|-------------|----------|----------|
| **Actual shell** | Git Bash (POSIX) | PowerShell 5.x | PowerShell 5.1 |
| **Tool name** | `Bash` | `powershell` | `bash` (misleading) |
| **`&&` chaining** | Works | Forbidden | Doesn't work |
| **Path separators** | Forward slash | Backslash | Backslash |
| **Python command** | `py` (per workspace) | `py` (per workspace) | `py` (per workspace) |
| **CWD persistence** | Resets between calls (subagent) | Persists | Resets between calls (implied) |
| **POSIX utilities** | Available (grep, find, etc.) | Not available | Not available |

**Transformation Note:** Generic MOSAIC templates must not include shell-specific syntax. Harness transformation must convert:
- POSIX `&&` → PowerShell `; if ($?) { }` (GHCP-CLI, OpenCode)
- `grep`/`find` CLI usage → `Select-String`/`Get-ChildItem` (GHCP-CLI, OpenCode)
- Relative paths → Absolute paths (Claude Code subagents, OpenCode)

---

## 6. Multi-Model & Dispatch Capabilities

| Aspect | Claude Code | GHCP-CLI | OpenCode |
|--------|-------------|----------|----------|
| **Model selection** | Fixed (primary model) | 18 models available per-dispatch | Configurable per agent type |
| **Reasoning effort tiers** | Not exposed | low/medium/high/xhigh/max | Not observed |
| **Context tier override** | Not exposed | `long_context` available | Not observed |
| **Agent type registry** | `.claude/agents/*.md` files | Custom agent types (27 observed) | `subagent_type` param from agent directory |
| **Session continuity** | Not exposed | Not observed | `task_id` for resuming sessions |
| **Concurrent subagent cap** | 20 (configurable) | Not documented | Not documented |
| **Nesting depth cap** | 3 (configurable to 5) | 1 (no `task` for subagents) | No cap observed |
| **Session spawn cap** | 200 | Not documented | Not documented |

**MOSAIC Impact:** GHCP-CLI's multi-model support is the richest — the orchestrator could route different subagents to different models based on task complexity. Claude Code and OpenCode tie agents to the session's model.

---

## 7. Unique Harness Features

Features that exist in only one harness and could benefit (or complicate) MOSAIC.

### Claude Code Only
| Feature | MOSAIC Relevance |
|---------|-----------------|
| **Configurable nesting depth/concurrency** | Safety net for hub-and-spoke — prevents runaway spawning |
| **Token budget visibility** | Orchestrator can make context-aware decisions about when to wrap up |
| **User memory propagation (MEMORY.md)** | Free persistence layer — project notes auto-propagate to all tiers |
| **Scratchpad directory** | Session-specific temp space for intermediate files |

### GHCP-CLI Only
| Feature | MOSAIC Relevance |
|---------|-----------------|
| **Sibling communication** | Subagents can message each other — partial workaround for hub-and-spoke, but conflicts with MOSAIC's routing model |
| **SQL session database** | Built-in SQLite with todos/deps tables — alternative state tracking mechanism |
| **Per-dispatch model selection** | Route complex tasks to stronger models, simple tasks to faster/cheaper ones |
| **Persistent memory (store/vote)** | Cross-session memory with voting — could supplement artifact persistence |

### OpenCode Only
| Feature | MOSAIC Relevance |
|---------|-----------------|
| **Channel system** | Output routing (analysis/commentary/final/summary) — could map to MOSAIC's structured responses |
| **Todowrite** | Native task tracking UI — complementary to Orchestration.md or potential alternative |
| **Session continuity (task_id)** | Multi-turn subagent interactions without re-establishing context |
| **Oververbosity control** | Default conciseness (3/10) aligns with MOSAIC's structured output preference |
| **Dynamic skill loading** | Skills injected mid-conversation on demand |

---

## 8. MOSAIC Compatibility Scorecard

Rating each harness on key MOSAIC architectural requirements.

| Requirement | Claude Code | GHCP-CLI | OpenCode |
|-------------|:-----------:|:--------:|:--------:|
| **Hub-and-spoke dispatch** | Good | Good | Good |
| **Subagent autonomy** | Strong | Weak | **Strongest** |
| **Artifact file writing** | Friction (override needed) | **Blocked** (needs testing) | Clean |
| **Code search from subagents** | Strong | **Weak** (PS fallback) | Strong |
| **HITL at orchestrator** | Strong (AskUserQuestion) | Strong (ask_user) | Strong (question) |
| **HITL at subagent** | Weak (relay needed) | Weak (relay needed) | **Strong** (question available) |
| **Skills injection** | Strong | Strong | Strong |
| **Behavioral override surface** | High | Moderate | **Highest** |
| **Prompt cleanliness** | Moderate | Low | **High** |
| **Shell compatibility** | **Best** (POSIX) | Weak (PS-only) | Weak (PS masquerading as bash) |

### Overall MOSAIC Fit

| Rank | Harness | Verdict |
|:----:|---------|---------|
| 1 | **OpenCode** | Best structural fit. Full subagent parity, no file-write restrictions, clean injection point, minimal harness interference. Main friction: patch-only editing and PowerShell shell. |
| 2 | **Claude Code** | Strong fit with known workarounds. Rich tool surface at both tiers, good safety nets. Main friction: artifact writing prohibition requires explicit override. |
| 3 | **GHCP-CLI** | Viable with significant workarounds. Rich model selection, but subagent tool poverty and file-write ban are serious obstacles. Best for orchestration patterns where subagents return results via text rather than files. |

---

## 9. Transformation Strategy Implications

### What Must Be Harness-Specific

These cannot live in generic templates — they must be handled in QuickReference transformation rules or harness injection points:

| Concern | Generic Template | Transformation Target |
|---------|-----------------|----------------------|
| Artifact writing override | Not needed | Claude Code: add counter-directive; GHCP-CLI: add counter-directive + test; OpenCode: none |
| Shell syntax in examples | Use pseudocode or POSIX | GHCP-CLI/OpenCode: convert to PowerShell |
| Code search fallback | Reference grep/glob generically | GHCP-CLI: add PowerShell Select-String/Get-ChildItem instructions |
| HITL relay pattern | Standard HITL checkpoint | Claude Code/GHCP-CLI: relay through orchestrator; OpenCode: direct |
| Agent tool prohibition | "Do not spawn child agents" | All: include this; Claude Code has harness safety net; GHCP-CLI: moot (no tool); OpenCode: instruction-only |
| Path format | Use forward slashes | GHCP-CLI/OpenCode: note backslash convention |
| Python command | `python` | All (this workspace): `py` |

### What Can Stay Generic

These work identically across all three harnesses:

- Communication protocol (status codes, JSON format)
- Agent identity and role assignment
- Capability and constraint definitions
- Error handling patterns
- Skills references (all harnesses support skills)
- Artifact naming conventions
- Workflow phase structure

---

## 10. Open Questions

| # | Question | Why It Matters |
|---|----------|---------------|
| 1 | Is GHCP-CLI's file-write ban runtime-enforced or prompt-only? | Determines if MOSAIC can work on GHCP-CLI at all with artifact-based patterns |
| 2 | Does OpenCode's `apply_patch` reliably handle large new file creation? | MOSAIC subagents create multi-hundred-line artifacts; patch format errors could corrupt them |
| 3 | Can GHCP-CLI's sibling communication be leveraged without breaking hub-and-spoke? | Could enable faster coordination for specific workflow patterns |
| 4 | How does OpenCode's channel system interact with MOSAIC JSON protocol output? | Need to confirm JSON responses route correctly through channels |
| 5 | What is the practical token cost of GHCP-CLI's phantom tool references? | Subagents waste tokens processing guidance for tools they don't have |

---

*Generated from individual harness analysis reports in this folder.*
