package plan_test

// action_aware_gaps_test.go covers the action-aware GapNoModel emission rules, verifying
// that gap emission depends on the agent's classified action and whether the deployed file
// already carries an embedded model.
//
// Emission rules under test:
//
//   - ActionUnchanged: never emit — the file is not rewritten, its embedded model stands.
//   - ActionCreate:    emit when the incoming model selection is not resolved.
//   - ActionUpdate:    emit only when neither the incoming model is resolved nor the deployed
//     file carries an embedded ModelID.
//   - ActionConflict:  same rule as ActionUpdate.
//
// Surrounding output (GapUnmappedTool, item classification, item ordering) is asserted
// to be unaffected by this logic.

import (
	"context"
	"testing"
	"time"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/manifest"
	"mosaic-deploy/internal/plan"
)

// ---------------------------------------------------------------------------
// ActionUnchanged — gap is always suppressed
// ---------------------------------------------------------------------------

// TestBuild_ActionUnchanged_NoModel_NoGapNoModel verifies that when an agent's file is current
// and will not be rewritten (ActionUnchanged), the plan emits no GapNoModel gap even when the
// agent has no resolved model selection. The deployed file's embedded model stands because no
// rewrite happens.
func TestBuild_ActionUnchanged_NoModel_NoGapNoModel(t *testing.T) {
	agent := makeAgent("test-agent", "1.0")
	wf := makeWorkflow("test-wf", agent.Key)
	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{agent},
		workflows:    []domain.Workflow{wf},
	}

	const agentTarget = "agents/test-agent.md"
	const hash = "sha256:unchanged"

	entry := makeManifestEntry(agentRef("test-agent"), agentTarget, "1.0", hash)
	entry.HarnessVersion = "1.0"
	entry.InjectionsVersion = "1.0"

	snap := presentSnapshot(domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "test-harness",
		UpdatedAt:     time.Now(),
		Entries:       []domain.ManifestEntry{entry},
	})

	input := plan.Input{
		Catalog:       cat,
		Module:        newFakeModule(),
		Mode:          domain.ModeUpdateWorkspace,
		WorkspacePath: "/fake/workspace",
		Scope:         domain.ScopeProject,
		GOOS:          "linux",
		Manifest:      snap,
		WorkflowIDs:   []string{"test-wf"},
		Models: map[string]domain.ModelSelection{
			// test-agent has no model — the gap should be suppressed because the file
			// is current and will not be rewritten.
			"orchestrator": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
		DeployedState: map[string]domain.DeployedArtifactState{
			agentTarget: {
				Present:           true,
				ContentHash:       hash,
				Version:           "1.0",
				HarnessVersion:  "1.0",
				InjectionsVersion: "1.0",
			},
		},
	}

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	// Verify the agent is actually ActionUnchanged so the assertion is meaningful.
	item, ok := findItem(p.Items, "test-agent")
	if !ok {
		t.Fatal("plan has no item for test-agent")
	}
	if item.Action != domain.ActionUnchanged {
		t.Fatalf("test-agent action = %q, want ActionUnchanged; check test setup", item.Action)
	}

	if _, has := findGapForSubject(p.Gaps, domain.GapNoModel, "test-agent"); has {
		t.Error("GapNoModel emitted for ActionUnchanged agent; the gap must be suppressed because no file rewrite occurs and the deployed file's embedded model stands")
	}
}

// ---------------------------------------------------------------------------
// ActionCreate — gap mirrors the existing behaviour (preserved)
// ---------------------------------------------------------------------------

// TestBuild_ActionCreate_NoModel_EmitsGapNoModel verifies that when an agent has no deployed
// file (ActionCreate) and no resolved model selection, the plan surfaces a GapNoModel gap.
// This is the preserved Create behaviour: a new file will be written and must have a model.
func TestBuild_ActionCreate_NoModel_EmitsGapNoModel(t *testing.T) {
	agent := makeAgent("test-agent", "1.0")
	wf := makeWorkflow("test-wf", agent.Key)
	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{agent},
		workflows:    []domain.Workflow{wf},
	}

	input := plan.Input{
		Catalog:       cat,
		Module:        newFakeModule(),
		Mode:          domain.ModeDeployWorkspace,
		WorkspacePath: "/fake/workspace",
		Scope:         domain.ScopeProject,
		GOOS:          "linux",
		Manifest:      absentSnapshot(),
		WorkflowIDs:   []string{"test-wf"},
		Models: map[string]domain.ModelSelection{
			// No model for test-agent.
			"orchestrator": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
		// No DeployedState: absent deployed file → ActionCreate.
	}

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	item, ok := findItem(p.Items, "test-agent")
	if !ok {
		t.Fatal("plan has no item for test-agent")
	}
	if item.Action != domain.ActionCreate {
		t.Fatalf("test-agent action = %q, want ActionCreate; check test setup", item.Action)
	}

	if _, has := findGapForSubject(p.Gaps, domain.GapNoModel, "test-agent"); !has {
		t.Error("GapNoModel missing for ActionCreate agent with no model; a new file will be written and must have a model")
	}
}

// TestBuild_ActionCreate_ResolvedModel_NoGapNoModel verifies that when an agent has no deployed
// file (ActionCreate) but has a resolved model selection, no GapNoModel gap is emitted.
func TestBuild_ActionCreate_ResolvedModel_NoGapNoModel(t *testing.T) {
	agent := makeAgent("test-agent", "1.0")
	wf := makeWorkflow("test-wf", agent.Key)
	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{agent},
		workflows:    []domain.Workflow{wf},
	}

	input := plan.Input{
		Catalog:       cat,
		Module:        newFakeModule(),
		Mode:          domain.ModeDeployWorkspace,
		WorkspacePath: "/fake/workspace",
		Scope:         domain.ScopeProject,
		GOOS:          "linux",
		Manifest:      absentSnapshot(),
		WorkflowIDs:   []string{"test-wf"},
		Models: map[string]domain.ModelSelection{
			agent.Key:      {ModelID: "test-model", Origin: domain.OriginHarnessList},
			"orchestrator": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
	}

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	item, ok := findItem(p.Items, "test-agent")
	if !ok {
		t.Fatal("plan has no item for test-agent")
	}
	if item.Action != domain.ActionCreate {
		t.Fatalf("test-agent action = %q, want ActionCreate; check test setup", item.Action)
	}

	if _, has := findGapForSubject(p.Gaps, domain.GapNoModel, "test-agent"); has {
		t.Error("unexpected GapNoModel for ActionCreate agent with a resolved model")
	}
}

// ---------------------------------------------------------------------------
// ActionUpdate — gap depends on resolved model AND deployed ModelID
// ---------------------------------------------------------------------------

// TestBuild_ActionUpdate_NoModel_NoDeployedModel_EmitsGapNoModel verifies that when an agent's
// deployed file is stale (ActionUpdate), the incoming model selection is unresolved, and the
// deployed file carries no embedded ModelID, the plan surfaces a GapNoModel gap. The
// regenerated file would be written without a model.
func TestBuild_ActionUpdate_NoModel_NoDeployedModel_EmitsGapNoModel(t *testing.T) {
	agent := makeAgent("test-agent", "1.1") // version bump over deployed "1.0" → ActionUpdate
	wf := makeWorkflow("test-wf", agent.Key)
	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{agent},
		workflows:    []domain.Workflow{wf},
	}

	const agentTarget = "agents/test-agent.md"
	const hash = "sha256:aaaaaa"

	entry := makeManifestEntry(agentRef("test-agent"), agentTarget, "1.0", hash)
	entry.HarnessVersion = "1.0"
	entry.InjectionsVersion = "1.0"

	snap := presentSnapshot(domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "test-harness",
		UpdatedAt:     time.Now(),
		Entries:       []domain.ManifestEntry{entry},
	})

	input := plan.Input{
		Catalog:       cat,
		Module:        newFakeModule(),
		Mode:          domain.ModeUpdateWorkspace,
		WorkspacePath: "/fake/workspace",
		Scope:         domain.ScopeProject,
		GOOS:          "linux",
		Manifest:      snap,
		WorkflowIDs:   []string{"test-wf"},
		Models: map[string]domain.ModelSelection{
			// No model for test-agent.
			"orchestrator": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
		DeployedState: map[string]domain.DeployedArtifactState{
			agentTarget: deployedState(hash, "1.0", "1.0", "1.0"), // no ModelID
		},
	}

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	item, ok := findItem(p.Items, "test-agent")
	if !ok {
		t.Fatal("plan has no item for test-agent")
	}
	if item.Action != domain.ActionUpdate {
		t.Fatalf("test-agent action = %q, want ActionUpdate; check test setup", item.Action)
	}

	if _, has := findGapForSubject(p.Gaps, domain.GapNoModel, "test-agent"); !has {
		t.Error("GapNoModel missing for ActionUpdate agent with no incoming model and no deployed ModelID; the regenerated file would lack a model")
	}
}

// TestBuild_ActionUpdate_NoModel_WithDeployedModel_SuppressesGapNoModel verifies that when an
// agent's deployed file is stale (ActionUpdate), the incoming model selection is unresolved,
// but the deployed file carries an embedded ModelID, the plan emits no GapNoModel gap. The
// embedded model will be preserved in the regenerated file.
func TestBuild_ActionUpdate_NoModel_WithDeployedModel_SuppressesGapNoModel(t *testing.T) {
	agent := makeAgent("test-agent", "1.1") // version bump over deployed "1.0" → ActionUpdate
	wf := makeWorkflow("test-wf", agent.Key)
	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{agent},
		workflows:    []domain.Workflow{wf},
	}

	const agentTarget = "agents/test-agent.md"
	const hash = "sha256:aaaaaa"

	entry := makeManifestEntry(agentRef("test-agent"), agentTarget, "1.0", hash)
	entry.HarnessVersion = "1.0"
	entry.InjectionsVersion = "1.0"

	snap := presentSnapshot(domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "test-harness",
		UpdatedAt:     time.Now(),
		Entries:       []domain.ManifestEntry{entry},
	})

	input := plan.Input{
		Catalog:       cat,
		Module:        newFakeModule(),
		Mode:          domain.ModeUpdateWorkspace,
		WorkspacePath: "/fake/workspace",
		Scope:         domain.ScopeProject,
		GOOS:          "linux",
		Manifest:      snap,
		WorkflowIDs:   []string{"test-wf"},
		Models: map[string]domain.ModelSelection{
			// No resolved model for test-agent; the deployed model covers the requirement.
			"orchestrator": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
		DeployedState: map[string]domain.DeployedArtifactState{
			agentTarget: deployedStateWithModel(hash, "1.0", "1.0", "1.0", "claude-sonnet-4-5"),
		},
	}

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	item, ok := findItem(p.Items, "test-agent")
	if !ok {
		t.Fatal("plan has no item for test-agent")
	}
	if item.Action != domain.ActionUpdate {
		t.Fatalf("test-agent action = %q, want ActionUpdate; check test setup", item.Action)
	}

	if _, has := findGapForSubject(p.Gaps, domain.GapNoModel, "test-agent"); has {
		t.Error("GapNoModel emitted for ActionUpdate agent whose deployed file carries a ModelID; the gap must be suppressed because the model will be preserved in the regenerated file")
	}
}

// TestBuild_ActionUpdate_ResolvedModel_NoGapNoModel verifies that when an agent's deployed
// file is stale (ActionUpdate) and the incoming model selection is resolved, no GapNoModel
// gap is emitted, regardless of what the deployed file carries.
func TestBuild_ActionUpdate_ResolvedModel_NoGapNoModel(t *testing.T) {
	agent := makeAgent("test-agent", "1.1")
	wf := makeWorkflow("test-wf", agent.Key)
	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{agent},
		workflows:    []domain.Workflow{wf},
	}

	const agentTarget = "agents/test-agent.md"
	const hash = "sha256:aaaaaa"

	entry := makeManifestEntry(agentRef("test-agent"), agentTarget, "1.0", hash)
	entry.HarnessVersion = "1.0"
	entry.InjectionsVersion = "1.0"

	snap := presentSnapshot(domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "test-harness",
		UpdatedAt:     time.Now(),
		Entries:       []domain.ManifestEntry{entry},
	})

	input := plan.Input{
		Catalog:       cat,
		Module:        newFakeModule(),
		Mode:          domain.ModeUpdateWorkspace,
		WorkspacePath: "/fake/workspace",
		Scope:         domain.ScopeProject,
		GOOS:          "linux",
		Manifest:      snap,
		WorkflowIDs:   []string{"test-wf"},
		Models: map[string]domain.ModelSelection{
			agent.Key:      {ModelID: "test-model", Origin: domain.OriginHarnessList},
			"orchestrator": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
		DeployedState: map[string]domain.DeployedArtifactState{
			agentTarget: deployedState(hash, "1.0", "1.0", "1.0"), // no ModelID; resolved incoming model covers it
		},
	}

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	item, ok := findItem(p.Items, "test-agent")
	if !ok {
		t.Fatal("plan has no item for test-agent")
	}
	if item.Action != domain.ActionUpdate {
		t.Fatalf("test-agent action = %q, want ActionUpdate; check test setup", item.Action)
	}

	if _, has := findGapForSubject(p.Gaps, domain.GapNoModel, "test-agent"); has {
		t.Error("unexpected GapNoModel for ActionUpdate agent with a resolved incoming model")
	}
}

// ---------------------------------------------------------------------------
// ActionConflict — same rules as ActionUpdate
// ---------------------------------------------------------------------------

// TestBuild_ActionConflict_NoModel_NoDeployedModel_EmitsGapNoModel verifies that when an
// agent's file has a local modification (ActionConflict), the incoming model selection is
// unresolved, and the deployed file carries no embedded ModelID, the plan surfaces a
// GapNoModel gap.
//
// ActionConflict is produced here by an FR-4 edge case: the manifest is present but contains
// no entry for the agent. A deployed file with no manifest record classifies as ActionConflict
// with ManifestMissing: true. This mechanism applies both before and after Stage 2 (the Stage 2
// change only removes the hash-mismatch trigger; FR-4 edge cases still produce ActionConflict).
func TestBuild_ActionConflict_NoModel_NoDeployedModel_EmitsGapNoModel(t *testing.T) {
	agent := makeAgent("test-agent", "1.0")
	wf := makeWorkflow("test-wf", agent.Key)
	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{agent},
		workflows:    []domain.Workflow{wf},
	}

	const agentTarget = "agents/test-agent.md"

	// Manifest is present but has NO entry for test-agent. A deployed file with no manifest
	// record → ActionConflict with ManifestMissing: true (FR-4 edge case).
	snap := presentSnapshot(domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "test-harness",
		UpdatedAt:     time.Now(),
		Entries:       nil, // no entry for test-agent
	})

	input := plan.Input{
		Catalog:       cat,
		Module:        newFakeModule(),
		Mode:          domain.ModeUpdateWorkspace,
		WorkspacePath: "/fake/workspace",
		Scope:         domain.ScopeProject,
		GOOS:          "linux",
		Manifest:      snap,
		WorkflowIDs:   []string{"test-wf"},
		Models: map[string]domain.ModelSelection{
			// No model for test-agent.
			"orchestrator": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
		DeployedState: map[string]domain.DeployedArtifactState{
			agentTarget: {
				Present:     true,
				ContentHash: "sha256:current",
				ModelID:     "", // no deployed model
			},
		},
	}

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	item, ok := findItem(p.Items, "test-agent")
	if !ok {
		t.Fatal("plan has no item for test-agent")
	}
	if item.Action != domain.ActionConflict {
		t.Fatalf("test-agent action = %q, want ActionConflict; check test setup", item.Action)
	}

	if _, has := findGapForSubject(p.Gaps, domain.GapNoModel, "test-agent"); !has {
		t.Error("GapNoModel missing for ActionConflict agent with no incoming model and no deployed ModelID; if the conflict is resolved with a rewrite, the file would lack a model")
	}
}

// TestBuild_ActionConflict_NoModel_WithDeployedModel_SuppressesGapNoModel verifies that when
// an agent's file has a local modification (ActionConflict), the incoming model selection is
// unresolved, but the deployed file carries an embedded ModelID, the plan emits no GapNoModel
// gap. The embedded model will be preserved if the conflict is resolved with a rewrite.
//
// ActionConflict is produced here by an FR-4 edge case: the manifest is present but contains
// no entry for the agent. A deployed file with no manifest record classifies as ActionConflict
// with ManifestMissing: true. This mechanism applies both before and after Stage 2 (the Stage 2
// change only removes the hash-mismatch trigger; FR-4 edge cases still produce ActionConflict).
func TestBuild_ActionConflict_NoModel_WithDeployedModel_SuppressesGapNoModel(t *testing.T) {
	agent := makeAgent("test-agent", "1.0")
	wf := makeWorkflow("test-wf", agent.Key)
	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{agent},
		workflows:    []domain.Workflow{wf},
	}

	const agentTarget = "agents/test-agent.md"

	// Manifest is present but has NO entry for test-agent. A deployed file with no manifest
	// record → ActionConflict with ManifestMissing: true (FR-4 edge case). The deployed file
	// carries a ModelID; the gap must be suppressed because the model will be preserved if the
	// conflict is resolved with a rewrite.
	snap := presentSnapshot(domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "test-harness",
		UpdatedAt:     time.Now(),
		Entries:       nil, // no entry for test-agent
	})

	input := plan.Input{
		Catalog:       cat,
		Module:        newFakeModule(),
		Mode:          domain.ModeUpdateWorkspace,
		WorkspacePath: "/fake/workspace",
		Scope:         domain.ScopeProject,
		GOOS:          "linux",
		Manifest:      snap,
		WorkflowIDs:   []string{"test-wf"},
		Models: map[string]domain.ModelSelection{
			// No resolved model for test-agent; the deployed model covers the requirement.
			"orchestrator": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
		DeployedState: map[string]domain.DeployedArtifactState{
			agentTarget: {
				Present:     true,
				ContentHash: "sha256:current",
				ModelID:     "claude-sonnet-4-5", // deployed model present
			},
		},
	}

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	item, ok := findItem(p.Items, "test-agent")
	if !ok {
		t.Fatal("plan has no item for test-agent")
	}
	if item.Action != domain.ActionConflict {
		t.Fatalf("test-agent action = %q, want ActionConflict; check test setup", item.Action)
	}

	if _, has := findGapForSubject(p.Gaps, domain.GapNoModel, "test-agent"); has {
		t.Error("GapNoModel emitted for ActionConflict agent whose deployed file carries a ModelID; the gap must be suppressed because the model will be preserved if the file is rewritten")
	}
}

// ---------------------------------------------------------------------------
// Surrounding output is unaffected by action-aware gap logic
// ---------------------------------------------------------------------------

// TestBuild_ActionAwareGap_UnmappedToolGapsUnaffected verifies that GapUnmappedTool gaps are
// still surfaced when the agent's action is ActionUnchanged. The action-aware gap logic for
// GapNoModel must not interfere with other gap types.
func TestBuild_ActionAwareGap_UnmappedToolGapsUnaffected(t *testing.T) {
	agent := makeAgent("test-agent", "1.0")
	agent.Tools = []string{"bash-shell"} // tool the fake module will report as unmapped
	wf := makeWorkflow("test-wf", agent.Key)
	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{agent},
		workflows:    []domain.Workflow{wf},
	}

	const agentTarget = "agents/test-agent.md"
	const hash = "sha256:unchanged"

	entry := makeManifestEntry(agentRef("test-agent"), agentTarget, "1.0", hash)
	entry.HarnessVersion = "1.0"
	entry.InjectionsVersion = "1.0"

	snap := presentSnapshot(domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "test-harness",
		UpdatedAt:     time.Now(),
		Entries:       []domain.ManifestEntry{entry},
	})

	module := newFakeModule()
	module.ToolsFn = func(req domain.ToolRequest) (domain.ToolResult, error) {
		resolutions := make([]domain.ToolResolution, len(req.Generic))
		for i, g := range req.Generic {
			outcome := domain.ToolMapped
			if g == "bash-shell" {
				outcome = domain.ToolUnmapped
			}
			resolutions[i] = domain.ToolResolution{Generic: g, Outcome: outcome}
		}
		return domain.ToolResult{Resolutions: resolutions}, nil
	}

	input := plan.Input{
		Catalog:       cat,
		Module:        module,
		Mode:          domain.ModeUpdateWorkspace,
		WorkspacePath: "/fake/workspace",
		Scope:         domain.ScopeProject,
		GOOS:          "linux",
		Manifest:      snap,
		WorkflowIDs:   []string{"test-wf"},
		Models: map[string]domain.ModelSelection{
			agent.Key:      {ModelID: "test-model", Origin: domain.OriginHarnessList},
			"orchestrator": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
		DeployedState: map[string]domain.DeployedArtifactState{
			agentTarget: {
				Present:           true,
				ContentHash:       hash,
				Version:           "1.0",
				HarnessVersion:  "1.0",
				InjectionsVersion: "1.0",
			},
		},
	}

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	// Confirm agent is ActionUnchanged so the gap is subject to suppression logic.
	item, ok := findItem(p.Items, "test-agent")
	if !ok {
		t.Fatal("plan has no item for test-agent")
	}
	if item.Action != domain.ActionUnchanged {
		t.Fatalf("test-agent action = %q, want ActionUnchanged; check test setup", item.Action)
	}

	// GapUnmappedTool must still be present.
	gap, has := findGap(p.Gaps, domain.GapUnmappedTool)
	if !has {
		t.Fatal("GapUnmappedTool missing; the action-aware GapNoModel logic must not suppress other gap types")
	}
	if gap.Subject != "bash-shell" {
		t.Errorf("GapUnmappedTool.Subject = %q, want %q", gap.Subject, "bash-shell")
	}
}

// TestBuild_ActionAwareGap_ItemClassificationUnaffected verifies that item actions are
// determined solely by classification, not by the presence or absence of model selections.
// An agent's Action field must not change because gap emission was suppressed or emitted.
func TestBuild_ActionAwareGap_ItemClassificationUnaffected(t *testing.T) {
	// Two worker agents: one with no deployed file (ActionCreate) and one fully current
	// (ActionUnchanged). Neither has an incoming model so both hit the gap decision logic.
	createAgent := makeAgent("create-agent", "1.0")
	unchangedAgent := makeAgent("unchanged-agent", "1.0")
	wf := makeWorkflow("test-wf", createAgent.Key, unchangedAgent.Key)
	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{createAgent, unchangedAgent},
		workflows:    []domain.Workflow{wf},
	}

	const unchangedTarget = "agents/unchanged-agent.md"
	const unchangedHash = "sha256:unchanged"

	entry := makeManifestEntry(agentRef("unchanged-agent"), unchangedTarget, "1.0", unchangedHash)
	entry.HarnessVersion = "1.0"
	entry.InjectionsVersion = "1.0"

	snap := presentSnapshot(domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "test-harness",
		UpdatedAt:     time.Now(),
		Entries:       []domain.ManifestEntry{entry},
	})

	input := plan.Input{
		Catalog:       cat,
		Module:        newFakeModule(),
		Mode:          domain.ModeUpdateWorkspace,
		WorkspacePath: "/fake/workspace",
		Scope:         domain.ScopeProject,
		GOOS:          "linux",
		Manifest:      snap,
		WorkflowIDs:   []string{"test-wf"},
		Models: map[string]domain.ModelSelection{
			// Neither worker agent has a model — both hit the gap decision path.
			"orchestrator": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
		DeployedState: map[string]domain.DeployedArtifactState{
			unchangedTarget: {
				Present:           true,
				ContentHash:       unchangedHash,
				Version:           "1.0",
				HarnessVersion:  "1.0",
				InjectionsVersion: "1.0",
			},
		},
	}

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	createItem, ok := findItem(p.Items, "create-agent")
	if !ok {
		t.Fatal("plan has no item for create-agent")
	}
	if createItem.Action != domain.ActionCreate {
		t.Errorf("create-agent action = %q, want ActionCreate; gap emission must not alter item classification", createItem.Action)
	}

	unchangedItem, ok := findItem(p.Items, "unchanged-agent")
	if !ok {
		t.Fatal("plan has no item for unchanged-agent")
	}
	if unchangedItem.Action != domain.ActionUnchanged {
		t.Errorf("unchanged-agent action = %q, want ActionUnchanged; gap suppression must not alter item classification", unchangedItem.Action)
	}
}

// TestBuild_ActionAwareGap_ItemOrderingUnaffected verifies that the plan item ordering
// (agents first, sorted by key; then skills; then hooks) is unchanged after the action-aware
// gap emission changes. The sort must be driven by kind and key, not by gap presence.
func TestBuild_ActionAwareGap_ItemOrderingUnaffected(t *testing.T) {
	// Two worker agents: alpha has no model (gap emitted), zeta has one. They should still
	// appear in alphabetical order with agents sorted before any other kind.
	alphaAgent := makeAgent("alpha-agent", "1.0")
	zetaAgent := makeAgent("zeta-agent", "1.0")
	wf := makeWorkflow("test-wf", alphaAgent.Key, zetaAgent.Key)
	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{alphaAgent, zetaAgent},
		workflows:    []domain.Workflow{wf},
	}

	input := plan.Input{
		Catalog:       cat,
		Module:        newFakeModule(),
		Mode:          domain.ModeDeployWorkspace,
		WorkspacePath: "/fake/workspace",
		Scope:         domain.ScopeProject,
		GOOS:          "linux",
		Manifest:      absentSnapshot(),
		WorkflowIDs:   []string{"test-wf"},
		Models: map[string]domain.ModelSelection{
			// alpha-agent has no model; zeta-agent and orchestrator have one.
			zetaAgent.Key: {ModelID: "test-model", Origin: domain.OriginHarnessList},
			"orchestrator": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
	}

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	// Collect agent item keys in document order.
	var agentKeys []string
	for _, item := range p.Items {
		if item.Ref.Kind == domain.ArtifactAgent {
			agentKeys = append(agentKeys, item.Ref.Key)
		}
	}

	alphaIdx, zetaIdx := -1, -1
	for i, k := range agentKeys {
		switch k {
		case "alpha-agent":
			alphaIdx = i
		case "zeta-agent":
			zetaIdx = i
		}
	}
	if alphaIdx == -1 || zetaIdx == -1 {
		t.Fatalf("agent item keys = %v; both alpha-agent and zeta-agent must appear", agentKeys)
	}
	if alphaIdx > zetaIdx {
		t.Errorf("agent ordering: alpha-agent (idx %d) appears after zeta-agent (idx %d); agents must be sorted by key regardless of gap emission", alphaIdx, zetaIdx)
	}
}
