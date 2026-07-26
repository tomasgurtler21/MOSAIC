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
	// Resolve the complete artifact set from workflow selections, utility agents, and hooks.
	set, err := ResolveArtifacts(in.Catalog, in.WorkflowIDs, in.UtilityAgentIDs, in.HookIDs)
	if err != nil {
		return domain.Plan{}, err
	}

	desc := in.Module.Descriptor()
	manifestUsable := in.Manifest.State == manifest.StatePresent

	var items []domain.PlanItem
	var gaps []domain.Gap
	var registrations []domain.RegistrationStep

	// Build plan items for each agent.
	for _, agent := range set.Agents {
		targetPath, pathErr := in.Module.TargetPath(domain.TargetPathRequest{
			Kind:     domain.ArtifactAgent,
			Key:      agent.Key,
			FileName: filepath.Base(agent.SourcePath),
			Scope:    in.Scope,
			GOOS:     in.GOOS,
		})
		if pathErr != nil {
			return domain.Plan{}, fmt.Errorf("resolve target path for agent %q: %w", agent.Key, pathErr)
		}

		// Surface a gap if the agent has no resolved model selection.
		model := in.Models[agent.Key]
		if !model.Resolved() {
			gaps = append(gaps, domain.Gap{
				Kind:    domain.GapNoModel,
				Subject: agent.Key,
				Detail:  "no model has been selected for agent " + agent.Key,
			})
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

		item := classifyAgentItem(agent, targetPath, model, manifestUsable, in.Manifest.Manifest, in.DeployedHashes, desc)
		items = append(items, item)
	}

	// Build plan items for each skill.
	for _, skill := range set.Skills {
		targetPath, pathErr := in.Module.TargetPath(domain.TargetPathRequest{
			Kind:     domain.ArtifactSkill,
			Key:      skill.Key,
			FileName: skill.EntryFile,
			Scope:    in.Scope,
			GOOS:     in.GOOS,
		})
		if pathErr != nil {
			return domain.Plan{}, fmt.Errorf("resolve target path for skill %q: %w", skill.Key, pathErr)
		}

		item := classifySkillItem(skill, targetPath, manifestUsable, in.Manifest.Manifest, in.DeployedHashes)
		items = append(items, item)
	}

	// Build plan items for each hook bundle.
	for _, hook := range set.Hooks {
		targetPath, pathErr := in.Module.TargetPath(domain.TargetPathRequest{
			Kind:  domain.ArtifactHook,
			Key:   hook.Key,
			Scope: in.Scope,
			GOOS:  in.GOOS,
		})
		if pathErr != nil {
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

		item := classifyHookItem(hook, targetPath, manifestUsable, in.Manifest.Manifest, in.DeployedHashes)
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
func classifyAgentItem(
	agent domain.Agent,
	targetPath string,
	model domain.ModelSelection,
	manifestUsable bool,
	m domain.Manifest,
	deployedHashes map[string]string,
	desc *domain.HarnessDescriptor,
) domain.PlanItem {
	ref := domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: agent.Key}
	item := domain.PlanItem{
		Ref:        ref,
		SourcePath: agent.SourcePath,
		TargetPath: targetPath,
		Model:      model,
	}

	currentHash, fileOnDisk := deployedHashes[targetPath]

	if !manifestUsable {
		// Without a trustworthy manifest, any file on disk could have been placed there
		// manually. The conservative policy classifies such files as locally modified.
		if fileOnDisk {
			item.Action = domain.ActionConflict
			item.Conflict = &domain.LocalModification{
				CurrentHash:     currentHash,
				ManifestMissing: true,
			}
			item.Reason = "deployed file found but no manifest is available; cannot confirm it was written by this tool"
		} else {
			item.Action = domain.ActionCreate
			item.Reason = "new artifact; no prior deployment found"
		}
		return item
	}

	entry, inManifest := m.Lookup(ref)
	if !inManifest {
		if fileOnDisk {
			// File is on disk but has no manifest record — conservatively classify as conflict.
			item.Action = domain.ActionConflict
			item.Conflict = &domain.LocalModification{
				CurrentHash:     currentHash,
				ManifestMissing: true,
			}
			item.Reason = "file exists on disk but has no manifest record; cannot confirm origin"
		} else {
			item.Action = domain.ActionCreate
			item.Reason = "new artifact; no prior deployment found"
		}
		return item
	}

	// The artifact is in the manifest. Check for local modification before staleness.
	if fileOnDisk && currentHash != entry.ContentHash {
		item.Action = domain.ActionConflict
		item.Conflict = &domain.LocalModification{
			RecordedHash: entry.ContentHash,
			CurrentHash:  currentHash,
		}
		item.Reason = "deployed file has been locally modified since last deployment"
		return item
	}

	// Compare version fields independently.
	stamps := domain.VersionStamps{
		Version:           agent.Version,
		TransformVersion:  desc.TransformVersion,
		InjectionsVersion: desc.InjectionsVersion,
	}
	deltas := AgentStaleness(entry, agent, stamps)
	if len(deltas) > 0 {
		item.Action = domain.ActionUpdate
		item.Stale = deltas
		item.Reason = "stale: " + formatVersionDeltas(deltas)
		return item
	}

	item.Action = domain.ActionUnchanged
	return item
}

// classifySkillItem classifies one skill into a PlanItem with the appropriate action.
func classifySkillItem(
	skill domain.Skill,
	targetPath string,
	manifestUsable bool,
	m domain.Manifest,
	deployedHashes map[string]string,
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

	currentHash, fileOnDisk := deployedHashes[targetPath]

	if !manifestUsable {
		if fileOnDisk {
			item.Action = domain.ActionConflict
			item.Conflict = &domain.LocalModification{
				CurrentHash:     currentHash,
				ManifestMissing: true,
			}
			item.Reason = "deployed skill file found but no manifest is available; cannot confirm origin"
		} else {
			item.Action = domain.ActionCreate
			item.Reason = "new skill; no prior deployment found"
		}
		return item
	}

	entry, inManifest := m.Lookup(ref)
	if !inManifest {
		if fileOnDisk {
			item.Action = domain.ActionConflict
			item.Conflict = &domain.LocalModification{
				CurrentHash:     currentHash,
				ManifestMissing: true,
			}
			item.Reason = "skill file exists on disk but has no manifest record; cannot confirm origin"
		} else {
			item.Action = domain.ActionCreate
			item.Reason = "new skill; no prior deployment found"
		}
		return item
	}

	if fileOnDisk && currentHash != entry.ContentHash {
		item.Action = domain.ActionConflict
		item.Conflict = &domain.LocalModification{
			RecordedHash: entry.ContentHash,
			CurrentHash:  currentHash,
		}
		item.Reason = "deployed skill file has been locally modified since last deployment"
		return item
	}

	deltas := SkillStaleness(entry, skill)
	if len(deltas) > 0 {
		item.Action = domain.ActionUpdate
		item.Stale = deltas
		item.Reason = "stale: " + formatVersionDeltas(deltas)
		return item
	}

	item.Action = domain.ActionUnchanged
	return item
}

// classifyHookItem classifies one hook bundle into a PlanItem with the appropriate action.
func classifyHookItem(
	hook domain.HookBundle,
	targetPath string,
	manifestUsable bool,
	m domain.Manifest,
	deployedHashes map[string]string,
) domain.PlanItem {
	ref := domain.ArtifactRef{Kind: domain.ArtifactHook, Key: hook.Key}
	item := domain.PlanItem{
		Ref:        ref,
		SourcePath: hook.SourceDir,
		TargetPath: targetPath,
	}

	currentHash, fileOnDisk := deployedHashes[targetPath]

	if !manifestUsable {
		if fileOnDisk {
			item.Action = domain.ActionConflict
			item.Conflict = &domain.LocalModification{
				CurrentHash:     currentHash,
				ManifestMissing: true,
			}
			item.Reason = "deployed hook found but no manifest is available; cannot confirm origin"
		} else {
			item.Action = domain.ActionCreate
			item.Reason = "new hook bundle; no prior deployment found"
		}
		return item
	}

	entry, inManifest := m.Lookup(ref)
	if !inManifest {
		if fileOnDisk {
			item.Action = domain.ActionConflict
			item.Conflict = &domain.LocalModification{
				CurrentHash:     currentHash,
				ManifestMissing: true,
			}
			item.Reason = "hook file exists on disk but has no manifest record; cannot confirm origin"
		} else {
			item.Action = domain.ActionCreate
			item.Reason = "new hook bundle; no prior deployment found"
		}
		return item
	}

	if fileOnDisk && currentHash != entry.ContentHash {
		item.Action = domain.ActionConflict
		item.Conflict = &domain.LocalModification{
			RecordedHash: entry.ContentHash,
			CurrentHash:  currentHash,
		}
		item.Reason = "deployed hook file has been locally modified since last deployment"
		return item
	}

	deltas := HookStaleness(entry, hook)
	if len(deltas) > 0 {
		item.Action = domain.ActionUpdate
		item.Stale = deltas
		item.Reason = "stale: " + formatVersionDeltas(deltas)
		return item
	}

	item.Action = domain.ActionUnchanged
	return item
}

// formatVersionDeltas returns a human-readable summary of version deltas for use in Reason fields.
func formatVersionDeltas(deltas []domain.VersionDelta) string {
	parts := make([]string, 0, len(deltas))
	for _, d := range deltas {
		parts = append(parts, fmt.Sprintf("%s changed from %q to %q", d.Field, d.Deployed, d.Source))
	}
	return strings.Join(parts, "; ")
}
