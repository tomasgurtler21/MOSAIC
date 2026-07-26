package tui

// select_overlay_test.go contains TDD tests for the inlineSelectOne overlay's
// width-constrained rendering and skip-entry behaviour.
//
// Tests are written in the RED phase: they compile against the current source
// but fail until the corresponding implementation is complete.
//
// Coverage:
//   - Option labels and descriptions are rendered within the configured width,
//     accounting for cursor prefix and description indent respectively.
//   - A "None" entry is present in the option list when the question allows
//     skipping, and absent when it does not.
//   - Navigating to and selecting the "None" entry produces a SkippedOne answer
//     with no OptionID.
//   - Selecting a real option and pressing ESC behave exactly as before the
//     addition of the skip entry.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Test fixtures
// ---------------------------------------------------------------------------

// longLabel is a label long enough to overflow any narrow terminal width
// without a width constraint applied to rendering.
const longLabel = "Deploy to production environment with all default configuration values"

// longDescription is a description long enough to overflow a typical narrow
// terminal width without a width constraint applied to rendering.
const longDescription = "This option will use the current workspace directory and apply all " +
	"pending configuration overrides automatically without further confirmation."

// threeRealOptions returns three distinct real options used across skip-entry tests.
func threeRealOptions() []domain.Option {
	return []domain.Option{
		{ID: "opt-a", Label: "Option A", Description: "First option"},
		{ID: "opt-b", Label: "Option B", Description: "Second option"},
		{ID: "opt-c", Label: "Option C", Description: "Third option"},
	}
}

// longLabelOptions returns options whose labels and descriptions are
// intentionally long, exercising the width-constraint path in view().
func longLabelOptions() []domain.Option {
	return []domain.Option{
		{ID: "alpha", Label: longLabel, Description: longDescription},
		{ID: "beta", Label: longLabel + " (variant B)", Description: longDescription},
	}
}

// disabledWithLongReason returns options where the second option is disabled
// with a long reason string, exercising the disabled-label width-constraint path.
func disabledWithLongReason() []domain.Option {
	return []domain.Option{
		{ID: "enabled", Label: longLabel},
		{
			ID:             "disabled",
			Label:          longLabel,
			Disabled:       true,
			DisabledReason: "no stored mapping for this environment configuration in the registry",
		},
	}
}

// newSelectOverlay is a test helper that creates an inlineSelectOne with the
// DefaultTheme and the given options, allow-skip setting, and terminal width.
func newSelectOverlay(opts []domain.Option, allowSkip bool, width int) *inlineSelectOne {
	q := domain.ChoiceQuestion{
		Question: domain.Question{
			ID:        domain.QHarness,
			Title:     "Select",
			Prompt:    "Choose an option.",
			AllowSkip: allowSkip,
		},
		Options: opts,
	}
	return newInlineSelectOne(q, DefaultTheme(), width, defaultHeight)
}

// maxVisibleLineWidth returns the maximum visible (ANSI-stripped) column width
// across all newline-separated segments of the rendered view.
func maxVisibleLineWidth(view string) int {
	max := 0
	for _, line := range strings.Split(view, "\n") {
		if w := lipgloss.Width(line); w > max {
			max = w
		}
	}
	return max
}

// ---------------------------------------------------------------------------
// T1.1: Width-constrained rendering of option labels and descriptions
// ---------------------------------------------------------------------------

// TestSelectOverlay_OptionLabel_DoesNotExceedConfiguredWidth verifies that no
// line produced by view() exceeds the configured width when the option label is
// longer than the available width. The 2-column cursor prefix must be budgeted
// against the width before rendering each label.
func TestSelectOverlay_OptionLabel_DoesNotExceedConfiguredWidth(t *testing.T) {
	const width = 40
	s := newSelectOverlay(longLabelOptions(), false, width)

	view := s.view()
	actual := maxVisibleLineWidth(view)

	if actual > width {
		t.Errorf(
			"view() produced a line with visible width %d, which exceeds the configured width %d;\n"+
				"option labels must be width-constrained, accounting for the 2-column cursor prefix",
			actual, width,
		)
	}
}

// TestSelectOverlay_OptionDescription_DoesNotExceedConfiguredWidth verifies that
// the description line shown beneath the selected option does not exceed the
// configured width. The description is indented 4 spaces, so the text must be
// rendered with a width budget of (configuredWidth - 4).
func TestSelectOverlay_OptionDescription_DoesNotExceedConfiguredWidth(t *testing.T) {
	const width = 40
	// cursor starts at 0, so the first option's description is visible in view()
	s := newSelectOverlay(longLabelOptions(), false, width)

	view := s.view()
	actual := maxVisibleLineWidth(view)

	if actual > width {
		t.Errorf(
			"view() produced a line with visible width %d, which exceeds the configured width %d;\n"+
				"the option description line must be width-constrained, accounting for the 4-space indent",
			actual, width,
		)
	}
}

// TestSelectOverlay_NarrowWidth_AllLinesWithinBound verifies that at a narrow
// terminal width (20 columns), every line produced by view() is within the bound.
// This explicitly catches unwrapped long option text at narrow widths.
func TestSelectOverlay_NarrowWidth_AllLinesWithinBound(t *testing.T) {
	const width = 20
	s := newSelectOverlay(longLabelOptions(), false, width)

	view := s.view()

	for i, line := range strings.Split(view, "\n") {
		if w := lipgloss.Width(line); w > width {
			t.Errorf(
				"line %d has visible width %d, exceeding the configured width %d:\n  %q",
				i, w, width, line,
			)
		}
	}
}

// TestSelectOverlay_DisabledLabelWithReason_DoesNotExceedConfiguredWidth verifies
// that disabled option labels — which are rendered as "Label (Reason)" — also
// respect the configured width. The combined string can be significantly wider
// than the label alone.
func TestSelectOverlay_DisabledLabelWithReason_DoesNotExceedConfiguredWidth(t *testing.T) {
	const width = 40
	s := newSelectOverlay(disabledWithLongReason(), false, width)

	view := s.view()
	actual := maxVisibleLineWidth(view)

	if actual > width {
		t.Errorf(
			"view() with disabled option produced a line with visible width %d, exceeding configured width %d;\n"+
				"disabled labels with reasons must also be width-constrained",
			actual, width,
		)
	}
}

// TestSelectOverlay_MinimumWidth_DoesNotPanic verifies that an unusually small
// configured width (5 columns) does not cause a panic. The implementation must
// clamp internal width arithmetic to a safe lower bound to avoid passing zero
// or negative widths to lipgloss.
func TestSelectOverlay_MinimumWidth_DoesNotPanic(t *testing.T) {
	const width = 5
	s := newSelectOverlay(longLabelOptions(), false, width)

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("view() panicked at configured width %d: %v", width, r)
		}
	}()

	view := s.view()
	if view == "" {
		t.Error("view() returned an empty string at minimum width; want non-empty output")
	}
}

// ---------------------------------------------------------------------------
// T1.2: None skip entry — presence and absence
// ---------------------------------------------------------------------------

// TestSelectOverlay_AllowSkipFalse_NoneEntryAbsent verifies that when AllowSkip
// is false the rendered view contains no "None"-labelled entry. The synthetic
// entry must only appear when the question explicitly allows skipping.
func TestSelectOverlay_AllowSkipFalse_NoneEntryAbsent(t *testing.T) {
	s := newSelectOverlay(threeRealOptions(), false, defaultWidth)

	view := s.view()

	if strings.Contains(view, "None") {
		t.Errorf(
			"view() contains 'None' when AllowSkip=false;\n"+
				"the synthetic skip entry must not appear when the question does not allow skipping:\n%s",
			view,
		)
	}
}

// TestSelectOverlay_AllowSkipTrue_NoneEntryPresent verifies that when AllowSkip
// is true the rendered option list includes a clearly-labelled "None" entry
// that the user can navigate to.
func TestSelectOverlay_AllowSkipTrue_NoneEntryPresent(t *testing.T) {
	s := newSelectOverlay(threeRealOptions(), true, defaultWidth)

	view := s.view()

	if !strings.Contains(view, "None") {
		t.Errorf(
			"view() does not contain 'None' when AllowSkip=true;\n"+
				"a visible 'None' entry must appear in the option list so the user can skip by selection:\n%s",
			view,
		)
	}
}

// ---------------------------------------------------------------------------
// T1.2: None skip entry — navigation, cursor bounds
// ---------------------------------------------------------------------------

// TestSelectOverlay_AllowSkipTrue_CursorCanReachNoneEntry verifies that the
// cursor can navigate past all N real options (indices 0..N-1) to reach the
// synthetic "None" entry at position N. With current code the cursor clamps at
// N-1 and cannot advance further; this test is the specification that the cursor
// bound must extend by one when AllowSkip is true.
func TestSelectOverlay_AllowSkipTrue_CursorCanReachNoneEntry(t *testing.T) {
	opts := threeRealOptions()
	s := newSelectOverlay(opts, true, defaultWidth)

	// Navigate down past every real option.
	for range opts {
		s.update(tea.KeyMsg{Type: tea.KeyDown})
	}

	// At this point the cursor should be on the None entry: the cursor prefix
	// ("▶ ") must appear on the None line, not just None somewhere in the view.
	view := s.view()
	if !strings.Contains(view, "▶ None") {
		t.Errorf(
			"after navigating down past all %d real options, the cursor prefix is not on the None entry;\n"+
				"pressing Down %d times from cursor=0 must place the cursor (▶) on the None entry at position %d:\n%s",
			len(opts), len(opts), len(opts), view,
		)
	}
}

// TestSelectOverlay_AllowSkipTrue_CursorClampsAtNoneEntry verifies that pressing
// Down while the cursor is already on the None entry (the last position) does not
// advance the cursor past it. A quiet off-by-one would not be caught by the
// answer-based tests, so this test verifies the bound explicitly.
func TestSelectOverlay_AllowSkipTrue_CursorClampsAtNoneEntry(t *testing.T) {
	opts := threeRealOptions()
	s := newSelectOverlay(opts, true, defaultWidth)

	// Navigate to the None entry (position len(opts)).
	for range opts {
		s.update(tea.KeyMsg{Type: tea.KeyDown})
	}

	// Press Down one extra time — cursor must stay on None.
	s.update(tea.KeyMsg{Type: tea.KeyDown})

	view := s.view()
	if !strings.Contains(view, "▶ None") {
		t.Errorf(
			"after pressing Down past the None entry, the cursor prefix is no longer on None;\n"+
				"the cursor must clamp at the None entry and not advance out of bounds:\n%s",
			view,
		)
	}
}

// ---------------------------------------------------------------------------
// T1.2: None skip entry — answer when None is selected
// ---------------------------------------------------------------------------

// TestSelectOverlay_AllowSkipTrue_SelectNone_ProducesSkipAnswer verifies that
// pressing Enter while the cursor is on the "None" entry produces a SkippedOne
// answer. The status must be SkippedOne, not Answered (even with an empty ID)
// and not Cancelled.
func TestSelectOverlay_AllowSkipTrue_SelectNone_ProducesSkipAnswer(t *testing.T) {
	opts := threeRealOptions()
	s := newSelectOverlay(opts, true, defaultWidth)

	// Navigate to the None entry (position len(opts)).
	for range opts {
		s.update(tea.KeyMsg{Type: tea.KeyDown})
	}
	s.update(tea.KeyMsg{Type: tea.KeyEnter})

	ans := s.answer()

	if ans.Status != domain.SkippedOne {
		t.Errorf(
			"selecting the None entry produced Status=%q; want SkippedOne\n"+
				"(must not produce Answered with empty ID, and must not produce Cancelled)",
			ans.Status,
		)
	}
}

// TestSelectOverlay_AllowSkipTrue_SkipAnswerCarriesNoOptionID verifies that the
// SkippedOne answer produced by selecting "None" carries an empty OptionID. The
// synthetic entry must not be backed by a real option ID of any kind.
func TestSelectOverlay_AllowSkipTrue_SkipAnswerCarriesNoOptionID(t *testing.T) {
	opts := threeRealOptions()
	s := newSelectOverlay(opts, true, defaultWidth)

	for range opts {
		s.update(tea.KeyMsg{Type: tea.KeyDown})
	}
	s.update(tea.KeyMsg{Type: tea.KeyEnter})

	ans := s.answer()

	if ans.OptionID != "" {
		t.Errorf(
			"skip answer has OptionID=%q; want empty string\n"+
				"(the None entry must produce a skip answer carrying no option ID)",
			ans.OptionID,
		)
	}
}

// ---------------------------------------------------------------------------
// T1.2: Real-option selection is unaffected by the skip entry
// ---------------------------------------------------------------------------

// TestSelectOverlay_AllowSkipTrue_SelectRealOption_ProducesAnsweredStatus
// verifies that when AllowSkip=true, selecting a real option still produces an
// Answered answer. The presence of the None entry must not alter the status of
// real-option answers.
func TestSelectOverlay_AllowSkipTrue_SelectRealOption_ProducesAnsweredStatus(t *testing.T) {
	opts := threeRealOptions()
	s := newSelectOverlay(opts, true, defaultWidth)

	// Navigate to the second real option (index 1) and select it.
	s.update(tea.KeyMsg{Type: tea.KeyDown})
	s.update(tea.KeyMsg{Type: tea.KeyEnter})

	ans := s.answer()

	if ans.Status != domain.Answered {
		t.Errorf(
			"selecting a real option (index 1) with AllowSkip=true produced Status=%q; want Answered",
			ans.Status,
		)
	}
}

// TestSelectOverlay_AllowSkipTrue_SelectRealOption_CorrectOptionID verifies that
// the OptionID in the answer matches the selected real option's ID even when the
// None entry is present. This guards against index-shift bugs where the synthetic
// entry displaces real options in the cursor-to-option mapping.
func TestSelectOverlay_AllowSkipTrue_SelectRealOption_CorrectOptionID(t *testing.T) {
	opts := threeRealOptions()
	s := newSelectOverlay(opts, true, defaultWidth)

	// Navigate to the second real option (index 1, ID "opt-b") and select it.
	s.update(tea.KeyMsg{Type: tea.KeyDown})
	s.update(tea.KeyMsg{Type: tea.KeyEnter})

	ans := s.answer()

	const want = "opt-b"
	if ans.OptionID != want {
		t.Errorf(
			"real option answer OptionID=%q; want %q\n"+
				"(the None entry must not shift or shadow real option indices)",
			ans.OptionID, want,
		)
	}
}

// ---------------------------------------------------------------------------
// T1.2: ESC cancellation is unaffected by AllowSkip
// ---------------------------------------------------------------------------

// TestSelectOverlay_AllowSkipTrue_EscProducesCancelledAnswer verifies that
// pressing ESC when AllowSkip=true still produces a Cancelled answer, exactly
// as it does when AllowSkip=false. The None entry must not alter ESC semantics.
func TestSelectOverlay_AllowSkipTrue_EscProducesCancelledAnswer(t *testing.T) {
	s := newSelectOverlay(threeRealOptions(), true, defaultWidth)

	s.update(tea.KeyMsg{Type: tea.KeyEsc})

	ans := s.answer()

	if ans.Status != domain.Cancelled {
		t.Errorf(
			"ESC with AllowSkip=true produced Status=%q; want Cancelled\n"+
				"(ESC cancel semantics must be unchanged by the addition of the None entry)",
			ans.Status,
		)
	}
}
