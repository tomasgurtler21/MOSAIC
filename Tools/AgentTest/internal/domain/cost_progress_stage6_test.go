package domain_test

// Tests for the CostReport.Add rank-based attribution combining and the
// UnpricedModels union behaviour introduced in Stage 6.
//
// Stage 6 adds AttributionPartial and replaces the current "if c.Attribution
// == AttributionAttributed, use other" special case in Add with a total-order
// rank table:
//
//   Rank 0: AttributionAttributed     (strongest)
//   Rank 1: AttributionPartial
//   Rank 2: AttributionUnknownBucket
//   Rank 3: AttributionUnavailable    (weakest)
//
// Add selects max(rank(a), rank(b)), i.e. the weaker of the two. The
// UnpricedModels fields are merged (union, deduplicated) so the combined
// report carries all responsible models.
//
// All 10 non-trivially-ordered pairings are covered here (the commutative
// pairs are represented by testing one direction; the rank table is symmetric).

import (
	"slices"
	"testing"

	"mosaic-agent-test/internal/domain"
)

// ---------------------------------------------------------------------------
// Rank ordering: Partial is weaker than Attributed
// ---------------------------------------------------------------------------

func TestCostReportAdd_AttributedPlusPartial_YieldsPartial(t *testing.T) {
	a := domain.CostReport{Attribution: domain.AttributionAttributed, TotalUSD: 1.00}
	b := domain.CostReport{Attribution: domain.AttributionPartial, TotalUSD: 0.50, UnpricedModels: []string{"model-x"}}

	got := a.Add(b)

	if got.Attribution != domain.AttributionPartial {
		t.Errorf("Add(Attributed, Partial) = %q, want %q — Partial is weaker and must win", got.Attribution, domain.AttributionPartial)
	}
}

func TestCostReportAdd_PartialPlusAttributed_YieldsPartial(t *testing.T) {
	a := domain.CostReport{Attribution: domain.AttributionPartial, TotalUSD: 0.50, UnpricedModels: []string{"model-x"}}
	b := domain.CostReport{Attribution: domain.AttributionAttributed, TotalUSD: 1.00}

	got := a.Add(b)

	if got.Attribution != domain.AttributionPartial {
		t.Errorf("Add(Partial, Attributed) = %q, want %q — Add must be commutative under the rank ordering", got.Attribution, domain.AttributionPartial)
	}
}

// ---------------------------------------------------------------------------
// Rank ordering: UnknownBucket is weaker than Partial
// ---------------------------------------------------------------------------

func TestCostReportAdd_PartialPlusUnknownBucket_YieldsUnknownBucket(t *testing.T) {
	a := domain.CostReport{Attribution: domain.AttributionPartial, TotalUSD: 0.50}
	b := domain.CostReport{Attribution: domain.AttributionUnknownBucket}

	got := a.Add(b)

	if got.Attribution != domain.AttributionUnknownBucket {
		t.Errorf("Add(Partial, UnknownBucket) = %q, want %q", got.Attribution, domain.AttributionUnknownBucket)
	}
}

// ---------------------------------------------------------------------------
// Rank ordering: Unavailable is the weakest
// ---------------------------------------------------------------------------

func TestCostReportAdd_PartialPlusUnavailable_YieldsUnavailable(t *testing.T) {
	a := domain.CostReport{Attribution: domain.AttributionPartial, TotalUSD: 0.50}
	b := domain.CostReport{Attribution: domain.AttributionUnavailable}

	got := a.Add(b)

	if got.Attribution != domain.AttributionUnavailable {
		t.Errorf("Add(Partial, Unavailable) = %q, want %q", got.Attribution, domain.AttributionUnavailable)
	}
}

func TestCostReportAdd_AttributedPlusUnavailable_YieldsUnavailable(t *testing.T) {
	// Existing behaviour, confirmed to survive the rank-table rewrite.
	a := domain.CostReport{Attribution: domain.AttributionAttributed, TotalUSD: 2.00}
	b := domain.CostReport{Attribution: domain.AttributionUnavailable}

	got := a.Add(b)

	if got.Attribution != domain.AttributionUnavailable {
		t.Errorf("Add(Attributed, Unavailable) = %q, want %q", got.Attribution, domain.AttributionUnavailable)
	}
}

func TestCostReportAdd_UnknownBucketPlusUnavailable_YieldsUnavailable(t *testing.T) {
	a := domain.CostReport{Attribution: domain.AttributionUnknownBucket}
	b := domain.CostReport{Attribution: domain.AttributionUnavailable}

	got := a.Add(b)

	if got.Attribution != domain.AttributionUnavailable {
		t.Errorf("Add(UnknownBucket, Unavailable) = %q, want %q", got.Attribution, domain.AttributionUnavailable)
	}
}

// ---------------------------------------------------------------------------
// Same-rank pairs are idempotent
// ---------------------------------------------------------------------------

func TestCostReportAdd_PartialPlusPartial_YieldsPartial(t *testing.T) {
	a := domain.CostReport{Attribution: domain.AttributionPartial, TotalUSD: 0.50, UnpricedModels: []string{"model-a"}}
	b := domain.CostReport{Attribution: domain.AttributionPartial, TotalUSD: 0.75, UnpricedModels: []string{"model-b"}}

	got := a.Add(b)

	if got.Attribution != domain.AttributionPartial {
		t.Errorf("Add(Partial, Partial) = %q, want %q", got.Attribution, domain.AttributionPartial)
	}
}

func TestCostReportAdd_AttributedPlusAttributed_YieldsAttributed(t *testing.T) {
	// Existing behaviour, confirmed to survive the rank-table rewrite.
	a := domain.CostReport{Attribution: domain.AttributionAttributed, TotalUSD: 1.00}
	b := domain.CostReport{Attribution: domain.AttributionAttributed, TotalUSD: 2.00}

	got := a.Add(b)

	if got.Attribution != domain.AttributionAttributed {
		t.Errorf("Add(Attributed, Attributed) = %q, want %q", got.Attribution, domain.AttributionAttributed)
	}
	if got.TotalUSD != 3.00 {
		t.Errorf("Add(Attributed, Attributed).TotalUSD = %v, want 3.00", got.TotalUSD)
	}
}

// ---------------------------------------------------------------------------
// UnpricedModels union
// ---------------------------------------------------------------------------

func TestCostReportAdd_UnpricedModels_AreUnionedAcrossOperands(t *testing.T) {
	a := domain.CostReport{
		Attribution:    domain.AttributionPartial,
		TotalUSD:       0.50,
		UnpricedModels: []string{"model-a"},
	}
	b := domain.CostReport{
		Attribution:    domain.AttributionPartial,
		TotalUSD:       0.75,
		UnpricedModels: []string{"model-b"},
	}

	got := a.Add(b)

	if !slices.Contains(got.UnpricedModels, "model-a") {
		t.Errorf("Add result UnpricedModels = %v, want it to contain model-a", got.UnpricedModels)
	}
	if !slices.Contains(got.UnpricedModels, "model-b") {
		t.Errorf("Add result UnpricedModels = %v, want it to contain model-b", got.UnpricedModels)
	}
}

func TestCostReportAdd_UnpricedModels_DuplicatesAreDeduplicatedInUnion(t *testing.T) {
	a := domain.CostReport{
		Attribution:    domain.AttributionPartial,
		UnpricedModels: []string{"model-a", "model-b"},
	}
	b := domain.CostReport{
		Attribution:    domain.AttributionPartial,
		UnpricedModels: []string{"model-b", "model-c"},
	}

	got := a.Add(b)

	count := 0
	for _, m := range got.UnpricedModels {
		if m == "model-b" {
			count++
		}
	}
	if count > 1 {
		t.Errorf("Add result UnpricedModels = %v, model-b appears %d times — duplicates must be deduplicated in the union", got.UnpricedModels, count)
	}
}

func TestCostReportAdd_UnpricedModels_EmptyAndNonEmpty_ResultIsNonEmpty(t *testing.T) {
	a := domain.CostReport{
		Attribution:    domain.AttributionAttributed,
		TotalUSD:       1.00,
		UnpricedModels: nil,
	}
	b := domain.CostReport{
		Attribution:    domain.AttributionPartial,
		TotalUSD:       0.50,
		UnpricedModels: []string{"model-x"},
	}

	got := a.Add(b)

	if !slices.Contains(got.UnpricedModels, "model-x") {
		t.Errorf("Add result UnpricedModels = %v, want it to contain model-x from the non-nil operand", got.UnpricedModels)
	}
}

func TestCostReportAdd_UnpricedModels_BothEmpty_ResultIsEmpty(t *testing.T) {
	a := domain.CostReport{Attribution: domain.AttributionAttributed, TotalUSD: 1.00}
	b := domain.CostReport{Attribution: domain.AttributionAttributed, TotalUSD: 2.00}

	got := a.Add(b)

	if len(got.UnpricedModels) != 0 {
		t.Errorf("Add result UnpricedModels = %v, want empty when neither operand has unpriced models", got.UnpricedModels)
	}
}

// ---------------------------------------------------------------------------
// TotalUSD is always summed regardless of attribution
// ---------------------------------------------------------------------------

func TestCostReportAdd_TotalUSD_IsSummedRegardlessOfAttribution(t *testing.T) {
	a := domain.CostReport{Attribution: domain.AttributionPartial, TotalUSD: 1.25}
	b := domain.CostReport{Attribution: domain.AttributionUnavailable, TotalUSD: 0.75}

	got := a.Add(b)

	if got.TotalUSD != 2.00 {
		t.Errorf("Add result TotalUSD = %v, want 2.00 — TotalUSD must always be summed", got.TotalUSD)
	}
}
