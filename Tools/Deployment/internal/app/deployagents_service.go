package app

// deployagents_service.go implements Service.DeployAgents: a merged deploy flow that asks
// a single catalog-scanned QDeployAgents question covering ordinary subagents,
// Infrastructure-category subagents, utility agents, and standalone agents, then deploys
// exactly the selected agents plus the skills they require.
//
// Model resolution runs in two batches. The first batch covers non-infrastructure agents
// (ordinary subagents, utility agents, standalone agents) with agentSkipOverride=false, so
// each agent whose tier model is unresolved receives a per-agent QAgentModel prompt. The
// second batch covers infrastructure agents using the same per-agent pattern, threaded from
// the first batch's state so no tier is re-prompted and no agent appears in both batches.
//
// buildContent is always called with nil workflow and infrastructure block maps, so no
// deployed orchestrator's managed regions are ever touched by this mode.

import (
	"context"

	"mosaic-deploy/internal/catalog"
	"mosaic-deploy/internal/config"
	"mosaic-deploy/internal/deploy"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/plan"
	"mosaic-deploy/internal/todo"
)

// classifiedAgents is the merged selection split into the buckets plan.Selection and
// plan.Input expect. Within each bucket, keys appear in the order the answer supplied
// them, deduplicated.
type classifiedAgents struct {
	SubagentIDs            []string // Role == RoleSubagent, Infrastructure == ""
	InfrastructureAgentIDs []string // Role == RoleSubagent, Infrastructure != ""
	UtilityAgentIDs        []string // Role == RoleUtility
	StandaloneAgentIDs     []string // Role == RoleStandalone
}

// classifyAgentSelection resolves each key through the catalog and routes it by
// domain.AgentRole, using the non-empty Agent.Infrastructure class string to separate
// Infrastructure-category subagents from ordinary ones.
//
// A key the catalog does not know is skipped silently, matching the existing behavior of
// the utility/infra flow. A key resolving to RoleOrchestrator is also skipped: it can
// only arrive from a hand-crafted non-interactive request, never from the option list.
func classifyAgentSelection(c catalog.Catalog, keys []string) classifiedAgents {
	seen := make(map[string]bool, len(keys))
	var result classifiedAgents
	for _, key := range keys {
		if seen[key] {
			continue
		}
		a, ok := c.Agent(key)
		if !ok {
			continue // unknown key, skip silently
		}
		if a.Role == domain.RoleOrchestrator {
			continue // orchestrator never enters the selection
		}
		seen[key] = true
		switch a.Role {
		case domain.RoleSubagent:
			if a.Infrastructure != "" {
				result.InfrastructureAgentIDs = append(result.InfrastructureAgentIDs, key)
			} else {
				result.SubagentIDs = append(result.SubagentIDs, key)
			}
		case domain.RoleUtility:
			result.UtilityAgentIDs = append(result.UtilityAgentIDs, key)
		case domain.RoleStandalone:
			result.StandaloneAgentIDs = append(result.StandaloneAgentIDs, key)
		}
	}
	return result
}

// deployAgents is the real implementation of Service.DeployAgents.
func deployAgents(ctx context.Context, s *service, req DeployAgentsRequest) (domain.RunSummary, error) {
	// Resolve harness.
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

	// Resolve workspace.
	workspace := req.WorkspacePath
	if workspace == "" {
		ws, werr := s.askWorkspace(ctx)
		if werr != nil {
			return domain.RunSummary{}, werr
		}
		workspace = ws
	}

	scope := req.Scope
	if scope == "" {
		scope = domain.ScopeProject
	}

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

	// Determine selected agent IDs following CD-6.
	// QDeployAgents is asked only when all four per-class slices are nil. Any non-nil slice
	// means the caller has already classified the selection and the merged question is skipped.
	subagentIDs := req.SubagentIDs
	utilityIDs := req.UtilityAgentIDs
	infraIDs := req.InfrastructureAgentIDs
	standaloneIDs := req.StandaloneAgentIDs

	needAsk := subagentIDs == nil && utilityIDs == nil && infraIDs == nil && standaloneIDs == nil
	if needAsk {
		if req.SkipAll[domain.QDeployAgents] {
			subagentIDs = []string{}
			utilityIDs = []string{}
			infraIDs = []string{}
			standaloneIDs = []string{}
		} else {
			hasAgents := len(s.deps.Catalog.Agents()) > 0 ||
				len(s.deps.Catalog.UtilityAgents()) > 0 ||
				len(s.deps.Catalog.StandaloneAgents()) > 0
			if hasAgents {
				keys, selErr := s.askDeployAgents(ctx)
				if selErr != nil {
					return domain.RunSummary{}, selErr
				}
				classified := classifyAgentSelection(s.deps.Catalog, keys)
				subagentIDs = classified.SubagentIDs
				utilityIDs = classified.UtilityAgentIDs
				infraIDs = classified.InfrastructureAgentIDs
				standaloneIDs = classified.StandaloneAgentIDs
			} else {
				subagentIDs = []string{}
				utilityIDs = []string{}
				infraIDs = []string{}
				standaloneIDs = []string{}
			}
		}
	}

	// Resolve infrastructure agent structs from the catalog for model resolution and
	// content generation. Keys not found in the catalog are silently skipped.
	infraAgents := make([]domain.Agent, 0, len(infraIDs))
	for _, id := range infraIDs {
		if a, ok := s.deps.Catalog.Agent(id); ok {
			infraAgents = append(infraAgents, a)
		}
	}

	// Build the probe set including all selected agents so their target paths are
	// enumerated and their on-disk state is probed.
	probeSet, err := plan.ResolveArtifactsFrom(s.deps.Catalog, plan.Selection{
		SubagentIDs:            subagentIDs,
		UtilityAgentIDs:        utilityIDs,
		InfrastructureAgentIDs: infraIDs,
		StandaloneAgentIDs:     standaloneIDs,
		ExcludeOrchestrator:    plan.OrchestratorExcludedFor(domain.ModeDeployAgents),
	})
	if err != nil {
		return domain.RunSummary{}, err
	}

	// Build a set of infrastructure agent keys to exclude from the first model-resolution
	// batch. Including infra agents in probeSet makes their target paths probeable, but their
	// models are resolved in the separate second batch; resolving them in the first batch too
	// would ask for each infrastructure agent's model twice.
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

	// Model resolution — first batch: non-infrastructure agents.
	// agentSkipOverride=false allows QAgentModel for ordinary subagents, utility agents, and
	// standalone agents when their tier model is unresolved, matching the behavior available
	// to infrastructure agents in the second batch.
	orderedFirstBatch := s.orderedAgentsForModelResolution(nil, nonInfraProbeAgents)
	modelRes, err := s.resolveModels(ctx, req.TierModels, req.AgentModels, req.SkipAll, harnessID, module, orderedFirstBatch, nil, false, false)
	if err != nil {
		return domain.RunSummary{}, err
	}

	// Model resolution — second batch: infrastructure agents.
	// agentSkipOverride=false so QAgentModel is available for infra agents regardless of the
	// first batch's agentSkippedAll state (which reflects whatever the first batch's users chose;
	// each batch's QAgentModel availability is independent).
	var infraModelRes modelResolution
	if len(infraAgents) > 0 {
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
			false, // infra agents always have QAgentModel available; not suppressed by first batch
		)
		if err != nil {
			return domain.RunSummary{}, err
		}
		// Merge second-batch models into the combined map.
		for k, v := range infraModelRes.models {
			modelRes.models[k] = v
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
	deployedState, err := probeDeployedStateWithIndex(workspace, plannedPaths, module.Descriptor().Frontmatter.ModelKey, nil, deployedAgentIndex, probeAgentByKey, nil)
	if err != nil {
		return domain.RunSummary{}, err
	}

	toolCfg, _ := s.deps.ToolConfig.Load()
	userCfg, err := s.deps.UserConfig.Load()
	if err != nil {
		return domain.RunSummary{}, err
	}
	toolMappingsVersion := config.HashToolDestinations(toolCfg.ToolDestinations, userCfg.ToolDestinations)

	// Build plan.Input. WorkflowIDs and HookIDs are nil — deploy-agents never touches
	// orchestrator workflow regions or hook registrations.
	planInput := plan.Input{
		Catalog:                s.deps.Catalog,
		Module:                 module,
		Mode:                   domain.ModeDeployAgents,
		WorkspacePath:          workspace,
		Scope:                  scope,
		GOOS:                   s.deps.GOOS,
		Manifest:               snap,
		WorkflowIDs:            nil,
		SubagentIDs:            subagentIDs,
		UtilityAgentIDs:        utilityIDs,
		InfrastructureAgentIDs: infraIDs,
		StandaloneAgentIDs:     standaloneIDs,
		HookIDs:                nil,
		Models:                 modelRes.models,
		DeployedState:          deployedState,
		ToolMappingsVersion:    toolMappingsVersion,
		ProtocolVersion:        protocol.Version,
		BundleVersion:          bundle.Version,
	}
	// Clear gaps from any prior abandoned attempt within the same session.
	// ErrPlanNotConfirmed restarts the flow with the same collector; without
	// this reset, gaps from the declined plan would persist into the final report.
	s.deps.Todo.Reset()
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

	// Build agent-by-key map for content generation, including infra agents.
	agentByKey := make(map[string]domain.Agent, len(probeSet.Agents)+len(infraAgents))
	for _, a := range probeSet.Agents {
		agentByKey[a.Key] = a
	}
	for _, a := range infraAgents {
		agentByKey[a.Key] = a
	}

	// No workflow blocks and no infrastructure blocks: this mode never rewrites the
	// orchestrator's workflow or infrastructure managed regions.
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
			GeneratedAt: now, Mode: domain.ModeDeployAgents,
		},
		DryRun: req.DryRun,
	}

	result, err := s.deps.Executor.Execute(ctx, execReq)
	if err != nil {
		return domain.RunSummary{}, err
	}

	if err := s.persistTierModels(harnessID, modelRes.interactivelyResolvedTiers); err != nil {
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

	return s.buildSummary(domain.ModeDeployAgents, harnessRef, workspace, result), nil
}
