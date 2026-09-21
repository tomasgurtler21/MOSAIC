---
version: "1.0.0"
harness: codex
---

# Orchestrator Injections — Codex

---

## Design Rationale

- **HarnessConstraints:** No orchestrator-specific constraint content for Codex at this time. The orchestrator deployed to Codex receives the same sandbox_mode and skills guidance as any other agent through the shared HarnessInjections.md. The orchestrator's `sandbox_mode` is set to `workspace-write` at deployment because it uses the `{tool-permissions}` placeholder, which grants the full capability set.

---

## Changelog

| Version | Date | Author | Summary |
|---------|------|--------|---------|
| 1.0.0 | 2026-09-20 | MOSAIC | Initial empty orchestrator injections file for Codex |
