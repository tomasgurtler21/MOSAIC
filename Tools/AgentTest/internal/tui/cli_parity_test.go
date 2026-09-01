package tui

// cli_parity_test.go verifies the TUI side of CLI/TUI parity (T10.3):
// every SettingKind listed in CLIParitySettingKinds is present in
// SettingsEntries(), so the parity declaration stays in sync with the
// actual settings screen content as settings are added or removed.
//
// The CLI side of the parity assertion lives in internal/cli/cli_tui_parity_test.go.
//
// Success criterion pinned here: AC10.5 — every TUI-overridable setting has
// an equivalent CLI flag and vice versa, with the parity rule stated where
// the settings are declared.

import "testing"

// TestTUICLIParity_ParityDeclaration_IsNotEmpty verifies that CLIParitySettingKinds
// is not empty. An empty declaration means the parity rule has not been stated
// for the TUI side — the implementation task (I10.3) must populate this list
// before this test passes.
func TestTUICLIParity_ParityDeclaration_IsNotEmpty(t *testing.T) {
	if len(CLIParitySettingKinds) == 0 {
		t.Errorf("CLIParitySettingKinds is empty; the parity rule must be stated explicitly in tui/parity.go (I10.3) — at least the four SettingKind values that correspond to CLI flags must be listed")
	}
}

// TestTUICLIParity_AllDeclaredKinds_PresentInSettingsEntries verifies that
// every SettingKind in CLIParitySettingKinds is produced by SettingsEntries()
// on a fresh Model. A kind listed in the declaration but absent from
// SettingsEntries() would mean the TUI settings screen does not actually
// offer that setting — the declaration would be misleading.
func TestTUICLIParity_AllDeclaredKinds_PresentInSettingsEntries(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	entries := m.SettingsEntries()

	presentKinds := make(map[SettingKind]bool, len(entries))
	for _, e := range entries {
		presentKinds[e.Kind] = true
	}

	for _, kind := range CLIParitySettingKinds {
		if !presentKinds[kind] {
			t.Errorf("CLIParitySettingKinds lists %q but SettingsEntries() produces no entry for that kind; the parity declaration and the settings screen are out of sync", kind)
		}
	}
}

// TestTUICLIParity_RetentionKind_InParityDeclaration verifies that
// SettingRetention is listed in CLIParitySettingKinds. Retention maps to
// --keep-sandbox / --keep-sandbox-on-failure on the CLI side.
func TestTUICLIParity_RetentionKind_InParityDeclaration(t *testing.T) {
	for _, kind := range CLIParitySettingKinds {
		if kind == SettingRetention {
			return
		}
	}
	t.Errorf("CLIParitySettingKinds does not include SettingRetention; the TUI retention setting maps to --keep-sandbox / --keep-sandbox-on-failure on the CLI and must appear in the parity declaration")
}

// TestTUICLIParity_RepetitionsKind_InParityDeclaration verifies that
// SettingRepetitions is listed in CLIParitySettingKinds. Repetitions maps
// to --repetitions on the CLI side.
func TestTUICLIParity_RepetitionsKind_InParityDeclaration(t *testing.T) {
	for _, kind := range CLIParitySettingKinds {
		if kind == SettingRepetitions {
			return
		}
	}
	t.Errorf("CLIParitySettingKinds does not include SettingRepetitions; the TUI repetitions setting maps to --repetitions on the CLI and must appear in the parity declaration")
}

// TestTUICLIParity_ReportPathKind_InParityDeclaration verifies that
// SettingReportPath is listed in CLIParitySettingKinds. Report path maps
// to --report-path on the CLI side.
func TestTUICLIParity_ReportPathKind_InParityDeclaration(t *testing.T) {
	for _, kind := range CLIParitySettingKinds {
		if kind == SettingReportPath {
			return
		}
	}
	t.Errorf("CLIParitySettingKinds does not include SettingReportPath; the TUI report-path setting maps to --report-path on the CLI and must appear in the parity declaration")
}

// TestTUICLIParity_CatalogKind_InParityDeclaration verifies that
// SettingCatalog is listed in CLIParitySettingKinds. Catalog folder maps
// to --catalog-folder on the CLI side.
func TestTUICLIParity_CatalogKind_InParityDeclaration(t *testing.T) {
	for _, kind := range CLIParitySettingKinds {
		if kind == SettingCatalog {
			return
		}
	}
	t.Errorf("CLIParitySettingKinds does not include SettingCatalog; the TUI catalog-folder setting maps to --catalog-folder on the CLI and must appear in the parity declaration")
}

// TestTUICLIParity_NoDuplicates_InCLIParitySettingKinds verifies that no
// SettingKind appears more than once in CLIParitySettingKinds. Duplicates
// would not break functionality but would indicate the declaration is not
// maintained carefully.
func TestTUICLIParity_NoDuplicates_InCLIParitySettingKinds(t *testing.T) {
	seen := make(map[SettingKind]bool, len(CLIParitySettingKinds))
	for _, kind := range CLIParitySettingKinds {
		if seen[kind] {
			t.Errorf("CLIParitySettingKinds contains duplicate entry %q; each SettingKind must appear exactly once in the parity declaration", kind)
		}
		seen[kind] = true
	}
}

// TestTUICLIParity_AllSettingsEntries_HaveParityDeclaration verifies the
// reverse direction: every SettingKind produced by SettingsEntries() on a
// fresh Model also appears in CLIParitySettingKinds. This ensures the
// declaration is complete — a new setting added to SettingsEntries() without
// a corresponding CLI flag entry would be caught here.
func TestTUICLIParity_AllSettingsEntries_HaveParityDeclaration(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	entries := m.SettingsEntries()

	declaredKinds := make(map[SettingKind]bool, len(CLIParitySettingKinds))
	for _, kind := range CLIParitySettingKinds {
		declaredKinds[kind] = true
	}

	for _, e := range entries {
		if !declaredKinds[e.Kind] {
			t.Errorf("SettingsEntries() produces a %q entry that is not listed in CLIParitySettingKinds; every TUI setting must have a corresponding CLI flag and be listed in the parity declaration — add %q to CLIParitySettingKinds or add the corresponding CLI flag", e.Kind, e.Kind)
		}
	}
}
