package harness_test

// Tests for the shared CLI harness catalog: invariant-based assertions on
// enumeration order, defensive-copy behaviour, entry validity, lookup and
// identity acceptance. Tests assert catalog properties rather than catalog
// content so that adding a new entry breaks only the things it genuinely
// affects.

import (
	"testing"

	"mosaic-common/harness"
)

// ---------------------------------------------------------------------------
// Enumeration
// ---------------------------------------------------------------------------

// TestCLIHarnesses_IsNonEmpty verifies the catalog always has at least one
// entry. A count assertion would break on every future addition; this
// assertion breaks only if someone removes all entries.
func TestCLIHarnesses_IsNonEmpty(t *testing.T) {
	entries := harness.CLIHarnesses()
	if len(entries) == 0 {
		t.Fatal("CLIHarnesses() returned an empty slice: the catalog must have at least one entry")
	}
}

// TestCLIHarnesses_AllDeclaredConstantsAppearAsEntries verifies that every
// exported HarnessID* constant the package declares has a corresponding
// catalog entry. A constant without an entry is a silent dead value; an entry
// without a constant cannot be safely switched on in a composition root.
func TestCLIHarnesses_AllDeclaredConstantsAppearAsEntries(t *testing.T) {
	// List every exported HarnessID* constant this package declares. When a
	// new constant is added to catalog.go it must be added here too, giving
	// a reviewer a single place to confirm both sides of the contract.
	knownIDs := []string{
		harness.HarnessIDClaudeCode,
		harness.HarnessIDOpenCode,
		harness.HarnessIDGHCPCLI,
	}
	for _, id := range knownIDs {
		if _, ok := harness.LookupCLIHarness(id); !ok {
			t.Errorf("exported constant %q has no corresponding catalog entry in CLIHarnesses()", id)
		}
	}
}

func TestCLIHarnesses_OrderIsStableAcrossCalls(t *testing.T) {
	first := harness.CLIHarnesses()
	second := harness.CLIHarnesses()

	if len(first) != len(second) {
		t.Fatalf("want same length across calls, got %d and %d", len(first), len(second))
	}
	for i := range first {
		if first[i].ID != second[i].ID {
			t.Errorf("order differs at index %d: %q vs %q", i, first[i].ID, second[i].ID)
		}
	}
}

func TestCLIHarnesses_ReturnsFreshSliceNotSharedWithCaller(t *testing.T) {
	entries := harness.CLIHarnesses()
	if len(entries) == 0 {
		t.Fatalf("want at least one entry to mutate for this test")
	}

	original := entries[0].ID
	entries[0].ID = "corrupted"

	fresh := harness.CLIHarnesses()
	if fresh[0].ID != original {
		t.Errorf("mutating a returned slice affected a later call: want %q, got %q", original, fresh[0].ID)
	}
}

// ---------------------------------------------------------------------------
// Entry validity: non-empty identity and label, uniqueness
// ---------------------------------------------------------------------------

func TestCLIHarnesses_EveryEntryHasNonEmptyIDAndLabel(t *testing.T) {
	for _, e := range harness.CLIHarnesses() {
		if e.ID == "" {
			t.Errorf("found entry with empty ID: %+v", e)
		}
		if e.Label == "" {
			t.Errorf("found entry with empty Label (ID=%q)", e.ID)
		}
	}
}

func TestCLIHarnesses_IdentitiesAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, e := range harness.CLIHarnesses() {
		if seen[e.ID] {
			t.Errorf("duplicate catalog ID: %q", e.ID)
		}
		seen[e.ID] = true
	}
}

func TestCLIHarnesses_ClaudeCodeEntryHasExpectedLabel(t *testing.T) {
	entry, ok := harness.LookupCLIHarness(harness.HarnessIDClaudeCode)
	if !ok {
		t.Fatalf("want %q to be found", harness.HarnessIDClaudeCode)
	}
	if entry.Label != "Claude Code CLI" {
		t.Errorf("want Label %q, got %q", "Claude Code CLI", entry.Label)
	}
}

func TestCLIHarnesses_OpenCodeEntryHasExpectedLabel(t *testing.T) {
	entry, ok := harness.LookupCLIHarness(harness.HarnessIDOpenCode)
	if !ok {
		t.Fatalf("want %q to be found", harness.HarnessIDOpenCode)
	}
	if entry.Label != "OpenCode CLI" {
		t.Errorf("want Label %q, got %q", "OpenCode CLI", entry.Label)
	}
}

// ---------------------------------------------------------------------------
// LookupCLIHarness
// ---------------------------------------------------------------------------

func TestLookupCLIHarness_KnownIdentitySucceeds(t *testing.T) {
	entry, ok := harness.LookupCLIHarness(harness.HarnessIDClaudeCode)
	if !ok {
		t.Fatalf("want ok == true for %q", harness.HarnessIDClaudeCode)
	}
	if entry.ID != harness.HarnessIDClaudeCode {
		t.Errorf("want entry.ID == %q, got %q", harness.HarnessIDClaudeCode, entry.ID)
	}
}

func TestLookupCLIHarness_UnknownIdentityFailsWithoutPanicking(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("LookupCLIHarness panicked on unknown identity: %v", r)
		}
	}()

	entry, ok := harness.LookupCLIHarness("does-not-exist")
	if ok {
		t.Fatalf("want ok == false for unknown identity, got entry %+v", entry)
	}
	if entry.ID != "" || entry.Label != "" {
		t.Errorf("want zero-valued entry for unknown identity, got %+v", entry)
	}
}

func TestLookupCLIHarness_EmptyIdentityFailsWithoutPanicking(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("LookupCLIHarness panicked on empty identity: %v", r)
		}
	}()

	if _, ok := harness.LookupCLIHarness(""); ok {
		t.Errorf("want ok == false for empty identity")
	}
}

// ---------------------------------------------------------------------------
// IsCLIHarness
// ---------------------------------------------------------------------------

func TestIsCLIHarness_TrueForKnownIdentities(t *testing.T) {
	if !harness.IsCLIHarness(harness.HarnessIDClaudeCode) {
		t.Errorf("want IsCLIHarness(%q) == true", harness.HarnessIDClaudeCode)
	}
	if !harness.IsCLIHarness(harness.HarnessIDOpenCode) {
		t.Errorf("want IsCLIHarness(%q) == true", harness.HarnessIDOpenCode)
	}
	if !harness.IsCLIHarness(harness.HarnessIDGHCPCLI) {
		t.Errorf("want IsCLIHarness(%q) == true", harness.HarnessIDGHCPCLI)
	}
}

func TestIsCLIHarness_FalseForUnknownIdentity(t *testing.T) {
	if harness.IsCLIHarness("fake") {
		t.Errorf("want IsCLIHarness(%q) == false: %q is Runner's tool-local test double, not a catalog entry", "fake", "fake")
	}
	if harness.IsCLIHarness("unknown-harness") {
		t.Errorf("want IsCLIHarness(%q) == false", "unknown-harness")
	}
}

func TestIsCLIHarness_FalseForEmptyIdentity(t *testing.T) {
	if harness.IsCLIHarness("") {
		t.Errorf("want IsCLIHarness(\"\") == false")
	}
}

// ---------------------------------------------------------------------------
// GHCP CLI catalog entry (T5.2)
//
// These tests pin the ghcp-cli catalog identity and label in the invariant
// style established above: they assert presence and resolves-through-lookup
// without asserting the catalog's size or the entry's index, so they survive
// future additions and the ordering is tested above by the
// TestCLIHarnesses_OrderIsStableAcrossCalls invariant.
// ---------------------------------------------------------------------------

// TestCLIHarnesses_GHCPCLIEntryPresent verifies that HarnessIDGHCPCLI
// resolves through LookupCLIHarness. This confirms the catalog entry exists
// and that the identity constant matches the entry's ID field.
func TestCLIHarnesses_GHCPCLIEntryPresent(t *testing.T) {
	entry, ok := harness.LookupCLIHarness(harness.HarnessIDGHCPCLI)
	if !ok {
		t.Fatalf("want %q to be found in the catalog via LookupCLIHarness, got not-found", harness.HarnessIDGHCPCLI)
	}
	if entry.ID != harness.HarnessIDGHCPCLI {
		t.Errorf("want entry.ID == %q, got %q", harness.HarnessIDGHCPCLI, entry.ID)
	}
}

// TestCLIHarnesses_GHCPCLIEntryHasExpectedLabel verifies that the ghcp-cli
// catalog entry carries the expected human-readable label. The label is
// presentation only but must be correct for selection UIs and help text.
func TestCLIHarnesses_GHCPCLIEntryHasExpectedLabel(t *testing.T) {
	entry, ok := harness.LookupCLIHarness(harness.HarnessIDGHCPCLI)
	if !ok {
		t.Fatalf("want %q to be found in the catalog", harness.HarnessIDGHCPCLI)
	}
	if entry.Label != "GitHub Copilot CLI" {
		t.Errorf("want Label %q, got %q", "GitHub Copilot CLI", entry.Label)
	}
}

// TestIsCLIHarness_TrueForGHCPCLI verifies that IsCLIHarness accepts the
// ghcp-cli identity. This is the predicate Runner's buildTUIDelegate gates on
// to decide whether to offer a harness in the TUI selection step.
func TestIsCLIHarness_TrueForGHCPCLI(t *testing.T) {
	if !harness.IsCLIHarness(harness.HarnessIDGHCPCLI) {
		t.Errorf("want IsCLIHarness(%q) == true", harness.HarnessIDGHCPCLI)
	}
}
