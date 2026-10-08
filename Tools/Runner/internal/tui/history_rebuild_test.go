package tui

// history_rebuild_test.go verifies that the progress screen shows the run's
// history rebuilt from the artifact's Execution Log: after every restart in
// this process and when a run is resumed in a fresh process. History rows
// replace whatever the screen held (so nothing is shown twice) and the live
// rows of the new session append after them.

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"

	"mosaic-common/interaction"
	tuicommon "mosaic-common/tui"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/tui/screens/runflow"
)

// ---------------------------------------------------------------------------
// historyRows
// ---------------------------------------------------------------------------

func TestHistoryRows_MapsEveryLogRowInOrder(t *testing.T) {
	got := historyRows(sampleLog())

	if want := sampleHistory(); !reflect.DeepEqual(got, want) {
		t.Errorf("historyRows =\n%+v\nwant\n%+v", got, want)
	}
}

func TestHistoryRows_RowsAreCompleteAndNotRunning(t *testing.T) {
	for _, row := range historyRows(sampleLog()) {
		if row.Status == "running" || row.Status == "" {
			t.Errorf("history row %q has status %q, want the logged final status", row.AgentInstance, row.Status)
		}
		if row.Elapsed != 0 {
			t.Errorf("history row %q has elapsed %v, want 0", row.AgentInstance, row.Elapsed)
		}
	}
}

func TestHistoryRows_EmptyLog_YieldsNoRows(t *testing.T) {
	if got := historyRows(nil); len(got) != 0 {
		t.Errorf("historyRows(nil) = %+v, want no rows", got)
	}
}

// ---------------------------------------------------------------------------
// loadHistory
// ---------------------------------------------------------------------------

func TestLoadHistory_ReadsTheRunArtifactThroughTheStoreFactory(t *testing.T) {
	var gotFolder string
	m := newRootModel(context.Background(), &stubNavSession{}, Options{
		Theme: tuicommon.DefaultTheme(),
		ArtifactStoreFactory: func(folder string) domain.ArtifactStore {
			gotFolder = folder
			return storeWithLog(sampleLog()...)
		},
	})
	m.selections.runFolder = restartTestRunFolder

	got := m.loadHistory()

	if gotFolder != restartTestRunFolder {
		t.Errorf("store factory called with %q, want %q", gotFolder, restartTestRunFolder)
	}
	if want := sampleHistory(); !reflect.DeepEqual(got, want) {
		t.Errorf("loadHistory =\n%+v\nwant\n%+v", got, want)
	}
}

func TestLoadHistory_ReadFailure_YieldsNoHistoryAndNeverFails(t *testing.T) {
	cases := map[string]error{
		"missing artifact": os.ErrNotExist,
		"refused artifact": &domain.RefusalError{Component: "artifact", Reason: "failed to parse execution log"},
		"other error":      errors.New("disk on fire"),
	}
	for name, readErr := range cases {
		t.Run(name, func(t *testing.T) {
			fx := newRestartFixture(t, &readOnlyStore{err: readErr})

			if got := fx.m.loadHistory(); len(got) != 0 {
				t.Errorf("loadHistory = %+v after a read error, want no history", got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// newProgressScreenWithHistory
// ---------------------------------------------------------------------------

func TestNewProgressScreenWithHistory_ResumedRun_ShowsHistory(t *testing.T) {
	fx := newRestartFixture(t, storeWithLog(sampleLog()...))
	fx.m.selections.isNewRun = false

	screen := fx.m.newProgressScreenWithHistory(stylesFromTheme(fx.m.theme))

	if want := sampleHistory(); !reflect.DeepEqual(screen.Rows(), want) {
		t.Errorf("Rows() =\n%+v\nwant\n%+v", screen.Rows(), want)
	}
}

func TestNewProgressScreenWithHistory_NewRun_StartsWithNoRows(t *testing.T) {
	fx := newRestartFixture(t, storeWithLog(sampleLog()...))
	fx.m.selections.isNewRun = true

	screen := fx.m.newProgressScreenWithHistory(stylesFromTheme(fx.m.theme))

	if screen == nil {
		t.Fatal("newProgressScreenWithHistory returned nil")
	}
	if rows := screen.Rows(); len(rows) != 0 {
		t.Errorf("Rows() = %+v for a new run, want none", rows)
	}
}

// ---------------------------------------------------------------------------
// After a restart in this process
// ---------------------------------------------------------------------------

// TestRestartHistory_ProgressScreenShowsExactlyTheLoggedHistory seeds the
// screen with rows of the session that just ended -- some of which are also in
// the log -- and asserts the restart leaves exactly the log's rows, no more.
func TestRestartHistory_ProgressScreenShowsExactlyTheLoggedHistory(t *testing.T) {
	for _, path := range restartDrivers {
		t.Run(path.name, func(t *testing.T) {
			fx := newRestartFixture(t, storeWithLog(sampleLog()...))
			for _, row := range sampleHistory()[:2] {
				fx.m.progressScreen.AppendRow(runflow.ProgressRow{AgentInstance: row.AgentInstance, Phase: row.Phase, Stage: row.Stage, Status: "running"})
				fx.m.progressScreen.CompleteRow(row.Status)
			}
			fx.m.progressScreen.AppendRow(runflow.ProgressRow{AgentInstance: "unlogged#9", Phase: "EXECUTION", Status: "running"})

			path.drive(t, fx.m)

			if want := sampleHistory(); !reflect.DeepEqual(fx.m.progressScreen.Rows(), want) {
				t.Errorf("progress rows after restart =\n%+v\nwant exactly the Execution Log history\n%+v",
					fx.m.progressScreen.Rows(), want)
			}
		})
	}
}

func TestRestartHistory_LiveRowsAppendAfterHistoryWithoutDuplicates(t *testing.T) {
	fx := newRestartFixture(t, storeWithLog(sampleLog()...))
	driveDoneContinue(t, fx.m)

	fx.m.Update(noticeMsg("reviewer#4", "phase=EXECUTION stage=\"Implementation.1\" status=running"))
	fx.m.Update(noticeMsg("reviewer#4", "phase=EXECUTION stage=\"Implementation.1\" status=SUCCESS"))

	want := append(sampleHistory(), runflow.ProgressRow{
		AgentInstance: "reviewer#4", Phase: "EXECUTION", Stage: "Implementation.1", Status: "SUCCESS",
	})
	got := fx.m.progressScreen.Rows()
	for i := range got {
		got[i].Elapsed = 0 // live rows carry a measured elapsed time; history rows carry none
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("progress rows =\n%+v\nwant history followed by the live row\n%+v", got, want)
	}
}

// TestRestartHistory_CompletionNoticeDoesNotRewriteHistory asserts a completion
// notice that arrives before any live row has started cannot overwrite the
// status of the last history row.
func TestRestartHistory_CompletionNoticeDoesNotRewriteHistory(t *testing.T) {
	fx := newRestartFixture(t, storeWithLog(sampleLog()...))
	driveDoneContinue(t, fx.m)

	fx.m.Update(noticeMsg("late#5", "phase=EXECUTION stage=\"\" status=SUCCESS"))

	if want := sampleHistory(); !reflect.DeepEqual(fx.m.progressScreen.Rows(), want) {
		t.Errorf("progress rows =\n%+v\nwant the history unchanged\n%+v", fx.m.progressScreen.Rows(), want)
	}
}

func TestRestartHistory_RendersHistoryRowsOnTheProgressView(t *testing.T) {
	fx := newRestartFixture(t, storeWithLog(sampleLog()...))
	driveStopRecoveryRetry(t, fx.m)

	view := fx.m.progressScreen.View()
	for _, want := range []string{"planner#1", "builder#2", "checkpoint#3", "BLOCKED"} {
		if !containsStr(view, want) {
			t.Errorf("progress view after restart does not show %q:\n%s", want, view)
		}
	}
}

// ---------------------------------------------------------------------------
// Fresh-process resume
// ---------------------------------------------------------------------------

func newFreshProcessResumeModel(store domain.ArtifactStore) *rootModel {
	return newRootModel(context.Background(), &stubNavSession{}, Options{
		Theme:                tuicommon.DefaultTheme(),
		ResolvedRunID:        restartTestRunID,
		InitialRunFolder:     restartTestRunFolder,
		ArtifactStoreFactory: func(string) domain.ArtifactStore { return store },
	})
}

func TestFreshProcessResume_ProgressScreenShowsHistoryFromTheLog(t *testing.T) {
	m := newFreshProcessResumeModel(storeWithLog(sampleLog()...))
	if m.selections.isNewRun {
		t.Fatal("precondition: the model treats a resolved existing run as a new run")
	}

	m.launchSession()

	if m.progressScreen == nil {
		t.Fatal("launchSession left no progress screen")
	}
	if want := sampleHistory(); !reflect.DeepEqual(m.progressScreen.Rows(), want) {
		t.Errorf("progress rows on a fresh-process resume =\n%+v\nwant\n%+v", m.progressScreen.Rows(), want)
	}
}

func TestFreshProcessResume_LiveRowsAppendAfterHistory(t *testing.T) {
	m := newFreshProcessResumeModel(storeWithLog(sampleLog()...))
	m.launchSession()

	m.Update(noticeMsg("reviewer#4", "phase=EXECUTION stage=\"\" status=running"))

	rows := m.progressScreen.Rows()
	if len(rows) != len(sampleLog())+1 {
		t.Fatalf("len(rows) = %d, want %d (history + 1 live): %+v", len(rows), len(sampleLog())+1, rows)
	}
	if rows[len(rows)-1].AgentInstance != "reviewer#4" || rows[len(rows)-1].Status != "running" {
		t.Errorf("last row = %+v, want the live reviewer#4 row", rows[len(rows)-1])
	}
}

func TestFreshProcessResume_UnreadableArtifact_StartsWithEmptyHistory(t *testing.T) {
	m := newFreshProcessResumeModel(&readOnlyStore{err: &domain.RefusalError{Component: "artifact", Reason: "bad table"}})

	m.launchSession()

	if m.progressScreen == nil {
		t.Fatal("launchSession left no progress screen")
	}
	if rows := m.progressScreen.Rows(); len(rows) != 0 {
		t.Errorf("progress rows = %+v, want none when the artifact cannot be read", rows)
	}
}

func TestNewRunLaunch_ProgressScreenHasNoHistory(t *testing.T) {
	m := newFreshProcessResumeModel(storeWithLog(sampleLog()...))
	m.selections.isNewRun = true

	m.launchSession()

	if rows := m.progressScreen.Rows(); len(rows) != 0 {
		t.Errorf("progress rows = %+v for a new run, want none", rows)
	}
}

// noticeMsg builds the session notice message that drives the progress rows.
func noticeMsg(title, message string) questionMsg {
	return questionMsg{
		kind:   questionNotice,
		notice: interaction.Notice{Level: interaction.NoticeInfo, Title: title, Message: message},
	}
}
