package engine_test

// Tests that creator-artifact injection on a review route-back treats a registry
// entry in the recorded (unprefixed) form and one in the run-prefixed form as
// the same artifact: the creator's own output is injected once either way.

import (
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/engine"
)

const creatorPathRunID = "20261007T174011Z-4f54"

// creatorInjectionInputs runs a route-back from tests-review-tdd to
// test-writer-tdd with the given registry and returns the dispatched inputs.
func creatorInjectionInputs(t *testing.T, registry []domain.ArtifactRegistryEntry) []string {
	t.Helper()
	aw := mustParseAndAdmit(t, creatorInjectionContent, "creator-injection", "1.0")
	agents := newTestAgents("test-writer-tdd", "tests-review-tdd", "implementation-tdd")
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "tests-review-tdd#5", domain.StatusCOMPLETED_NEEDS_ACTION, 5)
	state.RunID = creatorPathRunID

	dec := engine.Next(engine.NextInput{
		Workflow:            aw,
		Stages:              singleStageSet("TDD"),
		State:               state,
		LastResponse:        cnaResponse("tests-review-tdd#5"),
		LastOutputArtifacts: []string{"Stage-1/tests-review-tdd.md"},
		Agents:              agents,
		Seq:                 5,
		Now:                 fixedNow,
		Mode:                domain.ExecutionModeAutoReview,
		ArtifactRegistry:    registry,
	})

	return requireDispatch(t, dec).Request.InputArtifacts
}

// countForm counts paths denoting want in either form; the engine returns input
// paths relative to the run folder and the session makes them run-scoped (see the
// integration tests), so only the identity of the artifact is compared here.
func countForm(paths []string, want string) int {
	n := 0
	for _, p := range paths {
		if domain.StripRunPrefix(creatorPathRunID, p) == want {
			n++
		}
	}
	return n
}

// Regression coverage: the unprefixed registry form is already accepted.
func TestNext_CreatorInjection_UnprefixedRegistryEntry_InjectedOnce(t *testing.T) {
	registry := []domain.ArtifactRegistryEntry{
		{Artifact: "Stage-1/PlanProgress.md", CreatedIn: "Test.1", CreatedBy: "test-writer-tdd#1"},
	}

	inputs := creatorInjectionInputs(t, registry)

	if got := countForm(inputs, "Stage-1/PlanProgress.md"); got != 1 {
		t.Errorf("want the creator's output injected once, got %d in %v", got, inputs)
	}
}

func TestNext_CreatorInjection_PrefixedRegistryEntry_InjectedOnce(t *testing.T) {
	registry := []domain.ArtifactRegistryEntry{
		{Artifact: domain.RunScopedFolder(creatorPathRunID) + "/Stage-1/PlanProgress.md", CreatedIn: "Test.1", CreatedBy: "test-writer-tdd#1"},
	}

	inputs := creatorInjectionInputs(t, registry)

	if got := countForm(inputs, "Stage-1/PlanProgress.md"); got != 1 {
		t.Errorf("want the creator's output injected once, got %d in %v", got, inputs)
	}
}

func TestNext_CreatorInjection_RegistryEntryOfAnotherAgent_NotInjected(t *testing.T) {
	registry := []domain.ArtifactRegistryEntry{
		{Artifact: "Stage-1/Other.md", CreatedIn: "Test.1", CreatedBy: "implementation-tdd#2"},
	}

	inputs := creatorInjectionInputs(t, registry)

	if got := countForm(inputs, "Stage-1/Other.md"); got != 0 {
		t.Errorf("an artifact that is not an output of the creator row must not be injected, got %v", inputs)
	}
}
