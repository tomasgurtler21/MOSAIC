package app

// update.go implements the update use case (I18.4): staleness-driven redeployment against
// an existing workspace, per-file conflict prompting, and optional in-run workflow addition
// (AC18.6).

import (
	"context"
	"path/filepath"

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
	set, err := plan.ResolveArtifacts(s.deps.Catalog, workflowIDs, nil, nil)
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

	planInput := plan.Input{
		Catalog: s.deps.Catalog, Module: module, Mode: domain.ModeUpdate,
		WorkspacePath: workspace, Scope: scope, GOOS: s.deps.GOOS,
		Manifest: snap, WorkflowIDs: workflowIDs,
		DeployedState: deployedState,
		Models:        modelSelections,
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
		decision := req.ConflictDefault
		if decision == "" {
			decision = s.askLocalModification(ctx, item)
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
	contentFn := s.buildContent(module, agentByKey, modelSelections, req.CustomTools, nil, workflowBlocks, scope, deployedReader)

	versionStamps := buildVersionStamps(set.Agents, set.Skills, set.Hooks, p.Items, module.Descriptor())

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

	return s.buildSummary(domain.ModeUpdate, harnessRef, workspace, result), nil
}
