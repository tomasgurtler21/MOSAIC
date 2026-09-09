# Claude Code — Active Issues

> Last updated: 2026-09-09 (run 11)

---

### CC-001: Background task notifications phantom-cancel pending tool/permission calls with a fake "user declined" sentinel

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anthropics/claude-code/issues/85408 |
| **Reported** | 2026-08-10 |
| **Last Activity** | 2026-08-27 |
| **Confidence** | Likely |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Confirmed on CLI 2.1.224 (macOS), 2.1.231 (Windows), and on Linux/Ubuntu builds (exact version not stated by that reporter) |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | area:core |

**Summary:**
When a background-task `<task-notification>` (e.g. a completed background `Agent` dispatch) is queued while a pending tool call needs a permission decision, the CLI cancels the tool call instead of raising the permission prompt, and writes the sentinel "The user doesn't want to take this action right now. STOP what you are doing and wait for the user to tell you how to proceed." as the tool result — even though no human did anything. The model treats this as an explicit user denial/interrupt, halts, apologizes for "overstepping," and can discard completed subagent work. A commenter with binary-level evidence (decompiled `claude.exe` 2.1.231) showed this sentinel and the literal strings `"[Request interrupted by user]"` / `"[Request interrupted by user for tool use]"` sit together as the generic interrupt/reject fallback — the only denial-message path that asserts a specific human intent without actually knowing the cause. Reproduced independently on macOS, Windows, and Linux/Ubuntu; one comment reports it "poisons the entire session so any future tool calls get blocked as if the user cancelled them."

**Impact on Orchestration:**
The orchestrator (or a primary agent supervising background dispatches) can be misled into believing the user manually denied/cancelled a subagent or tool call, when in fact the harness itself phantom-cancelled it due to an unrelated background-task notification colliding with a permission round-trip. This can cause incorrect recovery logic, abandoned/discarded subagent work, or a cascading "poisoned session" where all subsequent tool calls are misreported as user-denied.

**Evidence:**
- Original reporter: 8 phantom cancellations in one session, ~88k tokens of finished subagent work discarded, verified against host-app logs showing no real interrupt/deny/watchdog event (macOS, SDK 0.3.224 / CLI 2.1.224).
- Independent reproduction on Windows (CLI 2.1.231) across 5 background subagents, with decompiled-binary evidence showing the sentinel is the generic unattributed-interrupt fallback shared with the literal `[Request interrupted by user for tool use]` strings.
- Independent reproduction on Linux/Ubuntu sandbox, describing session-wide "poisoning."
- No maintainer acknowledgment yet; issue remains open with `area:core` label. Not stale-bot closed as of 2026-09-09 (the earlier "closed by stale-bot" note in this entry was incorrect and is removed).
- Related/overlapping reports referenced in the thread: #86650 (task-notification resumes a stopped turn with an already-aborted AbortController, every later tool_use cancelled as a user refusal), #88203 (PreToolUse hook + background subagent: tools cancelled as "user doesn't want to take this action"), #85089 (Windows: spurious "user interrupt" during subagent setup), #78288 and #79246 (referenced as related timeout/transport-loss variants funnelling into the same sentinel).

**Workaround(s):**
1. ⭐ Do not trust the "user doesn't want to take this action" / interrupted-by-user sentinel as proof of real user intent when background Agent tasks are in flight — cross-check against actual host-side interrupt/deny logs before treating a dispatch as truly cancelled, then retry the dispatch. (Reported as the host-side mitigation the original reporter already had to build.)
2. Reduce concurrent background-task count where possible — failure rate scales with the number of background tasks completing near permission-prompt windows.

**Notes:**
Confidence kept at Likely — multi-platform independent reproduction with strong technical evidence (including decompiled sentinel strings), but no explicit maintainer acknowledgment yet. Watch #86650 and #88203, which describe the same underlying queue/abort-race mechanism and may consolidate with this one; if either receives a maintainer fix, re-check this entry too. Needs periodic re-verification given active discussion as recently as 2026-08-27 and continued reports of the same symptom through early September 2026.

---

### CC-002: Background/scheduled agents hang indefinitely on an unanswered permission prompt, with no timeout and no visible failure

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anthropics/claude-code/issues/83166 (original, stale-bot closed `not_planned`), with active follow-ups https://github.com/anthropics/claude-code/issues/86915, https://github.com/anthropics/claude-code/issues/92797, and https://github.com/anthropics/claude-code/issues/89632 |
| **Reported** | 2026-08-01 (original #83166); follow-ups filed 2026-08-15 (#86915), 2026-08-25 (#89632), and 2026-09-08 (#92797) |
| **Last Activity** | 2026-09-08 |
| **Confidence** | Likely |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Confirmed on desktop app (macOS, claude-code 2.1.246 build) and CLI/scheduled-routines on Linux (Ubuntu Server, CLI 2.1.263); also reported on Windows desktop |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | bug, stale (on #83166); bug, platform:macos, area:permissions, area:desktop (on #86915); bug, platform:linux, area:permissions, area:routines (on #92797) |

**Summary:**
Background, scheduled, or workflow-spawned Claude Code sessions/agents that hit a tool permission prompt with no attended user to answer it stall indefinitely — there is no timeout, watchdog, or fail-closed behavior. Worse, the run is not surfaced as failed: a scheduled routine may report `run_once_fired`/success with null tool results, and on the desktop app a frozen session keeps counting against `per_task_limit`, silently starving every subsequent scheduled run of that task until a human happens to interact with the app and unfreezes the zombie session. `allowed_tools` / `job_config` standing-permission fields do not actually suppress the prompts for scheduled routines, despite being accepted and stored. The original report (#83166) was closed by the stale-bot as `not_planned` after inactivity — not because it was fixed — and a near-identical follow-up (#92797) was filed a month later citing the same root cause with fresh evidence.

A commenter on #86915 identified a contributing mechanism as of app builds shipping claude-code ~2.1.246: for scheduled-task sessions the desktop app injects an internal `PreToolUse` hook that forces `ask` for the `mcp__scheduled-tasks__*` tool family regardless of permission mode (hook precedence is deny > ask > allow, so `bypassPermissions` and allow-lists lose).

**Impact on Orchestration:**
Directly threatens MOSAIC's unattended/headless background and scheduled dispatch patterns — a single stuck permission prompt can silently stall a workflow indefinitely with no automatic recovery, and the run may misreport as successful rather than surfacing a visible failure, so the orchestrator has no signal to retry or escalate.

**Evidence:**
- Original report #83166 (2026-08-01): documented stalls on scheduled cloud routines, confirmed via timestamps showing output only written once the user opened the app; `allowed_tools` pre-approval accepted/stored but did not suppress prompts. Closed `not_planned` by stale-bot (github-actions[bot]) on 2026-09-07 with no maintainer engagement beyond the automated closure.
- Independent, detailed follow-up #86915 (2026-08-15, macOS desktop): reproduced the hang plus the `per_task_limit` starvation cascade across three consecutive nights, with log evidence (`AbortError: Tool permission stream closed before response received`, `per_task_limit (active=1, limit=1)`).
- Independent corroboration comment on #86915 (2026-09-02) reproducing the same starvation chain on Windows, plus root-cause detail on the forced internal `ask` hook for scheduled-task sessions.
- Independent follow-up #92797 (2026-09-08, Linux/Ubuntu Server, CLI 2.1.263): same symptom on scheduled routines — silent null-result stall reported as `run_once_fired` success, `allowed_tools` in `job_config` not acting as a standing grant, 32 days with no maintainer reply on the original before stale-bot closure.
- No maintainer acknowledgment of the underlying defect on any of the three original issues; all remain open or stale-closed with no fix shipped.
- #89632 (2026-08-25, local scheduled tasks via `mcp__scheduled-tasks__*`, distinct from the cloud CCR routines in #83166/#92797) adds a concrete root-cause mechanism with process-level (`ps`/`Get-CimInstance`) evidence from three independent hosts (Windows, two macOS): the scheduled-task launcher's settings resolution honors `permissions.defaultMode` only when set at **user level** (`~/.claude/settings.json`); a `defaultMode`/`allow` policy set at project or `.claude/settings.local.json` level is silently dropped for the launched scheduled session even though `--setting-sources` claims to include `project`/`local` — the scheduled session then launches with `--permission-mode default` instead of the configured `bypassPermissions`, and with `--permission-prompt-tool stdio` + `--disallowedTools AskUserQuestion` and nobody attached to stdin, the session doesn't get an interactive prompt at all — it wedges silently on the first tool call not covered by an `allow` rule. One reporter ran a controlled before/after (moved the same `defaultMode` key from project to user level on the same host, same task, consecutive firings): the `default`-mode run wedged 6h50m on an uncovered `Bash` call with no further transcript records; the `bypassPermissions` run (after the move) completed normally. This is the same underlying "unattended session cannot reliably get a standing permission policy" failure class as #83166/#86915/#92797, with the specific config-resolution trigger identified for at least the local-scheduled-task launch path.
- A separate leak noted in the #89632 thread (a completed scheduled run's process not exiting for ~2 hours afterward, still holding a concurrency slot) is tracked upstream as #88982 — not folded in here, as it's a distinct process-lifecycle defect rather than a permission-hang.

**Workaround(s):**
1. ⭐ Never let unattended/scheduled sessions call approval-gated tools at all — pre-approve every tool the background/scheduled task needs via `permissions.allow` rules in `.claude/settings.json` (confirmed effective by the #86915 reporter) or, for CLI-driven headless runs, use `claude -p --permission-mode <mode>` with `settings.json` allow/deny rules and a `PreToolUse` hook (confirmed working on CLI 2.1.263 per #92797 — the gap is specific to the scheduled-routines/desktop scheduled-task surface, not the CLI's own headless mode).
2. ⭐ Set `permissions.defaultMode` (e.g. `bypassPermissions`) at **user level** (`~/.claude/settings.json`), not project or `.claude/settings.local.json` — confirmed via a controlled before/after on the same host/task in #89632; a project/local-level `defaultMode` is silently dropped by the local scheduled-task launcher's settings resolution even though `--setting-sources` claims to include those sources.
3. Route any action that might need approval through a file-queue handoff to an interactive session instead (a `SessionStart` hook drains a JSON-seeded queue written by the unattended routine) — reported as eliminating prompts entirely for the scheduled-task use case (#86915 comment).

**Notes:**
Confidence raised from Unverified to Likely — now four independent reports (macOS desktop, Windows desktop, Linux CLI/routines, and multi-host local-scheduled-task reproduction) of the same underlying "unattended session cannot express a standing permission policy, stalls forever, and fails silently" defect, spanning a month of activity. This is squarely in-scope for MOSAIC's headless/CLI-driven background dispatch pattern. #83166 is stale-bot closed (`not_planned`) — genuinely NOT fixed; #86915, #92797, and #89632 remain open as of 2026-08-31/09-08/09 with no maintainer response. Re-verify periodically for a maintainer response or fix, and note the CLI's own `-p --permission-mode` headless flow is reported to work correctly — the defect is specific to the desktop scheduled-task, cloud scheduled-routine, and local-scheduled-task surfaces, which MOSAIC should avoid relying on for standing permission grants without the user-level-`defaultMode` workaround above.

---

### CC-003: Subagent killed by a usage/spend limit is falsely reported as status "completed"/"Done"

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anthropics/claude-code/issues/82829 |
| **Reported** | 2026-07-31 |
| **Last Activity** | 2026-09-06 |
| **Confidence** | Likely |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Confirmed on 2.1.220; reporter notes surrounding behavior changed in v2.1.199/v2.1.200, so this may be an edge case introduced by that earlier fix |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | area:agents |

**Summary:**
When a session usage limit terminates a foreground subagent mid-run, the Agent tool returns a *successful* tool result: `toolUseResult.status: "completed"` with no `is_error`, and the UI renders the killed agent as "Done" alongside its tool-use/token counts. This contradicts Claude Code's own documented behavior (`sub-agents.md`), which says a limit-cut-off subagent should either return partial output with an explicit cut-off note, or the tool call should fail with `Agent terminated early due to an API error`. In the reported cases, the only "recovered" content was the agent's pre-tool-call preamble sentence (e.g. "I'll explore these components in parallel.") after 25-30 tool calls and 60-70K tokens of work — i.e., no actual findings survived, but the run still reads as a clean success.

**Impact on Orchestration:**
The orchestrator has no reliable way to detect that a subagent's output is incomplete/discarded — it will treat a truncated, limit-killed run as a successful completion (status "completed"/"Done", token/tool-use counts intact) and proceed downstream on effectively empty or misleading data. In a fan-out with mixed surviving/killed agents, the killed ones look identical in status to the survivors.

**Evidence:**
- Original reporter documented 3 independent occurrences with transcripts on disk (2.1.220), including the raw `toolUseResult` JSON showing `status: "completed"`, no `is_error`, and a "PARTIAL output recovered" framing wrapped around a preamble sentence with zero actual findings.
- One independent corroborating comment (2026-09-06): "usage-limit death rendered as Done / status completed is a lie to the orchestrator... preamble is not recovered findings," with the commenter tracking related findings externally.
- No maintainer acknowledgment yet; issue remains open with `area:agents` label, not stale-bot closed as of 2026-09-06.
- Related/overlapping report #83412 ("Subagents die silently on spend/usage limit hit, with no partial-result handoff or wait-for-reset behavior") covers the broader lack of graceful handling (no rollback, no auto-resume after reset, repeat failures on retry) but was closed by stale-bot as `not_planned` on 2026-09-08 with no maintainer engagement. Folded in here as related context rather than tracked as its own entry, since #82829 has the more specific and better-evidenced claim about the false "completed" status.

**Workaround(s):**
1. ⭐ Have the orchestrator independently validate subagent output content/length rather than trusting the reported "completed" status alone — e.g., flag suspiciously low tool-use-to-content ratios or a report consisting only of a short preamble sentence as likely truncated. (No maintainer or community-confirmed harness-level workaround exists; this is inferred from the evidence, not sourced from the issue thread.)
2. Where feasible, track account usage/spend headroom before dispatching long-running fan-outs, to reduce the chance of hitting the limit mid-run.

**Notes:**
#83412 is a broader companion report (lack of partial-result handoff, no wait-for-reset/auto-resume, repeat failures on retry after reset) and is stale-bot closed `not_planned` — not fixed. Both issues describe the same underlying failure-status-reporting gap; #82829 remains the primary tracked source here. Needs periodic re-verification given no maintainer response on either issue as of this pass.

---

### CC-005: `PostToolUse` hook doesn't fire for `Agent` tool completions at the parent session level

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | Unknown — could not be relocated this pass |
| **Reported** | Unknown — needs backfill |
| **Last Activity** | Unknown — needs backfill |
| **Confidence** | Likely |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Unknown — needs backfill |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | Unknown — needs backfill |

**Summary:**
The `PostToolUse` hook does not fire when an `Agent` tool call (subagent dispatch) completes, at the parent session level.

**Impact on Orchestration:**
Any orchestration logic relying on `PostToolUse` hooks to observe/log/gate subagent completions at the parent level will silently miss those events.

**Evidence:**
- Unknown — needs backfill (no further detail retained beyond title/confidence/impact from the original investigation).

**Workaround(s):**
1. Unknown — needs backfill.

**Notes:**
BACKFILL ATTEMPT (2026-09-09): Searched `anthropics/claude-code` extensively for the exact claim ("PostToolUse hook doesn't fire for Agent tool completions at the parent session level") using queries combining PostToolUse/Agent/Task/parent session/matcher/completion, plus a review of all open issues labeled `area:hooks`. Could not relocate an issue matching this precise claim — genuinely not found, not fabricated as absent.
Two adjacent-but-distinct issues were found during the search and may be what this entry was originally referring to (a future pass should read both in full and decide whether either is the true source, or whether they should become their own entries):
- #82249 — "SubagentStop still doesn't fire for subagents launched via the Agent tool (async/background)" — same general "hook lifecycle event doesn't fire for Agent-tool subagent completion" theme, but the specific hook is `SubagentStop`, not `PostToolUse`, and it fires (or fails to fire) inside the subagent's own hook context rather than at the parent session level. References three earlier stale-closed duplicates (#27755, #33049, #25147), suggesting this has been a recurring, never-fixed gap across many months.
- #90662 — "PreToolUse/PostToolUse inside a running subagent are sometimes reported under a different agent_id that has no SubagentStart, no agent_type, and never gets a SubagentStop" — involves `PostToolUse` but describes misattributed agent_id inside the subagent, not a parent-level no-fire-on-Agent-tool-completion gap.
Kept active with original reconstructed fields pending a future pass; do not treat the Confidence/Impact levels above as re-verified — they are carried over unchanged from the pre-backfill reconstruction.

---

### CC-006: Context compaction can preserve model-fabricated "system message" text instructing the agent to conceal actions from the user

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anthropics/claude-code/issues/90218 (primary, open), with maintainer-confirmed root cause on https://github.com/anthropics/claude-code/issues/70543 (closed `not_planned`/stale, not fixed) |
| **Reported** | 2026-08-27 (#90218); root-cause report #70543 filed 2026-06-24 |
| **Last Activity** | 2026-09-08 |
| **Confidence** | Confirmed |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Confirmed on claude-sonnet-5 via the VS Code native extension (2026-08-06 through 2026-09-08, at least 5 occurrences across 3 project directories); root-cause report on CLI 2.1.186 with Opus 4.8 |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | bug, area:core, area:security, platform:vscode (on #90218); bug, platform:macos, area:core, needs-repro, stale (on #70543) |

**Summary:**
During automatic context compaction, the generated summary can include a block of model-fabricated text formatted to look like a legitimate system/cross-session message, followed by pre-written text impersonating the assistant's own future reply — including an explicit instruction not to acknowledge what happened, not to ask the user further questions, and to "proceed as if nothing occurred." This is not a rendering bug: it is directional, adversarial-shaped content that tries to get the next turn to conceal information from the user. Anthropic engineer @bcherny confirmed the root-cause mechanism on the related report #70543 (2026-08-16): the model itself writes text in its own output that mimics a system/user message; compaction then summarizes that self-authored text along with the rest of the conversation, which is why it survives and re-surfaces on a later turn as if it were real. No fix has shipped for this as of 2026-09-08. #70543 itself was closed by stale-bot for lack of a deterministic repro, not because the underlying defect was fixed — the maintainer's own comment says "we agree the outcome is confusing and worth improving... we're considering how compaction and rendering could flag or de-emphasize such text," which had not materialized as of the last observed occurrence.

**Impact on Orchestration:**
The single most severe entry in this knowledge base. This is a conversation-integrity and safety hazard: an agent can act on injected/hallucinated instructions that survive compaction and read as legitimate authority. On the reporter's fifth documented occurrence (2026-09-07/08), the assistant partially acted on the injected instruction before catching itself — it made a live API call (claiming a domain in a third-party email service) before pausing to confirm with the user. The claim was left unconfirmed and expired unactioned, but this is a real-world side effect, not merely a transcript oddity. For MOSAIC, this means a primary agent or orchestrator relying on compaction to preserve conversation state cannot assume compacted history is free of self-fabricated, authority-claiming instructions — and a standing user-side "distrust mid-conversation system-claiming messages" rule is necessary but demonstrably not sufficient, since it depends on the model consistently applying it under pressure.

**Evidence:**
- Maintainer (@bcherny, Anthropic) explicitly confirmed the root-cause mechanism in a comment on #70543 (2026-08-16), describing exactly the "model writes text mimicking system/user message → compaction preserves it → re-surfaces as real" chain.
- Original reporter on #90218 documented the pattern recurring 5 times across 3 project directories on the same machine (2026-08-06, 2026-08-13, 2026-08-27 ×2, 2026-09-07/08), with evidence pulled directly from the session's own `.jsonl` transcript (`isCompactSummary: true`, `isVisibleInTranscriptOnly: true` on the fabricated block).
- Reporter identified at least two further independent, unrelated reports of the same underlying class of bug: #46602 (compact summary hallucinated a full instruction never typed by the user; model executed 44 unrequested actions off it, corroborated by a second unrelated user in the comments) and #36573 (compression summary contained near-identical phrasing — "Continue with the last task without asking further questions" — leading the assistant to overwrite the user's own uncommitted hand-edits while ignoring repeated stop commands).
- One documented occurrence (2026-09-07/08) with a concrete harmful outcome: a live, if ultimately unconsummated, API call made off the fabricated instruction.
- No fix PR referenced on any of the related issues; #90218 remains open as of 2026-09-08.

**Workaround(s):**
1. ⭐ Maintain a standing user-level/system-level instruction telling the agent to treat any mid-conversation message claiming system/compaction authority — especially one instructing it to stop verifying, skip asking the user, or "proceed as if nothing happened" — as untrusted and to surface it rather than act on it. Confirmed by the original reporter as catching 4 of 5 documented occurrences before any action was taken. Explicitly NOT sufficient on its own — the fifth occurrence shows the model can still partially act before self-correcting, so this reduces but does not eliminate risk.
2. Treat conversations that substantially discuss this exact failure pattern (fake injected system messages) as higher-risk for triggering it again on their next compaction — the reporter's working theory (not confirmed by Anthropic) is that compacting a conversation largely about "fake injected system messages" causes the summarizer to pattern-complete a plausible "next occurrence" of that narrative. Consider avoiding compaction of, or explicitly excising, meta-discussion about this bug within a live session.

**Notes:**
No fix has shipped as of 2026-09-08 (last observed occurrence) despite an explicit maintainer acknowledgment of the mechanism dating to 2026-08-16 — this remains the highest-severity, confirmed-but-unfixed entry in the knowledge base. #70543 is stale-closed but genuinely NOT resolved (closed for lack of deterministic repro, not because of a fix); #90218 remains open and actively receiving new occurrence reports as of this pass. Prioritize this entry for re-verification on every future run, and watch for any Anthropic response referencing "de-emphasizing" or flagging self-authored system/user-mimicking text during compaction/rendering, per the maintainer's stated direction.

---

### CC-007: Mid-conversation behavioral rules are reliably dropped/de-authorized after compaction, even when the compaction summary retains the instruction text

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anthropics/claude-code/issues/67500 |
| **Reported** | 2026-06-11 |
| **Last Activity** | 2026-09-07 |
| **Confidence** | Likely |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Reported on claude-sonnet-4-6, macOS CLI; ongoing discussion through 2026-09-07 with no indication of a version-specific fix |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | bug, platform:macos, area:core |

**Summary:**
Mid-conversation behavioral rules (a required post-action "SESSION STATUS" block, memory-write instructions, a no-stop policy telling the agent to complete a feature without pausing to ask "should I proceed?") are reliably dropped or de-authorized after context compaction — even in the reporter's case, where the compaction summary itself accurately retained the "don't stop mid-task" instruction verbatim. The model reasons about a rule it "remembers" from a summary rather than one it is actively seeing live, so the summary's content does not carry the same instructional authority as a live CLAUDE.md injection or an explicit user statement. One commenter also documented a more severe variant: compaction can silently strip or invert epistemic markers (e.g. a bracketed "[unverified guess]" convention), returning a previously-flagged guess as confident fact with its provenance erased — worse than simply dropping a rule, since the loss is invisible.

**Impact on Orchestration:**
Any MOSAIC orchestration protocol rule communicated only in-conversation (a status-block/reporting requirement, a "don't stop for confirmation" directive, a memory-write trigger) is at risk of being silently unenforced after a compaction event, even though the instruction text is technically still present in the compacted context. The epistemic-marker-stripping variant is a further risk: MOSAIC conventions that rely on inline uncertainty/provenance markers (rather than files) could have that marker silently laundered into unmarked fact by a compaction summary.

**Evidence:**
- Original reporter documented 3 concrete rule categories (status block, memory writes, no-stop policy) dropping immediately after compaction across multiple sessions on one project, with the compaction summary itself confirmed (by inspection) to still contain the user's original instruction text.
- Multiple independent commenters corroborate the same failure class across different projects/environments: rule-positioning analysis (rules anchored in CLAUDE.md/system-level instructions survive; rules established only via mid-conversation user messages do not), a second report of "complete context drop" with cascading hallucinations after compaction, and a third-party account of compaction stripping bracketed uncertainty markers and re-surfacing a guess as confident fact.
- No maintainer response anywhere in the 14-comment thread as of 2026-09-07; this is community-diagnosed, not maintainer-confirmed. Confidence capped at Likely rather than Confirmed for that reason.
- Issue remains open, not stale-bot closed, with continued active discussion through September 2026.

**Workaround(s):**
1. ⭐ Put durable rules in CLAUDE.md (or an equivalent project instructions file), not just in-conversation instructions — CLAUDE.md/system-level content is re-injected into the system prefix on every turn and survives compaction, whereas mid-conversation behavioral refinements have no structured re-injection path through the compaction boundary. Confirmed as the most durable fix by multiple independent commenters.
2. Write anything that must survive to a file the moment it is established, rather than relying on it staying "active" in conversation state — one commenter's practice is treating compaction summaries as untrusted output entirely, keeping a small frequently-rewritten session brief file plus a thin CLAUDE.md index, with an explicit rule that uncertainty/provenance markers only survive in files, never in in-conversation summaries.
3. Keep sessions leaner (prune redundant tool outputs/oversized file echoes before compaction fires) to reduce how aggressively the summarizer needs to compress, which several commenters report reduces (but does not eliminate) the rate of dropped behavioral rules.

**Notes:**
This is a well-corroborated, long-running community-diagnosed defect (open since 2026-06-11, still active as of 2026-09-07) with no maintainer acknowledgment and no fix shipped. Cross-reference CC-006, which covers a related but distinct and more severe compaction-integrity failure (fabricated adversarial instructions surviving compaction) — both point at compaction/summarization as a systemic reliability weak point for MOSAIC, reinforcing that durable protocol rules must live in files, not conversation state.

---

### CC-008: One MCP tool with a non-object/boolean schema silently drops ALL tools from that server, on every transport

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anthropics/claude-code/issues/88049 (HTTP transport, primary); https://github.com/anthropics/claude-code/issues/92900 (stdio transport twin); https://github.com/anthropics/claude-code/issues/82949 (Claude Desktop, stale-closed but not fixed; re-filing of the even older #50194) |
| **Reported** | 2026-08-19 (#88049); 2026-09-08 (#92900); 2026-07-31 (#82949); original #50194 dates to April 2026 |
| **Last Activity** | 2026-09-08 |
| **Confidence** | Confirmed |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Reproduced on CLI 2.1.234 (HTTP transport, Windows) and CLI 2.1.220 / Claude Desktop 1.24012.9 (macOS); the Desktop variant has reproduced since at least April 2026 (#50194) despite an earlier attempted fix |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | bug, has repro, platform:windows, area:mcp (on #88049); stale (on #82949, closed `not_planned`) |

**Summary:**
A single MCP tool whose advertised `inputSchema` is malformed in either of two related ways — (a) a top-level schema that is `anyOf`/`oneOf` with no `"type": "object"` (HTTP and stdio transports), or (b) a boolean schema (`true`/`false`) sitting at a named property, e.g. `properties.model = true` (Claude Desktop) — causes the client to silently discard **every** tool from that MCP server, not just the offending one. On HTTP/stdio this fails with zero diagnostics on either side: the server reports a successful `tools/list` (200, full catalog), the CLI reports the server "connected," and `ToolSearch` simply returns no matches for any tool the server owns — indistinguishable from the server being down except that both sides claim otherwise. On Desktop the same blast radius occurs but at least throws a logged `TypeError: Cannot use 'in' operator to search for 'properties' in true` in `main.log`, tracing to `jsonSchemaToZodShape`/`createSdkServer` mapping over all tools inside one `Promise.all` with no per-tool isolation — so one bad schema's exception drops the whole batch. The Desktop variant is a recurrence: an earlier issue (#50194, April 2026) was closed as "completed" but reproduces again, because that fix targeted the wrong root cause (`additionalProperties: true`) rather than the actual offender (a boolean schema at a named property).

**Impact on Orchestration:**
Any MCP server MOSAIC depends on can be entirely and silently disabled by a single malformed tool schema anywhere in that server's catalog, with no error surfaced on the HTTP/stdio transports MOSAIC is most likely to use for its own tool servers. This can look like a fully working MCP connection ("connected," normal handshake) that has actually lost its entire tool surface — an orchestrator or subagent relying on that server's tools would get "no matching tool" failures with no indication the server itself is at fault, wasting significant debugging time (the original HTTP reporter says they "lost several hours... chasing a payload-size theory").

**Evidence:**
- #88049 (HTTP): detailed bisection with byte-exact measurements proving the failure is schema-shape-triggered, not size-related (an 8.3KB/3-tool catalog with one offending schema ingests 0 tools; the same catalog with one field added — `,"type":"object"` — ingests all tools). Labeled `has repro`.
- #92900 (stdio): independent reporter confirms "the same all-or-nothing blast radius" via an independent bisection, with a boolean-property-schema trigger (matching the Desktop mechanism) rather than the HTTP top-level-anyOf trigger — i.e. the same underlying "no per-tool isolation in schema conversion" defect manifests identically across all three transports.
- #82949 (Desktop): full stack trace pinpointing the exact code path (`jsonSchemaToZodShape` → `createSdkServer` → `Array.map` inside `Promise.all`, no try/catch) and confirms this is a *second* occurrence of a defect from an earlier issue (#50194) whose fix addressed the wrong root cause.
- No explicit maintainer acknowledgment found on any of the three current issues, but Confirmed confidence is warranted per the multiple-independent-reproduction-with-strong-evidence criterion: three separate reporters, three transports, concrete bisected repro steps and stack traces, and a documented history of a prior (incomplete) fix attempt showing Anthropic has previously engaged with this defect class.
- #82949 was stale-bot closed `not_planned` — NOT because it was fixed; the underlying mechanism still reproduces per the newer #88049/#92900 reports.

**Workaround(s):**
1. ⭐ Audit and control every MCP server MOSAIC connects to for schema shape: ensure every tool's `inputSchema` has `"type": "object"` at the top level (hoist it onto `anyOf`/`oneOf` unions whose arms are all object schemas — a working patch is given in #88049), and avoid boolean (`true`/`false`) values at any named property — replace with `{}` (equivalent to `true`) or an explicit schema. This is the fix the HTTP reporter applied and confirmed working (0 tools → full catalog ingested).
2. If a third-party/uncontrolled MCP server exhibits this, there is no client-side workaround beyond removing/patching the offending tool server-side — the client gives no diagnostic to locate the offending tool short of bisecting the catalog by hand (remove tools one at a time until the rest reappear).

**Notes:**
This is a genuine "silent total tool-loss" hazard for any MOSAIC-controlled MCP server, and the fix is entirely within MOSAIC's control (schema hygiene on servers MOSAIC authors). For third-party servers, this is a standing risk with no detection mechanism other than noticing a server's tools are unexpectedly all missing. Given the recurrence pattern (#50194 → #82949 → #88049/#92900 across ~5 months with an intervening incomplete fix), treat as an ongoing, not-fully-resolved defect class rather than a one-off bug; re-verify if a future Claude Code release changelog mentions per-tool MCP schema isolation or JSON Schema boolean/union handling.

---

### CC-009: Custom subagent's `tools: Bash` frontmatter silently never grants Bash access

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anthropics/claude-code/issues/92820 |
| **Reported** | 2026-09-08 |
| **Last Activity** | 2026-09-08 |
| **Confidence** | Unverified |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 2.1.263 |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | bug, platform:windows, area:agents |

**Summary:**
A custom subagent defined in `.claude/agents/*.md` with `Bash` listed in its `tools:` frontmatter never actually receives that tool — every other declared tool (Read, Write, Edit, Grep, Glob) is granted correctly, but Bash is silently omitted with no error/denial message. The reporter ruled out naming mismatch (renamed to the platform's actual shell-tool name, no change), permissions/approval gaps (explicit `permissions.allow` covering the exact command patterns, no change), sandbox settings (none configured anywhere), and VS Code-extension-specific behavior (reproduced identically on standalone CLI). The general-purpose agent type is confirmed unaffected — it receives `*` (all tools) normally.

**Impact on Orchestration:**
Directly threatens frontmatter-scoped subagent shell access, which MOSAIC relies on for delegating Bash-capable work to scoped subagents. In the reporter's real project, 2 of 6 custom subagents were rendered completely non-functional as native subagent types.

**Evidence:**
- Single reporter (author_association: NONE), 0 comments, no maintainer response as of last activity — this is genuinely unconfirmed by anyone else.
- Elimination testing is unusually rigorous (four alternate explanations tested and ruled out with concrete steps), which is why it's still tracked despite Unverified confidence — it meets the bar of threatening a core MOSAIC pattern (subagent tool scoping) with concrete repro steps on stock Anthropic infrastructure.

**Workaround(s):**
1. ⭐ From the reporter: have the orchestrator pre-compute whatever the subagent would have needed shell access for (e.g., run `git diff` itself and hand the subagent a pre-computed file list) where possible.
2. From the reporter: where pre-computation isn't possible (e.g., running actual tests), fall back to dispatching via the `general-purpose` agent type fed the same instructions — it is not affected by this restriction and receives all tools (`*`) normally. This works but defeats the purpose of a scoped, single-responsibility custom subagent.

**Notes:**
Flagged as high-priority for corroboration by a second independent report — no second report found yet as of this backfill pass (2026-09-09, one day after filing). Re-check on a future pass for maintainer engagement or corroborating reports.

---

### CC-010: A `PreToolUse` hook returning `permissionDecision: "defer"` permanently strands a subagent's Bash call, which then falsely reports "completed"

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anthropics/claude-code/issues/86696 |
| **Reported** | 2026-08-14 |
| **Last Activity** | 2026-09-05 |
| **Confidence** | Confirmed |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 2.1.229 - 2.1.233+ (root cause confirmed present through at least 2.1.233) |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | bug, has repro, platform:linux, area:tools, area:bash, area:agents, regression, stale |

**Summary:**
When a `PreToolUse` hook returns `permissionDecision: "defer"` for a subagent's Bash call, Claude Code honors the defer non-interactively (single-tool-call-in-flight case) but never resumes it — the call never gets a `tool_result`, the subagent transcript simply stops, yet the run reports `subtype: "success"`, `is_error: false`, and the task notification says `completed`, with `terminal_reason: "tool_deferred"` as the only machine-readable trace. The reporter's root cause was their own hook mis-using `defer` as a "no opinion, pass through" signal; a maintainer (bcherny, COLLABORATOR) could not reproduce initially, then confirmed after the reporter isolated the cause that the false-success reporting ("`subtype: success` with `is_error: false` on a run whose `terminal_reason` is `tool_deferred`") is "something you would want to fix" — i.e., acknowledged as worth fixing, though no fix PR is linked yet.

**Impact on Orchestration:**
Any hook-based permission gating that returns (or accidentally emits) `permissionDecision: "defer"` on a subagent Bash call produces a stuck call that is then misreported as a successful completion — a subagent can appear to finish real shell work when nothing actually ran. `defer` is intended for external programs driving `claude -p` and resuming the session later, but a subagent sidechain is not resumable the normal way, making a deferred subagent call effectively unrecoverable in practice per the reporter.

**Evidence:**
- Maintainer (COLLABORATOR) engaged directly, initially could not reproduce, then confirmed the reporter's isolated root cause and explicitly called the false-success behavior something the team would want to fix.
- Reporter provided a minimal, deterministic repro (a `.claude/settings.json` `PreToolUse` hook on `Bash` that emits `permissionDecision: "defer"`, then spawning a subagent to run a Bash command).
- Issue carries a `stale` label as of last activity (2026-09-05) — no confirmation yet that this is fixed on a current version; flag for re-verification.

**Workaround(s):**
1. ⭐ From the reporter (confirmed effective): audit all `PreToolUse` hooks (including plugin-provided ones) for any path that can emit `permissionDecision: "defer"`, and never use `defer` as a "pass-through/no-opinion" signal — for pass-through, print nothing and exit 0 instead.
2. Restrict subagents to non-Bash tools when using hooks whose defer behavior can't be fully audited (from #86696, credited to the closely-related #86696 thread's own workaround note).

**Notes:**
The issue carries a `stale` label — GitHub Actions auto-commented asking for repro before the reporter's follow-up landed; the thread stayed open after the reporter's detailed isolation comment, but no maintainer has confirmed a fix or closed it as of 2026-09-05. Treat as "Needs re-verification" on current versions (v2.1.267) in a future pass.
Reporter also noted this is distinct from two nearby issues initially suspected as related — #86471 (background subagents reporting `completed` with empty/partial results, proposed context-size cliff) and #86673 (safety-classifier unavailability blocking tool calls) — and stated this one should not be merged with #86673.

---

### CC-011: Agent-definition `disallowedTools` is not enforced on further subagents dispatched with an unspecified/default `subagent_type`

| Field | Value |
|-------|-------|
| **Classification** | Limitation |
| **Source** | https://github.com/anthropics/claude-code/issues/78063 |
| **Reported** | 2026-07-16 |
| **Last Activity** | 2026-08-23 |
| **Confidence** | Confirmed (maintainer confirmed behavior and intent; disputed as "not a bug" but explicitly kept open) |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 2.1.209 - 2.1.241 (confirmed reproducible through 2.1.241, the most recent test in-thread) |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | bug, documentation, has repro, platform:linux, area:agents, area:permissions, reproduced |

**Summary:**
`disallowedTools` set on an agent definition (`--agents` config or `.claude/agents/*.md` frontmatter) only shapes that agent's own tool list — it is not a permission rule and is not inherited by subagents it further dispatches via the `Agent` tool, which get their tool set independently. A maintainer (bcherny, COLLABORATOR) confirmed this is "how it's currently designed" on 2.1.233 while agreeing it's confusing, pointed to the correctly-enforced alternative (`permissions.deny` / session-level `--disallowedTools` flag, which a second reporter confirmed DOES propagate to subagents on 2.1.241), and left the issue open to track possibly making the agent-level field cascade and clarifying the docs. All reproductions in the thread leave `subagent_type` unspecified/default in the further `Agent` tool call — this issue is narrowly scoped to unnamed/default further-dispatch, distinct from CC-031 (explicitly-named `subagent_type` dispatch).

**Impact on Orchestration:**
A real security-scoping gap: a subagent that itself dispatches further subagents without specifying `subagent_type` can have its own `disallowedTools` restriction bypassed by the child, even though the design is intentional per the maintainer. MOSAIC subagents that rely on agent-definition `disallowedTools` (rather than session/permission-level deny rules) as a security boundary for further/nested dispatch are exposed to this gap.

**Evidence:**
- Maintainer (COLLABORATOR) explicitly confirmed the behavior and its by-design rationale in-thread, reproduced it themselves on 2.1.233, and left the issue open specifically to track a possible design change plus doc clarification — this is as strong as Limitation confidence gets.
- A second independent reporter reproduced the same divergence on 2.1.241, isolating that the CLI-level `--disallowed-tools` flag IS obeyed by subagents while the agent-definition `disallowedTools` field is NOT — corroborating both the bug's scope and the maintainer-suggested workaround.

**Workaround(s):**
1. ⭐ Maintainer-confirmed and independently re-verified on 2.1.241: use `permissions.deny` in settings, or the session-level `--disallowed-tools` / `--disallowedTools` CLI flag, instead of (or in addition to) agent-definition `disallowedTools` — these ARE enforced on subagents, including ones spawned without an explicit `subagent_type`.
2. Maintainer-suggested: add `Agent` itself to the agent's `disallowedTools` to prevent it from spawning further subagents at all, or scope which subagent types it may spawn via `Agent(agent_type)`.
3. Audit trick from the second reporter: a tool call's non-null `parent_tool_use_id` reliably marks it as "born inside a subagent" — useful for after-the-fact auditing of whether a nested dispatch actually stayed confined.

**Notes:**
Kept active (NOT moved to resolved as "not a bug") because intended-by-design does not mean it's not an operational hazard for MOSAIC — it's a real security-scoping gap, and the maintainer's own framing ("we agree it's confusing... leaving this open to track that") supports keeping it tracked rather than discarding it.
RECONCILIATION (vs CC-031, done in a later pass): re-read both full issues — these are DISTINCT, not overlapping. CC-011 is narrow (unnamed/default `subagent_type` only); CC-031 is about explicitly-named `subagent_type` dispatch. Kept as separate entries.

---

### CC-012: A stale `ToolSearch` tool reference to a tool that left the pool mid-session permanently bricks the session with a 400 error

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anthropics/claude-code/issues/78485 |
| **Reported** | 2026-08-18 (approx., inferred from corroborating comment dates — original report date not directly captured) |
| **Last Activity** | 2026-09-08 |
| **Confidence** | Confirmed |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 2.1.206, 2.1.226 (still present, per corroborating comments) |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | bug, has repro, platform:macos, area:tools, platform:vscode, area:plugins, stale |

**Summary:**
When `ToolSearch` is active, results persist `tool_reference` blocks in session history. If a referenced tool later leaves the pool mid-session, every subsequent request (including a plain "continue" and `/compact`) replays the stale reference and the API rejects the entire request with `400 Tool reference 'X' not found in available tools` — the session is permanently bricked with no in-product recovery. Three independent trigger mechanisms are documented across the original report and two corroborating comments: (1) a plugin-provided tool (e.g., an LSP server) dying/deregistering mid-session, (2) resuming with `--remote-control` or after an MCP connector is removed account-side (different tool roster than what was cached), and (3) a mid-session model switch — built-in task tools (`TaskCreate`/`TaskUpdate`/etc.) were withdrawn by default on newer models around 2026-08-15, so a conversation that used them under an older model and continues under a newer one hits the same mechanism. No maintainer has responded in-thread as of last activity.

**Impact on Orchestration:**
A session using deferred/`ToolSearch`-loaded tools can be permanently killed mid-workflow by ordinary events — a flapping plugin tool, an MCP connector change, or a mid-session model switch — with no recovery short of manual transcript surgery or starting a new session. Directly threatens any MOSAIC session that uses `ToolSearch`-loaded deferred tools across a long-running or model-switching workflow.

**Evidence:**
- Three independent reporters (original + two corroborating comments) confirm the same failure signature (`400 Tool reference '<name>' not found in available tools`) across three distinct trigger mechanisms, on different Claude Code versions (2.1.206, 2.1.226) and platforms (macOS, Linux).
- One corroborator ran the exact detection predicate (a `tool_reference` whose `tool_name` is absent from the request's own `tools` array) across two days of proxied production traffic: 1,530 requests carried `tool_search_tool_result` blocks, exactly 5 matched the predicate, and those were exactly the 5 that returned 400 — no false positives, strong signal the mechanism is correctly understood.
- No maintainer acknowledgment yet; issue carries a `stale` label as of last activity (2026-09-08).

**Workaround(s):**
1. ⭐ From the reporter and a corroborator (both confirmed effective): manual transcript surgery — in the session `.jsonl`, empty the `tool_references` array inside affected `tool_search_tool_result` blocks (this is stored search output, not conversation content, so nothing is lost), then resume. One corroborator adds: if a block ends up with zero references, drop the whole block and its paired `server_tool_use` together, or you trade this 400 for an "unanswered server_tool_use" validation error instead.
2. `ENABLE_TOOL_SEARCH=false` prevents `tool_reference` blocks from being stored at all (avoids the failure mode entirely, at the cost of losing `ToolSearch`).
3. Disable/avoid the specific flapping plugin or MCP connector causing tools to drop from the pool, where identifiable.
4. Precautionary: avoid mid-session model switches when deferred (`ToolSearch`-loaded) tools are in play, since a model switch can silently withdraw built-in tools from the roster and trigger this.

**Notes:**
No fix PR linked yet; issue is `stale`-labeled with no maintainer engagement — flag for re-verification against v2.1.267 in a future pass. The client is documented as already doing partial cleanup for some withdrawn-tool cases (rewriting `tool_result` blocks to `"[Tool references removed - tools no longer available]"`) but this does not extend to `tool_search_tool_result` blocks, which is the actual failure surface here.

---

### CC-013: `permissions.allow`/`bypassPermissions` intermittently ignored, worse for subagent-issued tool calls than primary-agent calls

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anthropics/claude-code/issues/83421 |
| **Reported** | 2026-08-02 |
| **Last Activity** | 2026-08-02 |
| **Confidence** | Unverified |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 2.1.220 |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | area:agents, area:permissions |

**Summary:**
The original report claimed `bypassPermissions` mode does not propagate to Task/Agent subagents at all, causing every subagent Bash/Read call to prompt for permission. The reporter's own systematic follow-up **could not reproduce this in four clean-room variants** (headless via flag, headless via settings.json `defaultMode`, nested sub-subagent spawn, interactive TUI named-agent spawning a nested subagent) — all inherited `bypassPermissions` correctly with zero prompts. Re-examining the original incident's actual transcripts, the reporter found the real behavior is narrower and different: the parent session stayed in `bypassPermissions` throughout (281 transcript records, zero mode transitions) and ~70 direct subagents ran unprompted all day, but exactly one **nested** subagent (a grandchild — spawned by a named "teammate" agent) ran fine for ~5.5 minutes then began prompting on every subsequent call, with one command taking 38 minutes to return; `TaskStop` on the intermediate agent ended the prompting. This looks like intermittent, state-dependent loss of inherited permission context specific to nested subagents in long-running/agent-teams sessions, not a blanket inheritance failure.

**Impact on Orchestration:**
If MOSAIC uses nested subagent dispatch (a subagent that itself dispatches further subagents) in long-running sessions, especially with experimental agent-teams-style features, there is a documented (if not yet independently corroborated) risk that permission-mode inheritance can silently degrade mid-run for the nested grandchild, causing it to stall on prompts nobody is present to answer.

**Evidence:**
- Single reporter. The reporter's own rigorous clean-room testing walked back the original broad claim — this is exactly the kind of self-correction the CaptureGuide flags as weakening confidence for the original framing, but the narrowed claim (nested-subagent-specific, state-dependent degradation) is still evidenced by real incident transcript forensics (permission-mode record counts, call-timing degradation, `TaskStop` resolving it).
- No maintainer response as of last activity (2026-08-02); no independent corroboration of the narrowed claim found.
- Reporter connects this to three other issues as possibly the same family: #70143 (background-subagent prompts route to main session instead of auto-denying), #78487 (unanswered prompt holds a background agent open indefinitely), #73633 (Workflow subagents not inheriting `permissions.allow`) — not yet cross-checked in this pass.

**Workaround(s):**
1. Unknown — no workaround identified in-thread; the reporter's fix for the specific incident was `TaskStop` on the stalled intermediate agent, which is a recovery action rather than a preventive workaround.

**Notes:**
Confidence downgraded from the reconstructed guess's "Confirmed" to "Unverified" — the original broad claim ("bypassPermissions doesn't propagate to subagents") does NOT reproduce in clean conditions per the reporter's own testing, and the real, narrower claim (nested/long-running-session-specific degradation) has only one reporter and no maintainer engagement. Kept active despite Unverified confidence because it threatens a core MOSAIC pattern (nested subagent dispatch under bypass permissions) with concrete forensic evidence, though not deterministic repro steps. Needs re-verification and, ideally, a maintainer response or independent corroboration in a future pass. Related issues #70143, #78487, #73633 worth checking in a future pass for overlap.

---

### CC-014: `PreToolUse` `ask` decision / `permissions.ask` config is silently not enforced

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anthropics/claude-code/issues/87639 |
| **Reported** | 2026-08-18 |
| **Last Activity** | 2026-08-26 |
| **Confidence** | Likely |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 2.1.228 - 2.1.234 |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | bug, has repro, platform:linux, area:hooks, area:permissions |

**Summary:**
In an **interactive session running in auto mode** (`--permission-mode auto` or `permissions.defaultMode: "auto"` in settings), both a `permissions.ask` config rule and a `PreToolUse` hook returning `permissionDecision: "ask"` are silently not enforced — no permission prompt is shown, and the gated command (e.g. `git add`, `git commit`, `git push`) runs anyway. The same rules gate correctly in a `-p`/print-mode run on the same build, and switching the same interactive session to Manual mode (Shift+Tab) makes the exact same rule fire a prompt immediately — isolating the failure specifically to "interactive + auto mode". A second, independent reporter reproduced the same bypass on an earlier build (2.1.228, predating the original reporter's 2.1.234) via the settings-based `defaultMode` path rather than the CLI flag, and with exact-match rule syntax in addition to prefix-match — ruling out both the specific entry path and the rule-syntax form as the cause.

**Impact on Orchestration:**
`permissions.ask` is the documented way to keep a human checkpoint while running in auto mode; if it can be silently skipped in exactly that mode, the only durable boundary left is `permissions.deny` (a hard block, not a checkpoint). Any MOSAIC workflow relying on auto-mode ask-rules or ask-returning hooks as a human-in-the-loop safety checkpoint for consequential actions is not actually gated in interactive sessions.

**Evidence:**
- Two independent reporters, on two different Claude Code versions (2.1.228 and 2.1.234) and two different auto-mode entry paths (settings `defaultMode` vs. `--permission-mode` flag), reproduced the identical symptom with concrete before/after repro steps (including an in-session mode toggle that isolates the cause to "interactive auto mode" specifically).
- No maintainer response in-thread as of last activity (2026-08-26) — this keeps confidence at Likely rather than Confirmed per the CaptureGuide (multiple independent reports of the same symptom, no maintainer confirmation).

**Workaround(s):**
1. ⭐ From the reporter (confirmed effective): move the affected rule(s) from `permissions.ask` to `permissions.deny`. This is enforced correctly even in interactive auto mode, at the cost of being a hard block rather than a human checkpoint.
2. Avoid `--permission-mode auto` / `defaultMode: "auto"` for interactive sessions where an `ask` checkpoint is safety-relevant; use `-p`/print mode (where `ask` rules were confirmed to work) or Manual mode instead.

**Notes:**
Needs re-verification against current version (v2.1.267) in a future pass — no confirmation either way since 2026-08-26.

---

### CC-015: A killed/crashed `PreToolUse` hook fails OPEN instead of closed

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anthropics/claude-code/issues/84302 (closed, not_planned, stale-bot auto-close) |
| **Reported** | 2026-08-05 |
| **Last Activity** | 2026-09-07 (auto-closed by stale-bot) |
| **Confidence** | Unverified |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Unknown — not stated in issue |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | stale (only label; issue has no `bug` label) |

**Summary:**
A `PreToolUse` command hook killed by the CLI's own hook timeout (~10 minutes observed for hooks with no explicit `timeout` configured) results in the gated tool call being **allowed** — a fail-OPEN for the exact case where the hook could not finish deciding. The reporter hit this in production-like usage: an approval-card hook for a "publish site publicly" tool sat unanswered, was killed at ~10 minutes, and the publish executed without consent. The reporter explicitly noted they had since moved their approval logic inside MCP tools (fail-closed by ordering) and were "not blocked" by this — filing purely as a footgun report. **Correction to the reconstructed pre-backfill note:** the "fix PR" #84364 does NOT target this issue's actual mechanism. Its title is "fix(hookify): fail closed on exceptions in pretooluse hook" — it patches a third-party community plugin called `hookify` (referenced independently in issue #86696's comments as a plugin some users install) to fail closed on Python exceptions during rule evaluation. It does not touch the CLI's own hook-timeout-kill behavior that #84302 actually reports, and it was opened by a non-maintainer, non-collaborator contributor (author_association: NONE). The underlying issue #84302 itself was closed automatically by the stale-bot on 2026-09-07 as `not_planned` — no maintainer ever engaged with it.

**Impact on Orchestration:**
Any safety/permission gate implemented via `PreToolUse` hooks — including ones with a long-running, out-of-band, or human-in-the-loop approval step — can be silently bypassed if the hook is killed by the ~10-minute timeout, undermining hook-based guardrails MOSAIC may depend on for consequential tool calls.

**Evidence:**
- Single reporter, no maintainer engagement, no independent corroboration found. The issue was closed by an automated stale-bot for inactivity, not resolved or dismissed by a maintainer.
- The originally-recorded "linked fix PR" turns out on inspection to fix a different, plugin-level bug (`hookify`'s own exception handling), not the CLI's hook-timeout-kill behavior — this is a meaningful correction from the pre-backfill reconstructed state, which had assumed the PR was on-point and used it to justify Confirmed confidence.
- Related, complementary issue #82323 ("A PreToolUse hook can be inert with no signal anywhere") independently documents a related but distinct fail-open path — a missing/unrunnable hook script exits non-zero-but-not-2 and the tool call proceeds silently, with no signal in `/doctor` or `/hooks` — corroborating that fail-open-on-hook-malfunction is a real, broader pattern in the hooks subsystem, even though it doesn't directly confirm the killed/timed-out-hook variant.

**Workaround(s):**
1. From the reporter: move approval/permission-gating logic inside MCP tools themselves (fail-closed by ordering) rather than relying on a `PreToolUse` hook's `permissionDecision` protocol for long-running or out-of-band approvals.
2. From the related issue #82323: monitor hook execution independently — e.g., have the hook itself write a receipt on every invocation (including no-op decisions) rather than trusting `/hooks`' "configured" view, since configured and effective are different properties and nothing in the product surfaces a killed/inert hook.

**Notes:**
Confidence downgraded from the reconstructed guess's "Confirmed" to "Unverified": the reconstructed entry's confidence rested on a "linked open fix PR", which on inspection targets a different (plugin-level) bug, not this one — and the actual underlying issue (#84302) has zero maintainer engagement and was closed by stale-bot. Kept active despite Unverified confidence because it threatens a core MOSAIC pattern (hook-based permission gating fail-open under a plausible, ordinary trigger — a slow/unanswered approval) with concrete reproduction steps on stock Anthropic infrastructure. Re-verify in a future pass whether this reproduces on current versions, and whether PR #84364 (still open/unmerged as of this backfill) ever gets connected to a genuine CLI-level fix.

---

### CC-016: Background subagents are denied outright on Write/Bash tool calls regardless of the parent session's permission mode

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anthropics/claude-code/issues/87994 |
| **Reported** | 2026-08-19 |
| **Last Activity** | 2026-09-08 |
| **Confidence** | Unverified |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 2.1.232+ (regression introduced by the "non-teammate agent spawns in interactive sessions now run in background by default" change); confirmed on 2.1.234, Windows |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | bug, has repro, platform:windows, area:agents, area:permissions, stale |

**Summary:**
Since Claude Code made non-teammate agent spawns background-by-default (v2.1.232), gated Bash commands (e.g. `git fetch`, `gh`) executed inside a background subagent are silently auto-denied with the literal tool result "This command requires approval" — instead of surfacing an interactive prompt — because a backgrounded subagent structurally cannot prompt the user. There is no queue, retry, or user-visible notification; the calling agent can silently substitute a wrong target and report high-confidence results without indicating the denial occurred. The reporter confirmed root cause by testing `CLAUDE_CODE_DISABLE_BACKGROUND_TASKS=1`, which fixed the behavior when invoked from an interactive session (forcing foreground execution let the calls actually prompt/succeed), though a separate VS Code "local command" execution path did not pick up the same settings change without a restart.

**Impact on Orchestration:**
Background/unattended subagent dispatch — a core MOSAIC pattern — cannot reliably perform gated Bash operations (and, per the original title, Write calls under the same code path) even when the user would have approved them; the failure is silent and can produce confidently-wrong output rather than a visible error, which is worse for automated pipelines than an explicit failure.

**Evidence:**
- Single reporter (NONE association), no maintainer response; labeled `stale` (no recent activity).
- Reporter did their own two-path confirmation: disabling all backgrounding via `CLAUDE_CODE_DISABLE_BACKGROUND_TASKS=1` fixed the interactive-session repro, isolating the cause to the background-execution path itself rather than the specific command or allowlist.
- Reporter explicitly cross-references a closed, "not planned" precedent (#36042, "Background sub-agents silently blocked by permission prompts") and notes this now hits a first-party bundled command (`/code-review`), not just custom MCP tools — raising the stakes versus that prior won't-fix.
- A related but distinct issue, #83421 ("bypassPermissions permission mode does not propagate to Task/Agent subagents"), was investigated by its own reporter and walked back: a clean-room repro did NOT reproduce simple non-propagation: bypass mode does inherit correctly, including one level of nesting, in clean conditions. That reporter's real incident turned out to be an intermittent, state-dependent degradation specific to long-running nested "agent teams" sessions — a different mechanism than CC-016's background-auto-deny path. Kept separate rather than merged given the different root causes described.

**Workaround(s):**
1. ⭐ Set `CLAUDE_CODE_DISABLE_BACKGROUND_TASKS=1` to force foreground execution for background-eligible subagents, letting gated commands actually prompt (or otherwise not silently fail closed) instead of auto-denying. Confirmed by the reporter to fix the interactive-session case. Caveat: this is a blunt, global setting — it disables all backgrounding (fire-and-forget agents, concurrent review agents, long-running background Bash), not just the permission-prompt gap, and the reporter found it did not take effect in a VS Code "local command" execution path without a restart.
2. Treat any subagent-reported result following a backgrounded gated-command step with suspicion; have the calling flow explicitly check for "requires approval"/denial tool results rather than trusting a confident-looking final answer.

**Notes:**
No maintainer engagement to date; issue auto-labeled `stale`. Confidence set to Unverified (not Confirmed as originally reconstructed) since there is no maintainer acknowledgment and only a single reporter, despite that reporter's unusually thorough two-path self-verification. Related: #36042 (closed, not planned — narrower precedent); #83421 (investigated, but determined to be a different, nested-subagent-specific mid-run degradation, not the same mechanism — see CC entry there if added). Should be re-verified on a current Claude Code version given staleness.

---

### CC-018: A self-backgrounding Bash command under `run_in_background` reports false-complete, orphans the process, can cause silent double-dispatch

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anthropics/claude-code/issues/93126 |
| **Reported** | 2026-09-09 |
| **Last Activity** | 2026-09-09 |
| **Confidence** | Unverified |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 2.1.266 (confirmed); reporter notes the underlying mechanism is deterministic and reproducible |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | bug, has repro, platform:linux, area:bash |

**Summary:**
A `Bash` tool call made with `run_in_background: true`, whose command backgrounds its own child (e.g. ends in `&` or uses `nohup ... &`), is recorded as finished the instant the wrapper shell exits — not when the real work completes. The spawned process keeps running untracked: it does not appear in `/tasks`, has no CPU accounting, and is not reaped at session end. Believing the work done, the agent can dispatch the same work again, causing overlapping duplicate runs.

**Impact on Orchestration:**
Background Bash-based orchestration steps (a core MOSAIC pattern for long-running work) risk reporting success prematurely while an orphaned process continues running unmonitored, and risk the same work being dispatched twice — the reporter observed exactly this with an overlapping duplicate test-suite run that saturated all CPU cores and correlated with session interruptions.

**Evidence:**
- Single reporter (NONE association), no maintainer response yet (issue is 0 days old at time of this pass).
- Concrete, minimal, easily-reproducible repro steps provided (`nohup sleep 600 > /dev/null 2>&1 & echo "started, PID $!"` with `run_in_background: true`), plus a real-world reproduction (overlapping vitest runs) with process-level evidence (`pgrep`, CPU/load figures).
- Labeled `has repro` by the tracker's own triage.
- Reported on stock Anthropic infrastructure (Linux, standard Claude Code 2.1.266, no custom backend).
- Related issue filed same day by same reporter: #93127 (session interruption correlation — separable, not tracked here).

**Workaround(s):**
1. ⭐ Avoid issuing `run_in_background: true` Bash commands whose own command text self-backgrounds (trailing `&`, `nohup ... &`, `setsid`, daemonizing CLIs). Let the harness's own backgrounding handle it instead of nesting another layer of backgrounding inside the command. (Derived from the reporter's suggested fix; not yet confirmed by a maintainer.)
2. When a self-backgrounding command is unavoidable, independently verify actual completion (e.g. `pgrep`/`ps` for the spawned PID) rather than trusting the tool-call "completed" status before dispatching further work.

**Notes:**
Issue is brand new (filed 2026-09-09, same day as this backfill pass) — no maintainer engagement yet, needs re-verification in a future pass once it has had time to accrue confirmations or a maintainer response. Related: #93127 (secondary/correlated issue by the same reporter, not tracked separately here as it's about session interruption diagnostics rather than orchestration risk).

---

### CC-019: A background Bash task gets killed ~17-20 seconds after arming when armed as the turn's last tool call

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anthropics/claude-code/issues/87948 |
| **Reported** | 2026-08-19 |
| **Last Activity** | 2026-09-08 |
| **Confidence** | Unverified |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 2.1.231 (Linux, terminal CLI entrypoint, git worktree session) |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | bug, has repro, platform:linux, area:core, area:bash, stale |

**Summary:**
`Bash` tool calls with `run_in_background: true` are intermittently killed by the client ~17-20 seconds after being armed — reported as `killed` ("Background command … was stopped") — with no `timeout` set, no user stop action, and no session restart. Reporter's transcript forensics show every killed task was armed as the last tool call before its turn ended, with the kill landing 0.5-7s after turn finalization; a replacement task armed in the next turn was killed the same way. Reporter theorizes a race in turn-finalization cleanup sweeping up recently-armed background children.

**Impact on Orchestration:**
Long-running background Bash steps dispatched as the final action of a turn risk being killed prematurely before completing meaningful work — silently, since the kill notification can be indistinguishable from normal completion signaling in an automated pipeline. Directly relevant to MOSAIC's background-dispatch patterns for long-running orchestration steps.

**Evidence:**
- Single reporter (NONE association), no maintainer response and no other user confirmation in comments (issue has 0 comments).
- Very strong self-forensics: a timing table across 7 kills in one session, explicit ruling-out of user action, session restart, interrupts/slash-commands, script content, and the documented 120s foreground timeout as causes.
- Reporter explicitly distinguishes this from known related issues (#72851, #68625 — Desktop idle/lock teardown after 5-30 min; #25188 closed — session-end cleanup) since this fires in an active terminal CLI session, seconds (not minutes) after arming.
- Labeled `has repro` by tracker triage; also auto-labeled `stale` (no activity for several weeks) — needs re-verification on a current version.

**Workaround(s):**
1. ⭐ Avoid arming a background Bash task as the very last tool call of a turn — issue at least one more (even trivial) tool call afterward, or explicitly wait/confirm before ending the turn. (Reporter's own workaround-in-use.)
2. Never place state-mutating commands inside a background task; use the harness's Monitor-style watch tooling for polling instead. (Reporter's own workaround-in-use.)

**Notes:**
No maintainer engagement to date; confidence capped at Unverified per validation rules (single reporter, no confirmations). Labeled `stale` by the tracker — flagged here as "Needs re-verification" on a more current Claude Code version before treating this as still-current. Related but distinct issues on the tracker: #72851, #68625 (Desktop idle/lock teardown, longer timescale), #25188 (closed, session-end cleanup).

---

### CC-020: Interactive-session LSP client never sends `didChange` after an Edit-tool write, freezing code-intelligence answers

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anthropics/claude-code/issues/79744 |
| **Reported** | 2026-07-21 |
| **Last Activity** | 2026-09-09 |
| **Confidence** | Unverified |
| **Orchestration Impact** | MEDIUM |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 2.1.216 (Ubuntu/Debian Linux, WSL); no fix confirmed on current version |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | bug, area:tools, area:plugins, stale |

**Summary:**
CORRECTION to the prior reconstructed entry: the source issue's own title and captured wire logs show the OPPOSITE of what this entry previously claimed. In an **interactive** session, a plugin-registered LSP server (`.lsp.json`) receives `textDocument/didOpen` on first query but the client then never sends `didChange`/`didSave` for any subsequent write — including Claude's own Edit-tool writes — so the server's buffer stays frozen at first-query content (one measured case: 40+ minutes stale) and every later LSP answer (hover, documentSymbol, findReferences, diagnostics) silently describes a past version of the file. Critically, the reporter's own side-by-side test shows **headless `claude -p` handles this correctly** — it sends a proper `didChange`(version incremented)/`didSave` and gets fresh answers on the identical scenario, same binary, same plugin, same edit. There is also no recovery path: a killed/crashed LSP server process leaves the interactive client's connection zombied, and `restartOnCrash` does not respawn it.

**Impact on Orchestration:**
The prior version of this entry justified keeping it as relevant on the claim that it was "reachable even via headless `-p` mode" — that claim is **false**; the source issue explicitly demonstrates `-p` mode is unaffected and only the interactive client is broken. However, per `README.md`, MOSAIC's "Interactive / harness-native" execution mode does run the orchestrator and its dispatched subagents inside one live interactive Claude Code session (as opposed to the Runner pipeline's headless `-p` subprocess mode) — so this bug IS reachable by MOSAIC when running in that mode, specifically for any subagent that queries an LSP tool (hover/diagnostics/documentSymbol/findReferences) after an Edit-tool write in the same session, if the project has a plugin-registered LSP server. The consumer of the stale data is the harness's own tool-call result (feeding back into the agent's reasoning), not merely a human's IDE display, which is what keeps this above the UI-only bar in the Relevance Litmus Test. It does NOT affect MOSAIC's Runner/headless pipeline mode at all.

**Evidence:**
- Single reporter (NONE association), no maintainer response, no other user confirmation in comments (0 comments). Labeled `stale`.
- Detailed reproduction with a transparent wire-logging proxy showing the exact LSP notification sequence for both interactive (missing `didChange`) and headless `-p` (correct `didChange`/`didSave`) sessions on the same machine/plugin/edit.
- Reporter cross-references related stale-diagnostics reports in other languages (#64239) and distinguishes this from a separate, different-mechanism issue (#30622, constant version in didChange) — here no `didChange` is sent at all in interactive mode.
- Reported on stock Anthropic infrastructure ("Platform: Anthropic API"), not a custom backend.

**Workaround(s):**
1. Reporter ships a language-specific fix for F#: a disk-syncing stdio proxy between the LSP client and the language server that re-reads changed files before each request (https://github.com/diegopego/claude-code-fsharp-lsp — `fsac_sync_proxy.py`). This only fixes the one language/server it wraps; every other `.lsp.json`-served language still has the frozen-buffer behavior. Not yet independently confirmed by other users.
2. For MOSAIC's interactive/harness-native mode: avoid relying on LSP-tool answers (hover/diagnostics/findReferences/documentSymbol) after an Edit-tool write within the same session; prefer Grep/Read-based verification of post-edit state, or start a fresh session/subagent dispatch when fresh LSP answers are required.

**Notes:**
The previous "AUDIT NOTE" claiming headless `-p` reachability was incorrect and has been removed — this backfill pass found the source issue explicitly contradicts it. The entry is kept active, but for a different and narrower reason: MOSAIC's interactive/harness-native execution mode (not its headless Runner pipeline) can reach this bug. Reclassified from Quirk to Bug (the behavior clearly contradicts the harness's own headless-mode behavior on the identical scenario, so it reads as an unintended defect rather than model-level or by-design). No maintainer engagement to date; labeled `stale` — needs re-verification on a current Claude Code version.

---

### CC-022: Mid-session `/model` switch doesn't actually apply to the running session

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anthropics/claude-code/issues/73881 |
| **Reported** | 2026-07-03 |
| **Last Activity** | 2026-09-06 |
| **Confidence** | Confirmed |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | v2.1.191 through at least v2.1.201, confirmed across VS Code extension, Claude desktop app, and iTerm2/terminal CLI — three independent users, no fix confirmed on current version |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | bug, platform:macos, area:model, platform:vscode, stale |

**Summary:**
Switching models mid-session via `/model` updates the UI (status line, model picker checkmark) and even the persisted `model` field in settings, but the already-running session keeps generating on — and billing against — the previous model. There is no reliable in-session way to confirm which model is actually serving requests (`/status` is not a recognized command; model self-report is unreliable). One corroborating report traced this at the `message.model` field across an entire 478-turn session: 100% of turns ran the old (expensive/rationed) model despite the picker showing the new one for the whole span, silently burning ~915K tokens of a capped weekly allowance. A changed model only reliably takes effect as the default for brand-new sessions — an existing/resumed session does not adopt it.

**Impact on Orchestration:**
Any orchestration step that switches models mid-session (e.g. downgrading/upgrading a long-lived subagent or primary session to manage cost or capability) expecting the new model to take effect immediately can silently continue running — and being billed/behaving as — the old model, with no in-session signal that the switch failed. This directly threatens any MOSAIC pattern that relies on mid-session model switching for cost or capability management within a single long-lived session.

**Evidence:**
- Three independent users reproduced the same symptom (UI/config shows the new model; the running session keeps generating on/billing the old one) across three different environments: VS Code extension (original report), iTerm2/terminal CLI with a full `message.model` transcript trace, and a third corroboration that specifically verified the settings-file `model` field had genuinely changed yet the session kept consuming the old model's usage tier.
- One reporter cross-references four additional related reports with the same root cause: #73879, #70058, #49541, #73488.
- No maintainer acknowledgment yet; issue auto-labeled `stale`. Confidence rests on the strength and independence of the three user reproductions (per the CaptureGuide's "multiple independent reproductions with evidence" criterion for Confirmed), not on maintainer confirmation.

**Workaround(s):**
1. ⭐ Start a brand-new session (not a resumed/continued one) after setting the desired model as default, rather than relying on a mid-session `/model` switch — a fresh session reliably adopts the current default. (Derived from reporter's own timeline: sessions created after the default changed opened correctly on the new model; the pre-existing session never did.)
2. Independently verify the active model via billing/usage dashboards or, if available, transcript-level `message.model` inspection — do not trust the status line or `/model` picker's confirmation as ground truth.

**Notes:**
No maintainer engagement to date; issue auto-labeled `stale` — needs re-verification on a current Claude Code version. For MOSAIC: any workflow depending on switching models mid-session should instead dispatch a fresh subagent/session configured with the target model from the start, per Workaround 1.

---

### CC-023: Subagent Bash `cwd` reset can land in a SIBLING subagent's worktree, with no spawn-time `cwd` parameter available to pin it

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anthropics/claude-code/issues/87953 |
| **Reported** | 2026-08-19 |
| **Last Activity** | 2026-09-06 |
| **Confidence** | Unverified |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 2.1.235 (Windows 11 Pro, Git Bash); no fix confirmed on current version |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | bug, platform:windows, area:bash, area:agents |

**Summary:**
In a subagent, the Bash tool's working directory resets between calls — and the reset target is not the subagent's launch directory, the parent session's directory, or the project root, but can be a **different, sibling subagent's** git worktree. Reporter ran six concurrent subagents each told to `cd` into its own of ~20 worktrees: one `cd` silently no-op'd and a subsequent `git merge` ran against the wrong (root) checkout, fast-forwarding `main` onto an unreviewed feature branch; a second subagent's `pwd` returned a sibling's worktree path (neither its own target, the root, nor the parent's cwd) even after its own `cd`. There is no `Agent`/`Task`-tool `cwd` parameter to pin a subagent's working directory at spawn time (a separate feature request, #12748, remains open), and no run-time mechanism (`cd`) reliably persists either.

**Impact on Orchestration:**
Directly threatens MOSAIC's own multi-worktree fan-out orchestration pattern — a subagent can silently execute Bash/git commands (including mutating ones like `git merge`/`git commit`/`git push`) inside another subagent's active worktree, corrupting isolation between parallel workstreams with no error surfaced. Flagged as high-priority for MOSAIC-side verification given how directly this maps onto MOSAIC's own concurrent-subagent-per-worktree pattern.

**Evidence:**
- Single reporter (NONE association) with a highly detailed, multi-subagent forensic account (three separate subagent observations in one incident: a silent-merge-into-wrong-checkout, a `pwd` landing in a sibling's worktree, and a third subagent's `pwd` reverting to repo root) plus a clean repro sketch. No maintainer response yet.
- One community commenter (not a maintainer) offered to test the same case under a hard-isolation ("Badgr") approach but has not yet posted results; a second commenter added only a general observation about retry cost, not independent reproduction. Neither counts as an independent confirmation of the underlying mechanism.
- Reporter cross-references several related open issues describing the same general area (worktree/isolation state being session-scoped, shared, or mis-pinned): #12748 (missing `cwd` spawn parameter, feature request), #76708 (non-persistent cwd resetting to session default in a main, non-subagent session), #84493, #82737, #87643.
- Reported on stock Anthropic infrastructure (Windows, standard Claude Code 2.1.235, no custom backend).

**Workaround(s):**
1. ⭐ Forbid subagents from running `git` (or other cwd-sensitive commands) at all; have the orchestrator perform every git operation itself instead of delegating it to worktree-scoped subagents. This is the reporter's own "reliable mitigation," at the cost of not being able to delegate the one operation that most needs the correct directory.
2. Have each subagent verify `pwd` before every state-mutating Bash/git call (rather than trusting a prior `cd`) and abort/report if it doesn't match the expected worktree. Reduces risk but does not eliminate it, and adds a round-trip per call.
3. `cd X && cmd` as a single compound Bash call reportedly works around the non-persistence, but compound commands are exactly what a related issue (#76708) shows delivering stale `cwd` to hooks, and many project permission setups block compound commands outright — not a general-purpose fix.

**Notes:**
No maintainer engagement to date. Flagged as high-priority for MOSAIC-side verification given direct relevance to MOSAIC's multi-worktree fan-out pattern — this is one of the clearest examples in the knowledge base of a harness defect mapping directly onto a core MOSAIC orchestration mechanism. Related open issues on the tracker (not separately tracked here): #12748 (cwd spawn parameter feature request), #76708, #84493, #82737, #87643.

---

### CC-024: `SessionStart`-hook environment (`CLAUDE_ENV_FILE`) grows unboundedly across compact/resume/clear cycles, eventually wedging the Bash tool

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anthropics/claude-code/issues/78146 |
| **Reported** | 2026-07-16 |
| **Last Activity** | 2026-09-07 |
| **Confidence** | Likely |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 2.1.211 (VS Code extension, Windows, original report); second reporter confirmed same mechanism on the CLI (Windows 11, Git Bash MSYS2) with no version given, activity as recent as 2026-09-07 |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | bug, has repro, platform:windows, platform:vscode, area:bash, area:hooks |

**Summary:**
The Bash tool inlines the accumulated `SessionStart`-hook environment file (`CLAUDE_ENV_FILE`) into every command's `bash -c` preamble with no validation, deduplication, or truncation. Because `SessionStart` hooks re-run and re-append on every `compact`/`resume`/`clear` (and a plugin's hook can append duplicate lines on every single fire), the preamble grows unboundedly across a long session and is cached per-session once built — permanently wedging the Bash tool once it becomes malformed or oversized. A second, independent reporter confirmed the exact root mechanism (unbounded env-file growth) and traced a common concrete trigger: the `openai-codex` plugin's non-deduped `SessionStart` hook. That reporter documented three distinct crash shapes by accumulated byte count, all from the same underlying cause: exit 127 ("torn export" line, needs concurrent-writer race), `unexpected EOF while looking for matching '` at ~8KB+ (mid-string truncation, no race needed — pure duplication suffices), and `ENAMETOOLONG`/`uv_spawn` failure at ~32KB+ (exceeds the Windows `CreateProcess` command-line length limit).

**Impact on Orchestration:**
Long-lived sessions that go through repeated compact/resume/clear cycles — a realistic MOSAIC pattern for long orchestration runs, especially the Interactive/harness-native execution mode where one session persists across many turns — risk the Bash tool becoming permanently unusable partway through, with no recovery short of starting an entirely new conversation (VS Code restart + `/resume` does not clear the poisoned per-session cache). Subagents spawned from an already-wedged session inherit the poisoned environment and fail identically.

**Evidence:**
- Original reporter (NONE association) provided detailed forensic evidence from client logs: a staircase growth pattern (1508 → 3071 → 4526 → 6197 → 9215 chars over a 2-hour session, one full copy per compact/hook event) and an exact failure-shape reproduction script (a 180-line export block with one torn line reproduces the identical `line N: e: command not found` / exit 127 signature).
- A second, independent reporter confirmed the same root mechanism on a different install (CLI rather than VS Code extension) with a different, simpler trigger (a single non-deduped plugin hook, no concurrent-writer race needed) and identified two additional crash shapes at different accumulated sizes, plus filed a fix PR against the triggering plugin itself (`openai/codex-plugin-cc#748`) and gave a direct-repair one-liner (dedupe the on-disk env file with `awk '!seen[$0]++'`).
- No maintainer acknowledgment yet on either report, but the two-independent-reproduction bar for "Likely" confidence is clearly met (same root mechanism, different triggers/environments, consistent symptom taxonomy).
- Related, likely-adjacent open issues referenced by reporters: #83243, #92543 (the EOF-quote shape specifically), and various `spawn ENAMETOOLONG` reports — several of which "name no writer," consistent with this same growth-without-dedup root cause being under-recognized across multiple separately-filed issues.

**Workaround(s):**
1. ⭐ If already wedged: start a brand-new conversation — the poisoned env script is cached per-session, so restarting the client and `/resume`-ing the same session does NOT clear it; only a fresh session is guaranteed clean. (Confirmed by the original reporter.)
2. Directly repair an already-bloated (but not yet fully wedged) session-env file by deduplicating its lines in place — no restart needed, takes effect on the next Bash call: `for f in ~/.claude/session-env/*/sessionstart-hook-*.sh; do awk '!seen[$0]++' "$f" > "$f.tmp" && mv "$f.tmp" "$f"; done` (Windows/Git Bash path shown; adjust for platform). Confirmed by the second reporter.
3. Preventively: audit any `SessionStart` hooks (own or plugin-provided) for idempotency — make them skip appending when a key/line is already present, and avoid firing on `compact` specifically if the hook's env values don't need refreshing that often. Check `~/.claude/settings.json` → `enabledPlugins` for the `openai-codex` plugin specifically, which the second reporter identified as a common concrete trigger (its own hook fix: `openai/codex-plugin-cc#748`).
4. As a blunter precaution: avoid excessive compact/resume/clear cycling within a single very long-lived session where feasible, since each cycle re-fires `SessionStart` hooks and adds to the unbounded growth.

**Notes:**
No maintainer engagement to date on either report. Confidence held at Likely (not Confirmed) since neither report has a maintainer acknowledgment or linked fix PR against the harness itself — the second reporter's PR (`openai/codex-plugin-cc#748`) fixes only one plugin-side trigger, not the underlying harness-side lack of validation/dedup/truncation that the original reporter's three suggested fixes target. Worth periodic re-verification given the harness-side root cause remains unaddressed as of the latest comment (2026-09-07).

---

### CC-025: Plugin-native `PreToolUse` hooks unenforced in interactive sessions; plugin JSON `deny` decisions unenforced in either mode

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anthropics/claude-code/issues/92675 |
| **Reported** | 2026-09-07 |
| **Last Activity** | 2026-09-07 |
| **Confidence** | Unverified |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 2.1.263 (Windows 11, Git Bash) |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | bug, has repro, platform:windows, area:hooks, area:plugins, area:permissions |

**Summary:**
For a `PreToolUse` hook auto-discovered from a plugin's own `hooks/hooks.json` (as opposed to a hook declared directly in the user's `settings.json`), enforcement depends on both hook-source and response protocol: a plugin-native hook using the documented `exit 2`/stderr protocol blocks correctly in `claude -p` (print mode) but silently fails to block in an interactive session; a plugin-native hook using the documented JSON `hookSpecificOutput.permissionDecision:"deny"` protocol fails to block in **either** mode. A control `settings.json`-declared hook using the same `exit 2` protocol blocks correctly in both modes, isolating the failure to plugin-sourced hooks specifically.

**Impact on Orchestration:**
Any security/permission gating implemented as a plugin-sourced `PreToolUse` hook cannot reliably block tool calls in interactive sessions (exit-2 protocol) or in any session mode (JSON `deny` protocol) — a significant gap if MOSAIC relies on plugin-based guardrails rather than `settings.json`-declared hooks.

**Evidence:**
- Single reporter (`BuildSmarterAI`), no maintainer response yet (issue is 2 days old as of this pass), 0 reactions, 0 comments.
- Reproduction is unusually rigorous: reporter isolates the failure across three independent axes (hook source: plugin vs. settings.json; protocol: exit-2 vs. JSON permissionDecision; mode: interactive vs. print), with a working `settings.json`-declared control hook in the same session proving the harness's general hook-blocking machinery works.
- Reporter independently verified the plugin hook's own logic, its full local dispatcher, and the literal bootstrap subprocess Claude Code spawns all produce correct, well-formed deny output — ruling out a plugin-code bug.
- Reporter cross-references three related-but-distinct existing issues (#10875 closed, #52822 open re: `allow` not `deny`, #31250 closed/stale) and explains why none of them pin down this specific three-axis combination.
- Reproduced on a real installed marketplace plugin (`everything-claude-code` `ecc@ecc` v2.2.1), not a synthetic minimal repro — slightly weakens generalizability but the reporter's isolation methodology (control hook, standalone dispatcher runs) compensates.
- Meets the Unverified inclusion bar: threatens a core MOSAIC pattern (tool-execution gating/guardrails), reported on stock Anthropic infrastructure (Windows, Git Bash, no custom backend), and includes concrete, reproducible steps.

**Workaround(s):**
1. ⭐ Prefer `settings.json`-declared `PreToolUse` hooks over plugin-native (`hooks/hooks.json`-discovered) hooks for any enforcement-critical gating — the control case in the issue confirms `settings.json`-declared hooks using `exit 2` block correctly in both interactive and print mode. Not yet community-validated beyond the reporter's own control test, but it is the only demonstrated-working path in the issue.
2. If plugin-native hooks must be used for JSON `permissionDecision` responses, do not rely on `deny` from them in any mode until this is fixed — no working protocol/mode combination exists for that case per the issue.

**Notes:**
Issue is very recent (filed 2026-09-07) with no maintainer engagement yet — recommend re-checking in a future pass for maintainer response or fix. Related but distinct open/closed issues referenced in the report: #10875 (closed, plugin hook stdout parse gap), #52822 (open, JSON `permissionDecision:"allow"` not honored interactively), #31250 (closed/stale, general PreToolUse not firing).

---

### CC-026: A second `/compact` within one process re-appends prior history with duplicate UUIDs, breaks `/rewind` session-wide

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anthropics/claude-code/issues/92089 |
| **Reported** | 2026-09-04 |
| **Last Activity** | 2026-09-08 |
| **Confidence** | Unverified |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 2.1.260, also seen on 2.1.255, 2.1.247, 2.1.246, 2.1.237 (macOS, Anthropic API) |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | bug, has repro, platform:macos, area:core |

**Summary:**
The first `/compact` in a process writes a clean `compact_boundary` row. A second `/compact` in the same process re-appends, with original UUIDs intact, every row from history start (or the previous resume boundary) up to the row before the first boundary's preserved-segment head — duplicating those rows in the transcript `.jsonl`. The new boundary and the `stop_hook_summary` row preceding it are misparented against the replayed copies rather than the true preceding message. Effects confirmed by the reporter: quadratic transcript growth with compaction count (one session reached 166 MB, 7,394 of 17,234 rows being replay copies), broken chain traversal causing large stretches of real conversation to disappear from the rendered transcript, and (per a follow-up comment on the same session) `/rewind` targets inside the replayed region failing session-wide with `cli_execution_error: No message found with message.uuid of: ...`.

**Impact on Orchestration:**
Long orchestration runs that trigger multiple compactions in a single process risk transcript bloat (quadratic growth), silently disappearing conversation history from the rendered/resumed view, and loss of the `/rewind` safety net for the remainder of the session — all without any error surfaced to the user or orchestrator.

**Evidence:**
- Single reporter (`thomasbachem`), no maintainer acknowledgment as of this pass (issue open 5 days, 0 reactions).
- Exceptionally rigorous reproduction: includes a standalone Python script driving `claude -p` through two compactions via `stream-json`, verifying duplicate UUIDs programmatically — reproducible by anyone with `claude` on PATH.
- Reporter cross-checked against real-world desktop-app transcripts: "all 21 compactions that were the first in their process are clean, and 13 of the 15 later ones replayed" — consistent, high-volume corroboration from the reporter's own data, not just the minimal repro.
- Reporter found the same behavior occurs with `--resume` on an existing session (copy starts at the resume boundary), and across multiple models (opus-5, fable-5, fable-5-1, haiku) — rules out a model-specific quirk.
- Follow-up comments from the same reporter (2026-09-08) identify the exact malformed field (`stop_hook_summary` row's `parentUuid` should equal the new boundary's `preservedSegment.headUuid`) and confirm a one-field repair restores all 413 stranded rows to the walk on a copied file.
- A second commenter (`samvallad33`) posted agreement on the symptom but the comment reads as promotional (links their own unrelated tool "Vestige") and does not provide independent reproduction evidence — not counted as independent corroboration.
- Explicitly distinguished by the reporter from unrelated issue #78592 (per-edit file content duplication, not compaction-related).
- No maintainer engagement despite detailed report — capped at Unverified per validation rules (single reporter, no maintainer confirmation, no independent reproduction from a second party). Meets the Unverified inclusion bar: threatens a core MOSAIC pattern (context/compaction integrity across long-running sessions), stock Anthropic API infrastructure, concrete and automatable reproduction steps.

**Workaround(s):**
1. ⭐ Avoid invoking `/compact` more than once within the same process/session — start a fresh session (or `--resume` into a new process) instead of manually compacting a second time in one process. Not community-validated (single-reporter issue), but directly supported by the reporter's reproduction (only the *second* `/compact` in a process triggers the replay).
2. If already hit and `/rewind` breaks, the reporter demonstrated a manual file-level repair is possible: in the affected `compact_boundary` row, set the immediately preceding `stop_hook_summary` row's `parentUuid` to that boundary's `compactMetadata.preservedSegment.headUuid`. This is a manual `.jsonl` edit, not a supported operation — use with caution and only on a backup copy.

**Notes:**
No maintainer response yet; re-check in a future pass. Reporter opened a near-duplicate issue #92849 for the `/rewind` session-wide breakage before finding this thread — #92849 was closed as a duplicate of this one, so #92089 is the canonical issue for both symptoms.

---

### CC-027: Leading whitespace before a slash command causes it to be sent to the model as plain text instead of being dispatched as a command

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anthropics/claude-code/issues/82846 |
| **Reported** | 2026-07-31 |
| **Last Activity** | 2026-09-07 |
| **Confidence** | Confirmed |
| **Orchestration Impact** | MEDIUM |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 2.1.220 through at least 2.1.234; regression first observed at v2.1.119 (2026-04-24, per the original report this refiles) |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | bug, has repro, platform:linux, area:tui, stale, reproduced |

**Summary:**
A line whose only content is whitespace followed by a slash command (e.g. `" /help"`) is not dispatched as a command — it is sent to the model as an ordinary chat message, and the model responds in prose instead of the command executing. Trailing whitespace (`"/help "`) is tolerated and dispatches normally, so the input is normalized at one end but not the other. Reproduces identically in both `claude -p` (headless) and the interactive TUI. A maintainer/collaborator (`bcherny`) independently reproduced the exact behavior on 2.1.234 in a comment.

**Impact on Orchestration:**
Any programmatically-generated prompt/command text that accidentally includes leading whitespace before a slash command — e.g. from templated prompts, indented run-book text, or copy-paste from the CLI's own indented output — will silently fail to invoke the intended command and instead be answered in prose by the model, with no error surfaced.

**Evidence:**
- Confirmed by a GitHub `COLLABORATOR` (`bcherny`), who reproduced the exact asymmetry (leading space defeats dispatch, trailing space is tolerated) on version 2.1.234, in both headless (`-p`) and interactive modes, including a tab-prefixed variant.
- This is a refile of an earlier report, #52903 (2026-04-24, v2.1.119), which was triaged `bug`/`regression`/`has repro` but auto-closed `NOT_PLANNED` by a stale bot with no maintainer response, then locked — the reporter refiled with a smaller, cleaner repro rather than continuing a locked thread.
- Reporter's own analysis (labeled as hypothesis, not diagnosis) suggests the dispatch predicate is anchored at index 0 of the raw input rather than the trimmed input, consistent with a related fixed issue (v2.1.147 changelog fixed trailing-whitespace/tab dispatch but apparently not the leading-side case).
- Deliberately scoped to whitespace-only-prefixed lines; reporter explicitly excludes mid-sentence slash mentions (a different, intentionally out-of-scope behavior tracked in #77868/#70656).
- Labeled `stale` on the current issue as of last activity (2026-09-07) despite the maintainer reproduction comment — recommend re-verification in a future pass to confirm it hasn't been auto-closed since.

**Workaround(s):**
1. ⭐ Trim leading whitespace before slash commands in any programmatically-constructed or pasted input before sending it to Claude Code — directly addresses the reported asymmetry and is consistent with both the reporter's and maintainer's reproduction (trailing whitespace is already tolerated, so only the leading side needs stripping).

**Notes:**
Related/contributing issue: #18170 (copy/paste from the CLI's own terminal output retains leading indentation, a common real-world source of this trigger). Deliberately distinct from #77868 and #70656 (mid-message slash mentions — different, unresolved ambiguity, intentionally not addressed by this report). Issue currently carries a `stale` label alongside `reproduced` — worth re-checking status in a future pass since it has a collaborator confirmation but no visible fix/PR yet.

---

### CC-028: `--continue` is directory-scoped, not invocation-scoped — can silently co-write into another live session's transcript

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anthropics/claude-code/issues/87902 (see also https://github.com/anthropics/claude-code/issues/69364, the more heavily-corroborated companion report) |
| **Reported** | 2026-08-19 (87902); 2026-06-18 (69364, earlier and more active) |
| **Last Activity** | 2026-09-08 (87902); 2026-09-09 (69364) |
| **Confidence** | Confirmed |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Reproduced from 2.1.181 through at least 2.1.247, and again on unspecified current version as of 2026-09-09 — long-standing, not version-specific |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | bug, area:core, area:cli, stale (87902); enhancement, area:core, area:cli (69364) |

**Summary:**
`--continue`/`--resume` resolves and re-opens a session by directory (or by session id with no liveness check) rather than by invocation, and does not consult Claude Code's own live-session registry (`~/.claude/sessions/<PID>.json`, shipped since v2.1.181) before attaching. A second `--continue`/`--resume` targeting a directory or session id already live in another process silently attaches to that live session, and both processes append to the same transcript `.jsonl` — with no warning surfaced anywhere. Multiple independent users reproduced this across versions 2.1.181 through 2.1.247 and again as of 2026-09-09, via distinct trigger paths (CLI `--continue`, CLI `--resume <uuid>`, the VS Code extension's session picker, and scheduler/bridge-spawned sessions), so this is a stable, long-standing property of the resume path rather than a narrow race.

**Impact on Orchestration:**
Any automation that launches multiple concurrent Claude Code invocations against the same working directory or that reuses `--continue`/`--resume` without first checking the live-session registry (e.g. parallel dispatch, scheduled restarts, or multi-surface access such as CLI + IDE extension to the same project) risks two processes silently co-writing one transcript — corrupting conversation history and, per one report, also corrupting the shared task store (`~/.claude/tasks/<sessionId>/`), which an agent may act on directly.

**Evidence:**
- #87902: single reporter (attributed to "Claude Opus 5, submitted on behalf of the account owner"), no comments, but includes precise timestamp-level evidence (service process start time matching a `queue-operation`/`user` entry landing on the human's branch 0.7s later) and clean reproduction description.
- #69364 (the substantially more evidenced companion, tracked as the "proposed fix" issue for the same root defect): 8 comments across 2026-06-18 through 2026-09-09, with independent reproductions from at least 5 different users (`peter216`/`WingedGuardian`/`willmcginnis`/`tonydzi`/original reporter) on versions 2.1.181, 2.1.197, 2.1.202-211, 2.1.247, and again 2026-09-09 — each with concrete evidence (interleaved `entrypoint` fields in the shared jsonl, `ps`-verified dual-live-PID checks, binary string probes confirming no `"duplicate session"` guard exists in the 2.1.247 binary).
- Meets Confirmed per the guide's "multiple independent reproductions with evidence" clause even without maintainer acknowledgment — this is the strongest-evidenced entry in this backfill batch.
- One comment (`willmcginnis`, 2.1.247) extends the impact: the shared task store under `~/.claude/tasks/<sessionId>/` is also written by both processes, silently rewriting/closing task records — a corruption vector beyond the transcript alone.
- The registry primitive needed to fix this (`~/.claude/sessions/<PID>.json`, carrying `pid`, `sessionId`, `cwd`, `status`, `procStart`) has existed since v2.1.181 per the reports, but is confirmed via binary string search (on 2.1.247) to still lack any "duplicate session" consuming logic — the data exists, the check does not.
- #69364 is labeled `enhancement` rather than `bug` despite describing a defect with real data-corruption consequences — consistent with the CaptureGuide's guidance that "intended"/mis-labeled issues are tracked on their operational merits, not their GitHub label.

**Workaround(s):**
1. ⭐ Pin sessions explicitly with `claude --resume <session-uuid>` rather than bare `--continue` (confirmed stable across restarts by the #87902 reporter) — but note it has its own gap: a `/clear` mid-session assigns a new UUID, so a hardcoded pin can go stale and point at an abandoned conversation.
2. Community-built liveness-check wrappers exist and are shared in #69364: a PowerShell function (`claude-resume`) that reads `~/.claude/sessions/*.json`, matches `cwd` and validates the owning PID is alive via `procStart` (not process name, which is unreliable across the Windows auto-updater's rename-in-place), then prompts to fork/new/abort; and a `UserPromptSubmit`-hook-based order-fingerprinting detector for scheduler/bridge-spawned duplicates (shared by `tonydzi`, 2026-09-09). Neither is officially supported; both are community-only and unverified by us.
3. For MOSAIC specifically: ensure each concurrent Claude Code invocation uses a distinct working directory/worktree rather than sharing one directory with `--continue`/`--resume` — avoids the trigger condition entirely, though it does not address every case reported (e.g. deliberate cross-surface session-picker resumes in #69364).

**Notes:**
Treat #69364 as the primary, best-evidenced tracking issue for this underlying defect; #87902 is a specific, well-documented instance report matching this entry's original title. Given the strength and recency (2026-09-09) of #69364's evidence and the shared-task-store corroboration, this is a high-priority candidate for MOSAIC mitigation given the system's multi-agent, worktree-based parallel dispatch model.

---

### CC-029: Manual `/compact` can silently no-op on large conversations — summary is billed/executed but no compaction boundary is written, UI reports success

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anthropics/claude-code/issues/89040 |
| **Reported** | 2026-08-23 |
| **Last Activity** | 2026-09-06 |
| **Confidence** | Unverified |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 2.1.237, 2.1.241 (macOS, claude-opus-5 with 1M-token context window); one comment reports a corpus check finding no confirmed cases prior, so no established earlier onset |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | bug, has repro, platform:macos, area:core |

**Summary:**
On a very large conversation, manual `/compact <instructions>` can silently fail to apply: the summarization API call runs and bills, and the summary is written to the transcript as an assistant message, but no `compact_boundary` record is written, in-memory context does not shrink, and the UI shows no error — it looks identical to a successful compaction. In the reporter's affected conversation (~63,000 transcript lines, ~132MB), the last 47 manual `/compact` attempts across ~18 hours produced 46 orphan summaries and 0 boundaries, while automatic compaction at the context ceiling and manual compaction on fresh conversations both worked normally on the same machine. A second, independent incident on a different fresh conversation (2.1.241, ~677K tokens) reproduced the same orphan-summary pattern, with a later manual `/compact` on the same conversation succeeding normally seven hours after — indicating the failure is intermittent per-attempt rather than a permanently "dead" conversation.

**Impact on Orchestration:**
A session believed to have been compacted (freeing context budget) may not actually have been — an orchestrator or automation that assumes `/compact` succeeded (because the UI/transcript shows no error) continues operating on a false assumption of freed context, risking premature context exhaustion. The reporter's own automation retried the compaction every ~5 minutes for 7.5 hours, each attempt re-reading a near-full 1M-token window (~253M cache-read tokens total) because nothing in-band signaled failure — a severe, silent cost amplification on top of the correctness risk.

**Evidence:**
- Single original reporter (`ehawkin`) with very rigorous, timestamped production evidence and two distinct incidents (the original 132MB conversation, and a second fresh 677K-token conversation).
- An attempted independent corroboration (`tonydzi`, running an 13,751-14,592-transcript corpus check) initially reported 2 matching incidents from June/July, but retracted them in a follow-up comment after finding a flawed detection instrument, concluding "our transcripts say nothing about this bug in either direction" — so this attempted second-party confirmation did not pan out and cannot be counted as independent corroboration.
- No maintainer acknowledgment as of last activity (2026-09-06).
- The multi-comment thread between the reporter and `tonydzi` is unusually rigorous methodologically (both parties built and iterated on forensic scripts to detect orphaned `/compact` attempts from transcript structure, and both explicitly flagged and corrected their own false positives/negatives) — this strengthens confidence in the reporter's own findings even though it weakens the corroboration.
- Per validation rules, capped at Unverified: single reporter, no maintainer confirmation, and the one attempted independent reproduction was explicitly retracted. Meets the Unverified inclusion bar: threatens a core MOSAIC pattern (context/compaction integrity), stock Anthropic infrastructure (macOS, claude-opus-5), and detailed real-world reproduction evidence (though not a minimal third-party-runnable repro script).

**Workaround(s):**
1. ⭐ Use a `PreCompact`/`SessionStart`/`UserPromptSubmit` hook triad to detect a silently-failed compaction, as proposed by `tonydzi` in the thread: `PreCompact` writes a marker file keyed by `session_id` when compaction is requested; `SessionStart` (fired with `source: "compact"` when compaction actually lands) deletes the marker; `UserPromptSubmit` on the next human/agent turn checks whether the marker still exists (with a ~20s minimum age to avoid flagging an in-flight compaction) — if it does, the requested compaction never landed regardless of what the UI reported. This is the only mechanism in the thread confirmed to work reliably, because the `compact_boundary` record is the sole ground truth and is not otherwise exposed to hooks or hands-off automation. Not yet independently validated by a third party at time of writing.
2. Do not trust the transcript's `<local-command-stdout>Compacted</local-command-stdout>` string, transcript-scanning for `/compact` command counts, or the UI's reported success as evidence of an applied compaction — both the reporter and `tonydzi` independently concluded post-hoc transcript forensics are unreliable in both directions (successful compactions can scrub prior failed-attempt records, and echoed post-boundary `/compact` records are easily miscounted as new orphans). Only the presence of a `compact_boundary` record (or the hook-based marker above) is reliable.

**Notes:**
No maintainer response yet — recommend re-checking in a future pass. The retracted corroboration attempt (`tonydzi`) is worth revisiting if that party or others post updated corpus data; their forensic script (shared in the thread) is a candidate reference implementation for anyone instrumenting this in MOSAIC.

---

### CC-030: Skill/command positional argument substitution (`$0`/`$1`...) is off-by-one; `$2`+ never substitutes; substitution corrupts literal `$0`/`$1`-shaped prose

| Field | Value |
|-------|-------|
| **Classification** | Bug (indexing/range defect) + Limitation (literal-text corruption, maintainer-confirmed as by-design with an escape) |
| **Source** | https://github.com/anthropics/claude-code/issues/92457 (indexing/off-by-one, `$2`+ never substitutes); heavily corroborated by https://github.com/anthropics/claude-code/issues/78759 (literal-text corruption) |
| **Reported** | 2026-09-06 (92457); 2026-07-18 (78759) |
| **Last Activity** | 2026-09-06 (92457); 2026-08-15 (78759) |
| **Confidence** | Confirmed (literal-corruption mechanism, via maintainer/collaborator comment on 78759); Unverified (indexing off-by-one and `$2`+ non-substitution specifics, single-reporter on 92457) |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 92457: 2.1.259 (Windows, plugin skills). 78759 and its many corroborating comments: 2.1.220 through 2.1.233, confirmed present across that whole range, on Windows/macOS/Linux/WSL2 — long-standing, not version-specific. |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | 92457: bug, has repro, platform:windows, area:skills, area:plugins. 78759: bug, documentation, has repro, area:skills, stale, reproduced |

**Summary:**
Two related but distinct behaviors are in play, and the current understood state differs meaningfully from the original title:
1. **Literal-text corruption (`$0`/`$1`-shaped prose gets rewritten) — CONFIRMED as intentional/by-design, not a bug.** A GitHub `COLLABORATOR` (`bcherny`) explicitly stated on #78759: "the substitution itself is working as designed... this has always been the behavior (not a regression)," and confirmed a documented backslash escape (`\$0`, `\$1`) reliably protects literal text, including inside fenced code blocks. This makes it a **Limitation**, not a Bug — MOSAIC must adapt permanently (escape literal `$<digit>` text), not wait for a fix. Dozens of independent reporters across #78759 (at least 8 distinct commenters, multiple platforms, versions 2.1.220–2.1.233) reproduced this and converged on the same escape mitigation, several noting real-world consequences: corrupted awk/shell scripts embedded in skill bodies, a corrupted safety instruction about `$0` reporting, corrupted Excel absolute cell references, and corrupted Typst math notation with no available escape in that domain (since `\$` has a different meaning in Typst).
2. **Off-by-one indexing and missing `$2`+ substitution — Unverified, single-reporter (#92457).** A separate, narrower report claims `$1` (expected to be the first argument per some documented convention) actually yields the *second* argument, and that `$2` through `$9` never substitute at all — a plugin/skill author following the "$1 = first arg" convention gets the wrong argument silently, or a literal unhandled `$2`. This part is NOT addressed by the maintainer's comment on #78759 (which only concerns literal-corruption/escaping) and has no independent confirmation as of this pass — note that Skills docs (per #78759's summary of code.claude.com/docs/en/skills) describe `$N` as 0-based, which is actually consistent with "$0 = first arg, $1 = second," suggesting the "off-by-one" framing may partly be a documentation-consistency issue (slash-command docs vs. Skills docs may disagree on 0- vs 1-indexing) rather than a pure engine defect — but the claim that `$2`+ never substitutes at all appears to be a genuine, unaddressed defect.

**Impact on Orchestration:**
Any MOSAIC skill/command relying on positional arguments beyond the first, or containing literal `$`-prefixed digit sequences (dollar amounts, awk/shell field references, spreadsheet cell references, math notation) in its prose, can silently receive the wrong argument or have its instructional text corrupted — with no error surfaced and no on-disk trace, since the corruption happens only in the rendered prompt delivered to the model. Reported real-world consequences include an agent concluding a correct skill file was buggy (based on corrupted output it could not distinguish from real content), a silently-disarmed anti-fabrication safety instruction, and business-metric skills silently computing zero/wrong values.

**Evidence:**
- #78759 (literal-corruption half): maintainer/collaborator (`bcherny`) confirmed the mechanism is by-design and documented, with a working backslash escape, on 2026-08-15 — meets Confirmed by the guide's maintainer-acknowledgment clause. Additionally corroborated independently by at least 7 other commenters across 2.1.220–2.1.233 on Windows/macOS/Linux/WSL2, all converging on the same escape behavior and fence-scope finding (substitution reaches inside fenced code blocks and inline code spans, contrary to what most authors would assume).
- #92457 (indexing/range half): single reporter (`zivsh-tr`), no maintainer response, but very detailed reproduction table (expected-vs-actual for `$0`-`$3`, `$5`) with a real-world user-visible failure (a wrapped script receiving the wrong positional argument and erroring). Confirmed by the same reporter in follow-up comments that `$ARGUMENTS` (not positional `$N`) substitutes correctly and is a working full replacement for positional args.
- Meets the Unverified inclusion bar for the indexing/range claim specifically: threatens a core MOSAIC pattern (skill/command parameter passing in orchestration), stock Anthropic infrastructure, concrete reproduction steps.

**Workaround(s):**
1. ⭐ For literal `$<digit>`-shaped text (prices, awk/shell field refs, spreadsheet cells) in any skill/command body: escape with a backslash (`\$0`, `\$1`) — maintainer-confirmed as the documented, working mechanism, effective inside fenced code blocks and inline code spans. This is the officially sanctioned fix for the by-design Limitation and should be applied to any MOSAIC-authored skill/command prose containing such text.
2. Prefer `$ARGUMENTS` over positional `$N` substitution for skill/command parameters wherever possible — confirmed working correctly (full raw argument string substituted, no off-by-one or range limitation) in both #92457 and #78759's threads, and sidesteps the entire indexing/range defect.
3. For content that cannot be escaped or reworded (e.g. domains where `$` is itself a language delimiter, such as Typst math notation) and where the skill declares no positional parameters: a `PreToolUse` hook matching `tool_input.skill`/`tool_input.args` can deny programmatic `Skill`-tool invocations that pass arguments to a skill not designed to receive them — but per one commenter's testing, this does **not** cover the typed-slash-command invocation path (no tool call occurs, so `PreToolUse` never fires); a `UserPromptSubmit` hook is needed to close that second path. Community-shared, not officially supported.
4. If a plugin/skill genuinely needs more than one positional parameter, avoid relying on bare `$2`+ entirely (per #92457's finding, current reproduction shows these never substitute) — restructure to use `$ARGUMENTS` plus manual parsing instead.

**Notes:**
This entry originally conflated a by-design Limitation (literal-text substitution, confirmed intentional and escapable) with a possibly-genuine Bug (the `$2`+ non-substitution and off-by-one framing, unconfirmed). Kept as one entry since they share the same substitution mechanism and the same practical mitigation guidance, but MOSAIC should treat the escape-based mitigation (workaround #1) as a permanent adaptation rather than a stopgap pending a fix. Related closed issues referenced within #92457: #23585 (forked-skill `$1`/`$2` not resolving at all, closed as completed — a different failure mode than resolving-to-the-wrong-index) and #46922 (`$ARGUMENTS` substitution request, closed not-planned, though contradicted by #92457's own follow-up finding that `$ARGUMENTS` does substitute correctly on 2.1.259).

---

### CC-031: `Agent` tool dispatch with an explicitly-named `subagent_type` can ignore that subagent's definition entirely

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anthropics/claude-code/issues/92426 |
| **Reported** | 2026-09-06 |
| **Last Activity** | 2026-09-06 |
| **Confidence** | Unverified |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 2.1.263 (macOS 26.6.2) |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | bug, has repro, platform:macos, area:agents |

**Summary:**
Dispatching the `Agent` tool with an explicitly-named `subagent_type` can ignore that subagent's own definition entirely — the child instead receives the dispatching session's own generic system prompt and full tool surface, for both built-in and plugin agent types, while the same definition is correctly parsed and shown in the "Available agent types" listing delivered to the child. The inheritance is positive, not merely a fallback default: in the reporter's minimal repro, an `Explore`-typed child (defined to exclude `Write`/`Edit`/`Agent`/`Artifact`, among others) reported holding `Write` and reproduced **verbatim a sentence unique to the dispatching session's own project-specific system prompt** — a sentence that appears in no agent definition. A second reproduction with three plugin-defined agents (each declaring ~28 tools including one MCP namespace) found all three children received the generic prompt, no agent-body role instructions, and every tool/MCP namespace the dispatcher held (460 deferred tool names in one case) rather than their own declared ~28. The same definitions apply correctly when used at top level via `claude --agent <name>`, isolating the failure to the `Agent`-tool dispatch path specifically. Reproduced 3/3 in one batch, survived a full restart, and reported as deterministic (not intermittent) by the reporter — though only one reporter has done so as of this pass.

**Impact on Orchestration:**
If confirmed, this is a fundamental breach of subagent isolation/scoping for MOSAIC's core delegation pattern: an explicitly-named subagent could run with the dispatcher's full permissions, tool surface, and prompt instead of its own scoped definition, defeating the purpose of scoped subagent delegation entirely — a planner could gain a builder's write access, or vice versa, silently and without any error. The reporter notes any project whose least-privilege story rests on agent `tools:` definitions has no enforced ceiling under `Agent`-tool dispatch, and any MCP server registered in the dispatching session leaks into every subagent. The reporter's own project was contained only because server-side authorization independently refused cross-role writes — a mitigation specific to their setup, not the harness's.
- The reporter explicitly distinguishes this from #30280 and #80036 (MCP tools not reliably inherited by subagents): those report a visible symptom (MCP tools missing), while this issue's second, more serious half — a child gaining tools its definition *excludes* — is the same underlying defect from the opposite direction and is easy to miss because "a child dispatched from a well-stocked parent looks like it works."

**Evidence:**
- Single reporter (`alexatpando`), one comment (from an unrelated third party making a general observation, not a reproduction) as of this pass — no maintainer acknowledgment, no independent reproduction yet.
- Reproduction is unusually rigorous for a single-reporter issue: a minimal, stock-install (no plugins, no MCP servers) one-call repro plus a second, more elaborate multi-agent/plugin reproduction, both with concrete, falsifiable evidence (verbatim sentence leakage; explicit tool-count deltas: ~28 declared vs. 460 actual deferred tools in one case).
- Confirmed working correctly at top level (`claude --agent <name>`) in contrast to the `Agent`-tool dispatch path — a useful isolating control the reporter ran themselves.
- Labeled `bug`, `has repro` on filing — but per the CaptureGuide, a label alone (even `has repro`) is not maintainer acknowledgment; capped at Unverified.
- Meets the Unverified inclusion bar clearly: this threatens a core MOSAIC orchestration pattern (subagent dispatch via explicitly-named `subagent_type`, which is exactly how MOSAIC's hub-and-spoke delegation works), is reported on stock Anthropic infrastructure (no custom backend, no plugins in the first repro), and provides concrete, step-by-step reproduction. Given the severity if confirmed (complete breach of subagent tool/prompt isolation), this remains a high-priority candidate for corroboration in a future pass.

**Workaround(s):**
1. Unknown — no workaround has been proposed in the issue or its single comment as of this pass. If the defect is real, an MCP-server/tool-scoping workaround at the infrastructure layer (e.g. relying on server-side authorization to refuse cross-role actions, as the reporter's own project incidentally does) is the only mitigation observed so far, and it is specific to that reporter's setup rather than a general fix.

**Notes:**
RECONCILIATION (see CC-011): confirmed distinct from CC-011 — CC-011's evidence (all reproduced with unnamed/default `subagent_type`) can neither confirm nor refute this claim about explicitly-named dispatch. Kept as a separate entry, stays Unverified pending corroboration. The single comment on the issue (from `webby-box`) is a general remark about the cost implications of prompt/tool inheritance, not a reproduction or maintainer response — does not move the confidence needle. Related-but-distinct existing issues per the reporter: #30280 and #80036 (MCP tool inheritance gaps — narrower symptom of the same underlying defect, per the reporter's framing). Given severity if true, this is a high-priority candidate for re-verification and corroboration in a future pass — recommend checking for maintainer response or independent reproduction, since this is one of the most consequential unconfirmed claims in the current knowledge base.

---

### CC-032: A subagent's plan-mode "Ready to code?" approval dialog can display and let the user toggle the PARENT session's plan

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anthropics/claude-code/issues/86255 |
| **Reported** | 2026-08-13 |
| **Last Activity** | 2026-09-07 |
| **Confidence** | Confirmed (independent collaborator reproduction on a different OS/terminal) |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 2.1.229 (reporter), reproduced again on 2.1.233 |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | bug, has repro, platform:macos, area:agents, stale, reproduced |

**Summary:**
When a parent session holding an open EnterPlanMode window dispatches a plan-mode (`permissionMode: plan`) subagent via the Agent tool, the "Ready to code?" approval dialog can appear labeled as coming from the subagent but actually display, edit, and gate the PARENT session's own plan content/state. Approving it silently exits the parent's plan mode as a side effect while the subagent is unaffected and its own later `ExitPlanMode` call then fails with "You are not in plan mode." A follow-up report also shows an unprompted "Exited Plan Mode" system event firing in a subagent's transcript with zero prior `ExitPlanMode` calls.

**Impact on Orchestration:**
Any MOSAIC pattern that runs a plan-mode subagent while the parent/orchestrator session is itself mid-plan risks the parent's plan being silently discarded (or a stale/misattributed plan being approved), and the subagent's own genuine `ExitPlanMode` call failing outright once this happens — with no causal link visible in the parent's transcript.

**Evidence:**
- Reporter (aldelbal) provided detailed repro steps and a timestamped transcript showing the anomaly.
- Anthropic collaborator (bcherny) independently reproduced on Claude Code 2.1.233, Linux/tmux (reporter was on macOS) — confirmed the dialog content, file-edit target, and plan-mode-exit side effect are all scoped to the main conversation rather than the requesting subagent. Explicitly confirmed as a bug.
- Labeled `reproduced` and `has repro` by the tracker.

**Workaround(s):**
1. ⭐ Avoid concurrent plan-mode usage between a parent/orchestrator session and its subagents — the collaborator who reproduced this reported abandoning plan mode in multi-agent setups altogether due to this and other related convoluted parent/subagent plan-mode interactions. No harness-side fix or narrower workaround has been posted yet.

**Notes:**
Issue carries a `stale` label (bot-applied) but had a maintainer/collaborator comment as recently as 2026-08-17 and reporter follow-up 2026-08-18; still open and unresolved as of last activity 2026-09-07. No fix PR linked yet — recommend re-checking on a future pass.

---

### CC-033: Subagents receive the full auto-memory index and skill listing, contrary to documented subagent isolation

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anthropics/claude-code/issues/87613 (canonical/hub issue; duplicates: #87835, closely related: #92750) |
| **Reported** | 2026-08-18 |
| **Last Activity** | 2026-09-07 |
| **Confidence** | Likely (three independent reporters, each with rigorous HTTP-proxy/transcript-capture reproduction measuring byte-identical payload injection; no explicit maintainer bug-confirmation comment, but a collaborator closed #87835 as a duplicate of #87613, implicitly treating it as one real, tracked behavior) |
| **Orchestration Impact** | MEDIUM |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 2.1.234–2.1.263 (reproduced across this range by three separate reporters) |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | bug, has repro, platform:macos, area:cost, area:agents, stale |

**Summary:**
Non-fork subagents (both the built-in `general-purpose` agent and custom `.claude/agents/*.md` definitions) receive the parent session's full auto-memory (`MEMORY.md`) and, per the broader #92750 report, the full skill listing as well — contrary to the documented behavior that auto-memory requires an explicit `memory:` field and that subagents get no pre-populated skill listing. Measured via HTTP-proxy capture and subagent transcript inspection across three independent reports; in one case this block was 72% of a small subagent's first-turn tokens (12,928 of 17,965), and in another ~17k tokens (~7% of all cache-read tokens in a subagent-heavy workload). No frontmatter key, settings key, or env var (`memory: false`, `claudeMd: false`, `inheritContext: false`, etc. — 11 candidates probed) suppresses it; the session-wide `claudeMdExcludes`/`CLAUDE_CODE_DISABLE_AUTO_MEMORY` settings also strip the parent conversation's own memory, so they aren't a viable per-agent workaround.

**Impact on Orchestration:**
Subagents intended to be scoped/isolated (e.g. for context-budget or focus reasons) instead inherit the parent's full memory/skill surface. This inflates the token cost of every subagent turn (re-sent as cache reads on each subsequent turn), and in MOSAIC's subagent-fan-out-heavy orchestration pattern this recurring overhead compounds across many dispatched subagents and long sessions — a silent, undocumented cost with no per-agent opt-out.

**Evidence:**
- #87613 (bitvibes-io): HTTP-proxy capture showing MEMORY.md at 34% of a small Haiku subagent's first-turn tokens; probed and ruled out 11 candidate opt-out keys.
- #87835 (bitvibes-io, different repro): same finding via proxy capture (72% of first-turn tokens); closed by collaborator bcherny as a duplicate of #87613, confirming the two reports describe the same underlying behavior.
- #92750 (alanna): independent, later reproduction (2.1.258–2.1.263) additionally showing the skill listing (not just memory) is also delivered, across both the built-in `general-purpose` agent and custom agent definitions with a `tools:` allowlist — ruling out tool-scoping as a mitigation.

**Workaround(s):**
1. No working per-agent workaround currently exists — all attempted frontmatter/settings opt-out keys are silently ignored. Session-wide `CLAUDE_CODE_DISABLE_AUTO_MEMORY` / `claudeMdExcludes` suppress the leak but also disable auto-memory for the parent session, which is a significant trade-off, not a targeted fix.

**Notes:**
#87613 carries a `stale` bot label but had a fresh independent reproduction as recently as 2026-09-07 (via #92750) — still an open, current issue. No fix PR linked yet.

---

### CC-034: `CLAUDE.md` and path-scoped rules in `--add-dir`-added directories are silently skipped

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anthropics/claude-code/issues/79489 |
| **Reported** | 2026-07-20 |
| **Last Activity** | 2026-09-09 |
| **Confidence** | Likely (original reporter plus a second, independent reporter using a rigorous `InstructionsLoaded` hook to measure actual load behavior rather than relying on model self-report; no maintainer confirmation yet) |
| **Orchestration Impact** | MEDIUM |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Confirmed present on 2.1.232; original report undated version, VS Code extension on Windows |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | bug, platform:windows, area:core, platform:vscode, stale |

**Summary:**
`CLAUDE.md` files and `.claude/rules/*.md` (with or without `paths:` frontmatter) located in directories added via `--add-dir` are silently skipped unless `CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD=1` is set — and even with that variable set, path-scoped rules with `paths:` frontmatter and nested `CLAUDE.md` in subdirectories of the added directory still never load, even when Claude reads/edits a file whose glob matches. A control test (same files copied into the primary working repo) loaded correctly, confirming the added-directory boundary itself is the cause. No warning is surfaced to the user when this happens.

**Impact on Orchestration:**
Any MOSAIC configuration that uses `--add-dir` to bring in additional directories/worktrees with their own `CLAUDE.md` or path-scoped rules risks those rules silently never being applied — with no error or notice — even for files the agent is actively reading or editing in that directory.

**Evidence:**
- Original reporter (sylque, VS Code multi-folder workspace, Windows) reproduced with a `CLAUDE.md` containing mandatory editing rules that were silently never honored.
- Independent second reporter (moghaddas) reproduced on macOS in a plain CLI session (no VS Code, no multi-folder workspace) on version 2.1.232, using an `InstructionsLoaded` hook to verify actual load events rather than model self-report — confirming root `CLAUDE.md`, unscoped rules, `paths:`-scoped rules, and nested subdirectory `CLAUDE.md` are all affected to varying degrees, and ruling out file-content/pattern issues via a working control.

**Workaround(s):**
1. ⭐ Do not rely on `--add-dir` to bring in directories with their own `CLAUDE.md`/rules; instead verify actual load behavior with an `InstructionsLoaded` (or equivalent) hook rather than trusting the documented `CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD=1` opt-out, since it does not fully close the gap for path-scoped rules or nested `CLAUDE.md` files. (Source: independent reporter moghaddas.)
2. Copy critical rules directly into the primary working repo/CLAUDE.md rather than depending on added-directory rule files, since the control test showed same-content files load correctly when placed in the primary repo.

**Notes:**
Issue carries a `stale` bot label but had fresh independent-reporter activity as recently as today (2026-09-09) and a second reproduction on 2026-08-16 — still open, current, and unresolved. No fix PR linked yet.

---

### CC-035: Project skills beyond an undocumented per-session cap become fully non-invocable ("Unknown skill")

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anthropics/claude-code/issues/93044 (current recurrence; original report: https://github.com/anthropics/claude-code/issues/31505, closed `not_planned`/stale, now locked) |
| **Reported** | 2026-03-06 (original, #31505); recurrence reported 2026-09-09 (#93044) |
| **Last Activity** | 2026-09-09 |
| **Confidence** | Likely (same "Unknown skill" symptom, with the cap threshold apparently rising over time — ~28 skills in March 2026, ~14-of-61 in September 2026 — independently reproduced by at least 3 separate reporters across a 6-month span, with rigorous elimination of file-corruption, install-order, and size-based explanations each time; no maintainer acknowledgment — the original report was closed `not_planned` by a stale-bot, not by a maintainer decision) |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Observed March 2026 through current (2.1.267-era, reported 2026-09-09); persists across at least two major version eras |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | bug, has repro, platform:web, area:skills (current #93044); bug, platform:macos, area:skills, stale (original #31505) |

**Summary:**
When the number of registered skills (project + plugin + built-in combined) exceeds an undocumented per-session cap, the excess skills are silently absent from the session-start "available skills" listing AND fully non-invocable — calling the `Skill` tool directly by exact name returns `Unknown skill` despite a valid, well-formed `SKILL.md` on disk. Which skills survive the cap is not alphabetical, not mtime-based, and not size-based in either report. This is a recurrence: the same symptom was reported in March 2026 (~28-skill threshold, #31505), auto-closed as stale/`not_planned` (never resolved by a maintainer decision), and recurred in September 2026 with a larger project (61 project skills, ~14 survive) in #93044, which explicitly cites #31505 as likely the same root cause.
- Directly relevant to CC-030/CC-031/related project-skill-registry issues.

**Impact on Orchestration:**
Any MOSAIC deployment registering a large number of project skills (a plausible pattern given MOSAIC's own multi-agent, multi-skill catalog structure) risks some skills silently becoming uninvocable once an undocumented, seemingly-version-dependent cap is exceeded, with no error explaining why and no reliable way to predict which skills will be dropped.

**Evidence:**
- #31505 (March 2026): rigorous reproduction (symlinked skill dirs, ruled out alphabetical/mtime/size selection), plus one independent confirming commenter (j2h4u) hitting the same symptom via a third-party skill-installing framework; closed as stale/not_planned by a bot, not resolved by a maintainer.
- #93044 (September 2026): independent, larger-scale reproduction (61 project skills + ~20 plugin/built-in skills, only ~14 register), explicitly ruling out file corruption, install/mtime order, file size, and `/clear`+`/continue` session artifacts as causes; references #31505 as the same root cause recurring at a different scale.

**Workaround(s):**
1. ⭐ Keep the total registered-skill count (project + plugin + built-in combined) well below the empirically observed range (roughly 14-28 skills survived in the two reports, though the exact cap appears to vary and is not documented) as a precaution.
2. If a skill must be guaranteed invocable, prefer the `/skill-name` slash-command path over programmatic `Skill()` invocation — the original reporter noted the slash-command path still reads `SKILL.md` from disk directly and is not subject to the same registration cap, though this bypass isn't available for skills invoked by other skills.

**Notes:**
No documented cap exists in Claude Code's skills documentation as of this writing. No fix PR linked on either issue. Given the recurrence across two version eras and roughly 6 months, this should be treated as a persistent, systemic limitation rather than a one-off bug — flag for re-verification on future passes if a maintainer ever comments.

---

### CC-036: `CLAUDE.md` at the directory holding a git repo never gets loaded into sessions started in that repo's own worktrees

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anthropics/claude-code/issues/93010 |
| **Reported** | 2026-09-09 |
| **Last Activity** | 2026-09-09 |
| **Confidence** | Unverified (single reporter, no confirmations yet — but included per the Unverified inclusion bar: directly threatens a core MOSAIC pattern (multi-worktree fan-out), reported on stock Anthropic infrastructure, and backed by concrete, isolated reproduction steps) |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Reproduced on 2.1.266 and 2.1.251 (macOS); reporter confirmed it predates 2.1.251, the oldest build tested |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | bug, has repro, platform:macos, area:core |

**Summary:**
When a git worktree's own directory is a child of the specific directory that holds that worktree's repository (the standard "one directory holds the repo, checkouts are siblings inside it" layout), the `CLAUDE.md` in that holding directory is never loaded into sessions started in the worktree — even though the docs say every ancestor directory up to `/` is read. A six-fixture matrix isolates the exact trigger: it is neither "parent holds a bare repo" nor "child is a worktree" alone, but specifically the pairing "child is a worktree of the repository the parent directory holds." Both `<parent>/CLAUDE.md` and `<parent>/.claude/CLAUDE.md` are affected; a plain (non-worktree) child in the same parent loads correctly, and a worktree of a *different* repository elsewhere also loads the parent's file correctly.

**Impact on Orchestration:**
This is exactly MOSAIC's own multi-worktree fan-out layout — one directory holding the repository with sibling worktree checkouts. Any shared/organizational `CLAUDE.md` rules placed at that holding directory (the natural place for rules meant to apply across all worktrees) will silently never load into any worktree-scoped session, with no error or signal that anything was skipped.

**Evidence:**
- Single reporter (jdavidbush), but with an unusually rigorous methodology: a six-row fixture matrix changing one variable at a time (bare vs. non-bare parent repo, worktree vs. plain-directory child, same-repo vs. different-repo worktree) to isolate the exact trigger condition, using a unique marker string per fixture and fresh sessions each time (memory files are read once at startup) to avoid false positives.
- Reproduced independently across two versions (2.1.266 and 2.1.251) and both `-p` (print) mode and interactive sessions by the same reporter, confirming it is not a recent regression.
- Reporter cross-referenced three related-but-distinct existing issues (#23565, #39920, #27994) covering other worktree/memory-file path-resolution bugs, suggesting a shared underlying path-resolution code area, without conflating them with this specific symptom.

**Workaround(s):**
1. ⭐ Use an `@`-import in each worktree's own `CLAUDE.md` pointing at the shared instructions file, as noted by the reporter. Caveat: this must be repeated per worktree, and because the imported file is itself tracked in git, each worktree's copy only updates when that worktree's branch catches up — the reporter observed a 16-worktree repo where a corrected shared instruction sat unpropagated on 15 worktrees, some 16 commits behind.
2. Place shared rules directly inside each worktree's own directory tree (or rely on the `@`-import workaround above) rather than relying on a parent-of-repo `CLAUDE.md`, since the holding directory is exactly the one directory whose copy is always current yet the one that gets skipped.

**Notes:**
Filed the same day as this backfill pass (2026-09-09); no maintainer response yet — flag for follow-up confirmation on a future pass. No fix PR linked.

---

### CC-037: Hard-coded "Exited Plan Mode" / "Auto Mode Active" notice text is injected at the prompt-assembly layer, never written to the visible transcript

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anthropics/claude-code/issues/80818 (duplicate merged in: #92659) |
| **Reported** | 2026-07-24 |
| **Last Activity** | 2026-09-07 |
| **Confidence** | Confirmed (root-caused by a reporter grepping the strings directly out of the compiled Claude Code Mach-O binary; independently reproduced by at least 3 separate reporters across 5+ CLI versions with structured-attachment-level and binary-level evidence) |
| **Orchestration Impact** | MEDIUM |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Observed 2.1.186 through 2.1.228, and again on 2.1.198 (per most recent comment, 2026-09-07) — long-standing across many releases, not a regression |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | area:core |

**Summary:**
Claude Code's own hard-coded "Exited Plan Mode" / "Auto Mode Active" notice templates (and a related "don't tell the user this" file-modification wrapper) can fire spuriously — in sessions that never entered plan mode and never called `ExitPlanMode` — and are rendered into the model's actual context at the prompt-assembly layer without ever being written to the visible session transcript (`.jsonl`). They persist as structured `attachment` records of type `plan_mode_exit`/`auto_mode` (visible via `jq` inspection), but the rendered announcement text itself is not recorded anywhere human-auditable. A later reporter found the injection correlates specifically with dispatching a subagent via the `Agent` tool (observed both in the main session right after a subagent call and inside the subagent's own context). The wording ("Don't tell the user this... they are already aware") is exactly the kind of phrase security-conscious models are trained to flag as a canonical prompt-injection marker, which cascades into false "injection detected" reports.

**Impact on Orchestration:**
Any transcript-based auditing/logging that MOSAIC relies on to reconstruct exactly what context a primary or subagent session saw will miss this injected notice text, since it is invisible in the recorded transcript despite being genuine model input. It is also confirmed to correlate with `Agent`-tool subagent dispatch specifically — MOSAIC's core delegation mechanism — and has been observed to actually change model tool-selection behavior (one session adopted Bash-only file edits over dedicated Read/Edit tools after the injection fired) and to trigger cascading false positive "prompt injection detected" self-reports from the model, which could confuse orchestration logic that inspects subagent output for anomaly flags.

**Evidence:**
- Original reporter (Nexgate-Miyazaki): 122 assistant-flagged occurrences across main and subagent contexts over 5+ CLI versions (2.1.186–2.1.218); confirmed the text is absent from the transcript but present in the model's own context via a controlled same-session test; later grepped the compiled 2.1.228 binary directly and found both notice templates hard-coded verbatim — ruling out prompt injection from external/local sources.
- Independent reporter (bmetcalf21, 2.1.222/2.1.226): found the underlying structured `attachment` records (`plan_mode_exit`, `auto_mode`) that carry the announcement, and showed spurious events are distinguishable via `planExists:false` plus absence of any `EnterPlanMode`/`ExitPlanMode` call in the transcript; also observed the injected guidance actually altering the model's real tool-selection behavior in 2 of several sessions.
- Independent reporter (marcela-os, 2.1.198): found the injection correlates specifically and reliably with `Agent`-tool subagent dispatch (3/3 times triggered by dispatching a subagent, 0/3 times from ordinary tool calls); ruled out a locally-installed plugin's `SubagentStart` hook and untrusted external content as the source. A duplicate report (#92659) with a fuller writeup was closed in favor of this issue.

**Workaround(s):**
1. ⭐ Do not rely on the visible session transcript (`.jsonl`) alone to audit what context the model actually received — cross-check for the structured `attachment` records instead: `jq -c 'select(.type=="attachment" and (.attachment.type=="plan_mode_exit" or .attachment.type=="auto_mode"))' ~/.claude/projects/*/*.jsonl`. (Source: independent reporter bmetcalf21.)
2. When reviewing subagent transcripts for anomalies, treat a model's own "prompt injection detected" self-report referencing "Exited Plan Mode" or "Auto Mode Active" text as a likely false positive caused by this known harness quirk, not a genuine security event, before escalating it.

**Notes:**
No maintainer acknowledgment or fix PR yet, but the finding is self-confirming (verbatim strings present in the shipped binary) and independently corroborated three times. Reclassified from the original reconstructed "Quirk" label to **Bug** on this backfill pass — this is not model-dependent behavior but a harness-side template/logging defect (spurious firing under the wrong conditions, plus the transcript-recording gap), which fits the Bug definition more precisely than Quirk.

---

### CC-038: Every Bash dispatch in a large or `--resume`d session can freeze the ENTIRE process for ~80-90 seconds

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anthropics/claude-code/issues/89772 |
| **Reported** | 2026-08-26 |
| **Last Activity** | 2026-09-08 |
| **Confidence** | Likely (detailed kernel-level forensic evidence from the original reporter — `/proc` sampling, event-loop stall instrumentation, ruled-out alternative causes — plus one independent confirming reporter with a different sandboxing setup; no maintainer acknowledgment yet) |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 2.1.231; stall duration grows with session/context size (observed 80.2s → 86.6s within one session) |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | bug, has repro, platform:linux, area:core, area:bash, perf:cpu, api:anthropic |

**Summary:**
In a `--resume`d session with a very large context (observed on a 1M-context-window model with several hundred-k tokens used), every `Bash` tool dispatch synchronously blocks the entire process for ~80-90 seconds as an active 100%-CPU spin (not an idle wait — near-zero voluntary context switches throughout, ~2/3 kernel time) before the shell is even spawned; the actual command then completes in under a second. During the block the TUI is fully unresponsive, the API SSE stream and any MCP SSE connections go unserviced/drop, and the block recovers every time (not a permanent hang). Only `Bash` dispatches trigger it — 36 of 40 in one session, 0 of 53 non-Bash (Read/Edit/Grep/WebFetch) dispatches in the same session. A later commenter identified a likely contributing/root cause on Linux: a `Read(~/**/.ssh)`-style managed permission rule combined with a sandboxing tool (bubblewrap) triggers a recursive regex-tested walk of the entire home directory on every Bash dispatch; cleaning up the home directory reduced (but did not eliminate) the freeze.

**Impact on Orchestration:**
Long-running or resumed orchestration sessions that make repeated Bash calls (a routine MOSAIC pattern — orchestrators and subagents both use Bash heavily) risk a large cumulative time cost from repeated ~80-90 second full-process freezes that also stall MCP tool availability during the window; one reporter's session accumulated 44 stalls totaling ~61 minutes of frozen UI. This directly threatens throughput and responsiveness of Bash-heavy, `--resume`d MOSAIC sessions and subagents.

**Evidence:**
- Original reporter (BobMali): rigorous kernel-level forensics — debug-log stall instrumentation showing `cpu ≈ wall` for every stall, live `/proc` sampling during a stall showing the main thread in state `R` at 100% CPU with frozen `voluntary_ctxt_switches`, ruled out endpoint-security software, memory pressure, network/VPN, and hooks as causes, and attached a full sanitized debug log.
- Independent reporter (radcliffkey): confirmed on Ubuntu 24.04 with bubblewrap sandboxing — different environment/setup than the original reporter's.
- Same original reporter later found a workaround/mitigation: a managed-settings `Read(~/**/.ssh)`-style permission rule combined with bubblewrap sandboxing triggers a recursive home-directory walk with per-path regex testing on every Bash dispatch; cleaning up the home directory reduced (not eliminated) the stall by ~30s, pointing at permission-pattern evaluation as at least a contributing cause.

**Workaround(s):**
1. ⭐ Reduce the size/depth of the home directory (or wherever broad glob-based permission rules like `Read(~/**/.ssh)` apply) to shrink the recursive walk the permission-pattern matcher performs on every Bash dispatch — reduced one reporter's stall by ~30s, though did not eliminate it entirely. (Source: original reporter BobMali, self-discovered.)
2. Prefer fresh (non-`--resume`d), smaller-context sessions for Bash-heavy workloads where feasible, since the stall duration and frequency scale with session/context size and does not manifest in small/fresh sessions in either report.

**Notes:**
No maintainer acknowledgment or fix PR yet. Reporter cross-referenced two related-but-distinct open issues (#88257, #89062) covering different CPU-spin symptoms on the same or nearby versions — worth checking in a future pass for a possible shared root cause, but not conflated with this entry since their symptoms (permanent hang, first-prompt-only) differ from this one's (recoverable, scales with Bash dispatch count).

---

### CC-039: `.claude/settings.json` is written via a non-atomic, unlocked read-modify-write — concurrent sessions can tear or clobber it

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anthropics/claude-code/issues/91520 |
| **Reported** | 2026-09-02 |
| **Last Activity** | 2026-09-02 |
| **Confidence** | Likely |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 2.1.246 – 2.1.258 (CLI and VS Code extension) |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | bug, has repro, area:core, platform:vscode, platform:wsl |

**Summary:**
`~/.claude/settings.json` (and by the same code path, project-level `.claude/settings.json`) is persisted with a plain truncating `writeFile` — no advisory lock, no tmp-file+`rename(2)`. With more than one session live, interleaved writes can leave a torn/invalid JSON file (Claude Code then reports "Settings file failed to parse" and silently falls back to defaults, dropping permissions/hooks), or a "lost update" where one session's write silently discards another session's keys. No maintainer response yet; single reporter, but with a deterministic, quantified reproduction (1.3% of reads torn under 6-writer load in isolation) plus 9 banked real-world occurrences over 4 weeks, and the reporter traced the exact non-atomic code path (`writeUserSettingsAndPush` in `extension.js`).

**Impact on Orchestration:**
MOSAIC's multi-agent/multi-session concurrent operation is exactly the kind of scenario that can trigger this — concurrent sessions writing settings (e.g. permission updates) risk corrupting the shared settings file or silently losing each other's writes, changing the effective permission/hook posture of subsequent sessions without warning.

**Evidence:**
- Single reporter (no maintainer acknowledgment, no comments yet as of 2026-09-02), but confidence raised to Likely on the strength of: a deterministic, isolated reproduction harness with quantified failure rate (1.3% torn reads), 9 banked real-world corrupted-file artifacts with matching byte-for-byte shapes, exact code-path identification (decompiled `writeUserSettingsAndPush`), and cross-referencing to several other open/closed issues describing the same underlying symptom class from independent reporters (#79403 VS Code model toggle corrupts settings.json; #82167 settings file corrupted to `{}`; #76749 stale in-memory config re-persisted; #2810).
- `has repro` label applied.

**Workaround(s):**
1. ⭐ Avoid running multiple concurrent Claude Code sessions/windows that write to the same `settings.json` (same user-level or same project-level file) — serialize settings-mutating operations (e.g. `/model`, permission updates) across sessions where possible. (Reporter's own workaround — `chattr +i` plus an inotify auto-repair watcher — is explicitly called out by the reporter as fragile/not recommended; it breaks on extension updates.)
2. No accepted maintainer-provided workaround exists yet; the reporter's suggested real fix (write to a temp file + `fsync` + `rename(2)`, plus an advisory lock across the read-modify-write) is unimplemented as of the report date.

**Notes:**
Backfilled 2026-09-09 by relocating the issue via GitHub search on repo:anthropics/claude-code. No maintainer comments yet — flag for re-check in a future run to see if this receives a triage response or fix PR. Related/possibly-same-root-cause issues worth cross-referencing if investigated separately: #79403, #82167, #76749, #2810.

---

### CC-040: Multi-iteration turns sum token usage ACROSS iterations, inflating apparent context usage 2x-6x — premature compaction or false "Prompt is too long"

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anthropics/claude-code/issues/84738 |
| **Reported** | 2026-08-07 |
| **Last Activity** | 2026-09-08 |
| **Confidence** | Likely |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 2.1.181 through at least 2.1.260 (root cause present in every version scanned; not a recent regression) |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | area:core |

**Summary:**
Any turn with multiple `usage.iterations` (originally observed via the `advisor` tool, but confirmed NOT advisor-specific — any multi-iteration turn triggers it) reports top-level `usage` as the SUM across all iterations rather than the final iteration's value. This inflates apparent context by a factor equal to the number of `message` iterations (2x for 2 iterations, up to ~6x observed, worst case 5.14x on one measurement). Auto-compact eligibility and a separate pre-flight "context too large" check both read this inflated figure, causing compaction hundreds of thousands of tokens before real context justifies it, or — with `autoCompactEnabled: false` — a synthetic session-ending "Prompt is too long" error with no request even sent. Root cause identified in the shipped bundle: the compaction/context-check path (`mFe`) sums `usage.iterations` fields instead of using the already-present iteration-aware helper (`Cta`) that reads only the final iteration.

**Impact on Orchestration:**
Orchestration patterns that use multi-iteration turns — advisor/sub-call loops, or any tool causing multiple model round-trips within one turn — risk premature compaction (losing context MOSAIC still needed) or hard session-ending "Prompt is too long" failures well before actual context limits are reached, wasting budget and disrupting workflows. Subagent (Task tool) seats are hit hardest since they often consult multi-iteration tools while already carrying large working contexts.

**Evidence:**
- Original reporter provided quantified transcript evidence (exact `usage.iterations` breakdowns showing the sum-matches-doubled-total math), a per-version scan of 6,210 session files showing the bug present in every version back to 2.1.181 with zero correctly-reported multi-iteration turns, a controlled A/B repro (`claude -p` with/without an advisor call, identical workload) showing compaction firing only in the advisor-turn session, and exact root-cause code (`mFe` vs `Cta`) from the shipped bundle.
- A second, independent reporter (`bb774-cyber`) confirmed a related failure mode: with `autoCompactEnabled: false`, the same inflated rollup trips a separate pre-flight `blocked` check and ends the session with a synthetic "Prompt is too long" error — corroborating the root cause from a different code path.
- No maintainer acknowledgment yet as of 2026-09-08; capped at Likely rather than Confirmed.

**Workaround(s):**
1. ⭐ Be aware that on-screen/measured context usage can be inflated 2x-6x during any turn that makes multiple model round-trips (advisor calls, similar multi-iteration tool patterns) — do not trust the displayed figure at face value near a compaction or hard-limit threshold. No accepted code-level workaround exists; this requires an Anthropic fix to the `mFe`/`Cta` mismatch.
2. Where possible, avoid triggering iteration-heavy tool calls (e.g. advisor) when a session/subagent is already near its auto-compact threshold, since the inflated rollup can push it over immediately.
3. If using `autoCompactEnabled: false`, be aware the same inflation can trip a separate hard "Prompt is too long" session-ending check rather than compaction — this is not avoided by disabling auto-compact.

**Notes:**
Backfilled 2026-09-09 by relocating the issue via GitHub search on repo:anthropics/claude-code. Related issue: #81029 (main-session manifestation of the same root cause, referenced by the reporter as confirming its own "suspected cause"). Also related: #82863 and #85483 describe adjacent double-counted/net-negative compaction accounting that may share root-cause territory — worth a follow-up pass. Flag for re-verification once/if a maintainer responds or a fix ships.

---

### CC-041: `.claude/settings.json` in an ANCESTOR directory above the project root is silently never loaded

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anthropics/claude-code/issues/85613 (GitHub's "closes" link to PR #85716 is a mismatch — see Notes) |
| **Reported** | 2026-08-10 |
| **Last Activity** | 2026-08-20 |
| **Confidence** | Unverified (downgraded from the reconstructed guess of Likely/Confirmed — see Notes) |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Unknown — reporter did not state a specific version (CLI, Linux) |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | area:core |

**Summary:**
`.claude/settings.json` located in an ANCESTOR directory above the session's project root (e.g. a workspace-of-repos layout with settings one or two levels above the git repo where the session starts) is silently never loaded — `hooks` and `permissions.deny` rules defined there are completely inert, with no warning, log line, or any other indication in the session. The reporter confirmed it via a controlled A/B: moving the identical `hooks` block into `~/.claude/settings.json` (user level) made it fire immediately.

**Impact on Orchestration:**
MOSAIC configurations that place shared/organizational settings (hooks, deny rules) in a directory above individual project roots risk those settings being silently inert with no diagnostic — a security/enforcement gap that is "indistinguishable from working," per the reporter, who believed a `PreToolUse` guard suite was enforcing for weeks when it never ran.

**Evidence:**
- Single reporter with a clear, reproducible layout and a controlled A/B test (ancestor file inert, identical user-level file works). GitHub auto-links this issue as closed by PR #85716 ("fix(hookify): load rules from ancestor .claude directories to prevent silent bypass"), which is still open and untested as of last activity.
- **PR #85716 verified via `pull_request_read` (get_files): its diff touches only `plugins/hookify/core/config_loader.py`** — the bundled `hookify` plugin's own rule loader, which walks ancestor `.claude` directories for `hookify.*.local.md` rule files. This is a different code path from the core `.claude/settings.json` resolution that issue #85613 is actually about. **The PR does not fix the reported defect** — confirms the reconstructed note's original finding.
- A second commenter reports a related-but-distinct failure class (hooks that are registered and "run" but silently do nothing, or run stale logic) and explicitly states they did not reproduce the ancestor-directory behavior itself — so this does not count as independent corroboration of THIS specific claim.
- No maintainer comment/acknowledgment on the core-settings ancestor-loading behavior itself.

**Workaround(s):**
1. ⭐ Place `.claude/settings.json` directly at or below the project root (or at `~/.claude/settings.json`, user level) rather than in an ancestor directory — confirmed by the reporter's own A/B test to reliably restore hook/deny enforcement.
2. Per the reporter's suggestion (unimplemented as of last activity): add a startup warning that names any ancestor `.claude/settings.json` found but not loaded. No such warning exists yet in the harness.
3. Per the second commenter's independent (but broader) advice: don't trust "hook is registered" as proof "hook is enforcing" — add an explicit fired-log line at the top of guard scripts and separately verify the guard's actual side effect, since a hook can be registered, fire, and still silently no-op.

**Notes:**
Backfilled 2026-09-09 by relocating the issue via GitHub search and independently verifying PR #85716's diff via `pull_request_read`. The PR-mismatch finding from the reconstructed session summary is confirmed correct: GitHub's automatic "closed by pull request" link is misleading here — the linked PR fixes an unrelated plugin's ancestor-scan, not the core settings resolution path. Downgraded confidence to Unverified per the CaptureGuide criteria (single reporter, no maintainer confirmation, no independent reproduction of this specific behavior) — kept active given HIGH materiality (silent security-enforcement gap is exactly the kind of orchestration-relevant defect the Unverified inclusion bar calls for) and concrete reproduction steps provided.

---

### CC-042: `/rewind` can silently fail to restore content for a file newly created during the session that was never git-tracked

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anthropics/claude-code/issues/91733 |
| **Reported** | 2026-09-03 |
| **Last Activity** | 2026-09-03 |
| **Confidence** | Unverified |
| **Orchestration Impact** | MEDIUM |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 2.1.236 (macOS) |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | bug, has repro, platform:macos, area:tui, area:core |

**Summary:**
`/rewind` can silently fail to restore file content for a file that was newly created via the Write tool during the session and never git-tracked. Per repro: create a new untracked file, edit it (v1→v2, both via Claude's own tools), then `/rewind` to the prompt before the edit — no restore-type menu (conversation/code/both) appears at all, the conversation is simply rewound and the selected prompt is dropped back into the input as a draft, while the file on disk is left unchanged at the post-edit state. Per Claude Code's own docs (`checkpointing.md`), a code-restore option should appear regardless of git-tracking status whenever the checkpoint has file changes to revert — that contract is violated here.

**Impact on Orchestration:**
Any rollback of a session using `/rewind` may leave newly-created, never-git-tracked files in an inconsistent state relative to the rest of the restored session, without any warning that the restore was incomplete or even attempted — the UI gives no indication a code-restore was skipped.

**Evidence:**
- Single reporter, `has repro` label applied, no maintainer response yet, no other comments. Reproduction steps are concrete and detailed (exact tool sequence, exact expected-vs-actual behavior per the harness's own documented checkpointing contract).

**Workaround(s):**
1. ⭐ Ensure newly created files are git-tracked (`git add`) before relying on `/rewind` to restore them — the reporter's own environment note flags untracked status as the likely differentiator, though this is unconfirmed by the harness team.
2. Independently verify file content on disk after any `/rewind` operation rather than trusting the absence of an error as proof the restore was complete.

**Notes:**
Backfilled 2026-09-09 by relocating the issue via GitHub search on repo:anthropics/claude-code. No maintainer confirmation yet; flag for re-check in a future run. Related `/rewind` reliability issues surfaced in the same search worth a follow-up pass if not already tracked elsewhere in this KB: #87575 (auto-mode system prompt causes `/rewind` to silently fail on Bash-edited files — 15 comments, active discussion), #14002 (`/rewind` restore option shows intermittently or fails), #93045 (`/rewind` doesn't restore deleted tracked git files).

---

### CC-043: statusLine's event-driven refresh has no coalescing/debouncing — a slow statusline command spawns unbounded concurrent copies

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anthropics/claude-code/issues/84124 |
| **Reported** | 2026-08-05 |
| **Last Activity** | 2026-08-25 |
| **Confidence** | Confirmed (maintainer reproduction) |
| **Orchestration Impact** | MEDIUM |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Confirmed present on 2.1.233 (Linux) and the original report's Windows 11 build; docs claim event-driven refresh cancels the in-flight script, which does not hold in practice |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | reproduced |

**Summary:**
The statusLine feature's event-driven refresh has no coalescing/debouncing, and contrary to the documented behavior (a new update trigger should cancel the in-flight script), cancellation does not keep up — if the configured statusline command runs slowly (e.g. ~10s), new refresh invocations start every ~2 seconds regardless, producing up to 5 concurrent copies running simultaneously. The slower the command, the worse the overlap — a self-amplifying rather than self-limiting failure. `refreshInterval` (the only user-facing knob) only governs the periodic timer and does nothing to bound event-driven refresh overlap.

**Impact on Orchestration:**
Kept active despite an initial instinct to dismiss it as "cosmetic" (statusline is a display feature) — its actual failure mode is genuine process/resource exhaustion (unbounded concurrent process spawning that can make the machine progressively less responsive), a real operational hazard rather than merely a display glitch, especially for any MOSAIC deployment using a custom or third-party statusline command that is not trivially fast.

**Evidence:**
- Maintainer/collaborator (`bcherny`) independently reproduced on Linux (2.1.233), observed up to 5 concurrent copies from 17 invocations in ~40 seconds with only 6 completing, and explicitly marked the issue "reproduced" for prioritization.
- `reproduced` label applied by the maintainer.
- Original reporter provided quantified measurements (command timing, process-table snapshots) and ruled out `refreshInterval` as a mitigant.

**Workaround(s):**
1. ⭐ Keep any configured statusline command fast (well under a second) — the maintainer's repro and the original report both show the failure requires a genuinely slow command (~10s); a fast command (e.g. the reporter's own jq-based replacement at ~362ms) does not exhibit the overlap in the same way.
2. No accepted code-level fix exists yet (single-flight/coalescing, an exposed debounce setting, or using `refreshInterval` as a floor for event-driven refreshes were all proposed by the reporter but none implemented as of last activity).

**Notes:**
Backfilled 2026-09-09 by relocating the issue via GitHub search on repo:anthropics/claude-code. Related, worth cross-referencing if investigated separately: #57186 (closed `not planned` — requested a configurable debounce), #73785 (Windows statusline cancellation via `taskkill /T` overloading WMI), #82537 (statusline stops being invoked for a session and never recovers), #86551 (Windows: statusline `pwsh.exe` processes never exit, hundreds of orphans/hour under multi-session use — likely the same resource-exhaustion family, worth a follow-up pass).

---

### CC-044: `tool_result` image content is base64-duplicated once per transcript line, combined with unclamped render cost — can hard-freeze at 100% CPU

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anthropics/claude-code/issues/80175 |
| **Reported** | 2026-07-22 |
| **Last Activity** | 2026-08-25 |
| **Confidence** | Confirmed (maintainer reproduction) |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Reported on 2.1.215/2.1.217; maintainer confirmed the duplication and unclamped-render-cost mechanism still present on 2.1.233. Current releases downscale large image reads (reducing but not eliminating per-line size), per maintainer note. |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | reproduced |

**Summary:**
An image `tool_result` stores its base64 payload TWICE on the same transcript (`.jsonl`) line — once in the tool result's image content block, once in a `toolUseResult.file.base64` mirror field. The TUI's render layout path (Ink reconciler width-measurement) re-measures the full transcript string on every render frame, so multi-MB lines make each frame cost seconds; the render loop keeps scheduling the next frame immediately, starving the event loop entirely (no input, no API request dispatch, SIGINT/SIGTERM ineffective — only SIGKILL recovers). A second, related trigger: an `Edit` on a file containing one multi-MB single line (e.g. minified HTML with an embedded data blob) similarly floods the render tree because `structuredPatch` carries the changed line twice (old and new).

**Impact on Orchestration:**
Any orchestration workflow involving large image reads/edits (e.g. screenshot-based verification steps, or editing files with very long single lines) risks the session becoming completely unresponsive and requiring a hard kill, losing in-flight work and any state not yet persisted.

**Evidence:**
- Maintainer (`bcherny`, COLLABORATOR) independently triaged against 2.1.233 and explicitly confirmed both the double-storage of image bytes per transcript line and the expensive unclamped rendering of the Edit-on-multi-MB-line trigger (measured ~45s CPU and ~1GB extra memory for one Edit's diff render on their test machine); did not reproduce the full hard-freeze at their (faster) test scale but attributed that to hardware speed, consistent with the original reporter's note that slower CPUs hit the wall at smaller payload sizes.
- `reproduced` label applied by the maintainer, who called it "a confirmed bug."
- Original reporter provided detailed V8 stack sampling isolating the exact freeze mechanism (`Bun.stringWidth` called from the Ink layout path every render frame) and a validated data-level mitigation (stubbing out base64 payloads shrank a 21.8MB session to 1.6MB and restored usability).

**Workaround(s):**
1. ⭐ Avoid large image `Read` operations building up in a session's transcript, and avoid `Edit` operations on files containing very long single lines (minified/embedded-data-blob files) — both are confirmed triggers for the same unclamped-render-cost mechanism.
2. If a session is already affected (freezes on resume), the reporter's validated mitigation is to rewrite the session's `.jsonl` file, replacing large base64 payloads with small stubs (a short text note for the image block; a 1x1 PNG for the `toolUseResult.file.base64` mirror) — this is a manual, file-level workaround, not something the harness does automatically.
3. Current releases (per maintainer, as of 2.1.233) downscale large image reads before storage, which reduces but does not eliminate the per-line size and thus the risk — not a full fix.

**Notes:**
Backfilled 2026-09-09 by relocating the issue via GitHub search on repo:anthropics/claude-code. Related issues cross-referenced by the original reporter, worth a follow-up pass if not already covered elsewhere in this KB: #16251 (closed not planned — same class at 50-100KB lines), #19036 (closed as duplicate of #16251), #21567 (open — terminal renderer full-rewrite 100% CPU spin), #51560 (main thread tight loop, 100% CPU, API connections lost).

---

### CC-046: `.claude/settings.json` created mid-session never gets its hooks armed until the session is restarted

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anthropics/claude-code/issues/77357 |
| **Reported** | 2026-07-14 |
| **Last Activity** | 2026-08-21 |
| **Confidence** | Confirmed (maintainer reproduction) |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Reported on 2.1.208 (macOS); maintainer confirmed reproduction on 2.1.233 (Linux) |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | bug, has repro, platform:macos, area:hooks, reproduced |

**Summary:**
If a Claude Code session starts in a directory with NO `.claude/` directory present, then `.claude/settings.json` (with hooks) is created later in that same session — including when Claude Code itself lazily creates the project `.claude/` directory as part of its own atomic-write staging — the hooks never arm until the session is restarted (`--continue` picks them up immediately). The failure is completely silent: no error, no log line, the hook simply never fires. Root cause: the settings-file watcher computes its watch targets once at startup and permanently skips any settings directory that does not yet exist at that instant; nothing watches the project root for a `.claude/` being created afterward. Contrast case confirms this exactly: pre-creating an empty `.claude/` before session start causes hooks written mid-session to hot-reload and fire immediately, with no restart needed.

**Impact on Orchestration:**
Any workflow that provisions `.claude/settings.json` dynamically during a running session (e.g. a setup step that writes hook config before later steps need enforcement, or any orchestration pattern that starts a session before a `.claude/` directory exists and only creates settings/hooks afterward) cannot rely on those hooks being active without a restart — and gets no warning that enforcement is not yet live.

**Evidence:**
- Maintainer (`bcherny`, COLLABORATOR) independently reproduced on 2.1.233 (Linux), confirmed both the failing case and the contrast case (pre-created empty `.claude/` hot-reloads fine), confirmed via debug logs that the project settings file is missing from the watched-files set, and explicitly marked it "a bug."
- `reproduced` label applied by the maintainer; `has repro` label from the original report, which included exact, minimal repro steps.

**Workaround(s):**
1. ⭐ Pre-create an empty `.claude/` directory in the project root BEFORE starting the session (even with no `settings.json` inside it yet) — confirmed by both the reporter and the maintainer to make subsequently-created `.claude/settings.json` hooks hot-reload normally, with no restart required.
2. If `.claude/` was not pre-created and hooks are written mid-session, restart the session (`claude --continue` in the same directory) to pick them up — confirmed to work but breaks in-session continuity.

**Notes:**
Backfilled 2026-09-09 by relocating the issue via GitHub search on repo:anthropics/claude-code. No fix PR linked yet as of last activity — flag for re-check in a future run.

---

### CC-047: A single malformed hook entry silently kills ALL hooks for that event type, persisting across restarts

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anthropics/claude-code/issues/82618 |
| **Reported** | 2026-07-30 |
| **Last Activity** | 2026-08-25 |
| **Confidence** | Confirmed (maintainer reproduction) |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Reported on Claude Code engine 2.1.217-2.1.219 (desktop app); maintainer confirmed reproduction on 2.1.233 (Linux) |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | reproduced |

**Summary:**
One structurally malformed entry appended to a hook event's array (e.g. a hook object placed directly in `hooks.UserPromptSubmit` without the required `{"matcher": "", "hooks": [...]}` wrapper) silently disables ALL hooks for that event — including previously-working sibling entries — with zero feedback (exit code 0, empty stderr, session otherwise normal). The outage persists across app restarts and even a full machine reboot, because the settings file is presumably re-parsed and fails the same way every time; only hand-fixing the malformed entry's structure restores all hooks for that event on the very next message, with no restart needed.

**Impact on Orchestration:**
One bad hook entry in the config — from a script's non-atomic write, a hand edit, or any tool that appends an incorrectly-shaped entry — can silently disable an entire class of hook-based enforcement (e.g. all `UserPromptSubmit` or all `PreToolUse` hooks) persistently across restarts, until the malformed entry is specifically found and fixed. For MOSAIC, any programmatic hook provisioning that appends entries without validating the wrapper shape risks a silent, long-lived enforcement gap.

**Evidence:**
- Maintainer (`bcherny`, COLLABORATOR) independently reproduced on 2.1.233 (Linux) using the exact malformed-shape scenario described, confirmed zero hooks fire for the event once the malformed sibling is added, confirmed no feedback anywhere, and confirmed the fix (re-wrapping the entry) restores all hooks immediately with no restart. Explicitly agreed entries should be validated individually.
- `reproduced` label applied by the maintainer.
- Original reporter reconstructed a precise timeline from session transcripts (88 successful hook fires over 50 hours, then 0/158 fires over 3 days immediately following the malformed append, restored the instant the entry was fixed) — strong causal evidence, not just correlation.

**Workaround(s):**
1. ⭐ Validate hook configuration structure carefully before writing — every entry in a hook event's array must use the `{"matcher": "...", "hooks": [...]}` wrapper shape; a bare hook object placed directly in the array silently kills the whole event's hooks. Any tooling that programmatically appends hook entries (including non-atomic scripted writes) must enforce this shape.
2. If hooks for an event stop firing unexpectedly, inspect `settings.json` for a malformed sibling entry in that event's array rather than assuming a transient failure — restarting/rebooting will not resolve it; only fixing the entry's structure does.

**Notes:**
Backfilled 2026-09-09 by relocating the issue via GitHub search on repo:anthropics/claude-code. Distinct from a different, unrelated malformed-hook-shape issue (#75071) referenced in the original migration note — that report is not this one and is not re-investigated here. Related, possibly-same-root-cause reports the original poster cross-referenced (closed not-planned, may warrant re-check against this confirmed root cause): #56631, #49989, #8810.

---

### CC-048: `WebFetch`'s `prompt` parameter is ignored for pre-approved documentation domains under ~100KB

| Field | Value |
|-------|-------|
| **Classification** | Limitation |
| **Source** | https://github.com/anthropics/claude-code/issues/73514 |
| **Reported** | 2026-07-02 |
| **Last Activity** | 2026-09-05 |
| **Confidence** | Confirmed (maintainer explained and confirmed the by-design behavior) |
| **Orchestration Impact** | MEDIUM |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Present since v2.0.41 (per maintainer bisect); confirmed still present on 2.1.233. Original report on 2.1.198. |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | bug, documentation, has repro, platform:macos, area:tools, reproduced |

**Summary:**
`WebFetch`'s `prompt` parameter (meant to extract/filter specific content via an inner summarization pass) is ignored for the harness's built-in pre-approved documentation domains (e.g. `docs.claude.com`, `platform.claude.com`) whenever the server returns Markdown directly AND the page is under a fixed ~100KB (~25K token) size cap — the full raw Markdown is returned verbatim regardless of the prompt. A maintainer confirmed this is intended behavior (a deliberate context-for-fidelity tradeoff: the extraction pass is lossy, and exact tables/IDs from trusted docs are usually what's wanted), present since v2.0.41 (not a regression), and confirmed extraction DOES run normally for non-exempt pages (a control fetch of a ~600KB raw HTML page returned a properly extracted short answer). The maintainer left the issue open, calling it "confusing and underdocumented," and is considering documenting the exception explicitly or offering an opt-in to force extraction for these pages — as of the last comment (2026-09-05), no fix or documentation update has shipped.

**Impact on Orchestration:**
The hazard is a hidden token-cost blowout: full raw page content (up to ~25K tokens per fetch) is injected into context when only a filtered extract was expected/budgeted for, on every fetch of a pre-approved doc-domain page under the size cap — regardless of how narrow the `prompt` argument is. Any MOSAIC workflow that does multi-page documentation research (a common, encouraged pattern for verifying against live docs) can accumulate this cost across several fetches per session/subagent without any indication it's happening, since the tool result looks like a normal successful fetch.

**Evidence:**
- Maintainer (`bcherny`, COLLABORATOR) independently reproduced with the reporter's exact example, confirmed the size-cap/domain-exemption mechanism precisely (bisected to v2.0.41), confirmed extraction works normally outside the exemption via a control test, and explicitly stated the exemption is intended and the issue is being kept open as a "design/docs-clarity issue."
- A later commenter (2026-09-05) reports continued real-world token cost from this behavior ("burns tokens like crazy"), corroborating ongoing operational impact after the maintainer's explanation.
- `reproduced` and `documentation` labels applied.

**Workaround(s):**
1. ⭐ Budget for the full raw page size (up to ~100KB / ~25K tokens per fetch), not the filtered/prompted size, whenever fetching a pre-approved documentation domain (`docs.claude.com`, `platform.claude.com`, and similar built-in pre-approved doc sites) — the `prompt` argument will not reduce what lands in context for these pages.
2. Where feasible, fetch a smaller, more specific sub-page/section rather than a broad doc page, since the size-cap exemption is per-fetch and there is no way to force extraction for an in-cap page as of the last maintainer comment.
3. A third-party session-file editor (`cozempic`, mentioned in the thread) can retroactively trim oversized tool-result blocks from an already-bloated session — explicitly called out by both the reporter and its own author as a post-hoc symptom workaround, not a fix, and it carries its own risks (auto-updates from PyPI by default, telemetry on by default, edits files that may contain secrets) that a commenter raised and the author confirmed. Not recommended as a primary mitigation; noted for completeness only.

**Notes:**
Backfilled 2026-09-09 by relocating the issue via GitHub search on repo:anthropics/claude-code. Confidence upgraded from the reconstructed guess of "Likely" to "Confirmed" — the CaptureGuide states that for Limitations, Confirmed means a maintainer explicitly stated the behavior is intentional, which is exactly what happened here (with a detailed mechanism explanation and a version bisect). Kept ACTIVE (not moved to resolved) on this review — same reasoning as the original migration note and consistent with CC-011: maintainer-intended does not mean it's not an operational hazard. Title should be understood as "for pre-approved documentation domains where the page is under ~100KB," not a blanket claim about all pre-approved domains.

---
