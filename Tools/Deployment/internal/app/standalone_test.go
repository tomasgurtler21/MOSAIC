package app_test

// standalone_test.go verifies the DeployStandalone service method.
//
// All tests in this file are RED-phase TDD tests. They assert the full intended behaviour
// of the DeployStandalone method, which does not yet exist (I5.3 is not delivered). Every
// test that calls svc.DeployStandalone will receive a "not yet implemented" error until
// the implementation is delivered.
//
// Verified behaviours:
//
// Question asking (T5.1) — what the mode asks and does not ask:
//   - When StandaloneAgentIDs is nil, QStandaloneAgents is asked through Interaction.
//   - When StandaloneAgentIDs is non-nil, QStandaloneAgents is not asked (pre-answer convention).
//   - QWorkflows is never asked, regardless of which other fields are pre-answered.
//   - QHooks is never asked, regardless of which other fields are pre-answered.
//   - QUtilityAgents is never asked, regardless of which other fields are pre-answered.
//   - QInfrastructureAgents is never asked, regardless of which other fields are pre-answered.
//
// Selection handling (T5.2) — pre-supplied IDs, skip-all, and empty-catalog paths:
//   - Pre-supplied non-nil IDs bypass the question entirely.
//   - SkipAll[QStandaloneAgents] yields empty selection without presenting the question.
//   - An empty standalone catalog skips the question entirely (nothing to offer).
//   - A non-nil empty StandaloneAgentIDs means "none selected, do not ask".
//
// Planned artifact set (T5.3) — what is planned and what is not:
//   - The planner receives Mode = ModeStandaloneOnly, not ModeDeployNew.
//   - The planner receives no WorkflowIDs (empty or nil).
//   - The planner receives no HookIDs (empty or nil).
//   - The planner receives the pre-answered StandaloneAgentIDs.
//   - The RunSummary carries Mode = ModeStandaloneOnly.
//   - With the real planner, the resulting plan contains the selected standalone agents
//     and no workflow-driven agents.
//
// Deployed-state probing (T5.4) — already-deployed agents are not reported as new:
//   - A standalone agent whose file already exists on disk is not classified as ActionCreate
//     on a second run, confirming the probe set is built correctly.

import (
	"context"
	"os"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/deploy"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/plan"
)

// ---------------------------------------------------------------------------
// Fixtures for standalone tests
// ---------------------------------------------------------------------------

// minimalStandaloneAgent is a stub standalone agent used in standalone test scenarios.
var minimalStandaloneAgent = domain.Agent{
	Key:             "session-logger",
	NumericID:       "40",
	Version:         "1.0",
	Name:            "Session Logger",
	Description:     "Logs session events for analysis",
	Role:            domain.RoleStandalone,
	RecommendedTier: "LOW",
}

// minimalSkill is a stub skill used in the skill-inclusion test scenario.
var minimalSkill = domain.Skill{
	Key:         "debug-trace",
	Name:        "Debug Trace",
	Description: "Provides debugging and trace utilities",
	Version:     "1.0",
	EntryFile:   "SKILL.md",
}

// standaloneAgentWithSkill is a standalone agent that declares one required skill. Used to
// verify that the plan produced by DeployStandalone includes required skills in its artifact
// set (AC5.4 skill-inclusion clause).
var standaloneAgentWithSkill = domain.Agent{
	Key:             "trace-logger",
	NumericID:       "41",
	Version:         "1.0",
	Name:            "Trace Logger",
	Description:     "Logs execution traces using the debug-trace skill",
	Role:            domain.RoleStandalone,
	RecommendedTier: "LOW",
	RequiredSkills:  []string{"debug-trace"},
}

// catalogWithStandaloneAgents returns a catalog with one standalone agent in addition to
// the standard minimal catalog contents.
func catalogWithStandaloneAgents() *stubCatalog {
	cat := newMinimalCatalog()
	cat.standaloneAgents = []domain.Agent{minimalStandaloneAgent}
	return cat
}

// catalogWithStandaloneAgentAndSkill returns a catalog with a standalone agent that
// requires a skill, plus the skill itself. Used for skill-inclusion tests.
func catalogWithStandaloneAgentAndSkill() *stubCatalog {
	cat := newMinimalCatalog()
	cat.standaloneAgents = []domain.Agent{standaloneAgentWithSkill}
	cat.skills = []domain.Skill{minimalSkill}
	return cat
}

// standalonePlan returns a domain.Plan shaped as expected from DeployStandalone:
// ModeStandaloneOnly, no workflow agents, no hooks.
func standalonePlan(workspace string) domain.Plan {
	return domain.Plan{
		Mode:          domain.ModeStandaloneOnly,
		Harness:       minimalHarness,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items: []domain.PlanItem{
			{
				Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "session-logger"},
				TargetPath: "session-logger.md",
				Action:     domain.ActionCreate,
			},
		},
	}
}

// newStandaloneRequest returns a minimal StandaloneRequest for headless testing.
// StandaloneAgentIDs is set to an empty non-nil slice ("none, don't ask").
func newStandaloneRequest(workspace, harnessID string) app.StandaloneRequest {
	return app.StandaloneRequest{
		HarnessID:          harnessID,
		WorkspacePath:      workspace,
		Scope:              domain.ScopeProject,
		StandaloneAgentIDs: []string{},
		AutoConfirmPlan:    true,
	}
}

// ---------------------------------------------------------------------------
// T5.1 — QStandaloneAgents is asked when StandaloneAgentIDs is nil
// ---------------------------------------------------------------------------

// TestDeployStandalone_StandaloneAgentIDsNil_QStandaloneAgentsAsked verifies that when
// StandaloneAgentIDs is nil, the flow asks QStandaloneAgents through the Interaction port
// so the user can select which standalone agents to deploy (CD-6 pre-answer convention).
func TestDeployStandalone_StandaloneAgentIDsNil_QStandaloneAgentsAsked(t *testing.T) {
	// Arrange — StandaloneAgentIDs is nil, so QStandaloneAgents must be asked.
	stub := interactiontest.NewBuilder().
		AnswerSelectMany(domain.QStandaloneAgents, "", []string{}).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithStandaloneAgents()
	deps.Planner = &stubPlanner{plan: standalonePlan(workspace)}
	svc := app.New(deps)

	// Act — StandaloneAgentIDs is nil: the flow must ask QStandaloneAgents.
	_, _ = svc.DeployStandalone(context.Background(), app.StandaloneRequest{
		HarnessID:          "stub-harness",
		WorkspacePath:      workspace,
		StandaloneAgentIDs: nil, // nil → must ask
		AutoConfirmPlan:    true,
	})

	// Assert — QStandaloneAgents must be asked when StandaloneAgentIDs is nil.
	if !stub.WasAsked(domain.QStandaloneAgents, "") {
		t.Error("QStandaloneAgents was not asked even though StandaloneAgentIDs was nil; " +
			"DeployStandalone must ask QStandaloneAgents through the Interaction port " +
			"when the field is not pre-answered (CD-6 pre-answer convention)")
	}
}

// ---------------------------------------------------------------------------
// T5.1 — QStandaloneAgents is NOT asked when StandaloneAgentIDs is pre-answered
// ---------------------------------------------------------------------------

// TestDeployStandalone_StandaloneAgentIDsPreAnswered_QStandaloneAgentsNotAsked verifies
// that when StandaloneAgentIDs is non-nil, the flow does not ask QStandaloneAgents (CD-6).
func TestDeployStandalone_StandaloneAgentIDsPreAnswered_QStandaloneAgentsNotAsked(t *testing.T) {
	// Arrange — StandaloneAgentIDs is non-nil (pre-answered).
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithStandaloneAgents()
	deps.Planner = &stubPlanner{plan: standalonePlan(workspace)}
	svc := app.New(deps)

	// Act — pre-answered StandaloneAgentIDs: QStandaloneAgents must not be asked.
	_, err := svc.DeployStandalone(context.Background(), app.StandaloneRequest{
		HarnessID:          "stub-harness",
		WorkspacePath:      workspace,
		StandaloneAgentIDs: []string{"session-logger"}, // non-nil: must not ask
		AutoConfirmPlan:    true,
	})

	// Assert — the flow must complete without error when StandaloneAgentIDs is pre-answered.
	if err != nil {
		t.Fatalf("DeployStandalone returned error %v; "+
			"the flow must complete without error when StandaloneAgentIDs is pre-answered — "+
			"once I5.3 is implemented this test will both gate on err==nil and assert the "+
			"negative question-asking invariant", err)
	}
	// Assert — QStandaloneAgents must not be asked when StandaloneAgentIDs is pre-answered.
	if stub.WasAsked(domain.QStandaloneAgents, "") {
		t.Error("QStandaloneAgents was asked even though StandaloneAgentIDs was pre-answered (non-nil); " +
			"DeployStandalone must use the pre-answered selection directly " +
			"without asking (CD-6 pre-answer convention)")
	}
}

// ---------------------------------------------------------------------------
// T5.1 — QWorkflows is NEVER asked
// ---------------------------------------------------------------------------

// TestDeployStandalone_QWorkflowsNeverAsked verifies that QWorkflows is never asked by
// DeployStandalone, regardless of request field state. Workflows are outside the scope of
// this mode.
func TestDeployStandalone_QWorkflowsNeverAsked(t *testing.T) {
	// Arrange — StandaloneAgentIDs is nil so all questions would be triggered if present.
	stub := interactiontest.NewBuilder().
		AnswerSelectMany(domain.QStandaloneAgents, "", []string{}).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithStandaloneAgents()
	deps.Planner = &stubPlanner{plan: standalonePlan(workspace)}
	svc := app.New(deps)

	// Act — nil StandaloneAgentIDs so the flow asks everything it needs; QWorkflows must
	// not be one of them.
	_, err := svc.DeployStandalone(context.Background(), app.StandaloneRequest{
		HarnessID:          "stub-harness",
		WorkspacePath:      workspace,
		StandaloneAgentIDs: nil,
		AutoConfirmPlan:    true,
	})

	// Assert — the flow must complete without error when QStandaloneAgents is scripted.
	if err != nil {
		t.Fatalf("DeployStandalone returned error %v; "+
			"the flow must complete without error when all required questions are scripted", err)
	}
	// Assert — QWorkflows must never appear in the question sequence for this mode.
	if stub.WasAsked(domain.QWorkflows, "") {
		t.Error("QWorkflows was asked; DeployStandalone must never ask about workflows — " +
			"this mode deploys only standalone agents and has no workflow concept; " +
			"the resolver sequence must not include the workflows resolver")
	}
}

// ---------------------------------------------------------------------------
// T5.1 — QHooks is NEVER asked
// ---------------------------------------------------------------------------

// TestDeployStandalone_QHooksNeverAsked verifies that QHooks is never asked by
// DeployStandalone. Hook configuration is outside the scope of this mode.
func TestDeployStandalone_QHooksNeverAsked(t *testing.T) {
	// Arrange — StandaloneAgentIDs is nil so the flow asks everything relevant.
	stub := interactiontest.NewBuilder().
		AnswerSelectMany(domain.QStandaloneAgents, "", []string{}).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithStandaloneAgents()
	deps.Planner = &stubPlanner{plan: standalonePlan(workspace)}
	svc := app.New(deps)

	// Act — nil StandaloneAgentIDs; hook question must still be absent.
	_, err := svc.DeployStandalone(context.Background(), app.StandaloneRequest{
		HarnessID:          "stub-harness",
		WorkspacePath:      workspace,
		StandaloneAgentIDs: nil,
		AutoConfirmPlan:    true,
	})

	// Assert — the flow must complete without error when QStandaloneAgents is scripted.
	if err != nil {
		t.Fatalf("DeployStandalone returned error %v; "+
			"the flow must complete without error when all required questions are scripted", err)
	}
	// Assert — QHooks must never appear in the question sequence for this mode.
	if stub.WasAsked(domain.QHooks, "") {
		t.Error("QHooks was asked; DeployStandalone must never ask about hooks — " +
			"hook configuration is outside the scope of this mode; " +
			"the resolver sequence must not include the hooks resolver")
	}
}

// ---------------------------------------------------------------------------
// T5.1 — QUtilityAgents is NEVER asked
// ---------------------------------------------------------------------------

// TestDeployStandalone_QUtilityAgentsNeverAsked verifies that QUtilityAgents is never asked
// by DeployStandalone. Utility agents are outside the scope of this mode.
func TestDeployStandalone_QUtilityAgentsNeverAsked(t *testing.T) {
	// Arrange — StandaloneAgentIDs is nil so any question the flow triggers will be recorded.
	stub := interactiontest.NewBuilder().
		AnswerSelectMany(domain.QStandaloneAgents, "", []string{}).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithStandaloneAgents()
	deps.Planner = &stubPlanner{plan: standalonePlan(workspace)}
	svc := app.New(deps)

	// Act
	_, err := svc.DeployStandalone(context.Background(), app.StandaloneRequest{
		HarnessID:          "stub-harness",
		WorkspacePath:      workspace,
		StandaloneAgentIDs: nil,
		AutoConfirmPlan:    true,
	})

	// Assert — the flow must complete without error when QStandaloneAgents is scripted.
	if err != nil {
		t.Fatalf("DeployStandalone returned error %v; "+
			"the flow must complete without error when all required questions are scripted", err)
	}
	// Assert — QUtilityAgents must never appear for this mode.
	if stub.WasAsked(domain.QUtilityAgents, "") {
		t.Error("QUtilityAgents was asked; DeployStandalone must never ask about utility agents — " +
			"this mode is standalone-only; the resolver sequence must not include the utility-agent resolver")
	}
}

// ---------------------------------------------------------------------------
// T5.1 — QInfrastructureAgents is NEVER asked
// ---------------------------------------------------------------------------

// TestDeployStandalone_QInfrastructureAgentsNeverAsked verifies that QInfrastructureAgents
// is never asked by DeployStandalone. Infrastructure agents are outside the scope of this mode.
func TestDeployStandalone_QInfrastructureAgentsNeverAsked(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().
		AnswerSelectMany(domain.QStandaloneAgents, "", []string{}).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithStandaloneAgents()
	deps.Planner = &stubPlanner{plan: standalonePlan(workspace)}
	svc := app.New(deps)

	// Act
	_, err := svc.DeployStandalone(context.Background(), app.StandaloneRequest{
		HarnessID:          "stub-harness",
		WorkspacePath:      workspace,
		StandaloneAgentIDs: nil,
		AutoConfirmPlan:    true,
	})

	// Assert — the flow must complete without error when QStandaloneAgents is scripted.
	if err != nil {
		t.Fatalf("DeployStandalone returned error %v; "+
			"the flow must complete without error when all required questions are scripted", err)
	}
	// Assert — QInfrastructureAgents must never appear for this mode.
	if stub.WasAsked(domain.QInfrastructureAgents, "") {
		t.Error("QInfrastructureAgents was asked; DeployStandalone must never ask about infrastructure agents — " +
			"this mode is standalone-only; the resolver sequence must not include the infrastructure-agent resolver")
	}
}

// ---------------------------------------------------------------------------
// T5.2 — SkipAll[QStandaloneAgents] suppresses the question
// ---------------------------------------------------------------------------

// TestDeployStandalone_SkipAllStandaloneAgents_NoQuestionAsked verifies that when
// SkipAll[QStandaloneAgents] is true and StandaloneAgentIDs is nil, the flow yields an
// empty selection without presenting the question to the user (skip-all convention).
func TestDeployStandalone_SkipAllStandaloneAgents_NoQuestionAsked(t *testing.T) {
	// Arrange — no answer scripted for QStandaloneAgents; SkipAll must prevent the ask.
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithStandaloneAgents()
	deps.Planner = &stubPlanner{plan: standalonePlan(workspace)}
	svc := app.New(deps)

	// Act — StandaloneAgentIDs nil but SkipAll suppresses the question.
	_, err := svc.DeployStandalone(context.Background(), app.StandaloneRequest{
		HarnessID:          "stub-harness",
		WorkspacePath:      workspace,
		StandaloneAgentIDs: nil,
		SkipAll:            map[domain.QuestionID]bool{domain.QStandaloneAgents: true},
		AutoConfirmPlan:    true,
	})

	// Assert — the flow must complete without error when SkipAll suppresses the only question.
	if err != nil {
		t.Fatalf("DeployStandalone returned error %v; "+
			"the flow must complete without error when SkipAll suppresses the standalone agent question", err)
	}
	// Assert — QStandaloneAgents must not be asked when skipped via SkipAll.
	if stub.WasAsked(domain.QStandaloneAgents, "") {
		t.Error("QStandaloneAgents was asked even though SkipAll[QStandaloneAgents] was true; " +
			"the skip-all gate must suppress the question and yield an empty selection " +
			"without presenting QStandaloneAgents through the Interaction port")
	}
}

// ---------------------------------------------------------------------------
// T5.2 — Empty standalone catalog skips the question entirely
// ---------------------------------------------------------------------------

// TestDeployStandalone_EmptyStandaloneCatalog_NoQuestionAsked verifies that when the catalog
// has no standalone agents, the flow does not present QStandaloneAgents (there is nothing to
// offer, so asking would be a no-op).
func TestDeployStandalone_EmptyStandaloneCatalog_NoQuestionAsked(t *testing.T) {
	// Arrange — catalog with zero standalone agents.
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	// Minimal catalog has no standalone agents by default.
	deps.Catalog = newMinimalCatalog()
	deps.Planner = &stubPlanner{plan: standalonePlan(workspace)}
	svc := app.New(deps)

	// Act — StandaloneAgentIDs nil; but the catalog is empty so the question must be skipped.
	_, err := svc.DeployStandalone(context.Background(), app.StandaloneRequest{
		HarnessID:          "stub-harness",
		WorkspacePath:      workspace,
		StandaloneAgentIDs: nil,
		AutoConfirmPlan:    true,
	})

	// Assert — the flow must complete without error when the catalog is empty.
	if err != nil {
		t.Fatalf("DeployStandalone returned error %v; "+
			"the flow must complete without error when the standalone catalog is empty", err)
	}
	// Assert — QStandaloneAgents must not be asked when the catalog has no standalone agents.
	if stub.WasAsked(domain.QStandaloneAgents, "") {
		t.Error("QStandaloneAgents was asked even though the catalog has no standalone agents; " +
			"DeployStandalone must skip the question when there are no agents to offer, " +
			"matching the same empty-catalog gate used by DeployUtilityInfrastructure")
	}
}

// ---------------------------------------------------------------------------
// T5.2 — Non-nil empty StandaloneAgentIDs means "none selected, do not ask"
// ---------------------------------------------------------------------------

// TestDeployStandalone_NonNilEmptyStandaloneAgentIDs_NoQuestionAsked verifies that when
// StandaloneAgentIDs is an explicitly empty non-nil slice, the flow does not ask the question.
// This is the CD-6 "do not ask" sentinel: non-nil means pre-answered, empty means zero selection.
func TestDeployStandalone_NonNilEmptyStandaloneAgentIDs_NoQuestionAsked(t *testing.T) {
	// Arrange — explicitly empty slice, not nil.
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithStandaloneAgents()
	deps.Planner = &stubPlanner{plan: standalonePlan(workspace)}
	svc := app.New(deps)

	// Act — empty non-nil slice: pre-answered as "none".
	_, err := svc.DeployStandalone(context.Background(), app.StandaloneRequest{
		HarnessID:          "stub-harness",
		WorkspacePath:      workspace,
		StandaloneAgentIDs: []string{}, // non-nil empty → "none, do not ask"
		AutoConfirmPlan:    true,
	})

	// Assert — the flow must complete without error for a non-nil empty pre-answer.
	if err != nil {
		t.Fatalf("DeployStandalone returned error %v; "+
			"the flow must complete without error when StandaloneAgentIDs is a non-nil empty slice", err)
	}
	// Assert — QStandaloneAgents must not be asked for a non-nil empty pre-answer.
	if stub.WasAsked(domain.QStandaloneAgents, "") {
		t.Error("QStandaloneAgents was asked even though StandaloneAgentIDs was a non-nil empty slice; " +
			"a non-nil StandaloneAgentIDs always means pre-answered (CD-6 convention), " +
			"including the empty-slice case which means 'deploy no standalone agents'")
	}
}

// ---------------------------------------------------------------------------
// T5.3 — Planner receives Mode = ModeStandaloneOnly
// ---------------------------------------------------------------------------

// TestDeployStandalone_PlannerInput_ModeIsStandaloneOnly verifies that the planner receives
// Mode = ModeStandaloneOnly in its Input, so the planner produces a plan scoped to standalone
// agents — not a full deploy-new plan.
func TestDeployStandalone_PlannerInput_ModeIsStandaloneOnly(t *testing.T) {
	// Arrange — capturing planner records the Input it receives.
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithStandaloneAgents()
	capPlan := &capturingPlanner{result: standalonePlan(workspace)}
	deps.Planner = capPlan
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployStandalone(context.Background(), newStandaloneRequest(workspace, "stub-harness"))

	// Assert — planner must have been called.
	if capPlan.capturedInput == nil {
		t.Fatal("Planner.Build was not called; DeployStandalone must call the planner " +
			"to compute a deployment plan")
	}
	if capPlan.capturedInput.Mode != domain.ModeStandaloneOnly {
		t.Errorf("Planner.Build received Mode = %q, want %q; "+
			"DeployStandalone must pass ModeStandaloneOnly to the planner "+
			"so the planner knows to scope the artifact set to standalone agents only",
			capPlan.capturedInput.Mode, domain.ModeStandaloneOnly)
	}
}

// ---------------------------------------------------------------------------
// T5.3 — Planner receives no WorkflowIDs
// ---------------------------------------------------------------------------

// TestDeployStandalone_PlannerInput_WorkflowIDsIsEmpty verifies that the planner receives
// an empty or nil WorkflowIDs slice. This ensures no workflow-driven agent or orchestrator
// rewrite is planned.
func TestDeployStandalone_PlannerInput_WorkflowIDsIsEmpty(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithStandaloneAgents()
	capPlan := &capturingPlanner{result: standalonePlan(workspace)}
	deps.Planner = capPlan
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployStandalone(context.Background(), newStandaloneRequest(workspace, "stub-harness"))

	// Assert
	if capPlan.capturedInput == nil {
		t.Fatal("Planner.Build was not called; DeployStandalone must call the planner")
	}
	if len(capPlan.capturedInput.WorkflowIDs) > 0 {
		t.Errorf("Planner.Build received WorkflowIDs = %v; want empty; "+
			"DeployStandalone must pass no workflow IDs to the planner — "+
			"workflows are outside the scope of this mode and must not cause workflow agents or "+
			"orchestrator rewrites to be planned",
			capPlan.capturedInput.WorkflowIDs)
	}
}

// ---------------------------------------------------------------------------
// T5.3 — Planner receives no HookIDs
// ---------------------------------------------------------------------------

// TestDeployStandalone_PlannerInput_HookIDsIsEmpty verifies that the planner receives an
// empty or nil HookIDs slice. Hook artifacts must never be planned in this mode.
func TestDeployStandalone_PlannerInput_HookIDsIsEmpty(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithStandaloneAgents()
	capPlan := &capturingPlanner{result: standalonePlan(workspace)}
	deps.Planner = capPlan
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployStandalone(context.Background(), newStandaloneRequest(workspace, "stub-harness"))

	// Assert
	if capPlan.capturedInput == nil {
		t.Fatal("Planner.Build was not called; DeployStandalone must call the planner")
	}
	if len(capPlan.capturedInput.HookIDs) > 0 {
		t.Errorf("Planner.Build received HookIDs = %v; want empty; "+
			"DeployStandalone must not pass any hook IDs to the planner — "+
			"hooks are outside the scope of this mode",
			capPlan.capturedInput.HookIDs)
	}
}

// ---------------------------------------------------------------------------
// T5.3 — Planner receives the pre-answered StandaloneAgentIDs
// ---------------------------------------------------------------------------

// TestDeployStandalone_PlannerInput_IncludesPreAnsweredStandaloneAgents verifies that when
// StandaloneAgentIDs is pre-answered, the planner receives those IDs. The planner uses them
// to determine which standalone agents and their required skills to include in the plan.
func TestDeployStandalone_PlannerInput_IncludesPreAnsweredStandaloneAgents(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithStandaloneAgents()
	capPlan := &capturingPlanner{result: standalonePlan(workspace)}
	deps.Planner = capPlan
	svc := app.New(deps)

	// Act — pre-answer standalone agent selection.
	_, _ = svc.DeployStandalone(context.Background(), app.StandaloneRequest{
		HarnessID:          "stub-harness",
		WorkspacePath:      workspace,
		StandaloneAgentIDs: []string{"session-logger"}, // pre-answered
		AutoConfirmPlan:    true,
	})

	// Assert — planner must have received "session-logger" in its StandaloneAgentIDs.
	if capPlan.capturedInput == nil {
		t.Fatal("Planner.Build was not called; DeployStandalone must call the planner")
	}
	found := false
	for _, id := range capPlan.capturedInput.StandaloneAgentIDs {
		if id == "session-logger" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Planner.Build StandaloneAgentIDs = %v; want to contain \"session-logger\"; "+
			"the pre-answered StandaloneAgentIDs must be threaded through to the planner input "+
			"as StandaloneAgentIDs, not reused through an existing parameter such as UtilityAgentIDs "+
			"(AC5.5: standalone agents must resolve as their own class)",
			capPlan.capturedInput.StandaloneAgentIDs)
	}
}

// ---------------------------------------------------------------------------
// T5.3 — Standalone agents must NOT appear in UtilityAgentIDs (AC5.5)
// ---------------------------------------------------------------------------

// TestDeployStandalone_PlannerInput_StandaloneAgentsNotInUtilityAgentIDs verifies that
// standalone agents are NOT passed through UtilityAgentIDs. AC5.5 requires standalone
// agents to be resolved as their own class; reusing the utility parameter is forbidden.
func TestDeployStandalone_PlannerInput_StandaloneAgentsNotInUtilityAgentIDs(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithStandaloneAgents()
	capPlan := &capturingPlanner{result: standalonePlan(workspace)}
	deps.Planner = capPlan
	svc := app.New(deps)

	// Act — pre-answer with a specific standalone agent key.
	_, _ = svc.DeployStandalone(context.Background(), app.StandaloneRequest{
		HarnessID:          "stub-harness",
		WorkspacePath:      workspace,
		StandaloneAgentIDs: []string{"session-logger"},
		AutoConfirmPlan:    true,
	})

	// Assert — "session-logger" must not appear in UtilityAgentIDs.
	if capPlan.capturedInput == nil {
		t.Fatal("Planner.Build was not called; DeployStandalone must call the planner")
	}
	for _, id := range capPlan.capturedInput.UtilityAgentIDs {
		if id == "session-logger" {
			t.Errorf("Planner.Build UtilityAgentIDs = %v contains standalone agent \"session-logger\"; "+
				"standalone agents must be passed via StandaloneAgentIDs, not UtilityAgentIDs — "+
				"reusing the utility parameter silently merges two distinct agent classes (AC5.5)",
				capPlan.capturedInput.UtilityAgentIDs)
		}
	}
}

// ---------------------------------------------------------------------------
// T5.3 — RunSummary.Mode is ModeStandaloneOnly
// ---------------------------------------------------------------------------

// TestDeployStandalone_RunSummaryModeIsStandaloneOnly verifies that the RunSummary returned
// by DeployStandalone carries ModeStandaloneOnly. This distinguishes the mode in run records,
// the todo file metadata, and the CLI output.
func TestDeployStandalone_RunSummaryModeIsStandaloneOnly(t *testing.T) {
	// Arrange — stub planner returns a standalone plan with ModeStandaloneOnly.
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithStandaloneAgents()
	deps.Planner = &stubPlanner{plan: standalonePlan(workspace)}
	svc := app.New(deps)

	// Act
	summary, err := svc.DeployStandalone(context.Background(), newStandaloneRequest(workspace, "stub-harness"))

	// Assert — a nil error means the flow ran; check the mode.
	if err != nil {
		t.Fatalf("DeployStandalone returned error %v; "+
			"the flow must complete successfully when all required fields are pre-answered", err)
	}
	if summary.Mode != domain.ModeStandaloneOnly {
		t.Errorf("RunSummary.Mode = %q, want %q; "+
			"DeployStandalone must report ModeStandaloneOnly in the summary "+
			"so the mode is distinguishable from DeployNew in run records",
			summary.Mode, domain.ModeStandaloneOnly)
	}
}

// ---------------------------------------------------------------------------
// T5.3 (real-planner) — Planned artifact set excludes workflow-driven agents
// ---------------------------------------------------------------------------

// TestDeployStandalone_RealPlanner_PlanExcludesWorkflowDrivenAgents verifies that, with
// the real planner wired in, DeployStandalone produces a plan that contains the selected
// standalone agents and does NOT contain any workflow-driven agent. This is the direct
// regression test for the risk: "Reusing the deploy-new flow silently pulls in
// workflow-driven agents."
//
// Using a stub planner (as the other T5.3 tests do) only asserts which values are passed
// INTO Build; this test asserts what comes OUT of the real planner so that an
// implementation that accidentally passes non-empty WorkflowIDs to the planner is caught
// by the output-side check.
func TestDeployStandalone_RealPlanner_PlanExcludesWorkflowDrivenAgents(t *testing.T) {
	// Arrange — real planner; capturing executor records the computed plan without writing.
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithStandaloneAgents()
	capExec := &planCapturingExecutor{result: newMinimalExecResult(workspace)}
	deps.Planner = plan.New()
	deps.Executor = capExec
	svc := app.New(deps)

	// Act — pre-answer all selections and skip model questions so the flow runs headlessly.
	// "session-logger" is the standalone agent.
	// "test-runner" is a workflow-driven agent referenced by minimalWorkflow — it must not appear.
	_, err := svc.DeployStandalone(context.Background(), app.StandaloneRequest{
		HarnessID:          "stub-harness",
		WorkspacePath:      workspace,
		Scope:              domain.ScopeProject,
		StandaloneAgentIDs: []string{"session-logger"},
		SkipAll: map[domain.QuestionID]bool{
			domain.QTierModel:  true,
			domain.QAgentModel: true,
		},
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("DeployStandalone returned error %v; "+
			"with pre-answered selections the flow must complete successfully", err)
	}

	// Assert — executor must have been called (the full planning pipeline must have run).
	if capExec.capturedPlan == nil {
		t.Fatal("executor was not called; DeployStandalone must invoke the executor " +
			"via the planner — this test uses the real planner to exercise the output-side invariant")
	}

	// Assert — plan must include the selected standalone agent.
	foundStandalone := false
	for _, item := range capExec.capturedPlan.Items {
		if item.Ref.Kind == domain.ArtifactAgent && item.Ref.Key == "session-logger" {
			foundStandalone = true
		}
	}
	if !foundStandalone {
		t.Error("plan does not contain standalone agent \"session-logger\"; " +
			"DeployStandalone must plan the selected standalone agents")
	}

	// Assert — plan must NOT include any workflow-driven agents.
	// Derive excluded agent keys from minimalWorkflow.ReferencedAgents so the assertion
	// stays correct if the fixture's agent list changes.
	workflowAgentKeys := make(map[string]bool, len(minimalWorkflow.ReferencedAgents))
	for _, key := range minimalWorkflow.ReferencedAgents {
		workflowAgentKeys[key] = true
	}
	for _, item := range capExec.capturedPlan.Items {
		if item.Ref.Kind == domain.ArtifactAgent && workflowAgentKeys[item.Ref.Key] {
			t.Errorf("plan contains workflow-driven agent %q; "+
				"DeployStandalone must not pass any workflow IDs to the planner — "+
				"if WorkflowIDs is non-empty, the planner will include every workflow-referenced "+
				"agent; ensure WorkflowIDs is nil or empty in the planner input for this mode",
				item.Ref.Key)
		}
	}
}

// ---------------------------------------------------------------------------
// T5.3 (real-planner, skill inclusion) — Required skills appear in the plan
// ---------------------------------------------------------------------------

// TestDeployStandalone_RealPlanner_SkillForSelectedAgentInPlan verifies that when a
// selected standalone agent declares required skills, those skills appear as ArtifactSkill
// items in the plan produced by DeployStandalone. This directly covers AC5.4's
// skill-inclusion clause ("the resulting plan contains exactly the selected standalone
// agents plus the skills they require") and exercises the skill-resolution path through
// ResolveArtifactsFrom (or equivalent) for the standalone agent class.
//
// Without this test, the skill-resolution code path is entirely unexercised for standalone
// agents: minimalStandaloneAgent declares no required skills, so no existing test ever
// reaches the loop in ResolveArtifacts that calls c.Skill(key) for standalone agents.
func TestDeployStandalone_RealPlanner_SkillForSelectedAgentInPlan(t *testing.T) {
	// Arrange — real planner + capturing executor; catalog includes a standalone agent with
	// a required skill and the skill itself.
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithStandaloneAgentAndSkill()
	capExec := &planCapturingExecutor{result: newMinimalExecResult(workspace)}
	deps.Planner = plan.New()
	deps.Executor = capExec
	svc := app.New(deps)

	// Act — select the standalone agent that requires the "debug-trace" skill.
	_, err := svc.DeployStandalone(context.Background(), app.StandaloneRequest{
		HarnessID:          "stub-harness",
		WorkspacePath:      workspace,
		Scope:              domain.ScopeProject,
		StandaloneAgentIDs: []string{"trace-logger"},
		SkipAll: map[domain.QuestionID]bool{
			domain.QTierModel:  true,
			domain.QAgentModel: true,
		},
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("DeployStandalone returned error %v; "+
			"the flow must complete successfully when all inputs are pre-answered", err)
	}

	// Assert — executor must have been called so there is a plan to inspect.
	if capExec.capturedPlan == nil {
		t.Fatal("executor was not called; DeployStandalone must invoke the executor — " +
			"without this no plan was produced and the skill-inclusion check cannot run")
	}

	// Assert — plan must contain the selected standalone agent.
	foundAgent := false
	for _, item := range capExec.capturedPlan.Items {
		if item.Ref.Kind == domain.ArtifactAgent && item.Ref.Key == "trace-logger" {
			foundAgent = true
		}
	}
	if !foundAgent {
		t.Error("plan does not contain standalone agent \"trace-logger\"; " +
			"DeployStandalone must plan the selected standalone agents")
	}

	// Assert — plan must contain the required skill as an ArtifactSkill item.
	// This is the direct regression test for AC5.4's skill-inclusion clause: if
	// standalone agents are not entered into the artifact resolver's skill-resolution
	// loop, the skill will be missing even when the agent is present.
	foundSkill := false
	for _, item := range capExec.capturedPlan.Items {
		if item.Ref.Kind == domain.ArtifactSkill && item.Ref.Key == "debug-trace" {
			foundSkill = true
		}
	}
	if !foundSkill {
		t.Error("plan does not contain a skill item for \"debug-trace\" even though " +
			"the selected standalone agent \"trace-logger\" declares it as a required skill; " +
			"DeployStandalone must resolve standalone agents through the artifact resolver so " +
			"that required skills are included in the plan — AC5.4 requires the plan to hold " +
			"the selected agents AND their required skills; if standalone agents bypass the " +
			"skill-resolution loop in ResolveArtifacts, skills will never appear")
	}
}

// ---------------------------------------------------------------------------
// T5.4 — Already-deployed standalone agent is not reported as new on a second run
// ---------------------------------------------------------------------------

// TestDeployStandalone_RealPlanner_AlreadyDeployedAgentNotReportedAsNew verifies that when
// a standalone agent's target file already exists in the workspace, a second call to
// DeployStandalone classifies that agent as ActionUnchanged (or ActionUpdate if stale), never
// ActionCreate. This confirms that the probe set is built correctly for standalone agents so
// that the deployed-state map is populated before the planner builds the plan.
//
// Without a correct probe set, the planner's DeployedState map misses the standalone agent
// entry, causing every standalone agent to classify as ActionCreate on every run after the
// first — the same defect Stage 3 fixed for infrastructure agents and hook bundles.
func TestDeployStandalone_RealPlanner_AlreadyDeployedAgentNotReportedAsNew(t *testing.T) {
	// Arrange — real planner + capturing executor.
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithStandaloneAgents()
	capExec := &planCapturingExecutor{result: newMinimalExecResult(workspace)}
	deps.Planner = plan.New()
	deps.Executor = capExec
	svc := app.New(deps)

	// Simulate a previously-deployed standalone agent by writing a file at the target path.
	// The file carries a version stamp so the planner can produce ActionUnchanged rather than
	// ActionCreate. The version matches the source agent's version ("1.0").
	agentContent := []byte("---\nversion: 1.0\n---\n# Session Logger\n")
	agentTargetPath := workspace + "/session-logger.md"
	if err := os.WriteFile(agentTargetPath, agentContent, 0o644); err != nil {
		t.Fatalf("setup: could not write pre-deployed agent file: %v", err)
	}

	// Act — second run; the agent is already on disk so it must not be ActionCreate.
	_, err := svc.DeployStandalone(context.Background(), app.StandaloneRequest{
		HarnessID:          "stub-harness",
		WorkspacePath:      workspace,
		Scope:              domain.ScopeProject,
		StandaloneAgentIDs: []string{"session-logger"},
		SkipAll: map[domain.QuestionID]bool{
			domain.QTierModel:  true,
			domain.QAgentModel: true,
		},
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("DeployStandalone returned error %v; "+
			"the flow must complete successfully when the standalone agent file already exists", err)
	}

	// Assert — executor must have been called.
	if capExec.capturedPlan == nil {
		t.Fatal("executor was not called; DeployStandalone must invoke the executor; " +
			"the probe-set construction must include standalone agents so their deployed state is " +
			"read and the planner can classify them as non-new")
	}

	// Assert — the standalone agent must NOT be ActionCreate on the second run.
	// If it is ActionCreate, the probe set did not include the standalone agent and the
	// planner had no knowledge of the existing on-disk file.
	for _, item := range capExec.capturedPlan.Items {
		if item.Ref.Kind == domain.ArtifactAgent && item.Ref.Key == "session-logger" {
			if item.Action == domain.ActionCreate {
				t.Errorf("standalone agent \"session-logger\" was classified as ActionCreate even though "+
					"its file already exists at %q; "+
					"DeployStandalone must build the probe set to include standalone agents so that "+
					"plan.EnumerateTargetPaths and probeDeployedStateWithIndex detect their on-disk state — "+
					"without this, every run after the first reports all standalone agents as new "+
					"(the same defect Stage 3 fixed for infrastructure agents)",
					agentTargetPath)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// T5.3 — DryRun field is honoured
// ---------------------------------------------------------------------------

// TestDeployStandalone_DryRun_ExecutorCalledWithDryRunTrue verifies that when DryRun is
// true, the executor receives an ExecRequest with DryRun=true so that no files are written.
// This matches the DryRun contract established by DeployNew and DeployUtilityInfrastructure.
func TestDeployStandalone_DryRun_ExecutorCalledWithDryRunTrue(t *testing.T) {
	// Arrange — use a purpose-built executor that records DryRun.
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithStandaloneAgents()
	deps.Planner = &stubPlanner{plan: standalonePlan(workspace)}
	dryExec := &dryRunRecordingExecutor{result: newMinimalExecResult(workspace)}
	deps.Executor = dryExec
	svc := app.New(deps)

	// Act — DryRun = true.
	_, err := svc.DeployStandalone(context.Background(), app.StandaloneRequest{
		HarnessID:          "stub-harness",
		WorkspacePath:      workspace,
		StandaloneAgentIDs: []string{},
		DryRun:             true,
		AutoConfirmPlan:    true,
	})

	// Assert — dry-run must succeed without aborting.
	if err != nil {
		t.Fatalf("DeployStandalone DryRun=true returned error %v; "+
			"dry-run must compute the plan and return a summary without writing files", err)
	}
	// Assert — the executor must be called (DryRun uses simulateAction, not a no-op).
	if !dryExec.called {
		t.Fatal("executor was not called; DeployStandalone must call the executor even " +
			"for DryRun=true so the plan is simulated and a meaningful summary is returned — " +
			"the executor's DryRun flag suppresses writes, not the call itself")
	}
	// Assert — the executor must receive DryRun=true so it skips all file writes.
	if !dryExec.capturedDryRun {
		t.Error("executor was called but ExecRequest.DryRun was false; " +
			"DeployStandalone must thread DryRun=true from its request into ExecRequest.DryRun — " +
			"without this the executor will write files even when the caller requested a dry-run; " +
			"this matches the same DryRun threading contract as DeployNew and DeployUtilityInfrastructure")
	}
}

// ---------------------------------------------------------------------------
// compile-time interface check
// ---------------------------------------------------------------------------

// Ensure domain constants exist and are the expected types (compile-time guard).
var (
	_ domain.RunMode   = domain.ModeStandaloneOnly
	_ domain.QuestionID = domain.QStandaloneAgents
	_ deploy.Executor  = (*dryRunRecordingExecutor)(nil) // ensure the type is still usable
)
