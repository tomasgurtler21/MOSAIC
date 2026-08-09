package harness_test

// Tests for Runner's harness selection set (T4.4): the composition of the
// shared CLI harness catalog with Runner's tool-local "fake" test double,
// consumed by internal/cli's flag surfaces and internal/tui/screens'
// configuration screen.

import (
	"testing"

	commonharness "mosaic-common/harness"

	"mosaic-run/internal/harness"
)

// ---------------------------------------------------------------------------
// Selections
// ---------------------------------------------------------------------------

// TestSelections_FirstEntryIsFake verifies that Selections()[0] is the
// tool-local test double, so the flag's default and the list's head agree
// without either restating the other.
func TestSelections_FirstEntryIsFake(t *testing.T) {
	sels := harness.Selections()
	if len(sels) == 0 {
		t.Fatalf("want at least one selection, got none")
	}
	if sels[0].ID != harness.FakeHarnessID {
		t.Errorf("want Selections()[0].ID == %q, got %q", harness.FakeHarnessID, sels[0].ID)
	}
	if sels[0].CLIBacked {
		t.Errorf("want Selections()[0].CLIBacked == false for the tool-local test double")
	}
}

// TestSelections_ContainsEveryCatalogEntryInCatalogOrder verifies that every
// catalog entry follows the fake entry, preserving the catalog's declared
// order.
func TestSelections_ContainsEveryCatalogEntryInCatalogOrder(t *testing.T) {
	sels := harness.Selections()
	catalog := commonharness.CLIHarnesses()

	if len(sels) != len(catalog)+1 {
		t.Fatalf("want %d selections (fake + %d catalog entries), got %d: %+v", len(catalog)+1, len(catalog), len(sels), sels)
	}
	for i, entry := range catalog {
		got := sels[i+1]
		if got.ID != entry.ID {
			t.Errorf("selection %d: ID = %q, want %q (catalog order)", i+1, got.ID, entry.ID)
		}
		if got.Label != entry.Label {
			t.Errorf("selection %d: Label = %q, want %q", i+1, got.Label, entry.Label)
		}
		if !got.CLIBacked {
			t.Errorf("selection %d (%q): want CLIBacked == true for a catalog entry", i+1, got.ID)
		}
	}
}

// TestSelections_IncludesOpenCode verifies that "opencode" is among the
// accepted selections once the catalog carries it.
func TestSelections_IncludesOpenCode(t *testing.T) {
	sels := harness.Selections()
	found := false
	for _, s := range sels {
		if s.ID == commonharness.HarnessIDOpenCode {
			found = true
		}
	}
	if !found {
		t.Errorf("want %q among Selections(), got %+v", commonharness.HarnessIDOpenCode, sels)
	}
}

// ---------------------------------------------------------------------------
// Accepts
// ---------------------------------------------------------------------------

// TestAccepts_TrueForFake verifies that the tool-local test double is
// accepted.
func TestAccepts_TrueForFake(t *testing.T) {
	if !harness.Accepts(harness.FakeHarnessID) {
		t.Errorf("want Accepts(%q) == true", harness.FakeHarnessID)
	}
}

// TestAccepts_TrueForEveryCatalogEntry verifies that every catalog-declared
// CLI harness passes validation.
func TestAccepts_TrueForEveryCatalogEntry(t *testing.T) {
	for _, entry := range commonharness.CLIHarnesses() {
		if !harness.Accepts(entry.ID) {
			t.Errorf("want Accepts(%q) == true for catalog entry", entry.ID)
		}
	}
}

// TestAccepts_FalseForUnknownValue verifies that an unrecognised value is
// rejected.
func TestAccepts_FalseForUnknownValue(t *testing.T) {
	if harness.Accepts("not-a-real-harness") {
		t.Errorf("want Accepts(%q) == false", "not-a-real-harness")
	}
}

// TestAccepts_FalseForEmptyValue verifies that an empty value is rejected.
func TestAccepts_FalseForEmptyValue(t *testing.T) {
	if harness.Accepts("") {
		t.Error("want Accepts(\"\") == false")
	}
}

// ---------------------------------------------------------------------------
// CLISelections
// ---------------------------------------------------------------------------

// TestCLISelections_ExcludesFake verifies that the tool-local test double
// never appears among the CLI-backed subset a selection UI offers.
func TestCLISelections_ExcludesFake(t *testing.T) {
	for _, s := range harness.CLISelections() {
		if s.ID == harness.FakeHarnessID {
			t.Errorf("want %q excluded from CLISelections(), got %+v", harness.FakeHarnessID, s)
		}
	}
}

// TestCLISelections_MatchesCatalogExactly verifies that CLISelections()
// contains exactly the catalog's entries, in the catalog's order.
func TestCLISelections_MatchesCatalogExactly(t *testing.T) {
	sels := harness.CLISelections()
	catalog := commonharness.CLIHarnesses()

	if len(sels) != len(catalog) {
		t.Fatalf("want %d CLI selections, got %d: %+v", len(catalog), len(sels), sels)
	}
	for i, entry := range catalog {
		if sels[i].ID != entry.ID {
			t.Errorf("CLISelections()[%d].ID = %q, want %q", i, sels[i].ID, entry.ID)
		}
		if sels[i].Label != entry.Label {
			t.Errorf("CLISelections()[%d].Label = %q, want %q", i, sels[i].Label, entry.Label)
		}
	}
}

// ---------------------------------------------------------------------------
// FlagValues / FlagValueList
// ---------------------------------------------------------------------------

// TestFlagValues_PipeSeparatedInAcceptedOrder verifies the usage string's
// exact shape: "fake|claude-code|opencode".
func TestFlagValues_PipeSeparatedInAcceptedOrder(t *testing.T) {
	got := harness.FlagValues()
	want := "fake|claude-code|opencode"
	if got != want {
		t.Errorf("FlagValues() = %q, want %q", got, want)
	}
}

// TestFlagValueList_CommaSeparatedInAcceptedOrder verifies the usage-error
// message's exact shape: "fake, claude-code, opencode".
func TestFlagValueList_CommaSeparatedInAcceptedOrder(t *testing.T) {
	got := harness.FlagValueList()
	want := "fake, claude-code, opencode"
	if got != want {
		t.Errorf("FlagValueList() = %q, want %q", got, want)
	}
}
