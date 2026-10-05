package planstages_test

// Tests for ReadStages happy-path parsing and structural error cases
// (missing file, missing ## Stages heading, missing required columns).

import (
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/planstages"
)

// ---------------------------------------------------------------------------
// Happy path — no-groups workflow (requireApproach=false, no Approach column)
// ---------------------------------------------------------------------------

func TestReadStages_ThreeStagesNoApproach_ReturnsThreeEntries(t *testing.T) {
	set, err := planstages.ReadStages(planstagesFixture("three-stages-no-approach.md"), false)

	if err != nil {
		t.Fatalf("ReadStages returned unexpected error: %v", err)
	}
	if set.Count() != 3 {
		t.Errorf("want 3 stage entries, got %d", set.Count())
	}
}

func TestReadStages_ThreeStagesNoApproach_NumbersStartAtOne(t *testing.T) {
	set, err := planstages.ReadStages(planstagesFixture("three-stages-no-approach.md"), false)
	if err != nil {
		t.Fatalf("ReadStages: %v", err)
	}

	numbers := set.Numbers()
	if numbers[0] != 1 {
		t.Errorf("first stage number: want 1, got %d", numbers[0])
	}
}

func TestReadStages_ThreeStagesNoApproach_NumbersAreConsecutive(t *testing.T) {
	set, err := planstages.ReadStages(planstagesFixture("three-stages-no-approach.md"), false)
	if err != nil {
		t.Fatalf("ReadStages: %v", err)
	}

	numbers := set.Numbers()
	for i, n := range numbers {
		want := domain.StageNumber(i + 1)
		if n != want {
			t.Errorf("numbers[%d]: want %d, got %d", i, want, n)
		}
	}
}

func TestReadStages_ThreeStagesNoApproach_HITL_CheckmarkIsTrue(t *testing.T) {
	// Stage 2 in the fixture has HITL = TRUE.
	set, err := planstages.ReadStages(planstagesFixture("three-stages-no-approach.md"), false)
	if err != nil {
		t.Fatalf("ReadStages: %v", err)
	}

	entry, ok := set.Entry(2)
	if !ok {
		t.Fatal("stage 2 not found in returned set")
	}
	if !entry.HITL {
		t.Error("stage 2 HITL: want true (TRUE), got false")
	}
}

func TestReadStages_ThreeStagesNoApproach_HITL_CrossIsfalse(t *testing.T) {
	// Stage 1 in the fixture has HITL = FALSE.
	set, err := planstages.ReadStages(planstagesFixture("three-stages-no-approach.md"), false)
	if err != nil {
		t.Fatalf("ReadStages: %v", err)
	}

	entry, ok := set.Entry(1)
	if !ok {
		t.Fatal("stage 1 not found in returned set")
	}
	if entry.HITL {
		t.Error("stage 1 HITL: want false (FALSE), got true")
	}
}

func TestReadStages_ThreeStagesNoApproach_DependsOn_DashProducesEmptySlice(t *testing.T) {
	// Stage 1 has Depends On = "-" which means no dependencies.
	set, err := planstages.ReadStages(planstagesFixture("three-stages-no-approach.md"), false)
	if err != nil {
		t.Fatalf("ReadStages: %v", err)
	}

	entry, ok := set.Entry(1)
	if !ok {
		t.Fatal("stage 1 not found")
	}
	if len(entry.DependsOn) != 0 {
		t.Errorf("stage 1 DependsOn: want empty, got %v", entry.DependsOn)
	}
}

func TestReadStages_ThreeStagesNoApproach_DependsOn_CommaSeparatedParsed(t *testing.T) {
	// Stage 3 has Depends On = "1, 2".
	set, err := planstages.ReadStages(planstagesFixture("three-stages-no-approach.md"), false)
	if err != nil {
		t.Fatalf("ReadStages: %v", err)
	}

	entry, ok := set.Entry(3)
	if !ok {
		t.Fatal("stage 3 not found")
	}
	if len(entry.DependsOn) != 2 {
		t.Fatalf("stage 3 DependsOn: want 2 entries, got %v", entry.DependsOn)
	}
	if entry.DependsOn[0] != 1 {
		t.Errorf("stage 3 DependsOn[0]: want 1, got %d", entry.DependsOn[0])
	}
	if entry.DependsOn[1] != 2 {
		t.Errorf("stage 3 DependsOn[1]: want 2, got %d", entry.DependsOn[1])
	}
}

func TestReadStages_ThreeStagesNoApproach_Approach_IsZeroValue(t *testing.T) {
	// When needsApproach is false, Approach on each entry must be the zero value.
	set, err := planstages.ReadStages(planstagesFixture("three-stages-no-approach.md"), false)
	if err != nil {
		t.Fatalf("ReadStages: %v", err)
	}

	for _, entry := range set.Entries {
		if entry.Approach != "" {
			t.Errorf("stage %d Approach: want zero value, got %q", entry.Number, entry.Approach)
		}
	}
}

func TestReadStages_SingleStage_ReturnsOneEntryWithNumberOne(t *testing.T) {
	set, err := planstages.ReadStages(planstagesFixture("single-stage.md"), false)

	if err != nil {
		t.Fatalf("ReadStages: %v", err)
	}
	if set.Count() != 1 {
		t.Fatalf("want 1 entry, got %d", set.Count())
	}
	if set.Entries[0].Number != 1 {
		t.Errorf("entry Number: want 1, got %d", set.Entries[0].Number)
	}
}

// ---------------------------------------------------------------------------
// Happy path — grouped workflow (requireApproach=true, Approach column present)
// ---------------------------------------------------------------------------

func TestReadStages_FourStagesWithApproach_AllApproachesRead(t *testing.T) {
	set, err := planstages.ReadStages(planstagesFixture("four-stages-with-approach.md"), true)
	if err != nil {
		t.Fatalf("ReadStages: %v", err)
	}
	if set.Count() != 4 {
		t.Fatalf("want 4 entries, got %d", set.Count())
	}
}

func TestReadStages_FourStagesWithApproach_TDD_StoredVerbatim(t *testing.T) {
	set, err := planstages.ReadStages(planstagesFixture("four-stages-with-approach.md"), true)
	if err != nil {
		t.Fatalf("ReadStages: %v", err)
	}

	entry, ok := set.Entry(1)
	if !ok {
		t.Fatal("stage 1 not found")
	}
	if entry.Approach != "TDD" {
		t.Errorf("stage 1 Approach: want %q (verbatim), got %q", "TDD", entry.Approach)
	}
}

func TestReadStages_FourStagesWithApproach_ImplementationFirst_StoredVerbatim(t *testing.T) {
	set, err := planstages.ReadStages(planstagesFixture("four-stages-with-approach.md"), true)
	if err != nil {
		t.Fatalf("ReadStages: %v", err)
	}

	entry, ok := set.Entry(2)
	if !ok {
		t.Fatal("stage 2 not found")
	}
	if entry.Approach != "Implementation-First" {
		t.Errorf("stage 2 Approach: want %q (verbatim), got %q", "Implementation-First", entry.Approach)
	}
}

func TestReadStages_FourStagesWithApproach_ImplementationOnly_StoredVerbatim(t *testing.T) {
	set, err := planstages.ReadStages(planstagesFixture("four-stages-with-approach.md"), true)
	if err != nil {
		t.Fatalf("ReadStages: %v", err)
	}

	entry, ok := set.Entry(3)
	if !ok {
		t.Fatal("stage 3 not found")
	}
	if entry.Approach != "Implementation-Only" {
		t.Errorf("stage 3 Approach: want %q (verbatim), got %q", "Implementation-Only", entry.Approach)
	}
}

func TestReadStages_FourStagesWithApproach_TestsOnly_StoredVerbatim(t *testing.T) {
	set, err := planstages.ReadStages(planstagesFixture("four-stages-with-approach.md"), true)
	if err != nil {
		t.Fatalf("ReadStages: %v", err)
	}

	entry, ok := set.Entry(4)
	if !ok {
		t.Fatal("stage 4 not found")
	}
	if entry.Approach != "Tests-Only" {
		t.Errorf("stage 4 Approach: want %q (verbatim), got %q", "Tests-Only", entry.Approach)
	}
}

// ---------------------------------------------------------------------------
// Error cases — missing file
// ---------------------------------------------------------------------------

func TestReadStages_MissingFile_ReturnsError(t *testing.T) {
	_, err := planstages.ReadStages(planstagesFixture("does-not-exist.md"), false)

	if err == nil {
		t.Fatal("missing file must return an error")
	}
}

func TestReadStages_MissingFile_ReturnsRefusalError(t *testing.T) {
	_, err := planstages.ReadStages(planstagesFixture("does-not-exist.md"), false)

	asRefusalError(t, err)
}

func TestReadStages_MissingFile_RefusalError_ComponentIsPlanstages(t *testing.T) {
	_, err := planstages.ReadStages(planstagesFixture("does-not-exist.md"), false)

	re := asRefusalError(t, err)
	if re.Component != "planstages" {
		t.Errorf("RefusalError.Component: want %q, got %q", "planstages", re.Component)
	}
}

func TestReadStages_MissingFile_RefusalError_ResourceNamesFile(t *testing.T) {
	path := planstagesFixture("does-not-exist.md")
	_, err := planstages.ReadStages(path, false)

	re := asRefusalError(t, err)
	if !strings.Contains(re.Resource, "does-not-exist.md") {
		t.Errorf("RefusalError.Resource must contain file path; got %q", re.Resource)
	}
}

// ---------------------------------------------------------------------------
// Error cases — missing ## Stages heading
// ---------------------------------------------------------------------------

func TestReadStages_NoStagesHeading_ReturnsError(t *testing.T) {
	_, err := planstages.ReadStages(planstagesFixture("no-stages-heading.md"), false)

	if err == nil {
		t.Fatal("file without ## Stages heading must return an error")
	}
}

func TestReadStages_NoStagesHeading_ReturnsRefusalError(t *testing.T) {
	_, err := planstages.ReadStages(planstagesFixture("no-stages-heading.md"), false)

	asRefusalError(t, err)
}

func TestReadStages_NoStagesHeading_RefusalError_NamesFile(t *testing.T) {
	path := planstagesFixture("no-stages-heading.md")
	_, err := planstages.ReadStages(path, false)

	re := asRefusalError(t, err)
	if !strings.Contains(re.Error(), "no-stages-heading.md") {
		t.Errorf("RefusalError must mention the file name; got %q", re.Error())
	}
}

// ---------------------------------------------------------------------------
// Error cases — missing required columns
// ---------------------------------------------------------------------------

func TestReadStages_MissingStageColumn_ReturnsRefusalError(t *testing.T) {
	_, err := planstages.ReadStages(planstagesFixture("missing-stage-col.md"), false)

	if err == nil {
		t.Fatal("missing Stage column must return an error")
	}
	asRefusalError(t, err)
}

func TestReadStages_MissingStageColumn_RefusalError_NamesColumn(t *testing.T) {
	_, err := planstages.ReadStages(planstagesFixture("missing-stage-col.md"), false)

	re := asRefusalError(t, err)
	if !strings.Contains(re.Error(), "Stage") {
		t.Errorf("RefusalError must name the missing column %q; got %q", "Stage", re.Error())
	}
}

func TestReadStages_MissingHITLColumn_ReturnsRefusalError(t *testing.T) {
	_, err := planstages.ReadStages(planstagesFixture("missing-hitl-col.md"), false)

	if err == nil {
		t.Fatal("missing HITL column must return an error")
	}
	asRefusalError(t, err)
}

func TestReadStages_MissingHITLColumn_RefusalError_NamesColumn(t *testing.T) {
	_, err := planstages.ReadStages(planstagesFixture("missing-hitl-col.md"), false)

	re := asRefusalError(t, err)
	if !strings.Contains(re.Error(), "HITL") {
		t.Errorf("RefusalError must name the missing column %q; got %q", "HITL", re.Error())
	}
}

func TestReadStages_MissingDependsOnColumn_ReturnsRefusalError(t *testing.T) {
	_, err := planstages.ReadStages(planstagesFixture("missing-depends-col.md"), false)

	if err == nil {
		t.Fatal("missing Depends On column must return an error")
	}
	asRefusalError(t, err)
}

func TestReadStages_MissingDependsOnColumn_RefusalError_NamesColumn(t *testing.T) {
	_, err := planstages.ReadStages(planstagesFixture("missing-depends-col.md"), false)

	re := asRefusalError(t, err)
	if !strings.Contains(re.Error(), "Depends On") {
		t.Errorf("RefusalError must name the missing column %q; got %q", "Depends On", re.Error())
	}
}
