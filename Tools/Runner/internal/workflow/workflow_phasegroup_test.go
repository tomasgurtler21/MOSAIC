package workflow_test

import (
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/workflow"
)

func TestParse_GroupedPhase_Test_GroupField(t *testing.T) {
	// EXECUTION.Test.[StageNumber] must populate PhaseParsed.Group with "Test".
	table := mustParseGroupedPhases(t)

	got := table.Rows[2].PhaseParsed.Group
	if got != "Test" {
		t.Errorf("Rows[2].PhaseParsed.Group: want %q, got %q", "Test", got)
	}
}

func TestParse_GroupedPhase_Implementation_GroupField(t *testing.T) {
	// EXECUTION.Implementation.[StageNumber] must populate PhaseParsed.Group
	// with "Implementation".
	table := mustParseGroupedPhases(t)

	got := table.Rows[3].PhaseParsed.Group
	if got != "Implementation" {
		t.Errorf("Rows[3].PhaseParsed.Group: want %q, got %q", "Implementation", got)
	}
}

func TestParse_BareExecutionPhase_GroupField_IsEmpty(t *testing.T) {
	// A bare EXECUTION.[StageNumber] row (no group segment) must have an empty Group.
	table := mustParseGroupedPhases(t)

	got := table.Rows[1].PhaseParsed.Group
	if got != "" {
		t.Errorf("Rows[1].PhaseParsed.Group: want %q for bare EXECUTION phase, got %q", "", got)
	}
}

func TestParse_NonExecutionPhase_GroupField_IsEmpty(t *testing.T) {
	// A non-staged phase (RESEARCH) must have an empty Group.
	table := mustParseGroupedPhases(t)

	got := table.Rows[0].PhaseParsed.Group
	if got != "" {
		t.Errorf("Rows[0].PhaseParsed.Group: want %q for RESEARCH phase, got %q", "", got)
	}
}

func TestParse_GroupedPhase_PhaseName_StaysEXECUTION(t *testing.T) {
	// When the phase string has a group segment, PhaseParsed.Name must still
	// be "EXECUTION" — compat's staged-phase-name guard depends on this.
	table := mustParseGroupedPhases(t)

	got := table.Rows[2].PhaseParsed.Name
	if got != "EXECUTION" {
		t.Errorf("Rows[2].PhaseParsed.Name: want %q for EXECUTION.Test.[StageNumber], got %q", "EXECUTION", got)
	}
}

func TestParse_GroupedPhase_IsStaged_True(t *testing.T) {
	// A grouped EXECUTION phase must be flagged as staged.
	table := mustParseGroupedPhases(t)

	if !table.Rows[2].PhaseParsed.IsStaged {
		t.Error("Rows[2] (EXECUTION.Test.[StageNumber]): PhaseParsed.IsStaged must be true")
	}
}

func TestParse_GroupedPhase_Phase_LiteralPreserved(t *testing.T) {
	// The Phase field must carry the literal cell text, including the group segment.
	table := mustParseGroupedPhases(t)

	got := table.Rows[2].Phase
	want := "EXECUTION.Test.[StageNumber]"
	if got != want {
		t.Errorf("Rows[2].Phase: want %q, got %q", want, got)
	}
}

func TestParse_MalformedPhase_GroupWithoutStageNumber_ReturnsError(t *testing.T) {
	// EXECUTION.Test with no [StageNumber] following the group token must be refused
	// (the group segment exists but the required final segment is missing).
	info := domain.WorkflowInfo{ID: "malformed-phase", Version: "1.0"}

	_, err := workflow.Parse([]byte(malformedPhaseGroupNoStageContent), info)

	if err == nil {
		t.Fatal("Parse must return an error when EXECUTION.{Group} has no [StageNumber] segment")
	}
}

func TestParse_MalformedPhase_GroupWithoutStageNumber_ReturnsRefusalError(t *testing.T) {
	info := domain.WorkflowInfo{ID: "malformed-phase", Version: "1.0"}

	_, err := workflow.Parse([]byte(malformedPhaseGroupNoStageContent), info)

	re := asRefusalError(t, err)
	if !strings.Contains(re.Reason, "expected") {
		t.Errorf("RefusalError.Reason should describe expected EXECUTION phase format (P1), got %q", re.Reason)
	}
}

func TestParse_MalformedPhase_EmptyGroupSegment_ReturnsError(t *testing.T) {
	// EXECUTION..[StageNumber] with an empty segment between the two dots must
	// be refused (the group token must be a non-empty CamelCase string).
	info := domain.WorkflowInfo{ID: "malformed-phase", Version: "1.0"}

	_, err := workflow.Parse([]byte(malformedPhaseEmptyGroupContent), info)

	if err == nil {
		t.Fatal("Parse must return an error when the EXECUTION phase has an empty group segment")
	}
}

func TestParse_MalformedPhase_EmptyGroupSegment_ReturnsRefusalError(t *testing.T) {
	info := domain.WorkflowInfo{ID: "malformed-phase", Version: "1.0"}

	_, err := workflow.Parse([]byte(malformedPhaseEmptyGroupContent), info)

	re := asRefusalError(t, err)
	if !strings.Contains(re.Reason, "group segment") {
		t.Errorf("RefusalError.Reason should mention group segment (P2), got %q", re.Reason)
	}
}

func TestParse_MalformedPhase_StageNotBracketed_ReturnsError(t *testing.T) {
	// EXECUTION.Test.1 where "1" does not begin with "[" must be refused —
	// the stage segment must always start with "[".
	info := domain.WorkflowInfo{ID: "malformed-phase", Version: "1.0"}

	_, err := workflow.Parse([]byte(malformedPhaseStageNotBracketedContent), info)

	if err == nil {
		t.Fatal("Parse must return an error when the stage segment does not begin with '['")
	}
}

func TestParse_MalformedPhase_StageNotBracketed_ReturnsRefusalError(t *testing.T) {
	info := domain.WorkflowInfo{ID: "malformed-phase", Version: "1.0"}

	_, err := workflow.Parse([]byte(malformedPhaseStageNotBracketedContent), info)

	re := asRefusalError(t, err)
	if !strings.Contains(re.Reason, "expected") {
		t.Errorf("RefusalError.Reason should describe expected EXECUTION phase format (P1), got %q", re.Reason)
	}
}

func TestParse_MalformedPhase_RefusalError_ComponentIsWorkflow(t *testing.T) {
	// Phase-parse refusals must name "workflow" as the component.
	info := domain.WorkflowInfo{ID: "malformed-phase", Version: "1.0"}

	_, err := workflow.Parse([]byte(malformedPhaseGroupNoStageContent), info)

	re := asRefusalError(t, err)
	if re.Component != "workflow" {
		t.Errorf("RefusalError.Component: want %q, got %q", "workflow", re.Component)
	}
}

func TestParse_MalformedPhase_RefusalError_ResourceNamesWorkflow(t *testing.T) {
	// Phase-parse refusals must carry a non-empty Resource naming the workflow.
	info := domain.WorkflowInfo{ID: "malformed-phase-workflow", Version: "1.0"}

	_, err := workflow.Parse([]byte(malformedPhaseGroupNoStageContent), info)

	re := asRefusalError(t, err)
	if re.Resource == "" {
		t.Error("RefusalError.Resource must name the workflow, not be empty")
	}
}

func TestParse_MalformedPhase_WhitespaceGroupSegment_ReturnsError(t *testing.T) {
	// A group segment that contains whitespace (non-empty but invalid) must be
	// refused. This is the whitespace branch of P2: the segment is non-empty but
	// fails because it contains an internal space, unlike the empty-segment case.
	info := domain.WorkflowInfo{ID: "malformed-phase", Version: "1.0"}

	_, err := workflow.Parse([]byte(malformedPhaseWhitespaceGroupContent), info)

	if err == nil {
		t.Fatal("Parse must return an error when the EXECUTION phase group segment contains whitespace")
	}
}

func TestParse_MalformedPhase_WhitespaceGroupSegment_ReturnsRefusalError(t *testing.T) {
	info := domain.WorkflowInfo{ID: "malformed-phase", Version: "1.0"}

	_, err := workflow.Parse([]byte(malformedPhaseWhitespaceGroupContent), info)

	re := asRefusalError(t, err)
	if !strings.Contains(re.Reason, "group segment") {
		t.Errorf("RefusalError.Reason should mention group segment (P2), got %q", re.Reason)
	}
}
