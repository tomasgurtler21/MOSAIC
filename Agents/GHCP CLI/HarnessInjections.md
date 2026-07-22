# Harness Injections — GHCP CLI

> **Version:** 1.2.0  
> Harness-level injections baked into CodebaseAgnostic agents. Preserve as-is unless the harness constraint changes.

## Harness-Level Constraint Injections

### harness_constraints

**Parallel Tool Calls:** Issue multiple independent tool calls in a single response whenever possible. Sequential tool calls are only permitted when a later call depends on the result of an earlier one. This minimises inference API calls to improve speed and reduce cost.

### protocol_extension

None — removed at harness level.

### error_handling_extension

None — removed at harness level.

### context_limits

None — removed at harness level.
