# GitHub Copilot CLI — Active Issues

> Last updated: 2026-09-10 (run 2)

---

### GC-001: Memory-pressure watchdog force-compacts conversation at low context usage, can loop until OOM

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/github/copilot-cli/issues/4506 |
| **Reported** | 2026-08-16 |
| **Last Activity** | 2026-08-17 |
| **Confidence** | Unverified |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 1.0.80 (unknown if fixed since) |
| **Latest Platform Version** | v1.0.83 (released 2026-09-04) |
| **Labels** | area:sessions, area:context-memory |

**Summary:**
A process-level memory-pressure watchdog force-compacts the conversation whenever process memory is high, without checking whether the conversation itself is the memory being consumed. In the reported session, memory was dominated by the session's own `events.jsonl` (381 MB, held parsed in memory) — compaction ran repeatedly against a conversation at only 23% of a 400k-token window, recovered ~3 tokens (0.003%) each time, and looped indefinitely because the underlying memory pressure was unrelated to conversation size. The process eventually OOM'd and crashed. A contributing factor (`logLevel: "all"`, non-default) causes unbounded verbatim third-party tracing (`html5ever` tokenizer output) to be written to disk, pushing `events.jsonl` and process memory even higher. No maintainer response or comments; single reporter with exceptionally detailed technical evidence (log line frequency counts, exact memory metrics, V8 OOM stack trace, reproduction steps).

**Impact on Orchestration:**
Long-running MOSAIC sessions (interactive hub-and-spoke orchestration, or a headless Runner stage that stays open across many tool calls) can hit this watchdog and lose conversation history to compaction that provides zero memory relief, then eventually crash the process outright — losing session state entirely rather than gracefully degrading. Because the trigger is unrelated to actual context-window usage, this can strike even a conversation MOSAIC believes is well within budget, undermining the reliability of both primary-agent context management and the assumption that compaction is purely context-driven.

**Evidence:**
- Single reporter, no maintainer acknowledgment, no independent reproduction reports
- Extremely detailed, quantified technical evidence: log line counts, exact memory/heap metrics, `events.jsonl` size, and version/environment specifics — not a vague symptom report
- Meets the Unverified-inclusion bar: threatens a core MOSAIC concern (primary-agent context/session stability), reproduced on stock Copilot CLI infrastructure, and includes a clear, mechanical reproduction recipe

**Workaround(s):**
1. ★ Avoid setting `"logLevel": "all"` in `~/.copilot/settings.json` — this is the largest identified contributor to unbounded `events.jsonl`/log growth that feeds the memory-pressure trigger; default log level does not exhibit the same growth.
2. Periodically restart very long-running sessions (rather than keeping one process alive for many hours/days) to bound `events.jsonl` growth and process memory before the watchdog engages.
3. Avoid heavy `web_fetch` usage against large HTML pages in the same long-lived session, since HTML tokenizer trace volume (under `logLevel: all`) was a major contributor in the reported case.

**Notes:**
No maintainer response yet — needs re-verification for corroboration in a future run. If confirmed widespread, this argues for MOSAIC's Runner pipeline to bound individual headless session lifetimes (restart between stages rather than reusing one long-lived process) as a structural mitigation independent of any upstream fix.

---

### GC-002: Repeated compaction recursively re-summarizes prior summaries, degrading durable context

| Field | Value |
|-------|-------|
| **Classification** | Limitation |
| **Source** | https://github.com/github/copilot-cli/issues/4441 |
| **Reported** | 2026-08-11 |
| **Last Activity** | 2026-08-19 |
| **Confidence** | Likely |
| **Orchestration Impact** | MEDIUM |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 1.0.79 (documented/architectural behavior, not version-specific) |
| **Latest Platform Version** | v1.0.83 (released 2026-09-04) |
| **Labels** | area:context-memory |

**Summary:**
Filed as a feature request, but the underlying behavior is a confirmed, documented architectural property: each compaction re-summarizes the *previous summary* together with newer turns, rather than re-summarizing from original source material. Official Copilot CLI context-management docs confirm compaction replaces active history with a generated summary and explicitly warn that important context may be lost after many compaction cycles. The reporter's own session logs quantify the effect: a first compaction reduced 196 active messages to 1 replacement message; a second reduced 473 to 1. Early decisions, constraints, and gotchas progressively degrade with every additional compaction cycle since each cycle compresses an already-lossy compression rather than the original record. Reclassified from the GitHub "Feature request" framing to **Limitation** here because the recursive-summarization mechanism is doc-confirmed and by-design, not a defect — but it still poses a real degradation risk for MOSAIC's long sessions.

**Impact on Orchestration:**
Long MOSAIC orchestration runs (many workflow stages, or a single long interactive session) that pass through multiple compaction cycles will progressively lose fidelity on early decisions, constraints, and blackboard-relevant facts — exactly the kind of durable state MOSAIC's `Orchestration.md` blackboard pattern is meant to preserve externally, but any context the primary agent still needs to reason about mid-session (not yet written to a file artifact) is at risk of silent, cumulative degradation the more compactions a session goes through.

**Evidence:**
- Single reporter, but corroborated by official GHCP CLI documentation describing the same recursive-summarization mechanism and its documented risk of context loss over many cycles
- Reporter provided concrete, quantified before/after message counts from an actual session (196→1, 473→1) demonstrating the compounding effect directly
- Confidence set to Likely (not Confirmed) because there is no maintainer acknowledgment specifically discussing whether this is considered a problem to fix, and only one reporter/session is behind the quantified numbers — but the mechanism itself is doc-corroborated, not merely a single user's inference

**Workaround(s):**
1. ★ Maintain a manually/agent-updated `HANDOFF.md` durable-facts file: before each `/compact`, have the agent write/update a running log of decisions, facts, constraints, and gotchas to this file, then have it re-read the file immediately after compaction completes. Reporter (a Copilot CLI user, in a follow-up comment) confirmed this pattern lets them keep using a session across many compaction cycles without losing durable knowledge. Not upstream-confirmed by a maintainer, but a concrete, currently-usable mitigation.

**Notes:**
Classified as Limitation rather than Bug since the recursive-summarization mechanism is a documented architectural property of compaction, not an unintended defect — MOSAIC should treat this as a permanent constraint to design around (e.g., encouraging subagents/orchestrator to externalize durable facts to on-disk artifacts early and often, rather than relying on conversational context surviving many compaction cycles) rather than something to wait on a fix for. The issue also requests first-class `preCompact`/`postCompact` hook support to automate the HANDOFF.md pattern — worth tracking if that ships, since it would turn this from a manual workaround into a built-in mitigation.

---

### GC-003: Workspace `.mcp.json` detected by `mcp list`/`mcp get` but not wired into actual agent session (interactive, `-i`, `-p`)

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/github/copilot-cli/issues/4542 |
| **Reported** | 2026-08-20 |
| **Last Activity** | 2026-08-25 |
| **Confidence** | Confirmed |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 1.0.75 (regression point per comment) through 1.0.80; likely still present at time of scan |
| **Latest Platform Version** | 1.0.83 (released 2026-09-04) |
| **Labels** | area:configuration, area:mcp |

**Summary:**
A workspace-scoped `.mcp.json` file is correctly discovered by `copilot mcp list` / `copilot mcp get` (reported as `Status: Enabled`, `Source: Workspace`), but its servers are silently NOT connected into the actual running agent session — in default interactive mode, `-i` auto-prompt mode, or non-interactive `-p` mode. The tools simply don't exist for the agent to call; there is no error. The only fix is to re-pass the identical file explicitly via `--additional-mcp-config @.mcp.json` on every invocation. Reproduced independently on both macOS and Windows across two different MCP SDKs (Node and Python). One reporter narrowed the trigger further: it only happens for workspaces that are not "always trusted" — and that it used to work in 1.0.75, confirming a regression.

**Impact on Orchestration:**
This directly affects headless, automated operation — exactly how MOSAIC's Runner pipeline drives GHCP CLI as a subprocess backend (`copilot ... -p "..."`) for workflow stages. Any MOSAIC subagent or workflow stage that depends on a project-committed `.mcp.json` for its MCP tools (a common way to give a subagent domain-specific tools without per-invocation flags) will silently run with those tools missing — the agent will report "tool not available" or attempt to route around it, producing degraded or incorrect results with no visible configuration error. Because Runner drives many stage invocations as discrete `copilot` subprocess calls, this is not a one-time bootstrap issue — it can recur every single stage dispatch unless the launcher itself is patched to inject `--additional-mcp-config`.

**Evidence:**
- Two independent reporters, on different OSes (macOS Sequoia arm64, Windows 10/11) and different MCP server implementations, both show identical symptom: `mcp list`/`mcp get` reports "Enabled" but the tool is unreachable in session; both confirm `--additional-mcp-config @.mcp.json` restores it
- A third commenter narrows the trigger condition (untrusted workspace) and confirms it as a regression from a previously-working version (1.0.75)
- Reporter identifies a plausibly-related closed issue (#2198, "fixed in 1.0.12") that appears to have only fixed the discovery/listing layer, not session-bootstrapping — suggesting a partial regression of that original fix
- No maintainer acknowledgment yet, but the reproduction bar for Confirmed is met via multiple independent reproductions with clear evidence

**Workaround(s):**
1. ★ Explicitly pass `--additional-mcp-config @.mcp.json` on every `copilot` invocation (interactive or `-p`), even though the same file is already auto-discovered as workspace config. Confirmed working by both independent reporters. A shell wrapper/launcher that auto-detects `.mcp.json` in the cwd and injects this flag avoids having to remember it manually — relevant for MOSAIC's Runner pipeline, which could inject this flag automatically when invoking `copilot` as a subprocess.
2. One reporter found the issue does not occur when the workspace is marked "always trusted" in Copilot CLI's trust settings — trusting the workspace ahead of time may avoid the bug, though this is a single, unconfirmed report and trust prompts are themselves awkward in headless automation.

**Notes:**
Version-specific: regression reported to have appeared after 1.0.75 (still present on 1.0.80, tested by two reporters weeks apart). Related to previously-closed #2198, which only addressed the `mcp list`/`mcp get` discovery layer, not actual session wiring — worth re-checking whether a future fix PR references both issues together. If MOSAIC's `Tools/Runner` GHCP CLI backend invokes `copilot` per stage without `--additional-mcp-config`, this should be treated as HIGH priority for MOSAIC Response given it silently degrades tool availability with no error signal.

Additional corroboration found during this same capture pass: https://github.com/github/copilot-cli/issues/4779 ("MCP discovery ISSUE", filed 2026-09-09 directly against the current latest version 1.0.83) reports the same class of failure — workspace `.mcp.json`/`.github/mcp.json` config not loading into the session while global user-level config works fine, with trust prompt accepted. Zero comments, and the reporter self-closed it the next day (2026-09-10) with no explanation or fix evidence — per the "don't trust closure labels" rule this is not a verified resolution. Not tracked as its own KB entry (duplicate risk, thin evidence on its own), but it strengthens confidence that this general workspace-MCP-not-wired-into-session failure class is current as of the latest released version at time of this scan.

---

### GC-004: Subagent launched with a since-deprecated/unsupported model crashes the entire primary session

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/github/copilot-cli/issues/4427 |
| **Reported** | 2026-08-11 |
| **Last Activity** | 2026-08-11 |
| **Confidence** | Unverified |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 1.0.79 |
| **Latest Platform Version** | 1.0.83 (released 2026-09-04) |
| **Labels** | area:agents, area:models |

**Summary:**
In a long-running autopilot session, the primary agent (Opus 5) repeatedly dispatched a subagent configured to use `gemini-3.6-flash`. Partway through the ~2 hour session, that model was retracted from the supported model pool server-side. The next subagent dispatch attempt failed with `CAPIError: 400 The requested model is not supported` — and instead of the primary agent catching this and retrying with a different model, the error propagated up and killed the entire primary session. No maintainer or community response yet; single reporter, no comments.

**Impact on Orchestration:**
This is a direct hit on MOSAIC's core dispatch mechanism: the orchestrator (primary agent) invokes subagents via the harness's native agent/task tool, and a subagent's configured model is part of that agent's own definition. If a model referenced by any MOSAIC subagent (or a Runner-driven workflow stage) is deprecated or temporarily unavailable server-side, this failure mode means the entire orchestrator session — not just that one subagent dispatch — terminates. In a long autopilot-style orchestration run (exactly MOSAIC's target use case, whether interactive hub-and-spoke or headless Runner pipeline stages), this converts a single recoverable dispatch failure into total session loss, discarding the blackboard state accumulated in that session unless externally checkpointed.

**Evidence:**
- Single reporter, no maintainer acknowledgment, no independent reproduction — capped at Unverified per validation rules
- Included despite Unverified status because it meets all three inclusion bars: threatens a core MOSAIC pattern (subagent dispatch/invocation reliability), occurs on GHCP CLI's standard, natively-supported multi-provider model catalog (not a custom backend/proxy — using Gemini and Opus together is a first-class, documented capability of this harness, not an unsupported configuration), and provides a clear, concrete reproduction recipe (retract/ban a model mid-session while a subagent is configured to use it)
- The exact error string (`CAPIError: 400 The requested model is not supported.`) is reported directly from the session, not paraphrased
- A much older, related issue (#1237, filed against v0.400) reported the same error string for the built-in `explore` subagent and was closed as fixed in v0.0.407 via a model-fallback mechanism ("subagents now fall back to the session model when their default model is unavailable or blocked by policy"). A later commenter on that same closed issue (v1.0.21) reported the identical `CAPIError: 400 The requested model is not supported.` recurring, including on the *primary* session, not just a subagent. Taken together with this (#4427, v1.0.79) report, the fallback mechanism from v0.0.407 does not appear to fully cover the case of a model becoming unsupported *after* a session/subagent has already started using it — the general failure class (unsupported-model error propagating and breaking the session rather than triggering a graceful fallback) has recurred across CLI versions from v0.400 through at least v1.0.79.

**Workaround(s):**
No confirmed workaround reported. Possible mitigations worth evaluating (not yet community-validated):
1. Avoid pinning subagents to narrowly-available or preview/flash-tier models that are more likely to be deprecated mid-session; prefer models with longer support commitments for long-running orchestration sessions.
2. At the MOSAIC Runner level, consider wrapping stage dispatch so a model-not-supported error triggers a stage-level retry with a fallback model rather than propagating as a fatal session error — this would need to be built into `Tools/Runner` or the harness injection layer since the CLI itself does not do this.

**Notes:**
Single-reporter/Unverified; should be re-checked in a future run for corroboration or maintainer response. See cross-reference to closed issue #1237 in Evidence above — that issue was investigated during this same capture pass and found to have a "fixed" claim that appears incomplete given this and other later reports, but was not itself re-added as a separate KB entry since #4427/GC-004 already covers the currently-active manifestation. If confirmed, this argues for MOSAIC treating subagent model selection as a config value the orchestrator can validate/fall back on rather than a hard dependency, and for the Runner pipeline to checkpoint blackboard state frequently enough that a session-ending crash doesn't lose orchestration progress.

---

### GC-005: Sub-agents with full tool access silently return empty (no error, no log) once configured MCP tool-schema volume crosses an undocumented budget

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/github/copilot-cli/issues/4293 |
| **Reported** | 2026-07-29 |
| **Last Activity** | 2026-07-30 |
| **Confidence** | Unverified |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 1.0.73 |
| **Latest Platform Version** | 1.0.83 (released 2026-09-04) |
| **Labels** | area:agents, area:tools |

**Summary:**
Sub-agents dispatched via the `task` tool with a full-tool-access agent type (`general-purpose`, `rubber-duck`, `task`) silently return `Agent completed but produced no response.` — no error, no partial output, nothing in the session log — while a restricted-tool agent type (`explore`) launched with the identical model and prompt in the same session succeeds. The reporter ran a controlled test halving their configured MCP server count (12 → 6) and confirmed a direct causal link: the failure tracks total tool-schema volume materialized into the sub-agent's context at session start, not the agent type or model (reproduced identically across `gpt-5.6-sol`, `gpt-5.6-terra`, and `claude-opus-4.8`, in both sync and background dispatch modes). A session restart is required for a server-count change to take effect — schema is fixed at session start. The reporter explicitly reframes the ask: the MCP/tool-schema budget itself is a separate, already-tracked problem (referenced as #3542), but this issue is specifically about the **silent, undetectable failure mode** — no error, no log line, no UI signal — which persists regardless of how the underlying budget is fixed.

**Impact on Orchestration:**
This is a severe, direct hit on subagent invocation and response capture — core to MOSAIC's hub-and-spoke model, where the orchestrator dispatches subagents and depends on their structured JSON status responses to decide routing. If a MOSAIC deployment configures several MCP servers (plausible for tool-rich subagents like research or infrastructure agents) and the combined tool-schema volume crosses this undocumented per-session budget, a subagent dispatch can silently produce empty output that is indistinguishable from a legitimate empty/no-op response. The orchestrator has no signal to retry or escalate — it may interpret "no response" as a completed but empty result and continue routing on a workflow assumption that never actually executed, corrupting the run's blackboard state silently. This risk scales with the number of MCP servers/tools a MOSAIC deployment wires up, which is exactly the trend as more Skills/tools get added to subagents.

**Evidence:**
- Single reporter, but with an unusually rigorous self-directed controlled experiment: identical prompts/models tested with 12 vs. 6 configured MCP servers, isolating tool-schema volume (not agent type, not model, not sync/background mode) as the causal variable
- Reproduced across 3 different models (`gpt-5.6-sol`, `gpt-5.6-terra`, `claude-opus-4.8`) and both `sync`/`background` dispatch modes with consistent results
- Confirmed the session log contains zero `[ERROR]`/`[WARN]` entries during the failure window — genuinely silent, not merely unlogged-but-detectable
- Reporter cross-references related issues (#3542 token-budget/contextTier ignored, #2630 custom agent MCP servers not connected in sub-agent contexts, #3293 extension tools not propagated to sub-agents) suggesting a broader family of MCP-schema-and-subagent-context issues
- No maintainer response yet; capped at Unverified per validation rules despite strength of self-directed evidence, since only one reporter is involved

**Workaround(s):**
1. ★ Reduce the number of simultaneously configured MCP servers (per-session tool-schema volume) until full-tool-access sub-agents start returning output again; requires a full session restart to take effect since schema is fixed at session start, not re-evaluated when servers are toggled off mid-session. Confirmed by the reporter via controlled test.
2. Route sub-agent work through a restricted-tool agent type (e.g., `explore`-equivalent) when many MCP servers are configured, since restricted toolsets stay under the schema budget longer — but this defeats the purpose of specialist/full-tool subagent types and is explicitly called out by the reporter as an unsatisfying workaround, not a fix.

**Notes:**
Related family of issues (not independently tracked as separate GC- entries in this pass, but worth cross-referencing in a future scan): #3542 (MCP tool schema exceeds token limit / `contextTier: long_context` reportedly ignored), #2630 (custom agent `mcp-servers` not connected in sub-agent/`--prompt` contexts — investigated during this same pass and found to have been fixed between v1.0.46 and v1.0.47, confirmed by a community member; not tracked as a separate KB entry since it is resolved and this was a first-ever capture pass), #3293 (extension tools not propagated to sub-agents). If MOSAIC subagents accumulate many MCP tool declarations across Skills/Tools injections, this silent-failure mode should be treated as a standing risk — the orchestrator protocol's status-code contract (SUCCESS vs. no response) has no way to detect this failure mode as currently designed, since the harness itself reports no error.

**Needs re-verification** (added run 2, 2026-09-10): Re-checked via GitHub MCP — issue body and both comments are unchanged from the original capture, still open, no maintainer response. Last activity remains 2026-07-30, now exactly 6 weeks stale against a harness that has released multiple versions (1.0.73 → 1.0.83) since the report. No corroboration or refutation found this pass; re-verify again in a future run, ideally with a direct reproduction attempt at MOSAIC given the severity (silent subagent failure with zero error signal).

---

### GC-006: Create-file tool returns invalid/null response and retries forever when content is too large

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/github/copilot-cli/issues/4626 |
| **Reported** | 2026-08-26 |
| **Last Activity** | 2026-08-27 |
| **Confidence** | Unverified |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 1.0.80 (unknown if fixed since) |
| **Latest Platform Version** | v1.0.83 (released 2026-09-04) |
| **Labels** | area:tools |

**Summary:**
When the agent attempts to create a file whose content is too large, the create-file tool call fails with a generic, uninformative validation error (`"path": Required`, `"file_text": Required`) instead of a clear content-size error — and the agent then retries the identical failing call indefinitely rather than recognizing the cause and splitting the output into smaller files. The reporter confirmed the root cause is content size (not a real "path"/"file_text" problem) by manually instructing the agent to split output into smaller files, which immediately resolved it; without that manual intervention, new sessions attempting the same large-output task hit the same infinite-retry loop consistently. No maintainer response or comments; single reporter with a clear, reproducible trigger condition (asking the agent to write one large single-file artifact, e.g. a large implementation plan or research document).

**Impact on Orchestration:**
This directly threatens MOSAIC's Runner-driven workflow stages, which routinely have subagents write large single-file artifacts (`Research.md`, `Plan.md`, `PlanDetails.md`, consolidated review reports) into a run's `Orchestration-{run_id}/` folder. If such an artifact exceeds the undocumented size threshold, the subagent's file-write tool call fails with a generic validation error and the agent can enter an infinite retry loop rather than recovering — silently stalling a headless stage (burning tokens/turns with no forward progress) rather than either succeeding or failing cleanly with an actionable error.

**Evidence:**
- Single reporter, no maintainer acknowledgment, no independent reproduction reports
- Reporter did their own root-cause narrowing (using the CLI itself to investigate the failure, then confirming via manual intervention that splitting the file resolves it) rather than reporting a bare symptom
- Meets the Unverified-inclusion bar: directly threatens a core MOSAIC tool-execution pattern (writing large artifacts), stock infrastructure, and the reproduction condition (ask for one large output file) is concrete and easy to test directly

**Workaround(s):**
1. ★ Instruct the agent (via prompt/system guidance) to break large single-file outputs into multiple smaller files rather than writing one large document — reporter confirmed this reliably avoids the failure. For MOSAIC, this could be built into subagent prompt guidance for artifact-heavy stages (e.g., splitting a very large Research.md or Plan.md into sectioned files) as a preventive measure until an upstream fix ships.

**Notes:**
No confirmed maximum file-size threshold is documented — the exact trigger size is unknown. Needs re-verification on the current version and, ideally, a concrete size threshold determined empirically if MOSAIC hits this in practice, since knowing the threshold would let subagent artifact-writing guidance proactively stay under it.

---

### GC-007: OOM crash (`JavaScript heap out of memory`) on long `--resume` sessions; crash dumps written into user's cwd

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/github/copilot-cli/issues/4699 |
| **Reported** | 2026-09-02 |
| **Last Activity** | 2026-09-10 |
| **Confidence** | Likely |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 1.0.82 (still reported as of 2026-09-10, close to current 1.0.83) |
| **Latest Platform Version** | v1.0.83 (released 2026-09-04) |
| **Labels** | area:sessions, area:context-memory |

**Summary:**
Long `--resume` sessions repeatedly crash with a V8 heap OOM (`Allocation failed - JavaScript heap out of memory`), always pinned right at the process's self-imposed heap cap (the CLI passes `--optimize-for-size`, which caps the heap around 4 GiB regardless of available system RAM). Diagnostic dumps show `old_space` (long-lived, survived-GC data) accounts for nearly all heap usage at crash time, consistent with unbounded per-session state retention (conversation/tool history) rather than a transient spike, and correlates with sessions accumulating substantial CPU time (46 min–2h11m). Separately, the resulting Node diagnostic crash-report JSON files are written into the user's current working directory rather than a tool-owned location, so they appear as untracked files inside whatever git repo the user was working in. Two independent commenters confirm hitting the same OOM pattern; one reports the crash persists even after manually raising the heap limit via `--node-options="--max-old-space-size=6000"`, ruling out the heap cap alone as the root cause and pointing to genuine unbounded memory growth. 5 reactions on the original report.

**Impact on Orchestration:**
Directly threatens session/conversation stability for any long-running or resumed MOSAIC session — whether an extended interactive hub-and-spoke orchestration or a headless Runner pipeline stage that reuses a `--resume`'d session across multiple dispatches. A crash mid-session means total loss of in-memory session state unless externally checkpointed, and the raised-heap-limit test suggests this isn't simply a config knob MOSAIC can tune around — no workaround fully prevents the crash, raising the heap cap only delays it. The crash-dump-in-cwd side effect is a secondary but concrete risk: Runner-driven stages operating inside a git-tracked working tree could have crash artifacts appear as spurious untracked/uncommitted files.

**Evidence:**
- Original reporter provided detailed diagnostic data (heap/RSS metrics across 3 separate crashes, `old_space` breakdown, environment specifics ruling out `NODE_OPTIONS` or system memory pressure as the cause)
- Two independent corroborating comments, one from a different reporter confirming the identical failure pattern, another reporting a variant reproduction after attempting the `--max-old-space-size` workaround and still crashing — with cross-references to two additional related issues (#4686, #4725) suggesting a broader OOM-family pattern
- 5 upvotes/reactions on the original report
- No maintainer acknowledgment yet, but multiple independent reproductions with strong technical evidence support Likely confidence

**Workaround(s):**
1. Restart/checkpoint long-running sessions proactively before they accumulate excessive CPU time or session state, rather than relying on a single `--resume`'d session to persist indefinitely — partial mitigation only, since exact trigger conditions for the underlying leak aren't isolated.
2. Set a tool-owned crash-dump directory if possible (or run from a directory outside the working git repo) to avoid crash-report JSON files polluting a tracked repository — not confirmed configurable in the CLI itself; may require an external wrapper that changes cwd before invoking `copilot`.
3. Raising `--node-options="--max-old-space-size=..."` does NOT reliably prevent the crash (confirmed ineffective by one commenter) — do not rely on this as a fix.

**Notes:**
Still being actively reported as of 2026-09-10 (same day as this run 2 re-verification), against version 1.0.82 — essentially current. Cross-referenced by commenters against #4686 (an async-handle-leak pattern) and #4725 as possibly related; worth a dedicated follow-up pass on the broader OOM/memory-retention family in a future run. Note: the v1.0.83 release notes (2026-09-04) include "Long-running sessions on Linux return freed memory to the system instead of holding gigabytes of it" — this is a plausibly-related fix, but no comment in this thread has confirmed it resolves this specific OOM pattern (the 2026-09-10 corroborating comment is against a build still exhibiting the leak with `--max-old-space-size` raised, and doesn't state whether it was on 1.0.83). Flagged for re-verification on 1.0.83+ in a future run. High priority for MOSAIC given direct relevance to long-running Runner-driven or interactive sessions.

---

### GC-008: Session hangs after work appears complete; Escape recovery enters permanent "Cancelling"

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/github/copilot-cli/issues/2703 |
| **Reported** | 2026-04-14 |
| **Last Activity** | 2026-07-29 |
| **Confidence** | Likely |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 1.0.25 (original report); still reported as of 2026-07-29 |
| **Latest Platform Version** | v1.0.83 (released 2026-09-04) |
| **Labels** | area:sessions, area:input-keyboard |

**Summary:**
After a turn's visible work appears complete, the CLI can hang in a busy/"Thinking" state and never return to a usable prompt. Pressing Escape to try to recover makes it worse: the UI transitions into a permanent `Cancelling` state where normal input and slash commands stop working entirely — the only recovery is killing and restarting the process (`copilot --resume` can then recover the session content, per a corroborating comment). Reproduction is intermittent with no isolated deterministic trigger. A second independent reporter confirms hitting the identical symptom (stuck "Thinking", Escape leads to permanent "Cancelling", process kill required) and links it to a broader cluster of related hang/concurrency reports (#2182 PTY buffer deadlock, #2770, #405, #529, #575), suggesting a shared root cause in stream-draining/abort-signal handling rather than isolated bugs. The reporter's own issue notes this symptom family had previously been closed as fixed through v1.0.21, but recurred on v1.0.25 — an incomplete or regressed fix.

**Impact on Orchestration:**
A hung session with no recovery path except a hard process kill is severe for both MOSAIC execution modes: an interactive hub-and-spoke session would stall indefinitely waiting on a turn that never returns control, and a headless Runner-driven stage invoking the CLI as a subprocess would need external timeout/kill handling to detect and recover from this, since the CLI itself provides no internal escape once stuck. Because the trigger is intermittent and possibly tied to background/sub-agent activity (per the reporter's own note, unconfirmed), this could manifest unpredictably in exactly the multi-tool-call, potentially-subagent-invoking sessions MOSAIC drives.

**Evidence:**
- Two independent reporters describing the identical symptom (post-completion hang, Escape → permanent Cancelling, process kill required)
- Second reporter explicitly frames it as part of a recognized cluster of related hang/concurrency issues with plausible shared root cause (stream-draining, abort-signal propagation) rather than a one-off
- No maintainer acknowledgment or linked fix PR; original report is from April 2026 (1.0.25), corroborating comment from July 2026 — spans several releases with no confirmed resolution
- 2 reactions on the original report

**Workaround(s):**
1. Kill and restart the CLI process when stuck (no clean in-session recovery exists); `copilot --resume` can then be used to recover the session's prior content rather than losing it entirely, per the corroborating commenter.
2. One related/older issue in the cited cluster (#405) reported that using `--disable-parallel-tools-execution` stopped freezes for some users — unconfirmed for this exact issue, but worth testing if MOSAIC hits this pattern, since it may reduce the concurrency conditions believed to trigger the family of hangs.

**Notes:**
Flagged **Needs re-verification** on the current version (1.0.83) — last confirmed sighting was 2026-07-29, roughly 6+ weeks and several releases behind current at time of this scan, and the issue's own history shows this symptom family has been closed-then-recurred at least once already. Given the severity (unrecoverable hang, process kill required) and the plausible link to background/sub-agent activity, this is a high-priority candidate for direct reproduction testing at MOSAIC, especially in Runner-driven headless invocations where an external stage-level timeout/kill-and-retry wrapper would be a prudent structural mitigation regardless of upstream fix status.

---

### GC-009: Non-interactive (`-p`) sessions: tool-call approval silently and permanently revoked mid-session, unrecoverable "Permission denied and could not request permission from user"

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/github/copilot-cli/issues/4433 |
| **Reported** | 2026-08-11 |
| **Last Activity** | 2026-08-27 |
| **Confidence** | Unverified |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 1.0.78, 1.0.79 |
| **Latest Platform Version** | 1.0.83 (released 2026-09-04) |
| **Labels** | none applied |

**Summary:**
In non-interactive `-p`/`--prompt` mode, long sessions (~4-8 minutes, many tool calls) begin silently and permanently denying every write-capable tool call — and, inconsistently, some read-only/diagnostic commands and all MCP tool calls — partway through, even with `--allow-all` plus explicit `--allow-tool` grants already in force. There is no recovery for the remainder of that session. The GitHub issue is marked **Closed / completed**, but it was closed by the reporter themselves with no maintainer engagement, no linked fix/PR, and no confirmation the underlying cause was addressed — the single comment on the thread is unrelated third-party solicitation, not a resolution. Per the "verify closures yourself" rule, this closure does not represent a real fix and the behavior should be treated as still present.

**Impact on Orchestration:**
MOSAIC's Runner pipeline (`Tools/Runner`) drives headless, non-interactive CLI invocations per workflow stage — exactly the `-p`/non-TTY invocation pattern this bug targets. If a workflow stage runs long enough (many tool calls: file reads/writes/edits, subagent-equivalent shell invocations, MCP calls), tool execution could silently and permanently fail partway through with no way to recover within that session, corrupting or truncating the stage's work with an opaque, unrecoverable permission error — even though `--allow-all` was passed. This is a severe, silent tool-execution reliability risk specifically for headless orchestration, MOSAIC's primary automated execution path.

**Evidence:**
- Single reporter, no maintainer or third-party technical confirmation
- Exceptionally thorough reproduction: exact version/environment info, multiple independent runs isolating the variable (long vs. short session, same directory/flags/agent), ruled out workspace trust, permission flags, session/call volume alone, disable-MCP flags, working directory, and active managed/org policy (confirmed via the CLI's own startup log lines)
- Reporter decompiled the shipped Windows bundle and identified a plausible internal code path (`beginManagedSettingsResolution()` / `bypassPermissionsDisabledByPolicy` / a fail-closed branch zeroing `approveAllToolPermissionRequests`) consistent with the observed behavior, though could not fully confirm it fires for this account
- Meets all three Unverified-inclusion bars: threatens a core MOSAIC pattern (headless `-p` tool execution reliability), observed on stock Copilot CLI infrastructure (no custom backend/proxy), and includes concrete, detailed reproduction steps
- Issue closed by the reporter (not a maintainer) with `state_reason: completed` but zero evidence of an actual fix — the only comment is an unrelated product pitch, not a resolution confirmation

**Workaround(s):**
No confirmed workaround. The reporter found none — `--allow-all` combined with explicit `--allow-tool` grants does not prevent it, and the issue includes no maintainer-suggested mitigation.

**Notes:**
Tracked as ACTIVE despite the GitHub "closed/completed" status because the closure is self-administered by the reporter with no maintainer acknowledgment or fix evidence — per CaptureGuide's "don't trust duplicate/closure labels" rule, a stale/unverified closure must be investigated, not taken at face value. This should be re-verified in a future run: check if the reporter left an explanation elsewhere, if related issues have since surfaced, or if a newer CLI version (currently at 1.0.83, three-plus versions past the 1.0.78/1.0.79 report) has addressed it silently. Given the severity for MOSAIC's headless Runner pipeline, recommend prioritizing a re-check and/or direct reproduction attempt at MOSAIC. Thematically adjacent to GC-016 (hook `deny` ignored) and GC-019 (managed setting bypassed) — all concern permission/approval enforcement breaking down in non-interactive mode — worth a combined re-check.

---

### GC-010: No approval required for any tool execution inside Docker sandboxes, even with `/yolo off`

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/github/copilot-cli/issues/4609 |
| **Reported** | 2026-08-26 |
| **Last Activity** | 2026-09-10 |
| **Confidence** | Unverified |
| **Orchestration Impact** | MEDIUM |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 1.0.83 (Docker Sandboxes feature v0.39.0) |
| **Latest Platform Version** | 1.0.83 (released 2026-09-04) |
| **Labels** | area:permissions |

**Summary:**
When running Copilot CLI inside a Docker sandbox (`sbx run copilot`, a Windows-only opt-in sandboxing feature), no tool execution requires approval regardless of the `/yolo` setting — file create/edit/delete, arbitrary shell/curl calls, and running generated scripts all execute without prompting, even though `/yolo show` reports `off` and states individual approval is required. The same CLI running directly on Windows (outside the Docker sandbox) correctly prompts for approval with identical settings. Open, single reporter, no maintainer response yet.

**Impact on Orchestration:**
This is a complete bypass of the tool-approval/permission layer, scoped specifically to the Docker sandbox execution environment. It does not directly affect MOSAIC's primary headless execution path (Runner invokes `copilot` as a plain subprocess, not via the Docker sandbox feature) but would matter significantly if MOSAIC or a user ever adopts Docker sandboxing for isolation — any workflow relying on permission gating (e.g., HITL approval checkpoints, restricted tool scopes for a subagent) would silently have no enforcement at all inside that environment, with tools executing exactly as if `--allow-all` were passed even when it wasn't.

**Evidence:**
- Single reporter, no maintainer acknowledgment or third-party reproduction yet
- Labeled `area:permissions` and `Bug` issue type by the reporter/repo triage, but this alone is not maintainer confirmation
- Clear, specific reproduction steps (install `winget Docker.sbx`, `sbx run copilot`, `/yolo off`, ask for file/network/script operations) and relevant environment details (`permissions.disableBypassPermissionsMode: disable`, no `permissions-config.json`)
- Meets Unverified-inclusion bar: concrete repro steps provided, not a custom backend, and touches a core safety mechanism (tool execution permission gating) — though the Materiality bar is weaker here since MOSAIC's headless Runner does not currently invoke the Docker sandbox feature, hence MEDIUM rather than HIGH impact

**Workaround(s):**
No confirmed workaround reported. Avoiding the Docker Sandboxes feature entirely (running Copilot CLI directly on the host) is the only known mitigation, per the reporter's own comparison.

**Notes:**
Scope is currently narrow — this only reproduces inside the opt-in Docker Sandboxes feature (`sbx run copilot`), which MOSAIC's Runner pipeline does not use by default. Flagged for awareness in case MOSAIC adopts container-based sandboxing for subagent isolation in the future, at which point this would become a HIGH-impact permission-bypass concern. Thematically close to GC-019 (managed policy bypass on non-interactive path) — both show permission enforcement failing to apply in a specific execution mode. Re-check in a future run for maintainer response.

---

### GC-011: Steering message typed into a `preToolUse` "ask" denial prompt is silently dropped, never reaches the agent

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/github/copilot-cli/issues/4237 |
| **Reported** | 2026-07-23 |
| **Last Activity** | 2026-08-13 |
| **Confidence** | Unverified |
| **Orchestration Impact** | MEDIUM |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 1.0.74-4 |
| **Latest Platform Version** | 1.0.83 (released 2026-09-04) |
| **Labels** | area:permissions |

**Summary:**
When a `preToolUse` hook returns `permissionDecision: "ask"`, the CLI shows a permission prompt that lets the human type custom free-text when denying — but that steering text never reaches the agent's session; only a bare denial is registered. The reporter's own follow-up comment compares the CLI's docs against Claude Code's hooks docs and concludes this is a genuine interop bug: the CLI appears to conflate the "ask" and "deny" code paths, since for "deny" the reason text is meant for the agent, but for "ask" the human's own steering input (not just the hook-provided reason) should reach the agent and currently does not. Open, single reporter, no maintainer response.

**Impact on Orchestration:**
This affects human-in-the-loop correction during interactive sessions using `preToolUse` hooks (e.g., MOSAIC's `Catalog/HarnessInjections`/hook-based approval gating, if configured for GHCP CLI). When a human denies a tool call and provides a corrective instruction ("don't touch that file, use X instead"), the agent receives only a bare denial with no context — it must guess why the action was blocked and how to proceed, degrading the effectiveness of HITL approval steps that rely on steering feedback rather than pure allow/deny. This does not affect MOSAIC's headless Runner path (non-interactive `-p` mode cannot answer "ask" prompts at all — see GC-009), so exposure is limited to interactive/harness-native orchestration sessions that use `preToolUse` hooks with `ask` decisions.

**Evidence:**
- Single reporter, no maintainer acknowledgment
- Reporter's own follow-up comment provides comparative documentation evidence (GHCP CLI SDK docs, general GHCP hooks reference, and Claude Code hooks docs) showing the CLI's behavior deviates from its own documented contract and from the equivalent Claude Code mechanism — this is stronger self-corroboration than a typical single-reporter issue, though still not third-party or maintainer confirmed
- Clear, minimal, fully-specified reproduction steps (exact hook JSON, exact invocation) with screenshots
- Meets Unverified-inclusion bar: concrete repro steps, stock infrastructure, and touches a core HITL/tool-execution mechanism relevant to MOSAIC's Approve-gated workflow steps

**Workaround(s):**
No workaround identified in the issue. The human's steering text is lost regardless of prompt wording; there is no documented flag or hook option to force it through.

**Notes:**
Relevant primarily to MOSAIC's interactive/harness-native execution mode when `preToolUse` hooks with `ask` decisions are used for HITL gating — not the headless Runner pipeline (see GC-009 for why `-p` mode can't use "ask" interactively anyway). Related in theme to GC-016 (deny decision not enforced) and GC-017 (ask decision auto-approved by TUI) — all three show `preToolUse` hook verdicts not being carried through to session behavior correctly, though each is a distinct symptom/trigger and is tracked separately per CaptureGuide guidance. Re-check in a future run for maintainer response.

---

### GC-012: `/allow-all` slash command does not suppress bash/shell tool execution prompts

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/github/copilot-cli/issues/2955 |
| **Reported** | 2026-04-24 |
| **Last Activity** | 2026-08-29 |
| **Confidence** | Likely |
| **Orchestration Impact** | MEDIUM |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 1.0.35 (reported); confirmed again on Linux 2026-08-29 without a version stated |
| **Latest Platform Version** | 1.0.83 (released 2026-09-04) |
| **Labels** | area:permissions |

**Summary:**
Running the interactive `/allow-all` slash command mid-session does not suppress the "Do you want to run this command?" approval dialog for bash/shell tool calls — every shell invocation still prompts individually, even after `/reset-allowed-tools` followed by `/allow-all` again. Independently reproduced on both macOS (original reporter) and Linux (later commenter), no maintainer response. Reported against a fairly old version (1.0.35); the Linux confirmation is recent (2026-08-29) but doesn't state a version, so it's unconfirmed whether this persists on the current 1.0.83.

**Impact on Orchestration:**
This is the interactive `/allow-all` slash command, not the CLI launch flag (`--allow-all`/`--allow-all-tools`, tracked separately under GC-009's context) — so it mainly affects MOSAIC's interactive/harness-native orchestration sessions where a human or the orchestrator issues `/allow-all` mid-session expecting subsequent shell tool calls to proceed without prompts. If subagent delegation or tool-heavy stages rely on this to avoid prompt pile-up during an interactive run, they would instead hit repeated approval dialogs, stalling any unattended portion of an otherwise-interactive session. Headless Runner-driven sessions that pass allow flags at launch are less likely affected, since this bug is specific to the slash command.

**Evidence:**
- Two independent reporters on different OSes (macOS, Linux) report the identical symptom with matching repro steps
- Clear, minimal reproduction steps in both reports
- No maintainer acknowledgment or fix PR
- Confidence set to Likely rather than Confirmed: independent reproduction across platforms supports it, but reporter count is low (2) and no maintainer engagement exists

**Workaround(s):**
1. ★ Prefer native built-in tools (`view`, `grep`, `glob`) over bash/shell tool calls where possible, since built-in tools are not subject to the same prompt — per the original reporter (unconfirmed by others, but the only workaround offered).
2. Use launch-time flags (`--allow-all`/`--allow-tool=shell`) instead of the in-session `/allow-all` slash command, since GC-009's investigation suggests the launch flags are handled through a different code path (though that path has its own separate bug — see GC-009).

**Notes:**
Reported against 1.0.35, several releases behind current 1.0.83 (report is roughly 3+ months old) — flagged **Needs re-verification** on a current version, since the only recent confirmation (2026-08-29, Linux) doesn't specify version. Re-check in a future run whether this persists on 1.0.8x.

---

### GC-013: Agent Skill `allowed-tools` frontmatter is ignored in non-interactive (`-p`) mode, causing permission-denied loops

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/github/copilot-cli/issues/3699 |
| **Reported** | 2026-06-05 |
| **Last Activity** | 2026-07-14 |
| **Confidence** | Unverified |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 1.0.60 |
| **Latest Platform Version** | 1.0.83 (released 2026-09-04) |
| **Labels** | area:permissions, area:non-interactive, area:plugins |

**Summary:**
A Skill's `allowed-tools` frontmatter (e.g., declaring `bash`, `shell`, `powershell` as pre-approved) is honored in interactive Copilot CLI sessions but ignored in non-interactive `-p` mode — the declared tools still trigger repeated `Permission denied and could not request permission from user` errors, burning tokens, time, and cost on a mode that can never resolve the denial (no interactive prompt channel exists). Open, single reporter; the one follow-up comment is a non-maintainer design suggestion (propose a preflight capability check unifying interactive/non-interactive policy evaluation) rather than a confirmation of the underlying behavior.

**Impact on Orchestration:**
This directly threatens a core MOSAIC pattern: Skills (`Catalog/Skills/`) are deployed with frontmatter that can declare tool scoping, and MOSAIC's Runner pipeline drives GHCP CLI specifically in non-interactive `-p` mode for headless workflow stages. If a deployed Skill's `allowed-tools` declaration is silently ignored in this mode, any subagent or workflow stage using that Skill would repeatedly hit unrecoverable permission denials on tools it was explicitly declared to have — wasting tokens/turns on futile retries with no way to grant approval (there is no user to prompt), and potentially causing the stage to fail outright or produce degraded partial output.

**Evidence:**
- Single reporter, no maintainer acknowledgment
- One community comment proposes a design fix (unified preflight capability check across interactive/non-interactive modes) but does not independently confirm the reported behavior — it accepts the premise and argues for a specific remediation approach
- Reproduction steps are clear in principle (any skill with `allowed-tools`, compare interactive vs. `-p` invocation) though not maximally detailed
- Meets Unverified-inclusion bar: this squarely threatens a core MOSAIC orchestration pattern (Skills + headless non-interactive execution), is not tied to a custom backend, and the repro condition is concrete and easy to verify directly

**Workaround(s):**
No confirmed workaround reported. The suggested remediation (unified preflight policy evaluator) is a proposed fix design, not a user-side workaround. Possible mitigation to test directly: passing the same tools explicitly via `--allow-tool=<tool>` launch flags in addition to the Skill's `allowed-tools` frontmatter, though this is untested here and GC-009/GC-012 suggest launch-time allow-flags have their own separate reliability problems in `-p` mode.

**Notes:**
No activity since 2026-07-14 (roughly 8 weeks as of this scan) — flagged **Needs re-verification** on current version 1.0.83, since the report is against 1.0.60 (several releases behind). Given the direct relevance to MOSAIC's Skill-deployment + headless-Runner combination, this is a high-priority candidate for direct reproduction testing at MOSAIC rather than relying solely on upstream resolution. Cross-reference GC-009 (broader non-interactive permission-approval failures) and GC-012 (`/allow-all` not suppressing prompts) — all three point to a recurring theme of GHCP CLI's tool-approval system behaving unreliably specifically in automated/non-interactive contexts.

---

### GC-014: allowed_directories in permissions-config.json never loaded

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/github/copilot-cli/issues/4398 |
| **Reported** | 2026-08-07 |
| **Last Activity** | 2026-08-08 |
| **Confidence** | Unverified |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 1.0.79-6 (unknown if fixed since) |
| **Latest Platform Version** | v1.0.83 |
| **Labels** | area:permissions, area:configuration |

**Summary:**
`allowed_directories` entries configured per-location in `~/.copilot/permissions-config.json` are silently ignored — `/list-dirs` never shows them and file reads under those paths still trigger a permission prompt. No maintainer response or independent confirmation yet; reporter gave a precise, minimal repro (create directory, add to config under the location key, observe it's absent from the loaded allowlist).

**Impact on Orchestration:**
MOSAIC's headless Runner pipeline drives non-interactive CLI invocations per workflow stage; pre-configuring allowed directories is the intended way to avoid permission prompts blocking automated file access across subagent working trees. If this config section is never loaded, any headless stage touching a directory outside the default allowlist will hit a permission prompt that cannot be answered non-interactively — the run stalls or fails rather than proceeding.

**Evidence:**
- Single reporter, no maintainer acknowledgment, no independent reproduction yet.
- Reporter provided exact, reproducible CLI steps (mkdir, jq-edit the config, restart, `/list-dirs`, then a file-read prompt) — concrete and mechanical, not environment-specific.
- Labeled `area:permissions`, `area:configuration` by the tracker (filing-time label, not a confirmation).

**Workaround(s):**
No workaround identified in the issue thread. The only directories that reliably appear are the ones auto-derived from the cwd/workspace and a small hardcoded set (`/tmp`, plugin install dir) — pre-staging files inside those default directories is the only currently-known avoidance.

**Notes:**
Related to GC-015 and GC-018 by theme (permission/allowlist config-matching failures in the same subsystem) but the root cause is distinct: this is a config *section* (`allowed_directories`) apparently not being read at all, versus GC-015's tokenization failure on multi-word `commandIdentifiers` and GC-018's CLI-flag pattern matching gap. Not a duplicate of either — tracked separately per CaptureGuide guidance to verify rather than assume from surface similarity. Needs re-verification against a current 1.0.8x build once user has hands-on access.

---

### GC-015: commandIdentifiers with spaces still require approval

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/github/copilot-cli/issues/4150 |
| **Reported** | 2026-07-16 |
| **Last Activity** | 2026-07-17 |
| **Confidence** | Unverified |
| **Orchestration Impact** | MEDIUM |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 1.0.71 (unknown if fixed since) |
| **Latest Platform Version** | v1.0.83 |
| **Labels** | area:permissions, area:configuration |

**Summary:**
A `commandIdentifiers` entry containing a space (e.g. `"make fix"`) in `permissions-config.json` does not match the invoked command — the tool still prompts for approval. None of the documented pattern variants tested (`make:fix`, `make(fix)`, `shell(make fix)`, `bash(make fix)`) work either. Only the bare single-word identifier (`"make"`, matching the whole command regardless of arguments) is honored. No maintainer response; 2 upvotes, no further comments.

**Impact on Orchestration:**
Headless Runner stages that need to pre-approve a *specific* subcommand invocation (rather than blanket-approving an entire executable) cannot do so reliably — forcing operators to either accept broader wildcard approval (weakens the permission boundary MOSAIC relies on for safe automation) or leave the narrower rule in place and eat an unanswerable permission prompt in non-interactive runs.

**Evidence:**
- Single reporter, no maintainer acknowledgment.
- 2 upvotes (below the 5+ threshold for "Likely"), no independent reproduction reports.
- Reporter tested every pattern variant documented in the official CLI reference for this exact feature, which is a strong, mechanical repro.

**Workaround(s):**
1. ★ Use the bare command name without arguments as the identifier (e.g. `"make"` instead of `"make fix"`) — reporter confirmed this matches and auto-approves, at the cost of approving *all* invocations of that command rather than just the intended subcommand.

**Notes:**
Thematically related to GC-014 and GC-018 (permission-matching engine gaps) but a distinct trigger — this is specifically about multi-word/argument tokenization in `commandIdentifiers`, not location-scoped directory config or the `--allow-tool` CLI flag.

**Needs re-verification** (added run 2, 2026-09-10): Re-checked via GitHub MCP — issue body unchanged, still open, zero comments, no maintainer response. Last activity remains 2026-07-17, now roughly 8 weeks stale against a harness that has released multiple versions since the report (1.0.71 → 1.0.83). No corroboration or refutation found this pass; re-verify again in a future run.

---

### GC-016: preToolUse hook `deny` decision does not block tool execution

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/github/copilot-cli/issues/3874 |
| **Reported** | 2026-06-20 |
| **Last Activity** | 2026-07-14 |
| **Confidence** | Unverified |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | GitHub Copilot Chat Extension v1.0.65 (unknown if fixed since) |
| **Latest Platform Version** | v1.0.83 |
| **Labels** | area:permissions, area:plugins |

**Summary:**
A `preToolUse` hook that returns `{"permissionDecision":"deny"}` (with exit code 2), or the `permissionRequest`-style `{"behavior":"deny"}` shape, does not prevent the tool call from executing — the reporter tried every documented deny shape and the tool ran regardless. Two follow-up comments (from third-party users, not maintainers) analyze the likely dispatch-invariant bug but are technical speculation/suggested regression-test framing, not confirmations of independent reproduction or maintainer acknowledgment.

**Impact on Orchestration:**
This is a core safety mechanism for MOSAIC's Hooks-based guardrails: if `preToolUse` hooks are the intended enforcement point for blocking disallowed tool calls (e.g., a logging/policy hook denying a dangerous shell command) and `deny` is silently ignored, automated headless runs can execute commands the operator explicitly tried to block — with no visible failure signal. This is a silent-bypass class of issue, worse than a hard failure because nothing indicates the guard didn't apply.

**Evidence:**
- Single original reporter with a complete, minimal, mechanical repro (a hook binary that always returns deny, tested against multiple response shapes).
- Two additional user comments discuss the likely internal cause (hook output not normalized into one verdict before tool dispatch) but do not report their own independent reproduction — they read as third-party technical analysis, one explicitly bot-generated ("Generated with ax").
- No maintainer response, no linked fix PR.

**Workaround(s):**
No reliable workaround found in the thread. Do not rely on `preToolUse` hook `deny` as a hard gate for tool execution in the current version line; if a hard block is required, it must be enforced outside the CLI (e.g., at the OS/sandboxing layer) until this is confirmed fixed.

**Notes:**
Related in theme to GC-017 (hook `ask` decision auto-approved) — both represent hook-issued permission verdicts not actually gating dispatch — but the trigger conditions differ (this is `deny` never taking effect at all; GC-017 is `ask` flashing then auto-resolving to approved, tied to a specific TUI regression starting v1.0.53). Tracked as separate entries per CaptureGuide guidance since the underlying mechanism (TUI mounting vs. dispatch-path hook resolution) is not confirmed to be the same. Both should be re-checked together if either gets a maintainer response, since a real fix to hook-verdict handling may resolve both.

**Needs re-verification** (added run 2, 2026-09-10): Re-checked via GitHub MCP — issue body and all 3 comments unchanged from the original capture, still open, still no maintainer response or fix PR. Last activity remains 2026-07-14, now roughly 8 weeks stale against a harness that has released multiple versions since the report. No corroboration or refutation found this pass; re-verify again in a future run.

---

### GC-017: preToolUse hook `ask` decision auto-approved by TUI since v1.0.53

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/github/copilot-cli/issues/3590 |
| **Reported** | 2026-05-30 |
| **Last Activity** | 2026-07-14 |
| **Confidence** | Unverified |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 1.0.53–1.0.56 confirmed broken; 1.0.52 confirmed working (regression window; unknown if fixed since 1.0.56) |
| **Latest Platform Version** | v1.0.83 |
| **Labels** | area:permissions, area:plugins |

**Summary:**
When a `preToolUse` hook returns `permissionDecision: "ask"`, the TUI permission dialog renders for roughly 50-79ms and is auto-resolved as "approved" without any user interaction — confirmed via `events.jsonl` timing evidence (79ms resolution on v1.0.56 vs. 3-5 seconds of actual user wait time on v1.0.51/52). Reporter pinpoints this as a regression introduced in v1.0.53, tied to a TUI session-initialization/component-mounting change (also correlated with broken selection-highlight and question-background colors starting the same version). One follow-up comment (non-maintainer) frames the needed fix (tri-state hook verdict normalized before dispatch, binding responses to tool_use_id) but is not an independent reproduction.

**Impact on Orchestration:**
For MOSAIC's interactive/harness-native execution mode, a hook that intends to force a human decision point (`ask`) is silently converted into automatic approval — the exact permission gate the hook was installed to enforce never actually pauses execution. This defeats hook-based human-in-the-loop guardrails in interactive sessions using this version range.

**Evidence:**
- Single reporter, but with unusually strong quantitative evidence (before/after timing comparison across versions using the CLI's own event log, precise version bisection 1.0.52 good / 1.0.53+ broken).
- 1 upvote; one non-maintainer comment discussing likely fix approach, not an independent repro.
- No maintainer acknowledgment or linked fix PR.

**Workaround(s):**
1. ★ Downgrade to v1.0.52 (or earlier) — reporter confirmed the permission dialog correctly waits for user input on that version, and that sessions *created* under 1.0.52 continue to behave correctly even after an auto-update to a broken version, suggesting the regression is tied to session/TUI initialization rather than a global runtime toggle.

**Notes:**
This is TUI-specific (interactive mode) — MOSAIC's headless Runner pipeline doesn't render the TUI, so this exact symptom (dialog flashing) wouldn't manifest there, but any interactive harness-native orchestration session using hooks with `ask` verdicts on an affected version is exposed. See GC-016 for the related `deny`-not-enforced variant; needs re-verification on current 1.0.8x line since the regression window (1.0.53-1.0.56) is well behind the current v1.0.83.

**Needs re-verification** (added run 2, 2026-09-10): Re-checked via GitHub MCP — issue body and its single comment unchanged from the original capture, still open, no maintainer response. Last activity remains 2026-07-14, now roughly 8 weeks stale; the regression window (1.0.53-1.0.56) is now three-plus months behind current v1.0.83, so it remains unconfirmed whether this still reproduces on a current build. Re-verify again in a future run, ideally with a direct reproduction attempt on the current version.

---

### GC-018: --allow-tool='shell(docker ps)' pattern matching fails for non-git commands

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/github/copilot-cli/issues/2610 |
| **Reported** | 2026-04-09 |
| **Last Activity** | 2026-04-16 |
| **Confidence** | Unverified |
| **Orchestration Impact** | HIGH |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 1.0.21 (unknown if fixed since) |
| **Latest Platform Version** | v1.0.83 |
| **Labels** | area:permissions, area:tools |

**Summary:**
The documented `--allow-tool='shell(<subcommand>)'` pattern for pre-approving a specific shell subcommand works for `git` (reporter confirmed `shell(git push)`-style patterns match) but does not work for other commands such as `docker` — `shell(docker ps)` and `shell(docker ps:*)` both fail to match, and the run is denied with "Permission denied and could not request permission from user." No maintainer response; 4 upvotes, no further comments.

**Impact on Orchestration:**
This flag is a primary mechanism for granting narrow, non-interactive permission in headless Runner invocations (`-p`/`--prompt` mode with no human available to answer a prompt). If subcommand-scoped patterns only work for `git` and fail silently for other tools, any headless workflow stage that needs to pre-approve a specific non-git subcommand cannot do so — the stage will hit an unanswerable permission denial and fail outright rather than degrade gracefully.

**Evidence:**
- Single reporter, 4 upvotes (just under the 5+ "Likely" threshold), no maintainer acknowledgment or comments.
- Reporter isolated the difference cleanly: identical syntax works for `git` subcommands, fails for `docker`, suggesting command-specific (possibly hardcoded `git`-only) handling in the pattern matcher rather than a generic mechanism.

**Workaround(s):**
1. Use the bare command name without a subcommand pattern (e.g. `--allow-tool='shell(docker)'` or similar blanket form) if broader approval of the whole tool is acceptable — not explicitly tested in the issue thread, but consistent with the same class of workaround reported for GC-015.
2. For `git` specifically, subcommand-scoped patterns are confirmed to work as documented.

**Notes:**
Related by theme to GC-014 and GC-015 (permission-matching engine gaps across different config surfaces — CLI flag vs. config file location scoping vs. commandIdentifiers tokenization) but distinct root cause: this looks like command-specific parsing logic (git works, others don't) rather than a config-loading or tokenization defect. Tracked separately per CaptureGuide "verify yourself" guidance. Needs re-verification on current version — reported against 1.0.21, current is v1.0.83, a large version gap.

**Needs re-verification** (added run 2, 2026-09-10): Re-checked via GitHub MCP — issue body unchanged, still open, zero comments, no maintainer response. Last activity remains 2026-04-16, now roughly 21 weeks (5 months) stale against a harness many releases past the reported 1.0.21. No corroboration or refutation found this pass; this is now the most stale entry in the harness's active list and a high-priority candidate for direct reproduction at MOSAIC to resolve the long-standing uncertainty.

---

### GC-019: Non-interactive `--yolo` bypasses `disableBypassPermissionsMode` managed setting

| Field | Value |
|-------|-------|
| **Classification** | Bug |
| **Source** | https://github.com/github/copilot-cli/issues/4528 |
| **Reported** | 2026-08-19 |
| **Last Activity** | 2026-08-20 |
| **Confidence** | Unverified |
| **Orchestration Impact** | MEDIUM |
| **Reproduced at MOSAIC** | No |
| **MOSAIC Response** | Unevaluated |
| **Version(s) Affected** | 1.0.80 (unknown if fixed since) |
| **Latest Platform Version** | v1.0.83 |
| **Labels** | area:permissions, area:non-interactive, area:enterprise |

**Summary:**
When an admin-managed setting (`.github-private/copilot/managed-settings.json` with `"disableBypassPermissionsMode": "disable"`) is configured to forbid bypass-permissions mode, the interactive `/allow-all`/`/yolo` command correctly refuses ("Bypass permissions mode has been disabled by policy"), but passing `--yolo -p {prompt}` (non-interactive) silently ignores the managed setting and grants full permission bypass anyway. No maintainer response, no reactions/comments.

**Impact on Orchestration:**
For MOSAIC deployments in enterprise/managed environments, this means an org-level control intended to force human oversight (or at least block full auto-approval) can be silently circumvented simply by driving the CLI non-interactively — exactly the invocation style the Runner pipeline uses. This is a governance/security-posture gap rather than a workflow-breaking defect: it makes headless automation *more* permissive than the administrator intended, which could let an automated run take actions an org explicitly tried to disallow.

**Evidence:**
- Single reporter, 0 reactions, no maintainer acknowledgment, no independent confirmation.
- Reporter provided a precise two-step repro (set the managed setting, invoke with `--yolo -p`) and contrasted it with the correctly-enforced interactive path — internally consistent and mechanical.

**Workaround(s):**
No workaround identified in the issue thread; the managed setting appears to have no effect on the non-interactive path at all. Organizations relying on `disableBypassPermissionsMode` should not assume it constrains headless/`-p` invocations until this is confirmed fixed.

**Notes:**
Distinct from GC-016/GC-017 (which are about hook-issued verdicts not being enforced) and from GC-014/GC-015/GC-018 (config/flag pattern-matching gaps) — this is specifically about an enterprise policy toggle not being consulted at all on the non-interactive code path. Relevant primarily to MOSAIC deployments operating under org-managed Copilot CLI settings; low relevance for unmanaged/individual setups.

Re-checked run 2 (2026-09-10): Re-fetched via GitHub MCP — issue body unchanged, still open, zero comments, no maintainer response. Last activity 2026-08-20, only ~3 weeks stale — under the 6-week re-verification threshold, so no "Needs re-verification" flag added this pass; still worth re-checking on a future run given the enterprise-governance severity.
