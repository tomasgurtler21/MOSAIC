package app

// update.go implements the update use case (I18.4): staleness-driven redeployment against
// an existing workspace, per-file conflict prompting, and optional in-run workflow addition
// (AC18.6).

import (
	"context"
	"path/filepath"

	"mosaic-deploy/internal/config"
	"mosaic-deploy/internal/deploy"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/plan"
	"mosaic-deploy/internal/todo"
)

// deployedModelSelections builds the agent-key-keyed model selections implied by the models
// already embedded in deployed files. Only agents whose deployed artifact carries a non-empty
// ModelID appear in the result; every other agent is absent, so downstream gap evaluation
// still flags agents that genuinely have no model.
//
// This is a pure function: no I/O, no mutation of its arguments.
func deployedModelSelections(
	agents []domain.Agent,
	paths plan.PlannedPaths,
	deployedState map[string]domain.DeployedArtifactState,
) map[string]domain.ModelSelection {
	if len(agents) == 0 {
		return nil
	}
	var result map[string]domain.ModelSelection
	for _, agent := range agents {
		targetPath, ok := paths.Path(domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: agent.Key})
		if !ok {
			continue
		}
		state, exists := deployedState[targetPath]
		if !exists || state.ModelID == "" {
			continue
		}
		if result == nil {
			result = make(map[string]domain.ModelSelection)
		}
		result[agent.Key] = domain.ModelSelection{
			ModelID: state.ModelID,
			Origin:  domain.OriginDeployed,
		}
	}
	return result
}

// Update runs the update flow. See Service.Update for the contract.
func (s *service) Update(ctx context.Context, req UpdateRequest) (domain.RunSummary, error) {
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

	snap, _ := s.deps.Manifest.Load(workspace)

	// Probe the deployed orchestrator early to discover which workflow IDs are already
	// embedded in it. This replaces the prior approach of reading the orchestrator file
	// separately: we probe it once here and seed the full-probe map with the result so
	// the file is read at most once per run.
	var orchState domain.DeployedArtifactState
	var orchTargetPath string
	orchestrator := s.deps.Catalog.Orchestrator()
	if orchPath, pathErr := module.TargetPath(domain.TargetPathRequest{
		Kind:     domain.ArtifactAgent,
		Key:      orchestrator.Key,
		FileName: filepath.Base(orchestrator.SourcePath),
		Scope:    scope,
		GOOS:     s.deps.GOOS,
	}); pathErr == nil {
		orchTargetPath = orchPath
		orchState = probeDeployedArtifact(workspace, orchTargetPath, module.Descriptor().Frontmatter.ModelKey)
	}
	existing := discoverExistingWorkflows(orchState)
	workflowIDs := unionPreserveOrder(existing, req.AddWorkflowIDs)

	// Resolve the artifact set from the discovered workflow IDs so we can enumerate target
	// paths for the full probe. Plan.Build will also resolve internally; this call is the
	// app layer's own probe-preparation step, not a duplicate plan build.
	set, err := plan.ResolveArtifacts(s.deps.Catalog, workflowIDs, nil, nil, nil)
	if err != nil {
		return domain.RunSummary{}, err
	}

	plannedPaths, err := plan.EnumerateTargetPaths(set, module, scope, s.deps.GOOS)
	if err != nil {
		return domain.RunSummary{}, err
	}

	// Seed the full probe with the orchestrator state already probed above, so the
	// orchestrator file is read exactly once per run.
	var seed map[string]domain.DeployedArtifactState
	if orchTargetPath != "" {
		seed = map[string]domain.DeployedArtifactState{orchTargetPath: orchState}
	}
	deployedState := probeDeployedState(workspace, plannedPaths, module.Descriptor().Frontmatter.ModelKey, seed)

	modelSelections := deployedModelSelections(set.Agents, plannedPaths, deployedState)

	// Resolve models for agents that are newly being added in this run (those without a
	// deployed file). The orchestrator is excluded: its model comes from the deployed file
	// or is handled via gap reporting. Tier questions are always suppressed in the Update
	// flow (tierSkipOverride=true) to preserve the invariant that Update never asks
	// tier-level model questions.
	var newAgentModelRes modelResolution
	var newAgents []domain.Agent
	for _, agent := range set.Agents {
		if agent.Role == domain.RoleOrchestrator {
			continue
		}
		ref := domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: agent.Key}
		if tp, ok := plannedPaths.Path(ref); ok {
			if state, exists := deployedState[tp]; exists && state.Present {
				continue // agent already has a present deployed file; model comes from deployed state
			}
		}
		newAgents = append(newAgents, agent)
	}
	if len(newAgents) > 0 {
		newAgentModelRes = s.resolveModels(
			ctx, req.TierModels, req.AgentModels, req.SkipAll,
			harnessID, module, newAgents,
			nil, true, false,
		)
	}

	// Merge deployed model selections with newly resolved models for new agents.
	var allModels map[string]domain.ModelSelection
	if len(modelSelections) > 0 || len(newAgentModelRes.models) > 0 {
		allModels = make(map[string]domain.ModelSelection)
		for k, v := range modelSelections {
			allModels[k] = v
		}
		for k, v := range newAgentModelRes.models {
			allModels[k] = v
		}
	}

	// Compute the tool-mappings version hash from the loaded config stores so the planner
	// can detect staleness when the user modifies their tool-destination configuration.
	toolCfg, _ := s.deps.ToolConfig.Load()
	userCfg, _ := s.deps.UserConfig.Load()
	toolMappingsVersion := config.HashToolDestinations(toolCfg.ToolDestinations, userCfg.ToolDestinations)

	planInput := plan.Input{
		Catalog: s.deps.Catalog, Module: module, Mode: domain.ModeUpdate,
		WorkspacePath: workspace, Scope: scope, GOOS: s.deps.GOOS,
		Manifest: snap, WorkflowIDs: workflowIDs,
		DeployedState:       deployedState,
		Models:              allModels,
		ToolMappingsVersion: toolMappingsVersion,
		ProtocolVersion:     protocol.Version,
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

	// Review is always shown; AutoConfirmPlan only controls whether a decline/cancel answer
	// aborts the run (see deploy.go for the same rationale).
	ans, rerr := s.deps.Interaction.Review(ctx, p)
	if rerr != nil {
		return domain.RunSummary{}, rerr
	}
	if !req.AutoConfirmPlan && (ans.Status == domain.Cancelled || !ans.Confirm) {
		return domain.RunSummary{}, ErrPlanNotConfirmed
	}

	agentByKey := make(map[string]domain.Agent, len(set.Agents))
	for _, a := range set.Agents {
		agentByKey[a.Key] = a
	}
	hookPlans := buildHookPlans(module, set.Hooks, scope)

	workflowBlocks := s.buildWorkflowBlocks(workflowIDs)
	deployedReader := func(item domain.PlanItem) []byte {
		return readDeployedFile(workspace, item.TargetPath)
	}
	// Infrastructure agent selection is intentionally omitted from the update flow.
	// Update re-deploys whatever was already deployed; it does not re-prompt for
	// infrastructure agent choices. The InfrastructureAgents injection region is
	// preserved from the deployed file via the InjectionProject preservation pass.
	contentFn := s.buildContent(module, agentByKey, allModels, req.CustomTools, nil, workflowBlocks, nil, scope, deployedReader, toolMappingsVersion, protocol)

	versionStamps := buildVersionStamps(set.Agents, set.Skills, set.Hooks, p.Items, module.Descriptor(), toolMappingsVersion)

	now := s.now()
	execReq := deploy.ExecRequest{
		Plan:          p,
		MosaicRoot:    s.deps.MosaicRoot,
		Content:       contentFn,
		Conflicts:     conflicts,
		VersionStamps: versionStamps,
		Hooks:         hookPlans,
		Todo:          s.deps.Todo.Items(),
		TodoMeta: todo.Meta{
			Harness: harnessRef.DisplayName, WorkspacePath: workspace, DeploymentRoot: workspace,
			GeneratedAt: now, Mode: domain.ModeUpdate,
		},
		DryRun: req.DryRun,
	}

	result, err := s.deps.Executor.Execute(ctx, execReq)
	if err != nil {
		return domain.RunSummary{}, err
	}

	// Persist any custom model IDs accumulated during this run (from model questions asked
	// for newly-added agents). Also preserves any prior custom IDs already in UserConfig.
	customIDs := make([]string, 0, len(newAgentModelRes.accumulatedOptions))
	for _, opt := range newAgentModelRes.accumulatedOptions {
		customIDs = append(customIDs, opt.ID)
	}
	_ = s.persistCustomModelIDs(harnessID, customIDs)

	return s.buildSummary(domain.ModeUpdate, harnessRef, workspace, result), nil
}
