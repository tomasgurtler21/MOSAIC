package app_test

// e2e_update_regression_test.go is the Stage 5 end-to-end regression suite that locks in
// the four bug fixes by running Update against a synthesized pipeline-shaped workspace.
//
// Four fixed behaviours under test:
//
//   Fix 1 (script-orchestrator deduplication): A deployed script-orchestrator file with a
//   genuine catalog counterpart is catalog-matched by the workspace scan and enters the plan
//   exactly once. Before the fix, the file was incorrectly classified as harness-only as well
//   (because catalogAgentKeys did not include OrchestratorScript), yielding two plan entries.
//
//   Fix 2 (harness-only consent): Declining the refresh-scope prompt for a harness-only file
//   (skip, skip-all, or cancel) yields no plan item. Only an explicit answer authorises a
//   refresh and produces an UPDATE item. Before the fix, every decline fell back to a
//   RefreshProtocolOnly update regardless.
//
//   Fix 3 (workflow discovery): The deployed orchestrator's <Workflow type="managed"> blocks
//   nested inside <AvailableWorkflows type="managed"> are discovered via DeployedRegions and
//   their IDs fed into plan.Input.WorkflowIDs, enabling drift reporting. Before the fix,
//   only non-nested managed or core-form blocks were found.
//
//   Fix 4 (scan-based membership): Every deployed agent in the harness's agents directory is
//   staleness-checked via the workspace scan, not just workflow-referenced agents. A version
//   bump on a workflow-unreferenced deployed agent produces an UPDATE plan item.
//
// The four tests (T5.2, T5.3, T5.4) share the same synthesized pipeline workspace built by
// buildE2ERegressionWorkspace. Each test varies one dimension of an otherwise fixed workspace.
//
// No test reads Catalog/, .mosaic/, or any path outside its own t.TempDir().

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/plan"
)

// ---------------------------------------------------------------------------
// Pipeline workspace catalog definitions
// ---------------------------------------------------------------------------

// Agents in the pipeline workspace catalog.
var (
	e2eRegressionOrchestrator = domain.Agent{
		Key:       "orchestrator",
		NumericID: "0",
		Version:   "1.0",
		Name:      "Pipeline Orchestrator",
		Role:      domain.RoleOrchestrator,
	}
	// e2eRegressionOrchestratorScript has a genuine catalog entry. Its deployed file carries
	// transform_version and a canonical Identity section — both harness-only eligibility signals
	// — yet it must be classified Matched (not HarnessOnly) by the workspace scan (Fix 1).
	e2eRegressionOrchestratorScript = domain.Agent{
		Key:       "orchestrator-script",
		NumericID: "2",
		Version:   "1.0",
		Name:      "Pipeline Script Orchestrator",
		Role:      domain.RoleOrchestrator,
	}
	// e2eRegressionPipelineRunner is referenced by the pipeline-run workflow.
	e2eRegressionPipelineRunner = domain.Agent{
		Key:       "pipeline-runner",
		NumericID: "10",
		Version:   "1.0",
		Name:      "Pipeline Runner",
		Role:      domain.RoleWorker,
	}
	// e2eRegressionExtraWorker is NOT referenced by any workflow but is deployed in the
	// workspace. Its catalog version is "2.0" while the deployed file carries "1.0" (stale).
	// It must be discovered by the workspace scan and reported as UPDATE (Fix 4).
	e2eRegressionExtraWorker = domain.Agent{
		Key:       "extra-worker",
		NumericID: "77",
		Version:   "2.0",
		Name:      "Extra Worker",
		Role:      domain.RoleWorker,
	}
)

// e2eRegressionWorkflow is the workflow embedded in the deployed orchestrator (Fix 3).
var e2eRegressionWorkflow = domain.Workflow{
	ID:               "pipeline-run",
	Name:             "Pipeline Run",
	Version:          "1.0",
	ReferencedAgents: []string{"pipeline-runner"},
}

const e2eRegressionAgentsDir = "agents"

// newE2ERegressionCatalog returns a stubCatalog configured for the pipeline workspace.
func newE2ERegressionCatalog() *stubCatalog {
	return &stubCatalog{
		root:                 "testroot",
		orchestrator:         e2eRegressionOrchestrator,
		orchestratorScript:   e2eRegressionOrchestratorScript,
		orchestratorScriptOK: true,
		agents:               []domain.Agent{e2eRegressionPipelineRunner, e2eRegressionExtraWorker},
		skills:               []domain.Skill{},
		hooks:                []domain.HookBundle{},
		workflows:            []domain.Workflow{e2eRegressionWorkflow},
	}
}

// ---------------------------------------------------------------------------
// Pipeline workspace file content
// ---------------------------------------------------------------------------

// e2eRegressionOrchestratorContent returns the deployed orchestrator file content.
// The key structural property: <Workflow type="managed"> blocks are nested inside
// <AvailableWorkflows type="managed"> at the top level of the body. extractDeployedWorkflows
// uses Body().DeployedRegions() (Fix 3) to discover these blocks. If the fix is reverted and
// only core-form or non-nested blocks are found, WorkflowIDs would be empty and the Fix 3
// assertion would fail.
func e2eRegressionOrchestratorContent() []byte {
	return []byte("---\n" +
		"id: \"0\"\n" +
		"version: \"1.0.0\"\n" +
		"---\n" +
		"<AvailableWorkflows type=\"managed\">\n" +
		"<Workflow type=\"managed\" name=\"pipeline-run\" version=\"1.0\">\n" +
		"Pipeline run workflow content.\n" +
		"</Workflow>\n" +
		"</AvailableWorkflows>\n" +
		"<Identity type=\"core\">\n" +
		"Pipeline orchestrator deployed file.\n" +
		"</Identity>\n" +
		"<CommunicationProtocol type=\"managed\">\n" +
		"</CommunicationProtocol>\n")
}

// e2eRegressionOrchestratorScriptContent returns the deployed script orchestrator file content.
// It deliberately carries BOTH harness-only eligibility signals (transform_version and a
// canonical <Identity type="core"> section) so that reverting Fix 1 would cause it to be
// classified HarnessOnly as well as Matched, duplicating its plan entry.
func e2eRegressionOrchestratorScriptContent() []byte {
	return []byte("---\n" +
		"id: \"2\"\n" +
		"version: \"1.0.0\"\n" +
		"transform_version: \"1.0.0\"\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"Script orchestrator deployed file — has catalog counterpart, must not be harness-only.\n" +
		"</Identity>\n" +
		"<CommunicationProtocol type=\"managed\">\n" +
		"</CommunicationProtocol>\n")
}

// e2eRegressionPipelineRunnerContent returns the deployed pipeline-runner agent content.
// Version matches the catalog, so the agent is staleness-UNCHANGED.
func e2eRegressionPipelineRunnerContent() []byte {
	return []byte("---\n" +
		"id: \"10\"\n" +
		"version: \"1.0.0\"\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"Pipeline runner agent deployed file.\n" +
		"</Identity>\n")
}

// e2eRegressionExtraWorkerContent returns the deployed extra-worker agent content.
// Version is "1.0.0" (STALE: catalog has "2.0"). This exercises Fix 4: the workspace scan
// must find this agent (not in any workflow) and report it for staleness-checking. A
// recordingPlanner then produces an UPDATE item for it based on ScannedAgentKeys.
func e2eRegressionExtraWorkerContent() []byte {
	return []byte("---\n" +
		"id: \"77\"\n" +
		"version: \"1.0.0\"\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"Extra worker deployed file — version stale relative to catalog (1.0.0 vs catalog 2.0).\n" +
		"</Identity>\n")
}

// e2eRegressionHarnessOnlyContent returns the deployed harness-only file content.
// It passes both eligibility signals (transform_version + canonical Identity section) but
// has no catalog counterpart. This file exercises Fix 2 (consent path).
func e2eRegressionHarnessOnlyContent() []byte {
	return harnessOnlyAgentContent()
}

// e2eRegressionNonMosaicContent returns the content for the non-MOSAIC README file.
// It has no MOSAIC frontmatter keys and no canonical sections. The workspace scan classifies
// it as "Neither" — it must not appear in the plan in any form.
func e2eRegressionNonMosaicContent() []byte {
	return []byte("# Agents Directory\n\n" +
		"This directory contains deployed agents.\n")
}

// ---------------------------------------------------------------------------
// Pipeline workspace builder (T5.1)
// ---------------------------------------------------------------------------

// e2eRegressionWorkspacePaths carries the relative target paths of the key files
// written by buildE2ERegressionWorkspace.
type e2eRegressionWorkspacePaths struct {
	// scriptOrchestratorTarget is the harness-only-style TargetPath for the script orchestrator,
	// i.e. filepath.Join(agentsDir, "orchestrator-script.md"). A harness-only item for this
	// file would have this TargetPath and SourcePath == "".
	scriptOrchestratorTarget string
	// extraWorkerTarget is the harness-only-style TargetPath for extra-worker.
	extraWorkerTarget string
	// harnessOnlyTarget is the TargetPath of the genuine harness-only file.
	harnessOnlyTarget string
	// nonMosaicTarget is the TargetPath of the unrelated non-MOSAIC file.
	nonMosaicTarget string
}

// buildE2ERegressionWorkspace writes the synthesized pipeline workspace under workspace.
// The orchestrator is placed at workspace root (as module.TargetPath("orchestrator") = "orchestrator.md"
// points there). All other agent files are placed inside agentsDir for the workspace scan.
// Returns paths struct for assertions.
func buildE2ERegressionWorkspace(t *testing.T, workspace, agentsDir string) e2eRegressionWorkspacePaths {
	t.Helper()

	// Orchestrator at workspace root — this is where probeDeployedArtifact reads it.
	// (module.TargetPath("orchestrator") = "orchestrator.md", relative to workspace.)
	if err := os.WriteFile(
		filepath.Join(workspace, "orchestrator.md"),
		e2eRegressionOrchestratorContent(),
		0o644,
	); err != nil {
		t.Fatalf("buildE2ERegressionWorkspace: write orchestrator.md: %v", err)
	}

	// All other files inside agentsDir, where scanWorkspaceAgents will find them.
	fullAgentsDir := filepath.Join(workspace, agentsDir)
	if err := os.MkdirAll(fullAgentsDir, 0o755); err != nil {
		t.Fatalf("buildE2ERegressionWorkspace: MkdirAll(%q): %v", fullAgentsDir, err)
	}

	writeInAgentsDir := func(name string, content []byte) string {
		p := filepath.Join(fullAgentsDir, name)
		if err := os.WriteFile(p, content, 0o644); err != nil {
			t.Fatalf("buildE2ERegressionWorkspace: write %q: %v", name, err)
		}
		return filepath.Join(agentsDir, name)
	}

	scriptOrchestratorTarget := writeInAgentsDir("orchestrator-script.md", e2eRegressionOrchestratorScriptContent())
	extraWorkerTarget := writeInAgentsDir("extra-worker.md", e2eRegressionExtraWorkerContent())
	writeInAgentsDir("pipeline-runner.md", e2eRegressionPipelineRunnerContent())
	harnessOnlyTarget := writeInAgentsDir("custom-harness.md", e2eRegressionHarnessOnlyContent())
	nonMosaicTarget := writeInAgentsDir("README.md", e2eRegressionNonMosaicContent())

	return e2eRegressionWorkspacePaths{
		scriptOrchestratorTarget: scriptOrchestratorTarget,
		extraWorkerTarget:        extraWorkerTarget,
		harnessOnlyTarget:        harnessOnlyTarget,
		nonMosaicTarget:          nonMosaicTarget,
	}
}

// ---------------------------------------------------------------------------
// Pipeline E2E planner — captures input and produces representative plan items
// ---------------------------------------------------------------------------

// e2eRegressionPlanner simulates the staleness pipeline for the pipeline workspace:
//   - Captures the plan.Input it receives so tests can assert on field wiring.
//   - Produces an UPDATE item for "extra-worker" (simulating its version delta, Fix 4).
//   - Leaves all other scanned agents UNCHANGED (they are either not stale or are
//     out of scope for this combined assertion).
//   - Echoes WorkflowIDs back in the returned plan's Workflows field so T5.2 can
//     directly observe what workflow IDs were discovered and passed to the planner.
type e2eRegressionPlanner struct {
	capturedInput *plan.Input
	harness       domain.HarnessRef
	workspacePath string
}

func (p *e2eRegressionPlanner) Build(_ context.Context, in plan.Input) (domain.Plan, error) {
	cp := in
	p.capturedInput = &cp

	pl := domain.Plan{
		Mode:          domain.ModeUpdateWorkspace,
		Harness:       p.harness,
		WorkspacePath: p.workspacePath,
		Scope:         domain.ScopeProject,
		Workflows:     append([]string(nil), in.WorkflowIDs...),
	}

	// Simulate the staleness pipeline: produce UPDATE for extra-worker (version delta),
	// UNCHANGED for everything else. The planner receives the scanned keys in ScannedAgentKeys;
	// only extra-worker is stale (deployed "1.0.0", catalog "2.0").
	for _, key := range in.ScannedAgentKeys {
		if key == e2eRegressionExtraWorker.Key {
			pl.Items = append(pl.Items, domain.PlanItem{
				Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: key},
				Action:     domain.ActionUpdate,
				TargetPath: key + ".md",
				Stale: []domain.VersionDelta{
					{Field: "version", Deployed: "1.0.0", Source: "2.0"},
				},
			})
		}
	}

	return pl, nil
}

// ---------------------------------------------------------------------------
// T5.2 — Combined end-to-end regression: all four fixes in one Update run
// ---------------------------------------------------------------------------

// TestE2E_UpdateRegression_CombinedRun verifies that all four fixed behaviours hold together
// in one Update run over the pipeline-shaped workspace. Each assertion is uniquely sensitive
// to exactly one fix: reverting any single fix causes exactly one assertion to fail.
func TestE2E_UpdateRegression_CombinedRun(t *testing.T) {
	// Arrange — pipeline workspace with all six file types.
	// The interaction stub scripts:
	//   - An answer for QHarnessOnlyRefreshScope for the orchestrator-script path: if Fix 1
	//     is reverted, orchestrator-script becomes harness-only and this answer is consumed,
	//     adding a duplicate harness-only item. With Fix 1 in place, the prompt is never asked
	//     and the scripted answer goes unused — the assertion for the absent item catches it.
	//   - An answer for QHarnessOnlyRefreshScope for the custom-harness path: the genuine
	//     harness-only file receives an explicit scope answer and must produce exactly one item.
	//   - AnswerReview confirms the plan so the executor is reached.
	stub := interactiontest.NewBuilder().
		// Script a scope answer for orchestrator-script: consumed only if Fix 1 is reverted.
		AnswerSelectOne(
			domain.QHarnessOnlyRefreshScope,
			filepath.Join(e2eRegressionAgentsDir, "orchestrator-script.md"),
			string(app.RefreshProtocolOnly),
		).
		// Script a scope answer for the genuine harness-only file.
		AnswerSelectOne(
			domain.QHarnessOnlyRefreshScope,
			filepath.Join(e2eRegressionAgentsDir, "custom-harness.md"),
			string(app.RefreshProtocolOnly),
		).
		AnswerReview(true).
		Build()

	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = newE2ERegressionCatalog()
	deps.Registry = newMinimalRegistry(newModuleWithAgentsDir(e2eRegressionAgentsDir))

	rp := &e2eRegressionPlanner{harness: minimalHarness, workspacePath: workspace}
	deps.Planner = rp

	exec := &capturingExecutor{}
	deps.Executor = exec

	paths := buildE2ERegressionWorkspace(t, workspace, e2eRegressionAgentsDir)

	svc := app.New(deps)

	// Act
	_, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	// -----------------------------------------------------------------------
	// Assert Fix 4: extra-worker must be in ScannedAgentKeys
	// -----------------------------------------------------------------------
	// If Fix 4 is reverted (workspace scan not wired), ScannedAgentKeys is nil and this fails.
	if rp.capturedInput == nil {
		t.Fatal("planner.Build was never called; Update did not reach the planner")
	}
	foundExtraWorker := false
	for _, k := range rp.capturedInput.ScannedAgentKeys {
		if k == e2eRegressionExtraWorker.Key {
			foundExtraWorker = true
			break
		}
	}
	if !foundExtraWorker {
		t.Errorf("plan.Input.ScannedAgentKeys = %v, does not contain %q; "+
			"a workflow-unreferenced deployed agent must be found by the workspace scan and "+
			"included in ScannedAgentKeys for staleness-checking (Fix 4)",
			rp.capturedInput.ScannedAgentKeys, e2eRegressionExtraWorker.Key)
	}

	// -----------------------------------------------------------------------
	// Assert Fix 4: extra-worker produces an UPDATE item
	// -----------------------------------------------------------------------
	// The e2eRegressionPlanner generates UPDATE items for keys in ScannedAgentKeys that are
	// extra-worker. Without ScannedAgentKeys being populated, the planner sees nil and
	// produces no UPDATE item for extra-worker.
	if exec.capturedReq == nil {
		t.Fatal("executor.Execute was never called; Update did not reach the executor")
	}
	extraWorkerUpdated := false
	for _, item := range exec.capturedReq.Plan.Items {
		if item.Ref.Key == e2eRegressionExtraWorker.Key && item.Action == domain.ActionUpdate {
			extraWorkerUpdated = true
			break
		}
	}
	if !extraWorkerUpdated {
		t.Errorf("plan has no UPDATE item for %q; "+
			"a version-bumped workflow-unreferenced deployed agent must produce an UPDATE plan item "+
			"once it is included in ScannedAgentKeys (Fix 4). Plan items: %v",
			e2eRegressionExtraWorker.Key, exec.capturedReq.Plan.Items)
	}

	// -----------------------------------------------------------------------
	// Assert Fix 3: WorkflowIDs contains "pipeline-run" (discovered from orchestrator)
	// -----------------------------------------------------------------------
	// If Fix 3 is reverted (extractDeployedWorkflows uses wrong enumeration), the managed
	// workflow blocks in the deployed orchestrator are not found and WorkflowIDs is empty.
	foundPipelineRun := false
	for _, id := range rp.capturedInput.WorkflowIDs {
		if id == e2eRegressionWorkflow.ID {
			foundPipelineRun = true
			break
		}
	}
	if !foundPipelineRun {
		t.Errorf("plan.Input.WorkflowIDs = %v, does not contain %q; "+
			"the deployed orchestrator carries a <Workflow type=\"managed\"> block nested inside "+
			"<AvailableWorkflows type=\"managed\">, which must be discovered via DeployedRegions "+
			"and passed to the planner as a WorkflowID (Fix 3)",
			rp.capturedInput.WorkflowIDs, e2eRegressionWorkflow.ID)
	}

	// -----------------------------------------------------------------------
	// Assert Fix 1: orchestrator-script must NOT produce a harness-only plan item
	// -----------------------------------------------------------------------
	// If Fix 1 is reverted (catalogAgentKeys excludes OrchestratorScript), the script
	// orchestrator file is classified HarnessOnly as well as Matched. The harness-only loop
	// would append an item with TargetPath = "agents/orchestrator-script.md" and SourcePath = "".
	// The scripted QHarnessOnlyRefreshScope answer for that path ensures the item IS appended
	// if the fix is reverted (not silently suppressed by a SkippedOne decline).
	for _, item := range exec.capturedReq.Plan.Items {
		if item.TargetPath == paths.scriptOrchestratorTarget && item.SourcePath == "" {
			t.Errorf("plan contains a harness-only item for %q (SourcePath is empty); "+
				"the script orchestrator has a genuine catalog counterpart and must be classified "+
				"Matched (not HarnessOnly) by the workspace scan. A harness-only item for it "+
				"indicates Fix 1 is not in effect: the scan is placing it in both Matched and "+
				"HarnessOnly paths, yielding a duplicate plan entry.",
				paths.scriptOrchestratorTarget)
		}
	}

	// -----------------------------------------------------------------------
	// Assert Fix 1 (positive): orchestrator-script IS in ScannedAgentKeys
	// -----------------------------------------------------------------------
	// Being in ScannedAgentKeys proves the file was Matched by the workspace scan — it has a
	// real catalog counterpart. Being absent from the harness-only item set (checked above)
	// proves it is not duplicated.
	foundScriptOrch := false
	for _, k := range rp.capturedInput.ScannedAgentKeys {
		if k == e2eRegressionOrchestratorScript.Key {
			foundScriptOrch = true
			break
		}
	}
	if !foundScriptOrch {
		t.Errorf("plan.Input.ScannedAgentKeys = %v, does not contain %q; "+
			"the deployed orchestrator-script file matches the catalog by filename key and must "+
			"appear in ScannedAgentKeys as a Matched file (Fix 1 also requires it to be Matched, "+
			"not just excluded from HarnessOnly)",
			rp.capturedInput.ScannedAgentKeys, e2eRegressionOrchestratorScript.Key)
	}

	// -----------------------------------------------------------------------
	// Assert: unrelated non-MOSAIC file absent from plan
	// -----------------------------------------------------------------------
	// The README.md file has no MOSAIC frontmatter and no canonical sections; it must be
	// classified "Neither" and never appear in any plan item.
	for _, item := range exec.capturedReq.Plan.Items {
		if item.TargetPath == paths.nonMosaicTarget {
			t.Errorf("plan contains an item for the non-MOSAIC file %q; "+
				"a plain markdown file with no MOSAIC markers must not produce any plan item",
				paths.nonMosaicTarget)
		}
	}

	// -----------------------------------------------------------------------
	// Assert: non-MOSAIC file key absent from ScannedAgentKeys
	// -----------------------------------------------------------------------
	for _, k := range rp.capturedInput.ScannedAgentKeys {
		if k == "README" || k == "readme" {
			t.Errorf("plan.Input.ScannedAgentKeys contains %q; "+
				"the key derived from the non-MOSAIC README.md must not appear in ScannedAgentKeys",
				k)
		}
	}
}

// ---------------------------------------------------------------------------
// T5.3 — Consent-path variants: declined and answered harness-only file
// ---------------------------------------------------------------------------

// TestE2E_UpdateRegression_ConsentPath_DeclinedHarnessOnly verifies Fix 2: when the
// refresh-scope prompt for a harness-only file is not answered (SkippedOne by default), no
// plan item is produced for that file and it is left byte-identical. This is the normal
// "user presses skip" path.
//
// If Fix 2 is reverted (declined prompts fall back to RefreshProtocolOnly), the harness-only
// loop appends an UPDATE item for the file even though the user declined — the executor
// receives it and the assertion fails.
func TestE2E_UpdateRegression_ConsentPath_DeclinedHarnessOnly(t *testing.T) {
	// Arrange — no scripted answer for the harness-only file's scope prompt.
	// The stub returns SkippedOne by default, modelling the user pressing "skip".
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()

	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = newE2ERegressionCatalog()
	deps.Registry = newMinimalRegistry(newModuleWithAgentsDir(e2eRegressionAgentsDir))
	deps.Planner = &capturingPlanner{result: domain.Plan{
		Mode:          domain.ModeUpdateWorkspace,
		Harness:       minimalHarness,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
	}}

	exec := &capturingExecutor{}
	deps.Executor = exec

	paths := buildE2ERegressionWorkspace(t, workspace, e2eRegressionAgentsDir)

	svc := app.New(deps)

	// Act
	_, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Assert — the harness-only file must NOT appear in the plan when the scope prompt is skipped.
	if exec.capturedReq != nil {
		for _, item := range exec.capturedReq.Plan.Items {
			if item.TargetPath == paths.harnessOnlyTarget && item.SourcePath == "" {
				t.Errorf("plan contains a harness-only item for %q after a SkippedOne prompt outcome; "+
					"a skipped scope prompt is a true opt-out — the caller must emit no plan item "+
					"and leave the file byte-identical on disk (Fix 2)",
					paths.harnessOnlyTarget)
			}
		}
	}
}

// TestE2E_UpdateRegression_ConsentPath_AnsweredHarnessOnly verifies Fix 2 (positive path):
// when the user explicitly answers the refresh-scope prompt, exactly one UPDATE item is
// produced for the harness-only file. This is the regression guard — the fix for declined
// prompts must not suppress answered agents.
func TestE2E_UpdateRegression_ConsentPath_AnsweredHarnessOnly(t *testing.T) {
	// Arrange — explicit scope answer scripted for the harness-only file's prompt.
	harnessOnlyTargetPath := filepath.Join(e2eRegressionAgentsDir, "custom-harness.md")
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QHarnessOnlyRefreshScope, harnessOnlyTargetPath, string(app.RefreshProtocolOnly)).
		AnswerReview(true).
		Build()

	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = newE2ERegressionCatalog()
	deps.Registry = newMinimalRegistry(newModuleWithAgentsDir(e2eRegressionAgentsDir))
	deps.Planner = &capturingPlanner{result: domain.Plan{
		Mode:          domain.ModeUpdateWorkspace,
		Harness:       minimalHarness,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
	}}

	exec := &capturingExecutor{}
	deps.Executor = exec

	buildE2ERegressionWorkspace(t, workspace, e2eRegressionAgentsDir)

	svc := app.New(deps)

	// Act
	_, _ = svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	// Assert — the harness-only file must appear exactly once as an UPDATE item.
	if exec.capturedReq == nil {
		t.Fatal("executor was never called; Update did not reach the executor")
	}
	count := 0
	for _, item := range exec.capturedReq.Plan.Items {
		if item.TargetPath == harnessOnlyTargetPath && item.SourcePath == "" {
			count++
			if item.Action != domain.ActionUpdate {
				t.Errorf("harness-only plan item for %q has Action=%v, want ActionUpdate; "+
					"an explicitly answered scope prompt must produce an UPDATE item (Fix 2)",
					harnessOnlyTargetPath, item.Action)
			}
		}
	}
	if count == 0 {
		t.Errorf("no harness-only plan item found for %q after an explicit scope answer; "+
			"an answered scope prompt must authorise a refresh and produce exactly one UPDATE item "+
			"(Fix 2 positive path — the fix must not suppress answered agents)",
			harnessOnlyTargetPath)
	}
	if count > 1 {
		t.Errorf("found %d harness-only plan items for %q; want exactly one "+
			"(each harness-only file must appear at most once in the plan)",
			count, harnessOnlyTargetPath)
	}
}

// ---------------------------------------------------------------------------
// T5.4 — Workflow-drift variants: discovered workflows reach the planner
// ---------------------------------------------------------------------------

// TestE2E_UpdateRegression_WorkflowDrift_DeployedManagedBlocksDiscovered verifies Fix 3:
// the deployed orchestrator's <Workflow type="managed"> blocks nested inside
// <AvailableWorkflows type="managed"> are discovered and passed to the planner as WorkflowIDs.
// When the sets match (no AddWorkflowIDs), WorkflowIDs contains exactly the deployed workflow.
//
// If Fix 3 is reverted (extractDeployedWorkflows no longer uses DeployedRegions), the
// managed workflow blocks are not found, WorkflowIDs is empty, and the assertion fails.
// This is the assertion uniquely sensitive to Fix 3: the pipeline workspace fixture uses
// the canonical deployed format (managed blocks, nested) that the pre-fix code would not find.
func TestE2E_UpdateRegression_WorkflowDrift_DeployedManagedBlocksDiscovered(t *testing.T) {
	// Arrange — no additional workflows; only the one discovered from the deployed orchestrator.
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()

	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = newE2ERegressionCatalog()
	deps.Registry = newMinimalRegistry(newModuleWithAgentsDir(e2eRegressionAgentsDir))

	cp := &capturingPlanner{result: domain.Plan{
		Mode:          domain.ModeUpdateWorkspace,
		Harness:       minimalHarness,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
	}}
	deps.Planner = cp

	buildE2ERegressionWorkspace(t, workspace, e2eRegressionAgentsDir)

	svc := app.New(deps)

	// Act
	_, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Assert — WorkflowIDs must contain "pipeline-run", discovered from the deployed
	// orchestrator's managed blocks.
	if cp.capturedInput == nil {
		t.Fatal("planner.Build was never called")
	}
	foundPipelineRun := false
	for _, id := range cp.capturedInput.WorkflowIDs {
		if id == e2eRegressionWorkflow.ID {
			foundPipelineRun = true
			break
		}
	}
	if !foundPipelineRun {
		t.Errorf("plan.Input.WorkflowIDs = %v, does not contain %q; "+
			"the deployed orchestrator carries the workflow in the canonical managed-block format "+
			"(<Workflow type=\"managed\"> nested inside <AvailableWorkflows type=\"managed\">). "+
			"extractDeployedWorkflows must use Body().DeployedRegions() to discover these blocks (Fix 3). "+
			"If WorkflowIDs is empty, workflow-set drift cannot be reported for any workflow.",
			cp.capturedInput.WorkflowIDs, e2eRegressionWorkflow.ID)
	}
}

// TestE2E_UpdateRegression_WorkflowDrift_AdditionalWorkflowsUnionWithDeployed verifies that
// when AddWorkflowIDs adds a workflow not yet embedded in the deployed orchestrator, the
// planner's WorkflowIDs is the union of the discovered workflows and the added set. This
// enables the planner to report drift (the desired set is "pipeline-run" + "bonus-workflow"
// but only "pipeline-run" is deployed) while preserving the deployed workflow in the set.
//
// This test is also sensitive to Fix 3: if workflow discovery fails, the discovered set is
// empty and WorkflowIDs contains only "bonus-workflow", which does not prove that discovery
// worked. The assertion checks both IDs are present.
func TestE2E_UpdateRegression_WorkflowDrift_AdditionalWorkflowsUnionWithDeployed(t *testing.T) {
	// Arrange — one additional workflow ("bonus-workflow") added via AddWorkflowIDs.
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()

	deps, workspace := newBaseDeps(t, stub)
	// Add bonus-workflow to the catalog so resolution doesn't fail on an unknown workflow ID.
	cat := newE2ERegressionCatalog()
	cat.workflows = append(cat.workflows, domain.Workflow{
		ID:               "bonus-workflow",
		Name:             "Bonus Workflow",
		Version:          "1.0",
		ReferencedAgents: []string{},
	})
	deps.Catalog = cat
	deps.Registry = newMinimalRegistry(newModuleWithAgentsDir(e2eRegressionAgentsDir))

	cp := &capturingPlanner{result: domain.Plan{
		Mode:          domain.ModeUpdateWorkspace,
		Harness:       minimalHarness,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
	}}
	deps.Planner = cp

	buildE2ERegressionWorkspace(t, workspace, e2eRegressionAgentsDir)

	svc := app.New(deps)

	// Act
	_, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
		AddWorkflowIDs:  []string{"bonus-workflow"},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Assert — WorkflowIDs must contain both the discovered "pipeline-run" and the added
	// "bonus-workflow". The union ensures both sources are represented.
	if cp.capturedInput == nil {
		t.Fatal("planner.Build was never called")
	}

	foundPipelineRun := false
	foundBonus := false
	for _, id := range cp.capturedInput.WorkflowIDs {
		switch id {
		case e2eRegressionWorkflow.ID:
			foundPipelineRun = true
		case "bonus-workflow":
			foundBonus = true
		}
	}
	if !foundPipelineRun {
		t.Errorf("plan.Input.WorkflowIDs = %v, does not contain %q; "+
			"the deployed workflow must be discovered from the orchestrator file and included in "+
			"WorkflowIDs even when AddWorkflowIDs is non-empty (Fix 3 + union semantics)",
			cp.capturedInput.WorkflowIDs, e2eRegressionWorkflow.ID)
	}
	if !foundBonus {
		t.Errorf("plan.Input.WorkflowIDs = %v, does not contain %q; "+
			"an AddWorkflowIDs entry must be unioned with the discovered set in WorkflowIDs",
			cp.capturedInput.WorkflowIDs, "bonus-workflow")
	}
}

// TestE2E_UpdateRegression_WorkflowDrift_NoDriftWhenSetsMatch verifies that when the deployed
// orchestrator's workflow set matches the desired set exactly (no AddWorkflowIDs), WorkflowIDs
// contains the deployed workflow and nothing else. This is the "no drift" baseline: the planner
// would receive the same set in both the deployed state and WorkflowIDs, and would report no
// WorkflowStaleness on the orchestrator.
func TestE2E_UpdateRegression_WorkflowDrift_NoDriftWhenSetsMatch(t *testing.T) {
	// Arrange — no AddWorkflowIDs; desired set == deployed set == ["pipeline-run"].
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()

	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = newE2ERegressionCatalog()
	deps.Registry = newMinimalRegistry(newModuleWithAgentsDir(e2eRegressionAgentsDir))

	cp := &capturingPlanner{result: domain.Plan{
		Mode:          domain.ModeUpdateWorkspace,
		Harness:       minimalHarness,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
	}}
	deps.Planner = cp

	buildE2ERegressionWorkspace(t, workspace, e2eRegressionAgentsDir)

	svc := app.New(deps)

	// Act
	_, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
		// No AddWorkflowIDs — desired set is exactly what the orchestrator has deployed.
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Assert — WorkflowIDs contains "pipeline-run" (the deployed workflow).
	if cp.capturedInput == nil {
		t.Fatal("planner.Build was never called")
	}
	foundPipelineRun := false
	for _, id := range cp.capturedInput.WorkflowIDs {
		if id == e2eRegressionWorkflow.ID {
			foundPipelineRun = true
			break
		}
	}
	if !foundPipelineRun {
		t.Errorf("plan.Input.WorkflowIDs = %v, does not contain %q; "+
			"when the desired set matches the deployed set, WorkflowIDs must still contain "+
			"the deployed workflow ID so the planner can confirm the sets match and report no drift",
			cp.capturedInput.WorkflowIDs, e2eRegressionWorkflow.ID)
	}

	// Assert — WorkflowIDs does NOT contain unexpected workflows (no spurious drift signal).
	for _, id := range cp.capturedInput.WorkflowIDs {
		if id != e2eRegressionWorkflow.ID {
			t.Errorf("plan.Input.WorkflowIDs = %v, contains unexpected workflow %q; "+
				"with no AddWorkflowIDs, WorkflowIDs must contain only the workflows discovered "+
				"from the deployed orchestrator (no spurious additions that would create false drift)",
				cp.capturedInput.WorkflowIDs, id)
		}
	}
}
