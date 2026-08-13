package harness_test

// Tests for Runner's harness selection set: the composition of the shared CLI
// harness catalog with Runner's tool-local "fake" test double, consumed by
// internal/cli's flag surfaces and internal/tui/screens' configuration screen.

import (
	"strings"
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

// TestSelections_IncludesGHCPCLI verifies that "ghcp-cli" is among the
// accepted selections once the catalog carries it.
func TestSelections_IncludesGHCPCLI(t *testing.T) {
	sels := harness.Selections()
	found := false
	for _, s := range sels {
		if s.ID == commonharness.HarnessIDGHCPCLI {
			found = true
		}
	}
	if !found {
		t.Errorf("want %q among Selections(), got %+v", commonharness.HarnessIDGHCPCLI, sels)
	}
}

// TestSelections_GHCPCLIIsCLIBacked verifies that the ghcp-cli selection
// is marked as CLI-backed so it is offered by the TUI selection step.
func TestSelections_GHCPCLIIsCLIBacked(t *testing.T) {
	sels := harness.Selections()
	for _, s := range sels {
		if s.ID == commonharness.HarnessIDGHCPCLI {
			if !s.CLIBacked {
				t.Errorf("want Selections entry for %q to have CLIBacked == true", commonharness.HarnessIDGHCPCLI)
			}
			return
		}
	}
	t.Errorf("want %q among Selections() to check CLIBacked, not found in %+v", commonharness.HarnessIDGHCPCLI, sels)
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

// TestAccepts_TrueForGHCPCLI verifies that "ghcp-cli" is an accepted
// --harness value once the catalog carries it.
func TestAccepts_TrueForGHCPCLI(t *testing.T) {
	if !harness.Accepts(commonharness.HarnessIDGHCPCLI) {
		t.Errorf("want Accepts(%q) == true for the ghcp-cli catalog entry", commonharness.HarnessIDGHCPCLI)
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

// TestFlagValues_PipeSeparatedInAcceptedOrder verifies that FlagValues()
// produces a pipe-separated string in the documented accepted order: the
// test double first, then every catalog entry in catalog order. Asserting
// this from Selections() and CLIHarnesses() rather than a frozen string
// means a catalog addition breaks this test only if the ordering contract
// changes, not just because one more harness exists.
func TestFlagValues_PipeSeparatedInAcceptedOrder(t *testing.T) {
	got := harness.FlagValues()

	// No leading or trailing separator.
	if strings.HasPrefix(got, "|") {
		t.Errorf("FlagValues() = %q: has leading separator", got)
	}
	if strings.HasSuffix(got, "|") {
		t.Errorf("FlagValues() = %q: has trailing separator", got)
	}

	parts := strings.Split(got, "|")
	sels := harness.Selections()

	if len(parts) != len(sels) {
		t.Fatalf("FlagValues() = %q: %d pipe-separated tokens, want %d (one per selection in Selections())", got, len(parts), len(sels))
	}

	// First token must be the test double (test-double-first ordering contract).
	if parts[0] != harness.FakeHarnessID {
		t.Errorf("FlagValues() first token = %q, want FakeHarnessID %q: test double must come first", parts[0], harness.FakeHarnessID)
	}

	// Remaining tokens must match CLIHarnesses() in catalog order so a silent
	// reordering inside the catalog is still caught here.
	catalog := commonharness.CLIHarnesses()
	for i, entry := range catalog {
		if parts[i+1] != entry.ID {
			t.Errorf("FlagValues() token %d = %q, want %q (catalog entry in catalog order)", i+1, parts[i+1], entry.ID)
		}
	}
}

// TestFlagValueList_CommaSeparatedInAcceptedOrder verifies that FlagValueList()
// produces a comma-space-separated string in the documented accepted order:
// the test double first, then every catalog entry in catalog order. Asserting
// this from Selections() and CLIHarnesses() rather than a frozen string means
// a catalog addition breaks this test only if the ordering contract changes.
func TestFlagValueList_CommaSeparatedInAcceptedOrder(t *testing.T) {
	got := harness.FlagValueList()

	// No leading or trailing separator.
	if strings.HasPrefix(got, ", ") {
		t.Errorf("FlagValueList() = %q: has leading separator", got)
	}
	if strings.HasSuffix(got, ", ") {
		t.Errorf("FlagValueList() = %q: has trailing separator", got)
	}

	parts := strings.Split(got, ", ")
	sels := harness.Selections()

	if len(parts) != len(sels) {
		t.Fatalf("FlagValueList() = %q: %d comma-space-separated tokens, want %d (one per selection in Selections())", got, len(parts), len(sels))
	}

	// First token must be the test double (test-double-first ordering contract).
	if parts[0] != harness.FakeHarnessID {
		t.Errorf("FlagValueList() first token = %q, want FakeHarnessID %q: test double must come first", parts[0], harness.FakeHarnessID)
	}

	// Remaining tokens must match CLIHarnesses() in catalog order so a silent
	// reordering inside the catalog is still caught here.
	catalog := commonharness.CLIHarnesses()
	for i, entry := range catalog {
		if parts[i+1] != entry.ID {
			t.Errorf("FlagValueList() token %d = %q, want %q (catalog entry in catalog order)", i+1, parts[i+1], entry.ID)
		}
	}
}
