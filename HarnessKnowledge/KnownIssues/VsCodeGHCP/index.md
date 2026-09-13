# VS Code GitHub Copilot — Issue Index

> Last updated: 2026-09-10 (run 3)
> Latest platform version: 1.137.0 (released 2026-09-09)

## Active Issues (13)

| ID | Title | Type | Confidence | Impact | Workaround | Reproduced | Response |
|----|-------|------|------------|--------|------------|------------|----------|
| VC-001 | Nested runSubagent calls rejected 3 levels deep even when allow-listed | Bug | Unverified | HIGH | No | No | Unevaluated |
| VC-002 | Subagent client-tool call can be mis-routed to parent chat after restart | Bug | Unverified | MEDIUM | No | No | Unevaluated |
| VC-003 | Subagent tool call can hang forever, leaving its turn (and parent's) open indefinitely | Bug | Confirmed | HIGH | Partial | No | Unevaluated |
| VC-004 | Forked Skill subagents have no correlation handle — invisible to Agent Host UI | Bug | Unverified | HIGH | Partial | No | Unevaluated |
| VC-005 | "Agents" window subagent spawn: main chat ignores custom agent's tool restrictions | Bug | Unverified | MEDIUM | No | No | Unevaluated |
| VC-008 | Terminal auto-approval fails for commands containing a bare `--` token (PowerShell) | Bug | Confirmed | HIGH | Partial | No | Unevaluated |
| VC-009 | Agent mode hallucinates completed task and derails at very high token counts | Bug | Unverified | HIGH | Partial | No | Unevaluated |
| VC-010 | `runSubagent` skips allow-list/invocation-gating validation when agentName is omitted | Bug | Unverified | HIGH | No | No | Unevaluated |
| VC-011 | Background subagent output/completion signals silently dropped once parent's turn ends | Bug | Confirmed | HIGH | Partial | No | Unevaluated |
| VC-012 | Client-tool execution races cause double invocation, empty args, or dropped results (hang) | Bug | Confirmed | HIGH | Partial | No | Unevaluated |
| VC-015 | Copilot edit tools report "Updated" success even when the underlying file save fails | Bug | Unverified | HIGH | Partial | No | Unevaluated |
| VC-016 | MCP tools silently rejected as "disabled by the user" due to a discovery-timing race (ask mode) | Bug | Likely | MEDIUM | Partial | No | Unevaluated |
| VC-017 | MCP client discards structured result payload when a long-running "task" fails | Bug | Unverified | HIGH | Partial | No | Unevaluated |

## Resolved / Retired Issues (0)

> Resolved entries older than 3 months are automatically removed.

No resolved issues currently tracked.

---

## Quick Summary

*(Rewritten run 3, after a full re-verification pass re-checked all 13 active entries against live GitHub state — issue bodies and full comment threads re-fetched for every entry. Result: zero reclassifications, zero closures, zero confidence changes. The picture below is the same landscape captured at run 2, now confirmed current as of 2026-09-10.)*

**Agent Host subagent/delegation plumbing is in active, visibly unstable development — the dominant theme (VC-001–VC-005, VC-010–VC-012), and re-verification confirms nothing has moved.** A cluster of issues, mostly filed by one contributor (RyanEwen) doing a systematic architectural deep-dive, converges on a small set of root causes in how the Claude-backed Agent Host dispatches, tracks, and closes subagent turns: nested `runSubagent` calls can be rejected past depth 2 regardless of allow-list contents (VC-001), while the *opposite* failure — allow-list and `disable-model-invocation` checks being skipped entirely when `agentName` is omitted — lets an agent spawn a clone of itself with zero enforcement (VC-010, itself a regression of a previously "fixed" 1.114/1.115 issue). A subagent's tool call can hang forever with no timeout or signal (VC-003, confirmed via scripted 6x repro, fix PR still open/unreviewed on re-check), and even when a subagent's work genuinely completes, the completion signal can be silently dropped by the host's message router once the parent's turn has ended (VC-011, confirmed, also causes silent auto-denial of `AskUserQuestion`-style prompts). Both VC-003 and VC-011 trace back to a shared architectural seam in client-tool execution — a call admitted off a display signal rather than the SDK's authoritative invocation — that independently causes double execution, empty-argument invocation, and dropped results (VC-012, confirmed, foundational). Forked `Skill` subagents have no correlation handle back to their agent id and are invisible in the UI while the primary narrates their conclusions (VC-004), and a custom agent's declared tool restrictions may not be enforced on the session nominally running as that agent (VC-005). Re-verification turned up one small new data point: a third-party commenter on the related #334631 thread (VC-004's Notes) reported general agent instability since ~1.132.0 — the cluster's first non-RyanEwen corroboration, though ambiguous about which exact sub-symptom it matches, so it did not raise VC-004's confidence. **As of this run, still none of this cluster's ~6 open fix PRs have received any maintainer (non-reporter) review or comment** — it remains entirely one contributor's self-driven investigation; treat "fix PR open" in this cluster as weaker evidence of an imminent fix than it would normally be.

**Tool execution correctness (VC-008, VC-015, VC-016, VC-017) — unchanged on re-check.** `chat.tools.terminal.autoApprove` fails to match any command containing a bare `--` token on PowerShell — a parser-level bug that can silently convert an auto-approved headless command into a blocking interactive prompt (VC-008, confirmed root cause, fix PR still open). Copilot's edit tools can report "Updated" success before the underlying file save actually resolves, so a failed save is invisible to the tool result (VC-015, concrete repro on a custom filesystem provider, materiality on standard local/remote saves still unconfirmed). MCP tool integration has two independent client-side defects: tools can be silently rejected as "disabled by the user" due to an async-discovery race in ask mode (VC-016, likely, rigorous 48-tool repro matrix), and a spec-violating client bug discards a failed long-running MCP task's structured diagnostic payload, leaving only a generic error string (VC-017).

**Conversation stability (VC-009) — unchanged on re-check.** In very long, high-token stateful Agent-mode sessions, the model can hallucinate a fictitious completed task and autonomously act on it — a severe, hard-to-detect failure mode for unattended runs. Still reproduced only with GPT-5.6 Sol via the Responses API, still single-reporter with no maintainer response; needs broader reproduction to confirm it isn't model-specific.

## Recommended Mitigations

- **Avoid multi-hop subagent delegation on this harness for now, and don't trust `agents:` allow-lists or `disable-model-invocation` as hard enforcement.** VC-001 and VC-010 pull in opposite directions on the same code path (over-enforcement and total bypass) — keep delegation to a single hop from the primary where possible, and don't grant `runSubagent`/`agents` tool access to any agent that must never self-invoke.
- **Wrap every subagent dispatch in an external wall-clock timeout, and don't equate "no completion signal" with "failed."** VC-003 and VC-011 are two distinct defects that both leave a subagent's completion invisible to the orchestrator — one because the tool call truly hung, the other because a genuinely-completed subagent's signal was dropped in transit. Where possible, cross-check the on-disk transcript (or re-open the session) before assuming failure and re-dispatching, since VC-011's work is often recoverable while VC-003's is not.
- **Treat client-tool calls on this harness as potentially non-idempotent under contention.** VC-012's shared architectural seam can cause a tool to execute twice, run with empty arguments, or have its result silently dropped — prefer idempotent tool implementations and apply the same timeout/re-dispatch discipline used for VC-003/VC-011.
- **Don't trust the Agent Host UI/transcript as the audit trail for forked Skill subagents.** Per VC-004, have forked subagents write findings to a file/artifact (MOSAIC's blackboard pattern already does this) rather than relying on the primary's narration or the harness's own subagent surfacing.
- **Verify file edits actually landed, especially in remote/dev-container/network-mounted workspaces.** Per VC-015, an edit tool's "Updated" result doesn't guarantee the save succeeded — for edits the orchestrator will build subsequent steps on, consider an independent on-disk check.
- **Design terminal auto-approve rules defensively around `--`, and don't assume an MCP tool's "disabled" error is authoritative.** Per VC-008, prefer broader command-prefix rules over ones expecting a `--` separator to match. Per VC-016, retry an MCP tool call that fails with "disabled by the user" on a fresh request before concluding it's truly unavailable.
- **Cap session context growth in long-running Agent-mode sessions.** Per VC-009, prefer periodic resets/compaction or splitting long orchestration work across fresh sessions rather than letting one conversation grow into the hundreds of thousands of tokens.
- **Given zero maintainer engagement across this entire 13-issue cluster as of two consecutive runs, don't assume any linked fix PR reflects an accepted resolution direction** — re-verify against the live tracker before relying on a "fix in progress" framing for planning purposes.
