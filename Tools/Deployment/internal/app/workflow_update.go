package app

// workflow_update.go implements the workflow-only update use case: replacing the workflow set
// embedded in an already-deployed orchestrator. Agents, skills, and hooks that are already
// deployed are never touched. Agents required by the selected workflows that have no file in
// the workspace are deployed in the same run, using the deploy-new question flow.

import (
	"context"
	"path/filepath"

	"mosaic-deploy/internal/config"
	"mosaic-deploy/internal/deploy"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/plan"
	"mosaic-deploy/internal/todo"
)

// UpdateWorkflows runs the workflow-only update flow. See Service.UpdateWorkflows for the contract.
//
// The narrowed contract:
//   - Already-deployed artifacts (agent, skill, hook files that exist on disk) are never
//     planned, written, or version-stamped, regardless of staleness or local modification.
//   - Agents required by the selected workflows that have no file in the workspace are
//     deployed using the deploy-new question flow: same questions, same order, including
//     tier-level model questions and custom tool mapping.
//   - Skills required by newly-deployed agents that have no file are deployed in the same run.
//   - Model questions are asked only when there is at least one newly-required agent; when
//     every workflow-required agent is already deployed, no model or tool question is asked.
func (s *service) UpdateWorkflows(ctx context.Context, req WorkflowUpdateRequest) (domain.RunSummary, error) {
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

	snap, _ := s.deps.Manifest.Load(workspace)

	// Probe the deployed orchestrator to discover its embedded model and existing workflow set.
	// The orchestrator model is taken from the deployed file.
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

	// Replace semantics: use the user's selection directly. No union with the deployed set.
	// An unset (nil) WorkflowIDs causes the flow to ask through the Interaction port (CD-6).
	workflowIDs := req.WorkflowIDs
	if workflowIDs == nil {
		if req.SkipAll[domain.QWorkflows] {
			workflowIDs = []string{}
		} else {
			// Build the currently-deployed workflow set so the interaction port can mark
			// them for the user — a safety signal for the destructive replace semantics.
			deployedIDs := discoverExistingWorkflows(orchState)
			deployedSet := make(map[string]bool, len(deployedIDs))
			for _, id := range deployedIDs {
				deployedSet[id] = true
			}

			opts := make([]domain.Option, 0)
			for _, cat := range s.deps.Catalog.WorkflowCategories() {
				for _, wf := range cat.Workflows {
					hint := wf.Hint
					if deployedSet[wf.ID] {
						hint = "currently deployed"
					}
					opts = append(opts, domain.Option{
						ID: wf.ID, Label: wf.Name, Description: wf.Description, Hint: hint, Group: cat.Name,
					})
				}
			}
			q := domain.ChoiceQuestion{
				Question: domain.Question{ID: domain.QWorkflows, Title: "Select workflows", AllowSkip: true, AllowSkipAll: true},
				Options:  opts,
			}
			ans, err := s.deps.Interaction.SelectMany(ctx, q)
			if err != nil || ans.Status != domain.Answered {
				workflowIDs = []string{}
			} else {
				workflowIDs = ans.OptionIDs
			}
		}
	}

	// Resolve the artifact set from the selected workflow IDs to enumerate target paths for
	// the deployed-state probe. The plan will also resolve internally; this is the app layer's
	// probe-preparation step.
	set, err := plan.ResolveArtifacts(s.deps.Catalog, workflowIDs, nil, nil, nil)
	if err != nil {
		return domain.RunSummary{}, err
	}

	plannedPaths, err := plan.EnumerateTargetPaths(set, module, scope, s.deps.GOOS)
	if err != nil {
		return domain.RunSummary{}, err
	}

	// Seed the full probe with the orchestrator state already probed above so the orchestrator
	// file is read exactly once per run.
	var seed map[string]domain.DeployedArtifactState
	if orchTargetPath != "" {
		seed = map[string]domain.DeployedArtifactState{orchTargetPath: orchState}
	}

	// Build the agent-by-key map for id-based probe resolution.
	probeAgentByKey := make(map[string]domain.Agent, len(set.Agents))
	for _, a := range set.Agents {
		probeAgentByKey[a.Key] = a
	}

	deployedState, err := probeDeployedStateWithIndex(workspace, plannedPaths, module.Descriptor().Frontmatter.ModelKey, seed, deployedAgentIndex, probeAgentByKey, nil)
	if err != nil {
		return domain.RunSummary{}, err
	}

	// Detect agents required by the selected workflows that have no file in the workspace.
	// These are the agents this run will deploy for the first time.
	newAgents := newlyRequiredAgents(set.Agents, plannedPaths, deployedState, orchestrator.Key)

	// Resolve models for new agents using the deploy-new question flow:
	// - tierSkipOverride=false: tier questions are asked (same as deploy-new, unlike Update
	//   which passes true to suppress them)
	// - agentSkipOverride=false: per-agent questions are asked when no tier mapping resolves
	// This whole step is gated on a non-empty new-agent set: when every workflow-required
	// agent is already deployed, neither resolveModels nor resolveCustomTools is called, so
	// no model or tool question of any kind is asked.
	var newAgentModelRes modelResolution
	var customTools map[string]string
	var skippedTools map[string]bool
	if len(newAgents) > 0 {
		orderedNewAgents := s.orderedAgentsForModelResolution(workflowIDs, newAgents)
		var err error
		newAgentModelRes, err = s.resolveModels(
			ctx, nil, nil, req.SkipAll,
			harnessID, module, orderedNewAgents,
			nil, false, false,
			// tierSkipOverride=false, agentSkipOverride=false: deploy-new parity.
			// Update passes true for tierSkipOverride to suppress tier questions; this
			// flow deliberately does not, because the requirement is full deploy-new parity.
		)
		if err != nil {
			return domain.RunSummary{}, err
		}
		customTools, skippedTools = s.resolveCustomTools(ctx, nil, req.SkipAll, module, newAgents)
	}

	// Orchestrator model comes from the deployed file. Merge with newly resolved models
	// for new agents (new-agent selections take precedence for their own keys).
	models := make(map[string]domain.ModelSelection)
	if orchState.ModelID != "" {
		models[orchestrator.Key] = domain.ModelSelection{ModelID: orchState.ModelID, Origin: domain.OriginDeployed}
	}
	for k, v := range newAgentModelRes.models {
		models[k] = v
	}
	if len(models) == 0 {
		models = nil
	}

	toolCfg, _ := s.deps.ToolConfig.Load()
	userCfg, err := s.deps.UserConfig.Load()
	if err != nil {
		return domain.RunSummary{}, err
	}
	toolMappingsVersion := config.HashToolDestinations(toolCfg.ToolDestinations, userCfg.ToolDestinations)

	planInput := plan.Input{
		Catalog: s.deps.Catalog, Module: module, Mode: domain.ModeUpdateWorkflows,
		WorkspacePath: workspace, Scope: scope, GOOS: s.deps.GOOS,
		Manifest: snap, WorkflowIDs: workflowIDs,
		DeployedState:       deployedState,
		Models:              models,
		ToolMappingsVersion: toolMappingsVersion,
		ProtocolVersion:     protocol.Version,
		BundleVersion:       bundle.Version,
	}
	p, err := s.deps.Planner.Build(ctx, planInput)
	if err != nil {
		return domain.RunSummary{}, err
	}

	// Compute the set of newly-required skill keys from the new agents and deployed state.
	// This must happen after Planner.Build because it uses the same deployedState.
	newSkillKeys := newlyRequiredSkillKeys(newAgents, set.Skills, plannedPaths, deployedState)

	// Build the new-agent key set for the admission predicate.
	newAgentKeys := make(map[string]bool, len(newAgents))
	for _, a := range newAgents {
		newAgentKeys[a.Key] = true
	}

	// Admit only the orchestrator, ActionCreate items for newly-required agents, and
	// ActionCreate items for newly-required skills. Every other item (stale/unchanged/conflict
	// non-orchestrator agents, all skills outside newSkillKeys, all hooks) is dropped.
	// Registrations relate to hook artifacts and remain unconditionally cleared.
	filteredItems := make([]domain.PlanItem, 0, len(p.Items))
	for _, item := range p.Items {
		if admitWorkflowUpdateItem(item, orchestrator.Key, newAgentKeys, newSkillKeys) {
			filteredItems = append(filteredItems, item)
		}
	}
	p.Items = filteredItems
	p.Registrations = nil

	// Forward plan gaps to the todo collector before conflict handling.
	for _, g := range p.Gaps {
		s.deps.Todo.AddGap(g)
	}

	// Conflict handling for the orchestrator file: same decision options and backup behaviour
	// as the existing update flow. A skip decision records a GapSkippedFile gap.
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
				Kind:    domain.GapSkippedFile,
				Subject: item.Ref.Key,
				Detail:  "file was locally modified and the user chose to skip it",
			})
		}
	}

	// Review is always shown; AutoConfirmPlan only controls whether a decline/cancel aborts the run.
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

	workflowBlocks := s.buildWorkflowBlocks(workflowIDs)
	deployedReader := func(item domain.PlanItem) []byte {
		return readDeployedFile(workspace, item.TargetPath)
	}
	// Custom tools resolved above for new agents; already-deployed agents are never in the
	// input to resolveCustomTools so no already-deployed agent can trigger a tool question.
	contentFn := s.buildContent(module, agentByKey, models, customTools, skippedTools, workflowBlocks, nil, scope, deployedReader, toolMappingsVersion, protocol, bundle, nil)

	// Version stamps cover the orchestrator and all admitted new-agent and new-skill items.
	// set.Skills is passed so admitted skill items are stamped; set.Hooks is nil because hook
	// artifacts are entirely out of scope for this flow.
	versionStamps := buildVersionStamps(set.Agents, set.Skills, nil, p.Items, module.Descriptor(), toolMappingsVersion)

	now := s.now()
	execReq := deploy.ExecRequest{
		Plan:          p,
		MosaicRoot:    s.deps.MosaicRoot,
		Content:       contentFn,
		Conflicts:     conflicts,
		VersionStamps: versionStamps,
		Hooks:         nil, // no hook artifacts written in this mode
		TodoItems:     s.deps.Todo.Items,
		TodoMeta: todo.Meta{
			Harness: harnessRef.DisplayName, WorkspacePath: workspace, DeploymentRoot: workspace,
			GeneratedAt: now, Mode: domain.ModeUpdateWorkflows,
		},
		DryRun: req.DryRun,
		// UpdateWorkflows opts into all-or-nothing execution so a failed run restores every
		// written file to its pre-run bytes and leaves the workspace, orchestrator, manifest,
		// and checklist exactly as they were before the run. Fallback runs are excluded from
		// reversal by the executor and keep non-atomic semantics.
		Atomic: true,
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

	// Persist tier models and custom model IDs collected during this run (from model
	// questions asked for newly-required agents), matching deploy-new's persistence step.
	if len(newAgents) > 0 {
		if err := s.persistTierModels(harnessID, newAgentModelRes.tierModelsUsed); err != nil {
			s.notifyPersistFailure(ctx, err)
		}
		customIDs := make([]string, 0, len(newAgentModelRes.accumulatedOptions))
		for _, opt := range newAgentModelRes.accumulatedOptions {
			customIDs = append(customIDs, opt.ID)
		}
		if err := s.persistCustomModelIDs(harnessID, customIDs); err != nil {
			s.notifyPersistFailure(ctx, err)
		}
	}

	return s.buildSummary(domain.ModeUpdateWorkflows, harnessRef, workspace, result), nil
}
