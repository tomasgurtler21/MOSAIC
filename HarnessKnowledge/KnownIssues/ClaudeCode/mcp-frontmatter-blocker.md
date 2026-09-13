# MCP in Agent Frontmatter: What Works, What Doesn't

> **Status:** Not a blocker — `mcp__<server>__*` in `tools:` is the correct and only pattern needed; instruction routing quirks (CC-080, CC-081) are low-impact
> **Date:** 2026-09-13 (Round 5 verification; originals 2026-09-12 / 2026-09-13)
> **Claude Code version:** v2.1.270 (Rounds 1–2 on v2.1.269)
> **Related issues:** CC-079 (reframed), CC-080, CC-081
> **Affects:** Any agent that uses MCP tools

---

## Summary

**`mcp__<server>__*` in `tools:` is the only thing that matters.** MCP tool access works in all invocation modes — `--agent`, subagent foreground, subagent background — when the agent's `tools:` field includes MCP wildcards.

The earlier "hard blocker" conclusion was wrong because Rounds 1–2 tested agents that had `mcpServers:` string-references but **no `mcp__*` in their `tools:` field**. The `tools:` field is an allowlist — it filters out MCP tools not listed in it. The probes correctly observed zero MCP tools, but incorrectly attributed this to MCP being broken rather than to the allowlist doing its job.

**What caused the confusion between rounds:**

1. **Rounds 1–2** tested the non-updated agent pattern: `tools: Read, Write, ...` (no `mcp__*`) + `mcpServers: [server-name]`. Zero MCP tools → "blocker." But the `tools:` allowlist was filtering them. `mcpServers:` connected the server (instructions appeared), it just didn't add tools past the allowlist.
2. **Round 3** tested with `mcp__*` in `tools:` → tools appeared, calls succeeded → "not a blocker."
3. **A second confound:** Round 3 also tested **inline** `mcpServers:` definitions (full server config in YAML). MOSAIC never uses inline definitions — `mosaic-deploy` expands custom tools to `mcp__%s__*` in `tools:` and doesn't output `mcpServers:` at all. Inline server definitions are a user/project concern, not a MOSAIC deployment concern.

**Round 5 (this session, v2.1.270)** confirmed with live tests across both `--agent` and native subagent modes — see [Reproduction Evidence](#reproduction-evidence).

**CC-079 reframed:** The original description ("`mcpServers:` silently ignored when `tools:` present") is misleading. Round 5 Test 2 shows the server IS connected — MCP instructions for both `contact-user` and `github` were delivered. What `mcpServers:` doesn't do is bypass the `tools:` allowlist. This is consistent behavior, arguably by design: `tools:` is authoritative.

**Instruction routing (CC-080, CC-081)** is a separate, low-impact concern. MCP server instructions arrive late (after the first tool turn) and unscoped (all servers, not just the agent's). This affects guidance quality, not tool access. For MOSAIC this is low priority — agents get tools and schemas from turn one, which is what matters.

---

## The Invocation Modes

| Mode | How it runs | Example |
|------|------------|---------|
| **Primary** | Top-level agent in a `claude` session (no `--agent` flag) | The default assistant |
| **`--agent` launch** | `claude --agent <name>` or `claude --agent <name> -p "prompt"` | Running a specific agent from the CLI |
| **Subagent — foreground** | Agent tool, `run_in_background: false` | Orchestrator waiting on a worker |
| **Subagent — background** | Agent tool, `run_in_background: true` (the default), or `background: true` in frontmatter | Orchestrator fanning out workers |

Foreground vs. background choice: background is the default. In `-p` (headless) mode fork mode is off and Claude picks per call; frontmatter `background: true` forces background; `CLAUDE_CODE_DISABLE_BACKGROUND_TASKS=1` forces foreground everywhere.

---

## Tool Access Matrix

### What MOSAIC cares about (the `tools:` pattern)

| Frontmatter pattern | `--agent` | Subagent FG | Subagent BG |
|---------------------|-----------|-------------|-------------|
| **`tools:` with `mcp__<server>__*`** | ✅ OK | ✅ OK (loaded directly) | ✅ OK (loaded directly) |
| **No `tools:` (inherit all)** | ✅ OK | ✅ OK (MCP tools **deferred** — one `ToolSearch` call needed) | ✅ OK (deferred) |
| **`tools:` without `mcp__*`** + `mcpServers:` string ref | ❌ Zero MCP tools | ❌ Zero MCP tools | ❌ Zero MCP tools |

The first row is MOSAIC's deployed pattern — `mosaic-deploy` uses `custom_tool_template: "mcp__%s__*"` to expand custom tool names into `mcp__<server>__*` wildcards in `tools:`.

The third row is the **non-updated agent pattern** — agents deployed before `mosaic-deploy` handled MCP, or manually authored with `mcpServers:` instead of `mcp__*` in `tools:`. These agents have `mcpServers: [contact-user]` but their `tools:` field doesn't include `mcp__contact-user__*`, so they get zero MCP tools.

### Inline `mcpServers:` (outside MOSAIC's scope)

Inline `mcpServers:` means a full server definition in the agent's YAML (type, url, headers) — not just a name reference. MOSAIC never outputs this; server definitions are the user's/project's responsibility (in `.mcp.json`). Included here for completeness:

| Frontmatter pattern | Subagent FG | Subagent BG |
|---------------------|-------------|-------------|
| `tools:` with `mcp__*` + inline `mcpServers:` (trusted folder) | ✅ OK — server connects for this subagent only | ✅ OK |
| Inline `mcpServers:` in an untrusted folder | Silently not loaded | Same |
| Inline `mcpServers:` via `--agents` JSON | ✅ OK (no trust check) | ✅ OK |

### Key rules

- **The `tools:` allowlist is authoritative.** If `tools:` is present, only tools named in it are available. `mcpServers:` connects a server (instructions appear) but does NOT add tools past the allowlist. This is consistent behavior, not a bug.
- **Background subagents keep every MCP tool** but lose built-ins outside a fixed 19-tool allowlist — notably `AskUserQuestion` — and the MCP resource tools (#85230). For MOSAIC this makes `mcp__contact-user__*` the *only* way a background subagent can ask the user anything.
- **Inline `mcpServers:` definitions** connect when the subagent starts and disconnect when it finishes. Require trusted folder for `.claude/agents/`; `~/.claude/agents/`, `--agents` JSON, and SDK agents skip the trust check.

**Recommended MOSAIC pattern:**

```yaml
tools: Read, Write, Edit, Glob, Grep, mcp__github__*, mcp__contact-user__*
```

No `mcpServers:` field needed. The servers are defined in the project's `.mcp.json`; the `tools:` field grants access to their tools.

---

## Instruction Routing (CC-080, CC-081) — Low Impact

> **For MOSAIC this is low priority.** MCP tools (names, input schemas) are available from turn one — the agent can call them correctly. What arrives late is the server author's usage guidance (pagination tips, preferred call patterns). This affects call quality on the first turn, not tool access.

The question: does the agent receive the server's `instructions` (the server author's usage guidance)?

| Mode | Delivered? | When | Scoped to agent's tools? |
|------|-----------|------|--------------------------|
| Primary / `--agent` | YES | Session start | **NO** — all connected servers (CC-080) |
| Subagent (FG or BG) | **YES, late** | As an `mcp_instructions_delta` attachment (rendered as a `# MCP Server Instructions` system-reminder) **after the subagent's first tool call** | **NO** — all servers connected in the session, including to subagents with zero MCP tools; inline servers' instructions are included too |

Practical consequences for subagents:

- **Tools are visible from turn one; only the guidance is late.** The guidance arrives after the subagent's first *turn* — i.e. after the first round of tool calls completes, not after the first individual call. A first turn containing only non-MCP calls (e.g. reading the input artifact) means guidance is present before the first MCP call. But if the model batches a Read **and** an MCP call in parallel in its first turn, the MCP call still runs blind (Round 4, variant A). "Read something first" is only a reliable mitigation if the first turn contains no MCP call.
- **The cost of a blind first call is real but self-correcting.** In Round 4, both `harness-issue-hunter` runs made an unbounded `search_issues` call before seeing guidance; the 186K-character result exceeded the tool-output limit and the call failed. Both self-corrected on the next call once the guidance arrived.
- **Unscoped delivery wastes context** in subagents that don't use the servers, and can mislead them about capabilities they lack.

---

## Workarounds

### Tool access — solved

Use `mcp__<server>__*` in `tools:`. Works in every mode, foreground and background. No `mcpServers:` needed when the server is already defined in `.mcp.json`. Optionally use inline `mcpServers:` (full server definition, not string-ref) when a server should exist *only* for that subagent — but this is outside MOSAIC's deployment scope.

### Instructions for subagents — `SubagentStart` hook (recommended to evaluate)

`SubagentStart` hooks support `hookSpecificOutput.additionalContext` — documented as "String added to the subagent's context at the start of its conversation, before its first prompt." Verified in Round 3: delivered before the first tool call in both foreground and background.

Design sketch: a hook matched to MOSAIC agent types reads the agent's `tools:` line, and injects the instructions for exactly the `mcp__<server>__*` servers listed. This fixes timing *and* scoping, and keeps the instruction text in one harness-managed place instead of duplicated into every agent body.

Caveat: trust is model-dependent. With non-conflicting guidance, the foreground probe followed the hook-injected guidance; the background probe distrusted it and fell back to default behavior. The probe's conspicuous test label (`HOOK-CTX-9911`) likely contributed; realistic framing is untested.

### Instructions for subagents — body text (fallback)

Embed condensed server guidance in the agent body (system prompt). This is the only fully trusted channel, but it duplicates harness-managed content and drifts when the server changes its guidance.

### Excess instructions in primary / `--agent` (CC-080)

Move servers that only specific subagents need out of `.mcp.json` and into those agents' inline `mcpServers:`. The server then never connects in the main session, so its instructions never load there. Servers the primary itself uses remain unscoped.

---

## Impact on MOSAIC

**The two separate features:** `mcpServers:` (or `.mcp.json`) *connects to a server* — starts the process, establishes the connection, receives instructions. `tools:` *grants tools to the agent* — the allowlist that controls which connected server's tools the agent can call. Both must be satisfied: server connected AND tools in allowlist. Since `.mcp.json` already connects all project servers at session level, `mcpServers:` in agent frontmatter is redundant for MOSAIC — `mcp__<server>__*` in `tools:` is all that's needed.

- **No frontmatter blocker.** `mosaic-deploy` already handles this correctly via `custom_tool_template: "mcp__%s__*"` — a source agent declaring custom tool `github` gets `mcp__github__*` in its deployed `tools:` field.
- **Non-updated agents have dead weight.** Agents deployed before `mosaic-deploy` handled MCP (or manually authored) have `mcpServers: [contact-user]` without `mcp__contact-user__*` in `tools:`. The `mcpServers:` field connects the server redundantly (already connected via `.mcp.json`) while the `tools:` allowlist blocks its tools. These agents should either add `mcp__contact-user__*` to `tools:` or remove the dead `mcpServers:` field. The deploy tool previously warned about this; the warning has since been removed.
- **Instruction delivery** (CC-080/CC-081) is low-impact for MOSAIC. Agents get tool names and schemas from turn one — enough to call correctly. Late guidance affects call quality (pagination hints, field selection), not functionality. If this becomes a problem, a `SubagentStart` hook with `additionalContext` can inject guidance before the first turn.
- **Background dispatch is safe for MCP-using agents**, except those needing MCP *resources* or `AskUserQuestion` (use `contact-user` instead).
- **Inline server definitions** (full server config in frontmatter) are outside MOSAIC's deployment scope — server definitions belong in `.mcp.json`, not agent files.

---

## Upstream References

| Issue | Status | Relevance |
|-------|--------|-----------|
| [#85307](https://github.com/anthropics/claude-code/issues/85307) | Open | Instruction routing inverted — matches Round 3 (unscoped, late, injected into tool-less subagents) |
| [#29655](https://github.com/anthropics/claude-code/issues/29655) | Closed (not planned) | Subagents missing instructions — likely missed late delivery, as our Round 1 did |
| [#75283](https://github.com/anthropics/claude-code/issues/75283) | Stale-closed | Instructions appended to tool output, flagged as injection |
| [#47118](https://github.com/anthropics/claude-code/issues/47118) | Stale-closed | Root issue, `area:security` |
| [#58138](https://github.com/anthropics/claude-code/issues/58138) | Closed (dup of #47118) | Same phenomenon |
| [#85230](https://github.com/anthropics/claude-code/issues/85230) | Open | Background subagents lose MCP resource tools |
| [#79728](https://github.com/anthropics/claude-code/issues/79728) | Open | Explicit `tools:` allowlist collapses if MCP server unavailable at spawn (non-deterministic) |
| [#13254](https://github.com/anthropics/claude-code/issues/13254) | Closed (not planned) | "Background subagents cannot access MCP" — v2.0.60; **not reproducible on v2.1.270** |
| [#46228](https://github.com/anthropics/claude-code/issues/46228) | Closed (not planned) | Background subagents vs OAuth MCP servers — not tested (MOSAIC's github server uses a PAT header, not OAuth) |
| [#25200](https://github.com/anthropics/claude-code/issues/25200) | Closed (not planned) | Deferred MCP tools unusable in custom agents — not reproduced (deferred tools loaded via ToolSearch fine) |

---

## Reproduction Evidence

### Round 1 — 2026-09-12 (v2.1.269, Windows 11)

**CC-079:** 7-test matrix on `--agent` launch; full matrix in `Session-CC079-Verification-20260912.md`. Finding stands: `tools:` + `mcpServers:` string ref → zero MCP tools on `--agent`.

**CC-080/CC-081:** Primary received all servers' instructions at start. Three subagents reported no instructions — **superseded by Round 3**.

**Why Rounds 1–2 gave wrong conclusions — two separate confounds:**

1. **Tool access confusion:** Rounds 1–2 tested agents with `mcpServers:` string-refs but no `mcp__*` in `tools:`. The `tools:` allowlist filtered out MCP tools. The probes correctly observed zero MCP tools but attributed it to "MCP broken for subagents" rather than to the allowlist doing its job. Round 5 Test 2 confirms: `mcpServers:` connects the server (instructions appear), but `tools:` is the allowlist — no `mcp__*` entry means no MCP tools.

2. **Instruction visibility confusion (verified from transcripts):** All five Round 1/2 probes made **zero tool calls** — they inspected their context and answered in a single turn. The `mcp_instructions_delta` is only added after the first tool turn, so zero-tool-call probes never receive it. Their observation was accurate but incomplete. In 37/38 subagents that made at least one tool call, the delta appeared right after the first tool turn.

### Round 2 — 2026-09-13 (v2.1.269)

Test B (`mosaic-helper` with `tools:` built-ins only + `mcpServers: [github]`) got zero MCP tools. **Reinterpreted:** the `tools:` allowlist filtered the tools — `mcpServers:` connected the server but didn't grant tools past the allowlist. This is consistent behavior, confirmed again in Round 5 Test 2.

### Round 3 — 2026-09-13 (v2.1.270, Windows 11)

Setup: dependency-free stdio MCP canary server whose `instructions` field contains a unique string (`CANARY-INSTR-<label>-7731`) and exposes one `ping` tool. Headless parents (`claude -p --dangerously-skip-permissions --model sonnet`) each dispatched one probe subagent with explicit `run_in_background` true/false. Probes wrote reports to files; subagent transcripts (`subagents/agent-*.jsonl`) inspected directly. 19 runs.

| Probe | Frontmatter | Location | FG | BG |
|-------|------------|----------|----|----|
| A | inline `mcpServers`, no `tools:` | untrusted scratch | Inline **not loaded**; inherited parent server tools (deferred, via ToolSearch) | Same |
| B | `tools: Read, Write, mcp__inl__*` + inline | untrusted scratch | Inline **not loaded**; zero MCP tools | Same |
| C | `tools: Read, Write, mcp__ref__*` (`.mcp.json` server) | untrusted scratch | OK, direct | OK, direct |
| D | `tools: Read, Write` + inline | untrusted scratch | Zero MCP tools | Same |
| E | as C + `SubagentStart` hook `additionalContext` | untrusted scratch | Hook text present before first tool call | Same |
| E2 | as E, non-conflicting task ("use your guidance") | untrusted scratch | **Followed** hook guidance | **Distrusted** hook, used default |
| G | inline via `--agents` JSON (with / without `tools:`) | untrusted scratch | **OK** — inline tool present | **OK** |
| H | `tools: Read, Write, mcp__inl__*` + inline, project `.claude/agents/` | **trusted** (MOSAIC) | **OK** — inline tool direct; parent had only github/contact-user | **OK** |

Instruction delivery, all probes: `# MCP Server Instructions` arrived as an `mcp_instructions_delta` attachment immediately after the subagent's first tool call — including in B and D (zero MCP tools), and including every session-connected server (probe H received contact-user + github + inl blocks). No probe saw it in initial context. Every probe treated it as untrusted and did not act on it.

Confound noted: in `-p` with `--dangerously-skip-permissions`, the parent auto-enabled *all* `.mcp.json` servers regardless of `enabledMcpjsonServers`, so a "string reference to a server not enabled in the parent" test (probe F) was invalid and is not reported.

Not tested: HTTP/OAuth servers in background subagents; MCP resource tools; hook guidance with realistic (non-test) framing; `--agent` launch with inline `mcpServers:`.

### Round 4 — 2026-09-13 (v2.1.270, real agent)

Two background dispatches of the real `harness-issue-hunter` (`tools: ..., mcp__github__*`, no `mcpServers:`) from an interactive `mosaic-architect` session, read-only task: find ≤5 open anthropics/claude-code issues about "subagent MCP instructions". The prompt did not mention the server guidance.

| Variant | First turn | First MCP call | Guidance appeared | Second MCP call |
|---------|-----------|----------------|-------------------|-----------------|
| A — "Read index.md first" | `Read` **and** `search_issues` batched in parallel | Unbounded → 186,137 chars, exceeded tool-output limit, failed | After the first turn's batch | `fields: [number,title,state]`, `perPage: 10` → OK |
| B — "MCP call first" | `search_issues` | Same failure | After the first call | Same correction → OK |

Both: GitHub tools (~30–40+) available from the first turn in background mode. Both quoted the github section of `# MCP Server Instructions` verbatim, stated it arrived after the first turn, and explained which guidance they followed (pagination / minimal output) and which they skipped (`get_me`, judged unnecessary for a read-only query).

Side finding: [#84638](https://github.com/anthropics/claude-code/issues/84638) (open) — concurrent subagents with byte-identical inline `mcpServers:` configs share one MCP server process/session. Relevant if inline servers are used for parallel fan-out.

### Round 5 — 2026-09-13 (v2.1.270, Windows 11, live tests from mosaic-architect session)

Purpose: Verify the corrected understanding from Round 3 with targeted tests across both `--agent` and native subagent modes. Tests focused on whether `tools:` or `mcpServers:` grants MCP tools.

| # | Mode | `tools:` | `mcpServers:` | MCP tools? | MCP call? | Notes |
|---|------|----------|--------------|------------|-----------|-------|
| 1 | `--agent` | `Read, mcp__github__*` | *(none)* | ✅ yes | ✅ `get_me` succeeded | No `mcpServers:` needed — server connected via `.mcp.json` |
| 2 | `--agent` | `Read, Write` | `[github]` | ❌ zero | N/A | MCP instructions WERE delivered (both contact-user and github). Server connected, tools blocked by allowlist |
| 3 | `--agent` | `Read, Write, mcp__github__*` | `[github]` | ✅ 48 | ✅ `get_me` succeeded | Both fields present — tools granted by `tools:`, not by `mcpServers:` |
| 4 | subagent BG | `..., mcp__github__*` | *(none)* | ✅ 50+ | ✅ `get_me` succeeded | `harness-issue-hunter` dispatched via Agent tool |
| 5 | subagent BG | `R,W,E,B,Gl,Gr` | `[contact-user]` | ❌ zero | N/A | `implementation-review` — confirmed by session agent registry |

**Key finding:** Test 2 disproves the CC-079 framing "`mcpServers:` silently ignored." The server IS connected (MCP instructions from both servers were delivered to the agent). What doesn't happen: `mcpServers:` doesn't bypass the `tools:` allowlist. The `tools:` field is authoritative — it alone determines which MCP tools the agent can call.

**Additional discovery:** Agent definitions are cached at session start. Mid-session edits to an agent's frontmatter in `.claude/agents/` do NOT change what tools the Agent tool grants when dispatching that agent as a subagent. A session restart is required. (This is why initial subagent test attempts returned stale tool grants from the pre-edit frontmatter.)
