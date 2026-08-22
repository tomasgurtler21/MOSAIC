package tui

// settings.go defines the SettingsEntry abstraction that backs every per-run
// configuration setting on the settings screen. One type, one cursor, one
// action key drives all four settings regardless of their edit mode.

// SettingKind identifies one per-run configuration setting.
type SettingKind string

const (
	SettingRetention   SettingKind = "retention"
	SettingRepetitions SettingKind = "repetitions"
	SettingReportPath  SettingKind = "report_path"
	SettingCatalog     SettingKind = "catalog"
)

// SettingEditMode selects the interaction affordance for a SettingsEntry.
type SettingEditMode int

const (
	// EditCycle: pressing the action key cycles through a fixed set of values
	// (e.g., retention: Never → OnFailure → Always).
	EditCycle SettingEditMode = iota

	// EditInline: pressing the action key opens an inline text editor for the
	// value. Enter confirms the draft; Esc cancels and restores the original.
	EditInline

	// EditNumeric: pressing the action key opens an inline numeric editor.
	// Only digit characters are accepted. Enter confirms; Esc cancels.
	EditNumeric
)

// SettingsEntry is one configurable per-run setting. Every entry is editable
// by construction: the EditMode field selects the interaction affordance, and
// every SettingKind has exactly one EditMode.
type SettingsEntry struct {
	Kind     SettingKind
	Label    string          // display label, e.g. "Retain sandbox"
	Display  string          // current rendered value
	EditMode SettingEditMode // how the user changes this value
}
