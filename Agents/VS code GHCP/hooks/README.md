# VS Code GitHub Copilot Subagent Logger Hooks

This folder contains installation instructions for the subagent logger hooks for VS Code GitHub Copilot.

> **Important:** VS Code GitHub Copilot uses the same hook format as Claude Code. The actual hook scripts are located in `../Claude Code/hooks/` and should not be duplicated here.

## Prerequisites

VS Code hooks must be explicitly enabled:

```json
{
  "chat.hooks.enabled": true
}
```

Add this to your VS Code settings. To verify, type `/hooks` in the Copilot Chat.

## Overview

VS Code GitHub Copilot reads Claude Code configuration files directly, including:
- `.claude/settings.json` - Hook configurations
- `.claude/hooks/*.ps1` - Hook scripts

This means the Claude Code hooks work in VS Code GHCP without modification.

## Installation

### Option 1: Use Claude Code Setup (Recommended)

Follow the installation instructions in [`../Claude Code/hooks/README.md`](../Claude%20Code/hooks/README.md). The same `.claude/settings.json` and `.claude/hooks/` structure works for both platforms.

### Option 2: Use .github/hooks/ Location

If you prefer VS Code's native hook location:

#### Step 1: Copy Hook Scripts

```powershell
# From your project root
mkdir -p .github\hooks
copy "path\to\Agents\Claude Code\hooks\*.ps1" .github\hooks\
```

#### Step 2: Create Hook Configuration

Create `.github/hooks/subagent-logger.json`:

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Task",
        "hooks": [
          {
            "type": "command",
            "command": "powershell.exe -ExecutionPolicy Bypass -File \".github/hooks/capture-subagent-input.ps1\""
          }
        ]
      },
      {
        "matcher": "Agent",
        "hooks": [
          {
            "type": "command",
            "command": "powershell.exe -ExecutionPolicy Bypass -File \".github/hooks/capture-subagent-input.ps1\""
          }
        ]
      }
    ],
    "SubagentStop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "powershell.exe -ExecutionPolicy Bypass -File \".github/hooks/capture-subagent-output.ps1\""
          }
        ]
      }
    ]
  }
}
```

## VS Code Specific Configuration

### Enable Hooks

```json
{
  "chat.hooks.enabled": true
}
```

### Hook File Locations

VS Code GHCP searches for hooks in this priority order:

| Location | Description | Shared? |
|----------|-------------|---------|
| `.github/hooks/*.json` | Project-specific hooks | Yes (Git) |
| `.claude/settings.local.json` | Local workspace hooks | No |
| `.claude/settings.json` | Workspace-level hooks | Yes (Git) |
| `~/.claude/settings.json` | User-level hooks (global) | No |

### Additional Hook Locations Setting

You can add custom hook locations via VS Code settings:

```json
{
  "chat.hookFilesLocations": [
    "C:\\MyShared\\hooks"
  ]
}
```

## Viewing Hook Diagnostics

1. Open VS Code's Chat view
2. Right-click in the chat area
3. Select **Diagnostics**
4. Look for the "hooks" section to see loaded hooks and any errors

## Viewing Hook Output

1. Open the **Output** panel (View → Output)
2. Select **GitHub Copilot Chat Hooks** from the dropdown

## Known Differences from Claude Code

| Aspect | VS Code GHCP | Claude Code |
|--------|-------------|-------------|
| Matchers | May be ignored (runs for all tools) | Fully functional |
| Hook location | `.github/hooks/*.json` OR `.claude/settings.json` | `.claude/settings.json` only |
| Agent ID field | `agent_id` | Not provided |
| Transcript format | `.json` | `.jsonl` |

### Matcher Limitation

As of VS Code 1.109, matchers may be ignored, meaning hooks run for ALL tool invocations regardless of the matcher pattern. The PowerShell scripts handle this by:
- Only processing when `tool_name` is `Task` or `Agent`
- Passing through other tools without action

This is handled automatically - no changes needed to the scripts.

## Troubleshooting

See the troubleshooting section in [`../Claude Code/hooks/README.md`](../Claude%20Code/hooks/README.md#troubleshooting).

### VS Code Specific Issues

| Issue | Solution |
|-------|----------|
| Hooks not loading | Verify `chat.hooks.enabled: true` in settings |
| No output panel | Update to VS Code 1.109.3+ |
| Hooks run for wrong tools | Expected - VS Code may ignore matchers; scripts filter internally |

## Log Output

Logs are written to the same `OrchestrationLogs/` folder structure as Claude Code. See the main README for details:

```
OrchestrationLogs/
├── 00_orchestrator_session.md     # Orchestrator session transcript
├── subagent-activity.jsonl        # Machine-readable activity log
└── {agent_instance_id}/
    ├── 01_input.md                # Invocation prompt
    ├── 02_output.md               # Subagent response
    └── 03_session.md              # Full subagent session
```

## Related Documentation

- [VS Code Copilot Hooks](https://code.visualstudio.com/docs/copilot/customization/hooks) - Official documentation
- [Claude Code Hooks README](../Claude%20Code/hooks/README.md) - Main hook implementation
- [VS Code Hooks Research](../../Development/Research/vscode-copilot-hooks-research.md) - Detailed research
