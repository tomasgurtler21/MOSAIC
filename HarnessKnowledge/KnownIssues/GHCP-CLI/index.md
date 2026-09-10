# GitHub Copilot CLI — Issue Index

> Last updated: 2026-09-10 (run 2)
> Latest platform version: v1.0.83 (released 2026-09-04)

## Active Issues (19)

| ID | Title | Type | Confidence | Impact | Workaround | Reproduced | Response |
|----|-------|------|------------|--------|------------|------------|----------|
| GC-001 | Memory-pressure watchdog force-compacts conversation at low context usage, can loop until OOM | Bug | Unverified | HIGH | Yes | No | Unevaluated |
| GC-002 | Repeated compaction recursively re-summarizes prior summaries, degrading durable context | Limitation | Likely | MEDIUM | Yes | No | Unevaluated |
| GC-003 | Workspace `.mcp.json` detected by `mcp list`/`mcp get` but not wired into actual agent session | Bug | Confirmed | HIGH | Yes | No | Unevaluated |
| GC-004 | Subagent launched with a since-deprecated/unsupported model crashes the entire primary session | Bug | Unverified | HIGH | No | No | Unevaluated |
| GC-005 | Sub-agents with full tool access silently return empty once MCP tool-schema volume crosses an undocumented budget | Bug | Unverified | HIGH | Partial | No | Unevaluated |
| GC-006 | Create-file tool returns invalid/null response and retries forever when content is too large | Bug | Unverified | HIGH | Yes | No | Unevaluated |
| GC-007 | OOM crash on long `--resume` sessions; crash dumps written into user's cwd | Bug | Likely | HIGH | Partial | No | Unevaluated |
| GC-008 | Session hangs after work appears complete; Escape recovery enters permanent "Cancelling" | Bug | Likely | HIGH | Partial | No | Unevaluated |
| GC-009 | Non-interactive (`-p`) sessions: tool-call approval silently and permanently revoked mid-session | Bug | Unverified | HIGH | No | No | Unevaluated |
| GC-010 | No approval required for any tool execution inside Docker sandboxes, even with `/yolo off` | Bug | Unverified | MEDIUM | No | No | Unevaluated |
| GC-011 | Steering message in `preToolUse` "ask" denial prompt is silently dropped | Bug | Unverified | MEDIUM | No | No | Unevaluated |
| GC-012 | `/allow-all` slash command does not suppress bash/shell tool execution prompts | Bug | Likely | MEDIUM | Partial | No | Unevaluated |
| GC-013 | Agent Skill `allowed-tools` frontmatter ignored in non-interactive (`-p`) mode | Bug | Unverified | HIGH | No | No | Unevaluated |
| GC-014 | `allowed_directories` in permissions-config.json never loaded | Bug | Unverified | HIGH | No | No | Unevaluated |
| GC-015 | `commandIdentifiers` with spaces still require approval | Bug | Unverified | MEDIUM | Partial | No | Unevaluated |
| GC-016 | `preToolUse` hook `deny` decision does not block tool execution | Bug | Unverified | HIGH | No | No | Unevaluated |
| GC-017 | `preToolUse` hook `ask` decision auto-approved by TUI since v1.0.53 | Bug | Unverified | HIGH | Partial | No | Unevaluated |
| GC-018 | `--allow-tool='shell(docker ps)'` pattern matching fails for non-git commands | Bug | Unverified | HIGH | Partial | No | Unevaluated |
| GC-019 | Non-interactive `--yolo` bypasses `disableBypassPermissionsMode` managed setting | Bug | Unverified | MEDIUM | No | No | Unevaluated |

## Resolved / Retired Issues (0)

> Resolved entries older than 3 months are automatically removed.

| ID | Title | Fixed In / Status | Resolution Date |
|----|-------|-------------------|-----------------|

---

## Quick Summary

*(Rewritten run 2, after a full re-verification pass re-checked every entry against the live GitHub tracker — issue-by-issue, body + all comments. Result: zero closures, zero reclassifications, zero confidence changes. The main finding was staleness — five entries (GC-005, GC-015, GC-016, GC-017, GC-018) crossed the 6-week no-activity threshold and are now newly flagged "Needs re-verification," with GC-018 the most severe case at ~5 months stale with zero maintainer engagement. No entry in this harness has yet received a maintainer acknowledgment, fix PR, or "by design" statement — every classification and confidence level here rests on reporter-provided evidence quality alone.)*

**Context/session-memory management (GC-001, GC-002, GC-007):** A process-level memory-pressure watchdog can force-compact conversations for reasons unrelated to context-window usage and loop until the process OOMs (GC-001); compaction itself is architecturally lossy and recursively re-summarizes prior summaries over a long session, degrading durable facts (GC-002, Limitation — by design, doc-confirmed); and long `--resume`'d sessions independently crash from a separate heap-memory leak that a raised `--max-old-space-size` does not prevent, dumping crash reports into the user's cwd (GC-007, still actively reported as of this run's re-check, same day as capture). Together these mean long-running MOSAIC sessions (interactive or headless-resumed) are at risk of both silent context degradation and outright process death, with no full upstream fix confirmed for either.

**Subagent invocation and MCP-tool delegation (GC-003, GC-004, GC-005):** Workspace-scoped `.mcp.json` MCP server configs are silently not wired into the running session despite being correctly discovered by introspection commands — corroborated by a second, independently-filed issue found during capture (GC-003); a subagent configured with a model that becomes unsupported mid-session can crash the entire primary orchestrator session rather than failing gracefully, and history shows this failure class has recurred across CLI versions despite a prior "fixed via fallback" claim (GC-004); and subagents with heavy MCP tool-schema load can silently return empty responses with zero error signal once an undocumented schema-size budget is crossed — now flagged for re-verification after 6+ weeks of silence (GC-005). All three defeat MOSAIC's assumption that subagent dispatch either succeeds visibly or fails with a detectable signal.

**Tool-execution and process stability (GC-006, GC-008):** Writing a single large file can send the create-file tool into an infinite retry loop with an uninformative error (GC-006), and sessions can hang irrecoverably after work appears complete, with Escape making things worse rather than better — part of a maintainer-unacknowledged cluster of related hang/concurrency issues stretching back to a previously "fixed" version that regressed (GC-008, Needs re-verification, high-priority reproduction candidate given severity) — both require external detection/recovery since the CLI provides no internal escape.

**Non-interactive (`-p`) permission enforcement breakdown (GC-009, GC-012, GC-013, GC-019):** This remains the most concentrated risk area for MOSAIC's headless Runner pipeline, and re-verification found no change to any of it. Long non-interactive sessions can have tool approval silently and permanently revoked mid-session (GC-009, self-closed by the reporter with zero maintainer resolution — still tracked active per the "don't trust closures" rule); the interactive `/allow-all` command doesn't reliably suppress shell prompts, now confirmed stale against the current version and due for re-check (GC-012); Skill `allowed-tools` frontmatter — a first-class MOSAIC mechanism — is honored interactively but ignored in `-p` mode, causing unrecoverable permission-denied retry loops, now flagged for re-verification after 8 weeks of silence (GC-013); and an enterprise `disableBypassPermissionsMode` managed setting is silently bypassed specifically on the non-interactive code path (GC-019). The pattern across all four: permission enforcement in GHCP CLI appears to behave inconsistently between interactive and non-interactive invocation, in ways that sometimes make automation *fail* (GC-009, GC-013) and sometimes make it *more permissive than intended* (GC-019).

**`preToolUse` hook verdict enforcement gaps (GC-011, GC-016, GC-017):** Three distinct symptoms all point at the same subsystem not reliably carrying a hook's permission verdict through to actual session behavior: `deny` verdicts don't block execution at all, now newly flagged Needs re-verification after 8 weeks of silence (GC-016); `ask` verdicts are auto-approved by the TUI without user interaction on a specific, precisely-bisected version range, also newly stale-flagged (GC-017); and free-text steering input typed into an `ask` denial prompt never reaches the agent (GC-011). None have maintainer acknowledgment, but the pattern is consistent enough to suggest a shared root cause in verdict normalization before tool dispatch — worth a combined re-check if any one of the three gets maintainer attention.

**Permission/allowlist config-matching engine gaps (GC-014, GC-015, GC-018):** Three separate permission-configuration surfaces each have their own matching failure: `allowed_directories` location-scoped config is never loaded at all (GC-014); `commandIdentifiers` containing a space fail to tokenize/match, now newly stale-flagged (GC-015); and the `--allow-tool='shell(<subcommand>)'` CLI flag pattern only works reliably for `git`, failing for other commands like `docker` — this is now the single most stale entry in the entire harness, ~5 months with zero maintainer engagement, and the highest-priority candidate for a direct MOSAIC reproduction attempt to resolve the long-standing uncertainty (GC-018). Verified as genuinely distinct root causes rather than duplicates, despite the thematic overlap.

**Sandbox-specific permission bypass (GC-010):** Currently narrow in scope (only the opt-in Windows Docker Sandboxes feature, which MOSAIC's Runner does not use by default) but would become a HIGH-impact concern if MOSAIC ever adopts container-based subagent isolation.

## Recommended Mitigations

- **Do not treat `preToolUse` hooks as a hard security boundary in the current version line.** `deny` (GC-016) and `ask` (GC-017, TUI-specific) verdicts have both been reported as silently non-enforcing, and re-verification found no fix or maintainer engagement on either. If a hard block is required, enforce it outside the CLI (OS/sandboxing layer) until these are confirmed fixed.
- **Verify non-interactive (`-p`) permission behavior directly in MOSAIC's actual Runner environment before relying on it.** Multiple reports (GC-009, GC-012, GC-013, GC-019) show `-p` mode's permission handling diverging from interactive mode in both directions (unexpectedly restrictive and unexpectedly permissive) — don't assume `--allow-all`/`--allow-tool`/Skill `allowed-tools`/managed settings behave as documented in headless invocations without testing.
- **Bound headless session/process lifetimes.** Restarting long-running or `--resume`'d sessions periodically (rather than keeping one process alive indefinitely) mitigates both the memory-pressure compaction loop (GC-001) and the independent OOM leak on `--resume` (GC-007, still actively being reported as of this run).
- **Externalize durable facts to on-disk artifacts early and often** (e.g., a maintained `HANDOFF.md`-style running log) rather than relying on conversational context surviving many compaction cycles — compaction is architecturally lossy across long sessions (GC-002), and MOSAIC's blackboard (`Orchestration.md`) pattern already encourages this for exactly this reason.
- **Instruct subagents to split very large single-file artifact writes into multiple smaller files** as a preventive measure against the create-file infinite-retry bug (GC-006) until an upstream fix or documented size limit exists.
- **Prefer whole-command/bare permission identifiers over narrowly-scoped subcommand patterns** when configuring pre-approved tools for headless stages, since subcommand-level matching has multiple independent failure modes across different config surfaces (GC-014, GC-015, GC-018) — this trades some permission precision for reliability until the matching engine gaps are resolved. GC-018 in particular (~5 months stale, `git`-only subcommand matching) is worth a direct reproduction test given how long it's gone unconfirmed.
- **If a MOSAIC deployment relies on a committed `.mcp.json` for subagent MCP tools, defensively pass `--additional-mcp-config @.mcp.json` explicitly on every `copilot` invocation** rather than trusting workspace auto-discovery (GC-003), since the auto-wiring path has an active, reproducible regression corroborated by a second independently-filed report.
- **Treat "no subagent response" as ambiguous, not necessarily success or a clean no-op**, especially in deployments with many configured MCP servers/tools — GC-005 shows this can be a silent, undetectable failure mode with no error signal from the harness, and it's now stale enough (6+ weeks) to warrant a direct reproduction attempt given the severity.
- **Wrap headless/`-p` invocations with an external wall-clock timeout and kill/retry logic.** GC-008's unrecoverable "Thinking → Cancelling" hang has no internal escape, and GC-009's silent permission revocation can strand a stage indefinitely with no forward progress.
