package app

// update.go implements the update use case (I18.4): staleness-driven redeployment against
// an existing workspace, per-file conflict prompting, and optional in-run workflow addition
// (AC18.6).

import (
	"context"
	"errors"

	"mosaic-deploy/internal/deploy"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/plan"
	"mosaic-deploy/internal/todo"
)

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

	discoveryInput := plan.Input{
		Catalog: s.deps.Catalog, Module: module, Mode: domain.ModeUpdate,
		WorkspacePath: workspace, Scope: scope, GOOS: s.deps.GOOS, Manifest: snap,
	}
	existing := s.discoverExistingWorkflows(ctx, module, workspace, scope, discoveryInput)
	workflowIDs := unionPreserveOrder(existing, req.AddWorkflowIDs)

	planInput := plan.Input{
		Catalog: s.deps.Catalog, Module: module, Mode: domain.ModeUpdate,
		WorkspacePath: workspace, Scope: scope, GOOS: s.deps.GOOS,
		Manifest: snap, WorkflowIDs: workflowIDs,
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
		return domain.RunSummary{}, errors.New("update plan was not confirmed")
	}

	set, setErr := plan.ResolveArtifacts(s.deps.Catalog, workflowIDs, nil, nil)
	var agentByKey map[string]domain.Agent
	var hookPlans []domain.HookPlan
	if setErr == nil {
		agentByKey = make(map[string]domain.Agent, len(set.Agents))
		for _, a := range set.Agents {
			agentByKey[a.Key] = a
		}
		hookPlans = buildHookPlans(module, set.Hooks, scope)
	}
	workflowBlocks := s.buildWorkflowBlocks(workflowIDs)
	deployedReader := func(item domain.PlanItem) []byte {
		return readDeployedFile(workspace, item.TargetPath)
	}
	contentFn := s.buildContent(module, agentByKey, nil, req.CustomTools, nil, workflowBlocks, scope, deployedReader)

	var versionStamps map[string]domain.VersionStamp
	if setErr == nil {
		versionStamps = buildVersionStamps(set.Agents, set.Skills, set.Hooks, p.Items, module.Descriptor())
	} else {
		versionStamps = map[string]domain.VersionStamp{}
	}

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
