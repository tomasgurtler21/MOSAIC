// Package claudecode is the Claude Code harness adapter: the first and, in
// this effort, only real implementation of domain.HarnessAdapter. Everything
// Claude-Code-specific in the module lives here; no package outside
// internal/harness/ and cmd/ may import it.
//
// This file declares the harness's project-scoped layout as this adapter's
// own constants. They are deliberately not shared with the deployment tool:
// an adapter must know its harness's layout regardless, and sharing would
// mean importing an entire descriptor model for a handful of short paths.
package claudecode

// This harness's project-scoped layout.
const (
	HarnessID       = "claude-code"
	SettingsRelPath = ".claude/settings.json"
	HooksRelDir     = ".claude/hooks"
	AgentsRelDir    = ".claude/agents"
)

// ConfigHomeEnvVar is the environment variable this harness honours to
// relocate its user-scope configuration. Setting it into the spawn plan's
// environment is what lets the adapter report the user scope as neutralized
// rather than merely inspected.
const ConfigHomeEnvVar = "CLAUDE_CONFIG_DIR"
