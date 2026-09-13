# Claude Code — Resolved Issues

> Last updated: 2026-09-10 (run 15)

---

### CC-005: `PostToolUse` hook doesn't fire for `Agent` tool completions at the parent session level

| Field | Value |
|-------|-------|
| **Source** | Unconfirmed — could not be relocated on `anthropics/claude-code` |
| **Fixed In / Status** | N/A (not relevant — unsourceable) |
| **Resolution Date** | 2026-09-10 |
| **Original Orchestration Impact** | HIGH |

**Summary:**
Entry claimed the `PostToolUse` hook does not fire when an `Agent` tool call (subagent dispatch) completes, at the parent session level. The original capture never retained a source URL, and a prior backfill pass (2026-09-09) already searched extensively without relocating a matching issue.

**Resolution:**
A second independent search pass (2026-09-10, run 15 batch 1) repeated and extended the prior backfill attempt — combining PostToolUse/Agent/Task/parent-session/completion keyword queries and reviewing `area:hooks`-labeled issues — and still could not locate any GitHub issue matching this precise claim. Two adjacent candidates were re-examined and ruled out as the true source, same as the prior pass: #82249 ("`SubagentStop` still doesn't fire for subagents launched via the Agent tool (async/background)") describes a real, evidenced, still-open defect, but the specific hook is `SubagentStop` (not `PostToolUse`) and it fires/fails to fire in the subagent's own hook context, not at the parent session level; #90662 describes misattributed `agent_id` on hooks inside a running subagent, a different mechanism entirely. With no locatable source after two independent thorough searches, and no way to distinguish a real-but-mis-transcribed claim from a fabricated/conflated one, this entry fails the knowledge base's evidentiary bar and is retired as unsourceable rather than carried indefinitely as "Likely" confidence with unknown fields. Flagged as a suggested follow-up: #82249 itself is a real, well-evidenced, currently-untracked gap (hook lifecycle events not firing for Agent-tool async/background subagent completions) that a future discovery pass should consider adding as its own new KB entry on its own merits, rather than as a proxy for this retired claim.
