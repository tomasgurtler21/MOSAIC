package engine_test

import (
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/engine"
)

func TestNext_CreatorArtifactInjection_RegistryPresent_InjectsOutputArtifact(t *testing.T) {
	aw := mustParseAndAdmit(t, creatorInjectionContent, "creator-injection", "1.0")
	stages := singleStageSet("TDD")
	agents := newTestAgents("test-writer-tdd", "tests-review-tdd", "implementation-tdd")
	// tests-review-tdd (row 1) completed with CNA; OnFindings=test-writer-tdd.
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "tests-review-tdd#5", domain.StatusCOMPLETED_NEEDS_ACTION, 5)

	// Registry entry: test-writer-tdd's prior output, bare path after StripRunPrefix.
	registry := []domain.ArtifactRegistryEntry{
		{Artifact: "Stage-1/PlanProgress.md", CreatedIn: "Test.1", CreatedBy: "test-writer-tdd#1"},
	}
	// Review agent's last output artifacts (injected by existing review logic).
	reviewOutputs := []string{"Stage-1/tests-review-tdd.md"}

	dec := engine.Next(engine.NextInput{
		Workflow:            aw,
		Stages:              stages,
		State:               state,
		LastResponse:        cnaResponse("tests-review-tdd#5"),
		LastOutputArtifacts: reviewOutputs,
		Agents:              agents,
		Seq:                 5,
		Now:                 fixedNow,
		Mode:                domain.ExecutionModeAutoReview,
		ArtifactRegistry:    registry,
	})

	step := requireDispatch(t, dec)
	// The creator's prior output artifact must appear in the dispatched
	// test-writer-tdd step's InputArtifacts (it is not a table row entry,
	// so deduplication does not suppress it).
	found := false
	for _, a := range step.Request.InputArtifacts {
		if a == "Stage-1/PlanProgress.md" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("creator-artifact injection: want %q in dispatched InputArtifacts %v",
			"Stage-1/PlanProgress.md", step.Request.InputArtifacts)
	}
}

// TestNext_CreatorArtifactInjection_RegistryAbsent_NoInjection verifies that
// when ArtifactRegistry is nil (first invocation of the creator agent), no
// creator-artifact injection occurs -- the dispatched step's InputArtifacts
// contain only the table row entries and the review agent's LastOutputArtifacts.
func TestNext_CreatorArtifactInjection_RegistryAbsent_NoInjection(t *testing.T) {
	aw := mustParseAndAdmit(t, creatorInjectionContent, "creator-injection", "1.0")
	stages := singleStageSet("TDD")
	agents := newTestAgents("test-writer-tdd", "tests-review-tdd", "implementation-tdd")
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "tests-review-tdd#5", domain.StatusCOMPLETED_NEEDS_ACTION, 5)
	reviewOutputs := []string{"Stage-1/tests-review-tdd.md"}

	dec := engine.Next(engine.NextInput{
		Workflow:            aw,
		Stages:              stages,
		State:               state,
		LastResponse:        cnaResponse("tests-review-tdd#5"),
		LastOutputArtifacts: reviewOutputs,
		Agents:              agents,
		Seq:                 5,
		Now:                 fixedNow,
		Mode:                domain.ExecutionModeAutoReview,
		ArtifactRegistry:    nil, // first invocation: no prior outputs in registry
	})

	step := requireDispatch(t, dec)
	// Stage-1/PlanProgress.md is NOT in the table input for test-writer-tdd
	// and is NOT in reviewOutputs. With no registry, it must not appear.
	for _, a := range step.Request.InputArtifacts {
		if a == "Stage-1/PlanProgress.md" {
			t.Errorf("creator-artifact injection: no registry supplied, but %q appeared in InputArtifacts %v (spurious injection)",
				"Stage-1/PlanProgress.md", step.Request.InputArtifacts)
		}
	}
}

// TestNext_CreatorArtifactInjection_TemplatedOutput_MatchesResolvedPath verifies
// that creator-artifact injection fires correctly when the target row's declared
// output artifact contains a {StageNumber} template token. The comparison must
// use the resolved path (Stage-1/PlanProgress.md) rather than the raw template
// (Stage-{StageNumber}/PlanProgress.md), so a registry entry with the bare
// resolved path triggers injection.
func TestNext_CreatorArtifactInjection_TemplatedOutput_MatchesResolvedPath(t *testing.T) {
	aw := mustParseAndAdmit(t, creatorInjectionContent, "creator-injection", "1.0")
	// Stage 2: resolved output would be Stage-2/PlanProgress.md.
	stages := newTestStageSet([]domain.StageEntry{
		{Number: 1, HITL: false, Approach: "TDD"},
		{Number: 2, HITL: false, Approach: "TDD"},
	})
	agents := newTestAgents("test-writer-tdd", "tests-review-tdd", "implementation-tdd")
	// State: tests-review-tdd completed for stage 2 (CNA).
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-2", "tests-review-tdd#7", domain.StatusCOMPLETED_NEEDS_ACTION, 7)
	// Registry carries the bare resolved path for stage 2 (as session provides
	// after StripRunPrefix -- the resolved path, not the raw template).
	registry := []domain.ArtifactRegistryEntry{
		{Artifact: "Stage-2/PlanProgress.md", CreatedIn: "Test.2", CreatedBy: "test-writer-tdd#5"},
	}
	reviewOutputs := []string{"Stage-2/tests-review-tdd.md"}

	dec := engine.Next(engine.NextInput{
		Workflow:            aw,
		Stages:              stages,
		State:               state,
		LastResponse:        cnaResponse("tests-review-tdd#7"),
		LastOutputArtifacts: reviewOutputs,
		Agents:              agents,
		Seq:                 7,
		Now:                 fixedNow,
		Mode:                domain.ExecutionModeAutoReview,
		ArtifactRegistry:    registry,
	})

	step := requireDispatch(t, dec)
	// Stage-2/PlanProgress.md must be injected (resolved path match).
	found := false
	for _, a := range step.Request.InputArtifacts {
		if a == "Stage-2/PlanProgress.md" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("templated-output creator injection: want %q in dispatched InputArtifacts %v",
			"Stage-2/PlanProgress.md", step.Request.InputArtifacts)
	}
}

// ===== "next" keyword routing (handleNonExecutionSuccess) =====

// nextKeywordNonExecContent is a two-row non-EXECUTION workflow where both rows
// carry On Success = "next". The same agent appears in both rows to reflect the
// motivating scenario (same-agent consecutive rows would loop with name-based
// routing). Tests derive three behaviors from this fixture:
//   - row 0 success with "next" → advance to row 1 (DispatchDecision, agent-a).
//   - row 1 success with "next" → past the last row → CompleteDecision.
//   - case-insensitive variants (see nextKeywordMixedCaseContent and
//     nextKeywordUpperCaseContent) advance to row 1 the same way.
const nextKeywordNonExecContent = `## Next Keyword Non-Exec Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | agent-a | FALSE | next | - | - | out.md |
| PLANNING | agent-b | FALSE | next | - | - | final.md |
`

// nextKeywordMixedCaseContent is a two-row non-EXECUTION workflow where row 0
// carries On Success = "Next" (mixed case) to verify case-insensitive matching.
const nextKeywordMixedCaseContent = `## Next Keyword Mixed Case Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | agent-a | FALSE | Next | - | - | out.md |
| PLANNING | agent-b | FALSE | COMPLETE | - | out.md | final.md |
`

// nextKeywordUpperCaseContent is a two-row non-EXECUTION workflow where row 0
// carries On Success = "NEXT" (upper case) to verify case-insensitive matching.
const nextKeywordUpperCaseContent = `## Next Keyword Upper Case Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | agent-a | FALSE | NEXT | - | - | out.md |
| PLANNING | agent-b | FALSE | COMPLETE | - | out.md | final.md |
`

// nextIntoExecutionContent is a workflow with a single PLANNING row using
// On Success = "next" followed by EXECUTION rows. Used to verify that "next"
// correctly enters the EXECUTION code path when the next row is staged, and that
// it returns StopDecision when no stage set is available (same behavior as naming
// an EXECUTION agent with nil stages).
const nextIntoExecutionContent = `## Next Into Execution Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | planner | FALSE | next | - | - | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md |
| EXECUTION.[StageNumber] | implementation-tdd | FALSE | implementation-review | - | Stage-{StageNumber}/Plan.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.[StageNumber] | implementation-review | FALSE | COMPLETE | implementation-tdd | Stage-{StageNumber}/Plan.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/implementation-review.md |
`

// TestNext_NextKeyword_AdvancesToNextRow verifies that a SUCCESS response from a
// non-EXECUTION row with On Success = "next" (lowercase) dispatches the
// immediately following row by index rather than by agent-name lookup.
func TestNext_NextKeyword_AdvancesToNextRow(t *testing.T) {
	aw := mustParseAndAdmit(t, nextKeywordNonExecContent, "next-keyword-non-exec", "1.0")
	agents := newTestAgents("agent-a", "agent-b")
	// State: agent-a (row 0) just completed successfully.
	state := stateAfter("PLANNING", "", "agent-a#1", domain.StatusSUCCESS, 1)

	dec := engine.Next(engine.NextInput{
		Workflow:     aw,
		Stages:       nil, // no EXECUTION rows in this workflow
		State:        state,
		LastResponse: successResponse("agent-a#1"),
		Agents:       agents,
		Seq:          1,
		Now:          fixedNow,
		Mode:         domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	if agentName(step.Request.AgentInstanceID) != "agent-b" {
		t.Errorf("next keyword: want agent-b (row 1 by index), got %s", step.Request.AgentInstanceID)
	}
	if step.RowIndex != 1 {
		t.Errorf("next keyword: want RowIndex=1 (next row), got %d", step.RowIndex)
	}
}

// TestNext_NextKeyword_PastLastRow_ReturnsComplete verifies that a SUCCESS
// response from the last row in the workflow when On Success = "next" returns
// CompleteDecision (same as On Success = "COMPLETE").
func TestNext_NextKeyword_PastLastRow_ReturnsComplete(t *testing.T) {
	aw := mustParseAndAdmit(t, nextKeywordNonExecContent, "next-keyword-non-exec", "1.0")
	agents := newTestAgents("agent-a", "agent-b")
	// State: agent-b (row 1, the last row) just completed successfully.
	state := stateAfter("PLANNING", "", "agent-b#2", domain.StatusSUCCESS, 2)

	dec := engine.Next(engine.NextInput{
		Workflow:     aw,
		Stages:       nil,
		State:        state,
		LastResponse: successResponse("agent-b#2"),
		Agents:       agents,
		Seq:          2,
		Now:          fixedNow,
		Mode:         domain.ExecutionModeAutoReview,
	})

	requireComplete(t, dec)
}

// TestNext_NextKeyword_MixedCase_AdvancesToNextRow verifies that On Success =
// "Next" (mixed case) is treated identically to "next" (case-insensitive match).
func TestNext_NextKeyword_MixedCase_AdvancesToNextRow(t *testing.T) {
	aw := mustParseAndAdmit(t, nextKeywordMixedCaseContent, "next-keyword-mixed-case", "1.0")
	agents := newTestAgents("agent-a", "agent-b")
	state := stateAfter("PLANNING", "", "agent-a#1", domain.StatusSUCCESS, 1)

	dec := engine.Next(engine.NextInput{
		Workflow:     aw,
		Stages:       nil,
		State:        state,
		LastResponse: successResponse("agent-a#1"),
		Agents:       agents,
		Seq:          1,
		Now:          fixedNow,
		Mode:         domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	if agentName(step.Request.AgentInstanceID) != "agent-b" {
		t.Errorf("Next (mixed case): want agent-b (next row by index), got %s",
			step.Request.AgentInstanceID)
	}
}

// TestNext_NextKeyword_UpperCase_AdvancesToNextRow verifies that On Success =
// "NEXT" (upper case) is treated identically to "next" (case-insensitive match).
func TestNext_NextKeyword_UpperCase_AdvancesToNextRow(t *testing.T) {
	aw := mustParseAndAdmit(t, nextKeywordUpperCaseContent, "next-keyword-upper-case", "1.0")
	agents := newTestAgents("agent-a", "agent-b")
	state := stateAfter("PLANNING", "", "agent-a#1", domain.StatusSUCCESS, 1)

	dec := engine.Next(engine.NextInput{
		Workflow:     aw,
		Stages:       nil,
		State:        state,
		LastResponse: successResponse("agent-a#1"),
		Agents:       agents,
		Seq:          1,
		Now:          fixedNow,
		Mode:         domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	if agentName(step.Request.AgentInstanceID) != "agent-b" {
		t.Errorf("NEXT (upper case): want agent-b (next row by index), got %s",
			step.Request.AgentInstanceID)
	}
}

// TestNext_NextKeyword_IntoExecutionRow_TriggersStageResolution verifies that
// On Success = "next" from a pre-EXECUTION row whose successor is a staged
// EXECUTION row triggers full stage/group resolution (same dispatch path as
// naming an EXECUTION agent directly). The dispatched agent must be the first
// agent of the ordered group for the given stage and approach.
func TestNext_NextKeyword_IntoExecutionRow_TriggersStageResolution(t *testing.T) {
	aw := mustParseAndAdmit(t, nextIntoExecutionContent, "next-into-execution", "1.0")
	stages := singleStageSet("Implementation-Only")
	agents := newTestAgents("planner", "implementation-tdd", "implementation-review")
	// State: planner (row 0, PLANNING) just completed successfully.
	state := stateAfter("PLANNING", "", "planner#1", domain.StatusSUCCESS, 1)

	dec := engine.Next(engine.NextInput{
		Workflow:     aw,
		Stages:       stages,
		State:        state,
		LastResponse: successResponse("planner#1"),
		Agents:       agents,
		Seq:          1,
		Now:          fixedNow,
		Mode:         domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	if agentName(step.Request.AgentInstanceID) != "implementation-tdd" {
		t.Errorf("next into EXECUTION: want implementation-tdd (first EXECUTION group row), got %s",
			step.Request.AgentInstanceID)
	}
	if step.Stage == "" {
		t.Error("next into EXECUTION: want non-empty Stage (stage/group context resolved), got empty")
	}
}

// TestNext_NextKeyword_IntoExecutionRow_NilStages_ReturnsStop verifies that
// On Success = "next" into a staged EXECUTION row with no available stage set
// returns StopDecision with a non-empty reason. This is consistent with naming
// an EXECUTION agent directly when stages is nil: both yield StopDecision.
func TestNext_NextKeyword_IntoExecutionRow_NilStages_ReturnsStop(t *testing.T) {
	aw := mustParseAndAdmit(t, nextIntoExecutionContent, "next-into-execution", "1.0")
	agents := newTestAgents("planner", "implementation-tdd", "implementation-review")
	state := stateAfter("PLANNING", "", "planner#1", domain.StatusSUCCESS, 1)

	dec := engine.Next(engine.NextInput{
		Workflow:     aw,
		Stages:       nil, // no stage set available
		State:        state,
		LastResponse: successResponse("planner#1"),
		Agents:       agents,
		Seq:          1,
		Now:          fixedNow,
		Mode:         domain.ExecutionModeAutoReview,
	})

	stop := requireStop(t, dec)
	if stop.Reason == "" {
		t.Error("next into EXECUTION with nil stages: want non-empty StopDecision.Reason, got empty")
	}
}

// ===== ResolveArtifacts export: codify existing behavior (T1.4) =====
//
// These tests call engine.ResolveArtifacts (the newly exported name) to
// codify the existing template-expansion behavior so that the rename
// (I1.6) does not regress it. Tests are placed in the engine_test package
// (black-box) to verify the exported name is accessible from outside the
// engine package.

// TestResolveArtifacts_StageNumber_Substitution_InputArtifact verifies that
// {StageNumber} in an input artifact path is replaced with the current stage
// number when a stage context is present.
func TestResolveArtifacts_StageNumber_Substitution_InputArtifact(t *testing.T) {
	arts := []string{"Stage-{StageNumber}/Plan.md", "ContractsDesign.md"}
	stages := singleStageSet("TDD")
	got, err := engine.ResolveArtifacts(arts, 2, "Stage-2", stages, nil, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"Stage-2/Plan.md", "ContractsDesign.md"}
	if !stringSlicesEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestResolveArtifacts_StageNumber_Substitution_OutputArtifact verifies that
// {StageNumber} in an output artifact path is also replaced with the current
// stage number (same substitution rule for both directions).
func TestResolveArtifacts_StageNumber_Substitution_OutputArtifact(t *testing.T) {
	arts := []string{"Stage-{StageNumber}/PlanProgress.md"}
	stages := singleStageSet("TDD")
	got, err := engine.ResolveArtifacts(arts, 3, "Stage-3", stages, nil, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"Stage-3/PlanProgress.md"}
	if !stringSlicesEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestResolveArtifacts_StageWildcard_Input_ExpandedPerStage verifies that
// Stage-* in an input artifact path is expanded to one path per stage in
// the effective stage set.
func TestResolveArtifacts_StageWildcard_Input_ExpandedPerStage(t *testing.T) {
	arts := []string{"Stage-*/Plan.md"}
	stages := newTestStageSet([]domain.StageEntry{
		{Number: 1, HITL: false, Approach: "TDD"},
		{Number: 2, HITL: false, Approach: "TDD"},
		{Number: 3, HITL: false, Approach: "TDD"},
	})
	// Non-EXECUTION context (stageNum=0, stageStr="") uses refreshedStages for expansion.
	got, err := engine.ResolveArtifacts(arts, 0, "", stages, nil, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"Stage-1/Plan.md", "Stage-2/Plan.md", "Stage-3/Plan.md"}
	if !stringSlicesEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestResolveArtifacts_StageWildcard_Output_PassThrough verifies that Stage-*
// in an output artifact path is passed through unexpanded (output artifacts
// use the wildcard as a literal declaration, not a glob expansion).
func TestResolveArtifacts_StageWildcard_Output_PassThrough(t *testing.T) {
	arts := []string{"Stage-*/PlanProgress.md"}
	stages := newTestStageSet([]domain.StageEntry{
		{Number: 1, HITL: false, Approach: "TDD"},
		{Number: 2, HITL: false, Approach: "TDD"},
	})
	got, err := engine.ResolveArtifacts(arts, 1, "Stage-1", stages, nil, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"Stage-*/PlanProgress.md"}
	if !stringSlicesEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestResolveArtifacts_UnresolvablePlaceholder_ReturnsError verifies that
// {StageNumber} in an artifact path without a stage context (stageNum=0,
// stageStr="") returns an error rather than leaving the placeholder literal.
func TestResolveArtifacts_UnresolvablePlaceholder_ReturnsError(t *testing.T) {
	arts := []string{"Stage-{StageNumber}/Plan.md"}
	stages := singleStageSet("TDD")
	// No stage context: stageNum=0, stageStr="" (non-EXECUTION row).
	_, err := engine.ResolveArtifacts(arts, 0, "", stages, nil, true)
	if err == nil {
		t.Fatal("want error for {StageNumber} without stage context, got nil")
	}
	if !strings.Contains(err.Error(), "{StageNumber}") {
		t.Errorf("error %q: want mention of the unresolvable placeholder {StageNumber}", err.Error())
	}
}

// stringSlicesEqual returns true when a and b have the same length and equal
// elements in the same order.
func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
