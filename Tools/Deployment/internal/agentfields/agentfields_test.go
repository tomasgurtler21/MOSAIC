package agentfields_test

// agentfields_test.go verifies the field-name registry contract for MOSAIC-only deployed
// fields. All tests in this file run against the exported API of the agentfields package
// and document the invariants that every consumer derives from.
//
// Registry invariants verified here:
//
//   - Every entry in All() has a Deployed name that equals "mosaic_" + Legacy, confirming
//     the naming convention holds across the full FR-6 field set.
//   - The Deployed and Legacy names of every entry are distinct (the rename is non-trivial).
//   - All() returns the same entries in the same order on repeated calls (stable iteration).
//   - No harness-read field (model, tools) appears in the registry.
//   - No shared identity field (name, description, version) appears in the registry.
//   - ByGeneric finds fields that have a Generic name; fields without one return false.
//   - ByDeployedName finds fields by either the prefixed Deployed name or the Legacy name.
//   - ReadOrder always returns [Deployed, Legacy] — the reader tries prefixed first.
//   - IsMosaicOnlyDeployedKey returns true for Deployed names, true for Legacy names, and
//     false for every field that should not be in the registry.
//   - PromoteDropKeys covers both the Deployed and Legacy names for every non-id registry
//     entry, and also includes "model". It excludes mosaic_id and the generic "id" (the id
//     entry is excluded because promote restores id as the generic key).

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-deploy/internal/agentfields"
)

// ---------------------------------------------------------------------------
// All() — complete registry iteration
// ---------------------------------------------------------------------------

// TestAll_EveryEntryDeployedNameStartsWithMosaicPrefix verifies that every entry returned
// by All() has a Deployed name starting with "mosaic_". This pins the naming convention
// that distinguishes MOSAIC bookkeeping fields from harness-read fields in deployed files.
func TestAll_EveryEntryDeployedNameStartsWithMosaicPrefix(t *testing.T) {
	for _, f := range agentfields.All() {
		if !strings.HasPrefix(f.Deployed, "mosaic_") {
			t.Errorf("entry %q: Deployed name %q does not start with 'mosaic_'", f.Legacy, f.Deployed)
		}
	}
}

// TestAll_DeployedNameEqualsLegacyWithMosaicPrefix verifies the exact naming rule:
// every entry's Deployed name equals "mosaic_" + its Legacy name. This confirms the
// single registry is the authoritative source of the pairing without scattered literals.
func TestAll_DeployedNameEqualsLegacyWithMosaicPrefix(t *testing.T) {
	for _, f := range agentfields.All() {
		want := "mosaic_" + f.Legacy
		if f.Deployed != want {
			t.Errorf("entry with Legacy=%q: Deployed=%q, want %q (mosaic_+Legacy)",
				f.Legacy, f.Deployed, want)
		}
	}
}

// TestAll_DeployedAndLegacyNamesAreDistinct verifies that the Deployed name differs from
// the Legacy name for every entry — confirming that the rename is non-trivial (not a no-op).
func TestAll_DeployedAndLegacyNamesAreDistinct(t *testing.T) {
	for _, f := range agentfields.All() {
		if f.Deployed == f.Legacy {
			t.Errorf("entry %q: Deployed and Legacy are identical; the rename must change the key name",
				f.Legacy)
		}
	}
}

// TestAll_ContainsSixPlusOneEntries verifies that All() returns exactly ten entries: the
// six version stamp fields, plus the role field (Generic="role", Deployed="mosaic_role"),
// plus mosaic_protocol_version which exists in the registry for drop-set symmetry only
// (it is never written as frontmatter), plus the skill version field
// (Generic="version", Deployed="mosaic_version") added for the skill version prefix migration,
// plus the new mosaic_harness_version entry added alongside the retained mosaic_transform_version
// entry for backwards-compatible migration.
//
// This test FAILS until the new harness_version FieldName entry is added to the registry.
func TestAll_ContainsSixPlusOneEntries(t *testing.T) {
	entries := agentfields.All()
	if len(entries) != 10 {
		t.Errorf("All() returned %d entries, want 10 (old transform_version entry retained + new harness_version entry added + 8 existing entries)",
			len(entries))
	}
}

// TestAll_ContainsIDField verifies that the id field is present in the registry with the
// expected Generic name (the only entry that has one), Deployed name, and Legacy name.
func TestAll_ContainsIDField(t *testing.T) {
	var found bool
	for _, f := range agentfields.All() {
		if f.Generic == "id" {
			found = true
			if f.Deployed != "mosaic_id" {
				t.Errorf("id entry: Deployed=%q, want %q", f.Deployed, "mosaic_id")
			}
			if f.Legacy != "id" {
				t.Errorf("id entry: Legacy=%q, want %q", f.Legacy, "id")
			}
		}
	}
	if !found {
		t.Error("All() does not contain an entry with Generic==\"id\"; the id field must be in the registry")
	}
}

// TestAll_ContainsTransformVersionField verifies the transform_version entry is present with
// its correct Deployed and Legacy names and an empty Generic (deploy-only field).
// This OLD entry must be retained unchanged alongside the new harness_version entry so that
// migration stripping and backward-compatible reads can look it up via the registry.
func TestAll_ContainsTransformVersionField(t *testing.T) {
	var found bool
	for _, f := range agentfields.All() {
		if f.Legacy == "transform_version" {
			found = true
			if f.Generic != "" {
				t.Errorf("transform_version entry: Generic=%q, want empty (deploy-only field)", f.Generic)
			}
			if f.Deployed != "mosaic_transform_version" {
				t.Errorf("transform_version entry: Deployed=%q, want %q", f.Deployed, "mosaic_transform_version")
			}
		}
	}
	if !found {
		t.Error("All() does not contain an entry with Legacy==\"transform_version\"")
	}
}

// TestAll_ContainsHarnessVersionField verifies that the NEW harness_version entry is present
// in the registry with the correct Deployed and Legacy names and an empty Generic (deploy-only
// field). This entry is the active write-path entry after Stage 1; the old transform_version
// entry is retained alongside it for migration and backward-compatible reads.
//
// This test FAILS until the new harness_version entry is added to the registry.
func TestAll_ContainsHarnessVersionField(t *testing.T) {
	var found bool
	for _, f := range agentfields.All() {
		if f.Legacy == "harness_version" {
			found = true
			if f.Generic != "" {
				t.Errorf("harness_version entry: Generic=%q, want empty (deploy-only field)", f.Generic)
			}
			if f.Deployed != "mosaic_harness_version" {
				t.Errorf("harness_version entry: Deployed=%q, want %q", f.Deployed, "mosaic_harness_version")
			}
		}
	}
	if !found {
		t.Error("All() does not contain an entry with Legacy==\"harness_version\"; the new entry must be added to the registry")
	}
}

// TestAll_BothHarnessVersionAndTransformVersionEntryPresent verifies that BOTH the old
// transform_version entry and the new harness_version entry exist simultaneously in the
// registry. This dual-entry shape is required by the migration design: the old entry is
// needed for migration stripping and backward-compatible reads, while the new entry is the
// active write-path entry.
//
// This test FAILS until the new harness_version entry is added (until then only one entry
// for the harness version concept exists).
func TestAll_BothHarnessVersionAndTransformVersionEntryPresent(t *testing.T) {
	var foundOld, foundNew bool
	for _, f := range agentfields.All() {
		if f.Legacy == "transform_version" {
			foundOld = true
		}
		if f.Legacy == "harness_version" {
			foundNew = true
		}
	}
	if !foundOld {
		t.Error("All() does not contain the OLD entry with Legacy==\"transform_version\"; it must be retained for migration")
	}
	if !foundNew {
		t.Error("All() does not contain the NEW entry with Legacy==\"harness_version\"; it must be added for the write path")
	}
}

// ---------------------------------------------------------------------------
// ByDeployedName — harness_version dual-entry lookups
// ---------------------------------------------------------------------------

// TestByDeployedName_HarnessVersionLegacyName_ReturnsNewEntry verifies that
// ByDeployedName("harness_version") returns the new entry with Deployed="mosaic_harness_version".
// This is the primary lookup the write path uses after Stage 1.
//
// This test FAILS until the new harness_version entry is added to the registry.
func TestByDeployedName_HarnessVersionLegacyName_ReturnsNewEntry(t *testing.T) {
	f, ok := agentfields.ByDeployedName("harness_version")
	if !ok {
		t.Fatal("ByDeployedName(\"harness_version\") returned ok=false; the new harness_version entry must be findable by its Legacy name")
	}
	if f.Deployed != "mosaic_harness_version" {
		t.Errorf("ByDeployedName(\"harness_version\").Deployed = %q, want %q", f.Deployed, "mosaic_harness_version")
	}
	if f.Legacy != "harness_version" {
		t.Errorf("ByDeployedName(\"harness_version\").Legacy = %q, want %q", f.Legacy, "harness_version")
	}
	if f.Generic != "" {
		t.Errorf("ByDeployedName(\"harness_version\").Generic = %q, want empty (deploy-only field)", f.Generic)
	}
}

// TestByDeployedName_HarnessVersionPrefixedName_ReturnsNewEntry verifies that
// ByDeployedName("mosaic_harness_version") also returns the new entry. Both the prefixed and
// legacy names must index to the same entry.
//
// This test FAILS until the new harness_version entry is added to the registry.
func TestByDeployedName_HarnessVersionPrefixedName_ReturnsNewEntry(t *testing.T) {
	f, ok := agentfields.ByDeployedName("mosaic_harness_version")
	if !ok {
		t.Fatal("ByDeployedName(\"mosaic_harness_version\") returned ok=false; the new entry must be findable by its Deployed name")
	}
	if f.Legacy != "harness_version" {
		t.Errorf("ByDeployedName(\"mosaic_harness_version\").Legacy = %q, want %q", f.Legacy, "harness_version")
	}
}

// TestByDeployedName_TransformVersionEntryDistinctFromHarnessVersionEntry verifies that the
// transform_version and harness_version entries are distinct registry objects: looking up
// "transform_version" returns the OLD entry (Deployed="mosaic_transform_version"), and looking
// up "harness_version" returns the NEW entry (Deployed="mosaic_harness_version"). They must
// not collide or alias each other.
//
// This test FAILS until the new harness_version entry is added (currently only the old entry
// exists, so ByDeployedName("harness_version") returns ok=false).
func TestByDeployedName_TransformVersionEntryDistinctFromHarnessVersionEntry(t *testing.T) {
	oldEntry, oldOK := agentfields.ByDeployedName("transform_version")
	newEntry, newOK := agentfields.ByDeployedName("harness_version")

	if !oldOK {
		t.Error("ByDeployedName(\"transform_version\") returned ok=false; the old entry must be retained")
	}
	if !newOK {
		t.Error("ByDeployedName(\"harness_version\") returned ok=false; the new entry must be added")
	}
	if !oldOK || !newOK {
		return // cannot assert inequality if either lookup failed
	}

	if oldEntry.Deployed == newEntry.Deployed {
		t.Errorf("old and new entries have the same Deployed name %q; they must be distinct registry entries",
			oldEntry.Deployed)
	}
	if oldEntry.Legacy == newEntry.Legacy {
		t.Errorf("old and new entries have the same Legacy name %q; they must be distinct registry entries",
			oldEntry.Legacy)
	}
	if oldEntry.Deployed != "mosaic_transform_version" {
		t.Errorf("old entry Deployed = %q, want %q", oldEntry.Deployed, "mosaic_transform_version")
	}
	if newEntry.Deployed != "mosaic_harness_version" {
		t.Errorf("new entry Deployed = %q, want %q", newEntry.Deployed, "mosaic_harness_version")
	}
}

// TestAll_ContainsInjectionsVersionField verifies the injections_version entry.
func TestAll_ContainsInjectionsVersionField(t *testing.T) {
	var found bool
	for _, f := range agentfields.All() {
		if f.Legacy == "injections_version" {
			found = true
			if f.Deployed != "mosaic_injections_version" {
				t.Errorf("injections_version entry: Deployed=%q, want %q", f.Deployed, "mosaic_injections_version")
			}
		}
	}
	if !found {
		t.Error("All() does not contain an entry with Legacy==\"injections_version\"")
	}
}

// TestAll_ContainsToolMappingsVersionField verifies the tool_mappings_version entry.
func TestAll_ContainsToolMappingsVersionField(t *testing.T) {
	var found bool
	for _, f := range agentfields.All() {
		if f.Legacy == "tool_mappings_version" {
			found = true
			if f.Deployed != "mosaic_tool_mappings_version" {
				t.Errorf("tool_mappings_version entry: Deployed=%q, want %q", f.Deployed, "mosaic_tool_mappings_version")
			}
		}
	}
	if !found {
		t.Error("All() does not contain an entry with Legacy==\"tool_mappings_version\"")
	}
}

// TestAll_ContainsBundleVersionField verifies the bundle_version entry.
func TestAll_ContainsBundleVersionField(t *testing.T) {
	var found bool
	for _, f := range agentfields.All() {
		if f.Legacy == "bundle_version" {
			found = true
			if f.Deployed != "mosaic_bundle_version" {
				t.Errorf("bundle_version entry: Deployed=%q, want %q", f.Deployed, "mosaic_bundle_version")
			}
		}
	}
	if !found {
		t.Error("All() does not contain an entry with Legacy==\"bundle_version\"")
	}
}

// TestAll_ContainsOrchestratorInjectionsVersionField verifies the orchestrator_injections_version
// entry.
func TestAll_ContainsOrchestratorInjectionsVersionField(t *testing.T) {
	var found bool
	for _, f := range agentfields.All() {
		if f.Legacy == "orchestrator_injections_version" {
			found = true
			if f.Deployed != "mosaic_orchestrator_injections_version" {
				t.Errorf("orchestrator_injections_version entry: Deployed=%q, want %q",
					f.Deployed, "mosaic_orchestrator_injections_version")
			}
		}
	}
	if !found {
		t.Error("All() does not contain an entry with Legacy==\"orchestrator_injections_version\"")
	}
}

// TestAll_ContainsProtocolVersionFieldForDropSetSymmetry verifies the protocol_version entry
// is present in the registry for drop-set symmetry, even though it is never written as agent
// frontmatter (the deployed protocol version lives as an HTML comment, not a frontmatter field).
func TestAll_ContainsProtocolVersionFieldForDropSetSymmetry(t *testing.T) {
	var found bool
	for _, f := range agentfields.All() {
		if f.Legacy == "protocol_version" {
			found = true
			if f.Deployed != "mosaic_protocol_version" {
				t.Errorf("protocol_version entry: Deployed=%q, want %q", f.Deployed, "mosaic_protocol_version")
			}
		}
	}
	if !found {
		t.Error("All() does not contain an entry with Legacy==\"protocol_version\"")
	}
}

// TestAll_NoHarnessReadFieldInRegistry verifies that harness-consumed fields that must never
// be prefixed are absent from the registry. Specifically: model and tools.
func TestAll_NoHarnessReadFieldInRegistry(t *testing.T) {
	forbidden := []string{"model", "tools"}
	for _, f := range agentfields.All() {
		for _, bad := range forbidden {
			if f.Deployed == bad || f.Legacy == bad || f.Generic == bad {
				t.Errorf("harness-read field %q must not appear in the registry; found in entry {Generic:%q Deployed:%q Legacy:%q}",
					bad, f.Generic, f.Deployed, f.Legacy)
			}
		}
	}
}

// TestAll_NoSharedIdentityFieldInRegistry verifies that the purely descriptive shared
// identity fields — name and description — are absent from the registry. These fields
// are never prefixed or renamed during deployment.
//
// Note: "version" was previously in this list, but the skill version prefix migration
// added a registry entry mapping Generic="version" to Deployed="mosaic_version" for
// skill artifacts. That entry has a non-empty Generic, a non-"version" Deployed name,
// and a Legacy of "version". The "version" key is therefore legitimately present in
// the registry as a skill-specific mapping. Only name and description remain
// unconditionally absent from the registry.
func TestAll_NoSharedIdentityFieldInRegistry(t *testing.T) {
	forbidden := []string{"name", "description"}
	for _, f := range agentfields.All() {
		for _, bad := range forbidden {
			if f.Deployed == bad || f.Legacy == bad || f.Generic == bad {
				t.Errorf("shared identity field %q must not appear in the registry; found in entry {Generic:%q Deployed:%q Legacy:%q}",
					bad, f.Generic, f.Deployed, f.Legacy)
			}
		}
	}
}

// TestAll_StableOrderBetweenCalls verifies that two successive calls to All() return entries
// in the same order. Stability is required so that consumers that iterate All() in order to
// construct derived lists (e.g. drop sets, key orders) get consistent results.
func TestAll_StableOrderBetweenCalls(t *testing.T) {
	first := agentfields.All()
	second := agentfields.All()
	if len(first) != len(second) {
		t.Fatalf("All() returned %d entries on first call, %d on second; order must be stable", len(first), len(second))
	}
	for i := range first {
		if first[i].Legacy != second[i].Legacy {
			t.Errorf("position %d: Legacy=%q (first call) vs %q (second call); All() must return entries in a stable order",
				i, first[i].Legacy, second[i].Legacy)
		}
	}
}

// TestAll_ReturnsCopyNotSlice verifies that mutating the slice returned by All() does not
// affect subsequent calls. This defends against callers that append to or reorder the
// returned slice believing it is safe to do so.
func TestAll_ReturnsCopyNotSlice(t *testing.T) {
	first := agentfields.All()
	first[0].Deployed = "tampered"
	second := agentfields.All()
	if second[0].Deployed == "tampered" {
		t.Error("mutating the slice returned by All() affected the registry; All() must return a defensive copy")
	}
}

// ---------------------------------------------------------------------------
// ByGeneric
// ---------------------------------------------------------------------------

// TestByGeneric_IDField_ReturnsEntry verifies that looking up the generic "id" key returns
// the corresponding registry entry.
func TestByGeneric_IDField_ReturnsEntry(t *testing.T) {
	f, ok := agentfields.ByGeneric("id")
	if !ok {
		t.Fatal("ByGeneric(\"id\") returned ok=false; the id entry must be findable by its generic key")
	}
	if f.Deployed != "mosaic_id" {
		t.Errorf("ByGeneric(\"id\").Deployed = %q, want %q", f.Deployed, "mosaic_id")
	}
	if f.Legacy != "id" {
		t.Errorf("ByGeneric(\"id\").Legacy = %q, want %q", f.Legacy, "id")
	}
}

// TestByGeneric_DeployOnlyField_NotFound verifies that a field whose Generic is empty (a
// deploy-only field) cannot be found by ByGeneric, even when the caller passes a string that
// matches its Legacy name.
func TestByGeneric_DeployOnlyField_NotFound(t *testing.T) {
	// transform_version has no Generic name — it is a deploy-only stamp, not a generic source field.
	_, ok := agentfields.ByGeneric("transform_version")
	if ok {
		t.Error("ByGeneric(\"transform_version\") returned ok=true; deploy-only fields have no generic key and must not be found by ByGeneric")
	}
}

// TestByGeneric_UnknownKey_NotFound verifies that an arbitrary key not in the registry
// returns ok=false.
func TestByGeneric_UnknownKey_NotFound(t *testing.T) {
	_, ok := agentfields.ByGeneric("completely_unknown_key")
	if ok {
		t.Error("ByGeneric(\"completely_unknown_key\") returned ok=true; unknown keys must return ok=false")
	}
}

// ---------------------------------------------------------------------------
// ByDeployedName
// ---------------------------------------------------------------------------

// TestByDeployedName_PrefixedName_Found verifies that looking up a field by its Deployed
// (mosaic_-prefixed) name returns the correct entry.
func TestByDeployedName_PrefixedName_Found(t *testing.T) {
	f, ok := agentfields.ByDeployedName("mosaic_transform_version")
	if !ok {
		t.Fatal("ByDeployedName(\"mosaic_transform_version\") returned ok=false; prefixed name must be findable")
	}
	if f.Legacy != "transform_version" {
		t.Errorf("ByDeployedName(\"mosaic_transform_version\").Legacy = %q, want %q", f.Legacy, "transform_version")
	}
}

// TestByDeployedName_LegacyName_Found verifies that looking up a field by its Legacy
// (unprefixed) name also returns the correct entry. Backward compatibility requires that
// both the prefixed and legacy names index into the same entry.
func TestByDeployedName_LegacyName_Found(t *testing.T) {
	f, ok := agentfields.ByDeployedName("transform_version")
	if !ok {
		t.Fatal("ByDeployedName(\"transform_version\") returned ok=false; legacy name must also be findable")
	}
	if f.Deployed != "mosaic_transform_version" {
		t.Errorf("ByDeployedName(\"transform_version\").Deployed = %q, want %q", f.Deployed, "mosaic_transform_version")
	}
}

// TestByDeployedName_UnknownKey_NotFound verifies that an arbitrary key not in the registry
// returns ok=false.
func TestByDeployedName_UnknownKey_NotFound(t *testing.T) {
	_, ok := agentfields.ByDeployedName("some_random_key")
	if ok {
		t.Error("ByDeployedName(\"some_random_key\") returned ok=true; unknown keys must return ok=false")
	}
}

// TestByDeployedName_BothPrefixedAndLegacyReferToSameEntry verifies that the prefixed and
// legacy names for the same field return entries that are logically identical.
func TestByDeployedName_BothPrefixedAndLegacyReferToSameEntry(t *testing.T) {
	byPrefixed, ok1 := agentfields.ByDeployedName("mosaic_injections_version")
	byLegacy, ok2 := agentfields.ByDeployedName("injections_version")
	if !ok1 || !ok2 {
		t.Fatalf("lookup failed: prefixed ok=%v, legacy ok=%v", ok1, ok2)
	}
	if byPrefixed.Deployed != byLegacy.Deployed || byPrefixed.Legacy != byLegacy.Legacy {
		t.Errorf("prefixed and legacy lookups returned different entries: %+v vs %+v", byPrefixed, byLegacy)
	}
}

// ---------------------------------------------------------------------------
// ReadOrder
// ---------------------------------------------------------------------------

// TestReadOrder_DeployedBeforeLegacy verifies that ReadOrder always returns the Deployed
// (prefixed) name first, followed by the Legacy name. Read sites must try the prefixed name
// first to give it priority when both forms are present in a file.
func TestReadOrder_DeployedBeforeLegacy(t *testing.T) {
	for _, f := range agentfields.All() {
		order := agentfields.ReadOrder(f)
		if len(order) != 2 {
			t.Errorf("ReadOrder(%q): len=%d, want 2", f.Legacy, len(order))
			continue
		}
		if order[0] != f.Deployed {
			t.Errorf("ReadOrder(%q)[0]=%q, want Deployed name %q", f.Legacy, order[0], f.Deployed)
		}
		if order[1] != f.Legacy {
			t.Errorf("ReadOrder(%q)[1]=%q, want Legacy name %q", f.Legacy, order[1], f.Legacy)
		}
	}
}

// ---------------------------------------------------------------------------
// IsMosaicOnlyDeployedKey
// ---------------------------------------------------------------------------

// TestIsMosaicOnlyDeployedKey_PrefixedName_True verifies that every Deployed name in the
// registry is recognized as a MOSAIC-only deployed key.
func TestIsMosaicOnlyDeployedKey_PrefixedName_True(t *testing.T) {
	for _, f := range agentfields.All() {
		if !agentfields.IsMosaicOnlyDeployedKey(f.Deployed) {
			t.Errorf("IsMosaicOnlyDeployedKey(%q) = false, want true; Deployed names must be recognized", f.Deployed)
		}
	}
}

// TestIsMosaicOnlyDeployedKey_LegacyName_True verifies that every Legacy name in the
// registry is also recognized as a MOSAIC-only deployed key — backward compatibility.
func TestIsMosaicOnlyDeployedKey_LegacyName_True(t *testing.T) {
	for _, f := range agentfields.All() {
		if !agentfields.IsMosaicOnlyDeployedKey(f.Legacy) {
			t.Errorf("IsMosaicOnlyDeployedKey(%q) = false, want true; Legacy names must also be recognized", f.Legacy)
		}
	}
}

// TestIsMosaicOnlyDeployedKey_HarnessReadField_False verifies that model and tools — which
// the harness reads and must never be prefixed — are not recognized as MOSAIC-only deployed
// keys.
func TestIsMosaicOnlyDeployedKey_HarnessReadField_False(t *testing.T) {
	for _, key := range []string{"model", "tools"} {
		if agentfields.IsMosaicOnlyDeployedKey(key) {
			t.Errorf("IsMosaicOnlyDeployedKey(%q) = true, want false; harness-read fields must not be in the registry", key)
		}
	}
}

// TestIsMosaicOnlyDeployedKey_SharedIdentityField_False verifies that the purely
// descriptive shared identity fields (name, description) are not recognized as
// MOSAIC-only deployed keys.
//
// Note: "version" was previously included in this list, but the skill version prefix
// migration added a registry entry mapping Generic="version" to Deployed="mosaic_version"
// for skill artifacts. Because that entry has Legacy="version", IsMosaicOnlyDeployedKey("version")
// now intentionally returns true. The specific assertions for "version" are covered by
// TestSkillVersionEntry_IsMosaicOnlyDeployedKey_Version_True in
// skill_version_registry_test.go.
func TestIsMosaicOnlyDeployedKey_SharedIdentityField_False(t *testing.T) {
	for _, key := range []string{"name", "description"} {
		if agentfields.IsMosaicOnlyDeployedKey(key) {
			t.Errorf("IsMosaicOnlyDeployedKey(%q) = true, want false; shared identity fields must not be in the registry", key)
		}
	}
}

// ---------------------------------------------------------------------------
// IsGenericVocabularyKey
// ---------------------------------------------------------------------------

// TestIsGenericVocabularyKey_GenericFields_True verifies that the documented generic agent
// frontmatter vocabulary keys are all recognized.
func TestIsGenericVocabularyKey_GenericFields_True(t *testing.T) {
	genericKeys := []string{"id", "version", "name", "description", "role", "model", "tools",
		"recommended_tier", "tier_rationale", "required_skills"}
	for _, key := range genericKeys {
		if !agentfields.IsGenericVocabularyKey(key) {
			t.Errorf("IsGenericVocabularyKey(%q) = false, want true; %q is a documented generic vocabulary key", key, key)
		}
	}
}

// TestIsGenericVocabularyKey_DeployOnlyField_False verifies that deploy-only stamps are not
// recognized as generic vocabulary keys.
func TestIsGenericVocabularyKey_DeployOnlyField_False(t *testing.T) {
	for _, f := range agentfields.All() {
		if f.Generic != "" {
			// Generic name fields (e.g. "id") are vocabulary keys — skip.
			continue
		}
		if agentfields.IsGenericVocabularyKey(f.Legacy) {
			t.Errorf("IsGenericVocabularyKey(%q) = true, want false; deploy-only stamps are not generic vocabulary", f.Legacy)
		}
		if agentfields.IsGenericVocabularyKey(f.Deployed) {
			t.Errorf("IsGenericVocabularyKey(%q) = true, want false; prefixed deploy stamps are not generic vocabulary", f.Deployed)
		}
	}
}

// ---------------------------------------------------------------------------
// IsKnownMosaicKey
// ---------------------------------------------------------------------------

// TestIsKnownMosaicKey_GenericVocabularyField_True verifies that generic vocabulary keys
// are recognized as known to MOSAIC.
func TestIsKnownMosaicKey_GenericVocabularyField_True(t *testing.T) {
	if !agentfields.IsKnownMosaicKey("version") {
		t.Error("IsKnownMosaicKey(\"version\") = false, want true; version is a generic vocabulary key")
	}
}

// TestIsKnownMosaicKey_MosaicDeployedField_True verifies that MOSAIC-only deployed fields
// (both prefixed and legacy names) are recognized as known to MOSAIC.
func TestIsKnownMosaicKey_MosaicDeployedField_True(t *testing.T) {
	if !agentfields.IsKnownMosaicKey("mosaic_transform_version") {
		t.Error("IsKnownMosaicKey(\"mosaic_transform_version\") = false, want true")
	}
	if !agentfields.IsKnownMosaicKey("transform_version") {
		t.Error("IsKnownMosaicKey(\"transform_version\") = false, want true; legacy names must be known")
	}
}

// TestIsKnownMosaicKey_UnknownField_False verifies that a key MOSAIC has no knowledge of
// is not recognized.
func TestIsKnownMosaicKey_UnknownField_False(t *testing.T) {
	if agentfields.IsKnownMosaicKey("some_custom_harness_key") {
		t.Error("IsKnownMosaicKey(\"some_custom_harness_key\") = true, want false; unknown keys must not be recognized")
	}
}

// ---------------------------------------------------------------------------
// PromoteDropKeys
// ---------------------------------------------------------------------------

// TestPromoteDropKeys_ContainsBothNamesForNonIDEntries verifies that for every registry
// entry that is a deploy-only stamp (Generic == ""), PromoteDropKeys includes both the
// Deployed (prefixed) and Legacy name. This ensures promote strips both name forms out of
// the generic output for version stamp fields.
//
// Entries with a non-empty Generic (today: id and role) are excluded from the drop set
// because promote does not drop them — it reconstructs them under their generic key. The
// skip condition generalises from "Generic == id" to "Generic != "": both the id and role
// entries are excluded from PromoteDropKeys. This test is updated to match I3.5's behaviour.
func TestPromoteDropKeys_ContainsBothNamesForNonIDEntries(t *testing.T) {
	keys := make(map[string]bool)
	for _, k := range agentfields.PromoteDropKeys() {
		keys[k] = true
	}

	for _, f := range agentfields.All() {
		if f.Generic != "" {
			// Entries with a generic counterpart (id, role) are excluded from PromoteDropKeys;
			// promote reconstructs them under their generic key.
			continue
		}
		if !keys[f.Deployed] {
			t.Errorf("PromoteDropKeys does not contain Deployed name %q for legacy=%q entry",
				f.Deployed, f.Legacy)
		}
		if !keys[f.Legacy] {
			t.Errorf("PromoteDropKeys does not contain Legacy name %q", f.Legacy)
		}
	}
}

// TestPromoteDropKeys_DoesNotContainMosaicIDOrGenericID verifies that neither "mosaic_id"
// nor the generic "id" appears in PromoteDropKeys. Promote restores the id field from the
// deployed source as the generic "id" key, so the drop set must not include it.
func TestPromoteDropKeys_DoesNotContainMosaicIDOrGenericID(t *testing.T) {
	for _, k := range agentfields.PromoteDropKeys() {
		if k == "mosaic_id" {
			t.Error("PromoteDropKeys must not contain \"mosaic_id\"; promote restores id as the generic key")
		}
		if k == "id" {
			// "id" appears as the Legacy of the id entry and also as a generic vocab key.
			// Promote must not drop it — it restores it.
			t.Error("PromoteDropKeys must not contain \"id\"; promote restores the generic id")
		}
	}
}

// TestPromoteDropKeys_ContainsModel verifies that "model" is always in PromoteDropKeys.
// Model is a harness-added field that promote must remove even though it is not in the
// MOSAIC-only deployed field registry proper.
func TestPromoteDropKeys_ContainsModel(t *testing.T) {
	var found bool
	for _, k := range agentfields.PromoteDropKeys() {
		if k == "model" {
			found = true
			break
		}
	}
	if !found {
		t.Error("PromoteDropKeys does not contain \"model\"; model must be dropped by promote")
	}
}

// ---------------------------------------------------------------------------
// AC4.7 — no scattered literal "mosaic_" strings outside the registry
// ---------------------------------------------------------------------------

// TestNoMosaicLiteralOutsideAgentfields verifies that no production Go source file outside
// internal/agentfields contains the literal string "mosaic_" as a Go string value. All code
// that constructs or references a mosaic_-prefixed field name must derive it from the
// agentfields registry (agentfields.All(), ByGeneric, ByDeployedName, etc.) rather than
// hard-coding it. This is the static-analysis guard for AC4.7: the legacy ↔ prefixed pairing
// is defined in exactly one place and every site derives from it.
//
// Test files across the module are excluded from the scan: test assertions routinely use
// "mosaic_..."-prefixed key names as expected values, which is the correct use of registry
// output in tests and does not violate the centralization invariant.
//
// Scanned scope: all non-test .go files in the module, excluding internal/agentfields/ itself
// (the registry package that owns the literal strings) and vendor/ or testdata/ directories.
func TestNoMosaicLiteralOutsideAgentfields(t *testing.T) {
	// Resolve the module root (Tools/Deployment/) from this package's directory
	// (Tools/Deployment/internal/agentfields/).
	rel := filepath.Join("..", "..")
	moduleRoot, err := filepath.Abs(rel)
	if err != nil {
		t.Fatalf("resolve module root: %v", err)
	}

	agentfieldsDir := filepath.Join(moduleRoot, "internal", "agentfields")

	var violations []string
	walkErr := filepath.WalkDir(moduleRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			// Skip directories that are not Go source.
			if name == "vendor" || name == ".git" || name == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		// Only scan Go source files.
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		// Skip the agentfields package (production code and its tests). This package owns
		// the registry and is the single authorised source of literal "mosaic_" strings.
		if strings.HasPrefix(path, agentfieldsDir+string(filepath.Separator)) {
			return nil
		}
		// Skip test files throughout the module. Test assertions routinely use
		// "mosaic_..."-prefixed key names as expected values, which is correct usage of
		// registry output and does not violate the registry-centralization invariant.
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(content, []byte(`"mosaic_`)) {
			rel, relErr := filepath.Rel(moduleRoot, path)
			if relErr != nil {
				rel = path
			}
			violations = append(violations, rel)
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk module source: %v", walkErr)
	}

	for _, v := range violations {
		t.Errorf("production file %s contains a literal \"mosaic_\" string; "+
			"field names must be derived from agentfields.All() or its accessors, "+
			"not hard-coded (AC4.7)", v)
	}
}
