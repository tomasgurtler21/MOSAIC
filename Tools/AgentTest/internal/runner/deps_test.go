package runner_test

// Helpers that assemble a runner.Deps and runner.Request for a test, wiring
// the real workspace manager, fixture resolver and side-effect applier
// (nothing harness- or process-shaped about them) alongside the stub
// adapter, launcher and cost provider declared in testhelpers_test.go.

import (
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/fixtures"
	"mosaic-agent-test/internal/preflight"
	"mosaic-agent-test/internal/runner"
	"mosaic-agent-test/internal/sideeffects"
	"mosaic-agent-test/internal/workspace"
)

// recordingWorkspace decorates a real workspace.Manager, recording Create
// and Teardown calls so a test can assert their position in the overall
// setup/teardown sequence alongside the stub adapter and launcher.
type recordingWorkspace struct {
	inner workspace.Manager
	rec   *callRecorder
}

var _ workspace.Manager = (*recordingWorkspace)(nil)

func (w *recordingWorkspace) Create(key domain.RunKey) (domain.Sandbox, error) {
	w.rec.record("workspace.Create")
	return w.inner.Create(key)
}

func (w *recordingWorkspace) Locate(key domain.RunKey) (domain.Sandbox, error) {
	return w.inner.Locate(key)
}

func (w *recordingWorkspace) Teardown(s domain.Sandbox) error {
	w.rec.record("workspace.Teardown")
	return w.inner.Teardown(s)
}

func (w *recordingWorkspace) Reap(policy workspace.ReapPolicy) ([]workspace.ReapedSandbox, error) {
	return w.inner.Reap(policy)
}

// harness bundles everything one test needs to drive runner.Run: the
// wired-up Deps, the stub collaborators to script and inspect, and the
// shared call recorder.
type harness struct {
	Deps runner.Deps

	Adapter  *stubAdapter
	Launcher *stubLauncher
	Cost     *stubCostProvider
	Sink     *recordingSink
	Clock    *fakeClock
	Rec      *callRecorder
	Deployer *stubDeployer // the deployment port double, wired as Deps.Deploy

	FixtureRoot string
	// WorkspaceRoot is the directory the harness's own workspace.Manager is
	// rooted at, exposed so a test can construct a second, independent
	// Manager instance against the same directory — the shape a later
	// process rerunning the same suite actually takes.
	WorkspaceRoot string
}

// newHarness assembles a runner.Deps rooted at fresh temp directories, with
// a completed-by-default stub adapter and launcher. Individual tests
// override Adapter/Launcher/Cost fields before calling runner.Run.
func newHarness(t *testing.T) *harness {
	t.Helper()

	rec := &callRecorder{}
	clock := newFakeClock()

	workspaceRoot := t.TempDir()
	fixtureRoot := t.TempDir()

	realWs := workspace.NewManager(workspaceRoot, clock)
	ws := &recordingWorkspace{inner: realWs, rec: rec}

	resolver, err := fixtures.NewResolver(fixtureRoot)
	if err != nil {
		t.Fatalf("fixtures.NewResolver(%q) returned unexpected error: %v", fixtureRoot, err)
	}
	effects := sideeffects.NewApplier(resolver)

	adapter := &stubAdapter{
		rec: rec,
		plan: domain.SpawnPlan{
			Executable: "stub-subject",
			Timeout:    time.Minute,
		},
	}
	launcher := &stubLauncher{rec: rec}
	costProvider := &stubCostProvider{rec: rec, report: domain.CostReport{Attribution: domain.AttributionAttributed}}
	sink := &recordingSink{}
	deployer := &stubDeployer{}

	return &harness{
		Deps: runner.Deps{
			Workspaces: ws,
			Adapter:    adapter,
			Launcher:   launcher,
			Fixtures:   resolver,
			Effects:    effects,
			Cost:       costProvider,
			Clock:      clock,
			Progress:   sink,
			Deploy:     deployer,
		},
		Adapter:       adapter,
		Launcher:      launcher,
		Cost:          costProvider,
		Sink:          sink,
		Clock:         clock,
		Rec:           rec,
		Deployer:      deployer,
		FixtureRoot:   fixtureRoot,
		WorkspaceRoot: workspaceRoot,
	}
}

// newRequest builds a minimal runner.Request for a subagent-layer test with
// no assertions declared, so a test can add exactly the fields it cares
// about on top.
func newRequest(testID string) runner.Request {
	return runner.Request{
		Key: domain.RunKey{RunID: "run-" + testID, TestID: testID, RunNumber: 1},
		Test: preflight.ResolvedTest{
			Definition: domain.TestDefinition{
				ID:    testID,
				Layer: domain.LayerSubagent,
				Subject: domain.SubjectUnderTest{
					Identity:       "researcher",
					DefinitionPath: "researcher.md",
					OpeningMessage: "start",
				},
			},
			Registry: domain.StubRegistry{TestID: testID},
		},
		Settings: domain.RunSettings{},
		// RetainNever pins the default policy explicitly, so a test built
		// from this helper is never left depending on RetentionPolicy's
		// zero value ("") meaning the same thing.
		Retention: domain.RetainNever,
	}
}
