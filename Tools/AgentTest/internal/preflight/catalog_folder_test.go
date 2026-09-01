package preflight_test

// Tests for Stage 4: CatalogFolder threading through preflight.Validate.
//
// These tests verify that Validate resolves preflight.Overrides.CatalogFolder
// into Plan.CatalogFolder when set, and falls back to Input.CatalogFolder
// when nil. The resolution is specified as unconditional — even when Validate
// returns early (e.g. missing-suite path), the returned Plan still carries a
// resolved CatalogFolder.
//
// These tests reference fields not yet added to the implementation:
//   - preflight.Input.CatalogFolder (not yet on Input struct)
//   - preflight.Overrides.CatalogFolder (not yet on Overrides struct)
//   - preflight.Plan.CatalogFolder (not yet on Plan struct)
//
// They will fail to compile until the implementation adds those fields, which
// is the expected RED state for TDD.

import (
	"path/filepath"
	"testing"

	"mosaic-agent-test/internal/preflight"
)

// ---------------------------------------------------------------------------
// CatalogFolder resolution: override wins when non-nil
// ---------------------------------------------------------------------------

// TestValidate_CatalogFolder_OverrideWins verifies that when
// Overrides.CatalogFolder is non-nil, its value is used as Plan.CatalogFolder,
// taking precedence over Input.CatalogFolder — the same override-wins-when-non-
// nil convention Repetitions, Timeout, and TurnLimit use.
func TestValidate_CatalogFolder_OverrideWins(t *testing.T) {
	const defaultCatalog = "C:/default/catalog"
	const overrideCatalog = "C:/override/catalog"

	// Write a minimal but valid suite tree so Validate doesn't short-circuit
	// on a missing-suite error before reaching the CatalogFolder resolution.
	root := writeTree(t, map[string]string{
		"s.suite.yaml":          validSuite,
		"happy-path.test.yaml":  validDefinition,
		"happy-path.stubs.json": validRegistry,
		"agents/researcher.md":  "# stub researcher placeholder",
	})

	override := overrideCatalog
	plan, _ := preflight.Validate(preflight.Input{
		SuitePath:     filepath.Join(root, "s.suite.yaml"),
		FixtureRoot:   filepath.Join(root, "fixtures"),
		HarnessID:     "claude-code",
		CatalogFolder: defaultCatalog,
		Overrides: preflight.Overrides{
			CatalogFolder: &override,
		},
	})

	if plan.CatalogFolder != overrideCatalog {
		t.Errorf("Plan.CatalogFolder = %q, want %q (override must win over default when Overrides.CatalogFolder is non-nil)", plan.CatalogFolder, overrideCatalog)
	}
}

// TestValidate_CatalogFolder_DefaultUsedWhenOverrideNil verifies that when
// Overrides.CatalogFolder is nil, Plan.CatalogFolder receives Input.CatalogFolder
// (the process-wide default), not the zero string.
func TestValidate_CatalogFolder_DefaultUsedWhenOverrideNil(t *testing.T) {
	const defaultCatalog = "C:/default/catalog"

	root := writeTree(t, map[string]string{
		"s.suite.yaml":          validSuite,
		"happy-path.test.yaml":  validDefinition,
		"happy-path.stubs.json": validRegistry,
		"agents/researcher.md":  "# stub researcher placeholder",
	})

	plan, _ := preflight.Validate(preflight.Input{
		SuitePath:     filepath.Join(root, "s.suite.yaml"),
		FixtureRoot:   filepath.Join(root, "fixtures"),
		HarnessID:     "claude-code",
		CatalogFolder: defaultCatalog,
		Overrides: preflight.Overrides{
			CatalogFolder: nil, // explicitly nil: use the default
		},
	})

	if plan.CatalogFolder != defaultCatalog {
		t.Errorf("Plan.CatalogFolder = %q, want %q (default must be used when Overrides.CatalogFolder is nil)", plan.CatalogFolder, defaultCatalog)
	}
}

// TestValidate_CatalogFolder_EmptyDefaultPreservedWhenNoOverride verifies that
// when both Input.CatalogFolder and Overrides.CatalogFolder are empty/nil,
// Plan.CatalogFolder is the empty string. This exercises the zero-value path
// that remains valid (empty means "no override" for the deploy tool).
func TestValidate_CatalogFolder_EmptyDefaultPreservedWhenNoOverride(t *testing.T) {
	root := writeTree(t, map[string]string{
		"s.suite.yaml":          validSuite,
		"happy-path.test.yaml":  validDefinition,
		"happy-path.stubs.json": validRegistry,
		"agents/researcher.md":  "# stub researcher placeholder",
	})

	plan, _ := preflight.Validate(preflight.Input{
		SuitePath:     filepath.Join(root, "s.suite.yaml"),
		FixtureRoot:   filepath.Join(root, "fixtures"),
		HarnessID:     "claude-code",
		CatalogFolder: "", // empty default: no catalog override
		Overrides:     preflight.Overrides{CatalogFolder: nil},
	})

	if plan.CatalogFolder != "" {
		t.Errorf("Plan.CatalogFolder = %q, want empty string (empty default with nil override must produce empty Plan.CatalogFolder)", plan.CatalogFolder)
	}
}

// ---------------------------------------------------------------------------
// CatalogFolder resolution: unconditional (applies even on early-return paths)
// ---------------------------------------------------------------------------

// TestValidate_CatalogFolder_ResolvedOnEarlyReturn_MissingSuite verifies that
// even when Validate returns early due to a missing suite file, the returned
// Plan still carries a resolved CatalogFolder. The design specifies that the
// resolution happens unconditionally as the very first operation in Validate,
// before any early return.
func TestValidate_CatalogFolder_ResolvedOnEarlyReturn_MissingSuite(t *testing.T) {
	const defaultCatalog = "C:/default/catalog"
	const overrideCatalog = "C:/override/catalog"

	override := overrideCatalog
	// Deliberately missing suite file: the early-return path fires.
	plan, rpt := preflight.Validate(preflight.Input{
		SuitePath:     "/nonexistent/path/suite.yaml",
		FixtureRoot:   t.TempDir(),
		HarnessID:     "claude-code",
		CatalogFolder: defaultCatalog,
		Overrides: preflight.Overrides{
			CatalogFolder: &override,
		},
	})

	if !rpt.HasErrors() {
		t.Fatal("expected a missing-suite error, but Validate returned no errors (test setup may be wrong)")
	}
	if plan.CatalogFolder != overrideCatalog {
		t.Errorf("Plan.CatalogFolder = %q on early return, want %q (the CatalogFolder resolution must happen before any early return so every Plan carries a resolved value)", plan.CatalogFolder, overrideCatalog)
	}
}

// TestValidate_CatalogFolder_DefaultOnEarlyReturn_WhenNoOverride verifies that
// when the missing-suite early return fires and Overrides.CatalogFolder is nil,
// Plan.CatalogFolder still receives Input.CatalogFolder.
func TestValidate_CatalogFolder_DefaultOnEarlyReturn_WhenNoOverride(t *testing.T) {
	const defaultCatalog = "C:/default/catalog"

	plan, rpt := preflight.Validate(preflight.Input{
		SuitePath:     "/nonexistent/path/suite.yaml",
		FixtureRoot:   t.TempDir(),
		HarnessID:     "claude-code",
		CatalogFolder: defaultCatalog,
		Overrides:     preflight.Overrides{CatalogFolder: nil},
	})

	if !rpt.HasErrors() {
		t.Fatal("expected a missing-suite error, but Validate returned no errors (test setup may be wrong)")
	}
	if plan.CatalogFolder != defaultCatalog {
		t.Errorf("Plan.CatalogFolder = %q on early return, want %q (default must flow to Plan even when Validate returns early)", plan.CatalogFolder, defaultCatalog)
	}
}
