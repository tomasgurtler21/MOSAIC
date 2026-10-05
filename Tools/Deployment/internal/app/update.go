package app

// update.go implements the update use case: staleness-driven redeployment against an
// existing workspace, per-file conflict prompting, and optional in-run workflow addition.
//
// Agent-set membership rule: which deployed agents to staleness-check is determined by a
// workspace scan of the harness's deployed-agents directory, not by workflow discovery.
// Every .md file found in that directory is classified in one pass (see scanWorkspaceAgents):
// catalog-matched files enter the artifact set and run the full staleness pipeline;
// unmatched but eligible files enter the harness-only consent path; all others are left
// byte-identical with no plan item and no warning.
//
// Workflow discovery's role is narrower: it feeds plan.Input.WorkflowIDs for orchestrator
// workflow-set drift reporting and the workflow_update "currently deployed" hint. It no
// longer gates which non-orchestrator agents are checked.
//
// Hook membership rule: a Catalog hook bundle is refreshed only when at least one file of the
// harness's hook variant is already on disk (see deployedHookIDs); hooks are never added, and
// a manifest entry without files does not make a hook deployed. Only hooks the update writes
// reach the executor, so up-to-date and skipped hooks are not rewritten or re-registered.
//
// The manifest is a complementary per-item lookup (content hashes, recorded versions) and
// is not consulted for set membership. An agent present in the workspace but absent from
// the manifest is still staleness-checked.

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

	// Load the deployed-sections bundle once for the run. Failure aborts before any file is written.
	bundle, err := s.loadBundle()
	if err != nil {
		return domain.RunSummary{}, err
	}

	// Build the deployed-agent id index once for this run, before any per-agent probing.
	// Non-nil only when the harness declares a supported agents directory.
	agentsDir := module.Descriptor().Paths.Agents.Project
	var deployedAgentIndex DeployedAgentIndex
	if module.Descriptor().Paths.Agents.Supported && agentsDir != "" {
		deployedAgentIndex = buildDeployedAgentIndex(workspace, agentsDir)
	}

	// Scan the deployed-agents directory in one pass to classify every file found there.
	// Matched files enter Update's artifact set; unmatched but eligible files enter the
	// harness-only consent path; neither-classified files are left byte-identical.
	// Guarded on the harness declaring a supported agents directory.
	scan := WorkspaceAgentScan{}
	if module.Descriptor().Paths.Agents.Supported && agentsDir != "" {
		scan = scanWorkspaceAgents(workspace, agentsDir, s.deps.Catalog)
	}
	harnessOnlyAgents := scan.HarnessOnly
	scannedAgentKeys := scan.MatchedKeys()

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

	// When this mode excludes orchestrators from force-inclusion, the orchestrator enters the
	// artifact set only when found present on disk — the same path every other deployed agent
	// uses via scannedAgentKeys. The guard ensures this is a no-op for any future mode that
	// already force-includes the orchestrator (OrchestratorExcludedFor returns false for it).
	if plan.OrchestratorExcludedFor(domain.ModeUpdateWorkspace) && orchState.Present {
		scannedAgentKeys = append(scannedAgentKeys, orchestrator.Key)
	}

	// Probe the orchestrator-script when the catalog has one. The script is subject to the
	// same exclusion rule as the main orchestrator: OrchestratorExcludedFor governs both. When
	// excluded from force-inclusion, the script enters the artifact set via scannedAgentKeys
	// if found present on disk.
	var orchScriptState domain.DeployedArtifactState
	var orchScriptTargetPath string
	var orchScriptAgent domain.Agent
	if plan.OrchestratorExcludedFor(domain.ModeUpdateWorkspace) {
		if script, ok := s.deps.Catalog.OrchestratorScript(); ok {
			if scriptPath, pathErr := module.TargetPath(domain.TargetPathRequest{
				Kind:     domain.ArtifactAgent,
				Key:      script.Key,
				FileName: filepath.Base(script.SourcePath),
				Scope:    scope,
				GOOS:     s.deps.GOOS,
			}); pathErr == nil {
				orchScriptTargetPath = scriptPath
				orchScriptAgent = script
				orchScriptState = probeDeployedArtifact(workspace, orchScriptTargetPath, module.Descriptor().Frontmatter.ModelKey)
				if orchScriptState.Present {
					scannedAgentKeys = append(scannedAgentKeys, script.Key)
				}
			}
		}
	}

	existing := discoverExistingWorkflows(orchState)
	workflowIDs := unionPreserveOrder(existing, req.AddWorkflowIDs)

	// Resolve the artifact set from both the discovered workflow IDs and the workspace-scan
	// matched keys. Both sources union into the same set; this call is the app layer's own
	// probe-preparation step. plan.Build's internal resolution receives the identical values
	// via plan.Input.ScannedAgentKeys to ensure the probe map and the built plan agree.
	hookIDs := s.deployedHookIDs(module, workspace, scope)
	set, err := plan.ResolveArtifactsFrom(s.deps.Catalog, plan.Selection{
		HookIDs:             hookIDs,
		WorkflowIDs:         workflowIDs,
		ScannedAgentKeys:    scannedAgentKeys,
		ExcludeOrchestrator: plan.OrchestratorExcludedFor(domain.ModeUpdateWorkspace),
	})
	if err != nil {
		return domain.RunSummary{}, err
	}

	plannedPaths, err := plan.EnumerateTargetPaths(set, module, scope, s.deps.GOOS)
	if err != nil {
		return domain.RunSummary{}, err
	}

	// Seed the full probe with the orchestrator and orchestrator-script states already probed
	// above, so each file is read at most once per run.
	var seed map[string]domain.DeployedArtifactState
	if orchTargetPath != "" || orchScriptTargetPath != "" {
		seed = make(map[string]domain.DeployedArtifactState)
		if orchTargetPath != "" {
			seed[orchTargetPath] = orchState
		}
		if orchScriptTargetPath != "" {
			seed[orchScriptTargetPath] = orchScriptState
		}
	}

	// Build the agent-by-key map for id-based probe resolution.
	probeAgentByKey := make(map[string]domain.Agent, len(set.Agents))
	for _, a := range set.Agents {
		probeAgentByKey[a.Key] = a
	}

	deployedState, err := probeDeployedStateWithIndex(workspace, plannedPaths, module.Descriptor().Frontmatter.ModelKey, seed, deployedAgentIndex, probeAgentByKey, buildParseFailedPaths(scan.Matched))
	if err != nil {
		return domain.RunSummary{}, err
	}

	applyHookPresence(workspace, deployedState, hookPlansByTargetPath(module, set.Hooks, plannedPaths, scope))

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
		var err error
		newAgentModelRes, err = s.resolveModels(
			ctx, req.TierModels, req.AgentModels, req.SkipAll,
			harnessID, module, newAgents,
			nil, true, false,
		)
		if err != nil {
			return domain.RunSummary{}, err
		}
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
	userCfg, err := s.deps.UserConfig.Load()
	if err != nil {
		return domain.RunSummary{}, err
	}
	toolMappingsVersion := config.HashToolDestinations(toolCfg.ToolDestinations, userCfg.ToolDestinations)

	// Already-declared catalog-backed infrastructure agents are refreshed; none is ever added.
	orchRefresh := s.orchestratorRefresh([]probedOrchestrator{
		{Agent: orchestrator, TargetPath: orchTargetPath, State: orchState},
		{Agent: orchScriptAgent, TargetPath: orchScriptTargetPath, State: orchScriptState},
	})

	planInput := plan.Input{
		Catalog: s.deps.Catalog, Module: module, Mode: domain.ModeUpdateWorkspace,
		WorkspacePath: workspace, Scope: scope, GOOS: s.deps.GOOS,
		Manifest: snap, WorkflowIDs: workflowIDs, HookIDs: hookIDs,
		// ScannedAgentKeys carries the same slice used for the app layer's ResolveArtifactsFrom
		// call above, ensuring the probe map and the built plan resolve the identical artifact set.
		ScannedAgentKeys:    scannedAgentKeys,
		DeployedState:       deployedState,
		Models:              allModels,
		ToolMappingsVersion: toolMappingsVersion,
		ProtocolVersion:     protocol.Version,
		BundleVersion:       bundle.Version,
		InfrastructureDeclarations: refreshIntent(),
	}
	p, err := s.deps.Planner.Build(ctx, planInput)
	if err != nil {
		return domain.RunSummary{}, err
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
			if gap, ok := orchRefresh.skipGap(item); ok {
				s.deps.Todo.AddGap(gap)
			}
		}
	}

	// Only hooks the update writes keep their registration gaps and steps; the executor is
	// handed the same set, so unchanged and skipped hooks are neither rewritten nor re-registered.
	writtenHooks := writtenHookBundles(set.Hooks, p.Items, conflicts)
	dropUnwrittenHookRegistrations(&p, module, set.Hooks, writtenHooks, scope)
	for _, g := range p.Gaps {
		s.deps.Todo.AddGap(g)
	}

	harnessOnlyPlan := s.planHarnessOnlyRefresh(ctx, harnessOnlyAgents, &p)

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
	hookPlans := buildHookPlans(module, writtenHooks, scope)

	workflowBlocks := s.buildWorkflowBlocks(workflowIDs)
	deployedReader := func(item domain.PlanItem) []byte {
		return readDeployedFile(workspace, item.TargetPath)
	}
	// Update never asks about or adds infrastructure declarations; it refreshes stale
	// catalog-backed ones in place (refresh-only merge), leaving all others byte-identical.
	contentFn := s.buildContent(module, agentByKey, allModels, req.CustomTools, nil, workflowBlocks, nil, scope, deployedReader, toolMappingsVersion, protocol, bundle, harnessOnlyPlan, orchRefresh.refreshContentOptions(s)...)

	versionStamps := buildVersionStamps(set.Agents, set.Skills, set.Hooks, p.Items, module.Descriptor(), toolMappingsVersion)

	now := s.now()
	execReq := deploy.ExecRequest{
		Plan:          p,
		MosaicRoot:    s.deps.MosaicRoot,
		Content:       contentFn,
		Conflicts:     conflicts,
		VersionStamps: versionStamps,
		Hooks:         hookPlans,
		TodoItems:     s.deps.Todo.Items,
		TodoMeta: todo.Meta{
			Harness: harnessRef.DisplayName, WorkspacePath: workspace, DeploymentRoot: workspace,
			GeneratedAt: now, Mode: domain.ModeUpdateWorkspace,
		},
		DryRun: req.DryRun,
		// Update opts into all-or-nothing execution so a failed run restores every overwritten
		// file to its pre-run bytes and deletes every file the run created. Fallback runs are
		// excluded from reversal by the executor and keep today's non-atomic semantics.
		Atomic:      true,
		ToolVersion: s.deps.ToolVersion,
	}

	result, err := s.deps.Executor.Execute(ctx, execReq)
	if err != nil {
		return domain.RunSummary{}, err
	}

	// A reverted run must not be summarised: buildSummary enumerates deployed artifacts, and
	// after a reversal none of them exist on disk. Return *RevertedRunError so the caller sees
	// why the run failed and which paths (if any) the reversal could not restore.
	if result.Reverted {
		unrestored := make([]string, 0, len(result.RevertFailures))
		for _, rf := range result.RevertFailures {
			unrestored = append(unrestored, rf.Path)
		}
		return domain.RunSummary{}, &RevertedRunError{
			Cause:           result.Partial,
			UnrestoredPaths: unrestored,
		}
	}

	// Persist any custom model IDs accumulated during this run (from model questions asked
	// for newly-added agents). Also preserves any prior custom IDs already in UserConfig.
	customIDs := make([]string, 0, len(newAgentModelRes.accumulatedOptions))
	for _, opt := range newAgentModelRes.accumulatedOptions {
		customIDs = append(customIDs, opt.ID)
	}
	if err := s.persistCustomModelIDs(harnessID, customIDs); err != nil {
		s.notifyPersistFailure(ctx, err)
	}

	return s.buildSummary(domain.ModeUpdateWorkspace, harnessRef, workspace, result), nil
}
