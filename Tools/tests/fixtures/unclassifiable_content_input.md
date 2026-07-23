---
id: 1
version: 1.0.0
name: bad-agent
description: A file with an unrecognized section heading that cannot be classified
model: {model-identifier}
tools: []
---

# BadAgent Agent

You are the **BadAgent** agent in a multi-agent orchestration system.

**Goal:** Trigger the unclassifiable-content error path.

[INJECTION: identity_extension]

---

## Unknown Custom Section

This heading is not in the 7 canonical section names recognised by the transformer.
The content below it cannot be mapped to any canonical boundary.
The transformer must report this line to stderr with the file path and line number.

---

## Capabilities

### Core Capabilities
- Do things efficiently

[INJECTION: language_patterns]
