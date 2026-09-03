package screens

// progress_gate_events_test.go verifies ProgressScreen.TakeStopGateEvents: the
// record of confirmation-gate transitions the root model drains in order to log
// the stop lifecycle.
//
// The root model cannot observe the gate today. It polls GracefulStop() on
// every update, which reports a latched state rather than a transition, and
// nothing anywhere records *how* a resolution happened or which key caused it.
// Logging entry and resolution from a latch poll would emit one entry per
// repaint; hence a drainable record, reported exactly once.
//
// The negative rows are the load-bearing ones. A key ignored while the gate is
// pending must record nothing: the gate stays pending, so an entry would
// misname what happened, and the volume is unbounded -- arrow keys and terminal
// escape sequences arrive as bursts, and a held key would flood the single
// shared log with near-identical entries.
//
// RED: TakeStopGateEvents records nothing yet, so every positive assertion
// below fails.

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// escKey is the message bubbletea produces for the escape key, which cancels
// the gate alongside 'n'/'N' but is not a rune key.
func escKey() tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyEsc}
}

// pendingProgressScreen returns a screen with the gate open and its entry event
// already drained, so a later assertion sees only what the test itself caused.
func pendingProgressScreen(t *testing.T) *ProgressScreen {
	t.Helper()
	s := NewProgressScreen(80, 24, progressStyles())
	pressProgressKey(s, "s")
	if !s.ConfirmPending() {
		t.Fatal("precondition: ConfirmPending() = false after 's'; the gate must be open " +
			"before its resolution can be asserted")
	}
	s.TakeStopGateEvents()
	return s
}

// ---------------------------------------------------------------------------
// Entering the gate
// ---------------------------------------------------------------------------

// TestProgressScreen_StopGateEvents_RecordsEntryWithKey asserts that opening
// the gate is recorded, carrying the key verbatim so the log states which key
// the user actually pressed.
func TestProgressScreen_StopGateEvents_RecordsEntryWithKey(t *testing.T) {
	for _, key := range []string{"s", "S"} {
		t.Run(key, func(t *testing.T) {
			// Arrange
			s := NewProgressScreen(80, 24, progressStyles())

			// Act
			pressProgressKey(s, key)
			events := s.TakeStopGateEvents()

			// Assert
			if len(events) != 1 {
				t.Fatalf("TakeStopGateEvents() returned %d events after %q, want exactly 1 -- "+
					"entering the gate is a transition the log must record", len(events), key)
			}
			if events[0].Kind != StopGateEntered {
				t.Errorf("event Kind = %v, want StopGateEntered", events[0].Kind)
			}
			if events[0].Key != key {
				t.Errorf("event Key = %q, want %q recorded verbatim", events[0].Key, key)
			}
		})
	}
}

// TestProgressScreen_StopGateEvents_InertStopKeyRecordsNothing asserts that the
// stop key pressed after a stop is already confirmed records nothing. The gate
// is not entered in that state -- it is inert by design -- so an entry would
// claim a transition that never happened.
func TestProgressScreen_StopGateEvents_InertStopKeyRecordsNothing(t *testing.T) {
	// Arrange -- a screen with a confirmed stop already latched.
	s := armedProgressScreen()
	s.TakeStopGateEvents()

	// Act
	pressProgressKey(s, "s")

	// Assert
	if events := s.TakeStopGateEvents(); len(events) != 0 {
		t.Errorf("TakeStopGateEvents() returned %d events after 's' on an already-stopped run, "+
			"want 0 -- the gate is inert in that state and no transition occurred: %+v",
			len(events), events)
	}
}

// ---------------------------------------------------------------------------
// Resolving the gate
// ---------------------------------------------------------------------------

// TestProgressScreen_StopGateEvents_RecordsResolution asserts that each
// resolving key records a resolution naming the key and distinguishing a
// confirmed stop from a cancelled one. Without the outcome, the log cannot
// answer whether the run was actually stopping.
func TestProgressScreen_StopGateEvents_RecordsResolution(t *testing.T) {
	cases := []struct {
		key       string
		msg       tea.KeyMsg
		confirmed bool
	}{
		{key: "y", msg: runeKey("y"), confirmed: true},
		{key: "Y", msg: runeKey("Y"), confirmed: true},
		{key: "n", msg: runeKey("n"), confirmed: false},
		{key: "N", msg: runeKey("N"), confirmed: false},
		{key: "esc", msg: escKey(), confirmed: false},
	}

	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			// Arrange
			s := pendingProgressScreen(t)

			// Act
			s.Update(tc.msg)
			events := s.TakeStopGateEvents()

			// Assert
			if len(events) != 1 {
				t.Fatalf("TakeStopGateEvents() returned %d events after %q, want exactly 1 -- "+
					"resolving the gate is a transition the log must record", len(events), tc.key)
			}
			if events[0].Kind != StopGateResolved {
				t.Errorf("event Kind = %v, want StopGateResolved", events[0].Kind)
			}
			if events[0].Key != tc.key {
				t.Errorf("event Key = %q, want %q recorded verbatim", events[0].Key, tc.key)
			}
			if events[0].Confirmed != tc.confirmed {
				t.Errorf("event Confirmed = %v, want %v -- the log must distinguish a confirmed "+
					"stop from a cancelled one", events[0].Confirmed, tc.confirmed)
			}
		})
	}
}

// TestProgressScreen_StopGateEvents_IgnoredKeysRecordNothing asserts that keys
// the gate ignores record no transition. They leave the gate pending, so
// nothing has been entered and nothing resolved.
func TestProgressScreen_StopGateEvents_IgnoredKeysRecordNothing(t *testing.T) {
	for _, key := range []string{"a", "i", "s", "q", "x"} {
		t.Run(key, func(t *testing.T) {
			// Arrange
			s := pendingProgressScreen(t)

			// Act
			pressProgressKey(s, key)

			// Assert
			if events := s.TakeStopGateEvents(); len(events) != 0 {
				t.Errorf("TakeStopGateEvents() returned %d events after the ignored key %q, want 0 -- "+
					"the gate stayed pending, so no transition occurred: %+v", len(events), key, events)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Drain semantics
// ---------------------------------------------------------------------------

// TestProgressScreen_StopGateEvents_DrainsSoEachTransitionIsReportedOnce
// asserts that a drained transition is not reported again. The root model polls
// the screen on every update, including every repaint, so a record that
// survived its read would put one entry per repaint into the shared log.
func TestProgressScreen_StopGateEvents_DrainsSoEachTransitionIsReportedOnce(t *testing.T) {
	// Arrange
	s := NewProgressScreen(80, 24, progressStyles())
	pressProgressKey(s, "s")
	pressProgressKey(s, "y")

	// Act
	first := s.TakeStopGateEvents()
	second := s.TakeStopGateEvents()

	// Assert
	if len(first) != 2 {
		t.Fatalf("the first drain returned %d events, want 2 (entered, resolved): %+v", len(first), first)
	}
	if len(second) != 0 {
		t.Errorf("the second drain returned %d events, want 0 -- an already-reported transition "+
			"would be logged once per repaint: %+v", len(second), second)
	}
}

// TestProgressScreen_ResetStopState_ClearsUndrainedGateEvents asserts that a
// restart discards transitions recorded but not yet read. An event from the run
// that just ended must not be logged against the resumed one.
func TestProgressScreen_ResetStopState_ClearsUndrainedGateEvents(t *testing.T) {
	// Arrange
	s := NewProgressScreen(80, 24, progressStyles())
	pressProgressKey(s, "s")
	pressProgressKey(s, "y")

	// Act
	s.ResetStopState()

	// Assert
	if events := s.TakeStopGateEvents(); len(events) != 0 {
		t.Errorf("TakeStopGateEvents() returned %d events after a restart reset, want 0 -- "+
			"the previous run's transitions must not be logged against the resumed run: %+v",
			len(events), events)
	}
}
