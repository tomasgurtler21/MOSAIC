package tui

// parity.go declares the CLI/TUI parity rule for the TUI side: every
// SettingKind listed in CLIParitySettingKinds must have at least one
// equivalent CLI flag. The parity test (cli_parity_test.go) verifies that
// each kind listed here is present in SettingsEntries(). Adding a new
// per-run setting to the settings screen requires updating this list so
// that the CLI maintainer knows a corresponding flag must be added.
//
// The CLI side of the parity declaration lives in internal/cli/parity.go.

// CLIParitySettingKinds lists every SettingKind for which the CLI command
// surface has at least one equivalent flag. Every entry here must appear in
// SettingsEntries(). Adding a new per-run setting to the settings screen
// requires also adding the corresponding CLI flag(s) and updating this list.
//
// Parity rule: every entry in this list must have at least one flag in
// cli.TUIParityFlags or cli.TUIParityBoolFlags, and vice versa.
var CLIParitySettingKinds = []SettingKind{
	// SettingRetention maps to --keep-sandbox (RetainAlways) and
	// --keep-sandbox-on-failure (RetainOnFailure) on the CLI.
	SettingRetention,
	// SettingRepetitions maps to --repetitions on the CLI.
	SettingRepetitions,
	// SettingReportPath maps to --report-path on the CLI.
	SettingReportPath,
	// SettingCatalog maps to --catalog-folder on the CLI.
	SettingCatalog,
}
