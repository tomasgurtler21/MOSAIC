package tui

// completion_test.go verifies the TUI's COMPLETED phase-marker write behaviour.
//
// These tests are in package tui (internal) so they can access the unexported
// rootModel fields (screen, selections, artifactStoreFactory, clock) and the
// unexported message types (runDoneMsg, runErrorMsg).
//
// Tests are driven by sending synthetic messages to rootModel.Update, exactly
// as the existing flow_test.go and stop_recovery_test.go do.
//
// TDD RED: the positive tests (RunCompleted → SetPhase called with "COMPLETED")
// and the default-factory test fail because the runDoneMsg handler in app.go
// does not yet call SetPhase.
//
// The negative tests (other statuses → SetPhase not called) are trivially GREEN
// with the current implementation and serve as regression guards once the
// feature is implemented.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tuicommon "mosaic-common/tui"
	"mosaic-run/internal/artifact"
	"mosaic-run/internal/domain"
)

// ---------------------------------------------------------------------------
// Test doubles
// ---------------------------------------------------------------------------

// tuiSpyStore is a test double for domain.ArtifactStore that records every
// SetPhase call. Read, Create, and Apply panic if called, because completion
// tests must not trigger those methods.
type tuiSpyStore struct {
	setCalls       []tuiSetPhaseCall
	setErr         error   // optional error to return from SetPhase
	capturedFolder string  // set by the ArtifactStoreFactory wrapper; asserts correct run folder is passed
}

type tuiSetPhaseCall struct {
	state domain.ArtifactState
	phase string
	now   time.Time
}

func (s *tuiSpyStore) Read(_ context.Context) (domain.ArtifactState, error) {
	panic("tuiSpyStore.Read: unexpected call in TUI completion tests")
}

func (s *tuiSpyStore) Create(_ context.Context, _ domain.WorkflowInfo, _ string, _ domain.RunSettings, _ time.Time, _ string) (domain.ArtifactState, error) {
	panic("tuiSpyStore.Create: unexpected call in TUI completion tests")
}

func (s *tuiSpyStore) Apply(_ context.Context, _ domain.ArtifactState, _ domain.CompletedStep) (domain.ArtifactState, error) {
	panic("tuiSpyStore.Apply: unexpected call in TUI completion tests")
}

func (s *tuiSpyStore) SetPhase(_ context.Context, state domain.ArtifactState, phase string, now time.Time) (domain.ArtifactState, error) {
	s.setCalls = append(s.setCalls, tuiSetPhaseCall{state: state, phase: phase, now: now})
	state.CurrentState.Phase = phase
	if s.setErr != nil {
		return domain.ArtifactState{}, s.setErr
	}
	return state, nil
}

// fixedClock implements domain.Clock and always returns the same instant, so
// tests can assert on the exact timestamp passed to SetPhase.
type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

var testClockTime = time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)

// newCompletionModel creates a rootModel with a spy store injected as the
// ArtifactStoreFactory and a fixed clock. runFolder populates the resolved run
// folder on the model (via InitialRunFolder), mirroring what a production TUI
// sets after run identity resolves. Pass an empty string to simulate the
// pre-identity state.
func newCompletionModel(spy *tuiSpyStore, runFolder string) *rootModel {
	return newRootModel(context.Background(), &stubNavSession{
		outcome: domain.RunOutcome{Status: domain.RunCompleted},
	}, Options{
		Theme:            tuicommon.DefaultTheme(),
		InitialRunFolder: runFolder,
		ArtifactStoreFactory: func(folder string) domain.ArtifactStore {
			spy.capturedFolder = folder
			return spy
		},
		Clock: fixedClock{t: testClockTime},
	})
}

// ---------------------------------------------------------------------------
// T1.1 — Positive: COMPLETED marker written on RunCompleted
// ---------------------------------------------------------------------------

// TestCompletedMarker_WrittenOnRunCompleted verifies that when the session
// outcome is RunCompleted and the resolved run folder is non-empty, the TUI
// calls ArtifactStore.SetPhase with phase "COMPLETED" exactly once.
func TestCompletedMarker_WrittenOnRunCompleted(t *testing.T) {
	spy := &tuiSpyStore{}
	m := newCompletionModel(spy, "/some/run/folder")
	m.screen = screenProgress

	m.Update(runDoneMsg{outcome: domain.RunOutcome{Status: domain.RunCompleted}})

	if len(spy.setCalls) == 0 {
		t.Fatal("SetPhase was not called; want it called with phase=COMPLETED after RunCompleted")
	}
	if spy.setCalls[0].phase != "COMPLETED" {
		t.Errorf("SetPhase called with phase=%q, want %q", spy.setCalls[0].phase, "COMPLETED")
	}
	if len(spy.setCalls) > 1 {
		t.Errorf("SetPhase called %d times, want exactly 1", len(spy.setCalls))
	}
	if spy.capturedFolder != "/some/run/folder" {
		t.Errorf("ArtifactStoreFactory called with folder=%q, want %q", spy.capturedFolder, "/some/run/folder")
	}
}

// TestCompletedMarker_SetPhaseReceivesClockTime verifies that the timestamp
// passed to SetPhase comes from the injected Clock, not from time.Now() called
// directly inside Update.
func TestCompletedMarker_SetPhaseReceivesClockTime(t *testing.T) {
	spy := &tuiSpyStore{}
	m := newCompletionModel(spy, "/some/run/folder")
	m.screen = screenProgress

	m.Update(runDoneMsg{outcome: domain.RunOutcome{Status: domain.RunCompleted}})

	if len(spy.setCalls) == 0 {
		t.Fatal("SetPhase was not called; cannot assert timestamp")
	}
	if !spy.setCalls[0].now.Equal(testClockTime) {
		t.Errorf("SetPhase timestamp = %v, want %v (from injected clock)", spy.setCalls[0].now, testClockTime)
	}
}

// TestCompletedMarker_SetPhaseReceivesEmptyState verifies that SetPhase is
// called with a zero ArtifactState, matching the CLI's pattern: SetPhase
// re-reads the artifact from disk, so the caller supplies no prior state.
func TestCompletedMarker_SetPhaseReceivesEmptyState(t *testing.T) {
	spy := &tuiSpyStore{}
	m := newCompletionModel(spy, "/some/run/folder")
	m.screen = screenProgress

	m.Update(runDoneMsg{outcome: domain.RunOutcome{Status: domain.RunCompleted}})

	if len(spy.setCalls) == 0 {
		t.Fatal("SetPhase was not called; cannot assert state argument")
	}
	if !reflect.DeepEqual(spy.setCalls[0].state, domain.ArtifactState{}) {
		t.Errorf("SetPhase state argument = %+v, want zero ArtifactState{}", spy.setCalls[0].state)
	}
}

// TestCompletedMarker_NotWrittenWhenRunFolderEmpty verifies that when the
// resolved run folder is empty (run identity not yet settled), the TUI does NOT
// call SetPhase even though the outcome is RunCompleted.
func TestCompletedMarker_NotWrittenWhenRunFolderEmpty(t *testing.T) {
	spy := &tuiSpyStore{}
	m := newCompletionModel(spy, "") // empty run folder — pre-identity state
	m.screen = screenProgress

	m.Update(runDoneMsg{outcome: domain.RunOutcome{Status: domain.RunCompleted}})

	if len(spy.setCalls) != 0 {
		t.Errorf("SetPhase called %d time(s), want 0 when run folder is empty", len(spy.setCalls))
	}
}

// TestCompletedMarker_DoneScreen_ShownAfterRunCompleted verifies that the TUI
// transitions to the done screen after handling RunCompleted, regardless of the
// marker write. The run outcome is not downgraded by the marker write.
func TestCompletedMarker_DoneScreen_ShownAfterRunCompleted(t *testing.T) {
	spy := &tuiSpyStore{}
	m := newCompletionModel(spy, "/some/run/folder")
	m.screen = screenProgress

	m.Update(runDoneMsg{outcome: domain.RunOutcome{Status: domain.RunCompleted, Message: "all done"}})

	if m.screen != screenDone {
		t.Errorf("screen = %v after RunCompleted, want screenDone (%v)", m.screen, screenDone)
	}
}

// TestCompletedMarker_FailingWrite_TUIProceedsToDoneScreen verifies that when
// SetPhase returns an error, the TUI still transitions to the done screen
// rather than crashing or hanging. The write failure must be non-fatal, and
// the error must be surfaced as a warning in the done screen (not silently
// swallowed).
func TestCompletedMarker_FailingWrite_TUIProceedsToDoneScreen(t *testing.T) {
	spy := &tuiSpyStore{setErr: errors.New("disk full")}
	m := newCompletionModel(spy, "/some/run/folder")
	m.screen = screenProgress

	m.Update(runDoneMsg{outcome: domain.RunOutcome{Status: domain.RunCompleted}})

	if m.screen != screenDone {
		t.Errorf("screen = %v after failing SetPhase, want screenDone (%v)", m.screen, screenDone)
	}
	if m.doneScreen == nil {
		t.Fatal("doneScreen is nil after failing SetPhase; cannot assert warning text")
	}
	view := m.doneScreen.View()
	if !strings.Contains(view, "disk full") {
		t.Errorf("done screen does not show SetPhase error as warning; view = %q", view)
	}
}

// ---------------------------------------------------------------------------
// T1.1 — Negative: COMPLETED marker NOT written for other terminal statuses
// ---------------------------------------------------------------------------

// TestCompletedMarker_NotWrittenOnRunStopped verifies that SetPhase is not
// called when the outcome is RunStopped (the run must remain resumable).
func TestCompletedMarker_NotWrittenOnRunStopped(t *testing.T) {
	spy := &tuiSpyStore{}
	m := newCompletionModel(spy, "/some/run/folder")
	m.screen = screenProgress

	m.Update(runDoneMsg{outcome: domain.RunOutcome{Status: domain.RunStopped}})

	if len(spy.setCalls) != 0 {
		t.Errorf("SetPhase called %d time(s), want 0 when status is RunStopped", len(spy.setCalls))
	}
}

// TestCompletedMarker_NotWrittenOnRunDeviationUnresolved verifies that SetPhase
// is not called when the outcome is RunDeviationUnresolved.
func TestCompletedMarker_NotWrittenOnRunDeviationUnresolved(t *testing.T) {
	spy := &tuiSpyStore{}
	m := newCompletionModel(spy, "/some/run/folder")
	m.screen = screenProgress

	m.Update(runDoneMsg{outcome: domain.RunOutcome{Status: domain.RunDeviationUnresolved}})

	if len(spy.setCalls) != 0 {
		t.Errorf("SetPhase called %d time(s), want 0 when status is RunDeviationUnresolved", len(spy.setCalls))
	}
}

// TestCompletedMarker_NotWrittenOnRunRefused verifies that SetPhase is not
// called when the outcome is RunRefused.
func TestCompletedMarker_NotWrittenOnRunRefused(t *testing.T) {
	spy := &tuiSpyStore{}
	m := newCompletionModel(spy, "/some/run/folder")
	m.screen = screenProgress

	m.Update(runDoneMsg{outcome: domain.RunOutcome{Status: domain.RunRefused}})

	if len(spy.setCalls) != 0 {
		t.Errorf("SetPhase called %d time(s), want 0 when status is RunRefused", len(spy.setCalls))
	}
}

// TestCompletedMarker_NotWrittenOnRunFailed verifies that SetPhase is not
// called when the outcome is RunFailed.
func TestCompletedMarker_NotWrittenOnRunFailed(t *testing.T) {
	spy := &tuiSpyStore{}
	m := newCompletionModel(spy, "/some/run/folder")
	m.screen = screenProgress

	m.Update(runDoneMsg{outcome: domain.RunOutcome{Status: domain.RunFailed}})

	if len(spy.setCalls) != 0 {
		t.Errorf("SetPhase called %d time(s), want 0 when status is RunFailed", len(spy.setCalls))
	}
}

// TestCompletedMarker_NotWrittenOnRunStoppedByConsultant verifies that SetPhase
// is not called when the outcome is RunStoppedByConsultant. The run must remain
// resumable; writing COMPLETED would silently break resumability.
func TestCompletedMarker_NotWrittenOnRunStoppedByConsultant(t *testing.T) {
	spy := &tuiSpyStore{}
	m := newCompletionModel(spy, "/some/run/folder")
	m.screen = screenProgress

	m.Update(runDoneMsg{outcome: domain.RunOutcome{
		Status:     domain.RunStoppedByConsultant,
		StopReason: "consultant decided to halt the run",
	}})

	if len(spy.setCalls) != 0 {
		t.Errorf("SetPhase called %d time(s), want 0 when status is RunStoppedByConsultant", len(spy.setCalls))
	}
}

// TestCompletedMarker_NotWrittenOnRunError verifies that SetPhase is not called
// when the session goroutine sends a runErrorMsg (non-nil error return from
// Start). The error path must not write a COMPLETED marker.
func TestCompletedMarker_NotWrittenOnRunError(t *testing.T) {
	spy := &tuiSpyStore{}
	m := newCompletionModel(spy, "/some/run/folder")
	m.screen = screenProgress

	m.Update(runErrorMsg{err: errors.New("unexpected harness failure")})

	if len(spy.setCalls) != 0 {
		t.Errorf("SetPhase called %d time(s), want 0 on runErrorMsg path", len(spy.setCalls))
	}
}

// ---------------------------------------------------------------------------
// T1.2 — Default factory: nil ArtifactStoreFactory uses run-folder file store
// ---------------------------------------------------------------------------

// TestCompletedMarker_DefaultFactory_WritesFileToRunFolder verifies that when
// ArtifactStoreFactory is nil, the TUI constructs a real artifact.FileStore
// pointing to filepath.Join(runFolder, "Orchestration.md") and writes the
// COMPLETED marker through it. This pins the default resolution path so that
// production wiring cannot silently regress to a no-op.
func TestCompletedMarker_DefaultFactory_WritesFileToRunFolder(t *testing.T) {
	runDir := t.TempDir()
	orchPath := filepath.Join(runDir, "Orchestration.md")

	// Seed a valid Orchestration.md so SetPhase can re-read it before mutating.
	// (The real fileStore.SetPhase re-reads from disk before writing.)
	seedStore := artifact.NewFileStore(orchPath)
	ctx := context.Background()
	if _, err := seedStore.Create(
		ctx,
		domain.WorkflowInfo{ID: "test-workflow", Version: "1.0"},
		"test task",
		domain.RunSettings{},
		time.Now(),
		"test-run-id",
	); err != nil {
		t.Fatalf("seeding Orchestration.md: %v", err)
	}

	// Build model with nil factory (exercises the default resolution path) and
	// the temp run folder pre-set as InitialRunFolder.
	m := newRootModel(context.Background(), &stubNavSession{
		outcome: domain.RunOutcome{Status: domain.RunCompleted},
	}, Options{
		Theme:            tuicommon.DefaultTheme(),
		InitialRunFolder: runDir,
		// ArtifactStoreFactory intentionally nil — tests the default path.
	})
	m.screen = screenProgress

	m.Update(runDoneMsg{outcome: domain.RunOutcome{Status: domain.RunCompleted}})

	// After the marker write, the file must contain the COMPLETED phase value.
	content, err := os.ReadFile(orchPath)
	if err != nil {
		t.Fatalf("reading Orchestration.md after runDoneMsg: %v", err)
	}
	if !strings.Contains(string(content), "COMPLETED") {
		t.Errorf("Orchestration.md does not contain COMPLETED marker after RunCompleted:\n%s", content)
	}
}
