// Package plan computes a domain.Plan describing every action a deployment run would take,
// without performing any writes. The Planner reads from the catalog and the deployed-file
// state (supplied by the app layer), delegates artifact resolution and staleness comparisons
// to exported helper functions, and returns a fully renderable plan that both frontends can
// display for user review before any file is touched. The manifest is consulted only for
// bookkeeping: harness identity, known-entry listing, recorded content hash for
// local-modification conflict detection, and absent/corrupt-state detection.
package plan

import (
	"context"
	"errors"

	"mosaic-deploy/internal/catalog"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/manifest"
)

// Planner computes a domain.Plan from a catalog and manifest snapshot without performing
// any writes. Both frontends can render the returned plan for user review before any file
// is touched.
type Planner interface {
	// Build derives the complete plan for the given inputs. It performs no file writes.
	Build(ctx context.Context, in Input) (domain.Plan, error)
}

// New returns the production Planner implementation.
func New() Planner {
	return &planner{}
}

// Input carries every value the planner needs to compute a plan. All catalog and manifest
// reads have already been performed by the caller; Build performs no I/O.
type Input struct {
	Catalog         catalog.Catalog
	Module          domain.HarnessModule
	Mode            domain.RunMode
	WorkspacePath   string
	Scope           domain.Scope
	GOOS            string
	Manifest        manifest.Snapshot
	WorkflowIDs            []string                         // selection order is preserved into Plan.Workflows
	UtilityAgentIDs        []string                         // must already be filtered by the tool config allow-list
	InfrastructureAgentIDs []string                         // explicitly selected infrastructure agents
	HookIDs                []string
	Models          map[string]domain.ModelSelection // agent key -> resolved model selection
	// DeployedState supplies the full probed state of every planned target path: presence,
	// content hash, the version stamps read from the deployed file itself, and — for the
	// orchestrator — the embedded workflow IDs and their versions. This is the planner's
	// sole source of truth for version comparison; the manifest is bookkeeping only.
	// Keys are target paths relative to the deployment root. A missing key is equivalent to
	// an absent artifact (Present: false).
	DeployedState map[string]domain.DeployedArtifactState
	// ToolMappingsVersion is the hash of the effective tool-destination mapping set computed
	// from the user-local and project-level config stores at the composition point. When the
	// deployed file carries a different stamp, the planner emits ActionUpdate so the
	// destination field is regenerated from the updated registry module. Empty means no
	// config-declared mappings are active for this run.
	ToolMappingsVersion string
	// WorkspaceFileExists reports whether a file at the given workspace-relative path exists.
	// Build uses this function for hook registration target existence checks, keeping Build
	// free of direct file-system access and enabling controlled unit tests without real disk
	// I/O. When nil, all registration targets are treated as absent (no GapHookRegistration
	// gaps are surfaced for missing targets).
	WorkspaceFileExists func(relPath string) bool

	// ProtocolVersion is the version of the protocol source document loaded for this run.
	// The planner compares it against each deployed artifact's ProtocolVersion to detect
	// protocol staleness.
	ProtocolVersion string

	// BundleVersion is the version of the deployed-sections bundle loaded for this run.
	// The planner compares it against each deployed artifact's BundleVersion to detect
	// bundle staleness.
	BundleVersion string
}

// ArtifactSet is the derived deployment set: the union of referenced agents across all
// selected workflows, plus the orchestrator always, plus every transitively required skill,
// plus the selected utility agents and their skills, plus the selected hook bundles.
// All slices are deduplicated and deterministically ordered.
type ArtifactSet struct {
	Agents []domain.Agent
	Skills []domain.Skill
	Hooks  []domain.HookBundle
}

var (
	// ErrUnknownWorkflow is returned by ResolveArtifacts when a workflow ID is not found
	// in the catalog.
	ErrUnknownWorkflow = errors.New("workflow not found")

	// ErrUnknownAgent is returned by ResolveArtifacts when a workflow references an agent
	// that does not exist in the catalog. The error is not silent: the caller must handle
	// it rather than deploying a subtly incomplete artifact set.
	ErrUnknownAgent = errors.New("workflow references an agent that does not exist")
)
