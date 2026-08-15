package app

// deploy.go implements the deploy-new use case (I18.3): encoding the flow documented in
// Stage-18/Plan.md as a sequence of resolver calls, each of which either uses a pre-answered
// request field or asks exactly one question through the Interaction port (CD-6).

import (
	"context"

	"mosaic-deploy/internal/config"
	"mosaic-deploy/internal/deploy"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/plan"
	"mosaic-deploy/internal/todo"
)

// DeployNew runs the full deploy-new flow. See Service.DeployNew for the contract.
func (s *service) DeployNew(ctx context.Context, req DeployRequest) (domain.RunSummary, error) {
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
	defer module.Close() //nolint:errcheck // best-effort resource release; a close failure must not fail a completed run

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

	// Load the protocol source once for the run. Failure aborts before any file is written.
	protocol, err := s.loadProtocol()
	if err != nil {
		return domain.RunSummary{}, err
	}

	// Load the deployed-sections bundle once for the run. Failure aborts before any file is written.
	bundle, err := s.loadBundle()
	if err != nil {
		return domain.RunSummary{}, err
	}

	workflowIDs := req.WorkflowIDs
	if workflowIDs == nil {
		if req.SkipAll[domain.QWorkflows] {
			workflowIDs = []string{}
		} else {
			ids, err := s.askWorkflows(ctx)
			if err != nil {
				return domain.RunSummary{}, err
			}
			workflowIDs = ids
		}
	}

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

	hookIDs := req.HookIDs
	if hookIDs == nil {
		if req.SkipAll[domain.QHooks] {
			hookIDs = []string{}
		} else if len(s.deps.Catalog.Hooks()) > 0 {
			ids, err := s.askHooks(ctx)
			if err != nil {
				return domain.RunSummary{}, err
			}
			hookIDs = ids
		} else {
			hookIDs = []string{}
		}
	}

	// Resolve infrastructure agent structs from the catalog for model resolution and content
	// generation. Resolution happens before probe set construction so that only catalog-known
	// agent keys are added to the probe set. Any ID that does not exist in the catalog is
	// silently skipped, matching the existing behavior for content generation.
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
	// enumerated and their on-disk state is probed; without them the planner's DeployedState
	// map would always miss their entries, causing every infra agent to be classified as a new
	// create on every run after the first. Infrastructure agent model resolution is still
	// handled in a dedicated second batch below; they are filtered out of the first batch to
	// avoid duplicate prompts for the same agent.
	probeSet, err := plan.ResolveArtifacts(s.deps.Catalog, workflowIDs, utilityIDs, foundInfraKeys, hookIDs)
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

	orderedAgents := s.orderedAgentsForModelResolution(workflowIDs, nonInfraProbeAgents)
	modelRes, err := s.resolveModels(ctx, req.TierModels, req.AgentModels, req.SkipAll, harnessID, module, orderedAgents, nil, false, false)
	if err != nil {
		return domain.RunSummary{}, err
	}

	// Second resolveModels call for infrastructure agents, threading accumulated state from the
	// regular-agent call. This keeps infra-agent questions in a separate batch that follows all
	// regular-agent questions (non-interleaved), and propagates custom model IDs, tier resolutions,
	// and skip-all state from the first call so they are not re-asked for the infra batch.
	var infraModelRes modelResolution
	if len(infraAgents) > 0 {
		var err error
		infraModelRes, err = s.resolveModels(
			ctx,
			modelRes.tierModelsUsed, // already-resolved tiers are not re-asked
			req.AgentModels,
			req.SkipAll,
			harnessID,
			module,
			infraAgents,
			modelRes.accumulatedOptions, // custom model IDs entered in the regular batch
			modelRes.tierSkippedAll,     // carry tier skip-all state from the regular batch
			modelRes.agentSkippedAll,    // carry agent skip-all state from the regular batch
		)
		if err != nil {
			return domain.RunSummary{}, err
		}
		// Merge infra-agent model selections into the combined models map.
		for k, v := range infraModelRes.models {
			modelRes.models[k] = v
		}
	}

	customTools, skippedTools := s.resolveCustomTools(ctx, req.CustomTools, req.SkipAll, module, probeSet.Agents)

	snap, _ := s.deps.Manifest.Load(workspace)

	// Build the deployed-agent id index once for this run. The index is non-nil only when
	// the harness declares a supported agents directory; otherwise id-based resolution is
	// skipped and all agents are probed at their harness-computed planned paths.
	agentsDir := module.Descriptor().Paths.Agents.Project
	var deployedAgentIndex DeployedAgentIndex
	if module.Descriptor().Paths.Agents.Supported && agentsDir != "" {
		deployedAgentIndex = buildDeployedAgentIndex(workspace, agentsDir)
	}

	// Build the agent-by-key map for id-based probe resolution (covers workflow + utility agents).
	probeAgentByKey := make(map[string]domain.Agent, len(probeSet.Agents))
	for _, a := range probeSet.Agents {
		probeAgentByKey[a.Key] = a
	}

	// Enumerate every planned target path and probe the workspace for each one.
	// plan.Input.DeployedState is the single carrier of presence, content hash, and version
	// stamps for all planned target paths.
	plannedPaths, pathErr := plan.EnumerateTargetPaths(probeSet, module, scope, s.deps.GOOS)
	if pathErr != nil {
		return domain.RunSummary{}, pathErr
	}
	deployedState, err := probeDeployedStateWithIndex(workspace, plannedPaths, module.Descriptor().Frontmatter.ModelKey, nil, deployedAgentIndex, probeAgentByKey)
	if err != nil {
		return domain.RunSummary{}, err
	}

	// Compute the tool-mappings version hash from the loaded config stores so the planner
	// can detect staleness when the user modifies their tool-destination configuration.
	toolCfg, _ := s.deps.ToolConfig.Load()
	userCfg, err := s.deps.UserConfig.Load()
	if err != nil {
		return domain.RunSummary{}, err
	}
	toolMappingsVersion := config.HashToolDestinations(toolCfg.ToolDestinations, userCfg.ToolDestinations)

	planInput := plan.Input{
		Catalog: s.deps.Catalog, Module: module, Mode: domain.ModeDeployNew,
		WorkspacePath: workspace, Scope: scope, GOOS: s.deps.GOOS,
		Manifest: snap, WorkflowIDs: workflowIDs, UtilityAgentIDs: utilityIDs,
		InfrastructureAgentIDs: infraAgentIDs, HookIDs: hookIDs,
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

	// Review is always shown (it is the final step of both documented flows); AutoConfirmPlan
	// only controls whether a decline/cancel answer aborts the run, so headless callers that
	// pre-confirm still see it recorded as the last interaction before execution.
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
	workflowBlocks := s.buildWorkflowBlocks(workflowIDs)
	infraBlocks := s.buildInfrastructureBlocks(infraAgentIDs)
	contentFn := s.buildContent(module, agentByKey, modelRes.models, customTools, skippedTools, workflowBlocks, infraBlocks, scope, nil, toolMappingsVersion, protocol, bundle, nil)

	now := s.now()
	execReq := deploy.ExecRequest{
		Plan:          p,
		MosaicRoot:    s.deps.MosaicRoot,
		Content:       contentFn,
		Conflicts:     conflicts,
		VersionStamps: buildVersionStamps(append(probeSet.Agents, infraAgents...), probeSet.Skills, probeSet.Hooks, p.Items, module.Descriptor(), toolMappingsVersion),
		Hooks:         buildHookPlans(module, probeSet.Hooks, scope),
		TodoItems:     s.deps.Todo.Items,
		TodoMeta: todo.Meta{
			Harness: harnessRef.DisplayName, WorkspacePath: workspace, DeploymentRoot: workspace,
			GeneratedAt: now, Mode: domain.ModeDeployNew,
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

	// Merge accumulated custom model options from both resolveModels calls before persisting.
	allAccumulatedOptions := append(modelRes.accumulatedOptions, infraModelRes.accumulatedOptions...)
	customIDs := make([]string, 0, len(allAccumulatedOptions))
	for _, opt := range allAccumulatedOptions {
		customIDs = append(customIDs, opt.ID)
	}
	if err := s.persistCustomModelIDs(harnessID, customIDs); err != nil {
		s.notifyPersistFailure(ctx, err)
	}

	return s.buildSummary(domain.ModeDeployNew, harnessRef, workspace, result), nil
}

// orderedAgentsForModelResolution reorders agents into a processing sequence suitable for
// model-resolution questions: orchestrator first, then workflow-referenced agents in the
// order each workflow lists them, then any remaining agents in their original sorted order.
//
// Processing agents in workflow-definition order means a custom model ID entered for one
// agent is immediately visible as an option for the next agent in the same workflow, because
// resolveModels appends custom IDs to harnessOptions in-place as each answer arrives.
func (s *service) orderedAgentsForModelResolution(workflowIDs []string, sortedAgents []domain.Agent) []domain.Agent {
	agentByKey := make(map[string]domain.Agent, len(sortedAgents))
	for _, a := range sortedAgents {
		agentByKey[a.Key] = a
	}

	seen := make(map[string]bool, len(sortedAgents))
	ordered := make([]domain.Agent, 0, len(sortedAgents))

	// Orchestrator always first.
	orc := s.deps.Catalog.Orchestrator()
	if a, ok := agentByKey[orc.Key]; ok {
		seen[orc.Key] = true
		ordered = append(ordered, a)
	}

	// Workflow-referenced agents in the order each workflow lists them.
	for _, wfID := range workflowIDs {
		wf, ok := s.deps.Catalog.Workflow(wfID)
		if !ok {
			continue
		}
		for _, agentKey := range wf.ReferencedAgents {
			if seen[agentKey] {
				continue
			}
			if a, ok := agentByKey[agentKey]; ok {
				seen[agentKey] = true
				ordered = append(ordered, a)
			}
		}
	}

	// Any remaining agents (utility, infrastructure, etc.) in their original sorted order.
	for _, a := range sortedAgents {
		if !seen[a.Key] {
			seen[a.Key] = true
			ordered = append(ordered, a)
		}
	}

	return ordered
}
