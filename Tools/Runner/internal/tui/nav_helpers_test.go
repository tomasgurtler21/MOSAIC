package tui

// nav_helpers_test.go holds the shared fixtures and builders used across the
// navigation-behavior test files split out of the former navigation_test.go
// and seed_nav_test.go: the stub/capturing session doubles, rootModel
// constructors, question/choice builders, and small string-matching helpers.
//
// Tests are in package tui (internal) because screenID and rootModel fields
// are unexported. Tests drive the model through the Bubble Tea model/update
// cycle with no real terminal attached.

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-common/interaction"
	tuicommon "mosaic-common/tui"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/runscan"
	"mosaic-run/internal/runselect"
	"mosaic-run/internal/session"
	"mosaic-run/internal/tui/screens/runconfig"
	"mosaic-run/internal/tui/screens/runflow"
)

// ---------------------------------------------------------------------------
// Stub session
// ---------------------------------------------------------------------------

type stubNavSession struct {
	outcome domain.RunOutcome
	err     error
}

func (s *stubNavSession) Start(_ context.Context, _ domain.RunConfig) (domain.RunOutcome, error) {
	return s.outcome, s.err
}

// capturingSession is a session.Session double that stores the domain.RunConfig
// passed to Start so tests can assert on what RunConfig reaches the session boundary.
type capturingSession struct {
	outcome domain.RunOutcome
	err     error
	// configs is a buffered channel of size 1; Start sends the received config here.
	configs chan domain.RunConfig
}

func newCapturingSession() *capturingSession {
	return &capturingSession{
		outcome: domain.RunOutcome{Status: domain.RunCompleted, Message: "ok"},
		configs: make(chan domain.RunConfig, 1),
	}
}

func (s *capturingSession) Start(_ context.Context, cfg domain.RunConfig) (domain.RunOutcome, error) {
	s.configs <- cfg
	return s.outcome, s.err
}

// ---------------------------------------------------------------------------
// rootModel constructors
// ---------------------------------------------------------------------------

func newTestModel() *rootModel {
	sess := &stubNavSession{outcome: domain.RunOutcome{Status: domain.RunCompleted, Message: "ok"}}
	return newRootModel(context.Background(), sess, Options{
		Theme: tuicommon.DefaultTheme(),
	})
}

// newTestModelNewRun creates a rootModel with isNewRun = true set in selections,
// so navigation tests can exercise the new-run path without driving the run-select
// screen.
func newTestModelNewRun() *rootModel {
	m := newTestModel()
	m.selections.isNewRun = true
	return m
}

func sendKey(m *rootModel, keyType tea.KeyType) (tea.Model, tea.Cmd) {
	return m.Update(tea.KeyMsg{Type: keyType})
}

func newProgressScreen(m *rootModel) *runflow.ProgressScreen {
	style := stylesFromTheme(m.theme)
	return runflow.NewProgressScreen(m.width, m.height, style)
}

// newTestChoiceQuestion creates a ChoiceQuestion with the given title and option IDs.
func newTestChoiceQuestion(title string, optionIDs []string) interaction.ChoiceQuestion {
	opts := make([]interaction.Option, len(optionIDs))
	for i, id := range optionIDs {
		opts[i] = interaction.Option{ID: id, Label: id}
	}
	return interaction.ChoiceQuestion{
		Question: interaction.Question{Title: title},
		Options:  opts,
	}
}

// newTestModelWithDiscoverer creates a rootModel with the given OrchestratorDiscoverer
// injected via Options so that harness-screen Enter-key paths can be exercised.
func newTestModelWithDiscoverer(discoverer func(workDir, harnessID string) (string, error)) *rootModel {
	sess := &stubNavSession{outcome: domain.RunOutcome{Status: domain.RunCompleted, Message: "ok"}}
	return newRootModel(context.Background(), sess, Options{
		Theme:                  tuicommon.DefaultTheme(),
		OrchestratorDiscoverer: discoverer,
	})
}

// ---------------------------------------------------------------------------
// Run-candidate / scan fixtures
// ---------------------------------------------------------------------------

// newTestCandidate creates a RunCandidate with the given runID for test use.
func newTestCandidate(runID, folderPath string) runscan.RunCandidate {
	return runscan.RunCandidate{
		RunInfo: runscan.RunInfo{
			RunID:       runID,
			FolderPath:  folderPath,
			LastUpdated: time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC),
			Workflow:    "test-workflow",
			Task:        "test task",
		},
	}
}

// newModelWithScan creates a rootModel with the given scan candidates pre-loaded via Options.
func newModelWithScan(candidates []runscan.RunCandidate) *rootModel {
	sess := &stubNavSession{outcome: domain.RunOutcome{Status: domain.RunCompleted, Message: "ok"}}
	scanResult := &runscan.ScanResult{Candidates: candidates}
	return newRootModel(context.Background(), sess, Options{
		Theme:      tuicommon.DefaultTheme(),
		ScanResult: scanResult,
	})
}

// newModelWithScanAndDiscoverer creates a rootModel that both starts on the
// run-selection screen and can discover an orchestrator file, so a test can walk
// the whole path from choosing a run through to the setup screens.
func newModelWithScanAndDiscoverer(
	candidates []runscan.RunCandidate,
	discoverer func(workDir, harnessID string) (string, error),
) *rootModel {
	sess := &stubNavSession{outcome: domain.RunOutcome{Status: domain.RunCompleted, Message: "ok"}}
	scanResult := &runscan.ScanResult{Candidates: candidates}
	return newRootModel(context.Background(), sess, Options{
		Theme:                  tuicommon.DefaultTheme(),
		ScanResult:             scanResult,
		OrchestratorDiscoverer: discoverer,
	})
}

// ---------------------------------------------------------------------------
// RunSelectScreen choice fixtures
// ---------------------------------------------------------------------------

// newTestResumeChoice builds a selectable runselect.Choice{Kind: ChoiceResume} for
// runID, carrying the same fixture metadata as newTestCandidate.
func newTestResumeChoice(runID string) runselect.Choice {
	return runselect.Choice{
		ID:         runID,
		Kind:       runselect.ChoiceResume,
		Run:        newTestCandidate(runID, "/ws/Orchestration-"+runID).RunInfo,
		Selectable: true,
	}
}

// newTestUnresumableChoice builds a non-selectable runselect.Choice{Kind: ChoiceUnresumable}
// for runID, carrying reason.
func newTestUnresumableChoice(runID string, reason runscan.UnresumableReason) runselect.Choice {
	return runselect.Choice{
		ID:   runID,
		Kind: runselect.ChoiceUnresumable,
		Run: runscan.RunInfo{
			RunID:      runID,
			FolderPath: "/ws/Orchestration-" + runID,
		},
		Selectable: false,
		Reason:     reason,
	}
}

// newRunChoiceFixture is the new-run Choice every runselect.Question carries first.
func newRunChoiceFixture() runselect.Choice {
	return runselect.Choice{ID: runselect.NewRunChoiceID, Kind: runselect.ChoiceNewRun, Selectable: true}
}

// ---------------------------------------------------------------------------
// Run identity minting fixtures (Stage 2 / T2.1)
// ---------------------------------------------------------------------------

// fixedMintedRunID and fixedMintedRunFolder are the deterministic values returned
// by fixedMinter(). They satisfy domain.IsValidRunID and the scoped-folder naming
// convention, making T2.1/T2.2 assertions precise and reproducible.
const (
	fixedMintedRunID     = "20260801T120000Z-ab12"
	fixedMintedRunFolder = "/test/ws/Orchestration-20260801T120000Z-ab12"
)

// fixedMinter returns a RunIdentityMinter that always yields the fixed pair
// (fixedMintedRunID, fixedMintedRunFolder). Use wherever tests must assert on
// exact minted values rather than only checking validity.
func fixedMinter() RunIdentityMinter {
	return func() (string, string) {
		return fixedMintedRunID, fixedMintedRunFolder
	}
}

// factoryCall records the arguments received by a capturing session factory on
// a single invocation.
type factoryCall struct {
	runFolder string
	isNewRun  bool
}

// newModelWithScanMinterFactory creates a rootModel pre-wired with:
//   - a multi-candidate scan result (shows the run-select screen),
//   - an injectable RunIdentityMinter, and
//   - a session factory that records each call in *calls and always returns capSess.
//
// This lets T2.1 tests drive the run-select "new run" path and inspect both
// selections state and factory call arguments.
func newModelWithScanMinterFactory(
	candidates []runscan.RunCandidate,
	minter RunIdentityMinter,
	calls *[]factoryCall,
	capSess *capturingSession,
) *rootModel {
	scanResult := &runscan.ScanResult{Candidates: candidates}
	return newRootModel(context.Background(), capSess, Options{
		Theme:           tuicommon.DefaultTheme(),
		ScanResult:      scanResult,
		MintRunIdentity: minter,
		SessionFactory: func(runFolder string, isNewRun bool, orchFile string, cfg runconfig.ConfigSelection) session.Session {
			*calls = append(*calls, factoryCall{runFolder: runFolder, isNewRun: isNewRun})
			return capSess
		},
	})
}

// newModelWithScanMinterFactoryCallback creates a rootModel pre-wired with:
//   - a multi-candidate scan result (shows the run-select screen),
//   - an injectable RunIdentityMinter,
//   - a session factory that records each call in *calls, and
//   - an OnRunIDResolved callback.
//
// This lets tests drive the run-select screen and inspect callback invocations
// from the OnRunIDResolved path alongside normal selections state.
func newModelWithScanMinterFactoryCallback(
	candidates []runscan.RunCandidate,
	minter RunIdentityMinter,
	calls *[]factoryCall,
	capSess *capturingSession,
	onRunIDResolved func(runID string),
) *rootModel {
	scanResult := &runscan.ScanResult{Candidates: candidates}
	return newRootModel(context.Background(), capSess, Options{
		Theme:           tuicommon.DefaultTheme(),
		ScanResult:      scanResult,
		MintRunIdentity: minter,
		SessionFactory: func(runFolder string, isNewRun bool, orchFile string, cfg runconfig.ConfigSelection) session.Session {
			*calls = append(*calls, factoryCall{runFolder: runFolder, isNewRun: isNewRun})
			return capSess
		},
		OnRunIDResolved: onRunIDResolved,
	})
}

// ---------------------------------------------------------------------------
// String-matching helpers
// ---------------------------------------------------------------------------

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if len(sub) > 0 && containsStr(s, sub) {
			return true
		}
	}
	return false
}

func containsStr(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
