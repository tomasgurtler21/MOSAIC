package plan_test

// build_parse_failed_reason_test.go verifies that the CONFLICT reason for a deployed agent file
// that could not be parsed carries the parse problem reported by the probe, so the author can
// locate the offending character.
//
// Verified behaviours:
//   - A parse-failed deployed state with a non-empty ParseProblem yields a CONFLICT item whose
//     reason contains the problem text, including its line number and visible excerpt.
//   - The reason keeps the existing "could not be read/parsed" prefix.
//   - A parse-failed deployed state with no ParseProblem keeps the existing reason unchanged.

import (
	"context"
	"strings"
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/plan"
)

// buildParseFailedItem runs the real planner for one agent whose deployed state is the given
// parse-failed state, with no manifest available, and returns the agent's plan item.
func buildParseFailedItem(t *testing.T, state domain.DeployedArtifactState) domain.PlanItem {
	t.Helper()
	agent := makeAgent("corrupted-agent", "2.0")
	targetPath := "agents/corrupted-agent.md"

	input := buildParseFailedInput(absentSnapshot(), map[string]domain.DeployedArtifactState{
		targetPath: state,
	})
	result, err := plan.New().Build(context.Background(), input)
	if err != nil {
		t.Fatalf("Build returned unexpected error: %v", err)
	}
	item, found := findItem(result.Items, agent.Key)
	if !found {
		t.Fatalf("plan item for %q not found", agent.Key)
	}
	return item
}

func TestClassifyAgentItem_ParseFailed_ReasonIncludesParseProblemAndLine(t *testing.T) {
	// Arrange
	const problem = `frontmatter: line 7: malformed fence: "---<U+200B>"`
	state := parseFailedDeployedState("sha256:currentbytes")
	state.ParseProblem = problem

	// Act
	item := buildParseFailedItem(t, state)

	// Assert
	if item.Action != domain.ActionConflict {
		t.Fatalf("Action = %v, want conflict", item.Action)
	}
	if !strings.Contains(item.Reason, "could not be read/parsed") {
		t.Errorf("Reason = %q; want it to keep the 'could not be read/parsed' wording", item.Reason)
	}
	if !strings.Contains(item.Reason, problem) {
		t.Errorf("Reason = %q; want it to contain the parse problem %q", item.Reason, problem)
	}
	if !strings.Contains(item.Reason, "line 7") {
		t.Errorf("Reason = %q; want it to contain the line number", item.Reason)
	}
	if !strings.Contains(item.Reason, "<U+200B>") {
		t.Errorf("Reason = %q; want it to contain the visible excerpt", item.Reason)
	}
}

func TestClassifyAgentItem_ParseFailed_NoFrontmatterProblemAppearsInReason(t *testing.T) {
	// Arrange
	state := parseFailedDeployedState("sha256:currentbytes")
	state.ParseProblem = "no frontmatter"

	// Act
	item := buildParseFailedItem(t, state)

	// Assert
	if !strings.Contains(item.Reason, "no frontmatter") {
		t.Errorf("Reason = %q; want it to contain the parse problem 'no frontmatter'", item.Reason)
	}
}

func TestClassifyAgentItem_ParseFailed_EmptyParseProblem_KeepsExistingReason(t *testing.T) {
	// Arrange
	state := parseFailedDeployedState("sha256:currentbytes")

	// Act
	item := buildParseFailedItem(t, state)

	// Assert
	if item.Reason != "deployed file could not be read/parsed" {
		t.Errorf("Reason = %q; want the unchanged reason when no parse problem is carried", item.Reason)
	}
}
