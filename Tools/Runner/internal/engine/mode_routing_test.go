package engine_test

import (
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/engine"
)

// ===== Mode-gated routing (Stage 2) =====

// TestNext_Mode_Auto_Success_RoutesNormally verifies that in auto mode a SUCCESS
// response routes to the On Success target exactly as before.
func TestNext_Mode_Auto_Success_RoutesNormally(t *testing.T) {
	// quick-fix row 0: planner-tdd-soft, OnSuccess="plan-review"
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet("Implementation-Only")
	agents := newTestAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")
	state := stateAfter("PLANNING", "", "planner-tdd-soft#1", domain.StatusSUCCESS, 1)

	dec := engine.Next(engine.NextInput{
		Workflow:     aw,
		Stages:       stages,
		State:        state,
		LastResponse: successResponse("planner-tdd-soft#1"),
		Agents:       agents,
		Seq:          1,
		Now:          fixedNow,
		Mode:         domain.ExecutionModeAuto,
	})

	step := requireDispatch(t, dec)
	if agentName(step.Request.AgentInstanceID) != "plan-review" {
		t.Errorf("auto mode SUCCESS: want On Success route to plan-review, got %s",
			step.Request.AgentInstanceID)
	}
}

// TestNext_Mode_AutoReview_Success_RoutesNormally verifies that in auto-review
// mode SUCCESS routing is identical to auto — the auto-review extension applies
// only to CNA with an unambiguous On Findings hint.
func TestNext_Mode_AutoReview_Success_RoutesNormally(t *testing.T) {
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet("Implementation-Only")
	agents := newTestAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")
	state := stateAfter("PLANNING", "", "planner-tdd-soft#1", domain.StatusSUCCESS, 1)

	dec := engine.Next(engine.NextInput{
		Workflow:     aw,
		Stages:       stages,
		State:        state,
		LastResponse: successResponse("planner-tdd-soft#1"),
		Agents:       agents,
		Seq:          1,
		Now:          fixedNow,
		Mode:         domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	if agentName(step.Request.AgentInstanceID) != "plan-review" {
		t.Errorf("auto-review mode SUCCESS: want On Success route to plan-review, got %s",
			step.Request.AgentInstanceID)
	}
}

// TestNext_Mode_Orchestrated_FirstCall_ReturnsConsultDecision verifies that in
// orchestrated mode the engine returns a ConsultDecision on the very first call
// (before any agent has run), with trigger ConsultTriggerOrchestratedMode.
func TestNext_Mode_Orchestrated_FirstCall_ReturnsConsultDecision(t *testing.T) {
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet("Implementation-Only")
	agents := newTestAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")

	dec := engine.Next(engine.NextInput{
		Workflow:     aw,
		Stages:       stages,
		State:        emptyState(),
		LastResponse: nil,
		Agents:       agents,
		Seq:          0,
		Now:          fixedNow,
		Mode:         domain.ExecutionModeOrchestrated,
	})

	consult := requireConsult(t, dec)
	if consult.Trigger != domain.ConsultTriggerOrchestratedMode {
		t.Errorf("orchestrated mode first call: want trigger ConsultTriggerOrchestratedMode, got %q",
			consult.Trigger)
	}
	// First call: CurrentRow must be -1 (no row dispatched yet).
	if consult.CurrentRow != -1 {
		t.Errorf("orchestrated mode first call: want CurrentRow=-1 (no row yet), got %d",
			consult.CurrentRow)
	}
}

// TestNext_Mode_Orchestrated_AfterSuccess_ReturnsConsultDecision verifies that
// in orchestrated mode every call returns a ConsultDecision, not just the first.
// Even a SUCCESS response does not produce a routing decision from the engine.
func TestNext_Mode_Orchestrated_AfterSuccess_ReturnsConsultDecision(t *testing.T) {
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet("Implementation-Only")
	agents := newTestAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")
	state := stateAfter("PLANNING", "", "planner-tdd-soft#1", domain.StatusSUCCESS, 1)

	dec := engine.Next(engine.NextInput{
		Workflow:     aw,
		Stages:       stages,
		State:        state,
		LastResponse: successResponse("planner-tdd-soft#1"),
		Agents:       agents,
		Seq:          1,
		Now:          fixedNow,
		Mode:         domain.ExecutionModeOrchestrated,
	})

	consult := requireConsult(t, dec)
	if consult.Trigger != domain.ConsultTriggerOrchestratedMode {
		t.Errorf("orchestrated mode after SUCCESS: want trigger ConsultTriggerOrchestratedMode, got %q",
			consult.Trigger)
	}
	// After a PLANNING-phase SUCCESS the current phase is "PLANNING" and stage is "".
	if consult.CurrentPhase != "PLANNING" {
		t.Errorf("orchestrated mode after SUCCESS: want CurrentPhase=%q, got %q",
			"PLANNING", consult.CurrentPhase)
	}
	if consult.CurrentStage != "" {
		t.Errorf("orchestrated mode after SUCCESS: want CurrentStage=%q (empty for PLANNING phase), got %q",
			"", consult.CurrentStage)
	}
}

// TestNext_Mode_Orchestrated_AfterCNA_ReturnsConsultDecision verifies that
// in orchestrated mode even a CNA response does not produce a DeviationDecision
// or a DispatchDecision — it always produces ConsultDecision.
func TestNext_Mode_Orchestrated_AfterCNA_ReturnsConsultDecision(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	stages := singleStageSet("TDD")
	agents := newTestAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "build-review", "tests-review-tdd",
		"implementation-tdd", "implementation-review",
	)
	// build-review has an unambiguous OnFindings hint. In auto-review this dispatches;
	// in orchestrated it must still consult.
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "build-review#9", domain.StatusCOMPLETED_NEEDS_ACTION, 9)

	dec := engine.Next(engine.NextInput{
		Workflow:     aw,
		Stages:       stages,
		State:        state,
		LastResponse: cnaResponse("build-review#9"),
		Agents:       agents,
		Seq:          9,
		Now:          fixedNow,
		Mode:         domain.ExecutionModeOrchestrated,
	})

	consult := requireConsult(t, dec)
	if consult.Trigger != domain.ConsultTriggerOrchestratedMode {
		t.Errorf("orchestrated mode after CNA: want ConsultTriggerOrchestratedMode, got %q",
			consult.Trigger)
	}
	// After a CNA in EXECUTION the current phase and stage come from the state.
	if consult.CurrentPhase != "EXECUTION.[StageNumber]" {
		t.Errorf("orchestrated mode after CNA: want CurrentPhase=%q, got %q",
			"EXECUTION.[StageNumber]", consult.CurrentPhase)
	}
	if consult.CurrentStage != "Stage-1" {
		t.Errorf("orchestrated mode after CNA: want CurrentStage=%q, got %q",
			"Stage-1", consult.CurrentStage)
	}
}

// TestNext_Mode_Auto_CNA_AmbiguousOnFindings_ReturnsDeviation verifies that in
// auto mode CNA with an ambiguous On Findings hint still yields Deviation (not
// Dispatch). Ambiguous OnFindings must deviate in every mode.
func TestNext_Mode_Auto_CNA_AmbiguousOnFindings_ReturnsDeviation(t *testing.T) {
	// brownfield-tdd row 10: implementation-review has
	// OnFindings="implementation-tdd (or other based on issue)" — ambiguous.
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := singleStageSet("TDD")
	agents := newTestAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "implementation-review#11", domain.StatusCOMPLETED_NEEDS_ACTION, 11)

	dec := engine.Next(engine.NextInput{
		Workflow:     aw,
		Stages:       stages,
		State:        state,
		LastResponse: cnaResponse("implementation-review#11"),
		Agents:       agents,
		Seq:          11,
		Now:          fixedNow,
		Mode:         domain.ExecutionModeAuto,
	})

	requireDeviation(t, dec)
}

// TestNext_Mode_AutoReview_CNA_AmbiguousOnFindings_ReturnsDeviation verifies
// that in auto-review mode CNA with an ambiguous On Findings hint also yields
// Deviation. The auto-review extension only fires on unambiguous hints.
func TestNext_Mode_AutoReview_CNA_AmbiguousOnFindings_ReturnsDeviation(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := singleStageSet("TDD")
	agents := newTestAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "implementation-review#11", domain.StatusCOMPLETED_NEEDS_ACTION, 11)

	dec := engine.Next(engine.NextInput{
		Workflow:     aw,
		Stages:       stages,
		State:        state,
		LastResponse: cnaResponse("implementation-review#11"),
		Agents:       agents,
		Seq:          11,
		Now:          fixedNow,
		Mode:         domain.ExecutionModeAutoReview,
	})

	requireDeviation(t, dec)
}

// TestNext_Mode_Auto_NonCNA_NonSuccess_ReturnsDeviation verifies that in auto
// mode non-CNA non-SUCCESS statuses always yield Deviation regardless of On Findings.
func TestNext_Mode_Auto_NonCNA_NonSuccess_ReturnsDeviation(t *testing.T) {
	// tests-review-tdd has OnFindings="test-writer-tdd" — unambiguous — but the
	// status is PARTIALLY_DONE (not CNA), so it must deviate.
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := singleStageSet("TDD")
	agents := newTestAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "tests-review-tdd#9", domain.StatusPARTIALLY_DONE, 9)

	dec := engine.Next(engine.NextInput{
		Workflow:     aw,
		Stages:       stages,
		State:        state,
		LastResponse: nonSuccessResponse("tests-review-tdd#9", domain.StatusPARTIALLY_DONE),
		Agents:       agents,
		Seq:          9,
		Now:          fixedNow,
		Mode:         domain.ExecutionModeAuto,
	})

	requireDeviation(t, dec)
}

// TestNext_Mode_AutoReview_NonCNA_NonSuccess_ReturnsDeviation verifies that in
// auto-review mode non-CNA non-SUCCESS statuses also yield Deviation, not Dispatch.
func TestNext_Mode_AutoReview_NonCNA_NonSuccess_ReturnsDeviation(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := singleStageSet("TDD")
	agents := newTestAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "tests-review-tdd#9", domain.StatusBLOCKED, 9)

	dec := engine.Next(engine.NextInput{
		Workflow:     aw,
		Stages:       stages,
		State:        state,
		LastResponse: nonSuccessResponse("tests-review-tdd#9", domain.StatusBLOCKED),
		Agents:       agents,
		Seq:          9,
		Now:          fixedNow,
		Mode:         domain.ExecutionModeAutoReview,
	})

	requireDeviation(t, dec)
}

// ===== Review artifact injection (Stage 2) =====
//
// On the auto-review auto-route-back (CNA + unambiguous On Findings), the
// reviewing agent's output artifacts (in.LastOutputArtifacts) are appended to
// the dispatched step's Request.InputArtifacts, after the table row's entries,
// without duplicating any path already present.
//
// No injection occurs on SUCCESS-routed dispatches or Deviation decisions.

// TestNext_ArtifactInjection_AutoReview_CNA_InjectsLastOutputArtifacts verifies
// that on the auto-review auto-route-back the reviewing agent's output artifacts
// are present in the dispatched step's InputArtifacts, after the table row's
// own entries.
func TestNext_Mode_Unset_ReturnsDeviationAmbiguousRoute(t *testing.T) {
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet("Implementation-Only")
	agents := newTestAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")

	dec := engine.Next(engine.NextInput{
		Workflow:     aw,
		Stages:       stages,
		State:        emptyState(),
		LastResponse: nil,
		Agents:       agents,
		Seq:          0,
		Now:          fixedNow,
		Mode:         domain.ExecutionModeUnset,
	})

	dev := requireDeviation(t, dec)
	if dev.Info.Kind != domain.DeviationAmbiguousRoute {
		t.Errorf("ExecutionModeUnset: want DeviationKind=%q, got %q",
			domain.DeviationAmbiguousRoute, dev.Info.Kind)
	}
}

