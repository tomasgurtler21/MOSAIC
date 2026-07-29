package catalog

// This file declares the public contract surface of the catalog package.
// Implementations live in root.go, agents.go, skills.go, hooks.go, workflows.go,
// and tiers.go — none of which exist yet. Every name exported here must remain
// stable for stage 7 tests to compile. Do not add implementation logic to this file.

import (
	"errors"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
)

// ErrNotMosaicRoot is returned by ResolveRoot when no MOSAIC repository root can be
// found by walking up from the supplied directory.
var ErrNotMosaicRoot = errors.New("working directory is not a MOSAIC repository")

// Issue is a structured diagnostic produced during catalog loading. Codes are stable and
// used as discrimination keys by callers.
//
// Defined codes:
//
//	"index-orphan"       — workflow appears in the index but has no corresponding file on disk
//	"file-orphan"        — workflow file exists on disk but is not listed in the index
//	"hook-hash-mismatch" — a hook bundle's content_hash field does not match the computed hash
//	"missing-field"      — a required frontmatter field is absent from a source file
type Issue struct {
	Severity docformat.Severity
	Code     string // stable code; see above
	Subject  string // agent key, bundle id, workflow id, or file path
	Message  string // human-readable explanation
	Path     string // absolute path of the file that produced the issue, when applicable
}

// Catalog is a read-only snapshot of the MOSAIC source tree. All methods are safe for
// concurrent use. The catalog is immutable after Load returns.
type Catalog interface {
	// Root returns the absolute path of the MOSAIC repository root that was passed to Load.
	Root() string

	// Agents returns all worker agents sorted by Key. The orchestrator and utility agents
	// are excluded; use Orchestrator and UtilityAgents to access them.
	Agents() []domain.Agent

	// Agent looks up any agent by key, regardless of role.
	Agent(key string) (domain.Agent, bool)

	// Orchestrator returns the single orchestrator agent. The orchestrator is identified by
	// the file at Agents/Generic/Orchestrator/orchestrator.md.
	Orchestrator() domain.Agent

	// UtilityAgents returns all utility agents. They are never deployed automatically.
	UtilityAgents() []domain.Agent

	// Skills returns all skills.
	Skills() []domain.Skill

	// Skill looks up a skill by its folder-name key.
	Skill(key string) (domain.Skill, bool)

	// Hooks returns all hook bundles.
	Hooks() []domain.HookBundle

	// Hook looks up a hook bundle by its id key.
	Hook(key string) (domain.HookBundle, bool)

	// Workflows returns all workflows listed in the index, excluding underscore-prefixed files.
	Workflows() []domain.Workflow

	// Workflow looks up a workflow by its id.
	Workflow(id string) (domain.Workflow, bool)

	// WorkflowCategories returns workflows grouped by source-folder category, in index order.
	WorkflowCategories() []domain.WorkflowCategory

	// Tiers returns exactly the tier strings present in source: unnormalised, not case-folded,
	// not validated against any fixed list. Each TierInfo carries the first non-empty rationale
	// text found in source and the sorted list of agent keys that declared this tier.
	Tiers() []domain.TierInfo

	// WorkflowSection extracts the [[SECTION:Workflow:{id}]] block from the workflow's source
	// file and returns it byte-identically, including the two boundary-tag lines.
	WorkflowSection(id string) ([]byte, error)

	// ReadSource returns the raw bytes of a file previously reported by the catalog (via a
	// SourcePath or SourceDir field). Callers must not invent paths; only paths the catalog
	// has emitted are valid inputs.
	ReadSource(path string) ([]byte, error)

	// Issues reports any index/disk reconciliation mismatches and hook bundle integrity errors
	// encountered during Load. A non-empty list is always reported; it is never silently ignored.
	Issues() []Issue
}

// ResolveRoot walks up the directory tree from dir until it finds a MOSAIC repository root
// and returns its absolute path. If no root is found it returns ErrNotMosaicRoot.
//
// A directory is the MOSAIC root when it contains both Agents/Generic/SOURCE-FORMAT.md and
// Workflows/Index.md. Other layouts are rejected.
func ResolveRoot(dir string) (string, error) {
	return resolveRoot(dir)
}

// Load reads the full MOSAIC source tree rooted at root and returns an immutable Catalog.
// It opens no file for writing. Structural problems (missing fields, index/disk mismatches,
// hook integrity errors) are accumulated into Catalog.Issues rather than returned as errors.
// Load returns a non-nil error only when the root itself is unreadable or structurally invalid.
func Load(root string) (Catalog, error) {
	return loadCatalog(root)
}
