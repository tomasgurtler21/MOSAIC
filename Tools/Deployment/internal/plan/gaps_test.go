package plan_test

// gaps_test.go covers gap surfacing and change explanation records.
//
// Gap surfacing:
//   - An agent with no resolved model produces a GapNoModel gap when its action requires a
//     file write (ActionCreate, or ActionUpdate/ActionConflict without an embedded deployed
//     model). See action_aware_gaps_test.go for the full action × model matrix.
//   - An ActionUnchanged agent never produces a GapNoModel gap — no rewrite occurs.
//   - A generic tool that the harness module reports as ToolUnmapped produces GapUnmappedTool
//   - A hook registration step whose target already exists produces GapHookRegistration
//   - Every unresolved gap appears explicitly in Plan.Gaps; none is silently defaulted
//
// The tests below exercise the ActionCreate scenario for GapNoModel (the simplest case where
// a file will definitely be written and must have a model). The multi-action matrix is covered
// in action_aware_gaps_test.go.
//
// T16.9 — Change explanation records:
//   - Every ActionUpdate item has a non-empty Reason string
//   - Every ActionUpdate item's Stale slice contains all version deltas that drove the update
//   - Every ActionCreate item has a non-empty Reason string
//   - Every ActionConflict item has a non-empty Reason string
//   - ActionUnchanged items have an empty Stale slice and a nil Conflict pointer
//   - The planner provides complete attribution so the logging consumer can describe
//     the change without re-computing staleness independently

import (
	"context"
	"testing"
	"time"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/manifest"
	"mosaic-deploy/internal/plan"
)

// ---------------------------------------------------------------------------
// T16.8 — Gap surfacing: missing model
// ---------------------------------------------------------------------------

// TestBuild_AgentWithNoModel_ProducesGapNoModel verifies that when an agent has no deployed
// file (ActionCreate) and no entry in Input.Models, the plan surfaces a GapNoModel gap with
// Subject equal to the agent key. The new file that will be written must have a model.
func TestBuild_AgentWithNoModel_ProducesGapNoModel(t *testing.T) {
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
		// No model for test-agent — the model map is empty.
		Models: map[string]domain.ModelSelection{
			"orchestrator": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
	}

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	gap, ok := findGap(p.Gaps, domain.GapNoModel)
	if !ok {
		t.Fatal("Plan.Gaps missing GapNoModel; expected a gap for agent with no model selection")
	}
	if gap.Subject != "test-agent" {
		t.Errorf("GapNoModel.Subject = %q, want %q (the key of the agent with no model)",
			gap.Subject, "test-agent")
	}
}

// TestBuild_AgentWithUnresolvedModel_ProducesGapNoModel verifies that an agent with no
// deployed file (ActionCreate) whose ModelSelection has OriginUnresolved (the user skipped
// model selection) also produces a GapNoModel gap. An unresolved model and a missing model
// are both gaps when a file write will occur.
func TestBuild_AgentWithUnresolvedModel_ProducesGapNoModel(t *testing.T) {
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
			// test-agent has OriginUnresolved — no model was selected.
			agent.Key: {
				ModelID: "",
				Origin:  domain.OriginUnresolved,
			},
			"orchestrator": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
	}

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	_, ok := findGap(p.Gaps, domain.GapNoModel)
	if !ok {
		t.Error("Plan.Gaps missing GapNoModel; expected a gap for agent with OriginUnresolved model selection")
	}
}

// TestBuild_AllAgentsHaveResolvedModels_NoGapNoModel verifies that when every agent in the
// artifact set has a resolved model, no GapNoModel appears in Plan.Gaps.
func TestBuild_AllAgentsHaveResolvedModels_NoGapNoModel(t *testing.T) {
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
			agent.Key:    {ModelID: "test-model", Origin: domain.OriginHarnessList},
			"orchestrator": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
	}

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	for _, g := range p.Gaps {
		if g.Kind == domain.GapNoModel {
			t.Errorf("unexpected GapNoModel gap for subject %q; all agents have resolved models", g.Subject)
		}
	}
}

// ---------------------------------------------------------------------------
// T16.8 — Gap surfacing: unmapped generic tool
// ---------------------------------------------------------------------------

// TestBuild_AgentWithUnmappedTool_ProducesGapUnmappedTool verifies that when the harness
// module reports a generic tool as ToolUnmapped, the plan surfaces a GapUnmappedTool gap.
// The user must be told which tool needs a decision before the agent can be deployed.
func TestBuild_AgentWithUnmappedTool_ProducesGapUnmappedTool(t *testing.T) {
	agent := makeAgent("test-agent", "1.0")
	agent.Tools = []string{"bash-shell"} // a tool that the fake module will report as unmapped

	wf := makeWorkflow("test-wf", agent.Key)
	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{agent},
		workflows:    []domain.Workflow{wf},
	}

	module := newFakeModule()
	// Configure the module to report "bash-shell" as unmapped.
	module.ToolsFn = func(req domain.ToolRequest) (domain.ToolResult, error) {
		resolutions := make([]domain.ToolResolution, len(req.Generic))
		for i, g := range req.Generic {
			outcome := domain.ToolMapped
			if g == "bash-shell" {
				outcome = domain.ToolUnmapped
			}
			resolutions[i] = domain.ToolResolution{
				Generic: g,
				Outcome: outcome,
			}
		}
		return domain.ToolResult{Resolutions: resolutions}, nil
	}

	input := plan.Input{
		Catalog:       cat,
		Module:        module,
		Mode:          domain.ModeDeployWorkspace,
		WorkspacePath: "/fake/workspace",
		Scope:         domain.ScopeProject,
		GOOS:          "linux",
		Manifest:      absentSnapshot(),
		WorkflowIDs:   []string{"test-wf"},
		Models: map[string]domain.ModelSelection{
			agent.Key:    {ModelID: "test-model", Origin: domain.OriginHarnessList},
			"orchestrator": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
	}

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	gap, ok := findGap(p.Gaps, domain.GapUnmappedTool)
	if !ok {
		t.Fatal("Plan.Gaps missing GapUnmappedTool; expected a gap for unmapped generic tool")
	}
	if gap.Subject != "bash-shell" {
		t.Errorf("GapUnmappedTool.Subject = %q, want %q (the unmapped generic tool name)",
			gap.Subject, "bash-shell")
	}
}

// ---------------------------------------------------------------------------
// T16.8 — Gap surfacing: hook registration target already exists
// ---------------------------------------------------------------------------

// TestBuild_HookRegistrationTargetExists_ProducesGapHookRegistration verifies that when a
// hook bundle's registration step targets a path that already exists in the workspace,
// the plan surfaces a GapHookRegistration gap. The registration cannot be performed
// without user confirmation, so it must appear as an explicit gap.
//
// WorkspaceFileExists is supplied as an injected function so the test exercises Build
// without real disk I/O. The injector returns true only for the expected registration
// target path, which is what Build checks to decide whether to surface the gap.
func TestBuild_HookRegistrationTargetExists_ProducesGapHookRegistration(t *testing.T) {
	const registrationTarget = "settings.json"

	bundle := makeHookBundle("my-hooks", "1.0")
	bundle.Variants = map[string]domain.HookVariant{
		"test-harness": {
			HarnessID: "test-harness",
			Supported: true,
			Registration: []domain.RegistrationStep{
				{
					ID:          "register-in-settings",
					TargetPath:  registrationTarget, // this path is reported as existing by WorkspaceFileExists
					Fragment:    `"mosaic": true`,
					Performable: true,
					Instruction: "Add the MOSAIC snippet to settings.json",
				},
			},
		},
	}

	agent := makeAgent("test-agent", "1.0")
	wf := makeWorkflow("test-wf", agent.Key)

	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{agent},
		workflows:    []domain.Workflow{wf},
		hooks:        []domain.HookBundle{bundle},
	}

	module := newFakeModule()
	// Configure the module to return a HookPlan that includes the registration step.
	module.HookPlanFn = func(req domain.HookPlanRequest) (domain.HookPlan, error) {
		if req.Bundle.Key == "my-hooks" {
			return domain.HookPlan{
				Supported: true,
				TargetDir: "hooks/my-hooks",
				Registration: []domain.RegistrationStep{
					{
						ID:          "register-in-settings",
						TargetPath:  registrationTarget,
						Fragment:    `"mosaic": true`,
						Performable: true,
						Instruction: "Add the MOSAIC snippet to settings.json",
					},
				},
			}, nil
		}
		return domain.HookPlan{Supported: false, Reason: "unknown bundle"}, nil
	}

	input := plan.Input{
		Catalog:       cat,
		Module:        module,
		Mode:          domain.ModeDeployWorkspace,
		WorkspacePath: "/fake/workspace",
		Scope:         domain.ScopeProject,
		GOOS:          "linux",
		Manifest:      absentSnapshot(),
		WorkflowIDs:   []string{"test-wf"},
		HookIDs:       []string{"my-hooks"},
		Models: map[string]domain.ModelSelection{
			agent.Key:      {ModelID: "test-model", Origin: domain.OriginHarnessList},
			"orchestrator": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
		// Inject the existence check so Build needs no real disk access.
		WorkspaceFileExists: func(relPath string) bool {
			return relPath == registrationTarget
		},
	}

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	_, ok := findGap(p.Gaps, domain.GapHookRegistration)
	if !ok {
		t.Error("Plan.Gaps missing GapHookRegistration; expected a gap because registration target already exists")
	}
}

// ---------------------------------------------------------------------------
// T16.9 — Change explanation records
// ---------------------------------------------------------------------------

// TestBuild_ActionUpdate_ReasonIsNonEmpty verifies that every ActionUpdate plan item has a
// non-empty Reason string. The reason is the primary human-readable explanation used by both
// the TUI plan review screen and the log event (AC16.7, AC15.2).
func TestBuild_ActionUpdate_ReasonIsNonEmpty(t *testing.T) {
	agent := makeAgent("test-agent", "1.1") // version mismatch will trigger ActionUpdate
	wf := makeWorkflow("test-wf", agent.Key)
	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{agent},
		workflows:    []domain.Workflow{wf},
	}

	const agentTarget = "agents/test-agent.md"
	const hash = "sha256:aaaaaa"

	entry := makeManifestEntry(agentRef("test-agent"), agentTarget, "1.0", hash)
	entry.TransformVersion = "1.0"
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
			agent.Key:    {ModelID: "test-model", Origin: domain.OriginHarnessList},
			"orchestrator": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
		// Deployed file present with matching hash; version "1.0" in file vs "1.1" in source → update.
		DeployedState: map[string]domain.DeployedArtifactState{
			agentTarget: deployedState(hash, "1.0", "1.0", "1.0"),
		},
	}

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	for _, item := range p.Items {
		if item.Action == domain.ActionUpdate && item.Reason == "" {
			t.Errorf("ActionUpdate item %v has empty Reason; every update must explain why it is stale",
				item.Ref)
		}
	}
}

// TestBuild_ActionUpdate_StaleFieldsMatchVersionDeltas verifies that the Stale slice of an
// ActionUpdate item contains exactly the version deltas that drove the update, with correct
// Field, Deployed, and Source values. The logging consumer uses these to attribute the change.
func TestBuild_ActionUpdate_StaleFieldsMatchVersionDeltas(t *testing.T) {
	// Agent version 1.1 vs manifest 1.0 → exactly one delta for "version".
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
	entry.TransformVersion = "1.0"
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
			agent.Key:    {ModelID: "test-model", Origin: domain.OriginHarnessList},
			"orchestrator": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
		// Deployed file has version "1.0" in the file; source is "1.1" → one version delta.
		DeployedState: map[string]domain.DeployedArtifactState{
			agentTarget: deployedState(hash, "1.0", "1.0", "1.0"),
		},
	}

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	item, ok := findItem(p.Items, "test-agent")
	if !ok {
		t.Fatal("plan has no item for test-agent")
	}
	if item.Action != domain.ActionUpdate {
		t.Fatalf("test-agent action = %q; expected ActionUpdate for this test to be meaningful", item.Action)
	}
	if len(item.Stale) == 0 {
		t.Fatal("ActionUpdate item has empty Stale slice; expected at least one version delta")
	}
	// The only mismatch is "version": deployed file has "1.0", source is "1.1".
	found := false
	for _, delta := range item.Stale {
		if delta.Field == "version" && delta.Deployed == "1.0" && delta.Source == "1.1" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Stale = %v; expected a delta with Field=version, Deployed=1.0, Source=1.1", item.Stale)
	}
}

// TestBuild_ActionCreate_ReasonIsNonEmpty verifies that every ActionCreate plan item has a
// non-empty Reason string explaining why the artifact is being created.
func TestBuild_ActionCreate_ReasonIsNonEmpty(t *testing.T) {
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
			agent.Key:    {ModelID: "test-model", Origin: domain.OriginHarnessList},
			"orchestrator": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
	}

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	for _, item := range p.Items {
		if item.Action == domain.ActionCreate && item.Reason == "" {
			t.Errorf("ActionCreate item %v has empty Reason; every create must provide a human-readable explanation",
				item.Ref)
		}
	}
}

// TestBuild_ActionConflict_ReasonIsNonEmpty verifies that every ActionConflict plan item
// has a non-empty Reason string. The reason explains why the file is considered locally
// modified and what the user must decide.
func TestBuild_ActionConflict_ReasonIsNonEmpty(t *testing.T) {
	agent := makeAgent("test-agent", "1.0")
	wf := makeWorkflow("test-wf", agent.Key)
	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{agent},
		workflows:    []domain.Workflow{wf},
	}

	const agentTarget = "agents/test-agent.md"

	entry := makeManifestEntry(agentRef("test-agent"), agentTarget, "1.0", "sha256:recorded")
	entry.TransformVersion = "1.0"
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
			agent.Key:    {ModelID: "test-model", Origin: domain.OriginHarnessList},
			"orchestrator": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
		// Deployed file has a hash that differs from the manifest's recorded hash → conflict.
		DeployedState: map[string]domain.DeployedArtifactState{
			agentTarget: deployedState("sha256:locally-modified", "1.0", "1.0", "1.0"),
		},
	}

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	for _, item := range p.Items {
		if item.Action == domain.ActionConflict && item.Reason == "" {
			t.Errorf("ActionConflict item %v has empty Reason; every conflict must provide a human-readable explanation",
				item.Ref)
		}
	}
}

// TestBuild_ActionUnchanged_StaleIsEmpty verifies that ActionUnchanged items have an empty
// Stale slice. A zero staleness result must not carry phantom deltas.
func TestBuild_ActionUnchanged_StaleIsEmpty(t *testing.T) {
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
	entry.TransformVersion = "1.0"
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
			agent.Key:    {ModelID: "test-model", Origin: domain.OriginHarnessList},
			"orchestrator": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
		// Deployed file present with matching hash and all version stamps matching source.
		DeployedState: map[string]domain.DeployedArtifactState{
			agentTarget: deployedState(hash, "1.0", "1.0", "1.0"),
		},
	}

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	for _, item := range p.Items {
		if item.Action == domain.ActionUnchanged && len(item.Stale) != 0 {
			t.Errorf("ActionUnchanged item %v has non-empty Stale = %v; unchanged items must have no version deltas",
				item.Ref, item.Stale)
		}
	}
}

// TestBuild_ActionUnchanged_ConflictIsNil verifies that ActionUnchanged items have a nil
// Conflict pointer. An unchanged item has no local modification.
func TestBuild_ActionUnchanged_ConflictIsNil(t *testing.T) {
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
	entry.TransformVersion = "1.0"
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
			agent.Key:    {ModelID: "test-model", Origin: domain.OriginHarnessList},
			"orchestrator": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
		// Deployed file present with matching hash and version stamps → unchanged (no conflict).
		DeployedState: map[string]domain.DeployedArtifactState{
			agentTarget: deployedState(hash, "1.0", "1.0", "1.0"),
		},
	}

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	for _, item := range p.Items {
		if item.Action == domain.ActionUnchanged && item.Conflict != nil {
			t.Errorf("ActionUnchanged item %v has non-nil Conflict; unchanged items must not have a conflict record",
				item.Ref)
		}
	}
}

// TestBuild_LoggingConsumer_ActionRecordFields verifies that the data the planner puts in
// PlanItem maps to the fields a logging consumer needs via ActionRecord. The plan's Stale
// slice and Reason string are the same fields ActionRecord uses for per-item logging.
//
// Specifically: PlanItem.Stale is the same []VersionDelta type as ActionRecord.Stale.
// A logging consumer building an ActionRecord from a PlanItem can copy Stale directly
// without re-computing it from the manifest.
func TestBuild_LoggingConsumer_ActionRecordFields(t *testing.T) {
	// Verify that VersionDelta values in PlanItem.Stale carry all three fields needed by
	// the logging consumer: Field, Deployed, and Source.
	agent := makeAgent("test-agent", "1.1") // version mismatch
	wf := makeWorkflow("test-wf", agent.Key)
	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{agent},
		workflows:    []domain.Workflow{wf},
	}

	const agentTarget = "agents/test-agent.md"
	const hash = "sha256:aaaaaa"

	entry := makeManifestEntry(agentRef("test-agent"), agentTarget, "1.0", hash)
	entry.TransformVersion = "1.0"
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
			agent.Key:    {ModelID: "test-model", Origin: domain.OriginHarnessList},
			"orchestrator": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
		// Deployed file has version "1.0"; source is "1.1" → version delta fires.
		DeployedState: map[string]domain.DeployedArtifactState{
			agentTarget: deployedState(hash, "1.0", "1.0", "1.0"),
		},
	}

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	item, ok := findItem(p.Items, "test-agent")
	if !ok {
		t.Fatal("plan has no item for test-agent")
	}
	if item.Action != domain.ActionUpdate {
		t.Fatalf("test-agent action = %q; expected ActionUpdate for this test to be meaningful", item.Action)
	}

	for i, delta := range item.Stale {
		if delta.Field == "" {
			t.Errorf("Stale[%d].Field is empty; logging consumers require a non-empty field name", i)
		}
		if delta.Deployed == "" && delta.Source == "" {
			t.Errorf("Stale[%d]: both Deployed and Source are empty; at least one must be non-empty to explain the change", i)
		}
	}
}
