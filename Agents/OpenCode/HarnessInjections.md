---
version: "1.3.1"
harness: opencode
---

# Harness Injections — OpenCode

[[DEPLOYED:HarnessConstraints]]
- **Parallel Tool Calls:** Issue multiple independent tool calls in a single response whenever possible. Sequential tool calls are only permitted when a later call depends on the result of an earlier one. This minimises inference API calls to improve speed and reduce cost.
- **Working Directory vs Workspace Root:** File tool paths resolve relative to the **working directory**, not the workspace root. Orchestration is always at working directory.
[[/DEPLOYED:HarnessConstraints]]

[[DEPLOYED:LanguagePatterns]]
[[/DEPLOYED:LanguagePatterns]]

---

## Design Rationale

- **HarnessConstraints:** OpenCode's tool-call model requires explicit guidance on two points that differ from the default Claude Code harness: (1) parallel tool calls must be encouraged because OpenCode does not automatically batch them, and (2) the working directory / workspace root distinction is a persistent source of path errors in OpenCode sessions. Both constraints are authored here so every subagent deployed to OpenCode receives them without repetition in each agent file.
- **LanguagePatterns:** Language-specific coding patterns are agent-domain concerns, not harness concerns. No harness-level content is appropriate here regardless of harness. Declared empty to satisfy the canonical injection list.
- **ProtocolExtension, ErrorHandlingExtension, ContextLimits:** Not declared in this harness's yaml injections block. Removed at harness level — these are agent-level concerns.

---

## Changelog

| Version | Date | Author | Summary |
|---------|------|--------|---------|
| 1.3.1 | 2026-07-31 | MOSAIC | Reformat to workflows-style boundary-tag format; content preserved verbatim from opencode.yaml injections block |
