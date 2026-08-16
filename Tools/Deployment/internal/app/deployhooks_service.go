package app

// deployhooks_service.go implements Service.DeployHooks: a narrowed deploy flow that asks
// only harness, workspace, and hook-bundle selection questions, generates no agent content,
// and produces a plan containing exactly the selected hook bundles and their registration steps.

import (
	"context"

	"mosaic-deploy/internal/config"
	"mosaic-deploy/internal/deploy"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/plan"
	"mosaic-deploy/internal/todo"
)

// deployHooks is the real implementation of Service.DeployHooks.
// It follows the narrowed-flow template but omits all agent, model, workflow, and custom-tool
// resolvers, so only QHarness, QWorkspace, and QHooks are ever presented.
func deployHooks(ctx context.Context, s *service, req DeployHooksRequest) (domain.RunSummary, error) {
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

	scope := req.Scope
	if scope == "" {
		scope = domain.ScopeProject
	}

	// Hook selection — QWorkflows, QUtilityAgents, QInfrastructureAgents, QStandaloneAgents,
	// QDeployAgents, QTierModel, and QAgentModel are deliberately absent from this flow.
	hookIDs := req.HookIDs
	if hookIDs == nil {
		if req.SkipAll[domain.QHooks] {
			hookIDs = []string{}
		} else if len(s.deps.Catalog.Hooks()) > 0 {
			ids, askErr := s.askHooks(ctx)
			if askErr != nil {
				return domain.RunSummary{}, askErr
			}
			hookIDs = ids
		} else {
			hookIDs = []string{}
		}
	}

	// Resolve the artifact set for probing deployed state. Only HookIDs are populated;
	// no agent, skill, or orchestrator files enter the set for this mode.
	probeSet, err := plan.ResolveArtifactsFrom(s.deps.Catalog, plan.Selection{
		HookIDs:             hookIDs,
		ExcludeOrchestrator: plan.OrchestratorExcludedFor(domain.ModeDeployHooks),
	})
	if err != nil {
		return domain.RunSummary{}, err
	}

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

	// Load the protocol and bundle versions for stamp consistency even though no agent
	// content is generated; the planner and executor still record them.
	protocol, err := s.loadProtocol()
	if err != nil {
		return domain.RunSummary{}, err
	}
	bundle, err := s.loadBundle()
	if err != nil {
		return domain.RunSummary{}, err
	}

	planInput := plan.Input{
		Catalog: s.deps.Catalog, Module: module, Mode: domain.ModeDeployHooks,
		WorkspacePath: workspace, Scope: scope, GOOS: s.deps.GOOS,
		Manifest:               snap,
		WorkflowIDs:            nil,
		SubagentIDs:            nil,
		UtilityAgentIDs:        nil,
		InfrastructureAgentIDs: nil,
		StandaloneAgentIDs:     nil,
		HookIDs:                hookIDs,
		Models:                 nil,
		DeployedState:          deployedState,
		ToolMappingsVersion:    toolMappingsVersion,
		ProtocolVersion:        protocol.Version,
		BundleVersion:          bundle.Version,
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

	// No workflow blocks and no infrastructure blocks: this mode never rewrites the
	// orchestrator's workflow or infrastructure regions. buildContent receives nil maps.
	contentFn := s.buildContent(module, nil, nil, nil, nil, nil, nil, scope, nil, toolMappingsVersion, protocol, bundle, nil)

	now := s.now()
	execReq := deploy.ExecRequest{
		Plan:          p,
		MosaicRoot:    s.deps.MosaicRoot,
		Content:       contentFn,
		Conflicts:     conflicts,
		VersionStamps: buildVersionStamps(probeSet.Agents, probeSet.Skills, probeSet.Hooks, p.Items, module.Descriptor(), toolMappingsVersion),
		Hooks:         buildHookPlans(module, probeSet.Hooks, scope),
		TodoItems:     s.deps.Todo.Items,
		TodoMeta: todo.Meta{
			Harness: harnessRef.DisplayName, WorkspacePath: workspace, DeploymentRoot: workspace,
			GeneratedAt: now, Mode: domain.ModeDeployHooks,
		},
		DryRun: req.DryRun,
	}

	result, err := s.deps.Executor.Execute(ctx, execReq)
	if err != nil {
		return domain.RunSummary{}, err
	}

	return s.buildSummary(domain.ModeDeployHooks, harnessRef, workspace, result), nil
}
