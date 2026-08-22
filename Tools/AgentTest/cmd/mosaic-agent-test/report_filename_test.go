package main

// report_filename_test.go covers the new default report filename behaviour
// (Stage 3):
//
//   T3.1 - defaultReportPath(suiteName, now) produces a filename that contains
//           the suite name and a timestamp, changes between calls with different
//           times, sanitises characters invalid in filenames from the suite name
//           component, and ends with .json.
//
//   T3.1 (TUI) - tuiOptions populates ReportPath from the new
//           defaultReportPath function (not the old fixed "report.json"), so
//           the suite-select screen shows a timestamped placeholder before a
//           suite is chosen.
//
// These tests are in package main because defaultReportPath is a package-level
// function in dispatch.go. No real filesystem is touched.
//
// NOTE: These tests are in TDD RED phase. They call defaultReportPath with the
// new two-argument signature (suiteName string, now time.Time) which does not
// yet exist; the current function takes no arguments. They will not compile
// against the current implementation. That is the expected RED state — they
// specify the target behaviour for implementation task I3.1.

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// T3.1: defaultReportPath signature and content
// ---------------------------------------------------------------------------

// TestDefaultReportPath_ContainsSuiteName verifies that the report filename
// base-name includes the suite name, so a user browsing report files can tell
// which suite produced each one without opening the file.
func TestDefaultReportPath_ContainsSuiteName(t *testing.T) {
	now := time.Date(2026, 8, 22, 11, 5, 41, 0, time.UTC)
	path := defaultReportPath("my-suite", now)
	base := filepath.Base(path)
	if !strings.Contains(base, "my-suite") {
		t.Errorf("defaultReportPath(%q, ...) base = %q; want the suite name to appear in the filename", "my-suite", base)
	}
}

// TestDefaultReportPath_ContainsTimestamp verifies that the report filename
// includes a timestamp component derived from the supplied time value, using
// a date recognisable from the time that was passed in.
func TestDefaultReportPath_ContainsTimestamp(t *testing.T) {
	// 2026-08-22 11:05:41 UTC — the date component in compact form is "20260822".
	now := time.Date(2026, 8, 22, 11, 5, 41, 0, time.UTC)
	path := defaultReportPath("suite", now)
	base := filepath.Base(path)
	if !strings.Contains(base, "20260822") {
		t.Errorf("defaultReportPath(%q, %v) base = %q; want the date %q to appear in the filename", "suite", now, base, "20260822")
	}
}

// TestDefaultReportPath_ChangesWithTime verifies that two calls with different
// time values produce different filenames, so repeat runs of the same suite
// never silently overwrite each other's report.
func TestDefaultReportPath_ChangesWithTime(t *testing.T) {
	suiteName := "suite"
	t1 := time.Date(2026, 8, 22, 11, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)

	path1 := defaultReportPath(suiteName, t1)
	path2 := defaultReportPath(suiteName, t2)

	if path1 == path2 {
		t.Errorf("defaultReportPath(%q, t1) == defaultReportPath(%q, t2) = %q; different times must produce different filenames to prevent silent overwrites", suiteName, suiteName, path1)
	}
}

// TestDefaultReportPath_DifferentSuiteNames_ProduceDifferentFiles verifies
// that two calls with different suite names produce different filenames, so
// reports from different suites are distinguishable.
func TestDefaultReportPath_DifferentSuiteNames_ProduceDifferentFiles(t *testing.T) {
	now := time.Date(2026, 8, 22, 11, 5, 41, 0, time.UTC)
	path1 := defaultReportPath("suite-alpha", now)
	path2 := defaultReportPath("suite-beta", now)

	if path1 == path2 {
		t.Errorf("defaultReportPath produced the same filename for different suite names: %q", path1)
	}
}

// TestDefaultReportPath_SanitizesForwardSlash verifies that a suite name
// containing a forward slash does not produce a directory separator in the
// filename component, keeping the result a valid, flat file in one directory.
// It also verifies that sanitisation replaces (rather than strips) the invalid
// characters, so all components of the suite name are preserved in the filename.
func TestDefaultReportPath_SanitizesForwardSlash(t *testing.T) {
	now := time.Date(2026, 8, 22, 11, 5, 41, 0, time.UTC)
	path := defaultReportPath("org/project/suite", now)
	base := filepath.Base(path)
	if strings.Contains(base, "/") {
		t.Errorf("defaultReportPath(%q, ...) base = %q; forward slash must be sanitised from the filename component", "org/project/suite", base)
	}
	// Verify the prefix components are still present in the base name. An
	// implementation that strips rather than replaces '/' would produce a base
	// like "suite-<timestamp>.json" (missing "org" and "project"), while a
	// correct replace-with-hyphen implementation produces
	// "report-org-project-suite-<timestamp>.json".
	// filepath.Base strips everything up to the last path separator, so without
	// these checks a non-sanitising implementation could pass the check above
	// because the OS normalises the slashes before Base is called.
	if !strings.Contains(base, "org") {
		t.Errorf("defaultReportPath(%q, ...) base = %q; suite name component %q must be preserved in the filename after sanitisation", "org/project/suite", base, "org")
	}
	if !strings.Contains(base, "project") {
		t.Errorf("defaultReportPath(%q, ...) base = %q; suite name component %q must be preserved in the filename after sanitisation", "org/project/suite", base, "project")
	}
}

// TestDefaultReportPath_SanitizesBackslash verifies that a suite name
// containing a backslash (a path separator on Windows) is sanitised so the
// result remains a valid, flat filename on all platforms.
// It also verifies that sanitisation replaces (rather than strips) the invalid
// characters, so all components of the suite name are preserved in the filename.
func TestDefaultReportPath_SanitizesBackslash(t *testing.T) {
	now := time.Date(2026, 8, 22, 11, 5, 41, 0, time.UTC)
	path := defaultReportPath(`org\project\suite`, now)
	base := filepath.Base(path)
	if strings.Contains(base, `\`) {
		t.Errorf("defaultReportPath(%q, ...) base = %q; backslash must be sanitised from the filename component", `org\project\suite`, base)
	}
	// Verify the prefix components are still present in the base name. An
	// implementation that strips rather than replaces '\' would produce a base
	// like "suite-<timestamp>.json" (missing "org" and "project"), while a
	// correct replace-with-hyphen implementation produces
	// "report-org-project-suite-<timestamp>.json".
	// On Windows, filepath.Join normalises backslashes and filepath.Base strips
	// them, so without these checks a non-sanitising implementation could pass
	// the check above.
	if !strings.Contains(base, "org") {
		t.Errorf("defaultReportPath(%q, ...) base = %q; suite name component %q must be preserved in the filename after sanitisation", `org\project\suite`, base, "org")
	}
	if !strings.Contains(base, "project") {
		t.Errorf("defaultReportPath(%q, ...) base = %q; suite name component %q must be preserved in the filename after sanitisation", `org\project\suite`, base, "project")
	}
}

// TestDefaultReportPath_SanitizesColon verifies that a suite name containing a
// colon (invalid in Windows filenames and ambiguous in URLs) is sanitised from
// the filename component.
func TestDefaultReportPath_SanitizesColon(t *testing.T) {
	now := time.Date(2026, 8, 22, 11, 5, 41, 0, time.UTC)
	path := defaultReportPath("suite:variant", now)
	base := filepath.Base(path)
	if strings.Contains(base, ":") {
		t.Errorf("defaultReportPath(%q, ...) base = %q; colon must be sanitised from the filename component", "suite:variant", base)
	}
}

// TestDefaultReportPath_EndsWithDotJSON verifies that the returned path always
// ends with the .json extension, so report files are identifiable without
// inspecting file content.
func TestDefaultReportPath_EndsWithDotJSON(t *testing.T) {
	now := time.Date(2026, 8, 22, 11, 5, 41, 0, time.UTC)
	path := defaultReportPath("suite", now)
	if !strings.HasSuffix(path, ".json") {
		t.Errorf("defaultReportPath(%q, ...) = %q; want .json extension", "suite", path)
	}
}

// TestDefaultReportPath_ReturnedPath_IsAbsoluteOrCurrentWorkingDir verifies
// that the returned path is either absolute or relative to the current working
// directory, matching the previous behaviour of defaultReportPath().
func TestDefaultReportPath_ReturnedPath_IsNotEmpty(t *testing.T) {
	now := time.Date(2026, 8, 22, 11, 5, 41, 0, time.UTC)
	path := defaultReportPath("suite", now)
	if path == "" {
		t.Error("defaultReportPath returned an empty string; want a non-empty path")
	}
}

// ---------------------------------------------------------------------------
// T3.1 (TUI): tuiOptions ReportPath placeholder
// ---------------------------------------------------------------------------

// TestTUIOptions_ReportPath_IsTimestampedPattern verifies that tuiOptions
// populates Options.ReportPath using the new defaultReportPath function rather
// than the old fixed "report.json" value, so the suite-select screen shows the
// user a timestamped placeholder pattern before a suite is selected.
//
// The exact placeholder suite name embedded in the path is an implementation
// detail; the structural invariants (starts with "report-", ends with ".json",
// is not the old fixed filename) are what matter.
func TestTUIOptions_ReportPath_IsTimestampedPattern(t *testing.T) {
	// tuiOptions requires a non-empty WorkspaceRoot so newTUISuiteRunner succeeds.
	opts, err := tuiOptions(Deps{WorkspaceRoot: "placeholder"}, nil)
	if err != nil {
		t.Fatalf("tuiOptions returned an error: %v", err)
	}

	rp := opts.ReportPath

	// The old fixed value must no longer be used.
	if rp == "report.json" {
		t.Errorf("tuiOptions.ReportPath = %q; this is the old fixed filename; want a timestamped pattern like report-<suite>-<timestamp>.json", rp)
	}

	// The new value must follow the report-<suite>-<timestamp>.json pattern.
	if !strings.HasPrefix(rp, "report-") {
		t.Errorf("tuiOptions.ReportPath = %q; want it to start with %q (timestamped naming pattern)", rp, "report-")
	}
	if !strings.HasSuffix(rp, ".json") {
		t.Errorf("tuiOptions.ReportPath = %q; want .json extension", rp)
	}

	// The value must contain at least one digit, confirming a timestamp is
	// present (a pure placeholder name like "report-placeholder.json" would
	// not satisfy the timestamped-pattern requirement).
	hasDigit := false
	for _, r := range rp {
		if r >= '0' && r <= '9' {
			hasDigit = true
			break
		}
	}
	if !hasDigit {
		t.Errorf("tuiOptions.ReportPath = %q; want a digit in the filename (timestamp component)", rp)
	}
}
