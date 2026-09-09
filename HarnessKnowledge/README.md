# Harness Knowledge

Knowledge about the AI coding harnesses (platforms) that MOSAIC has been deployed on. The orchestration system itself is **harness-agnostic** — this folder documents the harnesses we explicitly support with tooling, captures, and issue tracking.

## Tracked Harnesses

| Harness | Folder Name | GitHub Repository | ID Prefix |
|---------|-------------|-------------------|-----------|
| **Claude Code** | `ClaudeCode` | `anthropics/claude-code` | CC- |
| **GitHub Copilot CLI** | `GHCP-CLI` | `github/copilot-cli` | GC- |
| **OpenCode** | `OpenCode` | `anomalyco/opencode` | OC- |
| **VS Code GitHub Copilot** | `VsCodeGHCP` | `microsoft/vscode` | VC- |

The **Folder Name** column is the canonical identifier used in all subdirectories under `HarnessKnowledge/`. Use these exact names when creating harness-specific folders here.

> **Note on OpenCode:** Previously published as `sst/opencode` and sometimes referenced as `opencode-ai/opencode`. Only `anomalyco/opencode` is current.

## Directory Layout

```
HarnessKnowledge/
├── README.md                          # This file — canonical harness list
├── SystemPromptCapture/               # Harness system prompt documentation
│   ├── CaptureGuide.md                # Agent-facing specs and prompts
│   ├── TestArtifacts/                 # Shared test files for captures
│   └── {Harness}/                     # Per-harness captures (Subagent + PrimaryAgent)
└── KnownIssues/                       # Harness issue knowledge base (bugs, limitations, quirks)
    ├── CaptureGuide.md                # Agent-facing format specs
    └── {Harness}/                     # Per-harness issue tracking
        ├── index.md                   # Quick-reference index
        ├── active-issues.md           # Currently active issues
        └── resolved-issues.md         # Issues that were active and later resolved
```

## Who Uses This

- **Harness Injection authors** — the primary consumer. Issue knowledge and system prompt captures inform what goes into `Catalog/HarnessInjections/{Harness}/` — the harness-specific constraints and workarounds injected into deployed agents
- **Harness Issue Hunter** (`Catalog/UtilityAgents/harness-issue-hunter.md`) — maintains KnownIssues
- **System Prompt Capture agents** — populate SystemPromptCapture
