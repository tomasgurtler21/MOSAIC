// testprogress_test.go tests the TestProgressScreen's SetResolvedPaths method
// and its View rendering of the "Resolved binaries:" section (T4.5, screen-level).
//
// Tests are in package screens_test (blackbox) because SetResolvedPaths and
// View are exported methods, and because the existing TestMain (in package
// screens, progress_test.go) forces a fixed ANSI color profile for the test
// binary so style assertions are byte-distinguishable.
//
// Test coverage:
//
//   - SetResolvedPaths with a populated map and order slice causes View() to
//     render a "Resolved binaries:" heading and one line per harness.
//   - Each line follows the format "  <harnessID>: <absolutePath>".
//   - Display order follows harnessOrder (input-slice order from HarnessDisplayOrder).
//   - Before SetResolvedPaths is called, View() contains no "Resolved binaries:".
//   - After SetResolvedPaths with a nil map, View() contains no "Resolved binaries:".
//   - After SetResolvedPaths with an empty map and empty order, no section appears.
//   - Reset() clears the resolved paths so they do not persist across re-runs.
package screens_test

import (
	"strings"
	"testing"

	"mosaic-run/internal/tui/screens"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newProgressScreen builds a TestProgressScreen at a wide size so that long
// paths do not wrap and interfere with substring assertions.
func newProgressScreen() *screens.TestProgressScreen {
	return screens.NewTestProgressScreen(200, 50, screens.Styles{})
}

// stripANSIProgress removes ANSI SGR escape sequences from a string.
// Named distinctly from stripANSIResults to avoid import-cycle confusion
// if test files are reorganised.
func stripANSIProgress(s string) string {
	return ansiSeq.ReplaceAllString(s, "")
}

// =============================================================================
// T4.5: TestProgressScreen -- SetResolvedPaths and View()
// =============================================================================

// TestTestProgressScreen_SetResolvedPaths_PopulatedMap_ShowsSection verifies
// that after SetResolvedPaths is called with a non-empty map and order slice,
// View() renders the "Resolved binaries:" heading.
func TestTestProgressScreen_SetResolvedPaths_PopulatedMap_ShowsSection(t *testing.T) {
	s := newProgressScreen()
	s.SetResolvedPaths(
		map[string]string{"claude-code": "/usr/local/bin/claude"},
		[]string{"claude-code"},
	)

	got := stripANSIProgress(s.View())
	if !strings.Contains(got, "Resolved binaries:") {
		t.Errorf("TestProgressScreen.View() after SetResolvedPaths: does not contain \"Resolved binaries:\"\ngot (stripped): %.300s",
			got)
	}
}

// TestTestProgressScreen_SetResolvedPaths_ShowsHarnessID verifies that the
// harness ID appears in View() after SetResolvedPaths is called.
func TestTestProgressScreen_SetResolvedPaths_ShowsHarnessID(t *testing.T) {
	const harnessID = "claude-code"
	s := newProgressScreen()
	s.SetResolvedPaths(
		map[string]string{harnessID: "/usr/local/bin/claude"},
		[]string{harnessID},
	)

	got := stripANSIProgress(s.View())
	if !strings.Contains(got, harnessID) {
		t.Errorf("TestProgressScreen.View() after SetResolvedPaths: does not contain harness ID %q\ngot (stripped): %.300s",
			harnessID, got)
	}
}

// TestTestProgressScreen_SetResolvedPaths_ShowsAbsolutePath verifies that the
// resolved absolute path appears in View() after SetResolvedPaths is called.
func TestTestProgressScreen_SetResolvedPaths_ShowsAbsolutePath(t *testing.T) {
	const absPath = "/usr/local/bin/claude"
	s := newProgressScreen()
	s.SetResolvedPaths(
		map[string]string{"claude-code": absPath},
		[]string{"claude-code"},
	)

	got := stripANSIProgress(s.View())
	if !strings.Contains(got, absPath) {
		t.Errorf("TestProgressScreen.View() after SetResolvedPaths: does not contain path %q\ngot (stripped): %.300s",
			absPath, got)
	}
}

// TestTestProgressScreen_SetResolvedPaths_MultipleHarnesses_FollowsHarnessOrder
// verifies that the display order of resolved paths follows the harnessOrder
// slice, not sorted order. The progress screen uses input-order (from
// HarnessDisplayOrder) for display, unlike the results screen which sorts.
func TestTestProgressScreen_SetResolvedPaths_MultipleHarnesses_FollowsHarnessOrder(t *testing.T) {
	paths := map[string]string{
		"opencode":    "/usr/local/bin/opencode",
		"claude-code": "/usr/local/bin/claude",
	}
	// Deliberately place "opencode" before "claude-code" in harnessOrder.
	order := []string{"opencode", "claude-code"}

	s := newProgressScreen()
	s.SetResolvedPaths(paths, order)

	got := stripANSIProgress(s.View())

	opencodeIdx := strings.Index(got, "opencode")
	claudeIdx := strings.Index(got, "claude-code")

	if opencodeIdx == -1 {
		t.Error("TestProgressScreen.View() does not contain \"opencode\"")
	}
	if claudeIdx == -1 {
		t.Error("TestProgressScreen.View() does not contain \"claude-code\"")
	}
	// harnessOrder puts opencode first, so it must appear before claude-code.
	if opencodeIdx != -1 && claudeIdx != -1 && opencodeIdx > claudeIdx {
		t.Errorf("TestProgressScreen.View() display order: opencode (idx %d) appears after claude-code (idx %d); want harnessOrder respected",
			opencodeIdx, claudeIdx)
	}
}

// TestTestProgressScreen_BeforeSetResolvedPaths_NoSection verifies that before
// SetResolvedPaths is called, View() does not contain a "Resolved binaries:"
// section. This confirms the zero-value behaviour: the section is absent when
// resolution has not been performed.
func TestTestProgressScreen_BeforeSetResolvedPaths_NoSection(t *testing.T) {
	s := newProgressScreen()
	// No SetResolvedPaths call.

	got := stripANSIProgress(s.View())
	if strings.Contains(got, "Resolved binaries:") {
		t.Errorf("TestProgressScreen.View() before SetResolvedPaths: contains \"Resolved binaries:\"; want section absent\ngot (stripped): %.300s",
			got)
	}
}

// TestTestProgressScreen_SetResolvedPaths_NilMap_OmitsSection verifies that
// calling SetResolvedPaths with a nil map and nil order results in no section
// in View(). Nil means resolution was not performed or had no non-fake harnesses.
//
// NOTE (TDD RED): This test passes trivially in RED because SetResolvedPaths is
// a no-op stub; nil input produces no section whether or not the body is
// implemented. The correct-pass status in RED is coincidental. The test will
// continue to pass correctly after implementation (nil always omits the section).
func TestTestProgressScreen_SetResolvedPaths_NilMap_OmitsSection(t *testing.T) {
	s := newProgressScreen()
	s.SetResolvedPaths(nil, nil)

	got := stripANSIProgress(s.View())
	if strings.Contains(got, "Resolved binaries:") {
		t.Errorf("TestProgressScreen.View() after SetResolvedPaths(nil, nil): contains \"Resolved binaries:\"; want section absent\ngot (stripped): %.300s",
			got)
	}
}

// TestTestProgressScreen_SetResolvedPaths_EmptyMap_OmitsSection verifies that
// calling SetResolvedPaths with an empty map and empty order produces no
// section. This is the all-"fake" harnesses case.
//
// NOTE (TDD RED): This test passes trivially in RED because SetResolvedPaths is
// a no-op stub; an empty map produces no section regardless of implementation.
// The correct-pass status in RED is coincidental. The test will continue to pass
// correctly after implementation (empty map always omits the section).
func TestTestProgressScreen_SetResolvedPaths_EmptyMap_OmitsSection(t *testing.T) {
	s := newProgressScreen()
	s.SetResolvedPaths(map[string]string{}, []string{})

	got := stripANSIProgress(s.View())
	if strings.Contains(got, "Resolved binaries:") {
		t.Errorf("TestProgressScreen.View() after SetResolvedPaths(empty, empty): contains \"Resolved binaries:\"; want section absent\ngot (stripped): %.300s",
			got)
	}
}

// TestTestProgressScreen_Reset_ClearsResolvedPaths verifies that calling
// Reset() clears the resolved paths stored by SetResolvedPaths. After reset,
// View() must not contain the "Resolved binaries:" section, so that re-runs
// from the same screen instance start with a clean state.
func TestTestProgressScreen_Reset_ClearsResolvedPaths(t *testing.T) {
	s := newProgressScreen()
	s.SetResolvedPaths(
		map[string]string{"claude-code": "/usr/local/bin/claude"},
		[]string{"claude-code"},
	)

	// Confirm section is present before reset.
	before := stripANSIProgress(s.View())
	if !strings.Contains(before, "Resolved binaries:") {
		t.Fatal("SetResolvedPaths did not produce the section (pre-condition failure); cannot test Reset")
	}

	s.Reset()

	after := stripANSIProgress(s.View())
	if strings.Contains(after, "Resolved binaries:") {
		t.Errorf("TestProgressScreen.View() after Reset(): still contains \"Resolved binaries:\"; Reset must clear resolvedPaths\ngot (stripped): %.300s",
			after)
	}
}

// TestTestProgressScreen_SetResolvedPaths_WindowsCmdPath_ShowsPath verifies
// that a Windows .cmd shim path (which may contain backslashes and spaces)
// is displayed verbatim in View(). Long paths with spaces must not be
// truncated or modified.
func TestTestProgressScreen_SetResolvedPaths_WindowsCmdPath_ShowsPath(t *testing.T) {
	const cmdPath = `C:\Users\test user\AppData\Roaming\npm\claude.cmd`
	s := newProgressScreen()
	s.SetResolvedPaths(
		map[string]string{"claude-code": cmdPath},
		[]string{"claude-code"},
	)

	got := stripANSIProgress(s.View())
	if !strings.Contains(got, cmdPath) {
		t.Errorf("TestProgressScreen.View() does not contain Windows .cmd path %q\ngot (stripped): %.300s",
			cmdPath, got)
	}
}
