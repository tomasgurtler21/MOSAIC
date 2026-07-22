# Harness Injections — VS Code GHCP

> **Version:** 1.3.0  
> Harness-level injections baked into CodebaseAgnostic agents. Preserve as-is unless the harness constraint changes.

## Harness-Level Constraint Injections

### harness_constraints

Two harness-level constraints are injected into every subagent's `[INJECTION: harness_constraints]` point:

**1. File Reading — Do Not Assume End of File:**
```
### File Reading — Do Not Assume End of File
When reading a file with the intent to read it fully, **never assume the file is complete just because the last returned line is blank or ends a section.** Always verify you have reached the true end:
- After reading a chunk, check if you received fewer lines than you requested — that signals the actual end of file
- If you received as many lines as requested, the file likely continues — issue another read starting from where the last one ended
- Keep paginating until you receive a short (or empty) response
- **Exception:** If you are intentionally reading a specific range (e.g., to find a particular function or section), you do not need to read the rest of the file
```

**2. Parallel Tool Calls:**
```
**Parallel Tool Calls:** Issue multiple independent tool calls in a single response whenever possible. Sequential tool calls are only permitted when a later call depends on the result of an earlier one. This minimises inference API calls to improve speed and reduce cost.
```

### protocol_extension

None — removed at harness level.

### context_limits

None — removed at harness level.
