package cli

// cli_tui_parity_test.go verifies the CLI side of CLI/TUI parity (T10.3):
// every per-run setting overridable in the TUI has an equivalent CLI flag,
// and the correspondence is stated explicitly in TUIParityFlags /
// TUIParityBoolFlags rather than held by coincidence.
//
// The TUI side of the parity assertion (every TUI SettingKind has a CLI
// flag equivalent) lives in internal/tui/cli_parity_test.go.
//
// Success criterion pinned here: AC10.5 — every TUI-overridable setting has
// an equivalent CLI flag and vice versa, with the parity rule stated where
// the settings are declared.

import "testing"

// TestCLITUIParity_ParityDeclaration_IsNotEmpty verifies that at least one
// flag appears in the parity declaration. An entirely empty declaration means
// the parity rule has not been stated at all — the implementation task
// (I10.3) must populate these lists before this test passes.
func TestCLITUIParity_ParityDeclaration_IsNotEmpty(t *testing.T) {
	combined := append(append([]string{}, TUIParityFlags...), TUIParityBoolFlags...)
	if len(combined) == 0 {
		t.Errorf("TUIParityFlags and TUIParityBoolFlags are both empty; the parity rule must be stated explicitly in parity.go (I10.3) — at least the four flags that correspond to the four TUI settings must be listed")
	}
}

// TestCLITUIParity_AllTUIParityValueFlags_AreRecognized verifies that every
// flag listed in TUIParityFlags is a recognised value-flag in this package's
// command surface. A phantom entry (listed in the declaration but absent from
// valueFlags) would break the tool's command line without a compile error.
func TestCLITUIParity_AllTUIParityValueFlags_AreRecognized(t *testing.T) {
	for _, flag := range TUIParityFlags {
		if !valueFlags[flag] {
			t.Errorf("TUIParityFlags contains %q, which is not a recognised value-flag (not in valueFlags); either add it to valueFlags or remove it from TUIParityFlags", flag)
		}
	}
}

// TestCLITUIParity_AllTUIParityBoolFlags_AreRecognized verifies that every
// flag listed in TUIParityBoolFlags is a recognised bool-flag in this
// package's command surface.
func TestCLITUIParity_AllTUIParityBoolFlags_AreRecognized(t *testing.T) {
	for _, flag := range TUIParityBoolFlags {
		if !boolFlags[flag] {
			t.Errorf("TUIParityBoolFlags contains %q, which is not a recognised bool-flag (not in boolFlags); either add it to boolFlags or remove it from TUIParityBoolFlags", flag)
		}
	}
}

// TestCLITUIParity_RepetitionsFlag_IsInParityDeclaration verifies that
// --repetitions is listed in TUIParityFlags. Repetitions is the canonical
// per-run setting with both CLI and TUI equivalents, and is the first
// setting the Stage 10 design discusses.
func TestCLITUIParity_RepetitionsFlag_IsInParityDeclaration(t *testing.T) {
	for _, flag := range TUIParityFlags {
		if flag == "repetitions" {
			return
		}
	}
	t.Errorf("TUIParityFlags does not include 'repetitions'; the TUI settings screen has a repetitions control that maps to --repetitions, so it must be listed in the parity declaration")
}

// TestCLITUIParity_CatalogFolderFlag_IsInParityDeclaration verifies that
// --catalog-folder is listed in TUIParityFlags.
func TestCLITUIParity_CatalogFolderFlag_IsInParityDeclaration(t *testing.T) {
	for _, flag := range TUIParityFlags {
		if flag == "catalog-folder" {
			return
		}
	}
	t.Errorf("TUIParityFlags does not include 'catalog-folder'; the TUI settings screen has a catalog-folder control that maps to --catalog-folder, so it must be listed in the parity declaration")
}

// TestCLITUIParity_ReportPathFlag_IsInParityDeclaration verifies that
// --report-path is listed in TUIParityFlags.
func TestCLITUIParity_ReportPathFlag_IsInParityDeclaration(t *testing.T) {
	for _, flag := range TUIParityFlags {
		if flag == "report-path" {
			return
		}
	}
	t.Errorf("TUIParityFlags does not include 'report-path'; the TUI settings screen has a report-path control that maps to --report-path, so it must be listed in the parity declaration")
}

// TestCLITUIParity_RetentionFlags_AreInParityDeclaration verifies that both
// --keep-sandbox and --keep-sandbox-on-failure are listed in TUIParityBoolFlags.
// The TUI retention setting is a three-state cycle (Never/OnFailure/Always)
// that maps to two CLI bool flags; both must be declared.
func TestCLITUIParity_RetentionFlags_AreInParityDeclaration(t *testing.T) {
	var foundKeepSandbox, foundKeepOnFailure bool
	for _, flag := range TUIParityBoolFlags {
		if flag == "keep-sandbox" {
			foundKeepSandbox = true
		}
		if flag == "keep-sandbox-on-failure" {
			foundKeepOnFailure = true
		}
	}
	if !foundKeepSandbox {
		t.Errorf("TUIParityBoolFlags does not include 'keep-sandbox'; the TUI retention setting's 'always' state corresponds to --keep-sandbox and must appear in the parity declaration")
	}
	if !foundKeepOnFailure {
		t.Errorf("TUIParityBoolFlags does not include 'keep-sandbox-on-failure'; the TUI retention setting's 'on-failure' state corresponds to --keep-sandbox-on-failure and must appear in the parity declaration")
	}
}

// TestCLITUIParity_NoDuplicates_InTUIParityFlags verifies that no flag name
// appears more than once in TUIParityFlags. A duplicate entry would not break
// the tool but would indicate the declaration is not maintained carefully.
func TestCLITUIParity_NoDuplicates_InTUIParityFlags(t *testing.T) {
	seen := make(map[string]bool, len(TUIParityFlags))
	for _, flag := range TUIParityFlags {
		if seen[flag] {
			t.Errorf("TUIParityFlags contains duplicate entry %q; each flag must appear exactly once in the parity declaration", flag)
		}
		seen[flag] = true
	}
}

// TestCLITUIParity_NoDuplicates_InTUIParityBoolFlags verifies that no flag
// name appears more than once in TUIParityBoolFlags.
func TestCLITUIParity_NoDuplicates_InTUIParityBoolFlags(t *testing.T) {
	seen := make(map[string]bool, len(TUIParityBoolFlags))
	for _, flag := range TUIParityBoolFlags {
		if seen[flag] {
			t.Errorf("TUIParityBoolFlags contains duplicate entry %q; each flag must appear exactly once in the parity declaration", flag)
		}
		seen[flag] = true
	}
}
