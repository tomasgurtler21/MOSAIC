package harness_test

// Tests for the shared CLI harness catalog: enumeration order, lookup, and
// entry validity (AC3.1-AC3.6). The catalog's implementation (catalog.go)
// does not exist yet at the time these tests are written; they are expected
// to fail to compile/pass until it is added (TDD RED phase).

import (
	"testing"

	"mosaic-common/harness"
)

// ---------------------------------------------------------------------------
// Enumeration
// ---------------------------------------------------------------------------

func TestCLIHarnesses_EnumeratesInStableDeclaredOrder(t *testing.T) {
	entries := harness.CLIHarnesses()

	if len(entries) != 2 {
		t.Fatalf("want 2 catalog entries, got %d: %+v", len(entries), entries)
	}
	if entries[0].ID != harness.HarnessIDClaudeCode {
		t.Errorf("want entries[0].ID == %q, got %q", harness.HarnessIDClaudeCode, entries[0].ID)
	}
	if entries[1].ID != harness.HarnessIDOpenCode {
		t.Errorf("want entries[1].ID == %q, got %q", harness.HarnessIDOpenCode, entries[1].ID)
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
