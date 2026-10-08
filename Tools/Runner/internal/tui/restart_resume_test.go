package tui

// restart_resume_test.go verifies that every restart path of the TUI resumes
// the run that already exists instead of creating a new one: after a stop, a
// launch-failure retry or a stop-recovery choice, the session is started with
// IsNewRun false and without the seed inputs the run was created with, so the
// session never refuses with "cannot create a new run here". A run whose
// artifact was never created (it failed before the session wrote it) is still
// started as new.

import (
	"errors"
	"fmt"
	"os"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-run/internal/domain"
)

// ---------------------------------------------------------------------------
// The session is started as a resume on every restart path
// ---------------------------------------------------------------------------

func TestRestartResume_ExistingArtifact_StartsSessionAsResumeWithoutSeed(t *testing.T) {
	for _, path := range restartDrivers {
		t.Run(path.name, func(t *testing.T) {
			fx := newRestartFixture(t, storeWithLog(sampleLog()...))

			cmd := path.drive(t, fx.m)
			cfg := startedConfig(t, cmd, fx.sess)

			if cfg.IsNewRun {
				t.Error("RunConfig.IsNewRun = true after the restart; want false -- " +
					"the session would refuse with \"cannot create a new run here\"")
			}
			if len(cfg.SeedInputs) != 0 {
				t.Errorf("RunConfig.SeedInputs = %v after the restart; want none -- "+
					"seed inputs must not be applied to a run that already exists", cfg.SeedInputs)
			}
			if cfg.RunFolder != restartTestRunFolder || cfg.RunID != restartTestRunID {
				t.Errorf("RunConfig run identity = (%q, %q), want (%q, %q)",
					cfg.RunFolder, cfg.RunID, restartTestRunFolder, restartTestRunID)
			}
		})
	}
}

func TestRestartResume_UnreadableArtifact_StillStartsAsResume(t *testing.T) {
	// A refused or unreadable artifact exists; the session reports its own
	// reason on resume instead of being asked to create a run over it.
	refusal := &domain.RefusalError{Component: "artifact", Reason: "failed to parse execution log"}
	for _, path := range restartDrivers {
		t.Run(path.name, func(t *testing.T) {
			fx := newRestartFixture(t, &readOnlyStore{err: refusal})

			cfg := startedConfig(t, path.drive(t, fx.m), fx.sess)

			if cfg.IsNewRun {
				t.Error("RunConfig.IsNewRun = true for a run whose artifact exists but is refused; want false")
			}
			if len(cfg.SeedInputs) != 0 {
				t.Errorf("RunConfig.SeedInputs = %v, want none", cfg.SeedInputs)
			}
		})
	}
}

// TestRestartResume_NoArtifactYet_StillStartsAsNewRun guards the other side of
// the rule: a new run that stopped before its artifact was written has nothing
// to resume, so it keeps starting as new and keeps its seed input.
func TestRestartResume_NoArtifactYet_StillStartsAsNewRun(t *testing.T) {
	fx := newRestartFixture(t, &readOnlyStore{err: os.ErrNotExist})

	cfg := startedConfig(t, driveDoneContinue(t, fx.m), fx.sess)

	if !cfg.IsNewRun {
		t.Error("RunConfig.IsNewRun = false although the run has no artifact; want true")
	}
	if len(cfg.SeedInputs) != 1 || cfg.SeedInputs[0] != restartTestSeed {
		t.Errorf("RunConfig.SeedInputs = %v, want [%q]", cfg.SeedInputs, restartTestSeed)
	}
}

// ---------------------------------------------------------------------------
// Session rebuild versus reuse
// ---------------------------------------------------------------------------

func TestRestartResume_SessionFactory_ReceivesResumeFlag_OnRebuildingPathsOnly(t *testing.T) {
	for _, path := range restartDrivers {
		t.Run(path.name, func(t *testing.T) {
			fx := newRestartFixture(t, storeWithLog(sampleLog()...))

			path.drive(t, fx.m)

			calls := *fx.factoryCalls
			if !path.rebuilds {
				if len(calls) != 0 {
					t.Errorf("session factory called %d times, want 0 -- this path reuses its session", len(calls))
				}
				return
			}
			if len(calls) != 1 {
				t.Fatalf("session factory called %d times, want exactly 1", len(calls))
			}
			if calls[0].isNewRun {
				t.Error("session factory received isNewRun = true; want false for a run that already exists")
			}
			if calls[0].runFolder != restartTestRunFolder {
				t.Errorf("session factory received runFolder %q, want %q", calls[0].runFolder, restartTestRunFolder)
			}
		})
	}
}

func TestRestartResume_StopRecoveryChoice_ReachesRunConfig(t *testing.T) {
	cases := []struct {
		name       string
		drive      func(t *testing.T, m *rootModel) tea.Cmd
		wantManual bool
	}{
		{"retry", driveStopRecoveryRetry, false},
		{"manual dispatch", driveStopRecoveryManual, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fx := newRestartFixture(t, storeWithLog(sampleLog()...))

			cfg := startedConfig(t, tc.drive(t, fx.m), fx.sess)

			if cfg.ManualDispatch != tc.wantManual {
				t.Errorf("RunConfig.ManualDispatch = %v, want %v", cfg.ManualDispatch, tc.wantManual)
			}
			if cfg.IsNewRun {
				t.Error("RunConfig.IsNewRun = true after a stop-recovery choice; want false")
			}
		})
	}
}

func TestRestartResume_ProgressScreen_IsKeptAcrossRestart(t *testing.T) {
	for _, path := range restartDrivers {
		t.Run(path.name, func(t *testing.T) {
			fx := newRestartFixture(t, storeWithLog(sampleLog()...))
			before := fx.m.progressScreen

			path.drive(t, fx.m)

			if fx.m.progressScreen != before {
				t.Error("the progress screen was replaced by the restart; want the same screen kept " +
					"(its rows are replaced by the run history instead)")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// restartAsResume
// ---------------------------------------------------------------------------

func TestRestartAsResume_WithoutRebuild_ReusesSessionAndReturnsStartCommand(t *testing.T) {
	fx := newRestartFixture(t, storeWithLog(sampleLog()...))
	fx.m.screen = screenStop

	cmd := fx.m.restartAsResume(false)

	if fx.m.screen != screenProgress {
		t.Errorf("screen = %v, want screenProgress", fx.m.screen)
	}
	if len(*fx.factoryCalls) != 0 {
		t.Errorf("session factory called %d times, want 0 when the session is reused", len(*fx.factoryCalls))
	}
	cfg := startedConfig(t, cmd, fx.sess)
	if cfg.IsNewRun || len(cfg.SeedInputs) != 0 {
		t.Errorf("started with IsNewRun=%v SeedInputs=%v, want a resume without seeds", cfg.IsNewRun, cfg.SeedInputs)
	}
}

func TestRestartAsResume_WithRebuild_RebuildsSessionWithCurrentSelections(t *testing.T) {
	fx := newRestartFixture(t, storeWithLog(sampleLog()...))
	fx.m.selections.config.ExecutablePath = "/opt/override"
	fx.m.screen = screenDone

	cmd := fx.m.restartAsResume(true)

	calls := *fx.factoryCalls
	if len(calls) != 1 || calls[0].isNewRun || calls[0].runFolder != restartTestRunFolder {
		t.Fatalf("factory calls = %+v, want one call for %q with isNewRun false", calls, restartTestRunFolder)
	}
	if fx.m.screen != screenProgress {
		t.Errorf("screen = %v, want screenProgress", fx.m.screen)
	}
	if cmd == nil {
		t.Fatal("restartAsResume returned a nil command")
	}
}

func TestRestartAsResume_WithRebuildAndNoFactory_KeepsSessionWithoutPanic(t *testing.T) {
	fx := newRestartFixture(t, storeWithLog(sampleLog()...))
	fx.m.sessionFactory = nil
	before := fx.m.sess

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("restartAsResume panicked without a session factory: %v", r)
		}
	}()
	cmd := fx.m.restartAsResume(true)

	if fx.m.sess != before {
		t.Error("the session changed although no factory is set; want the existing session kept")
	}
	if cmd == nil || fx.m.screen != screenProgress {
		t.Errorf("cmd nil = %v, screen = %v; want a start command on the progress screen", cmd == nil, fx.m.screen)
	}
}

func TestRestartAsResume_CreatesProgressScreenWhenAbsent(t *testing.T) {
	fx := newRestartFixture(t, storeWithLog(sampleLog()...))
	fx.m.progressScreen = nil

	fx.m.restartAsResume(false)

	if fx.m.progressScreen == nil {
		t.Fatal("progressScreen is nil after the restart; want one created")
	}
	if got := len(fx.m.progressScreen.Rows()); got != len(sampleLog()) {
		t.Errorf("created progress screen has %d rows, want the %d history rows", got, len(sampleLog()))
	}
}

func TestRestartAsResume_NeverChangesManualDispatchSelection(t *testing.T) {
	for _, manual := range []bool{true, false} {
		t.Run(fmt.Sprintf("manualDispatch=%v", manual), func(t *testing.T) {
			fx := newRestartFixture(t, storeWithLog(sampleLog()...))
			fx.m.selections.manualDispatch = manual

			fx.m.restartAsResume(false)

			if fx.m.selections.manualDispatch != manual {
				t.Errorf("selections.manualDispatch = %v after the restart, want %v unchanged",
					fx.m.selections.manualDispatch, manual)
			}
			if fx.m.screen != screenProgress {
				t.Errorf("screen = %v, want screenProgress (the restart did not run)", fx.m.screen)
			}
		})
	}
}

func TestRestartAsResume_ResetsStopState(t *testing.T) {
	fx := newRestartFixture(t, storeWithLog(sampleLog()...))
	armGracefulStop(t, fx.m)

	fx.m.restartAsResume(false)

	if fx.stopSignal.Requested() {
		t.Error("stop signal still armed after the restart; want it disarmed")
	}
	if fx.m.progressScreen.GracefulStop() {
		t.Error("progress screen still latches the previous stop; want it cleared")
	}
}

// ---------------------------------------------------------------------------
// runArtifactExists
// ---------------------------------------------------------------------------

func TestRunArtifactExists(t *testing.T) {
	cases := []struct {
		name      string
		store     *readOnlyStore
		runFolder string
		want      bool
	}{
		{"readable artifact", storeWithLog(sampleLog()...), restartTestRunFolder, true},
		{"missing artifact", &readOnlyStore{err: os.ErrNotExist}, restartTestRunFolder, false},
		{"missing artifact, wrapped", &readOnlyStore{err: fmt.Errorf("open: %w", os.ErrNotExist)}, restartTestRunFolder, false},
		{"refused artifact", &readOnlyStore{err: &domain.RefusalError{Component: "artifact", Reason: "bad table"}}, restartTestRunFolder, true},
		{"unreadable artifact", &readOnlyStore{err: errors.New("permission denied")}, restartTestRunFolder, true},
		{"no run folder", storeWithLog(sampleLog()...), "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fx := newRestartFixture(t, tc.store)
			fx.m.selections.runFolder = tc.runFolder

			if got := fx.m.runArtifactExists(); got != tc.want {
				t.Errorf("runArtifactExists() = %v, want %v", got, tc.want)
			}
		})
	}
}
