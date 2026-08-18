package app

// harness_only_agent.go declares the HarnessOnlyAgent type used by the harness-only
// discovery scan (scanHarnessOnlyAgents) and the refresh-scope prompt
// (askHarnessOnlyRefreshScope). The scan and eligibility implementations live in
// harness_only_discovery.go; the prompt implementation lives in harness_only_prompt.go.

import "mosaic-deploy/internal/domain"

// HarnessOnlyAgent is one deployed agent file that has no counterpart in the generic
// catalog and satisfies the two-signal eligibility rule.
type HarnessOnlyAgent struct {
	// TargetPath is the file's path relative to the deployment root, in the same form
	// plan items and the executor use: filepath.Join(agentsDir, fileName).
	TargetPath string
	// FileName is the base file name including its extension.
	FileName string
	// Key is the agent key derived from FileName the same way the catalog derives a key
	// from a generic source file name: the base name with a trailing ".agent.md" or
	// ".md" removed. Used only for catalog-counterpart exclusion and for log subjects;
	// it is never registered in the catalog.
	Key string
	// NumericID is the frontmatter `id` scalar, or "" when absent.
	NumericID string
	// Version is the frontmatter `version` scalar, or "" when absent. Harness-only
	// agents commonly have none; see the staleness contract in the Update flow design.
	Version string
	// TransformVersion is the frontmatter `transform_version` scalar. Never empty for
	// an eligible agent — its presence is signal one of the eligibility rule.
	TransformVersion string
	// Role is the frontmatter `role` scalar parsed through domain.ParseAgentRole.
	// Defaults to domain.RoleSubagent when the field is absent or unrecognised.
	Role domain.AgentRole
}
