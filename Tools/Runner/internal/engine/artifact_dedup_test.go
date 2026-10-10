package engine_test

// Tests that a route-back dispatch never lists the same artifact twice when
// the table row's entries and the injected review outputs name one file in
// different forms (with and without the run folder prefix).

import (
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/engine"
)

const dedupRunID = "20261007T174011Z-4f54"

var dedupRunPrefix = domain.RunScopedFolder(dedupRunID) + "/"

// routeBackInputs runs an auto-review route-back from build-review to
// test-writer-tdd with the given review outputs and returns the dispatched
// step's input artifacts.
func routeBackInputs(t *testing.T, reviewOutputs []string) []string {
	t.Helper()
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	agents := newTestAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "build-review", "tests-review-tdd",
		"implementation-tdd", "implementation-review",
	)
	state := stateAfterRow("EXECUTION", "Test.1", "build-review#9", domain.StatusCOMPLETED_NEEDS_ACTION, 9, 9)
	state.RunID = dedupRunID

	dec := engine.Next(engine.NextInput{
		Workflow:            aw,
		Stages:              singleStageSet("TDD"),
		State:               state,
		LastResponse:        cnaResponse("build-review#9"),
		LastOutputArtifacts: reviewOutputs,
		Agents:              agents,
		Seq:                 9,
		Now:                 fixedNow,
		Mode:                domain.ExecutionModeAutoReview,
	})

	return requireDispatch(t, dec).Request.InputArtifacts
}

// countNormalised counts the entries of paths that name the same file as want,
// ignoring the run folder prefix.
func countNormalised(paths []string, want string) int {
	n := 0
	for _, p := range paths {
		if strings.TrimPrefix(p, dedupRunPrefix) == want {
			n++
		}
	}
	return n
}

func TestNext_RouteBack_PrefixedReviewOutputMatchingTableInput_ListedOnce(t *testing.T) {
	// Stage-1/Plan.md is a table input of test-writer-tdd (unprefixed); the
	// review output names the same file with the run prefix.
	inputs := routeBackInputs(t, []string{dedupRunPrefix + "Stage-1/Plan.md", dedupRunPrefix + "Stage-1/findings.md"})

	if got := countNormalised(inputs, "Stage-1/Plan.md"); got != 1 {
		t.Errorf("want Stage-1/Plan.md exactly once, got %d in %v", got, inputs)
	}
	if got := countNormalised(inputs, "Stage-1/findings.md"); got != 1 {
		t.Errorf("want the new review output Stage-1/findings.md once (injection ran), got %d in %v", got, inputs)
	}
}

func TestNext_RouteBack_ReviewOutputInBothFormsInOneList_ListedOnce(t *testing.T) {
	inputs := routeBackInputs(t, []string{"Stage-1/findings.md", dedupRunPrefix + "Stage-1/findings.md"})

	if got := countNormalised(inputs, "Stage-1/findings.md"); got != 1 {
		t.Errorf("want Stage-1/findings.md exactly once, got %d in %v", got, inputs)
	}
}

func TestNext_RouteBack_FirstOccurrenceKeepsItsForm(t *testing.T) {
	inputs := routeBackInputs(t, []string{dedupRunPrefix + "Stage-1/findings.md", "Stage-1/findings.md"})

	found := false
	for _, p := range inputs {
		found = found || p == dedupRunPrefix+"Stage-1/findings.md"
	}
	if !found {
		t.Errorf("want the first (prefixed) form of the review output kept, got %v", inputs)
	}
	if got := countNormalised(inputs, "Stage-1/findings.md"); got != 1 {
		t.Errorf("want one occurrence, got %d in %v", got, inputs)
	}
}

func TestNext_RouteBack_UnprefixedReviewOutputMatchingTableInput_ListedOnce_Regression(t *testing.T) {
	// Regression coverage: identical plain forms were already de-duplicated.
	inputs := routeBackInputs(t, []string{"Stage-1/Plan.md"})

	if got := countNormalised(inputs, "Stage-1/Plan.md"); got != 1 {
		t.Errorf("want Stage-1/Plan.md exactly once, got %d in %v", got, inputs)
	}
}
