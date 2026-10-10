package domain_test

// Tests for domain.ValidateDispatchTarget: the shared check of a consultant's
// proposed row and stage against the routing table and the current stage set.

import (
	"errors"
	"testing"

	"mosaic-run/internal/domain"
)

// dispatchTargetTable returns a four-row table (1-based row numbers):
//
//	1 researcher (RESEARCH, non-staged)
//	2 dev        (EXECUTION.Test, staged)
//	3 dev        (EXECUTION.Implementation, staged)
//	4 reviewer   (REVIEW, non-staged)
func dispatchTargetTable() domain.RoutingTable {
	return domain.RoutingTable{Rows: []domain.RoutingRow{
		{Index: 0, Phase: "RESEARCH", Agent: "researcher", PhaseParsed: domain.PhaseParsed{Name: "RESEARCH"}},
		{Index: 1, Phase: "EXECUTION.Test.[StageNumber]", Agent: "dev",
			PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true, Group: "Test"}},
		{Index: 2, Phase: "EXECUTION.Implementation.[StageNumber]", Agent: "dev",
			PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true, Group: "Implementation"}},
		{Index: 3, Phase: "REVIEW", Agent: "reviewer", PhaseParsed: domain.PhaseParsed{Name: "REVIEW"}},
	}}
}

func stageSetOf(n int) *domain.StageSet {
	set := &domain.StageSet{}
	for i := 1; i <= n; i++ {
		set.Entries = append(set.Entries, domain.StageEntry{Number: domain.StageNumber(i)})
	}
	return set
}

func assertTargetReason(t *testing.T, err error, want domain.DispatchTargetReason) {
	t.Helper()
	var te *domain.DispatchTargetError
	if !errors.As(err, &te) {
		t.Fatalf("want *DispatchTargetError with reason %q, got %T: %v", want, err, err)
	}
	if te.Reason != want {
		t.Errorf("want reason %q, got %q", want, te.Reason)
	}
}

func TestValidateDispatchTarget_ValidNonStagedRow(t *testing.T) {
	got, err := domain.ValidateDispatchTarget(dispatchTargetTable(), stageSetOf(2),
		domain.DispatchTarget{Agent: "reviewer", Row: 4})

	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if got.Row.Index != 3 || got.Row.Agent != "reviewer" {
		t.Errorf("want resolved row index 3 (reviewer), got index %d agent %q", got.Row.Index, got.Row.Agent)
	}
	if got.StageNumber != 0 || got.RecordedStage != "" {
		t.Errorf("want no stage for a non-staged row, got %d / %q", got.StageNumber, got.RecordedStage)
	}
}

func TestValidateDispatchTarget_ValidStagedRowResolvesRowAndRecordedStage(t *testing.T) {
	got, err := domain.ValidateDispatchTarget(dispatchTargetTable(), stageSetOf(3),
		domain.DispatchTarget{Agent: "dev", Row: 3, Stage: 2})

	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if got.Row.Index != 2 {
		t.Errorf("want the named row (index 2), not the first row of the agent, got index %d", got.Row.Index)
	}
	if got.StageNumber != 2 {
		t.Errorf("want StageNumber 2, got %d", got.StageNumber)
	}
	if got.RecordedStage != "Implementation.2" {
		t.Errorf("want RecordedStage %q, got %q", "Implementation.2", got.RecordedStage)
	}
}

func TestValidateDispatchTarget_StageGroupMatchingRowIsAccepted(t *testing.T) {
	got, err := domain.ValidateDispatchTarget(dispatchTargetTable(), stageSetOf(3),
		domain.DispatchTarget{Agent: "dev", Row: 2, Stage: 1, StageGroup: "Test"})

	if err != nil {
		t.Fatalf("want no error for a stage group equal to the row's group, got %v", err)
	}
	if got.RecordedStage != "Test.1" {
		t.Errorf("want RecordedStage %q, got %q", "Test.1", got.RecordedStage)
	}
}

func TestValidateDispatchTarget_StageGroupOfAnotherGroupIsRejected(t *testing.T) {
	_, err := domain.ValidateDispatchTarget(dispatchTargetTable(), stageSetOf(3),
		domain.DispatchTarget{Agent: "dev", Row: 3, Stage: 1, StageGroup: "Test"})

	assertTargetReason(t, err, domain.ReasonStageGroupMismatch)
}

func TestValidateDispatchTarget_MissingRowIsRejected(t *testing.T) {
	_, err := domain.ValidateDispatchTarget(dispatchTargetTable(), stageSetOf(2),
		domain.DispatchTarget{Agent: "researcher", Row: domain.NoWorkflowRow})

	assertTargetReason(t, err, domain.ReasonMissingRow)
}

func TestValidateDispatchTarget_UnknownRowIsRejected(t *testing.T) {
	for _, row := range []domain.WorkflowRow{5, 99, -1} {
		_, err := domain.ValidateDispatchTarget(dispatchTargetTable(), stageSetOf(2),
			domain.DispatchTarget{Agent: "researcher", Row: row})

		assertTargetReason(t, err, domain.ReasonUnknownRow)
	}
}

func TestValidateDispatchTarget_AgentNotMatchingRowIsRejected(t *testing.T) {
	_, err := domain.ValidateDispatchTarget(dispatchTargetTable(), stageSetOf(2),
		domain.DispatchTarget{Agent: "reviewer", Row: 1})

	assertTargetReason(t, err, domain.ReasonAgentMismatch)
}

func TestValidateDispatchTarget_StagedRowWithoutStageIsRejected(t *testing.T) {
	_, err := domain.ValidateDispatchTarget(dispatchTargetTable(), stageSetOf(2),
		domain.DispatchTarget{Agent: "dev", Row: 2})

	assertTargetReason(t, err, domain.ReasonMissingStage)
}

func TestValidateDispatchTarget_StageOutsideCurrentSetIsRejected(t *testing.T) {
	_, err := domain.ValidateDispatchTarget(dispatchTargetTable(), stageSetOf(2),
		domain.DispatchTarget{Agent: "dev", Row: 2, Stage: 3})

	assertTargetReason(t, err, domain.ReasonUnknownStage)
}

func TestValidateDispatchTarget_StagedRowWithNilStageSetIsRejected(t *testing.T) {
	_, err := domain.ValidateDispatchTarget(dispatchTargetTable(), nil,
		domain.DispatchTarget{Agent: "dev", Row: 2, Stage: 1})

	assertTargetReason(t, err, domain.ReasonUnknownStage)
}

// A stage that appeared mid-run is valid as soon as the stage set holds it,
// and the same target is invalid against the earlier, smaller set.
func TestValidateDispatchTarget_StageAddedMidRunIsAccepted(t *testing.T) {
	target := domain.DispatchTarget{Agent: "dev", Row: 3, Stage: 3}

	_, errBefore := domain.ValidateDispatchTarget(dispatchTargetTable(), stageSetOf(2), target)
	got, errAfter := domain.ValidateDispatchTarget(dispatchTargetTable(), stageSetOf(3), target)

	assertTargetReason(t, errBefore, domain.ReasonUnknownStage)
	if errAfter != nil {
		t.Fatalf("want stage 3 accepted once the stage set holds it, got %v", errAfter)
	}
	if got.RecordedStage != "Implementation.3" {
		t.Errorf("want RecordedStage %q, got %q", "Implementation.3", got.RecordedStage)
	}
}

func TestValidateDispatchTarget_StageOnNonStagedRowIsRejectedNotDropped(t *testing.T) {
	cases := []struct {
		name   string
		target domain.DispatchTarget
	}{
		{"bare stage number", domain.DispatchTarget{Agent: "researcher", Row: 1, Stage: 1}},
		{"stage group only", domain.DispatchTarget{Agent: "researcher", Row: 1, StageGroup: "Test"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := domain.ValidateDispatchTarget(dispatchTargetTable(), stageSetOf(2), tc.target)

			assertTargetReason(t, err, domain.ReasonStageOnNonStagedRow)
		})
	}
}

// Checks run in the documented order: a missing row is reported before the
// agent/row relation is looked at.
func TestValidateDispatchTarget_MissingRowReportedBeforeAgentMismatch(t *testing.T) {
	_, err := domain.ValidateDispatchTarget(dispatchTargetTable(), stageSetOf(2),
		domain.DispatchTarget{Agent: "no-such-agent"})

	assertTargetReason(t, err, domain.ReasonMissingRow)
}
