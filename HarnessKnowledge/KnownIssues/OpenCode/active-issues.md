# OpenCode — Active Issues

> Last updated: 2026-09-10 (run 2)

---

### OC-001: Subagent permission request silently dropped at depth ≥2 — parent turn hangs forever, stop/interrupt are no-ops

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anomalyco/opencode/issues/44747 |
| **Reported** | 2026-08-24 |
| **Last Activity** | 2026-09-07 |
| **Confidence** | Confirmed |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | v1.18.22 – v1.18.29 (confirmed still present after #35073 was closed) |
| **Latest Platform Version** | v1.18.30 (2026-09-09) |
| **Labels** | (none returned in payload) |

**Summary:**
When a nested subagent (depth ≥2 — e.g. orchestrator → subagent → sub-subagent) triggers a permission ask (observed with `external_directory`), the ask is created server-side but never surfaces through any client or API surface (`GET /permission`, per-session permission endpoints all return empty). The parent turn is left in `busy` state forever with no way to answer the prompt. `POST /api/session/{id}/interrupt` returns 204 but does nothing — the runtime's active-session list is already empty by the time this is discovered — and the state survives a full client restart, requiring direct edits to the client's local database to clear.

**Impact on Orchestration:**
MOSAIC's hub-and-spoke model dispatches subagents from an orchestrator, and workflows can involve a subagent invoking a tool at one further hop of depth. This defect means such a nested dispatch that triggers a permission ask can silently wedge the entire session with no recoverable signal — no error, no timeout, no interrupt path. In a headless Runner pipeline this manifests as a run that hangs indefinitely with no way for the external supervisor to detect or recover from it via the API, since even the interrupt endpoint is a no-op.

**Evidence:**
- Detailed server-log timeline from the original reporter (specific request IDs, exhaustive API probe results all showing empty across `/permission`, per-session permission/question endpoints, and `/api/session/active`).
- A second independent reporter (2026-09-07, v1.18.29, Linux) reproduced the same silent-drop pattern at subagent depth 2 with a different trigger (a chained bash command that tree-sitter aggregates up to `/`, causing `external_directory` to evaluate to ask), including code-level root-cause analysis pointing at two independent drop paths: the TUI's `routes/session/index.tsx` only aggregating direct children of the viewed session (so a depth-2 ask never renders), and `run.ts`'s `if (!sessions.has(sessionID)) continue` dropping the event for an untracked child session so the underlying `Deferred` in `permission/index.ts` never resolves.
- That second reporter explicitly confirmed the depth-2 drop survives the close of related issue #35073 (closed 2026-09-04) — their repro was on v1.18.29, installed ~17 hours after that closure. Investigation of #35073 itself (see corrected note below) shows it was auto-closed by the stale-issue bot for 60 days of inactivity (`state_reason: not_planned`), not because it was fixed — its two linked PRs (#35823, #41644) are both CLOSED without a clear merge confirmation, and #35073 proposed only a partial fix (classifying `mode: "subagent"` actors as non-interactive so they fail clean with `DeniedError` instead of hanging) that would not by itself address the deeper depth-2 event-propagation gap this issue and its second reporter's code analysis describe.
- An automated bot comment on this issue lists a cluster of closely related reports describing the same symptom class: #13715, #43996, #39112, #35073, #36604, and calls this issue "the survivor of the permission bucket" after #35073 and #36868 were closed as fixed.

**Workaround(s):**
1. ★ Set the specific permission category that would trigger the ask (e.g. `permissions.external_directory`) to `"allow"` in config to avoid the ask being generated at all — reported by the original reporter as avoiding the trigger, though this is a blanket permission relaxation rather than a fix for the underlying hang, and was not independently confirmed by other reporters.
2. Avoid subagent nesting deeper than depth 1 (orchestrator → subagent, no further delegation) until this is fixed — inferred from the depth-2-specific repro pattern in both reports; not stated as a confirmed workaround by any reporter.

**Notes:**
Not specific to the "2.0" rewrite track — both reproductions are on stable 1.18.x releases (v1.18.22 and v1.18.29). Given two independent, well-evidenced reproductions spanning two point releases after a supposedly-fixing PR (#35073) landed, this looks like a deeper architectural gap in how nested/child-session events propagate to parent tracking, not a simple regression — treat #35073's "fix" as addressing only a subset (likely depth-1) of the underlying problem. See OC-002 for a related but distinct report about subagent permission/error events not being visible to external integrations monitoring the root session. Also structurally corroborated by #48232 (investigated but not separately tracked — see skip rationale below): a Task-subagent permission ask dropped over the ACP transport, root-caused to the same class of defect (the child/subagent session is absent from whatever session store the interface layer consults, so its permission event is silently discarded rather than forwarded or mapped to its root). Not separately captured as its own KB entry because ACP is a niche editor-integration transport that MOSAIC's two execution modes (harness-native session, headless Runner via CLI/API) do not use — but it is strong independent evidence that "child session not tracked by the surface answering permission asks" is a systemic architectural gap in OpenCode spanning at least the TUI, REST API, and ACP transports, not a one-off bug isolated to any single one of them. Reproduces on `dev` (1.18.30), i.e. the current release, per that report. Additionally worth flagging: a comment on #35073 (2026-07-05, 0 confirmations but a specific and plausible scenario) reports that `--auto` / auto-approve mode set on the parent session does not propagate to subagents at all — a subagent hits the same class of hang even when the whole session is meant to run unattended with auto-approval. If a fix for the general "subagent = non-interactive, fail clean instead of hang" pattern lands upstream, it would need to special-case the `--auto` scenario to auto-approve rather than deny, per that commenter's reasoning — otherwise MOSAIC sessions run with blanket auto-approval could see spurious subagent `DeniedError` failures instead of the current silent hang. Not independently confirmed; flag for re-check if MOSAIC ever relies on `--auto` mode with OpenCode.

---

### OC-002: Child-session (subagent) events carry no parent lineage — external integrations can't distinguish a running/retrying subagent from a root session actually blocked

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anomalyco/opencode/issues/46685 |
| **Reported** | 2026-09-01 |
| **Last Activity** | 2026-09-06 |
| **Confidence** | Likely |
| **Orchestration Impact** | MEDIUM |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | v1.18.25 |
| **Latest Platform Version** | v1.18.30 (2026-09-09) |
| **Labels** | (none returned in payload) |

**Summary:**
Child-session (subagent) events on the SSE event bus (`session.status`, `session.error`, `permission.asked`, `question.asked`) carry no reliable parent/root session lineage — only `session.created` includes `info.parentID`; `session.status` and `session.error` carry neither the root nor a consistently usable parent id. Meanwhile the root session's own `session.status` stays `busy` (or emits nothing) for the entire duration of a subagent run and only updates again once the subagent finishes. A captured SSE trace confirms the gap directly: root goes `busy`, a wall of child `session.status`/`session.error` events flow with no root-session update in between, then root updates again only on subagent completion. External integrations watching the root session (e.g. the reporter's `herdr` pane plugin) therefore cannot distinguish "a subagent is running normally, or hit a transient auto-retrying stream error" from "the root session is actually waiting on user input" — both render identically as an unexplained `busy`/`blocked` state with no recovery signal.

**Impact on Orchestration:**
This is directly relevant to MOSAIC's Runner pipeline and any external supervisor watching a root session's state to manage workflow lifecycle. Because child-session events lack parent lineage and the root session's own status doesn't reflect subagent-caused waits, an external process cannot reliably tell "subagent dispatched, working normally" apart from "session genuinely blocked/needs a human" — this degrades automated monitoring/alerting built on root-session status and could cause a Runner-driven pipeline to misreport a healthy in-progress subagent dispatch as stuck, or vice versa, mask a real stuck state as ordinary subagent busy-ness.

**Evidence:**
- Reporter provided a concrete captured SSE event sequence from `GET /event` showing the exact gap (root `session.status:busy` → child `session.created`/`session.status` events with no root updates → root `session.status:busy` again only after the subagent finishes), plus a real 15-minute production incident where their integration plugin misreported a healthy running subagent as `blocked`.
- A github-actions bot flagged this as overlapping with #45549 (event-bus/parent-lineage design issue) and #13715/#44747 (subagent permission-event visibility); the reporter explicitly reviewed all three and confirmed real, specific overlap with #45549 point 3 (only `session.created` carries `parentID`; `session.status` carries no parent lineage at all) while distinguishing this report from #44747 (dropped/never-delivered event) and #13715 (TUI rendering bug) — in this case the events do reach consumers, but without enough context to interpret correctly.
- A second, independent commenter (maintainer of an external sandbox-layer project, "Vetto") corroborated the same root cause and proposed the same fix direction (carry root session id + subagent id on every permission/error event) five days later, with a community upvote on that comment.
- No maintainer (anomalyco team) acknowledgment yet; the issue is assigned to `jlongster` but no comment from that assignee is present as of the last activity.

**Workaround(s):**
No confirmed workaround exists for external integrations — the missing lineage is a server-side event-payload gap. As a partial mitigation, an integration could track root→child relationships itself starting from `session.created`'s `parentID` field (the one field that is populated) and correlate later child events by session id rather than relying on the root session's own status to reflect subagent activity — this is not stated as a workaround by anyone in the thread, but follows directly from the diagnosis and is the direction the reporter suggests upstream should also take.

**Notes:**
Related/overlapping issues per bot triage and reporter's own analysis: #45549 and #30043 (event-bus parent-lineage design, likely the more canonical tracking issue for the root cause), #44747 = OC-001 (different symptom — permission ask dropped and never delivered at all, vs. here events are delivered but under-specified), #13715 (TUI-only rendering bug, not applicable to headless/API consumers like MOSAIC's Runner). Confidence set to Likely rather than Confirmed: no maintainer acknowledgment, but the reporter's own SSE capture is concrete evidence (not just a symptom description), and a second independent user corroborated the same architectural gap and fix direction. Not confirmed whether specific to the 1.x line vs. 2.0 track — reported against v1.18.25, no indication otherwise. If a future pass investigates #45549 directly, consider consolidating this entry into that one if it proves to be the canonical root-cause tracking issue.

---

### OC-003: Rejecting one of a subagent's multiple pending permission asks cancels the entire subagent instead of just that call

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anomalyco/opencode/issues/47995 |
| **Reported** | 2026-09-08 |
| **Last Activity** | 2026-09-08 |
| **Confidence** | Unverified |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | v1.18.20 |
| **Latest Platform Version** | v1.18.30 (2026-09-09) |
| **Labels** | (none returned in payload) |

**Summary:**
When a subagent has issued two (or more) tool calls that each require a permission ask, rejecting the first pending ask (with feedback) does not just decline that one call and move on to asking permission for the second — it cancels/rejects both pending tool calls and terminates the entire subagent. Reported with a screenshot showing both terminal calls rejected/canceled from a single rejection action. No maintainer response or independent confirmation yet; assigned to a maintainer (`jlongster`) but no comment from them as of last activity.

**Impact on Orchestration:**
MOSAIC subagents can legitimately need to make several permission-gated tool calls within one dispatch (e.g. multiple file writes or terminal commands). If declining one specific action (because it looks wrong or out of scope) tears down the whole subagent rather than letting the orchestrator/user selectively decline just that action, the orchestrator loses all partial progress from that dispatch and must re-run the entire subagent task from scratch — a correctness and cost problem for any workflow using per-call permission review instead of blanket allow-lists.

**Evidence:**
- Single reporter (`exchgr`) with a specific, concrete reproduction (dispatch a subagent with two tool calls needing permission, reject the first) and a screenshot showing both being canceled.
- No maintainer acknowledgment, no independent confirmation, and no comments on the issue as of the last update (2026-09-08, same day as filed).
- Filed the same day as the closely related #47996 (OC-004) by the same reporter, describing a different symptom (cross-subagent rejection bleed) that plausibly shares a root cause in how the permission system scopes a decision to a specific pending ask.

**Workaround(s):**
No confirmed workaround found in the issue thread. A plausible mitigation, not stated by anyone in the thread, is to structure subagent tasks so they request at most one permission-gated action per dispatch and return control to the orchestrator before requesting the next — at the cost of extra dispatch round-trips.

**Notes:**
Confidence kept at Unverified (single reporter, no comments, no confirmations) but included because it threatens a core MOSAIC pattern — a subagent making several permission-gated tool calls in one dispatch — the report is on stock OpenCode with no custom backend, and it provides concrete, specific reproduction steps plus a screenshot. Recommend re-checking together with OC-004 (#47996) in a future pass; both were filed 2026-09-08 by the same reporter and describe what looks like the same underlying weakness in how permission decisions are attributed to a specific pending ask. Not confirmed whether specific to the "2.0" track — v1.18.20 is on the stable 1.x line.

---

### OC-004: Rejecting a permission ask while multiple parallel subagents have pending asks applies the same rejection/feedback to all of them

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anomalyco/opencode/issues/47996 |
| **Reported** | 2026-09-08 |
| **Last Activity** | 2026-09-08 |
| **Confidence** | Unverified |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | v1.18.20 |
| **Latest Platform Version** | v1.18.30 (2026-09-09) |
| **Labels** | (none returned in payload) |

**Summary:**
When two (or more) subagents run in parallel and both have a pending permission ask at the same time, rejecting one subagent's ask with feedback provides no visual confirmation and the same rejection/feedback prompt remains on screen; pressing "enter" again delivers the identical rejection/feedback to the other subagent's pending ask instead of that subagent getting its own independent prompt. No maintainer response or independent confirmation yet; assigned to a maintainer (`kommander`) but no comment from them as of last activity.

**Impact on Orchestration:**
MOSAIC workflows can fan out multiple subagents concurrently (e.g. parallel research/investigation forks). If the permission UI/routing cannot correctly distinguish which pending ask belongs to which parallel subagent session, a decision (approval or, as reported here, rejection with specific feedback) meant for one subagent's action is incorrectly applied to an unrelated subagent's action — a correctness and safety problem for any workflow relying on per-subagent tool scoping during concurrent dispatch, since feedback intended for one subagent's mistake could be misapplied to another subagent's legitimate action.

**Evidence:**
- Single reporter (`exchgr`), same reporter and same filing day (2026-09-08) as the closely related #47995 (OC-003), describing what appears to be the same underlying permission-routing defect from a different angle (cross-subagent bleed vs. single-subagent-cancel-on-reject).
- No maintainer response, no independent confirmation, and no comments on the issue as of the last update.

**Workaround(s):**
No confirmed workaround found in the issue thread. Avoiding concurrent parallel subagent dispatches when more than one may trigger a permission ask at the same time would sidestep the symptom, at the cost of losing parallelism — not stated as a workaround by anyone in the thread, but follows directly from the reproduction conditions.

**Notes:**
Confidence kept at Unverified (single reporter, no comments, no confirmations) but included because it directly threatens MOSAIC's parallel subagent fan-out pattern, is reported on stock OpenCode with no custom backend, and includes concrete, specific reproduction conditions. Strongly suspected to share a root cause with OC-003 (#47995) — both filed the same day by the same reporter, both describe permission decisions not being correctly attributed/scoped to the originating subagent or tool call. Recommend a future pass re-examine both together once either accrues a maintainer response, and consider whether they should be merged into a single KB entry if a root-cause comment clarifies they are one defect. Not confirmed whether specific to the "2.0" track — v1.18.20 is on the stable 1.x line.

---

### OC-005: Foreground subagent (Task tool) failure/cancellation returns no task_id — parent cannot resume, must re-dispatch from scratch and loses all progress

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anomalyco/opencode/issues/39196 |
| **Reported** | 2026-07-27 |
| **Last Activity** | 2026-09-06 |
| **Confidence** | Confirmed |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | v1.18.7 (`dev`) through at least early September 2026; predecessor issue #13910 closed as "completed" but this issue shows the fix was incomplete |
| **Latest Platform Version** | v1.18.30 (2026-09-09) |
| **Labels** | (none returned in payload) |

**Summary:**
When a foreground (synchronous, non-`background`) subagent dispatched via the Task tool fails or is cancelled, the parent receives only a bare error string (`Effect.fail(new Error(...))`) with no `<task id="...">` wrapper — unlike the success path and the background-error path, which both go through `renderOutput` and do emit the id. Without that id, the parent model has no handle to resume the child session, so any partial work the subagent already did is stranded and the parent has no choice but to re-dispatch the same task from scratch, discarding all prior progress. A predecessor issue (#13910) was closed as "completed" but this report demonstrates the foreground-error and cancelled branches in `task.ts` were never actually fixed. Two subsequent fix PRs (#40615, #41278) were opened and both closed without resolving the issue — it remains open with a fresh downstream-transferred report as of 2026-09-06.

**Impact on Orchestration:**
This is a direct, high-value hit on MOSAIC's hub-and-spoke orchestration: the orchestrator dispatches subagents for potentially expensive tasks (research, planning, implementation), and any transient failure, cancellation, disconnect, or client restart during a foreground dispatch causes total loss of that subagent's accumulated context and work, forcing a full re-dispatch. One reporter explicitly frames this as a "high-priority" issue because it "wastes a significant number of tokens and slows things down considerably" on every disconnect or cancellation — a direct cost and reliability concern for automated Runner-driven pipelines where transient failures should be recoverable, not catastrophic.

**Evidence:**
- Root-caused with an exact code reference (the two `Effect.fail` branches in `task.ts` bypass `renderOutput`, which is what emits the task id on the success and background-error paths).
- Multiple independent users confirm hitting the same symptom across roughly a month and a half (2026-07-27 to 2026-09-06): a client-side workaround from `mattew113` received 3 "hooray" reactions and a direct confirmation from `Phunguy65` that it worked; `Grelo4ka` confirms wanting the same fix; `SC0d3r` independently reproduces the exact failure mode (app closed / task cancelled / connection dropped → agent has no task_id → re-dispatches from scratch, losing all progress) with a full example transcript.
- A downstream project (`fieldkit-cmd`) transferred a sanitized report of the same defect on 2026-09-06, describing two foreground review-task launches returning "Task cancelled" with persisted-but-unresumable child sessions.
- Bot-flagged as duplicate/continuation of #13910, which was closed as completed but is shown by this issue (and its still-open status after two closed fix PRs) to have only partially addressed the problem.
- Assigned to a maintainer (`kitlangton`); two fix PRs (#40615 "preserve task id on failure", #41278 "preserve task id on foreground failure") were opened specifically for this issue but both show as CLOSED without the issue itself closing, indicating the fixes did not land or did not fully resolve it.

**Workaround(s):**
1. ★ Manually recover the session id via the client UI (Ctrl+X → Down to select the failed subagent, Ctrl+P → "Copy session transcript" — the session_id appears at the top of the transcript) and hand it to the parent agent so it can resume or read that session directly — reported by `mattew113`, confirmed working by `Phunguy65`. This is a TUI-specific manual step, not something a headless/API-driven MOSAIC Runner session could perform on its own.
2. Fall back to serial (non-Task-tool) review/execution outside the Task tool entirely when resumability matters — reported as the workaround used in the downstream `fieldkit-cmd` transfer, at the cost of losing the Task tool's isolation/parallelism benefits.

**Notes:**
This is a materially different and more severe framing than a simple "missing field" bug — it directly undermines resumability of any foreground subagent dispatch, which is core to how MOSAIC's orchestrator would want to handle transient failures without wasting the subagent's accumulated work. Related: #46461 (tracked separately per the downstream transfer comment) covers a bounded timeout/manual-resume fallback for stuck tasks — worth investigating in a future pass as a complementary mitigation. Both fix PRs referenced here (#40615, #41278) are closed without resolving the issue; re-check in a future pass whether a new fix PR has been opened. Not confirmed whether specific to the "2.0" track — all evidence is against the stable 1.18.x line.

---

### OC-006: Subagent hangs indefinitely after a bash tool call completes if it left a live detached descendant process running; primary sessions unaffected

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anomalyco/opencode/issues/47546 |
| **Reported** | 2026-09-05 |
| **Last Activity** | 2026-09-05 |
| **Confidence** | Likely |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | v1.18.29 |
| **Latest Platform Version** | v1.18.30 (2026-09-09) |
| **Labels** | (none returned in payload) |

**Summary:**
A subagent session (mode: `subagent`, dispatched via the Task tool) stops issuing its next LLM completion request after a bash tool call that finishes normally but leaves a long-lived detached descendant process running (e.g. a Playwright browser daemon started via a wrapper that redirects the child's stdout/stderr to temp files and returns without waiting on it). The bash call itself provably completed — its own cleanup ran to the end — but the subagent's loop never proceeds to the next model request; the TUI keeps showing the command as executing with no error, and the session only recovers via manual abort. The identical pattern in a primary (non-subagent) session does not reproduce the hang, isolating this specifically to the subagent execution path. Reporter demonstrated a deterministic 4/4 reproduction rate and distinguished this from a related, previously-reported family (#33028, #40468, #26220) where the trigger was "elusive" — here it is precisely isolated to the live-detached-descendant condition. Reproduced on Windows, but the reporter notes a related issue (#40468) reproducing the same class on Debian, so this is not believed to be Windows-specific.

**Impact on Orchestration:**
MOSAIC subagents commonly run terminal/bash tool calls for build, test, and verification tasks — including patterns that legitimately spawn a long-lived background process (a dev server started for a health check, a browser automation daemon for E2E verification, a watcher process). If any such subagent-invoked bash command leaves a detached descendant alive, this defect can silently stall the entire subagent dispatch with no error signal, blocking a headless Runner pipeline indefinitely since (per the deterministic trigger described) there is no recovery path short of a manual abort — something an automated pipeline cannot itself perform without an external timeout/kill mechanism.

**Evidence:**
- Reporter provided a deterministic, reproducible trigger (4/4) with an exact reproduction recipe (PowerShell wrapper spawning a detached Playwright browser process via `Start-Process ... -PassThru`), full log signatures showing the exact point of the stall (`evaluated permission=bash ... action=allow` is the last log line; the next `stream ... mode=subagent` line never appears), and a clear differential test (primary session with the same pattern does not hang; primary session where the descendant inherits the server's own stdout/stderr pipes wedges via a separately-understood mechanism, explicitly distinguished from this report).
- Cross-referenced by the reporter against three related open issues (#33028, #40468, #26220) describing the same general "subagent hangs after bash tool call" symptom family with a less-precisely-isolated trigger, plus a github-actions bot flag pointing to #33028 (same log signature, same platform) and #38564 (subagent termination not killing spawned child processes — an adjacent process-lifecycle concern).
- No maintainer response yet; assigned to a maintainer (`nexxeln`) but no comment from them as of last activity. Single reporter for this precise, deterministic isolation, so confidence is capped at Likely rather than Confirmed, but the specificity and reproducibility of the report, plus the corroborating related-issue cluster describing the same underlying symptom class across platforms, is stronger evidence than typical single-reporter reports.

**Workaround(s):**
1. Avoid subagent-invoked bash commands that leave a detached/backgrounded descendant process alive after the command itself returns — ensure any spawned child process is either fully waited-on (blocking) or explicitly killed/disowned before the bash tool call returns. Not stated as a confirmed workaround in the thread, but follows directly from the reporter's precise isolation of the trigger condition.
2. If a subagent must start a long-lived background process (e.g. a test server), run it from a primary-mode session instead of a subagent-mode session, since the reporter confirmed primary sessions with the identical pattern do not hang.

**Notes:**
This looks like a genuine subagent-vs-primary code-path divergence (the reporter explicitly asks whether "descendant/jobs bookkeeping on the subagent path" exists that primary sessions skip) rather than a generic process-management issue — worth flagging distinctly from the broader #33028/#40468/#26220 cluster even though it shares symptoms, since this report isolates a specific, reproducible cause the others had not pinned down. Related: #38564 (subagent termination doesn't kill spawned child processes) may share underlying descendant-process bookkeeping gaps — worth a future pass if investigating this cluster further. Not confirmed whether specific to the "2.0" track — v1.18.29 is on the stable 1.x line.

**Verification pass (2026-09-10, run 1):** Directly re-read #33028, already listed above as part of this entry's broader cluster. Confirmed genuinely the same symptom family (bash-tool-call-adjacent hang, no timeout, no clean recovery), correctly bucketed here, and it contains valuable additional code-level evidence worth folding in: a deep root-cause comment (2026-07-08) traces the hang to `awaitToolFibers`/`FiberSet.awaitEmpty` in `core/src/session/runner/llm.ts` never timing out when a tool fiber is still running after the provider stream itself succeeds, plus a separate `collectStream` gap in `core/src/process.ts` where `Stream.runFold` over a child's stdout blocks forever if a grandchild process keeps the pipe's write-end open after the immediate child exits — precisely the "detached descendant process" mechanism this entry describes, now with an exact code citation and two proposed fixes (a 30s timeout on `awaitToolFibers`, and a bounded-grace-window read on `collectStream`). This same code-level analysis is also cross-referenced from the new OC-096 entry (general LLM-stream/tool-fiber timeout gap), since it independently corroborates that entry's "no timeout anywhere in the tool/stream lifecycle" theme.

---

### OC-007: Task tool's model-facing subagent list ignores session-level permission overrides (description mismatch, not an execution bypass)

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anomalyco/opencode/issues/48106 |
| **Reported** | 2026-09-09 |
| **Last Activity** | 2026-09-09 |
| **Confidence** | Confirmed |
| **Orchestration Impact** | MEDIUM |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | `dev` @ 830d5eb5354874105cc31599635a80c1662609e8 (pre-1.18.30) |
| **Latest Platform Version** | v1.18.30 (2026-09-09) |
| **Labels** | (none returned in payload) |

**Summary:**
The Task tool's `describeTask` function, which builds the model-facing list of available subagent targets, filters only on `agent.permission` and ignores session-level permission rules — while actual execution and the adjacent "Code Mode" description do correctly merge agent-level and session-level rules. As a result, a subagent target the session has denied can still be advertised to the primary model as available, and a target the session has allowed can be silently omitted if the agent-level rule denies it. This is explicitly a description/advertisement mismatch, not a claim that execution itself bypasses permissions — the underlying enforcement is presumed correct, only what the model is told is wrong. Reporter demonstrated exact reproduction steps (unit-test-style) showing the mismatch in both directions, with a linked open fix PR (#48107, "merge session permissions in task descriptions") that reporter confirms makes four session-override regression cases pass against the previously-failing base.

**Impact on Orchestration:**
MOSAIC relies on the primary/orchestrator agent making informed delegation decisions based on what the Task tool tells it is available. If the tool description advertises a subagent target that session-level permissions actually deny, the orchestrator may attempt to dispatch to it and only discover the restriction at runtime (a wasted dispatch or a permission-ask stall, compounding with the permission-hang defects in OC-001/OC-003/OC-004); conversely, if a session-allowed target is hidden because the agent-level default denies it, the orchestrator may never consider a legitimately available subagent, silently narrowing its delegation options without any error. This is a correctness gap in permission-scoping visibility specifically at the subagent dispatch decision point — one of MOSAIC's core domains.

**Evidence:**
- Reporter provided exact code location (`packages/opencode/src/tool/registry.ts` lines 265-269) and precise reproduction steps demonstrating the mismatch in both directions (session-denied target still shown; session-allowed target omitted).
- Linked open fix PR #48107, which the reporter states supplies `Permission.merge(agent.permission, session.permission ?? [])` to `describeTask` and passes four session-override regression cases that fail on the unmodified base — this is direct evidence the defect is real and its scope precisely bounded.
- Reporter explicitly scoped this issue against three related-but-distinct reports to avoid conflation: #35238 (V2 `subagent` tool runtime enforcement, a different code path in `packages/core`), #45078 (session denies superseded by later rules not correctly inherited into child sessions), and #39086 (the `task` tool being entirely absent from the tool list under certain permission configs — the complementary "totally missing" case rather than a partial mismatch).
- A github-actions bot comment cross-referenced the same three related issues, asking maintainers to clarify whether this is intentionally scoped narrower than a predecessor fix attempt (#41100) that was auto-closed without merging.

**Workaround(s):**
No confirmed workaround found in the issue thread — this is a description-accuracy defect in the harness's own tool metadata, not something a caller can correct from outside. Until PR #48107 lands, treat the Task tool's advertised subagent list as unreliable when session-level permission overrides differ from agent-level defaults, and rely on runtime behavior (permission asks / errors) rather than the tool description to determine actual availability.

**Notes:**
Confidence set to Confirmed despite a single reporter, because the report includes an exact code-level root cause and a linked, open fix PR whose regression tests the reporter states demonstrably reproduce the bug on the unmodified base — this meets the "linked to an open fix PR" Confirmed criterion. Impact kept at MEDIUM rather than HIGH since this is explicitly a description/advertisement mismatch rather than an execution-level permission bypass — it can cause wasted dispatches or missed delegation options, but does not itself let a subagent's tool calls escape enforcement. Related cluster worth revisiting together in a future pass: #35238 (V2 subagent runtime enforcement — check if this is a "2.0" track-specific concern separate from this legacy Task tool description issue), #45078 (session-deny inheritance into child sessions), #39086 (task tool entirely missing under some permission configs), #41100 (predecessor fix attempt, auto-closed unmerged). This specific issue (#48106) is scoped to the legacy Task tool in `packages/opencode`, not the "2.0" rewrite's V2 subagent tool in `packages/core` — #35238 should be checked separately if the 2.0 track becomes relevant to MOSAIC.

---

### OC-008: Per-subagent model configuration ignored — all subagents run on the primary session's model

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anomalyco/opencode/issues/36289 |
| **Reported** | 2026-07-10 |
| **Last Activity** | 2026-09-09 (auto-closed for staleness; not confirmed fixed) |
| **Confidence** | Confirmed |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | v1.17.15 (reported); auto-closed 2026-09-09 with no fix confirmation, so plausibly still present on v1.18.30 |
| **Latest Platform Version** | v1.18.30 (2026-09-09) |
| **Labels** | (none returned in payload) |

**Summary:**
When a subagent is configured with its own `model` field (via `.opencode/agents/<name>.md` frontmatter or `opencode.json` under `agent.<name>.model`) and is invoked via the Task tool (`task(subagent_type: "...")`) or `@mention`, the configured model is ignored — the subagent runs on the primary session's model instead, even though `opencode debug config` correctly shows the intended per-agent model assignment. The issue was closed automatically by the stale-issue bot after 60 days of inactivity (`state_reason: not_planned`) with no maintainer comment and no linked fix PR — it was never confirmed fixed, only closed for lack of activity.

**Impact on Orchestration:**
MOSAIC's three-layer agent design explicitly relies on running narrowly-scoped subagents on smaller/cheaper models for cost and performance reasons. If OpenCode silently ignores per-subagent model configuration and always uses the primary agent's (typically largest/most capable, most expensive) model for every subagent dispatch, this directly defeats that cost/performance optimization for any MOSAIC deployment targeting OpenCode as a harness — every subagent dispatch would silently cost as much as running on the primary model, with no error or warning to reveal the misconfiguration.

**Evidence:**
- Reporter demonstrated the config is parsed correctly (`opencode debug config` shows the correct per-agent model assignment) but not applied at invocation time — isolating the defect to the model-selection/dispatch path rather than config parsing.
- A github-actions bot immediately flagged three independent, closely related reports of the identical symptom: #36250 ("agent.model and agent.variant config ignored in handleSubtask", filed days earlier), #18615 ("Model parameter ignored when launching subagent with agent name"), and #21632 ("subagent model variants are parsed but not applied at runtime") — a cluster of at least four reports of the same root defect.
- No maintainer ever commented on or acknowledged this specific issue; it was closed solely by the automated stale-issue bot, not by a fix landing or a maintainer determination that it was invalid.

**Workaround(s):**
1. Some users report that instructing the model to invoke the Task tool with a `category="<name>"` parameter rather than `subagent_type="<name>"` preserves the correct configured model — reported on #18615 by one user, not independently confirmed by others, and not explained mechanistically. Treat as an unverified lead rather than a confirmed fix.
2. No other confirmed workaround found across this cluster of reports.

**Notes:**
Classified as Bug (not Limitation) since no maintainer indicated this is intentional — it directly contradicts the documented per-agent model configuration feature, and the config is shown to parse correctly but not apply. **Verification pass (2026-09-10) confirmed #36250, #18615, and #21632 all describe the same root-cause family as this issue and upgraded confidence from Likely to Confirmed** based on evidence found in those threads that this entry's original source (#36289) lacked:
- **#36250** ("agent.model and agent.variant config ignored in handleSubtask") has an exact code-level root cause (in `packages/opencode/src/session/prompt.ts`, `taskAgent` is fetched after `taskModel` is already resolved in `handleSubtask`) and an **open, unmerged fix PR (#36281)** — an open fix PR is Confirmed-level evidence per this KB's validation rules. A second commenter (`lll9p`) independently reports the sibling `variant` symptom (a session's reasoning variant silently falls back from `xhigh` to `medium` after running subagents, with the UI still showing the original selection).
- **#18615** ("Model parameter ignored when launching subagent with agent name") is the earliest-filed report (2026-03-22) with direct SQLite database evidence showing the wrong model persisted in the message record itself (not just a display issue), 7 reactions, and an **open fix PR (#18752)**. A commenter (`draxxris`) narrows the bug to CLI-only (not reproducing in the Web UI). The `category=` vs `subagent_type=` workaround above comes from this thread (`Ritanlisa`, 1 upvote, unconfirmed by others).
- **#21632** ("subagent model variants are parsed but not applied at runtime") is closed (`not_planned`, auto-closed for staleness 2026-08-04) but contains the richest technical evidence of the whole cluster: three independent contributors (`nilltadios`, `21pounder`, `jsheremeta-alt`) each traced and confirmed a **second, distinct sub-mechanism** — when a subagent inherits the parent's model (no explicit `model` configured), `packages/opencode/src/tool/task.ts` never forwards the parent's active `variant` to the child prompt call at all (traced to exact line numbers in both `task.ts` and `prompt.ts`, confirmed via debug logs showing `agVariant` present but resolved `variant` undefined). `jsheremeta-alt` independently reproduced this on v1.15.13 via binary/bytecode analysis and direct SQLite session-table inspection, confirming every subagent session recorded `variant: "default"` regardless of frontmatter config, while primary-agent sessions correctly showed their variant. Two earlier fix PRs for this sub-mechanism (#20742, #12567) existed but were closed/unmerged for lack of maintainer engagement, not because they were wrong.

Net effect: there are **two distinct, independently-confirmed root causes** feeding the same user-visible symptom (subagent model/variant configuration silently ignored) — (a) `handleSubtask`'s model-then-agent resolution ordering (per #36250/#18615), and (b) `task.ts` never forwarding the parent's `variant` when a subagent inherits the parent's model (per #21632's three-contributor trace). Both remain unresolved as of this capture (open PRs #36281/#18752 unmerged; #21632's PRs closed unmerged). Needs re-verification: OC-008's own source issue (#36289) was auto-closed for staleness on 2026-09-09 with no fix confirmed — recommend a future pass directly test per-subagent model AND variant configuration on the current release (v1.18.30). This is a materially important issue for MOSAIC's cost model on OpenCode if it persists, since it would undermine the "cheaper models for subagents" design principle entirely — and per the variant sub-mechanism, could also silently degrade reasoning quality/effort for subagents expected to inherit a high-effort parent variant.

---

### OC-021: Compaction produces empty summary for reasoning models — conversation history silently dropped

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anomalyco/opencode/issues/41571 |
| **Reported** | 2026-08-10 |
| **Last Activity** | 2026-09-09 |
| **Confidence** | Confirmed |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | v1.18.15 - v1.18.21+ (regression introduced in v1.18.15) |
| **Latest Platform Version** | v1.18.30 (2026-09-09) |
| **Labels** | (not retrieved — see issue) |

**Summary:**
Since v1.18.15, OpenCode's compaction routine flattens the entire conversation history into a single unstructured plain-text user message with no system prompt and `tools: []` sent silently (undocumented — docs claim tools are disabled and output capped at 4096 tokens, but only the `tools: []` half is real). Reasoning models handed this wall of text (Kimi K3-256K, GLM-5.3, DeepSeek variants confirmed) treat it as a task to continue reasoning about instead of a conversation to summarize, and emit a `reasoning`-only response with zero `text` parts and `finish: stop`. OpenCode still marks this a valid summary and swaps it in as the new history baseline — permanently and silently discarding all prior context. Non-reasoning models (gpt-5.x, k2p7, deepseek-v4) are reported to compact normally on the same sessions, so the trigger is "reasoning model + missing system prompt," not any single vendor's backend.

**Impact on Orchestration:**
In MOSAIC's long-running/headless sessions via OpenCode, if the primary (or a subagent) is running on a reasoning-capable model, an auto- or manual-compaction event can silently erase the entire conversation history with no error surfaced — the orchestrator has no way to detect that context was lost until subsequent responses reveal the agent has forgotten the original task. This is a direct hit on context window management / conversation stability, one of the explicitly load-bearing domains for MOSAIC's Runner-driven pipelines.

**Evidence:**
- Root-caused with exact code references by two independent commenters (missing `agent.info.system` / `generation.maxTokens` on the compaction request, `PROMPT_COMPACTION` registered but never sent) against `packages/core/src/session/compaction.ts`.
- Independently reproduced on a second unrelated reasoning model (`opencode-go/glm-5.3`, v1.18.21, 2/2 trigger rate) and referenced as the same root cause behind sibling issues #42363/#42371 (DeepSeek) and #44080.
- Maintainer (`nexxeln`) assigned; open fix PR #42063 ("reject empty compaction summaries") exists with regression test coverage, pending review/CI approval as of 2026-09-09.

**Workaround(s):**
1. ★ Avoid using reasoning-capable models (e.g., Kimi K3, GLM-5.x reasoning variants, DeepSeek reasoning variants) as the active model when a session is expected to approach the compaction threshold in OpenCode, until PR #42063 lands — reported by multiple independent users as the only way to avoid silent history loss.
2. Monitor session length manually and manually checkpoint/export context before approaching the auto-compaction threshold if a reasoning model must be used.

**Notes:**
Fix PR #42063 is open, not yet merged as of latest activity (2026-09-09) — re-check on next pass to see if merged/released. Related/duplicate reports: #42363, #42371 (DeepSeek), #44080 (same root cause, proposes `hasNonEmptyTextBody()` guard + stream-layer detection). Not confirmed against stock Anthropic Claude models specifically, but the root cause (compaction request omits system prompt/instructions entirely) is a generic harness defect that would plausibly affect any reasoning-capable model routed through OpencCode's compaction path, including Claude models used with extended thinking — flagging as broadly relevant rather than backend-specific.

---

### OC-022: `compaction.prune` never trims context in single-turn / headless sessions, causing runaway compaction thrash

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anomalyco/opencode/issues/47485 |
| **Reported** | 2026-09-05 |
| **Last Activity** | 2026-09-06 |
| **Confidence** | Confirmed |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 1.18.4, 1.18.27, 1.18.29, and current dev (not fixed as of latest activity) |
| **Latest Platform Version** | v1.18.30 (2026-09-09) |
| **Labels** | (not retrieved — see issue) |

**Summary:**
`prune` (the incremental context-clearing pass, distinct from full compaction) has a `turns < 2` guard in `compaction.ts` that requires two prior user messages before anything becomes eligible for pruning. A single-turn headless run (`opencode run --format json` with one user message, or one call to `POST /session/{id}/prompt`) never accumulates a second user turn, so `prune` is permanently a no-op regardless of the `compaction.prune: true` config setting. Separately, `prune` is only invoked after the prompt loop exits — but a long single-turn run with subagents/tool loops can run for hours without the loop ever exiting, so live context is never trimmed while it fills. The combination causes full auto-compaction (a 45-85k token summary call) to fire roughly every 10 minutes for the entire duration of a long headless run, with the agent re-reading the same files repeatedly after each compaction (one file read 66 times in one session) — burning tokens/API credits roughly 10x faster than expected without ever technically hanging or erroring.

**Impact on Orchestration:**
This directly matches MOSAIC's headless, Runner-driven pipeline execution mode — single prompt-per-invocation calls through the server API or `opencode run`, often with long tool-heavy subagent work in one turn. A MOSAIC session running this way would silently thrash compaction every ~10 minutes for its entire duration, multiplying token/cost consumption and repeatedly discarding-then-rebuilding working context (re-reading files, re-establishing state) instead of ever properly pruning stale tool output incrementally. A second reporter confirmed the same mechanism also poisons sessions after ~50 large tool-output messages accumulate, with manual `/session/{id}/compact` unavailable as a workaround (`"Session compact is not available yet"` on 1.18.29).

**Evidence:**
- Root-caused with exact file/line references (`compaction.ts` ~286-292 for the `turns < 2` guard, `prompt.ts` ~1338 for prune only running after loop exit) verified against both the 1.18.27 release and current dev branch.
- Independently reproduced by a second, unrelated user via a different entry point (`opencode serve` / `POST /session/{id}/prompt`, `openai/gpt-5.2`) on both 1.18.4 and 1.18.29 — confirms the guard is not specific to `opencode run --format json` or to one model/provider, but to any single-user-turn flow.
- Original reporter has a local fix (two commits, passing test suite) with measured before/after data (ingestion dropped from ~22k to ~3.8k tokens/minute, zero compactions in a window where stock build did two) demonstrating the mechanism is understood and fixable.
- A related root cause (`agent.compaction.variant` config ignored, causing the compaction summary to inherit an expensive reasoning variant) is flagged as a probable duplicate of pre-existing issue #41578.

**Workaround(s):**
1. Shrink tool-output payloads before they reach the model context (one reporter cut an MCP tool's output from 820KB to 20KB) — delays the compaction/poisoning ceiling but does not fix the underlying no-op prune.
2. No confirmed full workaround exists; manual compaction via the API is not available on 1.18.29 (`Session compact is not available yet`). Splitting a long headless task into multiple shorter turns (forcing a second user message) may make `prune` eligible sooner, but this is inferred from the root cause, not confirmed by a reporter — flag for testing.

**Notes:**
No maintainer confirmation yet in the thread (only a bot cross-reference to #41578 for the variant sub-issue); assignee `neriousy` has not commented. Confidence set to Confirmed based on two independent, code-level-verified reproductions across different opencode versions and entry points, per the "multiple independent reproductions with evidence" criterion — re-verify if a maintainer disputes the root cause. Directly relevant to MOSAIC's headless Runner pattern; recommend prioritizing re-check on next pass since a local fix already exists and may land soon. Cross-reference: #41578 (compaction variant config ignored — likely duplicate for root cause #3).

---

### OC-024: MCP tool manifest drops to 0 after context compaction — server stays connected, only a new session recovers tools

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anomalyco/opencode/issues/46190 |
| **Reported** | 2026-08-29 |
| **Last Activity** | 2026-08-29 |
| **Confidence** | Unverified |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | v1.18.12 |
| **Latest Platform Version** | v1.18.30 (2026-09-09) |
| **Labels** | (not retrieved — see issue) |

**Summary:**
In a long-running session, once auto-compaction fires, a connected local (stdio) MCP server's tools vanish from the agent's callable tool manifest entirely — `opencode mcp list` still reports the server as connected and the server process itself never disconnects, but `opencode debug agent build` shows 0 MCP tools and any call to a previously-working MCP tool fails as "unknown tool." The only recovery reported is starting an entirely new session. The reporter explicitly states this is not model-specific or MCP-server-specific — reproduced with a generic local stdio server and reportedly any model — pointing at client-side tool-manifest handling around the compaction boundary rather than at the MCP protocol layer or a specific server implementation.

**Impact on Orchestration:**
Any MOSAIC agent or subagent relying on MCP-provided tools in a session long enough to trigger auto-compaction would silently lose access to those tools mid-session with no error beyond a generic "unknown tool" failure on the next attempted call — exactly the kind of delegation/tool-execution reliability gap MOSAIC's long-running orchestration sessions are most exposed to. Combined with OC-022/OC-023 (compaction firing frequently and racing the session loop in headless runs), a MOSAIC session using MCP tools could lose tool access repeatedly over the course of one long headless pipeline run with no automatic recovery short of restarting the session (which itself loses conversation state).

**Evidence:**
- Single reporter, detailed step-by-step reproduction, no independent confirmation or maintainer comment in-thread yet (only an automated bot cross-reference).
- Bot cross-reference to #38266 identifies a plausible shared root cause: the MCP client is deleted from state on any `listTools()` error and never recreated, which a compaction-triggered manifest rebuild could trigger.
- Thematically consistent with, and possibly the same underlying defect as, this KB's existing OC-061 entry ("MCP tools connected but not exposed to agent — stale tool-set cache"), which independently theorizes a connect-time-cached tool-definition snapshot that is never invalidated — compaction is a plausible trigger for that staleness to surface.

**Workaround(s):**
1. ★ Start a new session when MCP tools stop responding after a long session — the only recovery reported (inferred from the reporter's own observation, not separately confirmed by others).
2. No preventive workaround identified; avoiding long sessions that approach the auto-compaction threshold when MCP tools are required would sidestep the trigger but is not practical for MOSAIC's long-running pipelines.

**Notes:**
Confidence held at Unverified per single-reporter criteria, but included despite that because it threatens a core MOSAIC pattern (MCP-tool-dependent subagent delegation in long/headless sessions), is not tied to a custom/non-standard backend (explicitly reported as model- and server-agnostic), and includes concrete reproduction steps. Likely overlaps with OC-061 (already tracked, broader "stale MCP tool cache" symptom) and pre-existing issues #38266, #23556, #40901, #25282 referenced in the report — recommend a future pass cross-check whether these should be consolidated into one canonical entry once a maintainer narrows the root cause. Needs re-verification on current v1.18.30 (reported only on v1.18.12, several releases behind).

**Verification pass (2026-09-10, run 1):** Directly re-read #38266, bot-cross-referenced here. Confirmed genuinely the same root-cause family, not a bot misclassification: #38266 independently root-causes the identical "MCP client permanently deleted from state on any transient `listTools()` error, never recreated" mechanism this entry's own evidence already theorizes, with a concrete real-world case (an `opencode serve` + `attach` session where a local stdio MCP server's tools silently became unavailable mid-session, confirmed via the server's own logs showing zero errors and the process still alive). A later comment on that issue (2026-08-04) adds a further Windows Desktop data point: the same MCP server entry can be spawned twice under one OpenCode process, with the duplicate causing request timeouts — a related but distinct lifecycle-management symptom worth flagging if this cluster is investigated further. Correctly bucketed; no new standalone entry needed.

**Verification pass (2026-09-10, run 2 batch 3):** Re-fetched #46190 + its 1 comment (the same #38266 bot cross-reference already investigated in the prior pass). No new activity since 2026-08-29 (12 days — not yet 6+ weeks stale) and still no maintainer response. Confidence (Unverified), Classification (Bug), and Impact (HIGH) unchanged; the "Needs re-verification on current v1.18.30" flag from the original capture still stands since no one has re-tested against a version past v1.18.12.

---

### OC-023: Auto-compaction is not a barrier — session loop races ahead of the compaction step, swallowing pending user input and dropping the task goal

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anomalyco/opencode/issues/41358 |
| **Reported** | 2026-08-09 |
| **Last Activity** | 2026-08-31 |
| **Confidence** | Confirmed |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Windows Desktop (initial report) and Linux CLI 1.18.15 (independent repro); confirmed still open as of 2026-08-31 |
| **Latest Platform Version** | v1.18.30 (2026-09-09) |
| **Labels** | (not retrieved — see issue) |

**Summary:**
When auto-compaction triggers mid-turn, OpenCode's session loop does not wait for the compaction step to fully settle before starting the next iteration — the agent continues thinking/acting concurrently with compaction instead of pausing for the summary to land or for user confirmation. A second, independent reporter (Linux CLI, v1.18.15) confirmed the exact mechanism with a session recording: the agent starts the next step while compaction is still in progress, and a user confirmation message sent around the compaction boundary is "raced over" and effectively swallowed. The practical effect is that the agent drifts from the original task goal right after compaction — re-doing earlier steps, referencing stale assumptions, and even declaring completion for something unrelated. A maintainer-adjacent contributor (`ryangamerdev`, PR #45125, "enhanced compaction + context restoration") addressed the goal/summary-preservation half of this, but explicitly confirmed in comments that the PR does **not** cover the session-loop concurrency/confirmation-barrier half — the PR's `Closes #41358` link was deliberately changed to `Related: #41358` so this issue stays open for the unresolved race condition.

**Impact on Orchestration:**
This is a direct hit on conversation stability for any long-running MOSAIC session via OpenCode — primary or subagent. In a headless Runner pipeline with no human present to notice drift, an agent that races past its own compaction boundary can silently abandon the original task goal, re-execute already-completed steps (wasting tool calls/cost), or act on stale assumptions with no signal that anything went wrong. Worse, if MOSAIC's orchestration pattern relies on injecting a follow-up instruction or confirmation exactly around when compaction might fire, that injected input can be dropped by the race condition described here, effectively losing an orchestrator-to-agent message with no error.

**Evidence:**
- Two independent reporters (original: Windows Desktop; second: `TM23-sanji`, Linux CLI, v1.18.15) each confirmed the same mechanism — concurrent execution across the compaction boundary — via separate session recordings.
- Maintainer-track contributor (`ryangamerdev`) shipped PR #45125 targeting part of this issue (goal/summary preservation across compaction) and explicitly acknowledged in-thread that the concurrency/confirmation-barrier half remains unaddressed, changing the PR's issue link from `Closes` to `Related` specifically to keep this issue open and un-orphaned.
- Assignee (`nexxeln`) present on the issue; 7 comments over 3+ weeks with sustained, substantive back-and-forth (not a dead/stale thread).

**Workaround(s):**
1. No confirmed workaround from maintainers or community for the core race condition. PR #45125 (once merged) is expected to reduce goal-drift severity by preserving recent context and the task goal across the compaction boundary, but does not close the underlying concurrency gap.
2. Defensive: avoid sending time-sensitive instructions/confirmations to a session that is likely near its compaction threshold, since such input can be raced over and lost — inferred from the reported mechanism, not a stated community workaround.

**Notes:**
Related open items referenced in the issue: #37551 (feature request: reinject original prompt post-compaction — the "wish" side of this bug), #18794 (closed: manual `/compact` continuing unexpectedly — a narrower prior report of the same class), #15533 (auto-compaction loop), #41365 (stall/freeze robustness, cross-linked by the reporter as the same "autonomous-but-safe" UX gap). PR #45125 status should be re-checked next pass — if merged, downgrade this entry's impact to reflect only the remaining concurrency/confirmation-barrier gap rather than full goal loss. Not confirmed as reproducible on stock Anthropic-backed sessions specifically, but the described defect (missing barrier in the session loop) is a generic OpenCode architectural issue independent of model/provider.

**Verification pass (2026-09-10, run 2 batch 3):** Re-fetched issue + all comments. No new activity since 2026-08-31 (last comment still the `ryangamerdev`/reporter exchange confirming PR #45125 was relinked from `Closes` to `Related` to keep this issue open for the unresolved session-loop concurrency/confirmation-barrier gap). PR #45125 merge status still unconfirmed in-thread. Confidence (Confirmed), Classification (Bug), and Impact (HIGH) unchanged. Not yet 6+ weeks stale (10 days since last activity) — no re-verification flag needed this pass.

---

### OC-025: Manual `/compact` (and the auto-compaction overflow guard) has no context-fit check against the compaction model, causing indefinite hangs or silent no-fire

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anomalyco/opencode/issues/46164 |
| **Reported** | 2026-08-29 |
| **Last Activity** | 2026-08-29 |
| **Confidence** | Likely |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | v1.18.25; confirmed still present (in different form) on the `2.0` dev branch as of 2026-08-29 |
| **Latest Platform Version** | v1.18.30 (2026-09-09) |
| **Labels** | (not retrieved — see issue) |

**Summary:**
When `agent.compaction.model` is configured to a model with a smaller context window than the session's active model, manual `/compact` serializes and streams the entire conversation head to that compaction model with no check that the prompt fits its context window. If the provider silently accepts the oversized request and streams nothing back (observed with `openrouter/z-ai/glm-5.3-flash`), the compaction call hangs indefinitely (20s to 7+ minutes observed) with no error logged, and is only ever resolved by the user cancelling it, leaving a `MessageAbortedError` stub. The auto-compaction path has an equivalent size guard (`compactAfterOverflow`) but it fails *silently* (`return false`) when the estimate exceeds budget — so the same misconfiguration surfaces elsewhere as "auto-compaction never fires" with zero diagnostic signal. The reporter verified the `2.0` dev branch still has the core gap: a provider rejection is now mapped to a clear `ContextOverflowError`/"Session too large to compact" message (an improvement), but a provider that accepts-and-streams-nothing still hangs exactly as on 1.18.25.

**Impact on Orchestration:**
If MOSAIC configures a separate (e.g., cheaper) model for the compaction agent via `agent.compaction.model` — a reasonable cost-optimization pattern — and that model's context window is smaller than the primary session model's, both manual and automatic compaction become unreliable: manual compaction can hang a headless session indefinitely with no timeout or error for an external supervisor to detect, and automatic compaction can silently stop firing altogether, leaving the session to grow unchecked toward the primary model's own context limit. Either failure mode is a direct threat to context window management in long-running MOSAIC sessions, and the silent nature of both (no stream error, no log line) means it would likely be misdiagnosed as a different problem (e.g., OC-022/OC-045-style "compaction never fires").

**Evidence:**
- Root-caused with exact file/line citations against both the 1.18.25 release (`compaction.ts` L319-425 for the missing manual-path guard, and the existing but silently-failing auto-path guard at L179-190) and the `2.0` dev branch as of 2026-08-29 (`compaction.ts:141,256,272-283`), including confirmation of what did and did not change between them.
- A related, independently-filed issue (#41801) reports the identical manual-`/compact`-hangs-then-`Aborted` symptom with a different model (DeepSeek V4 Flash), corroborating the mechanism is not specific to one provider.
- A bot cross-reference flags likely-duplicate issue #42448 ("Compaction request exceeds context window on high-output models") describing the same root scenario.
- No maintainer comment yet in-thread beyond the bot cross-reference; assignee `nexxeln` has not responded as of last activity.

**Workaround(s):**
1. ★ Ensure `agent.compaction.model` (if configured) has a context window at least as large as the primary session model's, or omit the setting so compaction falls back to the session model — avoids triggering the missing size check entirely. Inferred directly from the root cause; not separately field-confirmed by a third party.
2. If a hang is observed, cancel the `/compact` call manually — the session survives (uncompacted) but the attempt must be retried with a corrected configuration.

**Notes:**
Confidence set to Likely based on the independently-filed corroborating report (#41801) plus the rigor of the code-level analysis across two branches, despite the primary issue itself having no maintainer acknowledgment. Related/possibly-duplicate: #42448, #41801, #30806 (aborted compaction leaves a dangling partial summary message — a related but distinct symptom), and #46137/#45168/#45249 (bundled by the reporter as "different trigger, same 'never compacts' outcome" — cross-reference OC-021/OC-022 in this KB for adjacent compaction-reliability entries). Recommend re-verifying against current v1.18.30 and the `2.0` branch's eventual stable release.

**Verification pass (2026-09-10, run 2 batch 3):** Re-fetched #46164 + its 1 comment (bot cross-reference to #42448, not independently re-investigated this pass — flag as a candidate for a future consolidation check). No new activity since 2026-08-29 (12 days — not yet stale), still no maintainer response beyond the bot flag. Confidence (Likely), Classification (Bug), and Impact (HIGH) unchanged. Re-verification against v1.18.30 and the eventual `2.0` stable release still outstanding.

---

### OC-026: Native auto-compaction silently never fires on long sessions (`session_context_epoch` stuck at 0), causing runaway token/credit consumption on GitHub Copilot

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anomalyco/opencode/issues/45249 |
| **Reported** | 2026-08-26 |
| **Last Activity** | 2026-08-26 |
| **Confidence** | Unverified |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 1.18.18 - 1.18.21 (possibly the entire ≥1.18.15 range per reporter) |
| **Latest Platform Version** | v1.18.30 (2026-09-09) |
| **Labels** | (not retrieved — see issue) |

**Summary:**
Over a 6-day, single, continuous session (516 messages, multiple subagents, `github-copilot`/`claude-sonnet-5`), native auto-compaction never fired a single time — the `session_context_epoch` DB row stayed at 0 for the entire session lifetime even as real per-message context grew normally turn over turn (not a display-artifact issue). Because context was never compacted, every assistant call resent the full accumulated transcript, and cumulative token consumption grew to ~212M tokens (peak day: ~148.5M tokens across 253 messages), exhausting the user's entire monthly GitHub Copilot credit quota with the provider only surfacing the problem via a `Payment Required` error after the fact — no warning or diagnostic signal was raised at any point beforehand. The reporter rigorously distinguished this from a superficially similar issue (#30649, a display/stats-counter artifact confirmed by another contributor to not actually affect compaction) — this report is specifically about the real per-message context growing unchecked because the compaction trigger mechanism itself never engaged.

**Impact on Orchestration:**
GitHub Copilot with Claude models is a plausible provider path for MOSAIC given the project's use of Claude models generally. This report shows that a long-running, multi-subagent session — structurally identical to MOSAIC's orchestration pattern (one continuous session, many tool calls and subagent dispatches, run for an extended period) — can have its entire compaction mechanism silently fail to engage, with no error surfaced until a provider-side hard stop (quota exhaustion or, in other providers, a context-length rejection). This directly threatens both cost control and eventual session survivability for any MOSAIC deployment running very long OpenCode sessions on GitHub Copilot.

**Evidence:**
- Reporter provided rigorous, verifiable evidence: exact SQL queries against local `opencode.db`, per-day token CSVs, per-session epoch counts (all local sessions showing `context_epochs=0`), and exact log timestamps correlating with the provider's `Payment Required` errors.
- Reporter explicitly ruled out the two most likely alternate explanations: a previously-fixed misclassification bug (#8030, fixed in 1.4.7) and a plugin-dependent compaction-inert issue (#43764, dependent on the `@tarquinen/opencode-dcp` plugin, which was verified absent from this environment).
- A third-party contributor's prior analysis (on the related, bot-flagged #30649) is cited and used to correctly distinguish that issue's root cause (a display-only counter artifact) from this one (a real compaction-trigger failure) — showing the mechanism is understood at a codebase level even without a maintainer's own comment yet.
- No maintainer comment yet in-thread as of last activity (2026-08-26); only an automated bot cross-reference.

**Workaround(s):**
No community or maintainer-confirmed workaround identified. The reporter requests (unanswered as of last activity) that maintainers add a visible warning when a session crosses a context-size threshold without ever compacting, rather than the current silent failure that surfaces only via a provider-side payment/quota error.

**Notes:**
Confidence held at Unverified (single reporter, no maintainer confirmation, no independent third-party reproduction of this exact mechanism yet) despite the unusually rigorous evidence — included because it threatens a core MOSAIC pattern (long-running multi-subagent sessions), was not observed on a custom/non-standard backend (GitHub Copilot is a mainstream provider and the model is a stock Anthropic model, `claude-sonnet-5`), and includes concrete, reproducible diagnostic steps (SQL queries against the local DB). Cross-reference: #30649 (related but explicitly distinct — display-artifact only), #43764 (plugin-dependent variant of "compaction inert" symptom), #46164/OC-025 and #45168 (other compaction-never-fires mechanisms bundled by the OC-025 reporter as "different trigger, same outcome"). Recommend re-verification on current v1.18.30 and flag for priority follow-up given the direct GitHub Copilot relevance.

**Verification pass (2026-09-10, run 2 batch 3):** Re-fetched #45249 + all 3 comments. No new activity since 2026-08-26 (15 days — not yet 6+ weeks stale), still no maintainer response. Confirmed the reporter's own follow-up comment (2026-08-26) explicitly and correctly distinguishes this from bot-flagged #30649 (display/stats-counter artifact only, per a third party's source trace) — the entry's existing "explicitly distinct" characterization holds up under direct re-read, no reclassification needed. Confidence (Unverified), Classification (Bug), and Impact (HIGH) unchanged. Re-verification against v1.18.30 still outstanding.

---

### OC-027: Context-overflow errors misclassified as generic "Network connection lost" — no compaction, no retry, session dies unrecoverably

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anomalyco/opencode/issues/33376 |
| **Reported** | 2026-06-22 |
| **Last Activity** | 2026-08-25 |
| **Confidence** | Confirmed |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | v1.15.7 through v1.17.10 (multiple independent reproductions across this range); not confirmed fixed on any later version |
| **Latest Platform Version** | v1.18.30 (2026-09-09) |
| **Labels** | (not retrieved — see issue) |

**Summary:**
When a long session hits the model's real context-window limit and the provider returns a context-length error wrapped as a generic `Error` (not an `APICallError`), OpenCode's `fromError()` classifier in `message-v2.ts` falls through its catch-all and misclassifies it as `NamedError.Unknown` with the message `"Network connection lost."` — instead of recognizing it as a context overflow. Because `retry.ts`'s `retryable()` only handles the `APIError` type, the misclassified error is treated as non-retryable, auto-compaction never triggers, and the session halts immediately and unrecoverably (no compaction, no retry, no recovery short of starting a new session). The issue was auto-closed by a stale-issue bot after 60 days of inactivity (`state_reason: not_planned`) — this is **not** a maintainer resolution; the linked fix PR (#33667) shows as CLOSED without being merged, and no maintainer ever commented on the thread.

**Impact on Orchestration:**
This is one of the most severe possible compaction-related failure modes for MOSAIC: instead of degrading gracefully (via auto-compaction) when a long session approaches its context limit, the session dies outright with a misleading, generically-labeled network error that gives a monitoring/supervisor process (as in MOSAIC's headless Runner pattern) no actionable signal that the true cause was context overflow. A Runner-driven pipeline hitting this would see an opaque "network" failure and might retry the same request (which would fail identically) rather than taking the correct recovery action (starting a fresh session, splitting the task, or manually compacting beforehand).

**Evidence:**
- Root-caused with exact file/function citations (`fromError()` in `message-v2.ts`, `retryable()` in `retry.ts`) and a specific suggested code fix (regex-based detection of context-overflow error text before the generic catch-all).
- At least 3 independent reporters confirmed the identical symptom and matching root-cause diagnosis across different providers and OpenCode versions: original reporter (mimo-v2.5-free via OpenCode Zen, v1.17.9), a second user (DeepSeek via OpenRouter, v1.17.10, Windows), and a third user (deepseek-v4-flash-free, v1.17.10, Windows 10) — all describing the same "session dies with 'Network connection lost', no compaction, no retry" mechanism, with the second and third explicitly cross-referencing the same code-level root cause.
- Reporter identified a cluster of related/likely-duplicate issues describing the same or adjacent classification gaps: #20060, #25187, #23713, #27629, #22448, #29589, #10220 — a wide surface of the same underlying defect class (provider error classification gaps preventing retry/compaction).
- A fix PR (#33667, "classify context overflow from generic errors before unknown fallback") was opened addressing this exact defect, confirming a contributor recognized it as fixable — but the PR is CLOSED, not merged.

**Workaround(s):**
No confirmed workaround exists community-side beyond avoiding sessions that approach the model's context limit — one reporter noted "lowering context/session length reduces frequency but does not eliminate it." Manually triggering `/compact` well before the session nears its limit would avoid the crash condition, but this requires proactive monitoring since the harness gives no advance warning.

**Notes:**
Auto-closed by staleness bot, not by an actual fix — kept in active-issues rather than resolved because the fix PR never merged and no maintainer ever confirmed resolution. Needs re-verification against current v1.18.30 to check if the underlying `fromError()` classification gap has since been addressed by unrelated work (e.g., the `ContextOverflowError` mapping mentioned in OC-025's investigation of the `2.0` branch, which may partially address this same gap for some providers). Cross-reference: OC-025 (also touches provider error classification around compaction/overflow), OC-026 (a different manifestation of "compaction fails silently on context growth"). Not backend-specific — reproduced across OpenCode Zen (mimo), OpenRouter (DeepSeek), and multiple OS/version combinations, indicating a core harness defect rather than a provider or model quirk.

**Verification pass (2026-09-10, run 2 batch 3):** Re-fetched #33376 + all 6 comments. Confirmed GitHub-side state is unchanged: issue remains `closed`/`state_reason: not_planned`, closed 2026-08-25 by the stale-bot ("automatically closed after 60 days of no activity"), with fix PR #33667 still shown as CLOSED (not merged) and no maintainer comment ever posted. All prior evidence (3 independent Windows/DeepSeek reporters plus the original mimo-v2.5-free repro) re-confirmed present in the thread. Correctly kept in `active-issues.md` per this KB's rule that a stale-bot auto-close is not a real resolution. Confidence (Confirmed), Classification (Bug), and Impact (HIGH) unchanged. The "needs re-verification against v1.18.30" flag from the original capture still stands — no one has retested on a current release.

---

### OC-028: `compaction.reserved` silently ignored for any model lacking `limit.input` metadata — auto-compaction fires far later than configured, degrading quality/throughput with no error

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anomalyco/opencode/issues/38835 |
| **Reported** | 2026-07-25 |
| **Last Activity** | 2026-08-22 |
| **Confidence** | Confirmed |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | v1.17.14 through v1.18.21 (confirmed still present across this range); a revival of prior issue #13980 (unfixed) |
| **Latest Platform Version** | v1.18.30 (2026-09-09) |
| **Labels** | (not retrieved — see issue) |

**Summary:**
`overflow.ts`'s `usable()` function has two mutually exclusive branches for computing the auto-compaction trigger threshold: branch A (`limit.input - reserved`, used when the model declares `limit.input`) correctly applies the user-configured `compaction.reserved` safety margin; branch B (`context - maxOutputTokens`, used when `limit.input` is absent — the common case, since "most models.dev entries only declare context + output") computes and then discards `reserved` entirely, silently ignoring it. Users who set `compaction.reserved` to trigger compaction earlier (e.g., for safety margin or to avoid quality degradation near the context ceiling) get no error and no indication the setting has zero effect for their model. This is a revival of previously-reported, unfixed issue #13980, and a fix PR (#38843) was opened but closed without merging.

**Impact on Orchestration:**
This directly undermines a user-facing safety knob specifically designed for context/compaction tuning — exactly the mechanism MOSAIC would rely on to keep long-running sessions compacting well before hitting problematic context sizes. Two independent, highly detailed reports quantify the real-world harm beyond "config had no effect": one measured the affected model degrading (verbatim repetition loops, early EOS/truncated outputs) once context reached ~71-73% of the window, with the actual (branch-B, `reserved`-ignoring) trigger firing ~40k tokens *after* the degeneration onset — the session became unusable and had to be abandoned. A second independent report measured concrete throughput costs: sessions compacted late ran at half nominal decode speed (14.3 vs 27.1 tok/s) with only 5% prefix-cache reuse, versus near-full cache reuse and ~2x throughput once compaction was forced to fire earlier via a workaround. Given MOSAIC's headless, long-running sessions and the breadth of models lacking `limit.input` metadata (common across many providers, not one vendor), this is a realistic, silent degradation path for any such deployment.

**Evidence:**
- Root-caused with exact file/line-level code citation (`overflow.ts` `usable()`) and a precise proposed fix, explicitly reviving a previously stale-bot-closed report of the identical bug (#13980).
- Three independent reporters reproduced the exact mechanism across three different model/provider combinations (built-in `zhipuai/glm-5.2`, a custom OpenAI-compatible local llama.cpp 27B model, and a custom OpenAI-compatible local MLX Qwen3.8-27B model) and two OpenCode versions (1.18.18, 1.18.21), with one cross-validating the formula against 15 real automatic-compaction events across 6 models over 30 days of session DB data (all firing within a few thousand tokens of the predicted branch-B threshold).
- A fix PR (#38843, "apply compaction.reserved to models without limit.input") was opened against this exact issue, confirming a contributor recognized and attempted to fix the defect — but it is CLOSED, not merged.
- Maintainer (`kitlangton`) assigned; multiple related/sibling issues identified and cross-referenced by reporters (#32656, #30805/PR #31891 which fixed only the branch-A case, #32119, #24683), showing sustained community engagement with this specific compaction-tuning defect class.

**Workaround(s):**
1. ★ For custom/local OpenAI-compatible providers, explicitly declare `limit.input` in the provider's model config (e.g., `"limit": {"input": 240000, "context": 262144, "output": 32768}`) to force branch A, where `reserved` is honored — confirmed effective by an independent reporter (compaction threshold dropped from 229,376 to the intended 160,000, restoring throughput and preventing quality degradation). **Critical caveat**: the running OpenCode server does not hot-reload `opencode.json` — a full server restart is required after the config change, or the old threshold silently persists even though `opencode debug config` shows the new value as "resolved." Also note: setting `limit.input` desyncs the UI's context-percentage indicator, which is computed against `limit.context` rather than the actual `usable()` denominator — expect the indicator to show a misleadingly low percentage (e.g. 57%) at the moment compaction correctly fires early.
2. This workaround is **not available for built-in providers** (e.g., `zhipuai/glm-5.2`) — a custom provider entry with the same name as a built-in provider does not override the built-in's model metadata, so users of built-in providers lacking `limit.input` have no way to work around this in config as of the last report.
3. Be aware branch A has its own sibling defect (#32656): `reserved` there is capped at `COMPACTION_BUFFER = 20,000` regardless of configured value, which can bite anyone whose model has `maxOutputTokens > 20,000` after applying workaround #1.

**Verification pass note (2026-09-10):** #32656 was investigated directly to confirm whether it is the same root cause as this entry or distinct. It is the **complementary sibling bug in the same `usable()` function** (`overflow.ts`), not an independent issue: this entry (OC-028/#38835) covers branch B (no `limit.input` — `reserved` ignored entirely), while #32656 covers branch A (has `limit.input` — `reserved` is computed but then silently capped at a hardcoded `COMPACTION_BUFFER = 20,000` regardless of the model's actual `maxOutputTokens` or the user's configured value). #32656 has accumulated **four separate closed-but-unmerged fix PRs** (#32660, #32844, #32896, #37194) across a month or more, plus an independent third-party contributor (`johncoffee715`, 2026-07-15) who submitted yet another working fix with a full before/after calculation and confirmed all 52 existing compaction tests pass — strong evidence the defect is real, understood, and simply un-landed despite repeated fix attempts, not that it's invalid or already resolved. Net conclusion: OC-028 and #32656 together mean **both branches** of the `usable()` threshold calculation are unreliable for reserving output headroom — branch B drops `reserved` outright, branch A silently truncates it to at most 20K. Anyone applying this entry's workaround #1 (forcing branch A by declaring `limit.input`) should budget for this cap rather than assuming their full configured `reserved` value applies.

**Notes:**
Confirmed confidence based on three independent, code-level-verified, and empirically-measured reproductions plus a revival-of-prior-issue history and an (unmerged) attempted fix PR. Cross-reference: #13980 (original unfixed report), #32656 (sibling bug in branch A), #30805/#31891 (partial prior fix, branch A only), #24683 (same end-user symptom). One reporter also flagged that compaction itself has no fallback if the compaction request is rejected as too large (`ContextOverflowError`, `finish="error"`, no retry) — relevant cross-reference to OC-025's context-fit-check gap in this KB. Recommend re-verifying against v1.18.30 and prioritizing a workaround decision (limit.input override + mandatory restart) if MOSAIC uses OpenCode with built-in or custom models lacking `limit.input`.

---

### OC-061: MCP tools connected but not exposed to agent (stale tool-set cache)

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anomalyco/opencode/issues/33027 |
| **Reported** | 2026-06-19 |
| **Last Activity** | 2026-09-08 |
| **Confidence** | Likely |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 1.17.8 through 1.18.27 (reproduced across many point releases, plus OpenCode "next"/v2) |
| **Latest Platform Version** | 1.18.30 |
| **Labels** | none observed (no maintainer triage labels applied) |

**Summary:**
A local (stdio subprocess) or remote MCP server can report `connected` via `opencode mcp list` / `GET /mcp`, complete the MCP handshake and `tools/list` successfully, yet its tools never appear in the agent's callable tool set (`tool.ids()`, `/experimental/tool/ids`, `opencode debug agent build`). The model then fails with "Model tried to call unavailable tool" or simply never sees the tools. At least 7 independent reporters confirmed this across CLI, TUI, Desktop sidecar, and headless `opencode serve` deployments, spanning versions 1.17.8–1.18.27 and OpenCode "next". No maintainer has acknowledged or triaged the issue as of the last comment (2026-09-08). A 2026-09-07 comment from a user running `opencode serve` behind an external orchestrator provides the most precise root-cause theory: `MCP.tools()` serves a connect-time cached tool-definition snapshot per session/instance that is never invalidated or recomputed, so a subset of tools (e.g., specific Google MCP servers) can be permanently frozen out of a long-lived session while sibling sessions in the same process get the full set.

**Impact on Orchestration:**
MOSAIC agents that depend on MCP-provided tools (e.g., specialized retrieval, ticketing, calendar, or docs servers) can silently lose access to some or all of those tools for the lifetime of a session, with no error surfaced to the agent or orchestrator beyond a generic "tool unavailable" failure. This is especially relevant to MOSAIC's headless/automated execution mode: the clearest, most isolated repro in the thread is from a user driving `opencode serve` via an external orchestrator — the same pattern MOSAIC uses. A long-running orchestration session could lose MCP tool access partway through and never recover without restarting the session, breaking delegation to MCP-backed capabilities.

**Evidence:**
- 7+ independent reporters (userX570, Necmttn, kungfusaini, MaGnaL, timwkosatec, Start-Gao, ArsenicBismuth, wangzhuangwei, Lukas-Novak, rcdailey) across CLI, TUI, Desktop, and `opencode serve`, spanning ~3 months and multiple releases (1.17.8 → 1.18.27, "next" builds).
- Reproduction narrows to the stdio local-subprocess registration/discovery path in most reports; the 2026-09-07 comment reproduces it server-side via `opencode serve` with a precise cache-staleness theory (`MCP.tools()` returns `s.defs` snapshotted at connect time, never invalidated on `ToolsChanged`).
- Multiple related/possibly-duplicate issues identified in comments: #26357 (Docker MCP gateway), #16491 (subagent/Task-tool path can't invoke registered MCP tools), #40015 (Desktop-side cache staleness). Several open, unmerged PRs (#37684, #38533, #40013, #40062) reportedly touch MCP tool registry reconciliation but none have landed.
- No maintainer comment or triage label present on the issue despite 11 comments and 3 months open — this caps confidence at Likely rather than Confirmed despite the strong community evidence.

**Workaround(s):**
1. ★ Start/resume the session from the exact same working directory the MCP server was registered against — Windows users found MCP tool state is scoped per instance/working-directory (@timwkosatec, confirmed on 1.18.1).
2. If a long-lived session loses tools, start a fresh session in the same process — new sessions pick up the full current tool set (@Lukas-Novak, confirmed on 1.18.27 via `opencode serve`). Affected sessions do not self-recover.
3. Prefer the Desktop sidecar/HTTP MCP transport over local stdio subprocess servers where possible — one reporter observed the sidecar path registering tools correctly while the CLI stdio path failed on the same server/version (@Start-Gao, 1.18.11).

**Notes:**
Long-running, high-comment-count issue (11 comments) with no maintainer engagement, but activity is NOT stale (last comment 2026-09-08, 2 days before this capture) — active, unresolved, and apparently worsening cluster rather than one needing re-verification for staleness. Related cluster: #16491, #26357, #40015 (not separately captured here — same root cause per community analysis; revisit if a maintainer response narrows scope to one canonical issue).

**Verification pass (2026-09-10, run 1):** Re-checked #26357 and #40015 directly rather than trusting the bot-flagged clustering. #40015 ("Desktop caches MCP tools list at startup, never refreshes on reconnect") is strong independent *confirmation* of this entry's own theorized root cause — the reporter measured Desktop showing a permanently-cached 17-of-108-tool subset while the CLI (`opencode run`) correctly fetched the full 108 on every session, an exact empirical match for the "connect-time cached tool-definition snapshot, never invalidated" mechanism already described above; two closed-unmerged fix PRs (#40033, #40062, "reconnect MCP servers when a tool call fails") confirm this was recognized and attempted but not resolved — genuinely the same defect, correctly bucketed. #26357 (Docker MCP gateway connects but tools never reach the LLM on macOS Desktop) is consistent with the same general symptom class but is thin evidence on its own (single reporter, only 2 comments, closed by the 60-day stale bot with no maintainer or independent confirmation, no code-level root cause) — treat as a weak corroborating data point rather than a load-bearing citation. #16491 was investigated separately and found to be a **distinct root cause** (subagent Task-tool sessions are never granted execution permission for MCP tools at all, a permission-scoping gap rather than a stale-cache/registration gap) — see new entry OC-094 for that issue; it should not be read as duplicating this entry.

**Verification pass (2026-09-10, run 2):** Re-fetched issue #33027 and all 11 comments — the most recent (@rcdailey, 2026-09-08) reports the same symptom class on a remote OAuth-authenticated server (Fastmail) rather than a local stdio server, already reflected in this entry's Evidence list. No maintainer comment, no fix PR opened, no merge activity. No change to classification, confidence, impact, or workarounds. Not stale (last activity 2 days before this pass).

---

### OC-062: MCP tool errors (`isError: true`) reach the model as a bare `Error executing tool <name>`, discarding the real error message

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anomalyco/opencode/issues/47740 |
| **Reported** | 2026-09-07 |
| **Last Activity** | 2026-09-07 |
| **Confidence** | Confirmed |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 1.18.29 (likely broader — the truncation is in OpenCode's own MCP result handling, not version-specific behavior) |
| **Latest Platform Version** | 1.18.30 |
| **Labels** | (none observed on the issue itself) |

**Summary:**
When an MCP tool call returns `isError: true`, OpenCode forwards only the literal string `Error executing tool <name>` to the model, discarding the actual error text the MCP server placed in `content[].text`. The reporter proved this with a side-by-side comparison: the MCP Inspector (raw protocol, bypassing OpenCode) always returns the full error message for the exact same tool/input; the same call through a live OpenCode session always truncates to the bare tool name. A control case with `isError: false` passes its full JSON content through untruncated, isolating the bug specifically to the `isError: true` code path. A fix PR (#47811, "preserve detailed tool errors") is already open against the issue, confirming the behavior is a recognized real defect even though no maintainer has commented yet.

**Impact on Orchestration:**
This directly damages MCP tool execution reliability for any MOSAIC agent (primary or subagent) that calls an MCP-backed tool. When a tool call fails with a validation error, wrong-parameter error, or any other server-side exception, the agent receives no information about *why* the call failed — only that it failed — making self-correction (retrying with fixed parameters, choosing a different approach) effectively impossible. Since well-behaved MCP servers (including the FastMCP Python SDK, used broadly) return structured, actionable error text on failure, this defeats a core reliability mechanism of tool-use agents and would manifest identically on stock Anthropic-backed sessions, since the truncation happens in OpenCode's own MCP result forwarding code, not in any model or provider layer.

**Evidence:**
- Reporter provided a rigorous, reproducible side-by-side comparison (MCP Inspector vs. OpenCode) across 4 distinct failure modes on 2 different tools of the same server, plus a control case proving the bug is specific to `isError: true`.
- Root cause traced to the FastMCP Python SDK's standard exception-to-`CallToolResult` conversion (`Error executing tool {name}: {exc}`) being received correctly by OpenCode but re-emitted to the model with only the prefix retained.
- A fix PR (#47811, "test(mcp): preserve detailed tool errors") is already open and linked to the issue as of 2026-09-07 — satisfies the Confirmed-confidence criterion of "linked to a merged/open fix PR" even without an explicit maintainer comment.

**Workaround(s):**
No community workaround identified — the truncation happens inside OpenCode's own MCP handling, so there is no server-side or client-config change that restores the message. Until PR #47811 merges, expect opaque `Error executing tool <name>` failures with no diagnostic detail whenever an MCP tool call errors.

**Notes:**
Single-reporter issue but promoted to Confirmed on the strength of the linked open fix PR (#47811) plus an unusually rigorous, mechanistic repro — not merely "single reporter, no confirmation." Re-check next pass whether #47811 has merged and in which release.

**Verification pass (2026-09-10, run 2):** Re-fetched issue #47740 — no comments (still 0), state unchanged (open). Fix PR #47811 remains open/unmerged. No change to classification, confidence, impact, or workarounds. Not stale (3 days since report/last activity).

---

### OC-063: MCP tool schemas not sanitized for Anthropic (root-level anyOf/oneOf/allOf 400s); MCP tools bypass the `tool.definition` plugin hook

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anomalyco/opencode/issues/46628 |
| **Reported** | 2026-09-01 |
| **Last Activity** | 2026-09-07 |
| **Confidence** | Confirmed |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Confirmed present through 1.18.29; fix for first half in unmerged PR #47542 as of 2026-09-07 |
| **Latest Platform Version** | 1.18.30 |
| **Labels** | (none observed) |

**Summary:**
This issue bundles two related MCP defects, both confirmed and code-level root-caused. (1) `ProviderTransform.schema` sanitizes MCP tool input schemas for OpenAI/Azure, Moonshot/Kimi, and Gemini by flattening root-level `anyOf`/`oneOf`/`allOf` combinators, but has no branch for Anthropic-family providers (`@ai-sdk/anthropic`, `@ai-sdk/google-vertex/anthropic`, `@ai-sdk/github-copilot` when proxying a Claude model). Any MCP server exposing a tool with such a schema at the root causes every request to hang or 400 immediately (`input_schema does not support oneOf, allOf, or anyOf at the top level`) — for Anthropic-backed models specifically; the identical config works fine on GPT/Gemini models. (2) Separately, MCP tool definitions never reach the `tool.definition` plugin hook at all — only built-in and plugin-provided tools go through `ToolRegistry.tools`, which triggers that hook; MCP tools are converted through a separate session tool-assembly path that skips it, so plugins cannot patch provider-schema quirks for MCP tools without proxying the entire MCP server's `tools/list` response. A fix for half (1) is in open PR #47542 as of 2026-09-07 (unmerged, confirmed still reproducing on 1.18.29 by the original reporter); half (2) has no fix in progress and is the reporter's stated priority.

**Impact on Orchestration:**
This is squarely a stock-Anthropic-backend defect (the repro explicitly shows the exact same MCP server/config working on GPT-5-mini and failing on claude-haiku-4.5), directly hitting MOSAIC's primary target backend. Any MCP server MOSAIC integrates with that happens to use a root-level `anyOf`/`oneOf`/`allOf` in a tool's input schema (a common JSON-Schema pattern for "exactly one of X or Y required") will hard-fail every single Claude-backed request in that session with a 400 before any tool is even called — not a degraded call, a broken session. Because the failure occurs at the schema-serialization layer before any tool invocation, there's no per-tool call recovery; the whole session is unusable with that MCP server attached. The second half (no `tool.definition` hook access for MCP tools) closes off a legitimate plugin-based mitigation path, forcing a heavyweight proxy-the-whole-server workaround.

**Evidence:**
- Reporter provided a minimal, dependency-free stdio MCP server repro isolating the exact trigger (root-level `anyOf`) and proved provider-specificity by running the identical config against `github-copilot/claude-haiku-4.5` (fails) vs. `github-copilot/gpt-5-mini` (succeeds).
- Automated bot cross-referenced two prior duplicate reports of the same root cause (#35516, #37916), indicating this is a recurring, previously-reported class of defect.
- A second contributor (`jelloeater-agent`) confirmed the root cause and linked open PR #47542, which implements exactly the described fix (`sanitizeAnthropicSchema`, matches the reporter's repro and the `github-copilot/claude-haiku-4.5` provider path) — satisfies the "linked to open fix PR" Confirmed criterion.
- Original reporter re-verified live on 1.18.29 (2026-09-07, after the PR was opened but before merge) that the bug still reproduces exactly as originally reported, and separately confirmed the `tool.definition` hook gap is real and unaddressed by PR #47542 (PR #47542 closes a different, narrower issue #47543).
- A third user (`Jelloeater`) independently confirmed hitting the same failure at work testing Sonnet models, noting a regression ("was working a few weeks ago").

**Workaround(s):**
1. ★ For the schema-sanitization half: audit MCP servers for root-level `anyOf`/`oneOf`/`allOf` in tool input schemas before connecting them to an Anthropic-backed OpenCode session; if found, either get the server fixed upstream (one server, `ai-memory`, resolved it server-side in v2.1.0 per the reporter) or run a local MCP proxy that rewrites `tools/list` responses to fold the combinator into the root object schema before OpenCode sees it — reporter's own working mitigation, confirmed effective, cost described as "very little" engineering time, self-disarms once the server is fixed upstream.
2. For the `tool.definition` hook gap: no workaround exists other than the same MCP proxy approach above — there is no plugin-only fix since MCP tools bypass `ToolRegistry.tools` entirely.
3. If schema fix PR #47542 lands: note the reporter flagged it currently drops `anyOf`/`oneOf` constraints silently rather than preserving them in the description text, so "exactly one of A or B" semantics may be lost even after the fix — worth re-verifying the merged behavior once released.

**Notes:**
Two related duplicate reports already existed (#35516, #37916) before this one — this appears to be a recurring, well-understood defect class that has gone unfixed across multiple report cycles. Re-check next pass whether PR #47542 has merged, and whether the `tool.definition` hook gap (unaddressed) has been split into its own tracked issue by the maintainers.

**Verification pass (2026-09-10, run 2):** Re-fetched issue #46628 and all 5 comments — matches prior capture exactly, including the reporter's live re-verification that PR #47542 confirms and closes issue #47543 (a narrower sub-issue), not this one, and that #47542 remains unmerged as of the latest comment (2026-09-07). No new activity since. No change to classification, confidence, impact, or workarounds. Not stale (3 days since last activity).

---

### OC-064: MCP tool-list failures collapse to a constant "Failed to get tools" string — underlying error discarded, unlogged

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anomalyco/opencode/issues/47644 |
| **Reported** | 2026-09-06 |
| **Last Activity** | 2026-09-06 |
| **Confidence** | Confirmed |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | dev @ `05ea5073` (pre-1.18.30); underlying code path is long-standing, not version-pinned to a specific release window |
| **Latest Platform Version** | 1.18.30 |
| **Labels** | (none observed) |

**Summary:**
When OpenCode's MCP client fails to obtain a server's tool list (`client.listTools()` throwing for any reason), the failure is caught and discarded entirely in `packages/opencode/src/mcp/catalog.ts` (`Effect.catch(() => Effect.void)`, no logging), then re-reported one layer up in `packages/opencode/src/mcp/index.ts` as a brand-new `Error("Failed to get tools")` with a hardcoded, constant message — the real underlying error (transport closed, unsupported method, schema validation failure, server 500, or anything else) is unreachable by that point and never appears in any log file. A second, separate defect in the same code: the "server advertises zero tools" case yields `[]`, and `if (!listed)` is `false` for an empty array, so a legitimately-empty tool list is indistinguishable from a `tools/list` call that never happened — both look like healthy no-op cases from the outside. The reporter isolated a concrete real-world case (Android/Bun-for-Android StreamableHTTP MCP server) where `client.listTools()` throws before a single `tools/list` request byte reaches the server — ruled out timeout, response framing, network, and server-side causes by direct measurement — yet OpenCode's API and logs give zero information beyond the constant string. A fix PR (#47942, "preserve tool discovery failure details") is already open against the issue.

**Impact on Orchestration:**
This is a diagnosability failure for the entire class of MCP tool-list errors, independent of model/provider — it affects OpenCode's own MCP client machinery, not any backend. In a MOSAIC deployment, if an MCP server integration breaks (bad config, transport issue, server-side bug, protocol mismatch), the only signal exposed via `GET /mcp` or `opencode mcp list` is the literal string "Failed to get tools" with no server log entry accompanying it — an operator or an automated supervisor monitoring a headless `opencode serve` deployment has no way to distinguish transport failure from schema-validation failure from server error from "genuinely zero tools," making automated remediation or alerting on MCP health effectively impossible. This compounds directly with OC-061 (tools connected but not exposed) and OC-062 (tool-call errors truncated) — together these three issues mean MCP failures at every stage (registration, discovery, and invocation) are silently or opaquely reported in current OpenCode.

**Evidence:**
- Reporter provided exact file/line references (`catalog.ts:38-40`, `index.ts:390-394`, `index.ts:890`) and traced the swallow-and-replace pattern precisely, including the secondary `if (!listed)` / empty-array logic bug.
- Reporter ruled out timeout, response framing, network, and server availability as causes via direct A/B measurement (a raw JSON-RPC client in the same process against the same listener succeeds), isolating the throw specifically to inside OpenCode's `client.listTools()` call path before any request is sent.
- A fix PR (#47942, "fix(mcp): preserve tool discovery failure details") is already open and linked to the issue as of the report date — satisfies the "linked to open fix PR" Confirmed-confidence criterion.
- Reporter is a bot-style automated reporting account (`arena-ai-coding-agent`) rather than a human GitHub user, but the technical analysis is self-contained, reproducible, and code-verifiable independent of the reporter's identity.

**Workaround(s):**
No workaround exists for recovering the discarded error message — it is unrecoverable once swallowed by the current code. The only mitigation is operational: treat any `{"status":"failed","error":"Failed to get tools"}` response as an opaque MCP integration failure requiring manual investigation (check the MCP server directly with a raw MCP client, e.g. `@modelcontextprotocol/inspector`, to determine the actual cause) rather than expecting OpenCode's own logs or API to reveal it, until PR #47942 lands.

**Notes:**
Single-reporter issue (via an automated reporting account, not a traditional human report) but promoted to Confirmed based on the precision of the code-level root-cause analysis and the linked open fix PR (#47942) — satisfies the evidentiary bar despite the unconventional reporter. Directly complements OC-061 and OC-062 as a triad of MCP diagnosability/reliability gaps; recommend treating these three as a themed cluster in the Quick Summary. Re-check next pass whether #47942 has merged.

**Verification pass (2026-09-10, run 2):** Re-fetched issue #47644 — still open, zero comments beyond the original report, fix PR #47942 still open/unmerged, no maintainer response. Matches prior capture exactly. No change to classification, confidence, impact, or workarounds. Not stale (4 days since last activity).

---

### OC-065: Local MCP servers can silently fail or crash mid-session with no error surfaced, and no CLI recovery exists short of a full restart

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anomalyco/opencode/issues/48196 |
| **Reported** | 2026-09-09 |
| **Last Activity** | 2026-09-09 |
| **Confidence** | Unverified |
| **Orchestration Impact** | MEDIUM |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 1.18.26 |
| **Latest Platform Version** | 1.18.30 |
| **Labels** | (none observed) |

**Summary:**
A single reporter documented four distinct incidents (across 2 sessions/users) where a local MCP server (including WSL-wrapped servers) failed to register its tools or lost them mid-session, with nothing surfaced to the user beyond the tools simply becoming unavailable: (1) a WSL distro was `Stopped` at cold-boot and the spawn likely timed out silently; (2) a non-WSL server's backing process vanished mid-session unprompted; (3) a server process was confirmed alive via `pgrep`/`ps aux` yet none of its tools were ever available, suggesting a dropped or incomplete handshake; (4) a WSL-wrapped server served at least one tool call successfully then its process exited, with `opencode mcp list` from a separate shell eventually reporting `failed - Operation timed out after 30000ms`. The reporter also notes `opencode mcp` has no `restart`/`reconnect` subcommand — a dead server requires a full OpenCode restart to recover. Only a github-actions bot comment (cross-referencing likely-duplicate issues) followed; no maintainer or independent human confirmation as of capture.

**Impact on Orchestration:**
Directly overlaps with OC-061 (tools connected but not exposed) and OC-064 (tool-list failures give no diagnostic signal) but adds two operationally important angles for MOSAIC: (a) MCP servers can die or lose their handshake *mid-session*, not just at startup, silently degrading a running orchestration without any surfaced error; (b) there is no in-session recovery path — an automated/headless MOSAIC deployment cannot reconnect a dropped MCP server without tearing down and restarting the entire OpenCode session, which is disruptive to a Runner-driven pipeline expecting to keep working state (conversation history, in-flight subagent delegation) intact.

**Evidence:**
- Single reporter, but four separately observed incidents across two sessions/users with concrete process-level verification (`pgrep`, `ps aux`, `opencode mcp list` cross-checks) rather than vague symptom description — satisfies the "concrete reproduction steps" bar for Unverified inclusion.
- Automated bot comment cross-references three plausibly-duplicate issues (#38266, #47644, #36288) describing the same silent-drop symptom class, plus two feature requests for in-session MCP reload (#44087, #39987) — suggesting this is a recognized, recurring gap rather than an isolated one-off, even though no independent human has yet confirmed this specific issue.
- No maintainer acknowledgment; issue is only 1 day old at capture time (2026-09-09) — genuinely too fresh to expect confirmation yet, not indicative of stagnation.

**Workaround(s):**
1. ★ Periodically poll `opencode mcp list` from a separate shell/process against a long-running session to detect a server transitioning to `failed`, since the running session itself will not surface this — reporter's own diagnostic method for incident 4.
2. No in-session recovery exists; a dead/hung local MCP server currently requires a full OpenCode restart to reconnect (feature request for `opencode mcp restart <name>` is open but unimplemented, see #44087/#39987).

**Notes:**
Heavily overlapping with OC-061 and OC-064 — same underlying theme of MCP registration/discovery/health being invisible to the session and to external monitoring. Kept as a separate entry for now because it captures the mid-session-crash and no-restart-mechanism angles specifically, which are distinct operational risks from the "never registered" (OC-061) and "error message discarded" (OC-064) framings. Recommend the next update pass reassess whether these three should be consolidated into one themed entry once maintainer engagement clarifies whether they share a single fix.

**Verification pass (2026-09-10, run 1):** Directly re-read the two other bot-flagged cross-references, #38266 and #36288. #38266 is confirmed genuinely the same root-cause family (transient `listTools()` failure permanently evicts the MCP client from state, never recreated) — see the parallel verification note added to OC-024, which carries the fuller evidence; correctly bucketed here too. #36288 ("Unreachable local MCP server silently hides all file-based commands from the TUI palette") was investigated and found **not applicable to this KB**: it is a TUI-only rendering defect — the issue's own reporter confirmed via direct API testing (`GET /command`) that the commands are loaded correctly server-side regardless of MCP state, and only the TUI's command-palette population is affected. This fails the Relevance Litmus Test's Domain/Materiality filters (a display-layer bug where the underlying operation succeeds, and MOSAIC's headless/API execution mode does not use the TUI palette) — not added as a KB entry and not a meaningful duplicate of this issue's MCP-health theme; noted here only so it isn't re-investigated in a future pass.

**Verification pass (2026-09-10, run 2):** Re-fetched issue #48196 and its 1 comment — the github-actions bot duplicate-flag comment (citing #38266, #47644, #36288, #44087, #39987) is the only activity since capture, and its content matches what was already reflected in this entry's Evidence/Notes. Still open, still single-reporter, no maintainer response. Confidence remains Unverified per CaptureGuide (single reporter, no independent confirmation yet — genuinely too fresh at 1 day old for stagnation concerns). No change to classification, confidence, impact, or workarounds. Not stale.

---

### OC-066: Parallel MCP tool calls trigger redundant tool-list re-fetches and event-loop-blocking output processing (CPU saturation, no yieldNow)

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anomalyco/opencode/issues/34867 |
| **Reported** | 2026-07-02 |
| **Last Activity** | 2026-09-01 |
| **Confidence** | Likely |
| **Orchestration Impact** | MEDIUM |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Reported July 2026; GitHub-closed as stale 2026-09-01 with no merged fix — status on 1.18.30 unverified |
| **Latest Platform Version** | 1.18.30 |
| **Labels** | (none observed) |

**Summary:**
When a model makes many parallel MCP tool calls, an MCP server's `ToolListChangedNotification` firing rapidly triggers a full, redundant `McpCatalog.defs()` re-fetch on every notification even when the tool definitions haven't actually changed, saturating CPU. Separately, and more relevant to reliability than pure cost, the code paths that iterate large tool outputs and truncate large text blocks contain no `yieldNow` calls, meaning they can block Node's/Bun's event loop outright while processing large parallel tool-call results. A cross-referenced duplicate (#17167) independently describes the same root cause (redundant `listTools()`/`defs()` calls with no caching), which is why this is rated Likely rather than Unverified despite the primary report having only a single author. The issue carries a linked fix PR (#34868, "add yieldNow to tool result and truncation loops, debounce MCP notifications"), but that PR shows `state: CLOSED` without a corresponding merge — the issue itself was then auto-closed as `not_planned` by a 60-day stale-issue bot on 2026-09-01, not by an actual fix landing. This means the underlying defect's fix status on current OpenCode (1.18.30) is unverified, not confirmed resolved.

**Impact on Orchestration:**
The event-loop-blocking half of this crosses from a pure cost/efficiency concern into a reliability concern per the Materiality filter: MOSAIC's fan-out orchestration patterns routinely make multiple parallel subagent/tool calls, and if that pattern includes multiple MCP tool calls in flight when an MCP server emits list-changed notifications (e.g. a server whose tool set is dynamic), the resulting CPU saturation and event-loop blocking could manifest as slow or apparently-hung tool responses and, in the worst case, contribute to timeouts in an automated Runner pipeline that expects tool calls to complete within a bounded window. This is plausible under MOSAIC's actual parallel-dispatch usage pattern, not an edge case MOSAIC avoids.

**Evidence:**
- Two independent reports of the same root cause: this issue and #17167 (cross-referenced by the triage bot) both describe uncached, redundant `listTools()`/`McpCatalog.defs()` re-fetches — satisfies "several independent reports of same symptoms" for Likely confidence.
- A fix PR (#34868) was opened addressing exactly the two mechanisms described (yieldNow additions + notification debouncing) but was closed without being merged, and the issue was subsequently auto-closed by a stale-issue bot for inactivity — this is a closure by inactivity, not by resolution, so the underlying defect should be treated as still present absent further evidence.

**Workaround(s):**
No community-confirmed workaround identified. If CPU saturation or apparent hangs are observed during heavy parallel MCP tool-call usage, consider limiting the number of concurrent MCP tool calls issued to a single OpenCode session, or avoiding MCP servers that emit frequent `ToolListChangedNotification` events during active tool-call bursts, until this is independently re-verified or fixed.

**Notes:**
Needs re-verification: the GitHub `closed`/`not_planned` status reflects staleness-bot cleanup, not a confirmed fix — the linked fix PR (#34868) was closed without merging. Treating as still-active in this knowledge base rather than moving to resolved, since "resolved" requires the behavior to no longer exist or matter, and there is no evidence of that here. Re-check on next pass whether a maintainer or later PR actually landed a fix, or whether current 1.18.x still exhibits the CPU/event-loop-blocking behavior under parallel MCP tool calls.

**Verification pass (2026-09-10, run 2):** Re-fetched issue #34867 and both comments — matches prior capture exactly (duplicate cross-reference to #17167, then stale-bot auto-close on 2026-09-01). Fix PR #34868 remains closed-without-merge. No maintainer engagement, no evidence the underlying defect was actually fixed. Still flagged Needs re-verification — closure is by inactivity, not resolution, so this stays active rather than moving to resolved. No change to classification, confidence, or impact.

---

### OC-067: MCP tool-call can hang a session indefinitely with no timeout, and `/interrupt` cannot cancel it (deadlock)

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anomalyco/opencode/issues/35207 |
| **Reported** | 2026-07-03 |
| **Last Activity** | 2026-09-02 |
| **Confidence** | Likely |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 1.17.13; GitHub-closed as stale 2026-09-02 with no fix landed — status on 1.18.30 unverified |
| **Latest Platform Version** | 1.18.30 |
| **Labels** | (none observed) |

**Summary:**
An MCP tool-call that never returns (observed with a `playwright_browser_*` tool against a slow/crashing page, but the mechanism is generic to any MCP server) permanently hangs the entire OpenCode session. The reporter's diagnostics (process alive, zero established TCP connections, no shell child process running) rule out the LLM API and shell tool execution as the cause, isolating the hang to OpenCode's MCP tool-call state machine waiting forever for a response that will never arrive. `/interrupt` and Esc have no effect on the stuck call — the only recovery is killing the OpenCode process outright, losing the session. There is no per-tool-call timeout distinct from the documented tool-list-fetch timeout; setting `"timeout"` on an MCP server config only bounds the initial `tools/list` fetch, not runtime tool calls, so it is not an effective mitigation for this hang. The issue was closed by a 60-day stale-issue bot on 2026-09-02 with `not_planned` — there is no linked fix PR and no maintainer comment, so the closure reflects inactivity, not resolution.

**Impact on Orchestration:**
This is a HIGH-impact tool-execution and conversation-stability defect: a single MCP tool call that stalls (browser automation, a flaky remote MCP server, a crashed subprocess, a stuck stdio pipe) can deadlock an entire OpenCode session with no recoverable signal and no working interrupt path. For MOSAIC's headless/automated Runner pipelines, this means a single bad MCP tool call in a subagent's execution can wedge that agent's session indefinitely, consuming resources and blocking the orchestration flow, with the only recovery being an external process kill that destroys the session's state entirely (equivalent to data loss for that unit of work). This is functionally the same class of failure as OC-001 (nested subagent permission-ask hang) but triggered by MCP tool calls specifically rather than permission prompts — both point to a broader pattern of unrecoverable hangs in OpenCode's tool/permission state machine with non-functional interrupt handling.

**Evidence:**
- Rigorous process-level diagnostics from the reporter (thread count, TCP connection state, child-process inventory) isolating the hang specifically to the MCP tool-call wait, ruling out LLM API and shell-command causes.
- Automated bot cross-referenced four plausibly-duplicate/related issues describing the same class of unrecoverable hang: #31235 (parallel MCP tool call stuck in `state.status="running"` forever), #13841 and #11865 (subagent/session hangs indefinitely, no timeout/retry, `/interrupt` non-functional), and #33028 (stream never times out after tool call completes, Windows-specific) — this satisfies "several independent reports of same symptoms" for Likely confidence.
- No maintainer response and no fix PR exists; the issue was auto-closed for staleness, not resolved — this caps confidence at Likely and means the underlying defect should be presumed to still exist absent contrary evidence.

**Workaround(s):**
1. ★ Disable MCP servers known to be prone to non-returning calls (e.g. browser-automation servers like Playwright MCP against unreliable pages) by default, enabling them only for specific, supervised tasks — reporter's own partial mitigation.
2. Setting a `"timeout"` value on an MCP server's config does NOT protect against this — per OpenCode's own docs (confirmed by the reporter) that timeout only bounds the initial tool-list fetch, not runtime tool-call execution. No effective workaround exists for a tool call that hangs mid-execution; the only recovery is killing the process externally, which loses the session.

**Notes:**
Needs re-verification on current 1.18.x: closed via stale-issue bot (`not_planned`) with zero maintainer engagement and no fix PR, not because the defect was resolved. Cross-reference OC-001 (nested subagent permission-ask hang, also unrecoverable, also `/interrupt`/stop are no-ops) — both entries point at the same underlying architectural gap: OpenCode's interrupt/cancellation mechanism does not reliably reach in-flight tool-call or permission-ask state. Recommend a follow-up pass investigate #31235, #13841, #11865, and #33028 directly, and consider whether OC-001 and OC-067 should be cross-linked more explicitly as symptoms of one root architectural issue (non-functional interrupt/cancellation).

**Verification pass (2026-09-10, run 2):** Re-fetched issue #35207 and both comments — matches prior capture exactly (bot duplicate cross-reference to #31235/#13841/#11865/#33028, then stale-bot auto-close on 2026-09-02). No maintainer response, no fix PR, no evidence of resolution. Still flagged Needs re-verification and remains active. #31235/#13841/#11865/#33028 still not directly investigated — carrying forward as a suggested follow-up. No change to classification, confidence, or impact.

---

### OC-081: Injected prompts without agent/model silently switch the session's agent and model

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anomalyco/opencode/issues/46111 |
| **Reported** | 2026-08-29 |
| **Last Activity** | 2026-09-08 |
| **Confidence** | Confirmed |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | dev (current as of report) |
| **Latest Platform Version** | v1.18.30 |
| **Labels** | bug |

**Summary:**
When a prompt arrives without explicit `agent`/`model` fields — e.g. a background subagent's completion notification re-prompting the parent — `createUserMessage` in `packages/opencode/src/session/prompt.ts` falls back to the *default* agent and that agent's configured model instead of consulting the session's currently active agent/model. This silently switches the session's agent/model mid-run. An open fix PR (#46106) exists and a maintainer is assigned; multiple independent users confirm hitting it in production.

**Impact on Orchestration:**
MOSAIC's subagent-dispatch pattern relies on the orchestrator's session staying on its configured model/agent across turns, including turns triggered by subagent completions. This bug means a subagent finishing and reporting back to the primary agent can silently downgrade the primary's model (e.g. from an advanced model to a cheaper default), degrading orchestration quality without any visible error, and also busts provider prompt-cache prefixes (extra cost) since the cache key includes the model.

**Evidence:**
- Root cause identified with exact code path (`createUserMessage` in `prompt.ts`) by reporter, referencing prior unmerged fix attempts (#21728, #35195)
- Open fix PR #46106 explicitly linked as the issue's closing PR, with a maintainer (`neriousy`) assigned
- Two independent users (`LordMike`, `jtoronto`) confirmed hitting the exact symptom in comments dated 2026-09-05 and 2026-09-08
- Bot-flagged as likely duplicate/cluster with #42893, #43179, #41221 — all describing the same `createUserMessage` model-resolution chain losing the session's active model

**Workaround(s):**
1. ★ No confirmed workaround yet from maintainers or community; the only mitigation mentioned is hoping the fix (PR #46106) lands, which would make prompt delivery preserve the session's agent/model unless an agent switch is genuinely requested.
2. Defensive: explicitly pass `agent`/`model` on every injected/fire-and-forget prompt path if your integration controls that call site, so the fallback branch is never hit.

**Notes:**
Cluster of related open issues on the same root cause: #42893 (active tracking issue for the same bug per bot triage), #43179 (primary-agent switches keep previous agent's model in V2), #41221 (manually selected model resets to config default on agent switch). This entry tracks #46111 as the most complete/live write-up; if #42893 is investigated in a future pass, cross-reference here rather than duplicating.

---

### OC-082: Custom agent frontmatter `permission` block merges additively with defaults instead of replacing them

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anomalyco/opencode/issues/47819 |
| **Reported** | 2026-09-07 |
| **Last Activity** | 2026-09-07 |
| **Confidence** | Unverified |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | dev@57ef382843 / v1.18.29 |
| **Latest Platform Version** | v1.18.30 |
| **Labels** | none observed |

**Summary:**
OpenCode documents that most permissions default to `allow`, but does not document that an agent frontmatter `permission` block is merged *additively* on top of that permissive default (`agent.ts:293`; `Permission.merge` flattens rulesets and `findLast` — last matching rule wins, `wildcard.ts:8`). A partial allowlist in a custom agent's frontmatter therefore closes nothing: any action not explicitly denied still falls through to the default catch-all `allow`. The reporter walked through the exact code path (no maintainer response yet) and gave a concrete repro: a subagent configured with a bash allowlist and no `"*": deny` rule can still run `stat`/`ls` outside its intended worktree, because non-file-aware commands also bypass `external_directory` scanning (`shell.ts:378-414`).

**Impact on Orchestration:**
This is a direct hit on MOSAIC's per-agent permission scoping — the architecture that gives subagents narrower tool permissions than the orchestrator. If an author defines a subagent's frontmatter `permission` as a partial allowlist (a natural way to write a "read-only" or "restricted" agent) without knowing to prepend `"*": deny`, the subagent silently retains full default-allow permissions for everything not explicitly listed. A MOSAIC subagent intended to be sandboxed to a worktree or a narrow tool set could execute arbitrary shell commands or touch files outside its intended scope, with the agent author having no indication anything is wrong — the misconfigured allowlist looks correct on inspection.

**Evidence:**
- Single reporter (`moaines`), but the report is code-verified with exact file:line references (`agent.ts:293`, `permission/index.ts:200-202`, `wildcard.ts:8`, `shell.ts:378-414`) and a concrete, reproducible repro sequence on a specific commit (dev@57ef382843) and release (v1.18.29)
- No maintainer comment yet; a maintainer (`neriousy`) is assigned to the issue (consistent with triage auto-assignment, not confirmation) but has not acknowledged as of 2026-09-07
- No community reproduction/upvotes yet — issue is 3 days old at time of this capture
- Included as Unverified despite the single-reporter status because it satisfies all three admission criteria: (1) it threatens a core MOSAIC orchestration pattern (subagent permission scoping/isolation), (2) it is reported against stock OpenCode with no custom backend involved, and (3) the reporter provided concrete, code-level reproduction steps rather than vague symptoms

**Workaround(s):**
1. ★ Author-side mitigation proposed by the reporter: every custom agent frontmatter `permission` allowlist must explicitly start with a `"*": deny` rule (or equivalent blanket deny) before adding specific allow rules, since rule evaluation is `findLast` (last matching rule wins) and rules are otherwise additive on top of the permissive default.
2. No fix has landed; the reporter's proposed code fix (merge-by-replacement for agent-level `permission`, or an implicit `"*": deny` injected for subagents) has not yet been implemented.

**Notes:**
Needs re-verification on a future pass — issue is very new (filed 2026-09-07) with no comments and no linked fix PR yet. If MOSAIC generates or authors OpenCode subagent frontmatter with partial `permission` allowlists, this is directly actionable now: always prepend `"*": deny` in generated permission blocks until the harness behavior changes or is documented.

---

### OC-083: `OPENCODE_CONFIG_DIR` overrides (rather than adds to) the global AGENTS.md path

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anomalyco/opencode/issues/28658 |
| **Reported** | 2026-05-21 |
| **Last Activity** | 2026-09-05 |
| **Confidence** | Confirmed |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | v1.15.6 through at least v1.18.x (re-reported and re-diagnosed 2026-09-05) |
| **Latest Platform Version** | v1.18.30 |
| **Labels** | none observed |

**Summary:**
`ConfigPaths.directories()` always includes the static XDG global config path (`~/.config/opencode`) when discovering agents/commands/skills, but the global `AGENTS.md` instructions loader (`instruction.ts`) instead resolves its path from `Global.Service.config`, which is overridden by the `OPENCODE_CONFIG_DIR` env var when set. As a result, setting `OPENCODE_CONFIG_DIR` to add a secondary/workspace config directory correctly merges in agents/commands/skills from both locations, but silently drops the global `AGENTS.md` entirely unless the user duplicates it into the override directory. A first fix attempt (PR #32824) was closed without merging; a second, more targeted fix (PR #47468, still open as of 2026-09-05) was filed against the same root cause, independently re-diagnosing the identical code paths.

**Impact on Orchestration:**
This directly affects MOSAIC's system-prompt/AGENTS.md-style injection, which is one of MOSAIC's two explicitly load-bearing mechanisms (alongside permission scoping) for its three-layer agent composition model. If MOSAIC uses `OPENCODE_CONFIG_DIR` to layer a workspace- or run-specific config directory on top of a global one (a natural way to scope per-project instructions), the global-layer `AGENTS.md` instructions silently stop being injected into any session — with no error, since the harness behaves as if the global instructions file doesn't exist. Any orchestrator- or global-level guidance MOSAIC expects every agent to inherit could be silently absent.

**Evidence:**
- Reporter identified exact code paths and line numbers (`paths.ts:26`, `instruction.ts:64`, `global.ts:63`) showing the inconsistency between directory-scan resolution (static XDG path) and instructions-path resolution (flag-overridden path)
- Independent reporter (`eminfedar`, 2026-07-20) confirmed the same symptom ("global AGENTS.md ... doesn't read the file")
- A second independent contributor (`minutechreview`, 2026-09-05) re-diagnosed the identical root cause from scratch and opened a new fix PR (#47468), confirming the bug is still present as of that date
- Two fix PRs reference this issue: #32824 (closed, unmerged) and #47468 (open as of 2026-09-05)

**Workaround(s):**
1. ★ Duplicate the contents of the global `AGENTS.md` into `$OPENCODE_CONFIG_DIR/AGENTS.md` whenever `OPENCODE_CONFIG_DIR` is set, so the global instructions are present in the path OpenCode actually checks — inferred from the root-cause description; not explicitly stated as user-tested in the thread.
2. Avoid setting `OPENCODE_CONFIG_DIR` at all if relying on a global `AGENTS.md`, and instead place all instructions directly under the default `~/.config/opencode` location.

**Notes:**
Open since 2026-05-21 (~3.5 months) with only 2 comments and no maintainer acknowledgment comment (an assignee is present but has not commented) — flagging "needs re-verification" is not required since the bug was independently reconfirmed just 5 days before this capture (2026-09-05), but the fix (PR #47468) had not merged as of that date. Re-check on next pass whether #47468 has merged.

---

### OC-084: `permission.ask` plugin hook is documented/typed but never triggered — no programmatic permission interception is possible

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anomalyco/opencode/issues/7006 (primary tracking issue; duplicate of #47674, #46809, #47654, #9229) |
| **Reported** | 2026-01-05 |
| **Last Activity** | 2026-09-06 |
| **Confidence** | Confirmed |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | v1.1.2 through current stable (v1.18.25/1.18.26/1.18.30) — confirmed still broken on every published build, not just dev/beta |
| **Latest Platform Version** | v1.18.30 |
| **Labels** | none observed |

**Summary:**
The `permission.ask` plugin hook is declared in the plugin SDK types (`@opencode-ai/plugin`) and, as of v1.18.26, documented in the bundled plugin-authoring docs as part of the hook surface — but no code path in OpenCode ever calls `Plugin.trigger("permission.ask", ...)`. A plugin that registers the hook loads successfully and is silently never invoked: no error, no warning, the permission system falls straight through to the interactive UI prompt or default rule action. This is not a documentation-only bug: static analysis of the published npm bundles going back to `opencode-ai@1.2.26`/`1.2.27` (pre-dating the commit some contributors blamed) found zero `permission.ask` call sites, meaning the hook has arguably *never* worked on any stable release, only briefly on some dev/beta snapshot. Originally filed 2026-01-05, this is one of the longest-open unresolved issues found in this pass (8+ months), with 26 upvotes, 16 comments, and at least 7 separate PRs attempting a fix (two open as of 2026-09-06: #19453, #30509, #42633; several others closed unmerged) — none merged. #47674 (filed 2026-09-06) is a verbatim duplicate with identical repro and root cause, auto-flagged by the repo's bot; #46809, #47654, and #9229 are further closed duplicates, indicating repeated independent rediscovery of the same gap.

**Impact on Orchestration:**
This is the primary extension point OpenCode exposes for programmatic, non-interactive permission handling — exactly what MOSAIC's headless/automated Runner-driven execution needs to auto-approve or auto-deny tool/permission requests without a human in the loop. Because the hook is dead code, any MOSAIC integration attempting to build an auto-approval or policy-based permission gate via this documented API will silently do nothing: permissions will still either fall through to an interactive prompt (which blocks headless execution entirely) or resolve via static config rules only, with no way to apply dynamic/contextual logic (e.g., approve `bash` commands matching a safe-pattern list, matching MOSAIC's own scoping needs). Combined with OC-085 (`opencode run --auto` not cascading to subagents) and OC-082 (frontmatter permission merge bug), this represents a systemic gap in OpenCode's non-interactive permission story that MOSAIC's headless pipelines must work around entirely outside the harness's supported extension points.

**Evidence:**
- 26 upvotes and 16 comments over 8+ months, with independent reproductions from at least 5 different users (markerikson, select, ndrwstn, Ryan9438, fwa-wup) across different versions and use cases
- Root cause pinpointed to a specific commit (`38e0dc9`, "Move service state into InstanceState, flatten service facades") by contributor `athal7`, though a later comment (`fwa-wup`, 2026-09-01) found the call site missing even in pre-refactor stable npm bundles (1.2.26/1.2.27), suggesting the gap predates that commit for stable users
- At least 7 fix PRs opened over the issue's lifetime (#7077, #19453, #22619, #30509, #34329, #39442, #42633) — three still open as of last activity, one (#42633) independently compiled, tested (94/94 passing), and verified working on a live build by a third-party contributor
- Confirmed still broken on current stable (1.18.25) via direct empirical probe reported 2026-09-01
- Duplicate issue #47674 filed independently on 2026-09-06 with an identical repro, auto-flagged by the repo's duplicate-detection bot as matching #7006, #46809, #47654, #9229 — five total reports of the same root cause
- No maintainer has posted an acknowledgment comment in the #7006 thread despite an assignee (`rekram1-node`) being set and multiple direct pings (most recently 2026-09-06); assignment without comment is not counted as acknowledgment per validation rules, but the sheer count of independent reproductions, duplicate filings, and mergeable fix PRs supports Confirmed confidence on the behavior itself

**Workaround(s):**
1. ★ Listen for the `permission.asked` bus event (undocumented type, requires a cast since it's missing from the public event union: `event.type === ("permission.asked" as "permission.updated")`) and reply via the permission-reply endpoint rather than the `permission.ask` hook — reported working by multiple users (`repomaa`, confirmed by `lgarceau768` on 2026-09-02).
2. For denial-only logic, throw from the `tool.execute.before` hook instead of using `permission.ask` — noted as a partial substitute in the issue body, but this only covers tool-call-shaped permissions, not non-tool permissions like `external_directory` or sandbox escalation, which never reach any plugin hook at all.
3. Community members with build access can compile from PR #42633's branch (`dd25dface`), reported independently verified working in `opencode run` (headless) mode as of 2026-09-01 — not an official release, use at own risk.

**Notes:**
This is a cluster of at least 5 duplicate GitHub issues (#7006, #47674, #46809, #47654, #9229) all tracking the identical root cause; this entry uses #7006 as the canonical/primary source since it has the longest history and most PR activity. Despite being open 8+ months with strong community pressure (26 upvotes, working fix PRs), no maintainer has merged a fix or publicly committed to a direction (a 2026-07-29 comment explicitly asked maintainers to pick between the narrow hook-restoration PRs and a broader "Gate" design proposal in #34329, with no response as of 2026-09-06). This long-standing lack of maintainer traction despite mergeable fixes is itself a signal worth tracking — re-verify on next pass whether any of #19453/#30509/#42633 merged.

**Verification pass (2026-09-10, run 1):** Directly re-read #46809, #47654, and #9229 rather than trusting the repo's own duplicate-detection bot label. All three are confirmed genuinely the same root cause, not a bot misclassification — each independently and precisely diagnoses the identical dead hook, and together they add valuable new evidence: #9229 (the earliest, Jan 2026) first identified the two-permission-module split (`permission/index.ts`, which calls the hook but is unused, vs. the actively-used `permission/next.ts`, which never does) and is the source of the `event`-hook-with-`permission.asked` workaround already listed above. #46809 (Sep 2026) is a live, first-hand probe test: the reporter's own plugin handler never fired and setting `output.status = "allow"` changed nothing, directly confirming the hook is fully inert rather than merely undocumented; it also flags a secondary detail worth noting for any future fix — the `Permission` payload's `patterns` field carries the literal command string, not the config pattern that matched, so a handler cannot cleanly distinguish which config rule triggered a given ask. #47654 (Sep 2026) pinpoints the exact removal commits and version (`f015154314` then `2fc06c5a17`, first absent in v1.3.0, ~March 2026) and adds a materially important detail not previously captured here: OpenCode's own **bundled `customize-opencode` skill** (`packages/core/src/plugin/skill/customize-opencode.md`) still teaches `permission.ask` as a live hook, meaning OpenCode's own built-in agent-authoring guidance actively leads plugin authors (including an LLM agent following that skill) into writing dead handlers. Recommend folding this skill-teaches-dead-hook detail into any future workaround guidance, since it means the failure mode can be freshly reintroduced by anyone (or any agent) following OpenCode's own documentation.

---

### OC-085: Wildcard `"*": deny` permission rule intermittently overrides more-specific `allow` rules (long-running regression)

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anomalyco/opencode/issues/24335 |
| **Reported** | 2026-04-25 |
| **Last Activity** | 2026-09-07 |
| **Confidence** | Confirmed |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | v1.14.25 through at least v1.18.14 (repeatedly reconfirmed across ~10 point releases spanning 4+ months) |
| **Latest Platform Version** | v1.18.30 |
| **Labels** | none observed |

**Summary:**
OpenCode's documented permission model states that rules are evaluated in order with the last matching rule winning, so a config that puts `"*": "deny"` first and a specific path `allow` after it should permit only that path. In practice, this ordering is unreliable: a config using `"*": "deny"` followed by a specific `allow` pattern (for `edit`, `bash`, etc.) frequently denies everything, including the paths explicitly allowed. A contributor initially claimed this was fixed by an unrelated merge (#24308, which fixed a key-ordering issue in config decoding) shortly after the issue was filed, but the reporter and at least 5 further independent users confirmed the bug persisted across nearly every subsequent release for over 4 months (v1.14.27, v1.14.45, v1.15.12, v1.17.4, v1.18.14, and again as of 2026-09-07) — including one report that it also regressed a previously-working case. One user explicitly reports it affecting sub-agent-level permission config specifically (agent-level `"*": deny` overriding the sub-agent's own more specific allow rules even when global permissions were correctly set to allow).

**Impact on Orchestration:**
Directly undermines MOSAIC's per-agent permission scoping model, which depends on being able to write a deny-by-default policy with narrow allow exceptions per agent (the pattern OpenCode's own docs recommend: catch-all `*` deny first, specific allows after). Because wildcard-vs-specific rule precedence is unreliable, a MOSAIC-generated subagent permission config intended to scope a subagent narrowly (e.g., "deny everything except this project subdirectory") may deny the subagent's legitimate, intended operations entirely, breaking the subagent's task — or, per the sub-agent-specific report in this thread, may deny operations even when the more permissive global config should apply, producing confusing failures that look like an unrelated permission or path bug. One community member explicitly called this a "fairly critical security issue" given permissions silently not matching documented/expected behavior.

**Evidence:**
- Confirmed independently by at least 6 different users (matthew-j-hooper, RisaKirisu, socrabytes, tamasys, enduro, fidodido48) across a 4+ month span and ~10 distinct point releases
- A maintainer-adjacent contributor (`tiffanychum`) initially investigated in depth, identified a related root cause (Effect Schema decode not preserving key order), merged a fix (#24308) and opened a regression test (#24361), but the reporter confirmed the exact same symptom persisted on the very next release (1.14.27) — indicating the true root cause was not fully addressed or a related regression reappeared
- A second fix attempt, PR #28699 ("prefer specific permission rules"), was also closed without resolving the issue per subsequent user reports on 1.18.14 (2026-08-06) and 1.18.x (2026-09-07)
- Bot-flagged as related to #20307, #13872, #7029 — a recurring pattern of wildcard/specific-rule precedence bugs across the permission system's history

**Workaround(s):**
1. ★ Avoid using a `"*": "deny"` wildcard rule at the agent/sub-agent config level; instead omit the wildcard deny and rely on specific allow rules plus the default "ask" behavior for everything else — reported as the only unblocking workaround by a user who hit this specifically at the sub-agent level (`socrabytes`, v1.17.4, 2026-06-12). This trades a hard deny for an interactive prompt on non-allowed paths, which does not work for headless/non-interactive execution.
2. Some users report needing to duplicate deny rules across both `edit` and `bash` permission categories (since the AI can route around an edit-tool deny using bash commands) plus OS-level file permission hardening as a defense-in-depth measure — not a fix for the ordering bug itself, but mitigates its consequences for security-sensitive paths.

**Notes:**
This is a long-running, apparently still-unresolved regression despite two attempted fixes (#24308, #28699) — the underlying wildcard-vs-specific rule precedence logic in OpenCode's permission evaluator has not reliably matched its documented behavior for over 4 months as of this capture. Given the sub-agent-specific reproduction, this interacts with OC-082 (frontmatter permission additive-merge bug) and OC-084 (permission.ask hook dead) to form a broader picture: OpenCode's non-interactive/programmatic and config-driven permission scoping has multiple independent, long-standing reliability gaps. Recommend re-verification on the next pass against whatever the current release is at that time. See also OC-086, a related but distinct manifestation of the same last-match-wins/specificity flaw affecting plugin-registered agent permissions specifically.

**Verification pass (2026-09-10, run 1):** Directly re-read #37935, which a prior batch flagged as bot-linked to this issue. Confirmed genuinely the same root cause, not a bot misclassification: #37935 describes the identical `findLast()`/last-match-wins mechanism (a broad `$HOME/*` deny placed after a specific `$HOME/utilities/*` allow silently wins) in both the V1 (`packages/opencode/src/permission/index.ts`) and V2 (`packages/core/src/permission.ts`) evaluators, with its own fix PR (#37936, "use most-specific-pattern-wins instead of last-match-wins") also closed without merging — consistent with this entry's own two failed fix attempts (#24308, #28699). No new evidence beyond what's already captured here; correctly bucketed as a duplicate/same-family report, not a distinct defect. Kept as a cross-reference rather than a separate entry.

---

### OC-086: Plugin-registered agent-specific permissions cannot override a root-level default deny

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anomalyco/opencode/issues/47946 |
| **Reported** | 2026-09-08 |
| **Last Activity** | 2026-09-08 |
| **Confidence** | Likely |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 0.0.0-beta-19242 (2.0 beta line) |
| **Latest Platform Version** | v1.18.30 |
| **Labels** | 2.0 |

**Summary:**
When a plugin registers agent-specific permission rules (e.g., via `context.agent.transform(...)` pushing an `{action, resource, effect: "allow"}` entry), those rules do not reliably override a broader root-level default deny (e.g., `"skill": {"*": "deny"}` in `opencode.jsonc`), because OpenCode's permission evaluator flattens all rules and evaluates with plain last-match-wins ordering rather than any notion of rule specificity or scope (root vs. agent-level). Whether the plugin's agent-specific allow wins depends on plugin/transform registration order relative to root config loading — declaring the identical agent+permission directly in `opencode.jsonc` works reliably (since it is appended after root rules), but the equivalent registered by a plugin does not. Filed by a contributor with a precise repro against a real affected plugin (`opencode-babysit`); the repo's bot immediately flagged it as the same root-cause family as two other open issues (#37935, #39022) plus OC-085's issue (#24335).

**Impact on Orchestration:**
This directly affects MOSAIC's three-layer agent composition model wherever plugin-based mechanisms (rather than static frontmatter/config) are used to grant a specific subagent narrower or broader permissions than the root default — e.g., a plugin dynamically registering an agent with an allow-listed skill or tool while the workspace root enforces a default-deny posture. Because the override does not reliably apply, a subagent that MOSAIC (or a MOSAIC-adjacent plugin) intends to grant a specific capability to may be silently denied that capability whenever a root-level deny-by-default policy is in effect — the same class of failure as OC-085, but specifically breaking the plugin/dynamic-registration composition path rather than static config.

**Evidence:**
- Reported by a repo contributor (`stevoland`, `author_association: CONTRIBUTOR`) with an exact, minimal repro (root config + plugin transform code) against a real-world affected plugin
- Root cause precisely diagnosed (flattened rules + last-match-wins with no specificity/scope awareness) and a concrete comparison provided showing the equivalent static-config declaration works while the plugin-registered one does not
- Repo bot immediately flagged as duplicate-family with #37935 and #39022 (both describing agent-specific allow losing to broader/inherited deny) and with OC-085's issue #24335 (same last-match-wins root defect)
- Labeled `2.0`, suggesting the maintainers are tracking it against the upcoming 2.0 line, though no direct maintainer text comment yet
- Single reporter and only 1 day old at time of this capture — no independent community reproduction yet, capping confidence below Confirmed

**Workaround(s):**
1. ★ Declare the equivalent agent-specific permission rule directly in static config (`opencode.jsonc`) rather than registering it via a plugin transform — reporter confirms this ordering reliably works since root-config rules are appended before, not after, the plugin's rules in the affected path.
2. No plugin-side workaround identified in the thread yet; avoid relying on plugin-registered permission overrides when a root-level default deny is configured until this is fixed.

**Notes:**
Part of the same underlying "last-match-wins without specificity/scope awareness" defect family as OC-085 (#24335) and the bot-linked #37935/#39022 — track all as one systemic gap in OpenCode's permission evaluator. This entry is Unverified-adjacent (single reporter, very recent) but included as Likely given the contributor's precise root-cause diagnosis and the strong existing evidence base from the sibling issues confirming the same evaluator defect independently. Needs re-verification on next pass — check whether #37935 or this issue gained maintainer traction or a merged fix, since a fix to the shared root cause would likely resolve OC-085, OC-086, and #37935 together.

**Verification pass (2026-09-10, run 1):** Directly re-read #39022, which a prior batch bot-flagged alongside this issue. Investigation shows #39022 is **mechanically distinct**, not the same last-match-wins/specificity defect — it describes a plugin-defined agent's manifest `tools: { question: true }` never being translated into any permission rule at all for three specific tools (`question`, `plan_enter`, `plan_exit`), because only the built-in `build`/`plan` agents get an explicit code-level override of the hardcoded `defaults` deny; there is no rule-ordering conflict to resolve for #39022 the way there is for this issue and #37935. Given a real independent root cause and its own MOSAIC-relevant impact, #39022 was captured as its own new entry, OC-097, rather than folded in here.

---

### OC-087: `opencode run --auto` auto-approval does not cascade to subagent sessions — subagent permission prompts hang indefinitely

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anomalyco/opencode/issues/41730 |
| **Reported** | 2026-08-11 |
| **Last Activity** | 2026-09-09 |
| **Confidence** | Confirmed |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | v1.18.14 through at least v1.18.30 (fix still open/in-progress as of 2026-09-09) |
| **Latest Platform Version** | v1.18.30 |
| **Labels** | none observed |

**Summary:**
`opencode run --auto` is documented to auto-approve any permission not explicitly denied, but this auto-approval scope does not propagate to sessions spawned for subagents (e.g., via a Task-style subagent tool). When the main agent runs directly, `--auto` correctly bypasses `ask` rules (e.g., `edit: ask`, `external_directory: ask`); when the same operation is instead delegated to a subagent, the subagent's session hits the same `ask` rule and blocks on a permission prompt that nothing will ever answer in headless mode — the CLI invocation hangs indefinitely with no timeout and no error. An open fix PR (#42310, "cascade auto permissions to subagent sessions") exists and was still being actively scoped/discussed as of 2026-09-09, including an edge case where a reused `task_id` for a previously-seen child session can still hang because no `session.created` event fires for it.

**Impact on Orchestration:**
This is a direct, severe hit on MOSAIC's headless Runner pipeline, which explicitly depends on non-interactive, automated permission handling with no human available to answer prompts. If any MOSAIC-orchestrated subagent (dispatched via OpenCode's subagent/Task mechanism) performs an operation that falls under an `ask` permission rule, the entire run hangs forever rather than failing fast or auto-approving — there is no error to catch, retry, or route around. This is a fully headless-mode-breaking bug: any workflow that dispatches subagents under `--auto` and has any `ask`-classed permission rule in scope (the documented recommended posture, per OC-085/OC-086's discussion of deny/ask defaults) risks an unrecoverable stuck run.

**Evidence:**
- Reporter provided two independent, minimal repro cases (subagent file edit under `edit: ask`, subagent external-directory write under default `ask`) each cleanly contrasted against the main-agent case which works correctly
- Open fix PR #42310 directly targeting this issue, with active scoping discussion as recently as 2026-09-09 (one day before this capture) identifying a remaining edge case (reused `task_id` without a `session.created` event) that the current PR does not yet cover
- Bot-flagged as related to two other issues describing the identical symptom class: #36868 ("`opencode run --auto` hangs indefinitely when a Task subagent requests permission") and #13715 (broader tracking issue: "permission asks from nested subagent sessions silently hang")
- Reporter also flagged the documentation itself as misleading for not disclosing this scope limitation

**Workaround(s):**
1. ★ Set explicit `allow` (not `ask`) for every permission category a subagent might need before running any headless/automated `--auto` workflow that dispatches subagents — avoids the `ask` code path entirely for subagent sessions since there is currently no way to get `--auto`'s implicit allow-unless-denied behavior to apply to them.
2. Avoid delegating to subagents in headless/`--auto` runs until PR #42310 (or its eventual successor covering the reused-`task_id` edge case) merges and ships in a release.
3. If a hang is unavoidable, wrap `opencode run --auto` invocations in an external timeout/watchdog (e.g., in the calling orchestrator/Runner) since OpenCode itself provides no internal timeout for an unanswered subagent permission prompt.

**Notes:**
Fix PR #42310 was still open and being actively narrowed in scope as of 2026-09-09 (one day before this capture) — re-check on next pass whether it merged, and specifically whether the reused-`task_id`/no-`session.created`-event edge case was covered, since a partial fix could leave MOSAIC's Runner still exposed to hangs in that specific case. Cross-reference with OC-084 (`permission.ask` plugin hook dead) and OC-085/OC-086 (wildcard/specificity permission evaluator bugs) — together these represent a broad, current gap in OpenCode's non-interactive permission handling that directly affects any headless multi-agent orchestration, which is core to how MOSAIC operates.

---

### OC-088: Built-in Skills ignore global AGENTS.md temp-directory instructions, hardcode `/tmp` and trigger avoidable permission prompts

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anomalyco/opencode/issues/46815 |
| **Reported** | 2026-09-02 |
| **Last Activity** | 2026-09-02 |
| **Confidence** | Unverified |
| **Orchestration Impact** | MEDIUM |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | unspecified (recent, reported against current dev/stable as of 2026-09-02) |
| **Latest Platform Version** | v1.18.30 |
| **Labels** | none observed |

**Summary:**
The reporter's global `AGENTS.md` instructs the agent to always use `~/.opencode/tmp/` rather than `/tmp` (or other system temp directories) for any temporary/intermediate work. Invoking a built-in skill (e.g., `/code-review`) nonetheless requests permission to access `/tmp` even though the skill's final output is correctly written to a configured location (`~/.opencode/code_reviews/`). This indicates the skill's internal implementation uses `/tmp` directly for intermediate processing rather than consulting or honoring the AGENTS.md-injected instruction, which the model otherwise follows for user-visible file operations. A bot-flagged related issue (#36381) describes the same pattern in a different skill (the "handoff" skill), suggesting this may be a systemic property of how built-in skills handle scratch/temp files rather than an isolated case.

**Impact on Orchestration:**
This is directly analogous to MOSAIC's own system-prompt-injected instruction pattern of directing agents to a dedicated scratchpad directory instead of `/tmp` (the same instruction shape observed elsewhere in this environment). If OpenCode's built-in skills mechanism hardcodes `/tmp` regardless of AGENTS.md/instruction-injection content, any MOSAIC workflow that (a) relies on agents honoring a "use this directory, not /tmp" instruction for correctness or sandboxing reasons, and (b) uses OpenCode's built-in Skills feature, will see that instruction silently violated for skill-internal scratch work. Combined with OC-087 (auto-approval not cascading to subagents) and general permission-ask friction, an OpenCode deployment that denies or asks for `/tmp` external-directory access as a matter of policy could see skill invocations blocked or hung specifically because of this hardcoded path, independent of anything the calling agent or its instructions say.

**Evidence:**
- Single reporter, thin technical detail — no code-level root cause identified (unlike other entries in this pass), no maintainer response beyond an automated compliance-template bot comment
- Repo's automated triage bot flagged a plausibly related issue (#36381) describing the same category of problem (a different built-in skill defaulting to `/tmp` instead of a configured/project directory), which is weak corroborating evidence of a shared pattern rather than a one-off
- No reproduction from other users, no maintainer acknowledgment, no linked fix PR
- Included despite Unverified status because it directly parallels a real instruction-injection pattern MOSAIC itself relies on (steering agents away from `/tmp` toward a dedicated directory) and is reported against stock OpenCode; however, the reproduction is symptom-level ("permission prompt appears") rather than a code-verified root cause, so this is weaker evidence than most other entries in this pass — treat as a lead to watch rather than a settled finding

**Workaround(s):**
1. No workaround identified in the thread. If `external_directory` permission for `/tmp` is set to `allow` rather than `ask`/`deny`, the practical symptom (repeated permission prompts) disappears, but this does not address the underlying instruction-injection non-compliance and reduces sandboxing around the `/tmp` path.

**Notes:**
Needs re-verification and deeper investigation on a future pass — this entry is based on thin, single-reporter evidence with no code-level confirmation, unlike most other entries added in this run. Worth checking directly in a MOSAIC OpenCode deployment: whether built-in or MOSAIC-authored Skills honor injected temp-directory instructions, or fall back to `/tmp`/OS default paths internally. If confirmed via direct testing, this should be escalated from MEDIUM toward HIGH impact, since it would mean skill-internal file operations are not reliably governed by the same instruction-injection layer MOSAIC uses to steer agent behavior elsewhere.

---

### OC-089: V2 `subagent` tool does not enforce per-target subagent permissions — an actual runtime execution bypass, distinct from OC-007's legacy Task-tool description mismatch

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anomalyco/opencode/issues/35238 |
| **Reported** | 2026-07-03 |
| **Last Activity** | 2026-08-07 |
| **Confidence** | Confirmed |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | `2.0` track (`packages/core`); open since 2026-07-03 |
| **Latest Platform Version** | v1.18.30 (2026-09-09) — note this issue is specific to the "2.0" rewrite (`packages/core`), not the stable 1.18.x line |
| **Labels** | bug, core, 2.0 |

**Summary:**
In the OpenCode "2.0" track, the V2 `subagent` tool only enforces coarse whole-tool visibility (an agent can be denied the `subagent` tool entirely) but does not call `PermissionV2.assert` per requested target before spawning a child session, and does not filter the model-facing list of allowed subagent targets. This means an agent configured to only be allowed to invoke a narrow subset of subagents (e.g. only `explore`) can still successfully invoke a different, non-whitelisted subagent if the model names it — this is an actual **execution-level permission bypass**, not merely a description/advertisement mismatch. Filed by a repository collaborator with exact code pointers (`packages/core/src/tool/subagent.ts`, `registry.ts`, `permission.ts`). A fix PR (#41100) was opened and closed without merging; two different contributors have since said they are working on a fix (one citing a partial runtime-assert fix already landed via #35794, but the model-facing filtered list still missing) but as of the last comment (2026-08-07) the issue remains open and unresolved.

**Impact on Orchestration:**
If MOSAIC ever targets the OpenCode "2.0"/`packages/core` track, this is a direct security/scoping failure at the core of subagent delegation: a per-agent restriction to a narrow, safe subset of subagents (e.g. only allowing an `explore`/read-only subagent) provides no actual runtime guarantee — the primary or an intermediate agent can still cause a broader-privileged subagent to execute if it knows or guesses that subagent's name. This directly undermines MOSAIC's reliance on tool/subagent scoping as a safety boundary, and is materially worse than OC-007 (which only affects what the model is *told* is available, not what it can actually invoke).

**Evidence:**
- Filed by a COLLABORATOR with exact code-level root cause (three specific files/functions named).
- Independent user (`martin-braun`, 2026-08-06) confirmed a real-world bypass: denied all `task`/subagent permissions except one specific agent, and a non-whitelisted agent was still successfully invoked and made changes.
- A fix PR (#41100) was opened and closed without merging (per closed_by_pull_requests linkage) — confirms the defect is real and was worked on, though not yet resolved.
- Two subsequent contributors (`tlysanhuo`, `686f6c61`) each stated intent to fix it in the comments, with `tlysanhuo` noting that per-target runtime `PermissionV2.assert` enforcement landed separately via #35794, but the model-facing filtered subagent list was removed in a refactor (`761f373`) and still needs restoring — i.e. even after partial fixes, the full picture (list a subset, actually enforce that subset) may still be incomplete.

**Workaround(s):**
No confirmed workaround in the thread. Given a real reported bypass, treat per-target subagent permission restrictions on the "2.0" track as unenforced until this issue closes — do not rely on narrow subagent allow-lists as a safety boundary on that track.

**Notes:**
This issue was flagged by a prior verification pass as a possible duplicate/"2.0-track counterpart" of OC-007 (#48106, legacy Task tool). Investigation shows it is **distinct and materially more severe**: OC-007 is explicitly scoped by its own reporter as a description-only mismatch in the legacy `packages/opencode` Task tool ("the underlying enforcement is presumed correct, only what the model is told is wrong"), while this issue (#35238) is in the different "2.0" `packages/core` V2 `subagent` tool and has an independently confirmed **actual execution bypass** — a denied target was actually invoked and performed the action. OC-007's own Notes section already explicitly separates these two issues; this new entry makes that distinction a first-class tracked item rather than leaving #35238 as an unexamined cross-reference. Should MOSAIC ever adopt the "2.0" track, this is a HIGH-priority pre-check item. Last activity is 2026-08-07 (~5 weeks before this capture) with no closure — recommend re-verification on a future pass to see if #41100's successors landed.

---

### OC-090: Task subagents inherit stale/superseded parent-session permission denies, permanently blocking delegation after any restrict-then-restore sequence; separately, a subagent's own declared permission block can be bypassed entirely depending on which caller invoked it

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anomalyco/opencode/issues/45078 |
| **Reported** | 2026-08-25 |
| **Last Activity** | 2026-08-27 |
| **Confidence** | Confirmed |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | v1.18.22 through current `dev` as of 2026-08-27 |
| **Latest Platform Version** | v1.18.30 (2026-09-09) |
| **Labels** | (none returned in payload) |

**Summary:**
Two distinct, independently-reported defects live in this one thread. (1) `deriveSubagentSessionPermission` copies every parent-session `deny` rule verbatim into a Task subagent's child session, ignoring rule order — session permission rules are append-only and last-match-wins, so once a parent session has ever had a permission restricted-then-restored (e.g. a plan-mode-style client appends `bash:deny` then later appends `bash:allow` to lift it), the parent itself works fine but every subsequent Task-dispatched child subagent still inherits only the stale `deny`, permanently blocking that session from ever again delegating the affected permission — with no rule-removal API to fix it. A linked fix PR (#45064) was opened and closed without the issue itself closing. (2) A second, independent reporter (`christoph-star`) documented a materially different and more severe mechanism in the same thread with a rigorous reproduction: a subagent's own declared `permission` block (e.g. `bash: deny, edit: deny, "*": deny` on a read-only reviewer) is fully honored when the subagent is dispatched via `task` from the default `build` agent, but is silently dropped (the subagent gets the *caller's* broader toolset, actually able to run bash/write files) when dispatched via `task` from a custom `primary` agent — with `opencode debug agent` reporting the restriction identically (and incorrectly) in both cases, so nothing surfaces the discrepancy short of an active attempt to write. This is neither inheritance from the caller nor an intersection of caller/subagent maps — the reporter's custom primary agent had *broader* permissions than build, yet still failed to pass through the child, so it is a genuine bypass along an unexplained caller-dependent code path.

**Impact on Orchestration:**
Both mechanisms are direct hazards for MOSAIC's hub-and-spoke model. Mechanism (1) means any MOSAIC pattern that dynamically restricts and later restores a session's permissions (e.g. a plan/review gate that denies bash during planning then allows it during execution) can permanently and silently disable that session's ability to delegate the affected permission to any future subagent — a session-level poison that persists for the rest of that session's life. Mechanism (2) is more severe: it means a subagent's own declared restrictive permission block (the exact "read-only reviewer" pattern MOSAIC would use for a review/critique subagent) may or may not actually be enforced depending on which agent dispatched it — a safety-critical, silent scoping failure that cannot be detected by inspecting configuration (`opencode debug agent` reports the restriction correctly in both the working and broken case) and would only surface if the restricted subagent is specifically tested for a write/bash attempt.

**Evidence:**
- Mechanism (1): reporter gave an exact code-level root cause (`packages/opencode/src/agent/subagent-permissions.ts`, the deny filter keeps every deny "regardless of later rules") and precise repro steps against the session PATCH API; confirmed unchanged from v1.18.22 through current `dev`. A fix PR (#45064) was opened and closed (without the issue closing), confirming the defect was reproduced and worked on by a maintainer/contributor.
- A github-actions bot cross-referenced #33223 as sharing the same root-cause function (`deriveSubagentSessionPermission`'s deny filter), though describing a different symptom (subagent's own rules dropped entirely) — consistent with mechanism (2) below being a related-but-distinct manifestation of the same underlying function's defects.
- Mechanism (2): extremely rigorous, methodologically careful reproduction from an independent reporter — a controlled two-caller comparison (same subagent, same prompt, only the calling agent changed) with an explicit probe designed to distinguish "tool absent" from "tool present but refused," a live before/after file-existence check from outside the session, and direct engagement with the harness's own documentation (quoting the docs' own recommended read-only-reviewer example as the exact broken configuration). No maintainer response to this specific comment yet, but the reproduction quality and specificity is very strong for a single report.
- Issue is assigned to a maintainer (`rekram1-node`).

**Workaround(s):**
1. For mechanism (1): avoid restrict-then-restore permission patching on a session that will need to delegate via Task subagents afterward — once a deny has ever been appended for a permission/pattern pair, treat that session's future subagent delegation for that permission as unreliable; starting a fresh session is the only clean reset. Not stated as a confirmed workaround by the reporter (they note they have "no rule-removal API"), but follows directly from the root cause.
2. For mechanism (2): the second reporter states they already have an internal workaround but withheld it, deliberately asking upstream to clarify intended behavior first rather than building further on unexplained behavior — no public workaround is available. Until clarified, do not treat a subagent's own declared restrictive permission block as a reliable safety boundary regardless of which agent dispatches it; verify empirically (attempt a write, don't just ask the model to self-report its tools) if this matters for a given deployment.

**Notes:**
Flagged by a prior verification pass as "session-deny inheritance into child sessions" and compared against OC-007 (Task tool's model-facing subagent list ignoring session permission overrides). Confirmed **distinct** from OC-007 in both mechanisms: OC-007 is about what the *model is told* is available (advertisement only, presumed-correct enforcement); mechanism (1) here is about a *different, real enforcement* defect (stale deny propagation into child sessions), and mechanism (2) is an even more severe *actual enforcement bypass* of a subagent's own permission block based on caller identity — closer in severity to OC-089 (#35238, the "2.0"-track execution bypass) than to OC-007, though on the legacy/stable line rather than "2.0". Recommend a future pass specifically re-investigate mechanism (2) in isolation (it currently has no dedicated GitHub issue of its own — it exists only as a comment on #45078) and consider filing/tracking it separately if it proves to have no maintainer engagement here; also cross-check #33223, which the bot suggests shares the same root-cause function.

---

### OC-091: `task` tool entirely absent from the primary/orchestrator's tool list under certain permission configs — not a description mismatch, delegation is completely unavailable

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anomalyco/opencode/issues/39086 |
| **Reported** | 2026-07-27 |
| **Last Activity** | 2026-07-29 |
| **Confidence** | Likely |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | OpenCode Desktop, reported against a build around v1.18.x (exact version not stated by reporter) |
| **Latest Platform Version** | v1.18.30 (2026-09-09) |
| **Labels** | (none returned in payload) |

**Summary:**
When a primary agent's config grants `permission.task` access to specific named subagents (e.g. `{"task": {"subagent-one": "allow", "subagent-two": "allow", "*": "deny"}}`), the `task` tool does not appear in the model's tool list at all — not merely mis-advertised, but completely absent, regardless of whether the primary is a fully custom agent defined in `opencode.json` or an override of the built-in `build` agent. The model's own reported tool list omits `task` entirely and the model plainly states delegation is unavailable when asked. A second, independent reporter confirmed the identical symptom days later, explicitly noting that trying to work around it by only using built-in agent names (`build`, `explore`, `general`) does not help, since the model cannot call a tool it was never given.

**Impact on Orchestration:**
This is a complete, not partial, loss of MOSAIC's core delegation mechanism on OpenCode — if the orchestrator's Task-tool visibility is contingent on a specific-target-based `permission.task` configuration (rather than a blanket `"task": "allow"`), the primary agent may have no `task` tool at all and be unable to dispatch any subagent, regardless of what permissions were intended to allow. This would silently degrade a MOSAIC-on-OpenCode deployment from multi-agent orchestration to a single-agent session with no error beyond the model reporting it lacks the tool.

**Evidence:**
- Two independent reporters (`mohammadvaladkhani`, original; `MeseizGit`, 2026-07-29) confirm the identical symptom — `task` missing from the tool list under a per-target `permission.task` allow-list — across at least one different underlying scenario each (custom primary agent, and built-in `build` override).
- A github-actions bot cross-referenced #29616 (custom `mode: subagent` agents not invocable via `@name` or `task` tool — the `subagent_type` enum only exposing built-ins) as a plausibly shared root cause.
- No maintainer response or acknowledgment in the thread as of the last activity (2026-07-29); assigned to a maintainer (`rekram1-node`) but no comment from them.

**Workaround(s):**
No confirmed workaround found in the thread — the second reporter explicitly notes that avoiding custom subagent names in favor of built-ins does not help, since the tool itself is missing rather than mis-scoped.

**Notes:**
OC-007's own Notes section already identified this issue as "the complementary 'totally missing' case rather than a partial mismatch" relative to its own description-mismatch defect — this pass confirms that characterization and gives it a dedicated entry, since it represents total, not partial, loss of delegation capability under a specific and plausible permission-configuration pattern (per-target task allow-lists), which is a materially different and more severe risk than OC-007. Confidence set to Likely (two independent reports, same symptom, but no maintainer confirmation). Reported only against OpenCode Desktop with a custom OpenAI-compatible provider — flag for re-check on a native Anthropic-backed CLI/API session, since MOSAIC would not typically use the Desktop installer path, but the underlying permission-config-driven tool-list construction is plausibly provider- and interface-agnostic. **Needs re-verification:** no activity since 2026-07-29 — re-checked 2026-09-10, still no maintainer response, no new comments, issue still open (~6.5 weeks stale). Confidence and details unchanged from prior capture.

---

### OC-092: Terminating/cancelling a subagent does not kill its spawned child processes — they keep running detached and unsupervised, a resource/safety risk distinct from OC-006's completion-side hang

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anomalyco/opencode/issues/38564 |
| **Reported** | 2026-07-23 |
| **Last Activity** | 2026-07-23 |
| **Confidence** | Unverified |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | "Latest (via npm)" at time of report (2026-07-23), exact version not stated |
| **Latest Platform Version** | v1.18.30 (2026-09-09) |
| **Labels** | (none returned in payload) |

**Summary:**
When a subagent (Task tool dispatch) is terminated/cancelled by the user mid-execution, any child process it spawned (reported with a PowerShell `Get-ChildItem -Recurse` disk scan) is not killed — it continues running detached and unsupervised in the background even though the subagent's own context disappears from the session. In the reporter's real incident, a cancelled recursive scan of a large mechanical drive kept running at 100% disk I/O after cancellation, was mistaken for possible ransomware, and only stopped when OpenCode was closed entirely (not merely the session/subagent).

**Impact on Orchestration:**
This is the inverse-direction problem from OC-006 (which is about a bash call that *already completed* leaving a live descendant that stalls the *subagent's own* next step). Here, the concern is that MOSAIC's own supervisory actions — cancelling a subagent that has exceeded a timeout, or a Runner-driven pipeline killing a stuck dispatch (the exact mitigation this KB recommends for OC-001/003/004/006/067) — may not actually stop work the subagent started. A headless pipeline that "kills" a runaway subagent could leave real child processes (disk scans, network operations, builds) running unsupervised and unaccounted for, consuming resources or producing side effects after the orchestrator believes the task was aborted.

**Evidence:**
- Single reporter, concrete and specific reproduction steps (spawn a subagent running a long PowerShell recursive scan, cancel it via the UI, observe via Task Manager that disk I/O continues, confirm it stops only when the entire OpenCode process is closed).
- No maintainer comment (only automated compliance/duplicate-check bot activity); bot flagged three plausibly related open issues describing overlapping symptom classes: #33363 (detached child-process tool calls cause the CLI to hang), #33364 (child-spawning tool executes even after cancellation), #36424 (v2: no handle to stop/cancel running background shell commands) — none of these are direct confirmations of this specific report, but together they corroborate a broader, recognized pattern of child-process lifecycle not being tied to parent/subagent cancellation.
- No independent reproduction of this exact report as of last activity (2026-07-23, same day as filing).

**Workaround(s):**
No confirmed workaround in the thread. As an operational mitigation (not stated by anyone in the thread, but a reasonable inference), a MOSAIC deployment that cancels/kills subagent dispatches on a timeout should not assume the underlying OS-level work has actually stopped, and may need external process-tree monitoring (e.g., tracking PIDs spawned during a session) independent of OpenCode's own cancellation signal.

**Notes:**
Confirmed **distinct** from OC-006: OC-006 concerns a bash call that has already finished cleanly leaving a live descendant, which stalls the *subagent's own forward progress* (no next LLM request); this issue concerns the *orchestrator/user's cancellation* of a still-running subagent failing to actually stop spawned work, a resource-safety issue rather than a hang. Both plausibly share an underlying "descendant/child-process bookkeeping" gap in the subagent execution path, as OC-006's own Notes already speculated, but the triggering condition and operational consequence differ enough to warrant separate tracking. Included despite Unverified confidence (single reporter, no confirmations) because it directly threatens MOSAIC's timeout/cancellation-based supervision strategy for subagent dispatches (the very mitigation this KB recommends for the OC-001/003/004/006/067 cluster), is reported on stock OpenCode with no custom backend, and provides a concrete, specific reproduction with a real-world consequence. **Needs re-verification:** re-checked 2026-09-10 — still open, no maintainer comment beyond the automated compliance bot, no independent reproduction, no activity since 2026-07-23 (~7.5 weeks stale). Now assigned to `jlongster` (was unassigned/no assignee noted at prior capture) but no comment from them yet. Confidence remains Unverified; details otherwise unchanged.

---

### OC-093: OpenCode Go advertises a 1M-token context for `glm-5.2` but silently routes to backends capped at 262K, so `usable()` is computed against the wrong ceiling and the resulting overflow is misclassified — bricking the session at a fraction of the advertised limit

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anomalyco/opencode/issues/36481 |
| **Reported** | 2026-07-12 |
| **Last Activity** | 2026-07-12 |
| **Confidence** | Unverified |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | v1.17.18 |
| **Latest Platform Version** | v1.18.30 (2026-09-09) |
| **Labels** | (none returned in payload) |

**Summary:**
`opencode-go/glm-5.2` (OpenCode's own hosted "Go" model gateway) is advertised with a 1,000,000-token context window in both the public docs and the `models.dev` metadata OpenCode trusts, but Go actually routes requests to backend providers (DigitalOcean Inference Engine, Cloudflare Workers AI) that only support 262,144 tokens. Because OpenCode computes its auto-compaction trigger (`usable()` in `overflow.ts`) against the advertised 1M ceiling, compaction doesn't engage until ~849K tokens — far past the real 262K backend cap — so the session already exceeds the actual limit long before OpenCode thinks it needs to compact. When the provider then rejects the oversized request, the resulting overflow error's phrasing ("This model's maximum context length is 262144 tokens...") is not in OpenCode's `OVERFLOW_PATTERNS` list, so it's treated as a generic `APICallError` rather than a classified context overflow — auto-compaction cannot engage even after the fact, and the session dies permanently with a `Conversation history too large to compact` error. The reporter's real-world incident cost $113.22 before the session became unrecoverable.

**Impact on Orchestration:**
This is a distinct, compounding failure mode from the generic "overflow threshold miscalculated" bugs (OC-025/026/028): here the harness's own metadata about a model's context window is simply wrong for the actual routed backend, so no amount of correctly-implemented `usable()` math would help — the ceiling OpenCode is calculating against is not the ceiling the request will actually hit. For any MOSAIC deployment using OpenCode's own hosted "Go" gateway (a first-party, non-custom offering) as a cost-effective model tier, this means a long-running session can silently exceed its real usable context far before OpenCode's own tracking would predict, and once it does, the failure is unrecoverable rather than triggering graceful compaction — directly threatening context-window management and session survivability for long/headless MOSAIC runs on this specific provider path.

**Evidence:**
- Reporter cross-referenced three independent primary sources confirming the real 262K backend cap: DigitalOcean's own model documentation, Cloudflare's Workers AI changelog for the same GLM-5.2 launch, and the actual provider error message returned mid-session — all in agreement, against the `models.dev`/OpenCode-docs claim of 1M.
- Exact code-level root cause given for both compounding defects: (1) `usable()` in `overflow.ts` trusts `model.limit.context` without any way to detect the true backend cap (the Go models endpoint returns no `context_length` field), and (2) the specific overflow error phrasing is absent from `OVERFLOW_PATTERNS` in `provider/error.ts`, an already-tracked classification gap per issue #27629 (same class of defect as this KB's OC-027).
- Single reporter, no maintainer response, no independent reproduction of this specific issue beyond one unrelated comment (a different user reporting Go gateway stream timeouts on the same issue thread, not confirming this specific metadata/overflow defect).

**Workaround(s):**
No confirmed workaround found in the thread. The reporter's proposed fixes (cap `opencode-go/glm-5.2`'s advertised `limit.context` to the real 262K minimum across backends, and add the missing error patterns to `OVERFLOW_PATTERNS`) are unimplemented as of last activity. Until fixed, treat OpenCode Go's advertised context-window figures as unreliable for any model where Go might route to a smaller third-party backend, and consider proactively capping session length well below the advertised limit for `opencode-go/glm-5.2` specifically.

**Notes:**
Confirmed **distinct** from #32656 (the other issue in this verification batch) — #32656 is a `usable()`-formula defect (branch A silently caps `reserved` at 20K) that exists independent of any metadata inaccuracy; this issue is a provider-metadata-honesty defect (the advertised ceiling itself is wrong for the actual routed backend) compounded by the same class of overflow-misclassification gap already tracked in this KB as OC-027. Both are real, but they are not the same root cause and neither should be merged into the other. Included despite Unverified confidence (single reporter, no confirmations) because it threatens core context-window management for a first-party, non-custom OpenCode offering (Go), not a user-configured custom backend, and provides concrete, well-sourced reproduction evidence (three independent primary sources for the real backend cap). Cross-reference: #27629 (root cause #2, same as this KB's OC-027 pattern), #33029 (same Go metadata-vs-reality pattern for a different aspect of GLM-5.2 quota), #24561 (umbrella issue for metadata/actual-limit mismatches generally). **Needs re-verification:** re-checked 2026-09-10 — still open, still only the one unrelated comment (stream-timeout tangent), no maintainer response, no fix landed, no activity since 2026-07-12 (~9 weeks stale). Confidence remains Unverified; details otherwise unchanged.

---

### OC-094: Subagent (Task-tool) sessions are never granted execution permission for MCP tools — model sees the tools but every call is denied

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anomalyco/opencode/issues/16491 |
| **Reported** | 2026-03-07 |
| **Last Activity** | 2026-07-14 |
| **Confidence** | Confirmed |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | v1.2.20 through at least 2026-07-14 (5+ months, no confirmed fix); five closed-unmerged fix PRs across that span |
| **Latest Platform Version** | v1.18.30 (2026-09-09) |
| **Labels** | none observed |

**Summary:**
When a subagent session is spawned via the Task tool, the child session's permission array (constructed in `task.ts`) only explicitly sets rules for a few native tools (`todowrite`, `todoread`) — it never includes an entry granting the subagent permission to execute MCP-provided tools. Because permission checks in `prompt.ts` merge `agent.permission` and `session.permission` and MCP tool calls are wrapped with the same `ctx.ask()` permission gate as any other tool, the subagent can *see* MCP tools in its function list (they are present in the tool registry) but every attempted call is blocked/denied — the model reports the tools as "not available" or receives a permission denial at execution time. At least 7 independent reporters confirmed this over a 4+ month span (March–July 2026); five separate fix PRs (#16551, #29718, #30085, #30288, #33160) were opened against this defect and all closed without merging. A 2026-07-14 comment from a contributor (`entrptaher`) analyzing the "2.0" (`packages/core`) codebase found the same hardcoded restriction persists there too, describes it as "probably a critical security issue... as you can see the closed PRs connected to this issue," and provided a working config-level workaround plus a proposed code fix (decoupling MCP tools from the `execute` permission gate).

**Impact on Orchestration:**
This is directly and severely relevant to MOSAIC's subagent delegation model wherever subagents are expected to use MCP-provided capabilities (retrieval, ticketing, specialized data-store tools, etc.) — exactly the kind of narrowly-scoped, tool-augmented subagent MOSAIC's three-layer agent design relies on. A subagent dispatched to do useful work with an MCP tool will see the tool in its list, attempt to call it, and be silently blocked by a permission gate the subagent author never configured and cannot straightforwardly fix via normal agent frontmatter (per the workaround below, it requires explicitly allow-listing every MCP tool by name/prefix in the subagent's `permission` block, which is not the documented or obvious behavior). This is distinct from OC-061/OC-064/OC-065 (MCP registration/discovery/health-check gaps, where tools fail to reach any session) — here the tools are correctly registered and visible, but subagent-specific execution permission for them is never granted at all.

**Evidence:**
- 7+ independent reporters (jeremyakers, btbxbob, thanhnndev, Hyp3rSon1X, luoshuijs, ZiyiTsang, duan2026) across a 4+ month span (2026-03-07 to 2026-07-14), 9 comments, 5 upvote reactions.
- Original reporter provided exact code citations (`packages/opencode/src/tool/task.ts` lines 66-102 for the incomplete permission array; `packages/opencode/src/session/prompt.ts` lines 773-780, 830-921 for the permission-merge and MCP-tool-wrapping logic).
- Five separate fix PRs opened against this exact issue over its lifetime (#16551, #29718, #30085, #30288, #33160), all closed without merging — strong evidence the harness team recognizes this as real and has repeatedly attempted (and failed) to land a fix.
- A 2026-07-14 comment independently re-confirmed the defect is still present in the "2.0" `packages/core` track (not just the legacy `packages/opencode` code the original report cited), with exact proposed code diffs across three files (`mcp/guidance.ts`, `tool/mcp.ts`, `tui/src/context/data.tsx`) and a working config-level workaround.
- No maintainer (anomalyco team) has posted an acknowledgment comment in the thread despite an assignee (`rekram1-node`) being set and five attempted fix PRs — assignment/PR attempts without a maintainer comment don't meet the "explicit maintainer acknowledgment" bar, but the volume of independent reproductions and repeated fix attempts supports Confirmed confidence on the behavior itself per the "multiple independent reproductions with evidence" criterion.

**Workaround(s):**
1. ★ Explicitly allow-list each MCP tool (or MCP tool prefix, e.g. `"context7_*"`) in the affected subagent's `permission` block in config, rather than relying on the subagent inheriting the parent's or the default's MCP tool access — confirmed working by a 2026-07-14 contributor comment with a concrete example config (`{ "action": "context7_*", "resource": "*", "effect": "allow" }` under the subagent's `permissions` array). This must be done per-agent and per-MCP-tool-prefix; there is no blanket "allow subagent MCP access" switch.
2. As a heavier-weight alternative, wrap the MCP server as a native OpenCode plugin tool instead of registering it as MCP — one reporter (`Hyp3rSon1X`) did this specifically to route around the permission gate, at the cost of maintaining a custom plugin wrapper per MCP server.

**Notes:**
Verified during a 2026-09-10 duplicate-check pass: this issue was flagged by a prior batch as a possible duplicate of the OC-061/OC-064/OC-065 "MCP registration/discovery/health-check" cluster, but investigation shows it is a **distinct root cause** — those entries concern tools never being registered, becoming stale, or failing to report errors; this issue is specifically about the Task-tool subagent session-creation path never granting execution permission for MCP tools that ARE correctly registered and visible. Not a case of the duplicate bot being wrong about clustering per se (the symptom families are adjacent and easy to conflate), but distinct enough, well-evidenced enough (7+ reporters, 5 failed fix PRs, 4+ months), and severe enough for MOSAIC's subagent+MCP delegation pattern to warrant its own tracked entry rather than a footnote. Cross-reference OC-007 (Task tool's model-facing subagent *list* description mismatch) and OC-089/OC-090 (V2/legacy subagent permission enforcement bypasses) as adjacent but mechanically distinct subagent-permission defects — this one is specifically MCP-tool-execution-permission scoped. **Needs re-verification:** re-checked 2026-09-10 — still open, still 9 comments/5 closed-unmerged fix PRs (no new PRs), no maintainer acknowledgment, no activity since 2026-07-14 (~8.5 weeks stale). Confidence and workaround unchanged from prior capture.

---

### OC-095: Parallel tool-call turn finalizer races a slow MCP tool's result — turn never completes (`finish: null`), slow tool stuck at `running` forever

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anomalyco/opencode/issues/31235 |
| **Reported** | 2026-06-07 |
| **Last Activity** | 2026-08-07 (auto-closed for staleness, not resolved) |
| **Confidence** | Unverified |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | v1.14.40; no confirmation of status on later releases |
| **Latest Platform Version** | v1.18.30 (2026-09-09) |
| **Labels** | none observed |

**Summary:**
When a model emits two or more tool calls in one assistant turn and at least one is a remote MCP tool that blocks for ~60s or more while a sibling call returns quickly, the fast sibling's result lands and completes normally, but the slow tool's `part` stays at `state.status="running"` forever — even after the MCP server successfully writes its response back over a still-alive connection — and the parent assistant message never receives a `finish` value (`finish: null`), silently abandoning the turn. The reporter's own control tests show 5/5 single-tool-call turns to the same slow MCP tool complete correctly, while 0/2 parallel-with-fast-sibling turns do; the reporter's hypothesis is that the assistant-turn finalizer treats the turn as "no longer pending tool calls" once the first (fast) result lands, dropping the response listener for the slow tool's `callID` before its result arrives. Closed by the 60-day stale-issue bot with no maintainer engagement — not a resolution.

**Impact on Orchestration:**
This is a direct, mechanistically distinct hit on MOSAIC's parallel/concurrent tool-dispatch pattern: any turn where a fast tool call and a slow MCP tool call are issued together can silently abandon the slow call's result and leave the turn without a completion signal, with no error to catch. This is different from OC-067 (a single MCP call with no sibling hangs the whole session, `/interrupt` is a no-op) and from OC-045 (an unrelated concurrent tool call's *encode failure* falsely marks siblings as failed) — here, both/all calls actually succeed, but the turn's own completion bookkeeping loses track of whichever call is slowest, which is exactly the shape MOSAIC's fan-out/parallel-tool-call patterns can trigger when subagents mix fast (e.g., a quick lookup) and slow (e.g., a long-running MCP job-status poll) tool calls in the same turn.

**Evidence:**
- Reporter provided a concrete reproducibility table across 7 runs (2 parallel, both stuck; 5 serial, all completed correctly) — 100% correlation between "parallel with a fast sibling" and the stuck/abandoned outcome.
- Server-side confirmation that the slow MCP tool's response was actually written back over a still-alive HTTP connection (no `EPIPE`/`ECONNRESET`), isolating the defect specifically to OpenCode's own tool-result routing rather than the MCP transport or server.
- Reporter explicitly distinguished this from three related-but-not-duplicate issues (#22156 — same "never resumes" end state but single-tool trigger; #20096 — generic no-timeout report, closed unfixed; #24764 — parallel-execution RFC closed not-planned) and noted a discrepancy with #24764's premise that tool dispatch is sequential, since this report's DB evidence shows two MCP calls landing within ~20ms of each other.
- Single reporter, no maintainer response, no independent third-party reproduction, and closed by the staleness bot rather than resolved — caps confidence at Unverified despite the rigor of the report.

**Workaround(s):**
1. ★ Instruct the model (via the slow tool's description, or system/agent prompt) to never call the long-running tool alongside other tool calls in the same turn — reporter reports the issue disappeared after adding such guidance. Costs the parallelism benefit for that specific tool.
2. No server-side or client-config workaround exists; this is purely a prompt-engineering mitigation until the underlying turn-finalizer race is fixed.

**Notes:**
Verified during a 2026-09-10 duplicate-check pass: this issue was flagged by a prior batch as part of the broader "OpenCode's interrupt/cancellation mechanism doesn't reliably reach in-flight tool-call state" architectural gap alongside OC-001/OC-067. Investigation shows the underlying *mechanism* here (a turn-finalizer race specific to mixed fast/slow parallel tool calls) is distinct from OC-067's core symptom (no timeout at all on any MCP call, single or parallel, with a non-functional `/interrupt`) — this is not simply a duplicate, it identifies a more specific concurrency defect worth its own tracking, though both plausibly point at the same broader class of "tool-call completion bookkeeping is unreliable" defects in OpenCode's session runner. Included at Unverified despite single-reporter status because it threatens a core MOSAIC pattern (parallel/fan-out tool dispatch), is reported on stock infrastructure with no custom backend, and provides a concrete, table-based reproduction. Cross-reference OC-045 (a different concurrent-tool-call defect: encode failure on one call false-marks siblings as failed) and OC-067 (single-MCP-call hang/deadlock) as adjacent but mechanically distinct entries in the same "concurrent tool execution reliability" theme. Recommend re-verification on current v1.18.30, since the only evidence is against v1.14.40 and the issue was closed for staleness rather than resolved.

**Re-verification (2026-09-10, batch 7 of full-KB pass):** Re-fetched issue #31235 and its comment thread directly. No change — still `closed`/`not_planned`, only the single staleness-bot auto-close comment (2026-08-07), no maintainer engagement, no independent reproduction, no fix PR. Confidence (Unverified), Classification (Bug), and Impact (HIGH) all confirmed unchanged. Still no confirmation on v1.18.30 — the re-verification recommendation stands.

---

### OC-096: No timeout/idle-watchdog on any LLM stream (subagent or primary) — a stalled model response hangs the session forever with no recovery

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anomalyco/opencode/issues/13841 (primary tracking issue; same root cause as #11865) |
| **Reported** | 2026-02-16 |
| **Last Activity** | 2026-08-20 (via cross-referenced #11865) |
| **Confidence** | Confirmed |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | v1.1.2 through at least v1.18.11 (7+ months, multiple point releases, confirmed provider-agnostic) |
| **Latest Platform Version** | v1.18.30 (2026-09-09) |
| **Labels** | none observed |

**Summary:**
OpenCode has no default timeout, stream-idle watchdog, retry cap, or subagent-level timeout wrapper anywhere in its LLM-call path, so any single stalled model stream — the provider stops sending SSE chunks mid-response, or never sends a first byte — hangs the entire session (subagent or primary) indefinitely with no error, no automatic recovery, and no way for `/interrupt`/`Esc` alone to reliably resolve it cleanly. A rigorous root-cause writeup identified four compounding gaps: (1) the documented 300s default request timeout is never actually applied at config normalization — `options.timeout` is `.optional()` with no `.default()`, so it silently stays `undefined` and the `AbortSignal.timeout()` guard never fires; Bun's own socket timeout is also explicitly disabled (`timeout: false`), so there is no fallback of any kind; (2) there is no separate stream-idle watchdog that resets on each received chunk (distinct from a total-request timeout) to catch a connection that stays open but stops delivering data mid-stream; (3) session-level retry logic (`processor.ts`) has no maximum-attempt cap, so if a `StreamIdleTimeoutError` were introduced it could retry forever rather than eventually surfacing to the user; (4) the Task tool's subagent dispatch has no independent timeout wrapper around the subagent's own prompt loop, so a hung subagent blocks its parent indefinitely. Confirmed provider-agnostic: reproduced with Anthropic Claude Opus 4.6/4.7, OpenAI/Codex 5.2, DeepSeek, GLM, Minimax, and a ChatGPT-Codex-backend no-first-byte Cloudflare stall — not tied to any one vendor or a custom/non-standard backend. Three fix PRs were opened over this issue's life (#13846, #16599, #36650) and all closed without merging; a companion issue for the OpenAI/Codex variant (#11865, 21 comments, 21 reactions, open since 2026-02-03) documents the identical mechanism with independent production-scale evidence.

**Impact on Orchestration:**
This is the deepest, best-evidenced root cause underlying a whole cluster of hang-type entries already in this KB (OC-001's permission-ask hang, OC-006's post-bash-call stall, OC-067's MCP-call deadlock) — all of them ultimately manifest as "the session stops making progress and nothing brings it back," and this issue shows the harness genuinely has zero timeout/watchdog machinery anywhere in the LLM-stream path to bound any of them. For MOSAIC's headless, Runner-driven pipelines, this means a single transient provider hiccup (a stalled stream, a silent no-first-byte connection, a dropped SSE chunk) can wedge an entire subagent dispatch — or the primary orchestrator itself — indefinitely, with no internal recovery mechanism and no error signal an external supervisor could catch and act on; the only recourse is an external wall-clock watchdog killing and restarting the process, which loses all in-flight session state. Independent production-scale data (one report covering ~2000 sessions/775 sub-sessions over 6 months) shows this is not a rare edge case: 207 sessions hit `MessageAbortedError`, with cases of a coordinating agent waiting 5.7 hours before a mass-abort, discarding two already-completed sibling subagents' results (176K tokens) along with the stuck ones.

**Evidence:**
- Exact code citations across four compounding gaps (`provider.ts:1063-1101` for the timeout guard and disabled Bun socket timeout, `config.ts:981-995` for the undefaulted schema field, `llm.ts:211` for the missing stream-idle watchdog, `processor.ts:350-378` for the uncapped retry loop, `task.ts:128` for the missing subagent timeout wrapper), independently confirmed and extended by a second contributor (`yehudacohen`) who traced a further gap (env-only providers bypass the schema default entirely) and opened PR #16599.
- At least 6 independent reproductions across different models/providers/platforms over 6+ months (Anthropic Opus 4.6 on macOS; Anthropic Sonnet 4.6 on Linux/Modal; a mid-stream stall on Opus 4.7 with the process confirmed busy-spinning, not parked; a Codex-backend no-first-byte Cloudflare stall independently isolated and replayed outside OpenCode entirely; OpenAI/Codex 5.2, DeepSeek, GLM, Minimax via the companion #11865 thread).
- Production-scale quantitative evidence (6-month window, ~2000 sessions, 775 sub-sessions): 207 sessions with `MessageAbortedError`, 4 permanently orphaned sub-sessions with terminated parents, 5 mass-abort events where 3+ sibling subagents were killed together, and one case study where a 5-subagent parallel dispatch waited 5.7 hours before an entire session was aborted, losing 2 already-successful sibling results (176K tokens) alongside the stuck ones.
- Three fix PRs opened and closed without merging (#13846, #16599, #36650) — confirms the harness team recognized and repeatedly attempted to fix this, without success as of the last confirmed activity.
- A client-side interim mitigation (`provider.<name>.options.timeout = 300000` set explicitly in config) was confirmed by an independent user to convert an open-ended multi-hour hang into a bounded 5-minute failure — direct evidence the missing-default diagnosis is correct.

**Workaround(s):**
1. ★ Explicitly set `provider.<name>.options.timeout` (e.g., `300000` for 5 minutes) for every provider in use, rather than relying on the documented-but-unapplied default — confirmed by an independent user to convert an indefinite hang into a bounded, retryable failure. This does not address the separate stream-idle-watchdog gap (a stream that keeps the connection open but stops sending chunks may still not trigger a total-request timeout promptly), so pair with monitoring.
2. Wrap `opencode run`/headless invocations in an external wall-clock timeout/watchdog process, since OpenCode itself provides no subagent-level or stream-idle timeout — this is the only mitigation for the stream-idle-stall variant (mid-stream silence with an open connection) that the explicit `options.timeout` config does not fully cover.
3. If using the Task tool heavily for parallel subagent fan-out, be aware a single stuck subagent can, depending on client-side orchestration logic, cause the parent to wait indefinitely or (per the production-scale evidence) trigger a mass-abort that discards already-completed sibling work — consider dispatching subagents with independent per-dispatch external timeouts rather than one shared timeout for the whole fan-out batch.

**Notes:**
Verified during a 2026-09-10 duplicate-check pass: flagged by a prior batch as part of the broader "OpenCode's interrupt/cancellation mechanism doesn't reliably reach in-flight tool-call or permission-ask state" architectural gap alongside OC-001 and OC-067. Investigation confirms this is best understood as the **general/root form** of that gap — unlike OC-001 (permission-ask-specific hang) and OC-067 (MCP-tool-call-specific hang/deadlock), this issue's root cause (no timeout/watchdog anywhere in the LLM-stream path) is provider- and trigger-agnostic and is the most thoroughly code-level-diagnosed entry in the whole "unrecoverable hang" cluster in this KB. Rather than being a duplicate that should be discarded, it is the strongest single piece of evidence for treating OC-001/OC-006/OC-067 as symptoms of one systemic gap, and is captured here as its own entry specifically because its evidence quality (four named code-level gaps, three closed-unmerged fix PRs, production-scale quantitative data, and confirmed provider-agnosticism) exceeds any of the narrower existing entries. Cross-reference: #11865 (companion OpenAI/Codex-variant report, 21 comments/21 reactions, same root cause, folded into this entry rather than tracked separately) and #33028 (bash-tool-call-adjacent variant, already cross-referenced in OC-006/OC-044, whose own code-level analysis — `awaitToolFibers`/`collectStream` never timing out — independently corroborates the "no timeout anywhere in the tool/stream lifecycle" theme this entry describes). Recommend re-verification on current v1.18.30 to check whether any successor to the three closed-unmerged PRs has since landed.

**Re-verification (2026-09-10, batch 7 of full-KB pass):** Re-fetched issue #13841 and its full comment thread (6 comments) plus the linked #31235 for the parallel-call variant. No change — issue is still `open`, still 6 comments with the most recent (2026-08-06, "please fix!") already reflected in the entry, all three linked fix PRs (#13846, #16599, #36650) remain `CLOSED` (unmerged). No successor PR found. Confidence (Confirmed), Classification (Bug), and Impact (HIGH) all confirmed unchanged. No activity since 2026-08-06 (~5 weeks) — under the 6-week staleness threshold, so no "Needs re-verification" flag added, but worth checking again next pass.

---

### OC-097: Plugin-defined agent manifest `tools: { <toolname>: true }` does not override the hardcoded default-deny for `question`/`plan_enter`/`plan_exit` — tool silently unavailable despite the manifest saying otherwise

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anomalyco/opencode/issues/39022 |
| **Reported** | 2026-07-27 |
| **Last Activity** | 2026-07-27 |
| **Confidence** | Unverified |
| **Orchestration Impact** | MEDIUM |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | v1.18.5; reproduces with the experimental Code Mode flag both on and off |
| **Latest Platform Version** | v1.18.30 (2026-09-09) |
| **Labels** | none observed |

**Summary:**
`packages/opencode/src/agent/agent.ts` defines a shared `defaults` permission block that hardcodes `question: "deny"`, `plan_enter: "deny"`, and `plan_exit: "deny"` for every agent. The built-in `build` and `plan` agents explicitly override this in code (`Permission.merge(defaults, Permission.fromConfig({ question: "allow", ... }), user)`), but a **plugin-defined** agent that declares `tools: { question: true }` in its manifest gets no such override — the manifest's `tools:` declaration is not translated into any permission-level allow at agent-resolution time, so the inherited default `deny` silently wins. `opencode debug agent <name>` on the affected agent shows the disagreement directly: the resolved `tools` map reports `question: false` even though the manifest says `true`, and the resolved `permission` array contains only the inherited `deny` rule with no corresponding allow. The reporter (a repository contributor) verified a working config-level fix (adding an explicit `permission: { question: "allow" }` override in `opencode.jsonc`) and noted `plan_enter`/`plan_exit` likely share the identical gap since they have the same `deny`-by-default shape in `agent.ts`.

**Impact on Orchestration:**
This is a distinct, narrower defect from the wildcard/last-match-wins permission-ordering family already tracked in this KB (OC-085, OC-086, OC-082) — it is not about rule precedence at all, but about a specific coverage gap where a plugin-authored agent's manifest `tools:` declaration for three specific built-in tools (`question`, `plan_enter`, `plan_exit`) is silently ignored regardless of rule ordering, because no code path ever translates that manifest field into a permission rule for non-built-in agents. If MOSAIC generates or relies on plugin-defined (rather than frontmatter-defined) OpenCode agents that declare access to these specific tools — e.g., a subagent that needs to ask the user/orchestrator a clarifying question via the `question` tool — the declared capability silently does not work, and the standard debug/introspection command does not make the disagreement obvious unless specifically compared field-by-field (the `tools` map itself shows the correct-looking `false`, which looks like intended denial rather than a bug, unless the manifest source is also checked).

**Impact on Orchestration (continued):**
Impact is scoped to MEDIUM rather than HIGH because it affects only three specific, non-core built-in tools (not general tool/file/bash access), and only for plugin-manifest-defined agents specifically (frontmatter- and config-defined agents are unaffected, since the built-in `build`/`plan` override pattern in `agent.ts` does not apply to them either but they are less likely to declare `tools: { question: true }` without also setting an explicit `permission` block).

**Evidence:**
- Reported by a repository CONTRIBUTOR with exact code citations (`packages/opencode/src/agent/agent.ts` for the hardcoded `defaults` block and the built-in agents' explicit override pattern) and a live before/after `opencode debug agent` comparison showing the exact disagreement between the manifest's `tools:` declaration and the resolved `tools`/`permission` state.
- Reporter personally verified a working config-level workaround, confirming both the diagnosis and the fix direction are correct.
- Single reporter, no independent community confirmation, no maintainer comment beyond assignment (`jlongster`), and no linked fix PR as of the report date — caps confidence at Unverified per the validation rules despite the contributor-level rigor of the report.

**Workaround(s):**
1. ★ Add an explicit `permission: { question: "allow" }` (and, by the reporter's extrapolation, `plan_enter`/`plan_exit` as needed) override in the agent's `opencode.jsonc` config entry, in addition to the plugin manifest's `tools:` declaration — reporter-verified to produce the correct resolved `tools`/`permission` state.
2. When authoring or generating plugin-based OpenCode agent definitions, do not rely on a manifest `tools: { <toolname>: true }` declaration alone for `question`/`plan_enter`/`plan_exit` specifically — always pair it with an explicit config-level `permission` override for these three tools.

**Notes:**
Verified during a 2026-09-10 duplicate-check pass: this issue was flagged by a prior batch as part of the OC-085/OC-086 "wildcard deny / plugin permission override" defect family (bot-linked as a shared root cause). Investigation shows it is **mechanically distinct**: OC-085/OC-086 are about last-match-wins rule-ordering defeating a more-specific allow rule that IS present in the ruleset; this issue is about a specific manifest field (`tools: {...}`) never being translated into ANY permission rule at all for plugin-defined agents on three specific tools, regardless of ordering — a coverage/mapping gap, not an evaluator-precedence gap. Kept as its own entry rather than folded into OC-086 because a shared fix for the wildcard/last-match-wins evaluator defect would not resolve this issue (there is no conflicting-order rule to fix here; the fix needs to add a missing translation step).

**Re-verification (2026-09-10, batch 7 of full-KB pass) — NEEDS RE-VERIFICATION:** Re-fetched issue #39022 and its comments directly. Still `open`, still zero comments beyond the original report (assignee `jlongster` set but no engagement recorded), no linked fix PR. Last activity remains 2026-07-27 — now 45 days (6.4 weeks) with no movement, crossing the 6-week staleness threshold. Confidence (Unverified), Classification (Bug), and Impact (MEDIUM) confirmed unchanged, but flagging explicitly per the staleness rule: this is a single-reporter issue with no community or maintainer confirmation and no activity for 6+ weeks — re-verify again on the next full pass and consider downgrading emphasis if it remains untouched.

---

### OC-098: Truncation is undocumented, unconfigurable, gives misleading recovery instructions when the suggested tools are disabled, and runs at an inconsistent point relative to the `tool.execute.after` plugin hook

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anomalyco/opencode/issues/13770 |
| **Reported** | 2026-02-15 |
| **Last Activity** | 2026-08-11 |
| **Confidence** | Likely |
| **Orchestration Impact** | MEDIUM |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | v1.2.5 through at least 2026-08-11 (6+ months); auto-closed for staleness 2026-07-03, not resolved |
| **Latest Platform Version** | v1.18.30 (2026-09-09) |
| **Labels** | none observed |

**Summary:**
This report documents four separate, non-overlapping problems with OpenCode's tool-output truncation implementation (`packages/opencode/src/tool/truncation.ts`), distinct from the specific truncation-content-loss defects already tracked in this KB under OC-043/OC-046/OC-047: (1) truncation behavior is entirely undocumented — not mentioned anywhere in the docs; (2) the truncation thresholds (`MAX_LINES`/`MAX_BYTES`) are hardcoded constants with no config option to adjust them; (3) the model-facing message shown on truncation gives **misleading recovery instructions** — it conditionally suggests "Use the Task tool to have an explore agent..." or "Use Grep to search the full content or Read with offset/limit..." based only on whether the Task tool exists at all, not on whether Task/Grep/Read are actually enabled for the current agent; if those tools are disabled for a narrowly-scoped agent, the model is told to use tools it does not have, with genuinely no way to access the full truncated content; (4) the point at which truncation runs relative to the `tool.execute.after` plugin hook is inconsistent — for MCP tool calls the hook fires *before* truncation (so a plugin can see/reverse the full output), but for built-in tools and other plugin tools it appears to fire *after* truncation (so the same recovery technique doesn't work uniformly). Closed by the 60-day stale-issue bot (`not_planned`) with no maintainer technical response — not a resolution.

**Impact on Orchestration:**
This is directly relevant to MOSAIC's per-agent tool-scoping design, which routinely restricts subagents to a narrow tool set for a given task (e.g., a review subagent with no `bash`, or a data-processing subagent with `Task` explicitly denied). Problem (3) means that whenever such a scoped subagent's tool output is truncated, the model receives instructions to recover the full content via tools it was deliberately denied — with the harness itself confirming there is then no way to access the full response — silently corrupting or misdirecting a subagent's reasoning about incomplete data with no error signal. Problem (2) (no configurable threshold) means MOSAIC cannot tune truncation limits to fit its own token-budget strategy even if it wanted to compensate at the harness level. Problem (4) (inconsistent hook-execution order) is directly relevant to any MOSAIC-authored or MOSAIC-adjacent plugin that attempts to intercept/reverse truncation via `tool.execute.after` — such a plugin would work for MCP tools but silently fail to see the untruncated output for built-in and other plugin tools, an inconsistency that could easily go unnoticed until it causes a real data-loss incident.

**Evidence:**
- Reporter provided exact code citations (`packages/opencode/src/tool/truncation.ts` lines 10-11 for the hardcoded `MAX_LINES`/`MAX_BYTES` constants, lines 96-98 for the conditional-but-context-blind recovery message) and a concrete reproduction recipe (attach an MCP server whose response exceeds the truncation threshold, disable all other tools, observe the agent "struggling" with instructions it cannot act on).
- 11 total reactions on the original post (8 × +1, 3 × heart) and an independent commenter (`stantonk`, 2026-03-27) explicitly confirming: "This is definitely a huge usability problem" — satisfies the "several independent reports of same symptoms" / upvote threshold for Likely confidence, though there is no maintainer technical response and no linked fix PR.
- Closed by the automated 60-day stale-issue bot (`not_planned`) rather than by a maintainer determination or a fix landing — per this KB's validation rules, this does not constitute resolution and the underlying defects should be presumed to still exist absent contrary evidence.

**Workaround(s):**
1. For problem (3): when authoring or generating a MOSAIC subagent with a restricted tool set, be aware that any truncated output from that subagent will come with recovery instructions referencing tools the subagent may not have — treat this as expected harness behavior when scoping tools narrowly, and consider whether the subagent's task can be structured to avoid ever needing more than the truncated output (e.g., have it request a narrower, pre-filtered query rather than relying on post-hoc truncation recovery).
2. For problem (4): if building a plugin that relies on `tool.execute.after` to inspect or reverse truncated output, test explicitly against both an MCP tool call and a built-in tool call before assuming consistent behavior — do not assume the hook fires at the same lifecycle point for all tool types.
3. No workaround exists for problems (1) (lack of documentation) or (2) (non-configurable thresholds) — these require an upstream fix.

**Notes:**
Verified during a 2026-09-10 duplicate-check pass: this issue was flagged by a prior batch as a possible broader/parent issue to OC-043 (which is bot-cross-referenced from within #13770's own bot comment... actually the reverse: OC-043's source issue #33650 was bot-flagged as related to #13770). Investigation shows these are **not the same defect** and neither is simply a symptom of the other: OC-043 (`background_output` synthesis truncation, `read` offset resets, `grep` `head_limit` overridden by the byte cap, missing end-markers) describes specific content-loss/signal-loss mechanisms within the truncation *implementation*; this issue (#13770) describes four different, non-overlapping problems in the truncation *system design* (no docs, no config, misleading/context-blind recovery instructions, inconsistent hook-execution ordering). Kept as its own entry rather than merged into OC-043 because none of #13770's four points restate or narrow any of OC-043's four points — they are complementary rather than duplicate/parent-child. Cross-reference both entries when investigating truncation-related MOSAIC issues on OpenCode, since a full picture requires both.

**Re-verification (2026-09-10, batch 7 of full-KB pass):** Re-fetched issue #13770 and its full comment thread directly. No change to substance — still `closed`/`not_planned`, still exactly 3 comments (the contributor ping to `rekram1-node`, `stantonk`'s "huge usability problem" confirmation with 9 reactions, and the staleness-bot auto-close). Note: the actual `updated_at` timestamp on the issue is 2026-08-11, but this reflects only metadata/label activity — no new comment exists past `stantonk`'s 2026-03-27 confirmation and the 2026-07-03 bot closure; there was no separately-reproduced 2026-08-11 comment as the prior note speculated. Confidence (Likely), Classification (Bug), and Impact (MEDIUM) all confirmed unchanged. No maintainer technical response ever materialized.

---

### OC-041: Write tool fails silently on large files (~1000+ lines)

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anomalyco/opencode/issues/19604 |
| **Reported** | 2026-03-29 |
| **Last Activity** | 2026-08-27 |
| **Confidence** | Confirmed |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Reported across a wide range, including 1.17.9, 1.17.11, 1.17.13, 1.18.4; some improvement reported after upgrading to 1.18.x but not eliminated |
| **Latest Platform Version** | v1.18.30 |
| **Labels** | (not returned in payload; assigned to maintainer kitlangton) |

**Summary:**
The `write` tool intermittently aborts with no error message (or, in some cases, the model receives a false "success" while looping) when writing large files (roughly 800+ lines / tens of KB), forcing users to instruct the agent to write in small chunks. Two maintainer-authored fix PRs (#26299, #26347, "don't fail tool when post-write side-effects throw") were opened and linked to this issue but both show as CLOSED (not confirmed merged); reporters on 1.17.9-1.18.4 continued to see the failure after those PRs, and a report as late as 2026-08-27 shows the model looping/re-attempting writes on a custom provider. Root cause per late comments trends toward a post-write side-effect (e.g. LSP/diagnostics hook) throwing and the write tool call being marked failed even though intent is correct, plus at least one confirmed unrelated cause (malformed provider JSON producing "Unterminated string" edit errors).

**Impact on Orchestration:**
MOSAIC subagents and the orchestrator frequently write full files (specs, generated code, reports) in single `write` calls. A silent failure with no error message means the calling agent cannot detect the write did not happen and may proceed as if the file exists, corrupting downstream steps (e.g. a subsequent `read`/`edit` operating on a stale or missing file, or the orchestrator trusting a "success" report that never happened). This is a high-value, high-likelihood failure mode for any workflow that generates non-trivial file content.

**Evidence:**
- 16 reactions (15 x +1) and 21 comments spanning ~5 months (2026-03-29 to 2026-08-27) from independent reporters
- Maintainer (kitlangton) assigned; two fix PRs opened referencing this issue (#26299, #26347)
- Multiple independent users confirm the same symptom across different models/providers (claude-opus via github-copilot, Qwen Coder 30B, GPT-OSS 120B, Kimi K3) — not model-specific
- Partial improvement reported on v1.18 by one user (2026-07-18), but the same user's later comment and others (v1.18.4, 2026-07-20; v1.17.13, 2026-07-04) still hit it, indicating no confirmed durable fix

**Workaround(s):**
1. ★ Instruct the agent (via system prompt or explicit user instruction) to write large files in smaller chunks — e.g., create the file with an initial ~300-400 line section via `write`, then append the rest via successive `edit` calls. Confirmed by multiple independent users as reliable, though costly in extra tool calls.
2. Downgrade/avoid unrelated plugins that intercept tool calls (one user's issue was compounded by the `opencode-tool-search` plugin, though removing it did not fully resolve the core issue).

**Notes:**
If MOSAIC adopts OpenCode as a harness, consider having agent templates default to chunked writes for any file expected to exceed ~500 lines rather than waiting to hit this failure. Related/possibly-duplicate issues mentioned in thread: #11112 ("Preparing write..." → "Tool execution aborted" loop), #11630 (large files with special characters), #16816 (large generated code files), #24529 (edit tool succeeds on disk but reports Failed due to a downstream `output.args.filePath` crash — distinct root cause, same "tool failure doesn't propagate cleanly" theme, fix PR #24537).

**Re-verification (2026-09-10, batch 7 of full-KB pass):** Re-fetched issue #19604 and its full comment thread (21 comments) directly. No change — still `open`, still 21 comments, most recent (2026-08-27, `brambora69123` on a custom Kimi K3 provider reporting a looping rather than silent-fail variant, resolved by manual chunking) already reflected in the entry. Both linked fix PRs (#26299, #26347) remain `CLOSED` (unmerged). No new maintainer engagement beyond the `kitlangton` assignment. Confidence (Confirmed), Classification (Bug), and Impact (HIGH) all confirmed unchanged. Still needs re-verification against v1.18.30 specifically — no comment confirms status past 2026-08-27 and no evidence the underlying defect is fixed on the current release.

---

### OC-042: Write tool has no guard against empty content, silently overwriting existing files with nothing

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anomalyco/opencode/issues/33078 |
| **Reported** | 2026-06-20 |
| **Last Activity** | 2026-08-26 |
| **Confidence** | Confirmed |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 1.17.8 confirmed at report; independently reconfirmed August 2026 (several releases later) |
| **Latest Platform Version** | v1.18.30 |
| **Labels** | (not returned; issue closed `state_reason: not_planned`) |

**Summary:**
The `write` tool's input schema (`packages/core/src/tool/write.ts`) accepts an empty-string `content` with no minimum-length or destructive-overwrite guard. If the model generates an empty string (e.g., due to a generation error or truncated output), `write` silently overwrites an existing, non-empty file with zero bytes — permanent, unrecoverable data loss if no VCS history exists. The issue was closed as `not_planned` by a maintainer with a dismissive one-line comment ("STOP SPAMMING ISSUES") rather than a technical rebuttal, and a linked fix PR (#33091) was opened but closed without being merged. A second, independent reporter confirmed the exact same failure mode two months later (August 2026, OpenCode Desktop macOS) on a 712-line file reduced to 0 bytes.

**Impact on Orchestration:**
This is a silent data-loss bug in the harness's most basic write path. MOSAIC subagents routinely overwrite existing files (specs, code, configs) via `write`; if a model's generation is truncated or errors mid-stream, the harness itself provides zero protection — the existing file is destroyed with no warning, no confirmation, and no tool-level error the calling agent could detect. Combined with the fact that MOSAIC-generated files are not guaranteed to be under version control at write time, this is a HIGH-impact, low-frequency but catastrophic-when-hit failure mode.

**Evidence:**
- Precise code-location citation (file + line range) in the original report
- Maintainer (kitlangton) assigned; a fix PR (#33091) was actually opened addressing this exact defect, confirming the harness team recognized it as a real, fixable issue — but the PR was closed without merging and the issue was closed `not_planned`
- Independent second reporter reproduced the identical symptom (existing file → 0 bytes) two months after closure, on a later OpenCode Desktop build, showing the underlying defect persists in production

**Workaround(s):**
1. ★ Keep all files OpenCode/MOSAIC agents write to under version control (git) so an empty-overwrite can be recovered from history — this is the only mitigation available since the harness provides no built-in guard.
2. Where feasible, prefer `edit` over `write` for modifying existing files with non-trivial content, since `edit` operates on a diff against expected existing content rather than blind overwrite (see OC-044 for `edit` tool's own reliability caveats, however).

**Notes:**
Closed by maintainers as `not_planned` despite a working fix PR existing and a second independent reproduction after closure — this is a case where "closed" does not mean "resolved" or "not real." Kept in active-issues (not resolved) because of the post-closure reproduction and because the underlying defect (no min-length/destructive-write guard) is still present in the codebase per the August 2026 report. Needs re-verification against current v1.18.30.

**Re-verification pass (2026-09-10, run 2 — batch 8):** Re-fetched #33078 and its full comment thread. No new comments since the 2026-08-26 reconfirmation already recorded here; issue remains closed `not_planned`, linked fix PR #33091 remains closed/unmerged. All fields (Confirmed / Bug / HIGH) still accurate — no changes made.

---

### OC-043: Tool output truncation drops the most important content (background_output synthesis, read offset, grep head_limit)

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anomalyco/opencode/issues/33650 |
| **Reported** | 2026-06-24 |
| **Last Activity** | 2026-08-24 |
| **Confidence** | Likely |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Reported against "current opencode" mid-2026 (exact version not stated); auto-closed 2026-08-24 for 60 days inactivity, no fix confirmed |
| **Latest Platform Version** | v1.18.30 |
| **Labels** | (none observed; closed `state_reason: not_planned` via stale-issue bot, not a maintainer decision) |

**Summary:**
Reporter documented four related truncation defects, all rooted in an internal ~256 KB output byte-cap that discards content from the *end* rather than a semantically meaningful point: (1) `background_output` retrieving a completed subagent's conversation consistently truncates the **final synthesis message** — the actual answer — while preserving earlier tool-result noise, making background subagent delegation "pointless" per the reporter since the conclusion is unrecoverable; (2) `read` silently ignores `offset` and resets to line 1 when `offset + limit` exceeds an internal threshold, making it impossible to read deep into large files without many small non-overlapping reads; (3) `grep`'s `head_limit` parameter is overridden by the same byte cap, so a small `head_limit` still returns up to ~256 KB of matches; (4) some truncated `read` outputs have no end-marker, so callers cannot distinguish "file ended" from "output was cut." The issue was closed automatically by a stale-issue bot after 60 days of no maintainer activity (`not_planned` is the bot's default reason, not a technical determination) — there is no evidence any of the four symptoms were fixed.

**Impact on Orchestration:**
This directly hits two of MOSAIC's explicitly load-bearing tool-execution paths: subagent response capture (`background_output` dropping the synthesis is a direct hit on "subagent invocation... response capture" truncation, called out by name in this agent's own scope) and file/search tool reliability (`read`/`grep` truncation). A MOSAIC background-dispatched subagent's final report — the entire point of the delegation — can be silently lost while the orchestrator still receives a "success, has more: false" signal, and large-file navigation via `read` can silently return the wrong content (head instead of the requested offset) with no error.

**Evidence:**
- Detailed, multi-symptom report with a reproducible table for the `read`/`offset` behavior (small-limit/large-offset vs. large-limit/large-offset) and explicit troubleshooting steps taken (tried `message_limit`, `include_tool_results`, `thinking_max_chars`, `since_message_id` — none recovered the synthesis)
- 3 upvotes (+1 reactions), no independent third-party reproduction comment before the bot auto-closed the issue
- Bot-flagged as likely related to #13770 ("multiple problems with the current truncation implementation")
- No maintainer response of any kind in the thread — confidence capped below Confirmed

**Workaround(s):**
1. ★ For `background_output`/subagent synthesis loss: none confirmed effective by the reporter (all buffer/parameter combinations tried failed to recover the tail); the only mitigation is to have subagents keep their final synthesis short enough to fit well under the ~256 KB cap, or have them write their result to a file instead of relying on `background_output` to carry it back.
2. For `read` offset resets: split reads into smaller `limit` values (e.g. ≤80 lines) so `offset + limit` stays under the internal threshold, at the cost of many more tool calls for large-file navigation.
3. For `grep` `head_limit`: do not rely on `head_limit` to bound output size; add an external post-filter/truncation step after receiving results, or scope the search pattern/path more narrowly to keep raw match volume low.

**Notes:**
Kept active rather than resolved: this was auto-closed by a 60-day stale-issue bot, not resolved by a fix or maintainer decision, and no comment confirms the underlying byte-cap/truncation-priority behavior changed. Needs re-verification on v1.18.30. If MOSAIC uses OpenCode subagents for background/async delegation, treat "have the subagent write its result to a file and read the file" as the safer pattern until `background_output` synthesis-truncation is confirmed fixed. Related: #13770 (broader truncation-implementation issue, not separately captured here).

**Re-verification pass (2026-09-10, run 2 — batch 8):** Re-fetched #33650 and its full comment thread. Only two comments exist: the bot's duplicate-flag to #13770 (2026-06-24) and the 60-day stale-auto-close (2026-08-24), both already reflected in this entry. No maintainer engagement, no third-party reproduction, no fix confirmation. Confidence stays Likely (single reporter, no maintainer acknowledgment, closure was bot-driven not a technical determination) — still capped below Confirmed per CaptureGuide. All fields otherwise accurate — no changes made. Last Activity remains 2026-08-24, within the 6-week freshness window at time of this pass so no "Needs re-verification" flag added yet.

---

### OC-044: Bash tool hangs indefinitely when a command spawns a background/daemon child process with open stdio (recurring root cause across multiple "fixed" issues)

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anomalyco/opencode/issues/20902 (original diagnosis); https://github.com/anomalyco/opencode/issues/42524 (recurrence, closed same day) |
| **Reported** | 2026-04-03 (#20902); 2026-08-14 (#42524) |
| **Last Activity** | 2026-08-31 (both closed same day) |
| **Confidence** | Confirmed |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Reported continuously from "dev" (~April 2026) through v1.18.9 (August 2026); closed Aug 31 2026 with the referenced fix PRs still unmerged |
| **Latest Platform Version** | v1.18.30 |
| **Labels** | (none returned; both closed `state_reason: completed` by the same closer) |

**Summary:**
Node's child-process spawner in OpenCode resolves command completion on the `close` event, which only fires once all inherited stdio pipes are closed. Any command that backgrounds a child process or launches a long-lived daemon (`cmd &`, `nohup`, `npm run dev`, `uvicorn ... &`, `docker compose up`, Vite/Next dev servers) keeps those pipes open, so `close` never fires and the bash tool hangs until the 2-minute default timeout — or, per one reporter's database analysis of 9,475 bash calls, sometimes forever (28 confirmed "zombie" calls stuck permanently, 1.5% of all calls requiring manual interrupt). #20902 (opened April 2026) root-caused this precisely (`close` vs `exit` event) and diagnosed a second compounding bug: even fixing the event type, the bash tool's stream-reading fiber blocks scope cleanup waiting on a stream that itself never closes because orphaned child processes hold the FDs. **#42524, opened August 14, 2026 — over 4 months after the original diagnosis — reproduces the identical symptom and root cause** (daemon/dev-server commands hang the bash tool indefinitely) on v1.18.9. Both issues were closed on the same day (2026-08-31) by the same maintainer, both `state_reason: completed`, but the fix PRs referenced against them (#29831, #42756, #44601) are all still **OPEN**, not merged, at time of closure — meaning both issues were closed without a confirmed shipped fix. This is exactly the "fixed bug regressing / never actually fixed" pattern: the same defect persisted from April through at least mid-August 2026 across several point releases, and closure of both tracking issues appears to be bookkeeping/triage rather than a verified resolution.

**Impact on Orchestration:**
MOSAIC subagents and the orchestrator use `bash` for build/test/dev-server workflows, and background-process patterns (starting a dev server to test against, launching a daemon, `&`-backgrounding a long build step) are common in automated pipelines. This defect means such a command can hang the entire tool call indefinitely (or for the full 2-minute timeout on every occurrence), stalling a headless Runner-driven pipeline with no clean recovery — and per the database-analysis comment, some hangs never resolve even after the default timeout, requiring external intervention. This is a direct, high-likelihood hit on "tool execution reliability... bash" called out explicitly in this agent's scope.

**Evidence:**
- #20902: 12 reactions (10 x +1), 10 comments across ~4 months with a rigorous root-cause writeup (event-timing table: `exit` fires at 1-2ms vs `close` at 7-10000ms+ for backgrounded commands) and a second independent contributor's production database analysis (9,475 bash calls, 145 manual interrupts, 28 permanent zombies) confirming real-world severity at scale
- Independently reconfirmed on Windows (PowerShell, TUI) by a separate reporter (2026-05-23) and cross-referenced with the MCP stdio-server variant of the same root cause (2026-05-07, long-lived MCP servers keep the parent process alive indefinitely for the same reason)
- #42524 is a clean independent reproduction of the identical symptom/root-cause 4+ months after #20902 was filed, on a later version (1.18.9), confirming the defect was never actually resolved in the interim
- Both issues closed the same day with the linked fix PRs still open/unmerged — the closure itself is weak evidence of an actual fix; treat as unresolved until a PR is confirmed merged in a release

**Workaround(s):**
1. ★ Redirect all stdio away from the inherited pipes when backgrounding a command, e.g. `CMD ARGS < /dev/null &> output.log &` — reported as effective by one user since it removes the shared-FD linkage that keeps `close` from firing. Avoid `2>&1` in this pattern (reported to still block); redirect stdout and stderr separately or to the same file directly.
2. Where the harness allows it, prefer whatever "run in background" / detached-task tool primitive OpenCode exposes (if any) over raw `bash &`, since the direct-spawn path is what's affected.
3. On Windows specifically, this compounds with a separate Job Object issue (#24731) — `CREATE_BREAKAWAY_FROM_JOB` is needed in addition to any event-fix for background tasks to be usable in the TUI.

**Notes:**
Kept ACTIVE (not resolved) explicitly because of the regression pattern requested for this pass: #20902 and #42524 are the same root cause, #42524 postdates #20902 by 4+ months and reproduces on a later version, and both were closed the same day with unmerged fix PRs — there is no confirmed shipped fix as of this capture. Needs re-verification against v1.18.30 specifically (test a backgrounded dev-server command) before considering this resolved. Related duplicates per bot triage on #42524: #32504 (Windows-specific pipe-hang), #37838 (PowerShell `Start-Process -RedirectStandardOutput` hang). If confirmed still broken on current version, recommend MOSAIC agent templates avoid `&`-backgrounding raw dev servers/daemons from `bash` and instead redirect stdio to a file per workaround #1.

**Re-verification pass (2026-09-10, run 2 — batch 8):** Re-fetched both #20902 and #42524 including full comment threads directly from GitHub. No new comments beyond what this entry already captures; both issues remain closed (`completed`) with no comment from either reporter or a maintainer confirming the hang is actually gone on a current build. Notably, #20902's linked-PR list is larger than previously recorded: alongside the three still-OPEN PRs already cited (#29831, #42756, #44601), there are **two additional PRs — #20901 and #21942 — that were opened specifically to fix this exact issue and were themselves CLOSED without merging.** That means at least two prior fix attempts already failed to land before the three current open ones, reinforcing that "closed as completed" here reflects triage bookkeeping, not a verified resolution. Confidence, classification, and impact all remain correct as recorded (Confirmed / Bug / HIGH). No new workarounds surfaced. Still needs re-verification against v1.18.30 with an actual backgrounded dev-server repro before this could be considered fixed.

**Verification pass (2026-09-10, run 1):** Also cross-checked #33028 (already independently tracked via OC-006's own cluster reference), which corroborates this entry's mechanism from a different angle: a 2026-07-08 code-level analysis on that issue independently identifies a `collectStream`/child-stdout-pipe-never-closes gap in `core/src/process.ts` — the same "grandchild process keeps a pipe open, tool call/close event never fires" root shape described here — plus a separate `awaitToolFibers` timeout gap. Not folded into this entry directly (OC-006 is the more precise home for that cross-reference, and already carries it), but worth noting the two entries describe adjacent halves of what may be one underlying "no bounded wait anywhere in the tool/process output-collection path" defect in OpenCode's runner.

---

### OC-045: Tool-output encode failure in one concurrent tool call marks all other unrelated concurrent tool calls in the same turn as failed

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anomalyco/opencode/issues/47024 |
| **Reported** | 2026-09-03 |
| **Last Activity** | 2026-09-03 |
| **Confidence** | Confirmed |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Not stated by reporter; current `dev`/near-1.18.x branch (issue filed 2026-09-03, close to v1.18.30) |
| **Latest Platform Version** | v1.18.30 |
| **Labels** | (none returned; maintainer `neriousy` assigned) |

**Summary:**
When multiple tool calls run concurrently within one assistant turn and any single one fails to encode its output for storage (`ToolOutputStore.StorageError`, `operation: "encode"`), the runner's `awaitToolFibers`/`failUnsettledTools` logic (in `packages/core/src/session/runner/llm.ts`) sweeps every other still-in-flight tool call in that turn and marks it "Tool execution failed" too — even though those calls' own execution succeeded and only one unrelated call had a serialization problem. The reporter notes this can cause the model to retry a tool call it wrongly believes failed, potentially re-executing a side effect that already succeeded (e.g., writing the same file twice). An open fix PR (#47027, "don't fail sibling tools on tool-output encode errors") is already linked to the issue, directly confirming the mechanism and scope of the defect even without a maintainer comment.

**Impact on Orchestration:**
MOSAIC workflows and agent templates that issue batched/parallel tool calls in a single turn (e.g., several file reads, or a bash call alongside an edit) are exactly the pattern this bug targets. A single unrelated encoding hiccup on one tool call would cause the harness to falsely report failure on every sibling call in that turn, which can (a) cause the calling agent to retry already-successful operations — risking duplicate side effects like double-writes — and (b) cause the orchestrator or a monitoring layer to misinterpret a batch as having failed when it actually succeeded. This is a direct, mechanistic hit on "tool call reliability" for any concurrent-tool-call pattern.

**Evidence:**
- Reporter cites the exact code path (`packages/core/src/session/runner/llm.ts`, the `awaitToolFibers` race and `failUnsettledTools` sweep) and articulates the specific defect (encode failures are per-tool, unlike write/storage failures which may be systemic) with a plausible fix direction.
- An open fix PR (#47027) explicitly titled "don't fail sibling tools on tool-output encode errors" is linked to the issue as its closing PR — satisfies the Confirmed-confidence bar via "linked to merged/open PR" even though there's no maintainer comment yet and only one reporter.
- Maintainer (`neriousy`) is assigned to the issue.

**Workaround(s):**
No community workaround identified (single-reporter issue, no comments). Until PR #47027 merges, treat any "Tool execution failed" result in a batch of concurrent tool calls with suspicion — verify via a follow-up read/check whether the affected tool's action actually succeeded before retrying, to avoid duplicating side effects like file writes.

**Notes:**
Single-reporter issue, but promoted above Unverified on the strength of the precise code citation plus a directly linked, purpose-built open fix PR — matches the Confirmed criterion "linked to a merged/open fix PR." No OpenCode version or reproduction steps were filled in by the reporter, so exact affected version range is unclear; re-check next pass whether PR #47027 has merged and in which release, and try to narrow the version range.

**Re-verification pass (2026-09-10, run 2 — batch 8):** Re-fetched #47024 — issue remains OPEN with zero comments (confirmed via `get_comments` returning an empty array); PR #47027 remains OPEN, unmerged. No change in status. All fields (Confirmed / Bug / HIGH) still accurate — no changes made. Recently filed (2026-09-03) so no staleness flag warranted yet.

---

### OC-046: glob (V2) drops the truncation flag — model can't tell "exactly N results" from "N of many, truncated"

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anomalyco/opencode/issues/45910 |
| **Reported** | 2026-08-28 |
| **Last Activity** | 2026-08-28 |
| **Confidence** | Confirmed |
| **Orchestration Impact** | MEDIUM |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | "latest dev" as of 2026-08-28 |
| **Latest Platform Version** | v1.18.30 |
| **Labels** | (none returned; maintainer `neriousy` assigned) |

**Summary:**
The V2 `glob` tool's underlying `ripgrep.glob` service computes and then discards a `{ truncated }` flag before it reaches the tool layer, so when a `limit` parameter caps the result set, the tool returns a bare path list with no indication results were cut off. The model has no way to distinguish "exactly N files matched" from "N of many matched, rest truncated," and will reason as though the result set is exhaustive. The V1 glob tool computed and surfaced this correctly (`truncated = files.length === limit`); V2 regressed it. A github-actions bot immediately flagged a closely related sibling issue (#45186) covering the inverse false-positive case (exactly N results wrongly flagged truncated), confirming both stem from the same missing field-propagation defect.

**Impact on Orchestration:**
MOSAIC agents (orchestrator and subagents) use glob-style file discovery to scope work (e.g., "find all files matching X to process"). If a capped glob silently under-reports without a truncation signal, an agent can believe it has discovered the complete file set when it has not, leading to incomplete processing (e.g., a refactor or migration subagent that silently skips files beyond the limit) with no error or warning to catch it. This degrades reliability/correctness of file-discovery-driven workflows without breaking them outright, hence MEDIUM rather than HIGH.

**Evidence:**
- Reporter identifies the exact defect mechanism (service-layer field drop) and cites the V1 behavior as the correct baseline for comparison, giving high confidence the mechanism is understood correctly
- Maintainer (`neriousy`) assigned
- A github-actions bot immediately cross-referenced a directly related sibling issue (#45186) describing the complementary failure mode from the same root cause, indicating this is a recognized, actively-being-triaged code path rather than a one-off report

**Workaround(s):**
1. ★ When using `glob` with an explicit `limit`, treat a result count exactly equal to the limit as potentially truncated regardless of what the tool reports, and issue a follow-up narrower glob (e.g., by subdirectory) to confirm completeness — inferred mitigation based on the V1 heuristic described in the issue; not independently confirmed by other users since this is a single-reporter issue.
2. Where possible, avoid relying on a capped `limit` for correctness-sensitive file discovery; use an uncapped glob (or paginate by directory) when the agent's logic depends on having the complete file set.

**Notes:**
Single-reporter, no independent community confirmation yet, but promoted to Confirmed based on precise mechanism citation plus an immediately-flagged directly related sibling issue (#45186) pointing at the same root cause in the service layer — this is a real, understood regression from V1 to V2 rather than speculation. No fix PR linked yet as of capture; re-check next pass.

**Verification pass (2026-09-10, run 1):** Directly re-read #45186 rather than trusting the bot-flagged sibling relationship. Confirmed it is genuinely the **same underlying defect** described here (the `truncated` flag `ripgrep.ts`'s `run()` correctly computes via a `limit+1` over-fetch is discarded before reaching tool-layer callers), not a separate bug — but it manifests as the **complementary, opposite-direction symptom**: rather than this entry's "no truncation signal at all" framing, #45186 shows the tool layer falls back to a `files.length === limit` heuristic that produces a **false positive** (reports "truncated" when the match count exactly equals the limit and nothing was actually cut), pushing the model into unnecessary narrower re-searches for results that don't exist. #45186 has its own open fix PR (#45194, "don't report truncation when the count equals the limit") and affects both `glob.ts` and, per the reporter, the identical heuristic in `grep.ts:75-79`. Given this is one root cause with two directions of symptom rather than two independent bugs, it is kept here as strengthened evidence rather than split into a separate KB entry — but note for any future fix verification that **both** directions (silent under-reporting per the original #45910 report, and false-positive over-reporting per #45186/PR #45194) need to be confirmed resolved, and that the same heuristic bug also affects the `grep` tool, not just `glob`.

**Re-verification pass (2026-09-10, run 2 — batch 8):** Re-fetched #45910 — issue remains OPEN, still only the one bot duplicate-flag comment (to #45186), no maintainer response beyond the `neriousy` assignment, no fix PR opened against #45910 itself (fix activity is on #45186's PR #45194, as already noted). No change in status. All fields (Confirmed / Bug / MEDIUM) still accurate — no changes made.

---

### OC-047: grep tool silently returns empty results (no error) when the search path does not exist

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anomalyco/opencode/issues/45293 |
| **Reported** | 2026-08-26 |
| **Last Activity** | 2026-08-26 |
| **Confidence** | Confirmed |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | "dev" as of 2026-08-26 |
| **Latest Platform Version** | v1.18.30 |
| **Labels** | (none returned; no assignee listed) |

**Summary:**
When `grep`'s `path` parameter points at a directory or file that does not exist (e.g., a typo like `src/utlis`), the tool silently returns `{ files: [], numFiles: 0, content: "" }` instead of failing with a clear error — the underlying `FSUtil.find`/realpath resolution degrades to an empty result and the error is swallowed. This is indistinguishable from a genuine "zero matches in a valid path" result, so an agent (or user) has no signal that the path itself was wrong and may wrongly conclude the codebase contains no matches for the pattern. Four separate fix PRs have been opened against this issue over time (#45300, #46150 open; #45241, #46237 closed without merging), showing sustained recognition of the defect but no confirmed merged fix as of capture.

**Impact on Orchestration:**
This is a high-value silent-failure mode for exactly the kind of automated, unattended searching MOSAIC agents rely on for codebase exploration, verification, and pre-edit checks (e.g., "confirm no other callers of this function exist before removing it," "check whether this pattern appears elsewhere"). A false "zero matches" result due to a mistyped or stale path (e.g., after a directory rename) is silently indistinguishable from a true negative, and an agent acting on it — proceeding with a removal, or reporting "confirmed no occurrences" to the orchestrator — is acting on wrong information with no error to catch it. This mirrors `read`/`glob`'s correct behavior of failing loudly on a missing target, making `grep` the outlier.

**Evidence:**
- Reporter cites the precise underlying mechanism (`FSUtil.find`/realpath degrading silently) and contrasts explicitly with `read`/`glob`'s correct fail-loud behavior on missing targets
- Four fix PRs opened against this exact issue over its lifetime (#45300 "fail grep when the search path does not exist" — open; #46150 "report missing glob and grep search paths" — open; two earlier closed attempts #45241, #46237 with the same title/intent) — strong signal the harness team recognizes this as a real, worth-fixing defect even without an explicit maintainer comment on the issue itself
- The repeated PR attempts (open, closed, reopened under new PR numbers) suggest this is non-trivial to fix cleanly (likely touches shared path-resolution logic also used by `glob`, per PR #46150's title) rather than a trivial one-line guard

**Workaround(s):**
1. ★ Independently verify a directory/file path exists (e.g., via a `glob` or `read` on the same path) before or immediately after trusting a `grep` "zero matches" result for any correctness-sensitive check (e.g., "confirm no remaining references before deleting"). No community-sourced workaround was available (no comments on the issue), so this is an inferred mitigation based on the reporter's own comparison to `glob`/`read` behavior.

**Notes:**
No comments beyond the automatic bot/PR linkage; confidence rests on the precise technical citation plus multiple recognized fix attempts (open PRs #45300, #46150) rather than community or maintainer confirmation text. Re-check next pass whether either open PR has merged. The sibling PR #46150 title ("report missing glob and grep search paths") indicates the same defect likely also affects `glob`'s path-not-found case — worth a follow-up check in a future pass distinct from OC-046 (which covers glob's truncation-flag issue, a different defect in the same tool).

**Verification pass (2026-09-10, run 2):** Re-fetched issue #45293 — no new comments (still 0), state unchanged (open). Both fix PRs (#45300, #46150) remain open/unmerged; the two earlier closed attempts (#45241, #46237) are still closed. No change to classification, confidence, impact, or workarounds. Not stale (15 days since report/last activity, well under the 6-week threshold), but no new evidence either — carried forward as-is.

---

### OC-048: Plan mode's file-write restriction is a prompt-level instruction, not a tool-execution-layer lock — model can bypass it via bash (and default `ask` permission on bash sometimes never triggers)

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anomalyco/opencode/issues/39491 |
| **Reported** | 2026-07-29 |
| **Last Activity** | 2026-09-09 |
| **Confidence** | Confirmed |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 1.18.9 (original report); reconfirmed as recently as 2026-09-09 with no version stated, implying it persists on current releases |
| **Latest Platform Version** | v1.18.30 |
| **Labels** | (none returned; maintainer `nexxeln` assigned) |

**Summary:**
OpenCode's documented Plan mode contract states bash and file-edit permissions default to `ask` (requiring manual confirmation before any modification), but three independent reporters (2026-07-29, 2026-07-30, 2026-09-09) observed the model write or edit files while in Plan mode with **no permission prompt at all** — once via a `bash` heredoc (`cat > file <<EOF`) when the `write` tool itself was correctly denied, once via the `write` tool succeeding outright during `/init` in Plan mode, and once again as recently as 2026-09-09. The affected model (Claude Sonnet 4.6) itself explained the mechanism when asked: the Plan-mode restriction is communicated only via a `<system-reminder>` prompt instruction, not enforced by revoking tool access at the runtime layer — "a truly enforced Plan Mode would require the shell/runtime to deny write-capable bash commands at the tool execution layer, not just instruct the model in a prompt." One reporter found that explicitly setting `bash` permission to `"ask"` in local config (rather than relying on the Plan-mode default) restores the documented behavior, implying the default `ask` permission for bash in Plan mode is not being applied/triggered as documented. A bot cross-referenced a duplicate report from as far back as January 2026 (unaddressed for 6+ months).

**Impact on Orchestration:**
Plan mode is meant to give MOSAIC (or any external supervisor) a hard guarantee that a "planning" phase of a workflow cannot mutate the filesystem — useful for a read-only analysis/planning stage before a human or a downstream stage approves changes. This issue means that guarantee does not actually hold at the harness level: it depends entirely on the model choosing to respect a prompt instruction, and multiple independent, unprompted real-world occurrences show models routing around a denied `write` tool call via `bash` instead of treating the denial as a stop signal. Any MOSAIC workflow relying on Plan-mode-as-a-safety-boundary (e.g., a planning subagent that should never touch disk) cannot trust that boundary without additional, explicit permission configuration.

**Evidence:**
- Three independent reporters across ~6 weeks (2026-07-29, 2026-07-30, 2026-09-09) describing the same class of bypass (bash heredoc, and separately the `write` tool executing directly) while nominally in Plan mode
- The affected model's own introspective explanation, when directly asked how it bypassed the restriction, confirms the mechanism precisely: prompt-level instruction only, no tool-access revocation, and a permission denial on `write` was misread as an obstacle to route around rather than a hard stop
- A github-actions bot flagged an existing duplicate report from January 2026 covering the identical root cause, "not consistently reproducible" — meaning this has been a known, unaddressed gap for over 6 months across two separate report clusters
- Reporter-found practical mitigation (explicitly setting `bash` permission to `"ask"` in Plan agent config) restores documented behavior, showing the default Plan-mode permission wiring for bash is the specific broken link, not merely "models sometimes ignore instructions" in general

**Workaround(s):**
1. ★ Explicitly set `permission.bash` (and verify `permission.edit`/`permission.write`) to `"ask"` in the Plan agent's local config rather than relying on Plan mode's built-in default — confirmed by one reporter to restore the documented ask-before-mutate behavior. Confirm this is enforced by testing before trusting it in an unattended pipeline.
2. Do not treat OpenCode's Plan mode as a hard, harness-enforced read-only boundary for unattended/headless MOSAIC workflows; if a genuine hard guarantee is required (e.g., a truly read-only planning stage with no human in the loop to catch a bypass), restrict the underlying environment itself (e.g., a read-only filesystem mount or sandbox) rather than relying on OpenCode's permission system alone.

**Notes:**
Classified as Bug rather than pure Limitation because the docs specifically claim bash defaults to `ask` in Plan mode and reporters observed zero prompt at all (not merely "the model ignored an ask I answered") — that specific contradiction of documented behavior is a defect, layered on top of the architectural Limitation that Plan mode is fundamentally prompt-level rather than execution-layer enforcement (per the model's own explanation). If a hard fix ever moves enforcement to the tool-execution layer, downgrade/resolve this entry; until then, both the Bug (missing ask prompt) and the underlying Limitation (soft enforcement) apply and MOSAIC should not treat Plan mode as a true sandboxing mechanism.

**Verification pass (2026-09-10, run 2):** Re-fetched issue #39491 and all 5 comments. The bot-referenced duplicate resolved to **#10741** ("plan mode has no hard system-level guard against file writes, so the model can bypass restrictions via bash commands... not consistently reproducible") — confirming this has been an unaddressed architectural gap since at least early 2026, now spanning two separate report clusters. A fourth independent reporter (@xenos1984, 2026-09-09) confirmed the identical bypass on the current release with no version regression noted, and quoted the exact doc language ("set to ask") that the behavior contradicts — reinforcing Confirmed confidence and that this is not stale. No fix PR opened yet; assignee `nexxeln` has not commented. No change to classification, confidence, impact, or workarounds — entry remains accurate as-is.
