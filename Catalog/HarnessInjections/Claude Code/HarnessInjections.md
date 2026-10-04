---
version: "1.3.0"
harness: claude-code
---

# Harness Injections — Claude Code

<HarnessConstraints type="managed">
**Output Artifacts Are Not Report Files:** Every file listed in `output_artifacts` is an orchestration artifact, not a report file. The orchestrator passes it as input to other agents, and your final message never reaches those agents. Write every listed output artifact as a file — review and findings documents included — even where general guidance tells you to return findings in your final message instead of writing them to a file.
</HarnessConstraints>

---

## Design Rationale

- **HarnessConstraints:** Claude Code's built-in subagent prompt instructs subagents not to write report, summary, findings, or analysis `.md` files and to return findings in their final message instead (captured in `HarnessKnowledge/SystemPromptCapture/ClaudeCode/Subagent/SystemPrompt-clean.md`). A MOSAIC review artifact matches that description exactly, so review agents returned `SUCCESS` with their findings in chat and the declared artifact never written — a failure the orchestrator does not catch, because a `SUCCESS` review routes onward. The constraint counters the rule at the same level it is delivered, and frames artifacts as inputs to other agents, which fits the harness's own exception for files written as input to another tool. The dispatch's `output_artifacts` list is also the explicit request that the harness's "never create documentation files unless requested" rule asks for.
- **ProtocolExtension:** Not declared in this harness. Removed at the harness level — any orchestration-protocol extensions are authored at the agent level where they belong.
- **ErrorHandlingExtension:** Not declared in this harness. Removed at harness level — error handling guidance is agent-specific.
- **ContextLimits:** Not declared in this harness. Removed at harness level — context window guidance depends on model and agent configuration, not harness.

---

## Changelog

| Version | Date | Author | Summary |
|---------|------|--------|---------|
| 1.1.0 | 2026-07-31 | MOSAIC | Reformat to workflows-style boundary-tag format; add explicit empty sections for HarnessConstraints and LanguagePatterns to maintain declared-but-empty semantics |
| 1.2.0 | 2026-08-08 | MOSAIC | Remove the LanguagePatterns block: it is now a project-authored injection name rather than a tool-managed deployed region, so this harness no longer declares it |
| 1.3.0 | 2026-10-03 | MOSAIC | Fill HarnessConstraints with the output-artifacts constraint, countering Claude Code's subagent rule against writing findings `.md` files, which led review agents to report in chat instead of writing their artifact |
