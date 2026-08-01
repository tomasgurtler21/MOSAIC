package app

// deploy.go implements the deploy-new use case (I18.3): encoding the flow documented in
// Stage-18/Plan.md as a sequence of resolver calls, each of which either uses a pre-answered
// request field or asks exactly one question through the Interaction port (CD-6).

import (
	"context"

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

	workflowIDs := req.WorkflowIDs
	if workflowIDs == nil {
		if req.SkipAll[domain.QWorkflows] {
			workflowIDs = []string{}
		} else {
			workflowIDs = s.askWorkflows(ctx)
		}
	}

	utilityIDs := req.UtilityAgentIDs
	if utilityIDs == nil {
		if req.SkipAll[domain.QUtilityAgents] {
			utilityIDs = []string{}
		} else if len(s.deps.Catalog.UtilityAgents()) > 0 {
			utilityIDs = s.askUtilityAgents(ctx)
		} else {
			utilityIDs = []string{}
		}
	}

	infraAgentIDs := req.InfrastructureAgentIDs
	if infraAgentIDs == nil {
		if req.SkipAll[domain.QInfrastructureAgents] {
			infraAgentIDs = []string{}
		} else if len(s.deps.Catalog.InfrastructureAgents()) > 0 {
			infraAgentIDs = s.askInfrastructureAgents(ctx)
		} else {
			infraAgentIDs = []string{}
		}
	}

	hookIDs := req.HookIDs
	if hookIDs == nil {
		if req.SkipAll[domain.QHooks] {
			hookIDs = []string{}
		} else if len(s.deps.Catalog.Hooks()) > 0 {
			hookIDs = s.askHooks(ctx)
		} else {
			hookIDs = []string{}
		}
	}

	// Resolve the artifact set for probing deployed state and model/tool resolution.
	// Infrastructure agents are excluded here and passed directly to plan.Input because
	// they do not participate in model resolution or the pre-probe at deploy time.
	probeSet, err := plan.ResolveArtifacts(s.deps.Catalog, workflowIDs, utilityIDs, nil, hookIDs)
	if err != nil {
		return domain.RunSummary{}, err
	}

	modelRes := s.resolveModels(ctx, req.TierModels, req.AgentModels, req.SkipAll, harnessID, module, probeSet.Agents)
	customTools, skippedTools := s.resolveCustomTools(ctx, req.CustomTools, req.SkipAll, module, probeSet.Agents)

	snap, _ := s.deps.Manifest.Load(workspace)

	// Enumerate every planned target path and probe the workspace for each one.
	// plan.Input.DeployedState is the single carrier of presence, content hash, and version
	// stamps for all planned target paths.
	plannedPaths, pathErr := plan.EnumerateTargetPaths(probeSet, module, scope, s.deps.GOOS)
	if pathErr != nil {
		return domain.RunSummary{}, pathErr
	}
	deployedState := probeDeployedState(workspace, plannedPaths, module.Descriptor().Frontmatter.ModelKey, nil)

	planInput := plan.Input{
		Catalog: s.deps.Catalog, Module: module, Mode: domain.ModeDeployNew,
		WorkspacePath: workspace, Scope: scope, GOOS: s.deps.GOOS,
		Manifest: snap, WorkflowIDs: workflowIDs, UtilityAgentIDs: utilityIDs,
		InfrastructureAgentIDs: infraAgentIDs, HookIDs: hookIDs,
		Models:        modelRes.models,
		DeployedState: deployedState,
	}
	p, err := s.deps.Planner.Build(ctx, planInput)
	if err != nil {
		return domain.RunSummary{}, err
	}
	for _, g := range p.Gaps {
		s.deps.Todo.AddGap(g)
	}

	conflicts := map[string]domain.ConflictDecision{}
	for _, item := range p.Items {
		if item.Action != domain.ActionConflict {
			continue
		}
		decision := s.askLocalModification(ctx, item)
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

	agentByKey := make(map[string]domain.Agent, len(probeSet.Agents))
	for _, a := range probeSet.Agents {
		agentByKey[a.Key] = a
	}
	workflowBlocks := s.buildWorkflowBlocks(workflowIDs)
	infraBlocks := s.buildInfrastructureBlocks(infraAgentIDs)
	contentFn := s.buildContent(module, agentByKey, modelRes.models, customTools, skippedTools, workflowBlocks, infraBlocks, scope, nil)

	now := s.now()
	execReq := deploy.ExecRequest{
		Plan:          p,
		MosaicRoot:    s.deps.MosaicRoot,
		Content:       contentFn,
		Conflicts:     conflicts,
		VersionStamps: buildVersionStamps(probeSet.Agents, probeSet.Skills, probeSet.Hooks, p.Items, module.Descriptor()),
		Hooks:         buildHookPlans(module, probeSet.Hooks, scope),
		Todo:          s.deps.Todo.Items(),
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

	_ = s.persistTierModels(harnessID, modelRes.tierModelsUsed)

	return s.buildSummary(domain.ModeDeployNew, harnessRef, workspace, result), nil
}
