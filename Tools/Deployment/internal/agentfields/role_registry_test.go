package agentfields_test

// role_registry_test.go verifies the registry entry and classification predicates for the
// `role` field (Generic="role", Deployed="mosaic_role", Legacy="role").
//
// All tests in this file FAIL until I3.1 adds the role entry to the registry and I3.5
// generalises PromoteDropKeys to exclude entries with a non-empty Generic.
//
// Behaviours verified:
//
//   - All() contains a role entry with Generic="role", Deployed="mosaic_role", Legacy="role".
//   - ByGeneric("role") returns the role entry.
//   - ByDeployedName("mosaic_role") and ByDeployedName("role") both return the role entry.
//   - ReadOrder(roleField) returns ["mosaic_role", "role"] — prefixed tried first.
//   - IsGenericVocabularyKey("role") is true (unchanged; role stays in genericVocabularyKeys).
//   - IsGenericVocabularyKey("mosaic_role") is false (prefixed names are not generic vocabulary).
//   - IsMosaicOnlyDeployedKey("role") is true via the Legacy name.
//   - IsMosaicOnlyDeployedKey("mosaic_role") is true via the Deployed name.
//   - IsKnownMosaicKey("mosaic_role") is true so the unknown-field strip never removes it.
//   - PromoteDropKeys does not contain "role" or "mosaic_role" — promote reconstructs the
//     role field under its generic key rather than dropping it.

import (
	"testing"

	"mosaic-deploy/internal/agentfields"
)

// ---------------------------------------------------------------------------
// All() — role entry presence
// ---------------------------------------------------------------------------

// TestAll_ContainsRoleField verifies that All() contains a role entry with the correct
// Generic, Deployed, and Legacy names. FAILS until I3.1 adds the role entry to the registry.
func TestAll_ContainsRoleField(t *testing.T) {
	var found bool
	for _, f := range agentfields.All() {
		if f.Legacy == "role" && f.Generic == "role" {
			found = true
			if f.Deployed != "mosaic_role" {
				t.Errorf("role entry: Deployed=%q, want %q", f.Deployed, "mosaic_role")
			}
			if f.Generic != "role" {
				t.Errorf("role entry: Generic=%q, want %q", f.Generic, "role")
			}
			if f.Legacy != "role" {
				t.Errorf("role entry: Legacy=%q, want %q", f.Legacy, "role")
			}
		}
	}
	if !found {
		t.Error("All() does not contain an entry with Generic==\"role\" and Legacy==\"role\"; " +
			"the role field must be in the registry with {Generic:\"role\", Deployed:\"mosaic_role\", Legacy:\"role\"}")
	}
}

// ---------------------------------------------------------------------------
// ByGeneric — role field lookup by generic key
// ---------------------------------------------------------------------------

// TestByGeneric_RoleField_ReturnsEntry verifies that ByGeneric("role") returns the role
// registry entry. FAILS until I3.1 adds the role entry.
func TestByGeneric_RoleField_ReturnsEntry(t *testing.T) {
	f, ok := agentfields.ByGeneric("role")
	if !ok {
		t.Fatal("ByGeneric(\"role\") returned ok=false; the role entry must be findable by its generic key")
	}
	if f.Deployed != "mosaic_role" {
		t.Errorf("ByGeneric(\"role\").Deployed = %q, want %q", f.Deployed, "mosaic_role")
	}
	if f.Legacy != "role" {
		t.Errorf("ByGeneric(\"role\").Legacy = %q, want %q", f.Legacy, "role")
	}
}

// ---------------------------------------------------------------------------
// ByDeployedName — role field lookup by deployed and legacy name
// ---------------------------------------------------------------------------

// TestByDeployedName_PrefixedMosaicRole_Found verifies that looking up "mosaic_role" by its
// Deployed (mosaic_-prefixed) name returns the role entry. FAILS until I3.1.
func TestByDeployedName_PrefixedMosaicRole_Found(t *testing.T) {
	f, ok := agentfields.ByDeployedName("mosaic_role")
	if !ok {
		t.Fatal("ByDeployedName(\"mosaic_role\") returned ok=false; the prefixed name must be findable")
	}
	if f.Generic != "role" {
		t.Errorf("ByDeployedName(\"mosaic_role\").Generic = %q, want %q", f.Generic, "role")
	}
	if f.Legacy != "role" {
		t.Errorf("ByDeployedName(\"mosaic_role\").Legacy = %q, want %q", f.Legacy, "role")
	}
}

// TestByDeployedName_LegacyRole_Found verifies that looking up "role" by its Legacy (unprefixed)
// name via ByDeployedName returns the role registry entry. FAILS until I3.1 adds the role entry
// (role is only in genericVocabularyKeys today, not in the registry's Deployed/Legacy lookup).
func TestByDeployedName_LegacyRole_Found(t *testing.T) {
	f, ok := agentfields.ByDeployedName("role")
	if !ok {
		t.Fatal("ByDeployedName(\"role\") returned ok=false; the legacy name must also be findable for backward compatibility")
	}
	if f.Deployed != "mosaic_role" {
		t.Errorf("ByDeployedName(\"role\").Deployed = %q, want %q", f.Deployed, "mosaic_role")
	}
}

// TestByDeployedName_BothPrefixedAndLegacyRoleReferToSameEntry verifies that looking up
// "mosaic_role" and "role" return logically identical entries. FAILS until I3.1.
func TestByDeployedName_BothPrefixedAndLegacyRoleReferToSameEntry(t *testing.T) {
	byPrefixed, ok1 := agentfields.ByDeployedName("mosaic_role")
	byLegacy, ok2 := agentfields.ByDeployedName("role")
	if !ok1 || !ok2 {
		t.Fatalf("lookup failed: prefixed ok=%v, legacy ok=%v; both forms must be findable", ok1, ok2)
	}
	if byPrefixed.Deployed != byLegacy.Deployed || byPrefixed.Legacy != byLegacy.Legacy {
		t.Errorf("prefixed and legacy lookups returned different entries: %+v vs %+v", byPrefixed, byLegacy)
	}
}

// ---------------------------------------------------------------------------
// ReadOrder — role field read order
// ---------------------------------------------------------------------------

// TestReadOrder_RoleField_PrefixedBeforeLegacy verifies that ReadOrder for the role field
// returns ["mosaic_role", "role"] — the prefixed form is tried first, so readers that use
// ReadOrder prefer mosaic_role when both forms are present. FAILS until I3.1.
func TestReadOrder_RoleField_PrefixedBeforeLegacy(t *testing.T) {
	roleField, ok := agentfields.ByGeneric("role")
	if !ok {
		t.Fatal("ByGeneric(\"role\") returned ok=false; cannot test ReadOrder without the role entry")
	}
	order := agentfields.ReadOrder(roleField)
	if len(order) != 2 {
		t.Fatalf("ReadOrder(roleField) length = %d, want 2", len(order))
	}
	if order[0] != "mosaic_role" {
		t.Errorf("ReadOrder(roleField)[0] = %q, want \"mosaic_role\"; prefixed name must be tried first", order[0])
	}
	if order[1] != "role" {
		t.Errorf("ReadOrder(roleField)[1] = %q, want \"role\"; legacy name must be the fallback", order[1])
	}
}

// ---------------------------------------------------------------------------
// Classification predicates
// ---------------------------------------------------------------------------

// TestIsGenericVocabularyKey_RoleKey_True verifies that IsGenericVocabularyKey("role") remains
// true after the role entry is added to the registry. The generic vocabulary is unchanged.
// This test PASSES both before and after the implementation: it confirms no regression.
func TestIsGenericVocabularyKey_RoleKey_True(t *testing.T) {
	if !agentfields.IsGenericVocabularyKey("role") {
		t.Error("IsGenericVocabularyKey(\"role\") = false, want true; " +
			"role must remain in the generic vocabulary after the registry entry is added")
	}
}

// TestIsGenericVocabularyKey_MosaicRoleKey_False verifies that IsGenericVocabularyKey("mosaic_role")
// is false. Prefixed deployed names are never generic vocabulary.
// This test PASSES both before and after the implementation: it confirms no regression.
func TestIsGenericVocabularyKey_MosaicRoleKey_False(t *testing.T) {
	if agentfields.IsGenericVocabularyKey("mosaic_role") {
		t.Error("IsGenericVocabularyKey(\"mosaic_role\") = true, want false; " +
			"prefixed deployed names must not be treated as generic vocabulary keys")
	}
}

// TestIsMosaicOnlyDeployedKey_LegacyRoleKey_True verifies that IsMosaicOnlyDeployedKey("role")
// returns true via the Legacy name of the role entry. This enables the unknown-field strip to
// recognize the bare role key in deployed files as a known MOSAIC field.
// FAILS until I3.1 adds the role entry to the registry.
func TestIsMosaicOnlyDeployedKey_LegacyRoleKey_True(t *testing.T) {
	if !agentfields.IsMosaicOnlyDeployedKey("role") {
		t.Error("IsMosaicOnlyDeployedKey(\"role\") = false, want true; " +
			"\"role\" is the Legacy name of the role registry entry and must be recognized as a MOSAIC-only deployed key")
	}
}

// TestIsMosaicOnlyDeployedKey_PrefixedMosaicRoleKey_True verifies that
// IsMosaicOnlyDeployedKey("mosaic_role") returns true via the Deployed name of the role entry.
// This ensures the prefixed form is also recognized and not stripped by the unknown-field strip.
// FAILS until I3.1 adds the role entry to the registry.
func TestIsMosaicOnlyDeployedKey_PrefixedMosaicRoleKey_True(t *testing.T) {
	if !agentfields.IsMosaicOnlyDeployedKey("mosaic_role") {
		t.Error("IsMosaicOnlyDeployedKey(\"mosaic_role\") = false, want true; " +
			"\"mosaic_role\" is the Deployed name of the role registry entry and must be recognized as a MOSAIC-only deployed key")
	}
}

// TestIsKnownMosaicKey_MosaicRoleKey_True verifies that IsKnownMosaicKey("mosaic_role") returns
// true so that the unknown-field strip in the descriptor classifier never removes mosaic_role from
// a deployed file. FAILS until I3.1 adds the role entry so IsMosaicOnlyDeployedKey("mosaic_role")
// returns true.
func TestIsKnownMosaicKey_MosaicRoleKey_True(t *testing.T) {
	if !agentfields.IsKnownMosaicKey("mosaic_role") {
		t.Error("IsKnownMosaicKey(\"mosaic_role\") = false, want true; " +
			"the prefixed role key must be recognized as a known MOSAIC field so the unknown-field strip never removes it")
	}
}

// TestIsKnownMosaicKey_BareRoleKey_True verifies that IsKnownMosaicKey("role") remains true.
// This is already satisfied by IsGenericVocabularyKey("role"), so this test PASSES both
// before and after the implementation: it confirms no regression for the bare role key.
func TestIsKnownMosaicKey_BareRoleKey_True(t *testing.T) {
	if !agentfields.IsKnownMosaicKey("role") {
		t.Error("IsKnownMosaicKey(\"role\") = false, want true; " +
			"\"role\" must continue to be recognized as a known MOSAIC field")
	}
}

// ---------------------------------------------------------------------------
// PromoteDropKeys — role entry excluded from the drop set
// ---------------------------------------------------------------------------

// TestPromoteDropKeys_DoesNotContainRoleOrMosaicRole verifies that PromoteDropKeys does not
// include "role" or "mosaic_role". Promote reconstructs the role field under its generic key
// rather than dropping it, exactly as it does for the id field.
//
// This test FAILS once I3.1 adds the role entry to the registry but before I3.5 generalises
// the skip condition from "Generic == id" to "Generic != """. After both I3.1 and I3.5 this
// test PASSES, confirming the role entry is correctly excluded from the drop set.
func TestPromoteDropKeys_DoesNotContainRoleOrMosaicRole(t *testing.T) {
	for _, k := range agentfields.PromoteDropKeys() {
		if k == "role" {
			t.Error("PromoteDropKeys must not contain \"role\"; promote reconstructs the role under its generic key, just as it does for id")
		}
		if k == "mosaic_role" {
			t.Error("PromoteDropKeys must not contain \"mosaic_role\"; promote handles the role entry explicitly and must not drop it via the generic loop")
		}
	}
}
