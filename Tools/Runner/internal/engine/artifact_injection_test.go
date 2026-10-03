package engine_test

import (
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/engine"
)

func TestNext_ArtifactInjection_AutoReview_CNA_InjectsLastOutputArtifacts(t *testing.T) {
	// brownfield-tdd-build-verified row 8: build-review, OnFindings="test-writer-tdd".
	// build-review's own output artifact (LastOutputArtifacts) must be appended
	// to the dispatched test-writer-tdd step's InputArtifacts.
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	stages := singleStageSet("TDD")
	agents := newTestAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "build-review", "tests-review-tdd",
		"implementation-tdd", "implementation-review",
	)
	state := stateAfterRow("EXECUTION", "Test.1", "build-review#9", domain.StatusCOMPLETED_NEEDS_ACTION, 9, 9)
	// These are the output artifacts of the build-review step that just ran.
	reviewOutputs := []string{"Stage-1/build-review-tests.md"}

	dec := engine.Next(engine.NextInput{
		Workflow:            aw,
		Stages:              stages,
		State:               state,
		LastResponse:        cnaResponse("build-review#9"),
		LastOutputArtifacts: reviewOutputs,
		Agents:              agents,
		Seq:                 9,
		Now:                 fixedNow,
		Mode:                domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	// The dispatched step is test-writer-tdd. Its InputArtifacts must include
	// the build-review output artifact appended after the table row's own entries.
	found := false
	for _, a := range step.Request.InputArtifacts {
		if a == "Stage-1/build-review-tests.md" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("auto-review CNA auto-route-back: want review output artifact %q in dispatched step InputArtifacts, got %v",
			"Stage-1/build-review-tests.md", step.Request.InputArtifacts)
	}
}

// TestNext_ArtifactInjection_AutoReview_CNA_NoDeduplication_WhenUnique verifies
// that when LastOutputArtifacts contains paths not already in the table row's
// InputArtifacts, all of them are appended.
func TestNext_ArtifactInjection_AutoReview_CNA_NoDeduplication_WhenUnique(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	stages := singleStageSet("TDD")
	agents := newTestAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "build-review", "tests-review-tdd",
		"implementation-tdd", "implementation-review",
	)
	state := stateAfterRow("EXECUTION", "Test.1", "build-review#9", domain.StatusCOMPLETED_NEEDS_ACTION, 9, 9)
	// Two unique review outputs not in the table row's inputs.
	reviewOutputs := []string{"Stage-1/build-review-tests.md", "Stage-1/extra-finding.md"}

	dec := engine.Next(engine.NextInput{
		Workflow:            aw,
		Stages:              stages,
		State:               state,
		LastResponse:        cnaResponse("build-review#9"),
		LastOutputArtifacts: reviewOutputs,
		Agents:              agents,
		Seq:                 9,
		Now:                 fixedNow,
		Mode:                domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	inputSet := make(map[string]bool)
	for _, a := range step.Request.InputArtifacts {
		inputSet[a] = true
	}
	for _, expected := range reviewOutputs {
		if !inputSet[expected] {
			t.Errorf("auto-review CNA: want review output %q in dispatched InputArtifacts, got %v",
				expected, step.Request.InputArtifacts)
		}
	}
}

// TestNext_ArtifactInjection_AutoReview_CNA_DeduplicatesExisting verifies that
// paths already present in the table row's InputArtifacts are not duplicated
// when LastOutputArtifacts contains them too, and that a new unique path from
// LastOutputArtifacts IS injected (proving injection occurred at all).
func TestNext_ArtifactInjection_AutoReview_CNA_DeduplicatesExisting(t *testing.T) {
	// We use the build-review row (row 8) dispatching back to test-writer-tdd.
	// test-writer-tdd's table InputArtifacts are:
	//   Stage-1/Plan.md, ContractsDesign.md, Stage-1/PlanProgress.md
	// LastOutputArtifacts contains two entries:
	//   - "Stage-1/Plan.md": already in the table row — must NOT appear twice.
	//   - "Stage-1/build-review-findings.md": NOT in the table row — must appear
	//     exactly once, proving injection actually ran.
	// This combination tests both dedup (existing path) and injection (unique path)
	// in a single call, so a missing-injection bug would cause the unique path to
	// be absent and the test would fail.
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	stages := singleStageSet("TDD")
	agents := newTestAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "build-review", "tests-review-tdd",
		"implementation-tdd", "implementation-review",
	)
	state := stateAfterRow("EXECUTION", "Test.1", "build-review#9", domain.StatusCOMPLETED_NEEDS_ACTION, 9, 9)
	// Stage-1/Plan.md is already in test-writer-tdd's table InputArtifacts;
	// Stage-1/build-review-findings.md is not.
	reviewOutputs := []string{"Stage-1/Plan.md", "Stage-1/build-review-findings.md"}

	dec := engine.Next(engine.NextInput{
		Workflow:            aw,
		Stages:              stages,
		State:               state,
		LastResponse:        cnaResponse("build-review#9"),
		LastOutputArtifacts: reviewOutputs,
		Agents:              agents,
		Seq:                 9,
		Now:                 fixedNow,
		Mode:                domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)

	// The duplicate path must appear exactly once (dedup contract).
	dupCount := 0
	for _, a := range step.Request.InputArtifacts {
		if a == "Stage-1/Plan.md" {
			dupCount++
		}
	}
	if dupCount != 1 {
		t.Errorf("auto-review CNA dedup: want Stage-1/Plan.md exactly once in InputArtifacts, got %d occurrences in %v",
			dupCount, step.Request.InputArtifacts)
	}

	// The unique path must be present (injection contract — without this, a
	// completely missing injection implementation would still pass the dedup check).
	foundUnique := false
	for _, a := range step.Request.InputArtifacts {
		if a == "Stage-1/build-review-findings.md" {
			foundUnique = true
			break
		}
	}
	if !foundUnique {
		t.Errorf("auto-review CNA injection: want unique review output %q present in InputArtifacts (proves injection ran), got %v",
			"Stage-1/build-review-findings.md", step.Request.InputArtifacts)
	}
}

// TestNext_ArtifactInjection_AutoReview_Success_NoInjection verifies that a
// SUCCESS-routed dispatch never carries injected artifacts, even when
// LastOutputArtifacts is non-empty.
func TestNext_ArtifactInjection_AutoReview_Success_NoInjection(t *testing.T) {
	// planner-tdd-soft returns SUCCESS; routes to plan-review.
	// plan-review's table InputArtifacts do not include any "review-output.md".
	// Even with LastOutputArtifacts = ["review-output.md"], it must not appear.
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet("Implementation-Only")
	agents := newTestAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")
	state := stateAfter("PLANNING", "", "planner-tdd-soft#1", domain.StatusSUCCESS, 1)

	dec := engine.Next(engine.NextInput{
		Workflow:            aw,
		Stages:              stages,
		State:               state,
		LastResponse:        successResponse("planner-tdd-soft#1"),
		LastOutputArtifacts: []string{"review-output.md"},
		Agents:              agents,
		Seq:                 1,
		Now:                 fixedNow,
		Mode:                domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	for _, a := range step.Request.InputArtifacts {
		if a == "review-output.md" {
			t.Errorf("SUCCESS-routed dispatch must not carry injected artifact %q, but found it in InputArtifacts %v",
				"review-output.md", step.Request.InputArtifacts)
		}
	}
}

// TestNext_ArtifactInjection_Auto_CNA_NoInjection verifies that in auto mode
// a CNA response does not inject artifacts — it deviates, and Deviation carries
// no injected InputArtifacts.
func TestNext_ArtifactInjection_Auto_CNA_NoInjection(t *testing.T) {
	// In auto mode CNA deviates, so injection is irrelevant. This test also
	// confirms that the deviation decision itself is returned (not a dispatch).
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	stages := singleStageSet("TDD")
	agents := newTestAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "build-review", "tests-review-tdd",
		"implementation-tdd", "implementation-review",
	)
	state := stateAfterRow("EXECUTION", "Test.1", "build-review#9", domain.StatusCOMPLETED_NEEDS_ACTION, 9, 9)

	dec := engine.Next(engine.NextInput{
		Workflow:            aw,
		Stages:              stages,
		State:               state,
		LastResponse:        cnaResponse("build-review#9"),
		LastOutputArtifacts: []string{"Stage-1/build-review-tests.md"},
		Agents:              agents,
		Seq:                 9,
		Now:                 fixedNow,
		Mode:                domain.ExecutionModeAuto,
	})

	// auto mode CNA deviates — injection never happens on a non-Dispatch decision.
	requireDeviation(t, dec)
}

// TestNext_ArtifactInjection_AutoReview_CNA_InjectsAfterTableEntries verifies
// that injected artifacts are appended AFTER the table row's existing entries,
// not prepended or interleaved.
func TestNext_ArtifactInjection_AutoReview_CNA_InjectsAfterTableEntries(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	stages := singleStageSet("TDD")
	agents := newTestAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "build-review", "tests-review-tdd",
		"implementation-tdd", "implementation-review",
	)
	state := stateAfterRow("EXECUTION", "Test.1", "build-review#9", domain.StatusCOMPLETED_NEEDS_ACTION, 9, 9)
	injectedArtifact := "Stage-1/build-review-tests.md"
	reviewOutputs := []string{injectedArtifact}

	dec := engine.Next(engine.NextInput{
		Workflow:            aw,
		Stages:              stages,
		State:               state,
		LastResponse:        cnaResponse("build-review#9"),
		LastOutputArtifacts: reviewOutputs,
		Agents:              agents,
		Seq:                 9,
		Now:                 fixedNow,
		Mode:                domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	arts := step.Request.InputArtifacts

	// The injected artifact must appear after at least one table-provided artifact.
	injectedIdx := -1
	for i, a := range arts {
		if a == injectedArtifact {
			injectedIdx = i
			break
		}
	}
	if injectedIdx < 0 {
		t.Fatalf("injected artifact %q not found in InputArtifacts %v", injectedArtifact, arts)
	}
	if injectedIdx == 0 && len(arts) > 1 {
		// Injected is first and there are others — check if it's really a table entry
		// by seeing if a known table entry (e.g. Stage-1/Plan.md) comes after it.
		// For test-writer-tdd, table entries are: Stage-1/Plan.md, ContractsDesign.md, Stage-1/PlanProgress.md
		for _, tableEntry := range []string{"Stage-1/Plan.md", "ContractsDesign.md", "Stage-1/PlanProgress.md"} {
			for i, a := range arts {
				if a == tableEntry && i > injectedIdx {
					t.Errorf("injected artifact %q (index %d) appears before table entry %q (index %d); injected must come after table entries",
						injectedArtifact, injectedIdx, tableEntry, i)
				}
			}
		}
	}
	// Also verify: all known table entries appear before the injected artifact.
	for _, tableEntry := range []string{"Stage-1/Plan.md", "ContractsDesign.md", "Stage-1/PlanProgress.md"} {
		tableIdx := -1
		for i, a := range arts {
			if a == tableEntry {
				tableIdx = i
				break
			}
		}
		if tableIdx >= 0 && tableIdx > injectedIdx {
			t.Errorf("table entry %q (index %d) appears after injected artifact %q (index %d); injected must come last",
				tableEntry, tableIdx, injectedArtifact, injectedIdx)
		}
	}
}
// ===== Nil / empty LastOutputArtifacts on auto-review CNA path (Stage 2) =====

// TestNext_ArtifactInjection_AutoReview_CNA_NilLastOutputArtifacts verifies that
// when LastOutputArtifacts is nil the dispatched step's InputArtifacts match the
// table row exactly — no injection, no panic.
func TestNext_ArtifactInjection_AutoReview_CNA_NilLastOutputArtifacts(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	stages := singleStageSet("TDD")
	agents := newTestAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "build-review", "tests-review-tdd",
		"implementation-tdd", "implementation-review",
	)
	state := stateAfterRow("EXECUTION", "Test.1", "build-review#9", domain.StatusCOMPLETED_NEEDS_ACTION, 9, 9)

	dec := engine.Next(engine.NextInput{
		Workflow:            aw,
		Stages:              stages,
		State:               state,
		LastResponse:        cnaResponse("build-review#9"),
		LastOutputArtifacts: nil,
		Agents:              agents,
		Seq:                 9,
		Now:                 fixedNow,
		Mode:                domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	// With nil LastOutputArtifacts the table row's InputArtifacts must appear
	// and no extra entries should be present beyond what the table supplies.
	tableEntries := []string{"Stage-1/Plan.md", "ContractsDesign.md", "Stage-1/PlanProgress.md"}
	inputSet := make(map[string]bool)
	for _, a := range step.Request.InputArtifacts {
		inputSet[a] = true
	}
	for _, expected := range tableEntries {
		if !inputSet[expected] {
			t.Errorf("nil LastOutputArtifacts: table entry %q missing from InputArtifacts %v",
				expected, step.Request.InputArtifacts)
		}
	}
	// No artifact beyond the table entries should be present.
	tableSet := make(map[string]bool)
	for _, e := range tableEntries {
		tableSet[e] = true
	}
	for _, a := range step.Request.InputArtifacts {
		if !tableSet[a] {
			t.Errorf("nil LastOutputArtifacts: unexpected extra artifact %q in InputArtifacts %v (should match table row only)",
				a, step.Request.InputArtifacts)
		}
	}
}

// TestNext_ArtifactInjection_AutoReview_CNA_EmptyLastOutputArtifacts verifies that
// when LastOutputArtifacts is an empty slice the dispatched step's InputArtifacts
// match the table row exactly — same expectation as nil.
func TestNext_ArtifactInjection_AutoReview_CNA_EmptyLastOutputArtifacts(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	stages := singleStageSet("TDD")
	agents := newTestAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "build-review", "tests-review-tdd",
		"implementation-tdd", "implementation-review",
	)
	state := stateAfterRow("EXECUTION", "Test.1", "build-review#9", domain.StatusCOMPLETED_NEEDS_ACTION, 9, 9)

	dec := engine.Next(engine.NextInput{
		Workflow:            aw,
		Stages:              stages,
		State:               state,
		LastResponse:        cnaResponse("build-review#9"),
		LastOutputArtifacts: []string{},
		Agents:              agents,
		Seq:                 9,
		Now:                 fixedNow,
		Mode:                domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	tableEntries := []string{"Stage-1/Plan.md", "ContractsDesign.md", "Stage-1/PlanProgress.md"}
	inputSet := make(map[string]bool)
	for _, a := range step.Request.InputArtifacts {
		inputSet[a] = true
	}
	for _, expected := range tableEntries {
		if !inputSet[expected] {
			t.Errorf("empty LastOutputArtifacts: table entry %q missing from InputArtifacts %v",
				expected, step.Request.InputArtifacts)
		}
	}
	tableSet := make(map[string]bool)
	for _, e := range tableEntries {
		tableSet[e] = true
	}
	for _, a := range step.Request.InputArtifacts {
		if !tableSet[a] {
			t.Errorf("empty LastOutputArtifacts: unexpected extra artifact %q in InputArtifacts %v (should match table row only)",
				a, step.Request.InputArtifacts)
		}
	}
}
