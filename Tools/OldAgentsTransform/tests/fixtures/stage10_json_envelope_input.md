---
id: 101
version: 1.0.0
name: stage10-json-envelope-test
description: Generic agent whose Output Format section retains a JSON response envelope example, for Stage 10 report-coverage acceptance testing
---

# Stage10JsonEnvelopeTest Agent

You are the **Stage10JsonEnvelopeTest** agent.

[INJECTION: identity_extension]

---

## Capabilities

Core task execution.

[INJECTION: language_patterns]

---

## Constraints

- Stay within scope.

---

## Error Handling

Handle errors gracefully.

---

## Output Format

Return a JSON object matching the shape below.

```json
{
  "status_code": "SUCCESS",
  "status_message": "Wrote the requested output."
}
```

---

## Execution Philosophy

Execute with focus.
