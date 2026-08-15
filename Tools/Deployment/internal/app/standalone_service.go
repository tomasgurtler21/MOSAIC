package app

// standalone_service.go implements Service.DeployStandalone: a narrowed deploy flow that
// asks only the standalone agent selection question, never workflow, hook, utility, or
// infrastructure questions, and produces a plan containing exactly the selected standalone
// agents and the skills they require.

import (
	"context"

	"mosaic-deploy/internal/config"
	"mosaic-deploy/internal/deploy"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/plan"
	"mosaic-deploy/internal/todo"
)

// deployStandalone is the real implementation of Service.DeployStandalone.
// It reuses the deploy-new resolver sequence but omits all workflow, hook, utility, and
// infrastructure resolvers entirely, so only QStandaloneAgents is ever presented.
func deployStandalone(ctx context.Context, s *service, req StandaloneRequest) (domain.RunSummary, error) {
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

	// Standalone agent selection — QWorkflows, QHooks, QUtilityAgents, and
	// QInfrastructureAgents are deliberately absent from this flow.
	ids := req.StandaloneAgentIDs
	if ids == nil {
		if req.SkipAll[domain.QStandaloneAgents] {
			ids = []string{}
		} else if len(s.deps.Catalog.StandaloneAgents()) > 0 {
			selIDs, selErr := s.askStandaloneAgents(ctx)
			if selErr != nil {
				return domain.RunSummary{}, selErr
			}
			ids = selIDs
		} else {
			ids = []string{}
		}
	}

	// Resolve the artifact set for probing deployed state and model resolution.
	// Standalone agents enter artifact resolution as their own class (AC5.5), not via
	// the utility or infrastructure parameters. The probe set must include standalone agents
	// so their target paths are enumerated and their on-disk state is probed, preventing every
	// standalone agent from being classified as a new create on every run after the first.
	probeSet, err := plan.ResolveArtifactsFrom(s.deps.Catalog, plan.Selection{
		StandaloneAgentIDs:  ids,
		ExcludeOrchestrator: plan.OrchestratorExcludedFor(domain.ModeStandaloneOnly),
	})
	if err != nil {
		return domain.RunSummary{}, err
	}

	// Model resolution — single batch for standalone agents (no second batch needed since
	// there are no infrastructure agents with a separate resolution path).
	orderedAgents := s.orderedAgentsForModelResolution(nil, probeSet.Agents)
	modelRes, err := s.resolveModels(ctx, req.TierModels, req.AgentModels, req.SkipAll, harnessID, module, orderedAgents, nil, false, false)
	if err != nil {
		return domain.RunSummary{}, err
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
		Catalog: s.deps.Catalog, Module: module, Mode: domain.ModeStandaloneOnly,
		WorkspacePath: workspace, Scope: scope, GOOS: s.deps.GOOS,
		Manifest: snap, WorkflowIDs: nil, UtilityAgentIDs: nil,
		InfrastructureAgentIDs: nil, StandaloneAgentIDs: ids, HookIDs: nil,
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

	agentByKey := make(map[string]domain.Agent, len(probeSet.Agents))
	for _, a := range probeSet.Agents {
		agentByKey[a.Key] = a
	}

	// No workflow blocks and no infrastructure blocks: this mode never rewrites the
	// orchestrator's workflow or infrastructure regions. Standalone agent content is
	// generated independently via the agent-by-key map.
	contentFn := s.buildContent(module, agentByKey, modelRes.models, customTools, skippedTools, nil, nil, scope, nil, toolMappingsVersion, protocol, bundle, nil)

	now := s.now()
	execReq := deploy.ExecRequest{
		Plan:          p,
		MosaicRoot:    s.deps.MosaicRoot,
		Content:       contentFn,
		Conflicts:     conflicts,
		VersionStamps: buildVersionStamps(probeSet.Agents, probeSet.Skills, nil, p.Items, module.Descriptor(), toolMappingsVersion),
		Hooks:         nil,
		TodoItems:     s.deps.Todo.Items,
		TodoMeta: todo.Meta{
			Harness: harnessRef.DisplayName, WorkspacePath: workspace, DeploymentRoot: workspace,
			GeneratedAt: now, Mode: domain.ModeStandaloneOnly,
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

	customIDs := make([]string, 0, len(modelRes.accumulatedOptions))
	for _, opt := range modelRes.accumulatedOptions {
		customIDs = append(customIDs, opt.ID)
	}
	if err := s.persistCustomModelIDs(harnessID, customIDs); err != nil {
		s.notifyPersistFailure(ctx, err)
	}

	return s.buildSummary(domain.ModeStandaloneOnly, harnessRef, workspace, result), nil
}
