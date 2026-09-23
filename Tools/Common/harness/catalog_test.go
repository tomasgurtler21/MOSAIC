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
	if entry.AgentsDir != "" {
		t.Errorf("want zero-valued AgentsDir for unknown identity, got %q", entry.AgentsDir)
	}
	if entry.LoadingMechanism != harness.LoadingMechanismUnset {
		t.Errorf("want zero-valued LoadingMechanism (%v) for unknown identity, got %v", harness.LoadingMechanismUnset, entry.LoadingMechanism)
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

// ---------------------------------------------------------------------------
// AgentsDir field: non-empty convention and per-harness expected values
//
// AgentsDir is the harness-convention relative path to the project-level
// agents directory. These tests assert that every catalog entry has a
// non-empty AgentsDir and that each known harness resolves to the correct
// path as defined by the Deployment tool's builtin harness descriptors.
// ---------------------------------------------------------------------------

// TestCLIHarnesses_EveryEntryHasNonEmptyAgentsDir verifies that every catalog
// entry declares a non-empty agents directory convention. A missing value
// would prevent Runner from computing the snapshot directory path at runtime.
func TestCLIHarnesses_EveryEntryHasNonEmptyAgentsDir(t *testing.T) {
	for _, e := range harness.CLIHarnesses() {
		if e.AgentsDir == "" {
			t.Errorf("catalog entry %q has an empty AgentsDir: every CLI harness must declare its agents directory convention", e.ID)
		}
	}
}

// TestLookupCLIHarness_ClaudeCodeHasExpectedAgentsDir verifies that the
// claude-code catalog entry uses the ".claude/agents" convention, matching
// the claude-code.yaml descriptor's paths.agents.project value.
func TestLookupCLIHarness_ClaudeCodeHasExpectedAgentsDir(t *testing.T) {
	const want = ".claude/agents"
	entry, ok := harness.LookupCLIHarness(harness.HarnessIDClaudeCode)
	if !ok {
		t.Fatalf("want %q to be found in the catalog", harness.HarnessIDClaudeCode)
	}
	if entry.AgentsDir != want {
		t.Errorf("want AgentsDir == %q for %q, got %q", want, harness.HarnessIDClaudeCode, entry.AgentsDir)
	}
}

// TestLookupCLIHarness_OpenCodeHasExpectedAgentsDir verifies that the
// opencode catalog entry uses the ".opencode/agents" convention, matching
// the opencode.yaml descriptor's paths.agents.project value.
func TestLookupCLIHarness_OpenCodeHasExpectedAgentsDir(t *testing.T) {
	const want = ".opencode/agents"
	entry, ok := harness.LookupCLIHarness(harness.HarnessIDOpenCode)
	if !ok {
		t.Fatalf("want %q to be found in the catalog", harness.HarnessIDOpenCode)
	}
	if entry.AgentsDir != want {
		t.Errorf("want AgentsDir == %q for %q, got %q", want, harness.HarnessIDOpenCode, entry.AgentsDir)
	}
}

// TestLookupCLIHarness_GHCPCLIHasExpectedAgentsDir verifies that the
// ghcp-cli catalog entry uses the ".github/agents" convention, matching
// the ghcp-cli.yaml descriptor's paths.agents.project value. Note: the
// correct value is ".github/agents", NOT ".ghcp/agents".
func TestLookupCLIHarness_GHCPCLIHasExpectedAgentsDir(t *testing.T) {
	const want = ".github/agents"
	entry, ok := harness.LookupCLIHarness(harness.HarnessIDGHCPCLI)
	if !ok {
		t.Fatalf("want %q to be found in the catalog", harness.HarnessIDGHCPCLI)
	}
	if entry.AgentsDir != want {
		t.Errorf("want AgentsDir == %q for %q, got %q", want, harness.HarnessIDGHCPCLI, entry.AgentsDir)
	}
}

// ---------------------------------------------------------------------------
// LoadingMechanism field: validity, per-harness values, and zero-value for
// unknown identities.
//
// The LoadingMechanism field records how a harness locates agent definition
// files. Path-based harnesses (claude-code) copy agents to a snapshot dir
// and pass a file path. Name-based harnesses (opencode, ghcp-cli) modify
// agents in-place and pass only the agent name.
// ---------------------------------------------------------------------------

// TestCLIHarnesses_EveryEntryHasValidLoadingMechanism verifies that every
// catalog entry carries a non-zero LoadingMechanism. A zero value
// (LoadingMechanismUnset) means the field was never assigned, which would
// cause the Runner to refuse the run rather than silently misbehave.
func TestCLIHarnesses_EveryEntryHasValidLoadingMechanism(t *testing.T) {
	for _, e := range harness.CLIHarnesses() {
		if e.LoadingMechanism == harness.LoadingMechanismUnset {
			t.Errorf("catalog entry %q has LoadingMechanism == LoadingMechanismUnset (zero value): every CLI harness must declare a loading mechanism", e.ID)
		}
	}
}

// TestLookupCLIHarness_ClaudeCodeIsPathBased verifies that the claude-code
// harness uses path-based agent loading. Claude Code receives a full file
// path to the agent definition file, so it uses the copy-and-invoke snapshot
// strategy.
func TestLookupCLIHarness_ClaudeCodeIsPathBased(t *testing.T) {
	entry, ok := harness.LookupCLIHarness(harness.HarnessIDClaudeCode)
	if !ok {
		t.Fatalf("want %q to be found in the catalog", harness.HarnessIDClaudeCode)
	}
	if entry.LoadingMechanism != harness.LoadingMechanismPath {
		t.Errorf("want LoadingMechanism == LoadingMechanismPath for %q, got %v", harness.HarnessIDClaudeCode, entry.LoadingMechanism)
	}
}

// TestLookupCLIHarness_OpenCodeIsNameBased verifies that the opencode
// harness uses name-based agent loading. OpenCode resolves agents by name
// from the agents directory, so it uses the backup-and-transform snapshot
// strategy.
func TestLookupCLIHarness_OpenCodeIsNameBased(t *testing.T) {
	entry, ok := harness.LookupCLIHarness(harness.HarnessIDOpenCode)
	if !ok {
		t.Fatalf("want %q to be found in the catalog", harness.HarnessIDOpenCode)
	}
	if entry.LoadingMechanism != harness.LoadingMechanismName {
		t.Errorf("want LoadingMechanism == LoadingMechanismName for %q, got %v", harness.HarnessIDOpenCode, entry.LoadingMechanism)
	}
}

// TestLookupCLIHarness_GHCPCLIIsNameBased verifies that the ghcp-cli
// harness uses name-based agent loading. GHCP CLI resolves agents by name
// from the agents directory, so it uses the backup-and-transform snapshot
// strategy.
func TestLookupCLIHarness_GHCPCLIIsNameBased(t *testing.T) {
	entry, ok := harness.LookupCLIHarness(harness.HarnessIDGHCPCLI)
	if !ok {
		t.Fatalf("want %q to be found in the catalog", harness.HarnessIDGHCPCLI)
	}
	if entry.LoadingMechanism != harness.LoadingMechanismName {
		t.Errorf("want LoadingMechanism == LoadingMechanismName for %q, got %v", harness.HarnessIDGHCPCLI, entry.LoadingMechanism)
	}
}

// TestLoadingMechanism_PathAndNameAreDistinct verifies that
// LoadingMechanismPath and LoadingMechanismName have distinct values. If they
// were equal a harness assigned one would silently behave as the other.
func TestLoadingMechanism_PathAndNameAreDistinct(t *testing.T) {
	if harness.LoadingMechanismPath == harness.LoadingMechanismName {
		t.Errorf("want LoadingMechanismPath != LoadingMechanismName: both have value %v", harness.LoadingMechanismPath)
	}
}

// TestLoadingMechanism_UnsetIsZeroValue verifies that LoadingMechanismUnset
// is the zero value of the LoadingMechanism type. This guarantees that a
// CLIHarness literal that omits the field (keyed or unkeyed) is always
// detected as unconfigured rather than silently treated as path-based or
// name-based.
func TestLoadingMechanism_UnsetIsZeroValue(t *testing.T) {
	var m harness.LoadingMechanism
	if m != harness.LoadingMechanismUnset {
		t.Errorf("want zero value of LoadingMechanism to be LoadingMechanismUnset, got %v", m)
	}
}

// TestLoadingMechanism_StringRepresentations verifies that the String method
// returns a non-empty, meaningful label for each defined constant and an
// "unknown" marker for unrecognized values. This ensures log output and
// error messages are human-readable rather than bare integers.
func TestLoadingMechanism_StringRepresentations(t *testing.T) {
	cases := []struct {
		mechanism harness.LoadingMechanism
		wantEmpty bool
	}{
		{harness.LoadingMechanismUnset, false},
		{harness.LoadingMechanismPath, false},
		{harness.LoadingMechanismName, false},
		{harness.LoadingMechanism(99), false}, // unrecognized value must still produce output
	}
	for _, tc := range cases {
		s := tc.mechanism.String()
		if s == "" {
			t.Errorf("LoadingMechanism(%d).String() returned empty string: want a non-empty label", int(tc.mechanism))
		}
	}
	// Unrecognized values should contain the numeric value for diagnostics.
	unknown := harness.LoadingMechanism(99).String()
	if !containsSubstring(unknown, "99") {
		t.Errorf("LoadingMechanism(99).String() = %q: want it to contain the numeric value 99 for diagnostics", unknown)
	}
}

// containsSubstring is a local helper used to avoid importing "strings" in the
// test file solely for this check.
func containsSubstring(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
