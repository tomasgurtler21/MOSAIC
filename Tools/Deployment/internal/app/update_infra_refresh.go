package app

// update_infra_refresh.go holds the update flow's handling of infrastructure declarations in
// orchestrator-role files: each present file has the catalog-backed declarations it already
// carries refreshed against the catalog, and nothing else in the region changes. The update
// flow never adds a declaration.

import (
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/plan"
	"mosaic-deploy/internal/transform"
)

// refreshIntent is the planner intent for the update flow: refresh whatever catalog-backed
// declarations each orchestrator-role file already carries.
func refreshIntent() plan.InfrastructureDeclarationIntent {
	return plan.InfrastructureDeclarationIntent{RefreshDeclared: true}
}

// probedOrchestrator is an orchestrator-role file the update flow has already probed.
type probedOrchestrator struct {
	Agent      domain.Agent
	TargetPath string
	State      domain.DeployedArtifactState
}

// orchestratorRefresh builds the infrastructure handling for the present orchestrator-role
// files among probed. Only files whose declarations drifted are targets; a file whose
// declarations are current gets no infrastructure input, so its region is carried over
// byte-for-byte even when the file is rewritten for another reason.
func (s *service) orchestratorRefresh(probed []probedOrchestrator) orchestratorInfra {
	infraAgents := s.deps.Catalog.InfrastructureAgents()
	result := orchestratorInfra{keys: infraKeysOf(infraAgents), blocks: s.buildInfrastructureBlocks(infraKeysOf(infraAgents))}
	for _, p := range probed {
		if !p.State.Present || p.TargetPath == "" {
			continue
		}
		drift := plan.InfrastructureStaleness(p.State.InfrastructureDeclarations, s.deps.Catalog, refreshIntent())
		if drift.IsStale() {
			result.targets = append(result.targets, orchestratorTarget{
				Agent: p.Agent, TargetPath: p.TargetPath, State: p.State, Drift: drift,
			})
		}
	}
	return result
}

// refreshContentOptions returns the buildContent options that refresh, never extend, the
// stale declarations of each drifted orchestrator-role file, using only the catalog blocks
// of that file's stale keys.
func (o orchestratorInfra) refreshContentOptions(s *service) []buildContentOption {
	if len(o.targets) == 0 {
		return nil
	}
	byPath := make(map[string]infraContentInput, len(o.targets))
	for _, t := range o.targets {
		byPath[t.TargetPath] = infraContentInput{
			Blocks: s.buildInfrastructureBlocks(t.Drift.Keys()),
			Mode:   transform.InfrastructureMergeRefresh,
		}
	}
	return []buildContentOption{withInfrastructureDeclarations(byPath)}
}
