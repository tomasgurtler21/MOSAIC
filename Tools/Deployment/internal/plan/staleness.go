package plan

import (
	"fmt"
	"strings"

	"mosaic-deploy/internal/domain"
)

// AgentStaleness compares the source version stamps against the versions read from the
// deployed file, returning one delta per mismatching field in the fixed order
// version, transform_version, injections_version, orchestrator_injections_version.
// VersionDelta.Deployed carries the value actually present in the deployed file.
//
// Callers must only invoke this for a deployed artifact that is present; absence is
// handled by the classifier as ActionCreate before staleness is consulted.
//
// The agent parameter is accepted for future extension (for example, key-based log
// attribution or per-agent version overrides) but is not used by the current
// implementation. It is explicitly blanked with _ to signal this intent.
func AgentStaleness(deployed domain.DeployedArtifactState, _ domain.Agent, stamps domain.VersionStamps) []domain.VersionDelta {
	var deltas []domain.VersionDelta

	if deployed.Version != stamps.Version {
		deltas = append(deltas, domain.VersionDelta{
			Field:    "version",
			Deployed: deployed.Version,
			Source:   stamps.Version,
		})
	}
	if deployed.TransformVersion != stamps.TransformVersion {
		deltas = append(deltas, domain.VersionDelta{
			Field:    "transform_version",
			Deployed: deployed.TransformVersion,
			Source:   stamps.TransformVersion,
		})
	}
	if deployed.InjectionsVersion != stamps.InjectionsVersion {
		deltas = append(deltas, domain.VersionDelta{
			Field:    "injections_version",
			Deployed: deployed.InjectionsVersion,
			Source:   stamps.InjectionsVersion,
		})
	}
	if deployed.OrchestratorInjectionsVersion != stamps.OrchestratorInjectionsVersion {
		deltas = append(deltas, domain.VersionDelta{
			Field:    "orchestrator_injections_version",
			Deployed: deployed.OrchestratorInjectionsVersion,
			Source:   stamps.OrchestratorInjectionsVersion,
		})
	}
	if deployed.ToolMappingsVersion != stamps.ToolMappingsVersion {
		deltas = append(deltas, domain.VersionDelta{
			Field:    "tool_mappings_version",
			Deployed: deployed.ToolMappingsVersion,
			Source:   stamps.ToolMappingsVersion,
		})
	}

	return deltas
}

// SkillStaleness compares the skill's catalog version against the version read from the
// deployed file. Returns at most one delta with Field "version".
func SkillStaleness(deployed domain.DeployedArtifactState, skill domain.Skill) []domain.VersionDelta {
	if deployed.Version != skill.Version {
		return []domain.VersionDelta{{
			Field:    "version",
			Deployed: deployed.Version,
			Source:   skill.Version,
		}}
	}
	return nil
}

// HookStaleness compares the bundle's catalog version against the version read from the
// deployed file. Returns at most one delta with Field "version".
func HookStaleness(deployed domain.DeployedArtifactState, bundle domain.HookBundle) []domain.VersionDelta {
	if deployed.Version != bundle.Version {
		return []domain.VersionDelta{{
			Field:    "version",
			Deployed: deployed.Version,
			Source:   bundle.Version,
		}}
	}
	return nil
}

// WorkflowDeltaFieldPrefix prefixes the Field name of every workflow-drift VersionDelta so
// it can never collide with "version", "transform_version", or "injections_version", which
// deploy.resolveVersionStamp switches on when writing manifest version stamps.
const WorkflowDeltaFieldPrefix = "workflow:"

// WorkflowVersionChange records one workflow whose catalog version differs from the version
// recorded in the deployed orchestrator file.
type WorkflowVersionChange struct {
	ID       string // workflow ID
	Deployed string // version from the deployed file; "" when no comment was present
	Source   string // current catalog version
}

// WorkflowDrift summarises the difference between the deployed orchestrator's workflow set
// and the newly selected workflow set. The zero value means no drift.
//
// Added and Removed store workflow IDs in source order (selection order for Added, deployed
// order for Removed). Changed stores version mismatches in selection order.
//
// The unexported version maps carry the additional version data needed by Deltas() to emit
// truthful VersionDelta entries for added and removed workflows, without widening the public
// struct fields beyond what the design specifies.
type WorkflowDrift struct {
	Added   []string               // workflow IDs present in selected but not in deployed, in selection order
	Removed []string               // workflow IDs present in deployed but not in selected, in deployed order
	Changed []WorkflowVersionChange // IDs present in both but with a differing version, in selection order

	// addedVersions maps each Added ID to its catalog (source) version, so Deltas() can
	// emit a truthful Source value without widening Added to a richer type.
	addedVersions map[string]string // id -> catalog version

	// removedVersions maps each Removed ID to its deployed version, so Deltas() can
	// emit a truthful Deployed value without widening Removed to a richer type.
	removedVersions map[string]string // id -> deployed version
}

// WorkflowStaleness compares the deployed orchestrator's workflow set against the newly
// selected set and returns a WorkflowDrift describing every difference.
//
// The comparison is order-insensitive for set membership: a reordered but otherwise identical
// selection produces no drift. Per-workflow version comparison pairs the deployed file's
// version attribute (domain.DeployedWorkflow.Version) with the catalog's
// domain.Workflow.Version. A deployed block with no version attribute (empty string) is always
// treated as a version change and reported in Changed.
//
// Callers must only invoke this for agents with Role == domain.RoleOrchestrator. The
// decision to apply workflow staleness is the classifier's responsibility.
func WorkflowStaleness(deployed domain.DeployedWorkflows, selected []domain.Workflow) WorkflowDrift {
	var drift WorkflowDrift

	// Build a set of deployed workflow IDs for O(1) membership tests.
	deployedSet := make(map[string]struct{}, len(deployed))
	for _, d := range deployed {
		deployedSet[d.ID] = struct{}{}
	}

	// Build a set of selected workflow IDs for O(1) membership tests.
	selectedSet := make(map[string]struct{}, len(selected))
	for _, s := range selected {
		selectedSet[s.ID] = struct{}{}
	}

	// Determine Added: in selected but not in deployed. Preserve selection order.
	for _, s := range selected {
		if _, inDeployed := deployedSet[s.ID]; !inDeployed {
			drift.Added = append(drift.Added, s.ID)
			if drift.addedVersions == nil {
				drift.addedVersions = make(map[string]string)
			}
			drift.addedVersions[s.ID] = s.Version
		}
	}

	// Determine Removed: in deployed but not in selected. Preserve deployed order.
	for _, d := range deployed {
		if _, inSelected := selectedSet[d.ID]; !inSelected {
			drift.Removed = append(drift.Removed, d.ID)
			if drift.removedVersions == nil {
				drift.removedVersions = make(map[string]string)
			}
			drift.removedVersions[d.ID] = d.Version
		}
	}

	// Determine Changed: in both sets but with differing versions. Preserve selection order.
	// An empty deployed version (no version attribute) is always a mismatch.
	for _, s := range selected {
		deployedVer, inDeployed := deployed.Version(s.ID)
		if !inDeployed {
			continue // handled by Added
		}
		if deployedVer != s.Version {
			drift.Changed = append(drift.Changed, WorkflowVersionChange{
				ID:       s.ID,
				Deployed: deployedVer,
				Source:   s.Version,
			})
		}
	}

	return drift
}

// Stale reports whether the drift contains any workflow-set or version changes.
func (d WorkflowDrift) Stale() bool {
	return len(d.Added)+len(d.Removed)+len(d.Changed) > 0
}

// Deltas converts the drift into a slice of VersionDelta values, one per changed workflow.
// Every delta's Field is prefixed with WorkflowDeltaFieldPrefix so it cannot collide with
// the executor's resolveVersionStamp switch on "version", "transform_version", or
// "injections_version".
//
// Ordering: added first, then removed, then version-changed; within each group, source order.
//
//	added   -> {Field: "workflow:<id>", Deployed: "",         Source: catalogVersion}
//	removed -> {Field: "workflow:<id>", Deployed: deployedVer, Source: ""}
//	changed -> {Field: "workflow:<id>", Deployed: deployedVer, Source: catalogVersion}
func (d WorkflowDrift) Deltas() []domain.VersionDelta {
	if !d.Stale() {
		return nil
	}
	deltas := make([]domain.VersionDelta, 0, len(d.Added)+len(d.Removed)+len(d.Changed))

	for _, id := range d.Added {
		deltas = append(deltas, domain.VersionDelta{
			Field:    WorkflowDeltaFieldPrefix + id,
			Deployed: "",
			Source:   d.addedVersions[id],
		})
	}

	for _, id := range d.Removed {
		deltas = append(deltas, domain.VersionDelta{
			Field:    WorkflowDeltaFieldPrefix + id,
			Deployed: d.removedVersions[id],
			Source:   "",
		})
	}

	for _, c := range d.Changed {
		deltas = append(deltas, domain.VersionDelta{
			Field:    WorkflowDeltaFieldPrefix + c.ID,
			Deployed: c.Deployed,
			Source:   c.Source,
		})
	}

	return deltas
}

// Reason returns a one-line human-readable string naming every workflow responsible for the
// drift. Returns "" when Stale() is false.
//
// Format examples:
//
//	"workflows added: greenfield-tdd"
//	"workflows removed: quick-fix"
//	"workflow versions changed: quick-fix (1.0 -> 2.0)"
//	"workflows added: greenfield-tdd; workflows removed: quick-fix; workflow versions changed: changed-wf (1.0 -> 2.0)"
func (d WorkflowDrift) Reason() string {
	if !d.Stale() {
		return ""
	}
	var parts []string

	if len(d.Added) > 0 {
		parts = append(parts, "workflows added: "+strings.Join(d.Added, ", "))
	}
	if len(d.Removed) > 0 {
		parts = append(parts, "workflows removed: "+strings.Join(d.Removed, ", "))
	}
	if len(d.Changed) > 0 {
		changedParts := make([]string, len(d.Changed))
		for i, c := range d.Changed {
			changedParts[i] = fmt.Sprintf("%s (%s -> %s)", c.ID, c.Deployed, c.Source)
		}
		parts = append(parts, "workflow versions changed: "+strings.Join(changedParts, ", "))
	}

	return strings.Join(parts, "; ")
}
