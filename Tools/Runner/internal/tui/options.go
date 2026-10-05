package tui

import (
	"context"

	tuicommon "mosaic-common/tui"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/runscan"
	"mosaic-run/internal/runselect"
	"mosaic-run/internal/session"
	"mosaic-run/internal/testrun"
	"mosaic-run/internal/tui/screens/runconfig"
)

// Options configures the TUI run. All fields are optional.
type Options struct {
	// Interaction is the ProgramRef wired as the session Interact dependency.
	// Run() stores the tea.Program into it once the program is created so that
	// questions from the session goroutine reach the TUI overlay system.
	Interaction *ProgramRef

	// MouseEnabled enables mouse support.
	MouseEnabled bool

	// Theme sets the colour scheme. DefaultTheme() is used when zero.
	Theme tuicommon.Theme

	// Selection carries the unanswered selection question built by
	// runselect.Resolve when run identity was not settled before launch.
	// When non-nil, it is used directly to build the run-select screen and
	// takes precedence over ScanResult -- the production entry point
	// (cmd/mosaic-run) always supplies it, so the screen is built from the
	// same runselect decision the CLI refuses on, not a second copy of the
	// question-building rules.
	Selection *runselect.Question

	// ScanResult carries the run folder scan results. When Candidates has
	// more than one entry, the RunSelectScreen is shown before setup.
	// When nil or empty, the screen is skipped and a new run is assumed.
	//
	// Retained alongside Selection for callers (and this package's own
	// tests) that construct Options directly from a scan without going
	// through runselect.Resolve first. When Selection is nil, ScanResult is
	// adapted locally via candidatesToQuestion.
	ScanResult *runscan.ScanResult

	// ResolvedRunID is set when --run or --new-run resolved identity before
	// the TUI launched. When non-empty, the RunSelectScreen is skipped.
	ResolvedRunID string

	// IsNewRun is true when --new-run was given or the scan yielded zero candidates.
	IsNewRun bool

	// RecordedWorkflowID is the workflow a resumed run recorded when it was
	// created, for the entry points that settle run identity before the TUI
	// launches (--run <run_id>). Runs chosen on the run-select screen adopt
	// their recorded workflow there instead, and never need this.
	//
	// It exists because the setup sequence skips the workflow question on a
	// resumed run: a run that arrives pre-resolved with nothing here reaches
	// the session with no workflow at all and is refused, however well its
	// artifact records one. Always "" for a new run.
	RecordedWorkflowID domain.WorkflowID

	// InitialRunFolder is the resolved run-scoped folder path when --run or
	// single-candidate auto-resume resolved run identity before TUI launch.
	// It is carried into m.selections.runFolder so that readArtifactContent
	// and the COMPLETED-marker write in a later stage use the correct path.
	InitialRunFolder string

	// SessionFactory, when non-nil, is called after run identity is resolved to
	// construct the session with the correct run-scoped artifact store and harness
	// adapter. orchFile is the path entered by the user on the orchestrator file
	// screen (empty when called before setup completes). cfg carries the harness
	// adapter selection and timeout from the config screen (zero value = fake adapter).
	// When nil, the session passed to Run() is used directly (test/backward-compat path).
	SessionFactory func(runFolder string, isNewRun bool, orchFile string, cfg runconfig.ConfigSelection) session.Session

	// OrchestratorDiscoverer, when non-nil, is called after the user selects a harness
	// on the harness-selection screen to compute the orchestrator file path from the
	// harness's agents-directory convention. When nil, orchestrator discovery is skipped
	// and OrchestratorFilePath in RunConfig will be empty (test/backward-compat path).
	//
	// Signature mirrors harness.DiscoverOrchestrator so the production entry point can
	// inject it directly without the TUI importing the harness package.
	OrchestratorDiscoverer func(workDir, harnessID string) (string, error)

	// MintRunIdentity, when non-nil, is called when the user chooses "new run"
	// on the run-select screen, to resolve run identity before the session is
	// reconstructed. When nil, the previous behaviour is preserved: identity
	// is left empty and the session factory is called with an empty run folder.
	// Production callers always supply it; tests may omit it.
	MintRunIdentity RunIdentityMinter

	// ArtifactStoreFactory, when non-nil, builds the artifact store used for the
	// terminal COMPLETED phase-marker write, given the run's resolved run-scoped
	// folder. It is called at most once per terminal outcome, only when the
	// outcome status is domain.RunCompleted and the resolved folder is non-empty.
	//
	// The folder is not known at Options-construction time for a multi-candidate
	// run (it is settled on the run-select screen), which is why this is a
	// factory over a folder rather than a store.
	//
	// When nil, the TUI resolves the store itself as
	// artifact.NewFileStore(filepath.Join(runFolder, "Orchestration.md")),
	// mirroring the CLI's nil-store fallback in internal/cli/run.go.
	ArtifactStoreFactory func(runFolder string) domain.ArtifactStore

	// Clock supplies the timestamp handed to ArtifactStore.SetPhase for the
	// completion-marker write. When nil, a real UTC clock is used.
	Clock domain.Clock

	// OnRunIDResolved, when non-nil, is called once when the run-select screen
	// resolves a deferred run identity. It receives the resolved run_id. It is
	// nil-safe (skipped when nil) and is only invoked when the resolved run_id
	// is non-empty (empty run_id, e.g. from a nil minter, does not trigger it).
	OnRunIDResolved func(runID string)

	// StopSignal is the shared graceful-stop flag. rootModel calls Request()
	// when the user confirms a stop (replacing the old m.ctxCancel() call)
	// and Reset() before rebuilding and restarting a session on the
	// in-screen continue action, so a prior confirmed stop does not
	// immediately re-arm on the resumed run.
	//
	// The same instance must be the one closed over by SessionFactory's
	// construction of session.Deps.StopRequested, otherwise the TUI's
	// Request()/Reset() calls have no effect on the running session.
	StopSignal *session.StopSignal

	// Debug records the TUI-side stop-lifecycle events. It must be the same
	// instance the session receives as session.Deps.Debug, so both halves of
	// the stop lifecycle land in one ordered log and the sequence is
	// reconstructible from a single file without cross-file timestamp
	// correlation.
	//
	// Optional: nil is normalised to domain.NopDebugLogger in newRootModel,
	// mirroring session.New's treatment of Deps.Debug, so the root model never
	// nil-checks it.
	Debug domain.DebugLogger

	// ToolVersion is the semver string of the mosaic-run binary (e.g. "1.0.0").
	// When set, it is included in the entry screen titles so users see the tool
	// identity immediately on launch. An empty string omits the version.
	ToolVersion string

	// DevMode enables the test-mode flow. When true, the TUI shows a "Run Tests"
	// option that leads to the automated test catalog screens. When false, the
	// test flow is hidden and the test screens are not reachable.
	DevMode bool

	// TestCatalogLoader, when non-nil, is called to load the test catalog from
	// the given catalog root directory (the Tools/Runner/TestCatalog/ path derived
	// from the user-supplied MOSAIC root). It returns a CatalogPort that the
	// suite selection screen uses for workflow enumeration, and the orchestrator
	// uses for scope resolution and sidecar path derivation. When nil and DevMode
	// is true, entering the test catalog path screen will proceed but the
	// transition to the suite screen will fail with an error.
	TestCatalogLoader func(catalogRoot string) (testrun.CatalogPort, error)

	// TestRunnerFactory, when non-nil, is called by the test progress screen to
	// run the test orchestration. It receives the TestConfig (collected from the
	// test flow input screens) and a ProgressReporter (wired to the TUI via
	// tea.Program.Send). The TUI calls it in a background goroutine and delivers
	// the result as a testAllDoneMsg. When nil and DevMode is true, the "Run
	// Tests" option is visible but starting a run will show an error.
	TestRunnerFactory func(ctx context.Context, cfg testrun.TestConfig, reporter testrun.ProgressReporter) (*testrun.TestSummary, error)
}
