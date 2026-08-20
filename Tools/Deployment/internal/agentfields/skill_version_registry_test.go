package agentfields_test

// skill_version_registry_test.go verifies the registry entry that maps the skill source
// field "version" to the deployed field "mosaic_version", as part of the skill version
// prefix migration. All tests in this file FAIL until the registry entry
// {Generic: "version", Deployed: "mosaic_version", Legacy: "version"} is added.
//
// Invariants verified:
//
//   - All() contains an entry with Generic="version", Deployed="mosaic_version", Legacy="version".
//   - ByGeneric("version") finds the entry and returns the correct FieldName.
//   - ByDeployedName("mosaic_version") finds the entry by its prefixed name.
//   - ByDeployedName("version") finds the entry by its legacy name.
//   - ReadOrder on the entry returns ["mosaic_version", "version"] (prefixed first).
//   - IsMosaicOnlyDeployedKey("version") returns true (legacy form recognised).
//   - IsMosaicOnlyDeployedKey("mosaic_version") returns true (prefixed form recognised).
//   - PromoteDropKeys() does NOT contain "version" or "mosaic_version" (Generic != "" guard).

import (
	"testing"

	"mosaic-deploy/internal/agentfields"
)

// ---------------------------------------------------------------------------
// Registry presence
// ---------------------------------------------------------------------------

// TestAll_ContainsVersionEntry verifies that All() returns an entry with
// Generic="version", Deployed="mosaic_version", Legacy="version". This entry is
// the single authoritative definition of the skill version field rename.
// FAILS until the entry is added to the registry.
func TestAll_ContainsVersionEntry(t *testing.T) {
	var found bool
	for _, f := range agentfields.All() {
		if f.Generic == "version" {
			found = true
			if f.Deployed != "mosaic_version" {
				t.Errorf("version entry: Deployed=%q, want %q", f.Deployed, "mosaic_version")
			}
			if f.Legacy != "version" {
				t.Errorf("version entry: Legacy=%q, want %q", f.Legacy, "version")
			}
		}
	}
	if !found {
		t.Error("All() does not contain an entry with Generic==\"version\"; the skill version prefix registry entry must be present")
	}
}

// ---------------------------------------------------------------------------
// ByGeneric lookup
// ---------------------------------------------------------------------------

// TestSkillVersionEntry_ByGeneric_ReturnsEntry verifies that ByGeneric("version")
// finds the skill version registry entry. This enables callers to resolve
// the deployed field name from the source (generic) field name.
// FAILS until the registry entry is added.
func TestSkillVersionEntry_ByGeneric_ReturnsEntry(t *testing.T) {
	f, ok := agentfields.ByGeneric("version")
	if !ok {
		t.Fatal("ByGeneric(\"version\") returned ok=false; the skill version entry must be findable by its generic key")
	}
	if f.Deployed != "mosaic_version" {
		t.Errorf("ByGeneric(\"version\").Deployed = %q, want %q", f.Deployed, "mosaic_version")
	}
	if f.Legacy != "version" {
		t.Errorf("ByGeneric(\"version\").Legacy = %q, want %q", f.Legacy, "version")
	}
}

// TestSkillVersionEntry_ByGeneric_DeployedStartsWithMosaicPrefix verifies that the
// skill version entry's Deployed name starts with "mosaic_", consistent with the naming
// convention for all other MOSAIC-only deployed fields.
// FAILS until the registry entry is added.
func TestSkillVersionEntry_ByGeneric_DeployedStartsWithMosaicPrefix(t *testing.T) {
	f, ok := agentfields.ByGeneric("version")
	if !ok {
		t.Fatal("ByGeneric(\"version\") returned ok=false; the skill version entry must be findable by its generic key")
	}
	if len(f.Deployed) < 7 || f.Deployed[:7] != "mosaic_" {
		t.Errorf("ByGeneric(\"version\").Deployed = %q, want \"mosaic_\" prefix; skill version deployed name must follow the mosaic_ convention",
			f.Deployed)
	}
}

// ---------------------------------------------------------------------------
// ByDeployedName lookup — both prefixed and legacy forms
// ---------------------------------------------------------------------------

// TestSkillVersionEntry_ByDeployedName_PrefixedNameFound verifies that
// ByDeployedName("mosaic_version") finds the skill version registry entry.
// This enables the probe to resolve the ReadOrder from the deployed (prefixed) name.
// FAILS until the registry entry is added.
func TestSkillVersionEntry_ByDeployedName_PrefixedNameFound(t *testing.T) {
	f, ok := agentfields.ByDeployedName("mosaic_version")
	if !ok {
		t.Fatal("ByDeployedName(\"mosaic_version\") returned ok=false; prefixed name must be findable in the registry")
	}
	if f.Generic != "version" {
		t.Errorf("ByDeployedName(\"mosaic_version\").Generic = %q, want %q", f.Generic, "version")
	}
	if f.Legacy != "version" {
		t.Errorf("ByDeployedName(\"mosaic_version\").Legacy = %q, want %q", f.Legacy, "version")
	}
}

// TestSkillVersionEntry_ByDeployedName_LegacyNameFound verifies that
// ByDeployedName("version") finds the skill version registry entry. This is required
// so that readDeployedStamp can accept the legacy field name when reading deployed
// skill files written before the migration.
// FAILS until the registry entry is added.
func TestSkillVersionEntry_ByDeployedName_LegacyNameFound(t *testing.T) {
	f, ok := agentfields.ByDeployedName("version")
	if !ok {
		t.Fatal("ByDeployedName(\"version\") returned ok=false; legacy version name must be findable in the registry for backwards compatibility")
	}
	if f.Deployed != "mosaic_version" {
		t.Errorf("ByDeployedName(\"version\").Deployed = %q, want %q", f.Deployed, "mosaic_version")
	}
}

// TestSkillVersionEntry_ByDeployedName_BothNamesSameEntry verifies that looking up
// "mosaic_version" and "version" returns logically identical entries (same Deployed and
// Legacy values), confirming both names resolve to the single authoritative mapping.
// FAILS until the registry entry is added.
func TestSkillVersionEntry_ByDeployedName_BothNamesSameEntry(t *testing.T) {
	byPrefixed, ok1 := agentfields.ByDeployedName("mosaic_version")
	byLegacy, ok2 := agentfields.ByDeployedName("version")
	if !ok1 || !ok2 {
		t.Fatalf("lookup failed: prefixed ok=%v, legacy ok=%v; both forms must be findable", ok1, ok2)
	}
	if byPrefixed.Deployed != byLegacy.Deployed || byPrefixed.Legacy != byLegacy.Legacy {
		t.Errorf("prefixed and legacy lookups returned different entries: %+v vs %+v; both must resolve to the same registry entry",
			byPrefixed, byLegacy)
	}
}

// ---------------------------------------------------------------------------
// ReadOrder — prefixed name is tried first
// ---------------------------------------------------------------------------

// TestSkillVersionEntry_ReadOrder_MosaicVersionBeforeLegacy verifies that ReadOrder
// on the skill version entry returns ["mosaic_version", "version"] — the prefixed name
// first, the legacy name second. Read sites must try "mosaic_version" first so that newly
// deployed skill files (which carry the prefixed name) are read correctly.
// FAILS until the registry entry is added.
func TestSkillVersionEntry_ReadOrder_MosaicVersionBeforeLegacy(t *testing.T) {
	f, ok := agentfields.ByGeneric("version")
	if !ok {
		t.Fatal("ByGeneric(\"version\") not found; cannot verify ReadOrder")
	}
	order := agentfields.ReadOrder(f)
	if len(order) != 2 {
		t.Fatalf("ReadOrder(version entry): len=%d, want 2", len(order))
	}
	if order[0] != "mosaic_version" {
		t.Errorf("ReadOrder(version entry)[0]=%q, want \"mosaic_version\" (prefixed name must be tried first)", order[0])
	}
	if order[1] != "version" {
		t.Errorf("ReadOrder(version entry)[1]=%q, want \"version\" (legacy name must be tried second)", order[1])
	}
}

// ---------------------------------------------------------------------------
// IsMosaicOnlyDeployedKey — both field name forms are recognised
// ---------------------------------------------------------------------------

// TestSkillVersionEntry_IsMosaicOnlyDeployedKey_Version_True verifies that
// IsMosaicOnlyDeployedKey("version") returns true once the skill version entry is in the
// registry. The legacy name "version" is a MOSAIC-controlled field name in deployed skill
// files (it is the legacy form of "mosaic_version") and must be recognised as such.
// FAILS until the registry entry is added.
func TestSkillVersionEntry_IsMosaicOnlyDeployedKey_Version_True(t *testing.T) {
	if !agentfields.IsMosaicOnlyDeployedKey("version") {
		t.Error("IsMosaicOnlyDeployedKey(\"version\") = false, want true; " +
			"\"version\" is the legacy form of \"mosaic_version\" in deployed skill files and must be recognised as a MOSAIC-only deployed key")
	}
}

// TestSkillVersionEntry_IsMosaicOnlyDeployedKey_MosaicVersion_True verifies that
// IsMosaicOnlyDeployedKey("mosaic_version") returns true, recognising the prefixed
// deployed name as a MOSAIC-only deployed key.
// FAILS until the registry entry is added.
func TestSkillVersionEntry_IsMosaicOnlyDeployedKey_MosaicVersion_True(t *testing.T) {
	if !agentfields.IsMosaicOnlyDeployedKey("mosaic_version") {
		t.Error("IsMosaicOnlyDeployedKey(\"mosaic_version\") = false, want true; " +
			"\"mosaic_version\" is the prefixed deployed name for the skill version field")
	}
}

// ---------------------------------------------------------------------------
// PromoteDropKeys — version/mosaic_version must NOT be in the drop set
// ---------------------------------------------------------------------------

// TestSkillVersionEntry_PromoteDropKeys_DoesNotContainVersionOrMosaicVersion verifies
// that PromoteDropKeys() does not include "version" or "mosaic_version". The skill
// version registry entry has Generic="version" (non-empty), so the existing
// PromoteDropKeys guard (if f.Generic != "" { continue }) correctly excludes it from
// the drop set. Promote must not drop these keys: it does not process skills, and
// dropping "version" from agent files would be wrong.
//
// This test PASSES both before and after the implementation. Before: the entry does not
// exist, so "version"/"mosaic_version" are absent from the drop set trivially. After:
// the entry exists but has Generic!="", so the exclusion guard applies and the keys
// remain absent from the drop set. The test serves as a regression guard confirming
// that the new entry's non-empty Generic field is correctly handled by PromoteDropKeys.
func TestSkillVersionEntry_PromoteDropKeys_DoesNotContainVersionOrMosaicVersion(t *testing.T) {
	for _, k := range agentfields.PromoteDropKeys() {
		if k == "version" {
			t.Error("PromoteDropKeys() contains \"version\"; the skill version entry has Generic!=\"\" and must be excluded from the drop set")
		}
		if k == "mosaic_version" {
			t.Error("PromoteDropKeys() contains \"mosaic_version\"; the skill version entry has Generic!=\"\" and must be excluded from the drop set")
		}
	}
}
