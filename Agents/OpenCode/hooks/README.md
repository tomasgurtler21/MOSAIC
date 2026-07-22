# OpenCode Subagent Logger Plugin

A TypeScript plugin for OpenCode that automatically logs subagent invocations in the Multi-Agent Orchestration System.

## Overview

When the orchestrator invokes a subagent via the Task tool, this plugin captures:
- **Input**: The prompt/task sent to the subagent
- **Output**: The subagent's response (following Communication Protocol)
- **Session**: Full conversation history (if available)

## Installation

### Option 1: Project-level (Recommended)

Copy the plugin to your project's `.opencode/plugins/` directory:

```bash
# From your project root
mkdir -p .opencode/plugins
cp path/to/subagent-logger.ts .opencode/plugins/
```

### Option 2: Global Installation

Copy to your global OpenCode plugins directory:

```bash
# Windows
copy subagent-logger.ts %APPDATA%\opencode\plugins\

# Linux/macOS
cp subagent-logger.ts ~/.config/opencode/plugins/
```

### Optional: Install TypeScript Types

For development/editing:

```bash
npm install --save-dev @opencode-ai/plugin
```

## Usage

1. Install the plugin (see above)
2. Start OpenCode: `opencode`
3. Run your orchestration workflow
4. Logs appear automatically in `OrchestrationLogs/` folder

## Log Structure

```
OrchestrationLogs/
└── {agent_instance_id}/
    ├── 01_input.md       # Invocation prompt from orchestrator
    ├── 02_output.md      # Subagent's response to orchestrator
    └── 03_session.md     # Full subagent session history
```

### Example

After running an orchestration with Research and Implementation agents:

```
OrchestrationLogs/
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
| Timestamp | 2026-01-29T10:30:00.000Z |
| Agent Type | Research |
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

Contains the subagent's response following the Communication Protocol:

```markdown
# Subagent Output: Research#1

## Metadata

| Field | Value |
|-------|-------|
| Agent Instance ID | Research#1 |
| Session ID | abc123 |
| Start Time | 2026-01-29T10:30:00.000Z |
| End Time | 2026-01-29T10:32:15.000Z |
| Success | true |

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

Contains the full session history (all tool calls, responses, etc.):

```markdown
# Subagent Session: Research#1

## Metadata

| Field | Value |
|-------|-------|
| Agent Instance ID | Research#1 |
| Session ID | abc123 |
| Exported At | 2026-01-29T10:32:16.000Z |

## Content

[Full session transcript from opencode export]
```

## Agent Instance ID Detection

The plugin attempts to extract `agent_instance_id` from the Task prompt using multiple strategies:

1. **Pure JSON**: Parse entire prompt as JSON
2. **Embedded JSON**: Find JSON block containing `agent_instance_id`
3. **Regex Fallback**: Match pattern `"agent_instance_id": "AgentName#Number"`
4. **Timestamp Fallback**: Generate `Task_2026-01-29T10-30-00_001` if not found

## Session Export

The plugin attempts to export session history using:

```bash
opencode export {sessionId}
```

If session export fails (e.g., subagent sessions not supported), a placeholder file is created with manual export instructions.

## Troubleshooting

### Plugin Not Loading

1. Check file location is correct (`.opencode/plugins/`)
2. Ensure file has `.ts` extension
3. Check OpenCode log file for `service=plugin` entries at startup
4. Look for `service=SubagentLogger` entries to confirm plugin initialization

### Logs Not Appearing

1. Verify `OrchestrationLogs/` directory exists (plugin creates it automatically)
2. Check if Task tool is being invoked (not other tools)
3. Check OpenCode log file for `service=SubagentLogger` entries:
   - `Plugin loaded` - Plugin initialized successfully
   - `tool.execute.before fired` - Hook is triggering
   - `Task tool detected` - Task tool invocations are being captured

### Debug Logging

The plugin uses OpenCode's structured logging via `client.app.log()`. To see debug output:

1. Run OpenCode with debug logging: `opencode --log-level DEBUG`
2. Check log files at:
   - **Windows**: `%USERPROFILE%\.local\share\opencode\log\`
   - **Linux/macOS**: `~/.local/share/opencode/log/`
3. Search for `service=SubagentLogger` in the log file

**Note:** `console.log()` does NOT output to OpenCode log files. The plugin uses `client.app.log()` for proper logging.

### Fallback Logging

If OpenCode's `client.app.log()` fails (e.g., during initialization), the plugin writes to:
```
OrchestrationLogs/_debug.log
```

### Session Export Not Working

The `opencode export` command may not work for subagent sessions. In this case:
- Check the `03_session.md` file for manual export instructions
- Session data may be in `%LOCALAPPDATA%\opencode\` (Windows) or `~/.local/share/opencode/` (Linux/macOS)

## Communication Protocol Reference

This plugin is designed for the Multi-Agent Orchestration System's Communication Protocol v1.5.

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

- [OpenCode Plugins Documentation](https://opencode.ai/docs/plugins/)
- [Communication Protocol](../../../Development/Designs/CommunicationProtocol.md)
- [OpenCode Hooks Research](../../../Development/Research/opencode-hooks-research.md)
