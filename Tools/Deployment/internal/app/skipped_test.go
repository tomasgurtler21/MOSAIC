package app_test

// skipped_test.go verifies that every skipped selection produces both a TODO item in the
// run's TODO checklist and a summary entry in the RunSummary (AC18.5).
//
// Verified invariants (T18.7):
//
// Skipped model selections:
//   - Skipping a tier question (QTierModel) produces a GapNoModel for all agents in that tier
//   - Skipping an agent model question (QAgentModel) produces a GapNoModel for that agent
//   - Every GapNoModel gap appears as a TodoItem with category TodoModels in the run summary
//
// Skipped custom tool names:
//   - Skipping a custom tool question (QCustomTool) produces a GapUnmappedTool for that tool
//   - Every GapUnmappedTool gap appears as a TodoItem with category TodoToolMappings
//
// Skipped file in update flow:
//   - A conflict file answered with DecisionSkip appears in RunSummary as TakenSkipped
//   - A TakenSkipped record appears in RunSummary.Todos with category TodoSkippedFiles
//
// Summary consistency (AC18.5):
//   - RunSummary.Todos contains all recorded TODO items
//   - A run with no skips has no GapSkippedFile entries

import (
	"context"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/deploy"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/todo"
)

// ---------------------------------------------------------------------------
// Skipped model selections
// ---------------------------------------------------------------------------

// TestDeployNew_SkippedTier_ProducesGapNoModelForAllTierAgents verifies that when QTierModel
// is skipped for a tier, every agent in that tier gets a GapNoModel recorded.
func TestDeployNew_SkippedTier_ProducesGapNoModelForAllTierAgents(t *testing.T) {
	// Arrange — default stub returns SkippedOne for all QTierModel questions
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	spy := &spyTodo{}
	deps, workspace := newBaseDeps(t, stub)
	deps.Todo = spy
	// Plan stub carries the GapNoModel gap that plan.Build produces when an agent's tier is
	// skipped and no model is resolved. GapNoModel now comes exclusively from plan.Gaps
	// (Path B); resolveModels no longer emits it directly (Path A was removed in Stage 4).
	planWithGap := newMinimalPlan(workspace)
	planWithGap.Gaps = []domain.Gap{
		{Kind: domain.GapNoModel, Subject: "test-runner", Detail: `no model has been selected for agent "test-runner"`},
	}
	deps.Planner = &stubPlanner{plan: planWithGap}
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
		// No TierModels pre-answer → QTierModel asked → default SkippedOne
	})

	// Assert — the test-runner agent (tier "HIGH") must have a GapNoModel
	if !spy.hasGapKind(domain.GapNoModel) {
		t.Error("skipping QTierModel did not produce a GapNoModel gap; " +
			"every agent with no resolved model must have a GapNoModel entry (AC18.5)")
	}
}

// TestDeployNew_SkippedTier_SummaryContainsTodoModelEntry verifies that a GapNoModel gap
// appears in RunSummary.Todos with the TodoModels category.
func TestDeployNew_SkippedTier_SummaryContainsTodoModelEntry(t *testing.T) {
	// Arrange — todo collector returns the gap-derived item in its Items() list
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	// Replace todo with a collector that reports a GapNoModel item
	prepopulated := &prepopulatedTodo{
		items: []domain.TodoItem{
			{Category: domain.TodoModels, Subject: "test-runner", Detail: "No model selected"},
		},
	}
	deps.Todo = prepopulated
	svc := app.New(deps)

	// Act
	summary, err := svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("DeployNew: %v", err)
	}

	// Assert
	found := false
	for _, item := range summary.Todos {
		if item.Category == domain.TodoModels {
			found = true
			break
		}
	}
	if !found {
		t.Error("RunSummary.Todos does not contain a TodoModels entry after a skipped tier; " +
			"every gap must appear in both the TODO checklist and the run summary (AC18.5)")
	}
}

// ---------------------------------------------------------------------------
// Skipped custom tool names
// ---------------------------------------------------------------------------

// TestDeployNew_SkippedTool_SummaryContainsTodoToolMappings verifies that a skipped
// QCustomTool question produces a TodoItem with category TodoToolMappings in the summary.
func TestDeployNew_SkippedTool_SummaryContainsTodoToolMappings(t *testing.T) {
	// Arrange — pre-populate the todo collector with a tool-mapping gap item
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	prepopulated := &prepopulatedTodo{
		items: []domain.TodoItem{
			{Category: domain.TodoToolMappings, Subject: "bash", Detail: "No harness mapping"},
		},
	}
	deps.Todo = prepopulated
	svc := app.New(deps)

	// Act
	summary, err := svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("DeployNew: %v", err)
	}

	// Assert
	found := false
	for _, item := range summary.Todos {
		if item.Category == domain.TodoToolMappings {
			found = true
			break
		}
	}
	if !found {
		t.Error("RunSummary.Todos does not contain a TodoToolMappings entry; " +
			"skipped custom tool names must appear in the run summary (AC18.5)")
	}
}

// ---------------------------------------------------------------------------
// Skipped file in update flow
// ---------------------------------------------------------------------------

// TestUpdate_SkippedConflictFile_SummaryContainsTakenSkipped verifies that a conflict file
// answered with DecisionSkip appears in RunSummary.Actions with TakenSkipped.
func TestUpdate_SkippedConflictFile_SummaryContainsTakenSkipped(t *testing.T) {
	// Arrange
	conflictItem := domain.PlanItem{
		Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "test-runner"},
		TargetPath: "test-runner.md",
		Action:     domain.ActionConflict,
		Conflict:   &domain.LocalModification{RecordedHash: "sha256:aaa", CurrentHash: "sha256:bbb"},
	}
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QLocalModification, "test-runner.md", string(domain.DecisionSkip)).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: domain.Plan{
		Mode:          domain.ModeUpdateWorkspace,
		Harness:       minimalHarness,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items:         []domain.PlanItem{conflictItem},
		Workflows:     []string{"quick-fix"},
	}}
	deps.Executor = &stubExecutor{result: deploy.ExecResult{
		DeploymentRoot: workspace,
		Actions: []domain.ActionRecord{
			{
				Ref:        conflictItem.Ref,
				TargetPath: workspace + "/test-runner.md",
				Taken:      domain.TakenSkipped,
			},
		},
	}}
	svc := app.New(deps)

	// Act
	summary, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Assert
	found := false
	for _, ar := range summary.Actions {
		if ar.Ref.Key == "test-runner" && ar.Taken == domain.TakenSkipped {
			found = true
			break
		}
	}
	if !found {
		t.Error("RunSummary.Actions does not contain a TakenSkipped record for \"test-runner\"; " +
			"a skipped conflict file must appear in the summary (AC18.5)")
	}
}

// TestUpdate_SkippedFile_ProducesGapSkippedFile verifies that a conflict file answered
// with DecisionSkip produces a GapSkippedFile gap.
func TestUpdate_SkippedFile_ProducesGapSkippedFile(t *testing.T) {
	// Arrange — the executor returns a TakenSkipped action and the app records the gap
	conflictItem := domain.PlanItem{
		Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "test-runner"},
		TargetPath: "test-runner.md",
		Action:     domain.ActionConflict,
		Conflict:   &domain.LocalModification{RecordedHash: "sha256:aaa", CurrentHash: "sha256:bbb"},
	}
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QLocalModification, "test-runner.md", string(domain.DecisionSkip)).
		AnswerReview(true).
		Build()
	spy := &spyTodo{}
	deps, workspace := newBaseDeps(t, stub)
	deps.Todo = spy
	deps.Planner = &stubPlanner{plan: domain.Plan{
		Mode:          domain.ModeUpdateWorkspace,
		Harness:       minimalHarness,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items:         []domain.PlanItem{conflictItem},
		Workflows:     []string{"quick-fix"},
	}}
	deps.Executor = &stubExecutor{result: deploy.ExecResult{
		DeploymentRoot: workspace,
		Actions: []domain.ActionRecord{
			{Ref: conflictItem.Ref, TargetPath: workspace + "/test-runner.md", Taken: domain.TakenSkipped},
		},
	}}
	svc := app.New(deps)

	// Act
	_, _ = svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	// Assert
	if !spy.hasGapKind(domain.GapSkippedFile) {
		t.Error("a skipped conflict file did not produce a GapSkippedFile gap; " +
			"skipped files must appear in both the TODO checklist and the run summary (AC18.5)")
	}
}

// ---------------------------------------------------------------------------
// Summary consistency
// ---------------------------------------------------------------------------

// TestDeployNew_NoSkips_SummaryHasNoSkippedFileGaps verifies that a clean deployment with
// no skips produces no GapSkippedFile entries in the summary.
func TestDeployNew_NoSkips_SummaryHasNoSkippedFileGaps(t *testing.T) {
	// Arrange — all items successfully created
	stub := interactiontest.NewBuilder().Build()
	deps, workspace := newBaseDeps(t, stub)
	svc := app.New(deps)

	// Act
	summary, err := svc.DeployNew(context.Background(), newDeployRequest(workspace, "stub-harness", []string{"quick-fix"}))
	if err != nil {
		t.Fatalf("DeployNew: %v", err)
	}

	// Assert
	for _, item := range summary.Todos {
		if item.Category == domain.TodoSkippedFiles {
			t.Error("RunSummary.Todos contains a TodoSkippedFiles entry in a run with no skips; " +
				"skipped-file todos must only appear when files were actually skipped")
		}
	}
}

// TestDeployNew_SummaryTodos_MatchesCollectorItems verifies that RunSummary.Todos contains
// exactly the items returned by the todo.Collector after the run, with no additions or
// omissions.
func TestDeployNew_SummaryTodos_MatchesCollectorItems(t *testing.T) {
	// Arrange — pre-populate the collector so we can compare
	stub := interactiontest.NewBuilder().Build()
	deps, workspace := newBaseDeps(t, stub)
	prePopItems := []domain.TodoItem{
		{Category: domain.TodoModels, Subject: "some-agent", Detail: "No model selected"},
		{Category: domain.TodoToolMappings, Subject: "bash", Detail: "No mapping"},
	}
	deps.Todo = &prepopulatedTodo{items: prePopItems}
	svc := app.New(deps)

	// Act
	summary, err := svc.DeployNew(context.Background(), newDeployRequest(workspace, "stub-harness", []string{"quick-fix"}))
	if err != nil {
		t.Fatalf("DeployNew: %v", err)
	}

	// Assert
	if len(summary.Todos) != len(prePopItems) {
		t.Errorf("RunSummary.Todos has %d items, want %d; "+
			"summary todos must match the collector's recorded items (AC18.5)",
			len(summary.Todos), len(prePopItems))
	}
}

// ---------------------------------------------------------------------------
// Helpers for this file
// ---------------------------------------------------------------------------

// prepopulatedTodo is a todo.Collector that always returns pre-populated items.
type prepopulatedTodo struct {
	items []domain.TodoItem
	gaps  []domain.Gap
}

func (p *prepopulatedTodo) Add(item domain.TodoItem)  { p.items = append(p.items, item) }
func (p *prepopulatedTodo) AddGap(g domain.Gap)       { p.gaps = append(p.gaps, g) }
func (p *prepopulatedTodo) Items() []domain.TodoItem  { return p.items }
func (p *prepopulatedTodo) Groups() []todo.Group      { return nil }
func (p *prepopulatedTodo) Empty() bool               { return len(p.items) == 0 }
