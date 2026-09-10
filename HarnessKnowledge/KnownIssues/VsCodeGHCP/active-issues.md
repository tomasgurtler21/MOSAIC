# VS Code GitHub Copilot — Active Issues

> Last updated: 2026-09-10 (run 3)

---

### VC-001: Nested runSubagent calls rejected 3 levels deep even when target agent is allow-listed

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/microsoft/vscode/issues/335257 |
| **Reported** | 2026-09-09 |
| **Last Activity** | 2026-09-10 |
| **Confidence** | Unverified |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 1.136.1+ (reporter states it worked "a week or two ago"; broke around 1.136.1); still open on 1.136.2 |
| **Latest Platform Version** | 1.137.0 |
| **Labels** | new release |

**Summary:**
A coordinator->worker->specialist agent chain (A calls B via `runSubagent`, B calls C via `runSubagent`) works at depth 1 (A->B) but is rejected at depth 2 (B->C) with "Requested agent 'C' is not allowed by the current agent" — even when C is explicitly present in both A's and B's `agents:` allow-list and "Allow Invocations From Subagents" is enabled. Single report, no maintainer comment yet, but the issue is assigned to a maintainer (aeschli) and tagged `new release`, indicating triage has started.

**Impact on Orchestration:**
MOSAIC's harness-native execution mode relies on the primary agent dispatching subagents through the harness's native agent tool, and several MOSAIC utility agents (including this one) dispatch further subagents of their own type (a form of nesting). If VS Code Copilot's agent host hard-caps allow-listed delegation at 2 levels regardless of allow-list contents, any MOSAIC pattern that needs a subagent to itself delegate to another subagent on this harness would silently fail with a permission-style error rather than a clear "unsupported nesting depth" message — this could be mistaken for a misconfigured allow-list rather than a harness ceiling.

**Evidence:**
- Single reporter (author_association: NONE), no comments yet as of last check.
- Concrete, step-by-step reproduction provided (3-agent chain, explicit allow-list contents, toggle of "Allow Invocations From Subagents" ruled out as a factor).
- Issue has been triaged (assigned to maintainer aeschli, labeled `new release`) but no explicit acknowledgment yet.
- Meets Unverified inclusion bar: threatens a core MOSAIC delegation pattern (nested subagent dispatch), not on a custom backend, and reproduction steps are concrete rather than vague symptoms.

**Workaround(s):**
1. None reported yet. Possible mitigation if confirmed: avoid designing MOSAIC subagent chains on this harness that require a subagent to delegate to a further subagent (keep delegation to a single hop from the primary/orchestrator) until this is resolved.

**Notes:**
Related/nearby issues worth watching (not deep-dived, surfaced via keyword search near this report):
- #330815 "runSubagent still allows empty agentName and bypasses custom agent allow-list" — opposite failure mode (allow-list bypassed rather than over-enforced) on the same `runSubagent`/allow-list mechanism; suggests the allow-list enforcement path is generally unstable right now.
- #333369 "`runSubagent` bypasses `disable-model-invocation` when `agentName` is omitted" — another allow-list/invocation-gating edge case on the same code path.
- #332423 "Agent Host: failed transcript-less subagent blocks subsequent user turns" — different symptom (hung turn) but same Agent Host subagent-dispatch subsystem; see VC-003 below, which may be a duplicate or closely related to this one.
Needs re-verification once maintainer responds or a fix version is confirmed. **Re-check (2026-09-10, batch 1 of run 3):** No change — still no comments, no maintainer acknowledgment beyond the existing `aeschli` assignment and `new release` label; issue was last touched 2026-09-10T07:27:10Z but that is metadata-only (no new comment text), still 0 comments. Confidence, classification, and impact remain unchanged.

---

### VC-002: Subagent client-tool call can be mis-routed to the parent chat after a VS Code / Agent Host restart

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/microsoft/vscode/issues/331595 |
| **Reported** | 2026-08-19 |
| **Last Activity** | 2026-09-02 (issue itself has no new comments since; the dependency chain around it moved through 2026-09-09) |
| **Confidence** | Unverified |
| **Orchestration Impact** | MEDIUM |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 1.133.x (reproduced on a dev container / remote agent host, Claude model provider); exact behavior remains in flux, now pending #334146 |
| **Latest Platform Version** | 1.137.0 |
| **Labels** | (none surfaced) |

**Summary:**
When a client tool call that started inside a subagent is replayed by the Agent Host SDK after a VS Code / Agent Host restart, the host has no reliable way to determine which chat (parent or subagent) owns it, because the identity edges that normally carry subagent context (`agentID`, `parent_tool_use_id`, the in-memory `SubagentRegistry._innerToParent` map) are only populated by a live stream and don't survive a restart. The reporter (a contributor who did deep root-cause analysis) earlier confirmed the originally-described symptom — the call silently executing on the parent chat with a duplicate — is not reproducible on current `main` because it depends on the restructuring proposed in PR #330933; what remains as a real, current risk is narrower: the inner-to-parent routing edge does not survive a restart, which today manifests as a stalled/parked turn rather than a silent misroute. **Update (run 2):** the dependency chain has moved but not resolved. PR #330933 was closed by its own author — not declined by a maintainer, but *superseded*: it "could not be rebased" after its base PR #330746 (also self-closed, superseded by #330933) was closed, and the code it patched had moved to a different module on `main`. Its replacement, PR #334146 ("Admit a client tool call once, instead of racing two triggers"), carries forward the same subagent-routing fix and is still open, unmerged, with no maintainer review yet — only the same contributor's own comments. So the underlying restructuring this issue is downstream of is still alive, just under a new PR number, and still entirely unreviewed by a VS Code maintainer.

**Impact on Orchestration:**
If MOSAIC's interactive/harness-native mode on this harness has a subagent's tool call outstanding across a VS Code reload or Agent Host restart (e.g. long-running subagent work, extension host crash/restart, or a remote/dev-container session recycling), the tool result could either get silently attributed to the wrong chat (polluting the primary/orchestrator's context with a subagent's tool call) or the turn could stall indefinitely waiting on a signal that will never arrive. Either outcome would corrupt the hub-and-spoke trust model, where the orchestrator/primary agent should only see structured subagent responses via the intended channel.

**Evidence:**
- Reported by a CONTRIBUTOR (higher author association) with an unusually deep code-level root-cause writeup, assigned to maintainer roblourens.
- The reporter's own follow-up comment (2026-09-02) walked back the originally-described exact symptom as not currently reproducible on `main`, and reframed the remaining risk as contingent on other in-flight PRs (#330933, #330746, #334132).
- No maintainer acknowledgment yet; state is fluid and tied to unlanded changes.

**Workaround(s):**
1. None reported. If confirmed to still cause stalled turns, the practical mitigation would be to avoid relying on subagent tool-call continuity across a VS Code/extension-host restart — treat any subagent work in flight during a restart as failed and re-dispatch rather than trusting resumed state.

**Notes:**
This entry tracks an evolving internal Agent Host restart/state-recovery seam rather than a single fixed symptom. Related PRs/issues to watch: #334146 (successor to #330933, the root restructuring this is downstream of — still open, unmerged, unreviewed by a maintainer as of 2026-09-09), #330746 (self-closed, superseded by #330933/#334146, never landed — correcting the run-1 note that called this "already landed"), #330899 (broader client-tool execution design question that started this whole chain; still open, no maintainer decision), #334132 (adjacent transcript-priming defect found during this investigation, still open/unmerged — correcting the run-1 note that called this "already fixed"). Needs re-verification once #334146 lands or is declined — re-check whether the "stalled turn" framing (rather than "misrouted to parent") is the accurate current-state description. Note for future passes: none of PRs #330933, #330746, #330899, #334132, #334072, or #334146 have any maintainer (non-reporter) comment as of this run — this entire cluster of Agent Host subagent-plumbing fixes is being driven by one contributor (RyanEwen) with no visible Microsoft engineering engagement yet, which should factor into confidence assessment on future passes (a self-closed/superseded PR chain with zero maintainer input is weaker evidence of an accepted fix path than it might first appear). **Re-check (2026-09-10, batch 1 of run 3):** No change. Issue still has exactly 1 comment (the reporter's own 2026-09-02 walk-back, already reflected above); PR #334146 still open, unmerged, `mergeable_state: dirty`, still only a bot CODENOTIFY comment (no maintainer review). Confidence, classification, and impact remain Unverified/Bug/MEDIUM.

---

### VC-003: Subagent tool call can hang forever with no timeout, leaving its turn (and the parent's) open indefinitely

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/microsoft/vscode/issues/333931 |
| **Reported** | 2026-09-02 |
| **Last Activity** | 2026-09-02 (issue itself; fix PR #334072 was last updated 2026-09-09 with no new comments) |
| **Confidence** | Confirmed |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 1.135.0, reproduced on both patched and fully stock/reverted builds; dev container / remote agent host, Claude provider |
| **Latest Platform Version** | 1.137.0 |
| **Labels** | (none surfaced) |

**Summary:**
A subagent's tool call (e.g. `Bash`) is announced (`toolCallStart` / `toolCallReady`) and then never executed — no process runs, the agent host log goes silent, and no result, timeout, error, or confirmation prompt ever appears. Deep instrumentation by the reporter traced the exact break: `handleCanUseTool` (the host's permission callback) is entered for the stalled call and never returns — no confirmation is ever surfaced to the user, so there is nothing to click to unblock it. Because the tool call never resolves, the spawning `Agent`/`Task` call never completes, `subagent_completed` never fires, and the only code path that can close a subagent's turn is never reached — so the turn (and the parent's spawning turn) stays open forever with no watchdog to terminate it. Reproduced 6 times via a scripted driver, on both a patched and a completely stock/reverted build, ruling out local patches as the cause. Effect scales with subagent count — one run left 5 of 7 spawned subagent turns permanently open. **Update (run 2):** fix PR #334072 ("Keep observing a subagent whose chat outlives its spawning turn") remains open and unmerged, with no maintainer review or comment — the only activity is the reporter's own detailed PR description. Not yet safe to treat as an imminent fix; still tracked as active.

**Impact on Orchestration:**
This is a direct, severe hit to MOSAIC's core orchestration pattern: a subagent tool call can silently and permanently hang with zero signal to the orchestrator (no error, no timeout, no confirmation prompt) — the orchestrator has no way to detect or recover from this short of an external wall-clock timeout on the whole dispatch. Since it scales with the number of subagents, MOSAIC's multi-agent fan-out patterns (dispatching several subagents in one pass) are more likely to hit it, and a single stalled subagent leaves its work permanently unresolved with no automatic escalation path available to either the harness or MOSAIC.

**Evidence:**
- Contributor-level, systematic reproduction: 6 reproductions via a scripted (non-manual) driver, cross-checked on both patched and fully stock/reverted builds to rule out local causes.
- Root cause traced with process-level (`ps`), log-level, and code-instrumentation evidence down to the exact stalled call (`handleCanUseTool` entered, never exits) across two detailed follow-up comments.
- A fix PR (#334072) is open and linked to this issue, confirming the behavior is accepted as real and being addressed — though as of this run it is still unmerged and has received no maintainer review/comment (author-only activity), so "accepted as real" rests on the reporter's own analysis, not yet on Microsoft engineering confirmation.
- Related issue #332073 reports the same visible outcome (open turns that never close) from a different trigger (closing the window mid-flight), grouped together in #333174 — corroborating that the "turn never closes" failure mode is a known class, not a one-off.
- Assigned to maintainer roblourens.

**Workaround(s):**
1. ★ None from within the harness. The only practical mitigation is external: apply a wall-clock timeout around any subagent dispatch on this harness and treat a hang as a failure requiring re-dispatch, since the harness itself provides no timeout or recovery signal.
2. Avoid spawning subagents in dev container / remote agent host configurations until this is fixed, if repro is confirmed to be specific to that environment (reporter's repro environment was a dev container; not yet confirmed whether local/non-container agent hosts are also affected).

**Notes:**
Likely connected to #330899 (client tool admitted to execution off the stream-mapper ready rather than the runtime invocation) and/or #331595 (VC-002, subagent tool call handled on parent chat) — the reporter explicitly flags both as candidate root causes but has not confirmed which. Also see #332073 and the grouping issue #333174 for the "turn never closes" failure class more broadly. Track PR #334072 for the fix; still open/unmerged/unreviewed as of 2026-09-09. Same contributor (RyanEwen) authors this entire cluster (VC-002, VC-003, VC-004, and PRs #330933/#330746/#330899/#334132/#334146) with no confirmed maintainer engagement across any of it yet — re-verify once #334072 merges (or is superseded, as happened to #330933) and re-check whether the fix addresses the root cause (missing reaper/watchdog) or only the specific symptom reproduced here. **Re-check (2026-09-10, batch 1 of run 3):** No change. Issue still has exactly the same 2 comments recorded above (last dated 2026-09-02). PR #334072 still open, unmerged, `mergeable_state: dirty`, no maintainer review or comment beyond assignment. Confidence, classification, and impact remain Confirmed/Bug/HIGH.

---

### VC-004: Forked Skill subagents have no correlation handle from tool call to agent id — invisible to the Agent Host UI

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/microsoft/vscode/issues/334764 |
| **Reported** | 2026-09-06 |
| **Last Activity** | 2026-09-09 |
| **Confidence** | Unverified |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | agent host in dev container, `@anthropic-ai/claude-agent-sdk` 0.3.239 |
| **Latest Platform Version** | 1.137.0 |
| **Labels** | (none surfaced) |

**Summary:**
A `Skill` tool call that forks execution (e.g. `/code-review`, or any skill with forked/subagent execution) actually runs as a real subagent with its own transcript, but the Agent Host never recognizes it as a subagent because `SUBAGENT_TOOL_NAMES` only contains `Task` and `Agent`. The subagent's work (findings, narration content) is generated but never surfaced anywhere in the UI — no subagent chat channel, no card, no way to open it — while the parent chat narrates conclusions ("F2 confirmed", "Eight confirmed by reading the code") that reference content the user was never shown. Root cause is deeper than just adding `Skill` to the recognized tool set: none of the three existing strategies used to correlate a tool call to its spawned agent id work for forked `Skill` calls (no synthetic `agentId` suffix in the result text, no prompt to pattern-match, and the native lookup is an unimplemented placeholder), so the correlation handle genuinely doesn't exist yet. Single report, very detailed technical root-cause analysis, no comments yet.

**Impact on Orchestration:**
This directly implicates MOSAIC's Skill mechanism (`Catalog/Skills/`) and the fork execution pattern MOSAIC agents use (including this very agent's own "fork" subagent dispatch pattern) when running on this harness: a forked Skill's output becomes invisible to the invoking agent's actual UI/transcript surface even though the model narrates conclusions drawn from it. For MOSAIC specifically, if a deployed agent's Skill invocation forks into a subagent on this harness, the user (or any downstream log/audit trail relying on the visible chat) loses the actual work product and only sees the primary's summary of it — undermining transparency and auditability of the orchestration trail, and making debugging a bad Skill result much harder since the underlying transcript is unreachable from the UI.

**Evidence:**
- Detailed, code-level root-cause analysis from a CONTRIBUTOR: traced through `claudeSubagentRegistry.ts` (`SUBAGENT_TOOL_NAMES` set), all three existing correlation strategies (`TextSuffixStrategy`, `PromptMatchStrategy`, `NativeStrategy`), and confirmed via byte-level transcript inspection (590,596-byte subagent transcript, 11,942-character result text checked for the missing `agentId` suffix) that none can resolve a forked `Skill` call to its agent id.
- Confirmed that enumeration itself works (`listSubagents`/`getSubagentMessages` find the agent) — the defect is specifically the correlation handle from the originating tool call to the agent id, not general subagent tracking.
- Assigned to maintainer hediet, but no comments/acknowledgment yet.
- Single reporter — meets Unverified inclusion bar via concrete reproduction, non-custom-backend (stock SDK 0.3.239), and direct threat to a core MOSAIC pattern (Skill + fork subagent visibility/correlation).

**Workaround(s):**
1. None available from within the harness — the correlation handle needed to fix this doesn't currently exist in any accessible form.
2. Mitigation for MOSAIC: don't rely on the VS Code Copilot UI/transcript to audit a forked Skill's underlying work on this harness; if audit trail matters, prefer having the forked subagent explicitly write findings to a file/artifact (which MOSAIC's blackboard pattern already does) rather than depending on the primary's narration or the harness's own subagent UI.

**Notes:**
Reporter also flagged an unrelated observation from the same logs (not filed separately): the host discards SDK messages of type `command_lifecycle` for having no turn id — this type is untyped/unhandled in the SDK type defs the host uses. Worth watching but not tracked as its own entry since it's noted only as an aside with no operational detail. Reporter is the same contributor (RyanEwen) behind VC-002 and VC-003, all part of a broader ongoing investigation into Agent Host subagent plumbing.

**Update (run 2, follow-up pass):** The same contributor filed a closely related issue, #334631 ("Agent host renders nothing for a skill that runs in a subagent"), confirming the same overall theme — Skill-run subagents are not properly integrated into the Agent Host's subagent tracking — from a different angle: `SubagentRegistry` is keyed on a tool-use id that a skill-spawned agent never has (it's spawned by the skill runner, not a `Task` call), so it's never registered, and separately the SDK's final `result` message (which carries the entire output when the parent model itself never streamed any response parts) is read for its uuid and then discarded without its text ever being surfaced. Two further severe symptoms surfaced in that issue's comments, both from the same contributor: (a) a `run_in_background: true` subagent launch is marked `presentation: "hiddenAfterComplete"` — correct for a synchronous subagent but wrong for a backgrounded one, since the *launching* tool call completes in ~2 seconds while the actual agent runs for many more minutes, so the only UI affordance disappears almost immediately even though the agent is genuinely still working (confirmed: 286 further records delivered to the subagent's own channel after the launch call's presentation was hidden, over an 18-minute run); (b) a chat whose *first* turn is a cancelled skill-subagent turn becomes **permanently unusable** — the CLI persists no conversation for that turn (since the parent model never actually ran), so `resume` fails with "No conversation found," and the natural recovery (spawn fresh with the same `sessionId`) also fails with "already in use" because the dying process's transcript file still exists and nothing awaits its exit before reuse. This last failure mode (permanently wedged chat/session) is a correctness/data-loss-adjacent risk worth flagging distinctly for MOSAIC: a headless run that cancels a skill-subagent turn early (e.g., on a timeout) on this harness could permanently strand that session rather than being able to retry in it. Cross-referenced from VC-011's Notes. Raises this entry's evidence: two independent contributor-authored root-cause investigations converge on the same theme (Skill-as-subagent is not first-class in the Agent Host's subagent machinery), though confidence remains Unverified pending maintainer engagement on either issue. Worth checking this contributor's other recent issues on future passes — #332087 ("responses render only at turn end," own fix PR #332122) is a related but not-yet-investigated candidate from the same "Cause two: provider parity" cluster documented in #333174.

**Re-check (2026-09-10, batch 1 of run 3):** #334764 itself: still 0 comments, still no maintainer engagement beyond the `hediet` assignment, no linked fix PR. #334631: one additional third-party comment since last capture — user `rrubberr` (author_association NONE) reports general agent instability/errors since ~1.132.0 with a screenshot, asking if it's the same issue; RyanEwen's reply clarifies their own symptom is distinct (no stop button, agent claims it will report back but never does, animation just stops) — this is a plausible but unconfirmed independent corroboration of the broader "backgrounded subagent goes silently invisible" theme, not a clean duplicate of either specific sub-symptom. Not strong enough to raise confidence above Unverified (still no maintainer confirmation, and the corroboration itself is ambiguous about which exact defect it matches), but worth noting as the cluster's first non-RyanEwen data point. Confidence, classification, and impact remain Unverified/Bug/HIGH.

---

### VC-005: "Agents" window spawns a subagent with the selected custom agent, but the main chat itself ignores the custom agent's tool restrictions

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/microsoft/vscode/issues/334784 |
| **Reported** | 2026-09-06 |
| **Last Activity** | 2026-09-06 |
| **Confidence** | Unverified |
| **Orchestration Impact** | MEDIUM |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 1.136.1, Linux |
| **Latest Platform Version** | 1.137.0 |
| **Labels** | triage-needed, new release |

**Summary:**
Reporter states that selecting a custom agent (`.agent.md` chat mode) from the dropdown and sending a message causes Copilot to spawn a subagent using the selected custom agent, but the main/primary chat itself does not adopt the custom agent's instructions or tool restrictions — it remains unrestricted and can execute tools the custom agent's definition should have disallowed. Report is brief (3-step repro, no logs, no screenshots) and has received no comments or maintainer engagement beyond automated triage labeling as of this scan; the exact UI surface involved ("Agents windows" — possibly VS Code's dedicated Agents/background-sessions panel rather than the standard inline chat mode picker) is somewhat ambiguous from the report alone.

**Impact on Orchestration:**
If accurate, this would mean a custom agent's declared tool allow-list is not actually enforced on the session that is nominally running as that agent — a direct breach of the tool-scoping isolation MOSAIC relies on when deploying agents with restricted toolsets (e.g., a review-only subagent that should not have write access). The main risk for MOSAIC is silent over-permissioning: an agent intended to be read-only or narrowly scoped could execute disallowed tools without any error, which is a correctness/safety issue rather than a mere inconvenience.

**Evidence:**
- Single reporter (author_association: NONE), no comments, no independent confirmation.
- Report is thin: no logs, no screenshots, no detail on which specific tools were unexpectedly permitted, and the report's own wording is somewhat self-contradictory (a subagent is spawned "with the selected custom agent" but then describes "the main chat" — the exact chat/session topology being described is unclear).
- Auto-labeled `triage-needed` and `new release`; assigned to zhichli but no confirmation yet.
- Included cautiously: tool-scoping enforcement is a core MOSAIC concern (Domain filter), not a custom backend, and the repro steps — while brief — are concrete enough to attempt reproduction. However, this is a weaker Unverified case than VC-001/VC-004 due to the report's vagueness about which UI surface and exact tool-permission mechanism is involved.

**Workaround(s):**
1. None reported. Until clarified, MOSAIC users deploying tool-restricted custom agents to this harness should manually verify (e.g., by attempting a disallowed tool call) that the restriction is actually enforced in their specific session type, rather than assuming the `.agent.md` allow-list is authoritative.

**Notes:**
Needs re-verification with a more precise reproduction before confidence can be raised — specifically, clarify whether "Agents windows" refers to VS Code's Agents/background-sessions panel (a specific feature for running agents outside the main chat) versus the standard chat-mode picker, since the two have different architectural implications for MOSAIC (MOSAIC's harness-native mode primarily uses inline chat/agent mode, not necessarily a separate background-agents panel). If a future pass finds this is specific to the background Agents panel and MOSAIC doesn't use that surface, this should be re-assessed for the Materiality filter.

**Re-check (2026-09-10, batch 1 of run 3):** No change. Still 0 comments, no maintainer engagement beyond `triage-needed`/`new release` labels and `zhichli` assignment, still no clarification of which UI surface ("Agents windows") is meant. Not yet 6 weeks stale (4 days since last activity), so no re-verification flag needed yet, but this remains the weakest-evidenced entry in this batch. Confidence, classification, and impact remain Unverified/Bug/MEDIUM.

---

### VC-008: Terminal auto-approval fails for commands containing a bare `--` token (e.g. `git diff -- <path>`)

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/microsoft/vscode/issues/321748 |
| **Reported** | 2026-06-17 |
| **Last Activity** | 2026-09-07 |
| **Confidence** | Confirmed |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 1.123.2 through at least 1.134.0 (confirmed still occurring by a commenter on 1.134.0; fix not yet merged as of 1.137.0) |
| **Latest Platform Version** | 1.137.0 |
| **Labels** | (none applied; assigned to anthonykim1) |

**Summary:**
`chat.tools.terminal.autoApprove` rules fail to match any command containing a standalone `--` token (e.g. `git diff -- file.txt` is NOT auto-approved by a `"git diff": true` rule, even though `git diff file.txt` is). Root cause identified by a community contributor: on PowerShell (the default integrated shell on Windows), the `tree-sitter-powershell` grammar used to parse the command for auto-approve matching throws a parse error on a bare `--` token, which derails rule matching. A community member reports the bug is not isolated to `git diff` — other commands containing `--` are similarly affected. A community-contributed fix (masking the token before parsing, same technique used for a related prior issue #294010) exists; a PR (#335465) is open but not yet merged as of this scan.

**Impact on Orchestration:**
MOSAIC's headless/CLI-driven execution model depends on `chat.tools.terminal.autoApprove` rules working reliably so terminal tool calls proceed without a human in the loop. Since the underlying defect is in the shared command parser (not `git diff`-specific), any auto-approved command pattern that legitimately uses a standalone `--` pathspec/argument separator (common in git, npm, and many CLI tools) will unexpectedly fall through to an interactive approval prompt on Windows/PowerShell — which blocks or stalls unattended orchestration runs waiting on approval that never comes.

**Evidence:**
- Multiple independent reporters confirm the same symptom across versions 1.123.2 through 1.134.0 (stmax82, ckchessmaster, grebdioZ).
- A community contributor (iket0731) identified the exact root cause (tree-sitter-powershell ERROR node on bare `--`) and points to a prior fix for the same bug class (#294010).
- A fix PR (#335465, "Fix PowerShell parsing for standalone `--` tokens") is open, confirming the behavior is real and accepted as a defect — not yet merged.
- 7 upvotes (+1 reactions) on the original report.

**Workaround(s):**
1. ★ None fully reliable. One user asked for alternatives "other than blank auto-approve" and got no answer — i.e., the only known workaround reported in-thread is disabling command-specific matching and using a blanket auto-approve rule, which trades away the safety benefit of scoped allow rules.
2. Avoid shell-specific reliance on `--` separators in auto-approve rule design where possible (e.g., approve `git diff` broadly rather than expecting the `--` form to match a narrower rule) until the fix lands.

**Notes:**
Same underlying parser bug class as #294010 (already fixed there for a different trigger) — the fix technique (masking `--` before tree-sitter parse) is expected to generalize. Needs re-verification once PR #335465 merges and ships in a release; check whether the fix is scoped to `git diff` specifically or the general PowerShell command parser (community evidence suggests the latter, which would be the more impactful fix). Flagging as HIGH impact rather than MEDIUM because the defect sits in the general auto-approval matching path MOSAIC depends on for headless operation, not just a single git subcommand.

**Re-check (2026-09-10, batch 2 of run 3):** No change. Issue still open, 6 comments (unchanged since last capture), still no maintainer comment (only the auto-triage bot). PR #335465 still open/unmerged, authored by the same community contributor who diagnosed the root cause (iket0731), not the original reporter. Confidence, classification, and impact remain Confirmed/Bug/HIGH.

---

### VC-009: Agent mode hallucinates unrelated completed task and derails into autonomous unrelated work in long, high-token stateful sessions

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/microsoft/vscode/issues/332404 |
| **Reported** | 2026-08-24 |
| **Last Activity** | 2026-09-04 |
| **Confidence** | Unverified |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 1.135.0-insider and 1.137.0-insider (recurred on a newer build, so not fixed between the two occurrences) |
| **Latest Platform Version** | 1.137.0 |
| **Labels** | (none applied; assigned to roblourens) |

**Summary:**
In long-running Agent mode sessions using the Responses API's stateful conversation chaining (`previous_response_id`) at very high input-token counts (~340k-450k tokens, largely cached), the agent abruptly asserts that an unrelated, nonexistent task (e.g., a frontend web app) was already implemented and tested — with no such task ever requested anywhere in the visible conversation, system prompt, or tool outputs — then autonomously acts on that false belief: creates project scaffolding, installs npm packages, starts dev servers, and runs builds, all unrelated to the actual assigned task. The reporter observed this twice in independent sessions with the same model (GPT-5.6 Sol) and the same stateful-chaining conditions; the second occurrence happened after only modest (~9.5KB-16KB) tool output, narrowing the likely trigger from "large tool output" toward "long stateful conversation / high input token count" in general. No maintainer response yet beyond triage assignment.

**Impact on Orchestration:**
This is a conversation-stability failure directly in MOSAIC's Domain filter (context/conversation stability at high token counts) and highly material to MOSAIC's Materiality filter: MOSAIC's long multi-stage orchestration runs and long-lived primary-agent sessions can plausibly accumulate large context (hundreds of thousands of tokens with heavy caching) exactly like the reporter's sessions. If an agent silently substitutes a hallucinated "already completed" state for the real task and begins autonomous unrelated filesystem/process/package actions, an unattended headless MOSAIC run could burn significant time/resources on invented work, leave the real task's changes uncommitted, and produce a misleading "success" report to the orchestrator — a severe, hard-to-detect failure mode for automated fan-out workflows.

**Evidence:**
- Two independent occurrences by the same reporter, in different sessions, with concrete telemetry (input/output/cached token counts, response IDs, tool-output sizes) for each.
- Reporter performed diligent falsification: searched the full local transcript log for any trace of the invented task/content and found zero occurrences, ruling out a locally-visible prompt-injection source.
- Consistent trigger profile across both occurrences: GPT-5.6 Sol model, Responses API with `hasPreviousResponseId: true`, `truncation: disabled`, very high input token counts (340k+ and 448k+).
- No maintainer confirmation yet; issue is only assigned (roblourens), not acknowledged in comments. Single reporter overall (no third-party corroboration found).
- Meets the Unverified inclusion bar: threatens core conversation-stability/primary-agent-behavior pattern central to MOSAIC's long-running orchestration sessions; on stock first-party Copilot infrastructure (GPT-5.6 Sol via Copilot's own Responses API backend, not a custom `ANTHROPIC_BASE_URL`/BYOK proxy); reporter provided detailed, reproducible telemetry rather than vague symptoms.

**Workaround(s):**
1. None reported in-thread yet. Plausible mitigation for MOSAIC until resolved: avoid letting a single VS Code Copilot Agent-mode session accumulate hundreds of thousands of tokens of stateful history — prefer periodic context resets/compaction or splitting long orchestration work across fresh sessions/subagent dispatches rather than one continuously growing conversation.
2. Monitor for unexpected task-switch language ("already implemented", "tests passed" with no corresponding tool calls) as a detection signal, since the reporter notes these claims appeared without supporting tool-call evidence.

**Notes:**
Root cause is unclear — the reporter (and issue) cannot distinguish between model-level degradation at long context, corrupted server-side Responses-API state (`previous_response_id` chaining is opaque to the client), or a client-orchestration bug in how VS Code assembles/re-sends the stateful conversation. Classified as Bug per default (unintended, clearly incorrect behavior) rather than Quirk, but reclassify to Quirk if a maintainer later attributes this to model-level behavior specific to GPT-5.6 Sol. Needs re-verification for reproduction on non-Sol models and on stable (non-Insiders) builds. Watch for maintainer engagement — currently only triage-assigned.

**Re-check (2026-09-10, batch 2 of run 3):** No change. Still exactly 1 comment (the reporter's own 2026-09-04 second-occurrence report, already reflected above), no maintainer engagement beyond the existing `roblourens` assignment, no linked fix PR. 6 days since last activity — not yet stale enough for a "Needs re-verification" flag on the 6-week threshold, but worth watching given the single-reporter status. Confidence, classification, and impact remain Unverified/Bug/HIGH.

---

### VC-015: Copilot edit tools report "Updated" success even when the underlying file save fails

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/microsoft/vscode/issues/331790 |
| **Reported** | 2026-08-20 |
| **Last Activity** | 2026-08-24 |
| **Confidence** | Unverified |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 1.134.0 stable, Windows 11; reporter's repro uses a custom `FileSystemProvider` (virtual scheme, remote backend) but the described root cause is architectural, not scheme-specific |
| **Latest Platform Version** | 1.137.0 |
| **Labels** | (none applied; assigned to roblourens) |

**Summary:**
Copilot's edit tools (`replace_string_in_file`, `apply_patch`, `insert_edit_into_file`) apply changes by streaming text edits into the chat-editing session via `ChatResponseStream.textEdit`, and decide "successfully edited" *before* the actual downstream save/`writeFile` call resolves — the tool never awaits or inspects the save's outcome. The reporter (an extension author) demonstrated this with a custom `FileSystemProvider` whose `writeFile()` legitimately throws on backend rejection (network/remote save failure): the edit tool still reports "Updated" and the agent proceeds believing the change landed, when nothing was persisted. The only post-edit signal the tool reads back is file diagnostics, and a `FileSystemError` from `writeFile()` isn't part of that channel, so it's invisible unless the model happens to re-check diagnostics (observed to depend on reasoning effort — higher-effort Claude models sometimes catch it via diagnostics side-effects, default settings often don't). No maintainer comment yet beyond assignment.

**Impact on Orchestration:**
This is exactly the failure class MOSAIC's tool-execution trust model cannot tolerate: an edit tool call that reports success while the file was never actually written. Although the reporter's concrete trigger is a custom remote `FileSystemProvider`, the root cause described is that the tool's success signal is decided independently of the save's actual outcome — a property of the edit-tool architecture itself, not unique to virtual filesystems. Any condition where a local save can fail asynchronously after the tool has already returned "success" (permission errors, disk full, file locks, remote/networked workspaces such as dev containers or WSL/SSH remotes, which MOSAIC's headless runs may use) could trigger the same silent-loss behavior. A MOSAIC agent building subsequent steps on an edit it believes landed — when it didn't — would produce corrupted, hard-to-detect orchestration state.

**Evidence:**
- Single reporter (author_association: NONE), but with detailed code-level root-cause analysis (traced through `ChatResponseStream.textEdit` and the diagnostics-only feedback channel) and a concrete, working extension-based reproduction.
- Assigned to maintainer roblourens; no comments or acknowledgment yet as of this scan.
- Meets the Unverified inclusion bar: threatens a core MOSAIC tool-execution trust assumption (edit tool success = file written), not a custom model backend (this is a VS Code core file-write architecture issue, unrelated to which LLM backend is used), and the reproduction is concrete and code-traced rather than a vague symptom report.
- Caveat lowering confidence: the concrete repro is on a non-default (custom virtual scheme) `FileSystemProvider`; whether this reproduces on the standard local-disk or remote-SSH/dev-container filesystem providers VS Code ships with is unconfirmed. Flagged for re-verification.

**Workaround(s):**
1. ★ Partial, from the reporter: publish/watch an error diagnostic on the file when a save fails, since the edit tool re-reads diagnostics after each edit — this makes failures observable but only if the model chooses to re-check them (unreliable, and not something MOSAIC controls from outside the extension).
2. Mitigation for MOSAIC: after any edit tool call on this harness where the file lives on a non-trivial storage layer (remote/dev-container/network mount), have the agent (or an external check) verify the file's actual on-disk content/mtime rather than trusting the tool's "Updated" result at face value — especially for edits the orchestrator will build subsequent steps on.

**Notes:**
Needs re-verification to determine whether this reproduces with VS Code's own built-in filesystem providers (local disk, Remote-SSH, dev containers) or is strictly isolated to third-party `FileSystemProvider` extensions — this materially changes the Materiality assessment since MOSAIC agents typically don't run against custom virtual-scheme providers, but frequently do run in dev containers / remote workspaces where the underlying save path is not a plain local write. Suggested fix in the issue (await the save, propagate a `writeFile` rejection into the tool result as a failure) has no linked PR yet.

**Re-check (2026-09-10, batch 3 of run 3):** No change. Issue still has 0 comments, still assigned only to `roblourens` with no maintainer acknowledgment, still no linked fix PR (`closed_by_pull_requests` empty). 17 days since last activity (2026-08-24) — not yet 6-weeks stale, no re-verification flag needed yet, but this entry's open question (does it reproduce on stock local/remote-SSH/dev-container providers, not just a custom scheme) remains unanswered. Confidence, classification, and impact remain Unverified/Bug/HIGH.

---

### VC-010: `runSubagent` skips allow-list/invocation-gating validation entirely when `agentName` is omitted

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/microsoft/vscode/issues/330815 (allow-list bypass) and https://github.com/microsoft/vscode/issues/333369 (`disable-model-invocation` bypass) — same code defect, two configuration flags it defeats |
| **Reported** | 2026-08-14 (#330815), 2026-08-30 (#333369) |
| **Last Activity** | 2026-08-30 |
| **Confidence** | Unverified |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 1.132.x-1.136.0-insider; reporter of #330815 confirmed still present on 1.133.0 |
| **Latest Platform Version** | 1.137.0 |
| **Labels** | (none surfaced; #330815 assigned to aeschli, #333369 assigned to roblourens) |

**Summary:**
`runSubagentTool.ts`'s `invoke()` only validates the requested agent against the current mode's `agents:` allow-list (`validateSubagentAllowed`) and `disable-model-invocation` visibility when `agentName` is explicitly provided and non-empty (`if (subAgentName) { this.validateSubagentAllowed(...) ... }`). When `agentName` is omitted or empty, the code falls into an `else` branch that clones the *current* mode's instructions with **no validation at all** (`effectiveSubAgentName = subAgentName ?? currentModeInstructions?.name`). This produces two concrete, independently-filed bypasses of the same branch: (1) #330815 — a custom agent not present in its own `agents:` allow-list can still self-invoke via `runSubagent` with an empty `agentName`, letting it recursively spawn itself despite the allow-list forbidding it; (2) #333369 — an agent marked `disable-model-invocation: true` (meant to be invoked only by the user, never as a subagent) is cloned into a real subagent anyway when `agentName` is omitted, and #333369's root-cause analysis shows the flag is *only* a prompt-visibility filter with zero runtime enforcement in `runSubagentTool.ts` — even an **explicit** `agentName` naming that agent would bypass it, since no code path checks `visibility.agentInvocable`. Blast radius is capped at one recursion level by `chat.subagents.allowInvocationsFromSubagents` defaulting to `false` (which zeroes `maxDepth` for the spawned clone), but the bypass itself is unmitigated. #330815's reporter self-confirmed reproducibility on a retest (1.133.0); #333369 has detailed code-level analysis (exact file/line references) but is un-commented and un-acknowledged.

**Impact on Orchestration:**
This is a direct hit on MOSAIC's tool/agent-scoping trust model — the opposite failure mode from VC-001 (over-enforcement) and VC-005 (main chat ignoring restrictions): here, the allow-list mechanism itself is silently skippable by a specific call shape (omitted `agentName`), which a model can produce without any adversarial intent — it's the "use the current agent as a fallback" path, not an edge case requiring unusual input. Any MOSAIC deployment relying on a harness-declared `agents:` allow-list or `disable-model-invocation: true` to keep an agent from spawning itself or an unintended clone (e.g., to bound recursive fan-out, or to keep a manual-only reviewer agent from being invoked autonomously) cannot trust that restriction on this harness — a model that simply omits `agentName` defeats both checks.

**Evidence:**
- #330815: single reporter (NONE), reporter self-confirmed the bug persisted on a later retest (1.133.0); assigned to maintainer aeschli; references two prior closed issues (#306266, #306568) targeting the same behavior for 1.114/1.115, meaning this is a **regression or incomplete fix** of a previously "fixed" issue.
- #333369: single reporter (NONE, though issue states "confirmed by a human"), with an unusually precise code-level root-cause trace (exact file/line references into `runSubagentTool.ts`, `chatModel.ts`, `promptsServiceImpl.ts`, and the specific test — `runSubagentTool.test.ts:1214` — that currently locks the buggy behavior in as expected). No comments yet; assigned to roblourens.
- Meets the Unverified inclusion bar: directly threatens MOSAIC's tool/agent-scoping isolation model (Domain filter), not a custom backend (BYOK/Qwen noted in #333369 but the defect is explicitly in harness dispatch logic, not model-specific), and both issues provide concrete, code-traced reproduction steps rather than vague symptoms.

**Workaround(s):**
1. None from within the harness. Until fixed, MOSAIC should treat `agentName`-omitted `runSubagent` calls as a way for any agent with `agents` tool access to spawn a clone of itself regardless of allow-list or `disable-model-invocation` configuration — design custom agent prompts to always explicitly instruct naming the target agent, though this relies on model compliance rather than harness enforcement and is not a real mitigation for adversarial or accidental omission.
2. Where feasible, avoid granting `runSubagent`/`agents` tool access to any custom agent that must never self-invoke or must remain manual-only, since `disable-model-invocation` cannot be relied on to prevent it.

**Notes:**
Same underlying code branch (`runSubagentTool.ts` invoke(), the `else` branch taken when `agentName` is falsy) as the root cause of both reports — combined into a single entry rather than two near-duplicates per the CaptureGuide's "verify duplicate grouping yourself" rule. Also related to the broader "allow-list enforcement is generally unstable" theme flagged in VC-001's Notes. #330815 further references #306266/#306568/#306871 as prior related issues closed against 1.114/1.115 — worth checking on a future pass whether those PRs' fixes were reverted or were narrower than this current regression. Needs re-verification once either issue receives a maintainer response or a fix PR.

**Re-check (2026-09-10, batch 2 of run 3):** No change to either issue. #330815 still 2 comments (last 2026-08-18, the reporter's own 1.133.0 re-confirmation), still no maintainer response beyond the `aeschli` assignment, no linked fix PR. #333369 still 0 comments, still no maintainer response beyond the `roblourens` assignment, no linked fix PR. Neither is yet 6 weeks stale (23 and 11 days since last activity respectively). Confidence, classification, and impact remain Unverified/Bug/HIGH.

---

### VC-011: Background/parallel subagent output and completion signals are silently dropped once the parent's turn ends (recoverable from transcript, lost from live UI and possibly from orchestrator-visible completion)

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/microsoft/vscode/issues/332073 |
| **Reported** | 2026-08-22 |
| **Last Activity** | 2026-09-09 |
| **Confidence** | Confirmed |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 1.1.3 (reporter's initial build string, likely truncated/typo for a 1.1xx release) through 1.136.1; dev container / remote agent host, Claude provider |
| **Latest Platform Version** | 1.137.0 |
| **Labels** | (none surfaced; assigned to roblourens) |

**Summary:**
When a Claude agent-host turn spawns background subagents and the parent's own turn ends before they finish, every further SDK message the host receives is silently discarded by `ClaudeSdkMessageRouter.handle`, because it derives the active `turnId` from the prompt queue head (`peekParent()`), which is now empty/undefined — the router's drop path (`if (turnId === undefined) return;`) is silent at every log level. The work is real and fully recorded in the on-disk transcript (recoverable by restarting the agent host, which replays it), but nothing further renders live, and — critically for orchestration — `subagent_completed` and related signals are dropped by the same class of gate, so the host never learns a subagent finished. The reporter (contributor RyanEwen, same as VC-002/VC-003/VC-004) demonstrated at least three distinct triggers for the same terminal symptom, all confirmed live: (1) the base case — parent turn ends normally while subagents keep running; (2) closing the VS Code window mid-flight, which additionally destroys the in-memory subagent registry so the anchor needed to route resumed output can't be reconstructed even by the (already in-flight) fix for case 1; (3) a separate host-side gate in `AgentSideEffects` that drops a subagent's signals wholesale once its own chat has no "active turn" — observed dropping 92 signals across 5 signal types in one session, with 8 subagent turns started and 0 `subagent_completed` signals ever delivered. A fourth finding in the same thread: `AskUserQuestion` calls issued while the queue is empty are silently auto-denied (`Cancel`/"user cancelled" within milliseconds) by the same empty-queue condition — a second, independent casualty on the permission-request path, not just the message stream. Fix PR #332075 (open) addresses the base case; #334127 ("close abandoned turns deterministically") and #331872 address adjacent parts of the same signal-loss family. As of the most recent comment (2026-09-04), the reporter notes the fix in #332075 does not cover the window-close case, and that the host-side `AgentSideEffects` gate is a separate defect from the client-side fix in #334072 (VC-003's fix PR).

**Impact on Orchestration:**
This is a severe, distinct failure mode from VC-003 (hung tool call that never executes at all): here the subagent's work genuinely happens and completes, but the signal that tells the orchestrating session "this subagent is done, here is its result" can be silently dropped by the host's message-routing layer once the parent's own turn has ended — which is exactly the state MOSAIC's fan-out dispatch pattern puts the primary agent into while background subagents run. For MOSAIC's headless/automated orchestration, this means a dispatched subagent's completion may never surface to the orchestrator even though the subagent actually finished successfully — indistinguishable from VC-003's true hang from the orchestrator's point of view, but with a different underlying defect and a different recovery path (data is recoverable from the transcript after a host restart, which a human-driven interactive session can do but a headless/automated run may not know to attempt). The `AskUserQuestion` auto-denial finding is also directly relevant: any MOSAIC pattern where a subagent needs to ask a clarifying question while other queue activity is in flight risks the question being silently auto-cancelled rather than actually surfaced.

**Evidence:**
- Reported and progressively deepened by a CONTRIBUTOR (RyanEwen) with the same rigor as VC-002/VC-003/VC-004: live process/log instrumentation, exact code references, reproductions "against a fully stock reverted bundle" to rule out local patches, and reproduction confirmed live on 1.136.1 (most recent comment).
- A fix PR (#332075) is open and linked as closing this issue, and two further related fix PRs (#334127, #331872) are referenced in the thread — confirming the underlying defect class is accepted as real, though no maintainer has verbally acknowledged the issue and a maintainer-directed request for review input on the parent tracking issue (#333174, "Really no input/feedback at all?" — 2026-09-09) has gone unanswered as of this scan.
- Meets Confirmed per the CaptureGuide's criteria (linked open fix PR + multiple independent reproductions with strong evidence), despite the lack of maintainer commentary — flagged here explicitly since maintainer silence on the broader tracking issue is itself a signal worth watching.
- Verified per CaptureGuide's "don't trust duplicate/closure labels" rule: this is genuinely distinct from VC-003 (#333931) in root cause (message-routing/signal-dropping vs. a tool call that never executes), even though the reporter's own comment in this thread initially raised #333931 as one possible trigger before separating them explicitly ("Worth separating this from #334072... This gate is host-side and would still drop the signals regardless of that change").

**Workaround(s):**
1. ★ None from within the harness for the live-signal-drop behavior. If a background/parallel subagent dispatch's completion is not observed within an expected time window on this harness, do not assume failure — first check whether the transcript on disk actually completed (restart the agent host / reopen the session to force a replay) before treating it as a hung/failed dispatch, since VC-003's true-hang symptom and this issue's signal-drop symptom are indistinguishable from the orchestrator's live view alone.
2. Avoid relying on `AskUserQuestion`-style interactive clarification calls from a subagent while other queue/turn activity may be in flight on this harness, since such calls can be silently auto-denied rather than genuinely surfaced to the user.
3. For headless/automated MOSAIC runs specifically: since a human "just reopen the session" recovery isn't available, treat any subagent dispatch on this harness whose completion signal doesn't arrive within a wall-clock timeout as unconfirmed rather than failed, and where possible cross-check against the on-disk transcript directly before re-dispatching (re-dispatching a subagent whose work actually completed wastes the work and may produce duplicate side effects).

**Notes:**
This issue is one of ~20 individual reports the same contributor (RyanEwen) consolidated under tracking issue #333174 ("the external-harness chat surface has two root causes, not sixteen defects"), which groups this issue and VC-003 (#333931) together as related-but-distinct under "Cause two: provider parity and lifecycle" (Claude agent-host provider not yet at parity with Codex/Copilot providers on steering-message idempotency, turn-promotion, and restore-time interrupted-turn detection). #333174 itself is not tracked as a separate KB entry — it's a meta/index issue, not an independent defect — but is a valuable map for future passes; note its "cause two" table also lists #331595 (VC-002) and #332087 ("responses render only at turn end," own fix PR #332122, not yet investigated — candidate for a future pass) as part of the same underlying provider-parity gap. Cross-referenced from VC-003's Notes. Needs re-verification once #332075, #334127, and the (not-yet-filed-separately) `AgentSideEffects` gate fix all land — re-check whether the window-close case (registry loss) is addressed, since the reporter explicitly notes the in-memory anchor approach in #332075 cannot survive a host restart by construction.

**Re-check (2026-09-10, batch 2 of run 3):** No change. Issue still has exactly the same 4 comments recorded above (last dated 2026-09-04); the 2026-09-09 `updated_at` timestamp is metadata-only (no new comment text). Fix PR #332075 still open, unmerged, no maintainer review. Confidence, classification, and impact remain Confirmed/Bug/HIGH.

---

### VC-012: Client-tool execution triggers off a display signal (stream-mapper "ready") instead of the SDK's authoritative invocation — causes double execution, empty-argument invocation, and dropped results that hang the turn forever

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/microsoft/vscode/issues/330899 |
| **Reported** | 2026-08-14 |
| **Last Activity** | 2026-09-09 |
| **Confidence** | Confirmed |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 1.133.x, dev container / remote agent host, Claude provider with auto-approvals |
| **Latest Platform Version** | 1.137.0 |
| **Labels** | agent-host; assigned to roblourens |

**Summary:**
A client-contributed tool call is executed off the stream-mapper's `chat/toolCallReady` signal (emitted at `content_block_stop` with `confirmed: "not-needed"`) rather than off the SDK's own authoritative in-process invocation of that tool — the two are not ordered against each other. The reporter (contributor RyanEwen) traced this single architectural seam to three independently-reproducible, independently-patched defects: (1) **double invocation** — a call readied twice (once by the stream mapper, once by the permission flow) runs the underlying tool twice (fix PR #330683, later superseded by a broader fix #334146); (2) **empty-argument invocation** — a disconnect path synthesizes a ready with no `toolInput`, so the tool is invoked with `{}` and throws during preparation (fix PR #330684, later hardened with schema validation per a maintainer collaborator's suggestion); (3) **dropped result** — execution can finish before the SDK has registered its handler for that call, so the result is discarded on arrival and the turn "heartbeats forever" waiting for a response that already happened (fix PR #330730). A consolidating fix PR (#334146, "admit a client tool call once, instead of racing two triggers") is referenced in the related tracking issue (#333174) as the eventual single-trigger fix superseding the individual patches.

**Impact on Orchestration:**
This is a foundational tool-execution reliability defect directly in MOSAIC's Domain filter (tool call reliability, parameter handling). All three symptoms are severe for automated orchestration: a tool silently executing twice (e.g., a file write, a git operation, or a side-effecting API call) can corrupt state without any error signal; a tool invoked with empty arguments due to a race can fail in confusing, hard-to-diagnose ways unrelated to the actual task; and a dropped result causing a turn to hang forever is functionally the same class of unrecoverable stall as VC-003 and VC-011, just via a different mechanism (client-tool race rather than subagent dispatch or message routing). Any MOSAIC tool call routed as a "client tool" on this harness (as opposed to a tool the SDK executes server-side) is potentially exposed to all three failure modes.

**Evidence:**
- Reported by CONTRIBUTOR RyanEwen with precise code-level tracing (`agentSideEffects.ts` line references, exact mechanism for each of the three sub-defects) and filed at a maintainer's own request (a collaborator, kycutler, asked for this root-cause issue to be filed per a comment on PR #330684) — meaning triage explicitly recognized value in tracking the root cause as one design question.
- Three separate fix PRs (#330683 superseded by #334146, #330684, #330730) each carry, per the reporter, "a test that fails without the fix" — strong evidence the underlying defects are real and reproducible, not speculative.
- A further consolidating fix PR (#334146) is referenced as in progress in the related tracking issue #333174, indicating the single-trigger root-cause fix is being pursued, not just the three symptom patches.
- Directly cited as a candidate root cause by VC-002 (#331595) and VC-003 (#333931) in their own Notes/Evidence sections — this issue is the common architectural thread underlying multiple already-tracked symptoms.
- No explicit maintainer acknowledgment comment in the issue itself, but assigned to maintainer roblourens and the filing was made at a collaborator's explicit request — meets Confirmed bar via linked PRs + collaborator engagement rather than a direct "confirmed" comment.

**Workaround(s):**
1. None from within the harness — this is an internal race in the client-tool dispatch pipeline with no user-facing control to avoid it.
2. For MOSAIC: treat any client-tool call on this harness as potentially executing more than once or with incomplete arguments under contention (e.g., rapid consecutive tool calls, or calls near a permission-flow confirmation boundary); prefer idempotent tool implementations where practical, and apply the same wall-clock timeout/re-dispatch discipline recommended for VC-003 and VC-011 to detect the "dropped result, turn hangs forever" variant of this defect.

**Notes:**
This is the same underlying architectural seam VC-002 (#331595) and VC-003 (#333931) both flag as a candidate (but unconfirmed) root cause for their own symptoms — cross-referenced there. Also appears in #333174's "Client tool execution" cause cluster alongside several further related issues not individually investigated this pass (#331289 "calls still routed to a disconnected client," #331987 "a client tool is cancelled when the derived collection reads empty") — candidates for a future pass. Needs re-verification once #334146 lands and ships in a release; check whether it fully supersedes/closes #330683, #330684, and #330730, and whether VC-002/VC-003's symptoms are confirmed resolved by the same fix.

**Re-check (2026-09-10, batch 2 of run 3):** No change. Issue still has exactly the same 3 comments recorded above (last dated 2026-08-17), still no maintainer engagement beyond the `roblourens` assignment and the `agent-host` label. Consolidating fix PR #334146 still open, unmerged, 1 comment (bot only), no maintainer review. Confidence, classification, and impact remain Confirmed/Bug/HIGH.

---

### VC-016: MCP tools silently rejected as "disabled by the user" due to a static tool-availability snapshot racing async server discovery (ask mode)

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/microsoft/vscode/issues/334569 |
| **Reported** | 2026-09-04 |
| **Last Activity** | 2026-09-09 |
| **Confidence** | Likely |
| **Orchestration Impact** | MEDIUM |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 1.136.1, Copilot Chat 0.64.1; reporter's MCP server had 48 tools (firefox-devtools-mcp v0.10.2) |
| **Latest Platform Version** | 1.137.0 |
| **Labels** | new release |

**Summary:**
In Copilot Chat's **ask mode**, when a user (or agent) `@`-references an MCP server (toolset), the set of tools considered "available" for that request is captured as a **static snapshot at request-parse time** (`chatRequestParser.ts`). MCP server tool discovery is asynchronous and registers tools one-by-one; if the snapshot is taken before discovery finishes, only the subset of tools registered so far gets baked in — the rest are permanently rejected for that session with a misleading `"Tool X is currently disabled by the user, and cannot be called"` error, even though the tool cache shows all tools enabled and the server itself has fully registered and would execute the call. The reporter did an exhaustive 48-tool test matrix proving the split is purely timing-based (no correlation with risk level, alphabetical order, or actual user disablement), traced the exact interception line in `toolCalling.tsx`/`askAgentIntent.ts`, and submitted two candidate fix PRs (#334657, preferred — scopes to the referenced server; #334641 — looser, exposes all MCP tools). As of the last comment, a VS Code contributor (anthonykim1) asked for an English translation and reassigned the issue; no maintainer has yet acknowledged the root cause or picked a fix direction, and neither PR is merged.

**Impact on Orchestration:**
MOSAIC agents that rely on MCP-exposed tools could see a tool call fail with an error message that reads as a deliberate user/permission denial ("disabled by the user") when the real cause is a discovery-timing race — nothing is actually disabled. An automated MOSAIC run has no way to distinguish this from a genuine permission restriction, so it could either incorrectly report the tool as unavailable going forward, or an agent could waste turns trying to work around a "disabled" tool that was always going to be fine on the next request. Because this is explicitly scoped to **ask mode**'s tool-reference snapshot mechanism (not confirmed to affect full agent-mode tool execution), impact is capped at MEDIUM pending confirmation of whether MOSAIC's harness-native dispatch on this harness uses the same code path.

**Evidence:**
- Single reporter overall, but exceptionally rigorous: full 48-tool availability matrix with pass/fail classification, server-log cross-referencing, and a precise code-level root-cause trace (three files/line locations cited) confirming the interception point and the async-discovery-vs-snapshot race.
- Two candidate fix PRs are open/submitted against the issue (#334657 open, #334641 closed as superseded by #334657), meeting the "linked to open fix PR" confirmation signal — though both PRs are authored by the reporter, not yet reviewed or merged by a VS Code maintainer, so this falls short of true maintainer confirmation.
- A VS Code contributor (anthonykim1) engaged (asked for translation, reassigned to roblourens) but has not yet confirmed the root cause or fix direction.
- Reporter also flagged a related, broader architectural gap (MCP server/tool state is per-window, not coordinated across multiple VS Code windows, causing cache races and inconsistent tool registration across windows) as worth tracking separately — noted here but not spun into its own entry since it lacks a dedicated issue/PR yet.

**Workaround(s):**
1. ★ None from within the harness yet — the fix requires a merged PR. Until then, avoid relying on an MCP tool being available on the very first request of a new session/window against a server with many tools or slow startup; a subsequent request/session is more likely to have completed discovery.
2. If a "disabled by the user" error occurs for a tool known to be enabled in `mcp.json`/settings, do not treat it as authoritative — retry in a fresh request rather than assuming the tool is truly unavailable, since this may just be the discovery-timing race.

**Notes:**
Needs re-verification once either #334657 or #334641 merges, and to confirm whether MOSAIC's actual dispatch path (agent mode / autonomous tool execution, not ask mode's `@`-reference flow) shares the same static-snapshot mechanism — if agent mode uses a different (dynamic) tool-availability path, this issue may not materially affect MOSAIC's primary orchestration mode and should be downgraded. The related multi-window MCP state coordination gap the reporter surfaced (no cross-window sync for server connection/tool cache state) is worth a dedicated search on a future pass if MOSAIC ever runs multiple concurrent VS Code windows against the same MCP servers.

**Re-check (2026-09-10, batch 3 of run 3):** No material change. Issue now shows 12 comments (all from before this run's cutoff, already reflected above — the reporter's own English translation of the root-cause analysis and the two-PR comparison, plus contributor `anthonykim1`'s translation request and the bot's `connor4312`→`roblourens` reassignment). PR #334657 (recommended fix) still open/unmerged, PR #334641 (alternative) confirmed closed as superseded. No maintainer has acknowledged the root cause or picked a fix direction. Confidence, classification, and impact remain Likely/Bug/MEDIUM.

---

### VC-017: MCP client discards the tool result payload (`structuredContent`) when a long-running MCP "task" ends in `failed` status

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/microsoft/vscode/issues/335229 |
| **Reported** | 2026-09-09 |
| **Last Activity** | 2026-09-09 |
| **Confidence** | Unverified |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 1.136.2, reproduced with an MCP server (Qt Creator's built-in MCP server) that uses the MCP `tasks` extension (`tasks.requests.tools.call`) for long-running tool calls |
| **Latest Platform Version** | 1.137.0 |
| **Labels** | new release |

**Summary:**
For MCP servers that implement the MCP "tasks" protocol (used for long-running tool calls — the client polls `tasks/get` until a terminal status is reached), VS Code's MCP client (`McpTask` in `mcpServerRequestHandler.ts`) correctly calls `tasks/result` to fetch the final payload when the task reaches `completed` status, but on `failed` status it immediately rejects using only the terse `statusMessage` string and **never calls `tasks/result`** — so any structured payload the server attached to the failed task (e.g. `structuredContent` with detailed error/diagnostic data) is permanently discarded and never reaches the model. The reporter cites the MCP 2025-11-25 tasks spec, which explicitly requires that `tasks/result` on a terminal `failed` status return the full underlying `CallToolResult` (including `isError: true`, `content`, and `structuredContent`) — so this is a clear client-side spec violation, not ambiguous behavior. The reporter also notes the MCP SDK bundled in the Copilot Chat extension has the same asymmetry independently. Single report, very recent, no comments or maintainer engagement yet beyond triage assignment.

**Impact on Orchestration:**
MCP tools are a first-class extension point MOSAIC agents may rely on for specialized capabilities (builds, tests, external system integration). When such a tool legitimately fails and reports a rich, structured diagnostic payload (e.g., compiler error lists, structured validation failures) via the MCP tasks protocol, this bug means the agent — and therefore any MOSAIC orchestrator relying on the agent's downstream report — only ever sees a generic one-line failure message instead of the actionable diagnostic data the server tried to provide. This degrades an agent's (and MOSAIC's) ability to self-correct or accurately report failure root cause after a tool call fails, which is a direct hit to tool-execution reliability in the Domain filter — though it only manifests for MCP servers that use the tasks extension for long-running calls (not simple synchronous MCP tool calls), narrowing its likely frequency.

**Evidence:**
- Single reporter (author_association: NONE), but with an exact code-level root cause: cites the specific file (`mcpServerRequestHandler.ts`), class (`McpTask`), and the precise `failed` vs `completed` branch asymmetry, with the actual source snippet reproduced in the issue.
- Cross-references the authoritative MCP 2025-11-25 spec text for `tasks/result` behavior on terminal states, making the "should" behavior unambiguous rather than a matter of interpretation.
- Notes the same defect independently exists in the Copilot Chat extension's bundled MCP SDK (`requestStream()`), suggesting the bug is systemic to how this client-side library models tasks, not a one-off.
- No maintainer acknowledgment yet; assigned to meganrogge, labeled `new release` (auto-triage only).
- Meets the Unverified inclusion bar: threatens core tool-execution reliability (Domain filter) for any MCP server using the tasks pattern, not a custom model backend (this is client-side MCP protocol handling, backend-agnostic), and the reproduction is a precise code/spec-level trace rather than a vague symptom.

**Workaround(s):**
1. Reporter's in-thread workaround: have the agent make a separate follow-up tool call (e.g., a dedicated `list_issues`/diagnostics tool) to retrieve the detail that was dropped from the failed task's result, if the MCP server exposes one.
2. For MOSAIC: when integrating an MCP server that uses the tasks protocol for long-running operations, prefer designing the server (or wrapping it) so that failure diagnostics are also retrievable via a plain synchronous tool call, not solely embedded in the discarded `tasks/result` payload of a failed task.

**Notes:**
Scope is narrower than general MCP tool execution — only affects MCP servers using the tasks extension (`CreateTaskResult`/long-running polling pattern) for tool calls, which is a newer, less common MCP feature; simple synchronous MCP tool calls are unaffected. Needs re-verification once a fix PR appears; watch for whether the fix also addresses the parallel defect the reporter flagged in the Copilot extension's own bundled MCP SDK (`requestStream()`), since that could require a separate extension-side fix beyond the VS Code core change.

**Re-check (2026-09-10, batch 3 of run 3):** No change. Issue still has 0 comments, still assigned only to `meganrogge` (auto-triage `new release` label only), no linked fix PR (`closed_by_pull_requests` empty). 1 day since last activity — far from the 6-week staleness threshold. Confidence, classification, and impact remain Unverified/Bug/HIGH.
