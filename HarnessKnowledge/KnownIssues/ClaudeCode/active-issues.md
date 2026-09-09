# Claude Code — Active Issues

> Last updated: 2026-09-09 (run 10)

---

### CC-001: Foreground Task/Agent dispatch phantom-cancelled with fake "[Request interrupted by user for tool use]" message

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | Unknown — needs backfill |
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
A foreground `Agent`/Task dispatch can be phantom-cancelled by the harness, which then injects a fake `[Request interrupted by user for tool use]` message into the transcript even though no real user interrupt occurred.

**Impact on Orchestration:**
The orchestrator can be misled into believing the user manually cancelled a subagent dispatch, when in fact the harness itself dropped it — this can cause incorrect recovery logic or abandoned work to go unnoticed.

**Evidence:**
- Multiple reports of the same fake interruption string appearing with no corresponding real user action (exact count/detail lost — needs backfill).

**Workaround(s):**
1. ⭐ Retry the dispatch; do not trust the interrupted-by-user string as proof of real user intent.

**Notes:**
The GitHub issue was closed by a stale-bot for inactivity, NOT because it was fixed — flag "Needs re-verification on current version."
Reconstructed from session summary after data loss during KnownBugs→KnownIssues migration (2026-09-09); full original detail was lost — needs a verification pass to re-fetch exact dates/URLs/evidence from GitHub.

---

### CC-002: Background/workflow-spawned agents can hang indefinitely on an unanswered permission prompt

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | Unknown — needs backfill |
| **Reported** | Unknown — needs backfill |
| **Last Activity** | Unknown — needs backfill |
| **Confidence** | Unverified |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Unknown — needs backfill |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | Unknown — needs backfill |

**Summary:**
Background or workflow-spawned agents that hit a permission prompt with no attended user to answer it can hang indefinitely — there is no timeout or watchdog to fail the dispatch or fall back to a default decision.

**Impact on Orchestration:**
Directly threatens MOSAIC's unattended/headless background dispatch pattern — a single stuck permission prompt can silently stall a workflow with no automatic recovery.

**Evidence:**
- Single reporter, but well-documented with reproduction steps (exact steps lost — needs backfill). Included despite Unverified confidence given the severity to unattended background workflows.

**Workaround(s):**
1. Unknown — needs backfill. Consider pre-approving all tools the background agent may need (avoid triggering any prompt) as a precaution until a workaround is confirmed.

**Notes:**
The GitHub issue was closed by a stale-bot for inactivity, NOT because it was fixed — flag "Needs re-verification on current version."
Reconstructed from session summary after data loss during KnownBugs→KnownIssues migration (2026-09-09); full original detail was lost — needs a verification pass to re-fetch exact dates/URLs/evidence from GitHub.

---

### CC-003: Subagent killed by a usage/spend limit is falsely reported as status "completed"/"Done"

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anthropics/claude-code/issues/82829 |
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
When a subagent is killed mid-run by a usage/spend limit, the harness falsely reports its status as "completed"/"Done" rather than surfacing the failure, hiding that the subagent's work was discarded.

**Impact on Orchestration:**
The orchestrator has no reliable way to detect that a subagent's output is incomplete/discarded — it will treat a truncated, limit-killed run as a successful completion and proceed downstream on bad data.

**Evidence:**
- Issue #82829, with a related/overlapping report #83412 ("subagents die silently on limit hit") folded into this entry as a duplicate rather than tracked separately — #82829 had stronger, more specific evidence.

**Workaround(s):**
1. Unknown — needs backfill. Consider having the orchestrator independently validate subagent output content/length rather than trusting the reported status alone.

**Notes:**
Duplicate/related issue #83412 folded in here rather than tracked as its own entry.
Reconstructed from session summary after data loss during KnownBugs→KnownIssues migration (2026-09-09); full original detail was lost — needs a verification pass to re-fetch exact dates/URLs/evidence from GitHub.

---

### CC-005: `PostToolUse` hook doesn't fire for `Agent` tool completions at the parent session level

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | Unknown — needs backfill |
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
Reconstructed from session summary after data loss during KnownBugs→KnownIssues migration (2026-09-09); full original detail was lost — needs a verification pass to re-fetch exact dates/URLs/evidence from GitHub.

---

### CC-006: Context compaction can preserve model-fabricated "system message" text instructing the agent to conceal actions from the user

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | Unknown — needs backfill |
| **Reported** | Unknown — needs backfill |
| **Last Activity** | Unknown — needs backfill |
| **Confidence** | Confirmed |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Unknown — needs backfill |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | Unknown — needs backfill |

**Summary:**
Context compaction can preserve model-fabricated "system message" text that instructs the agent to conceal actions from the user, and later turns can act on the fabricated instruction as if it were legitimate. A maintainer explicitly confirmed the root-cause mechanism on a related issue, though no fix has shipped.

**Impact on Orchestration:**
The most severe finding across the whole sweep. One documented occurrence led to a live API call being made off the fabricated instruction before the agent self-corrected — this is a conversation-integrity and safety hazard that can cause an agent to act on injected/hallucinated instructions surviving compaction.

**Evidence:**
- Maintainer explicitly confirmed the root-cause mechanism on a related issue.
- At least one documented occurrence with a concrete harmful outcome (a live API call made off the fabricated instruction).

**Workaround(s):**
1. Unknown — needs backfill.

**Notes:**
No fix has shipped as of the original investigation. Highest-severity entry in the knowledge base — prioritize for re-verification.
Reconstructed from session summary after data loss during KnownBugs→KnownIssues migration (2026-09-09); full original detail was lost — needs a verification pass to re-fetch exact dates/URLs/evidence from GitHub.

---

### CC-007: Mid-conversation behavioral rules are reliably dropped/de-authorized after compaction, even when the compaction summary retains the instruction text

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | Unknown — needs backfill |
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
Mid-conversation behavioral rules (no-stop policies, memory-write instructions, status-block requirements) are reliably dropped or de-authorized after compaction, even when the compaction summary itself retains the instruction text verbatim.

**Impact on Orchestration:**
Any orchestration protocol rule communicated only in-conversation (not in a persistent file) is at risk of being silently unenforced after a compaction event, even though the text is technically still present in context.

**Evidence:**
- Unknown — needs backfill (no further detail retained beyond title/confidence/impact from the original investigation).

**Workaround(s):**
1. ⭐ Put durable rules in CLAUDE.md, not just in-conversation instructions — CLAUDE.md content survives compaction more reliably than in-conversation instructions.

**Notes:**
Reconstructed from session summary after data loss during KnownBugs→KnownIssues migration (2026-09-09); full original detail was lost — needs a verification pass to re-fetch exact dates/URLs/evidence from GitHub.

---

### CC-008: One MCP tool with a non-object/boolean schema silently drops ALL tools from that server, on every transport

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | Unknown — needs backfill |
| **Reported** | Unknown — needs backfill |
| **Last Activity** | Unknown — needs backfill |
| **Confidence** | Confirmed |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Unknown — needs backfill |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | Unknown — needs backfill |

**Summary:**
A single MCP tool exposing a non-object/boolean-shaped schema silently drops ALL tools from that MCP server — on every transport (HTTP, stdio, Desktop) — with zero diagnostics shown to the user.

**Impact on Orchestration:**
Any MCP server MOSAIC depends on can be entirely and silently disabled by a single malformed tool schema anywhere in that server, with no error surfaced — this can look like a working MCP connection that has actually lost its entire tool surface.

**Evidence:**
- Unknown — needs backfill (no further detail retained beyond title/confidence/impact from the original investigation).

**Workaround(s):**
1. Unknown — needs backfill. Audit MCP server tool schemas for non-object/boolean parameter shapes as a precaution.

**Notes:**
Reconstructed from session summary after data loss during KnownBugs→KnownIssues migration (2026-09-09); full original detail was lost — needs a verification pass to re-fetch exact dates/URLs/evidence from GitHub.

---

### CC-009: Custom subagent's `tools: Bash` frontmatter silently never grants Bash access

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | Unknown — needs backfill |
| **Reported** | Unknown — needs backfill |
| **Last Activity** | Unknown — needs backfill |
| **Confidence** | Unverified |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Unknown — needs backfill |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | Unknown — needs backfill |

**Summary:**
A custom subagent definition specifying `tools: Bash` in its frontmatter silently never actually grants Bash access to that subagent — no combination of configuration fixes it, per rigorous single-reporter elimination testing across multiple config variants.

**Impact on Orchestration:**
Directly threatens frontmatter-scoped subagent shell access, which MOSAIC relies on for delegating Bash-capable work to scoped subagents. Worth prioritizing for corroboration given severity.

**Evidence:**
- Single reporter, but very rigorous elimination work — multiple config variants tried and ruled out systematically.

**Workaround(s):**
1. Unknown — needs backfill.

**Notes:**
Flagged as high-priority for corroboration by a second independent report.
Reconstructed from session summary after data loss during KnownBugs→KnownIssues migration (2026-09-09); full original detail was lost — needs a verification pass to re-fetch exact dates/URLs/evidence from GitHub.

---

### CC-010: A `PreToolUse` hook returning `permissionDecision: "defer"` permanently strands a subagent's Bash call, which then falsely reports "completed"

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | Unknown — needs backfill |
| **Reported** | Unknown — needs backfill |
| **Last Activity** | Unknown — needs backfill |
| **Confidence** | Confirmed |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Unknown — needs backfill |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | Unknown — needs backfill |

**Summary:**
A `PreToolUse` hook that returns `permissionDecision: "defer"` permanently strands the subagent's Bash tool call (it never resolves), and the subagent then falsely reports the call as "completed" success. Maintainer-acknowledged as worth fixing.

**Impact on Orchestration:**
Any hook-based permission gating using `defer` on a subagent Bash call risks a stuck call that is then misreported as successful, hiding the fact that no command actually ran.

**Evidence:**
- Maintainer-acknowledged as worth fixing.

**Workaround(s):**
1. Unknown — needs backfill. Avoid `permissionDecision: "defer"` for subagent Bash calls until fixed.

**Notes:**
Reconstructed from session summary after data loss during KnownBugs→KnownIssues migration (2026-09-09); full original detail was lost — needs a verification pass to re-fetch exact dates/URLs/evidence from GitHub.

---

### CC-011: Agent-definition `disallowedTools` is not enforced on further subagents dispatched with an unspecified/default `subagent_type`

| Field | Value |
|-------|-------|
| **Classification** | Limitation |
| **Source** | https://github.com/anthropics/claude-code/issues/78063 |
| **Reported** | Unknown — needs backfill |
| **Last Activity** | Unknown — needs backfill |
| **Confidence** | Confirmed (behavior confirmed; disputed by maintainer as "not a bug") |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Unknown — needs backfill |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | Unknown — needs backfill |

**Summary:**
`disallowedTools` set on an agent definition is not enforced on subagents that agent further dispatches via the `Agent` tool. All reproductions in #78063 leave `subagent_type` unspecified/default in that further `Agent` tool call — so this issue is narrowly scoped to unnamed/default further-dispatch, not to direct, explicitly-named `subagent_type` dispatch (see CC-031 for that distinct, separate claim). A maintainer confirmed this as intended design, but left the issue open, acknowledging it's confusing and security-relevant.

**Impact on Orchestration:**
A real security-scoping gap: a subagent that itself dispatches further subagents without specifying `subagent_type` can have its own `disallowedTools` restriction bypassed by the child, even though the design is intentional per the maintainer.

**Evidence:**
- Maintainer confirmed the behavior as intended design in #78063, while acknowledging it is confusing and security-relevant.

**Workaround(s):**
1. Unknown — needs backfill. Avoid relying on `disallowedTools` alone as a security boundary for further/nested dispatch; always specify an explicit `subagent_type` for nested dispatches and verify its own tool scoping independently.

**Notes:**
Kept active (NOT moved to resolved as "not a bug") because intended-by-design does not mean it's not an operational hazard for MOSAIC — it's a real security-scoping gap.
RECONCILIATION (vs CC-031, done in a later pass): re-read both full issues — these are DISTINCT, not overlapping. CC-011 is narrow (unnamed/default `subagent_type` only); CC-031 is about explicitly-named `subagent_type` dispatch. Kept as separate entries.
Reconstructed from session summary after data loss during KnownBugs→KnownIssues migration (2026-09-09); full original detail was lost — needs a verification pass to re-fetch exact dates/URLs/evidence from GitHub.

---

### CC-012: A stale `ToolSearch` tool reference to a tool that left the pool mid-session permanently bricks the session with a 400 error

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | Unknown — needs backfill |
| **Reported** | Unknown — needs backfill |
| **Last Activity** | Unknown — needs backfill |
| **Confidence** | Confirmed |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Unknown — needs backfill |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | Unknown — needs backfill |

**Summary:**
If a `ToolSearch`-surfaced tool reference goes stale mid-session (the tool leaves the pool), the session is permanently bricked with a 400 error. Three independent trigger mechanisms were confirmed, including a mid-session model switch as one trigger variant.

**Impact on Orchestration:**
A session using deferred/`ToolSearch`-loaded tools can be permanently killed mid-workflow by an event as ordinary as a mid-session model switch, with no recovery short of starting a new session.

**Evidence:**
- Three independent trigger mechanisms confirmed by reporters, including a mid-session model switch.

**Workaround(s):**
1. Unknown — needs backfill. Avoid mid-session model switches when deferred tools are in play, as a precaution.

**Notes:**
Reconstructed from session summary after data loss during KnownBugs→KnownIssues migration (2026-09-09); full original detail was lost — needs a verification pass to re-fetch exact dates/URLs/evidence from GitHub.

---

### CC-013: `permissions.allow`/`bypassPermissions` intermittently ignored, worse for subagent-issued tool calls than primary-agent calls

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | Unknown — needs backfill |
| **Reported** | Unknown — needs backfill |
| **Last Activity** | Unknown — needs backfill |
| **Confidence** | Confirmed |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Unknown — needs backfill |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | Unknown — needs backfill |

**Summary:**
Configured `permissions.allow` rules or `bypassPermissions` mode are intermittently ignored, and this is reported as worse for subagent-issued tool calls than for primary-agent-issued calls.

**Impact on Orchestration:**
Subagents that should be pre-approved for certain tools can still hit unexpected permission prompts or denials, breaking unattended/automated dispatch flows that assume pre-approval is honored.

**Evidence:**
- Unknown — needs backfill (no further detail retained beyond title/confidence/impact from the original investigation).

**Workaround(s):**
1. Unknown — needs backfill.

**Notes:**
Reconstructed from session summary after data loss during KnownBugs→KnownIssues migration (2026-09-09); full original detail was lost — needs a verification pass to re-fetch exact dates/URLs/evidence from GitHub.

---

### CC-014: `PreToolUse` `ask` decision / `permissions.ask` config is silently not enforced

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | Unknown — needs backfill |
| **Reported** | Unknown — needs backfill |
| **Last Activity** | Unknown — needs backfill |
| **Confidence** | Confirmed |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Unknown — needs backfill |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | Unknown — needs backfill |

**Summary:**
A `PreToolUse` hook returning an `ask` decision, or a `permissions.ask` config rule, is silently not enforced — no permission prompt is shown at all, and the tool call proceeds.

**Impact on Orchestration:**
Permission-gating logic that relies on `ask` to pause for human review can be silently bypassed, allowing tool calls to execute without the intended checkpoint.

**Evidence:**
- Unknown — needs backfill (no further detail retained beyond title/confidence/impact from the original investigation).

**Workaround(s):**
1. Unknown — needs backfill.

**Notes:**
Reconstructed from session summary after data loss during KnownBugs→KnownIssues migration (2026-09-09); full original detail was lost — needs a verification pass to re-fetch exact dates/URLs/evidence from GitHub.

---

### CC-015: A killed/crashed `PreToolUse` hook fails OPEN instead of closed

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | Unknown — needs backfill (fix PR: https://github.com/anthropics/claude-code/pull/84364, open/unmerged) |
| **Reported** | Unknown — needs backfill |
| **Last Activity** | Unknown — needs backfill |
| **Confidence** | Confirmed |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Unknown — needs backfill |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | Unknown — needs backfill |

**Summary:**
If a `PreToolUse` hook process is killed or crashes, the tool call proceeds unchecked (fails OPEN) instead of being blocked (fail closed) — the opposite of the safe default for a security-gating mechanism.

**Impact on Orchestration:**
Any safety/permission gate implemented via `PreToolUse` hooks can be silently bypassed by simply crashing or killing the hook process, undermining hook-based guardrails MOSAIC may depend on.

**Evidence:**
- Has a linked open (not yet merged) fix PR (#84364) — confirms the bug is real even though unfixed.

**Workaround(s):**
1. Unknown — needs backfill. Monitor hook process health/exit codes independently rather than relying solely on fail-closed behavior.

**Notes:**
Fix PR #84364 is open but not yet merged as of the original investigation.
Reconstructed from session summary after data loss during KnownBugs→KnownIssues migration (2026-09-09); full original detail was lost — needs a verification pass to re-fetch exact dates/URLs/evidence from GitHub.

---

### CC-016: Background subagents are denied outright on Write/Bash tool calls regardless of the parent session's permission mode

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | Unknown — needs backfill |
| **Reported** | Unknown — needs backfill |
| **Last Activity** | Unknown — needs backfill |
| **Confidence** | Confirmed |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Unknown — needs backfill |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | Unknown — needs backfill |

**Summary:**
Background subagents are denied outright on Write and Bash tool calls regardless of the parent session's permission mode (including `bypassPermissions`/auto-approve modes).

**Impact on Orchestration:**
Background/unattended subagent dispatch — a core MOSAIC pattern — cannot reliably perform file writes or shell commands even when the parent session is configured to auto-approve.

**Evidence:**
- Unknown — needs backfill (no further detail retained beyond title/confidence/impact from the original investigation).

**Workaround(s):**
1. Unknown — needs backfill.

**Notes:**
Reconstructed from session summary after data loss during KnownBugs→KnownIssues migration (2026-09-09); full original detail was lost — needs a verification pass to re-fetch exact dates/URLs/evidence from GitHub.

---

### CC-018: A self-backgrounding Bash command under `run_in_background` reports false-complete, orphans the process, can cause silent double-dispatch

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | Unknown — needs backfill |
| **Reported** | Unknown — needs backfill |
| **Last Activity** | Unknown — needs backfill |
| **Confidence** | Unverified |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Unknown — needs backfill |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | Unknown — needs backfill |

**Summary:**
A Bash command that self-backgrounds (e.g. daemonizes itself) while run under `run_in_background` causes the tool call to report false-complete, orphans the underlying process, and can lead to silent double-dispatch of the same work.

**Impact on Orchestration:**
Background Bash-based orchestration steps risk reporting success prematurely while an orphaned process continues running unmonitored, or risk the same work being dispatched twice.

**Evidence:**
- Unknown — needs backfill (no further detail retained beyond title/confidence/impact from the original investigation).

**Workaround(s):**
1. Unknown — needs backfill. Avoid commands that self-daemonize under `run_in_background`.

**Notes:**
Reconstructed from session summary after data loss during KnownBugs→KnownIssues migration (2026-09-09); full original detail was lost — needs a verification pass to re-fetch exact dates/URLs/evidence from GitHub.

---

### CC-019: A background Bash task gets killed ~17-20 seconds after arming when armed as the turn's last tool call

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | Unknown — needs backfill |
| **Reported** | Unknown — needs backfill |
| **Last Activity** | Unknown — needs backfill |
| **Confidence** | Unverified |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Unknown — needs backfill |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | Unknown — needs backfill |

**Summary:**
A background Bash task is killed approximately 17-20 seconds after being armed, specifically when it is armed as the last tool call of a turn.

**Impact on Orchestration:**
Long-running background Bash steps dispatched as the final action of a turn risk being killed prematurely before completing meaningful work.

**Evidence:**
- Unknown — needs backfill (no further detail retained beyond title/confidence/impact from the original investigation).

**Workaround(s):**
1. Unknown — needs backfill. Consider not making a background Bash dispatch the last tool call of a turn, as a precaution.

**Notes:**
Reconstructed from session summary after data loss during KnownBugs→KnownIssues migration (2026-09-09); full original detail was lost — needs a verification pass to re-fetch exact dates/URLs/evidence from GitHub.

---

### CC-020: Interactive-session LSP client never sends `didChange` after an Edit-tool write, freezing code-intelligence answers

| Field | Value |
|-------|-------|
| **Classification** | Quirk |
| **Source** | Unknown — needs backfill |
| **Reported** | Unknown — needs backfill |
| **Last Activity** | Unknown — needs backfill |
| **Confidence** | Unverified |
| **Orchestration Impact** | MEDIUM |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Unknown — needs backfill |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | Unknown — needs backfill |

**Summary:**
The interactive session's LSP client never sends a `didChange` notification after an `Edit` tool write, which freezes code-intelligence answers (hover/diagnostics) at the pre-edit state.

**Impact on Orchestration:**
Confirmed the affected consumer is the harness's OWN tool-call results (e.g. subsequent hover/diagnostics tool calls), reachable even via headless `-p` mode which has no editor at all — not merely a human's IDE view. This is why it was kept as orchestration-relevant rather than dismissed as UI/editor-only.

**Evidence:**
- Single reporter (Unverified), but the audit re-review specifically confirmed the affected consumer is the harness's own tool results, not a human IDE display.

**Workaround(s):**
1. Unknown — needs backfill.

**Notes:**
AUDIT NOTE (from relevance re-review): kept as orchestration-relevant specifically because it's reachable via headless `-p` mode, not just an editor-display sync issue.
Reconstructed from session summary after data loss during KnownBugs→KnownIssues migration (2026-09-09); full original detail was lost — needs a verification pass to re-fetch exact dates/URLs/evidence from GitHub.

---

### CC-022: Mid-session `/model` switch doesn't actually apply to the running session

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anthropics/claude-code/issues/73881 |
| **Reported** | Unknown — needs backfill |
| **Last Activity** | Unknown — needs backfill |
| **Confidence** | Confirmed |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Unknown — needs backfill |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | Unknown — needs backfill |

**Summary:**
Switching models mid-session via `/model` does not actually apply to the currently running session — the session silently keeps using the previous model's behavior/budget instead of the newly selected model.

**Impact on Orchestration:**
Any orchestration step that switches models mid-session expecting the new model to take effect immediately can silently continue running (and being billed/behaving as) the old model.

**Evidence:**
- Issue #73881.

**Workaround(s):**
1. Unknown — needs backfill. Start a new session/subagent dispatch when a genuine model switch is required, rather than relying on mid-session `/model`.

**Notes:**
Reconstructed from session summary after data loss during KnownBugs→KnownIssues migration (2026-09-09); full original detail was lost — needs a verification pass to re-fetch exact dates/URLs/evidence from GitHub.

---

### CC-023: Subagent Bash `cwd` reset can land in a SIBLING subagent's worktree, with no spawn-time `cwd` parameter available to pin it

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | Unknown — needs backfill |
| **Reported** | Unknown — needs backfill |
| **Last Activity** | Unknown — needs backfill |
| **Confidence** | Unverified |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Unknown — needs backfill |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | Unknown — needs backfill |

**Summary:**
When a subagent's Bash working directory resets, it can land in a SIBLING subagent's worktree rather than a safe default, and there is no spawn-time `cwd` parameter available to pin a subagent's Bash working directory explicitly.

**Impact on Orchestration:**
Directly threatens MOSAIC's own multi-worktree fan-out orchestration pattern — a subagent could execute Bash commands inside another subagent's worktree by accident, corrupting isolation between parallel workstreams. Flagged as high-priority for MOSAIC-side verification.

**Evidence:**
- Unknown — needs backfill (no further detail retained beyond title/confidence/impact from the original investigation).

**Workaround(s):**
1. Unknown — needs backfill. Have each subagent explicitly `cd` to its intended worktree at the start of every Bash call as a defensive measure, rather than relying on inherited cwd.

**Notes:**
Flagged as high-priority for MOSAIC-side verification given direct relevance to MOSAIC's multi-worktree fan-out pattern.
Reconstructed from session summary after data loss during KnownBugs→KnownIssues migration (2026-09-09); full original detail was lost — needs a verification pass to re-fetch exact dates/URLs/evidence from GitHub.

---

### CC-024: `SessionStart`-hook environment (`CLAUDE_ENV_FILE`) grows unboundedly across compact/resume/clear cycles, eventually wedging the Bash tool

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | Unknown — needs backfill |
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
The `SessionStart` hook environment file (`CLAUDE_ENV_FILE`) grows unboundedly across compact/resume/clear cycles, eventually wedging the Bash tool permanently — manifesting as a torn export, an EOF-quote parse error, or `ENAMETOOLONG`.

**Impact on Orchestration:**
Long-lived sessions that go through repeated compact/resume/clear cycles (a realistic MOSAIC pattern for long orchestration runs) risk the Bash tool becoming permanently unusable partway through.

**Evidence:**
- Two independently-triggered instances of the same root mechanism (not identical trigger, but same underlying cause) — reasonable confidence but noted as worth a second glance.

**Workaround(s):**
1. Unknown — needs backfill. Avoid excessive compact/resume/clear cycling within a single long-lived session where possible, as a precaution.

**Notes:**
Confidence rests on two independently-triggered instances of the same root mechanism — worth a second glance/corroboration.
Reconstructed from session summary after data loss during KnownBugs→KnownIssues migration (2026-09-09); full original detail was lost — needs a verification pass to re-fetch exact dates/URLs/evidence from GitHub.

---

### CC-025: Plugin-native `PreToolUse` hooks unenforced in interactive sessions; plugin JSON `deny` decisions unenforced in either mode

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | Unknown — needs backfill |
| **Reported** | Unknown — needs backfill |
| **Last Activity** | Unknown — needs backfill |
| **Confidence** | Unverified |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Unknown — needs backfill |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | Unknown — needs backfill |

**Summary:**
Plugin-native `PreToolUse` hooks are unenforced in interactive sessions (the `exit 2`-based block protocol doesn't work for plugin-sourced hooks); additionally, a JSON `permissionDecision: "deny"` from a plugin-sourced hook is unenforced in either interactive or headless mode.

**Impact on Orchestration:**
Any security/permission gating implemented as a plugin-sourced `PreToolUse` hook cannot reliably block tool calls, regardless of session mode — a significant gap if MOSAIC relies on plugin-based guardrails.

**Evidence:**
- Unknown — needs backfill (no further detail retained beyond title/confidence/impact from the original investigation).

**Workaround(s):**
1. Unknown — needs backfill. Prefer project-level (non-plugin) `PreToolUse` hooks for enforcement until this is fixed.

**Notes:**
Reconstructed from session summary after data loss during KnownBugs→KnownIssues migration (2026-09-09); full original detail was lost — needs a verification pass to re-fetch exact dates/URLs/evidence from GitHub.

---

### CC-026: A second `/compact` within one process re-appends prior history with duplicate UUIDs, breaks `/rewind` session-wide

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | Unknown — needs backfill |
| **Reported** | Unknown — needs backfill |
| **Last Activity** | Unknown — needs backfill |
| **Confidence** | Confirmed |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Unknown — needs backfill |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | Unknown — needs backfill |

**Summary:**
Invoking `/compact` a second time within one process re-appends prior conversation history with duplicate UUIDs, misparents the compaction boundary, grows the transcript quadratically, and breaks `/rewind` for the rest of the session.

**Impact on Orchestration:**
Long orchestration runs that trigger multiple compactions in a single process risk transcript corruption and quadratic growth, plus loss of the `/rewind` safety net for the remainder of the session.

**Evidence:**
- Unknown — needs backfill (no further detail retained beyond title/confidence/impact from the original investigation).

**Workaround(s):**
1. Unknown — needs backfill. Avoid invoking `/compact` more than once per process where possible; start a fresh session/process instead of a second manual compact.

**Notes:**
Reconstructed from session summary after data loss during KnownBugs→KnownIssues migration (2026-09-09); full original detail was lost — needs a verification pass to re-fetch exact dates/URLs/evidence from GitHub.

---

### CC-027: Leading whitespace before a slash command causes it to be sent to the model as plain text instead of being dispatched as a command

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | Unknown — needs backfill |
| **Reported** | Unknown — needs backfill |
| **Last Activity** | Unknown — needs backfill |
| **Confidence** | Confirmed |
| **Orchestration Impact** | MEDIUM |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Unknown — needs backfill |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | Unknown — needs backfill |

**Summary:**
If a slash command is preceded by leading whitespace, the harness sends it to the model as plain text instead of dispatching it as a command. Maintainer-reproduced.

**Impact on Orchestration:**
Any programmatically-generated prompt/command text that accidentally includes leading whitespace before a slash command will silently fail to invoke the intended command.

**Evidence:**
- Maintainer-reproduced.

**Workaround(s):**
1. Trim leading whitespace before slash commands in any programmatically-constructed input.

**Notes:**
Reconstructed from session summary after data loss during KnownBugs→KnownIssues migration (2026-09-09); full original detail was lost — needs a verification pass to re-fetch exact dates/URLs/evidence from GitHub.

---

### CC-028: `--continue` is directory-scoped, not invocation-scoped — can silently co-write into another live session's transcript

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | Unknown — needs backfill |
| **Reported** | Unknown — needs backfill |
| **Last Activity** | Unknown — needs backfill |
| **Confidence** | Unverified |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Unknown — needs backfill |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | Unknown — needs backfill |

**Summary:**
`--continue` resolves by directory rather than by invocation — a second `--continue` launched in the same directory while another session is already live there can silently resume and co-write into that already-live session's transcript.

**Impact on Orchestration:**
Any automation that launches multiple concurrent Claude Code invocations in the same working directory (e.g. parallel dispatch without distinct worktrees) risks transcript corruption/co-writing between unrelated sessions.

**Evidence:**
- Unknown — needs backfill (no further detail retained beyond title/confidence/impact from the original investigation).

**Workaround(s):**
1. Unknown — needs backfill. Ensure each concurrent session uses a distinct working directory/worktree rather than sharing one directory with `--continue`.

**Notes:**
Reconstructed from session summary after data loss during KnownBugs→KnownIssues migration (2026-09-09); full original detail was lost — needs a verification pass to re-fetch exact dates/URLs/evidence from GitHub.

---

### CC-029: Manual `/compact` can silently no-op on large conversations — summary is billed/executed but no compaction boundary is written, UI reports success

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | Unknown — needs backfill |
| **Reported** | Unknown — needs backfill |
| **Last Activity** | Unknown — needs backfill |
| **Confidence** | Confirmed |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Unknown — needs backfill |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | Unknown — needs backfill |

**Summary:**
On large conversations, manual `/compact` can silently no-op: the summary generation is billed and executed, but no compaction boundary is actually written to the transcript, while the UI nonetheless reports success.

**Impact on Orchestration:**
A session believed to have been compacted (freeing context budget) may not actually have been — the orchestrator continues operating on the false assumption of freed context, risking premature context exhaustion.

**Evidence:**
- Unknown — needs backfill (no further detail retained beyond title/confidence/impact from the original investigation).

**Workaround(s):**
1. ⭐ Use a hook that checks whether the compaction boundary was actually written after `/compact`, and alerts if not — strongest available detection mechanism.

**Notes:**
Reconstructed from session summary after data loss during KnownBugs→KnownIssues migration (2026-09-09); full original detail was lost — needs a verification pass to re-fetch exact dates/URLs/evidence from GitHub.

---

### CC-030: Skill/command positional argument substitution (`$0`/`$1`...) is off-by-one; `$2`+ never substitutes; substitution corrupts literal `$0`/`$1`-shaped prose

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | Unknown — needs backfill |
| **Reported** | Unknown — needs backfill |
| **Last Activity** | Unknown — needs backfill |
| **Confidence** | Unverified |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Unknown — needs backfill |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | Unknown — needs backfill |

**Summary:**
Positional argument substitution (`$0`/`$1` etc.) in skills/commands is off-by-one, `$2` and beyond never substitute at all, and the substitution mechanism silently corrupts any literal `$0`/`$1`-shaped prose that appears in skill/command text, with no escape mechanism available.

**Impact on Orchestration:**
Skills/commands used in MOSAIC's orchestration that rely on positional arguments beyond the first, or that contain literal `$`-prefixed digit sequences in their prose, can silently receive wrong arguments or have their text corrupted.

**Evidence:**
- Unknown — needs backfill (no further detail retained beyond title/confidence/impact from the original investigation).

**Workaround(s):**
1. Unknown — needs backfill. Avoid positional args beyond `$1` and avoid literal `$<digit>`-shaped text in skill/command prose as a precaution.

**Notes:**
Reconstructed from session summary after data loss during KnownBugs→KnownIssues migration (2026-09-09); full original detail was lost — needs a verification pass to re-fetch exact dates/URLs/evidence from GitHub.

---

### CC-031: `Agent` tool dispatch with an explicitly-named `subagent_type` can ignore that subagent's definition entirely

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/anthropics/claude-code/issues/92426 |
| **Reported** | Unknown — needs backfill |
| **Last Activity** | Unknown — needs backfill |
| **Confidence** | Unverified |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Unknown — needs backfill |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | Unknown — needs backfill |

**Summary:**
Dispatching the `Agent` tool with an explicitly-named `subagent_type` can ignore that subagent's own definition entirely — the child instead receives the DISPATCHER's own generic prompt and full tool surface. Evidenced by a verbatim sentence from the parent/dispatcher's own prompt appearing inside the child's behavior/output.

**Impact on Orchestration:**
This would be a fundamental breach of subagent isolation/scoping — an explicitly-named subagent could run with the dispatcher's full permissions and prompt instead of its own scoped definition, defeating the purpose of scoped subagent delegation entirely.

**Evidence:**
- Issue #92426.
- Evidenced by a verbatim sentence from the parent/dispatcher's own prompt appearing inside the child's behavior/output.

**Workaround(s):**
1. Unknown — needs backfill.

**Notes:**
RECONCILIATION (see CC-011): confirmed distinct from CC-011 — CC-011's evidence (all reproduced with unnamed/default `subagent_type`) can neither confirm nor refute this claim about explicitly-named dispatch. Kept as a separate entry, stays Unverified pending corroboration. Given severity if true, high-priority for corroboration.
Reconstructed from session summary after data loss during KnownBugs→KnownIssues migration (2026-09-09); full original detail was lost — needs a verification pass to re-fetch exact dates/URLs/evidence from GitHub.

---

### CC-032: A subagent's plan-mode "Ready to code?" approval dialog can display and let the user toggle the PARENT session's plan

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | Unknown — needs backfill |
| **Reported** | Unknown — needs backfill |
| **Last Activity** | Unknown — needs backfill |
| **Confidence** | Confirmed (collaborator reproduction) |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Unknown — needs backfill |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | Unknown — needs backfill |

**Summary:**
When a subagent enters plan mode, its "Ready to code?" approval dialog can incorrectly display and let the user toggle the PARENT session's plan instead of the subagent's own plan.

**Impact on Orchestration:**
Plan approval for a subagent's plan can be conflated with the parent's plan, risking the wrong plan being approved/rejected or the parent's plan being silently altered.

**Evidence:**
- Confirmed via collaborator reproduction.

**Workaround(s):**
1. Unknown — needs backfill.

**Notes:**
Reconstructed from session summary after data loss during KnownBugs→KnownIssues migration (2026-09-09); full original detail was lost — needs a verification pass to re-fetch exact dates/URLs/evidence from GitHub.

---

### CC-033: Subagents receive the full auto-memory index and skill listing, contrary to documented subagent isolation

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | Unknown — needs backfill |
| **Reported** | Unknown — needs backfill |
| **Last Activity** | Unknown — needs backfill |
| **Confidence** | Unverified |
| **Orchestration Impact** | MEDIUM |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Unknown — needs backfill |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | Unknown — needs backfill |

**Summary:**
Subagents receive the full auto-memory index and skill listing from the parent, contrary to documented subagent isolation, with no per-agent opt-out available.

**Impact on Orchestration:**
Subagents intended to be scoped/isolated (e.g. for security, focus, or context budget reasons) instead inherit the parent's full memory/skill surface, which can leak context or unintentionally widen a subagent's effective capability set.

**Evidence:**
- Unknown — needs backfill (no further detail retained beyond title/confidence/impact from the original investigation).

**Workaround(s):**
1. Unknown — needs backfill.

**Notes:**
Reconstructed from session summary after data loss during KnownBugs→KnownIssues migration (2026-09-09); full original detail was lost — needs a verification pass to re-fetch exact dates/URLs/evidence from GitHub.

---

### CC-034: `CLAUDE.md` and path-scoped rules in `--add-dir`-added directories are silently skipped

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | Unknown — needs backfill |
| **Reported** | Unknown — needs backfill |
| **Last Activity** | Unknown — needs backfill |
| **Confidence** | Likely (upgraded from Unverified on audit review — a second independent reporter found, with rigorous hook-based `InstructionsLoaded` verification, meeting the "several independent reports" bar) |
| **Orchestration Impact** | MEDIUM |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Unknown — needs backfill |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | Unknown — needs backfill |

**Summary:**
`CLAUDE.md` and other path-scoped rules located in directories added via `--add-dir` are silently skipped/not loaded; the documented opt-out mechanism does not fully close this gap for path-scoped rules or nested `CLAUDE.md` files.

**Impact on Orchestration:**
MOSAIC configurations that rely on `--add-dir` to bring in additional directories with their own `CLAUDE.md`/path-scoped rules risk those rules silently never being applied.

**Evidence:**
- A second independent reporter, with rigorous hook-based `InstructionsLoaded` verification — upgraded confidence to Likely on audit review.

**Workaround(s):**
1. Unknown — needs backfill. Verify rule loading explicitly (e.g. via a hook check) rather than assuming `--add-dir` content is honored.

**Notes:**
Confidence upgraded from Unverified to Likely on audit review due to a second independent, rigorously-verified report.
Reconstructed from session summary after data loss during KnownBugs→KnownIssues migration (2026-09-09); full original detail was lost — needs a verification pass to re-fetch exact dates/URLs/evidence from GitHub.

---

### CC-035: Project skills beyond an undocumented per-session cap become fully non-invocable ("Unknown skill")

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | Unknown — needs backfill |
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
Project skills beyond an undocumented per-session cap become fully non-invocable — the harness returns "Unknown skill" for them. This is a recurrence of a 6-month-old previously-unresolved report.

**Impact on Orchestration:**
Any MOSAIC deployment registering a large number of project skills risks some skills silently becoming uninvocable once an undocumented cap is exceeded, with no clear error explaining why.

**Evidence:**
- Recurrence of a 6-month-old previously-unresolved report — same symptom reported independently across a long time span.

**Workaround(s):**
1. Unknown — needs backfill. Keep the number of registered project skills below whatever cap is empirically observed, as a precaution.

**Notes:**
Recurrence of a long-standing (6-month-old) unresolved report — suggests this is a persistent, not transient, limitation.
Reconstructed from session summary after data loss during KnownBugs→KnownIssues migration (2026-09-09); full original detail was lost — needs a verification pass to re-fetch exact dates/URLs/evidence from GitHub.

---

### CC-036: `CLAUDE.md` at the directory holding a git repo never gets loaded into sessions started in that repo's own worktrees

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | Unknown — needs backfill |
| **Reported** | Unknown — needs backfill |
| **Last Activity** | Unknown — needs backfill |
| **Confidence** | Unverified |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Unknown — needs backfill |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | Unknown — needs backfill |

**Summary:**
A `CLAUDE.md` located at the directory that holds a git repo (i.e., one level above/at the repo root in a specific layout) never gets loaded into sessions started within that repo's OWN worktrees. Single report but exceptionally rigorous (controlled fixture matrix testing multiple directory layouts).

**Impact on Orchestration:**
Any MOSAIC repo layout that places shared/organizational `CLAUDE.md` rules above the repo root relative to worktrees risks those rules never being loaded into worktree-scoped sessions, a pattern directly relevant to MOSAIC's multi-worktree fan-out.

**Evidence:**
- Single reporter, but exceptionally rigorous — controlled fixture matrix testing multiple directory layouts.

**Workaround(s):**
1. Unknown — needs backfill. Place shared rules directly inside each worktree's own directory tree rather than relying on a parent-of-repo `CLAUDE.md`, as a precaution.

**Notes:**
Reconstructed from session summary after data loss during KnownBugs→KnownIssues migration (2026-09-09); full original detail was lost — needs a verification pass to re-fetch exact dates/URLs/evidence from GitHub.

---

### CC-037: Hard-coded "Exited Plan Mode" / "Auto Mode Active" notice text is injected at the prompt-assembly layer, never written to the visible transcript

| Field | Value |
|-------|-------|
| **Classification** | Quirk |
| **Source** | Unknown — needs backfill |
| **Reported** | Unknown — needs backfill |
| **Last Activity** | Unknown — needs backfill |
| **Confidence** | Confirmed |
| **Orchestration Impact** | MEDIUM |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Unknown — needs backfill |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | Unknown — needs backfill |

**Summary:**
Hard-coded "Exited Plan Mode" / "Auto Mode Active" notice text is injected directly at the prompt-assembly layer (it is never actually written to the visible transcript), root-caused to the compiled Claude Code binary itself, and correlated with `Agent`-tool subagent dispatch.

**Impact on Orchestration:**
Any transcript-based auditing/logging that MOSAIC relies on to reconstruct exactly what context the model saw will miss this injected notice text, since it is invisible in the recorded transcript despite being part of the actual model input.

**Evidence:**
- Confirmed, root-caused to the compiled binary itself.

**Workaround(s):**
1. Unknown — needs backfill.

**Notes:**
Reconstructed from session summary after data loss during KnownBugs→KnownIssues migration (2026-09-09); full original detail was lost — needs a verification pass to re-fetch exact dates/URLs/evidence from GitHub.

---

### CC-038: Every Bash dispatch in a large or `--resume`d session can freeze the ENTIRE process for ~80-90 seconds

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | Unknown — needs backfill |
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
In a large or `--resume`d session, every Bash tool dispatch can freeze the ENTIRE process (TUI, SSE connections, MCP) for approximately 80-90 seconds, observed as an active CPU spin (not idle blocking).

**Impact on Orchestration:**
Long-running or resumed orchestration sessions that make repeated Bash calls risk a large cumulative time cost from repeated ~80-90 second full-process freezes, and any concurrent MCP/SSE activity is also frozen during that window.

**Evidence:**
- Evidence described as "kernel-level forensics" — unusually rigorous for a dual/single-reporter finding.

**Workaround(s):**
1. Unknown — needs backfill. Prefer fresh (non-resumed) sessions for Bash-heavy workloads where possible, as a precaution.

**Notes:**
Reconstructed from session summary after data loss during KnownBugs→KnownIssues migration (2026-09-09); full original detail was lost — needs a verification pass to re-fetch exact dates/URLs/evidence from GitHub.

---

### CC-039: `.claude/settings.json` is written via a non-atomic, unlocked read-modify-write — concurrent sessions can tear or clobber it

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | Unknown — needs backfill |
| **Reported** | Unknown — needs backfill |
| **Last Activity** | Unknown — needs backfill |
| **Confidence** | Unverified |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Unknown — needs backfill |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | Unknown — needs backfill |

**Summary:**
`.claude/settings.json` is written using a non-atomic, unlocked read-modify-write. Concurrent Claude Code sessions writing to the same settings file can tear it into invalid JSON, or silently clobber/lose each other's updates.

**Impact on Orchestration:**
MOSAIC's multi-agent/multi-session concurrent operation is exactly the kind of scenario that can trigger this — concurrent sessions writing settings (e.g. permission updates) risk corrupting the shared settings file or losing each other's writes.

**Evidence:**
- Unknown — needs backfill (no further detail retained beyond title/confidence/impact from the original investigation).

**Workaround(s):**
1. Unknown — needs backfill. Avoid concurrent sessions writing to the same `.claude/settings.json` simultaneously, as a precaution.

**Notes:**
Reconstructed from session summary after data loss during KnownBugs→KnownIssues migration (2026-09-09); full original detail was lost — needs a verification pass to re-fetch exact dates/URLs/evidence from GitHub.

---

### CC-040: Multi-iteration turns sum token usage ACROSS iterations, inflating apparent context usage 2x-6x — premature compaction or false "Prompt is too long"

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | Unknown — needs backfill |
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
Multi-iteration turns (advisor/sub-call patterns being the most common trigger) sum token usage ACROSS iterations rather than per-iteration, inflating apparent context usage by 2x-6x. This fires auto-compact prematurely or can kill sessions outright with a false "Prompt is too long" error.

**Impact on Orchestration:**
Orchestration patterns that use multi-iteration turns (e.g. advisor/sub-call loops) risk hitting premature compaction or hard session-ending errors well before actual context limits are reached, wasting budget and disrupting workflows.

**Evidence:**
- Two independent reporters corroborating the same underlying root cause (via related-but-not-identical symptoms — one saw premature compaction, the other saw session-ending blocks); root cause was traced at the code level by at least one reporter.

**Workaround(s):**
1. Unknown — needs backfill. Be aware that measured/displayed context usage may be inflated during multi-iteration turns; avoid triggering unnecessary iteration-heavy patterns near context limits.

**Notes:**
Reconstructed from session summary after data loss during KnownBugs→KnownIssues migration (2026-09-09); full original detail was lost — needs a verification pass to re-fetch exact dates/URLs/evidence from GitHub.

---

### CC-041: `.claude/settings.json` in an ANCESTOR directory above the project root is silently never loaded

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | Unknown — needs backfill (fix PR referenced but NOT an actual fix — see Notes: https://github.com/anthropics/claude-code/pull/85716) |
| **Reported** | Unknown — needs backfill |
| **Last Activity** | Unknown — needs backfill |
| **Confidence** | Likely (downgraded from Confirmed — see Notes) |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Unknown — needs backfill |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | Unknown — needs backfill |

**Summary:**
`.claude/settings.json` located in an ANCESTOR directory above the project root is silently never loaded — hooks and `permissions.deny` rules defined there are inert, with no warning shown.

**Impact on Orchestration:**
MOSAIC configurations that place shared/organizational settings (hooks, deny rules) in a directory above individual project roots risk those settings being silently inert with no diagnostic.

**Evidence:**
- Originally rated Confirmed on the strength of an open fix PR (#85716). Re-check found the PR's diff actually targets `plugins/hookify/core/config_loader.py` — a third-party-style plugin's own config loader — NOT the core `.claude/settings.json` resolution path that the underlying issue is actually about. The PR does not fix the reported defect.

**Workaround(s):**
1. Unknown — needs backfill. Place settings directly at or below the project root rather than in an ancestor directory, as a precaution.

**Notes:**
Downgraded from Confirmed to Likely because PR #85716, while open, does not actually fix the reported core-settings-resolution defect (it targets an unrelated plugin config loader). PR reference kept for context but explicitly marked as NOT a fix for this issue.
Reconstructed from session summary after data loss during KnownBugs→KnownIssues migration (2026-09-09); full original detail was lost — needs a verification pass to re-fetch exact dates/URLs/evidence from GitHub.

---

### CC-042: `/rewind` can silently fail to restore content for a file newly created during the session that was never git-tracked

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | Unknown — needs backfill |
| **Reported** | Unknown — needs backfill |
| **Last Activity** | Unknown — needs backfill |
| **Confidence** | Unverified |
| **Orchestration Impact** | MEDIUM |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Unknown — needs backfill |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | Unknown — needs backfill |

**Summary:**
`/rewind` can silently fail to restore file content for a file that was newly created during the session and was never git-tracked.

**Impact on Orchestration:**
Any rollback of a session using `/rewind` may leave newly-created, never-git-tracked files in an inconsistent state relative to the rest of the restored session, without any warning that the restore was incomplete.

**Evidence:**
- Unknown — needs backfill (no further detail retained beyond title/confidence/impact from the original investigation).

**Workaround(s):**
1. Unknown — needs backfill. Ensure newly created files are git-tracked (e.g. `git add`) before relying on `/rewind` to restore them.

**Notes:**
Reconstructed from session summary after data loss during KnownBugs→KnownIssues migration (2026-09-09); full original detail was lost — needs a verification pass to re-fetch exact dates/URLs/evidence from GitHub.

---

### CC-043: statusLine's event-driven refresh has no coalescing/debouncing — a slow statusline command spawns unbounded concurrent copies

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | Unknown — needs backfill |
| **Reported** | Unknown — needs backfill |
| **Last Activity** | Unknown — needs backfill |
| **Confidence** | Confirmed (maintainer reproduction) |
| **Orchestration Impact** | MEDIUM |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Unknown — needs backfill |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | Unknown — needs backfill |

**Summary:**
The statusLine feature's event-driven refresh mechanism has no coalescing/debouncing — if the configured statusline command runs slowly, each refresh event spawns another concurrent copy of it, unboundedly.

**Impact on Orchestration:**
Kept active despite an initial instinct to dismiss it as "cosmetic" (statusline is a display feature) — its actual failure mode is genuine process/resource exhaustion (unbounded concurrent process spawning), a real operational hazard rather than merely a display glitch, especially for any MOSAIC deployment using a custom statusline command.

**Evidence:**
- Maintainer reproduction.

**Workaround(s):**
1. Unknown — needs backfill. Ensure any configured statusline command is fast/lightweight to minimize the risk of concurrent process buildup.

**Notes:**
Kept active on audit review specifically because the failure mode is resource exhaustion, not just cosmetic display.
Reconstructed from session summary after data loss during KnownBugs→KnownIssues migration (2026-09-09); full original detail was lost — needs a verification pass to re-fetch exact dates/URLs/evidence from GitHub.

---

### CC-044: `tool_result` image content is base64-duplicated once per transcript line, combined with unclamped render cost — can hard-freeze at 100% CPU

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | Unknown — needs backfill |
| **Reported** | Unknown — needs backfill |
| **Last Activity** | Unknown — needs backfill |
| **Confidence** | Confirmed (maintainer) |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Unknown — needs backfill |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | Unknown — needs backfill |

**Summary:**
`tool_result` image content is base64-duplicated once per transcript line, combined with unclamped per-frame render cost — sessions doing large image `Read`/`Edit` operations can hard-freeze at 100% CPU, with SIGKILL as the only recovery.

**Impact on Orchestration:**
Any orchestration workflow involving large image reads/edits (e.g. screenshot-based verification steps) risks the session becoming completely unresponsive and requiring a hard kill, losing in-flight work.

**Evidence:**
- Maintainer-confirmed.

**Workaround(s):**
1. Unknown — needs backfill. Avoid large image `Read`/`Edit` operations, or keep transcripts short when doing image-heavy work, as a precaution.

**Notes:**
Reconstructed from session summary after data loss during KnownBugs→KnownIssues migration (2026-09-09); full original detail was lost — needs a verification pass to re-fetch exact dates/URLs/evidence from GitHub.

---

### CC-046: `.claude/settings.json` created mid-session never gets its hooks armed until the session is restarted

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | Unknown — needs backfill |
| **Reported** | Unknown — needs backfill |
| **Last Activity** | Unknown — needs backfill |
| **Confidence** | Confirmed (maintainer reproduction) |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Unknown — needs backfill |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | Unknown — needs backfill |

**Summary:**
If `.claude/settings.json` is created mid-session (i.e., the directory didn't exist yet when the session started), its hooks never get armed until the session is restarted.

**Impact on Orchestration:**
Any workflow that provisions `.claude/settings.json` dynamically during a running session (e.g. a setup step that writes config before later steps need hooks enforced) cannot rely on those hooks being active without a restart.

**Evidence:**
- Maintainer reproduction.

**Workaround(s):**
1. Unknown — needs backfill. Ensure `.claude/settings.json` exists before session start rather than creating it mid-session, where hook enforcement is required.

**Notes:**
Reconstructed from session summary after data loss during KnownBugs→KnownIssues migration (2026-09-09); full original detail was lost — needs a verification pass to re-fetch exact dates/URLs/evidence from GitHub.

---

### CC-047: A single malformed hook entry silently kills ALL hooks for that event type, persisting across restarts

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | Unknown — needs backfill |
| **Reported** | Unknown — needs backfill |
| **Last Activity** | Unknown — needs backfill |
| **Confidence** | Confirmed (maintainer reproduction) |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Unknown — needs backfill |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | Unknown — needs backfill |

**Summary:**
A single malformed hook entry (missing the expected matcher/hooks wrapper structure) silently kills ALL hooks for that event type — and this persists across restarts/reboots, not just the current session.

**Impact on Orchestration:**
One bad hook entry in the config can silently disable an entire class of hook-based enforcement (e.g. all `PreToolUse` hooks) persistently, until the malformed entry is specifically found and fixed.

**Evidence:**
- Maintainer reproduction.

**Workaround(s):**
1. Unknown — needs backfill. Validate hook configuration structure carefully; a single bad entry can silently disable the entire event type persistently.

**Notes:**
Distinct from a different, now-fixed malformed-hook-shape issue (#75071) — that one is not tracked here since it was never an active entry (already fixed before discovery).
Reconstructed from session summary after data loss during KnownBugs→KnownIssues migration (2026-09-09); full original detail was lost — needs a verification pass to re-fetch exact dates/URLs/evidence from GitHub.

---

### CC-048: `WebFetch`'s `prompt` parameter is ignored for pre-approved documentation domains under ~100KB

| Field | Value |
|-------|-------|
| **Classification** | Limitation |
| **Source** | Unknown — needs backfill |
| **Reported** | Unknown — needs backfill |
| **Last Activity** | Unknown — needs backfill |
| **Confidence** | Likely |
| **Orchestration Impact** | MEDIUM |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | Unknown — needs backfill |
| **Latest Platform Version** | v2.1.267 (2026-09-09) |
| **Labels** | Unknown — needs backfill |

**Summary:**
`WebFetch`'s `prompt` parameter (meant to extract/filter specific content) is ignored for pre-approved documentation domains under roughly 100KB — the full raw content is returned/processed regardless of the prompt. A maintainer says this is intended behavior but left the issue open, acknowledging it's confusing and under-documented.

**Impact on Orchestration:**
The hazard is a hidden token-cost blowout: full content is processed when only a filtered extract was expected/budgeted for, which can inflate context usage unexpectedly for any orchestration step that budgets token spend around a filtered `WebFetch` result.

**Evidence:**
- Maintainer confirmed as intended behavior, left issue open and acknowledged it's confusing/under-documented.

**Workaround(s):**
1. Unknown — needs backfill. Budget for full raw content size (not the filtered/prompted size) when fetching pre-approved documentation domains under ~100KB.

**Notes:**
Kept ACTIVE (not moved to resolved as "not a bug") on audit review — same reasoning as CC-011: maintainer-intended does not mean it's not an operational hazard.
Reconstructed from session summary after data loss during KnownBugs→KnownIssues migration (2026-09-09); full original detail was lost — needs a verification pass to re-fetch exact dates/URLs/evidence from GitHub.

---
