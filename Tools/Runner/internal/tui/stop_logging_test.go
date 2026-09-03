package tui

// stop_logging_test.go verifies that the TUI records the graceful-stop
// lifecycle into the shared debug log.
//
// The purpose is diagnostic completeness across one file: from the debug log
// alone, for a run in which a stop was attempted, one must be able to answer
// three questions -- was the confirmation gate entered, how was it resolved,
// and was the shared signal armed. The session records the other half of the
// lifecycle (which dispatch checkpoint observed the signal) through the same
// logger instance, so the sequence is reconstructible without correlating two
// files by timestamp.
//
// Two properties are as important as the entries themselves:
//
//   - Silence where nothing happened. A key ignored while the gate is pending
//     is not a resolution, and its volume is unbounded (a held key or a
//     terminal escape sequence arrives as a burst of key messages). A run in
//     which no stop is attempted must gain no entries at all.
//   - Exactly one armed entry per arming episode. The shared signal's Request()
//     is idempotent and is called on every update once a stop is confirmed, so
//     an entry driven by that poll would appear once per repaint.
//
// The second property must hold without becoming its own defect: a once-only
// latch would suppress the armed entry for a stop confirmed on a run resumed
// through a restart path, silently degrading the stop feature across a restart.
// TestStopLogging_RestartThenSecondStop_EmitsASecondArmedEntry pins that case.
//
// RED: the TUI has no debug logger and emits none of these entries yet.

import (
	"context"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	tuicommon "mosaic-common/tui"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/session"
)

// ---------------------------------------------------------------------------
// Recording debug logger
// ---------------------------------------------------------------------------

// tuiLogEntry holds one recorded call to tuiRecordingLogger.Log.
type tuiLogEntry struct {
	event   string
	message string
	fields  []domain.DebugField
}

// field returns the value recorded under key, and whether it was present.
func (e tuiLogEntry) field(key string) (string, bool) {
	for _, f := range e.fields {
		if f.Key == key {
			return f.Value, true
		}
	}
	return "", false
}

// tuiRecordingLogger accumulates Log calls so tests can assert on the emitted
// events, their order and their fields without touching the filesystem.
//
// Mutex-guarded because the real logger is shared: the TUI writes from the
// Bubble Tea goroutine while a session goroutine may write concurrently.
type tuiRecordingLogger struct {
	mu      sync.Mutex
	entries []tuiLogEntry
}

// Log implements domain.DebugLogger. It appends one entry per call.
func (r *tuiRecordingLogger) Log(event string, message string, fields ...domain.DebugField) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, tuiLogEntry{
		event:   event,
		message: message,
		fields:  append([]domain.DebugField(nil), fields...),
	})
}

// snapshot returns a copy of all recorded entries, taken under the lock.
func (r *tuiRecordingLogger) snapshot() []tuiLogEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]tuiLogEntry(nil), r.entries...)
}

// eventsNamed returns the recorded entries carrying the given event name, in
// the order they were logged.
func (r *tuiRecordingLogger) eventsNamed(event string) []tuiLogEntry {
	var out []tuiLogEntry
	for _, e := range r.snapshot() {
		if e.event == event {
			out = append(out, e)
		}
	}
	return out
}

// eventOrder returns the recorded event names in order, for an ordering
// assertion and for readable failure output.
func (r *tuiRecordingLogger) eventOrder() []string {
	entries := r.snapshot()
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.event)
	}
	return out
}

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// newStopLoggingModel returns a rootModel sitting on the progress screen with
// the given recorder wired as the shared debug logger, ready to receive gate
// keypresses.
func newStopLoggingModel(rec *tuiRecordingLogger, stopSignal *session.StopSignal) *rootModel {
	sess := &stubNavSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	m := newRootModel(context.Background(), sess, Options{
		Theme:      tuicommon.DefaultTheme(),
		StopSignal: stopSignal,
		Debug:      rec,
	})
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress
	return m
}

// pressProgressRune sends one plain character key to the model.
func pressProgressRune(m *rootModel, key string) {
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
}

// requireOneEntry returns the single entry recorded for event, failing when the
// count is anything else.
func requireOneEntry(t *testing.T, rec *tuiRecordingLogger, event string) tuiLogEntry {
	t.Helper()
	entries := rec.eventsNamed(event)
	if len(entries) != 1 {
		t.Fatalf("recorded %d %q entries, want exactly 1; the full event sequence was %v",
			len(entries), event, rec.eventOrder())
	}
	return entries[0]
}

// requireField returns the value of the named field, failing when it is absent.
func requireField(t *testing.T, entry tuiLogEntry, key string) string {
	t.Helper()
	value, ok := entry.field(key)
	if !ok {
		t.Fatalf("the %q entry carries no %q field; its fields were %+v", entry.event, key, entry.fields)
	}
	return value
}

// ---------------------------------------------------------------------------
// The three facts the log must be able to answer
// ---------------------------------------------------------------------------

// TestStopLogging_ConfirmedStop_RecordsEntryResolutionAndArming asserts that a
// confirmed stop leaves the log able to answer all three questions: the gate was
// entered, it was resolved by 'y' as confirmed, and the shared signal was armed.
func TestStopLogging_ConfirmedStop_RecordsEntryResolutionAndArming(t *testing.T) {
	// Arrange
	rec := &tuiRecordingLogger{}
	stopSignal := session.NewStopSignal()
	m := newStopLoggingModel(rec, stopSignal)

	// Act
	pressProgressRune(m, "s")
	pressProgressRune(m, "y")

	// Assert -- the gate was entered, and by which key.
	entered := requireOneEntry(t, rec, domain.EventTUIStopGateEntered)
	if key := requireField(t, entered, "key"); key != "s" {
		t.Errorf("gate-entered entry key field = %q, want %q", key, "s")
	}

	// Assert -- how it was resolved.
	resolved := requireOneEntry(t, rec, domain.EventTUIStopGateResolved)
	if key := requireField(t, resolved, "key"); key != "y" {
		t.Errorf("gate-resolved entry key field = %q, want %q", key, "y")
	}
	if outcome := requireField(t, resolved, "outcome"); outcome != "confirmed" {
		t.Errorf("gate-resolved entry outcome field = %q, want %q", outcome, "confirmed")
	}

	// Assert -- the signal was armed. Recorded as its own event so the three
	// facts stay separately answerable from the log.
	requireOneEntry(t, rec, domain.EventTUIStopSignalArmed)

	if !stopSignal.Requested() {
		t.Error("stopSignal.Requested() = false after a confirmed stop; the armed entry would " +
			"be recording something that did not happen")
	}
}

// TestStopLogging_ConfirmedStop_RecordsTheLifecycleInOrder asserts the entries
// arrive in the order the events occurred, in one sequence. Reconstructing the
// stop lifecycle from the log is the point of the work; an armed entry preceding
// the resolution that caused it would misdescribe it.
func TestStopLogging_ConfirmedStop_RecordsTheLifecycleInOrder(t *testing.T) {
	// Arrange
	rec := &tuiRecordingLogger{}
	m := newStopLoggingModel(rec, session.NewStopSignal())

	// Act
	pressProgressRune(m, "s")
	pressProgressRune(m, "y")

	// Assert
	want := []string{
		domain.EventTUIStopGateEntered,
		domain.EventTUIStopGateResolved,
		domain.EventTUIStopSignalArmed,
	}
	got := rec.eventOrder()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("recorded event sequence = %v, want %v", got, want)
	}
}

// TestStopLogging_CancelledStop_RecordsResolutionAndNoArming asserts that a
// cancelled stop is distinguishable in the log from a confirmed one, and that
// nothing claims the signal was armed -- the run is still going.
func TestStopLogging_CancelledStop_RecordsResolutionAndNoArming(t *testing.T) {
	for _, key := range []string{"n", "N"} {
		t.Run(key, func(t *testing.T) {
			// Arrange
			rec := &tuiRecordingLogger{}
			stopSignal := session.NewStopSignal()
			m := newStopLoggingModel(rec, stopSignal)

			// Act
			pressProgressRune(m, "s")
			pressProgressRune(m, key)

			// Assert
			resolved := requireOneEntry(t, rec, domain.EventTUIStopGateResolved)
			if got := requireField(t, resolved, "key"); got != key {
				t.Errorf("gate-resolved entry key field = %q, want %q", got, key)
			}
			if outcome := requireField(t, resolved, "outcome"); outcome != "cancelled" {
				t.Errorf("gate-resolved entry outcome field = %q, want %q", outcome, "cancelled")
			}

			if armed := rec.eventsNamed(domain.EventTUIStopSignalArmed); len(armed) != 0 {
				t.Errorf("recorded %d armed entries after a cancelled stop, want 0 -- the log would "+
					"assert an arming that never happened", len(armed))
			}
			if stopSignal.Requested() {
				t.Error("stopSignal.Requested() = true after a cancelled stop; want false")
			}
		})
	}
}

// TestStopLogging_EscCancellation_RecordsTheEscKey asserts the escape key is
// recorded as itself. The log states which key the user pressed, and 'esc' is
// the one resolving key that is not a character.
func TestStopLogging_EscCancellation_RecordsTheEscKey(t *testing.T) {
	// Arrange
	rec := &tuiRecordingLogger{}
	m := newStopLoggingModel(rec, session.NewStopSignal())

	// Act
	pressProgressRune(m, "s")
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})

	// Assert
	resolved := requireOneEntry(t, rec, domain.EventTUIStopGateResolved)
	if key := requireField(t, resolved, "key"); key != "esc" {
		t.Errorf("gate-resolved entry key field = %q, want %q", key, "esc")
	}
	if outcome := requireField(t, resolved, "outcome"); outcome != "cancelled" {
		t.Errorf("gate-resolved entry outcome field = %q, want %q", outcome, "cancelled")
	}
}

// ---------------------------------------------------------------------------
// Silence where nothing happened
// ---------------------------------------------------------------------------

// TestStopLogging_NoStopAttempted_RecordsNothing asserts that an ordinary run
// gains no entries from this work. The stop vocabulary is silent unless a stop
// is attempted.
func TestStopLogging_NoStopAttempted_RecordsNothing(t *testing.T) {
	// Arrange
	rec := &tuiRecordingLogger{}
	m := newStopLoggingModel(rec, session.NewStopSignal())

	// Act -- keys an ordinary run receives, none of which touches the gate.
	for _, key := range []string{"x", "z", "1"} {
		pressProgressRune(m, key)
	}

	// Assert
	if entries := rec.snapshot(); len(entries) != 0 {
		t.Errorf("recorded %d entries on a run with no stop attempt, want 0: %v",
			len(entries), rec.eventOrder())
	}
}

// TestStopLogging_IgnoredKeysWhilePending_RecordNothing asserts that keys the
// gate ignores add nothing to the log. They are not resolutions -- the gate
// stays pending -- and their volume is unbounded, so an entry each would flood
// the shared log with near-identical lines.
func TestStopLogging_IgnoredKeysWhilePending_RecordNothing(t *testing.T) {
	// Arrange
	rec := &tuiRecordingLogger{}
	m := newStopLoggingModel(rec, session.NewStopSignal())
	pressProgressRune(m, "s")
	entriesAfterEntry := len(rec.snapshot())

	// Act -- 'a' and 'i' are the artifact-view keys, live outside the gate and
	// ignored inside it; 's' re-pressed and arbitrary keys are ignored too.
	for _, key := range []string{"a", "i", "s", "x", "1"} {
		pressProgressRune(m, key)
	}

	// Assert
	if !m.progressScreen.ConfirmPending() {
		t.Fatal("precondition: the gate is no longer pending after keys it must ignore; " +
			"the ignored-key path is not what the assertion below observed")
	}
	if got := len(rec.snapshot()); got != entriesAfterEntry {
		t.Errorf("recorded %d entries after ignored keys, want the %d present before them: %v",
			got, entriesAfterEntry, rec.eventOrder())
	}
}

// TestStopLogging_RepeatedArming_RecordsOneArmedEntry asserts that the armed
// entry appears once per arming episode. The shared signal's Request() is
// idempotent and is called on every update once a stop is confirmed, so an entry
// driven by that poll rather than by the resolution would appear once per
// repaint.
func TestStopLogging_RepeatedArming_RecordsOneArmedEntry(t *testing.T) {
	// Arrange
	rec := &tuiRecordingLogger{}
	stopSignal := session.NewStopSignal()
	m := newStopLoggingModel(rec, stopSignal)
	pressProgressRune(m, "s")
	pressProgressRune(m, "y")

	// Act -- further updates, each of which re-requests the already-armed signal.
	for i := 0; i < 5; i++ {
		pressProgressRune(m, "x")
	}

	// Assert
	if armed := rec.eventsNamed(domain.EventTUIStopSignalArmed); len(armed) != 1 {
		t.Errorf("recorded %d armed entries after one confirmed stop and 5 further updates, "+
			"want exactly 1 -- the entry must follow the confirmation, not the per-update poll",
			len(armed))
	}
	if !stopSignal.Requested() {
		t.Error("precondition: stopSignal.Requested() = false; the repeated arming the test " +
			"exists to check never occurred")
	}
}

// ---------------------------------------------------------------------------
// The armed entry survives a restart
// ---------------------------------------------------------------------------

// TestStopLogging_RestartThenSecondStop_EmitsASecondArmedEntry asserts that a
// stop confirmed on a resumed run records its own armed entry.
//
// This is the case repeated-arming suppression can silently break. Suppress the
// repeat with a once-only latch and the second stop's gate entry and resolution
// are still recorded -- they are keypress-driven -- while the armed entry is
// not, because the latch is still set from the first stop. The log then cannot
// answer "was the signal armed" for the resumed run, and the stop feature has
// degraded across a restart with nothing to show for it.
func TestStopLogging_RestartThenSecondStop_EmitsASecondArmedEntry(t *testing.T) {
	for _, path := range restartPaths {
		t.Run(path.name, func(t *testing.T) {
			// Arrange -- a first confirmed stop, then a restart.
			rec := &tuiRecordingLogger{}
			stopSignal := session.NewStopSignal()
			m := newStopLoggingModel(rec, stopSignal)
			pressProgressRune(m, "s")
			pressProgressRune(m, "y")

			if armed := rec.eventsNamed(domain.EventTUIStopSignalArmed); len(armed) != 1 {
				t.Fatalf("precondition: recorded %d armed entries for the first stop, want 1; "+
					"the arm -> restart -> re-arm sequence never started", len(armed))
			}

			path.restart(t, m)

			// Act -- a second stop, confirmed on the resumed run.
			pressProgressRune(m, "s")
			pressProgressRune(m, "y")

			// Assert
			if armed := rec.eventsNamed(domain.EventTUIStopSignalArmed); len(armed) != 2 {
				t.Errorf("recorded %d armed entries across two confirmed stops, want 2 -- the "+
					"resumed run's stop must record its own arming; the full sequence was %v",
					len(armed), rec.eventOrder())
			}
			if !stopSignal.Requested() {
				t.Error("stopSignal.Requested() = false after the resumed run's stop was confirmed; " +
					"the second stop did not take effect, so the entry counts above describe " +
					"a run that never armed")
			}
		})
	}
}
