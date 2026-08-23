package cli

// parity.go declares the CLI/TUI parity rule for the CLI side: every flag
// listed in TUIParityFlags or TUIParityBoolFlags must have an equivalent
// SettingKind on the TUI's settings screen. The parity test
// (cli_tui_parity_test.go) verifies that each flag listed here is a
// recognized flag in this package's command surface. Adding a new per-run
// flag requires updating this list, which signals that a corresponding TUI
// setting must also be added.
//
// The TUI side of the parity declaration lives in internal/tui/parity.go.

// TUIParityFlags lists every value-flag that configures a per-run setting
// for which the TUI has an equivalent SettingKind on the settings screen.
//
// Parity rule: every entry here must appear in valueFlags, and each must
// correspond to a SettingKind in tui.CLIParitySettingKinds. Adding a new
// per-run CLI flag requires updating this list, which signals that a
// corresponding TUI setting must also be added.
var TUIParityFlags = []string{
	"repetitions",        // corresponds to SettingRepetitions
	"catalog-folder",     // corresponds to SettingCatalog
	"report-path",        // corresponds to SettingReportPath
	"max-concurrent-runs", // corresponds to SettingMaxConcurrentRuns
}

// TUIParityBoolFlags lists every bool-flag (a flag that takes no value)
// that configures a per-run setting for which the TUI must have an
// equivalent SettingKind on the settings screen.
//
// Parity rule: every entry here must appear in boolFlags, and each must
// correspond to a SettingKind in tui.CLIParitySettingKinds. Adding a new
// per-run bool flag requires updating this list, which signals that a
// corresponding TUI setting must also be added.
var TUIParityBoolFlags = []string{
	"keep-sandbox",            // corresponds to SettingRetention (RetainAlways)
	"keep-sandbox-on-failure", // corresponds to SettingRetention (RetainOnFailure)
}
