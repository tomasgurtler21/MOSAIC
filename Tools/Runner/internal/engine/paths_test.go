package engine_test

import (
	"strings"
	"testing"

	"mosaic-run/internal/compat"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/engine"
	"mosaic-run/internal/workflow"
)

func TestNext_Paths_StageNumber_SubstitutedInInput(t *testing.T) {
	// quick-fix row 2: implementation-tdd has input "Stage-{StageNumber}/Plan.md".
	// When dispatched in Stage-1, it must resolve to "Stage-1/Plan.md".
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet("Implementation-Only")
	agents := newTestAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")
	state := stateAfter("PLANNING", "", "plan-review#2", domain.StatusSUCCESS, 2)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("plan-review#2"),
		Agents:          agents,
		Seq:             2,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	// Should have resolved Stage-{StageNumber}/Plan.md → Stage-1/Plan.md.
	found := false
	for _, a := range step.Request.InputArtifacts {
		if a == "Stage-1/Plan.md" {
			found = true
		}
		if strings.Contains(a, "{StageNumber}") {
			t.Errorf("unresolved {StageNumber} found in input artifact: %s", a)
		}
	}
	if !found {
		t.Errorf("want Stage-1/Plan.md in InputArtifacts, got %v", step.Request.InputArtifacts)
	}
}

// TestNext_Paths_StageNumber_SubstitutedInOutput verifies {StageNumber} is
// replaced in output artifact paths too.
func TestNext_Paths_StageNumber_SubstitutedInOutput(t *testing.T) {
	// quick-fix row 2: implementation-tdd output is "Stage-{StageNumber}/PlanProgress.md".
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet("Implementation-Only")
	agents := newTestAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")
	state := stateAfter("PLANNING", "", "plan-review#2", domain.StatusSUCCESS, 2)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("plan-review#2"),
		Agents:          agents,
		Seq:             2,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	found := false
	for _, a := range step.Request.OutputArtifacts {
		if a == "Stage-1/PlanProgress.md" {
			found = true
		}
		if strings.Contains(a, "{StageNumber}") {
			t.Errorf("unresolved {StageNumber} found in output artifact: %s", a)
		}
	}
	if !found {
		t.Errorf("want Stage-1/PlanProgress.md in OutputArtifacts, got %v", step.Request.OutputArtifacts)
	}
}

// TestNext_Paths_StageStar_InputExpanded verifies that Stage-* in input artifacts
// is expanded to one path per stage in the stage set.
func TestNext_Paths_StageStar_InputExpanded(t *testing.T) {
	// quick-fix row 1 (plan-review) has input "Stage-*/Plan.md, Stage-*/PlanProgress.md".
	// With 3 stages, Stage-*/Plan.md expands to Stage-1/Plan.md, Stage-2/Plan.md, Stage-3/Plan.md.
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	threeStages := newTestStageSet([]domain.StageEntry{
		{Number: 1, HITL: false, Approach: "Implementation-Only"},
		{Number: 2, HITL: false, Approach: "Implementation-Only"},
		{Number: 3, HITL: false, Approach: "Implementation-Only"},
	})
	agents := newTestAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")
	state := stateAfter("PLANNING", "", "planner-tdd-soft#1", domain.StatusSUCCESS, 1)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          threeStages,
		State:           state,
		LastResponse:    successResponse("planner-tdd-soft#1"),
		Agents:          agents,
		Seq:             1,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)

	// Stage-*/Plan.md must have expanded to 3 entries.
	wantExpanded := []string{"Stage-1/Plan.md", "Stage-2/Plan.md", "Stage-3/Plan.md"}
	for _, want := range wantExpanded {
		found := false
		for _, a := range step.Request.InputArtifacts {
			if a == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("want %q in InputArtifacts after Stage-* expansion; got %v", want, step.Request.InputArtifacts)
		}
	}
	// The raw Stage-* pattern must not appear unexpanded.
	for _, a := range step.Request.InputArtifacts {
		if strings.Contains(a, "*") {
			t.Errorf("Stage-* still present unexpanded in InputArtifacts: %s", a)
		}
	}
}

// TestNext_Paths_StageStar_OutputPassedThrough verifies that Stage-* in output
// artifacts is dispatched as-is (not expanded).
func TestNext_Paths_StageStar_OutputPassedThrough(t *testing.T) {
	// quick-fix row 0 (planner-tdd-soft) output includes "Stage-*/Plan.md" and
	// "Stage-*/PlanProgress.md". These must pass through to the subagent unchanged.
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet("Implementation-Only")
	agents := newTestAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           emptyState(),
		LastResponse:    nil,
		Agents:          agents,
		Seq:             0,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	found := false
	for _, a := range step.Request.OutputArtifacts {
		if a == "Stage-*/Plan.md" {
			found = true
		}
	}
	if !found {
		t.Errorf("want Stage-*/Plan.md unexpanded in OutputArtifacts, got %v", step.Request.OutputArtifacts)
	}
}

// ===== Non-EXECUTION Stage-* resolution using refreshedStages (AC7.9) =====

// TestNext_Paths_NonExecution_StageStar_UsesRefreshedStages verifies that
// when refreshedStages is provided for a non-EXECUTION row, Stage-* input
// wildcards are expanded against the refreshed set.
func TestNext_Paths_NonExecution_StageStar_UsesRefreshedStages(t *testing.T) {
	// quick-fix row 1: plan-review has "Stage-*/Plan.md" input.
	// Run-start stages has 1 stage; refreshedStages has 3 stages.
	// The expansion must use the refreshed set (3 entries), not the run-start set.
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	runStartStages := singleStageSet("Implementation-Only")
	refreshed := newTestStageSet([]domain.StageEntry{
		{Number: 1, HITL: false, Approach: "Implementation-Only"},
		{Number: 2, HITL: false, Approach: "Implementation-Only"},
		{Number: 3, HITL: false, Approach: "Implementation-Only"},
	})
	agents := newTestAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")
	state := stateAfter("PLANNING", "", "planner-tdd-soft#1", domain.StatusSUCCESS, 1)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          runStartStages,
		State:           state,
		LastResponse:    successResponse("planner-tdd-soft#1"),
		Agents:          agents,
		Seq:             1,
		Now:             fixedNow,
		RefreshedStages: refreshed,
		Mode:            domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	// With refreshedStages(3), Stage-*/Plan.md must expand to 3 paths.
	count := 0
	for _, a := range step.Request.InputArtifacts {
		if strings.HasSuffix(a, "/Plan.md") && strings.HasPrefix(a, "Stage-") && !strings.Contains(a, "*") {
			count++
		}
	}
	if count != 3 {
		t.Errorf("want 3 expanded Stage-N/Plan.md paths (from refreshedStages), got %d; InputArtifacts=%v",
			count, step.Request.InputArtifacts)
	}
}

// TestNext_Paths_NonExecution_StageStar_NilRefreshed_UsesRunStartStages verifies
// that when refreshedStages is nil, Stage-* expansion uses the run-start stage set.
func TestNext_Paths_NonExecution_StageStar_NilRefreshed_UsesRunStartStages(t *testing.T) {
	// quick-fix row 1: plan-review, Stage-*/Plan.md with run-start stages = 2.
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	runStartStages := newTestStageSet([]domain.StageEntry{
		{Number: 1, HITL: false, Approach: "Implementation-Only"},
		{Number: 2, HITL: false, Approach: "Implementation-Only"},
	})
	agents := newTestAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")
	state := stateAfter("PLANNING", "", "planner-tdd-soft#1", domain.StatusSUCCESS, 1)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          runStartStages,
		State:           state,
		LastResponse:    successResponse("planner-tdd-soft#1"),
		Agents:          agents,
		Seq:             1,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	count := 0
	for _, a := range step.Request.InputArtifacts {
		if strings.HasSuffix(a, "/Plan.md") && strings.HasPrefix(a, "Stage-") && !strings.Contains(a, "*") {
			count++
		}
	}
	if count != 2 {
		t.Errorf("want 2 expanded Stage-N/Plan.md paths (from run-start stages), got %d; InputArtifacts=%v",
			count, step.Request.InputArtifacts)
	}
}

// ===== DispatchStep carries artifact paths exactly =====

// TestNext_Artifacts_PreExecutionRow_NoTemplateExpansion verifies that
// pre-execution artifact paths with no template variables are passed through
// unchanged.
func TestNext_Artifacts_PreExecutionRow_NoTemplateExpansion(t *testing.T) {
	// quick-fix row 0: planner-tdd-soft output = "Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md"
	// Plan.md has no template — must appear unchanged.
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet("Implementation-Only")
	agents := newTestAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           emptyState(),
		LastResponse:    nil,
		Agents:          agents,
		Seq:             0,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	found := false
	for _, a := range step.Request.OutputArtifacts {
		if a == "Plan.md" {
			found = true
		}
	}
	if !found {
		t.Errorf("want Plan.md in OutputArtifacts unchanged, got %v", step.Request.OutputArtifacts)
	}
}

// TestNext_Artifacts_InputArtifactsMatchRow verifies that InputArtifacts in the
// ProtocolRequest are populated from the routing row (after template resolution).
func TestNext_Artifacts_InputArtifactsMatchRow(t *testing.T) {
	// quick-fix row 1 (plan-review): inputs are Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md.
	// After Stage-* expansion with 1 stage: Plan.md, Stage-1/Plan.md, Stage-1/PlanProgress.md.
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet("Implementation-Only")
	agents := newTestAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")
	state := stateAfter("PLANNING", "", "planner-tdd-soft#1", domain.StatusSUCCESS, 1)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("planner-tdd-soft#1"),
		Agents:          agents,
		Seq:             1,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	if len(step.Request.InputArtifacts) == 0 {
		t.Error("want non-empty InputArtifacts for plan-review row, got empty")
	}
	// "Plan.md" (no template) must appear.
	foundPlanMd := false
	for _, a := range step.Request.InputArtifacts {
		if a == "Plan.md" {
			foundPlanMd = true
		}
	}
	if !foundPlanMd {
		t.Errorf("want Plan.md in InputArtifacts, got %v", step.Request.InputArtifacts)
	}
}

// ===== Unresolvable template path → StopDecision (AC7.6) =====

// TestNext_Paths_UnresolvableStageNumber_ReturnsStop verifies that when an artifact
// path contains {StageNumber} in a context where the stage number cannot be resolved
// (e.g. a pre-execution PLANNING row has no stage context), the engine returns a
// StopDecision with a non-empty reason rather than silently ignoring or panicking.
func TestNext_Paths_UnresolvableStageNumber_ReturnsStop(t *testing.T) {
	// A PLANNING row with {StageNumber} in its input — {StageNumber} is only valid
	// inside EXECUTION phase. There is no current stage context when dispatching this
	// pre-execution row, so the engine cannot substitute the placeholder.
	const unresolvableTemplateContent = `## Unresolvable Template Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | agent-a | FALSE | COMPLETE | - | Stage-{StageNumber}/Plan.md | out.md |
`
	info := domain.WorkflowInfo{ID: "unresolvable-template", Version: "1.0"}
	table, err := workflow.Parse([]byte(unresolvableTemplateContent), info)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	aw, err := compat.Admit(table)
	if err != nil {
		t.Fatalf("Admit: %v", err)
	}
	agents := newTestAgents("agent-a")

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          nil,
		State:           emptyState(),
		LastResponse:    nil,
		Agents:          agents,
		Seq:             0,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	stop := requireStop(t, dec)
	if stop.Reason == "" {
		t.Error("want non-empty StopDecision.Reason for unresolvable {StageNumber} placeholder")
	}
}

