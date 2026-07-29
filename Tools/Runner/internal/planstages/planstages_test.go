package planstages_test

// Tests for planstages.ReadStages.
//
// Coverage:
//
//   Happy path - single-group workflow (needsApproach=false, no Approach column):
//   - three-stages-no-approach.md: returns 3 StageEntry values.
//   - Stage numbers in returned entries start at 1 and are consecutive.
//   - HITL column decoded: ✅ → true, ❌ → false.
//   - Depends On column parsed: "-" produces empty DependsOn slice.
//   - Depends On column parsed: "1, 2" produces []StageNumber{1, 2}.
//   - Approach is zero value (empty string) when needsApproach is false.
//   - single-stage.md: single entry returned with Number == 1.
//
//   Happy path - two-group workflow (needsApproach=true, Approach column present):
//   - four-stages-with-approach.md: Approach column read and decoded on all entries.
//   - "TDD" decoded to ApproachTDD.
//   - "Implementation-First" decoded to ApproachImplementationFirst.
//   - "Implementation-Only" decoded to ApproachImplementationOnly.
//   - "Tests-Only" decoded to ApproachTestsOnly.
//
//   Dependency validation (FR-16a):
//   - Valid backward dependencies accepted: stage 3 depends on 1,2 → no error.
//   - Forward dependency rejected: stage 2 depends on stage 3 → *domain.RefusalError.
//   - Forward-dep refusal error names the offending stage number.
//   - Missing dependency rejected: stage 2 depends on stage 99 → *domain.RefusalError.
//   - Missing-dep refusal error names the nonexistent dependency.
//
//   Stage number consecutiveness validation:
//   - Gap in stage numbers rejected (1, 3) → *domain.RefusalError.
//   - Stage numbers not starting at 1 rejected → *domain.RefusalError.
//
//   Approach validation (needsApproach=true):
//   - Unrecognised approach value rejected → *domain.RefusalError.
//   - Missing Approach column when required rejected → *domain.RefusalError.
//   - no-approach-col.md with needsApproach=false is accepted (single-group mode).
//
//   Error cases - missing file:
//   - Missing file returns error.
//   - Missing file returns *domain.RefusalError.
//   - RefusalError.Component is "planstages".
//   - RefusalError.Resource names the file path.
//
//   Error cases - missing ## Stages heading:
//   - File without ## Stages heading returns *domain.RefusalError.
//   - RefusalError names the file.
//
//   Error cases - missing required columns:
//   - Missing Stage column returns *domain.RefusalError.
//   - Missing HITL column returns *domain.RefusalError.
//   - Missing Depends On column returns *domain.RefusalError.
//   - RefusalError for missing column names the column.

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/planstages"
)

const planstagesTestdataDir = "../../testdata/planstages"

// planstagesFixture returns the absolute path to a named fixture file.
func planstagesFixture(name string) string {
	return filepath.Join(planstagesTestdataDir, name)
}

// asRefusalError asserts that err wraps a *domain.RefusalError and returns it.
// Calls t.Fatal on failure.
func asRefusalError(t *testing.T, err error) *domain.RefusalError {
	t.Helper()
	var re *domain.RefusalError
	if !errors.As(err, &re) {
		t.Fatalf("want *domain.RefusalError, got %T: %v", err, err)
	}
	return re
}

// --- Happy path: single-group, no Approach column ---

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
	// Stage 2 in the fixture has HITL = ✅.
	set, err := planstages.ReadStages(planstagesFixture("three-stages-no-approach.md"), false)
	if err != nil {
		t.Fatalf("ReadStages: %v", err)
	}

	entry, ok := set.Entry(2)
	if !ok {
		t.Fatal("stage 2 not found in returned set")
	}
	if !entry.HITL {
		t.Error("stage 2 HITL: want true (✅), got false")
	}
}

func TestReadStages_ThreeStagesNoApproach_HITL_CrossIsfalse(t *testing.T) {
	// Stage 1 in the fixture has HITL = ❌.
	set, err := planstages.ReadStages(planstagesFixture("three-stages-no-approach.md"), false)
	if err != nil {
		t.Fatalf("ReadStages: %v", err)
	}

	entry, ok := set.Entry(1)
	if !ok {
		t.Fatal("stage 1 not found in returned set")
	}
	if entry.HITL {
		t.Error("stage 1 HITL: want false (❌), got true")
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

// --- Happy path: two-group, Approach column present ---

func TestReadStages_FourStagesWithApproach_AllApproachesDecoded(t *testing.T) {
	set, err := planstages.ReadStages(planstagesFixture("four-stages-with-approach.md"), true)
	if err != nil {
		t.Fatalf("ReadStages: %v", err)
	}
	if set.Count() != 4 {
		t.Fatalf("want 4 entries, got %d", set.Count())
	}
}

func TestReadStages_FourStagesWithApproach_TDD_Decoded(t *testing.T) {
	set, err := planstages.ReadStages(planstagesFixture("four-stages-with-approach.md"), true)
	if err != nil {
		t.Fatalf("ReadStages: %v", err)
	}

	entry, ok := set.Entry(1)
	if !ok {
		t.Fatal("stage 1 not found")
	}
	if entry.Approach != domain.ApproachTDD {
		t.Errorf("stage 1 Approach: want %q, got %q", domain.ApproachTDD, entry.Approach)
	}
}

func TestReadStages_FourStagesWithApproach_ImplementationFirst_Decoded(t *testing.T) {
	set, err := planstages.ReadStages(planstagesFixture("four-stages-with-approach.md"), true)
	if err != nil {
		t.Fatalf("ReadStages: %v", err)
	}

	entry, ok := set.Entry(2)
	if !ok {
		t.Fatal("stage 2 not found")
	}
	if entry.Approach != domain.ApproachImplementationFirst {
		t.Errorf("stage 2 Approach: want %q, got %q", domain.ApproachImplementationFirst, entry.Approach)
	}
}

func TestReadStages_FourStagesWithApproach_ImplementationOnly_Decoded(t *testing.T) {
	set, err := planstages.ReadStages(planstagesFixture("four-stages-with-approach.md"), true)
	if err != nil {
		t.Fatalf("ReadStages: %v", err)
	}

	entry, ok := set.Entry(3)
	if !ok {
		t.Fatal("stage 3 not found")
	}
	if entry.Approach != domain.ApproachImplementationOnly {
		t.Errorf("stage 3 Approach: want %q, got %q", domain.ApproachImplementationOnly, entry.Approach)
	}
}

func TestReadStages_FourStagesWithApproach_TestsOnly_Decoded(t *testing.T) {
	set, err := planstages.ReadStages(planstagesFixture("four-stages-with-approach.md"), true)
	if err != nil {
		t.Fatalf("ReadStages: %v", err)
	}

	entry, ok := set.Entry(4)
	if !ok {
		t.Fatal("stage 4 not found")
	}
	if entry.Approach != domain.ApproachTestsOnly {
		t.Errorf("stage 4 Approach: want %q, got %q", domain.ApproachTestsOnly, entry.Approach)
	}
}

// --- Dependency validation (FR-16a) ---

func TestReadStages_ValidBackwardDependencies_Accepted(t *testing.T) {
	// three-stages-no-approach.md has stage 3 depending on stages 1 and 2.
	// These are valid backward references and must not produce an error.
	_, err := planstages.ReadStages(planstagesFixture("three-stages-no-approach.md"), false)

	if err != nil {
		t.Errorf("valid backward dependencies must not produce an error, got: %v", err)
	}
}

func TestReadStages_ForwardDependency_ReturnsError(t *testing.T) {
	// forward-dep.md has stage 2 depending on stage 3 (a forward reference).
	_, err := planstages.ReadStages(planstagesFixture("forward-dep.md"), false)

	if err == nil {
		t.Fatal("forward dependency must return an error")
	}
}

func TestReadStages_ForwardDependency_ReturnsRefusalError(t *testing.T) {
	_, err := planstages.ReadStages(planstagesFixture("forward-dep.md"), false)

	asRefusalError(t, err)
}

func TestReadStages_ForwardDependency_RefusalError_NamesStage(t *testing.T) {
	// The error message must name the stage that has the forward dependency
	// so the user can locate and fix it without reading the full table.
	_, err := planstages.ReadStages(planstagesFixture("forward-dep.md"), false)

	re := asRefusalError(t, err)
	// Stage 2 is the offending stage.
	if !strings.Contains(re.Error(), "2") {
		t.Errorf("RefusalError must mention stage 2; got %q", re.Error())
	}
}

func TestReadStages_MissingDependency_ReturnsRefusalError(t *testing.T) {
	// missing-dep.md has stage 2 depending on stage 99, which does not exist.
	_, err := planstages.ReadStages(planstagesFixture("missing-dep.md"), false)

	if err == nil {
		t.Fatal("dependency on nonexistent stage must return an error")
	}
	asRefusalError(t, err)
}

func TestReadStages_MissingDependency_RefusalError_NamesNonexistentStage(t *testing.T) {
	_, err := planstages.ReadStages(planstagesFixture("missing-dep.md"), false)

	re := asRefusalError(t, err)
	if !strings.Contains(re.Error(), "99") {
		t.Errorf("RefusalError must mention the nonexistent dependency 99; got %q", re.Error())
	}
}

// --- Stage number consecutiveness validation ---

func TestReadStages_GapInNumbers_ReturnsRefusalError(t *testing.T) {
	// gap-in-numbers.md has stages 1 and 3 (skipping 2).
	_, err := planstages.ReadStages(planstagesFixture("gap-in-numbers.md"), false)

	if err == nil {
		t.Fatal("gap in stage numbers must return an error")
	}
	asRefusalError(t, err)
}

func TestReadStages_StartsAtTwo_ReturnsRefusalError(t *testing.T) {
	// starts-at-two.md begins with stage 2, not 1.
	_, err := planstages.ReadStages(planstagesFixture("starts-at-two.md"), false)

	if err == nil {
		t.Fatal("stages not starting at 1 must return an error")
	}
	asRefusalError(t, err)
}

// --- Approach validation (needsApproach=true) ---

func TestReadStages_UnrecognisedApproach_ReturnsRefusalError(t *testing.T) {
	// bad-approach.md has Approach = "BDD" which is not in the recognised set.
	_, err := planstages.ReadStages(planstagesFixture("bad-approach.md"), true)

	if err == nil {
		t.Fatal("unrecognised approach value must return an error")
	}
	asRefusalError(t, err)
}

func TestReadStages_UnrecognisedApproach_RefusalError_NamesValue(t *testing.T) {
	_, err := planstages.ReadStages(planstagesFixture("bad-approach.md"), true)

	re := asRefusalError(t, err)
	if !strings.Contains(re.Error(), "BDD") {
		t.Errorf("RefusalError must name the unrecognised value %q; got %q", "BDD", re.Error())
	}
}

func TestReadStages_MissingApproachColumn_WhenRequired_ReturnsRefusalError(t *testing.T) {
	// no-approach-col.md has no Approach column; needsApproach=true must refuse.
	_, err := planstages.ReadStages(planstagesFixture("no-approach-col.md"), true)

	if err == nil {
		t.Fatal("missing required Approach column must return an error")
	}
	asRefusalError(t, err)
}

func TestReadStages_MissingApproachColumn_WhenNotRequired_Accepted(t *testing.T) {
	// The same file is valid when needsApproach is false (single-group workflow).
	_, err := planstages.ReadStages(planstagesFixture("no-approach-col.md"), false)

	if err != nil {
		t.Errorf("missing Approach column must be accepted when needsApproach=false; got: %v", err)
	}
}

// --- Error cases: missing file ---

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

// --- Error cases: missing ## Stages heading ---

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

// --- Error cases: missing required columns ---

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
