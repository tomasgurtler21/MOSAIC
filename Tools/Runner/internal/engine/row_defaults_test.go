package engine_test

// Tests for the shared row default resolution used by engine-routed and
// consultation-routed dispatches.

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/engine"
)

func stagedDefaultsRow() domain.RoutingRow {
	return domain.RoutingRow{
		Index:           2,
		Agent:           "test-writer",
		Phase:           "EXECUTION.Test.[StageNumber]",
		PhaseParsed:     domain.PhaseParsed{Name: "EXECUTION", IsStaged: true, Group: "Test"},
		InputArtifacts:  []string{"Stage-*/Plan.md", "Stage-{StageNumber}/notes.md"},
		OutputArtifacts: []string{"Stage-{StageNumber}/tests.md", "Stage-*/Plan.md"},
	}
}

func defaultsStages(stage2HITL bool) *domain.StageSet {
	return &domain.StageSet{Entries: []domain.StageEntry{
		{Number: 1, Approach: "TDD"},
		{Number: 2, HITL: stage2HITL, Approach: "TDD"},
	}}
}

func TestResolveRowDefaults_StagedRow_ExpandsInputsKeepsOutputWildcardsAndOrsStageHITL(t *testing.T) {
	got, err := engine.ResolveRowDefaults(stagedDefaultsRow(), 2, defaultsStages(true), nil)

	if err != nil {
		t.Fatalf("ResolveRowDefaults: unexpected error %v", err)
	}
	wantIn := []string{"Stage-1/Plan.md", "Stage-2/Plan.md", "Stage-2/notes.md"}
	if !reflect.DeepEqual(got.InputArtifacts, wantIn) {
		t.Errorf("inputs = %v, want %v", got.InputArtifacts, wantIn)
	}
	wantOut := []string{"Stage-2/tests.md", "Stage-*/Plan.md"}
	if !reflect.DeepEqual(got.OutputArtifacts, wantOut) {
		t.Errorf("outputs = %v, want %v", got.OutputArtifacts, wantOut)
	}
	if !got.HITL {
		t.Errorf("HITL = false, want true from the plan stage")
	}
}

func TestResolveRowDefaults_RowHITLWithoutStageHITL_IsTrue(t *testing.T) {
	row := stagedDefaultsRow()
	row.HITL = true

	got, err := engine.ResolveRowDefaults(row, 1, defaultsStages(false), nil)

	if err != nil {
		t.Fatalf("ResolveRowDefaults: unexpected error %v", err)
	}
	if !got.HITL {
		t.Errorf("HITL = false, want true from the row")
	}
}

func TestResolveRowDefaults_NoHITLAnywhere_IsFalse(t *testing.T) {
	got, err := engine.ResolveRowDefaults(stagedDefaultsRow(), 1, defaultsStages(true), nil)

	if err != nil {
		t.Fatalf("ResolveRowDefaults: unexpected error %v", err)
	}
	if got.HITL {
		t.Errorf("HITL = true for stage 1 (no HITL on row or stage 1), want false")
	}
	if len(got.InputArtifacts) == 0 {
		t.Errorf("want resolved inputs for a valid staged row, got none")
	}
}

func TestResolveRowDefaults_NonStagedRow_UsesRefreshedStagesForExpansion(t *testing.T) {
	row := domain.RoutingRow{
		Agent:          "final-review",
		Phase:          "REVIEW",
		PhaseParsed:    domain.PhaseParsed{Name: "REVIEW"},
		InputArtifacts: []string{"Stage-*/Plan.md"},
	}
	refreshed := &domain.StageSet{Entries: []domain.StageEntry{{Number: 1}, {Number: 2}, {Number: 3}}}

	got, err := engine.ResolveRowDefaults(row, 0, defaultsStages(false), refreshed)

	if err != nil {
		t.Fatalf("ResolveRowDefaults: unexpected error %v", err)
	}
	want := []string{"Stage-1/Plan.md", "Stage-2/Plan.md", "Stage-3/Plan.md"}
	if !reflect.DeepEqual(got.InputArtifacts, want) {
		t.Errorf("inputs = %v, want %v", got.InputArtifacts, want)
	}
}

func TestResolveRowDefaults_NonStagedRowWithRowHITL_IsTrue(t *testing.T) {
	row := domain.RoutingRow{
		Agent:       "final-review",
		Phase:       "REVIEW",
		PhaseParsed: domain.PhaseParsed{Name: "REVIEW"},
		HITL:        true,
	}

	got, err := engine.ResolveRowDefaults(row, 0, defaultsStages(false), nil)

	if err != nil {
		t.Fatalf("ResolveRowDefaults: unexpected error %v", err)
	}
	if !got.HITL {
		t.Errorf("HITL = false, want true from the row")
	}
}

func TestResolveRowDefaults_StageArgumentErrors(t *testing.T) {
	nonStaged := domain.RoutingRow{Agent: "final-review", PhaseParsed: domain.PhaseParsed{Name: "REVIEW"}}
	cases := []struct {
		name   string
		row    domain.RoutingRow
		stage  domain.StageNumber
		stages *domain.StageSet
		want   error
	}{
		{"staged row without stage", stagedDefaultsRow(), 0, defaultsStages(false), engine.ErrRowDefaultsStageRequired},
		{"staged row with stage missing from set", stagedDefaultsRow(), 7, defaultsStages(false), engine.ErrRowDefaultsStageNotInSet},
		{"staged row with nil stage set", stagedDefaultsRow(), 1, nil, engine.ErrRowDefaultsStageNotInSet},
		{"non-staged row with a stage", nonStaged, 1, defaultsStages(false), engine.ErrRowDefaultsStageOnNonStagedRow},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := engine.ResolveRowDefaults(tc.row, tc.stage, tc.stages, nil)

			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want one wrapping %v", err, tc.want)
			}
			if !reflect.DeepEqual(got, domain.DispatchDefaults{}) {
				t.Errorf("want zero defaults on error, got %+v", got)
			}
		})
	}
}

func TestResolveRowDefaults_UnresolvablePattern_ReturnsErrorNamingAgentAndPattern(t *testing.T) {
	row := domain.RoutingRow{
		Agent:          "final-review",
		PhaseParsed:    domain.PhaseParsed{Name: "REVIEW"},
		InputArtifacts: []string{"Stage-{StageNumber}/Plan.md"},
	}

	_, err := engine.ResolveRowDefaults(row, 0, defaultsStages(false), nil)

	if err == nil {
		t.Fatalf("want an error for a {StageNumber} pattern on a non-staged row, got nil")
	}
	for _, want := range []string{"final-review", "{StageNumber}"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err.Error(), want)
		}
	}
}
