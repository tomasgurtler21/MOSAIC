package domain

// RunMode distinguishes the available top-level flows.
type RunMode string

const (
	// ModeDeployWorkspace deploys agents, skills, and hooks to a workspace for the first time.
	ModeDeployWorkspace RunMode = "deploy-workspace"
	// ModeUpdateWorkspace updates an existing workspace deployment to the latest source versions.
	ModeUpdateWorkspace RunMode = "update-workspace"
	// ModeUpdateWorkflows replaces the workflow set in an already-deployed orchestrator.
	ModeUpdateWorkflows RunMode = "update-workflows"
	// ModePromoteToGeneric generates a generic agent source file from a harness-only agent.
	// It is never triggered by an Update run; it is always an explicit, opt-in command.
	ModePromoteToGeneric RunMode = "promote-to-generic"
	// ModeTransformHarness converts already-deployed agents from one harness's form to
	// another's, preserving injection content. Never triggered by an Update run.
	ModeTransformHarness RunMode = "transform-harness"

	// ModeDeployAgents deploys a user-chosen mix of subagents, utility agents, and
	// standalone agents plus the skills they require. It asks no workflow or hook
	// question and never rewrites the deployed orchestrator.
	ModeDeployAgents RunMode = "deploy-agents"

	// ModeDeployHooks deploys hook bundles alone. It asks no agent, model, or workflow
	// question and never rewrites the deployed orchestrator.
	ModeDeployHooks RunMode = "deploy-hooks"
)

// PlanAction is the intended deployment action for one artifact.
type PlanAction string

const (
	ActionCreate    PlanAction = "create"
	ActionUpdate    PlanAction = "update"    // stale on at least one version field
	ActionUnchanged PlanAction = "unchanged"
	ActionConflict  PlanAction = "locally-modified"
)

// VersionDelta records one independent staleness comparison.
// Field is one of: "version", "harness_version", "injections_version".
type VersionDelta struct {
	Field    string
	Deployed string
	Source   string
}

// LocalModification records the hash comparison that triggered an ActionConflict classification.
type LocalModification struct {
	RecordedHash    string // from the manifest; empty when ManifestMissing
	CurrentHash     string
	ManifestMissing bool // true => conservative classification when no manifest is present
}

// ConflictDecision is the user's answer for one locally-modified file. The zero value is the
// empty string, which is never a valid decision: an ActionConflict item with no decision is a
// programming error (deploy.ErrUndecidedConflict).
type ConflictDecision string

const (
	DecisionOverwrite           ConflictDecision = "overwrite"
	DecisionSkip                ConflictDecision = "skip"
	DecisionBackupThenOverwrite ConflictDecision = "backup-then-overwrite"
)

// PlanItem is one artifact's planned deployment action, fully renderable before any write.
type PlanItem struct {
	Ref        ArtifactRef
	SourcePath string
	// TargetPath is relative to the deployment root, which the executor resolves (CD-12).
	TargetPath string
	Action     PlanAction
	Stale      []VersionDelta     // non-empty only for ActionUpdate; names every field that drove it
	Model      ModelSelection     // agents only
	Conflict   *LocalModification // non-nil only for ActionConflict
	Reason     string             // one-line human-readable explanation, rendered by both frontends

	// SourceVersion is the source agent's declared version (frontmatter `version`).
	// Non-empty only for agent artifacts whose source file declares a version.
	// The executor copies this into ActionRecord.SourceVersion so the version is
	// visible in the deploy --output json wire contract.
	SourceVersion string
}

// Plan is the complete, pre-computed description of what a deployment run will do.
// It is fully renderable before any write and carries no mutable state.
type Plan struct {
	Mode          RunMode
	Harness       HarnessRef
	WorkspacePath string
	Scope         Scope
	Items         []PlanItem       // deterministic order: kind, then key
	Gaps          []Gap
	Registrations []RegistrationStep
	Workflows     []string // selected workflow ids, in selection order
}

// Counts returns the number of items for each PlanAction.
func (p Plan) Counts() map[PlanAction]int {
	result := make(map[PlanAction]int)
	for _, item := range p.Items {
		result[item.Action]++
	}
	return result
}

// Empty reports whether the plan contains no item with an action other than ActionUnchanged.
func (p Plan) Empty() bool {
	for _, item := range p.Items {
		if item.Action != ActionUnchanged {
			return false
		}
	}
	return true
}
