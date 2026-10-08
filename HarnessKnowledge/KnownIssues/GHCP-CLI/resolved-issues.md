# GitHub Copilot CLI — Resolved Issues

> Last updated: 2026-10-06 (GC-022 added from MOSAIC experiments)

---

### GC-022: `--agent <id>` dropped at session start (`subagent.deselected`); the default agent runs instead (1.0.91 regression)

| Field | Value |
|-------|-------|
| **Source** | MOSAIC experiments, 2026-10-04 to 2026-10-06 (Windows 11); no upstream issue filed or found. Investigation: `MOSAIC/Investigation-GHCP-AgentDeselect.md` |
| **Fixed In / Status** | 1.0.92 (no explicit release-note entry; see Resolution for the related changelog entries) |
| **Resolution Date** | 2026-10-06 |
| **Original Orchestration Impact** | HIGH |

**Summary:**
With `--agent <id>` in non-interactive mode, CLI 1.0.91 emitted `subagent.deselected` (`data: {}`) before the first `session.custom_agents_updated` event and ran the default Copilot agent, which had none of the custom agent's instructions and none of its tool restrictions. In the Runner this looked like unrelated defects: `protocol response not extractable` (BLOCKED/E501), `reply contains no JSON object`, `unknown action ""`. It made 9 of 23 GHCP MosaicTest runs fail on 2026-10-04: 21% of invocations were dropped during the suite, 100% afterwards. 1.0.87 kept the agent.

The default agent then searched the workspace with tools even for an agent declared `tools: []`. In 6 of 20 dropped runs it found the agent's definition in another harness folder (`.claude/agents/`, `.opencode/agents/`), imitated it, and returned a valid-looking protocol response. So this failure cannot be reliably detected from the reply; only the event stream shows it.

Recorded after the fix (the entry never went through `active-issues.md`), because it was diagnosed and resolved within the same investigation.

**Resolution:**
Deciding probes on 2026-10-06 (40 runs, cells interleaved per round): 1.0.92 through `copilot.cmd` kept the agent 0/20 dropped, in both the cluttered test workspace and a clean copy. 1.0.91 run directly dropped it 20/20, with stdin and with `-p` prompt delivery alike. The full GHCP MosaicTest suite on 1.0.92 then had 0 drops in 118 invocations. The CLI self-updates in the background and `copilot --version` can lag the code that actually runs; the running version is the `pkg\win32-x64\<version>` path in the JSONL events. Raw data: `C:\AI\MOSAIC\HarnessProbes\GhcpAgentDeselect\`. Changelog context (bundled `changelog.json` in `%LOCALAPPDATA%\copilot\pkg\win32-x64\1.0.92\`; the release page shows only the latest version):
- **1.0.88** fixed: "Custom-agent startup now distinguishes model-list load failures from an empty catalog, preventing false unavailable warnings and silent required-agent deselection." So the CLI deliberately deselects a requested agent at startup when it judges the agent's model unavailable, and that check depends on the model list having loaded. Our drop came before the agent list arrived and was intermittent, then constant. That fits the same check running against a model list that had not loaded yet: the 1.0.88 fix covered failed loads, and 1.0.91 apparently still deselected when the list was merely late. **Likely, not confirmed:** the deselect event carries no reason (`data: {}`). Every MOSAIC agent sets `model:` in its frontmatter, so all were exposed to this check.
- **1.0.92** has no entry for it. Changes that touch this path: "Custom agent model entries keep model-bound reasoning effort only when that model is selected" (copilot-agent-runtime #23934), "Remove retired models from the model picker and supported CLI selections" (#24575), "Custom agents launched through ACP task calls now resolve and run correctly" (#24626; matches upstream copilot-cli #5030, "ACP task tool cannot launch custom agents since 1.0.89"), and "Improve first-run startup by extracting the bundled CLI package in a child process" (#24128; related to the package folder still being written during the 10-04 suite). The fix is probably one of these or an unlisted change; the PRs are in a private repository.

MOSAIC follow-up: Runner fails an invocation on `subagent.deselected` (`MOSAIC/Requirements-runner.md`, addendum), so a recurrence after a future self-update is reported as a harness error.
