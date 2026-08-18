package domain_test

// Tests for pricing-entry validation semantics (T2.3).
//
// Design invariants under test:
//   - An entry with HasThreshold=false has no long-context tier.
//   - An entry with HasThreshold=true exposes a long-context tier.
//   - Lookup on a model absent from the table returns found=false (the explicit
//     not-configured outcome), never a zero-valued entry.
//   - PricingTable.With returns a copy; the original is not mutated.

import (
	"testing"

	"mosaic-log-analyzer/internal/domain"
)

// ---------------------------------------------------------------------------
// Helpers — build valid entries to keep test cases focused
// ---------------------------------------------------------------------------

// validEntry returns an entry with all required rates set and no long-context tier.
func validEntry(model domain.ModelID) domain.ModelPricing {
	return domain.ModelPricing{
		Model:                model,
		Input:                domain.Rate(3_000_000_000),    // $3.00/1M in nanodollars
		CachedInput:          domain.Rate(300_000_000),      // $0.30/1M
		CacheWrite:           domain.Rate(3_750_000_000),    // $3.75/1M
		OutputUnderThreshold: domain.Rate(15_000_000_000),   // $15.00/1M
		HasThreshold:         false,
	}
}

// validEntryWithThreshold returns a valid entry with a long-context tier.
func validEntryWithThreshold(model domain.ModelID) domain.ModelPricing {
	e := validEntry(model)
	e.HasThreshold = true
	e.OutputAtThreshold = domain.Rate(22_500_000_000) // $22.50/1M
	e.ContextLengthThreshold = 200_000
	return e
}

// ---------------------------------------------------------------------------
// T2.3 — HasLongContextTier
// ---------------------------------------------------------------------------

func TestModelPricing_HasLongContextTier_False_WhenNoThreshold(t *testing.T) {
	p := validEntry("model-a")
	if p.HasLongContextTier() {
		t.Error("HasLongContextTier must be false when HasThreshold=false")
	}
}

func TestModelPricing_HasLongContextTier_True_WhenThresholdPresent(t *testing.T) {
	p := validEntryWithThreshold("model-a")
	if !p.HasLongContextTier() {
		t.Error("HasLongContextTier must be true when HasThreshold=true")
	}
}

// ---------------------------------------------------------------------------
// T2.3 — Validate
// ---------------------------------------------------------------------------

func TestModelPricing_Validate_ValidWithoutThreshold(t *testing.T) {
	p := validEntry("model-a")
	if err := p.Validate(); err != nil {
		t.Errorf("valid entry without threshold must pass Validate: %v", err)
	}
}

func TestModelPricing_Validate_ValidWithThreshold(t *testing.T) {
	p := validEntryWithThreshold("model-a")
	if err := p.Validate(); err != nil {
		t.Errorf("valid entry with threshold must pass Validate: %v", err)
	}
}

func TestModelPricing_Validate_EmptyModelID_IsInvalid(t *testing.T) {
	p := validEntry("")
	if err := p.Validate(); err == nil {
		t.Error("entry with empty ModelID must fail Validate")
	}
}

func TestModelPricing_Validate_WithThreshold_OutputAtThresholdRequired(t *testing.T) {
	// When HasThreshold=true the OutputAtThreshold rate is required.
	p := validEntry("model-a")
	p.HasThreshold = true
	p.ContextLengthThreshold = 200_000
	p.OutputAtThreshold = 0 // zero rate — treated as missing when threshold present
	// The design requires OutputAtThreshold is "required only when HasThreshold is true".
	// An entry with a zero rate when a threshold is claimed is invalid.
	if err := p.Validate(); err == nil {
		t.Error("entry with HasThreshold=true and zero OutputAtThreshold must fail Validate")
	}
}

func TestModelPricing_Validate_EachRequiredRate_MissingIsInvalid(t *testing.T) {
	// Each of the four always-required rates must be non-zero; a zero rate signals
	// a missing entry. The design states "an entry missing any of the four
	// always-required rates is not [valid]". This table pins each branch
	// independently so a partial implementation (e.g. only checking Input) is
	// caught before other rates are added.
	cases := []struct {
		name   string
		adjust func(*domain.ModelPricing)
	}{
		{"Input=0", func(p *domain.ModelPricing) { p.Input = 0 }},
		{"CachedInput=0", func(p *domain.ModelPricing) { p.CachedInput = 0 }},
		{"CacheWrite=0", func(p *domain.ModelPricing) { p.CacheWrite = 0 }},
		{"OutputUnderThreshold=0", func(p *domain.ModelPricing) { p.OutputUnderThreshold = 0 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := validEntry("model-a")
			tc.adjust(&p)
			if err := p.Validate(); err == nil {
				t.Errorf("entry with %s must fail Validate (zero rate signals missing)", tc.name)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// T2.3 — PricingTable — Lookup not-configured outcome
// ---------------------------------------------------------------------------

func TestPricingTable_Lookup_Empty_IsNotConfigured(t *testing.T) {
	table := domain.EmptyPricingTable()
	_, found := table.Lookup("any-model")
	if found {
		t.Error("Lookup on empty table must return found=false (explicit not-configured outcome)")
	}
}

func TestPricingTable_Lookup_AbsentModel_IsNotConfigured(t *testing.T) {
	// Table has one model; lookup for a different one must return not-configured.
	table := domain.NewPricingTable([]domain.ModelPricing{validEntry("model-a")})
	_, found := table.Lookup("model-b")
	if found {
		t.Error("Lookup for absent model must return found=false, not a zero-valued entry")
	}
}

func TestPricingTable_Lookup_PresentModel_IsFound(t *testing.T) {
	entry := validEntry("model-a")
	table := domain.NewPricingTable([]domain.ModelPricing{entry})
	got, found := table.Lookup("model-a")
	if !found {
		t.Fatal("Lookup must find a model that was added to the table")
	}
	if got.Model != "model-a" {
		t.Errorf("Lookup returned wrong model: %q", got.Model)
	}
}

func TestPricingTable_Lookup_ReturnsCorrectEntry(t *testing.T) {
	entryA := validEntry("model-a")
	entryB := validEntryWithThreshold("model-b")
	table := domain.NewPricingTable([]domain.ModelPricing{entryA, entryB})

	gotA, foundA := table.Lookup("model-a")
	if !foundA {
		t.Fatal("Lookup(model-a) not found")
	}
	if gotA.HasThreshold {
		t.Error("model-a must not have a threshold")
	}

	gotB, foundB := table.Lookup("model-b")
	if !foundB {
		t.Fatal("Lookup(model-b) not found")
	}
	if !gotB.HasThreshold {
		t.Error("model-b must have a threshold")
	}
	if gotB.ContextLengthThreshold != 200_000 {
		t.Errorf("model-b ContextLengthThreshold = %d, want 200000", gotB.ContextLengthThreshold)
	}
}

// ---------------------------------------------------------------------------
// T2.3 — PricingTable — structural properties
// ---------------------------------------------------------------------------

func TestPricingTable_Len_Empty(t *testing.T) {
	if got := domain.EmptyPricingTable().Len(); got != 0 {
		t.Errorf("EmptyPricingTable.Len() = %d, want 0", got)
	}
}

func TestPricingTable_Len_AfterInsert(t *testing.T) {
	table := domain.NewPricingTable([]domain.ModelPricing{
		validEntry("m1"),
		validEntry("m2"),
	})
	if got := table.Len(); got != 2 {
		t.Errorf("table with 2 entries Len() = %d, want 2", got)
	}
}

func TestPricingTable_Models_Sorted(t *testing.T) {
	table := domain.NewPricingTable([]domain.ModelPricing{
		validEntry("zzz-model"),
		validEntry("aaa-model"),
		validEntry("mmm-model"),
	})
	models := table.Models()
	if len(models) != 3 {
		t.Fatalf("Models() len = %d, want 3", len(models))
	}
	if models[0] != "aaa-model" || models[1] != "mmm-model" || models[2] != "zzz-model" {
		t.Errorf("Models() not sorted ascending: %v", models)
	}
}

func TestPricingTable_Models_Empty(t *testing.T) {
	models := domain.EmptyPricingTable().Models()
	if len(models) != 0 {
		t.Errorf("EmptyPricingTable.Models() len = %d, want 0", len(models))
	}
}

// ---------------------------------------------------------------------------
// T2.3 — PricingTable.With — immutability contract
// ---------------------------------------------------------------------------

func TestPricingTable_With_AddsEntry(t *testing.T) {
	entry := validEntry("model-a")
	table := domain.EmptyPricingTable().With(entry)
	_, found := table.Lookup("model-a")
	if !found {
		t.Error("With() must make the entry findable in the returned table")
	}
}

func TestPricingTable_With_DoesNotMutateOriginal(t *testing.T) {
	original := domain.EmptyPricingTable()
	_ = original.With(validEntry("model-a"))
	// The original must still be empty.
	if _, found := original.Lookup("model-a"); found {
		t.Error("With() must return a copy; the original table must not be mutated")
	}
	if original.Len() != 0 {
		t.Errorf("original.Len() = %d after With(), want 0", original.Len())
	}
}

func TestPricingTable_With_ReplacesExistingEntry(t *testing.T) {
	original := validEntry("model-a")
	updated := validEntryWithThreshold("model-a")

	table := domain.NewPricingTable([]domain.ModelPricing{original}).With(updated)
	got, found := table.Lookup("model-a")
	if !found {
		t.Fatal("model-a must still be findable after replacement")
	}
	if !got.HasThreshold {
		t.Error("With() must replace the entry; updated entry must have HasThreshold=true")
	}
	if table.Len() != 1 {
		t.Errorf("Len() after replacement = %d, want 1 (no duplicate)", table.Len())
	}
}

func TestPricingTable_With_PreservesOtherEntries(t *testing.T) {
	table := domain.NewPricingTable([]domain.ModelPricing{
		validEntry("model-a"),
		validEntry("model-b"),
	}).With(validEntryWithThreshold("model-c"))

	for _, id := range []domain.ModelID{"model-a", "model-b", "model-c"} {
		if _, found := table.Lookup(id); !found {
			t.Errorf("With() must preserve existing entry %q", id)
		}
	}
	if table.Len() != 3 {
		t.Errorf("Len() = %d, want 3 after adding one new entry", table.Len())
	}
}
