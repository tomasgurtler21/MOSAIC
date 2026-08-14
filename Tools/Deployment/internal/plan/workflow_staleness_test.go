package plan_test

// workflow_staleness_test.go covers workflow staleness detection for the orchestrator.
//
// WorkflowStaleness function (T5.1, T5.2):
//   Workflow-set changes:
//   - A workflow added to the selected set marks the orchestrator stale.
//   - A workflow removed from the selected set marks the orchestrator stale.
//   - A reordered but otherwise identical selection does not produce drift.
//   - An empty deployed set with a non-empty selection treats every selected ID as Added.
//   - Both deployed and selected empty produces no drift.
//
//   Per-workflow version changes:
//   - A catalog version bump for a workflow present in both deployed and selected is Changed.
//   - Matching versions on both sides produces no drift.
//   - A deployed block with no version comment (empty string) is treated as stale (Changed).
//   - A deployed version higher than the catalog version (downgrade) is still stale.
//
//   WorkflowDrift.Deltas() field names and ordering (T5.1, T5.2):
//   - Every delta field carries the WorkflowDeltaFieldPrefix so it cannot collide with
//     "version", "transform_version", or "injections_version".
//   - Ordering: added first, then removed, then version-changed.
//   - Added delta: Deployed is empty, Source is the catalog version.
//   - Removed delta: Source is empty, Deployed is the deployed version.
//
// Orchestrator-specific classification (T5.3):
//   - Non-orchestrator agents are never subject to workflow staleness; workflow markers in a
//     worker agent's deployed state are ignored by the planner's classifier.
//   - When the orchestrator has both a version-field mismatch and a workflow set change, the
//     result is a single ActionUpdate item whose Stale slice contains deltas from both sources.
//   - When only the workflow set changed, the orchestrator is still ActionUpdate.
//   - When the workflow set is unchanged and version stamps match, the orchestrator is unchanged.
//
// Reason rendering (T5.4):
//   - Reason() names every workflow responsible for the drift.
//   - Reason() is empty when there is no drift.

import (
	"context"
	"strings"
	"testing"
	"time"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/manifest"
	"mosaic-deploy/internal/plan"
)

// ---------------------------------------------------------------------------
// Helpers for workflow staleness tests
// ---------------------------------------------------------------------------

// makeDeployedWorkflows builds a DeployedWorkflows slice from alternating id/version pairs.
// Panics if the argument count is not even.
func makeDeployedWorkflows(idVersionPairs ...string) domain.DeployedWorkflows {
	if len(idVersionPairs)%2 != 0 {
		panic("makeDeployedWorkflows: id/version pairs must come in pairs")
	}
	out := make(domain.DeployedWorkflows, 0, len(idVersionPairs)/2)
	for i := 0; i < len(idVersionPairs); i += 2 {
		out = append(out, domain.DeployedWorkflow{
			ID:      idVersionPairs[i],
			Version: idVersionPairs[i+1],
		})
	}
	return out
}

// makeSelectedWorkflows builds a []domain.Workflow slice from alternating id/version pairs.
// Panics if the argument count is not even.
func makeSelectedWorkflows(idVersionPairs ...string) []domain.Workflow {
	if len(idVersionPairs)%2 != 0 {
		panic("makeSelectedWorkflows: id/version pairs must come in pairs")
	}
	out := make([]domain.Workflow, 0, len(idVersionPairs)/2)
	for i := 0; i < len(idVersionPairs); i += 2 {
		out = append(out, domain.Workflow{
			ID:      idVersionPairs[i],
			Version: idVersionPairs[i+1],
		})
	}
	return out
}

// ---------------------------------------------------------------------------
// T5.1 — WorkflowStaleness: workflow-set changes
// ---------------------------------------------------------------------------

// TestWorkflowStaleness_WorkflowAdded_Stale verifies that when a workflow is in the newly
// selected set but was not present in the deployed orchestrator, WorkflowStaleness reports
// drift and the new workflow ID appears in WorkflowDrift.Added.
func TestWorkflowStaleness_WorkflowAdded_Stale(t *testing.T) {
	deployed := makeDeployedWorkflows("quick-fix", "1.0")
	selected := makeSelectedWorkflows("quick-fix", "1.0", "greenfield-tdd", "2.0")

	drift := plan.WorkflowStaleness(deployed, selected)

	if !drift.Stale() {
		t.Error("WorkflowStaleness with an added workflow: Stale() = false, want true")
	}
	found := false
	for _, id := range drift.Added {
		if id == "greenfield-tdd" {
			found = true
		}
	}
	if !found {
		t.Errorf("WorkflowDrift.Added = %v; want to contain %q (the newly selected workflow)",
			drift.Added, "greenfield-tdd")
	}
}

// TestWorkflowStaleness_WorkflowRemoved_Stale verifies that when a workflow is in the deployed
// orchestrator but not in the newly selected set, WorkflowStaleness reports drift and the
// removed workflow ID appears in WorkflowDrift.Removed.
func TestWorkflowStaleness_WorkflowRemoved_Stale(t *testing.T) {
	deployed := makeDeployedWorkflows("quick-fix", "1.0", "greenfield-tdd", "2.0")
	selected := makeSelectedWorkflows("quick-fix", "1.0") // greenfield-tdd removed

	drift := plan.WorkflowStaleness(deployed, selected)

	if !drift.Stale() {
		t.Error("WorkflowStaleness with a removed workflow: Stale() = false, want true")
	}
	found := false
	for _, id := range drift.Removed {
		if id == "greenfield-tdd" {
			found = true
		}
	}
	if !found {
		t.Errorf("WorkflowDrift.Removed = %v; want to contain %q (the removed workflow)",
			drift.Removed, "greenfield-tdd")
	}
}

// TestWorkflowStaleness_IdenticalSetDifferentOrder_NotStale verifies that reordering an
// otherwise identical selection does not produce workflow drift. Pure reordering must not
// trigger a plan update.
func TestWorkflowStaleness_IdenticalSetDifferentOrder_NotStale(t *testing.T) {
	deployed := makeDeployedWorkflows("quick-fix", "1.0", "greenfield-tdd", "2.0")
	selected := makeSelectedWorkflows("greenfield-tdd", "2.0", "quick-fix", "1.0") // reversed

	drift := plan.WorkflowStaleness(deployed, selected)

	if drift.Stale() {
		t.Errorf("WorkflowStaleness with reordered identical selection: Stale() = true, want false; "+
			"Added=%v Removed=%v Changed=%v", drift.Added, drift.Removed, drift.Changed)
	}
}

// TestWorkflowStaleness_EmptyDeployed_AllSelectedAreAdded verifies that when the deployed
// orchestrator carries no workflow markers (e.g. a pre-Stage-5 file or a fresh deployment),
// every selected workflow is treated as Added.
func TestWorkflowStaleness_EmptyDeployed_AllSelectedAreAdded(t *testing.T) {
	var deployed domain.DeployedWorkflows // nil — no workflow blocks in the deployed file
	selected := makeSelectedWorkflows("quick-fix", "1.0", "greenfield-tdd", "2.0")

	drift := plan.WorkflowStaleness(deployed, selected)

	if !drift.Stale() {
		t.Error("WorkflowStaleness with empty deployed set: Stale() = false, want true " +
			"(all selected workflows are Added)")
	}
	if len(drift.Added) != 2 {
		t.Errorf("WorkflowDrift.Added = %v (len %d); want both selected workflows to be Added "+
			"when the deployed set is empty", drift.Added, len(drift.Added))
	}
}

// TestWorkflowStaleness_EmptyBothSets_NotStale verifies that when both the deployed and
// selected sets are empty, WorkflowStaleness reports no drift.
func TestWorkflowStaleness_EmptyBothSets_NotStale(t *testing.T) {
	var deployed domain.DeployedWorkflows
	var selected []domain.Workflow

	drift := plan.WorkflowStaleness(deployed, selected)

	if drift.Stale() {
		t.Error("WorkflowStaleness with both sets empty: Stale() = true, want false")
	}
}

// TestWorkflowStaleness_IdenticalSingleWorkflow_NotStale verifies that when both the deployed
// and selected sets contain exactly the same workflow at the same version, there is no drift.
func TestWorkflowStaleness_IdenticalSingleWorkflow_NotStale(t *testing.T) {
	deployed := makeDeployedWorkflows("quick-fix", "1.0")
	selected := makeSelectedWorkflows("quick-fix", "1.0")

	drift := plan.WorkflowStaleness(deployed, selected)

	if drift.Stale() {
		t.Errorf("WorkflowStaleness with identical single-workflow sets: Stale() = true, want false; "+
			"Added=%v Removed=%v Changed=%v", drift.Added, drift.Removed, drift.Changed)
	}
}

// ---------------------------------------------------------------------------
// T5.2 — WorkflowStaleness: per-workflow version changes
// ---------------------------------------------------------------------------

// TestWorkflowStaleness_CatalogVersionBumped_Stale verifies that when a workflow is present
// in both the deployed orchestrator and the new selection, but the catalog's current version
// differs from the deployed version, WorkflowStaleness reports drift. The selection is
// otherwise unchanged.
func TestWorkflowStaleness_CatalogVersionBumped_Stale(t *testing.T) {
	deployed := makeDeployedWorkflows("quick-fix", "1.0") // deployed version: 1.0
	selected := makeSelectedWorkflows("quick-fix", "2.0") // catalog bumped to 2.0

	drift := plan.WorkflowStaleness(deployed, selected)

	if !drift.Stale() {
		t.Error("WorkflowStaleness with catalog version bump: Stale() = false, want true")
	}
	if len(drift.Changed) != 1 {
		t.Fatalf("WorkflowDrift.Changed = %v (len %d); want 1 entry for the version-bumped workflow",
			drift.Changed, len(drift.Changed))
	}
	if drift.Changed[0].ID != "quick-fix" {
		t.Errorf("WorkflowDrift.Changed[0].ID = %q, want %q", drift.Changed[0].ID, "quick-fix")
	}
	if drift.Changed[0].Deployed != "1.0" {
		t.Errorf("WorkflowDrift.Changed[0].Deployed = %q, want %q (value from the deployed file)",
			drift.Changed[0].Deployed, "1.0")
	}
	if drift.Changed[0].Source != "2.0" {
		t.Errorf("WorkflowDrift.Changed[0].Source = %q, want %q (catalog version)",
			drift.Changed[0].Source, "2.0")
	}
}

// TestWorkflowStaleness_MatchingVersions_NotStale verifies that when both the deployed and
// catalog versions for a workflow match, there is no version-change drift.
func TestWorkflowStaleness_MatchingVersions_NotStale(t *testing.T) {
	deployed := makeDeployedWorkflows("quick-fix", "3.0")
	selected := makeSelectedWorkflows("quick-fix", "3.0")

	drift := plan.WorkflowStaleness(deployed, selected)

	if drift.Stale() {
		t.Errorf("WorkflowStaleness with matching versions: Stale() = true, want false; "+
			"Changed=%v", drift.Changed)
	}
}

// TestWorkflowStaleness_DeployedVersionEmpty_Stale verifies that when a workflow block in the
// deployed orchestrator carries no version attribute, the block is treated as stale. An
// unversioned deployed block cannot be proven current.
func TestWorkflowStaleness_DeployedVersionEmpty_Stale(t *testing.T) {
	deployed := makeDeployedWorkflows("quick-fix", "") // no version attribute on the workflow region's open tag
	selected := makeSelectedWorkflows("quick-fix", "1.0")

	drift := plan.WorkflowStaleness(deployed, selected)

	if !drift.Stale() {
		t.Error("WorkflowStaleness with empty deployed version: Stale() = false, want true; " +
			"a missing workflow version attribute must be treated as stale")
	}
	if len(drift.Changed) != 1 {
		t.Fatalf("expected 1 Changed entry for the unversioned deployed block, got %d: %v",
			len(drift.Changed), drift.Changed)
	}
	if drift.Changed[0].Deployed != "" {
		t.Errorf("WorkflowDrift.Changed[0].Deployed = %q, want empty string "+
			"(reflecting the absence of a version attribute on the workflow region)", drift.Changed[0].Deployed)
	}
}

// TestWorkflowStaleness_VersionDowngrade_Stale verifies that direction of a version
// difference is irrelevant: a deployed version that is higher than the catalog version is
// still treated as stale. Any difference between deployed and catalog is a change.
func TestWorkflowStaleness_VersionDowngrade_Stale(t *testing.T) {
	deployed := makeDeployedWorkflows("quick-fix", "3.0") // deployed has a newer version
	selected := makeSelectedWorkflows("quick-fix", "1.0") // catalog rolled back

	drift := plan.WorkflowStaleness(deployed, selected)

	if !drift.Stale() {
		t.Error("WorkflowStaleness with version downgrade: Stale() = false, want true; " +
			"any difference between deployed and catalog version is stale")
	}
}

// ---------------------------------------------------------------------------
// T5.1 / T5.2 — WorkflowDrift.Deltas: field names and ordering
// ---------------------------------------------------------------------------

// TestWorkflowDrift_Deltas_FieldNamesCarryPrefix verifies that every VersionDelta produced by
// WorkflowDrift.Deltas() has its Field value prefixed with WorkflowDeltaFieldPrefix ("workflow:").
// This collision guard prevents workflow deltas from interfering with the executor's
// resolveVersionStamp switch on "version", "transform_version", and "injections_version".
func TestWorkflowDrift_Deltas_FieldNamesCarryPrefix(t *testing.T) {
	deployed := makeDeployedWorkflows("quick-fix", "1.0")
	selected := makeSelectedWorkflows("quick-fix", "2.0", "greenfield-tdd", "1.0") // one changed, one added

	drift := plan.WorkflowStaleness(deployed, selected)
	deltas := drift.Deltas()

	if len(deltas) == 0 {
		t.Fatal("Deltas() returned no deltas for a stale drift; expected at least one")
	}
	for _, d := range deltas {
		if !strings.HasPrefix(d.Field, plan.WorkflowDeltaFieldPrefix) {
			t.Errorf("delta.Field = %q does not start with WorkflowDeltaFieldPrefix %q; "+
				"all workflow deltas must carry the prefix to avoid colliding with the executor's "+
				"version-stamp field names (version, transform_version, injections_version)",
				d.Field, plan.WorkflowDeltaFieldPrefix)
		}
	}
}

// TestWorkflowDrift_Deltas_WorkflowIDEmbeddedInFieldName verifies that each delta's Field
// contains the workflow ID so the executor (and debugging tools) can identify which workflow
// caused the delta.
func TestWorkflowDrift_Deltas_WorkflowIDEmbeddedInFieldName(t *testing.T) {
	deployed := makeDeployedWorkflows("quick-fix", "1.0")
	selected := makeSelectedWorkflows("greenfield-tdd", "2.0") // quick-fix removed, greenfield-tdd added

	drift := plan.WorkflowStaleness(deployed, selected)
	deltas := drift.Deltas()

	if len(deltas) == 0 {
		t.Fatal("Deltas() returned no deltas; expected at least one for the workflow changes")
	}

	hasGreenfield := false
	hasQuickFix := false
	for _, d := range deltas {
		if strings.Contains(d.Field, "greenfield-tdd") {
			hasGreenfield = true
		}
		if strings.Contains(d.Field, "quick-fix") {
			hasQuickFix = true
		}
	}
	if !hasGreenfield {
		t.Errorf("Deltas() = %v; none contain \"greenfield-tdd\"; "+
			"each delta's Field must embed the workflow ID", deltas)
	}
	if !hasQuickFix {
		t.Errorf("Deltas() = %v; none contain \"quick-fix\"; "+
			"each delta's Field must embed the workflow ID", deltas)
	}
}

// TestWorkflowDrift_Deltas_Ordering_AddedRemovedChanged verifies that Deltas() returns deltas
// in the documented order: added first, then removed, then version-changed.
func TestWorkflowDrift_Deltas_Ordering_AddedRemovedChanged(t *testing.T) {
	// deployed: "removed-wf" (v1.0) and "changed-wf" (v1.0)
	// selected: "added-wf"   (v1.0) and "changed-wf"  (v2.0 — version bumped)
	deployed := makeDeployedWorkflows("removed-wf", "1.0", "changed-wf", "1.0")
	selected := makeSelectedWorkflows("added-wf", "1.0", "changed-wf", "2.0")

	drift := plan.WorkflowStaleness(deployed, selected)
	deltas := drift.Deltas()

	// Expect 3 deltas: 1 added, 1 removed, 1 changed
	if len(deltas) != 3 {
		t.Fatalf("Deltas() returned %d deltas; expected 3 (1 added, 1 removed, 1 version-changed)",
			len(deltas))
	}

	// First: added-wf (added) — Deployed must be empty, Source must be the catalog version
	if !strings.Contains(deltas[0].Field, "added-wf") {
		t.Errorf("deltas[0].Field = %q; want it to reference %q (added workflows come first)",
			deltas[0].Field, "added-wf")
	}
	if deltas[0].Deployed != "" {
		t.Errorf("deltas[0].Deployed = %q; want empty string for an added workflow "+
			"(it was not in the deployed file)", deltas[0].Deployed)
	}
	if deltas[0].Source == "" {
		t.Errorf("deltas[0].Source is empty; want the catalog version for the added workflow")
	}

	// Second: removed-wf (removed) — Source must be empty, Deployed must carry the deployed version
	if !strings.Contains(deltas[1].Field, "removed-wf") {
		t.Errorf("deltas[1].Field = %q; want it to reference %q (removed workflows come second)",
			deltas[1].Field, "removed-wf")
	}
	if deltas[1].Source != "" {
		t.Errorf("deltas[1].Source = %q; want empty string for a removed workflow "+
			"(it is not in the new selection)", deltas[1].Source)
	}
	if deltas[1].Deployed == "" {
		t.Errorf("deltas[1].Deployed is empty; want the version from the deployed file " +
			"for the removed workflow")
	}

	// Third: changed-wf (version-changed) — both fields must be non-empty and differ
	if !strings.Contains(deltas[2].Field, "changed-wf") {
		t.Errorf("deltas[2].Field = %q; want it to reference %q (version-changed workflows come last)",
			deltas[2].Field, "changed-wf")
	}
	if deltas[2].Deployed == "" || deltas[2].Source == "" {
		t.Errorf("deltas[2]: Deployed=%q Source=%q; both must be non-empty for a version-changed workflow",
			deltas[2].Deployed, deltas[2].Source)
	}
	if deltas[2].Deployed == deltas[2].Source {
		t.Errorf("deltas[2]: Deployed=%q == Source=%q; must differ for a version-changed workflow",
			deltas[2].Deployed, deltas[2].Source)
	}
}

// TestWorkflowDrift_Deltas_NoStale_EmptyOrNil verifies that when there is no drift,
// Deltas() returns nil or an empty slice (not a non-nil slice with entries).
func TestWorkflowDrift_Deltas_NoStale_EmptyOrNil(t *testing.T) {
	deployed := makeDeployedWorkflows("quick-fix", "1.0")
	selected := makeSelectedWorkflows("quick-fix", "1.0")

	drift := plan.WorkflowStaleness(deployed, selected)
	deltas := drift.Deltas()

	if len(deltas) != 0 {
		t.Errorf("Deltas() for a non-stale drift: got %d deltas, want 0", len(deltas))
	}
}

// ---------------------------------------------------------------------------
// T5.3 — Non-orchestrator agents unaffected; composition with version-field deltas
// ---------------------------------------------------------------------------

// TestBuild_WorkerAgent_NotAffectedByWorkflowStaleness verifies that a worker agent is never
// classified as stale due to workflow markers in its deployed state. Workflow staleness applies
// only to agents with Role == RoleOrchestrator; the classifier must not apply it to workers.
func TestBuild_WorkerAgent_NotAffectedByWorkflowStaleness(t *testing.T) {
	const targetPath = "agents/test-agent.md"
	const hash = "sha256:aabbcc1122"

	agent := makeAgent("test-agent", "1.0")
	// agent.Role = RoleWorker (set by makeAgent)

	manifestEntry := makeManifestEntry(agentRef("test-agent"), targetPath, "1.0", hash)
	manifestEntry.TransformVersion = "1.0"
	manifestEntry.InjectionsVersion = "1.0"

	snap := presentSnapshot(domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "test-harness",
		UpdatedAt:     time.Now(),
		Entries:       []domain.ManifestEntry{manifestEntry},
	})

	// The worker agent's deployed state deliberately includes workflow markers that would
	// be stale if applied — these must be ignored because the agent is not an orchestrator.
	ds := map[string]domain.DeployedArtifactState{
		targetPath: {
			Present:           true,
			ContentHash:       hash,
			Version:           "1.0",
			TransformVersion:  "1.0",
			InjectionsVersion: "1.0",
			Workflows: domain.DeployedWorkflows{
				{ID: "some-workflow", Version: "OLD-VERSION"}, // stale if checked — must be ignored
			},
		},
	}

	input := buildInputWithSingleAgent(agent, snap, ds)

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	item, ok := findItem(p.Items, "test-agent")
	if !ok {
		t.Fatal("plan has no item for test-agent")
	}
	// Worker agent must be ActionUnchanged — workflow markers in its deployed state are irrelevant.
	if item.Action != domain.ActionUnchanged {
		t.Errorf("worker agent with stale workflow markers in deployed state: Action = %q, want %q; "+
			"workflow staleness must only apply to orchestrator-role agents",
			item.Action, domain.ActionUnchanged)
	}
}

// TestBuild_OrchestratorWorkflowOnly_Stale_SingleUpdateItem verifies that when the only
// reason for staleness is a workflow set change (all three version stamps match), the
// orchestrator is classified as a single ActionUpdate item — not ActionUnchanged.
func TestBuild_OrchestratorWorkflowOnly_Stale_SingleUpdateItem(t *testing.T) {
	const orchPath = "agents/orchestrator.md"
	const hash = "sha256:orch0001"

	orch := makeOrchestrator()
	// orch.Version = "1.0"; the deployed file carries "1.0" — no version-field staleness

	manifestEntry := makeManifestEntry(agentRef("orchestrator"), orchPath, "1.0", hash)
	manifestEntry.TransformVersion = "1.0"
	manifestEntry.InjectionsVersion = "1.0"

	snap := presentSnapshot(domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "test-harness",
		UpdatedAt:     time.Now(),
		Entries:       []domain.ManifestEntry{manifestEntry},
	})

	// Deployed orchestrator carries only "quick-fix"; selection expands with "greenfield-tdd".
	ds := map[string]domain.DeployedArtifactState{
		orchPath: {
			Present:           true,
			ContentHash:       hash,
			Version:           "1.0", // matches catalog — no version-field staleness
			TransformVersion:  "1.0",
			InjectionsVersion: "1.0",
			Workflows: domain.DeployedWorkflows{
				{ID: "quick-fix", Version: "1.0"},
			},
		},
	}

	wfQuickFix := makeWorkflow("quick-fix", "orchestrator")
	wfGreenfield := makeWorkflow("greenfield-tdd", "orchestrator")
	cat := &fakeCatalog{
		orchestrator: orch,
		workflows:    []domain.Workflow{wfQuickFix, wfGreenfield},
	}
	module := newFakeModule()
	input := plan.Input{
		Catalog:       cat,
		Module:        module,
		Mode:          domain.ModeUpdate,
		WorkspacePath: "/fake/workspace",
		Scope:         domain.ScopeProject,
		GOOS:          "linux",
		Manifest:      snap,
		WorkflowIDs:   []string{"quick-fix", "greenfield-tdd"}, // greenfield-tdd is newly selected
		Models: map[string]domain.ModelSelection{
			"orchestrator": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
		DeployedState: ds,
	}

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	item, ok := findItem(p.Items, "orchestrator")
	if !ok {
		t.Fatal("plan has no item for orchestrator")
	}
	if item.Action != domain.ActionUpdate {
		t.Errorf("orchestrator with workflow-only staleness: Action = %q, want %q; "+
			"adding a workflow to the selection must classify the orchestrator as ActionUpdate",
			item.Action, domain.ActionUpdate)
	}
}

// TestBuild_OrchestratorWorkflowStaleness_ComposesWithVersionDeltas verifies that when the
// orchestrator has both a version-field mismatch and a workflow set change, the result is a
// single ActionUpdate item whose Stale slice contains deltas from both sources — no splitting
// into separate plan items.
func TestBuild_OrchestratorWorkflowStaleness_ComposesWithVersionDeltas(t *testing.T) {
	const orchPath = "agents/orchestrator.md"
	const hash = "sha256:orch0002"

	// Orchestrator version mismatch: deployed has "1.0", catalog has "2.0"
	orch := makeOrchestrator()
	orch.Version = "2.0"

	manifestEntry := makeManifestEntry(agentRef("orchestrator"), orchPath, "1.0", hash)
	manifestEntry.TransformVersion = "1.0"
	manifestEntry.InjectionsVersion = "1.0"

	snap := presentSnapshot(domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "test-harness",
		UpdatedAt:     time.Now(),
		Entries:       []domain.ManifestEntry{manifestEntry},
	})

	// Deployed: version "1.0" (stale), carries "quick-fix"; selection also adds "greenfield-tdd"
	ds := map[string]domain.DeployedArtifactState{
		orchPath: {
			Present:           true,
			ContentHash:       hash,
			Version:           "1.0", // differs from catalog "2.0" — version-field staleness
			TransformVersion:  "1.0",
			InjectionsVersion: "1.0",
			Workflows: domain.DeployedWorkflows{
				{ID: "quick-fix", Version: "1.0"},
			},
		},
	}

	wfQuickFix := makeWorkflow("quick-fix", "orchestrator")
	wfGreenfield := makeWorkflow("greenfield-tdd", "orchestrator")
	cat := &fakeCatalog{
		orchestrator: orch,
		workflows:    []domain.Workflow{wfQuickFix, wfGreenfield},
	}
	module := newFakeModule()
	input := plan.Input{
		Catalog:       cat,
		Module:        module,
		Mode:          domain.ModeUpdate,
		WorkspacePath: "/fake/workspace",
		Scope:         domain.ScopeProject,
		GOOS:          "linux",
		Manifest:      snap,
		WorkflowIDs:   []string{"quick-fix", "greenfield-tdd"},
		Models: map[string]domain.ModelSelection{
			"orchestrator": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
		DeployedState: ds,
	}

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	item, ok := findItem(p.Items, "orchestrator")
	if !ok {
		t.Fatal("plan has no item for orchestrator")
	}
	if item.Action != domain.ActionUpdate {
		t.Fatalf("orchestrator with version mismatch + workflow addition: Action = %q, want %q",
			item.Action, domain.ActionUpdate)
	}

	// The Stale slice must contain at least one version-field delta and at least one workflow delta.
	// Both staleness sources must compose into this single item — not split across two items.
	hasVersionDelta := false
	hasWorkflowDelta := false
	for _, d := range item.Stale {
		if d.Field == "version" {
			hasVersionDelta = true
		}
		if strings.HasPrefix(d.Field, plan.WorkflowDeltaFieldPrefix) {
			hasWorkflowDelta = true
		}
	}
	if !hasVersionDelta {
		t.Errorf("orchestrator Stale = %v; expected a version-field delta named %q; "+
			"both staleness sources must appear in the single ActionUpdate item",
			item.Stale, "version")
	}
	if !hasWorkflowDelta {
		t.Errorf("orchestrator Stale = %v; expected at least one workflow delta with prefix %q; "+
			"workflow staleness deltas must compose with version-field deltas",
			item.Stale, plan.WorkflowDeltaFieldPrefix)
	}
}

// TestBuild_OrchestratorUnchangedWorkflows_StayUnchanged verifies that when the deployed
// workflow set matches the newly selected set at the same versions, the orchestrator is
// ActionUnchanged (assuming version stamps also match). A reordered selection must produce
// the same result.
func TestBuild_OrchestratorUnchangedWorkflows_StayUnchanged(t *testing.T) {
	const orchPath = "agents/orchestrator.md"
	const hash = "sha256:orch0003"

	orch := makeOrchestrator()

	manifestEntry := makeManifestEntry(agentRef("orchestrator"), orchPath, "1.0", hash)
	manifestEntry.TransformVersion = "1.0"
	manifestEntry.InjectionsVersion = "1.0"

	snap := presentSnapshot(domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "test-harness",
		UpdatedAt:     time.Now(),
		Entries:       []domain.ManifestEntry{manifestEntry},
	})

	// Both deployed and selected contain exactly "quick-fix" at version "1.0".
	ds := map[string]domain.DeployedArtifactState{
		orchPath: {
			Present:           true,
			ContentHash:       hash,
			Version:           "1.0",
			TransformVersion:  "1.0",
			InjectionsVersion: "1.0",
			Workflows: domain.DeployedWorkflows{
				{ID: "quick-fix", Version: "1.0"},
			},
		},
	}

	wf := makeWorkflow("quick-fix", "orchestrator")
	cat := &fakeCatalog{
		orchestrator: orch,
		workflows:    []domain.Workflow{wf},
	}
	module := newFakeModule()
	input := plan.Input{
		Catalog:       cat,
		Module:        module,
		Mode:          domain.ModeUpdate,
		WorkspacePath: "/fake/workspace",
		Scope:         domain.ScopeProject,
		GOOS:          "linux",
		Manifest:      snap,
		WorkflowIDs:   []string{"quick-fix"}, // same as deployed
		Models: map[string]domain.ModelSelection{
			"orchestrator": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
		DeployedState: ds,
	}

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	item, ok := findItem(p.Items, "orchestrator")
	if !ok {
		t.Fatal("plan has no item for orchestrator")
	}
	if item.Action != domain.ActionUnchanged {
		t.Errorf("orchestrator with unchanged workflow set at identical versions: Action = %q, want %q; "+
			"an identical workflow set must not cause staleness",
			item.Action, domain.ActionUnchanged)
	}
}

// TestBuild_WorkflowVersionBump_OrchestratorStale verifies that when a workflow's catalog
// version has been bumped but the selection is otherwise unchanged, the orchestrator is
// classified as ActionUpdate. This covers the case where the user did not change their
// workflow selection but a workflow author released a new version.
func TestBuild_WorkflowVersionBump_OrchestratorStale(t *testing.T) {
	const orchPath = "agents/orchestrator.md"
	const hash = "sha256:orch0004"

	orch := makeOrchestrator()

	manifestEntry := makeManifestEntry(agentRef("orchestrator"), orchPath, "1.0", hash)
	manifestEntry.TransformVersion = "1.0"
	manifestEntry.InjectionsVersion = "1.0"

	snap := presentSnapshot(domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "test-harness",
		UpdatedAt:     time.Now(),
		Entries:       []domain.ManifestEntry{manifestEntry},
	})

	// Deployed: "quick-fix" at version "1.0"; catalog now has "quick-fix" at "2.0"
	ds := map[string]domain.DeployedArtifactState{
		orchPath: {
			Present:           true,
			ContentHash:       hash,
			Version:           "1.0",
			TransformVersion:  "1.0",
			InjectionsVersion: "1.0",
			Workflows: domain.DeployedWorkflows{
				{ID: "quick-fix", Version: "1.0"}, // deployed version
			},
		},
	}

	// Catalog now has quick-fix at version "2.0"
	wf := makeWorkflow("quick-fix", "orchestrator")
	wf.Version = "2.0" // catalog version bumped
	cat := &fakeCatalog{
		orchestrator: orch,
		workflows:    []domain.Workflow{wf},
	}
	module := newFakeModule()
	input := plan.Input{
		Catalog:       cat,
		Module:        module,
		Mode:          domain.ModeUpdate,
		WorkspacePath: "/fake/workspace",
		Scope:         domain.ScopeProject,
		GOOS:          "linux",
		Manifest:      snap,
		WorkflowIDs:   []string{"quick-fix"}, // selection unchanged
		Models: map[string]domain.ModelSelection{
			"orchestrator": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
		DeployedState: ds,
	}

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	item, ok := findItem(p.Items, "orchestrator")
	if !ok {
		t.Fatal("plan has no item for orchestrator")
	}
	if item.Action != domain.ActionUpdate {
		t.Errorf("orchestrator with workflow version bump at unchanged selection: Action = %q, want %q; "+
			"a catalog version bump for a deployed workflow must classify the orchestrator as ActionUpdate",
			item.Action, domain.ActionUpdate)
	}
}

// ---------------------------------------------------------------------------
// T5.4 — Reason text names the responsible workflows
// ---------------------------------------------------------------------------

// TestWorkflowDrift_Reason_NamesAddedWorkflow verifies that when a workflow is added to the
// selection, WorkflowDrift.Reason() includes the added workflow's ID.
func TestWorkflowDrift_Reason_NamesAddedWorkflow(t *testing.T) {
	deployed := makeDeployedWorkflows("quick-fix", "1.0")
	selected := makeSelectedWorkflows("quick-fix", "1.0", "greenfield-tdd", "2.0")

	drift := plan.WorkflowStaleness(deployed, selected)
	reason := drift.Reason()

	if reason == "" {
		t.Fatal("Reason() is empty for a stale drift; expected a non-empty explanation")
	}
	if !strings.Contains(reason, "greenfield-tdd") {
		t.Errorf("Reason() = %q; expected to contain %q (the added workflow ID)",
			reason, "greenfield-tdd")
	}
}

// TestWorkflowDrift_Reason_NamesRemovedWorkflow verifies that when a workflow is removed
// from the selection, WorkflowDrift.Reason() includes the removed workflow's ID.
func TestWorkflowDrift_Reason_NamesRemovedWorkflow(t *testing.T) {
	deployed := makeDeployedWorkflows("quick-fix", "1.0", "greenfield-tdd", "2.0")
	selected := makeSelectedWorkflows("quick-fix", "1.0") // greenfield-tdd removed

	drift := plan.WorkflowStaleness(deployed, selected)
	reason := drift.Reason()

	if reason == "" {
		t.Fatal("Reason() is empty for a stale drift; expected a non-empty explanation")
	}
	if !strings.Contains(reason, "greenfield-tdd") {
		t.Errorf("Reason() = %q; expected to contain %q (the removed workflow ID)",
			reason, "greenfield-tdd")
	}
}

// TestWorkflowDrift_Reason_NamesVersionChangedWorkflow verifies that when a workflow's
// version changes, WorkflowDrift.Reason() includes the workflow's ID. The review screen
// reads this reason to explain why the orchestrator is updating.
func TestWorkflowDrift_Reason_NamesVersionChangedWorkflow(t *testing.T) {
	deployed := makeDeployedWorkflows("quick-fix", "1.0")
	selected := makeSelectedWorkflows("quick-fix", "2.0") // version bumped

	drift := plan.WorkflowStaleness(deployed, selected)
	reason := drift.Reason()

	if reason == "" {
		t.Fatal("Reason() is empty for a stale drift; expected a non-empty explanation")
	}
	if !strings.Contains(reason, "quick-fix") {
		t.Errorf("Reason() = %q; expected to contain %q (the version-changed workflow ID)",
			reason, "quick-fix")
	}
}

// TestBuild_OrchestratorWorkflowRemoved_Stale verifies that removing a workflow from the
// selected set marks the orchestrator as ActionUpdate. This mirrors
// TestBuild_OrchestratorWorkflowOnly_Stale_SingleUpdateItem which covers the addition case;
// both the addition and removal code paths in classifyAgentItem must be exercised.
func TestBuild_OrchestratorWorkflowRemoved_Stale(t *testing.T) {
	const orchPath = "agents/orchestrator.md"
	const hash = "sha256:orch0010"

	orch := makeOrchestrator()
	// orch.Version = "1.0"; deployed file carries "1.0" — no version-field staleness

	manifestEntry := makeManifestEntry(agentRef("orchestrator"), orchPath, "1.0", hash)
	manifestEntry.TransformVersion = "1.0"
	manifestEntry.InjectionsVersion = "1.0"

	snap := presentSnapshot(domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "test-harness",
		UpdatedAt:     time.Now(),
		Entries:       []domain.ManifestEntry{manifestEntry},
	})

	// Deployed orchestrator carries both "quick-fix" and "greenfield-tdd".
	// The new selection removes "greenfield-tdd" — only "quick-fix" is selected.
	ds := map[string]domain.DeployedArtifactState{
		orchPath: {
			Present:           true,
			ContentHash:       hash,
			Version:           "1.0", // matches catalog — no version-field staleness
			TransformVersion:  "1.0",
			InjectionsVersion: "1.0",
			Workflows: domain.DeployedWorkflows{
				{ID: "quick-fix", Version: "1.0"},
				{ID: "greenfield-tdd", Version: "2.0"}, // greenfield-tdd is being removed
			},
		},
	}

	wfQuickFix := makeWorkflow("quick-fix", "orchestrator")
	cat := &fakeCatalog{
		orchestrator: orch,
		workflows:    []domain.Workflow{wfQuickFix},
	}
	module := newFakeModule()
	input := plan.Input{
		Catalog:       cat,
		Module:        module,
		Mode:          domain.ModeUpdate,
		WorkspacePath: "/fake/workspace",
		Scope:         domain.ScopeProject,
		GOOS:          "linux",
		Manifest:      snap,
		WorkflowIDs:   []string{"quick-fix"}, // greenfield-tdd is no longer selected
		Models: map[string]domain.ModelSelection{
			"orchestrator": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
		DeployedState: ds,
	}

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	item, ok := findItem(p.Items, "orchestrator")
	if !ok {
		t.Fatal("plan has no item for orchestrator")
	}
	if item.Action != domain.ActionUpdate {
		t.Errorf("orchestrator with workflow removal: Action = %q, want %q; "+
			"removing a workflow from the selection must classify the orchestrator as ActionUpdate",
			item.Action, domain.ActionUpdate)
	}
}

// TestBuild_Orchestrator_PlanItemReason_NamesWorkflows verifies that after a workflow-driven
// classification, the PlanItem.Reason field contains the workflow ID responsible for the
// staleness. This test closes the gap between WorkflowDrift.Reason() (which is tested at the
// unit level) and build.go's responsibility to write that reason into PlanItem.Reason.
func TestBuild_Orchestrator_PlanItemReason_NamesWorkflows(t *testing.T) {
	const orchPath = "agents/orchestrator.md"
	const hash = "sha256:orch0011"

	orch := makeOrchestrator()

	manifestEntry := makeManifestEntry(agentRef("orchestrator"), orchPath, "1.0", hash)
	manifestEntry.TransformVersion = "1.0"
	manifestEntry.InjectionsVersion = "1.0"

	snap := presentSnapshot(domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "test-harness",
		UpdatedAt:     time.Now(),
		Entries:       []domain.ManifestEntry{manifestEntry},
	})

	// Deployed orchestrator carries "quick-fix"; selection adds "greenfield-tdd".
	// The PlanItem.Reason must contain "greenfield-tdd" after classification.
	ds := map[string]domain.DeployedArtifactState{
		orchPath: {
			Present:           true,
			ContentHash:       hash,
			Version:           "1.0",
			TransformVersion:  "1.0",
			InjectionsVersion: "1.0",
			Workflows: domain.DeployedWorkflows{
				{ID: "quick-fix", Version: "1.0"},
			},
		},
	}

	wfQuickFix := makeWorkflow("quick-fix", "orchestrator")
	wfGreenfield := makeWorkflow("greenfield-tdd", "orchestrator")
	cat := &fakeCatalog{
		orchestrator: orch,
		workflows:    []domain.Workflow{wfQuickFix, wfGreenfield},
	}
	module := newFakeModule()
	input := plan.Input{
		Catalog:       cat,
		Module:        module,
		Mode:          domain.ModeUpdate,
		WorkspacePath: "/fake/workspace",
		Scope:         domain.ScopeProject,
		GOOS:          "linux",
		Manifest:      snap,
		WorkflowIDs:   []string{"quick-fix", "greenfield-tdd"},
		Models: map[string]domain.ModelSelection{
			"orchestrator": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
		DeployedState: ds,
	}

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	item, ok := findItem(p.Items, "orchestrator")
	if !ok {
		t.Fatal("plan has no item for orchestrator")
	}
	if item.Action != domain.ActionUpdate {
		t.Fatalf("orchestrator with workflow addition: Action = %q, want %q",
			item.Action, domain.ActionUpdate)
	}
	if !strings.Contains(item.Reason, "greenfield-tdd") {
		t.Errorf("PlanItem.Reason = %q; expected to contain %q (the added workflow ID); "+
			"build.go must propagate WorkflowDrift.Reason() into PlanItem.Reason",
			item.Reason, "greenfield-tdd")
	}
}

// TestWorkflowDrift_Reason_EmptyWhenNotStale verifies that WorkflowDrift.Reason() returns
// an empty string when there is no drift. An empty reason is the contract for the non-stale case.
func TestWorkflowDrift_Reason_EmptyWhenNotStale(t *testing.T) {
	deployed := makeDeployedWorkflows("quick-fix", "1.0")
	selected := makeSelectedWorkflows("quick-fix", "1.0")

	drift := plan.WorkflowStaleness(deployed, selected)

	if reason := drift.Reason(); reason != "" {
		t.Errorf("Reason() = %q for a non-stale drift; want empty string", reason)
	}
}

// TestWorkflowDrift_Reason_NamesMultipleResponsibleWorkflows verifies that when multiple
// workflows are responsible for staleness, Reason() names all of them (not just the first).
func TestWorkflowDrift_Reason_NamesMultipleResponsibleWorkflows(t *testing.T) {
	// quick-fix removed, greenfield-tdd added, changed-wf version bumped
	deployed := makeDeployedWorkflows("quick-fix", "1.0", "changed-wf", "1.0")
	selected := makeSelectedWorkflows("greenfield-tdd", "2.0", "changed-wf", "2.0")

	drift := plan.WorkflowStaleness(deployed, selected)
	reason := drift.Reason()

	if reason == "" {
		t.Fatal("Reason() is empty for a stale drift with multiple responsible workflows")
	}
	for _, id := range []string{"quick-fix", "greenfield-tdd", "changed-wf"} {
		if !strings.Contains(reason, id) {
			t.Errorf("Reason() = %q; expected to contain %q", reason, id)
		}
	}
}
