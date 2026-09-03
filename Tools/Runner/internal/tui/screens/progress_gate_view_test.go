package screens

// progress_gate_view_test.go verifies how the stop-confirmation gate presents
// itself: the pending prompt renders as its own delimited banner rather than as
// one more bare line in the agent-row stack, and each resolution leaves an
// acknowledgement behind so neither outcome is silent.
//
// Both properties exist because the gate was previously easy to miss on a
// screen that repaints every second, and cancelling it produced no feedback at
// all.

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

const confirmPromptText = "Stop after current step?"

// gateScreenWidth is the width every screen in this file is constructed at, so
// width assertions and the fixture cannot drift apart.
const gateScreenWidth = 80

// ansiPattern matches SGR escape sequences so assertions can inspect the
// printable text of a rendered line regardless of the active colour profile.
var ansiPattern = regexp.MustCompile("\x1b\\[[0-9;]*m")

// stripANSI removes SGR escape sequences from a rendered line.
func stripANSI(s string) string {
	return ansiPattern.ReplaceAllString(s, "")
}

// verticalBoxRunes are the vertical box-drawing characters a bordered box may
// use for its left and right edges. Nothing else the progress screen renders
// produces one of these.
const verticalBoxRunes = "│┃║╎╏┆┇┊┋"

// isVerticalBoxRune reports whether r delimits the side of a box.
func isVerticalBoxRune(r rune) bool {
	return strings.ContainsRune(verticalBoxRunes, r)
}

// pendingGateScreen returns a progress screen with a populated row stack and
// the confirmation gate open. The rows matter: an empty row stack lets weaker
// delimitation assertions pass vacuously against an undelimited prompt.
func pendingGateScreen(t *testing.T) (*ProgressScreen, string) {
	t.Helper()

	const agentRow = "planner-agent#1"
	s := NewProgressScreen(gateScreenWidth, 24, progressStyles())
	s.AppendRow(ProgressRow{AgentInstance: agentRow, Phase: "PLANNING", Status: "running"})

	pressProgressKey(s, "s")
	if !s.ConfirmPending() {
		t.Fatalf("ConfirmPending() = false after 's'; the gate must be open for this test to mean anything")
	}
	return s, agentRow
}

// findLine returns the index of the first line of view containing substr.
func findLine(t *testing.T, view, substr string) int {
	t.Helper()

	for i, line := range strings.Split(view, "\n") {
		if strings.Contains(stripANSI(line), substr) {
			return i
		}
	}
	t.Fatalf("View() contains no line with %q:\n%s", substr, view)
	return -1
}

// TestProgressScreen_View_PendingPrompt_RendersInDelimitedBlock asserts that
// the pending prompt sits inside its own delimited block: the prompt line
// begins and ends with a vertical box-drawing character. Nothing else the
// screen renders produces one, so this cannot pass against a prompt appended as
// a bare line to the row stack.
func TestProgressScreen_View_PendingPrompt_RendersInDelimitedBlock(t *testing.T) {
	s, _ := pendingGateScreen(t)

	view := s.View()
	promptIdx := findLine(t, view, confirmPromptText)
	promptLine := strings.TrimSpace(stripANSI(strings.Split(view, "\n")[promptIdx]))

	runes := []rune(promptLine)
	if len(runes) < 2 {
		t.Fatalf("prompt line %q is too short to be delimited:\n%s", promptLine, view)
	}
	if !isVerticalBoxRune(runes[0]) {
		t.Errorf("prompt line starts with %q, want a vertical box-drawing character (the prompt must be a delimited banner, not a bare line):\n%s", string(runes[0]), view)
	}
	if !isVerticalBoxRune(runes[len(runes)-1]) {
		t.Errorf("prompt line ends with %q, want a vertical box-drawing character (the prompt must be a delimited banner, not a bare line):\n%s", string(runes[len(runes)-1]), view)
	}
}

// TestProgressScreen_View_PendingPrompt_KeepsAgentRowsVisible asserts that the
// banner is rendered in place: the agent rows stay on screen above it, with no
// screen switch and no overlay hiding them.
func TestProgressScreen_View_PendingPrompt_KeepsAgentRowsVisible(t *testing.T) {
	s, agentRow := pendingGateScreen(t)

	view := s.View()
	rowIdx := findLine(t, view, agentRow)
	promptIdx := findLine(t, view, confirmPromptText)

	if rowIdx >= promptIdx {
		t.Errorf("agent row is on line %d and the prompt on line %d; the rows must remain rendered above the banner:\n%s", rowIdx, promptIdx, view)
	}
}

// TestProgressScreen_View_PendingBanner_FitsScreenWidth asserts that the banner
// occupies exactly the screen width, borders included. A box whose body is
// styled to the full width renders two columns wider than every other line and
// wraps in a terminal of exactly that width, which destroys the visual
// distinctness the banner exists to provide.
func TestProgressScreen_View_PendingBanner_FitsScreenWidth(t *testing.T) {
	s, _ := pendingGateScreen(t)

	view := s.View()
	lines := strings.Split(view, "\n")
	promptIdx := findLine(t, view, confirmPromptText)

	if got := lipgloss.Width(lines[promptIdx]); got != gateScreenWidth {
		t.Errorf("banner prompt line width = %d, want %d (the banner must total the screen width, borders included, or it wraps):\n%s", got, gateScreenWidth, view)
	}
}

// TestProgressScreen_View_PendingBanner_PersistsAcrossRepaints asserts that the
// banner survives repainting. The screen repaints every second, so a prompt
// that renders only on the repaint following the keypress would be invisible in
// practice.
func TestProgressScreen_View_PendingBanner_PersistsAcrossRepaints(t *testing.T) {
	s, agentRow := pendingGateScreen(t)

	for repaint := 1; repaint <= 3; repaint++ {
		view := s.View()
		if !strings.Contains(stripANSI(view), confirmPromptText) {
			t.Fatalf("repaint %d does not show the confirmation prompt while the gate is pending:\n%s", repaint, view)
		}
		if !strings.Contains(stripANSI(view), agentRow) {
			t.Fatalf("repaint %d does not show the agent rows alongside the banner:\n%s", repaint, view)
		}
	}
}

// statusLineOf returns the printable text of the screen's status line, which is
// the second line of the view.
func statusLineOf(t *testing.T, s *ProgressScreen) string {
	t.Helper()

	lines := strings.Split(s.View(), "\n")
	if len(lines) < 2 {
		t.Fatalf("View() has fewer than two lines, so it has no status line:\n%s", s.View())
	}
	return strings.TrimSpace(stripANSI(lines[1]))
}

// rawStatusLineOf returns the screen's status line with its styling intact, so
// assertions can inspect how the line is rendered rather than only what it says.
func rawStatusLineOf(t *testing.T, s *ProgressScreen) string {
	t.Helper()

	lines := strings.Split(s.View(), "\n")
	if len(lines) < 2 {
		t.Fatalf("View() has fewer than two lines, so it has no status line:\n%s", s.View())
	}
	return lines[1]
}

// errorStyleMarker returns the escape sequence that distinguishes the Error
// style from the Body style under the active colour profile, or "" when the
// profile renders the two identically and the distinction is not observable in
// the output at all.
func errorStyleMarker() string {
	st := progressStyles()

	body := make(map[string]bool)
	for _, seq := range ansiPattern.FindAllString(st.Body.Render("x"), -1) {
		body[seq] = true
	}
	for _, seq := range ansiPattern.FindAllString(st.Error.Render("x"), -1) {
		if !body[seq] {
			return seq
		}
	}
	return ""
}

// TestProgressScreen_GateAcknowledgements_AreNotErrors asserts that neither
// resolution acknowledges itself in the error style. Both outcomes -- stopping
// and continuing -- are the user getting what they asked for, and an
// acknowledgement rendered as an error reads as a failure even though its
// wording says otherwise.
func TestProgressScreen_GateAcknowledgements_AreNotErrors(t *testing.T) {
	marker := errorStyleMarker()
	if marker == "" {
		t.Skip("Error and Body styles render identically under the active colour profile; the distinction is not observable in View()")
	}

	tests := []struct {
		name string
		key  string
	}{
		{"confirmed", "y"},
		{"cancelled", "n"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := pendingGateScreen(t)

			pressProgressKey(s, tc.key)

			raw := rawStatusLineOf(t, s)
			if strings.TrimSpace(stripANSI(raw)) == "" {
				t.Fatalf("status line is empty after %q resolved the gate; there is no acknowledgement to inspect", tc.key)
			}
			if strings.Contains(raw, marker) {
				t.Errorf("acknowledgement after %q is rendered in the error style; want a non-error status (the resolution is not a failure): %q", tc.key, raw)
			}
		})
	}
}

// TestProgressScreen_ConfirmedGate_EmitsAcknowledgement asserts that confirming
// the stop is acknowledged on screen rather than resolving silently.
func TestProgressScreen_ConfirmedGate_EmitsAcknowledgement(t *testing.T) {
	s, _ := pendingGateScreen(t)

	if before := statusLineOf(t, s); before != "" {
		t.Fatalf("status line = %q before the gate was resolved; want empty, or the assertion below proves nothing", before)
	}

	pressProgressKey(s, "y")

	if got := statusLineOf(t, s); got == "" {
		t.Error("status line is empty on the repaint following a confirmed stop; the confirmation must be acknowledged, not silent")
	}
}

// TestProgressScreen_CancelledGate_EmitsAcknowledgement asserts that cancelling
// is acknowledged too. Cancellation was previously indistinguishable from the
// prompt never having appeared: the line simply vanished on the next repaint.
func TestProgressScreen_CancelledGate_EmitsAcknowledgement(t *testing.T) {
	s, _ := pendingGateScreen(t)

	if before := statusLineOf(t, s); before != "" {
		t.Fatalf("status line = %q before the gate was resolved; want empty, or the assertion below proves nothing", before)
	}

	pressProgressKey(s, "n")

	if got := statusLineOf(t, s); got == "" {
		t.Error("status line is empty on the repaint following a cancelled stop; the cancellation must be acknowledged, not silent")
	}
}

// TestProgressScreen_Acknowledgements_DistinguishTheTwoOutcomes asserts that
// the two resolutions do not produce the same message. An acknowledgement that
// reads identically for "stopping" and "continuing" tells the user nothing.
func TestProgressScreen_Acknowledgements_DistinguishTheTwoOutcomes(t *testing.T) {
	confirmed, _ := pendingGateScreen(t)
	pressProgressKey(confirmed, "y")

	cancelled, _ := pendingGateScreen(t)
	pressProgressKey(cancelled, "n")

	confirmAck := statusLineOf(t, confirmed)
	cancelAck := statusLineOf(t, cancelled)

	if confirmAck == cancelAck {
		t.Errorf("confirming and cancelling both acknowledge with %q; the two outcomes must be distinguishable", confirmAck)
	}
}

// TestProgressScreen_Acknowledgement_PersistsAcrossRepaints asserts that the
// acknowledgement is not cleared by the next repaint. The screen applies no
// expiry to the status slot, so a status set once stands until something
// overwrites it.
func TestProgressScreen_Acknowledgement_PersistsAcrossRepaints(t *testing.T) {
	s, _ := pendingGateScreen(t)

	pressProgressKey(s, "y")

	first := statusLineOf(t, s)
	if first == "" {
		t.Fatal("status line is empty on the repaint following a confirmed stop; there is no acknowledgement to persist")
	}
	for repaint := 2; repaint <= 3; repaint++ {
		if got := statusLineOf(t, s); got != first {
			t.Errorf("status line on repaint %d = %q, want %q (the acknowledgement must persist until it is overwritten)", repaint, got, first)
		}
	}
}

// TestProgressScreen_ConfirmedGate_KeepsStopNoticeAlongsideAcknowledgement
// asserts that acknowledging the confirmation does not displace the persistent
// stop notice. That notice is the user's standing confirmation that the request
// was accepted, and it must survive the new acknowledgement.
func TestProgressScreen_ConfirmedGate_KeepsStopNoticeAlongsideAcknowledgement(t *testing.T) {
	s, _ := pendingGateScreen(t)

	pressProgressKey(s, "y")

	view := stripANSI(s.View())
	if !strings.Contains(view, "Stopping after current step completes") {
		t.Errorf("View() does not show the persistent stop notice once the stop was confirmed:\n%s", view)
	}
	if statusLineOf(t, s) == "" {
		t.Errorf("status line is empty once the stop was confirmed; the acknowledgement and the notice must both be present:\n%s", view)
	}
}
