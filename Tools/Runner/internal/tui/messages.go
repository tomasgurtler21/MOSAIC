package tui

import (
	"mosaic-run/internal/domain"
	"mosaic-run/internal/testrun"
	"mosaic-run/internal/tui/screens/runconfig"
)

// runSetupSelections holds all inputs collected during the setup phase.
type runSetupSelections struct {
	orchestratorFile string
	workflowID       domain.WorkflowID
	task             string

	// seedInput is the optional seed-input path collected on the seed screen.
	// Empty means no seeding. Only ever populated for a new run; a resumed run
	// skips the seed screen and leaves this empty.
	seedInput string

	config runconfig.ConfigSelection

	// Run identity resolved by RunSelectScreen or pre-launch flags.
	runID     string // resolved run_id; empty if not yet resolved
	runFolder string // resolved run-scoped folder path (absolute)
	isNewRun  bool   // true = create new artifact; false = resume existing

	// manualDispatch is set to true when the user chooses Manual Dispatch on
	// the stop recovery screen. It is carried into the next RunConfig so the
	// session layer can use ManualResolver for the first routing decision.
	// Cleared to false for a Retry choice.
	manualDispatch bool
}

// runDoneMsg is sent by the session goroutine when the run completes.
type runDoneMsg struct {
	outcome domain.RunOutcome
}

// runErrorMsg is sent by the session goroutine when Start returns a non-nil error.
type runErrorMsg struct{ err error }

// stepCompleteMsg is sent by the session's Notify call to inform the progress screen.
type stepCompleteMsg struct {
	agentInstance string
	phase         string
	stage         string
	status        string
}

// gracefulStopRequestMsg is sent by the progress screen when the user presses 's'.
type gracefulStopRequestMsg struct{}

// artifactContentMsg carries the current artifact file content to the artifact screen.
type artifactContentMsg struct{ content string }

// testDeployStartMsg signals that catalog deployment has begun.
type testDeployStartMsg struct{}

// testDeployDoneMsg signals that deployment completed (Err is nil on success).
type testDeployDoneMsg struct{ Err error }

// testRunStartMsg signals that a single test invocation has begun.
type testRunStartMsg struct {
	Harness  string
	Workflow string
	Mode     string
}

// testRunDoneMsg signals that a single test invocation has completed.
type testRunDoneMsg struct {
	Result testrun.TestRunResult
}

// testAllDoneMsg signals that all tests have completed.
type testAllDoneMsg struct {
	Summary *testrun.TestSummary
}

// testResolvedPathsMsg carries the resolved harness binary paths to the TUI
// progress screen before test execution begins. The factory sends this message
// immediately after resolution succeeds so the progress screen can display
// the "Resolved binaries:" section before the first deploy-start notification.
type testResolvedPathsMsg struct {
	Paths        map[string]string
	HarnessOrder []string
}

// testFlowSelections holds the inputs collected in the test catalog/suite/harness/GHCP
// mode screens. They are assembled into a testrun.TestConfig when the test run starts.
type testFlowSelections struct {
	mosaicRoot         string
	scope              testrun.TestScope
	workflows          []string
	mode               string
	harnesses          []string
	ghcpPermissionMode string
}
