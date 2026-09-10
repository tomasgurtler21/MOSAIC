# Cross-Harness Known Issues Comparison

| Field | Value |
|-------|-------|
| **Harnesses Compared** | Claude Code, GHCP-CLI, OpenCode, VS Code GHCP |
| **Created** | 2026-09-10 |
| **Source** | Individual `index.md` and `active-issues.md` files in each harness folder |
| **Purpose** | Side-by-side comparison of known issue landscapes to inform MOSAIC's harness-specific mitigation strategy, identify cross-cutting risks, and prioritize which harness behaviors require permanent adaptation vs. waiting for upstream fixes |

---

## 1. Issue Landscape Overview

| Dimension | Claude Code | GHCP-CLI | OpenCode | VS Code GHCP |
|-----------|:-----------:|:--------:|:--------:|:------------:|
| **Active Issues** | 63 | 19 | 49 | 13 |
| **Resolved Issues** | 1 | 0 | 0 | 0 |
| **Latest Version** | v2.1.267 | v1.0.83 | v1.18.30 | v1.137.0 |
| **HIGH Impact Count** | 54 | 16 | 43 | 11 |
| **MEDIUM Impact Count** | 9 | 3 | 6 | 2 |
| **Confirmed** | 25 | 1 | 30 | 3 |
| **Likely** | 18 | 4 | 5 | 1 |
| **Unverified** | 20 | 14 | 14 | 9 |
| **Any Workaround** | 63 (Yes/Partial) | 10 | 32 | 8 |
| **No Workaround** | 0 | 9 | 17 | 5 |
| **Maintainer Engagement** | Moderate (some confirmed, some engaged) | **None** — zero maintainer responses across all 19 entries | High velocity repo, some confirmed | **None** — zero maintainer comment across all 13 entries (one contributor cluster) |
| **Scan Maturity** | Run 15 (mature) | Run 2 (early) | Run 2 (early) | Run 3 (early) |

### Key Takeaways

- **Claude Code has the most issues tracked but also the most mature scan** (15 runs). Its large count reflects thoroughness of capture, not necessarily worse reliability — it has the most Confirmed entries with maintainer engagement.
- **GHCP-CLI and VS Code GHCP have zero maintainer engagement** across their entire issue sets. Every classification and confidence level rests on reporter evidence alone. This makes their issue lists inherently less reliable — some may be fixed, some may be non-issues.
- **OpenCode has a very high proportion of Confirmed issues** (30 of 49) relative to its scan maturity (run 2), suggesting the issues are well-evidenced.
- **All four harnesses have zero issues reproduced at MOSAIC** and all carry `Unevaluated` MOSAIC Response — the entire knowledge base is upstream-sourced intelligence, not hands-on validation.

---

## 2. Cross-Cutting Risk Themes

The most important finding: **every harness shares the same core risk themes**, though the specific failure modes differ. These are not harness-specific problems — they're systemic to the agentic coding tool category.

### 2.1 Subagent Dispatch & Delegation Reliability

The single most critical theme for MOSAIC's hub-and-spoke model. Every harness has issues where a subagent dispatch either silently fails, hangs, or returns misleading results.

| Failure Mode | Claude Code | GHCP-CLI | OpenCode | VS Code GHCP |
|-------------|:-----------:|:--------:|:--------:|:------------:|
| **Subagent silently returns empty/no response** | CC-003 (killed by limit, reported "completed") | GC-005 (MCP schema budget, zero error signal) | — | VC-003 (tool call hangs forever, no timeout) |
| **Subagent completion signal lost/dropped** | CC-001 (phantom-cancel with fake "user declined") | — | OC-005 (no task_id on failure, can't resume) | VC-011 (completion signals dropped after parent turn ends) |
| **Subagent hangs indefinitely** | CC-002 (permission prompt, fails silently) | GC-008 (session hangs, permanent "Cancelling") | OC-001 (nested permission ask dropped, parent hangs), OC-006 (detached process), OC-096 (no LLM stream timeout) | VC-003 (tool call hangs forever) |
| **Subagent model/config crash** | — | GC-004 (deprecated model crashes entire session) | OC-008 (per-subagent model config ignored) | — |
| **Nested dispatch broken** | CC-011 (disallowedTools not cascading) | — | OC-089/090/091 (permission bypass/absence) | VC-001 (rejected at depth 2), VC-010 (allow-list skipped when agentName omitted) |
| **No internal timeout/watchdog** | — | — | OC-096 (confirmed, no timeout anywhere) | VC-003 (confirmed, no timeout) |

**Cross-harness verdict:** No harness provides reliable, guaranteed subagent dispatch with deterministic success-or-failure signaling. MOSAIC **must** implement external timeout/watchdog logic around every subagent dispatch on every harness — this cannot be delegated to the harness.

**Harness ranking (subagent reliability):**
1. **Claude Code** — Most issues are edge cases (permission prompts, usage limits); core dispatch generally works
2. **OpenCode** — Many confirmed issues but the maintainer velocity is high; core dispatch works but permission scoping is unreliable
3. **GHCP-CLI** — Fewer tracked issues but the ones present are severe (silent empty returns, session crashes)
4. **VS Code GHCP** — Systematic architectural instability in the Agent Host subagent plumbing, actively being investigated by one contributor with zero Microsoft engineering engagement

### 2.2 Permission & Hook Enforcement Gaps

Every harness has issues where configured permissions, hooks, or approval mechanisms are silently not enforced.

| Failure Mode | Claude Code | GHCP-CLI | OpenCode | VS Code GHCP |
|-------------|:-----------:|:--------:|:--------:|:------------:|
| **Hook deny/ask not enforced** | CC-014 (ask not enforced), CC-015 (killed hook fails open), CC-010 (defer strands call) | GC-016 (deny ignored), GC-017 (ask auto-approved) | OC-084 (permission.ask hook dead since v1.2.x), OC-085 (wildcard deny clobbers allow) | — |
| **Permission bypass in non-interactive/headless mode** | CC-016 (background subagents denied Write/Bash) | GC-009 (approval revoked mid-session), GC-013 (Skill allowed-tools ignored in -p), GC-019 (managed setting bypassed) | OC-087 (--auto doesn't cascade to subagents) | VC-008 (auto-approve fails on `--` token) |
| **Tool scoping not enforced on subagents** | CC-011 (disallowedTools, by-design), CC-031 (named dispatch ignores definition) | — | OC-089 (V2 subagent tool actual bypass), OC-090 (stale parent denies + caller-identity bypass) | VC-005 (main chat ignores tool restrictions), VC-010 (allow-list skipped) |
| **Config/allowlist not loaded** | CC-041 (ancestor settings), CC-034 (--add-dir rules), CC-036 (CLAUDE.md above repo root) | GC-014 (allowed_directories never loaded), GC-015 (commandIdentifiers with spaces) | OC-082 (permission merges instead of replaces), OC-083 (config dir overrides path) | — |

**Cross-harness verdict:** Permission enforcement is unreliable across **all** harnesses, especially in non-interactive/headless mode — exactly where MOSAIC's Runner pipeline operates. MOSAIC should treat harness-level permission/hook enforcement as a convenience layer, not a security boundary. Critical guardrails must be enforced externally (OS/sandbox level, or by MOSAIC's own protocol-level validation).

**Worst harness for headless permission reliability:** **GHCP-CLI** — four separate issues (GC-009, GC-012, GC-013, GC-019) all show the non-interactive `-p` path's permission handling diverging from interactive, in both directions (unexpectedly restrictive and unexpectedly permissive).

### 2.3 Context Compaction & Session Stability

Every harness has issues where long sessions degrade, compact incorrectly, or crash.

| Failure Mode | Claude Code | GHCP-CLI | OpenCode | VS Code GHCP |
|-------------|:-----------:|:--------:|:--------:|:------------:|
| **Compaction loses/corrupts state** | CC-006 (preserves fabricated instructions), CC-007 (behavioral rules dropped) | GC-002 (recursive re-summarization degrades facts) | OC-021 (empty summary on reasoning models), OC-023 (races ahead, drops task goal) | — |
| **Compaction doesn't fire / fires wrong** | CC-029 (manual compact silently no-ops) | GC-001 (force-compacts at low usage, loops until OOM) | OC-022 (never trims in single-turn), OC-025 (hangs or no-fire), OC-026 (auto never fires), OC-028 (reserved ignored) | — |
| **OOM / session crash** | CC-044 (100% CPU on base64), CC-038 (Bash freeze 80-90s) | GC-007 (OOM on long --resume, heap leak) | OC-027 (overflow misclassified as network error, session dies), OC-093 (advertised vs actual context mismatch) | VC-009 (hallucinates at ~340k+ tokens) |
| **Post-compaction tool/state loss** | CC-067 (skills become uninvocable after compaction) | — | OC-024 (MCP tools drop to 0 after compaction) | — |

**Cross-harness verdict:** No harness provides reliable automatic compaction for long-running sessions. MOSAIC's blackboard pattern (`Orchestration.md`) and the principle of externalizing durable state to on-disk artifacts are validated as essential — not optional — by every harness's compaction failures. The specific recommendation: **cap session lifetime and checkpoint aggressively** rather than trusting any harness's compaction to preserve orchestration state.

**Worst harness for session stability:** **OpenCode** — eight separate compaction-related issues covering every phase of the compaction lifecycle (detection, execution, and post-compaction state), several Confirmed.

### 2.4 Tool Execution Reliability

Issues where a tool call succeeds (no error) but the result is wrong, incomplete, or never happened.

| Failure Mode | Claude Code | GHCP-CLI | OpenCode | VS Code GHCP |
|-------------|:-----------:|:--------:|:--------:|:------------:|
| **Write/edit silently fails on large content** | — | GC-006 (create-file infinite retry on large files) | OC-041 (write fails silently on ~1000+ lines) | VC-015 (edit reports success before save resolves) |
| **Write/edit wrong target or no-op** | CC-055 (Edit writes into parent's tree), CC-056 (pinned to launch-time worktree) | — | OC-042 (empty content silently overwrites) | — |
| **Tool output truncated/lost** | — | — | OC-043 (truncation drops important content), OC-046 (glob drops truncation flag) | — |
| **Tool call races / double execution** | — | — | OC-045 (encode failure fails sibling calls), OC-095 (parallel race, turn never completes) | VC-012 (double invocation, empty args, dropped results) |
| **Bash/shell hangs** | CC-038 (freeze 80-90s in large sessions) | — | OC-044 (hangs on background/daemon processes — 5 failed fix attempts) | — |

**Cross-harness verdict:** File writing reliability is a universal concern. MOSAIC should **verify writes after the fact** for any safety-critical artifact, rather than trusting a tool's reported success. The pattern of silent write failures on large files (GHCP-CLI GC-006, OpenCode OC-041) directly threatens MOSAIC's artifact-based workflow where subagents write `Research.md`, `Plan.md`, etc.

### 2.5 MCP Tool Integration

Issues specific to MCP (Model Context Protocol) tool connectivity and execution.

| Failure Mode | Claude Code | GHCP-CLI | OpenCode | VS Code GHCP |
|-------------|:-----------:|:--------:|:--------:|:------------:|
| **MCP tools not wired/connected** | CC-008 (one malformed schema drops all tools) | GC-003 (workspace .mcp.json not wired into session) | OC-061 (stale tool-set cache), OC-024 (tools drop to 0 after compaction) | VC-016 (discovery-timing race, tools rejected as "disabled") |
| **MCP subagent permission gap** | — | GC-005 (full-tool subagents empty when schema budget exceeded) | OC-094 (subagent MCP calls all denied — 5 failed fix attempts) | — |
| **MCP tool errors swallowed/unhelpful** | — | — | OC-062 (bare error string), OC-064 (constant "Failed to get tools") | VC-017 (failed task payload discarded) |
| **MCP server crashes / hangs** | — | — | OC-065 (server crash, no restart), OC-067 (tool call hangs, deadlock) | — |
| **MCP concurrency issues** | — | — | OC-066 (CPU saturation, event-loop blocking) | — |

**Cross-harness verdict:** MCP integration is the least mature subsystem across all harnesses. If MOSAIC subagents depend on MCP tools, they face silent tool-loss, permission gaps, and unhelpful error reporting on every harness. Recommendation: treat MCP tool availability as **best-effort**, not guaranteed, and have agents fall back to native tools (grep, glob, bash) when possible.

### 2.6 Worktree & Isolation Issues

Specific to multi-worktree or isolation patterns.

| Failure Mode | Claude Code | GHCP-CLI | OpenCode | VS Code GHCP |
|-------------|:-----------:|:--------:|:--------:|:------------:|
| **Worktree isolation leaks** | CC-054/055/056/057 (session-wide latch, Edit writes wrong tree, cwd unresolvable) | — | — | — |
| **Subagent cwd confusion** | CC-023 (lands in sibling's worktree), CC-073 (cwd resets outside project) | — | — | VC-002 (tool call mis-routed to parent after restart) |
| **Nested worktree blocked** | CC-075 (EnterWorktree refused in pinned-cwd subagent) | — | — | — |

**Cross-harness verdict:** Worktree isolation issues are almost exclusively a Claude Code problem (6 entries), reflecting Claude Code's unique `EnterWorktree` / `isolation:"worktree"` mechanism. MOSAIC should treat worktree isolation as **leaky, not airtight** on Claude Code, and design subagents to explicitly verify their working directory at the start of every Bash call.

---

## 3. Mode-Specific Risk Assessment

MOSAIC operates in two fundamentally different execution modes, and a harness's risk profile can be radically different between them. A harness that is dangerous for Runner/headless execution may work perfectly well in harness-native interactive mode, and vice versa.

### 3.1 Runner / Headless Mode

MOSAIC's Runner pipeline drives harnesses as non-interactive subprocesses (`-p` mode, `--prompt`, or equivalent). No human is available to answer permission prompts, click approval dialogs, or manually recover from hangs.

| Risk Category | Claude Code | GHCP-CLI | OpenCode | VS Code GHCP |
|--------------|:-----------:|:--------:|:--------:|:------------:|
| **Permission enforcement** | MEDIUM — background subagents denied Write/Bash (CC-016), but generally works with permissions.allow | **CRITICAL** — 4 separate breakdowns: approval revoked mid-session (GC-009), /allow-all ineffective (GC-012), Skill allowed-tools ignored (GC-013), managed setting bypassed (GC-019) | HIGH — `--auto` doesn't cascade to subagents (OC-087), showstopper for multi-agent headless runs | HIGH — auto-approve parser bug on `--` token (VC-008) blocks common commands |
| **Session stability** | HIGH — compaction issues, Bash freezes (CC-038) | **CRITICAL** — OOM on --resume (GC-007), memory-pressure compaction loop (GC-001) | HIGH — compaction never fires or fires wrong (OC-022/026), overflow kills session (OC-027) | HIGH — hallucination at high token counts (VC-009) |
| **Unrecoverable hangs** | MEDIUM — Bash freeze is recoverable | HIGH — permanent "Cancelling" state (GC-008), no internal escape | HIGH — no timeout anywhere (OC-096), session hangs forever | HIGH — tool call hangs forever (VC-003), no timeout |
| **Silent tool-execution failure** | LOW — most failures visible | HIGH — empty subagent (GC-005), large-file retry loop (GC-006) | HIGH — silent write failure (OC-041), MCP denial (OC-094) | HIGH — edit reports success before save (VC-015) |

**Runner mode ranking:**
1. **Claude Code** — Fewest headless-specific issues; permission enforcement generally works; most tool failures are visible
2. **OpenCode** — Good tool surface but `--auto` not cascading to subagents (OC-087) is a hard blocker for multi-agent headless runs until fixed; no internal timeout
3. **VS Code GHCP** — Agent Host plumbing instability causes unrecoverable hangs; narrower issue count but architecturally deep
4. **GHCP-CLI** — **Most dangerous for headless execution.** Four separate permission-enforcement breakdowns in `-p` mode, OOM crashes on long sessions, and an unrecoverable session-hang state. Every headless invocation is at risk.

### 3.2 Harness-Native / Interactive Mode

The primary agent runs interactively in the harness's own UI/CLI, with a human available to answer prompts, approve tool calls, and intervene on hangs. Subagents are dispatched through the harness's native agent/task tool.

| Risk Category | Claude Code | GHCP-CLI | OpenCode | VS Code GHCP |
|--------------|:-----------:|:--------:|:--------:|:------------:|
| **Subagent dispatch reliability** | LOW — core dispatch works; edge cases around permission prompts (CC-002), usage-limit misreporting (CC-003) | MEDIUM — silent empty returns (GC-005), deprecated model crash (GC-004), but session hang (GC-008) can be recovered by kill+resume | HIGH — nested permission ask dropped (OC-001), no timeout (OC-096), permission bypass (OC-089/090) | **HIGHEST** — systematic plumbing instability (VC-003/011/012), tool calls hang forever, completion signals dropped, double execution |
| **Permission/hook enforcement** | MEDIUM — hook deny/ask gaps (CC-010/014/015), but human can intervene | MEDIUM — hook deny not enforced (GC-016), ask auto-approved (GC-017), but human can catch and retry | HIGH — permission.ask hook dead (OC-084), wildcard deny clobbers allow (OC-085), but human can approve manually | HIGH — allow-list bypasses (VC-010), tool restrictions ignored (VC-005), but human can observe |
| **Context/session stability** | MEDIUM — compaction loses behavioral rules (CC-007), skills lost post-compaction (CC-067), but human can notice degradation | MEDIUM — recursive summarization degrades facts (GC-002), but HANDOFF.md workaround works interactively | HIGH — 8 compaction issues, but human can checkpoint manually | LOW — hallucination at ~340k+ tokens (VC-009) is the main concern; fewer compaction issues |
| **Tool execution** | LOW — worktree issues exist (CC-055/056) but most tool calls work correctly | LOW — large-file retry (GC-006) is the main concern; human can intervene and split files | MEDIUM — silent write failure on large files (OC-041), bash hangs on daemons (OC-044) | MEDIUM — edit success before save (VC-015), client-tool races (VC-012) |
| **HITL effectiveness** | **BEST** — human can approve, retry, and steer via AskUserQuestion (primary-only) | Good — human can approve via TUI, kill+resume on hangs | Good — human can approve, question tool works both tiers | Weakest — AskUserQuestion silently auto-denied when queue empty (VC-011), subagent UI visibility gaps (VC-004) |

**Harness-native mode ranking:**
1. **Claude Code** — Most issues are edge cases in interactive mode. Core dispatch, tools, and HITL all work. Worktree cluster is the main concern but only affects multi-worktree patterns.
2. **GHCP-CLI** — Dramatically better than its Runner ranking. The `-p` permission breakdown cluster (its defining weakness) doesn't apply. Session hangs (GC-008) are recoverable via kill+resume. Large-file retry (GC-006) is manageable with human intervention.
3. **OpenCode** — Permission evaluator bugs and compaction failures still apply in interactive mode. Bash daemon hangs (OC-044, 5 failed fixes) and no-timeout-anywhere (OC-096) are real risks even with a human watching.
4. **VS Code GHCP** — **Most problematic for interactive use.** Agent Host plumbing instability (VC-003/011/012) causes hangs, dropped signals, and double execution regardless of whether a human is present. The human can detect the problem but has no in-harness recovery path — external kill is the only option.

### 3.3 Mode Ranking Comparison

| Harness | Runner Rank | Native Rank | Delta | Takeaway |
|---------|:----------:|:----------:|:-----:|----------|
| **Claude Code** | 1 (best) | 1 (best) | — | Consistently lowest risk across both modes |
| **GHCP-CLI** | 4 (worst) | 2 | **+2** | Its defining weakness (non-interactive permission breakdown) vanishes in interactive mode |
| **OpenCode** | 2 | 3 | **-1** | Permission and compaction issues affect both modes roughly equally; `--auto` cascade is Runner-specific but OC-096 no-timeout is universal |
| **VS Code GHCP** | 3 | 4 (worst) | **-1** | Agent Host plumbing instability is primarily a harness-native problem (interactive subagent dispatch); headless/CLI-driven invocations may bypass some of this machinery |

---

## 4. Issue Classification Comparison

| Metric | Claude Code | GHCP-CLI | OpenCode | VS Code GHCP |
|--------|:-----------:|:--------:|:--------:|:------------:|
| **Bugs** | 59 | 18 | 48 | 13 |
| **Limitations** | 3 (CC-011, CC-048, CC-051) | 1 (GC-002) | 0 | 0 |
| **Quirks** | 0 | 0 | 0 | 0 |
| **Bug/Limitation** | 1 (CC-030) | 0 | 0 | 0 |

**Observation:** Almost everything across all harnesses is classified as Bug, not Limitation. Only Claude Code has identified by-design behaviors (3 confirmed Limitations), likely because its scan is the most mature (15 runs) and has the most maintainer engagement. As other harnesses' scans mature, expect some Bugs to be reclassified as Limitations — particularly OpenCode's permission-system behaviors and GHCP-CLI's compaction architecture.

---

## 5. Workaround Coverage

| Workaround Status | Claude Code | GHCP-CLI | OpenCode | VS Code GHCP |
|------------------|:-----------:|:--------:|:--------:|:------------:|
| **Yes (confirmed workaround)** | 28 (44%) | 3 (16%) | 3 (6%) | 0 (0%) |
| **Partial (incomplete/trade-off)** | 35 (56%) | 7 (37%) | 29 (59%) | 8 (62%) |
| **No workaround** | 0 (0%) | 9 (47%) | 17 (35%) | 5 (38%) |

**Cross-harness verdict:** Claude Code has the best workaround coverage — every issue has at least a partial workaround. GHCP-CLI has the worst: nearly half its issues have no known workaround at all. This correlates with the lack of maintainer engagement — workarounds often come from maintainer suggestions in issue comments.

---

## 6. Thematic Clusters — Unique to Each Harness

Issues that are distinctive to a single harness and have no clear parallel elsewhere.

### Claude Code Only
| Theme | Key Issues | MOSAIC Impact |
|-------|-----------|---------------|
| **Worktree isolation failures** | CC-023, CC-054–057, CC-063, CC-073, CC-075 | Directly threatens multi-worktree fan-out patterns |
| **Auto Mode defeats safety mechanisms** | CC-060 | Bash-first steering defeats CLAUDE.md loading, /rewind, hooks, and Edit/Write permissions |
| **Skills become uninvocable after compaction** | CC-067 | Skill-dependent subagents can silently lose their skill mid-session |
| **Windows hook execution failures** | CC-068–071 | Windows-specific: statusline leaks, hooks never fire, backslash path handling |
| **Token usage inflation** | CC-040 | Multi-iteration turns report 2x-6x actual usage; compaction can *increase* context |

### GHCP-CLI Only
| Theme | Key Issues | MOSAIC Impact |
|-------|-----------|---------------|
| **Non-interactive permission breakdown cluster** | GC-009, GC-012, GC-013, GC-019 | Four distinct ways `-p` mode's permission handling fails — the harness's defining weakness |
| **OOM on --resume (heap leak)** | GC-007 | Long-lived sessions crash from unbounded memory growth even with raised heap limits |
| **Create-file infinite retry on large files** | GC-006 | Directly threatens artifact-writing pattern (Research.md, Plan.md) |

### OpenCode Only
| Theme | Key Issues | MOSAIC Impact |
|-------|-----------|---------------|
| **Permission evaluator ordering defects** | OC-082, OC-085, OC-086, OC-097 | Wildcard deny clobbers specific allow; plugin permissions can't override root deny |
| **Bash tool hangs on daemons (5 failed fixes)** | OC-044 | Treat as durable — not getting fixed soon |
| **MCP subagent permission gap (5 failed fix attempts)** | OC-094 | Subagents see MCP tools but every call denied — core dispatch failure |
| **Concurrent tool-call races** | OC-045, OC-095 | One failed tool can falsely fail siblings; slow MCP tool stuck forever |

### VS Code GHCP Only
| Theme | Key Issues | MOSAIC Impact |
|-------|-----------|---------------|
| **Agent Host subagent plumbing (one-contributor investigation)** | VC-001–004, VC-010–012 | Systematic architectural instability; all fix PRs from one contributor, zero MS engineering engagement |
| **Client-tool execution races (foundational)** | VC-012 | Double execution, empty args, dropped results — shared root cause for VC-002/003/011 |
| **Forked Skill subagents invisible** | VC-004 | Skill work happens but is never surfaced in UI or completion signals |
| **High-token hallucination** | VC-009 | Agent invents and acts on fictional tasks at ~340k+ tokens |

---

## 7. MOSAIC Mitigation Priority Matrix

Recommended mitigations ordered by cross-cutting impact. The **Mode** column indicates which execution mode each mitigation applies to — mitigations affecting only one mode can be skipped when planning for the other.

| Priority | Mitigation | Mode | Harnesses | Issue IDs |
|:--------:|-----------|:----:|-----------|-----------|
| **1** | **External wall-clock timeout around every subagent dispatch.** No harness provides reliable internal timeout or guaranteed completion signaling. | Both | All four | CC-001/002/003, GC-005/008, OC-001/005/006/096, VC-003/011 |
| **2** | **Verify file writes after the fact** for safety-critical artifacts. Don't trust a tool's reported success. | Both | All four | GC-006, OC-041/042, VC-015 |
| **3** | **Externalize durable state to on-disk artifacts early and often.** Don't rely on conversational context surviving compaction. Validates MOSAIC's Orchestration.md blackboard pattern as essential. | Both | All four | CC-006/007/067, GC-001/002, OC-021–028, VC-009 |
| **4** | **Cap session lifetime and restart proactively** rather than running one session indefinitely. | Both | All four | CC-038/040/044, GC-001/007, OC-022/026/027/093, VC-009 |
| **5** | **Treat harness permission/hook enforcement as convenience, not security.** Enforce critical guardrails externally. | Both | All four | CC-010/014/015/025, GC-016/017, OC-084/085/089/090, VC-005/010 |
| **6** | **Treat MCP tool availability as best-effort.** Have agents fall back to native tools when MCP tools are unavailable or silently failing. | Both | All four | CC-008, GC-003/005, OC-024/061/062/064/065/094, VC-016/017 |
| **7** | **Instruct subagents to split large artifact writes** into multiple smaller files. | Both | CC, GC, OC | GC-006, OC-041 |
| **8** | **For GHCP-CLI headless: pass `--additional-mcp-config @.mcp.json` explicitly** and verify non-interactive permission behavior directly before relying on it. | Runner | GC only | GC-003, GC-009/012/013/019 |
| **9** | **For Claude Code: treat worktree isolation as leaky.** Have subagents explicitly verify their cwd at the start of every Bash call. | Both | CC only | CC-023/054–057/063/073/075 |
| **10** | **For VS Code GHCP: keep delegation to single hop** from primary; don't trust allow-lists as hard enforcement. | Native | VC only | VC-001/003/010/011/012 |
| **11** | **For GHCP-CLI native: use kill+resume as the standard recovery** for session hangs. Don't wait indefinitely. | Native | GC only | GC-008 |
| **12** | **For OpenCode headless: verify `--auto` cascades to subagents** before relying on multi-agent headless runs. Block until OC-087 is fixed or a workaround found. | Runner | OC only | OC-087 |

---

## 8. MOSAIC Compatibility Scorecard — Known Issues Perspective

Rating each harness on how its known issue landscape affects MOSAIC's core patterns. **Separated by execution mode** because a harness's risk profile can be radically different between Runner and harness-native (see Section 3.3 for the ranking delta).

### 8.1 Runner / Headless Mode Scorecard

| MOSAIC Pattern | Claude Code | GHCP-CLI | OpenCode | VS Code GHCP |
|---------------|:-----------:|:--------:|:--------:|:------------:|
| **Headless permission enforcement** | Low risk | **Highest risk** (4 independent breakdowns) | High risk (--auto not cascading) | High risk (auto-approve parser bug) |
| **Artifact file writing** | Low risk (workarounds exist) | High risk (large-file retry loop) | Moderate risk (silent fail on 1000+ lines) | Moderate risk (success before save) |
| **Long-running session stability** | Moderate risk (compaction, freezes) | **Highest risk** (OOM crash, compaction loop) | High risk (8 compaction issues) | Moderate risk (hallucination at high tokens) |
| **Hang recovery (no human available)** | Low risk (Bash freeze is recoverable) | High risk (permanent "Cancelling" state) | **Highest risk** (no timeout anywhere) | High risk (tool call hangs forever) |
| **MCP tool reliance** | Low risk (1 issue) | High risk (workspace config not wired, schema budget) | **Highest risk** (7 MCP issues) | Moderate risk (discovery race, task payload loss) |
| **Silent dispatch failure** | Low risk | High risk (empty subagent, zero error signal) | High risk (silent write, MCP denial) | High risk (edit success before save) |

**Runner mode ranking:**

| Rank | Harness | Verdict |
|:----:|---------|---------|
| 1 | **Claude Code** | **Lowest Runner risk.** Permission enforcement generally works in non-interactive contexts. Most tool failures are visible. Compaction and Bash freezes are the main concerns. |
| 2 | **OpenCode** | **Moderate risk.** `--auto` not cascading (OC-087) is a hard blocker for multi-agent headless runs. No internal timeout anywhere (OC-096). But high maintainer velocity suggests these may improve. |
| 3 | **VS Code GHCP** | **High risk.** Agent Host plumbing instability causes unrecoverable hangs with no timeout. Narrower issue count but architecturally deep. |
| 4 | **GHCP-CLI** | **Highest Runner risk.** Non-interactive permission enforcement is comprehensively broken across 4 independent mechanisms. OOM on long sessions. Nearly half the issues have no workaround. Zero maintainer engagement. |

### 8.2 Harness-Native / Interactive Mode Scorecard

| MOSAIC Pattern | Claude Code | GHCP-CLI | OpenCode | VS Code GHCP |
|---------------|:-----------:|:--------:|:--------:|:------------:|
| **Hub-and-spoke dispatch** | Low risk (edge cases only) | Moderate risk (silent empty returns, but human can detect) | High risk (permission bypass, no timeout) | **Highest risk** (systematic plumbing instability, hangs, signal drops) |
| **HITL effectiveness** | **Best** (human can approve, retry, steer) | Good (TUI approval, kill+resume) | Good (question tool both tiers) | Weakest (AskUserQuestion silently auto-denied, subagent UI gaps) |
| **Permission/hook enforcement** | Moderate risk (hook gaps, but human intervenes) | Moderate risk (hook deny/ask issues, but human catches) | High risk (evaluator ordering bugs affect both modes) | High risk (allow-list bypasses, tool restrictions ignored) |
| **Long-running session stability** | Moderate risk (compaction, behavioral rule loss) | Moderate risk (recursive summarization, but HANDOFF.md works) | High risk (8 compaction issues) | Low risk (fewer compaction issues; hallucination only at extreme token counts) |
| **Artifact file writing** | Low risk | Low risk (human can split files) | Moderate risk (silent fail on large files) | Moderate risk (success before save) |
| **MCP tool reliance** | Low risk | Moderate risk (workspace config issue, but human can pass flag) | **Highest risk** (7 MCP issues) | Moderate risk (discovery race, task payload loss) |
| **Skills injection** | Moderate risk (CC-067 compaction loss) | Unknown (not enough data) | Moderate risk (OC-088 hardcoded /tmp) | High risk (VC-004 forked skills invisible) |

**Harness-native mode ranking:**

| Rank | Harness | Verdict |
|:----:|---------|---------|
| 1 | **Claude Code** | **Lowest interactive risk.** Most issues are edge cases when a human is present. Best HITL surface. Worktree cluster is the main concern but only for multi-worktree patterns. |
| 2 | **GHCP-CLI** | **Much better than its Runner ranking.** The `-p` permission breakdown cluster — its defining weakness — doesn't apply interactively. Session hangs recoverable via kill+resume. Large-file retry manageable with human intervention. |
| 3 | **OpenCode** | **Permission and compaction issues persist in both modes.** Bash daemon hangs (5 failed fixes) and no-timeout-anywhere are real risks even with a human. But high maintainer velocity is encouraging. |
| 4 | **VS Code GHCP** | **Most problematic for interactive use.** Agent Host plumbing instability (hangs, dropped signals, double execution) has no in-harness recovery regardless of human presence. Zero Microsoft engineering engagement. |

---

## 9. Open Questions

| # | Question | Why It Matters |
|---|----------|---------------|
| 1 | Has any MOSAIC user reproduced **any** of these 144 issues first-hand? | The entire knowledge base is upstream-sourced. Until MOSAIC reproduces at least the highest-impact issues on its own infrastructure, confidence in their real-world relevance is theoretical. |
| 2 | Which compaction failures actually manifest at MOSAIC's typical session lengths? | CC/GC/OC all have compaction issues, but MOSAIC sessions may not run long enough to trigger them. Empirical session-length data would let us de-prioritize inapplicable entries. |
| 3 | Is GHCP-CLI's `-p` mode permission breakdown version-gated? | GC-009/012/013 were reported against versions 3-5+ releases behind current. A single reproduction attempt on v1.0.83 could resolve or confirm a large fraction of the GHCP-CLI risk. |
| 4 | Does VS Code GHCP's Agent Host subagent plumbing instability affect Copilot Chat's built-in agent modes, or only the external Claude-provider path? | RyanEwen's entire investigation is on a dev-container Claude provider setup. If MOSAIC uses Copilot's native models (GPT-5.6 Sol/Terra) via the built-in provider, the blast radius may be narrower. |
| 5 | What is the practical threshold for OpenCode OC-041's silent large-file write failure? | OC-041 says "~1000+ lines" — is this a hard limit or does it depend on content? Knowing the exact threshold lets MOSAIC subagent templates proactively split artifacts. |

---

*Generated from individual harness issue knowledge bases in this folder. See each harness's `index.md` for per-harness summaries and `active-issues.md` for full issue details.*
