# Harness Injections — OpenCode

> **Version:** 1.3.1  
> Harness-level injections baked into CodebaseAgnostic agents. Preserve as-is unless the harness constraint changes.

## Harness-Level Constraint Injections

### harness_constraints

**Parallel Tool Calls:** Issue multiple independent tool calls in a single response whenever possible. Sequential tool calls are only permitted when a later call depends on the result of an earlier one. This minimises inference API calls to improve speed and reduce cost.

**Working Directory vs Workspace Root:** File tool paths resolve relative to the **working directory**, not the workspace root. Orchestration is always at working directory.

### protocol_extension

None — removed at harness level.

### error_handling_extension

None — removed at harness level.

### context_limits

None — removed at harness level.
