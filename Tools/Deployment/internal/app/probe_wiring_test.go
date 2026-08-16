package app_test

// probe_wiring_test.go verifies that both the deploy-new and update flows correctly wire the
// deployed-state probe into plan.Input before calling the planner.
//
// Verified invariants (T3.4, T3.5):
//
// T3.4 — Deployed-state population:
//   - The deploy-new flow probes the deployed state and passes plan.Input.DeployedState to
//     the planner, with one entry per planned target path (including absent paths).
//   - The update flow does the same.
//
// T3.5 — Update-flow workflow discovery sourced from probe:
//   - The update flow discovers existing workflow IDs from the deployed orchestrator via the
//     probe result, not via a separate direct file read.
//   - The discovered workflow IDs are merged with any newly requested IDs and passed to the
//     planner in plan.Input.WorkflowIDs.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/manifest"
	"mosaic-deploy/internal/plan"
)

// ---------------------------------------------------------------------------
// Spy planner for input capture
// ---------------------------------------------------------------------------

// spyPlanner is a plan.Planner implementation that records the plan.Input it receives on
// each Build call, so tests can inspect how the app layer populated the input.
type spyPlanner struct {
	capturedInputs []plan.Input
	response       domain.Plan
	err            error
}

func (s *spyPlanner) Build(_ context.Context, in plan.Input) (domain.Plan, error) {
	s.capturedInputs = append(s.capturedInputs, in)
	return s.response, s.err
}

// lastCaptured returns the most recently captured plan.Input, failing the test if Build was
// never called.
func (s *spyPlanner) lastCaptured(t *testing.T) plan.Input {
	t.Helper()
	if len(s.capturedInputs) == 0 {
		t.Fatal("planner.Build was never called; the flow did not reach the plan-build step")
	}
	return s.capturedInputs[len(s.capturedInputs)-1]
}

// ---------------------------------------------------------------------------
// T3.4 — Deploy-new flow: DeployedState populated for every planned target path
// ---------------------------------------------------------------------------

// TestDeployNew_DeployedStatePopulated_ContainsEntryPerPlannedPath verifies that after a
// deploy-new run, plan.Input.DeployedState contains one entry for every artifact the planner
// will classify, including paths where no file is deployed yet (absent entries).
func TestDeployNew_DeployedStatePopulated_ContainsEntryPerPlannedPath(t *testing.T) {
	// Arrange — write the orchestrator file to the workspace; the agent file is absent.
	ws := t.TempDir()
	orchestratorContent := []byte("---\nversion: \"1.0\"\n---\nOrchestrator body.\n")
	writeProbeWiringFile(t, ws, "orchestrator.md", orchestratorContent)
	// "test-runner.md" is intentionally NOT written — it must appear with Present: false.

	spy := &spyPlanner{response: newMinimalPlan(ws)}
	stub := interactiontest.NewBuilder().Build()
	deps, _ := newBaseDeps(t, stub)
	deps.Planner = spy
	deps.Manifest = &stubManifestStore{}
	svc := app.New(deps)

	// Act
	_, err := svc.DeployNew(context.Background(), newDeployRequest(ws, "stub-harness", []string{"quick-fix"}))
	if err != nil {
		t.Fatalf("DeployNew: %v", err)
	}

	// Assert — the planner must have received a non-nil DeployedState.
	captured := spy.lastCaptured(t)
	if captured.DeployedState == nil {
		t.Fatal("plan.Input.DeployedState is nil; the deploy-new flow must populate it before calling Build")
	}

	// Per-path invariant: every planned path must appear in DeployedState.
	// The stub harness maps agent key -> "<key>.md".

	// orchestrator.md was written to the workspace; it must appear with Present: true.
	if orchState, ok := captured.DeployedState["orchestrator.md"]; !ok {
		t.Error("orchestrator.md not found in plan.Input.DeployedState; the probe must include every planned path")
	} else if !orchState.Present {
		t.Errorf("DeployedState[orchestrator.md].Present = false, want true (file was written to workspace)")
	}

	// test-runner.md was intentionally not written; it must still appear with Present: false.
	if runnerState, ok := captured.DeployedState["test-runner.md"]; !ok {
		t.Error("test-runner.md not found in plan.Input.DeployedState; absent paths must still receive an entry")
	} else if runnerState.Present {
		t.Errorf("DeployedState[test-runner.md].Present = true, want false (file was not written to workspace)")
	}
}

// TestDeployNew_DeployedStateIncludesAbsentPaths_EntryPresentFalse verifies that paths where
// no file exists on disk are still present in plan.Input.DeployedState with Present: false.
func TestDeployNew_DeployedStateIncludesAbsentPaths_EntryPresentFalse(t *testing.T) {
	ws := t.TempDir()
	// Write NO files — every target path is absent.

	spy := &spyPlanner{response: newMinimalPlan(ws)}
	stub := interactiontest.NewBuilder().Build()
	deps, _ := newBaseDeps(t, stub)
	deps.Planner = spy
	svc := app.New(deps)

	_, err := svc.DeployNew(context.Background(), newDeployRequest(ws, "stub-harness", []string{"quick-fix"}))
	if err != nil {
		t.Fatalf("DeployNew: %v", err)
	}

	captured := spy.lastCaptured(t)
	if captured.DeployedState == nil {
		t.Fatal("plan.Input.DeployedState is nil; the flow must probe even when no files exist")
	}
	// Every entry in DeployedState for an absent file must have Present: false.
	for path, state := range captured.DeployedState {
		if state.Present {
			t.Errorf("DeployedState[%q].Present = true but no files were written to workspace; expected false",
				path)
		}
	}
}

// ---------------------------------------------------------------------------
// T3.4 — Update flow: DeployedState populated for every planned target path
// ---------------------------------------------------------------------------

// TestUpdate_DeployedStatePopulated_ContainsEntryPerPlannedPath verifies that the update flow
// also populates plan.Input.DeployedState before calling the planner.
func TestUpdate_DeployedStatePopulated_ContainsEntryPerPlannedPath(t *testing.T) {
	ws := t.TempDir()

	spy := &spyPlanner{response: domain.Plan{
		Mode:          domain.ModeUpdateWorkspace,
		Harness:       minimalHarness,
		WorkspacePath: ws,
		Scope:         domain.ScopeProject,
	}}
	stub := interactiontest.NewBuilder().Build()
	deps, _ := newBaseDeps(t, stub)
	deps.Planner = spy
	svc := app.New(deps)

	_, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   ws,
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	captured := spy.lastCaptured(t)
	if captured.DeployedState == nil {
		t.Fatal("plan.Input.DeployedState is nil; the update flow must populate it before calling Build")
	}

	// Per-path invariant: at minimum, the orchestrator path must appear in DeployedState.
	// With no deployed orchestrator and no workflow IDs discovered, the plan includes only the
	// orchestrator. The stub harness maps "orchestrator" -> "orchestrator.md".
	if _, ok := captured.DeployedState["orchestrator.md"]; !ok {
		t.Error("orchestrator.md not found in plan.Input.DeployedState; the update flow must probe every planned path including the orchestrator")
	}
}

// ---------------------------------------------------------------------------
// T3.5 — Update-flow workflow discovery sourced from probe
// ---------------------------------------------------------------------------

// TestUpdate_WorkflowDiscovery_UsesDeployedOrchestratorContent verifies that the update flow
// discovers the workflow IDs already embedded in the deployed orchestrator and includes them
// in the plan's workflow selection, ensuring workflows are not lost on update.
//
// The orchestrator file in the workspace contains two workflow sections. The update request
// specifies no AddWorkflowIDs. After the flow runs, the planner's WorkflowIDs must include
// both IDs found in the deployed orchestrator.
func TestUpdate_WorkflowDiscovery_UsesDeployedOrchestratorContent(t *testing.T) {
	ws := t.TempDir()

	// Write a deployed orchestrator containing two workflow sections.
	orchestratorContent := []byte("---\nversion: \"1.0\"\n---\n\n" +
		"<Workflow type=\"core\" name=\"quick-fix\" version=\"1.0\">\n" +
		"Quick fix content.\n" +
		"</Workflow>\n\n" +
		"<Workflow type=\"core\" name=\"code-review\" version=\"1.0\">\n" +
		"Code review content.\n" +
		"</Workflow>\n")
	// The stubHarnessModule maps orchestrator key -> "orchestrator.md"
	writeProbeWiringFile(t, ws, "orchestrator.md", orchestratorContent)

	spy := &spyPlanner{response: domain.Plan{
		Mode:          domain.ModeUpdateWorkspace,
		Harness:       minimalHarness,
		WorkspacePath: ws,
		Scope:         domain.ScopeProject,
		Workflows:     []string{"quick-fix", "code-review"},
	}}
	stub := interactiontest.NewBuilder().Build()
	deps, _ := newBaseDeps(t, stub)
	deps.Planner = spy
	// The catalog must know about both workflows for them to resolve.
	cat := newMinimalCatalog()
	cat.workflows = append(cat.workflows, domain.Workflow{
		ID:      "code-review",
		Name:    "Code Review",
		Version: "1.0",
	})
	deps.Catalog = cat
	svc := app.New(deps)

	_, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   ws,
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	captured := spy.lastCaptured(t)

	// The planner must have received both workflow IDs discovered from the deployed orchestrator.
	hasQuickFix := false
	hasCodeReview := false
	for _, id := range captured.WorkflowIDs {
		if id == "quick-fix" {
			hasQuickFix = true
		}
		if id == "code-review" {
			hasCodeReview = true
		}
	}
	if !hasQuickFix {
		t.Errorf("WorkflowIDs missing %q; existing-workflow discovery must include it from the deployed orchestrator", "quick-fix")
	}
	if !hasCodeReview {
		t.Errorf("WorkflowIDs missing %q; existing-workflow discovery must include it from the deployed orchestrator", "code-review")
	}
}

// TestUpdate_WorkflowDiscovery_NoSeparateOrchestratorRead verifies that the update flow does
// not perform a second read of the orchestrator file after the initial probe. This is checked
// indirectly: the probe state for the orchestrator path must carry the workflow IDs that
// reach the planner, confirming they came from the probe and not a second os.ReadFile call.
func TestUpdate_WorkflowDiscovery_NoSeparateOrchestratorRead(t *testing.T) {
	ws := t.TempDir()
	orchestratorContent := []byte("---\nversion: \"1.0\"\n---\n\n" +
		"<Workflow type=\"core\" name=\"quick-fix\" version=\"1.0\">\n" +
		"Content.\n" +
		"</Workflow>\n")
	writeProbeWiringFile(t, ws, "orchestrator.md", orchestratorContent)

	spy := &spyPlanner{response: domain.Plan{
		Mode:          domain.ModeUpdateWorkspace,
		Harness:       minimalHarness,
		WorkspacePath: ws,
		Scope:         domain.ScopeProject,
		Workflows:     []string{"quick-fix"},
	}}
	stub := interactiontest.NewBuilder().Build()
	deps, _ := newBaseDeps(t, stub)
	deps.Planner = spy
	svc := app.New(deps)

	_, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   ws,
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	captured := spy.lastCaptured(t)

	// If the probe correctly seeds the orchestrator state, the DeployedState entry for the
	// orchestrator must carry the workflow IDs. This confirms the single-probe path.
	orchState, ok := captured.DeployedState["orchestrator.md"]
	if !ok {
		t.Fatal("orchestrator.md not found in plan.Input.DeployedState; probe must include the orchestrator path")
	}
	if !orchState.Present {
		t.Error("orchestrator.md must be Present: true in DeployedState")
	}
	orchIDs := orchState.Workflows.IDs()
	found := false
	for _, id := range orchIDs {
		if id == "quick-fix" {
			found = true
		}
	}
	if !found {
		t.Error("orchestrator's DeployedState.Workflows does not contain 'quick-fix'; " +
			"the probe must extract workflow IDs from the deployed orchestrator content")
	}
}

// ---------------------------------------------------------------------------
// End-to-end conflict detection (AC3.5)
// ---------------------------------------------------------------------------

// TestDeployNew_LocallyModifiedFile_ConflictDetectedEndToEnd verifies that when a workspace
// file's content hash (as probed from disk) differs from the hash recorded in the manifest,
// the planner classifies the item as ActionConflict and the flow asks QLocalModification.
//
// This is an end-to-end test: it uses the real plan builder so that the full path from
// probe → DeployedState → planner classification is exercised rather than a pre-scripted
// plan stub. The test will be in RED phase until the deploy-new flow wires the probe and
// populates plan.Input.DeployedState before calling the planner.
func TestDeployNew_LocallyModifiedFile_ConflictDetectedEndToEnd(t *testing.T) {
	ws := t.TempDir()

	// Write "test-runner.md" with content that has a specific hash.
	fileContent := []byte("---\nversion: \"1.0\"\n---\nThis file was locally modified after deployment.\n")
	writeProbeWiringFile(t, ws, "test-runner.md", fileContent)

	// The manifest records a different hash for the same target path, simulating a file that
	// was modified on disk after the last deployment wrote the recorded hash.
	manifestEntry := domain.ManifestEntry{
		Ref:         domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "test-runner"},
		TargetPath:  "test-runner.md",
		Version:     "1.0",
		ContentHash: "sha256:0000000000000000000000000000000000000000000000000000000000000000",
	}
	snap := manifest.Snapshot{
		State: manifest.StatePresent,
		Manifest: domain.Manifest{
			Entries: []domain.ManifestEntry{manifestEntry},
		},
	}

	// Pre-answer QLocalModification so the flow does not block waiting for user input.
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QLocalModification, "test-runner.md", string(domain.DecisionSkip)).
		Build()
	deps, _ := newBaseDeps(t, stub)
	deps.Manifest = &stubManifestStore{snap: snap}
	// Use the real planner: conflict classification must come from the planner reading
	// DeployedState[test-runner.md].ContentHash != manifestEntry.ContentHash, not from a pre-scripted plan.
	deps.Planner = plan.New()
	svc := app.New(deps)

	// Act — the probe must populate DeployedState[test-runner.md] with the real file hash;
	// the real planner must see the hash mismatch and classify test-runner.md as ActionConflict.
	_, err := svc.DeployNew(context.Background(), newDeployRequest(ws, "stub-harness", []string{"quick-fix"}))
	if err != nil {
		t.Fatalf("DeployNew: %v", err)
	}

	// Assert — QLocalModification must have been asked for test-runner.md, proving that:
	//   (1) the probe ran and set DeployedState[test-runner.md].ContentHash to the real file hash,
	//   (2) the real planner detected that hash != manifest's recorded hash and returned
	//       ActionConflict for that item,
	//   (3) the deploy-new flow forwarded the conflict to the user via QLocalModification.
	if !stub.WasAsked(domain.QLocalModification, "test-runner.md") {
		t.Error("QLocalModification was not asked for test-runner.md; " +
			"expected end-to-end conflict detection to fire when the probed file hash " +
			"differs from the manifest's recorded hash (DeployedState must be populated " +
			"from the probe before calling the planner)")
	}
}

// ---------------------------------------------------------------------------
// Helpers (local to this test file)
// ---------------------------------------------------------------------------

// writeProbeWiringFile writes content to name inside dir. Fails the test fatally if the write fails.
func writeProbeWiringFile(t *testing.T, dir, name string, content []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
		t.Fatalf("writeProbeWiringFile %q: %v", name, err)
	}
}
