# Claude Code — Issue Index

> Last updated: 2026-09-10 (run 15)
> Latest platform version: v2.1.267 (released 2026-09-09)

## Active Issues (63)

| ID | Title | Type | Confidence | Impact | Workaround | Reproduced | Response |
|----|-------|------|------------|--------|------------|------------|----------|
| CC-001 | Background task notifications phantom-cancel pending calls with fake "user declined" sentinel | Bug | Likely | HIGH | Yes | No | Unevaluated |
| CC-002 | Background/scheduled agents hang indefinitely on unanswered permission prompt, fail silently | Bug | Likely | HIGH | Yes | No | Unevaluated |
| CC-003 | Subagent killed by usage limit falsely reported "completed" | Bug | Likely | HIGH | Partial | No | Unevaluated |
| CC-006 | Compaction can preserve fabricated "system message" concealment instructions | Bug | Confirmed | HIGH | Partial | No | Unevaluated |
| CC-007 | Mid-conversation behavioral rules dropped after compaction | Bug | Likely | HIGH | Yes | No | Unevaluated |
| CC-008 | One malformed MCP tool schema drops ALL tools from that server | Bug | Confirmed | HIGH | Yes | No | Unevaluated |
| CC-009 | Custom subagent `tools: Bash` frontmatter silently never grants Bash | Bug | Unverified | HIGH | Yes | No | Unevaluated |
| CC-010 | `PreToolUse` `defer` decision permanently strands subagent Bash call | Bug | Confirmed | HIGH | Partial | No | Unevaluated |
| CC-011 | `disallowedTools` not enforced on default-`subagent_type` nested dispatch | Limitation | Confirmed | HIGH | Partial | No | Unevaluated |
| CC-012 | Stale `ToolSearch` reference permanently bricks session (400 error) | Bug | Confirmed | HIGH | Yes | No | Unevaluated |
| CC-013 | `permissions.allow`/`bypassPermissions` intermittently ignored for subagents | Bug | Likely | HIGH | Partial | No | Unevaluated |
| CC-014 | `PreToolUse` `ask` decision silently not enforced | Bug | Likely | HIGH | Yes | No | Unevaluated |
| CC-015 | Killed/crashed `PreToolUse` hook fails OPEN instead of closed | Bug | Unverified | HIGH | Partial | No | Unevaluated |
| CC-016 | Background subagents denied Write/Bash regardless of permission mode | Bug | Unverified | HIGH | Yes | No | Unevaluated |
| CC-018 | Self-backgrounding Bash under `run_in_background` false-completes, orphans process | Bug | Unverified | HIGH | Partial | No | Unevaluated |
| CC-019 | Background Bash task killed ~17-20s after arming as turn's last call | Bug | Unverified | HIGH | Yes | No | Unevaluated |
| CC-020 | Interactive-session LSP client never sends `didChange` after Edit-tool write | Bug | Unverified | MEDIUM | Partial | No | Unevaluated |
| CC-022 | Mid-session `/model` switch doesn't apply to running session | Bug | Confirmed | HIGH | Partial | No | Unevaluated |
| CC-023 | Subagent Bash `cwd` reset can land in sibling subagent's worktree | Bug | Likely | HIGH | Partial | No | Unevaluated |
| CC-024 | `SessionStart` env file grows unboundedly, eventually wedges Bash tool | Bug | Likely | HIGH | Yes | No | Unevaluated |
| CC-025 | Plugin-native `PreToolUse` hooks unenforced; plugin `deny` unenforced | Bug | Unverified | HIGH | Partial | No | Unevaluated |
| CC-026 | Second `/compact` in one process corrupts transcript, breaks `/rewind` | Bug | Unverified | HIGH | Partial | No | Unevaluated |
| CC-027 | Leading whitespace before slash command sends it as plain text | Bug | Confirmed | MEDIUM | Yes | No | Unevaluated |
| CC-028 | `--continue` is directory-scoped, can co-write into another live session | Bug | Confirmed | HIGH | Partial | No | Unevaluated |
| CC-029 | Manual `/compact` can silently no-op on large conversations | Bug | Unverified | HIGH | Yes | No | Unevaluated |
| CC-030 | Skill/command positional arg substitution off-by-one; `$2`+ never works | Bug/Limitation | Confirmed | HIGH | Yes | No | Unevaluated |
| CC-031 | Explicitly-named `subagent_type` dispatch can ignore subagent's own definition | Bug | Unverified | HIGH | No | No | Unevaluated |
| CC-032 | Subagent plan-mode approval dialog can show PARENT session's plan | Bug | Confirmed | HIGH | Partial | No | Unevaluated |
| CC-033 | Subagents receive full auto-memory index and skill listing (no opt-out) | Bug | Likely | MEDIUM | Partial | No | Unevaluated |
| CC-034 | `CLAUDE.md`/path-scoped rules in `--add-dir` dirs silently skipped | Bug | Likely | MEDIUM | Partial | No | Unevaluated |
| CC-035 | Project skills beyond undocumented per-session cap become non-invocable | Bug | Likely | HIGH | Partial | No | Unevaluated |
| CC-036 | `CLAUDE.md` above git repo root never loaded into repo's own worktrees | Bug | Unverified | HIGH | Partial | No | Unevaluated |
| CC-037 | Hard-coded plan-mode notice text injected at prompt layer, invisible in transcript | Bug | Confirmed | MEDIUM | Partial | No | Unevaluated |
| CC-038 | Every Bash dispatch in large/resumed session can freeze process ~80-90s | Bug | Likely | HIGH | Partial | No | Unevaluated |
| CC-039 | `.claude/settings.json` non-atomic write — concurrent sessions can tear/clobber | Bug | Likely | HIGH | Partial | No | Unevaluated |
| CC-040 | Multi-iteration turns sum token usage across iterations, inflate 2x-6x | Bug | Confirmed | HIGH | Partial | No | Unevaluated |
| CC-041 | `.claude/settings.json` in ancestor directory silently never loaded | Bug | Unverified | HIGH | Yes | No | Unevaluated |
| CC-042 | `/rewind` can fail to restore never-git-tracked newly-created file | Bug | Unverified | MEDIUM | Partial | No | Unevaluated |
| CC-043 | statusLine refresh has no debouncing — slow command spawns unbounded copies | Bug | Confirmed | MEDIUM | Yes | No | Unevaluated |
| CC-044 | Duplicated base64 image content + unclamped render cost hard-freezes at 100% CPU | Bug | Confirmed | HIGH | Partial | No | Unevaluated |
| CC-046 | Mid-session-created `.claude/settings.json` hooks never arm until restart | Bug | Confirmed | HIGH | Yes | No | Unevaluated |
| CC-047 | One malformed hook entry silently kills all hooks for that event type | Bug | Confirmed | HIGH | Yes | No | Unevaluated |
| CC-048 | `WebFetch` `prompt` param ignored for pre-approved docs domains under ~100KB | Limitation | Confirmed | MEDIUM | Yes | No | Unevaluated |
| CC-050 | `SubagentStop` hook never fires when subagent killed via `TaskStop`/session exit | Bug | Unverified | MEDIUM | Yes | No | Unevaluated |
| CC-051 | Idle background shells reaped under macOS memory pressure, mislabeled "stopped by user" | Limitation | Confirmed | HIGH | Yes | No | Unevaluated |
| CC-052 | Background Bash task killed with no TaskStop, no pressure, no crash (Windows/Linux) | Bug | Likely | HIGH | Yes | No | Unevaluated |
| CC-054 | `EnterWorktree` isolation state is a session-wide latch, not scoped per-agent | Bug | Confirmed | HIGH | Yes | No | Unevaluated |
| CC-055 | `isolation:"worktree"` subagents: Edit writes into parent's tree, git-guard bypassable, auto-reap orphans cwd | Bug | Unverified | HIGH | Yes | No | Unevaluated |
| CC-056 | `Write`/`Edit` stay pinned to launch-time worktree after subagent's own `EnterWorktree` switch | Bug | Unverified | HIGH | Yes | No | Unevaluated |
| CC-057 | Session cwd resolution can become permanently unresolvable (EPERM) after worktree use | Bug | Likely | HIGH | Partial | No | Unevaluated |
| CC-059 | Project-level `.claude/rules/` symlinks to outside-project targets silently never load | Bug | Confirmed | MEDIUM | Yes | No | Unevaluated |
| CC-060 | Auto Mode's Bash-first steering defeats nested CLAUDE.md/rules loading, `/rewind`, hooks, and Edit/Write-scoped permissions | Bug | Confirmed | HIGH | Yes | No | Unevaluated |
| CC-063 | `EnterWorktree`/project switches don't propagate to hook subprocess `CLAUDE_PROJECT_DIR` | Bug | Confirmed | HIGH | Partial | No | Unevaluated |
| CC-067 | Nested/scoped skills discovered-but-not-invoked become permanently uninvocable after compaction | Bug | Confirmed | HIGH | Partial | No | Unevaluated |
| CC-068 | Windows statusline `pwsh.exe` host processes never exit, leak hundreds of orphans/hour | Bug | Unverified | MEDIUM | Yes | No | Unevaluated |
| CC-069 | `UserPromptSubmit` hook, correctly registered and verified working, never fires in CLI | Bug | Unverified | HIGH | No | No | Unevaluated |
| CC-070 | Plugin hooks' `commandWindows` field silently ignored on Windows, forces Git Bash | Bug | Unverified | MEDIUM | Yes | No | Unevaluated |
| CC-071 | Windows hook commands with unquoted backslash paths silently never execute | Bug | Likely | HIGH | Yes | No | Unevaluated |
| CC-072 | `UserPromptSubmit` skips firing for mid-turn/interrupt messages (stale, unresolved) | Bug | Likely | MEDIUM | No | No | Unevaluated |
| CC-073 | Bash cwd resets outside project dir/added dirs; hooks then receive stale, not actual, cwd | Bug | Confirmed | HIGH | Yes | No | Unevaluated |
| CC-074 | `/rewind` checkpoint race can drop restore option for tracked-file edits/deletions | Bug | Likely | MEDIUM | Partial | No | Unevaluated |
| CC-075 | `EnterWorktree` unconditionally refuses inside any pinned-cwd subagent, no fallback | Limitation | Unverified | HIGH | Yes | No | Unevaluated |
| CC-078 | `CLAUDE.md`/path-scoped rules never load when Claude creates a brand-new matching file | Bug | Unverified | MEDIUM | Partial | No | Unevaluated |

## Resolved / Retired Issues (1)

> Resolved entries older than 3 months are automatically removed.

| ID | Title | Fixed In / Status | Resolution Date |
|----|-------|--------------------|-----------------|
| CC-005 | `PostToolUse` hook doesn't fire for `Agent` tool completions at the parent session level | N/A (not relevant — unsourceable) | 2026-09-10 |

---

## Quick Summary

*(Rewritten run 15, after a full 13-batch re-verification pass re-checked every one of the 64 entries carried from run 14 against live GitHub state. Result: 62 of 63 surviving entries were confirmed unchanged (same confidence/classification/impact, no maintainer resolution); CC-005 was retired as unsourceable after two independent search passes failed to locate any matching GitHub issue; a handful of entries picked up incremental evidence (see below) but no reclassifications or resolutions occurred. See individual entries for full detail.)*

**Worktree isolation — the single biggest new cluster found across these runs** — Directly threatens MOSAIC's own multi-worktree fan-out pattern from multiple independent angles: `EnterWorktree` isolation state is a session-wide latch rather than scoped per-agent, confirmed by a maintainer on Linux and independently corroborated a fifth time via the "Agent Teams" spawn path — including a false-green test run where the exit code didn't reflect the agent's own tests actually executing (CC-054); `isolation:"worktree"` subagents can still have `Edit` calls land in the parent's tree with a bypassable git-write guard (CC-055); `Write`/`Edit` can stay pinned to a subagent's launch-time worktree even after it calls `EnterWorktree` itself, while `Bash` correctly follows — an inverse-direction desync (CC-056); session cwd resolution can become permanently unresolvable (EPERM) after worktree use, correlated with macOS TCC-protected directories (CC-057); a subagent's Bash `cwd` can land in a sibling's worktree outright, Likely-confidence after a second reflog-verified data-loss report (CC-023); and `EnterWorktree` unconditionally refuses to run inside any subagent that already has a pinned cwd (`isolation:"worktree"` or explicit `cwd`), with no documented fallback — meaning worktree-isolated subagents can't nest a further worktree switch at all (CC-075, new). Separately: `EnterWorktree`/project-directory switches don't propagate to hook subprocesses' `CLAUDE_PROJECT_DIR`, so `PreToolUse`/`PostToolUse` hooks silently act on the wrong project, maintainer-confirmed (CC-063); and a plain Bash session's cwd resets outside the project directory (or an `--add-dir` directory) whenever `cd` leaves it, with hooks then receiving that stale reset value instead of the triggering command's real directory — maintainer (`bcherny`) confirmed and is considering a fix (CC-073, new).

**Subagent dispatch, isolation & scoping failures** — `disallowedTools` doesn't cascade to nested default-typed dispatches (CC-011, maintainer-confirmed by-design), an explicitly-named `subagent_type` dispatch may ignore the subagent's own definition entirely and inherit the dispatcher's (CC-031, single-reporter, high-priority for corroboration), subagents inherit the full auto-memory/skill index with no opt-out, corroborated by 3 independent HTTP-proxy reproductions (CC-033, Likely), a subagent's plan-mode approval dialog can show the parent's plan (CC-032, confirmed cross-OS), and `SubagentStop` never fires when a subagent is killed via `TaskStop`/session exit, degrading hook-based lifecycle telemetry (CC-050, Unverified).

**Permission and hook enforcement gaps** — A cluster where configured permission/hook enforcement is silently not honored, several narrower than first reconstructed: `permissions.allow`/`bypassPermissions` for subagents, upgraded to Likely after a fourth independent corroboration (CC-013), `PreToolUse` `ask` silently not enforced in interactive auto-mode (CC-014, Likely), a killed/crashed `PreToolUse` hook fails open, its supposed fix PR actually patches an unrelated plugin (CC-015, Unverified), background subagents denied Write/Bash regardless of mode (CC-016, Unverified), plugin-sourced hooks unenforced (CC-025), a `defer` decision strands calls that then falsely report success, maintainer engaged (CC-010), a single malformed hook entry kills all hooks for its event type persistently, maintainer-reproduced (CC-047), and a well-formed, pre-existing `UserPromptSubmit` hook can silently never fire for any prompt in a live CLI session on Windows despite passing its own registration checks (CC-069, Unverified). A dedicated follow-up on Windows-specific hook execution found two more distinct mechanisms: a plugin hook's `commandWindows` field is silently ignored, forcing Git Bash and causing MSYS2 crashes (CC-070, new, Unverified), and Windows hook commands with unquoted backslash paths silently never execute at all — Git Bash eats the backslashes — corroborated by two independent reporters the same week, a recurring pattern maintainers have partially chased before (CC-071, new, Likely). Also newly tracked: `UserPromptSubmit` skips firing for mid-turn/interrupt messages — stale-bot closed with no real fix confirmation, flagged for re-verification against current version (CC-072, new, Likely).

**Context compaction and conversation-stability hazards** — Still the most severe cluster, headlined by a maintainer-confirmed root cause: compaction can preserve model-fabricated instructions to conceal actions, with a documented live-API-call occurrence (CC-006, Confirmed). Multi-iteration turns inflating measured token usage 2x-6x is now **Confirmed** after 4 independent reproductions converged on the same root-cause code — including a newly-found, more severe variant where compaction can *increase* net context below the harness's "rebuild floor," disproportionately hitting subagent/workflow-subagent compactions (CC-040). Mid-conversation behavioral rules are dropped post-compaction even when the summary retains the text (CC-007) — and Auto Mode's Bash-first steering instruction independently defeats that same mitigation by construction, also disabling `/rewind`, `PostToolUse` hooks, and Edit/Write-scoped permission rules whenever Auto Mode is active — extremely well-evidenced, Confirmed (CC-060, new/expanded). Manual `/compact` silently no-op (CC-029) and a second `/compact` corrupting the transcript/`/rewind` (CC-026) remain Unverified — single-reporter, no maintainer response, kept active given severity but not overstated.

**Settings/config and rules-loading gaps** — `.claude/settings.json` is written non-atomically and can tear under concurrent sessions (CC-039, Likely), settings created mid-session never arm hooks until restart, maintainer-reproduced (CC-046), `--add-dir`-added directories silently skip their `CLAUDE.md`/path-scoped rules (CC-034, Likely), ancestor-directory settings never loading (CC-041, Unverified — its "fix PR" targets an unrelated plugin), and a `CLAUDE.md` above a repo's root never loading into that repo's own worktrees (CC-036, single-reporter but rigorous fixture-matrix test). Project-level `.claude/rules/` symlinks to outside-project targets silently never load, confirmed via bisection and ~10 independent reproducers (CC-059). A brand-new file created via the native `Write` tool never triggers nested `CLAUDE.md`/rules loading either, since the loading gate is keyed off a prior `Read` call and a just-created file has none — the same `Read`-gated architecture as CC-060, reached via a distinct trigger with no Auto Mode involved (CC-078, new, Unverified).

**Session/process reliability and resource exhaustion** — A stale `ToolSearch` reference permanently bricks a session across three independent trigger mechanisms (CC-012), every Bash dispatch in a large/resumed session can freeze the whole process for ~80-90s (CC-038), duplicated base64 image content plus unclamped render cost can hard-freeze at 100% CPU, maintainer-confirmed (CC-044), an undebounced statusLine refresh can spawn unbounded concurrent processes, maintainer-reproduced (CC-043), and — new this run — Windows statusline `pwsh.exe` host processes never exit, leaking hundreds of orphans/hour (CC-068, Unverified, distinct mechanism from CC-043).

**Task/agent status misreporting and background dispatch reliability** — Usage-limit kills reported as "completed" (CC-003), background/scheduled agents hanging indefinitely or failing silently on an unanswered permission prompt, now corroborated across macOS/Windows/Linux via four reports including a root-cause detail that the scheduled-task launcher only honors `permissions.defaultMode` set at user-level settings (CC-002, Likely), background task notifications phantom-cancelling pending calls with a fake "user declined" sentinel — NOT stale/abandoned, actively corroborated (CC-001). New this run: idle background shells get reaped under macOS memory pressure and mislabeled "stopped by user," with a documented opt-out env var — Limitation, Confirmed (CC-051); a distinct Windows/Linux background-Bash-kill mechanism unrelated to memory pressure or turn-position (CC-052, Likely); and nested/scoped skills discovered-but-not-invoked can become permanently uninvocable after compaction, maintainer-confirmed with a fix in review (CC-067) — distinct from the per-session skill-count cap (CC-035, Likely).

**`/rewind` reliability** — Beyond CC-042 (never-git-tracked file not restored) and CC-060's Auto Mode-specific casualty, a checkpoint file-tracking race (log signature "tracking 0 files") can drop the "Restore code" option entirely for ordinarily-tracked file edits or deletions — 5 independent reporters across CLI and Desktop over 8 months (CC-074, new, Likely).

**By-design limitations MOSAIC must permanently adapt to** — CC-011 (`disallowedTools` scoping), CC-048 (`WebFetch` prompt ignored for small pre-approved docs, Confirmed with the maintainer's exact 100KB/25K-token cap explained), and CC-051 (macOS idle-shell memory-pressure reaping) are all maintainer-confirmed intentional but still carry real operational risk. CC-030 splits into an Unverified Bug (`$2`+ non-substitution) and a Confirmed, maintainer-by-design Limitation (literal `$<digit>`-text corruption, escapable with a backslash). CC-075 (`EnterWorktree` refusing inside a pinned-cwd subagent) reads as a protective design choice rather than a confirmed by-design statement, but still blocks a plausible MOSAIC pattern (nesting worktree switches inside an already worktree-isolated subagent) with no documented fallback.

**What run 15's verification pass changed:** No reclassifications, resolutions, or confidence swings beyond CC-005's retirement (see above). A handful of entries gained incremental corroborating evidence without changing their rating: CC-033's mitigation (a thin-pointer-table `MEMORY.md` pattern) is now recorded as Workaround #2 and the index Workaround column upgraded to **Partial**; CC-018 gained a reporter retraction that narrows its scope but reaffirms the core orphaning mechanism; CC-023, CC-029, and CC-044 each picked up an additional corroborating comment (CC-044's most notably: two more historically-failed fix PRs for the same bash-hang root cause were found, strengthening its "recurring, never actually fixed" framing).

## Recommended Mitigations

- Put durable behavioral rules in `CLAUDE.md` rather than in-conversation instructions — but be aware Auto Mode's Bash-first steering can independently defeat CLAUDE.md loading itself, along with `/rewind` and hooks, whenever it's active (CC-007, CC-060).
- Never trust a subagent's reported "completed" status alone as proof of real, non-truncated work — independently validate output content where the result matters (CC-003, CC-001, CC-002).
- Avoid mid-session model switches when deferred (`ToolSearch`-loaded) tools are in play — a confirmed session-bricking trigger via three separate mechanisms (CC-012).
- Do not rely on `disallowedTools` alone as a security boundary for nested/further subagent dispatch — verify tool scoping explicitly (CC-011, CC-031).
- Pre-approve every tool a background/scheduled subagent may need, and set `permissions.defaultMode` at **user-level** settings specifically for scheduled/background launchers — project/local-level settings are silently ignored by that launch path (CC-002, CC-016).
- Avoid launching multiple concurrent Claude Code sessions in the same working directory with `--continue` (corrupts the shared task store, not just the transcript), and avoid concurrent writes to a shared `.claude/settings.json` (CC-028, CC-039).
- Treat `isolation:"worktree"` as leaky, not airtight: have each subagent explicitly `cd`/re-verify its intended worktree at the start of every Bash call, don't assume `Write`/`Edit` follow a subagent's own later `EnterWorktree` switch, and don't assume a hook's `CLAUDE_PROJECT_DIR` (or its "cwd" argument) reflects the current worktree/directory (CC-023, CC-054, CC-055, CC-056, CC-063, CC-073). Don't plan to nest a further `EnterWorktree` switch inside an already worktree-isolated or pinned-cwd subagent — it's refused outright with no fallback (CC-075). Also don't assume a `CLAUDE.md` placed above a repo root reaches that repo's own worktrees (CC-036), avoid symlinking `.claude/rules/` to outside-project targets (CC-059), and don't count on nested rules loading for a file your own session just created (CC-078).
- On Windows, avoid unquoted backslash paths in hook `command` strings (Git Bash silently drops them) and don't rely on plugin hooks' `commandWindows` field — it's currently ignored (CC-070, CC-071). Independently verify `/rewind`'s "Restore code" option actually appeared rather than assuming its absence means nothing changed (CC-074).
- Be skeptical of on-screen context-usage figures during multi-iteration (advisor/sub-call) turns, and don't assume `/compact` always reduces context — it can occasionally increase it below the harness's rebuild floor, hitting subagent/workflow seats hardest (CC-040).
- On macOS, expect long-idle background Bash shells to be reaped under memory pressure and reported as user-stopped — use `CLAUDE_CODE_DISABLE_BG_SHELL_PRESSURE_REAP=1` if that's not acceptable (CC-051).
- Keep configured statusline commands fast and non-PowerShell-spawning on Windows, and keep project/nested skill counts and invocation patterns modest, to avoid resource exhaustion and silent skill-invocation failures (CC-043, CC-068, CC-035, CC-067).
- Escape literal `$<digit>`-shaped text in skill/command prose with a backslash to avoid corruption by positional-arg substitution (CC-030).

**Needs re-verification on current version:** CC-009 and CC-018 (filed within a day of the backfill pass, single reporter, zero/low activity); CC-050, CC-068, CC-069, CC-070, and CC-072 (all single reporter or stale-bot-closed with no maintainer engagement) — worth a follow-up corroboration pass once they've had time to accrue community response. Re-checked in run 15 but none had crossed enough new time/activity to change status.

**Not yet investigated — flagged for a future pass:**
- `#87657` (intermittent per-session hook-registry loss, desktop app) — no deterministic repro yet, worth revisiting if it gains corroboration.
- `#22700`, `#23766` (closed) and `#61922` (already fixed) — a broader "hooks forced through/mishandled by Git Bash on Windows" pattern the maintainers have partially chased before; worth a quick look to fully round out the CC-070/CC-071 Windows-hook-shell cluster.
- `#31940`, `#31939` (per-subagent `additionalDirectories` scoping; missing `agent_id` in `PreToolUse` hook input) and `#62590` (jj-workspace `@`-file refs resolve to the parent git repo, likely out of scope since MOSAIC uses git) — surfaced while investigating the worktree cluster, possibly relevant to CC-054/055/056/063/073.
- `#82249` (new, surfaced during CC-005's retirement search) — `SubagentStop` doesn't fire for subagents launched via the Agent tool asynchronously/in the background; real, evidenced, currently-untracked. Strong candidate for its own new entry, distinct from CC-050's TaskStop/session-exit scenario.
- `#90449` (new, surfaced during CC-060 re-verification) — reportedly contains a contributor's own "documented source cause" writeup for the Auto Mode Bash-first-steering defect; worth reading directly to see if it adds anything to the CC-060 cluster.
- Any remaining adjacent leads noted inline in individual entries' own Notes fields.
