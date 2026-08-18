package domain_test

// Tests for report shapes and their behavioral contracts.
//
// Design invariants under test (AC2.5):
//   - Report.IsEmpty reports true only when there are no named runs and no
//     unattributable bucket; the presence of either makes it non-empty.
//   - Report.HasUnpricedModels reports whether UnpricedModels is non-empty.
//   - Report.FindRun locates named runs by ID and returns ok=false for absent ones.
//   - Report.FindRun never returns the unattributable bucket; it is held separately.
//   - The report shapes can express unpriced money (MoneyUnpriced state), provisional
//     (in-progress) runs (RunReport.Provisional=true), and an unattributable bucket
//     (Report.Unattributable) held separately from the named-run slice.
//
// All assertions here call stub methods that panic("not implemented") until
// implementation is complete — this is the expected TDD RED-phase behavior.

import (
	"testing"

	"mosaic-log-analyzer/internal/domain"
)

// ---------------------------------------------------------------------------
// Report.IsEmpty
// ---------------------------------------------------------------------------

func TestReport_IsEmpty_NoRuns_NoUnattributable(t *testing.T) {
	r := domain.Report{}
	if !r.IsEmpty() {
		t.Error("Report with no runs and no unattributable bucket must return IsEmpty()=true")
	}
}

func TestReport_IsEmpty_WithNamedRun_IsFalse(t *testing.T) {
	r := domain.Report{
		Runs: []domain.RunReport{
			{Run: domain.NamedRun("20260802T074635Z-480e")},
		},
	}
	if r.IsEmpty() {
		t.Error("Report with named runs must return IsEmpty()=false")
	}
}

func TestReport_IsEmpty_WithUnattributableOnly_IsFalse(t *testing.T) {
	// A report containing only the unattributable bucket still has data.
	r := domain.Report{
		Unattributable: &domain.RunReport{
			Run: domain.UnattributableRun(),
		},
	}
	if r.IsEmpty() {
		t.Error("Report with only an unattributable bucket must return IsEmpty()=false")
	}
}

// ---------------------------------------------------------------------------
// Report.HasUnpricedModels
// ---------------------------------------------------------------------------

func TestReport_HasUnpricedModels_EmptyList_ReturnsFalse(t *testing.T) {
	r := domain.Report{}
	if r.HasUnpricedModels() {
		t.Error("Report with empty UnpricedModels must return HasUnpricedModels()=false")
	}
}

func TestReport_HasUnpricedModels_WithUnpricedModels_ReturnsTrue(t *testing.T) {
	r := domain.Report{
		Runs: []domain.RunReport{
			{Run: domain.NamedRun("20260802T074635Z-480e")},
		},
		UnpricedModels: []domain.ModelID{"unknown-model"},
	}
	if !r.HasUnpricedModels() {
		t.Error("Report with non-empty UnpricedModels must return HasUnpricedModels()=true")
	}
}

// ---------------------------------------------------------------------------
// Report.FindRun
// ---------------------------------------------------------------------------

func TestReport_FindRun_ExistingRun_IsFound(t *testing.T) {
	const runID = "20260802T074635Z-480e"
	r := domain.Report{
		Runs: []domain.RunReport{
			{Run: domain.NamedRun(runID)},
		},
	}
	got, ok := r.FindRun(runID)
	if !ok {
		t.Fatal("FindRun must find a named run that exists in the report")
	}
	if got.Run.ID != runID {
		t.Errorf("FindRun returned run ID %q, want %q", got.Run.ID, runID)
	}
}

func TestReport_FindRun_AbsentRun_IsNotFound(t *testing.T) {
	r := domain.Report{}
	_, ok := r.FindRun("20260802T074635Z-480e")
	if ok {
		t.Error("FindRun must return ok=false for a run not in the report")
	}
}

func TestReport_FindRun_MultipleRuns_ReturnsCorrectOne(t *testing.T) {
	const targetID = "20260802T074635Z-480e"
	const otherID = "20260101T000000Z-0001"
	r := domain.Report{
		Runs: []domain.RunReport{
			{Run: domain.NamedRun(otherID), Provisional: false},
			{Run: domain.NamedRun(targetID), Provisional: true},
		},
	}
	got, ok := r.FindRun(targetID)
	if !ok {
		t.Fatal("FindRun must find the target run among multiple runs")
	}
	if got.Run.ID != targetID {
		t.Errorf("FindRun returned run ID %q, want %q", got.Run.ID, targetID)
	}
	if !got.Provisional {
		t.Error("FindRun must return the exact run, including its Provisional flag")
	}
}

func TestReport_FindRun_DoesNotReturnUnattributable(t *testing.T) {
	// The unattributable bucket is held separately and must not be reachable via
	// FindRun. FindRun searches named runs only.
	r := domain.Report{
		Unattributable: &domain.RunReport{
			Run: domain.UnattributableRun(),
		},
	}
	_, ok := r.FindRun("unknown-run")
	if ok {
		t.Error("FindRun must not return the unattributable bucket; it is not a named run")
	}
}

// ---------------------------------------------------------------------------
// AC2.5 — Report shapes express unpriced money
// ---------------------------------------------------------------------------

func TestReport_CanExpressUnpricedMoney(t *testing.T) {
	// A report whose runs contain unpriced models must be discoverable via
	// HasUnpricedModels. The MoneyUnpriced state exists in the type system so
	// that frontends can distinguish "no pricing data" from "$0 cost".
	r := domain.Report{
		Runs: []domain.RunReport{
			{
				Run: domain.NamedRun("20260802T074635Z-480e"),
				Totals: domain.Totals{
					Money: domain.CategoryMoney{
						Input:    domain.MoneyValue{State: domain.MoneyUnpriced},
						Complete: false,
					},
				},
				UnpricedModels: []domain.ModelID{"claude-opus-4"},
			},
		},
		UnpricedModels: []domain.ModelID{"claude-opus-4"},
	}
	if !r.HasUnpricedModels() {
		t.Error("report with an unpriced model must return HasUnpricedModels()=true")
	}
}

// ---------------------------------------------------------------------------
// AC2.5 — Report shapes express provisional (in-progress) runs
// ---------------------------------------------------------------------------

func TestReport_CanExpressProvisionalRun(t *testing.T) {
	// RunReport.Provisional is the in-progress label source (AC3.6, AC10.5).
	// FindRun must return the run with its Provisional flag intact.
	const runID = "20260802T074635Z-480e"
	r := domain.Report{
		Runs: []domain.RunReport{
			{
				Run:         domain.NamedRun(runID),
				Provisional: true,
			},
		},
	}
	got, ok := r.FindRun(runID)
	if !ok {
		t.Fatal("FindRun must find the provisional run")
	}
	if !got.Provisional {
		t.Error("FindRun must return the run with Provisional=true preserved")
	}
}

// ---------------------------------------------------------------------------
// AC2.5 — Unattributable bucket held separately from named runs
// ---------------------------------------------------------------------------

func TestReport_UnattributableHeldSeparatelyFromNamedRuns(t *testing.T) {
	// Report.AllRuns totals named runs only; the unattributable bucket is
	// excluded by contract (AC3.7, AC10.6). This test verifies the structural
	// separation: named runs are findable via FindRun, the unattributable bucket
	// is not, and the report is non-empty when both are present.
	const runID = "20260802T074635Z-480e"
	r := domain.Report{
		Runs: []domain.RunReport{
			{Run: domain.NamedRun(runID)},
		},
		Unattributable: &domain.RunReport{
			Run: domain.UnattributableRun(),
		},
	}

	if r.IsEmpty() {
		t.Error("report with named runs must not be empty")
	}

	// The unattributable bucket must not be reachable via FindRun.
	if _, ok := r.FindRun("unknown-run"); ok {
		t.Error("unattributable bucket must not be findable via FindRun")
	}

	// The named run must still be findable with the unattributable bucket present.
	if _, ok := r.FindRun(runID); !ok {
		t.Errorf("named run %q must be findable when an unattributable bucket is also present", runID)
	}
}
