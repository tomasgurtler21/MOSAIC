package catalog

// This file declares the public contract surface of the catalog package.
// Implementations live in root.go, agents.go, skills.go, hooks.go, workflows.go,
// and tiers.go — none of which exist yet. Every name exported here must remain
// stable for stage 7 tests to compile. Do not add implementation logic to this file.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
)

// DefaultCatalogDirName is the name of the default catalogue subdirectory within
// the MOSAIC root. DefaultCatalogRoot joins this name onto the MOSAIC root.
const DefaultCatalogDirName = "Catalog"

// DefaultCatalogRoot returns the absolute path of the default catalogue root,
// which is mosaicRoot/Catalog. No I/O is performed.
func DefaultCatalogRoot(mosaicRoot string) string {
	return filepath.Join(mosaicRoot, DefaultCatalogDirName)
}

// ErrCatalogFolderMissing is returned by ValidateCatalogRoot when the supplied
// path does not exist on disk.
var ErrCatalogFolderMissing = errors.New("catalog folder does not exist")

// ErrCatalogFolderNotDirectory is returned by ValidateCatalogRoot when the
// supplied path exists but is not a directory.
var ErrCatalogFolderNotDirectory = errors.New("catalog folder is not a directory")

// ValidateCatalogRoot checks that path exists and is a directory.
// A sparse directory (one with no agent or skill subdirectories) is valid.
// Returns ErrCatalogFolderMissing if the path does not exist, or
// ErrCatalogFolderNotDirectory if it exists but is not a directory.
func ValidateCatalogRoot(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%w: %s", ErrCatalogFolderMissing, path)
		}
		return fmt.Errorf("catalog root: stat %s: %w", path, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%w: %s", ErrCatalogFolderNotDirectory, path)
	}
	return nil
}

// ErrNotMosaicRoot is returned by ResolveRoot when the supplied directory is not a
// MOSAIC repository root.
var ErrNotMosaicRoot = errors.New("working directory is not a MOSAIC repository")

// Issue is a structured diagnostic produced during catalog loading. Codes are stable and
// used as discrimination keys by callers.
//
// Defined codes:
//
//	"index-orphan"         — workflow appears in the index but has no corresponding file on disk
//	                         (produced exclusively by CheckWorkflowIndex, never by normal catalog loading)
//	"file-orphan"          — workflow file exists on disk but is not listed in the index
//	                         (produced exclusively by CheckWorkflowIndex, never by normal catalog loading)
//	"hook-hash-mismatch"   — a hook bundle's content_hash field does not match the computed hash
//	"missing-field"        — a required frontmatter field is absent from a source file
//	"duplicate-agent-id"   — two catalog agents declare the same numeric `id`
//	"invalid-role"         — a frontmatter `role` value is not one of "subagent", "orchestrator", "utility", or "standalone"
//	"missing-skill-folder" — a required_skills entry names a key with no Skills/<key> folder
//	"bundle-unknown-target"     — bundle block Target is not in docformat.CanonicalDeployed
//	"bundle-unknown-applies-to" — bundle block AppliesTo is not "subagent" or "orchestrator"
//	"bundle-missing-spec-doc"   — bundle block SpecifiedIn path does not exist under the MOSAIC root
//	"bundle-version-mismatch"   — deployed file bundle_version stamp differs from bundle.Version
//	"bundle-region-drift"       — deployed bundle region body differs from the bundle block content
type Issue struct {
	Severity docformat.Severity
	Code     string // stable code; see above
	Subject  string // agent key, bundle id, workflow id, file path, or duplicate id
	Message  string // human-readable explanation
	Path     string // absolute path of the file that produced the issue, when applicable
	Node     string // region name (for bundle-region-drift) or other sub-object identifier
}

// Catalog is a read-only snapshot of the MOSAIC source tree. All methods are safe for
// concurrent use. The catalog is immutable after Load returns.
type Catalog interface {
	// Root returns the absolute path of the MOSAIC repository root that was passed to Load.
	Root() string

	// CatalogRoot returns the absolute path of the catalogue root used by this catalog.
	// When Load was called with an empty catalogRoot, this equals DefaultCatalogRoot(Root()).
	CatalogRoot() string

	// Agents returns all worker agents sorted by Key. The orchestrator, utility, and
	// standalone agents are excluded; use Orchestrator, UtilityAgents, and StandaloneAgents
	// to access them.
	Agents() []domain.Agent

	// Agent looks up any agent by key, regardless of role.
	Agent(key string) (domain.Agent, bool)

	// Orchestrator returns the single orchestrator agent. The orchestrator is identified by
	// the file at Orchestrator/orchestrator.md.
	Orchestrator() domain.Agent

	// OrchestratorScript returns the script-mode orchestrator agent, loaded from
	// Catalog/Orchestrator/orchestrator-script.md. The bool is false when the catalog root
	// has no such file, which is not an error: a catalog lacking it loads and deploys normally.
	//
	// The returned agent has Role == domain.RoleOrchestrator and Key == "orchestrator-script",
	// distinct from the main orchestrator's key, so both coexist in the agent index and both
	// are reachable through Agent(key).
	OrchestratorScript() (domain.Agent, bool)

	// UtilityAgents returns all utility agents. They are excluded from Agents() and
	// StandaloneAgents(). They are never deployed automatically.
	UtilityAgents() []domain.Agent

	// StandaloneAgents returns all standalone agents sorted by Key. They are deployed only
	// by the standalone-only deploy mode and are excluded from Agents(), UtilityAgents(),
	// and InfrastructureAgents().
	StandaloneAgents() []domain.Agent

	// InfrastructureAgents returns all worker agents that have a non-empty Infrastructure
	// field, sorted by Key. This is a filtered view of Agents(); it does not return a
	// separate agent class.
	InfrastructureAgents() []domain.Agent

	// Skills returns all skills.
	Skills() []domain.Skill

	// Skill looks up a skill by its folder-name key.
	Skill(key string) (domain.Skill, bool)

	// Hooks returns all hook bundles.
	Hooks() []domain.HookBundle

	// Hook looks up a hook bundle by its id key.
	Hook(key string) (domain.HookBundle, bool)

	// Workflows returns all eligible workflow files found on disk under Catalog/Workflows/
	// subdirectories, excluding root-level files and underscore-prefixed files.
	Workflows() []domain.Workflow

	// Workflow looks up a workflow by its id.
	Workflow(id string) (domain.Workflow, bool)

	// WorkflowCategories returns workflows grouped by source-folder category, in alphabetical
	// order by category name. Within a category, workflows are sorted ascending by ID.
	WorkflowCategories() []domain.WorkflowCategory

	// Tiers returns exactly the tier strings present in source: unnormalised, not case-folded,
	// not validated against any fixed list. Each TierInfo carries the first non-empty rationale
	// text found in source and the sorted list of agent keys that declared this tier.
	Tiers() []domain.TierInfo

	// WorkflowSection extracts the <Workflow type="core" name="{id}"> block from the workflow's
	// source file and returns it byte-identically, including the two boundary-tag lines.
	WorkflowSection(id string) ([]byte, error)

	// ReadSource returns the raw bytes of a file previously reported by the catalog (via a
	// SourcePath or SourceDir field). Callers must not invent paths; only paths the catalog
	// has emitted are valid inputs.
	ReadSource(path string) ([]byte, error)

	// Issues reports hook bundle integrity errors encountered during Load. It does not emit
	// file-orphan or index-orphan codes; those are produced exclusively by CheckWorkflowIndex.
	// A non-empty list is always reported; it is never silently ignored.
	Issues() []Issue

	// AgentByNumericID looks up any agent by its frontmatter `id` scalar, compared as the
	// verbatim string the source declared. The second result is false when no agent declares
	// that id, including when id is the empty string (the orchestrator and any agent with no
	// `id` are never returned by this method).
	//
	// The index behind this method is built at load time alongside agentIdx. Agents with an
	// empty NumericID are omitted from it. When two catalog agents declare the same `id`, Load
	// reports a catalog.Issue with code "duplicate-agent-id" (severity error) and the index
	// keeps the first by key order — the issue, not the silent pick, is the contract.
	AgentByNumericID(id string) (domain.Agent, bool)
}

// ResolveRoot validates that dir is the MOSAIC repository root and returns its absolute path.
// If dir is "" or "." it is converted to an absolute path before validation. If the supplied
// directory is not the MOSAIC root, ErrNotMosaicRoot is returned — the function does NOT walk
// up the directory tree. The deploy tool must be invoked from the repository root.
//
// A directory is the MOSAIC root when it contains:
//
//	Catalog/SourceFilesFormat.md
//
// Catalog/Workflows/Index.md is no longer a required marker; a root is recognised
// identically whether or not that file exists. A directory carrying the marker only at
// the legacy Agents/Generic/ location (without the Catalog/ prefix) is not accepted.
func ResolveRoot(dir string) (string, error) {
	return resolveRoot(dir)
}

// Load reads the full MOSAIC source tree and returns an immutable Catalog.
// mosaicRoot is the MOSAIC repository root (required). catalogRoot is the catalogue root
// from which agents, workflows, skills, and hooks are loaded; when empty, it defaults to
// DefaultCatalogRoot(mosaicRoot). Bundle and protocol files are always resolved from mosaicRoot.
// Structural problems are accumulated into Catalog.Issues rather than returned as errors.
// Load returns a non-nil error only when a root is unreadable or structurally invalid.
func Load(mosaicRoot, catalogRoot string) (Catalog, error) {
	if catalogRoot == "" {
		catalogRoot = DefaultCatalogRoot(mosaicRoot)
	}
	absCatalogRoot, err := filepath.Abs(catalogRoot)
	if err != nil {
		return nil, fmt.Errorf("catalog.Load: catalogue root: %w", err)
	}
	return loadCatalog(mosaicRoot, absCatalogRoot)
}
