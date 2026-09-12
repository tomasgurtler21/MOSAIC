# Engineering Blocker: No Correct MCP Frontmatter Configuration Exists

> **Status:** Active blocker (no upstream fix available)
> **Date:** 2026-09-12
> **Claude Code version:** v2.1.269
> **Related issues:** CC-079, CC-080, CC-081
> **Affects:** Any agent that uses MCP tools

---

## Summary

It is impossible to write Claude Code agent frontmatter that produces correct MCP behavior across all three invocation modes (primary agent, `--agent` launch, and subagent dispatch via the Agent tool). Three independent bugs interact to create a situation where every configuration is broken in at least one mode.

---

## The Three Invocation Modes

| Mode | How it runs | Example |
|------|------------|---------|
| **Primary** | Top-level agent in a `claude` session (no `--agent` flag) | The default assistant |
| **`--agent` launch** | `claude --agent <name>` or `claude --agent <name> -p "prompt"` | Running a specific agent from the CLI |
| **Subagent dispatch** | Another agent calls the `Agent` tool to spawn this agent | Orchestrator dispatching a worker |

Most MOSAIC agents run in all three modes depending on context. An agent like `harness-issue-hunter` might be launched directly via `--agent` by the user, or dispatched as a subagent by the orchestrator. The frontmatter must work in both cases.

---

## The Three Bugs

### CC-079: `mcpServers:` silently ignored on `--agent` launch when `tools:` is present

When an agent file has both `tools:` and `mcpServers:` in its frontmatter, the `--agent` launcher ignores `mcpServers:` entirely. The `tools:` allowlist becomes the sole authority, and since it only lists built-in tool names, zero MCP tools are granted. No error, no warning.

```yaml
# BROKEN on --agent launch:
tools: Read, Bash, Glob, Grep
mcpServers:
  - github
# Result: Read, Bash, Glob, Grep. Zero github tools. mcpServers silently dropped.
```

This does NOT affect subagent dispatch (where `mcpServers:` works even alongside `tools:`), nor primary agents without `--agent`.

### CC-080: Primary agents receive MCP instructions for ALL servers regardless of tool scope

The `# MCP Server Instructions` system prompt block is injected into primary and `--agent` agents based on which MCP servers are connected to the session — not based on the agent's `tools:` scope. An agent with `tools: Read, Write` that has no MCP tools at all still gets the full instruction block for every connected MCP server.

This wastes context tokens and can mislead the model into discussing capabilities it doesn't have.

### CC-081: Subagents receive NO MCP instructions regardless of tool scope

Subagents dispatched via the Agent tool receive zero `# MCP Server Instructions` blocks — even when they have full access to MCP tools via `mcp__<server>__*` in their allowlist. The harness simply does not inject MCP instructions into subagent contexts.

This means subagents using MCP tools operate without any server-specific usage guidance (tool selection hints, pagination advice, authentication patterns).

---

## Interaction Matrix

### Tool Access

The question: does the agent actually get the MCP tools it needs?

| Frontmatter pattern | `--agent` | Subagent | Primary |
|---------------------|-----------|----------|---------|
| `tools:` + `mcpServers:` | **BROKEN** — mcpServers silently ignored (CC-079) | OK | OK |
| `tools:` with `mcp__<server>__*`, no `mcpServers:` | OK | OK | OK |
| `mcpServers:` only, no `tools:` | OK (full default + MCP) | OK | OK |

**Best option for tool access:** Use `mcp__<server>__*` wildcards directly in the `tools:` field. Drop `mcpServers:` entirely. This works in all three modes.

```yaml
# Recommended pattern for tool access:
tools: Read, Write, Edit, Glob, Grep, mcp__github__*, mcp__contact-user__*
```

### MCP Instruction Routing

The question: does the agent receive the MCP server's usage instructions in its system prompt?

| Mode | Receives instructions? | Scoped to agent's tools? |
|------|----------------------|--------------------------|
| Primary | YES | NO — receives ALL servers' instructions |
| `--agent` | YES | NO — receives ALL servers' instructions |
| Subagent | **NO** | N/A |

**No fix exists.** There is no frontmatter field, value, or combination that controls instruction routing. The harness hardcodes the behavior: primary/`--agent` always gets all instructions; subagents never get any.

### Combined View

| Concern | Primary / `--agent` | Subagent |
|---------|--------------------:|:---------|
| Tool access | Fixable (`mcp__*` in `tools:`) | Fixable (`mcp__*` in `tools:`) |
| Correct instructions | **BROKEN** — gets too many | **BROKEN** — gets none |

---

## Impact on MOSAIC

### Wasted context tokens (CC-080)

Every primary agent and `--agent` launch pays the token cost for instruction blocks from MCP servers it may not use. With 5 MCP servers connected (github, contact-user, Gmail, Google Calendar, Google Drive), this adds hundreds of tokens of irrelevant guidance to every agent's context.

### Uninformed MCP tool usage (CC-081)

Subagents dispatched with MCP tools lack guidance that exists and would help. The github MCP server's instructions include:

- "Always call `get_me` first to understand current user permissions and context"
- "Use `search_*` tools for targeted queries with specific criteria"
- "Use `minimal_output` parameter set to true if the full information is not needed"
- "Use pagination with batches of 5-10 items"
- PR review workflow (create pending review, add comments, then submit)

None of this reaches subagents like `harness-issue-hunter` that actually need it.

### No single-source frontmatter

MOSAIC's deployment system (`mosaic-deploy`) generates agent files from canonical source templates. The promise is one source file, many deployments. This blocker means the source file's frontmatter cannot be correct for all invocation modes — requiring either runtime workarounds or body-text duplication of harness-managed content.

---

## Workarounds

### For tool access (solves CC-079)

Use `mcp__<server>__*` wildcards in the `tools:` field. Remove `mcpServers:` entirely.

```yaml
# Before (broken on --agent):
tools: Read, Write, Edit, Bash, Glob, Grep
mcpServers:
  - github
  - contact-user

# After (works everywhere):
tools: Read, Write, Edit, Bash, Glob, Grep, mcp__github__*, mcp__contact-user__*
```

### For missing instructions (works around CC-081)

Embed condensed MCP usage instructions directly in the agent's body text. Since the harness won't deliver them to subagents, the agent definition must carry its own copy.

This is an anti-pattern (duplicating harness-managed content into user-managed files), but it's the only option. The duplication means these instructions won't auto-update when the MCP server changes its guidance — they become a maintenance burden.

### For excess instructions (no workaround for CC-080)

No mitigation exists. Minimizing the number of MCP servers in `.mcp.json` reduces the surface, but this trades functionality for token efficiency.

---

## Upstream References

| Issue | Status | Description |
|-------|--------|-------------|
| [#85307](https://github.com/anthropics/claude-code/issues/85307) | Open | Consolidated report: instruction routing is inverted |
| [#75283](https://github.com/anthropics/claude-code/issues/75283) | Stale-closed | Instructions appended to Bash output, flagged as injection |
| [#47118](https://github.com/anthropics/claude-code/issues/47118) | Stale-closed | Root issue, `area:security` label |
| [#29655](https://github.com/anthropics/claude-code/issues/29655) | Stale-closed | Missing-instructions half |
| [#58138](https://github.com/anthropics/claude-code/issues/58138) | Closed (dup of #47118) | Same phenomenon |

Zero maintainer engagement on any of these issues. All closures are stale-bot, not human decisions.

---

## Reproduction Evidence

Tested 2026-09-12 on Claude Code v2.1.269, Windows 11.

**CC-079 (tool access):** 7-test matrix covering all combinations of `tools:`, `mcpServers:`, and MCP wildcard syntax. Full matrix in `Session-CC079-Verification-20260912.md`.

**CC-080/CC-081 (instruction routing):** Three subagents dispatched via Agent tool from a `mosaic-architect` primary session:

1. `claude` type (Tools: *, including mcp__github__*) — **no** MCP instructions in context
2. `claude` type (same) — **no** MCP instructions in context  
3. `anthropic-subagent-creator` (tools: Read,Write,Edit,Glob,Grep, no MCP tools) — **no** MCP instructions in context

Primary agent (mosaic-architect, no MCP tools in allowlist) — **full** MCP instruction blocks for github and contact-user present in context.
