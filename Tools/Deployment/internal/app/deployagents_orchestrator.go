package app

// deployagents_orchestrator.go holds the orchestrator handling of the deploy-agents flow:
// when infrastructure agents are deployed, every orchestrator-role file already present in
// the workspace whose declarations need to change joins the run, so the new agents are
// declared there. The files are never created, never rebuilt from the catalog beyond the
// declarations, and keep their deployed workflows and model.

import (
	"path/filepath"
	"strings"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/plan"
	"mosaic-deploy/internal/transform"
)

// orchestratorTarget is one present orchestrator-role file that needs declaration changes.
type orchestratorTarget struct {
	Agent      domain.Agent
	TargetPath string
	State      domain.DeployedArtifactState
	Drift      plan.InfrastructureDrift
}

// orchestratorInfra is the set of orchestrator-role files this run must rewrite so that the
// selected infrastructure agents are declared. The zero value means no file is affected.
type orchestratorInfra struct {
	targets []orchestratorTarget
	keys    []string // selected infrastructure agent keys known to the catalog
	blocks  []transform.InfrastructureBlock
}

// probeOrchestratorInfra finds the present orchestrator-role files whose declarations lack
// or hold a stale copy of any selected infrastructure agent. Files that are absent, have no
// catalog counterpart, or already declare every selected agent currently are left out, so
// they are neither planned, asked about, nor written.
func (s *service) probeOrchestratorInfra(module domain.HarnessModule, workspace string, scope domain.Scope, infraAgents []domain.Agent) orchestratorInfra {
	if len(infraAgents) == 0 {
		return orchestratorInfra{}
	}
	keys := infraKeysOf(infraAgents)
	blocks := s.buildInfrastructureBlocks(keys)
	intent := plan.InfrastructureDeclarationIntent{EnsureKeys: keys}

	candidates := []domain.Agent{s.deps.Catalog.Orchestrator()}
	if script, ok := s.deps.Catalog.OrchestratorScript(); ok {
		candidates = append(candidates, script)
	}

	result := orchestratorInfra{keys: keys, blocks: blocks}
	for _, agent := range candidates {
		if agent.Key == "" {
			continue
		}
		if _, known := s.deps.Catalog.Agent(agent.Key); !known {
			continue
		}
		path, err := module.TargetPath(domain.TargetPathRequest{
			Kind:     domain.ArtifactAgent,
			Key:      agent.Key,
			FileName: filepath.Base(agent.SourcePath),
			Scope:    scope,
			GOOS:     s.deps.GOOS,
		})
		if err != nil {
			continue
		}
		state := probeDeployedArtifact(workspace, path, module.Descriptor().Frontmatter.ModelKey)
		if !state.Present {
			continue
		}
		drift := plan.InfrastructureStaleness(state.InfrastructureDeclarations, s.deps.Catalog, intent)
		if !drift.IsStale() {
			continue
		}
		result.targets = append(result.targets, orchestratorTarget{Agent: agent, TargetPath: path, State: state, Drift: drift})
	}
	return result
}

// agentKeys returns the orchestrator-role keys that join the artifact set.
func (o orchestratorInfra) agentKeys() []string {
	keys := make([]string, 0, len(o.targets))
	for _, t := range o.targets {
		keys = append(keys, t.Agent.Key)
	}
	return keys
}

// paths returns the target paths of the affected files.
func (o orchestratorInfra) paths() []string {
	paths := make([]string, 0, len(o.targets))
	for _, t := range o.targets {
		paths = append(paths, t.TargetPath)
	}
	return paths
}

// seedStates returns the already-probed states by target path, so each file is read once.
func (o orchestratorInfra) seedStates() map[string]domain.DeployedArtifactState {
	if len(o.targets) == 0 {
		return nil
	}
	seed := make(map[string]domain.DeployedArtifactState, len(o.targets))
	for _, t := range o.targets {
		seed[t.TargetPath] = t.State
	}
	return seed
}

// isOrchestratorKey reports whether key belongs to an affected orchestrator-role file.
func (o orchestratorInfra) isOrchestratorKey(key string) bool {
	for _, t := range o.targets {
		if t.Agent.Key == key {
			return true
		}
	}
	return false
}

// intent returns the declaration intent for the planner: ensure every selected key.
func (o orchestratorInfra) intent() plan.InfrastructureDeclarationIntent {
	return plan.InfrastructureDeclarationIntent{EnsureKeys: o.keys}
}

// withDeployedModels adds each affected orchestrator's deployed model to models, copying the
// map so the caller's value is not mutated. Models of other agents are kept.
func (o orchestratorInfra) withDeployedModels(models map[string]domain.ModelSelection) map[string]domain.ModelSelection {
	if len(o.targets) == 0 {
		return models
	}
	merged := make(map[string]domain.ModelSelection, len(models)+len(o.targets))
	for k, v := range models {
		merged[k] = v
	}
	for _, t := range o.targets {
		if t.State.ModelID != "" {
			merged[t.Agent.Key] = domain.ModelSelection{ModelID: t.State.ModelID, Origin: domain.OriginDeployed}
		}
	}
	return merged
}

// withoutOrchestrators returns agents minus the affected orchestrator-role agents. Their
// models come from the deployed files, so they must not enter model or tool questions.
func (o orchestratorInfra) withoutOrchestrators(agents []domain.Agent) []domain.Agent {
	kept := make([]domain.Agent, 0, len(agents))
	for _, a := range agents {
		if !o.isOrchestratorKey(a.Key) {
			kept = append(kept, a)
		}
	}
	return kept
}

// annotate makes each affected orchestrator item name the infrastructure agents behind it.
// The planner compares against an empty workflow selection in this flow, so its own reason
// and deltas would describe workflow removals that are never carried out; an update item is
// therefore reported with the declaration drift alone. A conflict item keeps its reason and
// gains the drift.
func (o orchestratorInfra) annotate(items []domain.PlanItem) {
	for i := range items {
		t, ok := o.targetFor(items[i].TargetPath)
		if !ok {
			continue
		}
		switch items[i].Action {
		case domain.ActionUpdate:
			items[i].Stale = t.Drift.Deltas()
			items[i].Reason = "stale: " + t.Drift.Reason()
		case domain.ActionConflict:
			items[i].Reason += "; " + t.Drift.Reason()
		}
	}
}

func (o orchestratorInfra) targetFor(path string) (orchestratorTarget, bool) {
	for _, t := range o.targets {
		if t.TargetPath == path {
			return t, true
		}
	}
	return orchestratorTarget{}, false
}

// skipGap returns the manual-step entry for an orchestrator item the user chose to skip: the
// declarations were not added, and the entry says how to add them by hand or by re-running.
func (o orchestratorInfra) skipGap(item domain.PlanItem) (domain.Gap, bool) {
	t, ok := o.targetFor(item.TargetPath)
	if !ok {
		return domain.Gap{}, false
	}
	driftKeys := t.Drift.Keys()
	var blocks []transform.InfrastructureBlock
	for _, b := range o.blocks {
		for _, k := range driftKeys {
			if b.Key == k {
				blocks = append(blocks, b)
			}
		}
	}
	var fragment strings.Builder
	for _, b := range blocks {
		// A label line names each declaration by its section name so the user can find it again.
		section, _ := transform.AssembleInfrastructureBlocks([]transform.InfrastructureBlock{b})
		fragment.WriteString("<!-- InfrastructureAgent:" + b.Key + " -->\n")
		fragment.Write(section)
	}
	return domain.Gap{
		Kind:    domain.GapManualStep,
		Subject: item.TargetPath,
		Owner:   item.Ref.Key,
		Detail: item.TargetPath + " was skipped, so the infrastructure agents " + strings.Join(driftKeys, ", ") +
			" are not declared in it. Paste the fragment below inside its <InfrastructureAgents> region, " +
			"or deploy the agents again and choose to overwrite the file.",
		Fragment: fragment.String(),
	}, true
}

// contentOptions returns the buildContent options that declare the selected agents in each
// affected file and keep each file's AvailableWorkflows region exactly as deployed.
func (o orchestratorInfra) contentOptions(s *service) []buildContentOption {
	if len(o.targets) == 0 {
		return nil
	}
	paths := o.paths()
	return []buildContentOption{
		withInfrastructureDeclarations(s.infraContentForPaths(paths, o.keys, transform.InfrastructureMergeEnsure)),
		withPreservedWorkflows(paths),
	}
}

// withPreservedWorkflows keeps the AvailableWorkflows region of the orchestrator-role file at
// each target path byte-for-byte as deployed. Workflows are never rebuilt from the catalog.
func withPreservedWorkflows(paths []string) buildContentOption {
	set := make(map[string]bool, len(paths))
	for _, p := range paths {
		set[p] = true
	}
	return func(o *buildContentOptions) { o.preserveWorkflows = set }
}

// deployedReaderFor returns a deployed-bytes reader that serves only the affected
// orchestrator-role files. Every other item is rendered without deployed bytes, as before.
func (o orchestratorInfra) deployedReaderFor(workspace string) func(domain.PlanItem) []byte {
	if len(o.targets) == 0 {
		return nil
	}
	return func(item domain.PlanItem) []byte {
		if _, ok := o.targetFor(item.TargetPath); !ok {
			return nil
		}
		return readDeployedFile(workspace, item.TargetPath)
	}
}
