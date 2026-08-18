package domain_test

// Tests for domain.FormatStageValue, domain.ParseStageValue, and
// domain.RecordedPhaseName -- the pure functions that own the recorded
// stage/phase value's spelling (canonical phase and stage recording).
//
// Coverage:
//
//   FormatStageValue:
//   - Grouped row -> "{group}.{n}" (e.g. "Test.1").
//   - Ungrouped row (empty group) -> "{n}" (e.g. "4").
//   - Zero stage number -> "".
//
//   ParseStageValue:
//   - Target grouped form ("Test.1") -> group="Test", n=1, ok=true.
//   - Target ungrouped form ("1") -> group="", n=1, ok=true.
//   - Legacy form ("Stage-1") -> group="", n=1, ok=true.
//   - Empty string -> group="", n=0, ok=false.
//   - Unrecognised value -> ok=false, never panics.
//
//   RecordedPhaseName:
//   - Target bare form ("EXECUTION") -> "EXECUTION" (no-op).
//   - Legacy grouped form ("EXECUTION.Test.[StageNumber]") -> "EXECUTION".
//   - Legacy ungrouped form ("EXECUTION.[StageNumber]") -> "EXECUTION".
//   - Empty string -> "".
//   - A non-EXECUTION phase is returned unchanged.

import (
	"testing"

	"mosaic-run/internal/domain"
)

func TestFormatStageValue_Grouped_JoinsGroupAndNumber(t *testing.T) {
	got := domain.FormatStageValue("Test", 1)
	if got != "Test.1" {
		t.Errorf("FormatStageValue(Test, 1): want %q, got %q", "Test.1", got)
	}
}

func TestFormatStageValue_Grouped_DifferentGroup(t *testing.T) {
	got := domain.FormatStageValue("Implementation", 4)
	if got != "Implementation.4" {
		t.Errorf("FormatStageValue(Implementation, 4): want %q, got %q", "Implementation.4", got)
	}
}

func TestFormatStageValue_Ungrouped_PlainNumber(t *testing.T) {
	got := domain.FormatStageValue("", 4)
	if got != "4" {
		t.Errorf("FormatStageValue(\"\", 4): want %q, got %q", "4", got)
	}
}

func TestFormatStageValue_ZeroStageNumber_ReturnsEmpty(t *testing.T) {
	got := domain.FormatStageValue("Test", 0)
	if got != "" {
		t.Errorf("FormatStageValue(Test, 0): want %q (non-staged), got %q", "", got)
	}
}

func TestParseStageValue_TargetGroupedForm(t *testing.T) {
	group, n, ok := domain.ParseStageValue("Test.1")
	if !ok {
		t.Fatalf("ParseStageValue(%q): want ok=true, got false", "Test.1")
	}
	if group != "Test" {
		t.Errorf("ParseStageValue(%q): group: want %q, got %q", "Test.1", "Test", group)
	}
	if n != 1 {
		t.Errorf("ParseStageValue(%q): n: want 1, got %d", "Test.1", n)
	}
}

func TestParseStageValue_TargetGroupedForm_ImplementationHigherStage(t *testing.T) {
	group, n, ok := domain.ParseStageValue("Implementation.4")
	if !ok {
		t.Fatalf("ParseStageValue(%q): want ok=true, got false", "Implementation.4")
	}
	if group != "Implementation" {
		t.Errorf("ParseStageValue(%q): group: want %q, got %q", "Implementation.4", "Implementation", group)
	}
	if n != 4 {
		t.Errorf("ParseStageValue(%q): n: want 4, got %d", "Implementation.4", n)
	}
}

func TestParseStageValue_TargetUngroupedForm(t *testing.T) {
	group, n, ok := domain.ParseStageValue("1")
	if !ok {
		t.Fatalf("ParseStageValue(%q): want ok=true, got false", "1")
	}
	if group != "" {
		t.Errorf("ParseStageValue(%q): group: want %q (ungrouped), got %q", "1", "", group)
	}
	if n != 1 {
		t.Errorf("ParseStageValue(%q): n: want 1, got %d", "1", n)
	}
}

func TestParseStageValue_LegacyForm_RecoversNumberAndNoGroup(t *testing.T) {
	// Legacy runner versions wrote "Stage-N", with no group information --
	// this must still resolve, per AC4.5.
	group, n, ok := domain.ParseStageValue("Stage-1")
	if !ok {
		t.Fatalf("ParseStageValue(%q): want ok=true (legacy form), got false", "Stage-1")
	}
	if group != "" {
		t.Errorf("ParseStageValue(%q): group: want %q (legacy carries no group), got %q", "Stage-1", "", group)
	}
	if n != 1 {
		t.Errorf("ParseStageValue(%q): n: want 1, got %d", "Stage-1", n)
	}
}

func TestParseStageValue_Empty_ReturnsNotOk(t *testing.T) {
	group, n, ok := domain.ParseStageValue("")
	if ok {
		t.Fatalf("ParseStageValue(\"\"): want ok=false, got true (group=%q, n=%d)", group, n)
	}
	if group != "" || n != 0 {
		t.Errorf("ParseStageValue(\"\"): want zero group/number even though ok=false, got group=%q n=%d", group, n)
	}
}

func TestParseStageValue_Unrecognised_ReturnsNotOkWithoutPanicking(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("ParseStageValue must never panic; recovered: %v", r)
		}
	}()
	_, _, ok := domain.ParseStageValue("not-a-stage-value")
	if ok {
		t.Error("ParseStageValue(\"not-a-stage-value\"): want ok=false, got true")
	}
}

func TestRecordedPhaseName_TargetBareForm_IsNoOp(t *testing.T) {
	got := domain.RecordedPhaseName("EXECUTION")
	if got != "EXECUTION" {
		t.Errorf("RecordedPhaseName(%q): want %q, got %q", "EXECUTION", "EXECUTION", got)
	}
}

func TestRecordedPhaseName_LegacyGroupedForm_StripsGroupAndStagePlaceholder(t *testing.T) {
	got := domain.RecordedPhaseName("EXECUTION.Test.[StageNumber]")
	if got != "EXECUTION" {
		t.Errorf("RecordedPhaseName(%q): want %q, got %q", "EXECUTION.Test.[StageNumber]", "EXECUTION", got)
	}
}

func TestRecordedPhaseName_LegacyUngroupedForm_StripsStagePlaceholder(t *testing.T) {
	got := domain.RecordedPhaseName("EXECUTION.[StageNumber]")
	if got != "EXECUTION" {
		t.Errorf("RecordedPhaseName(%q): want %q, got %q", "EXECUTION.[StageNumber]", "EXECUTION", got)
	}
}

func TestRecordedPhaseName_Empty_ReturnsEmpty(t *testing.T) {
	got := domain.RecordedPhaseName("")
	if got != "" {
		t.Errorf("RecordedPhaseName(\"\"): want %q, got %q", "", got)
	}
}

func TestRecordedPhaseName_NonExecutionPhase_ReturnedUnchanged(t *testing.T) {
	got := domain.RecordedPhaseName("PLANNING")
	if got != "PLANNING" {
		t.Errorf("RecordedPhaseName(%q): want unchanged %q, got %q", "PLANNING", "PLANNING", got)
	}
}
