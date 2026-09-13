---
version: "1.4.0"
harness: vscode-ghcp
---

# Harness Injections — VS Code GHCP

<HarnessConstraints type="managed">
When reading a file with the intent to read it fully, **never assume the file is complete just because the last returned line is blank or ends a section.** Always verify you have reached the true end:
- After reading a chunk, check if you received fewer lines than you requested — that signals the actual end of file
- If you received as many lines as requested, the file likely continues — issue another read starting from where the last one ended
- Keep paginating until you receive a short (or empty) response
- **Exception:** If you are intentionally reading a specific range (e.g., to find a particular function or section), you do not need to read the rest of the file

**Parallel Tool Calls:** Issue multiple independent tool calls in a single response whenever possible. Sequential tool calls are only permitted when a later call depends on the result of an earlier one. This minimises inference API calls to improve speed and reduce cost.
</HarnessConstraints>

---

## Design Rationale

- **HarnessConstraints:** VS Code GHCP's file-reading tool returns paginated results without an explicit end-of-file signal, making it easy for agents to silently read a truncated file. The file-reading guidance injected here prevents that class of error for every agent deployed to this harness. The parallel tool calls guidance now lives in this same block: it is harness-specific behavioural guidance for every agent deployed to VS Code GHCP, not an agent-level custom constraint, so it belongs alongside the file-reading guidance rather than in a separate block. Note: an earlier reformat had placed the parallel-tool-calls text in a now-removed `CustomConstraints` block on the reasoning that it was a general behavioural constraint distinct from harness-specific file-reading limitations — that reasoning is superseded; `CustomConstraints` was never harness-managed content and has been removed from the canonical vocabulary, so the text is merged back into `HarnessConstraints`, matching the shape already used by GHCP CLI and OpenCode.
- **ProtocolExtension, ErrorHandlingExtension, ContextLimits:** Not declared in this harness's yaml injections block. Removed at harness level — these are agent-level concerns.

---

## Changelog

| Version | Date | Author | Summary |
|---------|------|--------|---------|
| 1.3.0 | 2026-07-31 | MOSAIC | Reformat to workflows-style boundary-tag format; correct content-attribution mismatch (Parallel Tool Calls moved from harness_constraints to CustomConstraints, matching vscode-ghcp.yaml) |
| 1.4.0 | 2026-08-08 | MOSAIC | Correct the 1.3.0 attribution: merge Parallel Tool Calls back into HarnessConstraints and remove the CustomConstraints block; LanguagePatterns block removed (empty, now a project-authored injection name rather than a declared region) |
