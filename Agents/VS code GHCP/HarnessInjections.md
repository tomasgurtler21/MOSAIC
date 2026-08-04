---
version: "1.3.0"
harness: vscode-ghcp
---

# Harness Injections — VS Code GHCP

[[DEPLOYED:HarnessConstraints]]
When reading a file with the intent to read it fully, **never assume the file is complete just because the last returned line is blank or ends a section.** Always verify you have reached the true end:
- After reading a chunk, check if you received fewer lines than you requested — that signals the actual end of file
- If you received as many lines as requested, the file likely continues — issue another read starting from where the last one ended
- Keep paginating until you receive a short (or empty) response
- **Exception:** If you are intentionally reading a specific range (e.g., to find a particular function or section), you do not need to read the rest of the file
[[/DEPLOYED:HarnessConstraints]]

[[DEPLOYED:CustomConstraints]]
**Parallel Tool Calls:** Issue multiple independent tool calls in a single response whenever possible. Sequential tool calls are only permitted when a later call depends on the result of an earlier one. This minimises inference API calls to improve speed and reduce cost.
[[/DEPLOYED:CustomConstraints]]

[[DEPLOYED:LanguagePatterns]]
[[/DEPLOYED:LanguagePatterns]]

---

## Design Rationale

- **HarnessConstraints:** VS Code GHCP's file-reading tool returns paginated results without an explicit end-of-file signal, making it easy for agents to silently read a truncated file. The file-reading guidance injected here prevents that class of error for every agent deployed to this harness.
- **CustomConstraints:** The parallel tool calls guidance is placed in `CustomConstraints` (not `HarnessConstraints`) because it is a general behavioural constraint that supplements agent-level custom constraints rather than being a harness-specific file-reading limitation. This matches the attribution in `vscode-ghcp.yaml`. Note: the previous `HarnessInjections.md` incorrectly listed both constraints under the `harness_constraints` heading; this reformat corrects that misattribution by moving the parallel-tool-calls text to its canonical injection name.
- **LanguagePatterns:** Language-specific coding patterns are agent-domain concerns, not harness concerns. No harness-level content is appropriate here regardless of harness. Declared empty to satisfy the canonical injection list.
- **ProtocolExtension, ErrorHandlingExtension, ContextLimits:** Not declared in this harness's yaml injections block. Removed at harness level — these are agent-level concerns.

---

## Changelog

| Version | Date | Author | Summary |
|---------|------|--------|---------|
| 1.3.0 | 2026-07-31 | MOSAIC | Reformat to workflows-style boundary-tag format; correct content-attribution mismatch (Parallel Tool Calls moved from harness_constraints to CustomConstraints, matching vscode-ghcp.yaml) |
