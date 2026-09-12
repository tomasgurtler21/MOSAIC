package app

// mcpconflict.go detects a specific tool-field conflict in deployed Claude Code agent
// frontmatter: when both a tools: field and an mcpServers: field are present, Claude Code
// silently ignores the mcpServers entries (CC-079). This detection is advisory only -- it
// surfaces an informational gap so the user knows to remove mcpServers and re-supply any
// MCP tools via the corrected mechanism. It never blocks or aborts the run.

import (
	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
)

const claudeCodeHarness = "claude-code"

// checkConflictingToolFields inspects deployed frontmatter for tool-field conflicts and
// returns gaps to surface. It does not perform I/O.
//
// fm:         parsed frontmatter from the deployed file (pre-rebuild), obtained via
//             docformat.Parse(src).Frontmatter(). Callers use fm.Get(key) to check field
//             presence.
// harnessKey: the harness identifier (e.g. "claude-code"), available at the call site as
//             module.Ref().ID.
// agentKey:   the agent's key, used as Gap.Owner and Gap.Subject.
//
// Returns nil when no conflict is detected.
func checkConflictingToolFields(fm *docformat.Frontmatter, harnessKey string, agentKey string) []domain.Gap {
	if harnessKey != claudeCodeHarness {
		return nil
	}
	if fm == nil || !fm.Present() {
		return nil
	}
	_, hasTools := fm.Get("tools")
	_, hasMCPServers := fm.Get("mcpServers")
	if !hasTools || !hasMCPServers {
		return nil
	}
	return []domain.Gap{
		{
			Kind:    domain.GapConflictingToolField,
			Subject: agentKey,
			Owner:   agentKey,
			Detail: "Deployed frontmatter contains both tools: and mcpServers: fields. " +
				"Claude Code ignores mcpServers when tools is also present (CC-079). " +
				"Remove the mcpServers field from the deployed agent file and re-supply " +
				"any MCP tools via the tools: field using the corrected mechanism.",
		},
	}
}
