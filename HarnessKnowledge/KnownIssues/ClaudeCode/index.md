# Claude Code — Issue Index

> Last updated: 2026-09-09 (run 11)
> Latest platform version: v2.1.267 (released 2026-09-09)

## Active Issues (44)

| ID | Title | Type | Confidence | Impact | Workaround | Reproduced | Response |
|----|-------|------|------------|--------|------------|------------|----------|
| CC-001 | Background task notifications phantom-cancel pending calls with fake "user declined" sentinel | Bug | Likely | HIGH | Yes | No | Unevaluated |
| CC-002 | Background/scheduled agents hang indefinitely on unanswered permission prompt, fail silently | Bug | Likely | HIGH | Yes | No | Unevaluated |
| CC-003 | Subagent killed by usage limit falsely reported "completed" | Bug | Likely | HIGH | Partial | No | Unevaluated |
| CC-005 | `PostToolUse` hook doesn't fire for `Agent` tool completions | Bug | Likely | HIGH | No | No | Unevaluated |
| CC-006 | Compaction can preserve fabricated "system message" concealment instructions | Bug | Confirmed | HIGH | Partial | No | Unevaluated |
| CC-007 | Mid-conversation behavioral rules dropped after compaction | Bug | Likely | HIGH | Yes | No | Unevaluated |
| CC-008 | One malformed MCP tool schema drops ALL tools from that server | Bug | Confirmed | HIGH | Yes | No | Unevaluated |
| CC-009 | Custom subagent `tools: Bash` frontmatter silently never grants Bash | Bug | Unverified | HIGH | Yes | No | Unevaluated |
| CC-010 | `PreToolUse` `defer` decision permanently strands subagent Bash call | Bug | Confirmed | HIGH | Partial | No | Unevaluated |
| CC-011 | `disallowedTools` not enforced on default-`subagent_type` nested dispatch | Limitation | Confirmed | HIGH | Partial | No | Unevaluated |
| CC-012 | Stale `ToolSearch` reference permanently bricks session (400 error) | Bug | Confirmed | HIGH | Yes | No | Unevaluated |
| CC-013 | `permissions.allow`/`bypassPermissions` intermittently ignored for subagents | Bug | Unverified | HIGH | No | No | Unevaluated |
| CC-014 | `PreToolUse` `ask` decision silently not enforced | Bug | Likely | HIGH | Yes | No | Unevaluated |
| CC-015 | Killed/crashed `PreToolUse` hook fails OPEN instead of closed | Bug | Unverified | HIGH | Partial | No | Unevaluated |
| CC-016 | Background subagents denied Write/Bash regardless of permission mode | Bug | Unverified | HIGH | Yes | No | Unevaluated |
| CC-018 | Self-backgrounding Bash under `run_in_background` false-completes, orphans process | Bug | Unverified | HIGH | Partial | No | Unevaluated |
| CC-019 | Background Bash task killed ~17-20s after arming as turn's last call | Bug | Unverified | HIGH | Yes | No | Unevaluated |
| CC-020 | Interactive-session LSP client never sends `didChange` after Edit-tool write | Bug | Unverified | MEDIUM | Partial | No | Unevaluated |
| CC-022 | Mid-session `/model` switch doesn't apply to running session | Bug | Confirmed | HIGH | Partial | No | Unevaluated |
| CC-023 | Subagent Bash `cwd` reset can land in sibling subagent's worktree | Bug | Unverified | HIGH | Partial | No | Unevaluated |
| CC-024 | `SessionStart` env file grows unboundedly, eventually wedges Bash tool | Bug | Likely | HIGH | Yes | No | Unevaluated |
| CC-025 | Plugin-native `PreToolUse` hooks unenforced; plugin `deny` unenforced | Bug | Unverified | HIGH | Partial | No | Unevaluated |
| CC-026 | Second `/compact` in one process corrupts transcript, breaks `/rewind` | Bug | Unverified | HIGH | Partial | No | Unevaluated |
| CC-027 | Leading whitespace before slash command sends it as plain text | Bug | Confirmed | MEDIUM | Yes | No | Unevaluated |
| CC-028 | `--continue` is directory-scoped, can co-write into another live session | Bug | Confirmed | HIGH | Partial | No | Unevaluated |
| CC-029 | Manual `/compact` can silently no-op on large conversations | Bug | Unverified | HIGH | Yes | No | Unevaluated |
| CC-030 | Skill/command positional arg substitution off-by-one; `$2`+ never works | Bug/Limitation | Confirmed | HIGH | Yes | No | Unevaluated |
| CC-031 | Explicitly-named `subagent_type` dispatch can ignore subagent's own definition | Bug | Unverified | HIGH | No | No | Unevaluated |
| CC-032 | Subagent plan-mode approval dialog can show PARENT session's plan | Bug | Confirmed | HIGH | Partial | No | Unevaluated |
| CC-033 | Subagents receive full auto-memory index and skill listing (no opt-out) | Bug | Likely | MEDIUM | No | No | Unevaluated |
| CC-034 | `CLAUDE.md`/path-scoped rules in `--add-dir` dirs silently skipped | Bug | Likely | MEDIUM | Partial | No | Unevaluated |
| CC-035 | Project skills beyond undocumented per-session cap become non-invocable | Bug | Likely | HIGH | Partial | No | Unevaluated |
| CC-036 | `CLAUDE.md` above git repo root never loaded into repo's own worktrees | Bug | Unverified | HIGH | Partial | No | Unevaluated |
| CC-037 | Hard-coded plan-mode notice text injected at prompt layer, invisible in transcript | Bug | Confirmed | MEDIUM | Partial | No | Unevaluated |
| CC-038 | Every Bash dispatch in large/resumed session can freeze process ~80-90s | Bug | Likely | HIGH | Partial | No | Unevaluated |
| CC-039 | `.claude/settings.json` non-atomic write — concurrent sessions can tear/clobber | Bug | Likely | HIGH | Partial | No | Unevaluated |
| CC-040 | Multi-iteration turns sum token usage across iterations, inflate 2x-6x | Bug | Likely | HIGH | Partial | No | Unevaluated |
| CC-041 | `.claude/settings.json` in ancestor directory silently never loaded | Bug | Unverified | HIGH | Yes | No | Unevaluated |
| CC-042 | `/rewind` can fail to restore never-git-tracked newly-created file | Bug | Unverified | MEDIUM | Partial | No | Unevaluated |
| CC-043 | statusLine refresh has no debouncing — slow command spawns unbounded copies | Bug | Confirmed | MEDIUM | Yes | No | Unevaluated |
| CC-044 | Duplicated base64 image content + unclamped render cost hard-freezes at 100% CPU | Bug | Confirmed | HIGH | Partial | No | Unevaluated |
| CC-046 | Mid-session-created `.claude/settings.json` hooks never arm until restart | Bug | Confirmed | HIGH | Yes | No | Unevaluated |
| CC-047 | One malformed hook entry silently kills all hooks for that event type | Bug | Confirmed | HIGH | Yes | No | Unevaluated |
| CC-048 | `WebFetch` `prompt` param ignored for pre-approved docs domains under ~100KB | Limitation | Confirmed | MEDIUM | Yes | No | Unevaluated |

## Resolved / Retired Issues (0)

> Resolved entries older than 3 months are automatically removed.

No resolved issues currently tracked.

---

## Quick Summary

*(Rewritten run 11, after a full backfill pass re-verified every entry against the live GitHub tracker — several entries changed confidence or classification from the original reconstructed pass; see individual entries for details.)*

**Subagent dispatch, isolation & scoping failures** — Several distinct gaps in how subagents are dispatched and isolated from their parent/siblings: `disallowedTools` doesn't cascade to nested default-typed dispatches (CC-011, maintainer-confirmed by-design), an explicitly-named `subagent_type` dispatch may ignore the subagent's own definition entirely and inherit the dispatcher's (CC-031, single-reporter, high-priority for corroboration), subagents inherit the full auto-memory/skill index with no opt-out — now corroborated by 3 independent HTTP-proxy reproductions (CC-033, upgraded to Likely), a subagent's Bash `cwd` can land in a sibling's worktree with no way to pin it (CC-023, directly threatens MOSAIC's own worktree fan-out), and a subagent's plan-mode approval dialog can show the parent's plan (CC-032, confirmed cross-OS).

**Permission and hook enforcement gaps** — A cluster of issues where configured permission/hook enforcement is silently not honored, several of which turned out narrower than first reconstructed: `permissions.allow`/`bypassPermissions` for subagents (CC-013, downgraded to Unverified — broad claim couldn't be reproduced, real gap is narrower/intermittent for nested subagents in long sessions), `PreToolUse` `ask` silently not enforced in interactive auto-mode specifically (CC-014, downgraded to Likely), a killed/crashed `PreToolUse` hook fails open (CC-015, downgraded to Unverified — its supposed fix PR actually patches an unrelated third-party plugin), background subagents denied Write/Bash regardless of mode (CC-016, downgraded to Unverified), plugin-sourced hooks unenforced (CC-025), a `defer` decision strands calls that then falsely report success — maintainer engaged (CC-010), and a single malformed hook entry kills all hooks for its event type persistently, maintainer-reproduced (CC-047).

**Context compaction and conversation-stability hazards** — Still the most severe cluster, headlined by a maintainer-confirmed root cause: compaction can preserve model-fabricated instructions to conceal actions, with a documented occurrence of a live API call made off the fabrication (CC-006, Confirmed). Also: mid-conversation behavioral rules are dropped post-compaction even when the summary retains the text (CC-007), multi-iteration turns inflate measured token usage 2x-6x with the exact root-cause code identified (CC-040, Likely). Two entries were downgraded after re-verification found their corroboration was retracted or absent: manual `/compact` silently no-op (CC-029, downgraded to Unverified) and a second `/compact` corrupting the transcript/`/rewind` (CC-026, downgraded to Unverified) — both single-reporter with no maintainer response, kept active given severity but no longer overstated as Confirmed.

**Settings/config loading and file-integrity gaps** — `.claude/settings.json` is written non-atomically and can tear under concurrent sessions (CC-039, Likely), settings created mid-session never arm hooks until restart, maintainer-reproduced (CC-046), and `--add-dir`-added directories silently skip their `CLAUDE.md`/path-scoped rules (CC-034, Likely, second independent hook-verified report). Ancestor-directory settings never loading (CC-041) was downgraded to Unverified after independently re-confirming its "fix PR" actually targets an unrelated plugin config loader, not the core resolution path. A `CLAUDE.md` above a repo's root never loading into that repo's own worktrees (CC-036) is single-reporter but an exceptionally rigorous fixture-matrix test, and hits MOSAIC's own worktree pattern directly.

**Session/process reliability and resource exhaustion** — A stale `ToolSearch` reference permanently bricks a session, now confirmed across three independent trigger mechanisms (CC-012), every Bash dispatch in a large/resumed session can freeze the whole process for ~80-90s with kernel-level forensic evidence (CC-038), duplicated base64 image content plus unclamped render cost can hard-freeze at 100% CPU, maintainer-confirmed (CC-044), and an undebounced statusLine refresh can spawn unbounded concurrent processes, maintainer-reproduced (CC-043).

**Task/agent status misreporting** — Multiple ways a subagent's true failure state is hidden: usage-limit kills reported as "completed" (CC-003), background/scheduled agents hanging indefinitely or failing silently on an unanswered permission prompt — now corroborated across macOS/Windows/Linux via three separate reports (CC-002, upgraded to Likely), and background task notifications phantom-cancelling pending tool/permission calls with a fake "user declined" sentinel, corroborated cross-platform with decompiled-binary evidence (CC-001) — note this is NOT stale/abandoned as originally recorded; it remains an open, actively-corroborated issue.

**By-design limitations MOSAIC must permanently adapt to** — CC-011 (`disallowedTools` scoping) and CC-048 (`WebFetch` prompt ignored for small pre-approved docs, now Confirmed with the maintainer's exact 100KB/25K-token cap mechanism explained) are both maintainer-confirmed intentional but still carry real operational risk (a security scoping gap, and a hidden token-cost blowout, respectively). CC-030 also split on re-investigation: the `$2`+ non-substitution defect stays an Unverified Bug, while the literal `$<digit>`-text corruption is a separate, maintainer-confirmed-by-design Limitation with a working backslash-escape workaround.

## Recommended Mitigations

- Put durable behavioral rules in `CLAUDE.md` rather than relying on in-conversation instructions, which are unreliable across compaction (CC-007).
- Never trust a subagent's reported "completed" status alone as proof of real, non-truncated work — independently validate output content where the result matters (CC-003, CC-001, CC-002).
- Avoid mid-session model switches when deferred (`ToolSearch`-loaded) tools are in play — this is a known session-bricking trigger, now confirmed via three separate mechanisms (CC-012).
- Do not rely on `disallowedTools` alone as a security boundary for nested/further subagent dispatch — verify tool scoping explicitly, especially for explicitly-named `subagent_type` dispatches (CC-011, CC-031).
- Pre-approve every tool a background/scheduled subagent may need — background dispatch can hang or fail silently on any unanswered prompt, and Write/Bash access for background subagents is unreliable regardless of permission mode (CC-002, CC-016).
- Avoid launching multiple concurrent Claude Code sessions in the same working directory with `--continue` (now Confirmed — corrupts the shared task store, not just the transcript), and avoid concurrent writes to a shared `.claude/settings.json` (CC-028, CC-039).
- Have each subagent explicitly `cd` to its intended worktree at the start of every Bash call rather than trusting inherited `cwd` — a subagent's cwd can land in a sibling's worktree (CC-023), and don't assume a `CLAUDE.md` placed above a repo root reaches that repo's own worktrees (CC-036).
- Be skeptical of on-screen context-usage figures during multi-iteration (advisor/sub-call) turns — displayed usage can be inflated 2x-6x, causing premature compaction (CC-040).
- Keep configured statusline commands fast, and keep project skill counts below the undocumented per-session cap, to avoid resource exhaustion and silent skill-invocation failures (CC-043, CC-035).
- Escape literal `$<digit>`-shaped text in skill/command prose with a backslash to avoid corruption by positional-arg substitution (CC-030).

**Needs re-verification on current version:** CC-009 and CC-018 (both filed within a day of this backfill pass, single reporter, zero/low comment activity) — worth a follow-up corroboration pass once they've had time to accrue community response.
