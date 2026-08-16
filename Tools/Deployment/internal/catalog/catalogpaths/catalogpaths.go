package catalogpaths

// catalogpaths is the single source of truth for every catalog subpath in the Go codebase.
//
// This package is a leaf — it imports only path/filepath from the standard library. No
// other package in this module may declare a catalog subpath literal.
//
// Two distinct path families are exposed:
//
//   - Catalog-root-relative: Rel* constants and builder functions that take a catalog root
//     (which --catalog-folder may override). These MUST NOT contain a leading "Catalog/"
//     segment; that segment is already baked into the supplied root.
//
//   - MOSAIC-root-relative: MosaicRel* constants joined with a MOSAIC repository root.
//     These carry the leading catalog directory segment themselves.
//
// The two forms must never be interchanged. Mixing them silently breaks the --catalog-folder
// override.

import "path/filepath"

// ---------------------------------------------------------------------------
// Form 1: catalog-root-relative.
//
// These are relative to a *catalog root* — the directory that --catalog-folder
// (or the catalogRoot parameter) points at. They MUST NOT contain a leading
// "Catalog" segment: that segment is already baked into the supplied root, and
// re-adding it would break the --catalog-folder override.
// ---------------------------------------------------------------------------

// Rel* are the catalog-root-relative subpaths, in slash form.
const (
	RelOrchestratorDir       = "Orchestrator"
	RelSubagentsDir          = "Subagents"
	RelUtilityAgentsDir      = "UtilityAgents"
	RelSkillsDir             = "Skills"
	RelHooksDir              = "Hooks"
	RelWorkflowsDir          = "Workflows"
	RelSourceFilesFormatFile = "SourceFilesFormat.md"
	RelBundleFile            = "DeployedSections.md"
)

// OrchestratorFileName is the orchestrator document's base name.
const OrchestratorFileName = "orchestrator.md"

// OrchestratorScriptFileName is the script orchestrator document's base name.
const OrchestratorScriptFileName = "orchestrator-script.md"

// AgentFileExt is the extension every catalog agent source file carries.
const AgentFileExt = ".md"

// ---------------------------------------------------------------------------
// Form 2: MOSAIC-root-relative.
//
// These carry the leading catalog directory segment themselves and are joined
// with a MOSAIC repository root, never with a catalog root. They are const so
// that consumers requiring compile-time constants (harness RepoContentDir) can
// build on them by concatenation.
// ---------------------------------------------------------------------------

// CatalogDirName is the catalog directory's name directly under the MOSAIC root.
const CatalogDirName = "Catalog"

// MosaicRel* are MOSAIC-root-relative paths in slash form. Each equals
// CatalogDirName + "/" + its catalog-root-relative counterpart.
const (
	MosaicRelSourceFilesFormatFile = CatalogDirName + "/" + RelSourceFilesFormatFile
	MosaicRelBundleFile            = CatalogDirName + "/" + RelBundleFile
)

// MosaicRelHarnessInjectionsDir is the MOSAIC-root-relative parent directory holding
// one subdirectory of per-harness injection content per harness.
const MosaicRelHarnessInjectionsDir = "Catalog/HarnessInjections"

// HarnessDirName* are the on-disk directory names under MosaicRelHarnessInjectionsDir.
// They are display-style names and are NOT derivable from a harness descriptor id.
const (
	HarnessDirNameClaudeCode = "Claude Code"
	HarnessDirNameGhcpCLI    = "GHCP CLI"
	HarnessDirNameOpenCode   = "OpenCode"
	HarnessDirNameVSCodeGHCP = "VS Code GHCP"
)

// HarnessContentDir* are the MOSAIC-root-relative content directories for the four
// built-in harnesses, in slash form. Declared as const-by-concatenation so each
// harness package's exported RepoContentDir stays a compile-time constant.
const (
	HarnessContentDirClaudeCode = MosaicRelHarnessInjectionsDir + "/" + HarnessDirNameClaudeCode
	HarnessContentDirGhcpCLI    = MosaicRelHarnessInjectionsDir + "/" + HarnessDirNameGhcpCLI
	HarnessContentDirOpenCode   = MosaicRelHarnessInjectionsDir + "/" + HarnessDirNameOpenCode
	HarnessContentDirVSCodeGHCP = MosaicRelHarnessInjectionsDir + "/" + HarnessDirNameVSCodeGHCP
)

// ---------------------------------------------------------------------------
// Builders
//
// Each builder joins a catalog root with the corresponding Rel* subpath and
// returns an OS-native path. Every builder is pure, allocation-only, and
// performs no I/O: it never checks existence and never returns an error.
// ---------------------------------------------------------------------------

// OrchestratorDir returns the directory that holds the orchestrator document.
func OrchestratorDir(catalogRoot string) string {
	return filepath.Join(catalogRoot, RelOrchestratorDir)
}

// OrchestratorFile returns the path to the orchestrator document.
func OrchestratorFile(catalogRoot string) string {
	return filepath.Join(OrchestratorDir(catalogRoot), OrchestratorFileName)
}

// OrchestratorScriptFile returns the path to the script orchestrator document.
func OrchestratorScriptFile(catalogRoot string) string {
	return filepath.Join(OrchestratorDir(catalogRoot), OrchestratorScriptFileName)
}

// SubagentsDir returns the parent directory of the per-category subagent directories.
func SubagentsDir(catalogRoot string) string {
	return filepath.Join(catalogRoot, RelSubagentsDir)
}

// SubagentCategoryDir returns the directory for a specific subagent category.
func SubagentCategoryDir(catalogRoot, category string) string {
	return filepath.Join(SubagentsDir(catalogRoot), category)
}

// SubagentFile returns the path to a specific subagent file.
func SubagentFile(catalogRoot, category, agentKey string) string {
	return filepath.Join(SubagentCategoryDir(catalogRoot, category), agentKey+AgentFileExt)
}

// UtilityAgentsDir returns the utility agents directory.
func UtilityAgentsDir(catalogRoot string) string {
	return filepath.Join(catalogRoot, RelUtilityAgentsDir)
}

// UtilityAgentFile returns the path to a specific utility agent file.
func UtilityAgentFile(catalogRoot, agentKey string) string {
	return filepath.Join(UtilityAgentsDir(catalogRoot), agentKey+AgentFileExt)
}

// SkillsDir returns the skills root directory.
func SkillsDir(catalogRoot string) string {
	return filepath.Join(catalogRoot, RelSkillsDir)
}

// SkillDir returns the directory for a specific skill.
func SkillDir(catalogRoot, skillKey string) string {
	return filepath.Join(SkillsDir(catalogRoot), skillKey)
}

// HooksDir returns the hooks root directory.
func HooksDir(catalogRoot string) string {
	return filepath.Join(catalogRoot, RelHooksDir)
}

// WorkflowsDir returns the workflows root directory.
func WorkflowsDir(catalogRoot string) string {
	return filepath.Join(catalogRoot, RelWorkflowsDir)
}

// SourceFilesFormatFile returns the path to the SourceFilesFormat.md marker document.
func SourceFilesFormatFile(catalogRoot string) string {
	return filepath.Join(catalogRoot, RelSourceFilesFormatFile)
}

// BundleFile returns the path to the DeployedSections.md bundle document.
func BundleFile(catalogRoot string) string {
	return filepath.Join(catalogRoot, RelBundleFile)
}

// RelStandaloneAgentsDir is the catalog-root-relative subpath for the standalone agents
// directory. It must not contain a leading "Catalog/" segment.
const RelStandaloneAgentsDir = "StandaloneAgents"

// StandaloneAgentsDir returns the standalone agents root directory.
func StandaloneAgentsDir(catalogRoot string) string {
	return filepath.Join(catalogRoot, RelStandaloneAgentsDir)
}

// StandaloneAgentCategoryDir returns the directory for a specific standalone agent category.
func StandaloneAgentCategoryDir(catalogRoot, category string) string {
	return filepath.Join(StandaloneAgentsDir(catalogRoot), category)
}

// StandaloneAgentFile returns the path to a standalone agent file. An empty category yields
// the flat, uncategorised location directly under StandaloneAgentsDir.
func StandaloneAgentFile(catalogRoot, category, agentKey string) string {
	if category == "" {
		return filepath.Join(StandaloneAgentsDir(catalogRoot), agentKey+AgentFileExt)
	}
	return filepath.Join(StandaloneAgentCategoryDir(catalogRoot, category), agentKey+AgentFileExt)
}
