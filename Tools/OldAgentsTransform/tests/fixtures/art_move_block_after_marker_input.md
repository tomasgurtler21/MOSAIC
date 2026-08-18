---
id: 7
version: 1.0.0
name: artifact-move-agent-after
description: Generic agent with an after-marker artifact block for OutputArtifactTemplate order-independence testing
model: {model-identifier}
tools: [file_read, file_write]
---

# ArtifactMoveAgentAfter Agent

You are the **ArtifactMoveAgentAfter** agent.

[INJECTION: identity_extension]

---

## Capabilities

### Core Capabilities
- Do useful things

[INJECTION: language_patterns]
[INJECTION: codebase_context]
[INJECTION: output_artifact_template]

### Design Artifact Structure

Your output artifact must follow this structure:

- `run_id` — copied verbatim from the task invocation's `run_id` field
- `created_by` — your own `agent_instance_id`
- `human_approved` — `false`

---

## Constraints

- Stay within scope

[INJECTION: harness_constraints]
[INJECTION: custom_constraints]

---

## Error Handling

- Retry transient errors

[INJECTION: error_handling_extension]

---

## Execution Philosophy

- Context Management: Dedicate full context window.
[INJECTION: context_limits]
- Quality over Completeness.
