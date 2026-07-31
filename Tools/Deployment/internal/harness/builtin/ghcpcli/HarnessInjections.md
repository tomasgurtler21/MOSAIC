---
version: "1.2.0"
harness: ghcp-cli
---

# Harness Injections — GHCP CLI

[[INJECTION:HarnessConstraints]]
**Parallel Tool Calls:** Issue multiple independent tool calls in a single response whenever possible. Sequential tool calls are only permitted when a later call depends on the result of an earlier one. This minimises inference API calls to improve speed and reduce cost.
[[/INJECTION:HarnessConstraints]]

[[INJECTION:LanguagePatterns]]
[[/INJECTION:LanguagePatterns]]

---

## Design Rationale

- **HarnessConstraints:** GHCP CLI does not automatically encourage parallel tool calls. The constraint is injected at harness level so every subagent deployed to GHCP CLI receives it without requiring per-agent duplication.
- **LanguagePatterns:** Language-specific coding patterns are agent-domain concerns, not harness concerns. No harness-level content is appropriate here regardless of harness. Declared empty to satisfy the canonical injection list.
- **ProtocolExtension, ErrorHandlingExtension, ContextLimits:** Not declared in this harness's yaml injections block. Removed at harness level — these are agent-level concerns.

---

## Changelog

| Version | Date | Author | Summary |
|---------|------|--------|---------|
| 1.2.0 | 2026-07-31 | MOSAIC | Reformat to workflows-style boundary-tag format; content preserved verbatim from ghcp-cli.yaml injections block |
