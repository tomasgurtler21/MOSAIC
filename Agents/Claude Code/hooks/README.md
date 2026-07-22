# Claude Code Subagent Logger Hooks

PowerShell hooks for Claude Code (and VS Code GitHub Copilot) that automatically log subagent invocations in the Multi-Agent Orchestration System.

## Overview

When the orchestrator invokes a subagent via the Task/Agent tool, these hooks capture:
- **Input**: The prompt/task sent to the subagent (via PreToolUse hook on Task tool)
- **Output**: The subagent's response (via SubagentStop hook + transcript reading)
- **Session**: Full conversation history from the subagent's transcript

## Compatibility

| Harness | Supported | Notes |
|----------|-----------|-------|
| **Claude Code** | ✅ Yes | Primary target, uses `.claude/settings.json` |
| **VS Code GitHub Copilot** | ✅ Yes | Reads Claude config files directly (v1.109+) |
| **Operating System** | Windows | PowerShell scripts (Linux/Mac not yet supported) |

## Installation

### Prerequisites

For VS Code GitHub Copilot, enable hooks in settings:

```json
{
  "chat.hooks.enabled": true
}
```

Claude Code CLI works without additional settings.

### Step 1: Copy Hook Scripts to Your Project

Copy the PowerShell scripts to your project's `.claude/hooks/` directory:

```powershell
# From your project root
mkdir -p .claude\hooks
copy path\to\capture-subagent-input.ps1 .claude\hooks\
copy path\to\capture-subagent-output.ps1 .claude\hooks\
```

Or copy from this repository:

```powershell
xcopy /s "Agents\Claude Code\hooks\*.ps1" "YOUR_PROJECT\.claude\hooks\"
```

### Step 2: Configure Hooks in settings.json

Create or update `.claude/settings.json` in your project root (NOT in `.claude/hooks/`):

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Task",
        "hooks": [
          {
            "type": "command",
            "command": "powershell.exe -ExecutionPolicy Bypass -File \".claude/hooks/capture-subagent-input.ps1\""
          }
        ]
      },
      {
        "matcher": "Agent",
        "hooks": [
          {
            "type": "command",
            "command": "powershell.exe -ExecutionPolicy Bypass -File \".claude/hooks/capture-subagent-input.ps1\""
          }
        ]
      }
    ],
    "SubagentStop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "powershell.exe -ExecutionPolicy Bypass -File \".claude/hooks/capture-subagent-output.ps1\""
          }
        ]
      }
    ]
  }
}
```

**Note:** We configure both `Task` and `Agent` matchers because Claude Code renamed the Task tool to Agent in v2.1.63, but `Task` still works as an alias.

### Step 3: Verify Installation

1. Start Claude Code in your project
2. Run `/hooks` to see loaded hooks
3. Run an orchestration workflow that uses subagents
4. Check `OrchestrationLogs/` folder for output

## Log Structure

```
OrchestrationLogs/
├── 00_orchestrator_session.md     # Orchestrator session transcript (overwritten on each subagent event)
├── subagent-activity.jsonl        # Machine-readable activity log (one JSON per line)
├── _pending_tasks.json            # Internal state file (for correlating start/stop)
├── _hook_debug.log                # Debug log (only when DEBUG_MODE=true)
└── {agent_instance_id}/
    ├── 01_input.md                # Invocation prompt from orchestrator
    ├── 02_output.md               # Subagent's response to orchestrator
    └── 03_session.md              # Full subagent session history
```

### Example

After running an orchestration with Research and Implementation agents:

```
OrchestrationLogs/
├── 00_orchestrator_session.md     # Main orchestrator's conversation (latest snapshot)
├── subagent-activity.jsonl
├── Research#1/
│   ├── 01_input.md
│   ├── 02_output.md
│   └── 03_session.md
├── Planning#2/
│   ├── 01_input.md
│   ├── 02_output.md
│   └── 03_session.md
└── Implementation#3/
    ├── 01_input.md
    ├── 02_output.md
    └── 03_session.md
```

## File Contents

### 01_input.md

Contains the task invocation message from the orchestrator:

```markdown
# Subagent Input: Research#1

## Metadata

| Field | Value |
|-------|-------|
| Agent Instance ID | Research#1 |
| Session ID | abc123 |
| Timestamp | 2026-02-13T10:30:00.000Z |
| Subagent Type | general-purpose |
| Model | Default |

## Content

```json
{
  "agent_instance_id": "Research#1",
  "task_description": "Analyze requirements and identify key features",
  "input_artifacts": [],
  "output_artifacts": ["Orchestration/Research.md"],
  "input_files": ["docs/requirements.md"],
  "output_files": []
}
```
```

### 02_output.md

Contains the subagent's response:

```markdown
# Subagent Output: Research#1

## Metadata

| Field | Value |
|-------|-------|
| Agent Instance ID | Research#1 |
| Agent ID | subagent-456 |
| Session ID | abc123 |
| Start Time | 2026-02-13T10:30:00.000Z |
| End Time | 2026-02-13T10:32:15.000Z |
| Duration | 00:02:15.000 |
| Tools Used | 8 |

## Content

```json
{
  "agent_instance_id": "Research#1",
  "status_code": "SUCCESS",
  "status_message": "Requirements analysis completed. Created Research.md with 12 functional requirements identified."
}
```
```

### 03_session.md

Contains the full session transcript from the subagent:

```markdown
# Subagent Session: Research#1

## Metadata

| Field | Value |
|-------|-------|
| Agent Instance ID | Research#1 |
| Agent ID | subagent-456 |
| Session ID | abc123 |
| Message Count | 24 |

## Content

### user (2026-02-13T10:30:00.000Z)

[System prompt and task instructions...]

---

### assistant (2026-02-13T10:30:05.000Z)

I'll analyze the requirements...

---

### tool_use

**Tool:** Read
```json
{"file_path": "docs/requirements.md"}
```

---

[... more messages ...]
```
```

### subagent-activity.jsonl

Machine-readable log for analysis tools (one JSON object per line):

```jsonl
{"timestamp":"2026-02-13T10:30:00.000Z","event":"subagent_start","agentInstanceId":"Research#1","agentType":"general-purpose","sessionId":"abc123"}
{"timestamp":"2026-02-13T10:32:15.000Z","event":"subagent_stop","agentInstanceId":"Research#1","agentId":"subagent-456","agentType":"general-purpose","sessionId":"abc123","duration":"00:02:15.000","toolsUsed":8}
```

## How It Works

### Hook Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│ Orchestrator invokes Task/Agent tool → Spawn Subagent               │
└────────────────────────────────┬────────────────────────────────────┘
                                 │
                                 ▼
┌─────────────────────────────────────────────────────────────────────┐
│ PreToolUse Hook (matcher: Task/Agent)                               │
│   - Receives tool_input.prompt (the full subagent instructions)     │
│   - Extracts agent_instance_id from prompt                          │
│   - Creates OrchestrationLogs/{agent_instance_id}/                  │
│   - Writes 01_input.md                                              │
│   - Stores pending task info for correlation                        │
└────────────────────────────────┬────────────────────────────────────┘
                                 │
                                 ▼
┌─────────────────────────────────────────────────────────────────────┐
│ Subagent Executes (in isolated context)                             │
│   - Has its own transcript at agent_transcript_path                 │
│   - Performs research, implementation, etc.                         │
│   - Returns response following Communication Protocol               │
└────────────────────────────────┬────────────────────────────────────┘
                                 │
                                 ▼
┌─────────────────────────────────────────────────────────────────────┐
│ SubagentStop Hook                                                   │
│   - Receives agent_id, agent_type, agent_transcript_path            │
│   - Retrieves pending task info via session_id                      │
│   - Reads agent_transcript_path (JSONL format)                      │
│   - Extracts final response from transcript                         │
│   - Writes 02_output.md and 03_session.md                           │
│   - Appends to subagent-activity.jsonl                              │
└─────────────────────────────────────────────────────────────────────┘
```

### Why Two Hooks?

Claude Code's `SubagentStart` and `SubagentStop` hooks don't provide the actual prompt or response content directly. They only provide:
- `agent_id` - unique identifier for the subagent instance
- `agent_type` - the name/type of the agent
- `agent_transcript_path` - path to the subagent's JSONL transcript (SubagentStop only)

To capture the actual content:
1. **PreToolUse on Task tool** - captures `tool_input.prompt` (what's sent TO the subagent)
2. **SubagentStop** - reads `agent_transcript_path` to get the full conversation including response

### Agent Instance ID Detection

The input hook attempts to extract `agent_instance_id` from the prompt using multiple strategies:

1. **Pure JSON**: Parse entire prompt as JSON with `agent_instance_id` field
2. **JSON Regex**: Match pattern `"agent_instance_id": "AgentName#Number"`
3. **Invoked As**: Match "You are being invoked as AgentName#N" pattern
4. **Word Boundary**: Find any `AgentName#Number` pattern
5. **Timestamp Fallback**: Generate `Task_2026-02-13T10-30-00_001` if not found

## Configuration Options

### Debug Mode

To enable verbose debug logging, edit both PowerShell scripts and set:

```powershell
$DebugMode = $true
```

Debug output will be written to `OrchestrationLogs/_hook_debug.log`.

### Custom Log Directory

To change the log directory, edit both PowerShell scripts:

```powershell
$LogsDir = "CustomLogsFolder"
```

## Troubleshooting

### Hooks Not Executing

1. **Verify hook configuration:**
   ```
   In Claude Code, run: /hooks
   ```
   This shows all loaded hooks and any configuration errors.

2. **Check PowerShell execution policy:**
   ```powershell
   Get-ExecutionPolicy
   ```
   If restricted, the `-ExecutionPolicy Bypass` flag in the hook command should override it.

3. **Verify script paths:**
   - Scripts must be at `.claude/hooks/` relative to project root
   - Paths in settings.json use forward slashes

4. **Check VS Code output panel:**
   - Open Output panel (View → Output)
   - Select "GitHub Copilot Chat Hooks" channel

### No Output Files Created

1. **Enable debug mode:**
   - Set `$DebugMode = $true` in both scripts
   - Check `OrchestrationLogs/_hook_debug.log` for error messages

2. **Check OrchestrationLogs folder:**
   - The folder should be created automatically
   - If not, check for permission issues

3. **Verify subagent is being invoked:**
   - The hooks only fire for Task/Agent tool invocations
   - Regular tool calls (Read, Edit, etc.) don't trigger these hooks

### Input File Created but No Output

1. **SubagentStop hook may not be firing:**
   - Check if `SubagentStop` is in your settings.json

2. **Transcript path not available:**
   - The `agent_transcript_path` is provided by Claude Code (v2.0.42+)
   - Check `03_session.md` for error details

3. **Pending task correlation failed:**
   - Check `_pending_tasks.json` to see if tasks are being tracked
   - Session IDs must match between PreToolUse and SubagentStop

### VS Code GitHub Copilot Specific

1. **Hooks not loading from .claude/settings.json:**
   - Requires VS Code 1.109.3 or later
   - Check setting: `chat.hooks.enabled: true`

2. **Matcher not working (all tools fire):**
   - Known limitation in VS Code - matchers may be ignored
   - The scripts already filter by tool_name internally

## Communication Protocol Reference

This hook system is designed for the Multi-Agent Orchestration System's Communication Protocol v1.5.

**Input Message (Orchestrator → Subagent):**
```json
{
  "agent_instance_id": "{AgentName}#{Number}",
  "task_description": "What to do",
  "input_artifacts": ["Orchestration/artifact1.md"],
  "output_artifacts": ["Orchestration/output.md"],
  "input_files": ["src/file1.ts"],
  "output_files": ["src/file2.ts"],
  "constraints": "Optional restrictions",
  "include_result_summary": false,
  "human_in_the_loop": false
}
```

**Output Message (Subagent → Orchestrator):**
```json
{
  "agent_instance_id": "{AgentName}#{Number}",
  "status_code": "SUCCESS|COMPLETED_NEEDS_ACTION|PARTIALLY_DONE|NEEDS_CLARIFICATION|CAPABILITY_EXCEEDED|BLOCKED",
  "status_message": "Description of outcome"
}
```

## Related Documentation

- [Claude Code Hooks](https://docs.anthropic.com/en/docs/claude-code/hooks) - Official Claude Code hooks documentation
- [VS Code Copilot Hooks](https://code.visualstudio.com/docs/copilot/customization/hooks) - VS Code hooks documentation
- [Communication Protocol](../../../Development/Designs/CommunicationProtocol.md) - Multi-Agent Orchestration Protocol
- [SubagentLoggingHooksRequirements](../../../Development/Designs/SubagentLoggingHooksRequirements.md) - Requirements specification
- [OpenCode Hooks Research](../../../Development/Research/opencode-hooks-research.md) - OpenCode comparison
- [VS Code Hooks Research](../../../Development/Research/vscode-copilot-hooks-research.md) - VS Code GHCP hooks research
