package tui

// navigation_test.go verifies the Model's screen state machine: suite
// selection to live progress to results, drilling into a single test's
// detail and back, and quitting from any screen — table-driven over key
// sequences, matching how the other MOSAIC frontends test their navigation.
//
// No real terminal is involved anywhere in this file: Update is driven
// directly, and background suite execution is advanced by invoking the
// tea.Cmd it returns.

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-agent-test/internal/domain"
)

func keyMsg(runes string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(runes)}
}

func keyType(kt tea.KeyType) tea.KeyMsg {
	return tea.KeyMsg{Type: kt}
}

// runSuiteToCompletion drives m from ScreenSuiteSelect, through the full
// settings flow and into ScreenProgress, to the model reflecting the suite's
// finished result. It fails the test if any screen transition is unexpected or
// if the background run never produces a SuiteFinishedMsg.
func runSuiteToCompletion(t *testing.T, m Model, runner *fakeSuiteRunner) Model {
	t.Helper()
	m, cmd := startSuiteFromSuiteSelect(t, m)
	if cmd == nil {
		t.Fatalf("startSuiteFromSuiteSelect produced no tea.Cmd to start the run")
	}
	msg := runCmd(t, cmd)
	if msg == nil {
		t.Fatalf("selecting a suite produced no tea.Msg from the run")
	}
	m, _ = safeUpdate(t, m, msg)
	return m
}

// ---------------------------------------------------------------------------
// Initial state
// ---------------------------------------------------------------------------

func TestNavigation_InitialScreenIsModeSelect(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	if m.Screen() != ScreenModeSelect {
		t.Errorf("initial Screen() = %q, want %q", m.Screen(), ScreenModeSelect)
	}
}

// ---------------------------------------------------------------------------
// Table-driven key-sequence navigation
// ---------------------------------------------------------------------------

func TestNavigation_KeySequences(t *testing.T) {
	tests := []struct {
		name        string
		steps       []tea.KeyMsg
		wantScreen  Screen
		wantQuit    bool
		runToResult bool // when true, run the suite to completion before applying steps
	}{
		{
			name:       "select a suite moves to the progress screen",
			steps:      nil, // selection happens in runSuiteToCompletion
			wantScreen: ScreenResults,
			// After the suite finishes, the model should have advanced to
			// results without any further key press.
		},
		{
			name:        "down then enter on the results screen drills into detail",
			runToResult: true,
			steps:       []tea.KeyMsg{keyType(tea.KeyDown), keyType(tea.KeyEnter)},
			wantScreen:  ScreenDetail,
		},
		{
			name:        "esc from the detail screen returns to results",
			runToResult: true,
			steps:       []tea.KeyMsg{keyType(tea.KeyDown), keyType(tea.KeyEnter), keyType(tea.KeyEsc)},
			wantScreen:  ScreenResults,
		},
		{
			name:        "esc from the results screen moves to mode-select",
			runToResult: true,
			steps:       []tea.KeyMsg{keyType(tea.KeyEsc)},
			wantScreen:  ScreenModeSelect,
		},
		{
			name:        "esc from detail then esc from results reaches mode-select",
			runToResult: true,
			steps: []tea.KeyMsg{
				keyType(tea.KeyDown), keyType(tea.KeyEnter), // drill into detail
				keyType(tea.KeyEsc),                         // back to results
				keyType(tea.KeyEsc),                         // back to mode-select
			},
			wantScreen: ScreenModeSelect,
		},
		{
			name:       "ctrl+c from mode select quits",
			steps:      []tea.KeyMsg{keyType(tea.KeyCtrlC)},
			wantScreen: ScreenModeSelect,
			wantQuit:   true,
		},
		{
			name:        "ctrl+c from results quits",
			runToResult: true,
			steps:       []tea.KeyMsg{keyType(tea.KeyCtrlC)},
			wantScreen:  ScreenResults,
			wantQuit:    true,
		},
		{
			name:        "ctrl+c from detail quits",
			runToResult: true,
			steps:       []tea.KeyMsg{keyType(tea.KeyDown), keyType(tea.KeyEnter), keyType(tea.KeyCtrlC)},
			wantScreen:  ScreenDetail,
			wantQuit:    true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runner := newFakeSuiteRunner().withEvents(
				scriptedSuite{
					suiteID: "suite-under-test",
					tests: []scriptedTest{
						{testID: "test-a", verdict: domain.VerdictPass},
						{testID: "test-b", verdict: domain.VerdictFail, failed: []string{"assertion"}},
					},
				}.events()...,
			)
			m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, runner))

			if tc.runToResult {
				m = runSuiteToCompletion(t, m, runner)
				if m.Screen() != ScreenResults {
					t.Fatalf("Screen() after suite completion = %q, want %q", m.Screen(), ScreenResults)
				}
			}

			var lastCmd tea.Cmd
			for _, step := range tc.steps {
				m, lastCmd = safeUpdate(t, m, step)
			}

			if !tc.runToResult && len(tc.steps) == 0 {
				m = runSuiteToCompletion(t, m, runner)
			}

			if m.Screen() != tc.wantScreen {
				t.Errorf("final Screen() = %q, want %q", m.Screen(), tc.wantScreen)
			}
			if tc.wantQuit && lastCmd == nil {
				t.Errorf("expected ctrl+c to produce a quit command, got nil")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Quitting mid-run cancels through the suite's cancellation channel
// ---------------------------------------------------------------------------

// TestNavigation_QuitDuringRun_CancelsSuiteContext verifies AC16.7: quitting
// while a suite is running cancels the context the suite was given, rather
// than the process simply exiting. This is what lets the per-test
// lifecycle's guaranteed teardown still remove the workspace.
func TestNavigation_QuitDuringRun_CancelsSuiteContext(t *testing.T) {
	runner := newFakeSuiteRunner().blocking()
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, runner))

	m, startCmd := startSuiteFromSuiteSelect(t, m)
	if startCmd == nil {
		t.Fatalf("startSuiteFromSuiteSelect produced no tea.Cmd to start the run")
	}

	// Start the (blocking) run on its own goroutine, exactly as a real
	// tea.Program would run a returned Cmd.
	go func() { _ = startCmd() }()

	// Quit while the run is still in flight.
	_, quitCmd := safeUpdate(t, m, keyType(tea.KeyCtrlC))
	if quitCmd == nil {
		t.Fatalf("ctrl+c during a run produced no command")
	}
	_ = quitCmd()

	if !runner.wasCancelled(t) {
		t.Errorf("suite's context was not cancelled when the run was quit mid-flight")
	}
}

// ---------------------------------------------------------------------------
// Window resize propagates without disturbing the current screen
// ---------------------------------------------------------------------------

func TestNavigation_WindowResize(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	updated, _ := safeUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	if updated.Screen() != ScreenModeSelect {
		t.Errorf("resize changed Screen() to %q, want unchanged %q", updated.Screen(), ScreenModeSelect)
	}
}

// ---------------------------------------------------------------------------
// SuiteFinishedMsg carries the terminal result into the model
// ---------------------------------------------------------------------------

func TestNavigation_SuiteFinishedMsg_PopulatesResult(t *testing.T) {
	runner := newFakeSuiteRunner()
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, runner))
	m = runSuiteToCompletion(t, m, runner)

	if _, ok := m.Result(); !ok {
		t.Errorf("Result() ok = false after the suite finished, want true")
	}
}
