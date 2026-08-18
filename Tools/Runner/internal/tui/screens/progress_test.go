package screens

// progress_test.go verifies the per-row style classification of ProgressScreen.
//
// Each Communication Protocol status code must resolve to a specific lipgloss
// role: BLOCKED and CAPABILITY_EXCEEDED take the Error role; SUCCESS takes
// Success; the running row keeps Selected with its ▶ indicator; all remaining
// recognised advisory codes (COMPLETED_NEEDS_ACTION, PARTIALLY_DONE,
// NEEDS_CLARIFICATION) and any unrecognised non-empty string take Warning.
//
// RED phase: the two Error-branch tests (BLOCKED, CAPABILITY_EXCEEDED) are
// intentionally failing until the implementation splits progress.go's catch-all
// branch (I10.1). All other tests are regression guards that already pass and
// must continue to pass.

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// TestMain forces a fixed ANSI color profile for the entire test binary before
// any test runs. Without this, lipgloss auto-detects the environment, which
// produces Ascii (plain-text) output in non-terminal contexts such as CI.
// Under Ascii every role renders identically, making style assertions useless.
// Forcing ANSI ensures each distinct Foreground color produces a unique escape
// sequence, so Error, Warning, and Success renders are byte-distinguishable.
func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.ANSI)
	os.Exit(m.Run())
}

// progressStyles returns a Styles value representative of DefaultTheme where
// every role carries a distinct foreground color (and Bold where DefaultTheme
// applies it). Tests use this helper so the styles passed to the screen are
// the same objects used to build expectations — the assertions never hard-code
// ANSI escape sequences and remain resilient to lipgloss version changes.
func progressStyles() Styles {
	return Styles{
		Title:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12")),
		Subtitle: lipgloss.NewStyle().Foreground(lipgloss.Color("14")),
		Body:     lipgloss.NewStyle().Foreground(lipgloss.Color("15")),
		Muted:    lipgloss.NewStyle().Foreground(lipgloss.Color("8")),
		Selected: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10")),
		Checked:  lipgloss.NewStyle().Foreground(lipgloss.Color("10")),
		Success:  lipgloss.NewStyle().Foreground(lipgloss.Color("10")),
		Warning:  lipgloss.NewStyle().Foreground(lipgloss.Color("11")),
		Error:    lipgloss.NewStyle().Foreground(lipgloss.Color("9")),
		Help:     lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14")),
		Border:   lipgloss.NewStyle().Foreground(lipgloss.Color("8")),
	}
}

// extractFirstRowLine renders a ProgressScreen configured with the given
// styles (80×24), appends one row for "agent#1" at phase "run", completes
// it with the supplied status, and returns the single rendered row line.
//
// View() structure (parts joined with "\n"):
//
//	[0] title
//	[1] statusLine  (empty spaces — SetStatus is not called)
//	[2] border
//	[3] first (and only) row line       ← returned here
//	[4] border
//	[5] help
func extractFirstRowLine(styles Styles, status string) string {
	s := NewProgressScreen(80, 24, styles)
	s.AppendRow(ProgressRow{AgentInstance: "agent#1", Phase: "run"})
	s.CompleteRow(status)
	return firstRowLine(s)
}

// extractRunningRowLine renders a ProgressScreen with one row appended but
// not yet completed (runningIdx == 0) and returns the rendered row line.
func extractRunningRowLine(styles Styles) string {
	s := NewProgressScreen(80, 24, styles)
	s.AppendRow(ProgressRow{AgentInstance: "agent#1", Phase: "run"})
	// Do not call CompleteRow — the row remains the current running row.
	return firstRowLine(s)
}

// firstRowLine splits View() output on newlines and returns the line at index 3,
// which is the first row line in the standard view layout.
func firstRowLine(s *ProgressScreen) string {
	lines := strings.Split(s.View(), "\n")
	if len(lines) < 4 {
		return ""
	}
	return lines[3]
}

// completedLabel returns the label text used for a completed (non-running,
// non-empty) row in progress.go: "<AgentInstance>  <Status>".
func completedLabel(agentInstance, status string) string {
	return fmt.Sprintf("%s  %s", agentInstance, status)
}

// ---------------------------------------------------------------------------
// Error branch — BLOCKED and CAPABILITY_EXCEEDED
//
// These tests are RED with the current implementation, which routes every
// non-SUCCESS, non-empty, non-running status through the catch-all Warning
// branch. After I10.1 splits the catch-all, both statuses must render in the
// Error style.
// ---------------------------------------------------------------------------

// TestProgressRow_Blocked_RendersInErrorStyle asserts that a BLOCKED row is
// drawn in the Error (red) style, not the Warning style.
//
// RED: the current catch-all routes BLOCKED to Warning.
func TestProgressRow_Blocked_RendersInErrorStyle(t *testing.T) {
	styles := progressStyles()
	label := completedLabel("agent#1", "BLOCKED")
	got := extractFirstRowLine(styles, "BLOCKED")

	wantError := "  " + styles.Error.Render(label)
	wantWarning := "  " + styles.Warning.Render(label)

	if got != wantError {
		t.Errorf("BLOCKED row rendered in wrong style:\n  got  %q\n  want %q (Error-styled)", got, wantError)
	}
	if got == wantWarning {
		t.Errorf("BLOCKED row must not render in Warning style; BLOCKED is a terminal failure, not an advisory outcome")
	}
}

// TestProgressRow_CapabilityExceeded_RendersInErrorStyle asserts that a
// CAPABILITY_EXCEEDED row is drawn in the Error style, not the Warning style.
//
// RED: the current catch-all routes CAPABILITY_EXCEEDED to Warning.
func TestProgressRow_CapabilityExceeded_RendersInErrorStyle(t *testing.T) {
	styles := progressStyles()
	label := completedLabel("agent#1", "CAPABILITY_EXCEEDED")
	got := extractFirstRowLine(styles, "CAPABILITY_EXCEEDED")

	wantError := "  " + styles.Error.Render(label)
	wantWarning := "  " + styles.Warning.Render(label)

	if got != wantError {
		t.Errorf("CAPABILITY_EXCEEDED row rendered in wrong style:\n  got  %q\n  want %q (Error-styled)", got, wantError)
	}
	if got == wantWarning {
		t.Errorf("CAPABILITY_EXCEEDED row must not render in Warning style; it is a terminal failure, not an advisory outcome")
	}
}

// ---------------------------------------------------------------------------
// Warning branch — advisory non-SUCCESS protocol statuses
//
// These tests are regression guards. The current catch-all already routes
// these statuses to Warning and the split implementation must preserve that.
// ---------------------------------------------------------------------------

// TestProgressRow_CompletedNeedsAction_RendersInWarningStyle asserts that a
// COMPLETED_NEEDS_ACTION row renders in the Warning style.
func TestProgressRow_CompletedNeedsAction_RendersInWarningStyle(t *testing.T) {
	styles := progressStyles()
	label := completedLabel("agent#1", "COMPLETED_NEEDS_ACTION")
	got := extractFirstRowLine(styles, "COMPLETED_NEEDS_ACTION")

	want := "  " + styles.Warning.Render(label)
	if got != want {
		t.Errorf("COMPLETED_NEEDS_ACTION row rendered in wrong style:\n  got  %q\n  want %q (Warning-styled)", got, want)
	}
}

// TestProgressRow_PartiallyDone_RendersInWarningStyle asserts that a
// PARTIALLY_DONE row renders in the Warning style.
func TestProgressRow_PartiallyDone_RendersInWarningStyle(t *testing.T) {
	styles := progressStyles()
	label := completedLabel("agent#1", "PARTIALLY_DONE")
	got := extractFirstRowLine(styles, "PARTIALLY_DONE")

	want := "  " + styles.Warning.Render(label)
	if got != want {
		t.Errorf("PARTIALLY_DONE row rendered in wrong style:\n  got  %q\n  want %q (Warning-styled)", got, want)
	}
}

// TestProgressRow_NeedsClarification_RendersInWarningStyle asserts that a
// NEEDS_CLARIFICATION row renders in the Warning style.
func TestProgressRow_NeedsClarification_RendersInWarningStyle(t *testing.T) {
	styles := progressStyles()
	label := completedLabel("agent#1", "NEEDS_CLARIFICATION")
	got := extractFirstRowLine(styles, "NEEDS_CLARIFICATION")

	want := "  " + styles.Warning.Render(label)
	if got != want {
		t.Errorf("NEEDS_CLARIFICATION row rendered in wrong style:\n  got  %q\n  want %q (Warning-styled)", got, want)
	}
}

// ---------------------------------------------------------------------------
// Unrecognised status — must not render as Success
//
// Design decision: an unknown status string is treated as Warning, never
// Success. An unrecognised code must not flatter a potential failure with a
// green indicator.
// ---------------------------------------------------------------------------

// TestProgressRow_UnrecognizedStatus_RendersInWarningNotSuccess asserts that
// a status string not in the known Communication Protocol set renders in the
// Warning style and explicitly does not render in the Success style.
func TestProgressRow_UnrecognizedStatus_RendersInWarningNotSuccess(t *testing.T) {
	styles := progressStyles()
	const unknown = "UNKNOWN_STATUS"
	label := completedLabel("agent#1", unknown)
	got := extractFirstRowLine(styles, unknown)

	wantWarning := "  " + styles.Warning.Render(label)
	wantSuccess := "  " + styles.Success.Render(label)

	if got != wantWarning {
		t.Errorf("unrecognized status row rendered in wrong style:\n  got  %q\n  want %q (Warning-styled)", got, wantWarning)
	}
	if got == wantSuccess {
		t.Errorf("unrecognized status must not render in Success style; unknown codes are not successes")
	}
}

// ---------------------------------------------------------------------------
// Regression guards for already-correct branches
// ---------------------------------------------------------------------------

// TestProgressRow_Success_RendersInSuccessStyle asserts that a SUCCESS row
// renders in the Success style. This is an existing-behavior regression guard.
func TestProgressRow_Success_RendersInSuccessStyle(t *testing.T) {
	styles := progressStyles()
	label := completedLabel("agent#1", "SUCCESS")
	got := extractFirstRowLine(styles, "SUCCESS")

	want := "  " + styles.Success.Render(label)
	if got != want {
		t.Errorf("SUCCESS row rendered in wrong style:\n  got  %q\n  want %q (Success-styled)", got, want)
	}
}

// TestProgressRow_Running_HasIndicatorAndSelectedStyle asserts that the
// currently-running row displays the ▶ indicator and renders its label in the
// Selected style. The label uses the "phase=<Phase>" format (not the status
// format) consistent with the existing running branch in progress.go.
func TestProgressRow_Running_HasIndicatorAndSelectedStyle(t *testing.T) {
	styles := progressStyles()
	label := fmt.Sprintf("agent#1  phase=%s", "run")
	got := extractRunningRowLine(styles)

	wantLine := "▶ " + styles.Selected.Render(label)

	if got != wantLine {
		t.Errorf("running row rendered incorrectly:\n  got  %q\n  want %q (Selected-styled with ▶ indicator)", got, wantLine)
	}
	if !strings.HasPrefix(got, "▶ ") {
		t.Errorf("running row missing ▶ indicator: got %q", got)
	}
}
