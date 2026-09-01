package tui

// defaults.go defines the types that carry suite-level declared defaults and
// the provenance vocabulary that labels where a displayed value came from.
// These types support Stage 10's requirement that the repetitions setting
// displays the suite file's actual authored default (a number, not a generic
// label) and marks whether that number is a suite default or a user override.

// SuiteDefaults holds the declared defaults from a suite file, resolved
// before run start so the TUI can show actual values.
type SuiteDefaults struct {
	// Repetitions is the number of times each test runs by default, as
	// declared in the suite file. Nil means the suite declares no default
	// and the tool's own built-in default applies.
	Repetitions *int
}

// SettingProvenance names the source of a displayed setting value.
type SettingProvenance int

const (
	// ProvenanceSuiteDefault means the value comes from the suite file's
	// declared default, resolved via Options.ResolveSuiteDefaults.
	ProvenanceSuiteDefault SettingProvenance = iota

	// ProvenanceUserOverride means the user has explicitly changed the
	// value from the default (via the TUI settings screen or a CLI flag).
	ProvenanceUserOverride

	// ProvenanceUnknown means the suite's defaults could not be resolved
	// (the resolver returned an error or is nil), so whether the displayed
	// value is a default or a user override cannot be determined.
	ProvenanceUnknown
)
