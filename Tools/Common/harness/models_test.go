package harness_test

// Tests for the per-harness model catalog: enumeration order, accessor
// behaviour, copy safety, seed content, and the invariant that every identity
// catalog entry has model data.

import (
	"testing"

	"mosaic-common/harness"
)

// ---------------------------------------------------------------------------
// ModelCatalogs — enumeration
// ---------------------------------------------------------------------------

// TestModelCatalogs_IsNonEmpty verifies that the catalog always has at least
// one entry. A count assertion would break on every future addition; this
// assertion breaks only if someone removes all entries.
func TestModelCatalogs_IsNonEmpty(t *testing.T) {
	entries := harness.ModelCatalogs()
	if len(entries) == 0 {
		t.Fatal("ModelCatalogs() returned an empty slice: the catalog must have at least one entry")
	}
}

// TestModelCatalogs_OrderIsStableAcrossCalls verifies that successive calls to
// ModelCatalogs() return entries in the same order. The catalog order is part
// of its documented contract.
func TestModelCatalogs_OrderIsStableAcrossCalls(t *testing.T) {
	first := harness.ModelCatalogs()
	second := harness.ModelCatalogs()

	if len(first) != len(second) {
		t.Fatalf("want same length across calls, got %d and %d", len(first), len(second))
	}
	for i := range first {
		if first[i].HarnessID != second[i].HarnessID {
			t.Errorf("order differs at index %d: %q vs %q", i, first[i].HarnessID, second[i].HarnessID)
		}
	}
}

// TestModelCatalogs_OrderMatchesIdentityCatalog verifies that ModelCatalogs()
// returns entries in the same order as CLIHarnesses(). The design document
// requires "the same stable order CLIHarnesses uses".
func TestModelCatalogs_OrderMatchesIdentityCatalog(t *testing.T) {
	identities := harness.CLIHarnesses()
	models := harness.ModelCatalogs()

	if len(models) != len(identities) {
		t.Fatalf("ModelCatalogs() has %d entries but CLIHarnesses() has %d: every identity catalog entry must have a model catalog entry", len(models), len(identities))
	}
	for i := range identities {
		if models[i].HarnessID != identities[i].ID {
			t.Errorf("index %d: ModelCatalogs()[%d].HarnessID = %q, want %q to match CLIHarnesses()[%d].ID", i, i, models[i].HarnessID, identities[i].ID, i)
		}
	}
}

// ---------------------------------------------------------------------------
// ModelCatalogs — entry validity
// ---------------------------------------------------------------------------

// TestModelCatalogs_EveryEntryHasNonEmptyHarnessID verifies that every model
// catalog entry has a non-empty HarnessID.
func TestModelCatalogs_EveryEntryHasNonEmptyHarnessID(t *testing.T) {
	for i, e := range harness.ModelCatalogs() {
		if e.HarnessID == "" {
			t.Errorf("index %d: ModelCatalog has empty HarnessID", i)
		}
	}
}

// TestModelCatalogs_EveryEntryHasNonEmptyIDs verifies that every model catalog
// entry declares at least one model identifier. An entry with no IDs makes a
// harness un-selectable and is almost certainly a seed data omission.
func TestModelCatalogs_EveryEntryHasNonEmptyIDs(t *testing.T) {
	for _, e := range harness.ModelCatalogs() {
		if len(e.IDs) == 0 {
			t.Errorf("harness %q: ModelCatalog has an empty IDs list; every CLI-backed harness must declare at least one model", e.HarnessID)
		}
	}
}

// TestModelCatalogs_EveryEntryHasNonEmptyFormatHint verifies that every model
// catalog entry carries a human-readable format hint. An absent hint leaves
// selection UIs and help text without guidance.
func TestModelCatalogs_EveryEntryHasNonEmptyFormatHint(t *testing.T) {
	for _, e := range harness.ModelCatalogs() {
		if e.FormatHint == "" {
			t.Errorf("harness %q: ModelCatalog has an empty FormatHint; every CLI-backed harness must declare one", e.HarnessID)
		}
	}
}

// TestModelCatalogs_HarnessIDsAreUnique verifies that no two model catalog
// entries share a HarnessID.
func TestModelCatalogs_HarnessIDsAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, e := range harness.ModelCatalogs() {
		if seen[e.HarnessID] {
			t.Errorf("duplicate model catalog HarnessID: %q", e.HarnessID)
		}
		seen[e.HarnessID] = true
	}
}

// ---------------------------------------------------------------------------
// ModelCatalogs — copy safety
// ---------------------------------------------------------------------------

// TestModelCatalogs_ReturnsFreshSliceNotSharedWithCaller verifies that
// mutating the slice returned by ModelCatalogs() does not affect a later call.
func TestModelCatalogs_ReturnsFreshSliceNotSharedWithCaller(t *testing.T) {
	entries := harness.ModelCatalogs()
	if len(entries) == 0 {
		t.Fatalf("want at least one entry to mutate")
	}

	original := entries[0].HarnessID
	entries[0].HarnessID = "corrupted"

	fresh := harness.ModelCatalogs()
	if fresh[0].HarnessID != original {
		t.Errorf("mutating a returned slice affected a later call: want %q, got %q", original, fresh[0].HarnessID)
	}
}

// TestModelCatalogs_IDsSliceIsNotSharedWithCaller verifies that mutating an
// element of the IDs sub-slice inside a ModelCatalogs() result does not affect
// a subsequent call. A shallow copy of the outer []ModelCatalog would copy the
// slice header but leave the backing array shared; this test catches that.
func TestModelCatalogs_IDsSliceIsNotSharedWithCaller(t *testing.T) {
	entries := harness.ModelCatalogs()
	if len(entries) == 0 {
		t.Fatalf("want at least one entry to test IDs copy safety")
	}
	if len(entries[0].IDs) == 0 {
		t.Fatalf("want at least one model ID in entries[0] to mutate")
	}

	original := entries[0].IDs[0]
	entries[0].IDs[0] = "corrupted"

	fresh := harness.ModelCatalogs()
	if len(fresh) == 0 || len(fresh[0].IDs) == 0 {
		t.Fatalf("second ModelCatalogs() call returned empty data")
	}
	if fresh[0].IDs[0] != original {
		t.Errorf("mutating IDs element from ModelCatalogs() affected a later call: want %q, got %q", original, fresh[0].IDs[0])
	}
}

// TestModelCatalogs_IDsMutationDoesNotAffectLookup verifies that mutating an
// IDs element obtained from ModelCatalogs() does not change what
// LookupModelCatalog returns for the same harness. This pins cross-accessor
// isolation: the two accessors must not share the same backing array.
func TestModelCatalogs_IDsMutationDoesNotAffectLookup(t *testing.T) {
	entries := harness.ModelCatalogs()
	if len(entries) == 0 {
		t.Fatalf("want at least one entry for cross-accessor copy-safety test")
	}
	harnessID := entries[0].HarnessID
	if len(entries[0].IDs) == 0 {
		t.Fatalf("want at least one model ID in %q to mutate", harnessID)
	}

	original := entries[0].IDs[0]
	entries[0].IDs[0] = "corrupted"

	cat, ok := harness.LookupModelCatalog(harnessID)
	if !ok {
		t.Fatalf("LookupModelCatalog(%q) returned not-found after ModelCatalogs() mutation", harnessID)
	}
	if len(cat.IDs) == 0 {
		t.Fatalf("LookupModelCatalog(%q) returned empty IDs after ModelCatalogs() mutation", harnessID)
	}
	if cat.IDs[0] != original {
		t.Errorf("mutating IDs from ModelCatalogs() affected LookupModelCatalog: want %q, got %q", original, cat.IDs[0])
	}
}

// ---------------------------------------------------------------------------
// LookupModelCatalog
// ---------------------------------------------------------------------------

// TestLookupModelCatalog_ClaudeCodeSucceeds verifies that the claude-code
// harness is present in the model catalog and resolves with the expected
// seed content.
func TestLookupModelCatalog_ClaudeCodeSucceeds(t *testing.T) {
	cat, ok := harness.LookupModelCatalog(harness.HarnessIDClaudeCode)
	if !ok {
		t.Fatalf("LookupModelCatalog(%q) returned not-found; want a catalog entry", harness.HarnessIDClaudeCode)
	}
	if cat.HarnessID != harness.HarnessIDClaudeCode {
		t.Errorf("want HarnessID %q, got %q", harness.HarnessIDClaudeCode, cat.HarnessID)
	}
	if len(cat.IDs) == 0 {
		t.Errorf("want at least one model ID for %q, got none", harness.HarnessIDClaudeCode)
	}
	if cat.FormatHint == "" {
		t.Errorf("want a non-empty FormatHint for %q", harness.HarnessIDClaudeCode)
	}
}

// TestLookupModelCatalog_OpenCodeSucceeds verifies that the opencode harness
// is present in the model catalog with non-empty IDs and format hint.
func TestLookupModelCatalog_OpenCodeSucceeds(t *testing.T) {
	cat, ok := harness.LookupModelCatalog(harness.HarnessIDOpenCode)
	if !ok {
		t.Fatalf("LookupModelCatalog(%q) returned not-found; want a catalog entry", harness.HarnessIDOpenCode)
	}
	if cat.HarnessID != harness.HarnessIDOpenCode {
		t.Errorf("want HarnessID %q, got %q", harness.HarnessIDOpenCode, cat.HarnessID)
	}
	if len(cat.IDs) == 0 {
		t.Errorf("want at least one model ID for %q, got none", harness.HarnessIDOpenCode)
	}
	if cat.FormatHint == "" {
		t.Errorf("want a non-empty FormatHint for %q", harness.HarnessIDOpenCode)
	}
}

// TestLookupModelCatalog_GHCPCLISucceeds verifies that the ghcp-cli harness
// is present in the model catalog with non-empty IDs and format hint.
func TestLookupModelCatalog_GHCPCLISucceeds(t *testing.T) {
	cat, ok := harness.LookupModelCatalog(harness.HarnessIDGHCPCLI)
	if !ok {
		t.Fatalf("LookupModelCatalog(%q) returned not-found; want a catalog entry", harness.HarnessIDGHCPCLI)
	}
	if cat.HarnessID != harness.HarnessIDGHCPCLI {
		t.Errorf("want HarnessID %q, got %q", harness.HarnessIDGHCPCLI, cat.HarnessID)
	}
	if len(cat.IDs) == 0 {
		t.Errorf("want at least one model ID for %q, got none", harness.HarnessIDGHCPCLI)
	}
	if cat.FormatHint == "" {
		t.Errorf("want a non-empty FormatHint for %q", harness.HarnessIDGHCPCLI)
	}
}

// TestLookupModelCatalog_UnknownHarnessReturnsFalse verifies that an unknown
// harness ID causes LookupModelCatalog to return (zero, false) rather than
// silently returning an empty catalog entry.
func TestLookupModelCatalog_UnknownHarnessReturnsFalse(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("LookupModelCatalog panicked on unknown harness: %v", r)
		}
	}()

	cat, ok := harness.LookupModelCatalog("does-not-exist")
	if ok {
		t.Fatalf("want ok == false for unknown harness, got catalog %+v", cat)
	}
	if cat.HarnessID != "" || cat.FormatHint != "" || len(cat.IDs) != 0 {
		t.Errorf("want zero-valued ModelCatalog for unknown harness, got %+v", cat)
	}
}

// TestLookupModelCatalog_EmptyHarnessReturnsFalse verifies that an empty
// string causes LookupModelCatalog to return (zero, false).
func TestLookupModelCatalog_EmptyHarnessReturnsFalse(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("LookupModelCatalog panicked on empty harness ID: %v", r)
		}
	}()

	if _, ok := harness.LookupModelCatalog(""); ok {
		t.Errorf("want ok == false for empty harness ID")
	}
}

// TestLookupModelCatalog_ReturnsFreshIDsSlice verifies that mutating the IDs
// slice returned by LookupModelCatalog does not affect a subsequent lookup.
func TestLookupModelCatalog_ReturnsFreshIDsSlice(t *testing.T) {
	cat, ok := harness.LookupModelCatalog(harness.HarnessIDClaudeCode)
	if !ok {
		t.Fatalf("want %q to be found", harness.HarnessIDClaudeCode)
	}
	if len(cat.IDs) == 0 {
		t.Fatalf("want at least one model ID to mutate for this test")
	}

	original := cat.IDs[0]
	cat.IDs[0] = "corrupted"

	cat2, ok := harness.LookupModelCatalog(harness.HarnessIDClaudeCode)
	if !ok {
		t.Fatalf("want %q to be found on second call", harness.HarnessIDClaudeCode)
	}
	if cat2.IDs[0] != original {
		t.Errorf("mutating returned IDs slice affected a later lookup: want %q, got %q", original, cat2.IDs[0])
	}
}

// ---------------------------------------------------------------------------
// Seed content: format hints
// ---------------------------------------------------------------------------

// TestLookupModelCatalog_ClaudeCodeFormatHint verifies the format hint seeded
// from the claude-code harness descriptor.
func TestLookupModelCatalog_ClaudeCodeFormatHint(t *testing.T) {
	cat, ok := harness.LookupModelCatalog(harness.HarnessIDClaudeCode)
	if !ok {
		t.Fatalf("want %q to be found", harness.HarnessIDClaudeCode)
	}
	want := "claude-<model>-<version>"
	if cat.FormatHint != want {
		t.Errorf("want FormatHint %q, got %q", want, cat.FormatHint)
	}
}

// TestLookupModelCatalog_OpenCodeFormatHint verifies the format hint seeded
// from the opencode harness descriptor.
func TestLookupModelCatalog_OpenCodeFormatHint(t *testing.T) {
	cat, ok := harness.LookupModelCatalog(harness.HarnessIDOpenCode)
	if !ok {
		t.Fatalf("want %q to be found", harness.HarnessIDOpenCode)
	}
	want := "provider/model-id"
	if cat.FormatHint != want {
		t.Errorf("want FormatHint %q, got %q", want, cat.FormatHint)
	}
}

// TestLookupModelCatalog_GHCPCLIFormatHint verifies the format hint seeded
// from the ghcp-cli harness descriptor.
func TestLookupModelCatalog_GHCPCLIFormatHint(t *testing.T) {
	cat, ok := harness.LookupModelCatalog(harness.HarnessIDGHCPCLI)
	if !ok {
		t.Fatalf("want %q to be found", harness.HarnessIDGHCPCLI)
	}
	want := "claude-<model>-<version> or gpt-<version>"
	if cat.FormatHint != want {
		t.Errorf("want FormatHint %q, got %q", want, cat.FormatHint)
	}
}

// ---------------------------------------------------------------------------
// Seed content: model IDs membership
// ---------------------------------------------------------------------------

// TestLookupModelCatalog_ClaudeCodeContainsKnownIDs verifies structural
// properties of the claude-code model catalog: it exists, is non-empty, and
// contains no empty ID strings. Specific ID values are user-maintained data
// and are not asserted here.
func TestLookupModelCatalog_ClaudeCodeContainsKnownIDs(t *testing.T) {
	cat, ok := harness.LookupModelCatalog(harness.HarnessIDClaudeCode)
	if !ok {
		t.Fatalf("want %q to be found", harness.HarnessIDClaudeCode)
	}
	if len(cat.IDs) == 0 {
		t.Fatalf("claude-code catalog must contain at least one model ID")
	}
	for _, id := range cat.IDs {
		if id == "" {
			t.Errorf("claude-code catalog contains an empty model ID")
		}
	}
}

// TestLookupModelCatalog_OpenCodeContainsKnownIDs verifies structural
// properties of the opencode model catalog: it exists, is non-empty, and
// contains no empty ID strings. Specific ID values are user-maintained data
// and are not asserted here.
func TestLookupModelCatalog_OpenCodeContainsKnownIDs(t *testing.T) {
	cat, ok := harness.LookupModelCatalog(harness.HarnessIDOpenCode)
	if !ok {
		t.Fatalf("want %q to be found", harness.HarnessIDOpenCode)
	}
	if len(cat.IDs) == 0 {
		t.Fatalf("opencode catalog must contain at least one model ID")
	}
	for _, id := range cat.IDs {
		if id == "" {
			t.Errorf("opencode catalog contains an empty model ID")
		}
	}
}

// TestLookupModelCatalog_GHCPCLIContainsKnownIDs verifies structural
// properties of the ghcp-cli model catalog: it exists, is non-empty, and
// contains no empty ID strings. Specific ID values are user-maintained data
// and are not asserted here.
func TestLookupModelCatalog_GHCPCLIContainsKnownIDs(t *testing.T) {
	cat, ok := harness.LookupModelCatalog(harness.HarnessIDGHCPCLI)
	if !ok {
		t.Fatalf("want %q to be found", harness.HarnessIDGHCPCLI)
	}
	if len(cat.IDs) == 0 {
		t.Fatalf("ghcp-cli catalog must contain at least one model ID")
	}
	for _, id := range cat.IDs {
		if id == "" {
			t.Errorf("ghcp-cli catalog contains an empty model ID")
		}
	}
}

// ---------------------------------------------------------------------------
// ModelIDs
// ---------------------------------------------------------------------------

// TestModelIDs_KnownHarnessReturnsNonEmptySlice verifies that ModelIDs returns
// a non-nil, non-empty slice for a known harness.
func TestModelIDs_KnownHarnessReturnsNonEmptySlice(t *testing.T) {
	ids := harness.ModelIDs(harness.HarnessIDClaudeCode)
	if ids == nil {
		t.Fatalf("ModelIDs(%q) returned nil; want a non-nil slice", harness.HarnessIDClaudeCode)
	}
	if len(ids) == 0 {
		t.Errorf("ModelIDs(%q) returned an empty slice; want at least one model ID", harness.HarnessIDClaudeCode)
	}
}

// TestModelIDs_UnknownHarnessReturnsNil verifies that ModelIDs returns nil for
// an unknown harness, as the design specifies ("nil when harnessID names no
// known CLI-backed harness").
func TestModelIDs_UnknownHarnessReturnsNil(t *testing.T) {
	ids := harness.ModelIDs("unknown-harness")
	if ids != nil {
		t.Errorf("ModelIDs(%q) = %v, want nil for unknown harness", "unknown-harness", ids)
	}
}

// TestModelIDs_ReturnsFreshSlice verifies that mutating the slice returned by
// ModelIDs does not affect a subsequent call.
func TestModelIDs_ReturnsFreshSlice(t *testing.T) {
	ids := harness.ModelIDs(harness.HarnessIDClaudeCode)
	if len(ids) == 0 {
		t.Fatalf("want at least one model ID to mutate")
	}

	original := ids[0]
	ids[0] = "corrupted"

	fresh := harness.ModelIDs(harness.HarnessIDClaudeCode)
	if len(fresh) == 0 {
		t.Fatalf("second call returned empty slice")
	}
	if fresh[0] != original {
		t.Errorf("mutating returned IDs slice affected a later call: want %q, got %q", original, fresh[0])
	}
}

// TestModelIDs_AllThreeCLIHarnesses verifies that ModelIDs returns a non-nil,
// non-empty list for each of the three CLI-backed harnesses.
func TestModelIDs_AllThreeCLIHarnesses(t *testing.T) {
	harnessIDs := []string{
		harness.HarnessIDClaudeCode,
		harness.HarnessIDOpenCode,
		harness.HarnessIDGHCPCLI,
	}
	for _, hid := range harnessIDs {
		ids := harness.ModelIDs(hid)
		if len(ids) == 0 {
			t.Errorf("ModelIDs(%q) returned an empty or nil slice; want at least one model ID", hid)
		}
	}
}

// ---------------------------------------------------------------------------
// IsModelID
// ---------------------------------------------------------------------------

// TestIsModelID_ValidPairReturnsTrue verifies that IsModelID returns true for
// a model identifier that belongs to the specified harness.
func TestIsModelID_ValidPairReturnsTrue(t *testing.T) {
	if !harness.IsModelID(harness.HarnessIDClaudeCode, "claude-sonnet-4-6") {
		t.Errorf("want IsModelID(%q, %q) == true", harness.HarnessIDClaudeCode, "claude-sonnet-4-6")
	}
}

// TestIsModelID_UnknownHarnessReturnsFalse verifies that IsModelID returns
// false when the harness is not in the catalog.
func TestIsModelID_UnknownHarnessReturnsFalse(t *testing.T) {
	if harness.IsModelID("unknown-harness", "claude-sonnet-4-6") {
		t.Errorf("want IsModelID(%q, %q) == false for unknown harness", "unknown-harness", "claude-sonnet-4-6")
	}
}

// TestIsModelID_UnknownModelReturnsFalse verifies that IsModelID returns false
// when the model ID is not in the harness's catalog.
func TestIsModelID_UnknownModelReturnsFalse(t *testing.T) {
	if harness.IsModelID(harness.HarnessIDClaudeCode, "not-a-real-model") {
		t.Errorf("want IsModelID(%q, %q) == false for unknown model", harness.HarnessIDClaudeCode, "not-a-real-model")
	}
}

// TestIsModelID_CrossHarnessReturnsFalse verifies that IsModelID returns false
// when the model is valid for a different harness than the one given. Validation
// is keyed on the resolved harness, never the union of all catalogs.
func TestIsModelID_CrossHarnessReturnsFalse(t *testing.T) {
	// "anthropic/claude-sonnet-4-20250514" is an opencode model, not claude-code.
	if harness.IsModelID(harness.HarnessIDClaudeCode, "anthropic/claude-sonnet-4-20250514") {
		t.Errorf("want IsModelID(%q, %q) == false: this model belongs to %q, not %q",
			harness.HarnessIDClaudeCode, "anthropic/claude-sonnet-4-20250514",
			harness.HarnessIDOpenCode, harness.HarnessIDClaudeCode)
	}
}

// TestIsModelID_EmptyHarnessReturnsFalse verifies that IsModelID returns false
// for an empty harness ID.
func TestIsModelID_EmptyHarnessReturnsFalse(t *testing.T) {
	if harness.IsModelID("", "claude-sonnet-4-6") {
		t.Errorf("want IsModelID(%q, %q) == false for empty harness ID", "", "claude-sonnet-4-6")
	}
}

// TestIsModelID_EmptyModelReturnsFalse verifies that IsModelID returns false
// for an empty model ID.
func TestIsModelID_EmptyModelReturnsFalse(t *testing.T) {
	if harness.IsModelID(harness.HarnessIDClaudeCode, "") {
		t.Errorf("want IsModelID(%q, %q) == false for empty model ID", harness.HarnessIDClaudeCode, "")
	}
}

// TestIsModelID_OpenCodeModel verifies that IsModelID returns true for an ID
// that exists in the opencode catalog. The ID is obtained dynamically so the
// test does not depend on any specific model ID string.
func TestIsModelID_OpenCodeModel(t *testing.T) {
	cat, ok := harness.LookupModelCatalog(harness.HarnessIDOpenCode)
	if !ok {
		t.Fatalf("want %q to be found", harness.HarnessIDOpenCode)
	}
	if len(cat.IDs) == 0 {
		t.Fatalf("opencode catalog must contain at least one model ID to test IsModelID")
	}
	id := cat.IDs[0]
	if !harness.IsModelID(harness.HarnessIDOpenCode, id) {
		t.Errorf("want IsModelID(%q, %q) == true for ID drawn from the catalog", harness.HarnessIDOpenCode, id)
	}
}

// TestIsModelID_GHCPCLIModel verifies that IsModelID returns true for an ID
// that exists in the ghcp-cli catalog. The ID is obtained dynamically so the
// test does not depend on any specific model ID string.
func TestIsModelID_GHCPCLIModel(t *testing.T) {
	cat, ok := harness.LookupModelCatalog(harness.HarnessIDGHCPCLI)
	if !ok {
		t.Fatalf("want %q to be found", harness.HarnessIDGHCPCLI)
	}
	if len(cat.IDs) == 0 {
		t.Fatalf("ghcp-cli catalog must contain at least one model ID to test IsModelID")
	}
	id := cat.IDs[0]
	if !harness.IsModelID(harness.HarnessIDGHCPCLI, id) {
		t.Errorf("want IsModelID(%q, %q) == true for ID drawn from the catalog", harness.HarnessIDGHCPCLI, id)
	}
}

// ---------------------------------------------------------------------------
// Coverage invariant: every identity catalog entry has model data (T8.2)
// ---------------------------------------------------------------------------

// TestModelCatalog_EveryIdentityCatalogEntryHasModelData verifies that every
// harness in the identity catalog (CLIHarnesses) has a corresponding model
// catalog entry, so adding a harness to the identity catalog without adding
// model data fails loudly here rather than silently producing an empty model
// list at runtime.
func TestModelCatalog_EveryIdentityCatalogEntryHasModelData(t *testing.T) {
	for _, identity := range harness.CLIHarnesses() {
		cat, ok := harness.LookupModelCatalog(identity.ID)
		if !ok {
			t.Errorf("identity catalog entry %q has no model catalog entry; add its model IDs and format hint to the model catalog", identity.ID)
			continue
		}
		if len(cat.IDs) == 0 {
			t.Errorf("model catalog entry for %q has an empty IDs list; every CLI-backed harness must declare at least one model", identity.ID)
		}
		if cat.FormatHint == "" {
			t.Errorf("model catalog entry for %q has an empty FormatHint; every CLI-backed harness must declare one", identity.ID)
		}
	}
}
