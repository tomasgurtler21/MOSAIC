---
version: "1.0.0"
harness: codex
---

# Harness Injections — Codex

<HarnessConstraints type="managed">
**Sandbox Mode:** Codex agents run in a sandboxed environment. File system access and tool capability are governed by the `sandbox_mode` field set at deployment time, not by an explicit tool allowlist. An agent deployed with `sandbox_mode = "read-only"` may read files but not write them; `sandbox_mode = "workspace-write"` permits both reads and writes.

**Skills:** Skills are read from the shared `.agents/skills/` directory within the workspace. Each skill resides in its own subdirectory named after the skill key. Load a skill by reading the files in that subdirectory before applying its guidance.

**Single Agent File:** Each deployed agent is a single TOML file at `.codex/agents/<key>.toml`. There is no multi-file agent bundle. Avoid referring to external configuration files that would not be present alongside the TOML file.
</HarnessConstraints>

---

## Design Rationale

- **HarnessConstraints:** Codex differs from the other built-in harnesses in two important ways: (1) it uses a single standalone TOML file per agent rather than a Markdown file, and (2) it expresses tool capability through `sandbox_mode` rather than an explicit per-agent tool list. Agents deployed to Codex must understand these constraints to avoid incorrect assumptions about available tools or file locations.
- **ProtocolExtension, ErrorHandlingExtension, ContextLimits:** Not declared in this harness. Removed at harness level — these are agent-level concerns.

---

## Changelog

| Version | Date | Author | Summary |
|---------|------|--------|---------|
| 1.0.0 | 2026-09-20 | MOSAIC | Initial Codex harness injection content: sandbox_mode capability model, skills root convention, single-file deployment structure |
