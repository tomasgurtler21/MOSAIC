package planstages_test

// Tests for ReadStages validation: dependency validation (FR-16a),
// stage number consecutiveness, and opaque approach token acceptance.

import (
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/planstages"
)

// ---------------------------------------------------------------------------
// Dependency validation (FR-16a)
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Stage number consecutiveness validation
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Opaque approach tokens (requireApproach=true)
// ---------------------------------------------------------------------------

func TestReadStages_OpaqueApproachToken_Accepted(t *testing.T) {
	// opaque-approach.md has Approach = "CustomFlow", which is not in any
	// fixed set. Arbitrary tokens are accepted verbatim.
	_, err := planstages.ReadStages(planstagesFixture("opaque-approach.md"), true)

	if err != nil {
		t.Fatalf("opaque approach token must be accepted verbatim; got error: %v", err)
	}
}

func TestReadStages_OpaqueApproachToken_StoredVerbatim(t *testing.T) {
	// "CustomFlow" must appear verbatim in StageEntry.Approach.
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
	_, err := planstages.ReadStages(planstagesFixture("empty-approach-cell.md"), true)

	if err == nil {
		t.Fatal("empty Approach cell when requireApproach=true must return an error")
	}
	asRefusalError(t, err)
}

func TestReadStages_EmptyApproachCell_RefusalError_NamesStage(t *testing.T) {
	// The S2 error message must name the stage number so the user can locate it.
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
	// explicitly empty cell.
	_, err := planstages.ReadStages(planstagesFixture("dash-approach-cell.md"), true)

	if err == nil {
		t.Fatal("dash Approach cell when requireApproach=true must return an error")
	}
	re := asRefusalError(t, err)
	// S2 message must say "empty Approach value" (dash is treated as empty).
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
	// old wording "two-group workflow") to accurately reflect the activation rule.
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
