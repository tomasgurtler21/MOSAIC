package planstages_test

// Tests for planstages.ReadStages.
//
// Coverage:
//
//   Happy path - no-groups workflow (requireApproach=false, no Approach column):
//   - three-stages-no-approach.md: returns 3 StageEntry values.
//   - Stage numbers in returned entries start at 1 and are consecutive.
//   - HITL column decoded: ✅ → true, ❌ → false.
//   - Depends On column parsed: "-" produces empty DependsOn slice.
//   - Depends On column parsed: "1, 2" produces []StageNumber{1, 2}.
//   - Approach is zero value (empty string) when requireApproach is false.
//   - single-stage.md: single entry returned with Number == 1.
//
//   Happy path - grouped workflow (requireApproach=true, Approach column present):
//   - four-stages-with-approach.md: Approach column read verbatim on all entries.
//   - "TDD" stored as domain.Approach("TDD").
//   - "Implementation-First" stored as domain.Approach("Implementation-First").
//   - "Implementation-Only" stored as domain.Approach("Implementation-Only").
//   - "Tests-Only" stored as domain.Approach("Tests-Only").
//
//   Opaque approach tokens (requireApproach=true):
//   - Arbitrary token "CustomFlow" accepted verbatim without error.
//   - "CustomFlow" stored exactly as-is in StageEntry.Approach.
//   - Two stages with the same opaque token both stored correctly.
//   - Empty Approach cell when requireApproach=true returns *domain.RefusalError (S2).
//   - Empty-cell RefusalError names the stage number.
//   - Dash "-" Approach cell when requireApproach=true returns *domain.RefusalError (S2).
//   - Missing Approach column when required returns *domain.RefusalError (S1).
//   - Missing-column error message mentions "workflow declares execution groups".
//   - no-approach-col.md with requireApproach=false is accepted (no-groups mode).
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

// --- Happy path: grouped workflow, Approach column present (verbatim tokens) ---

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
	// Stage 1 in four-stages-with-approach.md has Approach "TDD". It must be
	// stored as the verbatim string domain.Approach("TDD"), not decoded through
	// any enum switch.
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

// --- Opaque approach tokens (T4.4) ---

func TestReadStages_OpaqueApproachToken_Accepted(t *testing.T) {
	// opaque-approach.md has Approach = "CustomFlow", which is not in any
	// fixed set. Under Stage 4, arbitrary tokens are accepted verbatim.
	// This test is RED: the current implementation rejects "CustomFlow" as
	// unrecognised, but the new implementation accepts it without error.
	_, err := planstages.ReadStages(planstagesFixture("opaque-approach.md"), true)

	if err != nil {
		t.Fatalf("opaque approach token must be accepted verbatim; got error: %v", err)
	}
}

func TestReadStages_OpaqueApproachToken_StoredVerbatim(t *testing.T) {
	// "CustomFlow" must appear verbatim in StageEntry.Approach.
	// This test is RED: current implementation rejects "CustomFlow" before storing it.
	set, err := planstages.ReadStages(planstagesFixture("opaque-approach.md"), true)
	if err != nil {
		t.Fatalf("ReadStages: %v", err)
	}

	entry, ok := set.Entry(1)
	if !ok {
		t.Fatal("stage 1 not found")
	}
	if entry.Approach != "CustomFlow" {
		t.Errorf("stage 1 Approach: want %q (verbatim), got %q", "CustomFlow", entry.Approach)
	}
}

func TestReadStages_OpaqueApproachToken_BothStagesStored(t *testing.T) {
	// Both stages in opaque-approach.md have the same "CustomFlow" token.
	// Both must be stored correctly.
	// This test is RED: current implementation rejects "CustomFlow".
	set, err := planstages.ReadStages(planstagesFixture("opaque-approach.md"), true)
	if err != nil {
		t.Fatalf("ReadStages: %v", err)
	}

	for _, num := range []domain.StageNumber{1, 2} {
		entry, ok := set.Entry(num)
		if !ok {
			t.Fatalf("stage %d not found", num)
		}
		if entry.Approach != "CustomFlow" {
			t.Errorf("stage %d Approach: want %q, got %q", num, "CustomFlow", entry.Approach)
		}
	}
}

func TestReadStages_EmptyApproachCell_WhenRequired_ReturnsRefusalError(t *testing.T) {
	// empty-approach-cell.md has Approach column present but the cell is empty.
	// An empty (whitespace) cell when requireApproach=true must be refused (S2).
	// This test is RED: current implementation hits the switch default and produces
	// a different error message ("unrecognised Approach value"). The new implementation
	// must produce the S2 message naming the stage number.
	_, err := planstages.ReadStages(planstagesFixture("empty-approach-cell.md"), true)

	if err == nil {
		t.Fatal("empty Approach cell when requireApproach=true must return an error")
	}
	asRefusalError(t, err)
}

func TestReadStages_EmptyApproachCell_RefusalError_NamesStage(t *testing.T) {
	// The S2 error message must name the stage number so the user can locate it.
	// This test is RED: the current implementation may return a different error message.
	_, err := planstages.ReadStages(planstagesFixture("empty-approach-cell.md"), true)

	re := asRefusalError(t, err)
	// Stage 1 has the empty cell.
	if !strings.Contains(re.Error(), "1") {
		t.Errorf("S2 RefusalError must mention stage 1; got %q", re.Error())
	}
}

func TestReadStages_DashApproachCell_WhenRequired_ReturnsRefusalError(t *testing.T) {
	// dash-approach-cell.md has Approach = "-" which is the conventional "no value"
	// sentinel. A dash cell when requireApproach=true must be refused with the S2
	// message ("has an empty Approach value"), matching the same contract as an
	// explicitly empty cell. The current implementation refuses "-" as an
	// "unrecognised" value, which produces a different message; adding the assertion
	// below makes this test RED until Stage 4 introduces the S2 message for dash cells.
	_, err := planstages.ReadStages(planstagesFixture("dash-approach-cell.md"), true)

	if err == nil {
		t.Fatal("dash Approach cell when requireApproach=true must return an error")
	}
	re := asRefusalError(t, err)
	// S2 message must say "empty Approach value" (dash is treated as empty, same as
	// the sibling TestReadStages_EmptyApproachCell_RefusalError_NamesStage test).
	if !strings.Contains(re.Error(), "empty Approach value") {
		t.Errorf("S2 RefusalError must say \"empty Approach value\" (dash treated as empty); got %q", re.Error())
	}
	// The message must identify the stage number (stage 1 in dash-approach-cell.md).
	if !strings.Contains(re.Error(), "1") {
		t.Errorf("S2 RefusalError must mention stage 1; got %q", re.Error())
	}
}

func TestReadStages_MissingApproachColumn_WhenRequired_ReturnsRefusalError(t *testing.T) {
	// no-approach-col.md has no Approach column; requireApproach=true must refuse.
	_, err := planstages.ReadStages(planstagesFixture("no-approach-col.md"), true)

	if err == nil {
		t.Fatal("missing required Approach column must return an error")
	}
	asRefusalError(t, err)
}

func TestReadStages_MissingApproachColumn_ErrorMentionsGroupsDeclared(t *testing.T) {
	// The S1 error message must say "workflow declares execution groups" (not the
	// Stage 3 wording "two-group workflow") to accurately reflect the activation rule.
	// This test is RED: the current implementation produces the old message.
	_, err := planstages.ReadStages(planstagesFixture("no-approach-col.md"), true)

	re := asRefusalError(t, err)
	if strings.Contains(re.Error(), "two-group") {
		t.Errorf("S1 error must not say \"two-group workflow\"; got %q", re.Error())
	}
	if !strings.Contains(re.Error(), "execution groups") {
		t.Errorf("S1 error must mention \"execution groups\"; got %q", re.Error())
	}
}

func TestReadStages_MissingApproachColumn_WhenNotRequired_Accepted(t *testing.T) {
	// The same file is valid when requireApproach is false (no-groups workflow).
	_, err := planstages.ReadStages(planstagesFixture("no-approach-col.md"), false)

	if err != nil {
		t.Errorf("missing Approach column must be accepted when requireApproach=false; got: %v", err)
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
