package main

// Follow-up tests for AC17.5 ("every concrete adapter, store and provider is
// constructed in the composition root and nowhere else; both frontends
// receive the same wired set") against the WiringConfig/Deps/buildDeps/
// newSuiteRunner/RunnerDeps/cliOptions/tuiOptions contract.
//
// These are white-box tests against package main's own unexported wiring
// surface, for the same reason main_test.go's dispatch tests are: that
// surface is what this stage must specify, and "compiles and fails" is
// verifiable without spawning a real process.
//
// The contract names five identity-comparable properties that discharge
// AC17.5 (see ContractsDesign.md, cmd/mosaic-agent-test):
//  1. cliOptions(d, …) and tuiOptions(d, …) are called with the same d and
//     read nothing else.
//  2. cliOptions(d, …).Suite(rc) and newSuiteRunner(d, rc) wire the
//     identical runner.Deps for the same rc.
//  3. tuiOptions(d, …).Suite is newSuiteRunner(d, cli.RunConfig{WorkspaceRoot:
//     d.WorkspaceRoot}), not a second resolution of the root.
//  4. buildDeps returns a Deps with no zero-valued field on the success path.
//  5. d.Decoder is decoderFor(d.HarnessID) and d.Launcher was constructed
//     with that value (AC17.9; already covered directly by
//     TestDecoderFor_KnownHarness_MatchesTheAdapterPackagesDecoder in
//     main_test.go, so it is not repeated here).
//
// RunnerDeps exists, per its own doc comment, so a test can assert
// field-for-field that what the CLI path runs with and what the TUI path
// runs with are the same values — that is the seam properties 2 and 4 are
// driven through here, since the CLI and TUI SuiteRunner values are opaque
// behind an interface a wiring test must not reach inside.

import (
	"context"
	"errors"
	"reflect"
	"runtime"
	"testing"
	"time"

	"mosaic-agent-test/internal/cli"
	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/harness/claudecode"
	"mosaic-agent-test/internal/harness/fake"
	"mosaic-agent-test/internal/preflight"
	"mosaic-agent-test/internal/workspace"
)

// ---------------------------------------------------------------------------
// Fakes: minimal stand-ins for the collaborator ports Deps carries, used only
// so two Deps fields can be compared by identity. None perform real I/O.
// ---------------------------------------------------------------------------

type fakeLauncher struct{}

func (fakeLauncher) Launch(ctx context.Context, plan domain.SpawnPlan) (domain.SubjectResult, error) {
	return domain.SubjectResult{}, errors.New("fakeLauncher: not exercised by this test")
}

type fakeResolver struct{}

func (fakeResolver) Resolve(ref string) ([]byte, error) {
	return nil, errors.New("fakeResolver: not exercised")
}
func (fakeResolver) Validate(ref string) error { return errors.New("fakeResolver: not exercised") }

type fakeApplier struct{}

func (fakeApplier) Apply(subjectDir string, effects []domain.FileEffect) (domain.EffectLedger, error) {
	return domain.EffectLedger{}, errors.New("fakeApplier: not exercised")
}

type fakeCostProvider struct{}

func (fakeCostProvider) Cost(ctx context.Context, q domain.CostQuery) (domain.CostReport, error) {
	return domain.CostReport{}, errors.New("fakeCostProvider: not exercised")
}

type fakeClock struct{ t time.Time }

func (c fakeClock) Now() time.Time { return c.t }

type fakeProgressSink struct{}

func (fakeProgressSink) Emit(ev domain.ProgressEvent) {}

type discardWriter struct{}

func (*discardWriter) Write(p []byte) (int, error) { return len(p), nil }

// testDeps builds a Deps whose fields are every one distinguishable from a
// zero value, using real named function values (claudecode.DecodeEnvelope,
// preflight.Validate) where the field type is a func, so identity
// comparisons in these tests are meaningful rather than accidental.
func testDeps(t *testing.T) Deps {
	t.Helper()
	return Deps{
		Adapter:   fake.New(fake.Options{}),
		Decoder:   claudecode.DecodeEnvelope,
		Launcher:  fakeLauncher{},
		Fixtures:  fakeResolver{},
		Effects:   fakeApplier{},
		Cost:      fakeCostProvider{},
		Clock:     fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
		Preflight: preflight.Validate,

		SelfPath:        "C:/bin/mosaic-agent-test.exe",
		LoggerBundleDir: "C:/bundles/logger",

		HarnessID:     "claude-code",
		FixtureRoot:   "C:/fixtures",
		WorkspaceRoot: "C:/workspaces",
	}
}

func funcName(fn interface{}) string {
	if fn == nil {
		return "<nil>"
	}
	return runtime.FuncForPC(reflect.ValueOf(fn).Pointer()).Name()
}

// ---------------------------------------------------------------------------
// AC17.5, property 4 — buildDeps leaves no zero-valued field on success.
// ---------------------------------------------------------------------------

// TestBuildDeps_PopulatesEveryField_OnSuccess verifies buildDeps is total: a
// dependency added to Deps and forgotten in buildDeps must fail this test
// rather than reach a frontend as a nil port.
func TestBuildDeps_PopulatesEveryField_OnSuccess(t *testing.T) {
	cfg := WiringConfig{
		HarnessID:       "claude-code",
		FixtureRoot:     "C:/fixtures",
		WorkspaceRoot:   "C:/workspaces",
		SelfPath:        "C:/bin/mosaic-agent-test.exe",
		LoggerBundleDir: "C:/bundles/logger",
		CostToolPath:    "C:/bin/mosaic-log-analyzer.exe",
		CostTimeout:     30 * time.Second,
		Diag:            new(discardWriter),
	}

	d, err := buildDeps(cfg)
	if err != nil {
		t.Fatalf("buildDeps(%+v) returned an error: %v", cfg, err)
	}

	rv := reflect.ValueOf(d)
	rt := rv.Type()
	for i := 0; i < rv.NumField(); i++ {
		field := rv.Field(i)
		if field.IsZero() {
			t.Errorf("buildDeps returned a zero-valued field %q; every field must be populated on the success path (AC17.5)", rt.Field(i).Name)
		}
	}
}

// TestBuildDeps_UnrecognisedHarnessID_IsErrUnknownHarness verifies buildDeps
// itself decides the AC17.6 usage error, not merely newAdapter/decoderFor in
// isolation: buildDeps's own doc comment states "An unrecognised HarnessID
// surfaces here as ErrUnknownHarness, so the usage error is decided before
// any frontend starts." A buildDeps that constructed some other collaborator
// before checking the harness, or that wrapped a different error, would pass
// TestNewAdapter_UnrecognisedHarness_IsUsageError and
// TestDecoderFor_UnrecognisedHarness_IsError while still failing this
// contract, since neither of those tests goes through buildDeps.
func TestBuildDeps_UnrecognisedHarnessID_IsErrUnknownHarness(t *testing.T) {
	cfg := WiringConfig{
		HarnessID:       "not-a-real-harness",
		FixtureRoot:     "C:/fixtures",
		WorkspaceRoot:   "C:/workspaces",
		SelfPath:        "C:/bin/mosaic-agent-test.exe",
		LoggerBundleDir: "C:/bundles/logger",
		CostToolPath:    "C:/bin/mosaic-log-analyzer.exe",
		CostTimeout:     30 * time.Second,
		Diag:            new(discardWriter),
	}

	d, err := buildDeps(cfg)

	if err == nil {
		t.Fatal("buildDeps with an unrecognised HarnessID returned a nil error; the usage error must be decided before any frontend starts")
	}
	if !errors.Is(err, ErrUnknownHarness) {
		t.Errorf("buildDeps error = %v, want it to wrap %v", err, ErrUnknownHarness)
	}
	if !reflect.ValueOf(d).IsZero() {
		t.Errorf("buildDeps with an unrecognised HarnessID returned a non-zero Deps %+v; a failed build must not hand back a partially wired value", d)
	}
}

// ---------------------------------------------------------------------------
// AC17.5, property 1 — cliOptions and tuiOptions are pure projections of the
// same Deps.
// ---------------------------------------------------------------------------

// TestCLIOptionsAndTUIOptions_ProjectTheSameDepsFields verifies both
// frontend projections read their resolved defaults, preflight function and
// harness identity straight off the one Deps passed to each — never from
// anywhere else — so a value distinguishable in d shows up identically in
// both projections.
func TestCLIOptionsAndTUIOptions_ProjectTheSameDepsFields(t *testing.T) {
	d := testDeps(t)

	var stdout, stderr discardWriter
	cliOpts := cliOptions(d, &stdout, &stderr)
	tuiOpts, err := tuiOptions(d, []string{"suite.yaml"})
	if err != nil {
		t.Fatalf("tuiOptions(d, ...) returned an error: %v", err)
	}

	if cliOpts.WorkspaceRoot != d.WorkspaceRoot {
		t.Errorf("cliOptions(d, ...).WorkspaceRoot = %q, want d.WorkspaceRoot %q", cliOpts.WorkspaceRoot, d.WorkspaceRoot)
	}
	if cliOpts.FixtureRoot != d.FixtureRoot {
		t.Errorf("cliOptions(d, ...).FixtureRoot = %q, want d.FixtureRoot %q", cliOpts.FixtureRoot, d.FixtureRoot)
	}
	if cliOpts.DefaultHarness != d.HarnessID {
		t.Errorf("cliOptions(d, ...).DefaultHarness = %q, want d.HarnessID %q", cliOpts.DefaultHarness, d.HarnessID)
	}
	if tuiOpts.Harness != d.HarnessID {
		t.Errorf("tuiOptions(d, ...).Harness = %q, want d.HarnessID %q", tuiOpts.Harness, d.HarnessID)
	}

	if cliOpts.Preflight == nil {
		t.Fatal("cliOptions(d, ...).Preflight is nil, want d.Preflight")
	}
	if tuiOpts.Preflight == nil {
		t.Fatal("tuiOptions(d, ...).Preflight is nil, want d.Preflight")
	}
	wantPreflight := funcName(d.Preflight)
	if got := funcName(cliOpts.Preflight); got != wantPreflight {
		t.Errorf("cliOptions(d, ...).Preflight = %s, want d.Preflight %s", got, wantPreflight)
	}
	if got := funcName(tuiOpts.Preflight); got != wantPreflight {
		t.Errorf("tuiOptions(d, ...).Preflight = %s, want d.Preflight %s", got, wantPreflight)
	}
}

// ---------------------------------------------------------------------------
// AC17.5, property 2 & 3 — both frontends' suites are wired through
// newSuiteRunner, never independently.
// ---------------------------------------------------------------------------

// TestCLIOptionsSuite_DelegatesToNewSuiteRunner verifies cliOptions(d, ...).
// Suite is a factory that, called with an invocation's RunConfig, wires the
// suite through newSuiteRunner rather than constructing one of its own — the
// two must fail identically for a configuration newSuiteRunner rejects,
// which is observable now even though a passing wiring is not yet.
func TestCLIOptionsSuite_DelegatesToNewSuiteRunner(t *testing.T) {
	d := testDeps(t)
	rc := cli.RunConfig{WorkspaceRoot: "C:/workspaces/run-1"}

	var stdout, stderr discardWriter
	factory := cliOptions(d, &stdout, &stderr).Suite
	if factory == nil {
		t.Fatal("cliOptions(d, ...).Suite is nil; the CLI frontend must receive a suite factory bound to d")
	}

	_, factoryErr := factory(rc)
	_, directErr := newSuiteRunner(d, rc)

	if (factoryErr == nil) != (directErr == nil) {
		t.Errorf("cliOptions(d, ...).Suite(rc) error = %v, newSuiteRunner(d, rc) error = %v; the CLI factory must delegate to newSuiteRunner rather than wiring independently", factoryErr, directErr)
	}
}

// TestTUIOptionsSuite_IsNewSuiteRunnerWithComposedDefaultRoot verifies the
// interactive frontend's suite is newSuiteRunner(d, cli.RunConfig{
// WorkspaceRoot: d.WorkspaceRoot}) — the composed default root, not a second
// resolution of it. tuiOptions has no flag of its own to resolve a root
// from, so any root it wires must be d's.
func TestTUIOptionsSuite_IsNewSuiteRunnerWithComposedDefaultRoot(t *testing.T) {
	d := testDeps(t)

	_, wantErr := newSuiteRunner(d, cli.RunConfig{WorkspaceRoot: d.WorkspaceRoot})

	tuiOpts, gotErr := tuiOptions(d, []string{"suite.yaml"})

	// tuiOptions must surface newSuiteRunner's own failure as its error
	// rather than silently handing the TUI a nil or half-wired Suite: a
	// second, independent resolution of the root would let this pass with a
	// Suite that was never proven to be d's.
	if (gotErr == nil) != (wantErr == nil) {
		t.Errorf("tuiOptions(d, ...) error = %v, newSuiteRunner(d, RunConfig{WorkspaceRoot: d.WorkspaceRoot}) error = %v; tuiOptions must delegate rather than wire independently", gotErr, wantErr)
	}
	if wantErr == nil && tuiOpts.Suite == nil {
		t.Error("newSuiteRunner(d, RunConfig{WorkspaceRoot: d.WorkspaceRoot}) succeeded but tuiOptions(d, ...) produced a nil Suite")
	}
	if wantErr != nil && tuiOpts.Suite != nil {
		t.Errorf("newSuiteRunner(d, RunConfig{WorkspaceRoot: d.WorkspaceRoot}) failed (%v) but tuiOptions(d, ...) produced a non-nil Suite", wantErr)
	}
}

// ---------------------------------------------------------------------------
// AC17.5 (RunnerDeps) — the per-attempt collaborator set is a pure,
// field-for-field projection of Deps.
// ---------------------------------------------------------------------------

// TestDeps_RunnerDeps_ProjectsEveryCollaboratorFieldUnchanged verifies
// RunnerDeps reads every collaborator straight off d: the CLI path and the
// TUI path both call this on the identical d, so if the projection is field
// faithful, both frontends necessarily run with the same wired set. This is
// the seam RunnerDeps's own doc comment names for exactly this assertion.
func TestDeps_RunnerDeps_ProjectsEveryCollaboratorFieldUnchanged(t *testing.T) {
	d := testDeps(t)
	ws := workspace.NewManager(t.TempDir(), d.Clock)
	progress := fakeProgressSink{}

	got := d.RunnerDeps(ws, progress)

	if got.Adapter != d.Adapter {
		t.Error("RunnerDeps(ws, progress).Adapter is not d.Adapter")
	}
	if got.Launcher != d.Launcher {
		t.Error("RunnerDeps(ws, progress).Launcher is not d.Launcher")
	}
	if got.Fixtures != d.Fixtures {
		t.Error("RunnerDeps(ws, progress).Fixtures is not d.Fixtures")
	}
	if got.Effects != d.Effects {
		t.Error("RunnerDeps(ws, progress).Effects is not d.Effects")
	}
	if got.Cost != d.Cost {
		t.Error("RunnerDeps(ws, progress).Cost is not d.Cost")
	}
	if got.Clock != d.Clock {
		t.Error("RunnerDeps(ws, progress).Clock is not d.Clock")
	}
	if got.SelfPath != d.SelfPath {
		t.Errorf("RunnerDeps(ws, progress).SelfPath = %q, want d.SelfPath %q", got.SelfPath, d.SelfPath)
	}
	if got.LoggerBundleDir != d.LoggerBundleDir {
		t.Errorf("RunnerDeps(ws, progress).LoggerBundleDir = %q, want d.LoggerBundleDir %q", got.LoggerBundleDir, d.LoggerBundleDir)
	}
	if got.Workspaces != ws {
		t.Error("RunnerDeps(ws, progress).Workspaces is not the ws argument passed in")
	}
	if got.Progress != progress {
		t.Error("RunnerDeps(ws, progress).Progress is not the progress argument passed in")
	}
}

// TestDeps_RunnerDeps_IsIndependentOfWhichWorkspaceAndProgressAreGiven
// verifies the collaborator fields RunnerDeps yields depend only on d, never
// on which workspace manager or progress sink happen to be passed in — the
// property that makes "same d, same collaborators" hold across every call
// site, including two different attempts within the same run.
//
// This passes trivially against today's wrong-on-purpose RunnerDeps stub
// (both calls return the same zero value), the same way the composition-root
// exemption's "importing adapter passes the gate" test does before cmd/ is
// scanned — see this stage's PlanProgress.md notes. It stays alongside
// TestDeps_RunnerDeps_ProjectsEveryCollaboratorFieldUnchanged, which fails
// today and pins the real requirement.
func TestDeps_RunnerDeps_IsIndependentOfWhichWorkspaceAndProgressAreGiven(t *testing.T) {
	d := testDeps(t)
	ws1 := workspace.NewManager(t.TempDir(), d.Clock)
	ws2 := workspace.NewManager(t.TempDir(), d.Clock)

	got1 := d.RunnerDeps(ws1, fakeProgressSink{})
	got2 := d.RunnerDeps(ws2, fakeProgressSink{})

	if got1.Adapter != got2.Adapter || got1.Launcher != got2.Launcher ||
		got1.Fixtures != got2.Fixtures || got1.Effects != got2.Effects ||
		got1.Cost != got2.Cost || got1.Clock != got2.Clock ||
		got1.SelfPath != got2.SelfPath || got1.LoggerBundleDir != got2.LoggerBundleDir {
		t.Errorf("d.RunnerDeps varied its collaborator fields across two calls on the same d:\nfirst:  %+v\nsecond: %+v", got1, got2)
	}
}
