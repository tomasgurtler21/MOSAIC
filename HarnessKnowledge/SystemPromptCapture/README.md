# System Prompt Capture

Captures and documents the **harness-injected system prompt** — the instructions each AI coding harness injects into the LLM context before any agent-specific content.

## Why This Matters

- Tool descriptions contain behavioral instructions (e.g., "NEVER write new files unless explicitly required") that can override or conflict with agent instructions.
- Harnesses differ in critical ways — OpenCode encourages parallel subagent spawning; VS Code GHCP reportedly blocks it.
- Understanding the harness scaffold is necessary for writing effective agent instructions and debugging unexpected model behavior.

## Directory Layout

```
HarnessKnowledge/SystemPromptCapture/
├── README.md                              # This file (human context)
├── CaptureGuide.md                        # Agent-facing specs and prompts
├── TestArtifacts/
│   ├── large-file-3000-lines.txt          # Tests read-tool default line limits and truncation
│   └── varied-content-test.txt            # Tests read, edit, grep (special chars, indentation, unicode)
├── Analysis/                              # Cross-harness comparison (produced by architects)
└── {Harness}/
    ├── Subagent/                          # 5 files (see CaptureGuide.md for specs)
    └── PrimaryAgent/                      # Same 5 files, captured from primary agent session
```

Each harness is captured at two tiers — **Subagent** and **PrimaryAgent** — because they can differ. Subagents may have fewer built-in tools (permission restrictions), different reasoning effort settings, or different behavioral instructions. Both must be captured to understand the full picture.

## How to Run a Capture

1. Fill in `{Harness}`, `{Tier}`, and `{context}` in the prompts from `CaptureGuide.md`.
2. For the **Subagent** tier: spawn a subagent with each prompt.
3. For the **PrimaryAgent** tier: run each prompt directly in the primary agent session.
4. Run Steps 1-3 (system prompt capture) first, then Tool Output capture separately.

The prompts are self-contained — agents receive everything they need. Do not point agents at other harness folders or prior captures.

## Tool Landscape

The core tools table and harness-specific tool lists are in `CaptureGuide.md` (the agent needs them for capture). The notes below are operator context for interpreting captures — they are intentionally kept out of the agent-facing guide.

**What to watch for when reviewing captures:**

- **Spawn agent** is the most MOSAIC-critical tool. It has different names, parameter schemas, and behavioral instructions across harnesses. The description text directly affects orchestration behavior (e.g., OpenCode encourages parallel spawning; VS Code GHCP may restrict it).
- **Bash/Run commands** may be unavailable in subagent sessions on some harnesses. If a subagent capture is missing it, the primary agent capture will fill the gap.
- **Ask user** has different capabilities per harness. Some disable it in subagent context (e.g., Claude Code CC-017). Compare both tiers.
- **Web fetch** is available on all harnesses but may be disabled by organization policy. An absence doesn't necessarily mean the harness lacks the tool.

## When to Capture

- **After major harness version updates** — tool descriptions and behavioral instructions change.
- **After adding/removing MCP servers** — to verify MCP tools don't contaminate the built-in capture.
- **When debugging unexpected model behavior** — the harness prompt may explain it.

## Known Limitations

1. **Agent self-reflection is imperfect.** The model reproduces what it "sees" in context, but may miss content it has internalized without being able to quote verbatim. Hook-based capture (e.g., `experimental.chat.system.transform` in OpenCode) would be more precise.
2. **XML escaping.** The system prompt uses XML-like tags that conflict with the model's output format. Bracket notation is a pragmatic workaround but adds indirection.
3. **Truncation.** Very long tool descriptions (e.g., task tool with many agents listed) may be truncated by the Read tool's line limit. The raw capture may be incomplete for these.

## Capture Status

| Harness | Subagent Capture | Primary Capture | Tools JSON | Output Formats | Last Updated |
|---------|-----------------|-----------------|------------|----------------|--------------|
| **Claude Code** | TODO | TODO | TODO | TODO | -- |
| **OpenCode** | TODO | TODO | TODO | TODO | -- |
| **VS Code GHCP** | TODO | TODO | TODO | TODO | -- |
| **GHCP CLI** | TODO | ✅ | ✅ | ✅ | 2026-09-06 |

> First-generation captures exist for OpenCode, VS Code GHCP, and GHCP CLI but have not been migrated to this location. Claude Code has never been captured.
