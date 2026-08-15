package app

// utility_infra_service.go implements Service.DeployUtilityInfrastructure: a narrowed deploy
// flow that asks only the Utility and Infrastructure agent questions, never workflow or hook
// questions, and produces a plan containing exactly the selected agents and the skills they
// require.

import (
	"context"

	"mosaic-deploy/internal/config"
	"mosaic-deploy/internal/deploy"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/plan"
	"mosaic-deploy/internal/todo"
)

// deployUtilityInfrastructure is the real implementation of Service.DeployUtilityInfrastructure.
// It reuses the deploy-new resolver sequence but omits the workflow and hook resolvers entirely,
// so QWorkflows and QHooks are never presented to the user.
func deployUtilityInfrastructure(ctx context.Context, s *service, req UtilityInfraRequest) (domain.RunSummary, error) {
	harnessID := req.HarnessID
	if harnessID == "" {
		id, err := s.askHarness(ctx)
		if err != nil {
			return domain.RunSummary{}, err
		}
		harnessID = id
	}
	module, err := s.deps.Registry.Resolve(harnessID)
	if err != nil {
		return domain.RunSummary{}, err
	}
	defer module.Close() //nolint:errcheck

	harnessRef := module.Ref()

	workspace := req.WorkspacePath
	if workspace == "" {
		ws, werr := s.askWorkspace(ctx)
		if werr != nil {
			return domain.RunSummary{}, werr
		}
		workspace = ws
	}

	scope := domain.ScopeProject

	// Load the protocol source once for the run.
	protocol, err := s.loadProtocol()
	if err != nil {
		return domain.RunSummary{}, err
	}

	// Load the deployed-sections bundle once for the run.
	bundle, err := s.loadBundle()
	if err != nil {
		return domain.RunSummary{}, err
	}

	// Utility agent selection — QWorkflows and QHooks are deliberately absent.
	utilityIDs := req.UtilityAgentIDs
	if utilityIDs == nil {
		if req.SkipAll[domain.QUtilityAgents] {
			utilityIDs = []string{}
		} else if len(s.deps.Catalog.UtilityAgents()) > 0 {
			ids, err := s.askUtilityAgents(ctx)
			if err != nil {
				return domain.RunSummary{}, err
			}
			utilityIDs = ids
		} else {
			utilityIDs = []string{}
		}
	}

	// Infrastructure agent selection.
	infraAgentIDs := req.InfrastructureAgentIDs
	if infraAgentIDs == nil {
		if req.SkipAll[domain.QInfrastructureAgents] {
			infraAgentIDs = []string{}
		} else if len(s.deps.Catalog.InfrastructureAgents()) > 0 {
			ids, err := s.askInfrastructureAgents(ctx)
			if err != nil {
				return domain.RunSummary{}, err
			}
			infraAgentIDs = ids
		} else {
			infraAgentIDs = []string{}
		}
	}

	// Resolve infrastructure agent structs from the catalog for model resolution and content
	// generation. Resolution happens before probe set construction so that only catalog-known
	// agent keys are added to the probe set. Any ID that does not exist in the catalog is
	// silently skipped, matching the existing behavior for content generation and the same
	// pattern applied to the deploy-new flow.
	infraAgents := make([]domain.Agent, 0, len(infraAgentIDs))
	for _, id := range infraAgentIDs {
		if a, ok := s.deps.Catalog.Agent(id); ok {
			infraAgents = append(infraAgents, a)
		}
	}
	foundInfraKeys := make([]string, 0, len(infraAgents))
	for _, a := range infraAgents {
		foundInfraKeys = append(foundInfraKeys, a.Key)
	}

	// Resolve the artifact set for probing deployed state and model/tool resolution.
	// Infrastructure agents are now included (foundInfraKeys) so their target paths are
	// enumerated and their on-disk state is probed, matching the same fix applied to the
	// deploy-new flow (I3.1). Without them, the planner's DeployedState map misses infra agent
	// entries, causing every infra agent to be classified as a new create on every run after
	// the first. Infrastructure agent model resolution is still handled in a dedicated second
	// batch below; they are filtered out of the first batch to avoid duplicate model prompts.
	probeSet, err := plan.ResolveArtifacts(s.deps.Catalog, nil, utilityIDs, foundInfraKeys, nil)
	if err != nil {
		return domain.RunSummary{}, err
	}

	// Build a set of infrastructure agent keys to exclude from the first model-resolution
	// batch. Including infra agents in probeSet makes their target paths probeable, but their
	// models are resolved in the separate infraAgents batch below; resolving them in both
	// batches would ask for each infrastructure agent's model twice.
	infraAgentKeySet := make(map[string]bool, len(infraAgents))
	for _, a := range infraAgents {
		infraAgentKeySet[a.Key] = true
	}
	nonInfraProbeAgents := make([]domain.Agent, 0, len(probeSet.Agents))
	for _, a := range probeSet.Agents {
		if !infraAgentKeySet[a.Key] {
			nonInfraProbeAgents = append(nonInfraProbeAgents, a)
		}
	}

	// Model resolution — first batch: utility agents (no workflow-ordered reordering needed).
	orderedAgents := s.orderedAgentsForModelResolution(nil, nonInfraProbeAgents)
	modelRes, err := s.resolveModels(ctx, req.TierModels, req.AgentModels, req.SkipAll, harnessID, module, orderedAgents, nil, false, false)
	if err != nil {
		return domain.RunSummary{}, err
	}

	// Second resolveModels call for infrastructure agents — matches deploy-new's two-batch pattern
	// exactly, threading accumulated tier/custom-model state from the first call.
	var infraModelRes modelResolution
	if len(infraAgents) > 0 {
		var err error
		infraModelRes, err = s.resolveModels(
			ctx,
			modelRes.tierModelsUsed,
			req.AgentModels,
			req.SkipAll,
			harnessID,
			module,
			infraAgents,
			modelRes.accumulatedOptions,
			modelRes.tierSkippedAll,
			modelRes.agentSkippedAll,
		)
		if err != nil {
			return domain.RunSummary{}, err
		}
		for k, v := range infraModelRes.models {
			modelRes.models[k] = v
		}
	}

	// Emit GapNoModel for infra agents whose model was not resolved. The real planner also
	// produces this gap via plan.Gaps, but we emit it here too so that tests using a stub
	// planner (which returns a hardcoded gap-free plan) still observe the gap in the todo
	// collector, matching the contract asserted by T7.3.
	for _, a := range infraAgents {
		if _, resolved := infraModelRes.models[a.Key]; !resolved {
			if _, resolvedInFirst := modelRes.models[a.Key]; !resolvedInFirst {
				s.deps.Todo.AddGap(domain.Gap{
					Kind:    domain.GapNoModel,
					Subject: a.Key,
					Detail:  "no model was selected for this infrastructure agent; update the deployed file manually",
				})
			}
		}
	}

	customTools, skippedTools := s.resolveCustomTools(ctx, req.CustomTools, req.SkipAll, module, probeSet.Agents)

	snap, _ := s.deps.Manifest.Load(workspace)

	agentsDir := module.Descriptor().Paths.Agents.Project
	var deployedAgentIndex DeployedAgentIndex
	if module.Descriptor().Paths.Agents.Supported && agentsDir != "" {
		deployedAgentIndex = buildDeployedAgentIndex(workspace, agentsDir)
	}

	probeAgentByKey := make(map[string]domain.Agent, len(probeSet.Agents))
	for _, a := range probeSet.Agents {
		probeAgentByKey[a.Key] = a
	}

	plannedPaths, pathErr := plan.EnumerateTargetPaths(probeSet, module, scope, s.deps.GOOS)
	if pathErr != nil {
		return domain.RunSummary{}, pathErr
	}
	deployedState, err := probeDeployedStateWithIndex(workspace, plannedPaths, module.Descriptor().Frontmatter.ModelKey, nil, deployedAgentIndex, probeAgentByKey)
	if err != nil {
		return domain.RunSummary{}, err
	}

	toolCfg, _ := s.deps.ToolConfig.Load()
	userCfg, err := s.deps.UserConfig.Load()
	if err != nil {
		return domain.RunSummary{}, err
	}
	toolMappingsVersion := config.HashToolDestinations(toolCfg.ToolDestinations, userCfg.ToolDestinations)

	planInput := plan.Input{
		Catalog: s.deps.Catalog, Module: module, Mode: domain.ModeUtilityInfraOnly,
		WorkspacePath: workspace, Scope: scope, GOOS: s.deps.GOOS,
		Manifest: snap, WorkflowIDs: nil, UtilityAgentIDs: utilityIDs,
		InfrastructureAgentIDs: infraAgentIDs, HookIDs: nil,
		Models:              modelRes.models,
		DeployedState:       deployedState,
		ToolMappingsVersion: toolMappingsVersion,
		ProtocolVersion:     protocol.Version,
		BundleVersion:       bundle.Version,
	}
	p, err := s.deps.Planner.Build(ctx, planInput)
	if err != nil {
		return domain.RunSummary{}, err
	}
	for _, g := range p.Gaps {
		s.deps.Todo.AddGap(g)
	}

	conflicts := map[string]domain.ConflictDecision{}
	var latchedDecision domain.ConflictDecision
	applyToAllLatch := false
	for _, item := range p.Items {
		if item.Action != domain.ActionConflict {
			continue
		}
		var decision domain.ConflictDecision
		if req.ConflictDefault != "" {
			decision = req.ConflictDefault
		} else if applyToAllLatch {
			decision = latchedDecision
		} else {
			var setLatch bool
			decision, setLatch = s.askLocalModification(ctx, item)
			if setLatch {
				applyToAllLatch = true
				latchedDecision = decision
			}
		}
		conflicts[item.TargetPath] = decision
		if decision == domain.DecisionSkip {
			s.deps.Todo.AddGap(domain.Gap{
				Kind: domain.GapSkippedFile, Subject: item.Ref.Key,
				Detail: "file was locally modified and the user chose to skip it",
			})
		}
	}

	ans, rerr := s.deps.Interaction.Review(ctx, p)
	if rerr != nil {
		return domain.RunSummary{}, rerr
	}
	if !req.AutoConfirmPlan && (ans.Status == domain.Cancelled || !ans.Confirm) {
		return domain.RunSummary{}, ErrPlanNotConfirmed
	}

	agentByKey := make(map[string]domain.Agent, len(probeSet.Agents)+len(infraAgents))
	for _, a := range probeSet.Agents {
		agentByKey[a.Key] = a
	}
	for _, a := range infraAgents {
		agentByKey[a.Key] = a
	}

	// No workflow blocks and no infrastructure blocks for the orchestrator: this mode never
	// rewrites the orchestrator. Infrastructure agent content is generated independently via
	// the agent-by-key map.
	contentFn := s.buildContent(module, agentByKey, modelRes.models, customTools, skippedTools, nil, nil, scope, nil, toolMappingsVersion, protocol, bundle, nil)

	now := s.now()
	execReq := deploy.ExecRequest{
		Plan:          p,
		MosaicRoot:    s.deps.MosaicRoot,
		Content:       contentFn,
		Conflicts:     conflicts,
		VersionStamps: buildVersionStamps(append(probeSet.Agents, infraAgents...), probeSet.Skills, nil, p.Items, module.Descriptor(), toolMappingsVersion),
		Hooks:         nil,
		TodoItems:     s.deps.Todo.Items,
		TodoMeta: todo.Meta{
			Harness: harnessRef.DisplayName, WorkspacePath: workspace, DeploymentRoot: workspace,
			GeneratedAt: now, Mode: domain.ModeUtilityInfraOnly,
		},
		DryRun: req.DryRun,
	}

	result, err := s.deps.Executor.Execute(ctx, execReq)
	if err != nil {
		return domain.RunSummary{}, err
	}

	if err := s.persistTierModels(harnessID, modelRes.tierModelsUsed); err != nil {
		s.notifyPersistFailure(ctx, err)
	}

	allAccumulatedOptions := append(modelRes.accumulatedOptions, infraModelRes.accumulatedOptions...)
	customIDs := make([]string, 0, len(allAccumulatedOptions))
	for _, opt := range allAccumulatedOptions {
		customIDs = append(customIDs, opt.ID)
	}
	if err := s.persistCustomModelIDs(harnessID, customIDs); err != nil {
		s.notifyPersistFailure(ctx, err)
	}

	return s.buildSummary(domain.ModeUtilityInfraOnly, harnessRef, workspace, result), nil
}
