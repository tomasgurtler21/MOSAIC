package plan

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/manifest"
)

// planner is the production implementation of Planner.
type planner struct{}

// Build derives the complete plan from the given inputs. It performs no file writes.
func (p *planner) Build(ctx context.Context, in Input) (domain.Plan, error) {
	// Resolve the complete artifact set from workflow selections, utility agents, infrastructure agents, and hooks.
	set, err := ResolveArtifacts(in.Catalog, in.WorkflowIDs, in.UtilityAgentIDs, in.InfrastructureAgentIDs, in.HookIDs)
	if err != nil {
		return domain.Plan{}, err
	}

	// Compute all target paths once; path logic is centralised in EnumerateTargetPaths so that
	// Build and the app-layer probe use exactly the same path computation (D2).
	paths, err := EnumerateTargetPaths(set, in.Module, in.Scope, in.GOOS)
	if err != nil {
		return domain.Plan{}, err
	}

	desc := in.Module.Descriptor()
	manifestUsable := in.Manifest.State == manifest.StatePresent

	var items []domain.PlanItem
	var gaps []domain.Gap
	var registrations []domain.RegistrationStep

	// Resolve the selected workflows from the catalog once, so classifyAgentItem can apply
	// workflow staleness to orchestrator-role agents without duplicating this lookup.
	// WorkflowIDs are guaranteed to be valid: ResolveArtifacts already returned ErrUnknownWorkflow
	// for any unrecognised ID, so Catalog.Workflow(id) always succeeds here.
	selectedWorkflows := make([]domain.Workflow, 0, len(in.WorkflowIDs))
	for _, id := range in.WorkflowIDs {
		if wf, ok := in.Catalog.Workflow(id); ok {
			selectedWorkflows = append(selectedWorkflows, wf)
		}
	}

	// Build plan items for each agent.
	for _, agent := range set.Agents {
		ref := domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: agent.Key}
		targetPath, _ := paths.Path(ref) // guaranteed present; EnumerateTargetPaths already errored if not

		model := in.Models[agent.Key]
		deployed := in.DeployedState[targetPath]

		// Classify before evaluating gaps: the GapNoModel emission rule depends on the action.
		item := classifyAgentItem(agent, targetPath, model, manifestUsable, in.Manifest.Manifest, deployed, desc, selectedWorkflows, in.ToolMappingsVersion, in.ProtocolVersion)
		items = append(items, item)

		// Surface a GapNoModel gap only when the file write that would occur requires a model
		// and neither the incoming selection nor the deployed file supplies one.
		//
		//   ActionUnchanged: no rewrite occurs; the deployed file's embedded model stands — never emit.
		//   ActionCreate:    a new file will be written — emit when the incoming model is unresolved.
		//   ActionUpdate:    the file will be regenerated — emit only when neither the incoming model
		//                    is resolved nor the deployed file carries an embedded ModelID that
		//                    regeneration will preserve.
		//   ActionConflict:  same rule as ActionUpdate.
		switch item.Action {
		case domain.ActionCreate:
			if !model.Resolved() {
				gaps = append(gaps, domain.Gap{
					Kind:    domain.GapNoModel,
					Subject: agent.Key,
					Detail:  "no model has been selected for agent " + agent.Key,
				})
			}
		case domain.ActionUpdate, domain.ActionConflict:
			if !model.Resolved() && deployed.ModelID == "" {
				gaps = append(gaps, domain.Gap{
					Kind:    domain.GapNoModel,
					Subject: agent.Key,
					Detail:  "no model has been selected for agent " + agent.Key,
				})
			}
		// ActionUnchanged: no gap emitted — file is not rewritten.
		}

		// Surface a gap for each generic tool the harness cannot map.
		if len(agent.Tools) > 0 {
			toolResult, toolErr := in.Module.Tools(domain.ToolRequest{
				AgentKey:    agent.Key,
				Generic:     agent.Tools,
				Placeholder: agent.ToolsPlaceholder,
			})
			if toolErr == nil {
				for _, res := range toolResult.Resolutions {
					if res.Outcome == domain.ToolUnmapped {
						gaps = append(gaps, domain.Gap{
							Kind:    domain.GapUnmappedTool,
							Subject: res.Generic,
							Detail:  fmt.Sprintf("generic tool %q has no harness mapping for agent %q", res.Generic, agent.Key),
						})
					}
				}
			}
		}
	}

	// Build plan items for each skill.
	for _, skill := range set.Skills {
		ref := domain.ArtifactRef{Kind: domain.ArtifactSkill, Key: skill.Key}
		targetPath, _ := paths.Path(ref) // guaranteed present; EnumerateTargetPaths already errored if not

		deployed := in.DeployedState[targetPath]
		item := classifySkillItem(skill, targetPath, manifestUsable, in.Manifest.Manifest, deployed)
		items = append(items, item)
	}

	// Build plan items for each hook bundle.
	for _, hook := range set.Hooks {
		ref := domain.ArtifactRef{Kind: domain.ArtifactHook, Key: hook.Key}
		targetPath, ok := paths.Path(ref)
		if !ok {
			// Hook not supported by this harness — skip plan item.
			continue
		}

		// Check registration steps for conflicts with existing workspace files.
		hookPlan, hookErr := in.Module.HookPlan(domain.HookPlanRequest{
			Bundle: hook,
			Scope:  in.Scope,
		})
		if hookErr == nil && hookPlan.Supported {
			for _, step := range hookPlan.Registration {
				registrations = append(registrations, step)
				if step.TargetPath != "" && registrationTargetExists(in, step.TargetPath) {
					// Registration target already exists in the workspace.
					gaps = append(gaps, domain.Gap{
						Kind:     domain.GapHookRegistration,
						Subject:  step.ID,
						Detail:   fmt.Sprintf("registration target %q already exists in workspace", step.TargetPath),
						Fragment: step.Fragment,
					})
				}
			}
		}

		deployed := in.DeployedState[targetPath]
		item := classifyHookItem(hook, targetPath, manifestUsable, in.Manifest.Manifest, deployed)
		items = append(items, item)
	}

	// Sort items deterministically: agents first, then skills, then hooks; within each kind by key.
	sort.Slice(items, func(i, j int) bool {
		oi := artifactKindOrder(items[i].Ref.Kind)
		oj := artifactKindOrder(items[j].Ref.Kind)
		if oi != oj {
			return oi < oj
		}
		return items[i].Ref.Key < items[j].Ref.Key
	})

	return domain.Plan{
		Mode:          in.Mode,
		Harness:       in.Module.Ref(),
		WorkspacePath: in.WorkspacePath,
		Scope:         in.Scope,
		Items:         items,
		Gaps:          gaps,
		Registrations: registrations,
		Workflows:     in.WorkflowIDs,
	}, nil
}

// registrationTargetExists reports whether the workspace file at the given relative path
// exists. When Input.WorkspaceFileExists is non-nil, that injected function is used,
// keeping Build free of direct file-system access. When nil, the target is treated as
// absent and no GapHookRegistration gap is surfaced for it.
func registrationTargetExists(in Input, relPath string) bool {
	if in.WorkspaceFileExists != nil {
		return in.WorkspaceFileExists(relPath)
	}
	return false
}

// artifactKindOrder returns a sort key for artifact kinds so that agents sort before skills
// and skills sort before hooks.
func artifactKindOrder(k domain.ArtifactKind) int {
	switch k {
	case domain.ArtifactAgent:
		return 0
	case domain.ArtifactSkill:
		return 1
	case domain.ArtifactHook:
		return 2
	default:
		return 99
	}
}

// classifyAgentItem classifies one agent into a PlanItem with the appropriate action.
// The classification contract (see ContractsDesign.md Classification Contract):
//  1. No deployed file → ActionCreate
//  2. No usable manifest → ActionConflict (ManifestMissing: true)
//  3. No manifest entry at current target path → ActionConflict (ManifestMissing: true)
//  4. Hash mismatch → ActionConflict (local modification)
//  5. Version staleness check against deployed-file versions
//  6a. For orchestrator-role agents: workflow staleness check; merged into deltas
//  6b. For all agents: protocol staleness check; merged into deltas
//  7. Stale or no version info → ActionUpdate
//  8. → ActionUnchanged
//
// selectedWorkflows is the full list of workflows resolved from plan.Input.WorkflowIDs for
// this run. It is only consulted for agents with Role == domain.RoleOrchestrator; it may
// be nil or empty for other agents with no effect.
//
// toolMappingsVersion is the current effective mapping hash from plan.Input.ToolMappingsVersion.
// When it differs from deployed.ToolMappingsVersion, a "tool_mappings_version" staleness
// delta is produced so the destination field is regenerated on the next update.
//
// protocolVersion is the current protocol source version from plan.Input.ProtocolVersion.
// Protocol staleness applies to every agent regardless of role.
func classifyAgentItem(
	agent domain.Agent,
	targetPath string,
	model domain.ModelSelection,
	manifestUsable bool,
	m domain.Manifest,
	deployed domain.DeployedArtifactState,
	desc *domain.HarnessDescriptor,
	selectedWorkflows []domain.Workflow,
	toolMappingsVersion string,
	protocolVersion string,
) domain.PlanItem {
	ref := domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: agent.Key}
	item := domain.PlanItem{
		Ref:        ref,
		SourcePath: agent.SourcePath,
		TargetPath: targetPath,
		Model:      model,
	}

	// Step 1: No deployed file — short-circuit to create before consulting the manifest.
	if !deployed.Present {
		item.Action = domain.ActionCreate
		item.Reason = "new artifact; no file at target path"
		return item
	}

	// Step 2: No usable manifest — any file on disk could have been placed there manually.
	if !manifestUsable {
		item.Action = domain.ActionConflict
		item.Conflict = &domain.LocalModification{
			CurrentHash:     deployed.ContentHash,
			ManifestMissing: true,
		}
		item.Reason = "deployed file found but no manifest is available; cannot confirm it was written by this tool"
		return item
	}

	// Step 3: Target-path-aware manifest lookup. An entry for the same Kind+Key at a
	// different target path (e.g. a different harness) does not apply to this run.
	entry, inManifest := m.LookupAt(ref, targetPath)
	if !inManifest {
		item.Action = domain.ActionConflict
		item.Conflict = &domain.LocalModification{
			CurrentHash:     deployed.ContentHash,
			ManifestMissing: true,
		}
		item.Reason = "file exists on disk but has no manifest record for this target path; cannot confirm origin"
		return item
	}

	// Step 4: Check for local modification before staleness.
	if deployed.ContentHash != entry.ContentHash {
		item.Action = domain.ActionConflict
		item.Conflict = &domain.LocalModification{
			RecordedHash: entry.ContentHash,
			CurrentHash:  deployed.ContentHash,
		}
		item.Reason = "deployed file has been locally modified since last deployment"
		return item
	}

	// Step 5: Compare version fields against the deployed-file versions.
	stamps := domain.VersionStamps{
		Version:             agent.Version,
		TransformVersion:    desc.TransformVersion,
		InjectionsVersion:   desc.InjectionsVersion,
		ToolMappingsVersion: toolMappingsVersion,
	}
	if agent.Role == domain.RoleOrchestrator {
		stamps.OrchestratorInjectionsVersion = desc.OrchestratorInjectionsVersion
	}
	deltas := AgentStaleness(deployed, agent, stamps)

	// Step 6a: For orchestrator-role agents, also compare the deployed workflow set
	// against the newly selected workflows. Workflow drift composes with version-field
	// deltas into a single update item; never produces a separate plan item.
	var drift WorkflowDrift
	if agent.Role == domain.RoleOrchestrator {
		drift = WorkflowStaleness(deployed.Workflows, selectedWorkflows)
		deltas = append(deltas, drift.Deltas()...)
	}

	// Step 6b: For every agent, compare the deployed protocol version against the source
	// version. Protocol staleness applies regardless of role, unlike workflow drift.
	protocolDrift := ProtocolStaleness(deployed, protocolVersion)
	if delta, ok := protocolDrift.Delta(); ok {
		deltas = append(deltas, delta)
	}

	// Step 7: Stale or no version info → update.
	if len(deltas) > 0 || !deployed.HasVersionInfo() {
		item.Action = domain.ActionUpdate
		item.Stale = deltas
		reasons := buildAgentUpdateReasons(deltas, !deployed.HasVersionInfo(), drift, protocolDrift)
		item.Reason = "stale: " + reasons
		return item
	}

	// Step 8: All checks passed — unchanged.
	item.Action = domain.ActionUnchanged
	return item
}

// classifySkillItem classifies one skill into a PlanItem with the appropriate action.
func classifySkillItem(
	skill domain.Skill,
	targetPath string,
	manifestUsable bool,
	m domain.Manifest,
	deployed domain.DeployedArtifactState,
) domain.PlanItem {
	ref := domain.ArtifactRef{Kind: domain.ArtifactSkill, Key: skill.Key}
	sourcePath := skill.SourceDir
	if skill.EntryFile != "" {
		sourcePath = filepath.Join(skill.SourceDir, skill.EntryFile)
	}
	item := domain.PlanItem{
		Ref:        ref,
		SourcePath: sourcePath,
		TargetPath: targetPath,
	}

	// Step 1: No deployed file.
	if !deployed.Present {
		item.Action = domain.ActionCreate
		item.Reason = "new skill; no file at target path"
		return item
	}

	// Step 2: No usable manifest.
	if !manifestUsable {
		item.Action = domain.ActionConflict
		item.Conflict = &domain.LocalModification{
			CurrentHash:     deployed.ContentHash,
			ManifestMissing: true,
		}
		item.Reason = "deployed skill file found but no manifest is available; cannot confirm origin"
		return item
	}

	// Step 3: Target-path-aware manifest lookup.
	entry, inManifest := m.LookupAt(ref, targetPath)
	if !inManifest {
		item.Action = domain.ActionConflict
		item.Conflict = &domain.LocalModification{
			CurrentHash:     deployed.ContentHash,
			ManifestMissing: true,
		}
		item.Reason = "skill file exists on disk but has no manifest record for this target path; cannot confirm origin"
		return item
	}

	// Step 4: Check for local modification.
	if deployed.ContentHash != entry.ContentHash {
		item.Action = domain.ActionConflict
		item.Conflict = &domain.LocalModification{
			RecordedHash: entry.ContentHash,
			CurrentHash:  deployed.ContentHash,
		}
		item.Reason = "deployed skill file has been locally modified since last deployment"
		return item
	}

	// Step 5: Staleness check.
	deltas := SkillStaleness(deployed, skill)

	// Step 7.
	if len(deltas) > 0 || !deployed.HasVersionInfo() {
		item.Action = domain.ActionUpdate
		item.Stale = deltas
		reasons := buildUpdateReasons(deltas, !deployed.HasVersionInfo())
		item.Reason = "stale: " + reasons
		return item
	}

	// Step 8.
	item.Action = domain.ActionUnchanged
	return item
}

// classifyHookItem classifies one hook bundle into a PlanItem with the appropriate action.
func classifyHookItem(
	hook domain.HookBundle,
	targetPath string,
	manifestUsable bool,
	m domain.Manifest,
	deployed domain.DeployedArtifactState,
) domain.PlanItem {
	ref := domain.ArtifactRef{Kind: domain.ArtifactHook, Key: hook.Key}
	item := domain.PlanItem{
		Ref:        ref,
		SourcePath: hook.SourceDir,
		TargetPath: targetPath,
	}

	// Step 1: No deployed file.
	if !deployed.Present {
		item.Action = domain.ActionCreate
		item.Reason = "new hook bundle; no file at target path"
		return item
	}

	// Step 2: No usable manifest.
	if !manifestUsable {
		item.Action = domain.ActionConflict
		item.Conflict = &domain.LocalModification{
			CurrentHash:     deployed.ContentHash,
			ManifestMissing: true,
		}
		item.Reason = "deployed hook found but no manifest is available; cannot confirm origin"
		return item
	}

	// Step 3: Target-path-aware manifest lookup.
	entry, inManifest := m.LookupAt(ref, targetPath)
	if !inManifest {
		item.Action = domain.ActionConflict
		item.Conflict = &domain.LocalModification{
			CurrentHash:     deployed.ContentHash,
			ManifestMissing: true,
		}
		item.Reason = "hook file exists on disk but has no manifest record for this target path; cannot confirm origin"
		return item
	}

	// Step 4: Check for local modification.
	if deployed.ContentHash != entry.ContentHash {
		item.Action = domain.ActionConflict
		item.Conflict = &domain.LocalModification{
			RecordedHash: entry.ContentHash,
			CurrentHash:  deployed.ContentHash,
		}
		item.Reason = "deployed hook file has been locally modified since last deployment"
		return item
	}

	// Step 5: Staleness check.
	deltas := HookStaleness(deployed, hook)

	// Step 7.
	if len(deltas) > 0 || !deployed.HasVersionInfo() {
		item.Action = domain.ActionUpdate
		item.Stale = deltas
		reasons := buildUpdateReasons(deltas, !deployed.HasVersionInfo())
		item.Reason = "stale: " + reasons
		return item
	}

	// Step 8.
	item.Action = domain.ActionUnchanged
	return item
}

// buildUpdateReasons composes the Reason string for an ActionUpdate item. It combines
// version-field delta text and the no-version-info sentinel, each in a separate formatter
// so they remain distinguishable downstream.
func buildUpdateReasons(deltas []domain.VersionDelta, noVersionInfo bool) string {
	var parts []string
	if len(deltas) > 0 {
		parts = append(parts, formatVersionDeltas(deltas))
	}
	if noVersionInfo {
		parts = append(parts, "deployed file carries no readable version stamps")
	}
	return strings.Join(parts, "; ")
}

// buildAgentUpdateReasons composes the Reason string for an agent ActionUpdate item. It
// extends buildUpdateReasons by also appending the workflow drift reason and protocol drift
// reason when present. Both are formatted separately rather than through formatVersionDeltas,
// which would produce unhelpful "workflow:x changed from..." or "protocol_version changed from..."
// text.
//
// Reason part order: version-field deltas; no-version-info note; workflow drift; protocol drift.
func buildAgentUpdateReasons(deltas []domain.VersionDelta, noVersionInfo bool, drift WorkflowDrift, protocolDrift ProtocolDrift) string {
	var parts []string

	// Version-field deltas only — exclude workflow-prefixed and protocol deltas from the
	// generic field formatter; both have dedicated human-readable reason methods.
	var versionDeltas []domain.VersionDelta
	for _, d := range deltas {
		if strings.HasPrefix(d.Field, WorkflowDeltaFieldPrefix) {
			continue
		}
		if d.Field == ProtocolDeltaField {
			continue
		}
		versionDeltas = append(versionDeltas, d)
	}
	if len(versionDeltas) > 0 {
		parts = append(parts, formatVersionDeltas(versionDeltas))
	}

	if noVersionInfo {
		parts = append(parts, "deployed file carries no readable version stamps")
	}

	// Workflow drift reason is formatted separately so it names workflow IDs.
	if drift.Stale() {
		parts = append(parts, drift.Reason())
	}

	// Protocol drift reason is formatted separately so it names the protocol.
	if protocolDrift.Stale() {
		parts = append(parts, protocolDrift.Reason())
	}

	return strings.Join(parts, "; ")
}

// formatVersionDeltas returns a human-readable summary of version deltas for use in Reason fields.
func formatVersionDeltas(deltas []domain.VersionDelta) string {
	parts := make([]string, 0, len(deltas))
	for _, d := range deltas {
		parts = append(parts, fmt.Sprintf("%s changed from %q to %q", d.Field, d.Deployed, d.Source))
	}
	return strings.Join(parts, "; ")
}
