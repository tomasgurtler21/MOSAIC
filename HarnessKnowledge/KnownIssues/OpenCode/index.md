# OpenCode — Issue Index

> Last updated: 2026-09-10 (run 2)
> Latest platform version: v1.18.30 (released 2026-09-09)

## Active Issues (49)

| ID | Title | Type | Confidence | Impact | Workaround | Reproduced | Response |
|----|-------|------|------------|--------|------------|------------|----------|
| OC-001 | Nested (depth ≥2) subagent permission ask silently dropped — parent hangs forever, interrupt is a no-op | Bug | Confirmed | HIGH | Partial | No | Unevaluated |
| OC-002 | Child-session events carry no parent lineage — external integrations can't distinguish running subagent from blocked root | Bug | Likely | MEDIUM | No | No | Unevaluated |
| OC-003 | Rejecting one of a subagent's multiple pending permission asks cancels the entire subagent | Bug | Unverified | HIGH | No | No | Unevaluated |
| OC-004 | Rejecting a permission ask during parallel subagents applies same rejection to all of them | Bug | Unverified | HIGH | No | No | Unevaluated |
| OC-005 | Foreground subagent failure/cancellation returns no task_id — parent can't resume, must re-dispatch from scratch | Bug | Confirmed | HIGH | Partial | No | Unevaluated |
| OC-006 | Subagent hangs after bash tool call if it left a live detached descendant process | Bug | Likely | HIGH | Partial | No | Unevaluated |
| OC-007 | Task tool's subagent list ignores session-level permission overrides (description mismatch) | Bug | Confirmed | MEDIUM | No | No | Unevaluated |
| OC-008 | Per-subagent model configuration ignored — all subagents run on primary model | Bug | Confirmed | HIGH | Partial | No | Unevaluated |
| OC-021 | Compaction produces empty summary for reasoning models — history silently dropped | Bug | Confirmed | HIGH | Partial | No | Unevaluated |
| OC-022 | `compaction.prune` never trims context in single-turn/headless sessions — compaction thrash every ~10 min | Bug | Confirmed | HIGH | Partial | No | Unevaluated |
| OC-023 | Auto-compaction not a barrier — session loop races ahead, swallows pending input, drops task goal | Bug | Confirmed | HIGH | No | No | Unevaluated |
| OC-024 | MCP tool manifest drops to 0 after compaction — server stays connected, only new session recovers | Bug | Unverified | HIGH | Partial | No | Unevaluated |
| OC-025 | Manual `/compact` (and auto overflow guard) has no context-fit check vs. compaction model — hangs or silent no-fire | Bug | Likely | HIGH | Partial | No | Unevaluated |
| OC-026 | Native auto-compaction never fires on long sessions (`session_context_epoch` stuck at 0) — runaway token/credit burn | Bug | Unverified | HIGH | No | No | Unevaluated |
| OC-027 | Context-overflow errors misclassified as "Network connection lost" — no compaction, no retry, session dies | Bug | Confirmed | HIGH | No | No | Unevaluated |
| OC-028 | `compaction.reserved` silently ignored for models lacking `limit.input` — compaction fires too late, degrades quality/throughput | Bug | Confirmed | HIGH | Partial | No | Unevaluated |
| OC-081 | Injected prompts without agent/model silently switch session's agent/model | Bug | Confirmed | HIGH | No | No | Unevaluated |
| OC-082 | Agent frontmatter `permission` merges additively instead of replacing defaults | Bug | Unverified | HIGH | Partial | No | Unevaluated |
| OC-083 | `OPENCODE_CONFIG_DIR` overrides (not adds to) global AGENTS.md path | Bug | Confirmed | HIGH | Partial | No | Unevaluated |
| OC-084 | `permission.ask` plugin hook never triggers (dead since ~v1.2.x) | Bug | Confirmed | HIGH | Partial | No | Unevaluated |
| OC-085 | Wildcard `"*": deny` intermittently overrides specific `allow` rules (long-running) | Bug | Confirmed | HIGH | Partial | No | Unevaluated |
| OC-086 | Plugin-registered agent permissions can't override root default deny | Bug | Likely | HIGH | Partial | No | Unevaluated |
| OC-087 | `--auto` auto-approval doesn't cascade to subagents — hangs indefinitely | Bug | Confirmed | HIGH | Partial | No | Unevaluated |
| OC-088 | Built-in Skills hardcode `/tmp`, ignore AGENTS.md temp-dir instructions | Bug | Unverified | MEDIUM | No | No | Unevaluated |
| OC-089 | V2 `subagent` tool doesn't enforce per-target permissions — actual execution bypass, not just description mismatch | Bug | Confirmed | HIGH | No | No | Unevaluated |
| OC-090 | Task subagents inherit stale parent denies; separately, subagent's own permission block can be bypassed based on caller identity | Bug | Confirmed | HIGH | Partial | No | Unevaluated |
| OC-091 | `task` tool entirely absent from tool list under certain permission configs — total loss of delegation, not partial | Bug | Likely | HIGH | No | No | Unevaluated |
| OC-092 | Cancelling a subagent doesn't kill its spawned child processes — resource/safety risk | Bug | Unverified | HIGH | No | No | Unevaluated |
| OC-093 | OpenCode Go advertises 1M context for glm-5.2 but routes to 262K backends; overflow misclassified, session bricked | Bug | Unverified | HIGH | No | No | Unevaluated |
| OC-094 | Subagent (Task-tool) sessions never granted execution permission for MCP tools — model sees tools, every call denied | Bug | Confirmed | HIGH | Yes | No | Unevaluated |
| OC-095 | Parallel tool-call turn finalizer races a slow MCP tool — turn never completes, slow tool stuck at running forever | Bug | Unverified | HIGH | Partial | No | Unevaluated |
| OC-096 | No timeout/idle-watchdog on any LLM stream (subagent or primary) — stalled response hangs session forever | Bug | Confirmed | HIGH | Partial | No | Unevaluated |
| OC-097 | Plugin agent manifest `tools: {question: true}` doesn't override hardcoded default-deny — tool silently unavailable | Bug | Unverified | MEDIUM | Yes | No | Unevaluated |
| OC-098 | Truncation is undocumented/unconfigurable, gives misleading recovery instructions for disabled tools, inconsistent hook ordering | Bug | Likely | MEDIUM | Partial | No | Unevaluated |
| OC-041 | Write tool fails silently on large files (~1000+ lines) | Bug | Confirmed | HIGH | Yes | No | Unevaluated |
| OC-042 | Write tool has no guard against empty content, silently overwrites existing files with nothing | Bug | Confirmed | HIGH | Partial | No | Unevaluated |
| OC-043 | Tool output truncation drops the most important content (background_output synthesis, read offset, grep head_limit) | Bug | Likely | HIGH | Partial | No | Unevaluated |
| OC-044 | Bash tool hangs indefinitely on commands that spawn background/daemon child processes | Bug | Confirmed | HIGH | Partial | No | Unevaluated |
| OC-045 | Tool-output encode failure in one concurrent tool call marks unrelated sibling tool calls as failed | Bug | Confirmed | HIGH | No | No | Unevaluated |
| OC-046 | glob (V2) drops the truncation flag — model can't tell results were limited | Bug | Confirmed | MEDIUM | Partial | No | Unevaluated |
| OC-047 | grep tool silently returns empty results when the search path does not exist | Bug | Confirmed | HIGH | Partial | No | Unevaluated |
| OC-048 | Plan mode file-write restriction is prompt-level only — model can bypass it via bash | Bug | Confirmed | HIGH | Partial | No | Unevaluated |
| OC-061 | MCP tools connected but not exposed to agent (stale tool-set cache) | Bug | Likely | HIGH | Partial | No | Unevaluated |
| OC-062 | MCP tool errors (`isError: true`) reach model as bare `Error executing tool <name>` | Bug | Confirmed | HIGH | No | No | Unevaluated |
| OC-063 | MCP tool schemas not sanitized for Anthropic (root anyOf/oneOf/allOf 400s); MCP tools skip `tool.definition` hook | Bug | Confirmed | HIGH | Yes | No | Unevaluated |
| OC-064 | MCP tool-list failures collapse to constant "Failed to get tools" string, underlying error discarded | Bug | Confirmed | HIGH | No | No | Unevaluated |
| OC-065 | Local MCP servers can silently fail/crash mid-session, no restart mechanism | Bug | Unverified | MEDIUM | Partial | No | Unevaluated |
| OC-066 | Parallel MCP tool calls cause CPU saturation + event-loop blocking (no yieldNow) | Bug | Likely | MEDIUM | No | No | Unevaluated |
| OC-067 | MCP tool-call can hang session indefinitely, no timeout, `/interrupt` non-functional (deadlock) | Bug | Likely | HIGH | Partial | No | Unevaluated |

## Resolved / Retired Issues (0)

> Resolved entries older than 3 months are automatically removed.

| ID | Title | Fixed In / Status | Resolution Date |
|----|-------|-------------------|-----------------|

---

## Quick Summary

OpenCode (`anomalyco/opencode`) is a very high-velocity repo. Run 2 was a full re-verification pass: all 49 entries were re-fetched from GitHub and re-assessed individually. Result: **zero entries closed, fixed, or reclassified** — every issue captured at run 1 is still open and still accurate as described. The pass did strengthen evidence on one entry and surfaced a clear staleness pattern worth flagging (see below), but the overall landscape and its six themes are unchanged from the original capture.

- **Subagent permission enforcement is not just unreliable delivery — it can be an outright bypass.** Beyond permission-ask requests getting dropped, mis-routed, or hanging with `/interrupt` as a no-op (OC-001, OC-003, OC-004, OC-006, OC-067, OC-096 — the last being the best-evidenced root cause: no timeout/idle-watchdog anywhere in OpenCode's LLM-stream path), verification turned up confirmed cases where a denied subagent target still executes (OC-089), a subagent's own declared permission block gets silently bypassed depending on which agent dispatched it (OC-090), and the delegation tool can be entirely absent from the tool list under some permission configs (OC-091). Cancelling a subagent also doesn't reliably kill its spawned child processes (OC-092). Failed/cancelled subagents return no resumable handle, forcing full re-dispatch and progress loss (OC-005). This is the single most dangerous cluster for MOSAIC's hub-and-spoke model — it threatens both the reliability and the safety guarantees of the orchestrator↔subagent dispatch loop. **Run 2 found this entire cluster has gone quiet on the tracker**: OC-091, OC-092, OC-093, and OC-094 are all now 6+ weeks stale with zero maintainer engagement (OC-094 despite 5 separate failed fix-PR attempts and 7+ independent reporters) — a pattern that looks like low maintainer bandwidth specifically on subagent-permission/delegation issues, not evidence the behaviors have gone away.
- **Context compaction is unreliable across multiple failure modes**, not just one bug: summaries silently come back empty on reasoning models (OC-021), the overflow guard misfires or never fires depending on model metadata (OC-025, OC-026, OC-028, plus a sibling code-path bug noted in OC-028), headless/single-turn sessions thrash on it (OC-022), and it isn't a real barrier — the model can act on stale/dropped state mid-compaction (OC-023). Overflow itself can also be misdiagnosed as a network error and kill the session outright (OC-027), and a provider can silently advertise more context than it actually honors, triggering the same failure (OC-093). Any MOSAIC long-running or headless Runner session is exposed here.
- **MCP tool integration has several independent silent-failure points, spanning discovery, permissions, and concurrency.** Tools connect but never surface, sometimes because of a startup caching bug confirmed by direct evidence (OC-061), or disappear after compaction (OC-024), or a server dies mid-session with no recovery (OC-065). Separately — and distinctly, per verification — subagent (Task-tool) sessions can see MCP tools but have every call denied due to a permission-scoping gap that's had 5 failed fix attempts over 4+ months and is now stale with no new activity (OC-094), and a parallel-tool-call race can drop a slow MCP tool's result entirely when a faster sibling finishes first (OC-095). Tool-list failures collapse to one unhelpful string (OC-064) and real error messages get truncated to nothing (OC-062).
- **Permission-system evaluator bugs undermine the model MOSAIC relies on for tool scoping.** Wildcard deny rules intermittently clobber specific allows — confirmed as a `findLast()`/last-match-wins ordering defect (OC-085) — and plugin-registered permissions can't override a root default deny (OC-086), with a related but distinct coverage gap where plugin-agent tool grants are silently ignored because plugin agents never get the built-in permission override (OC-097, now stale as of run 2). Agent frontmatter permission blocks merge instead of replace (OC-082), and the entire `permission.ask` plugin hook has never fired since early versions — confirmed via three independent reports including a live probe test and the exact removal commit (OC-084). Most consequential for automated runs: `--auto` does not cascade to subagents at all (OC-087), meaning any headless multi-agent OpenCode run risks hanging on a subagent permission prompt nobody is watching.
- **Tool execution has multiple silent-failure and hang modes** beyond permissions: writes can silently no-op on large files or on empty content (OC-041, OC-042), truncation flags/limits are dropped or bypassed inconsistently across grep/glob/background output in both directions — false negatives and false positives share the same root cause in `ripgrep.ts` (OC-043, OC-046, OC-047, OC-098), one failed concurrent tool call can falsely fail its siblings (OC-045), and bash hangs indefinitely on anything that spawns a detached/daemon child process — a bug pattern that has resurfaced repeatedly: run 2 verification found **two additional previously-unnoted failed fix PRs** (#20901, #21942) beyond the three already tracked, meaning this specific mechanism has now failed to stay fixed across at least 5 separate attempts (OC-044, corroborated by OC-096's broader watchdog-gap analysis).
- **Instruction/config injection has correctness gaps that affect MOSAIC's three-layer composition model**: injected prompts can silently switch the session's agent/model out from under the caller (OC-081), config-directory overrides silently replace rather than extend the global AGENTS.md path (OC-083), and built-in Skills hardcode `/tmp` regardless of AGENTS.md instructions to use a different temp directory (OC-088) — directly analogous to MOSAIC's own scratchpad-instead-of-/tmp convention.

**Duplicate-label verification caught real gaps (run 1):** a targeted re-investigation of ~25 issue numbers that prior batches (or GitHub's own bot) had flagged as duplicates of existing entries found 12 were genuinely distinct and independently relevant (OC-089 through OC-098, plus upgrades to OC-008's confidence and OC-028's/OC-044's/OC-046's evidence). Several of these were *more severe* than the entry they'd been clustered against (an actual permission bypass filed near a cosmetic description-mismatch bug; a total tool-list absence filed near a partial one). See `CaptureGuide.md`'s "Don't Trust Duplicate/Closure Labels" section — this pattern is now a standing instruction for future capture/update passes.

**Needs re-verification (flagged in run 2, 6+ weeks stale with zero maintainer engagement):** OC-091, OC-092, OC-093, OC-094, OC-097. Worth a priority re-check on the next pass, or direct MOSAIC reproduction given several are HIGH impact.

**Not yet investigated — flagged for a future discovery pass:** the #31235/#13841/#11865/#33028 cluster (cross-referenced from OC-067's notes as sharing the same "unrecoverable hang, non-functional interrupt" root cause) hasn't been opened and read individually in any pass yet — candidate for consolidation or a new standalone entry.

## Recommended Mitigations

- **Treat subagent permission enforcement as unreliable, not just permission-ask delivery** — until OC-089/090/091 land, don't assume a denied permission or a scoped tool list is actually enforced at the execution layer for dispatched subagents; verify with a canary call if the subagent's task is sensitive. Combine with an external watchdog/timeout around subagent dispatch (OC-096 shows there's no internal one) rather than trusting the session to progress or fail loudly (covers OC-001, OC-003, OC-004, OC-006, OC-067, OC-092, OC-096).
- **Treat `--auto`/non-interactive permission mode as unreliable for subagents** until OC-087 lands — critical for MOSAIC's headless Runner pipeline.
- **Don't rely on OpenCode's automatic compaction for long-running headless sessions.** Either checkpoint/summarize state externally at the MOSAIC protocol layer, or cap session length well below the model's *actually honored* context limit — don't trust provider-advertised limits at face value (OC-021/022/023/025/026/027/028/093).
- **Verify writes/edits after the fact for anything safety-critical** — don't trust a `success` tool-call status alone for `write`/`edit` on large files (OC-041, OC-042); a cheap read-back check is the practical mitigation until fixed.
- **Avoid granting broad wildcard permissions and expect deny rules to be unreliable** (OC-082, OC-085, OC-086, OC-097) — prefer minimal, explicit per-agent permission sets over `"*"` patterns, and don't build automation logic that depends on `permission.ask` plugin interception (OC-084), since it doesn't fire.
- **Assume MCP tool availability and MCP tool permission can both silently degrade mid-session** — if an MCP-dependent subagent produces suspiciously few/no tool calls (or every MCP call fails), don't assume the task didn't need them; check for OC-061/062/064/065/094/095 symptoms before trusting a "success" or absence-of-error response.
- **Avoid bash commands that launch daemons or background processes with open stdio inside OpenCode-run subagents** (OC-044, OC-092, OC-096) — redirect output to a file and detach explicitly, or the tool call/subagent itself may hang indefinitely with no internal recovery. This mechanism has now resisted at least 5 separate fix attempts, so treat it as durable rather than expecting an imminent fix.
- **When reviewing this KB for action items, don't skip an entry because its title looks like a duplicate of another** — per the verification pass above, distinct entries with adjacent numbers (e.g. OC-007/089/090/091, OC-043/046/098) often describe materially different failure modes filed under similar-sounding titles.
- **Don't read tracker silence as resolution** — the OC-091/092/093/094/097 cluster's lack of recent activity reflects low maintainer bandwidth on subagent-permission issues specifically, not that the behaviors stopped occurring; treat "Needs re-verification" flags as a prompt to re-check, not a downgrade in real-world risk.
