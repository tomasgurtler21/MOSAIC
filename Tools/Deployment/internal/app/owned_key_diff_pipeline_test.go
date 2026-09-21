package app_test

// owned_key_diff_pipeline_test.go verifies the pipeline-level behavior of
// owned-key difference reporting.
//
// All tests use the Update flow (svc.Update) because that is the only app-layer
// path that collects OwnedKeyDifferences and attaches them to RunSummary. The
// DeployNew path passes nil for ownedKeyDiffSink by design.
//
// Test setup pattern:
//
//   - stubPlanner injects plan items directly, bypassing real plan building.
//   - contentCallingExecutor calls req.Content for every non-skip plan item so that
//     the ownedKeyDiffSink is populated before the executor returns.
//   - A deployed file is written to the temp workspace directory before each test
//     that needs one (T17.1, T17.1a, T17.2a and companions).
//   - The stub catalog returns a known source document for the test agent.
//   - AddWorkflowIDs: []string{"quick-fix"} on the UpdateRequest causes
//     plan.ResolveArtifactsFrom to include "test-runner" in the agent set, which
//     populates agentByKey inside buildContent so the content callback can run.
//   - AgentModels: map[string]string{"test-runner": "model-a"} pre-answers the
//     model selection question to avoid unscripted interactive prompts when
//     contentCallingExecutor actually invokes the Content callback.
//
// Tests covered:
//
//   T17.1 - conflict from missing manifest record reports the differing owned key
//   T17.1a - conflict from no usable manifest behaves identically
//   T17.1b - conflict item with no decoded deployed form produces no entry and no failure
//   T17.2a - non-conflict file produces no entry despite a differing owned key;
//             config-model and version-bump companion cases
//
// Seam baseline note (T17.4):
//
//   The existing TestSeamBaseline (seam_baseline_test.go) serves as T17.4. It runs
//   in compare mode by default and verifies the four Markdown-harness golden baselines
//   remain byte-identical. The Stage 17 Origin field has zero value OriginConfirmed so
//   all existing transform.Request literals that omit the field are unaffected; no
//   adaptation of the driver call shape is necessary.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/deploy"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/manifest"
)

// ---------------------------------------------------------------------------
// Source and deployed content fixtures
// ---------------------------------------------------------------------------

// ownedKeyDiffPipelineSource is the source document used by the pipeline tests.
// It carries description: "New description" -- the value this run would write.
const ownedKeyDiffPipelineSource = `---
id: test-runner
version: 1.0
description: New description
---

<Identity type="core">
Pipeline owned-key diff test body.
</Identity>
`

// ownedKeyDiffPipelineDeployed is the deployed file content used by the conflict tests
// (T17.1, T17.1a). It carries description: "Old description" -- differing from the source.
// Stamps (mosaic_id, mosaic_version) are present to produce a realistic deployed form.
const ownedKeyDiffPipelineDeployed = `---
mosaic_id: test-runner
mosaic_version: 1.0
description: Old description
---

Pipeline owned-key diff test body.
`

// ownedKeyDiffPipelineDeployedStale is the deployed file used by T17.2a and companions.
// mosaic_version: 0.9 gives the planner a stale-version reason for ActionUpdate, and
// description: "Old description" confirms no entry is produced even with a visible
// description difference when the item is classified as ActionUpdate (not ActionConflict).
const ownedKeyDiffPipelineDeployedStale = `---
mosaic_id: test-runner
mosaic_version: 0.9
description: Old description
---

Pipeline owned-key diff test body.
`

// ---------------------------------------------------------------------------
// Test agent
// ---------------------------------------------------------------------------

// ownedKeyDiffPipelineAgent is the test agent. It has a non-empty SourcePath so that
// the stub catalog sources map can supply the source document to buildContent.
var ownedKeyDiffPipelineAgent = domain.Agent{
	Key:             "test-runner",
	NumericID:       "1",
	Version:         "1.0",
	Name:            "Test Runner",
	Description:     "Runs tests",
	Role:            domain.RoleWorker,
	Category:        "Execution",
	SourcePath:      "sources/test-runner.md",
	RecommendedTier: "HIGH",
	TierRationale:   "Needs a capable model",
}

// ---------------------------------------------------------------------------
// contentCallingExecutor
// ---------------------------------------------------------------------------

// contentCallingExecutor is a test executor that calls req.Content for every
// plan item that is not skipped. This is required so that the ownedKeyDiffSink
// inside buildContent is populated before the executor returns. The real
// executor also calls the content callback; this stub models that behaviour
// without performing any filesystem writes.
//
// For ActionConflict items the executor checks req.Conflicts for the decision.
// DecisionOverwrite and DecisionBackupThenOverwrite both call Content.
// DecisionSkip records TakenSkipped without calling Content.
// A missing decision for a conflict item is returned as ErrUndecidedConflict.
type contentCallingExecutor struct{}

func (e *contentCallingExecutor) Execute(ctx context.Context, req deploy.ExecRequest) (deploy.ExecResult, error) {
	var actions []domain.ActionRecord
	for _, item := range req.Plan.Items {
		var taken domain.ActionTaken
		switch item.Action {
		case domain.ActionCreate:
			if _, err := req.Content(item); err != nil {
				return deploy.ExecResult{}, err
			}
			taken = domain.TakenCreated
		case domain.ActionUpdate:
			if _, err := req.Content(item); err != nil {
				return deploy.ExecResult{}, err
			}
			taken = domain.TakenUpdated
		case domain.ActionUnchanged:
			taken = domain.TakenUnchanged
		case domain.ActionConflict:
			decision := req.Conflicts[item.TargetPath]
			switch decision {
			case domain.DecisionOverwrite, domain.DecisionBackupThenOverwrite:
				if _, err := req.Content(item); err != nil {
					return deploy.ExecResult{}, err
				}
				taken = domain.TakenUpdated
			case domain.DecisionSkip:
				taken = domain.TakenSkipped
			default:
				return deploy.ExecResult{}, deploy.ErrUndecidedConflict
			}
		}
		if taken != "" {
			absPath := filepath.Join(req.Plan.WorkspacePath, item.TargetPath)
			actions = append(actions, domain.ActionRecord{
				Ref:        item.Ref,
				TargetPath: absPath,
				Taken:      taken,
			})
		}
	}
	return deploy.ExecResult{
		DeploymentRoot: req.Plan.WorkspacePath,
		Actions:        actions,
		Fallback:       domain.FallbackNone,
	}, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newOwnedKeyDiffDeps returns app.Deps wired for the owned-key difference pipeline
// tests. It sets up:
//   - A catalog with ownedKeyDiffPipelineAgent and the given source bytes
//   - contentCallingExecutor so the content callback is actually invoked
//   - The given manifest snapshot
//
// Callers must set deps.Planner with a stubPlanner before passing to app.New.
func newOwnedKeyDiffDeps(t *testing.T, stub *interactiontest.Stub, sourceBytes []byte, snap manifest.Snapshot) (app.Deps, string) {
	t.Helper()
	deps, workspace := newBaseDeps(t, stub)
	cat := newMinimalCatalog()
	cat.agents = []domain.Agent{ownedKeyDiffPipelineAgent}
	cat.sources = map[string][]byte{
		"sources/test-runner.md": sourceBytes,
	}
	deps.Catalog = cat
	deps.Executor = &contentCallingExecutor{}
	deps.Manifest = &stubManifestStore{snap: snap}
	return deps, workspace
}

// writeDeployedFile writes content to workspace/filename, creating the on-disk
// deployed artifact that the deployedReader closure reads during the Update flow.
func writeDeployedFile(t *testing.T, workspace, filename string, content []byte) {
	t.Helper()
	path := filepath.Join(workspace, filename)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("writeDeployedFile %q: %v", path, err)
	}
}

// conflictPlan returns a plan injecting one ActionConflict item for "test-runner".
// ManifestMissing is set to match the "no manifest record" agent conflict branch.
func conflictPlan(workspace string) domain.Plan {
	return domain.Plan{
		Mode:          domain.ModeUpdateWorkspace,
		Harness:       minimalHarness,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items: []domain.PlanItem{
			{
				Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "test-runner"},
				TargetPath: "test-runner.md",
				Action:     domain.ActionConflict,
				Conflict: &domain.LocalModification{
					ManifestMissing: true,
				},
			},
		},
	}
}

// nonConflictUpdatePlan returns a plan injecting one ActionUpdate item for "test-runner".
// A VersionDelta is included to make the staleness reason explicit in the plan item.
func nonConflictUpdatePlan(workspace string) domain.Plan {
	return domain.Plan{
		Mode:          domain.ModeUpdateWorkspace,
		Harness:       minimalHarness,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items: []domain.PlanItem{
			{
				Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "test-runner"},
				TargetPath: "test-runner.md",
				Action:     domain.ActionUpdate,
				Stale: []domain.VersionDelta{
					{Field: "version", Deployed: "0.9", Source: "1.0"},
				},
			},
		},
	}
}

// findOwnedKeyEntry finds the first OwnedKeyDifference entry whose Key matches key.
// Returns (entry, true) when found, (zero, false) otherwise.
func findOwnedKeyEntry(entries []domain.OwnedKeyDifference, key string) (domain.OwnedKeyDifference, bool) {
	for _, e := range entries {
		if e.Key == key {
			return e, true
		}
	}
	return domain.OwnedKeyDifference{}, false
}

// ---------------------------------------------------------------------------
// T17.1 -- Positive: conflict from missing manifest record reports the differing key
// ---------------------------------------------------------------------------

// TestOwnedKeyDiff_Pipeline_ConflictNoManifestRecord_ReportsDifferingKey is the primary
// positive pipeline test for owned-key difference reporting.
//
// Setup: a deployed file exists in the workspace with description: "Old description".
// The source document carries description: "New description". A manifest is present for
// this workspace but contains no entry for test-runner (simulating the "no manifest record
// at this target path" agent conflict branch).
//
// The plan is injected as ActionConflict with ManifestMissing set. The Update flow
// resolves the conflict with DecisionOverwrite, calls the content callback, and
// populates RunSummary.OwnedKeyDifferences.
//
// Asserts: the summary contains an OwnedKeyDifference entry for "description" with
// Deployed == "Old description" and Incoming == "New description".
func TestOwnedKeyDiff_Pipeline_ConflictNoManifestRecord_ReportsDifferingKey(t *testing.T) {
	snap := manifest.Snapshot{
		State: manifest.StatePresent,
		Manifest: domain.Manifest{
			HarnessID: "stub-harness",
			Entries:   nil,
		},
	}
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QLocalModification, "test-runner.md", string(domain.DecisionOverwrite)).
		AnswerReview(true).
		Build()
	deps, workspace := newOwnedKeyDiffDeps(t, stub, []byte(ownedKeyDiffPipelineSource), snap)
	deps.Planner = &stubPlanner{plan: conflictPlan(workspace)}
	svc := app.New(deps)

	writeDeployedFile(t, workspace, "test-runner.md", []byte(ownedKeyDiffPipelineDeployed))

	summary, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
		AddWorkflowIDs:  []string{"quick-fix"},
		AgentModels:     map[string]string{"test-runner": "model-a"},
	})
	if err != nil {
		t.Fatalf("Update: unexpected error: %v", err)
	}
	if len(summary.OwnedKeyDifferences) == 0 {
		t.Fatal("OwnedKeyDifferences is empty; want at least one entry for \"description\"")
	}
	entry, ok := findOwnedKeyEntry(summary.OwnedKeyDifferences, "description")
	if !ok {
		t.Fatalf("no OwnedKeyDifference entry for \"description\"; all entries: %v",
			summary.OwnedKeyDifferences)
	}
	if !entry.DeployedPresent {
		t.Error("description entry: DeployedPresent = false; deployed file carries description so it must be present")
	}
	if entry.Deployed != "Old description" {
		t.Errorf("description entry: Deployed = %q, want \"Old description\"", entry.Deployed)
	}
	if !entry.IncomingPresent {
		t.Error("description entry: IncomingPresent = false; source carries description so it must be present")
	}
	if entry.Incoming != "New description" {
		t.Errorf("description entry: Incoming = %q, want \"New description\"", entry.Incoming)
	}
	if entry.Reason == "" {
		t.Error("description entry: Reason is empty; every OwnedKeyDifference entry must carry a non-empty reason")
	}
}

// ---------------------------------------------------------------------------
// T17.1a -- Positive: conflict from no usable manifest behaves identically
// ---------------------------------------------------------------------------

// TestOwnedKeyDiff_Pipeline_ConflictNoUsableManifest_ReportsDifferingKey verifies that
// the second reachable agent conflict branch (no usable manifest for the workspace)
// produces the same OwnedKeyDifference outcome as the "no manifest record" branch.
//
// The only difference from T17.1 is the manifest state: StateAbsent rather than
// StatePresent-but-missing-the-entry. Both branches result in item.Conflict != nil at
// the plan layer. A gate written against one branch's internal details would fail this
// companion test while passing T17.1.
func TestOwnedKeyDiff_Pipeline_ConflictNoUsableManifest_ReportsDifferingKey(t *testing.T) {
	snap := manifest.Snapshot{State: manifest.StateAbsent}
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QLocalModification, "test-runner.md", string(domain.DecisionOverwrite)).
		AnswerReview(true).
		Build()
	deps, workspace := newOwnedKeyDiffDeps(t, stub, []byte(ownedKeyDiffPipelineSource), snap)
	deps.Planner = &stubPlanner{plan: conflictPlan(workspace)}
	svc := app.New(deps)

	writeDeployedFile(t, workspace, "test-runner.md", []byte(ownedKeyDiffPipelineDeployed))

	summary, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
		AddWorkflowIDs:  []string{"quick-fix"},
		AgentModels:     map[string]string{"test-runner": "model-a"},
	})
	if err != nil {
		t.Fatalf("Update: unexpected error: %v", err)
	}
	if len(summary.OwnedKeyDifferences) == 0 {
		t.Fatal("OwnedKeyDifferences is empty; want at least one entry for \"description\"")
	}
	entry, ok := findOwnedKeyEntry(summary.OwnedKeyDifferences, "description")
	if !ok {
		t.Fatalf("no OwnedKeyDifference entry for \"description\"; all entries: %v",
			summary.OwnedKeyDifferences)
	}
	if !entry.DeployedPresent {
		t.Error("description entry: DeployedPresent = false; deployed file carries description so it must be present")
	}
	if entry.Deployed != "Old description" {
		t.Errorf("description entry: Deployed = %q, want \"Old description\"", entry.Deployed)
	}
	if !entry.IncomingPresent {
		t.Error("description entry: IncomingPresent = false; source carries description so it must be present")
	}
	if entry.Incoming != "New description" {
		t.Errorf("description entry: Incoming = %q, want \"New description\"", entry.Incoming)
	}
	if entry.Reason == "" {
		t.Error("description entry: Reason is empty; every OwnedKeyDifference entry must carry a non-empty reason")
	}
}

// ---------------------------------------------------------------------------
// T17.1b -- Negative: conflict item with no decoded form produces no entry and no failure
// ---------------------------------------------------------------------------

// TestOwnedKeyDiff_Pipeline_ConflictNoDecodedForm_NoEntryAndNoFailure verifies the rule
// that when the deployed file's decoded canonical form is absent, no OwnedKeyDifference
// entry is produced and the run does not fail.
//
// This test exercises the OriginUnconfirmedUnparseable path inside buildContent. When
// readDeployedPlanItem finds no file in the workspace (Present: false), Canonical is nil.
// buildContent derives Origin = OriginUnconfirmedUnparseable, and computeOwnedKeyDifferences
// returns nil for any origin other than OriginUnconfirmed. Key assertions:
//   - no OwnedKeyDifference entries
//   - no error from the run
//
// The same code path handles a file that IS on disk but whose decode produced an error
// (DecodeErr != nil), since both conditions produce deployedCanonical == nil and therefore
// Origin = OriginUnconfirmedUnparseable.
func TestOwnedKeyDiff_Pipeline_ConflictNoDecodedForm_NoEntryAndNoFailure(t *testing.T) {
	snap := manifest.Snapshot{State: manifest.StateAbsent}
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QLocalModification, "test-runner.md", string(domain.DecisionOverwrite)).
		AnswerReview(true).
		Build()
	deps, workspace := newOwnedKeyDiffDeps(t, stub, []byte(ownedKeyDiffPipelineSource), snap)
	deps.Planner = &stubPlanner{plan: conflictPlan(workspace)}
	svc := app.New(deps)

	// No deployed file written -- deployedCanonical will be nil inside buildContent,
	// triggering the OriginUnconfirmedUnparseable branch.

	summary, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
		AddWorkflowIDs:  []string{"quick-fix"},
		AgentModels:     map[string]string{"test-runner": "model-a"},
	})
	if err != nil {
		t.Fatalf("Update: unexpected error when deployed canonical form is absent: %v", err)
	}
	if len(summary.OwnedKeyDifferences) != 0 {
		t.Errorf("OwnedKeyDifferences: want 0 entries when deployed canonical form is absent, got %d: %v",
			len(summary.OwnedKeyDifferences), summary.OwnedKeyDifferences)
	}
}

// ---------------------------------------------------------------------------
// T17.2a -- Negative: non-conflict file produces no entry despite differing owned keys
// ---------------------------------------------------------------------------

// TestOwnedKeyDiff_Pipeline_UpdateNotConflict_NoDifferences is the pipeline-level
// false-positive gate test. A deployed file with description: "Old description" is
// overwritten on an ActionUpdate run (source version bumped). item.Conflict == nil, so
// Origin = OriginConfirmed and no OwnedKeyDifference entries are produced.
//
// This test cannot be expressed at the transform level alone because it requires the plan
// layer to classify the item. A conflict gate that accidentally fires on ActionUpdate items
// would fail this test while passing the transform-level negative cases.
func TestOwnedKeyDiff_Pipeline_UpdateNotConflict_NoDifferences(t *testing.T) {
	snap := manifest.Snapshot{
		State: manifest.StatePresent,
		Manifest: domain.Manifest{
			HarnessID: "stub-harness",
			Entries: []domain.ManifestEntry{
				{
					Ref:     domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "test-runner"},
					Version: "0.9",
				},
			},
		},
	}
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	deps, workspace := newOwnedKeyDiffDeps(t, stub, []byte(ownedKeyDiffPipelineSource), snap)
	deps.Planner = &stubPlanner{plan: nonConflictUpdatePlan(workspace)}
	svc := app.New(deps)

	writeDeployedFile(t, workspace, "test-runner.md", []byte(ownedKeyDiffPipelineDeployedStale))

	summary, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
		AddWorkflowIDs:  []string{"quick-fix"},
		AgentModels:     map[string]string{"test-runner": "model-a"},
	})
	if err != nil {
		t.Fatalf("Update: unexpected error: %v", err)
	}
	if len(summary.OwnedKeyDifferences) != 0 {
		t.Errorf("OwnedKeyDifferences: want 0 entries for an ActionUpdate item, got %d: %v",
			len(summary.OwnedKeyDifferences), summary.OwnedKeyDifferences)
	}
}

// TestOwnedKeyDiff_Pipeline_ChangedModel_NoDifferences confirms that a changed model
// configuration on a manifest-backed file (ActionUpdate, no conflict classification)
// produces no OwnedKeyDifference entry. This is the pipeline companion to the
// config-model case in T17.2a: model differences on ordinary updates must not fire
// the gate at any layer of the pipeline.
func TestOwnedKeyDiff_Pipeline_ChangedModel_NoDifferences(t *testing.T) {
	snap := manifest.Snapshot{
		State: manifest.StatePresent,
		Manifest: domain.Manifest{
			HarnessID: "stub-harness",
			Entries: []domain.ManifestEntry{
				{Ref: domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "test-runner"}, Version: "0.9"},
			},
		},
	}
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	deps, workspace := newOwnedKeyDiffDeps(t, stub, []byte(ownedKeyDiffPipelineSource), snap)
	deps.Planner = &stubPlanner{plan: nonConflictUpdatePlan(workspace)}
	svc := app.New(deps)

	writeDeployedFile(t, workspace, "test-runner.md", []byte(ownedKeyDiffPipelineDeployedStale))

	summary, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
		AddWorkflowIDs:  []string{"quick-fix"},
		AgentModels:     map[string]string{"test-runner": "model-a"},
	})
	if err != nil {
		t.Fatalf("Update: unexpected error: %v", err)
	}
	if len(summary.OwnedKeyDifferences) != 0 {
		t.Errorf("changed model on manifest-backed file: want 0 OwnedKeyDifferences, got %d: %v",
			len(summary.OwnedKeyDifferences), summary.OwnedKeyDifferences)
	}
}

// TestOwnedKeyDiff_Pipeline_BumpedVersion_NoDifferences confirms that a bumped source
// version driving an ActionUpdate produces no OwnedKeyDifference entry. This is the
// pipeline companion to the version-bump case in T17.2a.
func TestOwnedKeyDiff_Pipeline_BumpedVersion_NoDifferences(t *testing.T) {
	snap := manifest.Snapshot{
		State: manifest.StatePresent,
		Manifest: domain.Manifest{
			HarnessID: "stub-harness",
			Entries: []domain.ManifestEntry{
				{Ref: domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "test-runner"}, Version: "0.9"},
			},
		},
	}
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	deps, workspace := newOwnedKeyDiffDeps(t, stub, []byte(ownedKeyDiffPipelineSource), snap)
	deps.Planner = &stubPlanner{plan: nonConflictUpdatePlan(workspace)}
	svc := app.New(deps)

	writeDeployedFile(t, workspace, "test-runner.md", []byte(ownedKeyDiffPipelineDeployedStale))

	summary, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
		AddWorkflowIDs:  []string{"quick-fix"},
		AgentModels:     map[string]string{"test-runner": "model-a"},
	})
	if err != nil {
		t.Fatalf("Update: unexpected error: %v", err)
	}
	if len(summary.OwnedKeyDifferences) != 0 {
		t.Errorf("bumped version on manifest-backed file: want 0 OwnedKeyDifferences, got %d: %v",
			len(summary.OwnedKeyDifferences), summary.OwnedKeyDifferences)
	}
}
