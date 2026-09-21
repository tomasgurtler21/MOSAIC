package descriptor_test

// codex_drop_list_invariant_test.go implements the registry-wide carriage-container
// drop-list invariant (T13.13).
//
// Coverage:
//
//   Registry-wide drop-list invariant (T13.13):
//   For every registered harness descriptor, the frontmatter drop list (FrontmatterSpec.Drop)
//   must contain neither "mosaic_carriage" (CarriageValuesKey) nor "mosaic_carriage_markers"
//   (CarriageMarkersKey). A drop entry for either container key would instruct applyFrontmatter
//   to remove the container from the canonical form before the translator sees it, silently
//   discarding every user-owned key that was carried through decode. The user's custom TOML
//   keys (reasoning_effort, mcp_servers, etc.) would be lost on every redeploy without any
//   report entry, directly violating FR-9c ("nothing of the user's may ever be lost silently").
//
//   The test is written as a loop over the registry (not a per-harness table) so any harness
//   added later inherits the check automatically without requiring a new test case.
//
//   This is the first stage where all registered descriptors and the carriage container
//   constants are available together in a single test file. The constants are authoritative:
//   they are defined in agentformat.CarriageValuesKey and agentformat.CarriageMarkersKey,
//   and both are returned by agentformat.CarriageKeys(). The test references them via the
//   exported CarriageKeys() function rather than string literals so a constant rename
//   propagates automatically.

import (
	"testing"

	"mosaic-deploy/internal/agentformat"
)

// ---------------------------------------------------------------------------
// T13.13 -- Registry-wide drop-list invariant
// ---------------------------------------------------------------------------

// TestRegistryWide_DropList_NoCarriageContainerKey verifies that for every registered
// harness descriptor, the FrontmatterSpec.Drop list contains neither "mosaic_carriage"
// nor "mosaic_carriage_markers". Either key present in a drop list would silently discard
// every user-owned carriage key on encode, violating FR-9c.
//
// The loop uses discoverAllBuiltins (defined in minimal_grant_registry_test.go in this
// package) to discover all five registered built-in harnesses from the frozen catalog.
// The frozen catalog ensures the Codex harness reports Usable: true.
//
// Carriage container key names come from agentformat.CarriageKeys() rather than string
// literals so a constant rename automatically propagates to this test.
func TestRegistryWide_DropList_NoCarriageContainerKey(t *testing.T) {
	reg := discoverAllBuiltins(t)

	// Collect carriage container keys once, so we don't depend on string literals.
	carriageKeys := agentformat.CarriageKeys()
	if len(carriageKeys) == 0 {
		t.Fatal("agentformat.CarriageKeys() returned an empty slice; " +
			"the constant set must contain at least mosaic_carriage and mosaic_carriage_markers; " +
			"this is a test setup failure, not a harness failure")
	}

	for _, ref := range reg.List() {
		if !ref.Usable {
			// A non-usable harness cannot be resolved and its descriptor cannot be read.
			continue
		}

		m, err := reg.Resolve(ref.ID)
		if err != nil {
			t.Errorf("Resolve(%q): %v; cannot check drop list for this harness", ref.ID, err)
			continue
		}

		desc := m.Descriptor()
		if desc == nil {
			t.Errorf("harness %q: Descriptor() returned nil; cannot check drop list", ref.ID)
			continue
		}

		t.Run(ref.ID, func(t *testing.T) {
			for _, dropKey := range desc.Frontmatter.Drop {
				for _, carriageKey := range carriageKeys {
					if dropKey == carriageKey {
						t.Errorf("harness %q: FrontmatterSpec.Drop contains carriage container key %q; "+
							"a drop entry for this key would instruct applyFrontmatter to remove the "+
							"carriage container from the canonical form before the translator sees it, "+
							"silently discarding every user-owned key that was carried through decode; "+
							"this violates FR-9c (nothing of the user's may ever be lost silently); "+
							"remove %q from this harness descriptor's drop list",
							ref.ID, carriageKey, carriageKey)
					}
				}
			}
		})
	}
}
