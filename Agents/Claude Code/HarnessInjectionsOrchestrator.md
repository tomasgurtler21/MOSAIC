---
version: "1.0.0"
harness: claude-code
---

# Orchestrator Injections — Claude Code

[[INJECTION:HarnessConstraints]]
- Subagents do not have access to skills although you can see them in system instructions. To all subagents provide path to skills root folder, so they can then find their skill by name. Skills are in workspace, not at user folder or any global location. But cwd/.claude/skills.
- `Task` tool description has a lot of additional/duplicate fields to MOSAIC communication protocol. Fill only message according to communication protocol, and do not fill any additional fields. Subagents will ignore them anyway. MOSAIC communicattion protocol always takes precedence.
[[/INJECTION:HarnessConstraints]]

---

## Design Rationale

- **HarnessConstraints:** The orchestrator needs two Claude Code–specific instructions that are irrelevant to subagents: (1) the skills root path (`cwd/.claude/skills`) so the orchestrator can instruct subagents where to find their skills, and (2) the `Task` tool field discipline — the `Task` tool surface has many fields but MOSAIC protocol only uses `message`; subagents ignore the rest. These are orchestrator-operational details, not harness-level constraints for regular agents.

---

## Changelog

| Version | Date | Author | Summary |
|---------|------|--------|---------|
| 1.0.0 | 2026-07-31 | MOSAIC | Initial migration from hand-authored orchestrator content at .claude/agents/orchestrator.md |
