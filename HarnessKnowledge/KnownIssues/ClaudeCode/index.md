# Claude Code — Issue Index

> Last updated: 2026-09-09 (run 10)
> Latest platform version: v2.1.267 (released 2026-09-09)

## Active Issues (44)

| ID | Title | Type | Confidence | Impact | Workaround | Reproduced | Response |
|----|-------|------|------------|--------|------------|------------|----------|
| CC-001 | Foreground Agent dispatch phantom-cancelled with fake interrupt message | Bug | Likely | HIGH | Yes | No | Unevaluated |
| CC-002 | Background agents hang indefinitely on unanswered permission prompt | Bug | Unverified | HIGH | No | No | Unevaluated |
| CC-003 | Subagent killed by usage limit falsely reported "completed" | Bug | Likely | HIGH | No | No | Unevaluated |
| CC-005 | `PostToolUse` hook doesn't fire for `Agent` tool completions | Bug | Likely | HIGH | No | No | Unevaluated |
| CC-006 | Compaction can preserve fabricated "system message" concealment instructions | Bug | Confirmed | HIGH | No | No | Unevaluated |
| CC-007 | Mid-conversation behavioral rules dropped after compaction | Bug | Likely | HIGH | Yes | No | Unevaluated |
| CC-008 | One malformed MCP tool schema drops ALL tools from that server | Bug | Confirmed | HIGH | No | No | Unevaluated |
| CC-009 | Custom subagent `tools: Bash` frontmatter silently never grants Bash | Bug | Unverified | HIGH | No | No | Unevaluated |
| CC-010 | `PreToolUse` `defer` decision permanently strands subagent Bash call | Bug | Confirmed | HIGH | No | No | Unevaluated |
| CC-011 | `disallowedTools` not enforced on default-`subagent_type` nested dispatch | Limitation | Confirmed | HIGH | Partial | No | Unevaluated |
| CC-012 | Stale `ToolSearch` reference permanently bricks session (400 error) | Bug | Confirmed | HIGH | No | No | Unevaluated |
| CC-013 | `permissions.allow`/`bypassPermissions` intermittently ignored for subagents | Bug | Confirmed | HIGH | No | No | Unevaluated |
| CC-014 | `PreToolUse` `ask` decision silently not enforced | Bug | Confirmed | HIGH | No | No | Unevaluated |
| CC-015 | Killed/crashed `PreToolUse` hook fails OPEN instead of closed | Bug | Confirmed | HIGH | Partial | No | Unevaluated |
| CC-016 | Background subagents denied Write/Bash regardless of permission mode | Bug | Confirmed | HIGH | No | No | Unevaluated |
| CC-018 | Self-backgrounding Bash under `run_in_background` false-completes, orphans process | Bug | Unverified | HIGH | No | No | Unevaluated |
| CC-019 | Background Bash task killed ~17-20s after arming as turn's last call | Bug | Unverified | HIGH | No | No | Unevaluated |
| CC-020 | LSP client never sends `didChange` after Edit-tool write | Quirk | Unverified | MEDIUM | No | No | Unevaluated |
| CC-022 | Mid-session `/model` switch doesn't apply to running session | Bug | Confirmed | HIGH | Partial | No | Unevaluated |
| CC-023 | Subagent Bash `cwd` reset can land in sibling subagent's worktree | Bug | Unverified | HIGH | Partial | No | Unevaluated |
| CC-024 | `SessionStart` env file grows unboundedly, eventually wedges Bash tool | Bug | Likely | HIGH | No | No | Unevaluated |
| CC-025 | Plugin-native `PreToolUse` hooks unenforced; plugin `deny` unenforced | Bug | Unverified | HIGH | Partial | No | Unevaluated |
| CC-026 | Second `/compact` in one process corrupts transcript, breaks `/rewind` | Bug | Confirmed | HIGH | Partial | No | Unevaluated |
| CC-027 | Leading whitespace before slash command sends it as plain text | Bug | Confirmed | MEDIUM | Yes | No | Unevaluated |
| CC-028 | `--continue` is directory-scoped, can co-write into another live session | Bug | Unverified | HIGH | Partial | No | Unevaluated |
| CC-029 | Manual `/compact` can silently no-op on large conversations | Bug | Confirmed | HIGH | Yes | No | Unevaluated |
| CC-030 | Skill/command positional arg substitution off-by-one; `$2`+ never works | Bug | Unverified | HIGH | Partial | No | Unevaluated |
| CC-031 | Explicitly-named `subagent_type` dispatch can ignore subagent's own definition | Bug | Unverified | HIGH | No | No | Unevaluated |
| CC-032 | Subagent plan-mode approval dialog can show PARENT session's plan | Bug | Confirmed | HIGH | No | No | Unevaluated |
| CC-033 | Subagents receive full auto-memory index and skill listing (no opt-out) | Bug | Unverified | MEDIUM | No | No | Unevaluated |
| CC-034 | `CLAUDE.md`/path-scoped rules in `--add-dir` dirs silently skipped | Bug | Likely | MEDIUM | Partial | No | Unevaluated |
| CC-035 | Project skills beyond undocumented per-session cap become non-invocable | Bug | Likely | HIGH | Partial | No | Unevaluated |
| CC-036 | `CLAUDE.md` above git repo root never loaded into repo's own worktrees | Bug | Unverified | HIGH | Partial | No | Unevaluated |
| CC-037 | Hard-coded plan-mode notice text injected at prompt layer, invisible in transcript | Quirk | Confirmed | MEDIUM | No | No | Unevaluated |
| CC-038 | Every Bash dispatch in large/resumed session can freeze process ~80-90s | Bug | Likely | HIGH | Partial | No | Unevaluated |
| CC-039 | `.claude/settings.json` non-atomic write — concurrent sessions can tear/clobber | Bug | Unverified | HIGH | Partial | No | Unevaluated |
| CC-040 | Multi-iteration turns sum token usage across iterations, inflate 2x-6x | Bug | Likely | HIGH | Partial | No | Unevaluated |
| CC-041 | `.claude/settings.json` in ancestor directory silently never loaded | Bug | Likely | HIGH | Partial | No | Unevaluated |
| CC-042 | `/rewind` can fail to restore never-git-tracked newly-created file | Bug | Unverified | MEDIUM | Partial | No | Unevaluated |
| CC-043 | statusLine refresh has no debouncing — slow command spawns unbounded copies | Bug | Confirmed | MEDIUM | Partial | No | Unevaluated |
| CC-044 | Duplicated base64 image content + unclamped render cost hard-freezes at 100% CPU | Bug | Confirmed | HIGH | Partial | No | Unevaluated |
| CC-046 | Mid-session-created `.claude/settings.json` hooks never arm until restart | Bug | Confirmed | HIGH | Partial | No | Unevaluated |
| CC-047 | One malformed hook entry silently kills all hooks for that event type | Bug | Confirmed | HIGH | Partial | No | Unevaluated |
| CC-048 | `WebFetch` `prompt` param ignored for pre-approved docs domains under ~100KB | Limitation | Likely | MEDIUM | Partial | No | Unevaluated |

## Resolved / Retired Issues (0)

> Resolved entries older than 3 months are automatically removed.

No resolved issues currently tracked.

---

## Quick Summary

**Subagent dispatch, isolation & scoping failures** — Several distinct gaps in how subagents are dispatched and isolated from their parent/siblings: `disallowedTools` doesn't cascade to nested default-typed dispatches (CC-011), an explicitly-named `subagent_type` dispatch may ignore the subagent's own definition entirely and inherit the dispatcher's (CC-031), subagents inherit the full auto-memory/skill index with no opt-out (CC-033), a subagent's Bash `cwd` can land in a sibling's worktree (CC-023), and a subagent's plan-mode approval dialog can show the parent's plan (CC-032).

**Permission and hook enforcement gaps** — A cluster of issues where configured permission/hook enforcement is silently not honored: `permissions.allow`/`bypassPermissions` ignored for subagents (CC-013), `PreToolUse` `ask` silently not enforced (CC-014), a crashed `PreToolUse` hook fails open (CC-015), background subagents denied Write/Bash regardless of mode (CC-016), plugin-sourced hooks unenforced (CC-025), a `defer` decision strands calls that then falsely report success (CC-010), and a single malformed hook entry kills all hooks for its event type persistently (CC-047).

**Context compaction and conversation-stability hazards** — The most severe cluster: compaction can preserve model-fabricated instructions to conceal actions (CC-006), mid-conversation behavioral rules are dropped post-compaction even when the summary retains the text (CC-007), manual `/compact` can silently no-op while reporting success (CC-029), a second `/compact` in one process corrupts the transcript and breaks `/rewind` (CC-026), and multi-iteration turns inflate measured token usage 2x-6x, triggering premature compaction or false session-ending errors (CC-040).

**Settings/config loading and file-integrity gaps** — `.claude/settings.json` is written non-atomically and can tear under concurrent sessions (CC-039), ancestor-directory settings are silently never loaded (CC-041), settings created mid-session never arm hooks until restart (CC-046), and `--add-dir`-added directories silently skip their `CLAUDE.md`/path-scoped rules (CC-034); a `CLAUDE.md` above a repo's root can also never load into that repo's own worktrees (CC-036).

**Session/process reliability and resource exhaustion** — A stale `ToolSearch` reference permanently bricks a session (CC-012), every Bash dispatch in a large/resumed session can freeze the whole process for ~80-90s (CC-038), duplicated base64 image content plus unclamped render cost can hard-freeze at 100% CPU (CC-044), and an undebounced statusLine refresh can spawn unbounded concurrent processes (CC-043).

**Task/agent status misreporting** — Multiple ways a subagent's true failure state is hidden: usage-limit kills reported as "completed" (CC-003), phantom-cancelled foreground dispatches injected with a fake user-interrupt message (CC-001), and background agents hanging indefinitely on unanswered prompts with no watchdog (CC-002).

**By-design limitations MOSAIC must permanently adapt to** — CC-011 (`disallowedTools` scoping) and CC-048 (`WebFetch` prompt ignored for small pre-approved docs) are both maintainer-confirmed intentional but still carry real operational risk (a security scoping gap, and a hidden token-cost blowout, respectively).

## Recommended Mitigations

- Put durable behavioral rules in `CLAUDE.md` rather than relying on in-conversation instructions, which are unreliable across compaction (CC-007).
- Use a hook that verifies the compaction boundary was actually written after `/compact`, since success can be silently false (CC-029).
- Never trust a subagent's reported "completed" status alone as proof of real, non-truncated work — independently validate output content where the result matters (CC-003, CC-001).
- Avoid mid-session model switches when deferred (`ToolSearch`-loaded) tools are in play — this is a known session-bricking trigger (CC-012).
- Do not rely on `disallowedTools` alone as a security boundary for nested/further subagent dispatch — verify tool scoping explicitly, especially for explicitly-named `subagent_type` dispatches (CC-011, CC-031).
- Avoid launching multiple concurrent Claude Code sessions in the same working directory with `--continue`, and avoid concurrent writes to a shared `.claude/settings.json` — both risk file/transcript corruption under concurrency (CC-028, CC-039).
- Be skeptical of on-screen context-usage figures during multi-iteration (advisor/sub-call) turns — displayed usage can be inflated 2x-6x, causing premature compaction (CC-040).
- Keep configured statusline commands fast, and keep project skill counts below the undocumented per-session cap, to avoid resource exhaustion and silent skill-invocation failures (CC-043, CC-035).

**Needs re-verification on current version:** CC-001 and CC-002 (closed by stale-bot, not fixed) should be re-checked against v2.1.267 before being fully trusted as still-present.
