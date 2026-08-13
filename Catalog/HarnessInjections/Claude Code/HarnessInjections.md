---
version: "1.2.0"
harness: claude-code
---

# Harness Injections — Claude Code

[[DEPLOYED:HarnessConstraints]]
[[/DEPLOYED:HarnessConstraints]]

---

## Design Rationale

- **HarnessConstraints:** Claude Code's built-in system prompt already enforces the relevant tool-use and working-directory conventions. Adding a harness-level constraint block would be redundant and risk conflicts with Claude Code's own guidance. Declared with empty content so the injection point is acknowledged and suppressed cleanly.
- **ProtocolExtension:** Not declared in this harness. Removed at the harness level — any orchestration-protocol extensions are authored at the agent level where they belong.
- **ErrorHandlingExtension:** Not declared in this harness. Removed at harness level — error handling guidance is agent-specific.
- **ContextLimits:** Not declared in this harness. Removed at harness level — context window guidance depends on model and agent configuration, not harness.

---

## Changelog

| Version | Date | Author | Summary |
|---------|------|--------|---------|
| 1.1.0 | 2026-07-31 | MOSAIC | Reformat to workflows-style boundary-tag format; add explicit empty sections for HarnessConstraints and LanguagePatterns to maintain declared-but-empty semantics |
| 1.2.0 | 2026-08-08 | MOSAIC | Remove the LanguagePatterns block: it is now a project-authored injection name rather than a tool-managed deployed region, so this harness no longer declares it |
