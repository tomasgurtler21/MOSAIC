---
version: "1.1.0"
harness: claude-code
---

# Harness Injections — Claude Code

[[INJECTION:HarnessConstraints]]
[[/INJECTION:HarnessConstraints]]

[[INJECTION:LanguagePatterns]]
[[/INJECTION:LanguagePatterns]]

---

## Design Rationale

- **HarnessConstraints:** Claude Code's built-in system prompt already enforces the relevant tool-use and working-directory conventions. Adding a harness-level constraint block would be redundant and risk conflicts with Claude Code's own guidance. Declared with empty content so the injection point is acknowledged and suppressed cleanly.
- **LanguagePatterns:** Language-specific coding patterns are agent-domain concerns, not harness concerns. No harness-level content is appropriate here regardless of harness. Declared empty to satisfy the canonical injection list.
- **ProtocolExtension:** Not declared in this harness. Removed at the harness level — any orchestration-protocol extensions are authored at the agent level where they belong.
- **ErrorHandlingExtension:** Not declared in this harness. Removed at harness level — error handling guidance is agent-specific.
- **ContextLimits:** Not declared in this harness. Removed at harness level — context window guidance depends on model and agent configuration, not harness.

---

## Changelog

| Version | Date | Author | Summary |
|---------|------|--------|---------|
| 1.1.0 | 2026-07-31 | MOSAIC | Reformat to workflows-style boundary-tag format; add explicit empty sections for HarnessConstraints and LanguagePatterns to maintain declared-but-empty semantics |
