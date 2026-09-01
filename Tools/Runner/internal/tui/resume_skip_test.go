package tui

// resume_skip_test.go covers the two setup questions a resumed run must never
// be asked: which workflow to run, and what the task is.
//
// Both were answered when the run was created and both are recorded in the
// run's artifact. Asking them again invites an answer that contradicts the run
// being resumed, and at best wastes the user's time restating what the run
// already says about itself. A new run, which has recorded neither, must still
// be asked both.
//
// The tests drive the setup sequence from its real entry point -- the harness
// question, which is the last thing both kinds of run are asked before the
// paths diverge -- and assert on where the sequence comes to rest. Driving from
// there rather than placing the model on a screen by hand is deliberate: a
// question the user never presses a key on is still a question they were shown,
// so "was not asked" has to mean the sequence never rests there, not merely
// that a keypress on it was ignored.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	tuicommon "mosaic-common/tui"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/runscan"
)

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// recordedWorkflowID is the workflow the resumed runs below were created under.
// It matches the workflow recorded by makeCandidate, so a run chosen from the
// run-selection screen and a run set up by hand agree on what they are.
const recordedWorkflowID = "test-workflow"

// decoyWorkflowID is declared ahead of recordedWorkflowID in the orchestrator
// fixture, so it is what the workflow-selection screen offers first. A resumed
// run that is wrongly shown that screen comes away with this value, which names
// the bug rather than merely failing an equality check.
const decoyWorkflowID = "first-listed-workflow"

// writeOrchestratorFixture writes an orchestrator file declaring decoyWorkflowID
// followed by recordedWorkflowID, both at version 1.0, and returns its path.
func writeOrchestratorFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "orchestrator-script.md")
	content := `<Workflow type="core" name="` + decoyWorkflowID + `" version="1.0">
## First Listed Workflow

| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| PLANNING | planner | TRUE | - | Plan.md |
</Workflow>

<Workflow type="core" name="` + recordedWorkflowID + `" version="1.0">
## Test Workflow

| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| PLANNING | planner | TRUE | - | Plan.md |
</Workflow>
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write orchestrator fixture: %v", err)
	}
	return path
}

// newResumedRunModel returns a model in the state the run-selection screen
// leaves it in after the user chose to resume a run recorded under
// recordedWorkflowID: standing at the harness question with the run's identity
// and workflow already settled.
func newResumedRunModel(t *testing.T) *rootModel {
	t.Helper()
	orchPath := writeOrchestratorFixture(t)
	m := newTestModelWithDiscoverer(func(string, string) (string, error) { return orchPath, nil })
	m.selections.isNewRun = false
	m.selections.runID = "20260701T120000Z-a3f9"
	m.selections.runFolder = filepath.Join(t.TempDir(), "Orchestration-20260701T120000Z-a3f9")
	m.selections.workflowID = domain.WorkflowID(recordedWorkflowID)
	if m.screen != screenSetupHarness {
		t.Fatalf("precondition: screen = %v, want screenSetupHarness", m.screen)
	}
	return m
}

// newNewRunModelAtHarness returns a model for a brand-new run standing at the
// harness question, with nothing recorded and no workflow settled.
func newNewRunModelAtHarness(t *testing.T) *rootModel {
	t.Helper()
	orchPath := writeOrchestratorFixture(t)
	m := newTestModelWithDiscoverer(func(string, string) (string, error) { return orchPath, nil })
	m.selections.isNewRun = true
	if m.screen != screenSetupHarness {
		t.Fatalf("precondition: screen = %v, want screenSetupHarness", m.screen)
	}
	return m
}

// ---------------------------------------------------------------------------
// A resumed run adopts its recorded workflow at selection time
// ---------------------------------------------------------------------------

// TestRunSelect_ResumingARun_AdoptsItsRecordedWorkflow verifies that choosing a
// run to resume settles that run's workflow at the same moment, from the value
// the run itself recorded.
//
// This is what makes skipping the workflow question safe: the workflow is not
// dropped, it is already known by the time the setup screens begin.
func TestRunSelect_ResumingARun_AdoptsItsRecordedWorkflow(t *testing.T) {
	// Arrange
	candidates := []runscan.RunCandidate{
		makeCandidate("20260701T120000Z-a3f9", "/ws/Orchestration-20260701T120000Z-a3f9"),
	}
	m := newModelWithScan(candidates)
	if m.screen != screenRunSelect {
		t.Fatalf("precondition: screen = %v, want screenRunSelect", m.screen)
	}

	// Act: move past "Start a new run" onto the candidate and choose it.
	sendKey(m, tea.KeyDown)
	sendKey(m, tea.KeyEnter)

	// Assert
	if m.selections.workflowID != domain.WorkflowID(recordedWorkflowID) {
		t.Errorf("selections.workflowID = %q after resuming a run recorded under %q, want %q; "+
			"the recorded workflow must be adopted when the run is chosen, or the setup "+
			"sequence has nothing to run and must ask again",
			m.selections.workflowID, recordedWorkflowID, recordedWorkflowID)
	}
}

// ---------------------------------------------------------------------------
// The workflow question
// ---------------------------------------------------------------------------

// TestSetupFlow_ResumedRun_IsNotAskedWhichWorkflow verifies that the setup
// sequence never comes to rest on the workflow-selection screen for a resumed
// run.
//
// The run recorded a workflow when it was created and is being resumed as that
// workflow. Offering the question at all invites an answer that contradicts the
// run.
func TestSetupFlow_ResumedRun_IsNotAskedWhichWorkflow(t *testing.T) {
	// Arrange
	m := newResumedRunModel(t)

	// Act: answer the harness question, the last one a resumed run is asked.
	sendKey(m, tea.KeyEnter)

	// Assert
	if m.screen == screenSetupWorkflow {
		t.Errorf("the setup sequence stopped on the workflow-selection screen for a resumed " +
			"run; the workflow it recorded is already known and must not be asked for again")
	}
}

// TestSetupFlow_ResumedRun_KeepsTheWorkflowItRecorded verifies that a resumed
// run arrives at configuration still holding the workflow it was created under.
//
// The orchestrator fixture lists a different workflow first, so a sequence that
// shows the selection screen and takes an answer replaces the recorded value
// with that one and the run resumes as something it never was.
func TestSetupFlow_ResumedRun_KeepsTheWorkflowItRecorded(t *testing.T) {
	// Arrange
	m := newResumedRunModel(t)

	// Act
	sendKey(m, tea.KeyEnter)

	// Assert
	if m.selections.workflowID != domain.WorkflowID(recordedWorkflowID) {
		t.Errorf("selections.workflowID = %q, want %q; the workflow the run recorded was "+
			"replaced during setup, so the run would resume under a workflow it was never "+
			"created with", m.selections.workflowID, recordedWorkflowID)
	}
}

// TestSetupFlow_NewRun_IsAskedWhichWorkflow verifies that a new run is still
// asked which workflow to run, and that its answer is taken.
//
// A new run has recorded no workflow, so this question is the only thing that
// settles it. Skipping it would leave the run with nothing to execute.
func TestSetupFlow_NewRun_IsAskedWhichWorkflow(t *testing.T) {
	// Arrange
	m := newNewRunModelAtHarness(t)

	// Act: answer the harness question, then answer the workflow question.
	sendKey(m, tea.KeyEnter)
	if m.screen != screenSetupWorkflow {
		t.Fatalf("screen = %v after a new run answered the harness question, want "+
			"screenSetupWorkflow (%v); a new run has recorded no workflow and must be asked",
			m.screen, screenSetupWorkflow)
	}
	sendKey(m, tea.KeyEnter)

	// Assert
	if m.selections.workflowID != domain.WorkflowID(decoyWorkflowID) {
		t.Errorf("selections.workflowID = %q after a new run selected the first workflow "+
			"offered, want %q; the answer the user gave must be the one the run uses",
			m.selections.workflowID, decoyWorkflowID)
	}
	if m.screen != screenSetupTask {
		t.Errorf("screen = %v after a new run selected its workflow, want screenSetupTask (%v)",
			m.screen, screenSetupTask)
	}
}

// ---------------------------------------------------------------------------
// The task question
// ---------------------------------------------------------------------------

// TestSetupFlow_ResumedRun_IsNotAskedForTheTask verifies that the setup
// sequence never comes to rest on the task-entry screen for a resumed run, and
// that no task is collected from it.
//
// The task was entered when the run was created and lives in the run's
// artifact, which is where the session reads it from. The setup sequence has
// nothing to add and must not stop here.
func TestSetupFlow_ResumedRun_IsNotAskedForTheTask(t *testing.T) {
	// Arrange
	m := newResumedRunModel(t)

	// Act
	sendKey(m, tea.KeyEnter)

	// Assert
	if m.screen == screenSetupTask {
		t.Errorf("the setup sequence stopped on the task-entry screen for a resumed run; the " +
			"task it recorded is read from its artifact and must not be asked for again")
	}
	if m.selections.task != "" {
		t.Errorf("selections.task = %q for a resumed run, want empty; the task is read from "+
			"the run's artifact and must not be collected again during setup", m.selections.task)
	}
}

// TestSetupFlow_NewRun_IsAskedForTheTask verifies that a new run is still asked
// for its task description and that the entered text is taken.
func TestSetupFlow_NewRun_IsAskedForTheTask(t *testing.T) {
	// Arrange
	m := newTestModelNewRun()
	m.screen = screenSetupTask
	m.taskScreen.InputInit()

	// Act
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("build the thing")})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Assert
	if m.selections.task != "build the thing" {
		t.Errorf("selections.task = %q after a new run entered a task, want %q; a new run "+
			"has no recorded task and must be asked", m.selections.task, "build the thing")
	}
	if m.screen != screenSetupSeedInput {
		t.Errorf("screen = %v after a new run entered its task, want screenSetupSeedInput (%v)",
			m.screen, screenSetupSeedInput)
	}
}

// ---------------------------------------------------------------------------
// The sequence as a whole
// ---------------------------------------------------------------------------

// TestSetupFlow_ResumedRun_ReachesConfigurationDirectly verifies the two skips
// compose: after the harness question, a resumed run is standing at
// configuration.
//
// This is the user-visible statement of the whole change -- choose a run to
// continue, and the next thing you are asked about is how to run it.
func TestSetupFlow_ResumedRun_ReachesConfigurationDirectly(t *testing.T) {
	// Arrange
	m := newResumedRunModel(t)

	// Act
	sendKey(m, tea.KeyEnter)

	// Assert
	if m.screen != screenSetupConfig {
		t.Errorf("screen = %v, want screenSetupConfig (%v); a resumed run must reach "+
			"configuration without being asked for its workflow or its task",
			m.screen, screenSetupConfig)
	}
}

// TestRunSelect_ResumingARun_ReachesConfigurationWithoutAnsweringEitherQuestion
// walks the whole path a user takes: pick a run to continue from the list,
// answer the harness question, and arrive at configuration.
//
// The narrower tests above start from a model set up by hand, which leaves the
// seam between run selection and the setup sequence untested. This one crosses
// that seam using the model's own transitions, so a recorded workflow that is
// read correctly but never handed to the setup sequence is caught here.
func TestRunSelect_ResumingARun_ReachesConfigurationWithoutAnsweringEitherQuestion(t *testing.T) {
	// Arrange
	orchPath := writeOrchestratorFixture(t)
	candidates := []runscan.RunCandidate{
		makeCandidate("20260701T120000Z-a3f9", "/ws/Orchestration-20260701T120000Z-a3f9"),
	}
	m := newModelWithScanAndDiscoverer(candidates, func(string, string) (string, error) {
		return orchPath, nil
	})
	if m.screen != screenRunSelect {
		t.Fatalf("precondition: screen = %v, want screenRunSelect", m.screen)
	}

	// Act: choose the run to continue, then answer the harness question.
	sendKey(m, tea.KeyDown)
	sendKey(m, tea.KeyEnter)
	sendKey(m, tea.KeyEnter)

	// Assert
	if m.screen != screenSetupConfig {
		t.Errorf("screen = %v after choosing a run to continue and answering the harness "+
			"question, want screenSetupConfig (%v); continuing a run must lead straight to "+
			"configuration", m.screen, screenSetupConfig)
	}
	if m.selections.workflowID != domain.WorkflowID(recordedWorkflowID) {
		t.Errorf("selections.workflowID = %q, want %q; the workflow recorded by the chosen "+
			"run must survive the whole walk from the run list to configuration",
			m.selections.workflowID, recordedWorkflowID)
	}
	if m.selections.task != "" {
		t.Errorf("selections.task = %q, want empty; the task was never asked for and must "+
			"not have been collected", m.selections.task)
	}
}

// ---------------------------------------------------------------------------
// The entry point that settles run identity before the TUI launches
// ---------------------------------------------------------------------------

// newPreResolvedResumedRunModel returns a model built the way the production
// entry point builds one for `mosaic-run --run <run_id>`: identity already
// settled, no scan and no selection question, so the run-select screen never
// appears and the model starts at the harness question.
//
// recordedWorkflow is what the named run's artifact records; it is handed over
// through Options because this path has no run-select screen to pick it up on.
func newPreResolvedResumedRunModel(t *testing.T, recordedWorkflow domain.WorkflowID) *rootModel {
	t.Helper()
	orchPath := writeOrchestratorFixture(t)
	const runID = "20260701T120000Z-a3f9"
	sess := &stubNavSession{outcome: domain.RunOutcome{Status: domain.RunCompleted, Message: "ok"}}
	m := newRootModel(context.Background(), sess, Options{
		Theme:                  tuicommon.DefaultTheme(),
		Selection:              nil,
		ScanResult:             nil,
		ResolvedRunID:          runID,
		IsNewRun:               false,
		InitialRunFolder:       filepath.Join(t.TempDir(), "Orchestration-"+runID),
		RecordedWorkflowID:     recordedWorkflow,
		OrchestratorDiscoverer: func(string, string) (string, error) { return orchPath, nil },
	})
	if m.screen != screenSetupHarness {
		t.Fatalf("precondition: screen = %v, want screenSetupHarness; a pre-resolved run has "+
			"no run to select and must start at the harness question", m.screen)
	}
	return m
}

// TestSetupFlow_PreResolvedResumedRun_ReachesConfigurationWithItsRecordedWorkflow
// verifies that a resumed run whose identity was settled before the TUI
// launched -- `mosaic-run --run <run_id>` -- reaches configuration carrying the
// workflow its artifact records.
//
// This is the resume path that never touches the run-select screen. Every other
// resumed-run test here enters through that screen, or through a model with the
// workflow already placed in selections by hand, so both leave this entry point
// unwatched. It is the one place where skipping the workflow question takes
// something away without anything putting it back: the run arrives with no
// workflow, and the session refuses it for recording none -- against a run whose
// artifact records one perfectly well, which is the most misleading form the
// refusal can take.
func TestSetupFlow_PreResolvedResumedRun_ReachesConfigurationWithItsRecordedWorkflow(t *testing.T) {
	// Arrange
	m := newPreResolvedResumedRunModel(t, domain.WorkflowID(recordedWorkflowID))

	// Act: answer the harness question, the last one a resumed run is asked.
	sendKey(m, tea.KeyEnter)

	// Assert
	if m.selections.workflowID != domain.WorkflowID(recordedWorkflowID) {
		t.Errorf("selections.workflowID = %q for a run resumed by run_id, want %q; the run "+
			"records that workflow, so arriving without it has the session refuse a run that "+
			"has nothing wrong with it", m.selections.workflowID, recordedWorkflowID)
	}
	if m.screen != screenSetupConfig {
		t.Errorf("screen = %v, want screenSetupConfig (%v); resuming by run_id is still a "+
			"resume and must not be asked which workflow to run or what the task is",
			m.screen, screenSetupConfig)
	}
}

// TestSetupFlow_PreResolvedResumedRun_DoesNotSubstituteAnOfferedWorkflow is the
// counterpart guard: when the named run's artifact records no workflow, the
// setup sequence must not quietly supply one.
//
// The orchestrator fixture offers decoyWorkflowID first, so a sequence that
// falls back to the selection screen -- or to "whatever is listed first" --
// resumes the run as a workflow it was never created with. Refusing the run is
// the correct outcome here, and the session layer owns that refusal; setup's
// only obligation is not to paper over it.
func TestSetupFlow_PreResolvedResumedRun_DoesNotSubstituteAnOfferedWorkflow(t *testing.T) {
	// Arrange: a run whose artifact records no workflow.
	m := newPreResolvedResumedRunModel(t, "")

	// Act
	sendKey(m, tea.KeyEnter)

	// Assert
	if m.selections.workflowID != "" {
		t.Errorf("selections.workflowID = %q for a run that recorded no workflow, want empty; "+
			"a substituted workflow resumes the run as something it never was, which is worse "+
			"than refusing it", m.selections.workflowID)
	}
	// Coming to rest on the selection screen leaves workflowID empty at this
	// instant too, so the assertion above passes on exactly the shape it exists
	// to forbid: the user then picks decoyWorkflowID off that screen. Setup
	// carries the empty workflow through to configuration and the session layer
	// owns the refusal -- an empty recorded workflow is not a question to put
	// back to the user.
	if m.screen != screenSetupConfig {
		t.Errorf("screen = %v, want screenSetupConfig (%v); a resumed run recording no workflow "+
			"must still be carried past the workflow question -- offering the selection screen "+
			"here lets the user resume the run under a workflow it never had",
			m.screen, screenSetupConfig)
	}
}

// ---------------------------------------------------------------------------
// Back-navigation out of configuration
// ---------------------------------------------------------------------------

// TestSetupFlow_ResumedRun_BackFromConfiguration_ReturnsToTheHarnessQuestion
// verifies that stepping back from the first configuration prompt on a resumed
// run returns to the harness question.
//
// Back-navigation returns the user to the last question they were actually
// asked. For a resumed run the workflow and task questions were never put, so
// the harness question is that question; routing back to either skipped screen
// would show a resumed user a question the forward path exists to suppress, and
// showing it on the way back is no better than showing it on the way in.
func TestSetupFlow_ResumedRun_BackFromConfiguration_ReturnsToTheHarnessQuestion(t *testing.T) {
	// Arrange
	m := newResumedRunModel(t)
	sendKey(m, tea.KeyEnter)
	if m.screen != screenSetupConfig {
		t.Fatalf("precondition: screen = %v, want screenSetupConfig", m.screen)
	}

	// Act
	sendKey(m, tea.KeyEsc)

	// Assert
	if m.screen != screenSetupHarness {
		t.Errorf("screen = %v after stepping back from configuration on a resumed run, want "+
			"screenSetupHarness (%v); back-navigation must return to the last question the "+
			"user was asked, not to one the resume path deliberately skipped",
			m.screen, screenSetupHarness)
	}
}
