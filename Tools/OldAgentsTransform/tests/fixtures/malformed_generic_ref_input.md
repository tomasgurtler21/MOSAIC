---
version: 1.0.0
this is not a yaml key line
name: malformed-generic-ref
---

# MalformedGenericRef Agent

This file is a generic-reference fixture whose frontmatter contains a bare-word
line that does not match any `key: value` pattern. `_parse_frontmatter` will
return `success=False` with a "Malformed YAML line" error for that line,
exercising the guard added by the Defect 2 fix.

---

## Capabilities

Placeholder capabilities section.

---

## Constraints

Placeholder constraints section.

---
